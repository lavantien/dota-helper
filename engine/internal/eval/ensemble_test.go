package eval

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"poolguide/internal/stats"
)

func TestFitBlendWeightsKnownOptimum(t *testing.T) {
	c0 := []float64{2, -2, 1, -1}
	c1 := []float64{-1, 1, -2, 2}
	labels := []bool{true, false, false, true}
	step := 0.25
	w := FitBlendWeights([][]float64{c0, c1, make([]float64, 4)}, labels, step, 1.0, 3)
	if len(w) != 3 {
		t.Fatalf("weights = %v, want 3 components", w)
	}
	if w[0] != step || w[1] != step || w[2] != 0 {
		t.Fatalf("weights = %v, want [%v %v 0]", w, step, step)
	}
	blend := make([]float64, 4)
	for i := range labels {
		blend[i] = w[0]*c0[i] + w[1]*c1[i]
	}
	if got := stats.AUC(blend, labels); got != 1 {
		t.Fatalf("blend AUC = %v, want the known optimum 1", got)
	}
	if again := FitBlendWeights([][]float64{c0, c1, make([]float64, 4)}, labels, step, 1.0, 3); !slices.Equal(w, again) {
		t.Fatalf("fit drifted on a repeat: %v then %v", w, again)
	}
}

func TestFitBlendWeightsGuards(t *testing.T) {
	if w := FitBlendWeights(nil, []bool{true, false}, 0.1, 2, 3); len(w) != 0 {
		t.Fatalf("no components gave %v, want the empty slice", w)
	}
	oneRow := [][]float64{{1, 2}}
	if w := FitBlendWeights(oneRow, nil, 0.1, 2, 3); len(w) != 1 || w[0] != 0 {
		t.Fatalf("no labels gave %v, want a single zero weight", w)
	}
	same := []bool{true, true}
	for _, tc := range []struct {
		name            string
		step, maxWeight float64
	}{{"one class", 0.1, 2}, {"zero step", 0, 2}, {"zero cap", 0.1, 0}} {
		w := FitBlendWeights([][]float64{{1, 2}, {3, 4}}, same, tc.step, tc.maxWeight, 3)
		if w[0] != 0 || w[1] != 0 {
			t.Fatalf("%s gave weights %v, want zeros", tc.name, w)
		}
	}
}

func TestFitBlendWeightsGridStopsBelowCap(t *testing.T) {
	c0 := []float64{2, -2, 1, -1}
	labels := []bool{true, false, false, true}
	if w := FitBlendWeights([][]float64{c0}, labels, 1.0, 0.5, 3); w[0] != 0 {
		t.Fatalf("weight = %v, want 0: the grid must stop below the cap", w[0])
	}
	if w := FitBlendWeights([][]float64{c0}, labels, 0.25, 0.5, 3); w[0] != 0.25 {
		t.Fatalf("weight = %v, want the first improving step 0.25", w[0])
	}
}

func TestBootstrapResamplesDifferAndLeaveOOB(t *testing.T) {
	draw := func(seed uint64) [][]int {
		return bootstrapResamples(rand.New(rand.NewPCG(seed, seed)), 20, 50)
	}
	a, b, c := draw(1), draw(1), draw(2)
	if len(a) != 50 {
		t.Fatalf("drew %d bags, want 50", len(a))
	}
	for i := range a {
		if len(a[i]) != 20 {
			t.Fatalf("bag %d holds %d slots, want the 20 of a full resample", i, len(a[i]))
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatalf("same seed diverged in bag %d slot %d", i, j)
			}
		}
	}
	if slices.Equal(slices.Concat(a...), slices.Concat(c...)) {
		t.Fatalf("different seeds drew identical resamples")
	}
	oob := false
	for _, bag := range a {
		seen := make([]bool, 20)
		for _, j := range bag {
			seen[j] = true
		}
		for _, s := range seen {
			if !s {
				oob = true
				break
			}
		}
	}
	if !oob {
		t.Fatalf("every match landed in every bag: no out-of-bag slot")
	}
}

func TestEnsembleBaggingDeterministic(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	var train []Match
	for i := 0; i < 16; i++ {
		train = append(train, cmpMatch(int64(i+1), rng.IntN(2) == 0,
			cmpDrawSide(rng, 8), cmpDrawSide(rng, 8)))
	}
	var probes []DraftFeatures
	for i := 0; i < 4; i++ {
		probes = append(probes, FinalSetFeatures(cmpMatch(int64(1000+i), true,
			cmpDrawSide(rng, 8), cmpDrawSide(rng, 8))))
	}
	spec := EnsembleSpec{Bags: 5, NBAlpha: 1, KnnK: 3, Seed: 99, GridStep: 0.25, MaxWeight: 1, MaxPasses: 2}
	a, b := TrainEnsemble(spec, train, nil), TrainEnsemble(spec, train, nil)
	if !slices.Equal(a.Weights(), b.Weights()) {
		t.Fatalf("same seed gave weights %v and %v", a.Weights(), b.Weights())
	}
	for i, q := range probes {
		if a.ScoreMatch(q, 0.5) != b.ScoreMatch(q, 0.5) {
			t.Fatalf("same seed diverged on probe %d", i)
		}
	}
	other := TrainEnsemble(EnsembleSpec{Bags: 5, NBAlpha: 1, KnnK: 3, Seed: 100,
		GridStep: 0.25, MaxWeight: 1, MaxPasses: 2}, train, nil)
	if slices.Equal(a.Weights(), other.Weights()) {
		t.Fatalf("different bag seeds fitted identical weights")
	}
	for _, q := range probes {
		if a.ScoreMatch(q, 0.5) != other.ScoreMatch(q, 0.5) {
			return
		}
	}
	t.Fatalf("different bag seeds produced identical scores on every probe")
}

func TestEnsembleBlendFormula(t *testing.T) {
	rng := rand.New(rand.NewPCG(21, 21))
	var train []Match
	for i := 0; i < 10; i++ {
		train = append(train, cmpMatch(int64(i+1), rng.IntN(2) == 0,
			cmpDrawSide(rng, 6), cmpDrawSide(rng, 6)))
	}
	pk := map[int64]float64{}
	for _, m := range train {
		pk[m.ID] = 0.1 * float64(m.ID)
	}
	e := TrainEnsemble(EnsembleSpec{Bags: 4, NBAlpha: 1, KnnK: 2, Seed: 5,
		GridStep: 0.5, MaxWeight: 1, MaxPasses: 2}, train, pk)
	if len(e.nbs) != 4 || len(e.knns) != 4 {
		t.Fatalf("trained %d nb and %d knn bags, want 4 each", len(e.nbs), len(e.knns))
	}
	q := FinalSetFeatures(cmpMatch(99, true, cmpDrawSide(rng, 6), cmpDrawSide(rng, 6)))
	nbSum, knnSum := 0.0, 0.0
	for i := range e.nbs {
		nbSum += e.nbs[i].ScoreMatch(q)
		knnSum += e.knns[i].ScoreMatch(q)
	}
	if e.NBScore(q) != nbSum/4 || e.KNNScore(q) != knnSum/4 {
		t.Fatalf("bag averages broke: %v %v vs %v %v", e.NBScore(q), nbSum/4, e.KNNScore(q), knnSum/4)
	}
	w := e.Weights()
	want := w[0]*0.7 + w[1]*e.NBScore(q) + w[2]*e.KNNScore(q)
	if got := e.ScoreMatch(q, 0.7); got != want {
		t.Fatalf("blend = %v, want the weighted sum %v", got, want)
	}
	mut := e.Weights()
	mut[0] = 1e9
	if e.Weights()[0] == 1e9 {
		t.Fatalf("Weights leaked the internal slice")
	}
}

func TestEnsembleLearnsSkewedHero(t *testing.T) {
	var train []Match
	for i := 0; i < 20; i++ {
		win := i < 10
		radiant := []int{2 + i%8, 30 + i%5, 50 + i%3}
		if win {
			radiant = []int{1, 10 + i%8, 20 + i%6}
		}
		train = append(train, cmpMatch(int64(i), win, radiant, []int{40 + i%7, 60 + i%9}))
	}
	e := TrainEnsemble(EnsembleSpec{Bags: 5, NBAlpha: 1, KnnK: 3, Seed: 11,
		GridStep: 0.25, MaxWeight: 1, MaxPasses: 2}, train, nil)
	w := e.Weights()
	if w[1]+w[2] <= 0 {
		t.Fatalf("weights %v: no comparator weight for a learnable signal", w)
	}
	q := FinalSetFeatures(cmpMatch(99, true, []int{1, 70, 71}, []int{72, 73}))
	if got := e.ScoreMatch(q, 0); got <= 0 {
		t.Fatalf("hero-1 radiant probe scored %v, want positive", got)
	}
}

func TestComparatorsOnSyntheticDB(t *testing.T) {
	cfg := testConfig(t)
	db := seedModelDB(t)
	seedMatches(t, db)
	matches, _, err := LoadMatches(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(matches) != 4 {
		t.Fatalf("loaded %d matches, want the 4 seeded", len(matches))
	}
	train, hold := SplitMatches(matches, cfg.Eval.Split.TrainFrac)
	if len(train) != 2 || len(hold) != 2 {
		t.Fatalf("split = %d/%d, want 2/2", len(train), len(hold))
	}
	for _, m := range matches {
		if f := FinalSetFeatures(m); len(f.feats) != 10 {
			t.Fatalf("match %d final set = %d features, want the 10 picks", m.ID, len(f.feats))
		}
	}
	pk := map[int64]float64{}
	for _, m := range train {
		pk[m.ID] = 0.25
	}
	spec := EnsembleSpec{
		Bags:      4,
		NBAlpha:   cfg.Eval.NaiveBayes.Alpha,
		KnnK:      cfg.Eval.Knn.K,
		Seed:      uint64(cfg.Eval.Bootstrap.Seed),
		GridStep:  cfg.Eval.Fit.GridStep,
		MaxWeight: cfg.Eval.Fit.MaxWeight,
		MaxPasses: cfg.Eval.Fit.MaxPasses,
	}
	nb := TrainNaiveBayes(train, cfg.Eval.NaiveBayes.Alpha)
	kn := TrainKNN(train, cfg.Eval.Knn.K)
	en := TrainEnsemble(spec, train, pk)
	for _, m := range hold {
		f := FinalSetFeatures(m)
		if s := nb.ScoreMatch(f); math.IsNaN(s) || math.IsInf(s, 0) {
			t.Fatalf("nb score on match %d = %v", m.ID, s)
		}
		if s := kn.ScoreMatch(f); s < -1-1e-12 || s > 1+1e-12 {
			t.Fatalf("knn score on match %d = %v outside [-1,1]", m.ID, s)
		}
		if s := en.ScoreMatch(f, 0.25); math.IsNaN(s) || math.IsInf(s, 0) {
			t.Fatalf("ensemble score on match %d = %v", m.ID, s)
		}
	}
}
