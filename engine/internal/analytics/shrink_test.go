package analytics

import (
	"math"
	"testing"
)

func TestShrinkNoDataReturnsPrior(t *testing.T) {
	if got := Shrink(0, 0, 40, 0.5); got != 0.5 {
		t.Errorf("Shrink(0,0) = %v, want p0", got)
	}
	if got := Shrink(999, 0, 40, 0.32); got != 0.32 {
		t.Errorf("Shrink(w,0) = %v, want p0", got)
	}
}

func TestShrinkConvergesToRawRate(t *testing.T) {
	// n must dwarf alpha for the 1e-6 absolute tolerance to hold: at n=1e6 the
	// prior pull of alpha=40, p0=0.5 still shifts the posterior by 2e-5
	raw := 12300.0 / 100000000.0
	got := Shrink(12300, 100000000, 40, 0.5)
	if math.Abs(got-raw) > 1e-6 {
		t.Errorf("Shrink = %v, want ~%v", got, raw)
	}
}

func TestShrinkMonotoneTowardRaw(t *testing.T) {
	// raw rate 0.3 below p0 0.5: more samples must pull the posterior down
	const p0, alpha = 0.5, 40.0
	seq := []float64{
		Shrink(3, 10, alpha, p0),
		Shrink(30, 100, alpha, p0),
		Shrink(300, 1000, alpha, p0),
		Shrink(3000, 10000, alpha, p0),
	}
	for i := 1; i < len(seq); i++ {
		if seq[i] >= seq[i-1] {
			t.Errorf("posterior not decreasing: seq[%d]=%v seq[%d]=%v", i-1, seq[i-1], i, seq[i])
		}
		if seq[i] < 0.3 {
			t.Errorf("posterior overshot raw rate: %v", seq[i])
		}
	}
	if seq[len(seq)-1] >= seq[0] {
		t.Errorf("no movement toward raw: %v -> %v", seq[0], seq[len(seq)-1])
	}
}

func TestShrinkDeltaDegenerates(t *testing.T) {
	if got := ShrinkDelta(0.3, 0, 25); got != 0 {
		t.Errorf("ShrinkDelta(n=0) = %v, want 0", got)
	}
	if got := ShrinkDelta(0.3, 1e9, 25); math.Abs(got-0.3) > 1e-6 {
		t.Errorf("ShrinkDelta(n huge) = %v, want ~0.3", got)
	}
	if got, want := ShrinkDelta(0.3, 25, 25), 0.15; math.Abs(got-want) > 1e-12 {
		t.Errorf("ShrinkDelta = %v, want %v", got, want)
	}
}
