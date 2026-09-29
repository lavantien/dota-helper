package stats

import (
	"math"
	"testing"
)

// ch29: observed 5,1,3,3 against expected 4,2,4,2, contributions
// 1/4+1/2+1/4+1/2, chi2 exactly 3/2. Every expected count sits under 5, so ok
// is false and callers fall back to the exact binomial test: the chapter's
// own caveat on the 12-basket table.
func TestChiSquare2x2BookTable(t *testing.T) {
	stat, p, ok := ChiSquare2x2(5, 1, 3, 3)
	if math.Abs(stat-1.5) > 1e-12 {
		t.Errorf("stat = %v, want 3/2", stat)
	}
	if ok {
		t.Errorf("ok = true, want false (expected counts 4,2,4,2 all under 5)")
	}
	// 1.5 sits well under the 3.8415 critical value: independence stands
	if p <= 0.05 {
		t.Errorf("p = %v, want > 0.05", p)
	}
	if p < 0.2 || p > 0.24 {
		t.Errorf("p = %v, want ~0.2207", p)
	}
}

// ch29's D2 literal is the NIST df=1 alpha=0.05 upper-tail critical 3.8415.
// The margins-8 table (4+z, 4-z, 4-z, 4+z) puts every expected count at 4
// and the statistic at z^2, so p lands on 2*Q(z).
func TestChiSquare2x2CriticalValue(t *testing.T) {
	z := 1.9599639845400545 // z_{0.975}: z^2 = 3.8414588... rounds to 3.8415
	stat, p, ok := ChiSquare2x2(4+z, 4-z, 4-z, 4+z)
	if math.Abs(stat-3.8415) > 1e-3 {
		t.Errorf("stat = %v, want ~3.8415 (NIST df=1 alpha=0.05)", stat)
	}
	if math.Abs(p-0.05) > 1e-9 {
		t.Errorf("p = %v, want 0.05", p)
	}
	if ok {
		t.Errorf("ok = true, want false (expected counts 4 under 5)")
	}
	// second wiring point: z_{0.90} puts p at 0.2
	z90 := 1.2815515655446004
	_, p90, _ := ChiSquare2x2(4+z90, 4-z90, 4-z90, 4+z90)
	if math.Abs(p90-0.2) > 1e-9 {
		t.Errorf("p = %v, want 0.2", p90)
	}
}

// perfect association with every expected count at 5: stat 20, p ~ 0
func TestChiSquare2x2PerfectAssociation(t *testing.T) {
	stat, p, ok := ChiSquare2x2(10, 0, 0, 10)
	if math.Abs(stat-20) > 1e-9 {
		t.Errorf("stat = %v, want 20", stat)
	}
	if p > 1e-4 {
		t.Errorf("p = %v, want ~0", p)
	}
	if !ok {
		t.Errorf("ok = false, want true (expected counts all 5)")
	}
	// the same perfect association on half the counts flips ok and only ok:
	// expected 2.5 per cell
	stat2, _, ok2 := ChiSquare2x2(5, 0, 0, 5)
	if math.Abs(stat2-10) > 1e-9 {
		t.Errorf("stat = %v, want 10", stat2)
	}
	if ok2 {
		t.Errorf("ok = true, want false (expected counts 2.5)")
	}
}

func TestChiSquare2x2Independence(t *testing.T) {
	stat, p, ok := ChiSquare2x2(50, 50, 50, 50)
	if stat != 0 {
		t.Errorf("stat = %v, want 0", stat)
	}
	if p != 1 {
		t.Errorf("p = %v, want 1", p)
	}
	if !ok {
		t.Errorf("ok = false, want true (expected counts 25)")
	}
}

// swapping rows or columns leaves the statistic, p, and ok untouched
func TestChiSquare2x2SwapInvariant(t *testing.T) {
	stat, p, ok := ChiSquare2x2(5, 1, 3, 3)
	for _, tbl := range [][4]float64{
		{3, 3, 5, 1}, // rows swapped
		{1, 5, 3, 3}, // columns swapped
	} {
		s2, p2, ok2 := ChiSquare2x2(tbl[0], tbl[1], tbl[2], tbl[3])
		if math.Abs(s2-stat) > 1e-12 || math.Abs(p2-p) > 1e-12 || ok2 != ok {
			t.Errorf("swap changed the verdict: (%v,%v,%v) vs (%v,%v,%v)",
				s2, p2, ok2, stat, p, ok)
		}
	}
}

// hand-worked asymmetric table: N=40, margins 20/20 and 18/22, expected
// 9,11,9,11, deviations 3,-3,-3,3, stat 2 + 18/11 = 40/11
func TestChiSquare2x2HandTable(t *testing.T) {
	stat, p, ok := ChiSquare2x2(12, 8, 6, 14)
	if math.Abs(stat-40.0/11.0) > 1e-9 {
		t.Errorf("stat = %v, want 40/11", stat)
	}
	if !ok {
		t.Errorf("ok = false, want true (expected counts 9 and 11)")
	}
	if p <= 0.05 {
		t.Errorf("p = %v, want > 0.05 (40/11 under the 3.8415 critical)", p)
	}
}

func TestChiSquare2x2Degenerate(t *testing.T) {
	for _, tbl := range [][4]float64{
		{0, 0, 0, 0},   // no data
		{5, -1, 3, 3},  // negative count
		{0, 10, 0, 10}, // structural zero column: the 0/0 cells are undefined
	} {
		stat, p, ok := ChiSquare2x2(tbl[0], tbl[1], tbl[2], tbl[3])
		if !math.IsNaN(stat) || !math.IsNaN(p) || ok {
			t.Errorf("ChiSquare2x2(%v) = (%v,%v,%v), want (NaN,NaN,false)", tbl, stat, p, ok)
		}
	}
}
