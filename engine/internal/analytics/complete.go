package analytics

import (
	"math"
	"math/rand"

	"poolguide/internal/config"
)

// Matrix pairs a values grid with its observation mask.
type Matrix struct {
	Values   [][]float64
	Observed [][]bool
}

// Complete fills unobserved cells with a low-rank factorization trained by
// alternating least squares. Observed cells pass through unchanged, the
// output always deep-copies the input, and the run is fully deterministic:
// seeded init, fixed row-major order.
func Complete(m Matrix, cfg config.CompletionCfg) Matrix {
	rows := len(m.Values)
	cols := 0
	if rows > 0 {
		cols = len(m.Values[0])
	}
	r := cfg.Rank
	if r > rows {
		r = rows
	}
	if r > cols {
		r = cols
	}
	if r < 1 {
		r = 1
	}

	out := Matrix{
		Values:   make([][]float64, rows),
		Observed: make([][]bool, rows),
	}
	for i := 0; i < rows; i++ {
		out.Values[i] = append([]float64(nil), m.Values[i]...)
		out.Observed[i] = append([]bool(nil), m.Observed[i]...)
	}
	if rows == 0 || cols == 0 {
		return out
	}

	rng := rand.New(rand.NewSource(cfg.Seed))
	u := make([]float64, rows*r)
	v := make([]float64, cols*r)
	for i := range u {
		u[i] = rng.Float64()
	}
	for i := range v {
		v[i] = rng.Float64()
	}

	atA := make([]float64, r*r)
	b := make([]float64, r)
	rowObs := func(i, j int) bool { return m.Observed[i][j] }
	rowVal := func(i, j int) float64 { return m.Values[i][j] }
	colObs := func(i, j int) bool { return m.Observed[j][i] }
	colVal := func(i, j int) float64 { return m.Values[j][i] }

	for iter := 0; iter < cfg.MaxIter; iter++ {
		prevU := append([]float64(nil), u...)
		prevV := append([]float64(nil), v...)
		lsq(u, rows, cols, r, cfg.Lambda, cfg.PivotFloor, v, rowObs, rowVal, atA, b)
		lsq(v, cols, rows, r, cfg.Lambda, cfg.PivotFloor, u, colObs, colVal, atA, b)
		delta := maxAbsDiff(prevU, u)
		if d := maxAbsDiff(prevV, v); d > delta {
			delta = d
		}
		if delta < cfg.Tol {
			break
		}
	}

	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if !m.Observed[i][j] {
				out.Values[i][j] = dot(u[i*r:(i+1)*r], v[j*r:(j+1)*r])
			}
		}
	}
	return out
}

// lsq ridge-solves one factor side: for each of the count factors, minimize
// over observed cells against the current other-side factors. A factor with no
// observed cells keeps its previous estimate. atA and b are caller-owned
// scratch buffers reused across factors; solve destroys them.
func lsq(target []float64, count, otherCount, r int, lambda, pivotFloor float64, other []float64,
	obs func(i, j int) bool, val func(i, j int) float64, atA []float64, b []float64) {
	for i := 0; i < count; i++ {
		clear(atA)
		clear(b)
		has := false
		for j := 0; j < otherCount; j++ {
			if !obs(i, j) {
				continue
			}
			has = true
			ov := other[j*r : (j+1)*r]
			val := val(i, j)
			for p := 0; p < r; p++ {
				for q := 0; q < r; q++ {
					atA[p*r+q] += ov[p] * ov[q]
				}
				b[p] += val * ov[p]
			}
		}
		if !has {
			continue
		}
		for p := 0; p < r; p++ {
			atA[p*r+p] += lambda
		}
		if sol, ok := solve(atA, b, r, pivotFloor); ok {
			copy(target[i*r:(i+1)*r], sol)
		}
	}
}

// solve Gaussian-eliminates the n*n row-major system a*x=b with partial
// pivoting, in place. ok is false when a pivot underflows the singular guard.
func solve(a []float64, x []float64, n int, pivotFloor float64) ([]float64, bool) {
	for col := 0; col < n; col++ {
		best := col
		for row := col + 1; row < n; row++ {
			if math.Abs(a[row*n+col]) > math.Abs(a[best*n+col]) {
				best = row
			}
		}
		if math.Abs(a[best*n+col]) < pivotFloor {
			return nil, false
		}
		if best != col {
			for j := 0; j < n; j++ {
				a[col*n+j], a[best*n+j] = a[best*n+j], a[col*n+j]
			}
			x[col], x[best] = x[best], x[col]
		}
		piv := a[col*n+col]
		for row := col + 1; row < n; row++ {
			f := a[row*n+col] / piv
			if f == 0 {
				continue
			}
			for j := col; j < n; j++ {
				a[row*n+j] -= f * a[col*n+j]
			}
			x[row] -= f * x[col]
		}
	}
	for col := n - 1; col >= 0; col-- {
		s := x[col]
		for j := col + 1; j < n; j++ {
			s -= a[col*n+j] * x[j]
		}
		x[col] = s / a[col*n+col]
	}
	return x, true
}

func dot(a, b []float64) float64 {
	s := 0.0
	for k := range a {
		s += a[k] * b[k]
	}
	return s
}

func maxAbsDiff(a, b []float64) float64 {
	m := 0.0
	for i := range a {
		if d := math.Abs(a[i] - b[i]); d > m {
			m = d
		}
	}
	return m
}
