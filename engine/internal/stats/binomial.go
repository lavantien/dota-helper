package stats

import "math"

// twoSidedTol is the relative slack on the point-probability bar so outcomes
// that tie the observed one under floating-point rounding still count.
const twoSidedTol = 1e-9

func binomialArgsValid(k, n int, p float64) bool {
	return n >= 0 && k >= 0 && k <= n && p >= 0 && p <= 1
}

// BinomialPMF returns P(X = k) for X ~ Binomial(n, p), evaluated in log space
// via lgamma so large n never multiplies tiny factorials directly. Invalid
// arguments (negative n or k, k > n, p outside [0,1]) yield NaN.
func BinomialPMF(k, n int, p float64) float64 {
	if !binomialArgsValid(k, n, p) {
		return math.NaN()
	}
	switch p {
	case 0:
		if k == 0 {
			return 1
		}
		return 0
	case 1:
		if k == n {
			return 1
		}
		return 0
	}
	lgn, _ := math.Lgamma(float64(n + 1))
	lgk, _ := math.Lgamma(float64(k + 1))
	lgnk, _ := math.Lgamma(float64(n - k + 1))
	logp := lgn - lgk - lgnk + float64(k)*math.Log(p) + float64(n-k)*math.Log1p(-p)
	return math.Exp(logp)
}

// BinomialOneSided returns the exact upper tail P(X >= k), the sum of pmf
// cells from k to n: ch39's three-cell tail (45+10+1)/1024 for 8 heads of
// ten.
func BinomialOneSided(k, n int, p float64) float64 {
	if !binomialArgsValid(k, n, p) {
		return math.NaN()
	}
	sum := 0.0
	for x := k; x <= n; x++ {
		sum += BinomialPMF(x, n, p)
	}
	return clampProb(sum)
}

// BinomialTwoSided returns the point-probability p: the sum of pmf(x) over
// every x at most as likely as the observed k, with a relative tolerance for
// ties: ch39's set {0,1,2,8,9,10} summing to 7/64 for 8 heads of ten.
func BinomialTwoSided(k, n int, p float64) float64 {
	if !binomialArgsValid(k, n, p) {
		return math.NaN()
	}
	bar := BinomialPMF(k, n, p)
	sum := 0.0
	for x := 0; x <= n; x++ {
		if px := BinomialPMF(x, n, p); px <= bar*(1+twoSidedTol) {
			sum += px
		}
	}
	return clampProb(sum)
}

// clampProb bounds a summed probability to [0,1]: the pmf sums are exact in
// principle, but floating-point accumulation can land a hair past 1 (observed
// 1 + 2e-15 at n=144), which downstream corrections like BH reject.
func clampProb(p float64) float64 {
	if p > 1 {
		return 1
	}
	if p < 0 {
		return 0
	}
	return p
}
