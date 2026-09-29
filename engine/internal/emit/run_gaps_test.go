package emit

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/mine"
	"poolguide/internal/store"
)

// bareDB opens a duckdb carrying the full store+mine schema and no rows, so
// loader query-error arms can drop exactly one table each and success arms
// can seed only the rows they assert on.
func bareDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "bare.duckdb"))
	if err != nil {
		t.Fatalf("open bare db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := mine.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return db
}

func TestWriteFileCreatesParentsAndWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "out.js")
	if err := WriteFile(path, []byte("payload")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "payload" {
		t.Fatalf("written bytes = %q, %v", b, err)
	}
}

func TestWriteFileErrorPaths(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker.txt")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(filepath.Join(blocker, "child.js"), nil); err == nil {
		t.Fatal("write under a file path must fail at mkdir")
	}
	if err := WriteFile(dir, nil); err == nil {
		t.Fatal("write onto a directory must fail")
	}
}

// TestPickerWrapperCoversSmallPool runs the one-step Picker over a two-hero
// pool so the wrapper's load/format wiring is exercised without the full
// fixture seeding the golden tests pay for.
func TestPickerWrapperCoversSmallPool(t *testing.T) {
	cfg := fixtureCfg(t)
	two := cfg.Pool[:2]
	cfg.Pool = two
	db := bareDB(t)
	roster := rosterNPCs(t)
	for i, e := range two {
		execFixture(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES (?, ?, ?, ?, 'test')`,
			i+1, e.Slug, strings.ReplaceAll(e.Slug, "-", " "), fixtureNPC(roster, e.Slug))
		execFixture(t, db, `INSERT INTO popularity (slug, pick_share, source) VALUES (?, ?, 'hero_stats')`, e.Slug, 0.5)
		execFixture(t, db, `INSERT INTO hero_prior (slug, wr, tier, value) VALUES (?, 0.5, 'dedicated', 0.5)`, e.Slug)
		for _, other := range two {
			for _, kind := range []string{mine.KindEnemy, mine.KindAlly} {
				execFixture(t, db, `INSERT INTO norm_cell (hero, other, kind, pct, z, completed)
					VALUES (?, ?, ?, 0.5, 0.25, false)`, e.Slug, other.Slug, kind)
			}
		}
	}
	// one trend row so the wrapper also carries the trend line shape through
	execFixture(t, db, `INSERT INTO hero_trend_delta (slug, wr_delta_pp, share_delta_pp, from_date, to_date)
		VALUES (?, 1.5, -0.25, '2026-09-20', '2026-09-27')`, two[0].Slug)
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	out, err := Picker(db, cfg, fixtureGates(t), prose, fixtureDay)
	if err != nil {
		t.Fatalf("picker wrapper: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, pickerHeader) || !strings.Contains(s, "rosterCount: 2") {
		t.Fatalf("picker wrapper output misses the header or roster count")
	}
	if !strings.Contains(s, "    [1.5000, -0.2500],") {
		t.Fatalf("picker wrapper output misses the trend pair:\n%s", s)
	}
	for _, e := range two {
		if !strings.Contains(s, "'"+e.Slug+"'") {
			t.Fatalf("picker wrapper output misses pool hero %s", e.Slug)
		}
	}
}

func TestPickerWrapperFailsOnClosedDB(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	db.Close()
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	if _, err := Picker(db, cfg, fixtureGates(t), prose, fixtureDay); err == nil {
		t.Fatal("picker wrapper must fail over a closed db")
	}
}

// the guide wrapper needs nothing but the mined overall win rates: with an
// empty matchup surface it still ships every pool hero's overallWr line.
func TestGuideWrapperCoversPoolWinRates(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	for _, slug := range cfg.PoolSlugs() {
		execFixture(t, db, `INSERT INTO hero_prior (slug, wr, tier, value) VALUES (?, 0.5, 'dedicated', 0.5)`, slug)
	}
	out, err := Guide(db, cfg, fixtureDay)
	if err != nil {
		t.Fatalf("guide wrapper: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, guideHeaderLine1) || !strings.Contains(s, "date: '"+fixtureDay+"'") {
		t.Fatalf("guide wrapper output misses the header or date line")
	}
	for _, slug := range cfg.PoolSlugs() {
		if !strings.Contains(s, "'"+slug+"': 50,") {
			t.Fatalf("guide wrapper output misses the overall win rate for %s", slug)
		}
	}
}

func TestGuideWrapperFailsOnClosedDB(t *testing.T) {
	db := bareDB(t)
	db.Close()
	if _, err := Guide(db, fixtureCfg(t), fixtureDay); err == nil {
		t.Fatal("guide wrapper must fail over a closed db")
	}
}

func TestDataJSFromFilesErrorPaths(t *testing.T) {
	db := bareDB(t)
	if _, err := DataJSFromFiles(db, filepath.Join(t.TempDir(), "missing.json"), prosePath); err == nil {
		t.Fatal("data js must fail on a missing config path")
	}
	if _, err := DataJSFromFiles(db, hubPath, filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("data js must fail on a missing prose path")
	}
	db.Close()
	if _, err := DataJSFromFiles(db, hubPath, prosePath); err == nil {
		t.Fatal("data js must fail over a closed db")
	}
}

// loadHeatmapEnemies holds the mine/emit contract: exactly enemyCount rows in
// rank order, anything else refuses to emit.
func TestLoadHeatmapEnemiesRowCount(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	if _, err := loadHeatmapEnemies(db, cfg); err == nil ||
		!strings.Contains(err.Error(), "run mine before emit data") {
		t.Fatalf("empty heatmap must fail with the mine hint, got %v", err)
	}
	want := make([]string, 0, cfg.Heatmap.EnemyCount)
	for i := 0; i < cfg.Heatmap.EnemyCount; i++ {
		slug := fmt.Sprintf("enemy-%02d", i)
		want = append(want, slug)
		execFixture(t, db, `INSERT INTO heatmap_enemies (rank, slug) VALUES (?, ?)`, i+1, slug)
	}
	got, err := loadHeatmapEnemies(db, cfg)
	if err != nil {
		t.Fatalf("full heatmap load: %v", err)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("heatmap enemies = %v, want rank order %v", got, want)
	}
	execFixture(t, db, `INSERT INTO heatmap_enemies (rank, slug) VALUES (?, 'enemy-extra')`, cfg.Heatmap.EnemyCount+1)
	if _, err := loadHeatmapEnemies(db, cfg); err == nil {
		t.Fatal("overfull heatmap must fail")
	}
}
