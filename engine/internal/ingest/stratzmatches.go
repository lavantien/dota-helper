package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"poolguide/internal/config"
)

// Per-match draft backfill over stratz league matches (the per-match source
// the default token can reach; see the probe and the config backfill note).
// Reduced rows land append-only in matches.ndjson, one JSON object per line,
// with the resumable _backfill.json manifest and the cached _leagues.json
// league list beside it. Reruns skip seen match ids and never rewrite lines,
// so the file stays idempotent for ingest.

// matchDraftPick is one flattened pick/ban row; seq is the 0-based position
// in the full draft ordered by pickBans order.
type matchDraftPick struct {
	Seq       int  `json:"seq"`
	IsRadiant bool `json:"isRadiant"`
	IsPick    bool `json:"isPick"`
	HeroID    int  `json:"heroId"`
}

// matchPlayerRow echoes the per-player lane/role capture verbatim.
type matchPlayerRow struct {
	HeroID    int             `json:"heroId"`
	IsRadiant bool            `json:"isRadiant"`
	Lane      json.RawMessage `json:"lane,omitempty"`
	Position  json.RawMessage `json:"position,omitempty"`
	Role      json.RawMessage `json:"role,omitempty"`
	RoleBasic json.RawMessage `json:"roleBasic,omitempty"`
}

type matchRow struct {
	MatchID         int64            `json:"matchId"`
	StartDateTime   int64            `json:"startDateTime"`
	RadiantWin      bool             `json:"radiantWin"`
	DurationSeconds int              `json:"durationSeconds"`
	LobbyType       string           `json:"lobbyType"`
	GameMode        string           `json:"gameMode"`
	AverageRank     int              `json:"averageRank"`
	LeagueID        int64            `json:"leagueId"`
	Draft           []matchDraftPick `json:"draft"`
	Players         []matchPlayerRow `json:"players,omitempty"`
}

// matchGates carries the acceptance thresholds from the config hub.
type matchGates struct {
	MinDurationSec int
	Since          int64
	Until          int64
}

// acceptMatch applies the per-match gates: a complete 10-pick draft (5 per
// side, unique heroes), duration floor, and the patch window. Rejects are
// counted with reasons, never fatal.
func acceptMatch(m probeMatch, g matchGates) (bool, string) {
	if reason := draftReject(m.PickBans); reason != "" {
		return false, reason
	}
	if m.Duration < g.MinDurationSec {
		return false, fmt.Sprintf("duration %d < %d", m.Duration, g.MinDurationSec)
	}
	if m.StartDateTime < g.Since || m.StartDateTime > g.Until {
		return false, fmt.Sprintf("window %d outside [%d, %d]", m.StartDateTime, g.Since, g.Until)
	}
	return true, ""
}

func draftReject(pickBans []probePickBan) string {
	if len(pickBans) == 0 {
		return "draft missing pickBans"
	}
	picks, radiant := 0, 0
	heroes := map[int]bool{}
	for _, pb := range pickBans {
		if !pb.IsPick {
			continue
		}
		picks++
		if pb.IsRadiant {
			radiant++
		}
		if heroes[pb.HeroID] {
			return fmt.Sprintf("draft duplicate hero %d", pb.HeroID)
		}
		heroes[pb.HeroID] = true
	}
	if picks != 10 {
		return fmt.Sprintf("draft has %d picks, want 10", picks)
	}
	if radiant != 5 {
		return fmt.Sprintf("draft has %d radiant picks, want 5", radiant)
	}
	return ""
}

// flattenDraft sorts by order and stamps 0-based seq.
func flattenDraft(pickBans []probePickBan) []matchDraftPick {
	sorted := make([]probePickBan, len(pickBans))
	copy(sorted, pickBans)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	out := make([]matchDraftPick, len(sorted))
	for i, pb := range sorted {
		out[i] = matchDraftPick{Seq: i, IsRadiant: pb.IsRadiant, IsPick: pb.IsPick, HeroID: pb.HeroID}
	}
	return out
}

// rawToken renders a scalar RawMessage without its JSON quotes.
func rawToken(raw json.RawMessage) string {
	return strings.Trim(string(raw), `"`)
}

func reduceMatch(m probeMatch, leagueID int64) matchRow {
	row := matchRow{
		MatchID:         m.ID,
		StartDateTime:   m.StartDateTime,
		RadiantWin:      m.DidRadiantWin != nil && *m.DidRadiantWin,
		DurationSeconds: m.Duration,
		LobbyType:       rawToken(m.LobbyType),
		GameMode:        rawToken(m.GameMode),
		AverageRank:     m.AverageRank,
		LeagueID:        leagueID,
		Draft:           flattenDraft(m.PickBans),
	}
	for _, p := range m.Players {
		row.Players = append(row.Players, matchPlayerRow{
			HeroID: p.HeroID, IsRadiant: p.IsRadiant,
			Lane: p.Lane, Position: p.Position, Role: p.Role, RoleBasic: p.RoleBasic,
		})
	}
	return row
}

// parseMatchesPage decodes one league page, keeping accepted rows and the
// drop reasons for the rest.
func parseMatchesPage(body []byte, gates matchGates, leagueID int64) ([]matchRow, []string, error) {
	var d struct {
		League struct {
			Matches []probeMatch `json:"matches"`
		} `json:"league"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, nil, err
	}
	var rows []matchRow
	var drops []string
	for _, m := range d.League.Matches {
		ok, reason := acceptMatch(m, gates)
		if !ok {
			drops = append(drops, fmt.Sprintf("match %d: %s", m.ID, reason))
			continue
		}
		rows = append(rows, reduceMatch(m, leagueID))
	}
	return rows, drops, nil
}

// backfillManifest is the resume record at matchesRawDir/_backfill.json.
type backfillManifest struct {
	CrawledAt    string         `json:"crawledAt"`
	Bracket      string         `json:"bracket"`
	FilterEcho   string         `json:"filterEcho"`
	Pages        int            `json:"pages"`
	Kept         int            `json:"kept"`
	Dropped      int            `json:"dropped"`
	DropReasons  map[string]int `json:"dropReasons"`
	LastSkip     int            `json:"lastSkip"`
	LeagueIndex  int            `json:"leagueIndex"`
	LastLeagueID int64          `json:"lastLeagueId"`
	Done         bool           `json:"done"`
	RequestCount int            `json:"requestCount"`
}

// gqlFetcher is the one-method seam the crawler needs; *stratzClient
// satisfies it and tests fake it.
type gqlFetcher interface {
	query(q string) ([]byte, error)
}

func matchesNdjsonPath(cfg *config.Config) string {
	return filepath.Join(cfg.Paths.MatchesRawDir, "matches.ndjson")
}

func backfillManifestPath(cfg *config.Config) string {
	return filepath.Join(cfg.Paths.MatchesRawDir, "_backfill.json")
}

func loadBackfillManifest(cfg *config.Config) *backfillManifest {
	b, err := os.ReadFile(backfillManifestPath(cfg))
	if err != nil {
		return nil
	}
	var m backfillManifest
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

// loadSeenMatches rebuilds the dedupe set from the committed ndjson so
// appended lines stay unique across resumes. An unparsable line mid-file is
// corruption and fails loudly. An unterminated trailing line is a crash
// mid-append: a complete row missing only its newline is kept and terminated,
// a torn fragment is dropped, so later appends never glue onto it and ingest
// keeps parsing the file.
func loadSeenMatches(cfg *config.Config) (map[int64]bool, error) {
	raw, err := os.ReadFile(matchesNdjsonPath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return map[int64]bool{}, nil
		}
		return nil, err
	}
	seen := map[int64]bool{}
	s := string(raw)
	if s != "" && !strings.HasSuffix(s, "\n") {
		idx := strings.LastIndexByte(s, '\n')
		var head, tail string
		if idx < 0 {
			head, tail = "", s
		} else {
			head, tail = s[:idx+1], s[idx+1:]
		}
		var r matchRow
		if json.Unmarshal([]byte(tail), &r) == nil && r.MatchID != 0 {
			seen[r.MatchID] = true
			head += tail + "\n"
		}
		if err := os.WriteFile(matchesNdjsonPath(cfg), []byte(head), 0o644); err != nil {
			return nil, err
		}
		s = head
	}
	for i, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		var r matchRow
		if json.Unmarshal([]byte(line), &r) != nil || r.MatchID == 0 {
			return nil, fmt.Errorf("matches ndjson line %d corrupt: %q", i+1, line)
		}
		seen[r.MatchID] = true
	}
	return seen, nil
}

// FetchStratzMatches runs the per-match backfill with the shared client,
// limiter, and backoff. With refresh, a done manifest is re-opened instead
// of short-circuited: the window end extends to now and the walk resumes
// from the league list start, dedupe keeping already-committed matches out.
func FetchStratzMatches(cfg *config.Config, refresh bool) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	return fetchStratzMatches(newStratzClient(cfg, token), cfg, refresh)
}

// fetchStratzMatches walks league match pages until the target, the league
// list, or the per-run request budget is spent, appending accepted rows to
// matches.ndjson and checkpointing the manifest after every page.
func fetchStratzMatches(c gqlFetcher, cfg *config.Config, refresh bool) error {
	since, err := time.Parse("2006-01-02", cfg.PatchEpoch)
	if err != nil {
		return fmt.Errorf("patchEpoch: %w", err)
	}
	gates := matchGates{
		MinDurationSec: cfg.Backfill.MinDurationSec,
		Since:          since.Unix(),
		Until:          time.Now().Unix(),
	}
	manifest := loadBackfillManifest(cfg)
	if manifest != nil && manifest.Done && !refresh {
		fmt.Printf("matches backfill: done already, %d kept / %d dropped over %d requests\n",
			manifest.Kept, manifest.Dropped, manifest.RequestCount)
		return nil
	}
	if manifest != nil && refresh {
		// re-open the finished walk: cumulative kept and request counters
		// stay, the paging position resets so new league matches in the
		// extended window are picked up, the leagues cache re-lists to see
		// leagues that went live since the last pass, and the drop and page
		// counters reset so the re-walk does not double-count rejected rows
		manifest.Done = false
		manifest.LeagueIndex = 0
		manifest.LastSkip = 0
		manifest.Dropped = 0
		manifest.Pages = 0
		manifest.DropReasons = map[string]int{}
		if err := os.Remove(leaguesCachePath(cfg)); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("matches backfill: refresh re-opens the done manifest (%d kept cumulatively)\n", manifest.Kept)
	}
	if manifest == nil {
		manifest = &backfillManifest{
			Bracket:     cfg.Scope.Bracket,
			DropReasons: map[string]int{},
		}
	}
	if manifest.DropReasons == nil {
		manifest.DropReasons = map[string]int{}
	}

	leagues, listingCalls, err := fetchLeaguesCache(c, cfg, gates.Since)
	if err != nil {
		return err
	}
	manifest.RequestCount += listingCalls
	runRequests := listingCalls
	fmt.Printf("matches backfill: %d leagues cached, resuming at index %d skip %d\n",
		len(leagues), manifest.LeagueIndex, manifest.LastSkip)

	seen, err := loadSeenMatches(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Paths.MatchesRawDir, 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(matchesNdjsonPath(cfg), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriter(out)

	payload := matchesWindowPayload(fmt.Sprintf("%d", cfg.Backfill.GameMode), gates.Since, gates.Until)
	manifest.FilterEcho = payload
	take := clampTake(cfg.Backfill.Take)

	// crawlPage returns how many rows the page served (accepted + dropped):
	// a served count of zero is the league-exhausted signal, an all-dropped
	// page must keep paging.
	crawlPage := func(leagueID int64, skip int) (int, error) {
		body, err := c.query(leagueMatchesQuery(leagueID, payload, take, skip))
		if err != nil {
			return 0, err
		}
		rows, drops, err := parseMatchesPage(body, gates, leagueID)
		if err != nil {
			return 0, err
		}
		for _, r := range rows {
			if seen[r.MatchID] {
				continue
			}
			b, err := json.Marshal(r)
			if err != nil {
				return 0, err
			}
			if _, err := w.Write(append(b, '\n')); err != nil {
				return 0, err
			}
			seen[r.MatchID] = true
			manifest.Kept++
		}
		manifest.Dropped += len(drops)
		for _, d := range drops {
			manifest.DropReasons[dropKind(d)]++
		}
		manifest.Pages++
		manifest.RequestCount++
		runRequests++
		manifest.LastSkip = skip
		manifest.LastLeagueID = leagueID
		return len(rows) + len(drops), nil
	}

	done := false
budget:
	for i := manifest.LeagueIndex; i < len(leagues); i++ {
		if leagues[i].LastMatchDate < gates.Since {
			// dead league: skip without spending a request
			manifest.LeagueIndex = i + 1
			continue
		}
		manifest.LeagueIndex = i
		skip := 0
		if leagues[i].ID == manifest.LastLeagueID {
			skip = manifest.LastSkip
		}
		for {
			// the corpus target never stops a refresh: cumulative kept counts
			// committed matches, so the gate would no-op the window extension
			if !refresh && manifest.Kept >= cfg.Backfill.TargetMatches {
				done = true
				break budget
			}
			if runRequests >= cfg.Backfill.MaxRequests {
				break budget
			}
			n, err := crawlPage(leagues[i].ID, skip)
			if err != nil {
				return fmt.Errorf("league %d skip %d: %w", leagues[i].ID, skip, err)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			manifest.CrawledAt = time.Now().UTC().Format(time.RFC3339)
			if err := writeJson(backfillManifestPath(cfg), manifest); err != nil {
				return err
			}
			if n == 0 {
				// page exhausted (or fully deduped/seen): league done
				break
			}
			skip += take
		}
		if runRequests%20 == 0 || done {
			fmt.Printf("matches backfill: league %d %q done, %d kept so far, %d requests\n",
				leagues[i].ID, leagues[i].DisplayName, manifest.Kept, manifest.RequestCount)
		}
		manifest.LastSkip = 0
		manifest.LastLeagueID = 0
		manifest.LeagueIndex = i + 1
	}
	if (!refresh && manifest.Kept >= cfg.Backfill.TargetMatches) || manifest.LeagueIndex >= len(leagues) {
		done = true
	}
	manifest.Done = done
	manifest.CrawledAt = time.Now().UTC().Format(time.RFC3339)
	if err := writeJson(backfillManifestPath(cfg), manifest); err != nil {
		return err
	}
	status := "done"
	if !done {
		status = "paused (budget), rerun to resume"
	}
	fmt.Printf("matches backfill %s: %d kept, %d dropped (%s), %d pages, %d requests\n",
		status, manifest.Kept, manifest.Dropped, dropSummary(manifest.DropReasons), manifest.Pages, manifest.RequestCount)
	return nil
}

// dropKind buckets a drop reason into a counting key.
func dropKind(reason string) string {
	switch {
	case strings.Contains(reason, "draft"):
		return "draft"
	case strings.Contains(reason, "duration"):
		return "duration"
	case strings.Contains(reason, "window"):
		return "window"
	default:
		return "other"
	}
}

func dropSummary(reasons map[string]int) string {
	var parts []string
	for _, k := range []string{"draft", "duration", "window", "other"} {
		if reasons[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", k, reasons[k]))
		}
	}
	return strings.Join(parts, ", ")
}
