// Package analytics holds the pure math behind the pick engine: shrinkage,
// normalization, matrix completion, hero priors, and draft scoring.
package analytics

// Shrink blends an observed win rate toward the prior p0 with alpha pseudo
// counts: post = (w + a*p0) / (n + a). n == 0 means no data, so wins is
// garbage and the posterior is the prior.
func Shrink(wins, n, alpha, p0 float64) float64 {
	if n <= 0 {
		return p0
	}
	return (wins + alpha*p0) / (n + alpha)
}

// ShrinkDelta shrinks a win-rate delta d observed over n games.
func ShrinkDelta(d, n, alpha float64) float64 {
	return d * n / (n + alpha)
}
