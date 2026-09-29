package eval

import (
	"math"
	"math/rand/v2"
	"testing"
)

// cmpMatch builds a synthetic picks-only match; heroes are bare ids.
func cmpMatch(id int64, radiantWin bool, radiant, dire []int) Match {
	m := Match{ID: id, RadiantWin: radiantWin}
	seq := 0
	add := func(hero int, isRadiant bool) {
		m.Events = append(m.Events, DraftEvent{Seq: seq, IsRadiant: isRadiant, IsPick: true, HeroID: hero})
		seq++
	}
	for _, h := range radiant {
		add(h, true)
	}
	for _, h := range dire {
		add(h, false)
	}
	return m
}

// cmpSwapFlip relabels the sides of a match and flips the label with them:
// the same game read from the other side's name for radiant.
func cmpSwapFlip(m Match) Match {
	out := Match{ID: m.ID, StartTime: m.StartTime, RadiantWin: !m.RadiantWin}
	for _, ev := range m.Events {
		ev.IsRadiant = !ev.IsRadiant
		out.Events = append(out.Events, ev)
	}
	return out
}

func cmpDrawSide(rng *rand.Rand, heroes int) []int {
	var side []int
	for h := 0; h < heroes; h++ {
		if rng.IntN(2) == 0 {
			side = append(side, h)
		}
	}
	return side
}

func cmpShuffle(rng *rand.Rand, ms []Match) []Match {
	out := append([]Match(nil), ms...)
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// hand fixture: four drafts over heroes 1..4, alpha 1
//
//	m1: R{1,2} D{3,4} win    m2: R{1,3} D{2,4} win
//	m3: R{2,4} D{1,3} loss   m4: R{3,4} D{1,2} loss
//
// per-feature counts (win/loss):
//
//	(1,R) 2/0  (1,D) 0/2  (2,R) 1/1  (2,D) 1/1
//	(3,R) 1/1  (3,D) 1/1  (4,R) 0/2  (4,D) 2/0
//
// so P(f|c) = (count+1)/(2+2), the exact ch19 fractions every pin below
// reads off.
func nbHandTrain() []Match {
	return []Match{
		cmpMatch(1, true, []int{1, 2}, []int{3, 4}),
		cmpMatch(2, true, []int{1, 3}, []int{2, 4}),
		cmpMatch(3, false, []int{2, 4}, []int{1, 3}),
		cmpMatch(4, false, []int{3, 4}, []int{1, 2}),
	}
}

func feats(radiant, dire []int) DraftFeatures {
	return FinalSetFeatures(cmpMatch(999, true, radiant, dire))
}

func TestNaiveBayesHandFixtureExactFractions(t *testing.T) {
	nb := TrainNaiveBayes(nbHandTrain(), 1)
	// (1,R): (2+1)/4 over (0+1)/4, (4,D): (2+1)/4 over (0+1)/4 -> 2*log 3
	want := 2 * math.Log(3)
	if got := nb.ScoreMatch(feats([]int{1}, []int{4})); math.Abs(got-want) > 1e-12 {
		t.Fatalf("R{1} D{4} score = %v, want 2*ln3 = %v", got, want)
	}
	// (2,R) is 1/1: the exact tie scores log(1) = 0, prior odds are 2/2
	if got := nb.ScoreMatch(feats([]int{2}, nil)); got != 0 {
		t.Fatalf("R{2} score = %v, want the exact 0 tie", got)
	}
	if got := nb.ScoreMatch(feats(nil, nil)); got != 0 {
		t.Fatalf("empty draft score = %v, want the 2/2 prior log-odds 0", got)
	}
	// determinism: the ascending feature sum is bit-identical across calls
	first := nb.ScoreMatch(feats([]int{1, 2, 3, 4}, []int{1, 2, 3, 4}))
	for i := 0; i < 100; i++ {
		if got := nb.ScoreMatch(feats([]int{1, 2, 3, 4}, []int{1, 2, 3, 4})); got != first {
			t.Fatalf("repeated score drifted: %v then %v", first, got)
		}
	}
}

func TestNaiveBayesZeroCellRescue(t *testing.T) {
	nb := TrainNaiveBayes(nbHandTrain(), 1)
	// (4,R) never won: the smoothed (0+1)/4 over (2+1)/4 stays finite
	want := math.Log(1.0 / 3.0)
	got := nb.ScoreMatch(feats([]int{4}, nil))
	if math.IsInf(got, 0) || math.IsNaN(got) {
		t.Fatalf("zero cell scored %v, want the Laplace rescue", got)
	}
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("R{4} score = %v, want ln(1/3) = %v", got, want)
	}
	// hero 7 is out of vocabulary in both classes: skipped, not a prior copy
	if got := nb.ScoreMatch(feats([]int{7}, nil)); got != 0 {
		t.Fatalf("out-of-vocabulary hero scored %v, want 0", got)
	}
}

func TestNaiveBayesLabelFlipSymmetry(t *testing.T) {
	nb := TrainNaiveBayes(nbHandTrain(), 1)
	flipped := make([]Match, 0, 4)
	for _, m := range nbHandTrain() {
		flipped = append(flipped, cmpSwapFlip(m))
	}
	nb2 := TrainNaiveBayes(flipped, 1)
	q := cmpMatch(9, true, []int{1, 3}, []int{2, 4})
	a := nb.ScoreMatch(FinalSetFeatures(q))
	b := nb2.ScoreMatch(FinalSetFeatures(cmpSwapFlip(q)))
	if math.Abs(a+b) > 1e-12 {
		t.Fatalf("side relabel gave %v and %v, want exact negation", a, b)
	}
}

func TestNaiveBayesFlipSymmetryProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1234, 1234))
	for trial := 0; trial < 25; trial++ {
		var train []Match
		n := 3 + rng.IntN(10)
		for i := 0; i < n; i++ {
			train = append(train, cmpMatch(int64(i), rng.IntN(2) == 0,
				cmpDrawSide(rng, 6), cmpDrawSide(rng, 6)))
		}
		q := cmpMatch(999, true, cmpDrawSide(rng, 6), cmpDrawSide(rng, 6))
		flipped := make([]Match, 0, len(train))
		for _, m := range train {
			flipped = append(flipped, cmpSwapFlip(m))
		}
		a := TrainNaiveBayes(train, 1).ScoreMatch(FinalSetFeatures(q))
		b := TrainNaiveBayes(flipped, 1).ScoreMatch(FinalSetFeatures(cmpSwapFlip(q)))
		if math.Abs(a+b) > 1e-9 {
			t.Fatalf("trial %d: relabel gave %v and %v, want negation", trial, a, b)
		}
	}
}

func TestNaiveBayesTrainOrderInvariant(t *testing.T) {
	rng := rand.New(rand.NewPCG(77, 77))
	var train []Match
	for i := 0; i < 12; i++ {
		train = append(train, cmpMatch(int64(i), rng.IntN(2) == 0,
			cmpDrawSide(rng, 8), cmpDrawSide(rng, 8)))
	}
	nb := TrainNaiveBayes(train, 1)
	shuffled := TrainNaiveBayes(cmpShuffle(rng, train), 1)
	for i := 0; i < 5; i++ {
		q := FinalSetFeatures(cmpMatch(int64(500+i), true, cmpDrawSide(rng, 8), cmpDrawSide(rng, 8)))
		if nb.ScoreMatch(q) != shuffled.ScoreMatch(q) {
			t.Fatalf("probe %d: train order changed the score", i)
		}
	}
}

func TestNaiveBayesDegenerateTrain(t *testing.T) {
	if got := TrainNaiveBayes(nil, 1).ScoreMatch(feats([]int{1}, nil)); got != 0 {
		t.Fatalf("empty train scored %v, want 0", got)
	}
	allWin := []Match{cmpMatch(1, true, []int{1}, []int{2})}
	if got := TrainNaiveBayes(allWin, 1).ScoreMatch(feats([]int{1}, nil)); got != 0 {
		t.Fatalf("one-class train scored %v, want the flat 0 chance line", got)
	}
}

func TestNaiveBayesAlphaContract(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("non-positive alpha must panic, the zero-cell rescue would be gone")
		}
	}()
	TrainNaiveBayes(nil, 0)
}
