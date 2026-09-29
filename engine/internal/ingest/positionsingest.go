package ingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"poolguide/internal/config"
)

// ingestPositions loads the positions crawl cache into hero_position as a
// rebuild: the crawl folds a rolling window, so a hero vanishing from a
// position must drop its stale row rather than mix crawl windows. A missing
// cache disables the step silently (the crawl runs separately).
func ingestPositions(db *sql.DB, cfg *config.Config, rf *rosterFile) (int, error) {
	b, err := os.ReadFile(filepath.Join(cfg.Paths.PositionsRawDir, "_positions.json"))
	if err != nil {
		return 0, nil
	}
	var pf positionsFile
	if json.Unmarshal(b, &pf) != nil || len(pf.Positions) == 0 {
		return 0, nil
	}
	if pf.Bracket != cfg.Scope.Bracket || pf.GameMode != cfg.Scope.GameMode {
		return 0, fmt.Errorf("positions cache scope %s + gameMode %d does not match the hub scope %s + gameMode %d",
			pf.Bracket, pf.GameMode, cfg.Scope.Bracket, cfg.Scope.GameMode)
	}
	if pf.Take != cfg.Stratz.Take {
		return 0, fmt.Errorf("positions cache window take %d does not match the hub take %d, rerun make fetch-positions",
			pf.Take, cfg.Stratz.Take)
	}
	if _, err := db.Exec("DELETE FROM hero_position"); err != nil {
		return 0, err
	}
	slugByID := map[int]string{}
	for _, h := range rf.Heroes {
		slugByID[h.ID] = h.Slug
	}
	n := 0
	for _, p := range pf.Positions {
		for _, r := range p.Rows {
			slug, ok := slugByID[r.HeroID]
			if !ok {
				continue
			}
			if _, err := db.Exec(
				`INSERT OR REPLACE INTO hero_position
				 (slug, position, pick_count, win_count, source, scrape_date)
				 VALUES (?, ?, ?, ?, 'stratz_winday', ?)`,
				slug, p.Position, r.MatchCount, r.WinCount, fetchDate(pf.FetchedAt),
			); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
