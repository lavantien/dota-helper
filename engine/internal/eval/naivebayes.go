package eval

import (
	"math"
	"sort"
)

// DraftFeatures is the final-set view of one draft the comparator models
// share: the (hero, side) presence pairs of the finished picks, bans
// excluded. A feature encodes as heroID*2 + sideBit with sideBit 1 for
// radiant, so ascending feature order is hero then side and ScoreMatch
// sums in that fixed order for byte-identical output across calls.
type DraftFeatures struct {
	feats []int
}

// FinalSetFeatures projects one loaded match onto the comparator feature
// space: picks only, deduplicated (the loader already drops drafts that
// repeat a hero across picks and bans), sorted ascending.
func FinalSetFeatures(m Match) DraftFeatures {
	var feats []int
	for _, ev := range m.Events {
		if !ev.IsPick {
			continue
		}
		bit := 0
		if ev.IsRadiant {
			bit = 1
		}
		feats = append(feats, ev.HeroID*2+bit)
	}
	sort.Ints(feats)
	out := feats[:0]
	for i, f := range feats {
		if i == 0 || f != feats[i-1] {
			out = append(out, f)
		}
	}
	return DraftFeatures{feats: out}
}

// NaiveBayes is the ch19 multivariate-Bernoulli comparator: per (hero,
// side) presence feature, win and loss counts over the train split, with a
// Laplace add-alpha rescue so a zero cell scores a finite ratio instead of
// -Inf. A feature counted zero in both classes is out of vocabulary and
// skipped, the ch19 unknown-word rule; scoring it anyway would add another
// copy of the class prior per unknown feature.
type NaiveBayes struct {
	alpha           float64
	nWin, nLoss     int
	winCnt, lossCnt map[int]int
}

// TrainNaiveBayes counts the final-set features of every match. alpha is
// eval.naiveBayes.alpha at wiring and must be positive, or the zero-cell
// rescue is gone and TrainNaiveBayes panics.
func TrainNaiveBayes(matches []Match, alpha float64) *NaiveBayes {
	if alpha <= 0 {
		panic("eval: naive Bayes alpha must be positive")
	}
	nb := &NaiveBayes{alpha: alpha, winCnt: map[int]int{}, lossCnt: map[int]int{}}
	for _, m := range matches {
		if m.RadiantWin {
			nb.nWin++
		} else {
			nb.nLoss++
		}
		for _, f := range FinalSetFeatures(m).feats {
			if m.RadiantWin {
				nb.winCnt[f]++
			} else {
				nb.lossCnt[f]++
			}
		}
	}
	return nb
}

// ScoreMatch is the radiant-win log-odds of the draft: the prior log-odds
// plus, per present feature, the log ratio of its class conditionals
// P(f|win)/P(f|loss), each cell Laplace-smoothed against a binary
// denominator N_class + 2*alpha. Positive favors radiant, 0 is exactly the
// decision boundary, and the ascending feature sum makes equal inputs
// bit-identical. A train split holding only one label carries no
// discrimination and scores a flat 0, the stats.AUC chance-line
// convention.
func (nb *NaiveBayes) ScoreMatch(f DraftFeatures) float64 {
	if nb.nWin == 0 || nb.nLoss == 0 {
		return 0
	}
	score := math.Log(float64(nb.nWin) / float64(nb.nLoss))
	for _, ft := range f.feats {
		w, l := nb.winCnt[ft], nb.lossCnt[ft]
		if w == 0 && l == 0 {
			continue
		}
		pw := (float64(w) + nb.alpha) / (float64(nb.nWin) + 2*nb.alpha)
		pl := (float64(l) + nb.alpha) / (float64(nb.nLoss) + 2*nb.alpha)
		score += math.Log(pw / pl)
	}
	return score
}
