package analytics

import (
	"math"
	"testing"

	"poolguide/internal/config"
)

func testWeights() config.Weights {
	return config.Weights{KnownMu: 1.0,
		SynByRole:   map[string]float64{"1": 0.7, "2": 0.2, "3": 0.3, "4": 1, "5": 1},
		Prior:       0.5, GenericFit: 0.4, Exposure: 0.5, Flexibility: 0.3}
}

func testScoreConsts() config.ScoreConsts {
	return config.ScoreConsts{EnemySlots: 5, AllySlots: 4, MidPct: 0.5, FlexCap: 50, FlexHalf: 2}
}

func mainFixture() (Candidate, Draft) {
	cand := Candidate{
		Idx:   5,
		Role:  "2",
		Prior: 0.2,
		Mu:    map[int]float64{0: 0.8, 1: 0.6, 2: 0.4, 6: 0.1, 7: 0.3, 8: 0.5, 10: 0.7, 11: 0.9},
		Syn:   map[int]float64{3: 0.9, 4: 0.5, 6: 0.2, 7: 0.4, 8: 0.6, 10: 0.8, 11: 1.0},
	}
	d := Draft{
		RosterSize:      12,
		Pop:             map[int]float64{6: 0.4, 7: 0.3, 8: 0.2, 10: 0.1, 11: 0},
		VisibleEnemies:  []int{0, 1, 2},
		Allies:          []int{3, 4},
		Taken:           []int{0, 1, 2, 3, 4, 9},
	}
	return cand, d
}

func TestScoreHandComputed(t *testing.T) {
	cand, d := mainFixture()
	term := Score(cand, d, testWeights(), testScoreConsts())

	if term.KnownMu != 0.6 {
		t.Errorf("KnownMu = %v, want 0.6", term.KnownMu)
	}
	if term.KnownSyn != 0.7 {
		t.Errorf("KnownSyn = %v, want 0.7", term.KnownSyn)
	}
	if term.Prior != 0.2 {
		t.Errorf("Prior = %v, want 0.2", term.Prior)
	}
	if math.Abs(term.GenericFit-0.4) > 1e-9 {
		t.Errorf("GenericFit = %v, want 0.4", term.GenericFit)
	}
	if math.Abs(term.Exposure-0.22) > 1e-9 {
		t.Errorf("Exposure = %v, want 0.22", term.Exposure)
	}
	if math.Abs(term.Flexibility-(-math.Sqrt(0.08)/2)) > 1e-9 {
		t.Errorf("Flexibility = %v, want %v", term.Flexibility, -math.Sqrt(0.08)/2)
	}
	if term.GateDelta != 0 {
		t.Errorf("GateDelta = %v, want 0", term.GateDelta)
	}

	// exact accumulation order: KnownMu, KnownSyn (role weighted), Prior,
	// GenericFit, -Exposure, Flexibility, GateDelta. Fixture role is 2.
	w := testWeights()
	k, a := 3.0, 2.0
	want := 0.0
	want += w.KnownMu * (k / 5.0) * 0.6
	want += w.SynByRole["2"] * (a / 5.0) * 0.7
	want += w.Prior * 0.2
	want += w.GenericFit * ((4.0 - a) / 4.0) * 0.4
	want -= w.Exposure * ((5.0 - k) / 5.0) * 0.22
	want += w.Flexibility * (-math.Sqrt(0.08) / 2)
	if math.Abs(term.Score-want) > 1e-9 {
		t.Errorf("Score = %v, want %v", term.Score, want)
	}
}

// the syn phase weight keys on the role being picked: supports pair with the
// picked allies at full weight, cores at a reduced weight
func TestScoreSynRoleLadder(t *testing.T) {
	cand, d := mainFixture()
	w := testWeights()
	scores := map[string]float64{}
	for _, role := range []string{"1", "2", "3", "4", "5"} {
		c := cand
		c.Role = role
		scores[role] = Score(c, d, w, testScoreConsts()).Score
	}
	contrib := func(r string) float64 { return w.SynByRole[r] * (2.0 / 5.0) * 0.7 }
	if math.Abs((scores["4"]-scores["2"])-(contrib("4")-contrib("2"))) > 1e-12 {
		t.Errorf("support minus mid delta %v, want the syn ladder difference %v", scores["4"]-scores["2"], contrib("4")-contrib("2"))
	}
	if math.Abs((scores["1"]-scores["3"])-(contrib("1")-contrib("3"))) > 1e-12 {
		t.Errorf("carry minus offlane delta %v, want the syn ladder difference %v", scores["1"]-scores["3"], contrib("1")-contrib("3"))
	}
	if math.Abs(scores["5"]-scores["4"]) > 1e-12 {
		t.Errorf("both supports share a weight but scored differently: %v vs %v", scores["5"], scores["4"])
	}
}

func TestScoreSynRoleMissingPanics(t *testing.T) {
	cand, d := mainFixture()
	w := testWeights()
	w.SynByRole = map[string]float64{"1": 1}
	cand.Role = "2"
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on a role missing from synByRole")
		}
	}()
	Score(cand, d, w, testScoreConsts())
}

func TestScoreGateDeltaAdded(t *testing.T) {
	cand, d := mainFixture()
	base := Score(cand, d, testWeights(), testScoreConsts())
	cand.GateDelta = 0.25
	withGate := Score(cand, d, testWeights(), testScoreConsts())
	if withGate.GateDelta != 0.25 || math.Abs(withGate.Score-(base.Score+0.25)) > 1e-12 {
		t.Errorf("gate delta not added last: base %v got %v", base.Score, withGate.Score)
	}
}

func TestScoreFirstPickDegenerate(t *testing.T) {
	cand := Candidate{Idx: 5, Role: "2", Mu: map[int]float64{}, Syn: map[int]float64{}}
	d := Draft{
		RosterSize: 12,
		Pop:        map[int]float64{6: 0.4, 7: 0.3, 8: 0.2, 10: 0.1, 11: 0},
		Taken:      []int{9},
	}
	cand.Syn = map[int]float64{6: 0.2, 7: 0.4}
	cand.Mu = map[int]float64{6: 0.1, 7: 0.3}
	term := Score(cand, d, testWeights(), testScoreConsts())
	if term.KnownMu != 0 || term.KnownSyn != 0 {
		t.Errorf("first pick must zero known terms: %v %v", term.KnownMu, term.KnownSyn)
	}
	if term.GenericFit == 0 || term.Exposure == 0 {
		t.Errorf("first pick should still score generic fit and exposure: %+v", term)
	}
}

func TestScoreLastPickDegenerate(t *testing.T) {
	cand := Candidate{
		Idx:  9,
		Role: "2",
		Mu:   map[int]float64{0: 0.9, 1: 0.1, 2: 0.5, 3: 0.7, 4: 0.3},
		Syn: map[int]float64{5: 0.8, 6: 0.6, 7: 0.4, 8: 0.2},
	}
	d := Draft{
		RosterSize:     10,
		Pop:            map[int]float64{},
		VisibleEnemies: []int{0, 1, 2, 3, 4},
		Allies:         []int{5, 6, 7, 8},
		Taken:          []int{0, 1, 2, 3, 4, 5, 6, 7, 8},
	}
	term := Score(cand, d, testWeights(), testScoreConsts())
	if term.GenericFit != 0 || term.Exposure != 0 || term.Flexibility != 0 {
		t.Errorf("last pick must zero unseen terms: %+v", term)
	}
	if term.KnownMu == 0 || term.KnownSyn == 0 {
		t.Errorf("last pick known terms must be live: %+v", term)
	}
}

func TestScoreAllPopZeroPlainMean(t *testing.T) {
	cand, d := mainFixture()
	// every unseen ally weight is 0 -> plain mean of syn over [6,7,8,10,11]
	d.Pop = map[int]float64{6: 0, 7: 0, 8: 0, 10: 0, 11: 0}
	term := Score(cand, d, testWeights(), testScoreConsts())
	if math.Abs(term.GenericFit-0.6) > 1e-9 {
		t.Errorf("GenericFit = %v, want plain mean 0.6", term.GenericFit)
	}
	// exposure likewise falls back to plain mean of relu(0.5-mu) = 0.4,0.2,0,0,0
	if math.Abs(term.Exposure-0.12) > 1e-9 {
		t.Errorf("Exposure = %v, want plain mean 0.12", term.Exposure)
	}
}

func TestRankTieBreakByIdx(t *testing.T) {
	_, d := mainFixture()
	mk := func(idx int) Candidate {
		return Candidate{
			Idx:   idx,
			Role:  "2",
			Prior: 0.2,
			Mu:    map[int]float64{0: 0.8, 1: 0.6, 2: 0.4, 6: 0.1, 7: 0.3, 8: 0.5, 10: 0.7, 11: 0.9},
			Syn:   map[int]float64{3: 0.9, 4: 0.5, 6: 0.2, 7: 0.4, 8: 0.6, 10: 0.8, 11: 1.0},
		}
	}
	cands := []Candidate{mk(7), mk(2), mk(2)}
	got := Rank(cands, d, testWeights(), testScoreConsts())
	if got[0].Idx != 2 || got[1].Idx != 2 || got[2].Idx != 7 {
		t.Errorf("Rank order = %d %d %d, want 2 2 7", got[0].Idx, got[1].Idx, got[2].Idx)
	}
	if got[0].Score != got[1].Score {
		t.Errorf("identical candidates scored differently: %v vs %v", got[0].Score, got[1].Score)
	}
}
