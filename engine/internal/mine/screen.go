package mine

import (
	"poolguide/internal/config"
	"poolguide/internal/stats"
)

// ScreenCell is one raw pair cell offered to the significance screen: the
// hero whose row the cell belongs to (that hero's remaining cells form the
// test baseline), the cell's observed wins and matches, and whether win
// counts cover every match of the cell. A cell with partial coverage (some
// row served no win counts) carries no testable table and is never screened.
type ScreenCell struct {
	Hero     string
	Wins     int64
	N        int64
	WinCover bool
}

// EffectiveAlphas decides the shrinkage alpha of every cell in one family;
// the enemy matchup cells and the ally synergy cells are corrected
// separately, one call each. Every testable cell is tested for independence
// from its hero's remaining pool (chi-square, exact binomial fallback when
// an expected count falls under eval.screen.chiSqMinExpected), then stats.BH
// at eval.screen.q decides the family. Cells whose independence the family
// rejects keep alpha; cells it does not reject, and cells too thin to test
// (n under eval.screen.minMatches), take alpha*eval.screen.failAlphaMult so
// their shrunk delta pulls harder toward neutral. With the screen disabled
// every cell keeps alpha bit-for-bit.
func EffectiveAlphas(cells []ScreenCell, alpha float64, cfg *config.Config) []float64 {
	sc := cfg.Eval.Screen
	out := make([]float64, len(cells))
	if !sc.Enabled {
		for i := range out {
			out[i] = alpha
		}
		return out
	}
	fail := alpha * sc.FailAlphaMult
	poolW, poolN := map[string]int64{}, map[string]int64{}
	for _, c := range cells {
		if coverable(c) {
			poolW[c.Hero] += c.Wins
			poolN[c.Hero] += c.N
		}
	}
	var members []int
	ps := []float64{}
	for i, c := range cells {
		if c.N < int64(sc.MinMatches) {
			out[i] = fail
			continue
		}
		out[i] = alpha
		if !coverable(c) {
			continue
		}
		members = append(members, i)
		ps = append(ps, pairP(c.Wins, c.N, poolW[c.Hero], poolN[c.Hero], sc))
	}
	for j, keep := range stats.BH(ps, sc.Q) {
		if !keep {
			out[members[j]] = fail
		}
	}
	return out
}

// coverable reports whether a cell's counts can enter a test table: wins
// fully observed and inside [0, n]. Corrupt counts keep their plain alpha
// rather than poisoning the hero's baseline.
func coverable(c ScreenCell) bool {
	return c.WinCover && c.Wins >= 0 && c.Wins <= c.N
}

// pairP is the independence p-value of one cell's (w, n) against its hero's
// whole-family baseline (W, N): the chi-square of [[w, n-w], [W-w, rest]]
// when every expected count clears the configured floor, the exact
// two-sided binomial at the pool rate W/N otherwise. The caller guarantees
// 0 <= w <= n <= N and N > 0, and both routes land in [0,1] by construction
// (the binomial clamps its pmf sum at the source).
func pairP(w, n, W, N int64, sc config.EvalScreenCfg) float64 {
	a := float64(w)
	b := float64(n - w)
	c := float64(W - w)
	d := float64(N - n - (W - w))
	_, p, ok := stats.ChiSquare2x2(a, b, c, d)
	if !ok || minExpected2x2(a, b, c, d) < sc.ChiSqMinExpected {
		p = stats.BinomialTwoSided(int(w), int(n), float64(W)/float64(N))
	}
	return p
}

// minExpected2x2 is the smallest expected count of the independence table,
// the same closed form stats.ChiSquare2x2 gates its ok flag on, so raising
// eval.screen.chiSqMinExpected above the baked-in floor tightens the route.
func minExpected2x2(a, b, c, d float64) float64 {
	n := a + b + c + d
	min := (a + b) * (a + c) / n
	for _, m := range [3]float64{(a + b) * (b + d), (c + d) * (a + c), (c + d) * (b + d)} {
		if m/n < min {
			min = m / n
		}
	}
	return min
}
