package stats

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestSpearmanMonotone(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	ys := []float64{10, 20, 30, 40, 50}
	if got := Spearman(xs, ys); math.Abs(got-1) > 1e-12 {
		t.Errorf("Spearman increasing = %v, want 1", got)
	}
	if got := Spearman(xs, []float64{5, 4, 3, 2, 1}); math.Abs(got+1) > 1e-12 {
		t.Errorf("Spearman decreasing = %v, want -1", got)
	}
	// ranks, not values: a monotone but nonlinear map still gives 1
	if got := Spearman(xs, []float64{1, 4, 9, 16, 25}); math.Abs(got-1) > 1e-12 {
		t.Errorf("Spearman squares = %v, want 1", got)
	}
}

// hand-computed tie block: xs {1,2,3,4} ranks 1,2,3,4; ys {1,2,2,3} ranks
// 1, 2.5, 2.5, 4. deviations dx -1.5,-0.5, 0.5, 1.5 and dy -1.5, 0, 0, 1.5,
// so 4.5 / sqrt(5 * 4.5) = 3/sqrt(10)
func TestSpearmanTieBlock(t *testing.T) {
	got := Spearman([]float64{1, 2, 3, 4}, []float64{1, 2, 2, 3})
	if want := 3 / math.Sqrt(10); math.Abs(got-want) > 1e-12 {
		t.Errorf("Spearman = %v, want 3/sqrt(10) = %v", got, want)
	}
}

func TestSpearmanSwapSymmetric(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 11))
	xs, _ := genScored(rng, 100)
	ys, _ := genScored(rng, 100)
	if math.Abs(Spearman(xs, ys)-Spearman(ys, xs)) > 1e-12 {
		t.Errorf("Spearman not symmetric in its arguments")
	}
}

// length mismatch is a caller bug, matching the emit package's loud failure
func TestSpearmanLengthMismatchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Spearman with 3 xs and 2 ys must panic")
		}
	}()
	Spearman([]float64{1, 2, 3}, []float64{1, 2})
}

// constant input has zero rank variance: 0/0, the math package idiom is NaN
func TestSpearmanConstantInputNaN(t *testing.T) {
	if !math.IsNaN(Spearman([]float64{2, 2, 2, 2}, []float64{1, 2, 3, 4})) {
		t.Error("Spearman constant xs = not NaN")
	}
	if !math.IsNaN(Spearman([]float64{1, 2, 3, 4}, []float64{7, 7, 7, 7})) {
		t.Error("Spearman constant ys = not NaN")
	}
	if !math.IsNaN(Spearman(nil, nil)) {
		t.Error("Spearman empty = not NaN")
	}
}
