package ingest

import (
	"fmt"
	"path/filepath"
	"testing"

	"poolguide/internal/store"
)

func TestIngestMatchesFlattensAndIsIdempotent(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	cfg.Paths.MatchesRawDir = filepath.Join(base, "matches", "raw")

	// two matches: 12 draft entries (10 picks + 2 bans) and 10 (picks only);
	// one JSON object per line, ndjson style
	draft := func(n int) string {
		s := ""
		for i := 0; i < n; i++ {
			if i > 0 {
				s += ","
			}
			isPick := i >= 2
			s += fmt.Sprintf(`{"seq": %d, "isRadiant": %t, "isPick": %t, "heroId": %d}`, i, i%2 == 0, isPick, 100+i)
		}
		return s
	}
	row := func(id int64, start int64, win bool, dur, n int) string {
		return fmt.Sprintf(`{"matchId": %d, "startDateTime": %d, "radiantWin": %t, "durationSeconds": %d, "lobbyType": "PRACTICE", "gameMode": "CAPTAINS_MODE", "averageRank": 80, "leagueId": 42, "draft": [%s], "players": [{"heroId": 1, "isRadiant": true, "lane": 1, "position": 1, "role": 1, "roleBasic": 1}]}`,
			id, start, win, dur, draft(n))
	}
	wf(t, filepath.Join(cfg.Paths.MatchesRawDir, "matches.ndjson"),
		row(101, 1789500000, true, 2400, 12)+"\n"+row(102, 1789600000, false, 3100, 10)+"\n")
	wf(t, filepath.Join(cfg.Paths.MatchesRawDir, "_backfill.json"),
		`{"crawledAt": "2026-09-26T10:00:00Z", "bracket": "DIVINE_IMMORTAL",
		"filterEcho": "gameModeIds: [2], startDateTime: 0, endDateTime: 1",
		"pages": 2, "kept": 2, "dropped": 0, "lastSkip": 0, "leagueIndex": 1, "done": true, "requestCount": 4}`)

	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mN, dN, err := ingestMatches(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if mN != 2 || dN != 22 {
		t.Fatalf("ingestMatches = %d matches, %d draft rows, want 2 and 22", mN, dN)
	}
	var got int
	if err := db.QueryRow("SELECT count(*) FROM match_raw").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("match_raw = %d rows, want 2", got)
	}
	if err := db.QueryRow("SELECT count(*) FROM draft_timing").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 22 {
		t.Errorf("draft_timing = %d rows, want 22", got)
	}
	var radiantWin bool
	var lobby, mode, source string
	if err := db.QueryRow(`SELECT radiant_win, lobby_type, game_mode, source FROM match_raw WHERE match_id = 101`).
		Scan(&radiantWin, &lobby, &mode, &source); err != nil {
		t.Fatal(err)
	}
	if !radiantWin || lobby != "PRACTICE" || mode != "CAPTAINS_MODE" || source != "stratz_backfill" {
		t.Errorf("match_raw row = win %v lobby %q mode %q source %q", radiantWin, lobby, mode, source)
	}
	var seq0Hero int
	var seq0Pick bool
	if err := db.QueryRow(`SELECT hero_id, is_pick FROM draft_timing WHERE match_id = 101 AND seq = 0`).
		Scan(&seq0Hero, &seq0Pick); err != nil {
		t.Fatal(err)
	}
	if seq0Hero != 100 || seq0Pick {
		t.Errorf("draft_timing seq 0 = hero %d pick %v, want hero 100 ban", seq0Hero, seq0Pick)
	}

	// idempotent rerun: keyed replaces never append
	if _, _, err := ingestMatches(db, cfg); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM match_raw").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("match_raw after rerun = %d, want 2", got)
	}
	if err := db.QueryRow("SELECT count(*) FROM draft_timing").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 22 {
		t.Errorf("draft_timing after rerun = %d, want 22", got)
	}
}

func TestIngestMatchesSkipsWithoutManifest(t *testing.T) {
	base := t.TempDir()
	cfg := ingestTestConfig(base)
	cfg.Paths.MatchesRawDir = filepath.Join(base, "matches", "raw")
	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mN, dN, err := ingestMatches(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if mN != 0 || dN != 0 {
		t.Errorf("ingestMatches without a crawl = %d/%d, want 0/0", mN, dN)
	}
}
