package emit

import (
	"database/sql"
	"fmt"

	"poolguide/internal/config"
	"poolguide/internal/gates"
)

// Picker loads the derived tables and the authored prose and renders the
// picker artifact in one step; date is the run date so identical inputs give
// identical bytes.
func Picker(db *sql.DB, cfg *config.Config, doc *gates.Doc, prose *Prose, date string) ([]byte, error) {
	in, err := LoadPicker(db, cfg, doc)
	if err != nil {
		return nil, err
	}
	in.Prose = prose.Heroes
	in.Date = date
	return FormatPicker(in, cfg)
}

// Guide loads the mined matchup rows plus priors and renders the guide
// artifact.
func Guide(db *sql.DB, cfg *config.Config, date string) ([]byte, error) {
	in, err := LoadGuide(db, cfg)
	if err != nil {
		return nil, err
	}
	in.Date = date
	return FormatGuide(in, cfg)
}

// DataJSFromFiles merges the config hub, the authored prose, and the mined
// heatmap enemy columns into the GUIDE data script; cfgPath and prosePath are
// repo-relative file paths, db supplies the heatmap_enemies derived table.
func DataJSFromFiles(db *sql.DB, cfgPath, prosePath string) ([]byte, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		return nil, err
	}
	enemies, err := loadHeatmapEnemies(db, cfg)
	if err != nil {
		return nil, err
	}
	return DataJS(cfg, prose, enemies)
}

// loadHeatmapEnemies reads the mined enemy columns in rank order; a row count
// off the hub's enemyCount means mine has not run since the last rebuild.
func loadHeatmapEnemies(db *sql.DB, cfg *config.Config) ([]string, error) {
	rows, err := db.Query(`SELECT slug FROM heatmap_enemies ORDER BY rank`)
	if err != nil {
		return nil, fmt.Errorf("heatmap_enemies: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != cfg.Heatmap.EnemyCount {
		return nil, fmt.Errorf("heatmap_enemies holds %d rows, want %d, run mine before emit data", len(out), cfg.Heatmap.EnemyCount)
	}
	return out, nil
}
