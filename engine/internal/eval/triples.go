package eval

import (
	"encoding/json"
	"math"
	"math/bits"
	"sort"
)

// Frequent ally triples over the draft corpus, mined with the vertical
// apriori layout: one TID bitmap per itemset, intersection by wordwise
// AND, cardinality by popcount (KDD ch26). The event unit is the side
// composition: every train draft contributes its radiant and dire pool
// picks as two transactions, and exactly one of the two is a win, so the
// baseline win rate over side events is 1/2 by construction and lift reads
// as confidence against a fair coin. Each surviving triple is scored as
// the rule triple => side win with confidence, lift, and conviction
// (ch29), conviction reading +Inf under the perfect implication conf = 1.

const tidWordBits = 64

// tidSet is the TID bitmap of one itemset: bit e set when side event e
// contains the itemset.
type tidSet []uint64

func newTidSet(events int) tidSet { return make(tidSet, (events+tidWordBits-1)/tidWordBits) }

func (t tidSet) set(e int) { t[e/tidWordBits] |= 1 << (uint(e) % tidWordBits) }

// and returns the intersection bitmap of a and b.
func (a tidSet) and(b tidSet) tidSet {
	out := make(tidSet, len(a))
	for i, w := range a {
		out[i] = w & b[i]
	}
	return out
}

// count is the bitmap cardinality, the itemset's raw support.
func (a tidSet) count() int {
	n := 0
	for _, w := range a {
		n += bits.OnesCount64(w)
	}
	return n
}

// PairStat is one frequent ally pair.
type PairStat struct {
	Heroes [2]int    `json:"heroes"`
	Slugs  [2]string `json:"slugs"`
	N      int       `json:"n"`
}

// TripleStat is one frequent ally triple with its win-consequent rule
// scores. Conviction is +Inf when confidence is exactly 1 and marshals as
// JSON null.
type TripleStat struct {
	Heroes     [3]int    `json:"heroes"`
	Slugs      [3]string `json:"slugs"`
	N          int       `json:"n"`
	Wins       int       `json:"wins"`
	Support    float64   `json:"support"`
	Confidence float64   `json:"confidence"`
	Lift       float64   `json:"lift"`
	Conviction float64   `json:"conviction"`
}

// MarshalJSON renders an infinite conviction as null so the mined set
// stays embeddable in the eval report artifact.
func (t TripleStat) MarshalJSON() ([]byte, error) {
	type plain TripleStat // definition: strips the method, no recursion
	v := struct {
		plain
		Conviction *float64 `json:"conviction"`
	}{plain: plain(t)}
	if !math.IsInf(t.Conviction, 0) {
		c := t.Conviction
		v.Conviction = &c
	}
	return json.Marshal(v)
}

// TripleStats is the mined output plus the levelwise census the report
// prints: pool-restricted side events, frequent singles and pairs, the
// candidate join size, and the surviving triples sorted by support
// descending then heroes ascending.
type TripleStats struct {
	MinSupport int          `json:"minSupport"`
	SideEvents int          `json:"sideEvents"`
	WinEvents  int          `json:"winEvents"`
	WinRate    float64      `json:"winRate"`
	Singles    int          `json:"singles"`
	Candidates int          `json:"candidates"`
	Pairs      []PairStat   `json:"pairs"`
	Triples    []TripleStat `json:"triples"`
}

// MineTriples runs the levelwise vertical apriori over the train drafts
// restricted to pool heroes (the picker only knows pool heroes): frequent
// singletons, then pairs, then the antimonotone candidate join of triples
// whose three pairs all survived, each level counted by bitmap
// intersection. A triple is kept when its raw support reaches minSupport
// side events.
func MineTriples(m *Model, train []Match, minSupport int) TripleStats {
	if minSupport < 1 {
		minSupport = 1
	}
	ts := TripleStats{MinSupport: minSupport, Pairs: []PairStat{}, Triples: []TripleStat{}}

	// side events: one transaction per side per draft, pool picks only
	type sideEv struct {
		picks []int // pool roster indices
		won   bool
	}
	events := make([]sideEv, 0, 2*len(train))
	for _, mt := range train {
		var rad, dire []int
		for _, ev := range mt.Events {
			if !ev.IsPick {
				continue
			}
			idx, ok := m.IdxOf[ev.Slug]
			if !ok {
				continue
			}
			if _, pool := m.PoolPos[idx]; !pool {
				continue
			}
			if ev.IsRadiant {
				rad = append(rad, idx)
			} else {
				dire = append(dire, idx)
			}
		}
		events = append(events,
			sideEv{picks: rad, won: mt.RadiantWin},
			sideEv{picks: dire, won: !mt.RadiantWin},
		)
	}
	n := len(events)
	ts.SideEvents = n
	if n == 0 {
		return ts
	}

	singles := make([]tidSet, len(m.PoolIdx))
	for pi := range singles {
		singles[pi] = newTidSet(n)
	}
	win := newTidSet(n)
	for e, ev := range events {
		if ev.won {
			win.set(e)
		}
		for _, idx := range ev.picks {
			singles[m.PoolPos[idx]].set(e)
		}
	}
	ts.WinEvents = win.count()
	ts.WinRate = float64(ts.WinEvents) / float64(n)

	// L1: frequent singletons
	var freq []int // pool positions, ascending
	for pi, bm := range singles {
		if bm.count() >= minSupport {
			freq = append(freq, pi)
		}
	}
	ts.Singles = len(freq)

	// L2: frequent pairs, bitmaps kept for the L3 join
	pairBm := map[[2]int]tidSet{}
	for i := 0; i < len(freq); i++ {
		for j := i + 1; j < len(freq); j++ {
			bm := singles[freq[i]].and(singles[freq[j]])
			if bm.count() < minSupport {
				continue
			}
			pairBm[[2]int{freq[i], freq[j]}] = bm
			a, b := m.PoolIdx[freq[i]], m.PoolIdx[freq[j]]
			if a > b {
				a, b = b, a
			}
			ts.Pairs = append(ts.Pairs, PairStat{Heroes: [2]int{a, b}, Slugs: [2]string{m.Slugs[a], m.Slugs[b]}, N: bm.count()})
		}
	}
	sort.Slice(ts.Pairs, func(i, j int) bool { return pairLess(ts.Pairs[i].Heroes, ts.Pairs[j].Heroes) })

	// L3: the antimonotone join keeps triples whose three pairs all survived
	for a := 0; a < len(freq); a++ {
		for b := a + 1; b < len(freq); b++ {
			ab, ok := pairBm[[2]int{freq[a], freq[b]}]
			if !ok {
				continue
			}
			for c := b + 1; c < len(freq); c++ {
				if _, ok := pairBm[[2]int{freq[a], freq[c]}]; !ok {
					continue
				}
				if _, ok := pairBm[[2]int{freq[b], freq[c]}]; !ok {
					continue
				}
				ts.Candidates++
				bm := ab.and(singles[freq[c]])
				cnt := bm.count()
				if cnt < minSupport {
					continue
				}
				heroes := sortedTriple(m.PoolIdx[freq[a]], m.PoolIdx[freq[b]], m.PoolIdx[freq[c]])
				ts.Triples = append(ts.Triples, scoreTriple(m, heroes, n, cnt, bm.and(win).count(), ts.WinRate))
			}
		}
	}
	sort.Slice(ts.Triples, func(i, j int) bool {
		if ts.Triples[i].N != ts.Triples[j].N {
			return ts.Triples[i].N > ts.Triples[j].N
		}
		return tripleLess(ts.Triples[i].Heroes, ts.Triples[j].Heroes)
	})
	return ts
}

func pairLess(a, b [2]int) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	return a[1] < b[1]
}

func tripleLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// scoreTriple rates one surviving triple against the win consequent:
// confidence supp(X and win)/supp(X), lift conf/supp(win), conviction
// (1-supp(win))/(1-conf) with the conf = 1 perfect implication read as
// +Inf per ch29.
func scoreTriple(m *Model, heroes [3]int, total, n, wins int, winRate float64) TripleStat {
	t := TripleStat{
		Heroes: heroes, N: n, Wins: wins,
		Support:    float64(n) / float64(total),
		Confidence: float64(wins) / float64(n),
	}
	t.Lift = t.Confidence / winRate
	if wins == n {
		t.Conviction = math.Inf(1)
	} else {
		t.Conviction = (1 - winRate) / (1 - t.Confidence)
	}
	for i, h := range heroes {
		t.Slugs[i] = m.Slugs[h]
	}
	return t
}

// Completions counts mined triples the candidate would complete over
// allies: triples containing the candidate whose other two heroes are all
// present in allies, restricted to Lift >= minLift. Pass 0 to count every
// mined triple, 1.0 for the positively win-associated subset the gated
// tripleBonus term reads.
func (ts TripleStats) Completions(allies []int, candidate int, minLift float64) int {
	have := make(map[int]bool, len(allies))
	for _, a := range allies {
		have[a] = true
	}
	n := 0
	for _, t := range ts.Triples {
		if t.Lift < minLift {
			continue
		}
		self, ok := false, true
		for _, h := range t.Heroes {
			if h == candidate {
				self = true
			} else if !have[h] {
				ok = false
			}
		}
		if self && ok {
			n++
		}
	}
	return n
}

func sortedTriple(a, b, c int) [3]int {
	t := [3]int{a, b, c}
	if t[0] > t[1] {
		t[0], t[1] = t[1], t[0]
	}
	if t[1] > t[2] {
		t[1], t[2] = t[2], t[1]
	}
	if t[0] > t[1] {
		t[0], t[1] = t[1], t[0]
	}
	return t
}
