package mine

import (
	"math"
	"testing"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
)

func TestOverallWRPrimaryVsRowsAndPrior(t *testing.T) {
	cfg := testCfg(t)
	matchups := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "axe", WinCount: nullInt(550), Matches: 1000, Source: SrcStratzVS},
		{PoolSlug: "marci", EnemySlug: "centaur", WinCount: nullInt(520), Matches: 1000, Source: SrcStratzVS},
		// non-stratz_vs rows carry no win counts here and must not contribute
		{PoolSlug: "marci", EnemySlug: "axe", Dis: null(9), Matches: 9999, Source: SrcOpenDotaEp},
	}
	wrs := OverallWR(matchups, []string{"marci", "phantom-lancer", "clinkz"}, cfg)
	slots := float64(cfg.Score.EnemySlots)
	wantMarci := analytics.Shrink((550+520)/slots, 2000/slots, cfg.Shrink.OverallWrAlpha, wrPrior)
	if wrs["marci"] != wantMarci {
		t.Errorf("marci wr = %v, want %v", wrs["marci"], wantMarci)
	}
	// no vs rows: the 0.5 prior, the opendota snapshot wr never fills (ALL scope)
	if wrs["phantom-lancer"] != wrPrior || wrs["clinkz"] != wrPrior {
		t.Errorf("heroes without vs rows must read the prior, got %v %v",
			wrs["phantom-lancer"], wrs["clinkz"])
	}
}

func TestPriorsArePureWinRateZ(t *testing.T) {
	cfg := testCfg(t)
	pool := cfg.PoolSlugs()
	wrs := map[string]float64{}
	for i, slug := range pool {
		wrs[slug] = 0.5 + float64(i)*0.001
	}
	got := Priors(wrs, cfg)
	// recompute the expectation through the same public analytics path,
	// tier labels must not enter the score anywhere
	vals := make([]float64, len(pool))
	for i, slug := range pool {
		vals[i] = wrs[slug]
	}
	want := analytics.Prior(vals, cfg)
	for i, slug := range pool {
		if got[slug] != want[i] {
			t.Errorf("prior %s = %v, want %v", slug, got[slug], want[i])
		}
	}
}

// the frozen tier principle, pinned end to end through the real config hub:
// flipping every tier label must not move a single prior
func TestPriorsIgnoreTierLabels(t *testing.T) {
	cfg, err := config.Load("../../../config.json")
	if err != nil {
		t.Fatal(err)
	}
	pool := cfg.PoolSlugs()
	wrs := map[string]float64{}
	for i, slug := range pool {
		wrs[slug] = 0.42 + float64(i)*0.003
	}
	base := Priors(wrs, cfg)
	for i := range cfg.Pool {
		if cfg.Pool[i].Tier == "dedicated" {
			cfg.Pool[i].Tier = "flex"
		} else {
			cfg.Pool[i].Tier = "dedicated"
		}
	}
	flipped := Priors(wrs, cfg)
	for slug := range base {
		if base[slug] != flipped[slug] {
			t.Fatalf("prior for %s moved with a tier flip: %v vs %v", slug, base[slug], flipped[slug])
		}
	}
}

func TestPopularityNormalizesOverRoster(t *testing.T) {
	cfg := testCfg(t)
	stats := []HeroStat{
		{Slug: "marci", Bracket: cfg.Scope.Bracket, Picks: 300, Wins: 150},
		{Slug: "axe", Bracket: cfg.Scope.Bracket, Picks: 100, Wins: 50},
	}
	roster := []string{"marci", "axe", "centaur"}
	fallback := map[string]float64{"centaur": 0.05}
	shares, usedFallback := Popularity(stats, roster, fallback, cfg)
	if !usedFallback {
		t.Fatalf("fallback must be flagged when a roster hero has no stats")
	}
	sum := 0.0
	for _, slug := range roster {
		sum += shares[slug]
	}
	if math.Abs(sum-1) > 1e-12 {
		t.Fatalf("roster shares sum to %v, want 1", sum)
	}
	if shares["marci"] <= shares["axe"] || shares["axe"] <= shares["centaur"] {
		t.Fatalf("shares must preserve pick order: %v", shares)
	}
	if label := popSourceLabel(usedFallback); label != "stratz_raw_aggregate" {
		t.Errorf("fallback source label = %s", label)
	}
	if label := popSourceLabel(false); label != "hero_stats" {
		t.Errorf("primary source label = %s", label)
	}
}

func TestPopularityNoFallback(t *testing.T) {
	cfg := testCfg(t)
	stats := []HeroStat{{Slug: "a", Bracket: cfg.Scope.Bracket, Picks: 3, Wins: 1}}
	shares, usedFallback := Popularity(stats, []string{"a"}, nil, cfg)
	if usedFallback || shares["a"] != 1 {
		t.Fatalf("single-hero roster must fully own the share, got %v %v", shares, usedFallback)
	}
}

func TestNormalizeMatrixCompletesAndRanks(t *testing.T) {
	cfg := testCfg(t)
	pool := []string{"p1", "p2"}
	roster := []string{"e1", "e2", "e3", "e4"}
	thick := int64(cfg.Thresholds.ThinCellMatches) + 1
	cells := []Cell{
		{Hero: "p1", Other: "e1", Value: 5, N: thick},
		{Hero: "p1", Other: "e2", Value: -5, N: thick},
		{Hero: "p1", Other: "e3", Value: 9, N: 1}, // thin, refilled
		{Hero: "p2", Other: "e1", Value: 1, N: thick},
		{Hero: "p2", Other: "e4", Value: -1, N: thick},
	}
	got := NormalizeMatrix(pool, roster, cells, KindEnemy, cfg)
	if len(got) != len(pool)*len(roster) {
		t.Fatalf("got %d cells, want every pool x roster cell (%d)", len(got), len(pool)*len(roster))
	}
	byKey := map[string]NormCell{}
	for _, c := range got {
		byKey[c.Hero+"\x00"+c.Other] = c
		if c.Kind != KindEnemy || c.Pct == 0 || math.IsNaN(c.Z) {
			t.Fatalf("malformed cell %+v", c)
		}
	}
	if byKey["p1\x00e1"].Completed {
		t.Errorf("observed thick cell must not be flagged completed")
	}
	if !byKey["p1\x00e3"].Completed {
		t.Errorf("thin cell must be refilled and flagged completed")
	}
	if !byKey["p1\x00e4"].Completed {
		t.Errorf("missing cell must be completed")
	}
	if !byKey["p2\x00e2"].Completed || !byKey["p2\x00e3"].Completed {
		t.Errorf("p2 gaps must be completed")
	}
	// midrank over 4 values: percentiles are i/4 steps
	for _, c := range got {
		if c.Pct <= 0 || c.Pct >= 1 {
			t.Fatalf("pct %v outside (0,1)", c.Pct)
		}
	}
	again := NormalizeMatrix(pool, roster, cells, KindEnemy, cfg)
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("normalization is not deterministic at %+v vs %+v", got[i], again[i])
		}
	}
}
