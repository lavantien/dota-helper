package eval

import (
	"math"
	"testing"
)

func mkPick(poolN, rank int, pct float64, roles map[string]RoleRank) PickEval {
	return PickEval{PoolN: poolN, PoolRank: rank, Percentile: pct, RoleRank: roles}
}

func TestMeanPercentileSkipsUnranked(t *testing.T) {
	picks := []PickEval{
		mkPick(3, 1, 1.0, nil),
		mkPick(2, 2, 0.0, nil),
		mkPick(4, 2, 0.6666666666666666, nil),
		mkPick(1, 1, 0, nil), // single-candidate pick: no percentile
		mkPick(5, 0, 0, nil), // gated out of its own field: no percentile
	}
	want := (1.0 + 0.0 + 0.6666666666666666) / 3
	if got := MeanPercentile(picks); math.Abs(got-want) > 1e-12 {
		t.Fatalf("MeanPercentile = %v, want %v", got, want)
	}
	if got := MeanPercentile(nil); got != 0 {
		t.Fatalf("MeanPercentile(nil) = %v, want 0", got)
	}
}

func TestAnyRoleHitRate(t *testing.T) {
	picks := []PickEval{
		mkPick(3, 1, 1.0, map[string]RoleRank{"1": {Rank: 1, N: 3}, "2": {Rank: 2, N: 4}}),
		mkPick(3, 2, 0.5, map[string]RoleRank{"1": {Rank: 2, N: 3}, "2": {Rank: 1, N: 4}}), // top-1 via role 2
		mkPick(3, 3, 0.0, map[string]RoleRank{"1": {Rank: 3, N: 3}}),
		mkPick(3, 2, 0.5, map[string]RoleRank{"1": {Rank: 0, N: 3}}), // gated out of the role
	}
	if got := AnyRoleHitRate(picks, 1); got != 0.5 {
		t.Fatalf("top-1 hit rate = %v, want 0.5", got)
	}
	if got := AnyRoleHitRate(picks, 3); got != 0.75 {
		t.Fatalf("top-3 hit rate = %v, want 0.75", got)
	}
}

func TestDraftAUC(t *testing.T) {
	replays := []MatchReplay{
		{Advantage: 0.2, RadiantWin: true},
		{Advantage: -0.1, RadiantWin: false},
		{Advantage: 0.5, RadiantWin: true},
	}
	if got := DraftAUC(replays); math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("AUC = %v, want 1", got)
	}
	for i := range replays {
		replays[i].RadiantWin = !replays[i].RadiantWin
	}
	if got := DraftAUC(replays); math.Abs(got-0.0) > 1e-12 {
		t.Fatalf("inverted AUC = %v, want 0", got)
	}
	// one-class input reads as the 0.5 chance line, per stats.AUC
	one := []MatchReplay{
		{Advantage: 0.2, RadiantWin: true},
		{Advantage: -0.1, RadiantWin: true},
	}
	if got := DraftAUC(one); got != 0.5 {
		t.Fatalf("one-class AUC = %v, want 0.5", got)
	}
}

func TestSplitMatchesBoundary(t *testing.T) {
	mk := func(id int64, st int64) Match { return Match{ID: id, StartTime: st} }
	var ms []Match
	for i := 0; i < 10; i++ {
		ms = append(ms, mk(int64(i), int64(i)))
	}
	train, hold := SplitMatches(ms, 0.7)
	if len(train) != 7 || len(hold) != 3 {
		t.Fatalf("10-match 0.7 split = %d/%d, want 7/3", len(train), len(hold))
	}
	ms = ms[:5]
	train, hold = SplitMatches(ms, 0.7)
	if len(train) != 3 || len(hold) != 2 {
		t.Fatalf("5-match 0.7 split = %d/%d, want the truncated 3/2", len(train), len(hold))
	}
	train, hold = SplitMatches(ms[:1], 0.7)
	if len(train) != 0 || len(hold) != 1 {
		t.Fatalf("1-match 0.7 split = %d/%d, want 0/1", len(train), len(hold))
	}
}

func TestCalibrateDecileEdges(t *testing.T) {
	var trainScores []float64
	for i := 0; i < 100; i++ {
		trainScores = append(trainScores, float64(i))
	}
	holdScores := []float64{5, 15, 95}
	holdWon := []bool{false, true, true}
	cal := Calibrate(trainScores, holdScores, holdWon, 10)
	if len(cal.Edges) != 9 {
		t.Fatalf("edges = %v, want 9 decile edges", cal.Edges)
	}
	for i, e := range cal.Edges {
		if want := float64(10*(i+1) - 1); e != want {
			t.Fatalf("edge %d = %v, want the nearest-rank %v", i, e, want)
		}
	}
	byIdx := map[int]CalibrationBin{}
	for _, b := range cal.Bins {
		byIdx[b.Index] = b
	}
	for _, idx := range []int{0, 1, 9} {
		if b := byIdx[idx]; b.N != 1 {
			t.Fatalf("bin %d count = %d, want 1", idx, b.N)
		}
	}
	if byIdx[0].Rate != 0 || byIdx[1].Rate != 1 || byIdx[9].Rate != 1 {
		t.Fatalf("holdout rates = %v %v %v, want 0 1 1", byIdx[0].Rate, byIdx[1].Rate, byIdx[9].Rate)
	}
	if cal.Violations != 0 {
		t.Fatalf("violations = %d, want 0", cal.Violations)
	}
	// rates 1, 0, 0: exactly one adjacent drop (the 0 -> 0 pair is flat)
	cal = Calibrate(trainScores, holdScores, []bool{true, false, false}, 10)
	if cal.Violations != 1 {
		t.Fatalf("violations = %d, want 1", cal.Violations)
	}
}

func TestMatchesCIDeterministic(t *testing.T) {
	metric := func(idx []int) float64 {
		sum := 0.0
		for _, i := range idx {
			sum += float64(i)
		}
		return sum / float64(len(idx))
	}
	a := MatchesCI(50, 200, 42, metric)
	b := MatchesCI(50, 200, 42, metric)
	if a != b {
		t.Fatalf("same seed gave %v and %v", a, b)
	}
	if a.Lo > b.Hi || a.Lo == 0 && a.Hi == 0 {
		t.Fatalf("ci %v not ordered/nonempty", a)
	}
	if got := MatchesCI(0, 200, 42, metric); got != (CI{}) {
		t.Fatalf("n=0 ci = %v, want the zero interval", got)
	}
}
