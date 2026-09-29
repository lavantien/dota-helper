package analytics

import (
	"math"
	"testing"

	"poolguide/internal/config"
)

func allTrue(rows, cols int) [][]bool {
	m := make([][]bool, rows)
	for i := range m {
		m[i] = make([]bool, cols)
		for j := range m[i] {
			m[i][j] = true
		}
	}
	return m
}

func copy2(src [][]float64) [][]float64 {
	out := make([][]float64, len(src))
	for i, row := range src {
		out[i] = append([]float64(nil), row...)
	}
	return out
}

func rmse(cells [][2]int, got, want [][]float64) float64 {
	sum := 0.0
	for _, c := range cells {
		d := got[c[0]][c[1]] - want[c[0]][c[1]]
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(cells)))
}

func rank2Fixture() (Matrix, [][]float64, [][2]int, config.CompletionCfg) {
	truth := [][]float64{
		{2, 1, 0.5, 3, 1},
		{3, 6.5, 4.25, 2.5, 2.5},
		{7, 6, 3.5, 9.5, 4},
		{4.5, 3.5, 2, 6.25, 2.5},
	}
	mask := [][2]int{{0, 1}, {1, 3}, {2, 0}, {3, 4}}
	m := Matrix{Values: copy2(truth), Observed: allTrue(4, 5)}
	for _, c := range mask {
		m.Values[c[0]][c[1]] = 0
		m.Observed[c[0]][c[1]] = false
	}
	cfg := config.CompletionCfg{Rank: 2, Lambda: 0.1, MaxIter: 200, Tol: 1e-9, Seed: 42, PivotFloor: 1e-12}
	return m, truth, mask, cfg
}

func TestCompleteFullyObservedUnchanged(t *testing.T) {
	m := Matrix{Values: [][]float64{{1, 2}, {3, 4}}, Observed: allTrue(2, 2)}
	out := Complete(m, config.CompletionCfg{Rank: 1, Lambda: 0.1, MaxIter: 10, Tol: 1e-9, Seed: 7, PivotFloor: 1e-12})
	for i := range m.Values {
		for j := range m.Values[i] {
			if out.Values[i][j] != m.Values[i][j] {
				t.Errorf("observed cell [%d][%d] changed: %v -> %v", i, j, m.Values[i][j], out.Values[i][j])
			}
		}
	}
}

func TestCompleteBeatsColumnMeanBaseline(t *testing.T) {
	m, truth, mask, cfg := rank2Fixture()
	out := Complete(m, cfg)
	if got := rmse(mask, out.Values, truth); got >= columnMeanRMSE(m, truth, mask) {
		t.Errorf("ALS rmse %v not better than column-mean baseline %v", got, columnMeanRMSE(m, truth, mask))
	}
	for i := range m.Values {
		for j := range m.Values[i] {
			if m.Observed[i][j] && out.Values[i][j] != truth[i][j] {
				t.Errorf("observed cell [%d][%d] changed", i, j)
			}
		}
	}
}

// columnMeanRMSE is the strawman baseline: fill masked cells with the mean of
// the observed entries of their column.
func columnMeanRMSE(m Matrix, truth [][]float64, mask [][2]int) float64 {
	filled := copy2(m.Values)
	for j := 0; j < len(m.Values[0]); j++ {
		sum, n := 0.0, 0
		for i := range m.Values {
			if m.Observed[i][j] {
				sum += m.Values[i][j]
				n++
			}
		}
		if n == 0 {
			continue
		}
		for _, c := range mask {
			if c[1] == j {
				filled[c[0]][c[1]] = sum / float64(n)
			}
		}
	}
	return rmse(mask, filled, truth)
}

func TestCompleteDeterministic(t *testing.T) {
	m, _, _, cfg := rank2Fixture()
	a := Complete(m, cfg)
	b := Complete(m, cfg)
	for i := range a.Values {
		for j := range a.Values[i] {
			if a.Values[i][j] != b.Values[i][j] {
				t.Fatalf("nondeterministic at [%d][%d]: %v vs %v", i, j, a.Values[i][j], b.Values[i][j])
			}
		}
	}
}

func TestCompleteRank1ExactRecovery(t *testing.T) {
	truth := [][]float64{
		{0.5, -1, 2, 4},
		{1, -2, 4, 8},
		{1.5, -3, 6, 12},
	}
	mask := [][2]int{{0, 0}, {2, 3}}
	m := Matrix{Values: copy2(truth), Observed: allTrue(3, 4)}
	for _, c := range mask {
		m.Values[c[0]][c[1]] = 0
		m.Observed[c[0]][c[1]] = false
	}
	// lambda must be 0 here: L2 regularization biases exact recovery.
	cfg := config.CompletionCfg{Rank: 1, Lambda: 0, MaxIter: 1000, Tol: 1e-12, Seed: 42, PivotFloor: 1e-12}
	out := Complete(m, cfg)
	for _, c := range mask {
		if math.Abs(out.Values[c[0]][c[1]]-truth[c[0]][c[1]]) > 1e-6 {
			t.Errorf("cell [%d][%d] = %v, want %v", c[0], c[1], out.Values[c[0]][c[1]], truth[c[0]][c[1]])
		}
	}
}
