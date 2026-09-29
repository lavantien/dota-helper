package stats

import (
	"fmt"
	"math"
	"sort"
)

// Holm runs the step-down multiple-test procedure of Holm (1979) at
// family-wise level alpha: sort the p-values ascending, compare p(k)
// against alpha/(m-k+1) from k=1 up, reject while p(k) <= threshold, and
// every hypothesis from the first miss on is not rejected. The returned
// slice preserves the input order. Panics if any p or the level falls
// outside [0,1].
func Holm(ps []float64, alpha float64) []bool {
	validateFamily(ps, alpha)
	m := len(ps)
	order := ascending(ps)
	reject := make([]bool, m)
	for i, k := 0, 1; i < m; i, k = i+1, k+1 {
		if ps[order[i]] > alpha/float64(m-k+1) {
			break
		}
		reject[order[i]] = true
	}
	return reject
}

// BH runs the Benjamini-Hochberg step-up of Benjamini and Hochberg (1995)
// at false discovery rate q: sort the p-values ascending, keep the largest
// k with p(k) <= (k/m)*q, reject the first k hypotheses. The returned
// slice preserves the input order. Panics if any p or the rate falls
// outside [0,1].
func BH(ps []float64, q float64) []bool {
	validateFamily(ps, q)
	m := len(ps)
	order := ascending(ps)
	reject := make([]bool, m)
	kstar := 0
	for i, k := 0, 1; i < m; i, k = i+1, k+1 {
		if ps[order[i]] <= float64(k)/float64(m)*q {
			kstar = k
		}
	}
	for i := 0; i < kstar; i++ {
		reject[order[i]] = true
	}
	return reject
}

// ascending returns the indices of ps sorted by p-value; ties are left in
// arbitrary order because both procedures give tied p-values the same
// verdict.
func ascending(ps []float64) []int {
	order := make([]int, len(ps))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return ps[order[a]] < ps[order[b]] })
	return order
}

// validateFamily rejects p-values and levels outside [0,1], NaN included:
// a p-value is a probability, and the caller has no error channel.
func validateFamily(ps []float64, level float64) {
	if math.IsNaN(level) || level < 0 || level > 1 {
		panic(fmt.Sprintf("stats: level %v outside [0,1]", level))
	}
	for i, p := range ps {
		if math.IsNaN(p) || p < 0 || p > 1 {
			panic(fmt.Sprintf("stats: p[%d]=%v outside [0,1]", i, p))
		}
	}
}
