package analytics

import (
	"math"
	"sort"
)

// Midrank maps values to percentiles in (0, 1): ascending ranks with ties
// averaged, scaled as (rank - 0.5) / n. All-equal input yields 0.5 for every
// entry.
func Midrank(values []float64) []float64 {
	n := len(values)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
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
		pct := (avgRank - 0.5) / float64(n)
		for i := start; i <= end; i++ {
			out[idx[i]] = pct
		}
		start = end + 1
	}
	return out
}

// Z standardizes pcts to zero mean, unit population sd, with the sd floored
// (config normalize.zSdFloor) so constant input maps to zeros.
func Z(pcts []float64, zSdFloor float64) []float64 {
	n := len(pcts)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	mean := 0.0
	for _, p := range pcts {
		mean += p
	}
	mean /= float64(n)
	variance := 0.0
	for _, p := range pcts {
		d := p - mean
		variance += d * d
	}
	variance /= float64(n)
	sd := math.Sqrt(variance)
	if sd < zSdFloor {
		sd = zSdFloor
	}
	for i, p := range pcts {
		out[i] = (p - mean) / sd
	}
	return out
}
