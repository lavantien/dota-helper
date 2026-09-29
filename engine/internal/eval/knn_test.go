package eval

import (
	"math"
	"math/rand/v2"
	"testing"
)

// hand fixture: q = R{1,2} D{3,4}
//
//	m1 R{1,2} D{3,4} win  -> cosine 4/4 = 1
//	m2 R{1,2} D{3,5} loss -> cosine 3/4 = 0.75
//	m3 R{9}   D{8}   win  -> cosine 0, orthogonal
func knnHandTrain() []Match {
	return []Match{
		cmpMatch(1, true, []int{1, 2}, []int{3, 4}),
		cmpMatch(2, false, []int{1, 2}, []int{3, 5}),
		cmpMatch(3, true, []int{9}, []int{8}),
	}
}

func TestKNNCosineHandValues(t *testing.T) {
	q := feats([]int{1, 2}, []int{3, 4})
	// k=1 recovers the nearest neighbor's label exactly
	if got := TrainKNN(knnHandTrain(), 1).ScoreMatch(q); got != 1 {
		t.Fatalf("k=1 score = %v, want the +1 nearest", got)
	}
	// k=2: weights 1 and 0.75 voting +1 and -1
	want := (1.0 - 0.75) / 1.75
	if got := TrainKNN(knnHandTrain(), 2).ScoreMatch(q); math.Abs(got-want) > 1e-12 {
		t.Fatalf("k=2 score = %v, want 1/7 = %v", got, want)
	}
	// the orthogonal neighbor carries zero weight, and k past the train
	// size clamps to all of it
	for _, k := range []int{3, 99} {
		if got := TrainKNN(knnHandTrain(), k).ScoreMatch(q); math.Abs(got-want) > 1e-12 {
			t.Fatalf("k=%d score = %v, want the same %v", k, got, want)
		}
	}
	// a query identical to the loser's draft votes -1 under k=1
	loser := feats([]int{1, 2}, []int{3, 5})
	if got := TrainKNN(knnHandTrain(), 1).ScoreMatch(loser); got != -1 {
		t.Fatalf("k=1 loser score = %v, want -1", got)
	}
	// an empty query is orthogonal to everything: no weight, flat 0
	if got := TrainKNN(knnHandTrain(), 1).ScoreMatch(feats(nil, nil)); got != 0 {
		t.Fatalf("empty query score = %v, want 0", got)
	}
	if got := TrainKNN(nil, 3).ScoreMatch(q); got != 0 {
		t.Fatalf("empty train score = %v, want 0", got)
	}
}

func TestKNNTieBreakByMatchID(t *testing.T) {
	q := feats([]int{1, 2}, nil)
	// both neighbors share cosine 1/sqrt(2*3); the lower match id takes the
	// k=1 slot
	hi := cmpMatch(5, true, []int{1, 3}, []int{5})  // win, id 5
	lo := cmpMatch(2, false, []int{2, 4}, []int{6}) // loss, id 2
	if got := TrainKNN([]Match{hi, lo}, 1).ScoreMatch(q); got != -1 {
		t.Fatalf("tie broken to the id-5 winner: score = %v, want -1", got)
	}
	hi.ID, lo.ID = 1, 6
	if got := TrainKNN([]Match{hi, lo}, 1).ScoreMatch(q); got != 1 {
		t.Fatalf("tie broken to the id-6 loser: score = %v, want 1", got)
	}
	// k=2 takes both and the equal weights cancel whatever the tie picked
	if got := TrainKNN([]Match{hi, lo}, 2).ScoreMatch(q); got != 0 {
		t.Fatalf("k=2 tie pair score = %v, want the 0 cancellation", got)
	}
}

func TestKNNRandomInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(55, 55))
	for trial := 0; trial < 20; trial++ {
		var train []Match
		n := 2 + rng.IntN(12)
		for i := 0; i < n; i++ {
			train = append(train, cmpMatch(int64(i+1), rng.IntN(2) == 0,
				cmpDrawSide(rng, 10), cmpDrawSide(rng, 10)))
		}
		k := 1 + rng.IntN(n)
		kn := TrainKNN(train, k)
		q := FinalSetFeatures(cmpMatch(999, true, cmpDrawSide(rng, 10), cmpDrawSide(rng, 10)))
		got := kn.ScoreMatch(q)
		if got > 1+1e-12 || got < -1-1e-12 {
			t.Fatalf("trial %d: score %v outside [-1,1]", trial, got)
		}
		if again := kn.ScoreMatch(q); again != got {
			t.Fatalf("trial %d: repeated call drifted from %v", trial, got)
		}
		if shuffled := TrainKNN(cmpShuffle(rng, train), k).ScoreMatch(q); shuffled != got {
			t.Fatalf("trial %d: train order changed the score", trial)
		}
	}
}

func TestKNNKContract(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("non-positive k must panic")
		}
	}()
	TrainKNN(nil, 0)
}
