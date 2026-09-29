package mine

import (
	"testing"
)

// canonSource folds the raw crawl labels onto the picker vocabulary and
// passes unknown labels through untouched, so a retired or future source
// stays visible in the persisted source list instead of masquerading.
func TestCanonSourceIdentityFallback(t *testing.T) {
	cases := map[string]string{
		SrcStratzVS:   SrcStratz,
		SrcStratz:     SrcStratz,
		SrcOpenDotaEx: SrcOpenDotaEx,
		SrcOpenDotaEp: SrcOpenDotaEx,
		"dotabuff":    "dotabuff",
		"future_src":  "future_src",
	}
	for in, want := range cases {
		if got := canonSource(in); got != want {
			t.Errorf("canonSource(%q) = %q, want %q", in, got, want)
		}
	}
}

// a direction crawled with zero matches still pools as an observation with
// delta 0 rather than dividing by zero
func TestSynergyPairDeltasZeroMatchDirection(t *testing.T) {
	rows := []SynergyRaw{
		{HeroA: "axe", HeroB: "marci", Synergy: 5, MatchCount: 0, Source: SrcStratz},
	}
	pd := SynergyPairDeltas(rows)
	want := []PairDelta{{Hero: "axe", Other: "marci", Delta: 0, N: 0}}
	if len(pd) != 1 || pd[0] != want[0] {
		t.Fatalf("pair deltas = %+v, want %+v (zero matches reads delta 0)", pd, want)
	}
}

// rows that pass the match floor but carry no usable canonical value are
// dropped before pooling: stratz_vs without a synergy value and explorer
// rows without a win count
func TestMatchupCellsDropsUncanonicalRows(t *testing.T) {
	cfg := testCfg(t)
	rows := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "axe", Synergy: null(2), Matches: 60, Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "centaur", Matches: 100, Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "ursa", Matches: 100, Source: SrcOpenDotaEx, Confidence: ConfHigh},
	}
	cells := MatchupCells(rows, cfg)
	if len(cells) != 1 || cells[0].Other != "axe" {
		t.Fatalf("cells = %+v, want only the axe cell (uncanonical rows drop)", cells)
	}
}

// within one crawled direction the strict source tier applies in list order:
// a backup row listed before a stratz row must not keep its tier
func TestSynergyTierImprovesWithinDirection(t *testing.T) {
	rows := []SynergyRaw{
		{HeroA: "axe", HeroB: "marci", Synergy: -2, MatchCount: 50, Source: SrcOpenDotaEx},
		{HeroA: "axe", HeroB: "marci", Synergy: 3, MatchCount: 100, Source: SrcStratz},
	}
	pd := SynergyPairDeltas(rows)
	want := []PairDelta{{Hero: "axe", Other: "marci", Delta: 3, N: 100}}
	if len(pd) != 1 || pd[0] != want[0] {
		t.Fatalf("pair deltas = %+v, want %+v (stratz listed second still wins the tier)", pd, want)
	}
	cells := SynergyCells(rows, testCfg(t), nil)
	byKey := map[string]Cell{}
	for _, c := range cells {
		byKey[c.Hero+"\x00"+c.Other] = c
	}
	if got := joinSources(byKey["axe\x00marci"].Sources); got != "stratz" {
		t.Errorf("sources = %s, want stratz only", got)
	}
}

// cutKey splits on the nul separator and falls back to the whole key as the
// hero half when no separator survives
func TestCutKeyWithoutSeparator(t *testing.T) {
	cases := []struct {
		key   string
		hero  string
		other string
	}{
		{"a\x00b", "a", "b"},
		{"\x00b", "", "b"},
		{"solo", "solo", ""},
	}
	for _, c := range cases {
		hero, other := cutKey(c.key)
		if hero != c.hero || other != c.other {
			t.Errorf("cutKey(%q) = %q, %q; want %q, %q", c.key, hero, other, c.hero, c.other)
		}
	}
}

// off-bracket rows and non-positive pick counts never enter the popularity
// vector: the in-scope total excludes them entirely
func TestPopularitySkipsOutOfScopeAndNonPositivePicks(t *testing.T) {
	cfg := testCfg(t)
	stats := []HeroStat{
		{Slug: "axe", Bracket: cfg.Scope.Bracket, Picks: 100, Wins: 50},
		{Slug: "centaur", Bracket: "HERALD_GUARDIAN", Picks: 900, Wins: 450},
		{Slug: "ursa", Bracket: cfg.Scope.Bracket, Picks: 0, Wins: 0},
		{Slug: "phantom-lancer", Bracket: cfg.Scope.Bracket, Picks: -5, Wins: 0},
	}
	shares, usedFallback := Popularity(stats, []string{"axe", "centaur", "ursa", "phantom-lancer"}, nil, cfg)
	if usedFallback {
		t.Fatal("out-of-scope rows must not trigger the fallback flag")
	}
	if len(shares) != 1 || shares["axe"] != 1 {
		t.Fatalf("shares = %v, want axe alone at 1.0", shares)
	}
}
