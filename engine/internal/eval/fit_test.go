package eval

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
	"poolguide/internal/gates"
)

func TestGateDeltaDecomposition(t *testing.T) {
	m, cfg := handModel(t,
		gates.Rule{Id: "t-h1-bon2", Target: "h1", When: []gates.Condition{
			{Kind: gates.KindEnemyVisibleAny, Heroes: []string{"h6"}},
		}, Action: gates.ActionBonus},
		gates.Rule{Id: "t-h1-pen2", Target: "h1", When: []gates.Condition{
			{Kind: gates.KindEnemyCountMin, Count: 1},
		}, Action: gates.ActionPenalty},
	)
	rules := gateRules(m.Gates)
	action := map[string]string{}
	for _, r := range rules {
		action[r.Id] = r.Action
	}
	mt := handDraft()
	fired := 0
	for _, i := range []int{2, 6, 7} {
		s := stateBefore(m, mt, i)
		for _, role := range []string{"1", "2"} {
			for _, idx := range m.PoolIdx {
				res := gates.Evaluate(rules, m.Slugs[idx], gateState(s, role, 0), cfg)
				bonus, penalty := 0, 0
				for _, id := range res.FiredRuleIds {
					switch action[id] {
					case gates.ActionBonus:
						bonus++
					case gates.ActionPenalty:
						penalty++
					}
				}
				want := float64(bonus)*cfg.GateDeltas["bonus"] + float64(penalty)*cfg.GateDeltas["penalty"]
				fired += bonus + penalty
				if math.Abs(res.Delta-want) > 1e-12 {
					t.Fatalf("%s/%s delta %v, want counts %d/%d recomposed to %v",
						m.Slugs[idx], role, res.Delta, bonus, penalty, want)
				}
			}
		}
	}
	if fired < 4 {
		t.Fatalf("only %d rules fired across the sampled states, the pin needs coverage", fired)
	}
}

func TestScoreRecombinationMatchesAnalytics(t *testing.T) {
	_, cfg := handModel(t)
	sc := cfg.Score
	rng := rand.New(rand.NewSource(7))
	randMap := func() map[int]float64 {
		m := map[int]float64{}
		for i := 0; i < 8; i++ {
			m[i] = rng.Float64()
		}
		return m
	}
	for trial := 0; trial < 100; trial++ {
		idxs := rng.Perm(8)
		taken := idxs[:rng.Intn(3)]
		visible := idxs[3 : 3+rng.Intn(3)]
		allies := idxs[6 : 6+rng.Intn(2)]
		pop := map[int]float64{}
		for i := 0; i < 8; i++ {
			pop[i] = rng.Float64()
		}
		d := analytics.Draft{
			RosterSize: 8, Pop: pop,
			VisibleEnemies: visible, Allies: allies, Taken: taken,
		}
		c := analytics.Candidate{
			Idx: idxs[2], Role: "1", Prior: rng.Float64(),
			Mu: randMap(), Syn: randMap(),
		}
		w := config.Weights{
			KnownMu: rng.NormFloat64(), SynByRole: map[string]float64{"1": rng.NormFloat64()}, Prior: rng.NormFloat64(),
			GenericFit: rng.NormFloat64(), Exposure: rng.NormFloat64(), Flexibility: rng.NormFloat64(),
		}
		bonus, penalty := rng.Intn(3), rng.Intn(3)
		c.GateDelta = float64(bonus)*cfg.GateDeltas["bonus"] + float64(penalty)*cfg.GateDeltas["penalty"]
		term := analytics.Score(c, d, w, sc)
		co := [7]float64{w.KnownMu, w.Prior, w.GenericFit, w.Exposure, w.Flexibility,
			cfg.GateDeltas["bonus"], cfg.GateDeltas["penalty"]}
		p := fitPick{k: len(visible), a: len(allies)}
		r := fitRow{
			idx: c.Idx, role: "1", knownMu: term.KnownMu, knownSyn: term.KnownSyn, prior: term.Prior,
			genericFit: term.GenericFit, exposure: term.Exposure, flexibility: term.Flexibility,
			bonus: bonus, penalty: penalty,
		}
		if got := scoreOf(&p, &r, &co, w.SynByRole, sc); math.Abs(got-term.Score) > 1e-12 {
			t.Fatalf("trial %d: recombined %v, Score gave %v", trial, got, term.Score)
		}
	}
}

func TestPickCacheMatchesSequentialReplay(t *testing.T) {
	m, cfg := handModel(t)
	matches := []Match{handDraft()}
	cache := buildPickCache(cfg, m, matches)

	check := func(tag string, c *config.Config) {
		t.Helper()
		out := SequentialReplay(c, m, matches)
		co := coeffsOf(c)
		if len(cache) != len(out[0].Picks) {
			t.Fatalf("%s: %d cached picks, replay saw %d", tag, len(cache), len(out[0].Picks))
		}
		for i := range cache {
			p, rp := &cache[i], out[0].Picks[i]
			var pr *fitRow
			for j := range p.rows {
				if p.rows[j].picked {
					pr = &p.rows[j]
				}
			}
			if pr == nil {
				if rp.Ranked() {
					t.Fatalf("%s: pick %d has no picked row but the replay ranked it", tag, i)
				}
				continue
			}
			if got := scoreOf(p, pr, &co, c.Weights.SynByRole, c.Score); math.Abs(got-rp.Score) > 1e-12 {
				t.Fatalf("%s: pick %d cached score %v, replay %v", tag, i, got, rp.Score)
			}
			pct, ok := percentileOf(p, &co, c.Weights.SynByRole, c.Score)
			if ok != rp.Ranked() || (ok && math.Abs(pct-rp.Percentile) > 1e-12) {
				t.Fatalf("%s: pick %d cached pct %v (%v), replay %v (%v)", tag, i, pct, ok, rp.Percentile, rp.Ranked())
			}
		}
	}
	check("live", cfg)
	mutated := *cfg
	mutated.Weights = config.Weights{
		KnownMu: -0.3, SynByRole: synByRoleCopy(cfg.Weights.SynByRole), Prior: 0, GenericFit: 1.2, Exposure: -0.7, Flexibility: 2,
	}
	mutated.GateDeltas = map[string]float64{"bonus": 0.9, "penalty": -1.1}
	check("mutated", &mutated)
}

func TestAscendFindsSyntheticOptimum(t *testing.T) {
	_, cfg := handModel(t)
	fc, sc := cfg.Eval.Fit, cfg.Score
	zeroes := func() []fitRow {
		return []fitRow{
			{idx: 1, role: "1", knownMu: 1, prior: 0},
			{idx: 2, role: "1", knownMu: 0, prior: 1},
			{idx: 3, role: "1", knownMu: 0.5, prior: 0.6, picked: true},
		}
	}
	train := []fitPick{{k: 1, a: 0, ranked: true, rows: zeroes()}}
	start := coeffsOf(cfg)
	fitted, obj, passes := ascend(train, start, cfg.Weights.SynByRole, fc, sc)
	if obj != 1.0 {
		t.Fatalf("objective %v, want the known optimum 1.0", obj)
	}
	if fitted[1] != 0.2 {
		t.Fatalf("prior coefficient %v, want the grid value 0.2 inside (1/6, 1/4)", fitted[1])
	}
	if fitted[0] != start[0] || fitted[3] != start[3] {
		t.Fatalf("coefficients without leverage moved: %+v", fitted)
	}
	if passes < 1 || passes > fc.MaxPasses {
		t.Fatalf("passes %d out of range", passes)
	}
}

func TestAblationTableZeroesCoefficients(t *testing.T) {
	_, cfg := handModel(t)
	sc := cfg.Score
	rows := []fitRow{
		{idx: 1, role: "1", knownMu: 1, prior: 0},
		{idx: 2, role: "1", knownMu: 0, prior: 1},
		{idx: 3, role: "1", knownMu: 0.5, prior: 0.6, picked: true},
	}
	hold := []fitPick{{k: 1, a: 0, ranked: true, rows: rows}}
	fitted := coeffsOf(cfg)
	fitted[1] = 0.2
	table := ablationTable(hold, fitted, cfg.Weights.SynByRole, sc)
	if len(table) != len(fitCoeffNames) {
		t.Fatalf("%d ablation rows, want one per coefficient", len(table))
	}
	byName := map[string]AblationRow{}
	for _, r := range table {
		byName[r.Coefficient] = r
	}
	if d := byName["prior"].Delta; math.Abs(d-(-0.5)) > 1e-12 {
		t.Errorf("zeroing prior delta %v, want -0.5 (rank drops to 2 of 3)", d)
	}
	if d := byName["knownMu"].Delta; math.Abs(d-(-0.5)) > 1e-12 {
		t.Errorf("zeroing knownMu delta %v, want -0.5", d)
	}
	if d := byName["exposure"].Delta; d != 0 {
		t.Errorf("zeroing the dead exposure term moved the metric: %v", d)
	}
}

func TestFitWeightsWritesProposal(t *testing.T) {
	t.Chdir(repoRoot)
	cfg := testConfig(t)
	cfg.Eval.Bootstrap.Resamples = 50
	db := seedModelDB(t, cfg)
	seedMatches(t, db, cfg)

	propPath := func(root string) string {
		return filepath.Join(root, cfg.Paths.FitOut)
	}
	run := func(root string) WeightProposal {
		t.Helper()
		if _, err := FitWeights(Deps{DB: db, Cfg: cfg, RepoRoot: root}); err != nil {
			t.Fatalf("fit weights: %v", err)
		}
		b, err := os.ReadFile(propPath(root))
		if err != nil {
			t.Fatalf("read proposal: %v", err)
		}
		var p WeightProposal
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatalf("parse proposal: %v", err)
		}
		return p
	}
	a := run(t.TempDir())
	if len(a.Weights) != 5 || len(a.GateDeltas) != 2 {
		t.Fatalf("proposal carries %d weights and %d gate deltas", len(a.Weights), len(a.GateDeltas))
	}
	if len(a.Baseline.SynByRole) != len(cfg.Weights.SynByRole) {
		t.Fatalf("baseline synByRole snapshot missing: %+v", a.Baseline.SynByRole)
	}
	if a.TrainPct < a.CurrentTrainPct-1e-12 {
		t.Fatalf("train pct %.4f fell below the start %.4f, ascent must be monotone", a.TrainPct, a.CurrentTrainPct)
	}
	if math.Abs(a.Gain-(a.HoldoutPct-a.CurrentHoldoutPct)) > 1e-12 {
		t.Fatalf("gain %.4f does not equal holdout %.4f minus current %.4f", a.Gain, a.HoldoutPct, a.CurrentHoldoutPct)
	}
	if len(a.Ablation) != len(fitCoeffNames) || a.MeasuredAt == "" {
		t.Fatalf("proposal missing ablation rows or a measurement stamp: %+v", a)
	}
	b := run(t.TempDir())
	for k, v := range a.Weights {
		if b.Weights[k] != v {
			t.Fatalf("weights drifted between runs: %s %v vs %v", k, v, b.Weights[k])
		}
	}
	for k, v := range a.GateDeltas {
		if b.GateDeltas[k] != v {
			t.Fatalf("gate deltas drifted between runs: %s %v vs %v", k, v, b.GateDeltas[k])
		}
	}
}
