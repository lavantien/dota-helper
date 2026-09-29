package eval

import (
	"math"
	"sort"

	"poolguide/internal/stats"
)

// CI is a percentile bootstrap interval over match resamples.
type CI struct {
	Lo float64 `json:"lo"`
	Hi float64 `json:"hi"`
}

// MeanPercentile averages the full-pool pick percentiles, skipping picks
// with no defined percentile (single-candidate fields, gated-out picks).
func MeanPercentile(picks []PickEval) float64 {
	sum, n := 0.0, 0
	for _, p := range picks {
		if !p.Ranked() {
			continue
		}
		sum += p.Percentile
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// AnyRoleHitRate is the share of picks whose hero placed in the top k of at
// least one of its role rankings. Small role fields cap the ceiling: a
// 3-hero role always hits top 3.
func AnyRoleHitRate(picks []PickEval, k int) float64 {
	if len(picks) == 0 {
		return 0
	}
	hits := 0
	for _, p := range picks {
		for _, rr := range p.RoleRank {
			if rr.Rank >= 1 && rr.Rank <= k {
				hits++
				break
			}
		}
	}
	return float64(hits) / float64(len(picks))
}

// DraftAUC is the concordance of the draft-level advantage against the
// radiant win label.
func DraftAUC(replays []MatchReplay) float64 {
	scores := make([]float64, len(replays))
	labels := make([]bool, len(replays))
	for i, r := range replays {
		scores[i] = r.Advantage
		labels[i] = r.RadiantWin
	}
	return stats.AUC(scores, labels)
}

// SplitMatches partitions (start_time, match_id)-ordered matches into the
// leading trainFrac fraction and the holdout rest. The boundary is the
// truncated int of frac*n, pinned by test.
func SplitMatches(matches []Match, trainFrac float64) (train, holdout []Match) {
	n := int(trainFrac * float64(len(matches)))
	if n < 0 {
		n = 0
	}
	if n > len(matches) {
		n = len(matches)
	}
	return matches[:n], matches[n:]
}

// CalibrationBin is one score-decile bin evaluated on the holdout.
type CalibrationBin struct {
	Index int     `json:"index"`
	N     int     `json:"n"`
	Rate  float64 `json:"rate"` // realized win rate of the picking side
}

// Calibration holds the train-derived score bins with holdout realized
// rates and the count of adjacent nonempty bins whose rate drops.
type Calibration struct {
	Edges      []float64        `json:"edges"`
	Bins       []CalibrationBin `json:"bins"`
	Violations int              `json:"violations"`
}

// Calibrate bins holdout pick scores against decile edges fitted on train
// pick scores. Bins are upper-edge inclusive (a score exactly on an edge
// lands in the bin below it); repeated edges (heavy ties) collapse so no
// zero-width bin survives.
func Calibrate(trainScores, holdScores []float64, holdWon []bool, bins int) Calibration {
	if bins < 2 || len(trainScores) == 0 {
		return Calibration{}
	}
	sorted := append([]float64(nil), trainScores...)
	sort.Float64s(sorted)
	var edges []float64
	for i := 1; i < bins; i++ {
		e := quantile(sorted, float64(i)/float64(bins))
		if len(edges) == 0 || e > edges[len(edges)-1] {
			edges = append(edges, e)
		}
	}
	cal := Calibration{Edges: edges, Bins: make([]CalibrationBin, len(edges)+1)}
	for i := range cal.Bins {
		cal.Bins[i].Index = i
	}
	for i, s := range holdScores {
		b := sort.SearchFloat64s(edges, s)
		cal.Bins[b].N++
		if holdWon[i] {
			cal.Bins[b].Rate++
		}
	}
	var prev = -1
	for i := range cal.Bins {
		if cal.Bins[i].N == 0 {
			continue
		}
		cal.Bins[i].Rate /= float64(cal.Bins[i].N)
		if prev >= 0 && cal.Bins[i].Rate < cal.Bins[prev].Rate {
			cal.Violations++
		}
		prev = i
	}
	return cal
}

// quantile is the nearest-rank percentile of ascending values, the same
// convention stats.PercentileCI applies to its bootstrap distribution.
func quantile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// MatchesCI bootstraps a metric over resamples of n matches: metric gets
// the resampled match indices and must read only those.
func MatchesCI(n, resamples int, seed uint64, metric func(idx []int) float64) CI {
	if n < 1 || resamples < 1 {
		return CI{}
	}
	lo, hi := stats.PercentileCI(metric, n, resamples, seed, 2.5, 97.5)
	return CI{Lo: lo, Hi: hi}
}
