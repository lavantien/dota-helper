package eval

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
	"poolguide/internal/emit"
	"poolguide/internal/mine"
)

// minCVPairs is the qualifying-pair count the holdout deviance cross-check
// needs before it may outvote the moment estimate; below it the league
// holdout is too thin a window on the crawl-wide aggregates.
const minCVPairs = 30

// MomentFit is the empirical-Bayes moment match for one family.
type MomentFit struct {
	Alpha       float64 `json:"alpha"`
	Tau2        float64 `json:"tau2"`
	CI          CI      `json:"ci"`
	KeptCurrent bool    `json:"keptCurrent"`
}

// CVFit is the holdout deviance cross-check over the config grid.
type CVFit struct {
	Pairs           int     `json:"pairs"`
	Alpha           float64 `json:"alpha"`
	MeanDeviance    float64 `json:"meanDeviance"`
	CurrentDeviance float64 `json:"currentDeviance"`
	Adequate        bool    `json:"adequate"`
}

// AlphaFamilyResult is one shrinkage family's derivation.
type AlphaFamilyResult struct {
	Family  string    `json:"family"`
	Pairs   int       `json:"pairs"`
	Current float64   `json:"current"`
	Moment  MomentFit `json:"moment"`
	CV      CVFit     `json:"cv"`
	Chosen  float64   `json:"chosen"`
	Source  string    `json:"source"`
	Reason  string    `json:"reason"`
}

// AlphaProposal is the alpha fit artifact written next to paths.fitOut.
type AlphaProposal struct {
	Families   []AlphaFamilyResult `json:"families"`
	MeasuredAt string              `json:"measuredAt"`
}

// fitProposalPath derives the artifact path of one fit section from
// paths.fitOut: var/eval-fit.json -> var/eval-fit-alphas.json.
func fitProposalPath(fitOut, section string) string {
	return strings.TrimSuffix(fitOut, ".json") + "-" + section + ".json"
}

// momentAlpha matches moments over one family of pair deltas d_i with counts
// n_i: the population variance of the deltas decomposes into true
// between-pair variance tau2 plus mean sampling variance sigma2/n, and
// alpha = sigma2/tau2 is the pseudo-count whose ShrinkDelta reproduces that
// split. tau2 pinned at 0 means the spread is fully explained by sampling
// noise, alpha would be infinite, and keptCurrent records the fallback.
func momentAlpha(ds, ns []float64, sigma2 float64) (alpha, tau2 float64, kept bool) {
	m := float64(len(ds))
	if m < 2 {
		return 0, 0, true
	}
	mean := 0.0
	for _, d := range ds {
		mean += d
	}
	mean /= m
	spread, sampling := 0.0, 0.0
	for i, d := range ds {
		spread += (d - mean) * (d - mean)
		sampling += sigma2 / ns[i]
	}
	tau2 = spread/m - sampling/m
	if tau2 <= 0 {
		return 0, 0, true
	}
	return sigma2 / tau2, tau2, false
}

// momentAlphaCI bootstraps the moment alpha over pair resamples. A resample
// whose tau2 hits the floor contributes the point estimate: alpha is
// unsigned infinity there and the interval must stay finite, a convention
// that mildly narrows the interval and is recorded in the proposal reason.
func momentAlphaCI(ds, ns []float64, sigma2 float64, b config.EvalBootstrapCfg, point float64) CI {
	return MatchesCI(len(ds), b.Resamples, uint64(b.Seed), func(idx []int) float64 {
		rd, rn := make([]float64, len(idx)), make([]float64, len(idx))
		for i, j := range idx {
			rd[i], rn[i] = ds[j], ns[j]
		}
		a, _, kept := momentAlpha(rd, rn, sigma2)
		if kept {
			return point
		}
		return a
	})
}

// binomialDeviance is twice the log-likelihood gap between the observed wins
// and their prediction: 0 for a perfect prediction, positive otherwise.
func binomialDeviance(w, n int64, p float64) float64 {
	if n <= 0 || w < 0 || w > n {
		return 0
	}
	p = math.Min(math.Max(p, 1e-12), 1-1e-12)
	dev := 0.0
	if w > 0 {
		dev += float64(w) * math.Log(float64(w)/(float64(n)*p))
	}
	if n-w > 0 {
		dev += float64(n-w) * math.Log(float64(n-w)/(float64(n)*(1-p)))
	}
	return 2 * dev
}

// cvObs is one pair with a crawl-aggregate delta plus its league-holdout win
// record, the join the deviance cross-check scores.
type cvObs struct {
	d, nAgg  float64
	w, nHold int64
}

// cvAlpha sweeps the config grid by mean binomial deviance of the shrunk
// aggregate prediction against the holdout pair win rates. Pairs need holdout
// n >= minHoldoutN; fewer than minCVPairs qualifying pairs marks the check
// inadequate and leaves the estimates untouched. Ties keep the earliest
// (smallest) grid alpha.
func cvAlpha(grid []float64, minHoldoutN int64, obs []cvObs, current, midPct float64) CVFit {
	var fit CVFit
	for _, o := range obs {
		if o.nHold >= minHoldoutN {
			fit.Pairs++
		}
	}
	if fit.Pairs < minCVPairs {
		return fit
	}
	deviance := func(alpha float64) float64 {
		sum := 0.0
		n := 0
		for _, o := range obs {
			if o.nHold < minHoldoutN {
				continue
			}
			// d is pp; ShrinkDelta over the rate-unit delta already lands in
			// rate units, so the prediction is the midpoint plus that offset
			p := midPct + analytics.ShrinkDelta(o.d/100, o.nAgg, alpha)
			sum += binomialDeviance(o.w, o.nHold, p)
			n++
		}
		return sum / float64(n)
	}
	fit.Adequate = true
	fit.Alpha, fit.MeanDeviance = grid[0], deviance(grid[0])
	for _, a := range grid[1:] {
		if d := deviance(a); d < fit.MeanDeviance {
			fit.Alpha, fit.MeanDeviance = a, d
		}
	}
	fit.CurrentDeviance = deviance(current)
	return fit
}

// loadAlphaInputs reads the raw aggregate tables filtered to pool heroes:
// the crawl-wide counts, not the thin league corpus.
func loadAlphaInputs(db *sql.DB, cfg *config.Config) (matchups []mine.MatchupRaw, synergies []mine.SynergyRaw, err error) {
	pool := map[string]bool{}
	for _, s := range cfg.PoolSlugs() {
		pool[s] = true
	}
	rows, err := db.Query(`SELECT pool_slug, enemy_slug, dis, win_count, synergy, matches, source, confidence
		FROM matchup_raw`)
	if err != nil {
		return nil, nil, fmt.Errorf("matchup_raw: %w", err)
	}
	for rows.Next() {
		var r mine.MatchupRaw
		if err := rows.Scan(&r.PoolSlug, &r.EnemySlug, &r.Dis, &r.WinCount, &r.Synergy,
			&r.Matches, &r.Source, &r.Confidence); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if pool[r.PoolSlug] {
			matchups = append(matchups, r)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT hero_a, hero_b, match_count, synergy, source FROM synergy_raw`)
	if err != nil {
		return nil, nil, fmt.Errorf("synergy_raw: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r mine.SynergyRaw
		if err := rows.Scan(&r.HeroA, &r.HeroB, &r.MatchCount, &r.Synergy, &r.Source); err != nil {
			return nil, nil, err
		}
		if pool[r.HeroA] {
			synergies = append(synergies, r)
		}
	}
	return matchups, synergies, rows.Err()
}

// overallDeltas derives each pool hero's crawl-wide overall win observation
// from the stratz_vs rows, pooling the same hero-perspective sums OverallWR
// pools. The perspective was adjudicated against the independent opendota
// snapshot: per-hero sum(win_count)/sum(matches) correlates +0.81 with the
// snapshot wr and sits 1.3pp from it, the complement 3.6pp, so the served
// win counts track the pool hero's own wins.
func overallDeltas(rows []mine.MatchupRaw, cfg *config.Config) []mine.PairDelta {
	slots := cfg.Score.EnemySlots
	wins, picks := map[string]float64{}, map[string]float64{}
	for _, r := range rows {
		if r.Source != mine.SrcStratzVS || !r.WinCount.Valid || r.Matches <= 0 {
			continue
		}
		wins[r.PoolSlug] += float64(r.WinCount.Int64) / slots
		picks[r.PoolSlug] += float64(r.Matches) / slots
	}
	var slugs []string
	for s, n := range picks {
		if n > 0 {
			slugs = append(slugs, s)
		}
	}
	sort.Strings(slugs)
	out := make([]mine.PairDelta, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, mine.PairDelta{
			Hero: s, Delta: (wins[s]/picks[s] - 0.5) * 100, N: int64(picks[s]),
		})
	}
	return out
}

// holdoutPairRecords tallies the league-holdout win record per key: matchup
// keys cross sides, synergy keys stay on one side, overall keys are the pool
// hero itself.
func holdoutPairRecords(matches []Match) (matchup, synergy, overall map[string][2]int64) {
	matchup, synergy, overall = map[string][2]int64{}, map[string][2]int64{}, map[string][2]int64{}
	add := func(m map[string][2]int64, key string, won bool) {
		rec := m[key]
		rec[0], rec[1] = rec[0]+b2i(won), rec[1]+1
		m[key] = rec
	}
	for _, mt := range matches {
		for _, p := range mt.Events {
			if !p.IsPick {
				continue
			}
			won := p.IsRadiant == mt.RadiantWin
			add(overall, p.Slug, won)
			for _, e := range mt.Events {
				if !e.IsPick || e.Seq == p.Seq {
					continue
				}
				if e.IsRadiant != p.IsRadiant {
					add(matchup, p.Slug+"\x00"+e.Slug, won)
				} else if e.Seq > p.Seq {
					add(synergy, p.Slug+"\x00"+e.Slug, won)
				}
			}
		}
	}
	return matchup, synergy, overall
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// FitAlphas derives the shrinkage alphas from the raw aggregate tables and
// writes the proposal beside paths.fitOut.
func FitAlphas(d Deps) (string, error) {
	matchups, synergies, err := loadAlphaInputs(d.DB, d.Cfg)
	if err != nil {
		return "", err
	}
	matches, _, err := LoadMatches(d.DB)
	if err != nil {
		return "", err
	}
	_, holdout := SplitMatches(matches, d.Cfg.Eval.Split.TrainFrac)
	hoMatchup, hoSynergy, hoOverall := holdoutPairRecords(holdout)

	families := []struct {
		name    string
		current float64
		deltas  []mine.PairDelta
		hold    map[string][2]int64
		join    func(mine.PairDelta) string
		source  string
	}{
		{"matchup", d.Cfg.Shrink.MatchupAlpha, mine.MatchupPairDeltas(matchups, d.Cfg), hoMatchup,
			func(pd mine.PairDelta) string { return pd.Hero + "\x00" + pd.Other },
			"matchup_raw pooled per (pool hero, enemy), crawl-wide counts"},
		// the crawl stores whichever direction it ran; a holdout pair joins
		// under the matching direction
		{"synergy", d.Cfg.Shrink.SynergyAlpha, mine.SynergyPairDeltas(synergies), hoSynergy,
			func(pd mine.PairDelta) string { return pd.Hero + "\x00" + pd.Other },
			"synergy_raw pooled per crawled direction, crawl-wide counts"},
		{"overall", d.Cfg.Shrink.OverallWrAlpha, overallDeltas(matchups, d.Cfg), hoOverall,
			func(pd mine.PairDelta) string { return pd.Hero },
			"stratz_vs hero-perspective overall sums over matchup_raw, crawl-wide counts"},
	}

	prop := AlphaProposal{MeasuredAt: time.Now().UTC().Format(time.RFC3339)}
	for _, f := range families {
		ds := make([]float64, len(f.deltas))
		ns := make([]float64, len(f.deltas))
		var obs []cvObs
		for i, pd := range f.deltas {
			ds[i], ns[i] = pd.Delta, float64(pd.N)
			key := f.join(pd)
			if _, ok := f.hold[key]; !ok && f.name == "synergy" {
				key = pd.Other + "\x00" + pd.Hero
			}
			if rec, ok := f.hold[key]; ok {
				obs = append(obs, cvObs{d: pd.Delta, nAgg: float64(pd.N), w: rec[0], nHold: rec[1]})
			}
		}
		res := AlphaFamilyResult{Family: f.name, Pairs: len(f.deltas), Current: f.current, Source: f.source}
		alpha, tau2, kept := momentAlpha(ds, ns, mine.PPVar)
		res.Moment = MomentFit{Alpha: alpha, Tau2: tau2, KeptCurrent: kept}
		chosen := alpha
		switch {
		case kept:
			chosen = f.current
			res.Reason = "moment tau2 floor: sampling variance alone explains the spread, keeping the current alpha"
		default:
			res.Moment.CI = momentAlphaCI(ds, ns, mine.PPVar, d.Cfg.Eval.Bootstrap, alpha)
			res.CV = cvAlpha(d.Cfg.Eval.AlphaFit.Grid, int64(d.Cfg.Eval.AlphaFit.MinHoldoutN), obs, f.current, d.Cfg.Score.MidPct)
			if res.CV.Adequate {
				chosen = res.CV.Alpha
				res.Reason = fmt.Sprintf("holdout deviance over %d pairs chose grid alpha %g (moment said %.1f)", res.CV.Pairs, res.CV.Alpha, alpha)
			} else {
				res.Reason = fmt.Sprintf("moment estimate stands: %d qualifying holdout pairs is under the %d-pair adequacy floor", res.CV.Pairs, minCVPairs)
			}
		}
		res.Chosen = chosen
		prop.Families = append(prop.Families, res)
	}

	out := filepath.Join(d.RepoRoot, fitProposalPath(d.Cfg.Paths.FitOut, "alphas"))
	jb, err := json.MarshalIndent(prop, "", "  ")
	if err != nil {
		return "", err
	}
	if err := emit.WriteFile(out, append(jb, '\n')); err != nil {
		return "", fmt.Errorf("fit alphas: write proposal: %w", err)
	}
	return alphaSummary(prop, out), nil
}

func alphaSummary(p AlphaProposal, path string) string {
	var b strings.Builder
	for _, f := range p.Families {
		fmt.Fprintf(&b, "alpha %s: %d pairs, current %g, moment %.1f tau2 %.1f", f.Family, f.Pairs, f.Current, f.Moment.Alpha, f.Moment.Tau2)
		if f.Moment.CI != (CI{}) {
			fmt.Fprintf(&b, " [%.1f, %.1f]", f.Moment.CI.Lo, f.Moment.CI.Hi)
		}
		if f.CV.Adequate {
			fmt.Fprintf(&b, ", cv %g over %d pairs (deviance %.3f vs current %.3f)", f.CV.Alpha, f.CV.Pairs, f.CV.MeanDeviance, f.CV.CurrentDeviance)
		} else {
			fmt.Fprintf(&b, ", cv inadequate (%d pairs)", f.CV.Pairs)
		}
		fmt.Fprintf(&b, ", chose %g\n", f.Chosen)
		fmt.Fprintf(&b, "  %s\n", f.Reason)
	}
	return b.String() + "proposal written to " + path + "\n"
}
