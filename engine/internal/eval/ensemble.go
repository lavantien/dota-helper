package eval

import (
	"math/rand/v2"

	"poolguide/internal/stats"
)

// EnsembleSpec carries the bagging knobs that have no config keys of their
// own. The integrator maps NBAlpha from eval.naiveBayes.alpha, KnnK from
// eval.knn.k, Seed from eval.bootstrap.seed, and the ascent grid
// GridStep/MaxWeight/MaxPasses from eval.fit.
type EnsembleSpec struct {
	Bags      int     // bootstrap resamples B
	NBAlpha   float64 // per-bag naive Bayes Laplace alpha
	KnnK      int     // per-bag kNN neighbor count
	Seed      uint64  // PCG seed, drawn as (seed, seed) per the stats convention
	GridStep  float64 // blend weight grid step
	MaxWeight float64 // blend weight cap
	MaxPasses int     // coordinate ascent pass cap
}

// Ensemble is the ch23 bagged blend: B bootstrap resamples of the train
// matches each train a naive Bayes and a kNN, their averaged scores blend
// with the picker's draft advantage under weights fitted by grid ascent
// on train AUC.
type Ensemble struct {
	nbs     []*NaiveBayes
	knns    []*KNN
	weights []float64 // [picker, naiveBayes, knn]
}

// TrainEnsemble fits the whole blend: draws the bags, trains the 2*B
// comparators, scores the train matches with the bag averages and the
// picker advantages, then hands the three components to FitBlendWeights.
// pickerScores maps match id to the picker draft advantage; a train match
// with no entry blends as picker score 0. Bags must be positive or
// TrainEnsemble panics, matching the comparator constructors.
func TrainEnsemble(spec EnsembleSpec, train []Match, pickerScores map[int64]float64) *Ensemble {
	if spec.Bags <= 0 {
		panic("eval: ensemble bags must be positive")
	}
	e := &Ensemble{}
	rng := rand.New(rand.NewPCG(spec.Seed, spec.Seed))
	for _, idx := range bootstrapResamples(rng, len(train), spec.Bags) {
		bag := make([]Match, len(idx))
		for i, j := range idx {
			bag[i] = train[j]
		}
		e.nbs = append(e.nbs, TrainNaiveBayes(bag, spec.NBAlpha))
		e.knns = append(e.knns, TrainKNN(bag, spec.KnnK))
	}
	nbS := make([]float64, len(train))
	knS := make([]float64, len(train))
	pkS := make([]float64, len(train))
	labels := make([]bool, len(train))
	for i, m := range train {
		f := FinalSetFeatures(m)
		nbS[i], knS[i] = e.NBScore(f), e.KNNScore(f)
		pkS[i] = pickerScores[m.ID]
		labels[i] = m.RadiantWin
	}
	e.weights = FitBlendWeights([][]float64{pkS, nbS, knS}, labels,
		spec.GridStep, spec.MaxWeight, spec.MaxPasses)
	return e
}

// bootstrapResamples draws bags replicates of n train slots with
// replacement from one rng stream, the same draw pattern as
// stats.PercentileCI: every slot is an independent uniform pick, so a
// replicate of n slots leaves roughly 1/e of the rows out of bag.
func bootstrapResamples(rng *rand.Rand, n, bags int) [][]int {
	out := make([][]int, bags)
	if n <= 0 {
		return out
	}
	for b := range out {
		idx := make([]int, n)
		for j := range idx {
			idx[j] = rng.IntN(n)
		}
		out[b] = idx
	}
	return out
}

// NBScore is the bag-averaged naive Bayes log-odds of the draft.
func (e *Ensemble) NBScore(f DraftFeatures) float64 {
	if len(e.nbs) == 0 {
		return 0
	}
	sum := 0.0
	for _, nb := range e.nbs {
		sum += nb.ScoreMatch(f)
	}
	return sum / float64(len(e.nbs))
}

// KNNScore is the bag-averaged kNN radiant vote of the draft.
func (e *Ensemble) KNNScore(f DraftFeatures) float64 {
	if len(e.knns) == 0 {
		return 0
	}
	sum := 0.0
	for _, kn := range e.knns {
		sum += kn.ScoreMatch(f)
	}
	return sum / float64(len(e.knns))
}

// Weights returns a copy of the fitted blend weights in picker, naiveBayes,
// knn order.
func (e *Ensemble) Weights() []float64 {
	return append([]float64(nil), e.weights...)
}

// ScoreMatch blends the picker draft advantage of the match with the
// bagged comparator scores under the fitted weights.
func (e *Ensemble) ScoreMatch(f DraftFeatures, picker float64) float64 {
	return e.weights[0]*picker + e.weights[1]*e.NBScore(f) + e.weights[2]*e.KNNScore(f)
}

// FitBlendWeights maximizes the AUC of the non-negative weighted component
// sum by coordinate ascent: each weight scans the multiples of step up to
// and including maxWeight (when maxWeight is not a step multiple the grid
// stops at the largest multiple below it) holding the others fixed,
// keeping a value only on strict AUC improvement so ties keep the earlier,
// lower value, for at most maxPasses passes with an early stop once a
// pass improves nothing. AUC's scale invariance makes the absolute weight
// level meaningless; only the ratios matter. Degenerate input (no
// components, no labels, a broken grid) returns all zeros. Every component
// row must match the label count: a short row panics with an index error
// and a long row's tail is ignored.
func FitBlendWeights(components [][]float64, labels []bool, step, maxWeight float64, maxPasses int) []float64 {
	w := make([]float64, len(components))
	if len(components) == 0 || len(labels) == 0 || step <= 0 || maxWeight <= 0 || maxPasses < 1 {
		return w
	}
	cur := make([]float64, len(labels))
	base := make([]float64, len(labels))
	cand := make([]float64, len(labels))
	best := stats.AUC(cur, labels)
	for pass := 0; pass < maxPasses; pass++ {
		improved := false
		for j, comp := range components {
			for i := range base {
				base[i] = cur[i] - w[j]*comp[i]
			}
			bestV := w[j]
			for i := 0; ; i++ {
				v := step * float64(i)
				if v > maxWeight+step*1e-9 {
					break
				}
				for k := range cand {
					cand[k] = base[k] + v*comp[k]
				}
				if a := stats.AUC(cand, labels); a > best {
					best, bestV, improved = a, v, true
				}
			}
			if bestV != w[j] {
				w[j] = bestV
				for i := range cur {
					cur[i] = base[i] + bestV*comp[i]
				}
			}
		}
		if !improved {
			break
		}
	}
	return w
}
