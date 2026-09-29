package stats

import (
	"math"
	"math/rand/v2"
	"testing"
)

// ch18 fixture: six pinned scores, three positives, three negatives, one rank
// inversion (positive 0.625 under negative 0.6875), concordant 8 of 9.
func TestAUCCh18Fixture(t *testing.T) {
	scores := []float64{0.875, 0.6875, 0.75, 0.625, 0.25, 0.125}
	labels := []bool{true, false, true, true, false, false}
	if got, want := AUC(scores, labels), 8.0/9.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("AUC = %v, want 8/9", got)
	}
}

// hand example with a cross-class tie: positives {2,1}, negatives {1,0},
// pairs 1 + 1 + 0.5 + 1 = 3.5 of 4, dyadic so exact
func TestAUCTieWorthHalf(t *testing.T) {
	if got := AUC([]float64{2, 1, 1, 0}, []bool{true, false, true, false}); got != 0.875 {
		t.Errorf("AUC = %v, want 0.875", got)
	}
	// every score equal: all pairs tie, exactly half the weight
	if got := AUC([]float64{1, 1, 1, 1}, []bool{true, true, false, false}); got != 0.5 {
		t.Errorf("AUC all ties = %v, want 0.5", got)
	}
}

func TestAUCPerfectSeparation(t *testing.T) {
	if got := AUC([]float64{0.9, 0.8, 0.3, 0.2}, []bool{true, true, false, false}); got != 1 {
		t.Errorf("AUC = %v, want 1", got)
	}
	// same ranking, input order reversed: sorting owns the answer
	if got := AUC([]float64{0.2, 0.3, 0.8, 0.9}, []bool{false, false, true, true}); got != 1 {
		t.Errorf("AUC reversed = %v, want 1", got)
	}
}

// either class empty leaves no cross-class pair, ch18 declares the
// concordance undefined in words; this API pins the 0.5 chance line
func TestAUCSingleClassReturnsHalf(t *testing.T) {
	scores := []float64{0.9, 0.8, 0.7}
	if got := AUC(scores, []bool{true, true, true}); got != 0.5 {
		t.Errorf("AUC all positive = %v, want 0.5", got)
	}
	if got := AUC(scores, []bool{false, false, false}); got != 0.5 {
		t.Errorf("AUC all negative = %v, want 0.5", got)
	}
	if got := AUC(nil, nil); got != 0.5 {
		t.Errorf("AUC empty = %v, want 0.5", got)
	}
}

func TestAUCLengthMismatchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("AUC with 3 scores and 2 labels must panic")
		}
	}()
	AUC([]float64{1, 2, 3}, []bool{true, false})
}

func TestAUCLabelFlipSymmetry(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	scores, labels := genScored(rng, 300)
	flipped := make([]bool, len(labels))
	for i, pos := range labels {
		flipped[i] = !pos
	}
	if got, want := AUC(scores, labels)+AUC(scores, flipped), 1.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("AUC + flipped AUC = %v, want 1", got)
	}
}

// the rank-based O(n log n) computation must agree with the chapter's
// definition, the O(n^2) concordant-pair count, on tied random data
func TestAUCMatchesBruteForce(t *testing.T) {
	for trial := range 5 {
		rng := rand.New(rand.NewPCG(uint64(trial+1), uint64(trial+1)))
		scores, labels := genScored(rng, 200)
		if got, want := AUC(scores, labels), bruteAUC(scores, labels); math.Abs(got-want) > 1e-12 {
			t.Errorf("trial %d: AUC = %v, brute force %v", trial, got, want)
		}
	}
}

// backtest shape: 20k rows must stay a fast, in-range answer
func TestAUCLargeInput(t *testing.T) {
	rng := rand.New(rand.NewPCG(99, 99))
	scores, labels := genScored(rng, 20000)
	got := AUC(scores, labels)
	if got < 0 || got > 1 {
		t.Errorf("AUC = %v, out of [0,1]", got)
	}
}

// genScored draws labels coin-flip and scores quantized to fifths so tie
// groups are common, exercising the midrank half-credit path.
func genScored(rng *rand.Rand, n int) ([]float64, []bool) {
	scores := make([]float64, n)
	labels := make([]bool, n)
	for i := range scores {
		labels[i] = rng.IntN(2) == 0
		scores[i] = math.Round(rng.Float64()*5) / 5
	}
	return scores, labels
}

// bruteAUC is the chapter's definition straight: every positive-negative
// pair, concordant 1, tie 0.5, over the pair count.
func bruteAUC(scores []float64, labels []bool) float64 {
	nPos, nNeg := 0, 0
	for _, pos := range labels {
		if pos {
			nPos++
		} else {
			nNeg++
		}
	}
	if nPos == 0 || nNeg == 0 {
		return 0.5
	}
	concordant := 0.0
	for i, pos := range labels {
		if !pos {
			continue
		}
		for j, neg := range labels {
			if neg {
				continue
			}
			switch {
			case scores[i] > scores[j]:
				concordant++
			case scores[i] == scores[j]:
				concordant += 0.5
			}
		}
	}
	return concordant / float64(nPos*nNeg)
}
