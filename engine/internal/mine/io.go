package mine

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"poolguide/internal/config"
)

// fallbackPopFile is the committed raw aggregate popularity file inside
// paths.stratzRawDir.
const fallbackPopFile = "_popularity.json"

// EnsureSchema extends the store with the pieces mine owns: the hero_prior
// derived table and the raw columns newer than the committed store schema
// (matchup_raw win_count and synergy, hero_stats wr). Older dbs converge to
// one shape, newer ones are a no-op.
func EnsureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hero_prior (
		  slug VARCHAR PRIMARY KEY,
		  wr DOUBLE NOT NULL,
		  tier VARCHAR NOT NULL,
		  value DOUBLE NOT NULL
		)`,
		`ALTER TABLE matchup_raw ADD COLUMN IF NOT EXISTS win_count BIGINT`,
		`ALTER TABLE matchup_raw ADD COLUMN IF NOT EXISTS synergy DOUBLE`,
		`ALTER TABLE hero_stats ADD COLUMN IF NOT EXISTS wr DOUBLE`,
		`CREATE TABLE IF NOT EXISTS hero_trend_delta (
		  slug VARCHAR PRIMARY KEY,
		  wr_delta_pp DOUBLE NOT NULL,
		  share_delta_pp DOUBLE NOT NULL,
		  from_date VARCHAR NOT NULL,
		  to_date VARCHAR NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS heatmap_enemies (
		  rank INTEGER PRIMARY KEY,
		  slug VARCHAR NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("mine schema: %w", err)
		}
	}
	return nil
}

// loadTrendRows reads the dated hero_trend snapshots rebuilt from the
// committed trends ledger at ingest.
func loadTrendRows(db *sql.DB) ([]TrendRow, error) {
	rows, err := db.Query(`SELECT snapshot_date, slug, pick_share, wr FROM hero_trend`)
	if err != nil {
		return nil, fmt.Errorf("hero_trend: %w", err)
	}
	defer rows.Close()
	var out []TrendRow
	for rows.Next() {
		var r TrendRow
		if err := rows.Scan(&r.Date, &r.Slug, &r.Share, &r.WR); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rawTables struct {
	roster    []string
	matchups  []MatchupRaw
	synergies []SynergyRaw
	stats     []HeroStat
}

func loadRaw(db *sql.DB) (rawTables, error) {
	var out rawTables
	rows, err := db.Query(`SELECT slug FROM hero_roster ORDER BY hero_id`)
	if err != nil {
		return out, fmt.Errorf("hero_roster: %w", err)
	}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			return out, err
		}
		out.roster = append(out.roster, slug)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT pool_slug, enemy_slug, wr_delta, dis, win_count, synergy,
		matches, source, scrape_date, confidence FROM matchup_raw`)
	if err != nil {
		return out, fmt.Errorf("matchup_raw: %w", err)
	}
	for rows.Next() {
		var r MatchupRaw
		if err := rows.Scan(&r.PoolSlug, &r.EnemySlug, &r.WrDelta, &r.Dis, &r.WinCount,
			&r.Synergy, &r.Matches, &r.Source, &r.ScrapeDate, &r.Confidence); err != nil {
			rows.Close()
			return out, err
		}
		out.matchups = append(out.matchups, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT hero_a, hero_b, match_count, win_count, synergy, source,
		COALESCE(bracket, ''), COALESCE(window_note, ''), COALESCE(raw_path, '') FROM synergy_raw`)
	if err != nil {
		return out, fmt.Errorf("synergy_raw: %w", err)
	}
	for rows.Next() {
		var r SynergyRaw
		if err := rows.Scan(&r.HeroA, &r.HeroB, &r.MatchCount, &r.WinCount, &r.Synergy,
			&r.Source, &r.Bracket, &r.WindowNote, &r.RawPath); err != nil {
			rows.Close()
			return out, err
		}
		out.synergies = append(out.synergies, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT slug, bracket, pick_count, win_count, source, scrape_date, wr FROM hero_stats`)
	if err != nil {
		return out, fmt.Errorf("hero_stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s HeroStat
		if err := rows.Scan(&s.Slug, &s.Bracket, &s.Picks, &s.Wins, &s.Source, &s.ScrapeDate, &s.Wr); err != nil {
			return out, err
		}
		out.stats = append(out.stats, s)
	}
	return out, rows.Err()
}

// loadFallbackPop reads the raw aggregate popularity file and reduces it to
// slug -> pick share. Aggregates like winDay carry one row per hero per
// bucket, so rows sum per hero before shares divide by the total; hero ids
// resolve through the roster table and unknown ids drop out. A missing file
// is not an error, it just disables the fallback.
func loadFallbackPop(db *sql.DB, repoRoot string, cfg *config.Config) (map[string]float64, error) {
	path := filepath.Join(repoRoot, cfg.Paths.StratzRawDir, fallbackPopFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file struct {
		Rows []struct {
			HeroID     int   `json:"heroId"`
			MatchCount int64 `json:"matchCount"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sums := map[int]int64{}
	for _, r := range file.Rows {
		sums[r.HeroID] += r.MatchCount
	}
	slugByID := map[int]string{}
	rows, err := db.Query(`SELECT hero_id, slug FROM hero_roster`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, err
		}
		slugByID[id] = slug
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// only ids the roster resolves count toward the denominator
	var total int64
	for id, sum := range sums {
		if _, ok := slugByID[id]; ok {
			total += sum
		}
	}
	if total == 0 {
		return nil, nil
	}
	out := make(map[string]float64, len(sums))
	for id, sum := range sums {
		if slug, ok := slugByID[id]; ok {
			out[slug] = float64(sum) / float64(total)
		}
	}
	return out, nil
}

type warning struct {
	subject, detail string
}

type derived struct {
	pool           []string
	enemy          []Cell
	ally           []Cell
	norms          []NormCell
	pops           map[string]float64
	popSrc         string
	wrs            map[string]float64
	priors         map[string]float64
	tiers          map[string]string
	trends         map[string]TrendDelta
	heatmapEnemies []string
	warns          []warning
	fallbackUsed   bool
}

// persist rebuilds the derived tables idempotently: one transaction,
// delete-then-insert per table.
func persist(tx *sql.Tx, d derived, now time.Time) error {
	for _, t := range []string{"pair_shrunk", "norm_cell", "popularity", "hero_prior", "hero_trend_delta", "heatmap_enemies"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	if err := insertCells(tx, KindEnemy, d.enemy); err != nil {
		return err
	}
	if err := insertCells(tx, KindAlly, d.ally); err != nil {
		return err
	}
	if err := insertNorms(tx, d.norms); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO popularity (slug, pick_share, source) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("popularity: %w", err)
	}
	defer stmt.Close()
	for _, slug := range sortedSlugs(d.pops) {
		if _, err := stmt.Exec(slug, d.pops[slug], d.popSrc); err != nil {
			return fmt.Errorf("popularity %s: %w", slug, err)
		}
	}
	stmt, err = tx.Prepare(`INSERT INTO hero_prior (slug, wr, tier, value) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("hero_prior: %w", err)
	}
	defer stmt.Close()
	for _, slug := range d.pool {
		if _, err := stmt.Exec(slug, d.wrs[slug], d.tiers[slug], d.priors[slug]); err != nil {
			return fmt.Errorf("hero_prior %s: %w", slug, err)
		}
	}
	stmt, err = tx.Prepare(`INSERT INTO hero_trend_delta (slug, wr_delta_pp, share_delta_pp, from_date, to_date)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("hero_trend_delta: %w", err)
	}
	defer stmt.Close()
	for _, slug := range d.pool {
		td, ok := d.trends[slug]
		if !ok {
			continue
		}
		if _, err := stmt.Exec(slug, td.WrDeltaPP, td.ShareDeltaPP, td.FromDate, td.ToDate); err != nil {
			return fmt.Errorf("hero_trend_delta %s: %w", slug, err)
		}
	}
	stmt, err = tx.Prepare(`INSERT INTO heatmap_enemies (rank, slug) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("heatmap_enemies: %w", err)
	}
	defer stmt.Close()
	for i, slug := range d.heatmapEnemies {
		if _, err := stmt.Exec(i+1, slug); err != nil {
			return fmt.Errorf("heatmap_enemies %s: %w", slug, err)
		}
	}
	for _, w := range d.warns {
		if err := recordProvenance(tx, now, "acceptance", w.subject, w.detail); err != nil {
			return err
		}
	}
	if d.fallbackUsed {
		if err := recordProvenance(tx, now, "fallback", "popularity",
			"roster heroes missing from hero_stats were filled from "+fallbackPopFile); err != nil {
			return err
		}
	}
	return nil
}

func insertCells(tx *sql.Tx, kind string, cells []Cell) error {
	stmt, err := tx.Prepare(`INSERT INTO pair_shrunk (hero, other, kind, value, n, post_var, source, confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("pair_shrunk: %w", err)
	}
	defer stmt.Close()
	for _, c := range cells {
		if _, err := stmt.Exec(c.Hero, c.Other, kind, c.Value, c.N, c.PostVar,
			joinSources(c.Sources), c.Confidence); err != nil {
			return fmt.Errorf("pair_shrunk %s/%s: %w", c.Hero, c.Other, err)
		}
	}
	return nil
}

func insertNorms(tx *sql.Tx, cells []NormCell) error {
	stmt, err := tx.Prepare(`INSERT INTO norm_cell (hero, other, kind, pct, z, completed)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("norm_cell: %w", err)
	}
	defer stmt.Close()
	for _, c := range cells {
		if _, err := stmt.Exec(c.Hero, c.Other, c.Kind, c.Pct, c.Z, c.Completed); err != nil {
			return fmt.Errorf("norm_cell %s/%s/%s: %w", c.Hero, c.Other, c.Kind, err)
		}
	}
	return nil
}

func recordProvenance(tx *sql.Tx, now time.Time, kind, subject, detail string) error {
	_, err := tx.Exec(`INSERT INTO provenance (kind, subject, detail, recorded_at) VALUES (?, ?, ?, ?)`,
		kind, subject, detail, now.UTC().Format(time.RFC3339))
	return err
}

// joinSources packs the canonical source labels of one cell.
func joinSources(srcs []string) string {
	out := ""
	for i, s := range srcs {
		if i > 0 {
			out += "+"
		}
		out += s
	}
	return out
}
