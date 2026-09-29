package ingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"poolguide/internal/config"
)

// trendSnap is one line of the committed trends ledger.
type trendSnap struct {
	Date      string  `json:"date"`
	Scope     string  `json:"scope"`
	Field     string  `json:"field"`
	Window    int     `json:"window,omitempty"`
	Slug      string  `json:"slug"`
	PickShare float64 `json:"pick_share"`
	WR        float64 `json:"wr"`
}

// legacyTrendWindowDays attributes pre-window-stamp snapshots: the old hub
// take 200 was live-identical to take 30 on winDay, so those lines read as a
// 30-day window and no delta ever straddles the window cut.
const legacyTrendWindowDays = 30

// snapWindow reads a snapshot's window with the legacy attribution.
func snapWindow(s trendSnap) int {
	if s.Window == 0 {
		return legacyTrendWindowDays
	}
	return s.Window
}

// ingestTrends appends today's per-hero snapshot to the committed trends
// ledger (the source of truth: the duckdb under var/ is rebuildable) and
// rebuilds the hero_trend projection from the whole ledger. The snapshot
// date is the crawl stamp, so a same-day re-crawl appends nothing and the
// first crawl of the day wins. A missing popularity cache disables the
// step silently.
func ingestTrends(db *sql.DB, cfg *config.Config, rf *rosterFile) (int, error) {
	b, err := os.ReadFile(filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"))
	if err != nil {
		return 0, nil
	}
	var pf popularityFile
	if json.Unmarshal(b, &pf) != nil || len(pf.Rows) == 0 {
		return 0, nil
	}
	mode, ok := gameModeEnumToken(pf.GameMode)
	if !ok {
		return 0, fmt.Errorf("trends: gameMode %d has no known enum token", pf.GameMode)
	}
	scope := pf.Bracket + "+" + mode
	date := fetchDate(pf.FetchedAt)

	byHero := popRowsByHero(pf.Rows)
	slugByID := map[int]string{}
	for _, h := range rf.Heroes {
		slugByID[h.ID] = h.Slug
	}
	var total int64
	totals := map[string]popRow{}
	for id, r := range byHero {
		slug, ok := slugByID[id]
		if !ok {
			continue
		}
		totals[slug] = r
		total += r.MatchCount
	}
	if total == 0 {
		return 0, nil
	}

	ledgerPath := cfg.Paths.TrendsFile
	var snaps []trendSnap
	if raw, err := os.ReadFile(ledgerPath); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var s trendSnap
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				return 0, fmt.Errorf("trends ledger %s: bad line %q: %w", ledgerPath, line, err)
			}
			snaps = append(snaps, s)
		}
	}
	for _, s := range snaps {
		if s.Date == date && s.Scope == scope && snapWindow(s) == pf.Take {
			// today already snapshotted under this scope and window: rebuild
			// the projection and stop
			return rebuildTrendTable(db, snaps)
		}
	}
	slugs := make([]string, 0, len(totals))
	for slug := range totals {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		t := totals[slug]
		snaps = append(snaps, trendSnap{
			Date: date, Scope: scope, Field: pf.Field, Window: pf.Take, Slug: slug,
			PickShare: float64(t.MatchCount) / float64(total),
			WR:        float64(t.WinCount) / float64(t.MatchCount),
		})
	}
	var sb strings.Builder
	for _, s := range snaps {
		line, err := json.Marshal(s)
		if err != nil {
			return 0, err
		}
		sb.Write(line)
		sb.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
		return 0, err
	}
	// the ledger is the committed source of truth: write the new generation
	// beside it and swap atomically so a crash can never truncate history
	tmp := ledgerPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, ledgerPath); err != nil {
		return 0, err
	}
	return rebuildTrendTable(db, snaps)
}

// rebuildTrendTable projects the ledger into hero_trend, latest (scope,
// window) only: older snapshots stay in the ledger for the record, but the
// projection and the latest-pair deltas never straddle a scope or window
// change. Ties on date take the later ledger line, which is the newer ingest.
func rebuildTrendTable(db *sql.DB, snaps []trendSnap) (int, error) {
	latestDate, latestScope, latestWindow := "", "", 0
	for _, s := range snaps {
		if s.Date >= latestDate {
			latestDate, latestScope, latestWindow = s.Date, s.Scope, snapWindow(s)
		}
	}
	if _, err := db.Exec("DELETE FROM hero_trend"); err != nil {
		return 0, err
	}
	n := 0
	for _, s := range snaps {
		if s.Scope != latestScope || snapWindow(s) != latestWindow {
			continue
		}
		if _, err := db.Exec(
			`INSERT INTO hero_trend (snapshot_date, slug, pick_share, wr, source) VALUES (?, ?, ?, ?, 'stratz_winday')`,
			s.Date, s.Slug, s.PickShare, s.WR,
		); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
