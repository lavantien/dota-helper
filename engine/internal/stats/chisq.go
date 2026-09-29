package stats

import "math"

// minExpected is the classic guidance floor from ch29: trust the chi-square
// approximation only when every expected count reaches 5.
const minExpected = 5.0

// ChiSquare2x2 runs the plain (no Yates) chi-square test of independence on
// the row-major table [[a, b], [c, d]]. Expected counts come from the row and
// column marginals, and the df=1 upper tail is erfc(sqrt(stat/2)), so the
// NIST critical 3.8415 maps to p 0.05. ok reports whether every expected
// count reaches minExpected; callers fall back to the exact binomial test
// when it is false. A zero-total, negative-count, or structurally-zero table
// has no defined statistic and returns NaN with ok false.
func ChiSquare2x2(a, b, c, d float64) (stat, p float64, ok bool) {
	n := a + b + c + d
	if a < 0 || b < 0 || c < 0 || d < 0 || n == 0 {
		return math.NaN(), math.NaN(), false
	}
	ab, cd := a+b, c+d
	ac, bd := a+c, b+d
	obs := [4]float64{a, b, c, d}
	exp := [4]float64{ab * ac / n, ab * bd / n, cd * ac / n, cd * bd / n}
	stat = 0.0
	ok = true
	for i := range obs {
		dev := obs[i] - exp[i]
		stat += dev * dev / exp[i]
		if exp[i] < minExpected {
			ok = false
		}
	}
	return stat, math.Erfc(math.Sqrt(stat / 2)), ok
}
