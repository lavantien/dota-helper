package ingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"poolguide/internal/config"
)

// Ingest loads every committed raw cache into the duckdb raw tables:
// stratz per-hero crawl + roster + popularity and the opendota heroStats
// snapshot. The stratz pair tables are rebuilt per run (mirrors of the
// week-stamped caches), everything else is keyed and replaced.
func Ingest(db *sql.DB, cfg *config.Config) error {
	rf, err := readRosterFile(cfg)
	if err != nil {
		return fmt.Errorf("roster cache missing, run `fetch stratz` first: %w", err)
	}
	rosterSlugs := map[string]bool{}
	for _, h := range rf.Heroes {
		rosterSlugs[h.Slug] = true
	}

	if err := ingestRoster(db, rf); err != nil {
		return err
	}
	stratzCount, week, err := ingestStratzCaches(db, cfg, rosterSlugs)
	if err != nil {
		return err
	}
	// dotabuff is retired from scoring: matchup_raw is keyed REPLACE-only, so
	// rows an older ingest loaded would linger forever and read as filler
	if _, err := db.Exec("DELETE FROM matchup_raw WHERE source = 'dotabuff'"); err != nil {
		return err
	}
	odCount, odDate, err := ingestOpenDotaSnapshot(db, cfg, rosterSlugs)
	if err != nil {
		return err
	}
	matchesN, draftN, err := ingestMatches(db, cfg)
	if err != nil {
		return err
	}
	positionsN, err := ingestPositions(db, cfg, rf)
	if err != nil {
		return err
	}
	// popularity runs before trends so its scope and window guards reject a
	// bad cache before the trends ledger appends lines derived from it
	popNote, err := ingestPopularity(db, cfg, rf)
	if err != nil {
		return err
	}
	trendN, err := ingestTrends(db, cfg, rf)
	if err != nil {
		return err
	}

	if err := writeProvenance(db, rf, stratzCount, cfg.Stratz.Take, odCount, week, odDate, matchesN, draftN); err != nil {
		return err
	}
	fmt.Printf("ingest: roster %d, stratz_vs %d rows on completed week %d, hero_stats %d snapshot heroes%s, match_raw %d matches / %d draft rows, hero_position %d rows, hero_trend %d rows\n",
		len(rf.Heroes), stratzCount, week, odCount, popNote, matchesN, draftN, positionsN, trendN)
	return nil
}

func ingestRoster(db *sql.DB, rf *rosterFile) error {
	if _, err := db.Exec("DELETE FROM hero_roster"); err != nil {
		return err
	}
	for _, h := range rf.Heroes {
		if _, err := db.Exec(
			"INSERT INTO hero_roster (hero_id, slug, name, npc, source, raw_path) VALUES (?, ?, ?, ?, ?, ?)",
			h.ID, h.Slug, h.Name, h.Npc, "stratz", "_roster.json",
		); err != nil {
			return err
		}
	}
	return nil
}

func ingestStratzCaches(db *sql.DB, cfg *config.Config, rosterSlugs map[string]bool) (int, int64, error) {
	rf, err := readRosterFile(cfg)
	if err != nil {
		return 0, 0, err
	}
	slugByID := map[int]string{}
	for _, h := range rf.Heroes {
		slugByID[h.ID] = h.Slug
	}
	// the pair tables pin to one calendar week: caches from mixed weeks, any
	// unstamped legacy cache, or a week that is no longer the last completed
	// one would blend stale windows into one matchup_raw, so the offender set
	// is named and the run aborts
	weekBySlug := map[string]int64{}
	for _, slug := range cfg.PoolSlugs() {
		if c := loadCache(cfg, slug); c != nil {
			weekBySlug[slug] = c.Week
		}
	}
	var unstamped []string
	for s, w := range weekBySlug {
		if w == 0 {
			unstamped = append(unstamped, s)
		}
	}
	if len(unstamped) > 0 {
		sort.Strings(unstamped)
		return 0, 0, fmt.Errorf("stratz caches carry no week stamps (%s), rerun make fetch-stratz-refresh",
			strings.Join(unstamped, ", "))
	}
	week := int64(0)
	for _, w := range weekBySlug {
		if week == 0 {
			week = w
		}
		if w != week {
			var stale []string
			for s, sw := range weekBySlug {
				if sw != week {
					stale = append(stale, fmt.Sprintf("%s=%d", s, sw))
				}
			}
			sort.Strings(stale)
			return 0, 0, fmt.Errorf("stratz caches carry mixed calendar weeks (week %d vs %s), rerun make fetch-stratz-refresh",
				week, strings.Join(stale, ", "))
		}
	}
	if cur := LastCompletedWeekStart(time.Now()); week != cur {
		return 0, 0, fmt.Errorf("stratz caches pin completed week %d but the last completed week is %d, rerun make fetch-stratz-refresh",
			week, cur)
	}
	windowNote := fmt.Sprintf("stratz completed week %d", week)
	// the pair tables are a mirror of the week-stamped caches, not an
	// accumulate: rows the current crawl no longer serves (dropped by rowSane
	// or vanished) must not survive from an older window, so both tables are
	// rebuilt per source before the insert loops
	if _, err := db.Exec("DELETE FROM matchup_raw WHERE source = 'stratz_vs'"); err != nil {
		return 0, week, err
	}
	if _, err := db.Exec("DELETE FROM synergy_raw WHERE source = 'stratz'"); err != nil {
		return 0, week, err
	}
	n := 0
	for _, slug := range cfg.PoolSlugs() {
		c := loadCache(cfg, slug)
		if c == nil {
			continue
		}
		date := fetchDate(c.FetchedAt)
		for _, r := range c.Vs {
			if !rowSane(r, cfg) {
				continue
			}
			enemy, ok := slugByID[r.HeroID2]
			if !ok || !rosterSlugs[enemy] || enemy == slug {
				continue
			}
			if _, err := db.Exec(
				`INSERT OR REPLACE INTO matchup_raw
				 (pool_slug, enemy_slug, wr_delta, dis, win_count, synergy, matches, source, scrape_date, confidence)
				 VALUES (?, ?, NULL, NULL, ?, ?, ?, 'stratz_vs', ?, 'high')`,
				slug, enemy, r.WinCount, r.Synergy, r.MatchCount, date,
			); err != nil {
				return n, week, err
			}
			n++
		}
		for _, r := range c.With {
			if !rowSane(r, cfg) {
				continue
			}
			ally, ok := slugByID[r.HeroID2]
			if !ok || !rosterSlugs[ally] || ally == slug {
				continue
			}
			if _, err := db.Exec(
				`INSERT OR REPLACE INTO synergy_raw
				 (hero_a, hero_b, match_count, win_count, synergy, source, bracket, window_note, raw_path)
				 VALUES (?, ?, ?, ?, ?, 'stratz', ?, ?, ?)`,
				slug, ally, r.MatchCount, r.WinCount, r.Synergy, c.Bracket,
				windowNote, slug+".json",
			); err != nil {
				return n, week, err
			}
		}
	}
	return n, week, nil
}

// ingestOpenDotaSnapshot loads overall winrates from the newest committed
// opendota-*.json heroStats snapshot.
func ingestOpenDotaSnapshot(db *sql.DB, cfg *config.Config, rosterSlugs map[string]bool) (int, string, error) {
	files, err := filepath.Glob(filepath.Join(cfg.Paths.MatchupsDir, "opendota-*.json"))
	if err != nil {
		return 0, "", err
	}
	if len(files) == 0 {
		return 0, "", nil
	}
	sort.Strings(files)
	path := files[len(files)-1]
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	var snap struct {
		Date            string              `json:"date"`
		OverallWrBySlug map[string]float64 `json:"overall_wr_by_slug"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return 0, "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	n := 0
	slugs := make([]string, 0, len(snap.OverallWrBySlug))
	for slug := range snap.OverallWrBySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		if !rosterSlugs[slug] {
			continue
		}
		// the snapshot is all-public, unscoped data: it carries its own
		// bracket label so the mine-side bracket match can never admit it
		// into the scoped product under a borrowed label
		if _, err := db.Exec(
			`INSERT OR REPLACE INTO hero_stats
			 (slug, bracket, pick_count, win_count, wr, source, scrape_date)
			 VALUES (?, 'ALL', 0, 0, ?, 'opendota_snapshot', ?)`,
			slug, snap.OverallWrBySlug[slug], snap.Date,
		); err != nil {
			return n, snap.Date, err
		}
		n++
	}
	return n, snap.Date, nil
}

// ingestPopularity loads the stratz aggregate probe when present. It returns
// a summary note, empty when the probe has not run.
func ingestPopularity(db *sql.DB, cfg *config.Config, rf *rosterFile) (string, error) {
	b, err := os.ReadFile(filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"))
	if err != nil {
		return "", nil
	}
	var pf popularityFile
	if json.Unmarshal(b, &pf) != nil || len(pf.Rows) == 0 {
		return "", nil
	}
	if pf.Bracket != cfg.Scope.Bracket || pf.GameMode != cfg.Scope.GameMode {
		return "", fmt.Errorf("popularity cache scope %s + gameMode %d does not match the hub scope %s + gameMode %d",
			pf.Bracket, pf.GameMode, cfg.Scope.Bracket, cfg.Scope.GameMode)
	}
	if pf.Take != cfg.Stratz.Take {
		return "", fmt.Errorf("popularity cache window take %d does not match the hub take %d, rerun make fetch-stratz-refresh",
			pf.Take, cfg.Stratz.Take)
	}
	slugByID := map[int]string{}
	for _, h := range rf.Heroes {
		slugByID[h.ID] = h.Slug
	}
	// aggregates like winDay return one row per hero per bucket: sum to the
	// per-hero totals before computing shares
	totals := map[int]*popRow{}
	var total int64
	for _, r := range pf.Rows {
		t := totals[r.HeroID]
		if t == nil {
			t = &popRow{HeroID: r.HeroID}
			totals[r.HeroID] = t
		}
		t.MatchCount += r.MatchCount
		t.WinCount += r.WinCount
		total += r.MatchCount
	}
	if total == 0 {
		return "", nil
	}
	n := 0
	for _, t := range totals {
		slug, ok := slugByID[t.HeroID]
		if !ok {
			continue
		}
		share := float64(t.MatchCount) / float64(total)
		if _, err := db.Exec(
			`INSERT OR REPLACE INTO popularity (slug, pick_share, source) VALUES (?, ?, 'stratz')`,
			slug, share,
		); err != nil {
			return "", err
		}
		if _, err := db.Exec(
			`INSERT OR REPLACE INTO hero_stats
			 (slug, bracket, pick_count, win_count, wr, source, scrape_date)
			 VALUES (?, ?, ?, ?, NULL, 'stratz_aggregate', ?)`,
			slug, pf.Bracket, t.MatchCount, t.WinCount, fetchDate(pf.FetchedAt),
		); err != nil {
			return "", err
		}
		n++
	}
	return fmt.Sprintf(", popularity %d heroes via %s", n, pf.Field), nil
}

func writeProvenance(db *sql.DB, rf *rosterFile, stratzN, take, odN int, week int64, odDate string, matchesN, draftN int) error {
	if _, err := db.Exec("DELETE FROM provenance WHERE kind = 'crawl'"); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows := [][3]string{
		{"crawl", "stratz", fmt.Sprintf("%d roster heroes, %d stratz_vs rows on completed week %d, popularity via the rolling %d-day winDay bracket+gameMode scoped, not patch pure", len(rf.Heroes), stratzN, week, take)},
		{"crawl", "opendota_snapshot", fmt.Sprintf("%d hero winrates from %s", odN, odDate)},
	}
	if matchesN > 0 {
		rows = append(rows, [3]string{"crawl", "stratz_matches",
			fmt.Sprintf("%d league matches with %d draft rows, captains-mode drafts from the per-match backfill (see ref/dota2/README.md)", matchesN, draftN)})
	}
	for _, r := range rows {
		if _, err := db.Exec(
			"INSERT INTO provenance (kind, subject, detail, recorded_at) VALUES (?, ?, ?, ?)",
			r[0], r[1], r[2], now,
		); err != nil {
			return err
		}
	}
	return nil
}
