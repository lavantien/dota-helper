package eval

import (
	"sort"
)

// Directional pick-order transition statistics over the train drafts (KDD
// ch30: order asymmetry is the point). The response transition is the
// consecutive-pick pair across sides, pooled over draft positions and both
// sides, so P(our next pick = h | enemy just picked x) keeps the direction
// of the ask. Bans are not picks and never enter the stream; the roster is
// unrestricted here because any hero can be answered.

// SeqTransition is one directed response count: our next pick To after the
// enemy just picked From.
type SeqTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
	N    int    `json:"n"`
}

// SeqCensus is the direction-asymmetry count over unordered hero pairs:
// how many pairs the census compared, how many ran both ways, and how
// many differed materially.
type SeqCensus struct {
	Pairs      int `json:"pairs"`
	BothWays   int `json:"bothWays"`
	Asymmetric int `json:"asymmetric"`
}

// SeqStats is the pick-order census: per-side first-pick counts over the
// roster, the pooled response transitions sorted by (from, to), and the
// totals the rate helpers read. Maps marshal with sorted keys, so the
// structure is deterministic end to end.
type SeqStats struct {
	RosterSize    int             `json:"rosterSize"`
	Matches       int             `json:"matches"`
	FirstRadiant  map[string]int  `json:"firstRadiant"`
	FirstDire     map[string]int  `json:"firstDire"`
	FirstRadiantN int             `json:"firstRadiantN"`
	FirstDireN    int             `json:"firstDireN"`
	Transitions   []SeqTransition `json:"transitions"`
	TransitionN   int             `json:"transitionN"`

	counts    map[[2]string]int
	fromTotal map[string]int
}

// BuildSeq walks the train drafts' pick streams in seq order (bans
// dropped, events canonically sorted so the walk is total over any input
// permutation) and tallies each side's opening pick plus every
// consecutive pick pair that crosses sides.
func BuildSeq(m *Model, train []Match) SeqStats {
	s := SeqStats{
		RosterSize:   m.RosterSize,
		FirstRadiant: map[string]int{},
		FirstDire:    map[string]int{},
		Transitions:  []SeqTransition{},
		counts:       map[[2]string]int{},
		fromTotal:    map[string]int{},
	}
	for _, mt := range train {
		picks := make([]DraftEvent, 0, len(mt.Events))
		for _, ev := range mt.Events {
			if !ev.IsPick {
				continue
			}
			if _, ok := m.IdxOf[ev.Slug]; !ok {
				continue
			}
			picks = append(picks, ev)
		}
		// draft_timing seq is unique per match (the store's primary key);
		// the side and slug tie-breaks only keep the walk deterministic
		// for synthetic inputs that repeat it
		sort.SliceStable(picks, func(i, j int) bool {
			if picks[i].Seq != picks[j].Seq {
				return picks[i].Seq < picks[j].Seq
			}
			if picks[i].IsRadiant != picks[j].IsRadiant {
				return !picks[i].IsRadiant
			}
			return picks[i].Slug < picks[j].Slug
		})
		if len(picks) == 0 {
			continue
		}
		s.Matches++
		firstR, firstD := true, true
		for i, ev := range picks {
			switch {
			case ev.IsRadiant && firstR:
				s.FirstRadiant[ev.Slug]++
				s.FirstRadiantN++
				firstR = false
			case !ev.IsRadiant && firstD:
				s.FirstDire[ev.Slug]++
				s.FirstDireN++
				firstD = false
			}
			if i > 0 && picks[i-1].IsRadiant != ev.IsRadiant {
				s.counts[[2]string{picks[i-1].Slug, ev.Slug}]++
			}
		}
	}
	for k, n := range s.counts {
		s.Transitions = append(s.Transitions, SeqTransition{From: k[0], To: k[1], N: n})
		s.fromTotal[k[0]] += n
		s.TransitionN += n
	}
	sort.Slice(s.Transitions, func(i, j int) bool {
		if s.Transitions[i].From != s.Transitions[j].From {
			return s.Transitions[i].From < s.Transitions[j].From
		}
		return s.Transitions[i].To < s.Transitions[j].To
	})
	return s
}

// NextRate is the unsmoothed P(next = to | enemy just picked from): 0 for
// an unseen context.
func (s SeqStats) NextRate(from, to string) float64 {
	total := s.fromTotal[from]
	if total == 0 {
		return 0
	}
	return float64(s.counts[[2]string{from, to}]) / float64(total)
}

// LaplaceNext is the add-one smoothed P(next = to | enemy just picked
// from) over the full roster vocabulary, the gated seqPrior term's read:
// an unseen context spreads uniformly at 1/rosterSize.
func (s SeqStats) LaplaceNext(from, to string) float64 {
	return (float64(s.counts[[2]string{from, to}]) + 1) /
		(float64(s.fromTotal[from]) + float64(s.RosterSize))
}

// DirectionCensus counts unordered hero pairs whose two directions differ
// materially, the ch30 asymmetry census. A pair is compared when its
// busier direction reaches minCount; a one-directional pair counts as
// asymmetric, and a two-sided pair counts when the busier direction
// carries at least ratio times the quieter one.
func (s SeqStats) DirectionCensus(minCount int, ratio float64) SeqCensus {
	pairs := map[[2]string][2]int{}
	for k, n := range s.counts {
		u := k
		if u[0] > u[1] {
			u[0], u[1] = u[1], u[0]
		}
		c := pairs[u]
		if k[0] == u[0] {
			c[0] += n
		} else {
			c[1] += n
		}
		pairs[u] = c
	}
	var out SeqCensus
	for _, c := range pairs {
		hi, lo := c[0], c[1]
		if lo > hi {
			hi, lo = lo, hi
		}
		if hi < minCount {
			continue
		}
		out.Pairs++
		if lo == 0 {
			out.Asymmetric++
			continue
		}
		out.BothWays++
		if float64(hi) >= ratio*float64(lo) {
			out.Asymmetric++
		}
	}
	return out
}
