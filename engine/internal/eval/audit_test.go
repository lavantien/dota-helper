package eval

import (
	"math"
	"path/filepath"
	"testing"

	"poolguide/internal/mine"
	"poolguide/internal/stats"
	"poolguide/internal/store"
)

// TestAuditReadsStratzVsHeroPerspective pins the loader: stratz_vs serves the
// pool hero's own wins (verified in-db, see mine.canonicalPP), so the audit
// reads win_count as served and every lift and p stays on the hero side.
func TestAuditReadsStratzVsHeroPerspective(t *testing.T) {
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "audit.duckdb"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := mine.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	exec := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %s: %v", q, err)
		}
	}
	// hero-perspective counts as served: the hero beat e1 90 of 100
	for _, tc := range []struct{ other string; wins int }{
		{"e1", 90}, {"e2", 60}, {"e3", 60}, {"e4", 60},
	} {
		exec(`INSERT INTO matchup_raw (pool_slug, enemy_slug, win_count, matches, source, scrape_date, confidence)
			VALUES ('a', ?, ?, 100, 'stratz_vs', '2026-09-26', 'high')`, tc.other, tc.wins)
		exec(`INSERT INTO norm_cell (hero, other, kind, pct, z, completed)
			VALUES ('a', ?, 'enemy', ?, 0, false)`, tc.other, map[string]float64{"e1": 0.9, "e2": 0.5, "e3": 0.5, "e4": 0.5}[tc.other])
	}
	fams, err := Audit(db, cfg)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	var fam FamilyAudit
	for _, f := range fams {
		if f.Family == "matchup" {
			fam = f
		}
	}
	if fam.Tested != 4 || fam.HolmSurvivors != 1 {
		t.Fatalf("tested %d holm %d, want the hand calculation 4/1", fam.Tested, fam.HolmSurvivors)
	}
	for _, c := range fam.Cells {
		if c.Other != "e1" {
			continue
		}
		if c.W != 90 || c.BaseW != 180 {
			t.Fatalf("e1 counted as w=%d base=%d, want the served 90 against the 180 pool complement", c.W, c.BaseW)
		}
		if !c.Holm || !c.BH {
			t.Fatalf("e1 must survive both corrections")
		}
	}
	if fam.Spearman == nil || *fam.Spearman <= 0 {
		t.Fatalf("spearman %v, want positive", fam.Spearman)
	}
}

func TestAuditFamilyHandCalculation(t *testing.T) {
	// hero "a" faces four enemies 100 times each: one hot cell (90 wins vs
	// e1) against three identical middling cells (60 each). W=270, N=400,
	// base rate 0.675, so e1's complement is (180, 300).
	cells := []PairCount{
		{"a", "e1", 90, 100},
		{"a", "e2", 60, 100},
		{"a", "e3", 60, 100},
		{"a", "e4", 60, 100},
	}
	pcts := map[string]float64{"a\x00e1": 0.9, "a\x00e2": 0.5, "a\x00e3": 0.5, "a\x00e4": 0.5}
	fam := auditFamily("matchup", cells, pcts, 0.05, 30)
	if fam.Tested != 4 {
		t.Fatalf("tested %d cells, want 4", fam.Tested)
	}
	byKey := map[string]AuditCell{}
	for _, c := range fam.Cells {
		byKey[c.Hero+"/"+c.Other] = c
	}
	dep, fair := byKey["a/e1"], byKey["a/e2"]
	if dep.Method != "chisq" {
		t.Fatalf("dependent cell method %s, want chisq", dep.Method)
	}
	// hand table: e1 vs its complement is the 2x2 (90, 10, 180, 120)
	if stat, p, ok := stats.ChiSquare2x2(90, 10, 180, 120); !ok || math.Abs(dep.P-p) > 1e-15 {
		t.Fatalf("dependent p %v diverges from the direct chisq %v (ok=%v, stat=%v)", dep.P, p, ok, stat)
	}
	if dep.P > 1e-4 {
		t.Fatalf("dependent p %v, want the small tail of stat ~30.8", dep.P)
	}
	if fair.Method != "chisq" || fair.P < 0.03 || fair.P > 0.1 {
		t.Fatalf("fair cell p %v (%s), want the ~0.066 of stat ~3.42", fair.P, fair.Method)
	}
	if !dep.Holm || !dep.BH {
		t.Fatalf("dependent cell must survive Holm and BH")
	}
	if fair.Holm || fair.BH {
		t.Fatalf("fair cells must not survive: %+v", fair)
	}
	if fam.HolmSurvivors != 1 || fam.BHSurvivors != 1 {
		t.Fatalf("survivor counts %d/%d, want 1/1", fam.HolmSurvivors, fam.BHSurvivors)
	}
	// lift = (w/n) / (W/N): dependent 0.9/0.675, fair 0.6/0.675
	if math.Abs(dep.Lift-0.9/0.675) > 1e-12 {
		t.Fatalf("dependent lift %v", dep.Lift)
	}
	if fam.Spearman == nil || *fam.Spearman <= 0 {
		t.Fatalf("spearman %v over one hot and three flat cells, want positive", fam.Spearman)
	}
}

func TestAuditFamilyBinomialFallback(t *testing.T) {
	// thin hero: every cell's expected counts land under 5, so each falls
	// back to the exact two-sided binomial against the complement rate.
	cells := []PairCount{
		{"b", "e1", 8, 10},
		{"b", "e2", 5, 10},
		{"b", "e3", 5, 10},
	}
	fam := auditFamily("synergy", cells, nil, 0.05, 3)
	if fam.Tested != 3 {
		t.Fatalf("tested %d, want 3", fam.Tested)
	}
	for _, c := range fam.Cells {
		if c.Method != "binomial" {
			t.Fatalf("cell %s/%s method %s, want the binomial fallback", c.Hero, c.Other, c.Method)
		}
		want := stats.BinomialTwoSided(int(c.W), int(c.N), float64(c.BaseW)/float64(c.BaseN))
		if math.Abs(c.P-want) > 1e-12 {
			t.Fatalf("cell %s/%s p %v, want the two-sided binomial %v", c.Hero, c.Other, c.P, want)
		}
	}
	if fam.Spearman != nil {
		t.Fatalf("spearman without norm pcts must be nil, got %v", *fam.Spearman)
	}
}

func TestAuditFamilyGuardsCorruptRows(t *testing.T) {
	// the wins-exceed-matches row drops entirely and the thin row stays
	// under the floor, so the base is e1+e2 and both get tested
	cells := []PairCount{
		{"a", "e1", 90, 100},
		{"a", "e2", 50, 100},
		{"a", "bad", 120, 100},
		{"a", "thin", 1, 2},
	}
	fam := auditFamily("matchup", cells, nil, 0.05, 30)
	if fam.Tested != 2 {
		t.Fatalf("tested %d, want 2", fam.Tested)
	}
	for _, c := range fam.Cells {
		if c.Other == "bad" || c.Other == "thin" {
			t.Fatalf("guard row %s leaked into the audit", c.Other)
		}
	}
	// single-cell heroes have no complement to test against
	fam = auditFamily("matchup", []PairCount{{"c", "e1", 50, 100}}, nil, 0.05, 30)
	if fam.Tested != 0 {
		t.Fatalf("tested %d, want 0 (no complement)", fam.Tested)
	}
}
