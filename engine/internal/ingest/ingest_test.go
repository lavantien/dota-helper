package ingest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
	"poolguide/internal/store"
)

func wf(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ingestTestConfig(base string) *config.Config {
	return &config.Config{
		Patch:      "7.41f",
		PatchEpoch: "2026-09-15",
		Scope:      config.Scope{Bracket: "DIVINE_IMMORTAL"},
		Stratz:     config.StratzCfg{Take: 7, MinWithRows: 1, MinVsRows: 1, MaxAbsSynergy: 20},
		Paths: config.Paths{
			DBFile:       filepath.Join(base, "t.duckdb"),
			StratzRawDir: filepath.Join(base, "syn", "raw"),
			MatchupsDir:  filepath.Join(base, "matchups"),
		},
		Pool: []config.PoolEntry{
			{Slug: "dark-seer", Name: "Dark Seer", Role: "mid", Tier: "dedicated"},
			{Slug: "pudge", Name: "Pudge", Role: "pos4", Tier: "dedicated"},
		},
	}
}

// a popularity cache written under a different scope must never load under
// the hub's scope label
func TestIngestPopularityRejectsScopeMismatch(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-27T10:00:00Z")
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), `{
		"fetchedAt": "2026-09-27T10:00:00Z",
		"bracket": "ALL",
		"gameMode": 22,
		"take": 7,
		"field": "winDay",
		"rows": [{"heroId": 1, "matchCount": 600, "winCount": 300}]
	}`)
	_, err := ingestPopularity(db, cfg, rf)
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("scope-mismatched popularity cache must fail loudly, got %v", err)
	}
}

// a popularity cache crawled under a different window must never load under
// the hub's window label: mixed windows in hero_stats are silent staleness
func TestIngestPopularityRejectsWindowMismatch(t *testing.T) {
	db, cfg, rf := trendTestEnv(t, "2026-09-27T10:00:00Z")
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), `{
		"fetchedAt": "2026-09-27T10:00:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"gameMode": 22,
		"take": 30,
		"field": "winDay",
		"rows": [{"heroId": 1, "matchCount": 600, "winCount": 300}]
	}`)
	_, err := ingestPopularity(db, cfg, rf)
	if err == nil || !strings.Contains(err.Error(), "take") {
		t.Fatalf("window-mismatched popularity cache must fail loudly, got %v", err)
	}
}

// caches from mixed calendar weeks must never blend into one matchup_raw
func TestIngestStratzCachesRejectsMixedWeeks(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	ingestFixtures(t, cfg)
	// a week guaranteed different from whatever the fixtures pinned
	stale := LastCompletedWeekStart(time.Now()) - 7*86400
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "pudge.json"), fmt.Sprintf(`{
		"heroId": 14, "slug": "pudge", "name": "Pudge",
		"npc": "npc_dota_hero_pudge",
		"fetchedAt": "2026-09-24T10:06:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"week": %d,
		"vs": [{"heroId2": 1, "matchCount": 8000, "winCount": 3900, "synergy": 0.4}],
		"with": []
	}`, stale))
	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rosterSlugs := map[string]bool{"dark-seer": true, "pudge": true}
	_, _, err = ingestStratzCaches(db, cfg, rosterSlugs)
	if err == nil || !strings.Contains(err.Error(), "mixed calendar weeks") {
		t.Fatalf("mixed-week caches must fail loudly, got %v", err)
	}
}

// an unstamped legacy cache beside stamped ones must abort, not blend: the
// consensus loop may not skip the unstamped entry
func TestIngestStratzCachesRejectsUnstampedMix(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	ingestFixtures(t, cfg)
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "pudge.json"), `{
		"heroId": 14, "slug": "pudge", "name": "Pudge",
		"npc": "npc_dota_hero_pudge",
		"fetchedAt": "2026-09-24T10:06:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"vs": [{"heroId2": 1, "matchCount": 8000, "winCount": 3900, "synergy": 0.4}],
		"with": []
	}`)
	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rosterSlugs := map[string]bool{"dark-seer": true, "pudge": true}
	_, _, err = ingestStratzCaches(db, cfg, rosterSlugs)
	if err == nil || !strings.Contains(err.Error(), "no week stamps") {
		t.Fatalf("an unstamped cache beside stamped ones must fail loudly, got %v", err)
	}
}

// a cache set pinning a week that is no longer the last completed one is
// stale: re-ingesting it would label old-week pairs as current
func TestIngestStratzCachesRejectsStaleWeek(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	ingestFixtures(t, cfg)
	stale := LastCompletedWeekStart(time.Now()) - 7*86400
	for _, slug := range []string{"dark-seer", "pudge"} {
		path := filepath.Join(cfg.Paths.StratzRawDir, slug+".json")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		wf(t, path, strings.Replace(string(b), fmt.Sprintf(`"week": %d`, LastCompletedWeekStart(time.Now())), fmt.Sprintf(`"week": %d`, stale), 1))
	}
	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rosterSlugs := map[string]bool{"dark-seer": true, "pudge": true}
	_, _, err = ingestStratzCaches(db, cfg, rosterSlugs)
	if err == nil || !strings.Contains(err.Error(), "the last completed week is") {
		t.Fatalf("a stale pinned week must fail loudly, got %v", err)
	}
}

func ingestFixtures(t *testing.T, cfg *config.Config) {
	t.Helper()
	// the caches must pin the week the guard currently expects, so the stamp
	// is computed at test runtime, never frozen
	week := LastCompletedWeekStart(time.Now())
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"), `{
		"fetchedAt": "2026-09-24T10:00:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"source": "stratz",
		"heroes": [
			{"id": 1, "slug": "anti-mage", "name": "Anti-Mage", "npc": "npc_dota_hero_antimage"},
			{"id": 8, "slug": "dark-seer", "name": "Dark Seer", "npc": "npc_dota_hero_dark_seer"},
			{"id": 14, "slug": "pudge", "name": "Pudge", "npc": "npc_dota_hero_pudge"}
		]
	}`)
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "dark-seer.json"), fmt.Sprintf(`{
		"heroId": 8, "slug": "dark-seer", "name": "Dark Seer",
		"npc": "npc_dota_hero_dark_seer",
		"fetchedAt": "2026-09-24T10:05:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"week": %d,
		"vs": [
			{"heroId2": 1, "matchCount": 5000, "winCount": 2500, "synergy": 1.2},
			{"heroId2": 14, "matchCount": 6000, "winCount": 3100, "synergy": -0.8},
			{"heroId2": 999, "matchCount": 10, "winCount": 5, "synergy": 0.3}
		],
		"with": [
			{"heroId2": 14, "matchCount": 4000, "winCount": 2100, "synergy": 1.5}
		]
	}`, week))
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "pudge.json"), fmt.Sprintf(`{
		"heroId": 14, "slug": "pudge", "name": "Pudge",
		"npc": "npc_dota_hero_pudge",
		"fetchedAt": "2026-09-24T10:06:00Z",
		"bracket": "DIVINE_IMMORTAL",
		"week": %d,
		"vs": [{"heroId2": 1, "matchCount": 8000, "winCount": 3900, "synergy": 0.4}],
		"with": []
	}`, week))
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_crawl.json"),
		fmt.Sprintf(`{"crawledAt": "2026-09-24T10:30:00Z", "bracket": "DIVINE_IMMORTAL", "source": "stratz completed-week crawl",
		"take": 7, "week": %d,
		"heroes": [{"slug": "dark-seer", "heroId": 8, "fetchedAt": "2026-09-24T10:05:00Z", "cached": false, "accepted": true},
		{"slug": "pudge", "heroId": 14, "fetchedAt": "2026-09-24T10:06:00Z", "cached": false, "accepted": true}]}`, week))
	wf(t, filepath.Join(cfg.Paths.MatchupsDir, "opendota-2026-09-24.json"), `{
		"date": "2026-09-24",
		"overall_wr_by_slug": {"dark-seer": 47.53, "pudge": 50.0, "anti-mage": 49.1}
	}`)
}

func tableCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{"hero_roster", "matchup_raw", "synergy_raw", "hero_stats", "provenance"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts[table] = n
	}
	return counts
}

func TestIngestIsIdempotent(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	ingestFixtures(t, cfg)

	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Ingest(db, cfg); err != nil {
		t.Fatal(err)
	}
	first := tableCounts(t, db)
	want := map[string]int{
		"hero_roster": 3, "matchup_raw": 3, "synergy_raw": 1, "hero_stats": 3, "provenance": 2,
	}
	for table, n := range want {
		if first[table] != n {
			t.Errorf("after first ingest %s = %d, want %d", table, first[table], n)
		}
	}
	if err := Ingest(db, cfg); err != nil {
		t.Fatal(err)
	}
	second := tableCounts(t, db)
	for table, n := range want {
		if second[table] != n {
			t.Errorf("after second ingest %s = %d, want %d (idempotency broken)", table, second[table], n)
		}
	}

	// stratz vs row: wr_delta and dis stay NULL, matches come from matchCount
	var wrDelta, dis *float64
	var matches int64
	err = db.QueryRow(
		`SELECT wr_delta, dis, matches FROM matchup_raw WHERE pool_slug='dark-seer' AND enemy_slug='anti-mage' AND source='stratz_vs'`,
	).Scan(&wrDelta, &dis, &matches)
	if err != nil {
		t.Fatal(err)
	}
	if wrDelta != nil || dis != nil {
		t.Errorf("stratz_vs row should carry NULL wr_delta and dis, got %v %v", wrDelta, dis)
	}
	if matches != 5000 {
		t.Errorf("stratz_vs matches = %d, want 5000", matches)
	}

	// dotabuff retired from scoring: rows left in a db by an older ingest must
	// not survive a rerun as silent tier-2 filler
	if _, err := db.Exec(
		`INSERT INTO matchup_raw (pool_slug, enemy_slug, dis, matches, source, scrape_date, confidence)
		 VALUES ('dark-seer', 'axe', 2.0, 60, 'dotabuff', '2026-09-24', 'high')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := Ingest(db, cfg); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM matchup_raw WHERE source='dotabuff'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("dotabuff rows = %d after rerun, want 0 (retired source must purge)", n)
	}
}

// the pair tables mirror the week-stamped caches: a row the current crawl no
// longer serves (here seeded directly, in production dropped by rowSane or
// vanished from a thinner window) must not survive an older window's value
func TestIngestMirrorsPairTablesToCaches(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	ingestFixtures(t, cfg)
	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Ingest(db, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO matchup_raw (pool_slug, enemy_slug, synergy, win_count, matches, source, scrape_date, confidence)
		 VALUES ('dark-seer', 'axe', 2.0, 60, 120, 'stratz_vs', '2026-09-26', 'high')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO synergy_raw (hero_a, hero_b, match_count, win_count, synergy, source, bracket, window_note, raw_path)
		 VALUES ('dark-seer', 'axe', 120, 60, 2.0, 'stratz', 'DIVINE_IMMORTAL', 'stratz rolling aggregate, not patch pure', 'dark-seer.json')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := Ingest(db, cfg); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM matchup_raw WHERE source='stratz_vs' AND scrape_date <> '2026-09-24'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale stratz_vs rows = %d after rerun, want 0 (tables mirror the caches)", n)
	}
	if err := db.QueryRow(
		`SELECT count(*) FROM synergy_raw WHERE window_note <> 'stratz completed week ` +
			fmt.Sprintf(`%d'`, LastCompletedWeekStart(time.Now())),
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale synergy rows = %d after rerun, want 0 (tables mirror the caches)", n)
	}
}
