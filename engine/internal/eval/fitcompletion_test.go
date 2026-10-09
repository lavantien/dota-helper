package eval

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"poolguide/internal/analytics"
)

func plantedMatrix(seed int64, rows, cols, rank int, noise float64) analytics.Matrix {
	rng := rand.New(rand.NewSource(seed))
	u := make([][]float64, rows)
	v := make([][]float64, cols)
	for i := range u {
		u[i] = make([]float64, rank)
		for k := range u[i] {
			u[i][k] = rng.Float64()
		}
	}
	for j := range v {
		v[j] = make([]float64, rank)
		for k := range v[j] {
			v[j][k] = rng.Float64()
		}
	}
	m := analytics.Matrix{Values: make([][]float64, rows), Observed: make([][]bool, rows)}
	for i := 0; i < rows; i++ {
		m.Values[i] = make([]float64, cols)
		m.Observed[i] = make([]bool, cols)
		for j := 0; j < cols; j++ {
			s := 0.0
			for k := 0; k < rank; k++ {
				s += u[i][k] * v[j][k]
			}
			m.Values[i][j] = s + noise*rng.NormFloat64()
			m.Observed[i][j] = true
		}
	}
	return m
}

func TestCompletionCVRecoversPlantedRank(t *testing.T) {
	cfg := testConfig(t)
	fc, base := cfg.Eval.CompletionFit, cfg.Completion
	mats := []analytics.Matrix{
		plantedMatrix(1, 24, 40, 6, 0.1),
		plantedMatrix(2, 24, 40, 6, 0.1),
	}
	run := func() (CompletionCandidate, CompletionCandidate, []CompletionCandidate, [][]heldCell) {
		winner, current, grid, held, err := completionCV([]string{"enemy", "ally"}, mats, fc, base, cfg.Eval.Bootstrap.Seed)
		if err != nil {
			t.Fatalf("completion cv: %v", err)
		}
		return winner, current, grid, held
	}
	winner, current, grid, held := run()
	bestR4 := math.Inf(-1)
	for _, c := range grid {
		if c.Rank == fc.Ranks[0] && c.Spearman > bestR4 {
			bestR4 = c.Spearman
		}
	}
	if winner.Spearman-bestR4 < 0.02 {
		t.Fatalf("rank-%d candidates scored %.4f against the winner's %.4f, the underfit gap is too small to demonstrate recovery",
			fc.Ranks[0], bestR4, winner.Spearman)
	}
	if winner.Rank < 8 {
		t.Fatalf("winner rank %d, want at least the smallest grid rank covering the planted rank-6 structure", winner.Rank)
	}
	if winner.Spearman < 0.9 {
		t.Fatalf("winner spearman %.4f, want the planted structure recovered", winner.Spearman)
	}
	if current.Rank != base.Rank || current.Lambda != base.Lambda {
		t.Fatalf("current candidate %+v does not mirror the config", current)
	}
	wantMasked := 0
	for i := range mats {
		wantMasked += int(fc.MaskFrac * float64(len(mats[i].Values)*len(mats[i].Values[0])))
	}
	if len(held[0])+len(held[1]) != wantMasked {
		t.Fatalf("masked %d cells, want %d", len(held[0])+len(held[1]), wantMasked)
	}
	w2, c2, g2, h2 := run()
	if w2 != winner || c2 != current || len(g2) != len(grid) || len(h2[0]) != len(held[0]) {
		t.Fatal("completion cv is not deterministic under a fixed seed")
	}
}

func TestFitCompletionWritesProposal(t *testing.T) {
	t.Chdir(repoRoot)
	cfg := testConfig(t)
	cfg.Eval.Bootstrap.Resamples = 50
	db := seedModelDB(t)
	run := func(root string) CompletionProposal {
		t.Helper()
		if _, err := FitCompletion(Deps{DB: db, Cfg: cfg, RepoRoot: root}); err != nil {
			t.Fatalf("fit completion: %v", err)
		}
		path := filepath.Join(root, fitProposalPath(cfg.Paths.FitOut, "completion"))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read proposal: %v", err)
		}
		var p CompletionProposal
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatalf("parse proposal: %v", err)
		}
		return p
	}
	p := run(t.TempDir())
	if len(p.Kinds) != 2 || p.Masked == 0 {
		t.Fatalf("proposal covers %v with %d masked cells", p.Kinds, p.Masked)
	}
	if p.Winner.Rank != cfg.Eval.CompletionFit.Ranks[0] {
		t.Fatalf("winner rank %d, want the smallest grid rank for the rank-1 fixture", p.Winner.Rank)
	}
	if p.Winner.Spearman < 0.99 {
		t.Fatalf("winner spearman %.4f, want the rank-1 structure recovered", p.Winner.Spearman)
	}
	if p.DiffCiLow > 0 {
		t.Fatalf("guard opened on a tie: diff CI [%.4f, %.4f] over equal candidates", p.DiffCiLow, p.DiffCiHi)
	}
	if math.IsNaN(p.DiffCiLow) || math.IsNaN(p.DiffCiHi) {
		t.Fatalf("diff CI is NaN: [%v, %v]", p.DiffCiLow, p.DiffCiHi)
	}
	q := run(t.TempDir())
	if q.Winner != p.Winner || q.Masked != p.Masked {
		t.Fatal("fit completion is not deterministic across runs")
	}
}
