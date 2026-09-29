package mine

import (
	"math"
	"testing"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
	"poolguide/internal/stats"
)

// screenCfg copies the hub config and flips the screen on, so every fixture
// reads the real tunables (q, minMatches, chiSqMinExpected, failAlphaMult)
// instead of restating them.
func screenCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := *testCfg(t)
	cfg.Eval.Screen.Enabled = true
	return &cfg
}

func TestEffectiveAlphasVerdicts(t *testing.T) {
	cfg := screenCfg(t)
	cells := []ScreenCell{
		{Hero: "marci", Wins: 1000, N: 2000, WinCover: true}, // neutral filler
		{Hero: "marci", Wins: 1000, N: 2000, WinCover: true}, // neutral filler
		{Hero: "marci", Wins: 80, N: 100, WinCover: true},    // 80% vs 50.7% pool: dependent
		{Hero: "marci", Wins: 101, N: 200, WinCover: true},   // 50.5% vs 50.7% pool: independent
		{Hero: "marci", Wins: 5, N: 10, WinCover: true},      // thin, no test
		{Hero: "marci", Wins: 0, N: 200, WinCover: false},    // untestable coverage
	}
	plain, fail := cfg.Shrink.MatchupAlpha, cfg.Shrink.MatchupAlpha*cfg.Eval.Screen.FailAlphaMult
	want := []float64{fail, fail, plain, fail, fail, plain}
	got := EffectiveAlphas(cells, cfg.Shrink.MatchupAlpha, cfg)
	for i, w := range want {
		if got[i] != w {
			t.Errorf("alpha[%d] = %v, want %v", i, got[i], w)
		}
	}
}

func TestEffectiveAlphasDisabledKeepsPlainAlpha(t *testing.T) {
	cfg := testCfg(t) // hub default: eval.screen.enabled false
	if cfg.Eval.Screen.Enabled {
		t.Fatal("hub config must ship with the screen disabled")
	}
	cells := []ScreenCell{
		{Hero: "marci", Wins: 80, N: 100, WinCover: true},
		{Hero: "marci", Wins: 5, N: 10, WinCover: true},
		{Hero: "marci", Wins: 0, N: 200, WinCover: false},
	}
	got := EffectiveAlphas(cells, cfg.Shrink.MatchupAlpha, cfg)
	for i, a := range got {
		if a != cfg.Shrink.MatchupAlpha {
			t.Errorf("alpha[%d] = %v, want plain %v with the screen off", i, a, cfg.Shrink.MatchupAlpha)
		}
	}
}

// The two families are corrected separately: the same cells keep their plain
// alpha when screened as their own family, and lose it when the family grows
// with another hero's cells, because the BH thresholds scale with m.
func TestEffectiveAlphasCorrectsPerFamily(t *testing.T) {
	cfg := screenCfg(t)
	familyA := []ScreenCell{
		{Hero: "axe", Wins: 62, N: 100, WinCover: true},
		{Hero: "axe", Wins: 4000, N: 8000, WinCover: true},
	}
	var familyB []ScreenCell
	for i := 0; i < 20; i++ {
		familyB = append(familyB, ScreenCell{Hero: "centaur", Wins: 50, N: 100, WinCover: true})
	}
	alpha := cfg.Shrink.MatchupAlpha
	fail := alpha * cfg.Eval.Screen.FailAlphaMult
	for i, a := range EffectiveAlphas(familyA, alpha, cfg) {
		if a != alpha {
			t.Errorf("family A alone: alpha[%d] = %v, want plain %v", i, a, alpha)
		}
	}
	pooled := EffectiveAlphas(append(append([]ScreenCell(nil), familyA...), familyB...), alpha, cfg)
	for i, a := range pooled[:len(familyA)] {
		if a != fail {
			t.Errorf("pooled family: alpha[%d] = %v, want fail %v (m grew, threshold fell)", i, a, fail)
		}
	}
}

func TestPairPRoutesToBinomialUnderExpectedGate(t *testing.T) {
	sc := screenCfg(t).Eval.Screen
	// expected counts 4.06 and 3.94 fall under the gate: exact binomial
	got := pairP(8, 8, 500, 1000, sc)
	if want := stats.BinomialTwoSided(8, 8, 0.5); got != want {
		t.Errorf("pairP under gate = %v, want binomial %v", got, want)
	}
	if math.Abs(got-2.0/256) > 1e-12 {
		t.Errorf("pairP under gate = %v, want 2/256", got)
	}
	// healthy table: chi-square p
	_, wantChi, _ := stats.ChiSquare2x2(62, 38, 4000, 4000)
	if got := pairP(62, 100, 4062, 8100, sc); got != wantChi {
		t.Errorf("pairP over gate = %v, want chi-square %v", got, wantChi)
	}
	// the configured gate drives the route, not only the baked-in stats floor
	high := sc
	high.ChiSqMinExpected = 5000
	wantBin := stats.BinomialTwoSided(62, 100, 4062.0/8100.0)
	if got := pairP(62, 100, 4062, 8100, high); got != wantBin {
		t.Errorf("pairP with raised gate = %v, want binomial %v", got, wantBin)
	}
}

func TestEffectiveAlphasDeterministic(t *testing.T) {
	cfg := screenCfg(t)
	cells := []ScreenCell{
		{Hero: "axe", Wins: 62, N: 100, WinCover: true},
		{Hero: "axe", Wins: 4000, N: 8000, WinCover: true},
		{Hero: "centaur", Wins: 101, N: 200, WinCover: true},
		{Hero: "centaur", Wins: 5, N: 10, WinCover: true},
	}
	first := EffectiveAlphas(cells, cfg.Shrink.SynergyAlpha, cfg)
	again := EffectiveAlphas(cells, cfg.Shrink.SynergyAlpha, cfg)
	byCell := map[ScreenCell]float64{}
	for i, c := range cells {
		byCell[c] = first[i]
		if first[i] != again[i] {
			t.Errorf("repeat run moved alpha[%d]: %v vs %v", i, first[i], again[i])
		}
	}
	shuffled := []ScreenCell{cells[2], cells[0], cells[3], cells[1]}
	for i, a := range EffectiveAlphas(shuffled, cfg.Shrink.SynergyAlpha, cfg) {
		if byCell[shuffled[i]] != a {
			t.Errorf("shuffled run moved verdict for %+v: %v vs %v", shuffled[i], a, byCell[shuffled[i]])
		}
	}
}

func TestMatchupCellsScreenShapesShrinkage(t *testing.T) {
	cfg := screenCfg(t)
	rows := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "filler-a", Synergy: null(0), Matches: 2000, WinCount: nullInt(1000), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "filler-b", Synergy: null(0), Matches: 2000, WinCount: nullInt(1000), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "axe", Synergy: null(-10), Matches: 100, WinCount: nullInt(80), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "centaur", Synergy: null(-2), Matches: 200, WinCount: nullInt(101), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "huskar", Synergy: null(4), Matches: 10, WinCount: nullInt(5), Source: SrcStratzVS, Confidence: ConfHigh},
	}
	plain, fail := cfg.Shrink.MatchupAlpha, cfg.Shrink.MatchupAlpha*cfg.Eval.Screen.FailAlphaMult
	want := map[string]float64{
		"filler-a": analytics.ShrinkDelta(0, 2000, fail),
		"filler-b": analytics.ShrinkDelta(0, 2000, fail),
		"axe":      analytics.ShrinkDelta(-10, 100, plain),
		"centaur":  analytics.ShrinkDelta(-2, 200, fail),
		"huskar":   analytics.ShrinkDelta(4, 10, fail),
	}
	wantVar := map[string]float64{
		"axe":     PPVar * 100 / math.Pow(100+plain, 2),
		"centaur": PPVar * 200 / math.Pow(200+fail, 2),
	}
	cells := MatchupCells(rows, cfg)
	if len(cells) != len(want) {
		t.Fatalf("got %d cells, want %d", len(cells), len(want))
	}
	for _, c := range cells {
		if c.Value != want[c.Other] {
			t.Errorf("%s value = %v, want %v", c.Other, c.Value, want[c.Other])
		}
		if v, ok := wantVar[c.Other]; ok && c.PostVar != v {
			t.Errorf("%s postVar = %v, want %v", c.Other, c.PostVar, v)
		}
	}
	// the screened independent cell pulls toward neutral
	indep := analytics.ShrinkDelta(-2, 200, plain)
	for _, c := range cells {
		if c.Other != "centaur" {
			continue
		}
		if !(math.Abs(c.Value) < math.Abs(indep)) {
			t.Errorf("screened centaur value %v must shrink below plain %v", c.Value, indep)
		}
	}
}

// Flag off: with win-bearing rows the output stays bit-identical to the plain
// shrink formula, so the wins plumbing cannot perturb today's behavior.
func TestMatchupCellsScreenOffBitIdentical(t *testing.T) {
	cfg := testCfg(t)
	rows := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "axe", Synergy: null(-10), Matches: 100, WinCount: nullInt(80), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "centaur", Synergy: null(-2), Matches: 200, WinCount: nullInt(101), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "batrider", Synergy: null(-20), Matches: 100, Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "huskar", Synergy: null(4), Matches: 10, WinCount: nullInt(5), Source: SrcStratzVS, Confidence: ConfHigh},
	}
	cells := MatchupCells(rows, cfg)
	want := map[string]float64{
		"axe":      analytics.ShrinkDelta(-10, 100, cfg.Shrink.MatchupAlpha),
		"centaur":  analytics.ShrinkDelta(-2, 200, cfg.Shrink.MatchupAlpha),
		"batrider": analytics.ShrinkDelta(-20, 100, cfg.Shrink.MatchupAlpha),
		"huskar":   analytics.ShrinkDelta(4, 10, cfg.Shrink.MatchupAlpha),
	}
	for _, c := range cells {
		if c.Value != want[c.Other] {
			t.Errorf("%s value = %v, want bit-identical %v", c.Other, c.Value, want[c.Other])
		}
	}
}

// A cell whose rows never serve win counts carries no testable table: it
// neither passes nor fails, so it keeps its plain alpha.
func TestMatchupCellsPartialCoverageUntested(t *testing.T) {
	cfg := screenCfg(t)
	rows := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "filler-a", Synergy: null(0), Matches: 2000, WinCount: nullInt(1000), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "filler-b", Synergy: null(0), Matches: 2000, WinCount: nullInt(1000), Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "batrider", Synergy: null(-20), Matches: 100, Source: SrcStratzVS, Confidence: ConfHigh},
	}
	cells := MatchupCells(rows, cfg)
	var bat Cell
	for _, c := range cells {
		if c.Other == "batrider" {
			bat = c
		}
	}
	if bat.Hero == "" {
		t.Fatal("batrider cell missing")
	}
	if want := analytics.ShrinkDelta(-20, 100, cfg.Shrink.MatchupAlpha); bat.Value != want {
		t.Errorf("untestable cell value = %v, want plain %v", bat.Value, want)
	}
}

func TestSynergyCellsScreenShapesShrinkage(t *testing.T) {
	cfg := screenCfg(t)
	rows := []SynergyRaw{
		{HeroA: "marci", HeroB: "phantom-lancer", Synergy: 3, MatchCount: 50, WinCount: nullInt(40), Source: SrcStratz},
		{HeroA: "marci", HeroB: "axe", Synergy: 2, MatchCount: 100, WinCount: nullInt(50), Source: SrcStratz},
		{HeroA: "marci", HeroB: "centaur", Synergy: 2, MatchCount: 100, WinCount: nullInt(50), Source: SrcStratz},
	}
	plain, fail := cfg.Shrink.SynergyAlpha, cfg.Shrink.SynergyAlpha*cfg.Eval.Screen.FailAlphaMult
	want := map[string]float64{
		// 80% vs marci's 56% pool survives BH: plain alpha
		"marci\x00phantom-lancer": analytics.ShrinkDelta(3, 50, plain),
		// neutral cells fail; the phantom-lancer mirror is its hero's only
		// cell, so it tests against itself and fails too
		"marci\x00axe":            analytics.ShrinkDelta(2, 100, fail),
		"marci\x00centaur":        analytics.ShrinkDelta(2, 100, fail),
		"phantom-lancer\x00marci": analytics.ShrinkDelta(3, 50, fail),
		"axe\x00marci":            analytics.ShrinkDelta(2, 100, fail),
		"centaur\x00marci":        analytics.ShrinkDelta(2, 100, fail),
	}
	cells := SynergyCells(rows, cfg, nil)
	if len(cells) != len(want) {
		t.Fatalf("got %d cells, want %d", len(cells), len(want))
	}
	for _, c := range cells {
		key := c.Hero + "\x00" + c.Other
		if c.Value != want[key] {
			t.Errorf("%s value = %v, want %v", key, c.Value, want[key])
		}
	}
}
