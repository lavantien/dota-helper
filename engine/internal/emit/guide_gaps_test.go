package emit

import (
	"strings"
	"testing"
)

// heroes with a trend cell ship it as a [wrDelta, shareDelta] pair under the
// guide's trend block; heroes without one are simply absent.
func TestFormatGuideTrendSection(t *testing.T) {
	cfg := fixtureCfg(t)
	in := GuideInput{
		Date:    fixtureDay,
		Roster:  []RosterHero{{ID: 1, Slug: "axe", Name: "Axe", NPC: "axe"}},
		PriorWR: poolPriorWR(cfg),
		Rows: map[string][]GuideRow{
			"marci": {
				{Enemy: "axe", Dis: 9},
				{Enemy: "centaur", Dis: 4},
			},
		},
		Scraped: map[string]string{"marci": fixtureDay},
		Trend: map[string]TrendCell{
			"marci":  {WrDeltaPP: 1.25, ShareDeltaPP: -0.5, FromDate: "2026-09-20", ToDate: "2026-09-27"},
			"pudge":  {WrDeltaPP: 2, ShareDeltaPP: 0.75},
			"enigma": {},
		},
		TrendWindow: "2026-09-20 to 2026-09-27",
	}
	out, err := FormatGuide(in, cfg)
	if err != nil {
		t.Fatalf("format guide: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "    'marci': [1.25, -0.5],\n") {
		t.Fatalf("guide trend block misses the marci pair:\n%s", s)
	}
	// only heroes with mined coverage appear in the trend block
	if strings.Contains(s, "'pudge': [") || strings.Contains(s, "'enigma': [") {
		t.Fatalf("trend block must only list heroes with matchup rows:\n%s", s)
	}
	if !strings.Contains(s, "trendWindow: '2026-09-20 to 2026-09-27',") {
		t.Fatalf("guide trend window missing:\n%s", s)
	}
}

// TestLoadGuideQueryErrorArms drops one table at a time so each loader query's
// error return fires against a real duckdb.
func TestLoadGuideQueryErrorArms(t *testing.T) {
	cfg := fixtureCfg(t)
	tests := []struct {
		drop string
		want string
	}{
		{"hero_prior", "hero_prior:"},
		{"hero_trend_delta", "hero_trend_delta:"},
		{"norm_cell", "norm_cell:"},
		{"matchup_raw", "matchup_raw:"},
	}
	for _, tt := range tests {
		t.Run("missing "+tt.drop, func(t *testing.T) {
			db := bareDB(t)
			execFixture(t, db, "DROP TABLE "+tt.drop)
			if _, err := LoadGuide(db, cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("load guide must fail on a missing %s table, got %v", tt.drop, err)
			}
		})
	}
	db := bareDB(t)
	db.Close()
	if _, err := LoadGuide(db, cfg); err == nil || !strings.Contains(err.Error(), "hero_roster:") {
		t.Fatalf("load guide must fail over a closed db, got %v", err)
	}
}

// the trend window is the lexicographically latest 'from to' pair across the
// loaded rows, so stale snapshots never win.
func TestLoadGuideTrendWindow(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	execFixture(t, db, `INSERT INTO hero_trend_delta (slug, wr_delta_pp, share_delta_pp, from_date, to_date)
		VALUES ('axe', 1.5, -0.25, '2026-09-20', '2026-09-27')`)
	execFixture(t, db, `INSERT INTO hero_trend_delta (slug, wr_delta_pp, share_delta_pp, from_date, to_date)
		VALUES ('pudge', -2, 0.75, '2026-09-13', '2026-09-20')`)
	in, err := LoadGuide(db, cfg)
	if err != nil {
		t.Fatalf("load guide: %v", err)
	}
	if in.TrendWindow != "2026-09-20 to 2026-09-27" {
		t.Fatalf("trend window = %q, want the latest pair", in.TrendWindow)
	}
	if in.Trend["pudge"].WrDeltaPP != -2 || in.Trend["axe"].ShareDeltaPP != -0.25 {
		t.Fatalf("trend cells = %+v", in.Trend)
	}
}
