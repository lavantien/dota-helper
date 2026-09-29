package ingest

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/store"
)

func trendTestEnv(t *testing.T, fetchedAt string) (*sql.DB, *config.Config, *rosterFile) {
	t.Helper()
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	cfg.Scope.GameMode = 22
	cfg.Paths.TrendsFile = filepath.Join(base, "trends", "snapshots.ndjson")
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), `{
		"fetchedAt": "`+fetchedAt+`",
		"bracket": "DIVINE_IMMORTAL",
		"gameMode": 22,
		"take": 7,
		"field": "winDay",
		"rows": [
			{"heroId": 1, "matchCount": 600, "winCount": 300},
			{"heroId": 8, "matchCount": 300, "winCount": 150},
			{"heroId": 14, "matchCount": 100, "winCount": 60}
		]
	}`)
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

func TestIngestTrendsRejectsUnmappedGameMode(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-27T10:00:00Z")
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), `{
		"fetchedAt": "2026-09-27T10:00:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"gameMode": 21,
		"take": 7,
		"field": "winDay",
		"rows": [{"heroId": 1, "matchCount": 600, "winCount": 300}]
	}`)
	if _, err := ingestTrends(db, cfg, rf); err == nil || !strings.Contains(err.Error(), "gameMode") {
		t.Fatalf("unmapped gameMode stamp must fail loudly, got %v", err)
	}
}

// a corrupt ledger line must abort the run: the rewrite would persist the
// loss and silently shrink the trend history
func TestIngestTrendsFailsOnCorruptLedgerLine(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-28T10:00:00Z")
	wf(t, cfg.Paths.TrendsFile, "{\"date\": \"2026-09-27\" not json\n")
	if _, err := ingestTrends(db, cfg, rf); err == nil || !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("corrupt ledger line must fail loudly, got %v", err)
	}
}

// a scope re-cut leaves older-scope snapshots in the ledger, but the
// projection and the latest-pair deltas must never straddle scopes
func TestIngestTrendsProjectionStaysSingleScope(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-27T10:00:00Z")
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	old := `{"date":"2026-09-20","scope":"DIVINE_IMMORTAL+CAPTAINS_MODE","field":"winDay","slug":"anti-mage","pick_share":0.5,"wr":0.5}
{"date":"2026-09-20","scope":"DIVINE_IMMORTAL+CAPTAINS_MODE","field":"winDay","slug":"pudge","pick_share":0.5,"wr":0.5}
`
	cur, err := os.ReadFile(cfg.Paths.TrendsFile)
	if err != nil {
		t.Fatal(err)
	}
	wf(t, cfg.Paths.TrendsFile, old+string(cur))
	// same-day re-run rebuilds the projection from the whole ledger
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hero_trend WHERE snapshot_date = ?`, "2026-09-20").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("old-scope snapshots must stay out of the projection, found %d rows", n)
	}
	var cur2 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hero_trend`).Scan(&cur2); err != nil {
		t.Fatal(err)
	}
	if cur2 != 3 {
		t.Fatalf("projection rows = %d, want the 3 current-scope rows only", cur2)
	}
}

// the window cut isolates series: a ledger holding legacy no-window lines
// plus 7-day lines must project the 7-day series only, and a same-day
// snapshot under a different window appends instead of being deduped away
func TestIngestTrendsWindowCutIsolatesSeries(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-29T10:00:00Z")
	legacy := `{"date":"2026-09-26","scope":"DIVINE_IMMORTAL+ALL_PICK_RANKED","field":"winDay","slug":"anti-mage","pick_share":0.5,"wr":0.5}
{"date":"2026-09-27","scope":"DIVINE_IMMORTAL+ALL_PICK_RANKED","field":"winDay","slug":"anti-mage","pick_share":0.5,"wr":0.5}
`
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	cur, err := os.ReadFile(cfg.Paths.TrendsFile)
	if err != nil {
		t.Fatal(err)
	}
	wf(t, cfg.Paths.TrendsFile, legacy+string(cur))
	// same-day rerun rebuilds the projection from the whole ledger
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg.Paths.TrendsFile)
	if n := strings.Count(string(raw), "\n"); n != 5 {
		t.Fatalf("ledger has %d lines, want legacy 2 + fresh 3", n)
	}
	if !strings.Contains(string(raw), `"window":7`) {
		t.Fatalf("fresh snapshots must carry the window stamp:\n%s", raw)
	}
	var legacyN, freshN int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hero_trend WHERE snapshot_date < '2026-09-29'`).Scan(&legacyN); err != nil {
		t.Fatal(err)
	}
	if legacyN != 0 {
		t.Fatalf("legacy-window snapshots must stay out of the projection, found %d rows", legacyN)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM hero_trend`).Scan(&freshN); err != nil {
		t.Fatal(err)
	}
	if freshN != 3 {
		t.Fatalf("projection rows = %d, want the 3 fresh-window rows only", freshN)
	}
}

// a same-day snapshot under a different window is a new series, not a dedupe
// hit: the ledger keeps both and the projection follows the newer one
func TestIngestTrendsSameDayDualWindowAppends(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-29T10:00:00Z")
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), `{
		"fetchedAt": "2026-09-29T10:00:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"gameMode": 22,
		"take": 30,
		"field": "winDay",
		"rows": [{"heroId": 1, "matchCount": 900, "winCount": 450}]
	}`)
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg.Paths.TrendsFile)
	if n := strings.Count(string(raw), "\n"); n != 4 {
		t.Fatalf("same-day dual-window snapshot must append, got %d lines", n)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hero_trend`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("projection rows = %d, want only the newest window's row", n)
	}
}

// legacy lines without a window stamp read as the pre-cut 30-day window
func TestSnapWindowLegacyAttribution(t *testing.T) {
	if w := snapWindow(trendSnap{}); w != legacyTrendWindowDays {
		t.Fatalf("unstamped snapshot window = %d, want the legacy %d", w, legacyTrendWindowDays)
	}
	if w := snapWindow(trendSnap{Window: 7}); w != 7 {
		t.Fatalf("stamped snapshot window = %d, want 7", w)
	}
}

func TestIngestTrendsAppendsPerDayAndRebuilds(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-27T10:00:00Z")
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.Paths.TrendsFile)
	if err != nil {
		t.Fatal(err)
	}
	if first := strings.Count(string(raw), "\n"); first != 3 {
		t.Fatalf("ledger has %d lines, want one per roster hero", first)
	}
	if !strings.Contains(string(raw), `"scope":"DIVINE_IMMORTAL+ALL_PICK_RANKED"`) {
		t.Fatalf("scope label wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"slug":"anti-mage","pick_share":0.6`) {
		t.Fatalf("share wrong:\n%s", raw)
	}
	// same-day re-crawl appends nothing
	if _, err := ingestTrends(db, cfg, rf); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(cfg.Paths.TrendsFile)
	if n := strings.Count(string(raw), "\n"); n != 3 {
		t.Fatalf("same-day rerun appended: %d lines", n)
	}
	// next day appends and the projection carries both dates
	db2, cfg2, rf2 := trendTestEnv(t, "2026-10-04T10:00:00Z")
	cfg2.Paths.TrendsFile = cfg.Paths.TrendsFile
	if _, err := ingestTrends(db2, cfg2, rf2); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(cfg.Paths.TrendsFile)
	if n := strings.Count(string(raw), "\n"); n != 6 {
		t.Fatalf("next day should append 3 lines, got %d", n)
	}
	var dates int
	if err := db2.QueryRow(`SELECT COUNT(DISTINCT snapshot_date) FROM hero_trend`).Scan(&dates); err != nil {
		t.Fatal(err)
	}
	if dates != 2 {
		t.Fatalf("projection dates = %d, want 2", dates)
	}
}
