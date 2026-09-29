package emit

import (
	"strings"
	"testing"

	"poolguide/internal/gates"
)

// smallInput carries the minimum a one-hero pool needs to format; every case
// below breaks exactly one thing.
func smallInput(pool0 string) PickerInput {
	roster := []RosterHero{{ID: 1, Slug: pool0, Name: pool0, NPC: pool0}}
	cell := map[string]PickerCell{pool0: {Pct: 0.5}}
	return PickerInput{
		Date:       fixtureDay,
		Roster:     roster,
		Pop:        map[string]float64{pool0: 1},
		Mu:         map[string]map[string]PickerCell{pool0: cell},
		Syn:        map[string]map[string]PickerCell{pool0: cell},
		Prior:      map[string]float64{pool0: 1},
		HeroSource: map[string]string{},
		Prose:      map[string]ProseHero{pool0: {}},
		Gates:      &gates.Doc{},
	}
}

func TestFormatPickerRequiresGates(t *testing.T) {
	cfg := fixtureCfg(t)
	in := smallInput(cfg.PoolSlugs()[0])
	in.Gates = nil
	if _, err := FormatPicker(in, cfg); err == nil ||
		!strings.Contains(err.Error(), "requires a gates document") {
		t.Fatalf("format picker must require a gates document, got %v", err)
	}
}

func TestFormatPickerMissingPopularityFails(t *testing.T) {
	cfg := fixtureCfg(t)
	in := smallInput(cfg.PoolSlugs()[0])
	delete(in.Pop, cfg.PoolSlugs()[0])
	if _, err := FormatPicker(in, cfg); err == nil ||
		!strings.Contains(err.Error(), "no popularity for roster hero "+cfg.PoolSlugs()[0]) {
		t.Fatalf("format picker must fail on a roster hero without popularity, got %v", err)
	}
}

// the roster must cover every pool hero: with a one-hero roster and the real
// hub pool, the second pool slug is the first to come up missing.
func TestFormatPickerPoolHeroOutsideRosterFails(t *testing.T) {
	cfg := fixtureCfg(t)
	in := smallInput(cfg.PoolSlugs()[0])
	if _, err := FormatPicker(in, cfg); err == nil ||
		!strings.Contains(err.Error(), "missing from roster") {
		t.Fatalf("format picker must fail on a pool hero missing from the roster, got %v", err)
	}
}

func TestFormatPickerMissingPriorFails(t *testing.T) {
	cfg := fixtureCfg(t)
	cfg.Pool = cfg.Pool[:1] // the single roster hero is the whole pool
	in := smallInput(cfg.PoolSlugs()[0])
	delete(in.Prior, cfg.PoolSlugs()[0])
	if _, err := FormatPicker(in, cfg); err == nil ||
		!strings.Contains(err.Error(), "no prior for pool hero "+cfg.PoolSlugs()[0]) {
		t.Fatalf("format picker must fail on a pool hero without a prior, got %v", err)
	}
}

// a pool hero whose syn row never loaded fails under the syn prefix, distinct
// from the mu row error the missing-cell case raises.
func TestFormatPickerSynRowErrorFails(t *testing.T) {
	cfg := fixtureCfg(t)
	cfg.Pool = cfg.Pool[:1]
	slug := cfg.PoolSlugs()[0]
	in := smallInput(slug)
	in.Syn[slug] = nil
	if _, err := FormatPicker(in, cfg); err == nil ||
		!strings.Contains(err.Error(), "syn "+slug+": no cells") {
		t.Fatalf("format picker must fail on a missing syn row, got %v", err)
	}
}

func TestPackedRowArms(t *testing.T) {
	idxOf := map[string]int{"hero-a": 0}
	if _, err := packedRow(nil, idxOf, 1, 4); err == nil || err.Error() != "no cells" {
		t.Fatalf("packedRow(nil) = %v, want the no-cells error", err)
	}
	if _, err := packedRow(map[string]PickerCell{}, idxOf, 1, 4); err == nil ||
		!strings.Contains(err.Error(), "no cell for roster hero hero-a") {
		t.Fatalf("packedRow over an empty cell map = %v, want the missing-cell error", err)
	}
	row, err := packedRow(map[string]PickerCell{"hero-a": {Pct: 0.5, Conf: 2}}, idxOf, 1, 4)
	if err != nil || row != "0:0.5000:2" {
		t.Fatalf("packedRow = %q, %v", row, err)
	}
}

// TestLoadPickerQueryErrorArms drops one derived table at a time so every
// loader query's error return fires against a real duckdb.
func TestLoadPickerQueryErrorArms(t *testing.T) {
	cfg := fixtureCfg(t)
	doc := &gates.Doc{}
	tests := []struct {
		drop string
		want string
	}{
		{"popularity", "popularity:"},
		{"hero_prior", "hero_prior:"},
		{"hero_trend_delta", "hero_trend_delta:"},
		{"hero_position", "hero_position:"},
		{"norm_cell", "norm_cell:"},
	}
	for _, tt := range tests {
		t.Run("missing "+tt.drop, func(t *testing.T) {
			db := bareDB(t)
			execFixture(t, db, "DROP TABLE "+tt.drop)
			_, err := LoadPicker(db, cfg, doc)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("load picker must fail on a missing %s table, got %v", tt.drop, err)
			}
		})
	}
	db := bareDB(t)
	db.Close()
	if _, err := LoadPicker(db, cfg, doc); err == nil || !strings.Contains(err.Error(), "hero_roster:") {
		t.Fatalf("load picker must fail over a closed db, got %v", err)
	}
}

// the trend loader fills the latest-pair cells verbatim; the picker payload's
// trend lines come straight from them.
func TestLoadPickerTrendRows(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	execFixture(t, db, `INSERT INTO hero_trend_delta (slug, wr_delta_pp, share_delta_pp, from_date, to_date)
		VALUES ('axe', 1.5, -0.25, '2026-09-20', '2026-09-27')`)
	in, err := LoadPicker(db, cfg, &gates.Doc{})
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	td, ok := in.Trend["axe"]
	if !ok || td.WrDeltaPP != 1.5 || td.ShareDeltaPP != -0.25 || td.FromDate != "2026-09-20" || td.ToDate != "2026-09-27" {
		t.Fatalf("trend cell = %+v, ok = %v", td, ok)
	}
}

// heroPos aggregation: per-hero shares of the hero's own picks, share-desc
// with position ascending on ties, zero-pick rows and zero-total heroes out.
func TestLoadPickerHeroPosAggregation(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	for _, r := range [][4]any{{"axe", 1, 600, 320}, {"axe", 2, 300, 150}, {"axe", 3, 100, 40},
		{"axe", 5, 0, 0}, {"pudge", 4, 100, 55}, {"huskar", 2, 0, 0}} {
		execFixture(t, db, `INSERT INTO hero_position (slug, position, pick_count, win_count, source, scrape_date)
			VALUES (?, ?, ?, ?, 'stratz_winday', ?)`, r[0], r[1], r[2], r[3], fixtureDay)
	}
	in, err := LoadPicker(db, cfg, &gates.Doc{})
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	got := in.HeroPos["axe"]
	if len(got) != 3 {
		t.Fatalf("axe heroPos = %+v, want the three positive-pick rows", got)
	}
	if got[0].Position != 1 || got[0].Share != 0.6 || got[1].Position != 2 || got[2].Position != 3 {
		t.Fatalf("axe heroPos not share-descending: %+v", got)
	}
	if got[0].WR != 320.0/600.0 {
		t.Fatalf("axe position-1 wr = %v", got[0].WR)
	}
	if _, ok := in.HeroPos["huskar"]; ok {
		t.Fatal("hero with only zero-pick rows must be absent from heroPos")
	}
	if len(in.HeroPos["pudge"]) != 1 {
		t.Fatalf("pudge heroPos = %+v", in.HeroPos["pudge"])
	}
}

// equal shares break the tie on position ascending, the display order the
// guide page pins.
func TestLoadPickerHeroPosShareTieBreaksOnPosition(t *testing.T) {
	cfg := fixtureCfg(t)
	db := bareDB(t)
	for _, r := range [][4]int{{2, 50, 25}, {1, 50, 20}} {
		execFixture(t, db, `INSERT INTO hero_position (slug, position, pick_count, win_count, source, scrape_date)
			VALUES ('juggernaut', ?, ?, ?, 'stratz_winday', ?)`, r[0], r[1], r[2], fixtureDay)
	}
	in, err := LoadPicker(db, cfg, &gates.Doc{})
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	got := in.HeroPos["juggernaut"]
	if len(got) != 2 || got[0].Position != 1 || got[1].Position != 2 {
		t.Fatalf("tied shares must order position ascending, got %+v", got)
	}
}
