// Package stats holds the pure evaluation statistics behind the backtest:
// ROC area, rank correlation, and resampled confidence intervals.
package stats

import "sort"

// AUC is the area under the ROC curve of scores against binary labels, the
// ch18 concordance fraction: the share of positive-negative pairs where the
// positive outscores the negative, a tie worth 1/2. Computed in O(n log n) as
// the Mann-Whitney rank statistic (midranks for ties, U = R+ - n1(n1+1)/2
// over n1*n0), not the O(n^2) pair loop, because backtest samples run to
// ~20k rows. An empty class leaves no cross-class pair, which ch18 calls
// undefined; the API returns the 0.5 chance line instead. Panics when the
// slices differ in length.
func AUC(scores []float64, labels []bool) float64 {
	if len(scores) != len(labels) {
		panic("stats: AUC scores and labels differ in length")
	}
	n1 := 0
	for _, pos := range labels {
		if pos {
			n1++
		}
	}
	n0 := len(labels) - n1
	if n1 == 0 || n0 == 0 {
		return 0.5
	}
	ranks := midranks(scores)
	rsum := 0.0
	for i, pos := range labels {
		if pos {
			rsum += ranks[i]
		}
	}
	u := rsum - float64(n1)*float64(n1+1)/2
	return u / float64(n1*n0)
}

// midranks returns 1-based ascending ranks with ties averaged.
func midranks(values []float64) []float64 {
	n := len(values)
	out := make([]float64, n)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return values[idx[a]] < values[idx[b]] })
	for start := 0; start < n; {
		end := start
		for end+1 < n && values[idx[end+1]] == values[idx[start]] {
			end++
		}
		// 1-based average rank of the tie group [start..end]
		avgRank := float64(start+end+2) / 2
		for i := start; i <= end; i++ {
			out[idx[i]] = avgRank
		}
		start = end + 1
	}
	return out
}
