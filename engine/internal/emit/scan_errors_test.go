package emit

import (
	"database/sql"
	"testing"

	"poolguide/internal/gates"
)

// TestLoaderScanErrors recreates one derived table per case with a nullable
// column and inserts a NULL, so each loader's row-scan error return fires
// against a real duckdb instead of staying a dormant branch.
func TestLoaderScanErrors(t *testing.T) {
	cfg := fixtureCfg(t)
	doc := &gates.Doc{}
	tests := []struct {
		name   string
		table  string
		ddl    string
		insert string
		call   func(db *sql.DB) error
	}{
		{
			"roster npc null", "hero_roster",
			`CREATE TABLE hero_roster (hero_id INTEGER, slug VARCHAR, name VARCHAR, npc VARCHAR, source VARCHAR, raw_path VARCHAR)`,
			`INSERT INTO hero_roster VALUES (1, 'axe', 'Axe', NULL, 'test', NULL)`,
			func(db *sql.DB) error { _, err := LoadRoster(db); return err },
		},
		{
			"popularity share null", "popularity",
			`CREATE TABLE popularity (slug VARCHAR, pick_share DOUBLE, source VARCHAR)`,
			`INSERT INTO popularity VALUES ('axe', NULL, 'hero_stats')`,
			func(db *sql.DB) error { _, err := LoadPicker(db, cfg, doc); return err },
		},
		{
			"hero_prior value null", "hero_prior",
			`CREATE TABLE hero_prior (slug VARCHAR, wr DOUBLE, tier VARCHAR, value DOUBLE)`,
			`INSERT INTO hero_prior VALUES ('axe', 0.5, 'dedicated', NULL)`,
			func(db *sql.DB) error { _, err := LoadPicker(db, cfg, doc); return err },
		},
		{
			"hero_prior wr null", "hero_prior",
			`CREATE TABLE hero_prior (slug VARCHAR, wr DOUBLE, tier VARCHAR, value DOUBLE)`,
			`INSERT INTO hero_prior VALUES ('axe', NULL, 'dedicated', 0.5)`,
			func(db *sql.DB) error { _, err := LoadGuide(db, cfg); return err },
		},
		{
			"hero_trend_delta picker scan", "hero_trend_delta",
			`CREATE TABLE hero_trend_delta (slug VARCHAR, wr_delta_pp DOUBLE, share_delta_pp DOUBLE, from_date VARCHAR, to_date VARCHAR)`,
			`INSERT INTO hero_trend_delta VALUES ('axe', 1, 1, NULL, '2026-09-27')`,
			func(db *sql.DB) error { _, err := LoadPicker(db, cfg, doc); return err },
		},
		{
			"hero_trend_delta guide scan", "hero_trend_delta",
			`CREATE TABLE hero_trend_delta (slug VARCHAR, wr_delta_pp DOUBLE, share_delta_pp DOUBLE, from_date VARCHAR, to_date VARCHAR)`,
			`INSERT INTO hero_trend_delta VALUES ('axe', 1, 1, NULL, '2026-09-27')`,
			func(db *sql.DB) error { _, err := LoadGuide(db, cfg); return err },
		},
		{
			"hero_position picks null", "hero_position",
			`CREATE TABLE hero_position (slug VARCHAR, position INTEGER, pick_count BIGINT, win_count BIGINT, source VARCHAR, scrape_date VARCHAR)`,
			`INSERT INTO hero_position VALUES ('axe', 1, NULL, 1, 'stratz_winday', '2026-09-24')`,
			func(db *sql.DB) error { _, err := LoadPicker(db, cfg, doc); return err },
		},
		{
			"norm_cell pct null", "norm_cell",
			`CREATE TABLE norm_cell (hero VARCHAR, other VARCHAR, kind VARCHAR, pct DOUBLE, z DOUBLE, completed BOOLEAN)`,
			`INSERT INTO norm_cell VALUES ('axe', 'pudge', 'enemy', NULL, NULL, false)`,
			func(db *sql.DB) error { _, err := LoadPicker(db, cfg, doc); return err },
		},
		{
			"norm_cell z null", "norm_cell",
			`CREATE TABLE norm_cell (hero VARCHAR, other VARCHAR, kind VARCHAR, pct DOUBLE, z DOUBLE, completed BOOLEAN)`,
			`INSERT INTO norm_cell VALUES ('axe', 'pudge', 'enemy', 0.5, NULL, false)`,
			func(db *sql.DB) error { _, err := LoadGuide(db, cfg); return err },
		},
		{
			"matchup_raw scrape null", "matchup_raw",
			`CREATE TABLE matchup_raw (pool_slug VARCHAR, scrape_date VARCHAR, source VARCHAR)`,
			`INSERT INTO matchup_raw VALUES ('axe', NULL, 'stratz_vs')`,
			func(db *sql.DB) error { _, err := LoadGuide(db, cfg); return err },
		},
		{
			"heatmap_enemies slug null", "heatmap_enemies",
			`CREATE TABLE heatmap_enemies (rank INTEGER, slug VARCHAR)`,
			`INSERT INTO heatmap_enemies VALUES (1, NULL)`,
			func(db *sql.DB) error { _, err := loadHeatmapEnemies(db, cfg); return err },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := bareDB(t)
			execFixture(t, db, "DROP TABLE "+tt.table)
			execFixture(t, db, tt.ddl)
			execFixture(t, db, tt.insert)
			if err := tt.call(db); err == nil {
				t.Fatalf("loader must fail on %s", tt.name)
			}
		})
	}
}
