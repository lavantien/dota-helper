package stats

import (
	"math"
	"math/rand/v2"
	"sort"
)

// PercentileCI bootstraps a percentile confidence interval for metric. It
// draws resamples replicates of n row indices with replacement (the ch17
// bootstrap, each replicate leaving roughly 1/e of the rows out of bag) from
// a PCG stream seeded (seed, seed), so identical calls are byte-identical,
// applies metric to each replicate, and returns the loPct and hiPct
// percentiles of the collected values. Percentiles are nearest-rank: the
// ceil(p/100 * m)-th smallest, clamped so p=0 is the minimum and p=100 the
// maximum. The idx slice handed to metric is reused between replicates and
// must not be retained. n < 1 or resamples < 1 leaves nothing to rank and
// returns NaN without calling metric.
func PercentileCI(metric func(idx []int) float64, n, resamples int, seed uint64, loPct, hiPct float64) (lo, hi float64) {
	if n < 1 || resamples < 1 {
		return math.NaN(), math.NaN()
	}
	rng := rand.New(rand.NewPCG(seed, seed))
	idx := make([]int, n)
	vals := make([]float64, resamples)
	for r := range vals {
		for j := range idx {
			idx[j] = rng.IntN(n)
		}
		vals[r] = metric(idx)
	}
	sort.Float64s(vals)
	return nearestRank(vals, loPct), nearestRank(vals, hiPct)
}

// nearestRank picks the ceil(pct% * m)-th smallest of ascending vals,
// clamped into [1, m]. pct*m runs before the /100 so a dyadic pct with an
// exact integer rank lands on it instead of an ulp above.
func nearestRank(sorted []float64, pct float64) float64 {
	rank := int(math.Ceil(pct * float64(len(sorted)) / 100))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
