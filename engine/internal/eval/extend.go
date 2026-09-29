package eval

import (
	"poolguide/internal/config"
	"poolguide/internal/stats"
)

// Phase 5 report sections, diagnostics only: none of them feeds the live
// picker score, they place the picker beside the book's comparator and
// pattern techniques on the same holdout drafts. The Enabled flags on the
// config sections stay reserved for future live integration.

// ComparatorRow is one draft-level scorer's holdout AUC with its bootstrap
// interval: the picker's sequential draft advantage, naive Bayes, or kNN.
type ComparatorRow struct {
	Name    string  `json:"name"`
	AUC     float64 `json:"auc"`
	CI      CI      `json:"ci"`
	Matches int     `json:"matches"`
}

// EnsembleSection is the ch23 bagged blend: bags bootstrap resamples of the
// train split each fit a naive Bayes and a kNN, the bag averages blend with
// the picker advantage under ascent-fitted weights [picker, naiveBayes, knn].
type EnsembleSection struct {
	Bags    int       `json:"bags"`
	Weights []float64 `json:"weights"`
	AUC     float64   `json:"auc"`
	CI      CI        `json:"ci"`
	Matches int       `json:"matches"`
}

// ComparatorsSection lines the picker up against the comparator bank on the
// holdout drafts. Every row scores the same matches, so the AUCs compare
// like for like; the caller passes the two-sided holdout the replay AUC
// reads, keeping the populations comparable across report sections.
type ComparatorsSection struct {
	Rows     []ComparatorRow  `json:"rows"`
	Ensemble *EnsembleSection `json:"ensemble,omitempty"`
}

// SupportPoint is one threshold of the support sweep: cumulative pair and
// triple counts at that raw side-event support.
type SupportPoint struct {
	Support int `json:"support"`
	Pairs   int `json:"pairs"`
	Triples int `json:"triples"`
}

// TriplesSection is the ch23-28 vertical apriori view: the embedded stats
// are mined at the config threshold, the sweep re-reads one floor mining
// (support 1) so the threshold's place in the observed support landscape is
// data-derived, not asserted.
type TriplesSection struct {
	TripleStats
	Sweep []SupportPoint `json:"sweep"`
}

// SeqSection is the ch30 pick-order census over the train drafts with its
// direction-asymmetry summary under the config thresholds.
type SeqSection struct {
	SeqStats
	Census SeqCensus `json:"census"`
}

// buildComparators trains the bank on the train split and scores every
// holdout match it is given: the picker row replays the same sequential
// draft advantage the ensemble consumes, so pickerAdv must carry both splits
// keyed by match id. All intervals bootstrap over holdout match resamples.
func buildComparators(cfg *config.Config, train, holdout []Match, pickerAdv map[int64]float64) ComparatorsSection {
	picker := scoreVec(holdout, func(mt Match) float64 { return pickerAdv[mt.ID] })
	nb := TrainNaiveBayes(train, cfg.Eval.NaiveBayes.Alpha)
	nbScores := scoreVec(holdout, func(mt Match) float64 { return nb.ScoreMatch(FinalSetFeatures(mt)) })
	kn := TrainKNN(train, cfg.Eval.Knn.K)
	knScores := scoreVec(holdout, func(mt Match) float64 { return kn.ScoreMatch(FinalSetFeatures(mt)) })

	labels := make([]bool, len(holdout))
	for i, mt := range holdout {
		labels[i] = mt.RadiantWin
	}
	sec := ComparatorsSection{Rows: []ComparatorRow{
		{Name: "picker", AUC: stats.AUC(picker, labels)},
		{Name: "naiveBayes", AUC: stats.AUC(nbScores, labels)},
		{Name: "knn", AUC: stats.AUC(knScores, labels)},
	}}
	b := cfg.Eval.Bootstrap
	ci := func(scores []float64) CI {
		return MatchesCI(len(holdout), b.Resamples, uint64(b.Seed), func(idx []int) float64 {
			sub := make([]float64, len(idx))
			won := make([]bool, len(idx))
			for i, j := range idx {
				sub[i], won[i] = scores[j], labels[j]
			}
			return stats.AUC(sub, won)
		})
	}
	for i := range sec.Rows {
		sec.Rows[i].Matches = len(holdout)
		sec.Rows[i].CI = ci([][]float64{picker, nbScores, knScores}[i])
	}

	ens := TrainEnsemble(EnsembleSpec{
		Bags:      cfg.Eval.Ensemble.Bags,
		NBAlpha:   cfg.Eval.NaiveBayes.Alpha,
		KnnK:      cfg.Eval.Knn.K,
		Seed:      uint64(b.Seed),
		GridStep:  cfg.Eval.Fit.GridStep,
		MaxWeight: cfg.Eval.Fit.MaxWeight,
		MaxPasses: cfg.Eval.Fit.MaxPasses,
	}, train, pickerAdv)
	blend := scoreVec(holdout, func(mt Match) float64 {
		return ens.ScoreMatch(FinalSetFeatures(mt), pickerAdv[mt.ID])
	})
	sec.Ensemble = &EnsembleSection{
		Bags:    cfg.Eval.Ensemble.Bags,
		Weights: ens.Weights(),
		AUC:     stats.AUC(blend, labels),
		CI:      ci(blend),
		Matches: len(holdout),
	}
	return sec
}

// scoreVec maps one holdout scorer over the matches, holding the label
// vector in lockstep for the bootstrap.
func scoreVec(ms []Match, score func(Match) float64) []float64 {
	out := make([]float64, len(ms))
	for i, mt := range ms {
		out[i] = score(mt)
	}
	return out
}

// buildTriples mines the config-threshold view for the report and re-reads
// one floor mining for the sweep: every distinct pair support the data
// shows, with the cumulative pair and triple counts at that threshold.
func buildTriples(cfg *config.Config, m *Model, train []Match) TriplesSection {
	sec := TriplesSection{
		TripleStats: MineTriples(m, train, cfg.Eval.Triples.MinSupport),
		Sweep:       []SupportPoint{},
	}
	floor := MineTriples(m, train, 1)
	supports := make([]int, 0, len(floor.Pairs))
	for _, p := range floor.Pairs {
		supports = append(supports, p.N)
	}
	sortDesc(supports)
	for i, s := range supports {
		if i > 0 && s == supports[i-1] {
			continue
		}
		point := SupportPoint{Support: s}
		for _, p := range floor.Pairs {
			if p.N >= s {
				point.Pairs++
			}
		}
		for _, tr := range floor.Triples {
			if tr.N >= s {
				point.Triples++
			}
		}
		sec.Sweep = append(sec.Sweep, point)
	}
	return sec
}

// buildSeq walks the train pick streams and runs the direction census
// under the config thresholds.
func buildSeq(cfg *config.Config, m *Model, train []Match) SeqSection {
	s := BuildSeq(m, train)
	return SeqSection{
		SeqStats: s,
		Census:   s.DirectionCensus(cfg.Eval.Seq.CensusMinCount, cfg.Eval.Seq.CensusRatio),
	}
}

func sortDesc(v []int) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] > v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
