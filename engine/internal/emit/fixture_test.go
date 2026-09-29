package emit

import (
	"fmt"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/gates"
	"poolguide/internal/mine"
	"poolguide/internal/store"
)

const (
	gatesPath   = "../../../picker/gates.json"
	rosterPath  = "../../../ref/dota2/synergy/raw/_roster.json"
	fixtureDay  = "2026-09-24"
)

// rosterEntry is one committed stratz roster cache hero.
type rosterEntry struct{ Slug, NPC string }

// readRoster reads the committed stratz roster cache in file order.
func readRoster(t *testing.T) []rosterEntry {
	t.Helper()
	raw, err := os.ReadFile(rosterPath)
	if err != nil {
		t.Fatalf("read roster cache: %v", err)
	}
	var doc struct {
		Heroes []rosterEntry `json:"heroes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse roster cache: %v", err)
	}
	return doc.Heroes
}

// rosterNPCs maps roster slugs to npcs so fixture npcs are the real ones:
// slug-derived npcs would mask cdn divergences (lifestealer ships as
// life_stealer, necrophos as necrolyte) behind the goldens.
func rosterNPCs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range readRoster(t) {
		out[r.Slug] = r.NPC
	}
	return out
}

func fixtureNPC(roster map[string]string, slug string) string {
	if npc, ok := roster[slug]; ok {
		return npc
	}
	return config.NPCPrefix + strings.ReplaceAll(slug, "-", "_")
}

func fixtureCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load hub config: %v", err)
	}
	return cfg
}

func fixtureGates(t *testing.T) *gates.Doc {
	t.Helper()
	doc, err := gates.Load(gatesPath)
	if err != nil {
		t.Fatalf("load gates: %v", err)
	}
	return doc
}

// fixtureEnemySlugs takes the first six roster-cache heroes outside the pool,
// in roster file order, mirroring the derived enemy columns' source. Six
// enemy columns give every pool hero enough observed extremes for the guide
// z cut to keep minKept rows.
func fixtureEnemySlugs(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	pool := map[string]bool{}
	for _, s := range cfg.PoolSlugs() {
		pool[s] = true
	}
	var out []string
	for _, r := range readRoster(t) {
		if pool[r.Slug] {
			continue
		}
		out = append(out, r.Slug)
		if len(out) == 6 {
			return out
		}
	}
	t.Fatalf("roster cache has fewer than 6 non-pool heroes")
	return nil
}

// fixtureRosterSlugs is the pool plus the fixture enemy columns, so the
// roster stays unique when the heatmap columns join the pool.
func fixtureRosterSlugs(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	return append(append([]string(nil), cfg.PoolSlugs()...), fixtureEnemySlugs(t, cfg)...)
}

// buildFixtureDB seeds a small but complete derived-state database: the real
// hub pool plus six heatmap enemies as the roster, with hero ids assigned
// against slug order so the hero_id ordering is actually exercised.
func buildFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := fixtureCfg(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "fixture.duckdb"))
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := mine.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	slugs := fixtureRosterSlugs(t, cfg)
	roster := rosterNPCs(t)
	for i, slug := range slugs {
		execFixture(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES (?, ?, ?, ?, 'test')`,
			100-i, slug, strings.ReplaceAll(slug, "-", " "), fixtureNPC(roster, slug))
	}
	total := float64(len(slugs) * (len(slugs) + 1) / 2)
	for i, slug := range slugs {
		execFixture(t, db, `INSERT INTO popularity (slug, pick_share, source) VALUES (?, ?, 'hero_stats')`,
			slug, float64(i+1)/total)
	}
	pool := cfg.PoolSlugs()
	for i, slug := range pool {
		execFixture(t, db, `INSERT INTO hero_prior (slug, wr, tier, value) VALUES (?, ?, ?, ?)`,
			slug, 0.4+0.02*float64(i), "dedicated", -1+0.2*float64(i))
	}
	confLabels := []string{"high", "medium", "low"}
	for i, hero := range pool {
		for j, other := range slugs {
			for _, kind := range []string{mine.KindEnemy, mine.KindAlly} {
				src := "stratz"
				if i == 0 {
					src = "opendota_explorer"
				}
				execFixture(t, db, `INSERT INTO pair_shrunk (hero, other, kind, value, n, post_var, source, confidence)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					hero, other, kind, float64(j)-3, int64(120+j), 1.5, src, confLabels[(i+j)%3])
				execFixture(t, db, `INSERT INTO norm_cell (hero, other, kind, pct, z, completed)
					VALUES (?, ?, ?, ?, ?, ?)`,
					hero, other, kind, float64(j+1)/float64(len(slugs)+1),
					(float64(j)-float64(len(slugs)-1)/2)*0.75, j%5 == 0)
			}
		}
	}
	// position stats for the first two pool heroes: shares of the hero's own
	// picks per farm position, sorted share-descending at load
	for i, stats := range [][][3]int64{
		{{1, 600, 320}, {2, 300, 150}, {3, 100, 40}},
		{{4, 100, 55}},
	} {
		for _, s := range stats {
			execFixture(t, db, `INSERT INTO hero_position (slug, position, pick_count, win_count, source, scrape_date)
				VALUES (?, ?, ?, ?, 'stratz_winday', ?)`,
				pool[i], s[0], s[1], s[2], fixtureDay)
		}
	}
	// stratz_vs rows carry the crawl dates the guide provenance note reads;
	// the values themselves are irrelevant here (norm_cell z above is the
	// guide's matchup source), any roster enemies outside the pool serve
	synValues := []float64{4.02, -3.13, 1.49, 2.5, -1.5}
	enemies := slugs[len(cfg.PoolSlugs()):]
	for _, slug := range cfg.PoolSlugs()[:2] {
		for j, enemy := range enemies[:len(synValues)] {
			execFixture(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, synergy, win_count, matches, source, scrape_date, confidence)
				VALUES (?, ?, ?, 50, 100, 'stratz_vs', ?, 'high')`,
				slug, enemy, synValues[j], fixtureDay)
		}
	}
	return db
}

func execFixture(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

// TestMineToEmitPipelineSmoke runs the real mining step over a raw fixture
// database and formats both artifacts from the derived tables it wrote.
func TestMineToEmitPipelineSmoke(t *testing.T) {
	cfg := fixtureCfg(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "pipeline.duckdb"))
	if err != nil {
		t.Fatalf("open pipeline db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := mine.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	slugs := fixtureRosterSlugs(t, cfg)
	roster := rosterNPCs(t)
	for i, slug := range slugs {
		execFixture(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES (?, ?, ?, ?, 'test')`,
			100-i, slug, strings.ReplaceAll(slug, "-", " "), fixtureNPC(roster, slug))
		execFixture(t, db, `INSERT INTO hero_stats (slug, bracket, pick_count, win_count, source, scrape_date)
			VALUES (?, ?, ?, ?, 'stratz', ?)`,
			slug, cfg.Scope.Bracket, int64(200+13*i), int64(100+5*i), fixtureDay)
	}
	// stratz_vs enemy rows for every pool hero over the roster enemy columns
	// with realistic match counts so the shrunk cells spread wide enough for
	// the guide z cut, deltas staggered per hero; the backup row on an
	// already-covered pair proves the lower tier never feeds it under strict
	// source priority
	enemyCols := fixtureEnemySlugs(t, cfg)
	deltas := []float64{4.02, -3.13, 2.5, -1.8}
	for i, hero := range cfg.PoolSlugs() {
		for j, enemy := range enemyCols {
			execFixture(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, synergy, win_count, matches, source, scrape_date, confidence)
				VALUES (?, ?, ?, 1500, 3000, 'stratz_vs', ?, 'high')`,
				hero, enemy, deltas[(i+j)%len(deltas)], fixtureDay)
		}
	}
	execFixture(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, win_count, matches, source, scrape_date, confidence)
		VALUES (?, ?, 60, 100, 'opendota_explorer', ?, 'high')`,
		cfg.PoolSlugs()[0], enemyCols[0], fixtureDay)
	execFixture(t, db, `INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
		VALUES (?, ?, 100, 3.0, 'stratz'), (?, ?, 25, 1.0, 'stratz')`,
		cfg.PoolSlugs()[0], cfg.PoolSlugs()[2], cfg.PoolSlugs()[2], cfg.PoolSlugs()[0])

	if err := mine.Run(mine.Deps{DB: db, Cfg: cfg, RepoRoot: t.TempDir()}); err != nil {
		t.Fatalf("mine run: %v", err)
	}

	// the enemy columns mine derives: hero_stats picks ascend with the slug
	// index (pool first, enemies last), so the six enemies own the top ranks
	// in reverse roster order
	var ranked []string
	rrows, err := db.Query(`SELECT slug FROM heatmap_enemies ORDER BY rank`)
	if err != nil {
		t.Fatalf("query heatmap_enemies: %v", err)
	}
	for rrows.Next() {
		var slug string
		if err := rrows.Scan(&slug); err != nil {
			rrows.Close()
			t.Fatal(err)
		}
		ranked = append(ranked, slug)
	}
	if err := rrows.Err(); err != nil {
		t.Fatalf("scan heatmap_enemies: %v", err)
	}
	rrows.Close()
	if len(ranked) != cfg.Heatmap.EnemyCount {
		t.Fatalf("heatmap_enemies holds %d rows, want %d", len(ranked), cfg.Heatmap.EnemyCount)
	}
	for i := 0; i < len(enemyCols); i++ {
		if ranked[i] != enemyCols[len(enemyCols)-1-i] {
			t.Fatalf("heatmap column %d = %s, want %s", i+1, ranked[i], enemyCols[len(enemyCols)-1-i])
		}
	}
	dataOut, err := DataJSFromFiles(db, hubPath, prosePath)
	if err != nil {
		t.Fatalf("DataJSFromFiles over the mined tables: %v", err)
	}
	for _, slug := range enemyCols {
		if !strings.Contains(string(dataOut), "\n    '"+slug+"',\n") {
			t.Fatalf("data.js heatmap block misses derived enemy %s", slug)
		}
	}
	guideIn, err := LoadGuide(db, cfg)
	if err != nil {
		t.Fatalf("load guide: %v", err)
	}
	guideIn.Date = fixtureDay
	guideOut, err := FormatGuide(guideIn, cfg)
	if err != nil {
		t.Fatalf("format guide: %v", err)
	}
	if !strings.Contains(string(guideOut), "'"+cfg.PoolSlugs()[0]+"': '") {
		t.Fatalf("guide output misses the mined-coverage pool hero")
	}
	pickerIn, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	pickerIn.Prose = prose.Heroes
	pickerIn.Date = fixtureDay
	pickerOut, err := FormatPicker(pickerIn, cfg)
	if err != nil {
		t.Fatalf("format picker: %v", err)
	}
	rosterCount := len(fixtureRosterSlugs(t, cfg))
	if !strings.Contains(string(pickerOut), fmt.Sprintf("rosterCount: %d", rosterCount)) {
		t.Fatalf("picker output misses the roster count line")
	}
	if a, b := string(pickerOut), func() string {
		out, _ := FormatPicker(pickerIn, cfg)
		return string(out)
	}(); a != b {
		t.Fatalf("picker output over mined tables is not deterministic")
	}
}
