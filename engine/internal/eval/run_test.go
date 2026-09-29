package eval

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedMatches writes four complete 2-ban + 10-pick drafts with pool heroes
// on both sides so every match lands in the AUC set, start times ascending,
// radiant wins alternating.
func seedMatches(t *testing.T, db *sql.DB) {
	t.Helper()
	cfg := testConfig(t)
	slugs := seedRoster(t, cfg)
	idOf := map[string]int{}
	for _, slug := range slugs {
		var id int
		if err := db.QueryRow(`SELECT hero_id FROM hero_roster WHERE slug = ?`, slug).Scan(&id); err != nil {
			t.Fatalf("hero id for %s: %v", slug, err)
		}
		idOf[slug] = id
	}
	pool := cfg.PoolSlugs()
	type ev struct {
		rad, pick bool
		slug      string
	}
	for mi := 0; mi < 4; mi++ {
		matchID := int64(1000 + mi)
		start := int64(1700000000 + 86400*mi)
		// bans and picks all land on distinct pool heroes: banning pool
		// heroes is realistic and keeps the fixture roster-independent
		events := []ev{
			{true, false, pool[10]},
			{false, false, pool[11]},
			{true, true, pool[0]}, {false, true, pool[5]}, {true, true, pool[1]},
			{false, true, pool[6]}, {true, true, pool[2]}, {false, true, pool[7]},
			{true, true, pool[3]}, {false, true, pool[8]}, {true, true, pool[4]}, {false, true, pool[9]},
		}
		if _, err := db.Exec(`INSERT INTO match_raw
			(match_id, start_time, radiant_win, duration_seconds, lobby_type, game_mode, bracket, average_rank, source, fetched_at)
			VALUES (?, ?, ?, 2400, 'PRACTICE', 'CAPTAINS_MODE', 'TEST', 30, 'stratz_backfill', 'test')`,
			matchID, start, mi%2 == 0); err != nil {
			t.Fatalf("match_raw %d: %v", matchID, err)
		}
		for seq, e := range events {
			if _, err := db.Exec(`INSERT INTO draft_timing (match_id, seq, is_radiant, is_pick, hero_id)
				VALUES (?, ?, ?, ?, ?)`, matchID, seq, e.rad, e.pick, idOf[e.slug]); err != nil {
				t.Fatalf("draft_timing %d/%d: %v", matchID, seq, err)
			}
		}
	}
}

func TestRunOrchestratesAndIsByteIdentical(t *testing.T) {
	t.Chdir(repoRoot)
	cfg := testConfig(t)
	// the hub census floor (30) exceeds the 2-match fixture's transition
	// counts, so lower it to make the census assertion meaningful here
	cfg.Eval.Seq.CensusMinCount = 1
	db := seedModelDB(t)
	seedMatches(t, db)

	run := func(root string) []byte {
		t.Helper()
		if _, err := Run(Deps{DB: db, Cfg: cfg, RepoRoot: root}); err != nil {
			t.Fatalf("run: %v", err)
		}
		b, err := os.ReadFile(filepath.Join(root, cfg.Paths.EvalOut))
		if err != nil {
			t.Fatalf("read report: %v", err)
		}
		return b
	}
	a := run(t.TempDir())
	b := run(t.TempDir())
	if string(a) != string(b) {
		t.Fatalf("two runs on the same db produced different bytes")
	}

	var doc Report
	if err := json.Unmarshal(a, &doc); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if doc.Population.Matches != 4 {
		t.Fatalf("population matches = %d, want 4", doc.Population.Matches)
	}
	if doc.Split.Train != 2 || doc.Split.Holdout != 2 {
		t.Fatalf("split = %d/%d, want 2/2", doc.Split.Train, doc.Split.Holdout)
	}
	if doc.Sequential.Picks == 0 || doc.Sequential.RankedPicks == 0 {
		t.Fatalf("sequential picks %d ranked %d, want nonzero", doc.Sequential.Picks, doc.Sequential.RankedPicks)
	}
	// the auc set is holdout-only and every seeded match is two-sided
	if doc.Sequential.AUCMatches != 2 {
		t.Fatalf("auc matches = %d, want 2", doc.Sequential.AUCMatches)
	}
	if doc.Sequential.MeanPercentileCI == (CI{}) || doc.Sequential.AUCCI == (CI{}) {
		t.Fatalf("bootstrap intervals missing from the report")
	}
	if doc.FinalSet.Kind != "finalSet" || doc.FinalSet.Picks == 0 {
		t.Fatalf("final-set replay missing: %+v", doc.FinalSet)
	}
	if len(doc.Audits) != 2 {
		t.Fatalf("audits = %d families, want 2", len(doc.Audits))
	}
	if len(doc.Calibration.Bins) == 0 {
		t.Fatalf("calibration empty")
	}
	if len(doc.Comparators.Rows) != 3 || doc.Comparators.Ensemble == nil {
		t.Fatalf("comparators = %+v, want three rows plus the ensemble", doc.Comparators)
	}
	// the bank scores the same two-sided holdout the replay AUC reads
	for _, row := range doc.Comparators.Rows {
		if row.Matches != doc.Sequential.AUCMatches {
			t.Fatalf("comparator %s scored %d matches, want the %d two-sided of sequential.auc",
				row.Name, row.Matches, doc.Sequential.AUCMatches)
		}
	}
	if len(doc.Triples.Sweep) == 0 {
		t.Fatalf("triples section missing its support sweep")
	}
	if doc.Seq.Matches == 0 || doc.Seq.Census.Pairs == 0 {
		t.Fatalf("seq section = %+v, want the train census", doc.Seq)
	}
}

// TestLoadMatchesDropsCorruptDrafts pins the loader hygiene: an unknown
// hero id and a repeated hero both disqualify the match.
func TestLoadMatchesDropsCorruptDrafts(t *testing.T) {
	t.Chdir(repoRoot)
	db := seedModelDB(t)
	seedMatches(t, db)
	exec := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	// unknown hero id
	exec(`INSERT INTO match_raw (match_id, start_time, radiant_win, duration_seconds, lobby_type,
		game_mode, bracket, average_rank, source, fetched_at)
		VALUES (2000, 1700000000, true, 2400, 'PRACTICE', 'CAPTAINS_MODE', 'TEST', 30, 'stratz_backfill', 'test')`)
	exec(`INSERT INTO draft_timing (match_id, seq, is_radiant, is_pick, hero_id) VALUES (2000, 0, true, true, 99999)`)
	// duplicated hero
	exec(`INSERT INTO match_raw (match_id, start_time, radiant_win, duration_seconds, lobby_type,
		game_mode, bracket, average_rank, source, fetched_at)
		VALUES (2001, 1700000000, true, 2400, 'PRACTICE', 'CAPTAINS_MODE', 'TEST', 30, 'stratz_backfill', 'test')`)
	// a roster-resolved hero picked twice: a pure duplicate
	exec(`INSERT INTO draft_timing (match_id, seq, is_radiant, is_pick, hero_id) VALUES (2001, 0, true, true, 100)`)
	exec(`INSERT INTO draft_timing (match_id, seq, is_radiant, is_pick, hero_id) VALUES (2001, 1, false, true, 100)`)

	matches, stats, err := LoadMatches(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(matches) != 4 {
		t.Fatalf("loaded %d matches, want the 4 clean ones", len(matches))
	}
	if stats.Total != 6 || stats.Loaded != 4 ||
		stats.DroppedUnknown != 1 || stats.DroppedDuplicate != 1 || stats.DroppedNoDraft != 0 {
		t.Fatalf("load stats %+v, want 6 total 4 loaded 1 unknown 1 duplicate", stats)
	}
	for i := 1; i < len(matches); i++ {
		if matches[i].StartTime < matches[i-1].StartTime {
			t.Fatalf("matches not ordered by start time")
		}
	}
}
