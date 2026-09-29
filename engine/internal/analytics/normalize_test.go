package analytics

import (
	"math"
	"testing"
)

func TestMidrankTiesAveraged(t *testing.T) {
	got := Midrank([]float64{1, 2, 2, 3})
	want := []float64{0.125, 0.5, 0.5, 0.875}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Errorf("Midrank[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMidrankAllEqual(t *testing.T) {
	got := Midrank([]float64{7, 7, 7})
	for i, v := range got {
		if v != 0.5 {
			t.Errorf("Midrank[%d] = %v, want 0.5", i, v)
		}
	}
}

func TestMidrankHandVector(t *testing.T) {
	got := Midrank([]float64{0.1, 0.3, 0.2, 0.3})
	// ranks 1, 3.5, 2, 3.5 -> (r-0.5)/4
	want := []float64{0.125, 0.75, 0.375, 0.75}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Errorf("Midrank[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMidrankInRange(t *testing.T) {
	got := Midrank([]float64{5, 1, 4, 2, 3, 6, 9, 8, 7, 0})
	for i, v := range got {
		if v <= 0 || v >= 1 {
			t.Errorf("Midrank[%d] = %v, want in (0,1)", i, v)
		}
	}
}

func TestZHandVector(t *testing.T) {
	got := Z([]float64{0.125, 0.375, 0.75, 0.75}, 0.05)
	// mean 0.5, population sd sqrt(0.0703125)
	sd := math.Sqrt(0.0703125)
	for i, p := range []float64{0.125, 0.375, 0.75, 0.75} {
		if math.Abs(got[i]-(p-0.5)/sd) > 1e-12 {
			t.Errorf("Z[%d] = %v, want %v", i, got[i], (p-0.5)/sd)
		}
	}
}

func TestZAllEqualZero(t *testing.T) {
	got := Z([]float64{0.5, 0.5, 0.5}, 0.05)
	for i, v := range got {
		if v != 0 {
			t.Errorf("Z[%d] = %v, want 0", i, v)
		}
	}
}
