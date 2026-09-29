package ingest

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"poolguide/internal/config"
)

// ingestMatches loads the committed per-match backfill (matches.ndjson plus
// its manifest) into match_raw and draft_timing. Rows are keyed and replaced,
// never appended, so reruns are idempotent. A missing manifest means the
// crawl has not run: that disables the step, it never fails ingest.
func ingestMatches(db *sql.DB, cfg *config.Config) (int, int, error) {
	manifest := loadBackfillManifest(cfg)
	if manifest == nil {
		return 0, 0, nil
	}
	f, err := os.Open(filepath.Join(cfg.Paths.MatchesRawDir, "matches.ndjson"))
	if err != nil {
		return 0, 0, nil
	}
	defer f.Close()

	matchStmt, err := db.Prepare(`INSERT OR REPLACE INTO match_raw
		(match_id, start_time, radiant_win, duration_seconds, lobby_type, game_mode, bracket, average_rank, source, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'stratz_backfill', ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("match_raw prepare: %w", err)
	}
	defer matchStmt.Close()
	draftStmt, err := db.Prepare(`INSERT OR REPLACE INTO draft_timing
		(match_id, seq, is_radiant, is_pick, hero_id) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("draft_timing prepare: %w", err)
	}
	defer draftStmt.Close()

	matches, draftRows := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r matchRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return matches, draftRows, fmt.Errorf("matches.ndjson line %d: %w", matches+1, err)
		}
		if r.MatchID == 0 {
			continue
		}
		if _, err := matchStmt.Exec(r.MatchID, r.StartDateTime, r.RadiantWin, r.DurationSeconds,
			r.LobbyType, r.GameMode, cfg.Scope.Bracket, r.AverageRank, manifest.CrawledAt); err != nil {
			return matches, draftRows, fmt.Errorf("match_raw %d: %w", r.MatchID, err)
		}
		matches++
		for _, d := range r.Draft {
			if _, err := draftStmt.Exec(r.MatchID, d.Seq, d.IsRadiant, d.IsPick, d.HeroID); err != nil {
				return matches, draftRows, fmt.Errorf("draft_timing %d/%d: %w", r.MatchID, d.Seq, err)
			}
			draftRows++
		}
	}
	return matches, draftRows, sc.Err()
}
