package eval

import (
	"database/sql"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// tripModel is the triple-mining fixture: a 6-hero roster with pool p0..p4
// (roster indices 0..4) and one non-pool hero behind them.
func tripModel(t *testing.T) *Model {
	t.Helper()
	m := &Model{
		RosterSize: 6,
		Slugs:      []string{"p0", "p1", "p2", "p3", "p4", "n5"},
		IdxOf:      map[string]int{},
		Pop:        map[int]float64{},
		PoolPos:    map[int]int{},
		Roles:      map[int][]string{},
	}
	for i, s := range m.Slugs {
		m.IdxOf[s] = i
		m.Pop[i] = 1.0 / 6.0
	}
	for i := 0; i < 5; i++ {
		m.PoolIdx = append(m.PoolIdx, i)
		m.PoolPos[i] = i
		m.Roles[i] = []string{"1"}
	}
	return m
}

// tripMatch builds one synthetic draft: an opening non-pool ban, then the
// two sides' pool picks in seq order.
func tripMatch(id int64, rad, dire []string, radWins bool) Match {
	m := Match{ID: id, StartTime: 1000 * id, RadiantWin: radWins}
	ev := func(radSide, pick bool, slug string) {
		m.Events = append(m.Events, DraftEvent{
			Seq: len(m.Events) + 1, IsRadiant: radSide, IsPick: pick, Slug: slug,
		})
	}
	ev(true, false, "n5")
	for _, s := range rad {
		ev(true, true, s)
	}
	for _, s := range dire {
		ev(false, true, s)
	}
	return m
}

// tripFixture is 8 drafts over the 5-hero pool designed so exactly three
// ally triples clear minSupport 3 over the 16 side events:
//
//	{p0,p1,p2}: 4 sides, 3 wins -> conf 3/4, lift 3/2, conviction 2
//	{p0,p1,p4}: 3 sides, 3 wins -> conf 1, lift 2, conviction +Inf
//	{p2,p3,p4}: 3 sides, 2 wins -> conf 2/3, lift 4/3, conviction 3/2
//
// while {p0,p3} never co-occurs, so every triple through that pair stays
// infrequent and the antimonotone join never reaches it.
func tripFixture() []Match {
	return []Match{
		tripMatch(1, []string{"p0", "p1", "p2"}, []string{"p3"}, true),
		tripMatch(2, []string{"p0", "p1", "p2"}, []string{"p4"}, true),
		tripMatch(3, []string{"p0", "p1", "p2"}, []string{"p2", "p3", "p4"}, true),
		tripMatch(4, []string{"p0", "p1", "p2"}, []string{"p2", "p3", "p4"}, false),
		tripMatch(5, []string{"p2", "p3", "p4"}, []string{"p1"}, true),
		tripMatch(6, []string{"p0", "p1", "p4"}, nil, true),
		tripMatch(7, []string{"p0", "p1", "p4"}, nil, true),
		tripMatch(8, []string{"p0", "p1", "p4"}, []string{"p2"}, true),
	}
}

func checkTriple(t *testing.T, got TripleStat, heroes [3]int, slugs [3]string, n, wins int,
	support, confidence, lift, conviction float64) {
	t.Helper()
	if got.Heroes != heroes || got.Slugs != slugs || got.N != n || got.Wins != wins {
		t.Fatalf("triple identity %+v, want heroes %v slugs %v n %d wins %d", got, heroes, slugs, n, wins)
	}
	if got.Support != support || got.Confidence != confidence || got.Lift != lift || got.Conviction != conviction {
		t.Fatalf("triple %v scores support %v conf %v lift %v conviction %v, want %v %v %v %v",
			heroes, got.Support, got.Confidence, got.Lift, got.Conviction, support, confidence, lift, conviction)
	}
}

func TestMineTriplesHandFixture(t *testing.T) {
	m := tripModel(t)
	ts := MineTriples(m, tripFixture(), 3)
	if ts.MinSupport != 3 || ts.SideEvents != 16 || ts.WinEvents != 8 || ts.WinRate != 0.5 {
		t.Fatalf("header %+v, want minSupport 3 sideEvents 16 winEvents 8 winRate 0.5", ts)
	}
	if ts.Singles != 5 {
		t.Fatalf("singles %d, want 5 (every pool hero clears support)", ts.Singles)
	}
	wantPairs := []PairStat{
		{Heroes: [2]int{0, 1}, Slugs: [2]string{"p0", "p1"}, N: 7},
		{Heroes: [2]int{0, 2}, Slugs: [2]string{"p0", "p2"}, N: 4},
		{Heroes: [2]int{0, 4}, Slugs: [2]string{"p0", "p4"}, N: 3},
		{Heroes: [2]int{1, 2}, Slugs: [2]string{"p1", "p2"}, N: 4},
		{Heroes: [2]int{1, 4}, Slugs: [2]string{"p1", "p4"}, N: 3},
		{Heroes: [2]int{2, 3}, Slugs: [2]string{"p2", "p3"}, N: 3},
		{Heroes: [2]int{2, 4}, Slugs: [2]string{"p2", "p4"}, N: 3},
		{Heroes: [2]int{3, 4}, Slugs: [2]string{"p3", "p4"}, N: 3},
	}
	if !reflect.DeepEqual(ts.Pairs, wantPairs) {
		t.Fatalf("pairs %+v, want %+v", ts.Pairs, wantPairs)
	}
	if ts.Candidates != 5 {
		t.Fatalf("candidates %d, want 5 (the all-pairs-frequent joins: 012 014 024 124 234)", ts.Candidates)
	}
	if len(ts.Triples) != 3 {
		t.Fatalf("triples %d, want 3: %+v", len(ts.Triples), ts.Triples)
	}
	// order: support desc, then heroes ascending
	conf := 2.0 / 3.0
	checkTriple(t, ts.Triples[0], [3]int{0, 1, 2}, [3]string{"p0", "p1", "p2"},
		4, 3, 4.0/16.0, 0.75, 0.75/0.5, 0.5/(1-0.75))
	checkTriple(t, ts.Triples[1], [3]int{0, 1, 4}, [3]string{"p0", "p1", "p4"},
		3, 3, 3.0/16.0, 1, 1/0.5, math.Inf(1))
	checkTriple(t, ts.Triples[2], [3]int{2, 3, 4}, [3]string{"p2", "p3", "p4"},
		3, 2, 3.0/16.0, conf, conf/0.5, 0.5/(1-conf))
}

// bruteTriples recounts every pool triple at or above minSupport by
// scanning the side sets directly, the exhaustive cross-check the bitmap
// apriori must reproduce.
func bruteTriples(t *testing.T, m *Model, train []Match, minSupport int) map[[3]int][2]int {
	t.Helper()
	var sides []struct {
		picks []int
		won   bool
	}
	for _, mt := range train {
		rad, dire := map[int]bool{}, map[int]bool{}
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
				rad[idx] = true
			} else {
				dire[idx] = true
			}
		}
		for _, side := range []struct {
			set map[int]bool
			won bool
		}{{rad, mt.RadiantWin}, {dire, !mt.RadiantWin}} {
			var picks []int
			for idx := range side.set {
				picks = append(picks, idx)
			}
			sides = append(sides, struct {
				picks []int
				won   bool
			}{picks, side.won})
		}
	}
	out := map[[3]int][2]int{}
	pool := append([]int(nil), m.PoolIdx...)
	sort.Ints(pool)
	for i := 0; i < len(pool); i++ {
		for j := i + 1; j < len(pool); j++ {
			for k := j + 1; k < len(pool); k++ {
				key := [3]int{pool[i], pool[j], pool[k]}
				for _, s := range sides {
					has := func(idx int) bool {
						for _, p := range s.picks {
							if p == idx {
								return true
							}
						}
						return false
					}
					if has(key[0]) && has(key[1]) && has(key[2]) {
						nw := out[key]
						nw[0]++
						if s.won {
							nw[1]++
						}
						out[key] = nw
					}
				}
			}
		}
	}
	for key, nw := range out {
		if nw[0] < minSupport {
			delete(out, key)
		}
	}
	return out
}

func TestMineTriplesAntimonotoneAndExhaustive(t *testing.T) {
	m := tripModel(t)
	ts := MineTriples(m, tripFixture(), 3)

	// exhaustive agreement: the mined set is exactly the brute-force set
	brute := bruteTriples(t, m, tripFixture(), 3)
	if len(ts.Triples) != len(brute) {
		t.Fatalf("mined %d triples, brute force sees %d: %+v", len(ts.Triples), len(brute), brute)
	}
	for _, tr := range ts.Triples {
		nw, ok := brute[tr.Heroes]
		if !ok || nw[0] != tr.N || nw[1] != tr.Wins {
			t.Fatalf("triple %v n %d wins %d, brute force says %v", tr.Heroes, tr.N, tr.Wins, nw)
		}
	}
	// antimonotone: a triple is mined only when all three of its pairs are
	// themselves frequent
	pairSet := map[[2]int]bool{}
	for _, p := range ts.Pairs {
		pairSet[p.Heroes] = true
	}
	for _, tr := range ts.Triples {
		for _, pair := range [][2]int{{tr.Heroes[0], tr.Heroes[1]}, {tr.Heroes[0], tr.Heroes[2]}, {tr.Heroes[1], tr.Heroes[2]}} {
			if !pairSet[pair] {
				t.Fatalf("triple %v mined but pair %v is not frequent", tr.Heroes, pair)
			}
		}
	}
	// {p0,p3} never co-occurs: infrequent pair, so no triple through it
	if pairSet[[2]int{0, 3}] {
		t.Fatalf("pair {p0,p3} mined with zero co-occurrences")
	}
	if brute[[3]int{0, 1, 3}][0] != 0 {
		t.Fatalf("brute force unexpectedly counts {p0,p1,p3}")
	}
}

func TestMineTriplesDeterministic(t *testing.T) {
	m := tripModel(t)
	a := MineTriples(m, tripFixture(), 3)

	shuffled := tripFixture()
	rng := rand.New(rand.NewSource(7))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	for i := range shuffled {
		evs := shuffled[i].Events
		for j, k := 0, len(evs)-1; j < k; j, k = j+1, k-1 {
			evs[j], evs[k] = evs[k], evs[j]
		}
	}
	b := MineTriples(m, shuffled, 3)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("mining depends on input order:\n%+v\n%+v", a, b)
	}
}

func TestTripleCompletions(t *testing.T) {
	m := tripModel(t)
	ts := MineTriples(m, tripFixture(), 3)
	allies := []int{0, 1} // p0, p1
	if got := ts.Completions(allies, 2, 0); got != 1 {
		t.Fatalf("p2 over {p0,p1} completes %d, want 1 ({p0,p1,p2})", got)
	}
	if got := ts.Completions(allies, 2, 1.0); got != 1 {
		t.Fatalf("lift-gated completions %d, want 1 (lift 3/2 clears 1)", got)
	}
	if got := ts.Completions(allies, 2, 1.6); got != 0 {
		t.Fatalf("completions above lift 1.6 = %d, want 0 ({p0,p1,p2} lifts 3/2)", got)
	}
	if got := ts.Completions(allies, 4, 1.0); got != 1 {
		t.Fatalf("p4 over {p0,p1} completes %d, want 1 ({p0,p1,p4})", got)
	}
	if got := ts.Completions(allies, 3, 0); got != 0 {
		t.Fatalf("p3 over {p0,p1} completes %d, want 0 (no mined triple needs only p0,p1)", got)
	}
	if got := ts.Completions([]int{0, 1, 2}, 4, 0); got != 1 {
		t.Fatalf("p4 over {p0,p1,p2} completes %d, want 1", got)
	}
	if got := ts.Completions(nil, 5, 0); got != 0 {
		t.Fatalf("non-pool candidate completes %d, want 0", got)
	}
}

func TestTripleStatMarshalInf(t *testing.T) {
	ts := MineTriples(tripModel(t), tripFixture(), 3)
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal mined set: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"conviction":null`) {
		t.Fatalf("perfect-implication conviction must render as null: %s", s)
	}
	if !strings.Contains(s, `"conviction":2`) {
		t.Fatalf("finite conviction 2 must render as a number: %s", s)
	}
}

func TestMineTriplesEmpty(t *testing.T) {
	ts := MineTriples(tripModel(t), nil, 3)
	if ts.SideEvents != 0 || ts.WinRate != 0 || len(ts.Pairs) != 0 || len(ts.Triples) != 0 {
		t.Fatalf("empty train mines %+v", ts)
	}
}

// openCorpusDB opens the live duckdb read-only; the corpus smoke tests
// never mutate the store.
func openCorpusDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		t.Fatalf("open %s read-only: %v", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping %s read-only: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// corpusDBPath resolves the live duckdb path, skipping the test when the
// corpus db is absent.
func corpusDBPath(t *testing.T) string {
	t.Helper()
	cfg := testConfig(t)
	path := filepath.Join(repoRoot, cfg.Paths.DBFile)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no corpus db at %s", path)
	}
	return path
}

// corpusProbeSupport is the below-config threshold the corpus invariants
// run at: the shipped eval.triples.minSupport mines nothing on the league
// backfill (no pool pair reaches it), so the structural checks need a
// threshold the corpus actually sustains.
const corpusProbeSupport = 3

// assertTripleInvariants checks the structural contract of one mined set:
// antimonotone pair closure and the (support desc, heroes asc) order.
func assertTripleInvariants(t *testing.T, ts TripleStats) {
	t.Helper()
	pairSet := map[[2]int]bool{}
	for _, p := range ts.Pairs {
		pairSet[p.Heroes] = true
	}
	for i, tr := range ts.Triples {
		for _, pair := range [][2]int{{tr.Heroes[0], tr.Heroes[1]}, {tr.Heroes[0], tr.Heroes[2]}, {tr.Heroes[1], tr.Heroes[2]}} {
			if !pairSet[pair] {
				t.Fatalf("triple %v mined without frequent pair %v", tr.Heroes, pair)
			}
		}
		if i > 0 {
			prev := ts.Triples[i-1]
			if prev.N < tr.N || (prev.N == tr.N && tripleLess(tr.Heroes, prev.Heroes)) {
				t.Fatalf("triples out of order at %d: %+v then %+v", i, prev, tr)
			}
		}
	}
}

func TestMineTriplesCorpus(t *testing.T) {
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

	ts := MineTriples(model, train, cfg.Eval.Triples.MinSupport)
	if ts.SideEvents != 2*len(train) {
		t.Fatalf("side events %d, want both sides of %d train drafts", ts.SideEvents, len(train))
	}
	// every draft contributes exactly one winning side
	if ts.WinEvents != len(train) || ts.WinRate != 0.5 {
		t.Fatalf("win events %d rate %v, want %d and 0.5", ts.WinEvents, ts.WinRate, len(train))
	}
	assertTripleInvariants(t, ts)
	if again := MineTriples(model, train, cfg.Eval.Triples.MinSupport); !reflect.DeepEqual(ts, again) {
		t.Fatalf("corpus mining is not deterministic")
	}

	// the shipped threshold leaves the structural checks vacuous on this
	// corpus, so rerun them at a probe threshold with the exhaustive
	// brute-force cross-check
	probe := MineTriples(model, train, corpusProbeSupport)
	assertTripleInvariants(t, probe)
	brute := bruteTriples(t, model, train, corpusProbeSupport)
	if len(probe.Triples) != len(brute) {
		t.Fatalf("probe mined %d triples, brute force sees %d", len(probe.Triples), len(brute))
	}
	for _, tr := range probe.Triples {
		nw, ok := brute[tr.Heroes]
		if !ok || nw[0] != tr.N || nw[1] != tr.Wins {
			t.Fatalf("probe triple %v n %d wins %d, brute force says %v", tr.Heroes, tr.N, tr.Wins, nw)
		}
	}

	ranked := append([]TripleStat(nil), ts.Triples...)
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].Lift > ranked[i].Lift {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	t.Logf("corpus triples: train %d drafts, sideEvents %d, singles %d, pairs %d, candidates %d, triples %d (minSupport %d)",
		len(train), ts.SideEvents, ts.Singles, len(ts.Pairs), ts.Candidates, len(ts.Triples), ts.MinSupport)
	t.Logf("corpus probe (minSupport %d): singles %d, pairs %d, candidates %d, triples %d",
		corpusProbeSupport, probe.Singles, len(probe.Pairs), probe.Candidates, len(probe.Triples))
	for i, tr := range ranked {
		if i == 5 {
			break
		}
		t.Logf("top lift %d: %v n %d wins %d conf %.4f lift %.4f conviction %.4f",
			i+1, tr.Slugs, tr.N, tr.Wins, tr.Confidence, tr.Lift, tr.Conviction)
	}
}
