package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
	"poolguide/internal/emit"
	"poolguide/internal/gates"
)

// fitCoeffNames is the coefficient vector of the linear score recombination
// in the fixed order the ascent sweeps: the five fitted weights, then the two
// gate deltas. Names parallel the config keys. The syn phase is NOT in the
// vector: its weight is the authored weights.synByRole map keyed on the role
// being picked, applied to every row through the cached role.
var fitCoeffNames = [7]string{"knownMu", "prior", "genericFit", "exposure", "flexibility", "bonus", "penalty"}

// fitRow freezes one candidate of one pick: the role being picked, the Term
// breakdown analytics.Score computes (weight-independent fields only), plus
// the fired bonus and penalty rule counts, so any coefficient vector can
// recombine the score.
type fitRow struct {
	idx                               int
	role                              string
	knownMu, knownSyn, prior          float64
	genericFit, exposure, flexibility float64
	bonus, penalty                    int
	picked                            bool
}

// fitPick is one pick's full candidate field plus the pick-time slot counts.
// ranked mirrors PickEval.Ranked: the picked hero competed and the field held
// more than one candidate.
type fitPick struct {
	matchID int64
	k, a    int
	ranked  bool
	rows    []fitRow
}

// scoreOf recombines one cached row under a coefficient vector and the
// authored per-role syn weights. The expression order mirrors analytics.Score
// term for term, so the live config vector reproduces the replay's scores;
// only a candidate with three or more fired gate rules of mixed action can
// differ in the last ulp, where gates sums in rule order and this sums the
// same multiset grouped by action.
func scoreOf(p *fitPick, r *fitRow, c *[7]float64, synByRole map[string]float64, sc config.ScoreConsts) float64 {
	kf, af := float64(p.k), float64(p.a)
	s := 0.0
	s += c[0] * (kf / sc.EnemySlots) * r.knownMu
	s += synByRole[r.role] * (af / sc.EnemySlots) * r.knownSyn
	s += c[1] * r.prior
	s += c[2] * ((sc.AllySlots - af) / sc.AllySlots) * r.genericFit
	s -= c[3] * ((sc.EnemySlots - kf) / sc.EnemySlots) * r.exposure
	s += c[4] * r.flexibility
	s += c[5]*float64(r.bonus) + c[6]*float64(r.penalty)
	return s
}

// percentileOf ranks the picked row under a coefficient vector with the Rank
// tie rule, score descending and ties by ascending roster index.
func percentileOf(p *fitPick, c *[7]float64, synByRole map[string]float64, sc config.ScoreConsts) (float64, bool) {
	if !p.ranked {
		return 0, false
	}
	var picked *fitRow
	for i := range p.rows {
		if p.rows[i].picked {
			picked = &p.rows[i]
			break
		}
	}
	if picked == nil {
		return 0, false
	}
	sp := scoreOf(p, picked, c, synByRole, sc)
	rank := 1
	for i := range p.rows {
		r := &p.rows[i]
		if r.picked {
			continue
		}
		if sr := scoreOf(p, r, c, synByRole, sc); sr > sp || (sr == sp && r.idx < picked.idx) {
			rank++
		}
	}
	n := len(p.rows)
	return (float64(n) - float64(rank)) / float64(n-1), true
}

func meanFitPercentile(picks []fitPick, c *[7]float64, synByRole map[string]float64, sc config.ScoreConsts) float64 {
	sum, n := 0.0, 0
	for i := range picks {
		if pct, ok := percentileOf(&picks[i], c, synByRole, sc); ok {
			sum += pct
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func coeffsOf(cfg *config.Config) [7]float64 {
	w, g := cfg.Weights, cfg.GateDeltas
	return [7]float64{w.KnownMu, w.Prior, w.GenericFit, w.Exposure, w.Flexibility, g["bonus"], g["penalty"]}
}

func configOfCoeffs(c [7]float64, synByRole map[string]float64) (config.Weights, map[string]float64) {
	return config.Weights{
			KnownMu: c[0], SynByRole: synByRole, Prior: c[1], GenericFit: c[2], Exposure: c[3], Flexibility: c[4]},
		map[string]float64{"bonus": c[5], "penalty": c[6]}
}

// buildPickCache walks the drafts in pick order exactly as SequentialReplay
// does and freezes every pick's candidate field. Field membership and gate
// eligibility depend only on the hard gates, so the cache stays valid for
// every weight vector and gate delta pair.
func buildPickCache(cfg *config.Config, m *Model, matches []Match) []fitPick {
	rules := gateRules(m.Gates)
	action := make(map[string]string, len(rules))
	for _, r := range rules {
		action[r.Id] = r.Action
	}
	var out []fitPick
	for _, mt := range matches {
		for i, ev := range mt.Events {
			idx, ok := m.IdxOf[ev.Slug]
			if !ok || !ev.IsPick {
				continue
			}
			if _, pool := m.PoolPos[idx]; !pool {
				continue
			}
			out = append(out, cachePick(cfg, m, rules, action, stateBefore(m, mt, i), idx, mt.ID))
		}
	}
	return out
}

// cachePick mirrors scorePick's candidate construction but freezes Terms and
// gate rule counts instead of ranking under the live weights.
func cachePick(cfg *config.Config, m *Model, rules []gates.Rule, action map[string]string, s draftState, picked int, matchID int64) fitPick {
	taken := map[int]bool{}
	for _, idx := range s.taken {
		taken[idx] = true
	}
	gatedMemo := map[string]int{}
	ruleCounts := func(role string, idx int) (int, int) {
		gc, ok := gatedMemo[role]
		if !ok {
			gc = gatedCount(cfg, m, rules, s, role)
			gatedMemo[role] = gc
		}
		res := gates.Evaluate(rules, m.Slugs[idx], gateState(s, role, gc), cfg)
		bonus, penalty := 0, 0
		for _, id := range res.FiredRuleIds {
			switch action[id] {
			case gates.ActionBonus:
				bonus++
			case gates.ActionPenalty:
				penalty++
			}
		}
		return bonus, penalty
	}
	draft := analytics.Draft{
		RosterSize: m.RosterSize, Pop: m.Pop,
		VisibleEnemies: s.enems, Allies: s.allies, Taken: s.taken,
	}
	pick := fitPick{matchID: matchID, k: len(s.enems), a: len(s.allies)}
	row := func(idx, pi int, role string, bonus, penalty int) fitRow {
		term := analytics.Score(analytics.Candidate{
			Idx: idx, Role: role, Prior: m.Prior[pi], Mu: m.Mu[pi], Syn: m.Syn[pi],
		}, draft, cfg.Weights, cfg.Score)
		return fitRow{
			idx: idx, role: role, knownMu: term.KnownMu, knownSyn: term.KnownSyn, prior: term.Prior,
			genericFit: term.GenericFit, exposure: term.Exposure, flexibility: term.Flexibility,
			bonus: bonus, penalty: penalty,
		}
	}
	pickedRole, eligible := evalRole(cfg, m, rules, s, picked)
	if pickedRole == "" {
		pickedRole = m.Roles[picked][0]
	}
	for pi, idx := range m.PoolIdx {
		if taken[idx] || idx == picked {
			continue
		}
		role, ok := evalRole(cfg, m, rules, s, idx)
		if !ok {
			continue
		}
		bonus, penalty := ruleCounts(role, idx)
		pick.rows = append(pick.rows, row(idx, pi, role, bonus, penalty))
	}
	if eligible {
		bonus, penalty := ruleCounts(pickedRole, picked)
		pick.rows = append(pick.rows, withPicked(row(picked, m.PoolPos[picked], pickedRole, bonus, penalty)))
	}
	pick.ranked = eligible && len(pick.rows) > 1
	return pick
}

func withPicked(r fitRow) fitRow {
	r.picked = true
	return r
}

// gridValues is the sweep grid: -maxWeight..+maxWeight in whole steps of
// gridStep with 0 on it. The step count floors, so a non-integral
// maxWeight/gridStep ratio keeps the grid inside the bound.
func gridValues(fc config.EvalFitCfg) []float64 {
	n := int(math.Floor(fc.MaxWeight/fc.GridStep + 1e-9))
	if n < 1 {
		n = 1
	}
	out := make([]float64, 0, 2*n+1)
	for i := -n; i <= n; i++ {
		out = append(out, float64(i)*fc.GridStep)
	}
	return out
}

// ascend runs coordinate ascent over the fixed coefficient order. Each sweep
// tries every grid value against the incumbent and keeps a move only on
// strict improvement, so the earliest grid value wins ties and an off-grid
// incumbent survives until something beats it. A pass with no strict
// improvement has converged and stops the ascent; the pass cap bounds the
// refinement sweeps that revisit coefficient interactions.
func ascend(picks []fitPick, start [7]float64, synByRole map[string]float64, fc config.EvalFitCfg, sc config.ScoreConsts) ([7]float64, float64, int) {
	cur := start
	curObj := meanFitPercentile(picks, &cur, synByRole, sc)
	grid := gridValues(fc)
	passes := 0
	for p := 0; p < fc.MaxPasses; p++ {
		passStart := curObj
		for j := range cur {
			incumbent, bestVal, bestObj := cur[j], cur[j], curObj
			for _, v := range grid {
				if v == incumbent {
					continue
				}
				cur[j] = v
				if obj := meanFitPercentile(picks, &cur, synByRole, sc); obj > bestObj {
					bestObj, bestVal = obj, v
				}
			}
			cur[j] = bestVal
			curObj = bestObj
		}
		passes = p + 1
		if curObj == passStart {
			break
		}
	}
	return cur, curObj, passes
}

// AblationRow is one coefficient zeroed on the holdout against the fitted
// vector: the zeroed holdout mean percentile and its delta.
type AblationRow struct {
	Coefficient string  `json:"coefficient"`
	HoldoutPct  float64 `json:"holdoutPct"`
	Delta       float64 `json:"delta"`
}

func ablationTable(holdout []fitPick, fitted [7]float64, synByRole map[string]float64, sc config.ScoreConsts) []AblationRow {
	base := meanFitPercentile(holdout, &fitted, synByRole, sc)
	out := make([]AblationRow, 0, len(fitCoeffNames))
	for j, name := range fitCoeffNames {
		zeroed := fitted
		zeroed[j] = 0
		pct := meanFitPercentile(holdout, &zeroed, synByRole, sc)
		out = append(out, AblationRow{Coefficient: name, HoldoutPct: pct, Delta: pct - base})
	}
	return out
}

// WeightProposal is the fit artifact written to paths.fitOut (var/ scratch).
// Promote re-reads and re-checks it before any config rewrite.
type WeightProposal struct {
	Weights           map[string]float64 `json:"weights"`
	GateDeltas        map[string]float64 `json:"gateDeltas"`
	TrainPct          float64            `json:"trainPct"`
	CurrentTrainPct   float64            `json:"currentTrainPct"`
	HoldoutPct        float64            `json:"holdoutPct"`
	CurrentHoldoutPct float64            `json:"currentHoldoutPct"`
	Gain              float64            `json:"gain"`
	GainCiLow         float64            `json:"gainCiLow"`
	GainCiHi          float64            `json:"gainCiHi"`
	AUC               float64            `json:"auc"`
	Top1              float64            `json:"top1"`
	Top3              float64            `json:"top3"`
	Passes            int                `json:"passes"`
	Ablation          []AblationRow      `json:"ablation"`
	MeasuredAt        string             `json:"measuredAt"`
	// Baseline records the config vector the gain was measured against, so
	// promote can refuse a proposal whose measuring config is no longer live.
	Baseline *WeightBaseline `json:"baseline,omitempty"`
}

// WeightBaseline is the live coefficient vector at measurement time, plus the
// authored synByRole map the gain was measured under.
type WeightBaseline struct {
	Weights    map[string]float64 `json:"weights"`
	GateDeltas map[string]float64 `json:"gateDeltas"`
	SynByRole  map[string]float64 `json:"synByRole,omitempty"`
}

// FitWeights runs the phase 4 weight fit: cache the per-pick Terms once,
// maximize TRAIN mean pick percentile by coordinate ascent, and evaluate the
// fitted vector on the holdout with a paired bootstrap gain interval. The
// objective is the pick percentile because the draft-level AUC measured on
// this population sits at chance (see ref/dota2/eval/README.md); AUC and
// top-k ride along in the proposal as report-only context.
func FitWeights(d Deps) (string, error) {
	matches, _, err := LoadMatches(d.DB)
	if err != nil {
		return "", err
	}
	model, err := BuildModel(d.DB, d.Cfg)
	if err != nil {
		return "", err
	}
	train, holdout := SplitMatches(matches, d.Cfg.Eval.Split.TrainFrac)
	trainIDs, holdIDs := idSet(train), idSet(holdout)

	cache := buildPickCache(d.Cfg, model, matches)
	var trainCache, holdCache []fitPick
	for i := range cache {
		if holdIDs[cache[i].matchID] {
			holdCache = append(holdCache, cache[i])
		} else if trainIDs[cache[i].matchID] {
			trainCache = append(trainCache, cache[i])
		}
	}
	sc := d.Cfg.Score
	syn := synByRoleCopy(d.Cfg.Weights.SynByRole)
	start := coeffsOf(d.Cfg)
	fitted, trainPct, passes := ascend(trainCache, start, syn, d.Cfg.Eval.Fit, sc)
	currentTrain := meanFitPercentile(trainCache, &start, syn, sc)

	seqCur := SequentialReplay(d.Cfg, model, matches)
	cur := replayMetrics("current", d.Cfg, model, seqCur, holdIDs, matches)
	fitCfg := *d.Cfg
	fitCfg.Weights, fitCfg.GateDeltas = configOfCoeffs(fitted, syn)
	seqFit := SequentialReplay(&fitCfg, model, matches)
	fit := replayMetrics("fitted", &fitCfg, model, seqFit, holdIDs, matches)

	var holdPicksFit, holdPicksCur [][]PickEval
	for i := range seqFit {
		if holdIDs[seqFit[i].MatchID] {
			holdPicksFit = append(holdPicksFit, seqFit[i].Picks)
			holdPicksCur = append(holdPicksCur, seqCur[i].Picks)
		}
	}
	b := d.Cfg.Eval.Bootstrap
	gainCI := MatchesCI(len(holdPicksFit), b.Resamples, uint64(b.Seed), func(idx []int) float64 {
		var f, c []PickEval
		for _, i := range idx {
			f = append(f, holdPicksFit[i]...)
			c = append(c, holdPicksCur[i]...)
		}
		return MeanPercentile(f) - MeanPercentile(c)
	})

	prop := WeightProposal{
		Weights:           weightsMap(fitted),
		GateDeltas:        gateDeltasMap(fitted),
		TrainPct:          trainPct,
		CurrentTrainPct:   currentTrain,
		HoldoutPct:        fit.MeanPercentile,
		CurrentHoldoutPct: cur.MeanPercentile,
		Gain:              fit.MeanPercentile - cur.MeanPercentile,
		GainCiLow:         gainCI.Lo,
		GainCiHi:          gainCI.Hi,
		AUC:               fit.AUC,
		Top1:              fit.TopK["top1"],
		Top3:              fit.TopK["top3"],
		Passes:            passes,
		Ablation:          ablationTable(holdCache, fitted, syn, sc),
		MeasuredAt:        time.Now().UTC().Format(time.RFC3339),
		Baseline: &WeightBaseline{
			Weights:    weightsMap(start),
			GateDeltas: gateDeltasMap(start),
			SynByRole:  syn,
		},
	}
	out := filepath.Join(d.RepoRoot, d.Cfg.Paths.FitOut)
	jb, err := json.MarshalIndent(prop, "", "  ")
	if err != nil {
		return "", err
	}
	if err := emit.WriteFile(out, append(jb, '\n')); err != nil {
		return "", fmt.Errorf("fit weights: write proposal: %w", err)
	}
	return weightSummary(d.Cfg, prop), nil
}

func weightsMap(c [7]float64) map[string]float64 {
	return map[string]float64{
		"knownMu": c[0], "prior": c[1],
		"genericFit": c[2], "exposure": c[3], "flexibility": c[4],
	}
}

func gateDeltasMap(c [7]float64) map[string]float64 {
	return map[string]float64{"bonus": c[5], "penalty": c[6]}
}

// synByRoleCopy snapshots the authored map so the proposal's baseline records
// the exact per-role weights the gain was measured under.
func synByRoleCopy(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func weightSummary(cfg *config.Config, p WeightProposal) string {
	guard := cfg.Eval.Fit.MinHoldoutGain
	verdict := "guard FAILS (keep current values)"
	if p.Gain >= guard && p.GainCiLow > 0 {
		verdict = "guard passes (eval promote weights may apply)"
	}
	s := fmt.Sprintf("weight fit: %d passes, train pct %.4f (was %.4f)\n", p.Passes, p.TrainPct, p.CurrentTrainPct)
	s += fmt.Sprintf("holdout pct %.4f (was %.4f), gain %+.4f [%.4f, %.4f]\n", p.HoldoutPct, p.CurrentHoldoutPct, p.Gain, p.GainCiLow, p.GainCiHi)
	s += fmt.Sprintf("fitted auc %.4f, top1 %.4f, top3 %.4f (report-only)\n", p.AUC, p.Top1, p.Top3)
	for _, a := range p.Ablation {
		s += fmt.Sprintf("ablation %-12s zeroed holdout %.4f, delta %+.4f\n", a.Coefficient, a.HoldoutPct, a.Delta)
	}
	return s + "proposal written to " + cfg.Paths.FitOut + ": " + verdict + "\n"
}
