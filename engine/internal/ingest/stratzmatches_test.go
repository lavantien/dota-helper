package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
)

func backfillTestConfig(base string) *config.Config {
	return &config.Config{
		Patch:      "7.41f",
		PatchEpoch: "2026-09-15",
		Scope:      config.Scope{Bracket: "DIVINE_IMMORTAL"},
		Backfill: config.BackfillCfg{
			Take: 100, MaxRequests: 50, TargetMatches: 3,
			MinDurationSec: 900, GameMode: 2, LobbyType: 1,
		},
		Paths: config.Paths{
			MatchesRawDir: filepath.Join(base, "matches", "raw"),
			DBFile:        filepath.Join(base, "t.duckdb"),
		},
	}
}

// fakeMatchesClient serves canned bodies in order, like the stratz fixtures.
type fakeMatchesClient struct {
	bodies []string
	calls  int
}

func (f *fakeMatchesClient) query(q string) ([]byte, error) {
	if f.calls >= len(f.bodies) {
		return []byte(`{"data":{"league":{"matches":[]}}}`), nil
	}
	b := f.bodies[f.calls]
	f.calls++
	return []byte(b), nil
}

// pickBansJSON builds a draft with the first `picks` picks filled (radiant
// first) plus two ban rows at negative order, exercising the flatten.
func pickBansJSON(seed, picks int) string {
	var rows []string
	for i := 0; i < picks; i++ {
		id := seed*100 + i
		rad := i < 5
		if !rad {
			id = seed*200 + (i - 5)
		}
		rows = append(rows, fmt.Sprintf(`{"isPick": true, "heroId": %d, "order": %d, "isRadiant": %t}`, id, i, rad))
	}
	rows = append(rows, `{"isPick": false, "heroId": 7, "order": -2, "isRadiant": true}`)
	rows = append(rows, `{"isPick": false, "heroId": 8, "order": -1, "isRadiant": false}`)
	return strings.Join(rows, ", ")
}

func dupHeroPickBansJSON(seed int) string {
	return strings.Replace(pickBansJSON(seed, 10),
		fmt.Sprintf(`"heroId": %d`, seed*100+1), fmt.Sprintf(`"heroId": %d`, seed*100+0), 1)
}

func matchJSON(id int64, start int64, dur int, pickBans string) string {
	return fmt.Sprintf(`{"id": %d, "didRadiantWin": true, "durationSeconds": %d, "startDateTime": %d,
		"lobbyType": "PRACTICE", "gameMode": "CAPTAINS_MODE", "averageRank": 80, "bracket": 8,
		"pickBans": [%s], "players": [{"heroId": 1, "isRadiant": true, "lane": 1, "position": 1, "role": 1, "roleBasic": 1}]}`,
		id, dur, start, pickBans)
}

func leaguePage(matches ...string) string {
	return fmt.Sprintf(`{"data":{"league":{"matches":[%s]}}}`, strings.Join(matches, ", "))
}

func TestLeagueMatchesQueryGolden(t *testing.T) {
	q := leagueMatchesQuery(11, matchesWindowPayload("2", 1000, 2000), 100, 300)
	want := "query { league(id: 11) { matches(request: { gameModeIds: [2], startDateTime: 1000, endDateTime: 2000, take: 100, skip: 300 }) { " +
		matchFieldsSel + " } } }"
	if q != want {
		t.Errorf("query drift:\n got %s\nwant %s", q, want)
	}
	if q2 := leagueMatchesQuery(11, matchesWindowPayload("", 1000, 2000), 5, 0); !strings.Contains(q2, "request: { startDateTime: 1000") {
		t.Errorf("window-only payload malformed: %s", q2)
	}
}

func TestParseMatchesPageKeepsGoodRowsWithReasons(t *testing.T) {
	gates := matchGates{MinDurationSec: 900, Since: 1000, Until: 2000}
	body := leaguePage(
		matchJSON(1, 1500, 2400, pickBansJSON(1, 10)),
		matchJSON(2, 1500, 2000, pickBansJSON(2, 10)),
		matchJSON(3, 1500, 1900, pickBansJSON(3, 10)),
		matchJSON(4, 1500, 2400, ""),                   // no pickBans
		matchJSON(5, 1500, 2400, pickBansJSON(5, 9)),   // 9 picks
		matchJSON(6, 1500, 800, pickBansJSON(6, 10)),   // short duration
		matchJSON(7, 1500, 2400, dupHeroPickBansJSON(7)), // duplicate hero
	)
	rows, drops, err := parseMatchesPage([]byte(body), gates, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("kept %d rows, want the 3 complete drafts (drops: %v)", len(rows), drops)
	}
	if len(drops) != 4 {
		t.Fatalf("drop reasons = %v, want 4", drops)
	}
	joined := strings.Join(drops, "; ")
	for _, want := range []string{"draft", "duration"} {
		if !strings.Contains(joined, want) {
			t.Errorf("drops %q missing a %q reason", joined, want)
		}
	}
	first := rows[0]
	if first.MatchID != 1 || !first.RadiantWin || first.DurationSeconds != 2400 {
		t.Errorf("row[0] = %+v", first)
	}
	if first.LobbyType != "PRACTICE" || first.GameMode != "CAPTAINS_MODE" || first.AverageRank != 80 {
		t.Errorf("row[0] echo fields = %+v", first)
	}
	if first.LeagueID != 42 {
		t.Errorf("row[0].leagueId = %d, want the queried league", first.LeagueID)
	}
	if len(first.Draft) != 12 {
		t.Fatalf("draft = %d entries, want 10 picks + 2 bans", len(first.Draft))
	}
	if first.Draft[0].Seq != 0 || first.Draft[0].HeroID != 7 || first.Draft[0].IsPick {
		t.Errorf("draft must sort by order with 0-based seq: %+v", first.Draft[0])
	}
	if len(first.Players) != 1 || first.Players[0].HeroID != 1 || string(first.Players[0].Lane) != "1" {
		t.Errorf("players capture = %+v", first.Players)
	}
}

func TestAcceptMatchEdges(t *testing.T) {
	gates := matchGates{MinDurationSec: 900, Since: 1000, Until: 2000}
	pb := func(n int) []probePickBan {
		var out []probePickBan
		for i := 0; i < n; i++ {
			out = append(out, probePickBan{HeroID: i + 1, Order: i, IsPick: true, IsRadiant: i < 5})
		}
		return out
	}
	tests := []struct {
		name    string
		m       probeMatch
		wantOK  bool
		wantSub string
	}{
		{"good", probeMatch{ID: 1, StartDateTime: 1500, Duration: 2000, PickBans: pb(10)}, true, ""},
		{"no draft", probeMatch{ID: 2, StartDateTime: 1500, Duration: 2000}, false, "draft"},
		{"nine picks", probeMatch{ID: 3, StartDateTime: 1500, Duration: 2000, PickBans: pb(9)}, false, "draft"},
		{"unbalanced sides", probeMatch{ID: 4, StartDateTime: 1500, Duration: 2000, PickBans: func() []probePickBan {
			p := pb(10)
			p[5].IsRadiant = true
			return p
		}()}, false, "draft"},
		{"duplicate hero", probeMatch{ID: 5, StartDateTime: 1500, Duration: 2000, PickBans: func() []probePickBan {
			p := pb(10)
			p[0].HeroID = 2 // duplicates the second pick
			return p
		}()}, false, "draft"},
		{"short duration", probeMatch{ID: 6, StartDateTime: 1500, Duration: 899, PickBans: pb(10)}, false, "duration"},
		{"before epoch", probeMatch{ID: 7, StartDateTime: 999, Duration: 2000, PickBans: pb(10)}, false, "window"},
		{"after now", probeMatch{ID: 8, StartDateTime: 2001, Duration: 2000, PickBans: pb(10)}, false, "window"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := acceptMatch(tt.m, gates)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (reason %q)", ok, tt.wantOK, reason)
			}
			if tt.wantSub != "" && !strings.Contains(reason, tt.wantSub) {
				t.Errorf("reason %q missing %q", reason, tt.wantSub)
			}
		})
	}
}

func TestPageAlive(t *testing.T) {
	since := int64(1000)
	if pageAlive([]leagueEntry{{LastMatchDate: 999}}, since) {
		t.Error("fully dead page reported alive, listing would walk history")
	}
	if !pageAlive([]leagueEntry{{LastMatchDate: 999}, {LastMatchDate: 1000}}, since) {
		t.Error("page with one live league reported dead")
	}
}

func readNdjson(t *testing.T, dir string) []matchRow {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "matches.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []matchRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r matchRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("bad ndjson line: %v", err)
		}
		rows = append(rows, r)
	}
	return rows
}

func loadBackfillManifestForTest(t *testing.T, cfg *config.Config) backfillManifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfg.Paths.MatchesRawDir, "_backfill.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m backfillManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFetchStratzMatchesResumesAndDedupes(t *testing.T) {
	base := t.TempDir()
	cfg := backfillTestConfig(base)
	since := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC).Unix()
	inWindow := since + 5*86400
	preEpoch := since - 86400
	fake := &fakeMatchesClient{bodies: []string{
		fmt.Sprintf(`{"data":{"leagues":[{"id": 11, "displayName": "Test Cup", "lastMatchDate": %d}, {"id": 12, "displayName": "Dead Cup", "lastMatchDate": 100}]}}`, inWindow+60),
		leaguePage(matchJSON(1, inWindow, 2400, pickBansJSON(1, 10))),
		leaguePage(matchJSON(2, inWindow, 2000, pickBansJSON(2, 10)), matchJSON(1, inWindow, 2400, pickBansJSON(1, 10))),
		leaguePage(matchJSON(9, preEpoch, 2400, pickBansJSON(9, 10))), // pre-epoch, dropped
		leaguePage(),
	}}
	if err := fetchStratzMatches(fake, cfg, false); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 5 {
		t.Fatalf("first crawl made %d requests, want 5 (leagues + 4 league pages, dead league 12 skipped for free)", fake.calls)
	}
	rows := readNdjson(t, cfg.Paths.MatchesRawDir)
	if len(rows) != 2 {
		t.Fatalf("ndjson rows = %d, want 2 unique accepted matches (dup id 1 written once)", len(rows))
	}
	man := loadBackfillManifestForTest(t, cfg)
	if !man.Done || man.Kept != 2 || man.Dropped != 1 || man.RequestCount != 5 || man.Pages != 4 {
		t.Errorf("manifest = %+v, want done, kept 2, dropped 1, pages 4, requestCount 5", man)
	}
	if man.DropReasons["window"] != 1 {
		t.Errorf("dropReasons = %v, want window:1", man.DropReasons)
	}

	// second call: manifest done, zero live requests, append stays idempotent
	before := fake.calls
	if err := fetchStratzMatches(fake, cfg, false); err != nil {
		t.Fatal(err)
	}
	if fake.calls != before {
		t.Errorf("second crawl made %d new requests on a done manifest", fake.calls-before)
	}
	if got := len(readNdjson(t, cfg.Paths.MatchesRawDir)); got != 2 {
		t.Errorf("ndjson rows = %d after rerun, want 2", got)
	}

	// third call with refresh: the done manifest re-opens, the leagues cache
	// re-lists, dedupe keeps the already-committed rows unique, the re-walk
	// resets the drop and page counters (kept stays cumulative), and the walk
	// runs to league exhaustion (the cumulative target never stops a refresh)
	fake.bodies = append(fake.bodies,
		fmt.Sprintf(`{"data":{"leagues":[{"id": 11, "displayName": "Test Cup", "lastMatchDate": %d}]}}`, inWindow+3600),
		leaguePage(matchJSON(1, inWindow, 2400, pickBansJSON(1, 10)), matchJSON(3, inWindow+120, 2100, pickBansJSON(3, 10))),
	)
	if err := fetchStratzMatches(fake, cfg, true); err != nil {
		t.Fatal(err)
	}
	if fake.calls != before+2 {
		t.Fatalf("refresh crawl made %d new requests, want 2 (leagues re-list + served page; the exhausted page serves free)", fake.calls-before)
	}
	rows = readNdjson(t, cfg.Paths.MatchesRawDir)
	if len(rows) != 3 {
		t.Fatalf("ndjson rows = %d after refresh, want 3 (new match 3 added, dup 1 still unique)", len(rows))
	}
	man = loadBackfillManifestForTest(t, cfg)
	if !man.Done || man.Kept != 3 || man.LeagueIndex != 1 {
		t.Errorf("refresh manifest = %+v, want done, kept 3 cumulative, walked past the league", man)
	}
	if man.Dropped != 0 || man.Pages != 2 || man.DropReasons["window"] != 0 {
		t.Errorf("refresh counters = dropped %d pages %d reasons %v, want reset then two pages, no drops", man.Dropped, man.Pages, man.DropReasons)
	}

	// fourth refresh with cumulative kept already at the target: the refresh
	// must keep walking the window (extension) instead of no-op'ing on the
	// cumulative target; the dup page serves the seen count, the empty page
	// exhausts the league
	before2 := fake.calls
	fake.bodies = append(fake.bodies,
		fmt.Sprintf(`{"data":{"leagues":[{"id": 11, "displayName": "Test Cup", "lastMatchDate": %d}]}}`, inWindow+7200),
		leaguePage(matchJSON(1, inWindow, 2400, pickBansJSON(1, 10))),
	)
	if err := fetchStratzMatches(fake, cfg, true); err != nil {
		t.Fatal(err)
	}
	if fake.calls != before2+2 {
		t.Fatalf("target-satisfied refresh made %d new requests, want 2 (leagues re-list + dup page; the empty page serves free)", fake.calls-before2)
	}
	if got := len(readNdjson(t, cfg.Paths.MatchesRawDir)); got != 3 {
		t.Errorf("ndjson rows = %d after target-satisfied refresh, want 3", got)
	}
	man = loadBackfillManifestForTest(t, cfg)
	if !man.Done || man.Kept != 3 || man.Dropped != 0 || man.Pages != 2 {
		t.Errorf("manifest = %+v, want done, kept 3, drops reset, 2 pages walked", man)
	}
}

// a crash mid-append can leave a torn trailing line: the crawl must drop it
// (or repair a complete row missing its newline) so later appends never glue
// onto it and ingest keeps parsing the file
func TestFetchStratzMatchesRepairsTornTail(t *testing.T) {
	base := t.TempDir()
	cfg := backfillTestConfig(base)
	cfg.Backfill.TargetMatches = 2
	inWindow := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC).Unix()
	os.MkdirAll(cfg.Paths.MatchesRawDir, 0o755)
	wf(t, matchesNdjsonPath(cfg),
		`{"matchId": 11}`+"\n"+`{"matchId": 12}`+"\n"+`{"matchId": 13, "startDa`)
	fake := &fakeMatchesClient{bodies: []string{
		fmt.Sprintf(`{"data":{"leagues":[{"id": 11, "displayName": "Test Cup", "lastMatchDate": %d}]}}`, inWindow+60),
		leaguePage(matchJSON(13, inWindow, 2400, pickBansJSON(13, 10))),
	}}
	if err := fetchStratzMatches(fake, cfg, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(matchesNdjsonPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 3 || lines[0] != `{"matchId": 11}` || lines[2] == `{"matchId": 13, "startDa` {
		t.Fatalf("torn tail not repaired or committed lines disturbed:\n%s", raw)
	}
	var appended matchRow
	if err := json.Unmarshal([]byte(lines[2]), &appended); err != nil || appended.MatchID != 13 {
		t.Fatalf("appended line = %q, want the full match 13 row", lines[2])
	}
}

// a committed line corrupt mid-file is corruption, not a torn tail: the crawl
// must fail loudly instead of silently dropping history
func TestFetchStratzMatchesFailsOnCorruptCommittedLine(t *testing.T) {
	base := t.TempDir()
	cfg := backfillTestConfig(base)
	os.MkdirAll(cfg.Paths.MatchesRawDir, 0o755)
	wf(t, matchesNdjsonPath(cfg), "not json\n"+`{"matchId": 11}`+"\n")
	fake := &fakeMatchesClient{}
	if err := fetchStratzMatches(fake, cfg, false); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("mid-file corruption must fail loudly, got %v", err)
	}
}

func TestFetchStratzMatchesStopsAtTarget(t *testing.T) {
	base := t.TempDir()
	cfg := backfillTestConfig(base)
	cfg.Backfill.TargetMatches = 2
	inWindow := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC).Unix()
	fake := &fakeMatchesClient{bodies: []string{
		fmt.Sprintf(`{"data":{"leagues":[{"id": 11, "displayName": "Test Cup", "lastMatchDate": %d}]}}`, inWindow+60),
		leaguePage(matchJSON(1, inWindow, 2400, pickBansJSON(1, 10)), matchJSON(2, inWindow, 2000, pickBansJSON(2, 10))),
	}}
	if err := fetchStratzMatches(fake, cfg, false); err != nil {
		t.Fatal(err)
	}
	man := loadBackfillManifestForTest(t, cfg)
	if !man.Done || man.Kept != 2 {
		t.Errorf("manifest = %+v, want done at target 2", man)
	}
	if len(readNdjson(t, cfg.Paths.MatchesRawDir)) != 2 {
		t.Error("ndjson must carry exactly the target rows")
	}
}
