package stats

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
)

// meanOf closes over data and means the rows idx selects; a bad index panics,
// so it doubles as a bounds check on the resampler.
func meanOf(data []float64) func(idx []int) float64 {
	return func(idx []int) float64 {
		sum := 0.0
		for _, i := range idx {
			sum += data[i]
		}
		return sum / float64(len(idx))
	}
}

// the mean's interval shrinks like 1/sqrt(n): 25 rows against 2000 rows from
// the same seeded uniform
func TestBootstrapCIShrinksAsNGrows(t *testing.T) {
	rng := rand.New(rand.NewPCG(21, 21))
	small := make([]float64, 25)
	for i := range small {
		small[i] = rng.Float64() * 10
	}
	large := make([]float64, 2000)
	for i := range large {
		large[i] = rng.Float64() * 10
	}
	loS, hiS := PercentileCI(meanOf(small), len(small), 400, 42, 2.5, 97.5)
	loL, hiL := PercentileCI(meanOf(large), len(large), 400, 42, 2.5, 97.5)
	if widthS, widthL := hiS-loS, hiL-loL; widthS <= widthL {
		t.Errorf("n=25 width %v not wider than n=2000 width %v", widthS, widthL)
	}
}

func TestBootstrapSameSeedIdentical(t *testing.T) {
	rng := rand.New(rand.NewPCG(33, 33))
	data := make([]float64, 80)
	for i := range data {
		data[i] = rng.Float64()
	}
	lo1, hi1 := PercentileCI(meanOf(data), len(data), 200, 7, 5, 95)
	lo2, hi2 := PercentileCI(meanOf(data), len(data), 200, 7, 5, 95)
	if lo1 != lo2 || hi1 != hi2 {
		t.Errorf("same seed differs: (%v,%v) vs (%v,%v)", lo1, hi1, lo2, hi2)
	}
	lo3, _ := PercentileCI(meanOf(data), len(data), 200, 8, 5, 95)
	if lo3 == lo1 {
		t.Error("different seed gave identical interval")
	}
}

func TestBootstrapMetricCalledExactlyResamplesTimes(t *testing.T) {
	calls := 0
	metric := func(idx []int) float64 {
		calls++
		return 0
	}
	PercentileCI(metric, 10, 137, 1, 2.5, 97.5)
	if calls != 137 {
		t.Errorf("metric called %d times, want 137", calls)
	}
}

func TestBootstrapConstantMetric(t *testing.T) {
	lo, hi := PercentileCI(func(idx []int) float64 { return 3.25 }, 30, 100, 5, 2.5, 97.5)
	if lo != 3.25 || hi != 3.25 {
		t.Errorf("constant metric gave (%v,%v), want (3.25,3.25)", lo, hi)
	}
}

// nearest-rank percentiles pinned on a recorded stream: p=0 the minimum,
// p=100 the maximum, p=50 with 10 resamples the 5th smallest (ceil(0.5*10))
func TestBootstrapNearestRankPercentiles(t *testing.T) {
	data := []float64{0, 1, 2, 3, 4}
	var seen []float64
	metric := func(idx []int) float64 {
		v := meanOf(data)(idx)
		seen = append(seen, v)
		return v
	}
	lo, hi := PercentileCI(metric, len(data), 10, 9, 0, 100)
	sorted := append([]float64(nil), seen...)
	sort.Float64s(sorted)
	if lo != sorted[0] {
		t.Errorf("p=0 gave %v, want min %v", lo, sorted[0])
	}
	if hi != sorted[len(sorted)-1] {
		t.Errorf("p=100 gave %v, want max %v", hi, sorted[len(sorted)-1])
	}
	med, _ := PercentileCI(metric, len(data), 10, 9, 50, 50)
	if med != sorted[4] {
		t.Errorf("p=50 gave %v, want 5th smallest %v", med, sorted[4])
	}
}

// exact-integer rank boundaries must not shift by an ulp: 2.5% of 400 is
// exactly 10, the 10th smallest, not the 11th
func TestBootstrapNearestRankExactBoundary(t *testing.T) {
	sorted := make([]float64, 400)
	for i := range sorted {
		sorted[i] = float64(i)
	}
	if got := nearestRank(sorted, 2.5); got != 9 {
		t.Errorf("nearestRank(2.5%% of 400) = %v, want 9 (10th smallest)", got)
	}
	if got := nearestRank(sorted, 97.5); got != 389 {
		t.Errorf("nearestRank(97.5%% of 400) = %v, want 389 (390th smallest)", got)
	}
}

// no rows or no resamples leaves nothing to rank: NaN, metric never called
func TestBootstrapDegenerateNaN(t *testing.T) {
	calls := 0
	metric := func(idx []int) float64 {
		calls++
		return 1
	}
	if lo, hi := PercentileCI(metric, 0, 100, 3, 2.5, 97.5); !math.IsNaN(lo) || !math.IsNaN(hi) {
		t.Errorf("n=0 gave (%v,%v), want NaN", lo, hi)
	}
	if lo, hi := PercentileCI(metric, 10, 0, 3, 2.5, 97.5); !math.IsNaN(lo) || !math.IsNaN(hi) {
		t.Errorf("resamples=0 gave (%v,%v), want NaN", lo, hi)
	}
	if calls != 0 {
		t.Errorf("metric called %d times on degenerate input, want 0", calls)
	}
}
