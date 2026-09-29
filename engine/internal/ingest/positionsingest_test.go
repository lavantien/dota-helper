package ingest

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/store"
)

// positionsTestEnv seeds a two-position cache; bracket and gameMode let tests
// pin the scope-stamp guard arms.
func positionsTestEnv(t *testing.T, bracket string, gameMode int) (*sql.DB, *config.Config, *rosterFile) {
	t.Helper()
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	cfg.Scope.GameMode = 22
	cfg.Paths.PositionsRawDir = filepath.Join(base, "positions", "raw")
	wf(t, filepath.Join(cfg.Paths.PositionsRawDir, "_positions.json"), fmt.Sprintf(`{
		"fetchedAt": "2026-09-27T10:00:00Z",
		"bracket": %q,
		"gameMode": %d,
		"take": 7,
		"field": "winDay",
		"positions": [
			{"position": 1, "rows": [
				{"heroId": 1, "matchCount": 600, "winCount": 300},
				{"heroId": 8, "matchCount": 300, "winCount": 150}
			]},
			{"position": 2, "rows": [{"heroId": 14, "matchCount": 100, "winCount": 60}]}
		]
	}`, bracket, gameMode))
	rf := &rosterFile{}
	rf.Heroes = []rosterEntry{
		{ID: 1, Slug: "anti-mage"},
		{ID: 8, Slug: "dark-seer"},
		{ID: 14, Slug: "pudge"},
	}
	db, err := store.Open(filepath.Join(base, "t.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, cfg, rf
}

// a positions cache written under a different scope must never load under
// the hub's scope label
func TestIngestPositionsRejectsScopeMismatch(t *testing.T) {
	db, cfg, rf := positionsTestEnv(t, "ALL", 22)
	if _, err := ingestPositions(db, cfg, rf); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("scope-mismatched positions cache must fail loudly, got %v", err)
	}
	_, cfg, rf = positionsTestEnv(t, "DIVINE_IMMORTAL", 2)
	if _, err := ingestPositions(db, cfg, rf); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("mode-mismatched positions cache must fail loudly, got %v", err)
	}
}

// a positions cache crawled under a different window must never load under
// the hub's window label
func TestIngestPositionsRejectsWindowMismatch(t *testing.T) {
	db, cfg, rf := positionsTestEnv(t, "DIVINE_IMMORTAL", 22)
	wf(t, filepath.Join(cfg.Paths.PositionsRawDir, "_positions.json"), `{
		"fetchedAt": "2026-09-27T10:00:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"gameMode": 22,
		"take": 30,
		"field": "winDay",
		"positions": [{"position": 1, "rows": [{"heroId": 1, "matchCount": 600, "winCount": 300}]}]
	}`)
	if _, err := ingestPositions(db, cfg, rf); err == nil || !strings.Contains(err.Error(), "take") {
		t.Fatalf("window-mismatched positions cache must fail loudly, got %v", err)
	}
}

// a hero vanishing from a position in a later crawl must drop the stale row:
// the crawl folds a rolling window, so leftovers would mix crawl windows in
// the emitted shares
func TestIngestPositionsDropsVanishedRows(t *testing.T) {
	db, cfg, rf := positionsTestEnv(t, "DIVINE_IMMORTAL", 22)
	if _, err := ingestPositions(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	wf(t, filepath.Join(cfg.Paths.PositionsRawDir, "_positions.json"), `{
		"fetchedAt": "2026-09-28T10:00:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"gameMode": 22,
		"take": 7,
		"field": "winDay",
		"positions": [
			{"position": 1, "rows": [{"heroId": 1, "matchCount": 700, "winCount": 340}]}
		]
	}`)
	if _, err := ingestPositions(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hero_position`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("hero_position rows = %d, want 1 (stale rows from the prior crawl dropped)", n)
	}
}
