package mine

import (
	"database/sql"
	"math"
	"testing"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
)

func testCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../../config.json")
	if err != nil {
		t.Fatalf("load hub config: %v", err)
	}
	return cfg
}

func null(f float64) sql.NullFloat64 { return sql.NullFloat64{Float64: f, Valid: true} }
func nullInt(i int64) sql.NullInt64  { return sql.NullInt64{Int64: i, Valid: true} }

func TestCanonicalPPSigns(t *testing.T) {
	cfg := testCfg(t)
	cases := []struct {
		row  MatchupRaw
		want float64
		ok   bool
	}{
		// stratz_vs serves the hero perspective: the vs value is positive when
		// the pool hero beats the enemy (verified in-db, see canonicalPP)
		{MatchupRaw{Source: SrcStratzVS, Synergy: null(2)}, 2, true},
		{MatchupRaw{Source: SrcOpenDotaEx, WinCount: nullInt(55), Matches: 100}, 5, true},
		{MatchupRaw{Source: SrcOpenDotaEx, WinCount: nullInt(45), Matches: 100}, -5, true},
		{MatchupRaw{Source: SrcStratzVS}, 0, false},
		{MatchupRaw{Source: SrcOpenDotaEp}, 0, false},
		{MatchupRaw{Source: SrcOpenDotaEx, WinCount: nullInt(1), Matches: 0}, 0, false},
		{MatchupRaw{Source: "unknown"}, 0, false},
	}
	for _, c := range cases {
		got, ok := canonicalPP(c.row, cfg.Score.MidPct)
		if ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("canonicalPP(%+v) = %v, %v; want %v, %v", c.row, got, ok, c.want, c.ok)
		}
	}
}

// strict source priority: stratz rows are the exclusive source, backup
// labels pool only on pairs stratz never served
func TestMatchupCellsStrictSourcePriority(t *testing.T) {
	cfg := testCfg(t)
	rows := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "axe", Synergy: null(2), Matches: 60, Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "axe", WinCount: nullInt(70), Matches: 40, Source: SrcOpenDotaEx, Confidence: ConfMed},
		{PoolSlug: "marci", EnemySlug: "centaur", WinCount: nullInt(55), Matches: 0, Source: SrcOpenDotaEx, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "invoker", WinCount: nullInt(6), Matches: 10, Source: SrcOpenDotaEx, Confidence: ConfLow},
		{PoolSlug: "marci", EnemySlug: "pudge", WinCount: nullInt(55), Matches: 100, Source: SrcOpenDotaEx, Confidence: ConfHigh},
	}
	cells := MatchupCells(rows, cfg)
	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3 (matches 0 row drops)", len(cells))
	}
	axe := cells[0]
	if axe.Hero != "marci" || axe.Other != "axe" {
		t.Fatalf("first cell is %s/%s, want marci/axe", axe.Hero, axe.Other)
	}
	// stratz_vs present: the backup row on the same pair never pools in
	want := analytics.ShrinkDelta(2, 60, cfg.Shrink.MatchupAlpha)
	if axe.Value != want {
		t.Errorf("axe value = %v, want %v", axe.Value, want)
	}
	if axe.N != 60 {
		t.Errorf("axe n = %d, want 60", axe.N)
	}
	if joinSources(axe.Sources) != "stratz" {
		t.Errorf("axe sources = %s, want stratz", joinSources(axe.Sources))
	}
	if axe.Confidence != ConfHigh {
		t.Errorf("axe confidence = %s, want high", axe.Confidence)
	}
	invoker := cells[1]
	wantInv := analytics.ShrinkDelta(10, 10, cfg.Shrink.MatchupAlpha)
	if math.Abs(invoker.Value-wantInv) > 1e-9 {
		t.Errorf("invoker value = %v, want %v", invoker.Value, wantInv)
	}
	wantVar := PPVar * 10 / math.Pow(10+cfg.Shrink.MatchupAlpha, 2)
	if invoker.PostVar != wantVar {
		t.Errorf("invoker postVar = %v, want %v", invoker.PostVar, wantVar)
	}
	// no stratz row on the pair: the backup label pools alone
	pudge := cells[2]
	wantPudge := analytics.ShrinkDelta(5, 100, cfg.Shrink.MatchupAlpha)
	if math.Abs(pudge.Value-wantPudge) > 1e-9 {
		t.Errorf("pudge value = %v, want %v", pudge.Value, wantPudge)
	}
	if joinSources(pudge.Sources) != "opendota_explorer" {
		t.Errorf("pudge sources = %s, want opendota_explorer", joinSources(pudge.Sources))
	}
}

func TestSynergyCellsSymmetrizesAndWarns(t *testing.T) {
	cfg := testCfg(t)
	rows := []SynergyRaw{
		{HeroA: "marci", HeroB: "phantom-lancer", Synergy: 3, MatchCount: 100, Source: SrcStratz},
		{HeroA: "phantom-lancer", HeroB: "marci", Synergy: 1, MatchCount: 25, Source: SrcStratz},
		{HeroA: "marci", HeroB: "axe", Synergy: -2, MatchCount: 50, Source: SrcOpenDotaEx},
		{HeroA: "marci", HeroB: "ursa", Synergy: 1, MatchCount: 30, Source: SrcStratz},
		{HeroA: "marci", HeroB: "ursa", Synergy: 4, MatchCount: 70, Source: SrcOpenDotaEx},
	}
	var warns []string
	cells := SynergyCells(rows, cfg, func(subject, detail string) {
		warns = append(warns, subject+": "+detail)
	})
	if len(cells) != 6 {
		t.Fatalf("got %d cells, want 6 (three symmetric pairs)", len(cells))
	}
	fwd := analytics.ShrinkDelta(3, 100, cfg.Shrink.SynergyAlpha)
	back := analytics.ShrinkDelta(1, 25, cfg.Shrink.SynergyAlpha)
	avg := (fwd + back) / 2
	byKey := map[string]Cell{}
	for _, c := range cells {
		byKey[c.Hero+"\x00"+c.Other] = c
	}
	for _, key := range []string{"marci\x00phantom-lancer", "phantom-lancer\x00marci"} {
		if byKey[key].Value != avg {
			t.Errorf("%s value = %v, want averaged %v", key, byKey[key].Value, avg)
		}
		if byKey[key].N != 125 {
			t.Errorf("%s n = %d, want 125", key, byKey[key].N)
		}
	}
	if byKey["axe\x00marci"].Value != analytics.ShrinkDelta(-2, 50, cfg.Shrink.SynergyAlpha) {
		t.Errorf("single-direction mirror cell carries the wrong value")
	}
	// stratz present on the direction: the explorer row is backup only
	if got := byKey["marci\x00ursa"]; got.Value != analytics.ShrinkDelta(1, 30, cfg.Shrink.SynergyAlpha) || got.N != 30 {
		t.Errorf("ursa cell = %+v, want the stratz-only delta over n=30", got)
	}
	if len(warns) != 1 || warns[0][:8] != "synergy " {
		t.Fatalf("divergence warnings = %v, want one synergy divergence", warns)
	}
}

func TestMatchupPairDeltasMirrorShrinkInputs(t *testing.T) {
	cfg := testCfg(t)
	rows := []MatchupRaw{
		{PoolSlug: "marci", EnemySlug: "axe", Synergy: null(2), Matches: 60, Source: SrcStratzVS, Confidence: ConfHigh},
		{PoolSlug: "marci", EnemySlug: "axe", WinCount: nullInt(20), Matches: 40, Source: SrcOpenDotaEx, Confidence: ConfMed},
		{PoolSlug: "marci", EnemySlug: "invoker", WinCount: nullInt(45), Matches: 100, Source: SrcOpenDotaEx, Confidence: ConfLow},
	}
	pd := MatchupPairDeltas(rows, cfg)
	cells := MatchupCells(rows, cfg)
	if len(pd) != len(cells) {
		t.Fatalf("got %d pair deltas, want the %d pooled cells", len(pd), len(cells))
	}
	for i := range pd {
		if pd[i].Hero != cells[i].Hero || pd[i].Other != cells[i].Other || pd[i].N != cells[i].N {
			t.Fatalf("pair delta %d is %s/%s n=%d, want the cell %s/%s n=%d",
				i, pd[i].Hero, pd[i].Other, pd[i].N, cells[i].Hero, cells[i].Other, cells[i].N)
		}
		if want := analytics.ShrinkDelta(pd[i].Delta, float64(pd[i].N), cfg.Shrink.MatchupAlpha); cells[i].Value != want {
			t.Errorf("%s/%s shrunk %v does not recombine the pooled delta %v", pd[i].Hero, pd[i].Other, cells[i].Value, pd[i].Delta)
		}
	}
}

func TestSynergyPairDeltasPerDirection(t *testing.T) {
	rows := []SynergyRaw{
		{HeroA: "marci", HeroB: "phantom-lancer", Synergy: 3, MatchCount: 100, Source: SrcStratz},
		{HeroA: "phantom-lancer", HeroB: "marci", Synergy: 1, MatchCount: 25, Source: SrcStratz},
		{HeroA: "marci", HeroB: "axe", Synergy: -2, MatchCount: 50, Source: SrcOpenDotaEx},
	}
	pd := SynergyPairDeltas(rows)
	want := []PairDelta{
		{Hero: "marci", Other: "axe", Delta: -2, N: 50},
		{Hero: "marci", Other: "phantom-lancer", Delta: 3, N: 100},
		{Hero: "phantom-lancer", Other: "marci", Delta: 1, N: 25},
	}
	if len(pd) != len(want) {
		t.Fatalf("got %d pair deltas, want %d (one per crawled direction, keys ascending)", len(pd), len(want))
	}
	for i, w := range want {
		if pd[i] != w {
			t.Errorf("pair delta %d = %+v, want %+v", i, pd[i], w)
		}
	}
}

func TestSynergyCellsNoWarnWithoutCallback(t *testing.T) {
	cfg := testCfg(t)
	rows := []SynergyRaw{
		{HeroA: "marci", HeroB: "axe", Synergy: 1, MatchCount: 10, Source: SrcStratz},
	}
	if cells := SynergyCells(rows, cfg, nil); len(cells) != 2 {
		t.Fatalf("nil warn callback must not fail the run, got %d cells", len(cells))
	}
}
