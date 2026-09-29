package stats

import (
	"math"
	"math/rand"
	"slices"
	"testing"
)

// chapter 40's list P in input order H1..H10 (unsorted on purpose, the
// chapter's own sort is H2,H6,H5,H4,H8,H3,H1,H9,H10,H7): H1=1/5 H2=1/1000
// H3=3/20 H4=7/400 H5=1/160 H6=9/2000 H7=9/10 H8=7/200 H9=1/4 H10=3/10.
var ch40P = []float64{
	1.0 / 5.0, 1.0 / 1000.0, 3.0 / 20.0, 7.0 / 400.0, 1.0 / 160.0,
	9.0 / 2000.0, 9.0 / 10.0, 7.0 / 200.0, 1.0 / 4.0, 3.0 / 10.0,
}

// chapter 40's list Q pins only H1=H2=1/80; the four fillers sit high
// enough to leave every documented verdict alone.
var ch40Q = []float64{1.0 / 80.0, 1.0 / 80.0, 0.5, 0.5, 0.5, 0.5}

func TestHolmChapterListP(t *testing.T) {
	// holm_reject={H2,H6,H5} n=3: k=3 is the boundary row, p=1/160 equals
	// the k=3 threshold alpha/(10-3+1)=1/160 and the rule's <= lets it
	// through; k=4 is the first miss (7/400 > 1/140) and the rest inherit
	// the stop.
	want := []bool{false, true, false, false, true, true, false, false, false, false}
	if got := Holm(ch40P, 1.0/20.0); !slices.Equal(got, want) {
		t.Errorf("Holm(list P) = %v, want %v", got, want)
	}
}

func TestBHChapterListP(t *testing.T) {
	// bh_kstar=4 reject={H2,H6,H5,H4} n=4: the pace holds at k=4
	// (7/400 <= 1/50) and breaks at k=5 (7/200 > 1/40).
	want := []bool{false, true, false, true, true, true, false, false, false, false}
	if got := BH(ch40P, 1.0/20.0); !slices.Equal(got, want) {
		t.Errorf("BH(list P) = %v, want %v", got, want)
	}
}

func TestChapterListQ(t *testing.T) {
	// bonferroni at 1/120 and holm's first step (same bar, alpha/m with
	// m=6) both reject nothing: p=1/80 > 1/120. bh does not care and keeps
	// k*=2 (1/80 <= 1/60).
	if got := Holm(ch40Q, 1.0/20.0); !slices.Equal(got, make([]bool, 6)) {
		t.Errorf("Holm(list Q) = %v, want all false", got)
	}
	want := []bool{true, true, false, false, false, false}
	if got := BH(ch40Q, 1.0/20.0); !slices.Equal(got, want) {
		t.Errorf("BH(list Q) = %v, want %v", got, want)
	}
}

func TestHolmFirstStepIsBonferroniBar(t *testing.T) {
	// the chapter's family-size demo: the same p=1/160 clears alpha/m at
	// m=5 (bar 1/100) and dies at m=50 (bar 1/1000). holm's k=1 threshold
	// is that bar, and with every p identical the verdict is unanimous.
	p := 1.0 / 160.0
	for _, c := range []struct {
		m    int
		want bool
	}{{5, true}, {50, false}} {
		family := make([]float64, c.m)
		for i := range family {
			family[i] = p
		}
		got := Holm(family, 1.0/20.0)
		unanimous := true
		for _, r := range got {
			unanimous = unanimous && r == c.want
		}
		if !unanimous {
			t.Errorf("m=%d: Holm(family of 1/160) = %v, want all %v", c.m, got, c.want)
		}
	}
}

func TestBHAllIdentical(t *testing.T) {
	// identical p-values share one verdict: the largest holding k is m
	// exactly when p <= q (the bar at k=m is q itself), and 0 otherwise.
	if got := BH([]float64{0.01, 0.01, 0.01, 0.01}, 0.05); !slices.Equal(got, []bool{true, true, true, true}) {
		t.Errorf("BH(4x 0.01) = %v, want all true", got)
	}
	if got := BH([]float64{0.9, 0.9, 0.9, 0.9}, 0.05); !slices.Equal(got, make([]bool, 4)) {
		t.Errorf("BH(4x 0.9) = %v, want all false", got)
	}
}

func TestLevelExtremes(t *testing.T) {
	// at level 1 every bar is above every p in [0,1], at level 0 no p>0
	// clears any bar.
	none := make([]bool, 10)
	all := make([]bool, 10)
	for i := range all {
		all[i] = true
	}
	for _, proc := range []struct {
		name string
		f    func([]float64, float64) []bool
	}{{"holm", Holm}, {"bh", BH}} {
		if got := proc.f(ch40P, 1); !slices.Equal(got, all) {
			t.Errorf("%s(list P, 1) = %v, want all true", proc.name, got)
		}
		if got := proc.f(ch40P, 0); !slices.Equal(got, none) {
			t.Errorf("%s(list P, 0) = %v, want all false", proc.name, got)
		}
	}
}

func TestEmptyInput(t *testing.T) {
	if got := Holm(nil, 0.05); len(got) != 0 {
		t.Errorf("Holm(nil) = %v, want empty", got)
	}
	if got := BH([]float64{}, 0.05); len(got) != 0 {
		t.Errorf("BH(empty) = %v, want empty", got)
	}
}

func TestInvalidInputPanics(t *testing.T) {
	for _, c := range []struct {
		ps    []float64
		level float64
	}{
		{[]float64{0.1, 1.5}, 0.05},
		{[]float64{-0.1}, 0.05},
		{[]float64{0.1, math.NaN()}, 0.05},
		{[]float64{0.1}, 1.5},
		{[]float64{0.1}, -0.1},
		{[]float64{0.1}, math.NaN()},
	} {
		for _, proc := range []struct {
			name string
			f    func([]float64, float64) []bool
		}{{"holm", Holm}, {"bh", BH}} {
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				proc.f(c.ps, c.level)
			}()
			if !panicked {
				t.Errorf("%s(%v, %v) did not panic", proc.name, c.ps, c.level)
			}
		}
	}
}

func TestMonotoneInP(t *testing.T) {
	// both procedures are monotone in p within a run: if p_i is rejected
	// and p_j <= p_i then p_j is rejected too.
	rng := rand.New(rand.NewSource(40))
	for run := 0; run < 200; run++ {
		ps := make([]float64, rng.Intn(20))
		for i := range ps {
			ps[i] = rng.Float64()
		}
		for _, got := range [][]bool{Holm(ps, 0.05), BH(ps, 0.05)} {
			for i := range ps {
				for j := range ps {
					if got[i] && ps[j] <= ps[i] && !got[j] {
						t.Fatalf("run %d: rejected p[%d]=%v but not p[%d]=%v", run, i, ps[i], j, ps[j])
					}
				}
			}
		}
	}
}

func TestVerdictsFollowInputPositions(t *testing.T) {
	// permuting the input permutes the output the same way.
	rng := rand.New(rand.NewSource(1995))
	for run := 0; run < 200; run++ {
		m := rng.Intn(20)
		ps := make([]float64, m)
		for i := range ps {
			ps[i] = rng.Float64()
		}
		perm := rng.Perm(m)
		shuffled := make([]float64, m)
		for i, j := range perm {
			shuffled[j] = ps[i]
		}
		for _, proc := range []struct {
			name string
			f    func([]float64, float64) []bool
		}{{"holm", Holm}, {"bh", BH}} {
			base := proc.f(ps, 0.05)
			shuf := proc.f(shuffled, 0.05)
			for i, j := range perm {
				if shuf[j] != base[i] {
					t.Fatalf("run %d %s: verdict for p[%d]=%v moved", run, proc.name, i, ps[i])
				}
			}
		}
	}
}
