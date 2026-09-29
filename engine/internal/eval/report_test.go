package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func sampleReport() Report {
	return Report{
		Patch:          "7.41f",
		Population:     LoadPopulation(nil, LoadStats{}),
		PopulationNote: populationNote,
		Split:          SplitInfo{Total: 4, Train: 2, Holdout: 2, TrainFrac: 0.7},
		Sequential: ReplayMetrics{
			Kind: "sequential", Picks: 10, RankedPicks: 8, MeanPercentile: 0.6,
			MeanPercentileCI: CI{Lo: 0.5, Hi: 0.7},
			TopK:             map[string]float64{"top1": 0.2, "top3": 0.5},
			TopKCI:           map[string]CI{"top1": {Lo: 0.1, Hi: 0.3}, "top3": {Lo: 0.4, Hi: 0.6}},
			AUC:              0.55, AUCCI: CI{Lo: 0.48, Hi: 0.62}, AUCMatches: 30, MeanAdvantage: 0.1,
		},
		FinalSet:   ReplayMetrics{Kind: "finalSet", TopK: map[string]float64{}, TopKCI: map[string]CI{}},
		Calibration: Calibration{
			Edges: []float64{0.1, 0.2}, Bins: []CalibrationBin{{Index: 0, N: 3, Rate: 0.4}},
			Violations: 1,
		},
		Audits: []FamilyAudit{{Family: "matchup", Cells: []AuditCell{}}},
		Ablation: ablationNote,
	}
}

func TestMarshalReportDeterministic(t *testing.T) {
	a, err := MarshalReport(sampleReport())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := MarshalReport(sampleReport())
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("two marshals of the same report differ")
	}
	var back Report
	if err := json.Unmarshal(a, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Sequential.TopK["top1"] != 0.2 || back.Audits[0].Family != "matchup" {
		t.Fatalf("roundtrip lost fields: %+v", back)
	}
}

func TestWriteReportCreatesPath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "eval", "latest.json")
	if err := WriteReport(out, sampleReport()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(sampleTopKRead(t, out)) != 2 {
		t.Fatalf("written report does not parse with both topK entries")
	}
}

func sampleTopKRead(t *testing.T, path string) map[string]float64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc struct {
		Sequential struct {
			TopK map[string]float64 `json:"topK"`
		} `json:"sequential"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc.Sequential.TopK
}

func TestTrimAuditCellsKeepsSurvivorsPlusCappedSample(t *testing.T) {
	cells := make([]AuditCell, 10)
	for i := range cells {
		cells[i].Hero = "h" + strconv.Itoa(i)
	}
	cells[2].Holm = true
	cells[7].BH = true
	audits := []FamilyAudit{{Family: "matchup", Cells: cells}}

	// cap 0 keeps everything and still records the totals
	full := TrimAuditCells(cloneAudits(audits), 0)
	if len(full[0].Cells) != 10 || full[0].CellsTotal != 10 {
		t.Fatalf("cap 0 trimmed: %d cells of %d", len(full[0].Cells), full[0].CellsTotal)
	}
	// cap 3: both survivors plus the first 3 non-survivors in order
	trimmed := TrimAuditCells(cloneAudits(audits), 3)
	got := trimmed[0].Cells
	if len(got) != 5 {
		t.Fatalf("got %d cells, want 2 survivors + 3 sample", len(got))
	}
	kept := map[string]bool{}
	for _, c := range got {
		kept[c.Hero] = true
	}
	for _, i := range []int{0, 1, 2, 3, 7} {
		if !kept["h"+strconv.Itoa(i)] {
			t.Errorf("expected cell h%d in the trimmed family", i)
		}
	}
	if kept["h8"] || kept["h4"] {
		t.Errorf("trimmed family kept cells past the capped sample")
	}
	if trimmed[0].CellsTotal != 10 {
		t.Errorf("cellsTotal = %d, want the pre-trim 10", trimmed[0].CellsTotal)
	}
	// the cap bounds the non-survivor sample, never the survivors
	tight := TrimAuditCells(cloneAudits(audits), 1)
	if len(tight[0].Cells) != 3 {
		t.Fatalf("cap 1 got %d cells, want 2 survivors + 1 sample", len(tight[0].Cells))
	}
}

func cloneAudits(a []FamilyAudit) []FamilyAudit {
	return append([]FamilyAudit(nil), a...)
}

func TestSummaryCarriesHeadlines(t *testing.T) {
	s := Summary(sampleReport())
	for _, want := range []string{"population", "split:", "sequential:", "finalSet:", "calibration:", "audit matchup:", "ablation:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary misses %q:\n%s", want, s)
		}
	}
}
