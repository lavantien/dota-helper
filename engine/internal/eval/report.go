package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"poolguide/internal/emit"
)

// populationNote is the mismatch caveat frozen into every report: the
// replay population is the league backfill, the picker scores against
// public ranked aggregates.
const populationNote = "population is the stratz league captains-mode backfill (unranked practice lobbies), which differs from the public divine-immortal ranked all-pick aggregates the picker scores against"

// Population profiles the evaluated matches plus the loader's drop
// accounting, so the counts never silently undercount match_raw.
type Population struct {
	Matches            int            `json:"matches"`
	DroppedNoDraft     int            `json:"droppedNoDraft"`
	DroppedUnknown     int            `json:"droppedUnknown"`
	DroppedDuplicate   int            `json:"droppedDuplicate"`
	ByGameMode         map[string]int `json:"byGameMode"`
	ByLobbyType        map[string]int `json:"byLobbyType"`
	ByAverageRank      map[string]int `json:"byAverageRank"`
	BySource           map[string]int `json:"bySource"`
	MeanDurationSec    float64        `json:"meanDurationSec"`
}

// LoadPopulation profiles loaded matches; the league share reads from the
// per-match source column, not a hardcoded label.
func LoadPopulation(matches []Match, stats LoadStats) Population {
	p := Population{
		DroppedNoDraft:   stats.DroppedNoDraft,
		DroppedUnknown:   stats.DroppedUnknown,
		DroppedDuplicate: stats.DroppedDuplicate,
		ByGameMode:       map[string]int{},
		ByLobbyType:      map[string]int{},
		ByAverageRank:    map[string]int{},
		BySource:         map[string]int{},
	}
	for _, m := range matches {
		p.Matches++
		p.ByGameMode[m.GameMode]++
		p.ByLobbyType[m.LobbyType]++
		p.ByAverageRank[rankLabel(m.AverageRank)]++
		p.BySource[m.Source]++
		p.MeanDurationSec += float64(m.DurationSec)
	}
	if p.Matches > 0 {
		p.MeanDurationSec /= float64(p.Matches)
	}
	return p
}

func rankLabel(rank int) string {
	if rank == 0 {
		return "null"
	}
	return strconv.Itoa(rank)
}

// SplitInfo records the time-ordered train/holdout cut.
type SplitInfo struct {
	Total        int     `json:"total"`
	Train        int     `json:"train"`
	Holdout      int     `json:"holdout"`
	TrainFrac    float64 `json:"trainFrac"`
	TrainStart   string  `json:"trainStart"`
	TrainEnd     string  `json:"trainEnd"`
	HoldoutStart string  `json:"holdoutStart"`
	HoldoutEnd   string  `json:"holdoutEnd"`
}

// ReplayMetrics is one replay variant evaluated on the holdout.
type ReplayMetrics struct {
	Kind             string             `json:"kind"`
	Picks            int                `json:"picks"`
	RankedPicks      int                `json:"rankedPicks"`
	MeanPercentile   float64            `json:"meanPercentile"`
	MeanPercentileCI CI                 `json:"meanPercentileCI"`
	TopK             map[string]float64 `json:"topK"`
	TopKCI           map[string]CI      `json:"topKCI"`
	AUC              float64            `json:"auc"`
	AUCCI            CI                 `json:"aucCI"`
	AUCMatches       int                `json:"aucMatches"`
	MeanAdvantage    float64            `json:"meanAdvantage"`
}

// Report is the eval artifact marshaled to paths.evalOut. It deliberately
// carries no wall-clock stamp: identical db and config give identical bytes.
type Report struct {
	Patch          string            `json:"patch"`
	Population     Population        `json:"population"`
	PopulationNote string            `json:"populationNote"`
	Split          SplitInfo         `json:"split"`
	Sequential     ReplayMetrics     `json:"sequential"`
	FinalSet       ReplayMetrics     `json:"finalSet"`
	Calibration    Calibration       `json:"calibration"`
	Comparators    ComparatorsSection `json:"comparators"`
	Triples        TriplesSection    `json:"triples"`
	Seq            SeqSection        `json:"seq"`
	Audits         []FamilyAudit     `json:"audits"`
	Ablation       string            `json:"ablation"`
}

const ablationNote = "per-term ablation lives in the weight fit proposal (paths.fitOut, var/eval-fit.json)"

// TrimAuditCells caps each family's serialized cells: Holm and BH survivors
// always stay, the non-survivors keep a leading prefix of max non-survivor
// cells in the family's deterministic order. max <= 0 keeps every cell.
// CellsTotal records the pre-trim count either way.
func TrimAuditCells(audits []FamilyAudit, max int) []FamilyAudit {
	for i := range audits {
		f := &audits[i]
		f.CellsTotal = len(f.Cells)
		if max <= 0 {
			continue
		}
		survivors := survivorCount(f)
		if len(f.Cells)-survivors <= max {
			continue
		}
		kept := make([]AuditCell, 0, survivors+max)
		sampled := 0
		for _, c := range f.Cells {
			switch {
			case c.Holm || c.BH:
				kept = append(kept, c)
			case sampled < max:
				kept = append(kept, c)
				sampled++
			}
		}
		f.Cells = kept
	}
	return audits
}

func survivorCount(f *FamilyAudit) int {
	n := 0
	for _, c := range f.Cells {
		if c.Holm || c.BH {
			n++
		}
	}
	return n
}

// MarshalReport renders the report deterministically (map keys sort).
func MarshalReport(r Report) ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteReport marshals and persists the report.
func WriteReport(path string, r Report) error {
	b, err := MarshalReport(r)
	if err != nil {
		return err
	}
	return emit.WriteFile(path, b)
}

// Summary renders the human-readable headline block.
func Summary(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "eval: %d matches (%s)\n", r.Population.Matches, populationNote)
	fmt.Fprintf(&b, "population: game modes %s, lobbies %s, mean duration %.0fs\n",
		countsLine(r.Population.ByGameMode), countsLine(r.Population.ByLobbyType), r.Population.MeanDurationSec)
	fmt.Fprintf(&b, "split: %d train [%s..%s], %d holdout [%s..%s]\n",
		r.Split.Train, r.Split.TrainStart, r.Split.TrainEnd,
		r.Split.Holdout, r.Split.HoldoutStart, r.Split.HoldoutEnd)
	for _, m := range []ReplayMetrics{r.Sequential, r.FinalSet} {
		fmt.Fprintf(&b, "%s: %d picks (%d ranked), mean percentile %.4f [%.4f, %.4f]",
			m.Kind, m.Picks, m.RankedPicks, m.MeanPercentile, m.MeanPercentileCI.Lo, m.MeanPercentileCI.Hi)
		for _, k := range sortedTopK(m.TopK) {
			ci := m.TopKCI[k]
			fmt.Fprintf(&b, ", %s %.4f [%.4f, %.4f]", k, m.TopK[k], ci.Lo, ci.Hi)
		}
		fmt.Fprintf(&b, ", auc %.4f [%.4f, %.4f] over %d matches\n",
			m.AUC, m.AUCCI.Lo, m.AUCCI.Hi, m.AUCMatches)
	}
	fmt.Fprintf(&b, "calibration: %d bins, %d monotonicity violations\n",
		len(r.Calibration.Bins), r.Calibration.Violations)
	for _, row := range r.Comparators.Rows {
		fmt.Fprintf(&b, "comparator %s: auc %.4f [%.4f, %.4f] over %d matches\n",
			row.Name, row.AUC, row.CI.Lo, row.CI.Hi, row.Matches)
	}
	if e := r.Comparators.Ensemble; e != nil {
		fmt.Fprintf(&b, "ensemble: %d bags, weights [picker %.2f, naiveBayes %.2f, knn %.2f], auc %.4f [%.4f, %.4f]\n",
			e.Bags, e.Weights[0], e.Weights[1], e.Weights[2], e.AUC, e.CI.Lo, e.CI.Hi)
	}
	fmt.Fprintf(&b, "triples: %d sides, %d singles, %d pairs, %d triples at support %d, sweep %d points\n",
		r.Triples.SideEvents, r.Triples.Singles, len(r.Triples.Pairs), len(r.Triples.Triples),
		r.Triples.MinSupport, len(r.Triples.Sweep))
	fmt.Fprintf(&b, "seq: %d matches, %d transitions, census %d pairs (%d both ways, %d asymmetric)\n",
		r.Seq.Matches, r.Seq.TransitionN, r.Seq.Census.Pairs, r.Seq.Census.BothWays, r.Seq.Census.Asymmetric)
	for _, a := range r.Audits {
		line := fmt.Sprintf("audit %s: %d tested, holm %d, bh %d", a.Family, a.Tested, a.HolmSurvivors, a.BHSurvivors)
		if a.Spearman != nil {
			line += fmt.Sprintf(", spearman %.4f", *a.Spearman)
		}
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "ablation: %s\n", r.Ablation)
	return b.String()
}

func countsLine(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		if k == "" {
			parts[i] = fmt.Sprintf("unset=%d", counts[k])
			continue
		}
		parts[i] = fmt.Sprintf("%s=%d", k, counts[k])
	}
	return strings.Join(parts, ", ")
}

func sortedTopK(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dateOf(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}
