package stats

import "math"

// Spearman is the Pearson correlation of the average ranks of xs and ys,
// ties sharing the mean of their rank span, so monotone input gives exactly
// +-1 whatever the values. Length mismatch is a caller bug and panics.
// Constant input on either side has zero rank variance and returns NaN,
// like the degenerate cases of the math package.
func Spearman(xs, ys []float64) float64 {
	if len(xs) != len(ys) {
		panic("stats: Spearman xs and ys differ in length")
	}
	rx, ry := midranks(xs), midranks(ys)
	mx, my := 0.0, 0.0
	for i := range rx {
		mx += rx[i]
		my += ry[i]
	}
	n := float64(len(rx))
	mx /= n
	my /= n
	sxy, sxx, syy := 0.0, 0.0, 0.0
	for i := range rx {
		dx, dy := rx[i]-mx, ry[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	return sxy / math.Sqrt(sxx*syy)
}
