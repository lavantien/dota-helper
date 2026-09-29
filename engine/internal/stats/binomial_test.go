package stats

import (
	"math"
	"testing"
)

// ch39 pins the fair-coin fixture: n=10, p=1/2, the Pascal row
// (1,10,45,120,210,252,210,120,45,10,1) over 1024.
var fairPascal = [11]int{1, 10, 45, 120, 210, 252, 210, 120, 45, 10, 1}

func TestBinomialPMFFairCoinFractions(t *testing.T) {
	for k, cnt := range fairPascal {
		want := float64(cnt) / 1024.0
		if got := BinomialPMF(k, 10, 0.5); math.Abs(got-want) > 1e-12 {
			t.Errorf("pmf(%d,10,1/2) = %v, want %d/1024 = %v", k, got, cnt, want)
		}
	}
}

func TestBinomialPMFSumsToOne(t *testing.T) {
	for _, tc := range []struct {
		n int
		p float64
	}{
		{10, 0.5}, {20, 0.3}, {7, 0.85}, {100, 0.99}, {0, 0.5}, {5, 0}, {5, 1},
	} {
		sum := 0.0
		for k := 0; k <= tc.n; k++ {
			sum += BinomialPMF(k, tc.n, tc.p)
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("sum pmf(k,%d,%v) = %v, want 1", tc.n, tc.p, sum)
		}
	}
}

func TestBinomialPMFInvalidAndDegenerate(t *testing.T) {
	for _, tc := range []struct {
		k, n int
		p    float64
	}{
		{0, -1, 0.5}, {-1, 10, 0.5}, {11, 10, 0.5}, {5, 10, -0.1}, {5, 10, 1.1},
	} {
		if got := BinomialPMF(tc.k, tc.n, tc.p); !math.IsNaN(got) {
			t.Errorf("pmf(%d,%d,%v) = %v, want NaN", tc.k, tc.n, tc.p, got)
		}
	}
	// degenerate p collapses X to a point mass at 0 or n
	if got := BinomialPMF(0, 5, 0); got != 1 {
		t.Errorf("pmf(0,5,0) = %v, want 1", got)
	}
	if got := BinomialPMF(1, 5, 0); got != 0 {
		t.Errorf("pmf(1,5,0) = %v, want 0", got)
	}
	if got := BinomialPMF(5, 5, 1); got != 1 {
		t.Errorf("pmf(5,5,1) = %v, want 1", got)
	}
	if got := BinomialPMF(0, 5, 1); got != 0 {
		t.Errorf("pmf(0,5,1) = %v, want 0", got)
	}
}

// ch39: pone_ge8 = 7/128, the exact tail (45+10+1)/1024.
func TestBinomialOneSidedChapter(t *testing.T) {
	if got, want := BinomialOneSided(8, 10, 0.5), 7.0/128.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("oneSided(8,10,1/2) = %v, want 7/128 = %v", got, want)
	}
	// k=0 counts every outcome; k=n is the single cell p^n
	if got := BinomialOneSided(0, 10, 0.5); math.Abs(got-1) > 1e-12 {
		t.Errorf("oneSided(0,10,1/2) = %v, want 1", got)
	}
	if got, want := BinomialOneSided(10, 10, 0.5), 1.0/1024.0; math.Abs(got-want) > 1e-15 {
		t.Errorf("oneSided(10,10,1/2) = %v, want 1/1024 = %v", got, want)
	}
	// the tail shrinks as k rises
	prev := BinomialOneSided(0, 10, 0.5)
	for k := 1; k <= 10; k++ {
		cur := BinomialOneSided(k, 10, 0.5)
		if cur >= prev {
			t.Errorf("oneSided not decreasing at k=%d: %v -> %v", k, prev, cur)
		}
		prev = cur
	}
}

// ch39 prints ptwo_8=7/64 and ptwo_9=11/512, and the per-k staircase
// (1/512, 11/512, 7/64, 11/32, 193/256, 1) for k = 0..5.
func TestBinomialTwoSidedChapter(t *testing.T) {
	want := []float64{1.0 / 512.0, 11.0 / 512.0, 7.0 / 64.0, 11.0 / 32.0, 193.0 / 256.0, 1.0}
	for k, w := range want {
		if got := BinomialTwoSided(k, 10, 0.5); math.Abs(got-w) > 1e-9 {
			t.Errorf("twoSided(%d,10,1/2) = %v, want %v", k, got, w)
		}
	}
	if got, w := BinomialTwoSided(9, 10, 0.5), 11.0/512.0; math.Abs(got-w) > 1e-9 {
		t.Errorf("twoSided(9,10,1/2) = %v, want 11/512 = %v", got, w)
	}
}

// k and 10-k share a two-sided p under a fair coin (ch39 diagram note).
func TestBinomialTwoSidedSymmetry(t *testing.T) {
	for k := 0; k <= 10; k++ {
		lo := BinomialTwoSided(k, 10, 0.5)
		hi := BinomialTwoSided(10-k, 10, 0.5)
		if math.Abs(lo-hi) > 1e-12 {
			t.Errorf("twoSided(%d) != twoSided(%d): %v vs %v", k, 10-k, lo, hi)
		}
	}
}

// biased coin, n=2, p=0.3: pmf = (0.49, 0.42, 0.09) by hand
func TestBinomialTailsBiasedCoin(t *testing.T) {
	if got, want := BinomialOneSided(1, 2, 0.3), 0.51; math.Abs(got-want) > 1e-9 {
		t.Errorf("oneSided(1,2,0.3) = %v, want %v", got, want)
	}
	if got, want := BinomialOneSided(2, 2, 0.3), 0.09; math.Abs(got-want) > 1e-9 {
		t.Errorf("oneSided(2,2,0.3) = %v, want %v", got, want)
	}
	// bar 0.42 keeps cells 1 and 2; bar 0.09 keeps only cell 2
	if got, want := BinomialTwoSided(1, 2, 0.3), 0.51; math.Abs(got-want) > 1e-9 {
		t.Errorf("twoSided(1,2,0.3) = %v, want %v", got, want)
	}
	if got, want := BinomialTwoSided(2, 2, 0.3), 0.09; math.Abs(got-want) > 1e-9 {
		t.Errorf("twoSided(2,2,0.3) = %v, want %v", got, want)
	}
}

// pmf sums are probabilities: accumulation error must never push either
// tail past 1 (the T1 mine screen hit 1.0000000000000429 on real pair counts,
// which BH rejects). Zero tolerance: any excess is a bug.
func TestBinomialTailsWithinUnitInterval(t *testing.T) {
	ns := []int{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233}
	ps := []float64{0.5, 0.3, 0.85}
	for j := 2; j <= 12; j++ {
		for i := 1; i < j; i++ {
			ps = append(ps, float64(i)/float64(j))
		}
	}
	for _, n := range ns {
		for k := 0; k <= n; k++ {
			for _, p := range ps {
				if got := BinomialOneSided(k, n, p); got > 1 || got < 0 {
					t.Errorf("oneSided(%d,%d,%v) = %v, outside [0,1]", k, n, p, got)
				}
				if got := BinomialTwoSided(k, n, p); got > 1 || got < 0 {
					t.Errorf("twoSided(%d,%d,%v) = %v, outside [0,1]", k, n, p, got)
				}
			}
		}
	}
}

func TestBinomialTailsInvalid(t *testing.T) {
	for _, tc := range []struct {
		k, n int
		p    float64
	}{
		{0, -1, 0.5}, {-1, 10, 0.5}, {11, 10, 0.5}, {5, 10, -0.1}, {5, 10, 1.1},
	} {
		if got := BinomialOneSided(tc.k, tc.n, tc.p); !math.IsNaN(got) {
			t.Errorf("oneSided(%d,%d,%v) = %v, want NaN", tc.k, tc.n, tc.p, got)
		}
		if got := BinomialTwoSided(tc.k, tc.n, tc.p); !math.IsNaN(got) {
			t.Errorf("twoSided(%d,%d,%v) = %v, want NaN", tc.k, tc.n, tc.p, got)
		}
	}
	// point-mass nulls: the observed k carries all the mass or none of it.
	// At p=0 the cells tying k>=1 at pmf 0 sum to 0, not 1: an outcome
	// impossible under the null has point-probability p 0.
	if got := BinomialTwoSided(0, 5, 0); math.Abs(got-1) > 1e-12 {
		t.Errorf("twoSided(0,5,0) = %v, want 1", got)
	}
	if got := BinomialTwoSided(2, 5, 0); got != 0 {
		t.Errorf("twoSided(2,5,0) = %v, want 0", got)
	}
	if got := BinomialTwoSided(5, 5, 1); math.Abs(got-1) > 1e-12 {
		t.Errorf("twoSided(5,5,1) = %v, want 1", got)
	}
	if got := BinomialTwoSided(4, 5, 1); got != 0 {
		t.Errorf("twoSided(4,5,1) = %v, want 0", got)
	}
}
