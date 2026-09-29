package mine

import (
	"database/sql"
	"time"

	"poolguide/internal/config"
)

// Deps wires the mining run. RepoRoot resolves relative hub paths such as
// paths.stratzRawDir for the popularity fallback; Now stamps provenance rows
// and defaults to utc now.
type Deps struct {
	DB       *sql.DB
	Cfg      *config.Config
	RepoRoot string
	Now      func() time.Time
}

// Run derives every shrunk, normalized, and prior table from the raw tables
// and persists them in a single idempotent rebuild transaction. Raw noise
// outside the pool is ignored: matchup rows for non-pool heroes and synergy
// mirror directions seen from non-pool heroes are dropped before shrinking.
func Run(d Deps) error {
	now := d.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := EnsureSchema(d.DB); err != nil {
		return err
	}
	raw, err := loadRaw(d.DB)
	if err != nil {
		return err
	}
	fallback, err := loadFallbackPop(d.DB, d.RepoRoot, d.Cfg)
	if err != nil {
		return err
	}

	poolSet := map[string]bool{}
	for _, slug := range d.Cfg.PoolSlugs() {
		poolSet[slug] = true
	}
	var matchups []MatchupRaw
	for _, r := range raw.matchups {
		if poolSet[r.PoolSlug] {
			matchups = append(matchups, r)
		}
	}
	var synergies []SynergyRaw
	for _, r := range raw.synergies {
		if poolSet[r.HeroA] {
			synergies = append(synergies, r)
		}
	}

	var warns []warning
	enemy := MatchupCells(matchups, d.Cfg)
	allyAll := SynergyCells(synergies, d.Cfg, func(subject, detail string) {
		warns = append(warns, warning{subject: subject, detail: detail})
	})
	var ally []Cell
	for _, c := range allyAll {
		if poolSet[c.Hero] {
			ally = append(ally, c)
		}
	}

	pool := d.Cfg.PoolSlugs()
	norms := append(
		NormalizeMatrix(pool, raw.roster, enemy, KindEnemy, d.Cfg),
		NormalizeMatrix(pool, raw.roster, ally, KindAlly, d.Cfg)...,
	)
	wrs := OverallWR(matchups, pool, d.Cfg)
	priors := Priors(wrs, d.Cfg)
	pops, usedFallback := Popularity(raw.stats, raw.roster, fallback, d.Cfg)
	heatmapEnemies, err := HeatmapEnemies(pops, d.Cfg.Heatmap.EnemyCount)
	if err != nil {
		return err
	}
	trendRows, err := loadTrendRows(d.DB)
	if err != nil {
		return err
	}
	trends := Trends(pool, trendRows)

	res := derived{
		pool:           pool,
		enemy:          enemy,
		ally:           ally,
		norms:          norms,
		pops:           pops,
		popSrc:         popSourceLabel(usedFallback),
		wrs:            wrs,
		priors:         priors,
		tiers:          primaryTiers(d.Cfg),
		trends:         trends,
		heatmapEnemies: heatmapEnemies,
		warns:          warns,
		fallbackUsed:   usedFallback,
	}

	tx, err := d.DB.Begin()
	if err != nil {
		return err
	}
	if err := persist(tx, res, now()); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
