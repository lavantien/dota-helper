package emit

import (
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/mine"
)

func TestFormatGuideGolden(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadGuide(db, cfg)
	if err != nil {
		t.Fatalf("load guide: %v", err)
	}
	in.Date = fixtureDay
	got, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatalf("format guide: %v", err)
	}
	checkGolden(t, "guide.golden", got)
}

// the fixture roster npcs carry the full npc prefix, so this proves the
// iconBySlug values are prefix-stripped for the cdn url scheme.
func TestFormatGuideIconNamesAreShortNPC(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadGuide(db, cfg)
	if err != nil {
		t.Fatalf("load guide: %v", err)
	}
	in.Date = fixtureDay
	got, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatalf("format guide: %v", err)
	}
	if strings.Contains(string(got), config.NPCPrefix) {
		t.Fatalf("guide iconBySlug leaks the npc prefix in icon names")
	}
}

// backup-backed kept rows must surface in the hero's source note instead of
// riding under the stratz_vs stamp; a backup row under the cut counts nothing
func TestFormatGuideCountsBackupRowsInNote(t *testing.T) {
	cfg := fixtureCfg(t)
	in := GuideInput{
		Date:    fixtureDay,
		Roster:  []RosterHero{{ID: 1, Slug: "axe", Name: "Axe", NPC: "axe"}},
		PriorWR: poolPriorWR(cfg),
		Rows: map[string][]GuideRow{
			"marci": {
				{Enemy: "axe", Dis: 9, Src: mine.SrcStratz},
				{Enemy: "centaur", Dis: cfg.Thresholds.DisCut, Src: mine.SrcStratz},
				{Enemy: "pudge", Dis: 8, Src: mine.SrcOpenDotaEx},
			},
			"abaddon": {
				{Enemy: "axe", Dis: 9, Src: mine.SrcStratz},
				{Enemy: "centaur", Dis: cfg.Thresholds.DisCut, Src: mine.SrcStratz},
				{Enemy: "pudge", Dis: 8, Src: mine.SrcStratz},
				{Enemy: "sniper", Dis: 0.2, Src: mine.SrcOpenDotaEx},
			},
		},
		Scraped: map[string]string{"marci": fixtureDay, "abaddon": fixtureDay},
	}
	got, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "'marci': 'stratz_vs crawl " + fixtureDay + " (+1 opendota_explorer)'"
	if !strings.Contains(string(got), want) {
		t.Fatalf("note must count backup-backed kept rows, want %q in:\n%s", want, got)
	}
	plain := "'abaddon': 'stratz_vs crawl " + fixtureDay + "'"
	if !strings.Contains(string(got), plain) {
		t.Fatalf("hero with no backup-backed kept rows keeps the plain note, want %q in:\n%s", plain, got)
	}
}

func TestFormatGuideDeterministic(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadGuide(db, cfg)
	if err != nil {
		t.Fatalf("load guide: %v", err)
	}
	in.Date = fixtureDay
	a, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatalf("first format: %v", err)
	}
	b, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatalf("second format: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("guide output is not deterministic")
	}
}

// poolPriorWR covers every pool slug with a stub win rate, minus any dropped.
func poolPriorWR(cfg *config.Config, drop ...string) map[string]float64 {
	out := map[string]float64{}
	for _, s := range cfg.PoolSlugs() {
		skip := false
		for _, d := range drop {
			if s == d {
				skip = true
			}
		}
		if !skip {
			out[s] = 0.5
		}
	}
	return out
}

func TestFormatGuideMinKeptFails(t *testing.T) {
	cfg := fixtureCfg(t)
	in := GuideInput{
		Date:    fixtureDay,
		Roster:  []RosterHero{{ID: 1, Slug: "axe", Name: "Axe", NPC: "axe"}},
		PriorWR: poolPriorWR(cfg),
		Rows: map[string][]GuideRow{
			"marci": {
				{Enemy: "axe", Dis: 9},
				{Enemy: "centaur", Dis: cfg.Thresholds.DisCut - 0.5},
			},
		},
		Scraped: map[string]string{"marci": fixtureDay},
	}
	if _, err := FormatGuide(in, cfg); err == nil {
		t.Fatalf("guide with fewer than minKept kept rows must fail")
	} else if !strings.Contains(err.Error(), "minKept") && !strings.Contains(err.Error(), "want >=") {
		t.Fatalf("unexpected error shape: %v", err)
	}
}

func TestFormatGuidePoolWinRatesCovered(t *testing.T) {
	cfg := fixtureCfg(t)
	hero := cfg.PoolSlugs()[0]
	in := GuideInput{
		Date:    fixtureDay,
		Roster:  []RosterHero{{ID: 1, Slug: "axe", Name: "Axe", NPC: "axe"}},
		PriorWR: poolPriorWR(cfg),
		Rows: map[string][]GuideRow{
			"marci": {
				{Enemy: "axe", Dis: 9},
				{Enemy: "centaur", Dis: 4},
				{Enemy: "invoker", Dis: 2},
			},
		},
		Scraped: map[string]string{"marci": fixtureDay},
	}
	out, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatalf("format guide: %v", err)
	}
	// the pool hero carries no rows, its win rate must still ship in overallWr
	if n := strings.Count(string(out), "'"+hero+"'"); n != 1 {
		t.Fatalf("pool hero without rows must appear exactly once (overallWr), got %d", n)
	}
}

func TestFormatGuidePoolWinRateMissingFails(t *testing.T) {
	cfg := fixtureCfg(t)
	hero := cfg.PoolSlugs()[0]
	in := GuideInput{
		Date:    fixtureDay,
		Roster:  []RosterHero{{ID: 1, Slug: "axe", Name: "Axe", NPC: "axe"}},
		PriorWR: poolPriorWR(cfg, hero),
		Rows: map[string][]GuideRow{
			"marci": {
				{Enemy: "axe", Dis: 9},
				{Enemy: "centaur", Dis: 4},
				{Enemy: "invoker", Dis: 2},
			},
		},
		Scraped: map[string]string{"marci": fixtureDay},
	}
	if _, err := FormatGuide(in, cfg); err == nil || !strings.Contains(err.Error(), hero) || !strings.Contains(err.Error(), "overall win rate") {
		t.Fatalf("pool hero without a mined win rate must fail naming the hero, got %v", err)
	}
}

func TestFormatGuideMissingWinRateFails(t *testing.T) {
	cfg := fixtureCfg(t)
	hero := cfg.PoolSlugs()[0]
	in := GuideInput{
		Date:    fixtureDay,
		Roster:  []RosterHero{{ID: 1, Slug: "axe", Name: "Axe", NPC: "axe"}},
		PriorWR: poolPriorWR(cfg, hero),
		Rows: map[string][]GuideRow{
			hero: {
				{Enemy: "axe", Dis: 9},
				{Enemy: "centaur", Dis: 4},
				{Enemy: "invoker", Dis: 2},
			},
		},
		Scraped: map[string]string{hero: fixtureDay},
	}
	if _, err := FormatGuide(in, cfg); err == nil || !strings.Contains(err.Error(), "overall win rate") {
		t.Fatalf("guide hero without a mined win rate must fail, got %v", err)
	}
}
