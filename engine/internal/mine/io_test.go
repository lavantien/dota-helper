package mine

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"poolguide/internal/store"
)

func openFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "fixture.duckdb"))
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

// fixedNow stamps provenance deterministically in tests.
func fixedNow() time.Time {
	return time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)
}

func TestRunDerivesAndPersistsIdempotently(t *testing.T) {
	cfg := testCfg(t)
	// the fixture roster is the whole universe here, so the heatmap width
	// matches it instead of the live hub's 26
	cfg.Heatmap.EnemyCount = 3
	db := openFixtureDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES
		(2, 'spectre', 'Spectre', 'spectre', 'test'),
		(3, 'axe', 'Axe', 'axe', 'test'),
		(7, 'phantom-lancer', 'Phantom Lancer', 'phantom_lancer', 'test')`)
	exec(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, win_count, matches, source, scrape_date, confidence)
		VALUES ('spectre', 'axe', 30, 60, 'opendota_explorer', '2026-09-24', 'high')`)
	exec(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, synergy, matches, source, scrape_date, confidence)
		VALUES ('spectre', 'axe', -3.0, 40, 'stratz_vs', '2026-09-24', 'high')`)
	// divergent pair: both directions crawled with different values
	exec(t, db, `INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
		VALUES ('spectre', 'phantom-lancer', 100, 3.0, 'stratz'),
		('phantom-lancer', 'spectre', 25, 1.0, 'stratz')`)
	exec(t, db, `INSERT INTO hero_stats (slug, bracket, pick_count, win_count, source, scrape_date)
		VALUES ('spectre', ?, 1000, 550, 'stratz', '2026-09-24'),
		('axe', ?, 500, 250, 'stratz', '2026-09-24'),
		('phantom-lancer', ?, 200, 90, 'stratz', '2026-09-24')`,
		cfg.Scope.Bracket, cfg.Scope.Bracket, cfg.Scope.Bracket)

	deps := Deps{DB: db, Cfg: cfg, RepoRoot: t.TempDir(), Now: fixedNow}
	if err := Run(deps); err != nil {
		t.Fatalf("run: %v", err)
	}

	pool := cfg.PoolSlugs()
	rosterLen := 3
	wantPair := 1 + 2 // one enemy cell, two symmetric ally cells
	wantNorm := len(pool) * rosterLen * 2
	assertCount(t, db, "SELECT count(*) FROM pair_shrunk", wantPair)
	assertCount(t, db, "SELECT count(*) FROM norm_cell", wantNorm)
	assertCount(t, db, "SELECT count(*) FROM hero_prior", len(pool))
	assertCount(t, db, "SELECT count(*) FROM popularity", rosterLen)
	assertCount(t, db, `SELECT count(*) FROM provenance WHERE kind = 'acceptance' AND subject LIKE 'synergy%'`, 1)

	var shareSum float64
	if err := db.QueryRow(`SELECT sum(pick_share) FROM popularity`).Scan(&shareSum); err != nil {
		t.Fatalf("sum popularity: %v", err)
	}
	if shareSum < 0.999999 || shareSum > 1.000001 {
		t.Fatalf("popularity shares sum to %v, want 1", shareSum)
	}

	// the enemy columns rank pick-share descending: spectre 1000, axe 500,
	// phantom-lancer 200 picks, the pool mirror stays a column
	var ranked []string
	rows, err := db.Query(`SELECT slug FROM heatmap_enemies ORDER BY rank`)
	if err != nil {
		t.Fatalf("query heatmap_enemies: %v", err)
	}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ranked = append(ranked, slug)
	}
	rows.Close()
	wantRanked := []string{"spectre", "axe", "phantom-lancer"}
	if len(ranked) != len(wantRanked) {
		t.Fatalf("heatmap_enemies holds %d rows, want %d", len(ranked), len(wantRanked))
	}
	for i := range wantRanked {
		if ranked[i] != wantRanked[i] {
			t.Fatalf("heatmap column %d = %s, want %s", i+1, ranked[i], wantRanked[i])
		}
	}

	var pctBefore []float64
	rows, err = db.Query(`SELECT pct FROM norm_cell ORDER BY hero, other, kind`)
	if err != nil {
		t.Fatalf("query norm_cell: %v", err)
	}
	for rows.Next() {
		var p float64
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		pctBefore = append(pctBefore, p)
	}
	rows.Close()

	if err := Run(deps); err != nil {
		t.Fatalf("second run: %v", err)
	}
	assertCount(t, db, "SELECT count(*) FROM norm_cell", wantNorm)
	assertCount(t, db, "SELECT count(*) FROM pair_shrunk", wantPair)
	assertCount(t, db, "SELECT count(*) FROM heatmap_enemies", rosterLen)
	var pctAfter []float64
	rows, err = db.Query(`SELECT pct FROM norm_cell ORDER BY hero, other, kind`)
	if err != nil {
		t.Fatalf("query norm_cell again: %v", err)
	}
	for rows.Next() {
		var p float64
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		pctAfter = append(pctAfter, p)
	}
	rows.Close()
	if len(pctBefore) != len(pctAfter) {
		t.Fatalf("norm_cell count changed across runs")
	}
	for i := range pctBefore {
		if pctBefore[i] != pctAfter[i] {
			t.Fatalf("norm_cell pct is not deterministic across rebuilds at index %d", i)
		}
	}
}

func TestRunUsesPopularityFallback(t *testing.T) {
	cfg := testCfg(t)
	// two-hero fixture universe, same shaping as the idempotency test
	cfg.Heatmap.EnemyCount = 2
	db := openFixtureDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES
		(1, 'axe', 'Axe', 'axe', 'test'),
		(2, 'centaur', 'Centaur', 'centaur', 'test')`)
	exec(t, db, `INSERT INTO hero_stats (slug, bracket, pick_count, win_count, source, scrape_date)
		VALUES ('axe', ?, 100, 50, 'stratz', '2026-09-24')`, cfg.Scope.Bracket)

	root := t.TempDir()
	fallbackDir := filepath.Join(root, cfg.Paths.StratzRawDir)
	if err := os.MkdirAll(fallbackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fallback := `{"fetchedAt": "2026-09-24T10:00:00Z", "bracket": "ALL", "field": "winDay",
		"note": "test aggregate", "rows": [
		{"heroId": 1, "matchCount": 15, "winCount": 8},
		{"heroId": 1, "matchCount": 10, "winCount": 5},
		{"heroId": 2, "matchCount": 75, "winCount": 40},
		{"heroId": 9, "matchCount": 999, "winCount": 500}]}`
	if err := os.WriteFile(filepath.Join(fallbackDir, fallbackPopFile), []byte(fallback), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(Deps{DB: db, Cfg: cfg, RepoRoot: root, Now: fixedNow}); err != nil {
		t.Fatalf("run: %v", err)
	}
	var src string
	var centaur float64
	if err := db.QueryRow(`SELECT source FROM popularity LIMIT 1`).Scan(&src); err != nil {
		t.Fatal(err)
	}
	if src != popSourceLabel(true) {
		t.Fatalf("popularity source = %s, want the fallback label", src)
	}
	if err := db.QueryRow(`SELECT pick_share FROM popularity WHERE slug = 'centaur'`).Scan(&centaur); err != nil {
		t.Fatal(err)
	}
	if centaur < 0.42 || centaur > 0.44 {
		t.Fatalf("centaur fallback share = %v, want ~0.43 (0.75 renormalized against axe's full stats share)", centaur)
	}
	assertCount(t, db, `SELECT count(*) FROM provenance WHERE kind = 'fallback' AND subject = 'popularity'`, 1)
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", query, err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", query, got, want)
	}
}
