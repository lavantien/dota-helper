package analytics

import (
	"math"
	"sort"

	"poolguide/internal/config"
)

// Candidate is one roster hero scored against a draft state. Mu and Syn are
// keyed by roster index and default to 0 for missing entries. There is no tier
// field on purpose: the tier label never enters scoring.
type Candidate struct {
	Idx   int
	Role  string
	Prior float64

	// GateDelta comes from the gates package: 0 when no bonus or penalty rule
	// fired. Hard gates exclude candidates before scoring.
	GateDelta float64

	Mu  map[int]float64
	Syn map[int]float64
}

// Draft is the draft state the candidate is scored in.
type Draft struct {
	RosterSize     int
	Pop            map[int]float64
	VisibleEnemies []int
	Allies         []int
	Taken          []int
}

// Term is the full scoring breakdown for one candidate. Exposure holds the
// unsigned raw value; the negative sign only enters Score.
type Term struct {
	Idx  int
	Role string

	Score       float64
	KnownMu     float64
	KnownSyn    float64
	Prior       float64
	GenericFit  float64
	Exposure    float64
	Flexibility float64
	GateDelta   float64
}

// Score runs the expectimax-lite approximation. The accumulation order below
// is load-bearing: a JavaScript twin sums the terms in exactly this order for
// bit-for-bit parity. Structural constants come from config.score.
func Score(c Candidate, d Draft, w config.Weights, sc config.ScoreConsts) Term {
	t := Term{Idx: c.Idx, Role: c.Role, Prior: c.Prior, GateDelta: c.GateDelta}

	taken := make(map[int]bool, len(d.Taken))
	for _, x := range d.Taken {
		taken[x] = true
	}
	visible := make(map[int]bool, len(d.VisibleEnemies))
	for _, e := range d.VisibleEnemies {
		visible[e] = true
	}

	k := float64(len(d.VisibleEnemies))
	a := float64(len(d.Allies))
	if k > 0 {
		sum := 0.0
		for _, e := range d.VisibleEnemies {
			sum += c.Mu[e]
		}
		t.KnownMu = sum / k
	}
	if a > 0 {
		sum := 0.0
		for _, ally := range d.Allies {
			sum += c.Syn[ally]
		}
		t.KnownSyn = sum / a
	}

	// Unseen roster indices in ascending order: not yet taken, not the
	// candidate. The enemy side additionally drops the already-visible picks.
	var unseenAllies, unseenEnemies []int
	for i := 0; i < d.RosterSize; i++ {
		if taken[i] || i == c.Idx {
			continue
		}
		unseenAllies = append(unseenAllies, i)
		if !visible[i] {
			unseenEnemies = append(unseenEnemies, i)
		}
	}

	t.GenericFit = popMean(unseenAllies, d.Pop, func(i int) float64 { return c.Syn[i] })
	t.Exposure = popMean(unseenEnemies, d.Pop, func(i int) float64 {
		gap := sc.MidPct - c.Mu[i]
		if gap < 0 {
			gap = 0
		}
		return gap
	})
	flexVals := make([]float64, 0, len(unseenEnemies))
	for _, i := range flexTop(unseenEnemies, d.Pop, sc.FlexCap) {
		flexVals = append(flexVals, c.Mu[i])
	}
	t.Flexibility = -stdev(flexVals) / sc.FlexHalf

	synW, ok := w.SynByRole[c.Role]
	if !ok {
		// config validation pins every role id into synByRole, so a miss is
		// programmer error, same corruption class as the js twin's fail-loud
		panic("analytics.Score: weights.synByRole has no role " + c.Role)
	}

	s := 0.0
	s += w.KnownMu * (k / sc.EnemySlots) * t.KnownMu
	s += synW * (a / sc.EnemySlots) * t.KnownSyn
	s += w.Prior * t.Prior
	s += w.GenericFit * ((sc.AllySlots - a) / sc.AllySlots) * t.GenericFit
	s -= w.Exposure * ((sc.EnemySlots - k) / sc.EnemySlots) * t.Exposure
	s += w.Flexibility * t.Flexibility
	s += t.GateDelta
	t.Score = s
	return t
}

// Rank scores every candidate and orders by score descending, ties broken by
// ascending roster index.
func Rank(cands []Candidate, d Draft, w config.Weights, sc config.ScoreConsts) []Term {
	terms := make([]Term, len(cands))
	for i, c := range cands {
		terms[i] = Score(c, d, w, sc)
	}
	sort.Slice(terms, func(x, y int) bool {
		if terms[x].Score != terms[y].Score {
			return terms[x].Score > terms[y].Score
		}
		return terms[x].Idx < terms[y].Idx
	})
	return terms
}

// popMean is the popularity-weighted mean of v over idxs; when every weight
// is zero it falls back to the plain mean, which is 0 for an empty set.
func popMean(idxs []int, pop map[int]float64, v func(int) float64) float64 {
	if len(idxs) == 0 {
		return 0
	}
	sumW, sumWV := 0.0, 0.0
	for _, i := range idxs {
		w := pop[i]
		sumW += w
		sumWV += w * v(i)
	}
	if sumW == 0 {
		sum := 0.0
		for _, i := range idxs {
			sum += v(i)
		}
		return sum / float64(len(idxs))
	}
	return sumWV / sumW
}

// flexTop keeps the flexCap most popular indices, ties broken by ascending
// roster index.
func flexTop(idxs []int, pop map[int]float64, flexCap int) []int {
	sorted := append([]int(nil), idxs...)
	sort.Slice(sorted, func(x, y int) bool {
		if pop[sorted[x]] != pop[sorted[y]] {
			return pop[sorted[x]] > pop[sorted[y]]
		}
		return sorted[x] < sorted[y]
	})
	if len(sorted) > flexCap {
		sorted = sorted[:flexCap]
	}
	return sorted
}

// stdev is the population standard deviation; 0 for fewer than 2 values.
func stdev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	variance := 0.0
	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	return math.Sqrt(variance / float64(len(xs)))
}
