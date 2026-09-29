package eval

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

// seqModel is the pick-order fixture roster: 6 heroes, no pool restriction
// (sequences run over the full roster).
func seqModel() *Model {
	m := &Model{
		RosterSize: 6,
		Slugs:      []string{"w", "x", "y", "z", "u", "v"},
		IdxOf:      map[string]int{},
		Pop:        map[int]float64{},
		PoolPos:    map[int]int{},
		Roles:      map[int][]string{},
	}
	for i, s := range m.Slugs {
		m.IdxOf[s] = i
	}
	return m
}

// seqEv is one draft_timing row spec: seq, radiant, pick, slug.
type seqEv struct {
	seq     int
	radiant bool
	pick    bool
	slug    string
}

func seqMatch(id int64, radiantWin bool, evs ...seqEv) Match {
	m := Match{ID: id, StartTime: 1000 * id, RadiantWin: radiantWin}
	for _, e := range evs {
		m.Events = append(m.Events, DraftEvent{
			Seq: e.seq, IsRadiant: e.radiant, IsPick: e.pick, Slug: e.slug,
		})
	}
	return m
}

// seqFixture is 4 drafts with bans interleaved. First picks: radiant opens
// x,x,w,v; dire opens y,v,y,z. The directed response stream holds 12
// transitions, y->w twice and every other direction once, and only {z,v}
// runs both ways (1 vs 1) while {y,w} runs one way (2 vs 0).
func seqFixture() []Match {
	return []Match{
		seqMatch(1, true,
			seqEv{1, true, false, "u"},
			seqEv{2, true, true, "x"},
			seqEv{3, false, true, "y"},
			seqEv{4, false, false, "w"},
			seqEv{5, true, true, "z"},
			seqEv{6, false, true, "v"},
		),
		seqMatch(2, false,
			seqEv{1, true, true, "x"},
			seqEv{2, true, false, "u"},
			seqEv{3, false, true, "v"},
			seqEv{4, true, true, "y"},
			seqEv{5, false, true, "w"},
			seqEv{6, true, true, "z"},
		),
		seqMatch(3, true,
			seqEv{1, false, true, "y"},
			seqEv{2, true, true, "w"},
			seqEv{3, false, true, "v"},
			seqEv{4, true, true, "u"},
		),
		seqMatch(4, true,
			seqEv{1, true, true, "v"},
			seqEv{2, false, true, "z"},
			seqEv{3, true, true, "u"},
		),
	}
}

func TestBuildSeqHandFixture(t *testing.T) {
	s := BuildSeq(seqModel(), seqFixture())
	if s.RosterSize != 6 || s.Matches != 4 {
		t.Fatalf("header roster %d matches %d, want 6 and 4", s.RosterSize, s.Matches)
	}
	wantFirstR := map[string]int{"x": 2, "w": 1, "v": 1}
	wantFirstD := map[string]int{"y": 2, "v": 1, "z": 1}
	if !reflect.DeepEqual(s.FirstRadiant, wantFirstR) || !reflect.DeepEqual(s.FirstDire, wantFirstD) {
		t.Fatalf("first picks rad %v dire %v, want %v %v", s.FirstRadiant, s.FirstDire, wantFirstR, wantFirstD)
	}
	if s.FirstRadiantN != 4 || s.FirstDireN != 4 {
		t.Fatalf("first-pick totals %d %d, want 4 4 (bans are not opening picks)", s.FirstRadiantN, s.FirstDireN)
	}
	want := []SeqTransition{
		{"v", "u", 1}, {"v", "y", 1}, {"v", "z", 1},
		{"w", "v", 1}, {"w", "z", 1},
		{"x", "v", 1}, {"x", "y", 1},
		{"y", "w", 2}, {"y", "z", 1},
		{"z", "u", 1}, {"z", "v", 1},
	}
	if !reflect.DeepEqual(s.Transitions, want) {
		t.Fatalf("transitions %+v, want %+v", s.Transitions, want)
	}
	if s.TransitionN != 12 {
		t.Fatalf("transition total %d, want 12", s.TransitionN)
	}
}

func TestSeqRates(t *testing.T) {
	m := seqModel()
	s := BuildSeq(m, seqFixture())
	if got := s.NextRate("v", "y"); got != 1.0/3.0 {
		t.Fatalf("P(y|v) = %v, want 1/3", got)
	}
	if got := s.NextRate("y", "w"); got != 2.0/3.0 {
		t.Fatalf("P(w|y) = %v, want 2/3", got)
	}
	if got := s.NextRate("u", "x"); got != 0 {
		t.Fatalf("P(x|u) = %v, want 0 (u never picked into a response)", got)
	}
	// Laplace over the 6-hero roster: (n+1)/(total+6)
	if got := s.LaplaceNext("v", "y"); got != 2.0/9.0 {
		t.Fatalf("smoothed P(y|v) = %v, want 2/9", got)
	}
	if got := s.LaplaceNext("y", "w"); got != 3.0/9.0 {
		t.Fatalf("smoothed P(w|y) = %v, want 3/9", got)
	}
	if got := s.LaplaceNext("u", "x"); got != 1.0/6.0 {
		t.Fatalf("smoothed P(x|u) = %v, want 1/6 (unseen context spreads uniformly)", got)
	}
	sum := 0.0
	for _, slug := range m.Slugs {
		sum += s.LaplaceNext("v", slug)
	}
	if math.Abs(sum-1) > 1e-12 {
		t.Fatalf("smoothed distribution over v sums to %v, want 1", sum)
	}
}

func TestSeqDirectionCensus(t *testing.T) {
	s := BuildSeq(seqModel(), seqFixture())
	if c := s.DirectionCensus(1, 2); c != (SeqCensus{Pairs: 10, BothWays: 1, Asymmetric: 9}) {
		t.Fatalf("census(1,2) %+v, want 10 pairs, 1 both ways, 9 asymmetric", c)
	}
	// only {y,w} reaches count 2, and it is one-directional
	if c := s.DirectionCensus(2, 2); c != (SeqCensus{Pairs: 1, BothWays: 0, Asymmetric: 1}) {
		t.Fatalf("census(2,2) %+v, want 1 pair, 0 both ways, 1 asymmetric", c)
	}
}

func TestBuildSeqDeterministic(t *testing.T) {
	a := BuildSeq(seqModel(), seqFixture())

	shuffled := seqFixture()
	rng := rand.New(rand.NewSource(11))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	for i := range shuffled {
		evs := shuffled[i].Events
		for j, k := 0, len(evs)-1; j < k; j, k = j+1, k-1 {
			evs[j], evs[k] = evs[k], evs[j]
		}
	}
	b := BuildSeq(seqModel(), shuffled)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("seq stats depend on input order:\n%+v\n%+v", a, b)
	}
}

func TestBuildSeqSeqTieDeterministic(t *testing.T) {
	m := seqModel()
	// two picks share seq 2 (impossible through the loader's primary key,
	// reachable for synthetic callers): the walk must not depend on slice
	// order, and the canonical tie-break orders dire before radiant
	a := BuildSeq(m, []Match{seqMatch(1, true,
		seqEv{2, true, true, "x"},
		seqEv{2, false, true, "y"},
		seqEv{3, false, true, "v"},
	)})
	b := BuildSeq(m, []Match{seqMatch(1, true,
		seqEv{2, false, true, "y"},
		seqEv{2, true, true, "x"},
		seqEv{3, false, true, "v"},
	)})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("seq tie changes the walk:\n%+v\n%+v", a, b)
	}
	want := []SeqTransition{{"x", "v", 1}, {"y", "x", 1}}
	if !reflect.DeepEqual(a.Transitions, want) {
		t.Fatalf("tie transitions %+v, want %+v", a.Transitions, want)
	}
}

func TestBuildSeqEmpty(t *testing.T) {
	s := BuildSeq(seqModel(), nil)
	if s.Matches != 0 || len(s.Transitions) != 0 || s.TransitionN != 0 {
		t.Fatalf("empty train builds %+v", s)
	}
	if got := s.LaplaceNext("w", "x"); got != 1.0/6.0 {
		t.Fatalf("smoothed P(x|w) over empty corpus = %v, want 1/6", got)
	}
}

func TestBuildSeqCorpus(t *testing.T) {
	cfg := testConfig(t)
	path := corpusDBPath(t)
	t.Chdir(repoRoot) // BuildModel resolves the gates doc against the cwd
	db := openCorpusDB(t, path)
	matches, _, err := LoadMatches(db)
	if err != nil {
		t.Fatalf("load matches: %v", err)
	}
	model, err := BuildModel(db, cfg)
	if err != nil {
		t.Fatalf("build model: %v", err)
	}
	train, _ := SplitMatches(matches, cfg.Eval.Split.TrainFrac)

	s := BuildSeq(model, train)
	if s.Matches != len(train) || s.RosterSize != model.RosterSize {
		t.Fatalf("header matches %d roster %d, want %d and %d", s.Matches, s.RosterSize, len(train), model.RosterSize)
	}
	sumR, sumD := 0, 0
	for _, n := range s.FirstRadiant {
		sumR += n
	}
	for _, n := range s.FirstDire {
		sumD += n
	}
	if sumR != s.FirstRadiantN || sumD != s.FirstDireN {
		t.Fatalf("first-pick maps sum %d %d, totals %d %d", sumR, sumD, s.FirstRadiantN, s.FirstDireN)
	}
	sumT := 0
	for i, tr := range s.Transitions {
		sumT += tr.N
		if i > 0 && (s.Transitions[i-1].From > tr.From ||
			(s.Transitions[i-1].From == tr.From && s.Transitions[i-1].To >= tr.To)) {
			t.Fatalf("transitions out of order at %d: %+v", i, tr)
		}
	}
	if sumT != s.TransitionN {
		t.Fatalf("transition sum %d, total %d", sumT, s.TransitionN)
	}
	// the smoothed conditional over the busiest context is a distribution
	busiest := ""
	for _, tr := range s.Transitions {
		if s.fromTotal[tr.From] > s.fromTotal[busiest] {
			busiest = tr.From
		}
	}
	sum := 0.0
	for _, slug := range model.Slugs {
		sum += s.LaplaceNext(busiest, slug)
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("smoothed distribution over %s sums to %v, want 1", busiest, sum)
	}

	// rebuild from a permuted copy: match order reversed and every match's
	// event slice shuffled
	rng := rand.New(rand.NewSource(3))
	permuted := make([]Match, len(train))
	for i, mt := range train {
		evs := append([]DraftEvent(nil), mt.Events...)
		rng.Shuffle(len(evs), func(a, b int) { evs[a], evs[b] = evs[b], evs[a] })
		permuted[len(train)-1-i] = Match{
			ID: mt.ID, StartTime: mt.StartTime, RadiantWin: mt.RadiantWin,
			DurationSec: mt.DurationSec, GameMode: mt.GameMode, LobbyType: mt.LobbyType,
			Bracket: mt.Bracket, Source: mt.Source, AverageRank: mt.AverageRank, Events: evs,
		}
	}
	if again := BuildSeq(model, permuted); !reflect.DeepEqual(s, again) {
		t.Fatalf("corpus seq stats are not deterministic")
	}

	c := s.DirectionCensus(5, 2)
	t.Logf("corpus seq: train %d drafts, firstRadiant %d distinct over %d, firstDire %d over %d, transitions %d over %d contexts, census(5,2) %+v",
		len(train), len(s.FirstRadiant), s.FirstRadiantN, len(s.FirstDire), s.FirstDireN, len(s.Transitions), s.TransitionN, c)
}
