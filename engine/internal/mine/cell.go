// Package mine derives the shrunk and normalized tables from the raw crawl
// tables: per-cell shrinkage, ALS matrix completion, midrank normalization,
// per-hero priors, and popularity. Every step is a pure function over plain
// row structs; only the io layer touches the database.
package mine

import (
	"database/sql"
	"fmt"
	"sort"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
)

// Raw source labels, the matchup_raw / synergy_raw / hero_stats vocabulary
// fixed by the store schema comment. dotabuff is retired: ingest purges its
// rows, so no code path admits it back.
const (
	SrcStratzVS   = "stratz_vs"
	SrcStratz     = "stratz"
	SrcOpenDotaEx = "opendota_explorer"
	SrcOpenDotaEp = "opendota_endpoint"
)

// Confidence labels, the matchup_raw.confidence vocabulary.
const (
	ConfHigh = "high"
	ConfMed  = "medium"
	ConfLow  = "low"
)

// MatchupRaw is one matchup_raw row.
type MatchupRaw struct {
	PoolSlug, EnemySlug string
	WrDelta, Dis        sql.NullFloat64
	WinCount            sql.NullInt64
	Synergy             sql.NullFloat64
	Matches             int64
	Source, ScrapeDate  string
	Confidence          string
}

// SynergyRaw is one synergy_raw row.
type SynergyRaw struct {
	HeroA, HeroB        string
	MatchCount          int64
	WinCount            sql.NullInt64
	Synergy             float64
	Source, Bracket     string
	WindowNote, RawPath string
}

// HeroStat is one hero_stats row. Wr carries the direct win-rate served by
// opendota snapshot rows instead of pick and win counts.
type HeroStat struct {
	Slug, Bracket, Source, ScrapeDate string
	Picks, Wins                       int64
	Wr                                sql.NullFloat64
}

// Cell is one shrunk pair value: positive is good for the pool hero.
type Cell struct {
	Hero, Other string
	Value       float64
	N           int64
	PostVar     float64
	Sources     []string
	Confidence  string
}

// canonSource maps a raw source label onto the per-hero source vocabulary of
// the picker data contract: stratz | opendota_explorer.
func canonSource(src string) string {
	switch src {
	case SrcStratzVS, SrcStratz:
		return SrcStratz
	case SrcOpenDotaEx, SrcOpenDotaEp:
		return SrcOpenDotaEx
	}
	return src
}

// confRank ranks confidence labels for combining: high beats medium beats
// low, and unknown labels read as low.
func confRank(label string) int {
	switch label {
	case ConfHigh:
		return 0
	case ConfMed:
		return 1
	}
	return 2
}

func confLabel(rank int) string {
	switch rank {
	case 0:
		return ConfHigh
	case 1:
		return ConfMed
	}
	return ConfLow
}

// PPVar is the worst-case variance of a win-rate estimate in squared
// percentage points: 0.25 * 100^2. The posterior variance of a shrunk cell
// scales it by n/(n+alpha)^2, the squared shrinkage factor.
const PPVar = 2500.0

// canonicalPP adapts one raw matchup row to a delta in pp where positive is
// good for the pool hero. Sign conventions per source, fixed at the adapter:
//   - stratz_vs: hero-perspective, the served vs value is positive when the
//     pool hero beats the enemy. Verified in-db two ways: within-row
//     sign(synergy) agrees with sign(winCount/matches - 0.5) on 94.6% of
//     rows with |synergy| >= 3pp, and the pooled per-hero rate sits 1.35pp
//     from the independent opendota snapshot versus 3.61pp complemented.
//   - opendota_explorer: only counts are served, the delta is the win rate's
//     distance from the fair midpoint (config score.midPct) in pp.
func canonicalPP(r MatchupRaw, midPct float64) (float64, bool) {
	switch r.Source {
	case SrcStratzVS:
		if r.Synergy.Valid {
			return r.Synergy.Float64, true
		}
	case SrcOpenDotaEx:
		if r.WinCount.Valid && r.Matches > 0 {
			wr := float64(r.WinCount.Int64) / float64(r.Matches)
			return (wr - midPct) * 100, true
		}
	}
	return 0, false
}

// obs aggregates the raw observations of one directed pair. wins and nWin
// track the matches a win count was served for, the input the significance
// screen tests on.
type obs struct {
	sumN float64
	n    int64
	wins int64
	nWin int64
	rank int
	srcs map[string]bool
}

func (o *obs) add(d float64, n int64, wins sql.NullInt64, source, confidence string) {
	o.sumN += float64(n) * d
	o.n += n
	if wins.Valid {
		o.wins += wins.Int64
		o.nWin += n
	}
	if r := confRank(confidence); r > o.rank {
		o.rank = r
	}
	o.srcs[canonSource(source)] = true
}

// winCover reports whether every match of the cell also served a win count,
// the precondition for a testable screen table.
func (o *obs) winCover() bool {
	return o.nWin == o.n
}

func (o *obs) sources() []string {
	out := make([]string, 0, len(o.srcs))
	for s := range o.srcs {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// obsDelta is the match-weighted mean observation.
func (o *obs) delta() float64 {
	if o.n == 0 {
		return 0
	}
	return o.sumN / float64(o.n)
}

// PairDelta is one pooled raw pair observation before shrinkage: the
// match-weighted mean canonical delta in pp and the pooled match count. The
// empirical-Bayes alpha fit consumes these so it sees the same pooling the
// shrink sees.
type PairDelta struct {
	Hero, Other string
	Delta       float64
	N           int64
}

// sourceTier ranks a raw source for strict priority pooling: STRATZ rows are
// the product's exclusive source, every other label sits below. The readme
// source policy fixes the order; backups never mix into a pair the tier
// above already serves.
func sourceTier(src string) int {
	switch src {
	case SrcStratzVS, SrcStratz:
		return 0
	}
	return 2
}

// poolMatchups aggregates raw matchup rows per directed (pool hero, enemy)
// pair, dropping the rows MatchupCells would drop. Within one pair only the
// best available source tier pools. Keys come back ascending.
func poolMatchups(rows []MatchupRaw, cfg *config.Config) (keys []string, byKey map[string]*obs) {
	usable := map[string][]MatchupRaw{}
	for _, r := range rows {
		if r.Matches < int64(cfg.Thresholds.MatchupMinMatches) {
			continue
		}
		if _, ok := canonicalPP(r, cfg.Score.MidPct); !ok {
			continue
		}
		key := r.PoolSlug + "\x00" + r.EnemySlug
		usable[key] = append(usable[key], r)
	}
	byKey = map[string]*obs{}
	for key, rs := range usable {
		best := sourceTier(rs[0].Source)
		for _, r := range rs[1:] {
			if t := sourceTier(r.Source); t < best {
				best = t
			}
		}
		o := &obs{srcs: map[string]bool{}}
		for _, r := range rs {
			if sourceTier(r.Source) != best {
				continue
			}
			d, _ := canonicalPP(r, cfg.Score.MidPct)
			o.add(d, r.Matches, r.WinCount, r.Source, r.Confidence)
		}
		byKey[key] = o
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, byKey
}

// poolSynergies aggregates raw synergy rows per crawled direction (hero_a,
// hero_b) under the same strict source priority. Keys come back ascending.
func poolSynergies(rows []SynergyRaw) (keys []string, dirs map[string]*obs) {
	usable := map[string][]SynergyRaw{}
	for _, r := range rows {
		key := r.HeroA + "\x00" + r.HeroB
		usable[key] = append(usable[key], r)
	}
	dirs = map[string]*obs{}
	for key, rs := range usable {
		best := sourceTier(rs[0].Source)
		for _, r := range rs[1:] {
			if t := sourceTier(r.Source); t < best {
				best = t
			}
		}
		o := &obs{srcs: map[string]bool{}}
		for _, r := range rs {
			if sourceTier(r.Source) != best {
				continue
			}
			// synergy_raw carries no confidence column; medium is the neutral read.
			o.add(r.Synergy, r.MatchCount, r.WinCount, r.Source, ConfMed)
		}
		dirs[key] = o
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, dirs
}

func pairDeltas(keys []string, byKey map[string]*obs) []PairDelta {
	out := make([]PairDelta, 0, len(keys))
	for _, key := range keys {
		hero, other := cutKey(key)
		o := byKey[key]
		out = append(out, PairDelta{Hero: hero, Other: other, Delta: o.delta(), N: o.n})
	}
	return out
}

// MatchupPairDeltas pools the raw matchup rows exactly as MatchupCells does,
// stopping before the shrink.
func MatchupPairDeltas(rows []MatchupRaw, cfg *config.Config) []PairDelta {
	keys, byKey := poolMatchups(rows, cfg)
	return pairDeltas(keys, byKey)
}

// SynergyPairDeltas pools each crawled direction of the raw synergy rows as
// one observation, mirroring the per-direction pooling SynergyCells shrinks.
func SynergyPairDeltas(rows []SynergyRaw) []PairDelta {
	keys, dirs := poolSynergies(rows)
	return pairDeltas(keys, dirs)
}

// MatchupCells shrinks every pooled matchup observation into per (pool hero,
// enemy) cells. Rows under thresholds.matchupMinMatches and rows whose source
// serves no usable value are dropped. The whole enemy family runs through
// EffectiveAlphas, so with eval.screen enabled cells without significant
// evidence shrink under a raised alpha.
func MatchupCells(rows []MatchupRaw, cfg *config.Config) []Cell {
	keys, byKey := poolMatchups(rows, cfg)
	scr := make([]ScreenCell, len(keys))
	for i, key := range keys {
		hero, _ := cutKey(key)
		o := byKey[key]
		scr[i] = ScreenCell{Hero: hero, Wins: o.wins, N: o.n, WinCover: o.winCover()}
	}
	alphas := EffectiveAlphas(scr, cfg.Shrink.MatchupAlpha, cfg)
	cells := make([]Cell, 0, len(keys))
	for i, key := range keys {
		hero, other := cutKey(key)
		o := byKey[key]
		cells = append(cells, Cell{
			Hero: hero, Other: other,
			Value:      analytics.ShrinkDelta(o.delta(), float64(o.n), alphas[i]),
			N:          o.n,
			PostVar:    PPVar * float64(o.n) / square(float64(o.n)+alphas[i]),
			Sources:    o.sources(),
			Confidence: confLabel(o.rank),
		})
	}
	return cells
}

// SynergyCells shrinks every pooled synergy observation into symmetric per
// (pool hero, ally) cells. Both directions are shrunk separately and then
// averaged when the pair was crawled both ways; the divergence is reported
// through warn and never fails the run. Each surviving pair is stored under
// both orderings so the ally matrix stays symmetric for every pool hero,
// though with eval.screen enabled each ordering shrinks under its own hero's
// screened alpha.
func SynergyCells(rows []SynergyRaw, cfg *config.Config, warn func(subject, detail string)) []Cell {
	if warn == nil {
		warn = func(string, string) {}
	}
	keys, dirs := poolSynergies(rows)

	type pair struct {
		a, b     string
		fwd, rev *obs
	}
	seen := map[string]bool{}
	var pairs []pair
	for _, key := range keys {
		a, b := cutKey(key)
		if seen[unorderedKey(a, b)] {
			continue
		}
		seen[unorderedKey(a, b)] = true
		pairs = append(pairs, pair{a: a, b: b, fwd: dirs[key], rev: dirs[b+"\x00"+a]})
	}

	scr := make([]ScreenCell, 0, 2*len(pairs))
	for _, p := range pairs {
		w, n, cov := p.fwd.wins, p.fwd.n, p.fwd.winCover()
		if p.rev != nil {
			w += p.rev.wins
			n += p.rev.n
			cov = cov && p.rev.winCover()
		}
		for _, hero := range [2]string{p.a, p.b} {
			scr = append(scr, ScreenCell{Hero: hero, Wins: w, N: n, WinCover: cov})
		}
	}
	alphas := EffectiveAlphas(scr, cfg.Shrink.SynergyAlpha, cfg)

	var cells []Cell
	for i, p := range pairs {
		fwd, rev := p.fwd, p.rev
		plain := analytics.ShrinkDelta(fwd.delta(), float64(fwd.n), cfg.Shrink.SynergyAlpha)
		n := fwd.n
		srcs := fwd.sources()
		rank := fwd.rank
		if rev != nil {
			back := analytics.ShrinkDelta(rev.delta(), float64(rev.n), cfg.Shrink.SynergyAlpha)
			if back != plain {
				warn("synergy "+p.a+"/"+p.b, fmt.Sprintf("direction divergence %.4g pp vs %.4g pp", plain, back))
			}
			n += rev.n
			srcs = mergeSources(srcs, rev.sources())
			if rev.rank > rank {
				rank = rev.rank
			}
		}
		// each direction is screened against its own hero's remaining pool,
		// so a pair can carry different alphas in its two orderings
		for di, hero := range [2]string{p.a, p.b} {
			other := p.b
			if di == 1 {
				other = p.a
			}
			alphaEff := alphas[2*i+di]
			value := analytics.ShrinkDelta(fwd.delta(), float64(fwd.n), alphaEff)
			if rev != nil {
				value = (value + analytics.ShrinkDelta(rev.delta(), float64(rev.n), alphaEff)) / 2
			}
			cells = append(cells, Cell{
				Hero: hero, Other: other,
				Value:      value,
				N:          n,
				PostVar:    PPVar * float64(n) / square(float64(n)+alphaEff),
				Sources:    append([]string(nil), srcs...),
				Confidence: confLabel(rank),
			})
		}
	}
	return cells
}

func mergeSources(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func cutKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func unorderedKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

func square(x float64) float64 {
	return x * x
}
