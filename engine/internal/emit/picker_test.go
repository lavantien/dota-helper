package emit

import (
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/gates"
)

func TestFormatPickerGolden(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	in.Prose = prose.Heroes
	in.Date = fixtureDay
	got, err := FormatPicker(in, cfg)
	if err != nil {
		t.Fatalf("format picker: %v", err)
	}
	checkGolden(t, "picker.golden", got)
}

// the fixture roster npcs carry the full npc prefix, so this proves the icon
// names are prefix-stripped: the steam cdn 404s on npc_dota_hero_*.png.
func TestFormatPickerIconNamesAreShortNPC(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	in.Prose = prose.Heroes
	in.Date = fixtureDay
	got, err := FormatPicker(in, cfg)
	if err != nil {
		t.Fatalf("format picker: %v", err)
	}
	if strings.Contains(string(got), config.NPCPrefix) {
		t.Fatalf("picker output leaks the npc prefix in icon names")
	}
}

// the cell conf rank must come from the mined confidence label, not the
// source label: fixture hero zero carries backup-tier sources with cycling
// high/medium/low confidence, so its mu cells read 0/1/2 instead of the
// source-label default 2
func TestLoadPickerConfReadsConfidence(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	slugs := fixtureRosterSlugs(t, cfg)
	first := cfg.PoolSlugs()[0]
	// fixture: confidence label (0+j)%3, completed j%5==0, so j=0 reads the
	// completed override 2 and j=1..3 read the label ranks 1, 2, 0
	for _, tt := range []struct{ j, want int }{{0, 2}, {1, 1}, {2, 2}, {3, 0}} {
		cell, ok := in.Mu[first][slugs[tt.j]]
		if !ok {
			t.Fatalf("mu cell %s vs %s missing", first, slugs[tt.j])
		}
		if cell.Conf != tt.want {
			t.Errorf("mu[%s][%s].conf = %d, want %d", first, slugs[tt.j], cell.Conf, tt.want)
		}
	}
}

// heroPos is display-only: per pool hero the position stats land as
// [position, share, wr] triples sorted share-descending, and heroes without
// position rows carry an empty array
func TestFormatPickerHeroPosBlock(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	in.Prose = prose.Heroes
	in.Date = fixtureDay
	out, err := FormatPicker(in, cfg)
	if err != nil {
		t.Fatalf("format picker: %v", err)
	}
	s := string(out)
	first := cfg.PoolSlugs()[0]
	if !strings.Contains(s, "heroPos: [") {
		t.Fatal("payload missing the heroPos block")
	}
	// hero one: shares 0.6/0.3/0.1, wrs 0.5333/0.5/0.4 at 4 decimals
	if !strings.Contains(s, "[1, 0.6000, 0.5333], [2, 0.3000, 0.5000], [3, 0.1000, 0.4000]") {
		t.Fatal("heroPos stats not share-sorted or wrong decimals")
	}
	if in.HeroPos[first][0].Position != 1 || in.HeroPos[first][0].Share != 0.6 {
		t.Fatalf("loaded stats wrong: %+v", in.HeroPos[first])
	}
}

func TestFormatPickerDeterministic(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	in.Prose = prose.Heroes
	in.Date = fixtureDay
	a, err := FormatPicker(in, cfg)
	if err != nil {
		t.Fatalf("first format: %v", err)
	}
	b, err := FormatPicker(in, cfg)
	if err != nil {
		t.Fatalf("second format: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("picker output is not deterministic")
	}
}

func TestFormatPickerMissingProseFails(t *testing.T) {
	cfg := fixtureCfg(t)
	pool := cfg.PoolSlugs()
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	delete(prose.Heroes, pool[0])
	in := PickerInput{
		Date:       fixtureDay,
		Roster:     []RosterHero{{ID: 1, Slug: pool[0], Name: pool[0], NPC: pool[0]}},
		Pop:        map[string]float64{pool[0]: 1},
		Mu:         map[string]map[string]PickerCell{pool[0]: {}},
		Syn:        map[string]map[string]PickerCell{pool[0]: {}},
		Prior:      map[string]float64{},
		HeroSource: map[string]string{},
		Prose:      prose.Heroes,
		Gates:      &gates.Doc{},
	}
	if _, err := FormatPicker(in, cfg); err == nil || !strings.Contains(err.Error(), "no prose") {
		t.Fatalf("pool hero without authored prose must fail the picker format, got %v", err)
	}
}

func TestFormatPickerMissingCellFails(t *testing.T) {
	cfg := fixtureCfg(t)
	pool := cfg.PoolSlugs()
	in := PickerInput{
		Date:       fixtureDay,
		Roster:     []RosterHero{{ID: 1, Slug: pool[0], Name: pool[0], NPC: pool[0]}},
		Pop:        map[string]float64{pool[0]: 1},
		Mu:         map[string]map[string]PickerCell{pool[0]: {}},
		Syn:        map[string]map[string]PickerCell{pool[0]: {}},
		Prior:      map[string]float64{},
		HeroSource: map[string]string{},
		Prose:      map[string]ProseHero{pool[0]: {}},
		Gates:      &gates.Doc{},
	}
	if _, err := FormatPicker(in, cfg); err == nil {
		t.Fatalf("pool hero with a missing mu cell must fail the picker format")
	}
}

// aliasesLine pulls the single emitted aliases line so pair assertions never
// cross-contaminate with slugs rendered in the parallel arrays.
func aliasesLine(s string) string {
	for ln := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(ln, "  aliases: ") {
			return ln
		}
	}
	return ""
}

// aliases are search/display-only: the payload keeps pairs whose value is a
// roster slug and drops pairs whose value is unknown or whose key shadows a
// roster slug. expectations derive from the live hub config against the
// fixture roster, so no pair is hardcoded here.
func TestFormatPickerAliasesEmittedAndFiltered(t *testing.T) {
	cfg := fixtureCfg(t)
	db := buildFixtureDB(t)
	in, err := LoadPicker(db, cfg, fixtureGates(t))
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	in.Prose = prose.Heroes
	in.Date = fixtureDay
	format := func() string {
		out, err := FormatPicker(in, cfg)
		if err != nil {
			t.Fatalf("format picker: %v", err)
		}
		return string(out)
	}
	s := format()
	line := aliasesLine(s)
	if line == "" {
		t.Fatal("payload missing the aliases line")
	}
	roster := map[string]bool{}
	for _, slug := range fixtureRosterSlugs(t, cfg) {
		roster[slug] = true
	}
	if len(cfg.Aliases) == 0 {
		t.Fatal("hub config carries no aliases, fixture cannot exercise the filter")
	}
	for key, want := range cfg.Aliases {
		pair := "'" + key + "': '" + want + "'"
		if roster[want] && !roster[key] {
			if !strings.Contains(line, pair) {
				t.Errorf("roster-resolved alias %s missing from the aliases line", pair)
			}
		} else if strings.Contains(line, "'"+key+"': ") {
			t.Errorf("dead alias %s must be dropped from the aliases line", pair)
		}
	}
	cfg.Aliases["bogus-alias"] = "not-a-roster-slug"
	cfg.Aliases["axe"] = "sven" // key shadows the fixture roster slug axe
	line = aliasesLine(format())
	if strings.Contains(line, "bogus-alias") || strings.Contains(line, "'axe': ") {
		t.Fatal("aliases with unknown targets or shadowing keys must be dropped")
	}
}
