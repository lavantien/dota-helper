// Package store owns the duckdb file: open, schema, and bulk insert helpers.
// The db lives under var/ and is rebuildable from committed raw caches.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/duckdb/duckdb-go/v2"
)

// Open creates parent dirs, opens the db, and applies the schema.
// An existing db with an older schema is replaced: raw caches are the source of truth.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return db, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS hero_roster (
  hero_id INTEGER PRIMARY KEY,
  slug VARCHAR NOT NULL UNIQUE,
  name VARCHAR NOT NULL,
  npc VARCHAR NOT NULL,
  source VARCHAR NOT NULL,
  raw_path VARCHAR
);

-- dis semantics per ref/dota2/README.md: positive = listed enemy beats file hero.
CREATE TABLE IF NOT EXISTS matchup_raw (
  pool_slug VARCHAR NOT NULL,
  enemy_slug VARCHAR NOT NULL,
  wr_delta DOUBLE,          -- advantage in pp from the pool hero side, nullable per source
  dis DOUBLE,               -- retired dotabuff disadvantage metric, no live writer
  win_count BIGINT,         -- pair wins when the source counts them (stratz vs, explorer)
  synergy DOUBLE,           -- raw stratz vs synergy delta, sign as served
  matches BIGINT NOT NULL,
  source VARCHAR NOT NULL,  -- stratz_vs | opendota_explorer | opendota_endpoint
  scrape_date VARCHAR NOT NULL,
  confidence VARCHAR NOT NULL, -- high | medium | low
  PRIMARY KEY (pool_slug, enemy_slug, source)
);

CREATE TABLE IF NOT EXISTS synergy_raw (
  hero_a VARCHAR NOT NULL,  -- pool hero
  hero_b VARCHAR NOT NULL,  -- ally
  match_count BIGINT NOT NULL,
  win_count BIGINT,
  synergy DOUBLE NOT NULL,  -- pp delta, positive = pair overperforms
  source VARCHAR NOT NULL,  -- stratz | opendota_explorer
  bracket VARCHAR,
  window_note VARCHAR,
  raw_path VARCHAR,
  PRIMARY KEY (hero_a, hero_b, source)
);

CREATE TABLE IF NOT EXISTS hero_stats (
  slug VARCHAR NOT NULL,
  bracket VARCHAR NOT NULL,
  pick_count BIGINT NOT NULL,
  win_count BIGINT NOT NULL,
  wr DOUBLE,                -- direct winrate when the source only has that (opendota snapshot)
  source VARCHAR NOT NULL,
  scrape_date VARCHAR NOT NULL,
  PRIMARY KEY (slug, bracket, source)
);

-- per-farm-position aggregates from the scoped winDay crawl (display data,
-- never enters scoring)
CREATE TABLE IF NOT EXISTS hero_position (
  slug VARCHAR NOT NULL,
  position INTEGER NOT NULL,   -- 1..5 farm position
  pick_count BIGINT NOT NULL,
  win_count BIGINT NOT NULL,
  source VARCHAR NOT NULL,     -- stratz_winday
  scrape_date VARCHAR NOT NULL,
  PRIMARY KEY (slug, position, source)
);

-- dated per-hero snapshots rebuilt from the committed trends ledger
-- (ref/dota2/trends/snapshots.ndjson) at ingest; the ledger is the source of
-- truth, this table is the queryable projection
CREATE TABLE IF NOT EXISTS hero_trend (
  snapshot_date VARCHAR NOT NULL,
  slug VARCHAR NOT NULL,
  pick_share DOUBLE NOT NULL,
  wr DOUBLE NOT NULL,
  source VARCHAR NOT NULL,     -- stratz_winday
  PRIMARY KEY (snapshot_date, slug)
);

CREATE TABLE IF NOT EXISTS provenance (
  kind VARCHAR NOT NULL,    -- crawl | probe | fallback | acceptance
  subject VARCHAR NOT NULL,
  detail VARCHAR NOT NULL,
  recorded_at VARCHAR NOT NULL
);

CREATE TABLE IF NOT EXISTS pair_shrunk (
  hero VARCHAR NOT NULL,    -- pool hero
  other VARCHAR NOT NULL,   -- enemy or ally
  kind VARCHAR NOT NULL,    -- enemy | ally
  value DOUBLE NOT NULL,    -- shrunk pp delta, positive = good for pool hero
  n BIGINT NOT NULL,
  post_var DOUBLE NOT NULL,
  source VARCHAR NOT NULL,
  confidence VARCHAR NOT NULL,
  PRIMARY KEY (hero, other, kind)
);

CREATE TABLE IF NOT EXISTS norm_cell (
  hero VARCHAR NOT NULL,
  other VARCHAR NOT NULL,
  kind VARCHAR NOT NULL,
  pct DOUBLE NOT NULL,      -- midrank percentile in [0,1]
  z DOUBLE NOT NULL,
  completed BOOLEAN NOT NULL,
  PRIMARY KEY (hero, other, kind)
);

CREATE TABLE IF NOT EXISTS popularity (
  slug VARCHAR NOT NULL PRIMARY KEY,
  pick_share DOUBLE NOT NULL, -- in [0,1] within scope
  source VARCHAR NOT NULL
);

-- per-match draft backfill (stratz league matches; see ref/dota2/README.md)
CREATE TABLE IF NOT EXISTS match_raw (
  match_id BIGINT PRIMARY KEY,
  start_time BIGINT NOT NULL,
  radiant_win BOOLEAN NOT NULL,
  duration_seconds INTEGER NOT NULL,
  lobby_type VARCHAR,          -- echoed enum token, e.g. PRACTICE
  game_mode VARCHAR,           -- echoed enum token, e.g. CAPTAINS_MODE
  bracket VARCHAR NOT NULL,    -- scope bracket the crawl ran under
  average_rank INTEGER,
  source VARCHAR NOT NULL,     -- stratz_backfill
  fetched_at VARCHAR NOT NULL  -- crawl manifest stamp
);

CREATE TABLE IF NOT EXISTS draft_timing (
  match_id BIGINT NOT NULL,
  seq INTEGER NOT NULL,        -- 0-based pick/ban order within the match draft
  is_radiant BOOLEAN NOT NULL,
  is_pick BOOLEAN NOT NULL,
  hero_id INTEGER NOT NULL,
  PRIMARY KEY (match_id, seq)
);
`
