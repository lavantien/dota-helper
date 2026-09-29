package eval

import (
	"math"
	"sort"
)

// KNN is the ch6/ch20 draft-retrieval comparator: cosine similarity over
// one-hot final-set vectors, the k nearest train drafts voting radiant_win
// weighted by their similarity. Similarity ties break by match id
// ascending, so neighbor selection never depends on train order.
type KNN struct {
	k     int
	ids   []int64
	won   []bool
	norm  []float64 // sqrt of each train vector's popcount
	feats [][]int
}

// TrainKNN indexes the final-set vectors of every match. k is eval.knn.k
// at wiring and must be positive; a k past the train size clamps to the
// whole set at scoring time.
func TrainKNN(matches []Match, k int) *KNN {
	if k <= 0 {
		panic("eval: knn k must be positive")
	}
	kn := &KNN{k: k}
	for _, m := range matches {
		f := FinalSetFeatures(m)
		kn.ids = append(kn.ids, m.ID)
		kn.won = append(kn.won, m.RadiantWin)
		kn.norm = append(kn.norm, math.Sqrt(float64(len(f.feats))))
		kn.feats = append(kn.feats, f.feats)
	}
	return kn
}

// ScoreMatch returns the similarity-weighted radiant vote of the k nearest
// train drafts, in [-1, 1]: every neighbor votes +1 for a radiant win and
// -1 against, weighted by cosine. An empty train set, an empty query, or a
// query orthogonal to every selected neighbor leaves no vote weight and
// scores a flat 0.
func (kn *KNN) ScoreMatch(f DraftFeatures) float64 {
	if len(kn.ids) == 0 {
		return 0
	}
	q := f.feats
	type neighbor struct {
		sim float64
		id  int64
		won bool
	}
	ns := make([]neighbor, len(kn.ids))
	for i, tf := range kn.feats {
		ns[i] = neighbor{sim: cosineSim(q, tf, kn.norm[i]), id: kn.ids[i], won: kn.won[i]}
	}
	sort.Slice(ns, func(a, b int) bool {
		if ns[a].sim != ns[b].sim {
			return ns[a].sim > ns[b].sim
		}
		return ns[a].id < ns[b].id
	})
	if kn.k < len(ns) {
		ns = ns[:kn.k]
	}
	var vote, weight float64
	for _, n := range ns {
		y := -1.0
		if n.won {
			y = 1.0
		}
		vote += n.sim * y
		weight += n.sim
	}
	if weight == 0 {
		return 0
	}
	return vote / weight
}

// cosineSim is the ch6 similarity between the query and one train vector
// from their shared feature count and the precomputed train norm: for
// one-hot sets this is |A ∩ B| / sqrt(|A| * |B|). Both slices are sorted
// ascending, so the intersection is a linear merge.
func cosineSim(q, tr []int, trNorm float64) float64 {
	qNorm := math.Sqrt(float64(len(q)))
	if qNorm == 0 || trNorm == 0 {
		return 0
	}
	shared := 0
	for i, j := 0, 0; i < len(q) && j < len(tr); {
		switch {
		case q[i] < tr[j]:
			i++
		case q[i] > tr[j]:
			j++
		default:
			shared++
			i, j = i+1, j+1
		}
	}
	return float64(shared) / (qNorm * trNorm)
}
