package mine

import (
	"sort"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
)

// wrPrior is the fair-win rate the overall win rate shrinks toward; it is
// the neutral midpoint, not a tunable.
const wrPrior = 0.5

// OverallWR derives each pool hero's in-scope overall win rate. The only
// source is the hero's own stratz_vs rows: summed match and win counts give
// the overall rate in the crawled bracket, and a vs row exists for every
// match once per enemy, so both sums divide by the enemy slot count. The
// opendota snapshot wr is all-public data stamped bracket ALL, so it never
// fills under the scope policy; a pool hero with no vs rows reads the 0.5
// prior.
func OverallWR(matchups []MatchupRaw, pool []string, cfg *config.Config) map[string]float64 {
	wins, picks := map[string]float64{}, map[string]float64{}
	slots := float64(cfg.Score.EnemySlots)
	for _, r := range matchups {
		if r.Source != SrcStratzVS || !r.WinCount.Valid {
			continue
		}
		wins[r.PoolSlug] += float64(r.WinCount.Int64) / slots
		picks[r.PoolSlug] += float64(r.Matches) / slots
	}
	out := make(map[string]float64, len(pool))
	for _, slug := range pool {
		out[slug] = analytics.Shrink(wins[slug], picks[slug], cfg.Shrink.OverallWrAlpha, wrPrior)
	}
	return out
}

// primaryTiers maps each pool slug to the tier of its first hub entry. The
// persisted tier label shares this rule; it never enters scoring.
func primaryTiers(cfg *config.Config) map[string]string {
	out := map[string]string{}
	for _, e := range cfg.Pool {
		if _, ok := out[e.Slug]; !ok {
			out[e.Slug] = e.Tier
		}
	}
	return out
}

// Priors scores every pool hero with analytics.Prior over the shrunk overall
// win rates, in pool slug ascending order.
func Priors(wrs map[string]float64, cfg *config.Config) map[string]float64 {
	pool := cfg.PoolSlugs()
	vals := make([]float64, len(pool))
	for i, slug := range pool {
		vals[i] = wrs[slug]
	}
	scored := analytics.Prior(vals, cfg)
	out := make(map[string]float64, len(pool))
	for i, slug := range pool {
		out[slug] = scored[i]
	}
	return out
}

// Popularity normalizes in-scope pick shares so the roster shares sum to 1.
// hero_stats is primary: counts become shares of the in-scope pick total.
// The raw aggregate fallback (already share-shaped) fills roster heroes with
// no stats and flags that it was used; the merged vector is renormalized.
func Popularity(stats []HeroStat, roster []string, fallback map[string]float64, cfg *config.Config) (map[string]float64, bool) {
	picks := map[string]int64{}
	totalPicks := 0.0
	for _, s := range stats {
		if s.Bracket != cfg.Scope.Bracket || s.Picks <= 0 {
			continue
		}
		picks[s.Slug] += s.Picks
		totalPicks += float64(s.Picks)
	}
	usedFallback := false
	shares := make(map[string]float64, len(roster))
	for _, slug := range roster {
		if totalPicks > 0 && picks[slug] > 0 {
			shares[slug] = float64(picks[slug]) / totalPicks
			continue
		}
		if v, ok := fallback[slug]; ok {
			shares[slug] = v
			usedFallback = true
		}
	}
	total := 0.0
	for _, v := range shares {
		total += v
	}
	if total > 0 {
		for k := range shares {
			shares[k] /= total
		}
	}
	return shares, usedFallback
}

// popSourceLabel names the popularity provenance row.
func popSourceLabel(usedFallback bool) string {
	if usedFallback {
		return "stratz_raw_aggregate"
	}
	return "hero_stats"
}

// sortedSlugs returns map keys ascending.
func sortedSlugs(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
