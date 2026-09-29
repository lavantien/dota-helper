package eval

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"poolguide/internal/config"
)

// Deps wires the eval run: the live db, the config hub, and the repo root
// the output path resolves against. CfgPath is the hub file the loaded Cfg
// came from, the file promote rewrites; empty falls back to
// repoRoot/config.json.
type Deps struct {
	DB       *sql.DB
	Cfg      *config.Config
	RepoRoot string
	CfgPath  string
}

// Run orchestrates the whole eval: load matches, split by time, replay both
// variants, compute the holdout metrics with bootstrap intervals, audit the
// raw pair tables, write the report, and return its summary text.
func Run(d Deps) (string, error) {
	matches, loadStats, err := LoadMatches(d.DB)
	if err != nil {
		return "", err
	}
	model, err := BuildModel(d.DB, d.Cfg)
	if err != nil {
		return "", err
	}
	train, holdout := SplitMatches(matches, d.Cfg.Eval.Split.TrainFrac)
	sequential := SequentialReplay(d.Cfg, model, matches)
	finalSet := FinalSetReplay(d.Cfg, model, matches)

	trainIDs, holdoutIDs := idSet(train), idSet(holdout)
	wonBy := map[int64]bool{}
	for _, mt := range matches {
		wonBy[mt.ID] = mt.RadiantWin
	}
	seq := replayMetrics("sequential", d.Cfg, model, sequential, holdoutIDs, matches)
	fin := replayMetrics("finalSet", d.Cfg, model, finalSet, holdoutIDs, matches)

	// every replayed match's sequential draft advantage, the picker stream
	// the comparator section and the ensemble consume (train blends with it,
	// holdout scores against it)
	pickerAdv := make(map[int64]float64, len(sequential))
	for _, r := range sequential {
		pickerAdv[r.MatchID] = r.Advantage
	}

	// the comparator bank scores the same two-sided holdout the replay AUC
	// reads: a one-sided draft carries no pool picks on one side, so its
	// picker advantage is pinned at 0 and would drag the picker row toward
	// chance against comparators that still see a full draft
	var twoSided []Match
	for _, mt := range holdout {
		rad, dire := sidePoolCounts(model, mt)
		if rad >= d.Cfg.Eval.MinSidePool && dire >= d.Cfg.Eval.MinSidePool {
			twoSided = append(twoSided, mt)
		}
	}

	var trainScores, holdScores []float64
	var holdWon []bool
	for _, r := range sequential {
		isTrain, isHold := trainIDs[r.MatchID], holdoutIDs[r.MatchID]
		if !isTrain && !isHold {
			continue
		}
		for _, p := range r.Picks {
			won := p.Radiant == wonBy[r.MatchID]
			if isTrain {
				trainScores = append(trainScores, p.Score)
			} else {
				holdScores = append(holdScores, p.Score)
				holdWon = append(holdWon, won)
			}
		}
	}

	audits, err := Audit(d.DB, d.Cfg)
	if err != nil {
		return "", err
	}
	audits = TrimAuditCells(audits, d.Cfg.Eval.ReportMaxCells)
	rep := Report{
		Patch:          d.Cfg.Patch,
		Population:     LoadPopulation(matches, loadStats),
		PopulationNote: populationNote,
		Split:          splitInfo(matches, train, holdout, d.Cfg.Eval.Split.TrainFrac),
		Sequential:     seq,
		FinalSet:       fin,
		Calibration:    Calibrate(trainScores, holdScores, holdWon, d.Cfg.Eval.CalibrationBins),
		Comparators:    buildComparators(d.Cfg, train, twoSided, pickerAdv),
		Triples:        buildTriples(d.Cfg, model, train),
		Seq:            buildSeq(d.Cfg, model, train),
		Audits:         audits,
		Ablation:       ablationNote,
	}
	out := filepath.Join(d.RepoRoot, d.Cfg.Paths.EvalOut)
	if err := WriteReport(out, rep); err != nil {
		return "", fmt.Errorf("eval: write report: %w", err)
	}
	return Summary(rep), nil
}

func idSet(matches []Match) map[int64]bool {
	ids := make(map[int64]bool, len(matches))
	for _, m := range matches {
		ids[m.ID] = true
	}
	return ids
}

func splitInfo(all, train, holdout []Match, frac float64) SplitInfo {
	s := SplitInfo{
		Total: len(all), Train: len(train), Holdout: len(holdout), TrainFrac: frac,
	}
	if len(train) > 0 {
		s.TrainStart, s.TrainEnd = dateOf(train[0].StartTime), dateOf(train[len(train)-1].StartTime)
	}
	if len(holdout) > 0 {
		s.HoldoutStart, s.HoldoutEnd = dateOf(holdout[0].StartTime), dateOf(holdout[len(holdout)-1].StartTime)
	}
	return s
}

// replayMetrics evaluates one replay variant on its holdout picks. The AUC
// and advantage set keeps only matches where both sides drafted at least
// eval.minSidePool pool heroes, so the advantage signal is two-sided; the
// pick-level metrics use every holdout pool pick.
func replayMetrics(kind string, cfg *config.Config, model *Model, replays []MatchReplay, holdoutIDs map[int64]bool, matches []Match) ReplayMetrics {
	m := ReplayMetrics{Kind: kind, TopK: map[string]float64{}, TopKCI: map[string]CI{}}
	byID := map[int64]Match{}
	for _, mt := range matches {
		byID[mt.ID] = mt
	}
	var hold, aucSet []MatchReplay
	for _, r := range replays {
		if !holdoutIDs[r.MatchID] {
			continue
		}
		hold = append(hold, r)
		m.Picks += len(r.Picks)
		for _, p := range r.Picks {
			if p.Ranked() {
				m.RankedPicks++
			}
		}
		rad, dire := sidePoolCounts(model, byID[r.MatchID])
		if rad >= cfg.Eval.MinSidePool && dire >= cfg.Eval.MinSidePool {
			aucSet = append(aucSet, r)
		}
	}
	holdPicks := picksOf(hold)
	m.MeanPercentile = MeanPercentile(holdPicks)

	var pickGroups [][]PickEval
	for _, r := range hold {
		pickGroups = append(pickGroups, r.Picks)
	}
	gather := func(idx []int) []PickEval {
		var out []PickEval
		for _, i := range idx {
			out = append(out, pickGroups[i]...)
		}
		return out
	}
	b := cfg.Eval.Bootstrap
	m.MeanPercentileCI = MatchesCI(len(pickGroups), b.Resamples, uint64(b.Seed), func(idx []int) float64 {
		return MeanPercentile(gather(idx))
	})
	for _, k := range cfg.Eval.TopK {
		key := fmt.Sprintf("top%d", k)
		m.TopK[key] = AnyRoleHitRate(holdPicks, k)
		m.TopKCI[key] = MatchesCI(len(pickGroups), b.Resamples, uint64(b.Seed), func(idx []int) float64 {
			return AnyRoleHitRate(gather(idx), k)
		})
	}
	m.AUC = DraftAUC(aucSet)
	m.AUCMatches = len(aucSet)
	m.AUCCI = MatchesCI(len(aucSet), b.Resamples, uint64(b.Seed), func(idx []int) float64 {
		sub := make([]MatchReplay, len(idx))
		for i, j := range idx {
			sub[i] = aucSet[j]
		}
		return DraftAUC(sub)
	})
	for _, r := range aucSet {
		m.MeanAdvantage += r.Advantage
	}
	if len(aucSet) > 0 {
		m.MeanAdvantage /= float64(len(aucSet))
	}
	return m
}

// sidePoolCounts returns per-side pool pick counts of one match.
func sidePoolCounts(m *Model, mt Match) (radiant, dire int) {
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
			radiant++
		} else {
			dire++
		}
	}
	return radiant, dire
}

func picksOf(replays []MatchReplay) []PickEval {
	var out []PickEval
	for _, r := range replays {
		out = append(out, r.Picks...)
	}
	return out
}
