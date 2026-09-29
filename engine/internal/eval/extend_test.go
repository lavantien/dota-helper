package eval

import (
	"math/rand/v2"
	"testing"

	"poolguide/internal/config"
)

// cmpCfg copies the hub config and shrinks the diagnostics knobs so the
// section builders stay fixture-fast: 50 bootstrap resamples, 4 bags.
func cmpCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := *testConfig(t)
	cfg.Eval.Bootstrap.Resamples = 50
	cfg.Eval.Ensemble.Bags = 4
	return &cfg
}

// TestBuildComparatorsScoresEveryHoldoutMatch pins the comparator section:
// three rows scored over the two-sided holdout the caller passes (the same
// minSidePool population the replay AUC reads), plus the bagged ensemble
// with its blend weights and its own holdout interval, all deterministic.
func TestBuildComparatorsScoresEveryHoldoutMatch(t *testing.T) {
	cfg := cmpCfg(t)
	rng := rand.New(rand.NewPCG(7, 7))
	var train, holdout []Match
	for i := 0; i < 40; i++ {
		m := cmpMatch(int64(i), rng.IntN(2) == 0, cmpDrawSide(rng, 8), cmpDrawSide(rng, 8))
		if i < 25 {
			train = append(train, m)
		} else {
			holdout = append(holdout, m)
		}
	}
	adv := map[int64]float64{}
	for i, m := range append(append([]Match(nil), train...), holdout...) {
		adv[m.ID] = float64(i%3 - 1)
	}
	sec := buildComparators(cfg, train, holdout, adv)
	want := []string{"picker", "naiveBayes", "knn"}
	if len(sec.Rows) != len(want) {
		t.Fatalf("rows = %d, want %v", len(sec.Rows), want)
	}
	for i, name := range want {
		r := sec.Rows[i]
		if r.Name != name {
			t.Fatalf("row %d = %s, want %s", i, r.Name, name)
		}
		if r.Matches != len(holdout) {
			t.Errorf("%s matches = %d, want %d", r.Name, r.Matches, len(holdout))
		}
		if r.AUC < 0 || r.AUC > 1 || r.CI.Lo > r.AUC || r.CI.Hi < r.AUC {
			t.Errorf("%s auc %v ci [%v, %v] malformed", r.Name, r.AUC, r.CI.Lo, r.CI.Hi)
		}
	}
	if sec.Ensemble == nil {
		t.Fatal("ensemble section missing")
	}
	if sec.Ensemble.Bags != cfg.Eval.Ensemble.Bags {
		t.Errorf("ensemble bags = %d, want %d", sec.Ensemble.Bags, cfg.Eval.Ensemble.Bags)
	}
	if len(sec.Ensemble.Weights) != 3 {
		t.Errorf("ensemble weights = %v, want 3 components", sec.Ensemble.Weights)
	}
	if sec.Ensemble.Matches != len(holdout) || sec.Ensemble.AUC < 0 || sec.Ensemble.AUC > 1 {
		t.Errorf("ensemble auc %+v malformed", sec.Ensemble)
	}
	if sec.Ensemble.CI == (CI{}) {
		t.Error("ensemble bootstrap interval missing")
	}
	again := buildComparators(cfg, train, holdout, adv)
	if again.Ensemble.AUC != sec.Ensemble.AUC || again.Ensemble.Weights[0] != sec.Ensemble.Weights[0] {
		t.Error("ensemble training is not deterministic")
	}
}

// TestBuildTriplesCfgViewAndSweep pins the triples section: the embedded
// stats are the cfg-threshold view, and the sweep counts pairs and triples
// cumulatively at every distinct support the floor mining observed, so the
// cfg threshold's place in the support landscape is data-derived.
func TestBuildTriplesCfgViewAndSweep(t *testing.T) {
	cfg := cmpCfg(t)
	cfg.Eval.Triples.MinSupport = 3
	m := tripModel(t)
	train := tripFixture()
	sec := buildTriples(cfg, m, train)
	if sec.MinSupport != 3 || len(sec.Triples) != 3 {
		t.Fatalf("cfg view = %d triples at minSupport %d, want the 3 fixture triples", len(sec.Triples), sec.MinSupport)
	}
	if len(sec.Sweep) == 0 {
		t.Fatal("support sweep empty")
	}
	// the sweep floor is the smallest observed pair support: no fixture pair
	// co-occurs below 3, where all 8 observed pairs and 3 triples sit
	last := sec.Sweep[len(sec.Sweep)-1]
	if last != (SupportPoint{Support: 3, Pairs: 8, Triples: 3}) {
		t.Fatalf("sweep floor = %+v, want {3 8 3}", last)
	}
	for i := 1; i < len(sec.Sweep); i++ {
		lo, hi := sec.Sweep[i], sec.Sweep[i-1]
		if lo.Support >= hi.Support {
			t.Fatalf("sweep not strictly descending: %d then %d", hi.Support, lo.Support)
		}
		if lo.Pairs < hi.Pairs || lo.Triples < hi.Triples {
			t.Fatalf("cumulative counts shrink as support falls: %+v then %+v", hi, lo)
		}
	}
	for _, p := range sec.Sweep {
		if p.Support == cfg.Eval.Triples.MinSupport &&
			(p.Pairs != len(sec.Pairs) || p.Triples != len(sec.Triples)) {
			t.Fatalf("sweep at %d = %+v, cfg view %d pairs %d triples",
				p.Support, p, len(sec.Pairs), len(sec.Triples))
		}
	}
}

// TestBuildSeqSectionCarriesCensus pins the seq section: the train pick
// streams feed the direction census under the config thresholds.
func TestBuildSeqSectionCarriesCensus(t *testing.T) {
	cfg := cmpCfg(t)
	cfg.Eval.Seq.CensusMinCount = 1
	cfg.Eval.Seq.CensusRatio = 2
	sec := buildSeq(cfg, tripModel(t), tripFixture())
	if sec.RosterSize != 6 || sec.Matches != 8 {
		t.Fatalf("seq stats = roster %d matches %d, want 6 and 8", sec.RosterSize, sec.Matches)
	}
	if sec.Census.Pairs == 0 {
		t.Fatal("census compared no pairs")
	}
	if sec.Census.BothWays > sec.Census.Pairs || sec.Census.Asymmetric > sec.Census.Pairs {
		t.Fatalf("census = %+v, inconsistent with its pair count", sec.Census)
	}
}
