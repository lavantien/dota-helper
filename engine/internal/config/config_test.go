package config

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// the test loads the real config hub so contract drift breaks here first.
const realConfig = "../../../config.json"

func TestLoadReal(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Patch != "7.41f" {
		t.Errorf("patch = %q", c.Patch)
	}
	// counts derive from the hub itself: entries must cover every unique hero
	// once plus one extra per additional role slot
	rolesBySlug := map[string]int{}
	for _, e := range c.Pool {
		if rolesBySlug[e.Slug]++; rolesBySlug[e.Slug] == 1 {
			for _, other := range c.Pool {
				if other.Slug == e.Slug && other.Role == e.Role && other != e {
					t.Errorf("duplicate pool entry %s@%s", e.Slug, e.Role)
				}
			}
		}
	}
	extra := 0
	for _, n := range rolesBySlug {
		extra += n - 1
	}
	if len(c.Pool) != len(rolesBySlug)+extra {
		t.Errorf("pool entries = %d, want %d unique + %d multi-role extras", len(c.Pool), len(rolesBySlug), extra)
	}
}

// Heroes folds pool entries per slug: roles ascending, tier per role. the
// fixture is synthetic so the merge contract holds regardless of pool shape.
func TestHeroesFoldsMultiRoleEntries(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	c.Pool = []PoolEntry{
		{Slug: "phantom-lancer", Name: "Phantom Lancer", Role: "1", Tier: "dedicated"},
		{Slug: "dragon-knight", Name: "Dragon Knight", Role: "3", Tier: "flex"},
		{Slug: "dragon-knight", Name: "Dragon Knight", Role: "2", Tier: "dedicated"},
	}
	got := map[string]Hero{}
	for _, h := range c.Heroes() {
		got[h.Slug] = h
	}
	dk, ok := got["dragon-knight"]
	if !ok {
		t.Fatal("dragon-knight missing from merged heroes")
	}
	if !reflect.DeepEqual(dk.Roles, []string{"2", "3"}) {
		t.Errorf("dk roles = %v, want [2 3] regardless of entry order", dk.Roles)
	}
	if dk.Tier["2"] != "dedicated" || dk.Tier["3"] != "flex" {
		t.Errorf("dk tier = %v, want per-role tiers", dk.Tier)
	}
	if _, ok := got["phantom-lancer"]; !ok {
		t.Error("phantom-lancer missing from merged heroes")
	}
}

// the live pool is pinned: every hero single-role, every tier dedicated.
func TestPoolHeroesPinnedSingleRoleDedicated(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range c.Heroes() {
		if len(h.Roles) != 1 {
			t.Errorf("%s plays %v, want a single pinned role", h.Slug, h.Roles)
		}
		if h.Tier[h.Roles[0]] != "dedicated" {
			t.Errorf("%s tier[%s] = %q, want dedicated", h.Slug, h.Roles[0], h.Tier[h.Roles[0]])
		}
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	// strip role 4 down to a single entry regardless of pool shape
	kept := c.Pool[:0]
	seen4 := false
	for _, e := range c.Pool {
		if e.Role != "4" || !seen4 {
			kept = append(kept, e)
		}
		if e.Role == "4" {
			seen4 = true
		}
	}
	c.Pool = kept
	if err := c.Validate(); err == nil {
		t.Error("validate accepted a role with 1 entry")
	}
}

func TestValidateRejectsBadScope(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Scope)
	}{
		{"zero gameMode", func(s *Scope) { s.GameMode = 0 }},
		{"zero lobbyType", func(s *Scope) { s.LobbyType = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(&c.Scope)
			if err := c.Validate(); err == nil {
				t.Error("validate accepted a bad scope config")
			}
		})
	}
}

// the winDay family saturates at 30 days server-side, so a take at or above
// the cap silently pins no window and must never load
func TestValidateRejectsUnpinnedTake(t *testing.T) {
	for _, take := range []int{0, 30, 200} {
		c, err := Load(realConfig)
		if err != nil {
			t.Fatal(err)
		}
		c.Stratz.Take = take
		if err := c.Validate(); err == nil {
			t.Errorf("validate accepted stratz.take %d, at or above the 30-day server cap", take)
		}
	}
	for _, take := range []int{1, 7, 29} {
		c, err := Load(realConfig)
		if err != nil {
			t.Fatal(err)
		}
		c.Stratz.Take = take
		if err := c.Validate(); err != nil {
			t.Errorf("validate rejected a pinnable stratz.take %d: %v", take, err)
		}
	}
}

func TestValidateRejectsBadSynByRole(t *testing.T) {
	tests := []struct {
		name string
		mut  func(map[string]float64)
	}{
		{"missing role", func(m map[string]float64) { delete(m, "3") }},
		{"extra role", func(m map[string]float64) { m["6"] = 1 }},
		{"nan weight", func(m map[string]float64) { m["2"] = math.NaN() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			c.Weights.SynByRole = map[string]float64{"1": 0.3, "2": 0.2, "3": 0.3, "4": 1, "5": 1}
			tt.mut(c.Weights.SynByRole)
			if err := c.Validate(); err == nil {
				t.Error("validate accepted a bad synByRole map")
			}
		})
	}
}

func TestValidateRejectsBadBuildsCfg(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*BuildsCfg)
	}{
		{"zero min matches", func(b *BuildsCfg) { b.MinMatches = 0 }},
		{"zero topN", func(b *BuildsCfg) { b.TopN = 0 }},
		{"zero timingTopN", func(b *BuildsCfg) { b.TimingTopN = 0 }},
		{"negative minCost", func(b *BuildsCfg) { b.MinCost = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(&c.Builds)
			if err := c.Validate(); err == nil {
				t.Error("validate accepted a bad builds config")
			}
		})
	}
}

// TestMarshalRoundTripKeepsEveryHubField pins the lossless rewrite promote
// relies on: unmarshal into Config plus MarshalIndent must carry every field
// the hub file carries, including sections with no consumer in code.
func TestMarshalRoundTripKeepsEveryHubField(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var c2 Config
	if err := json.Unmarshal(b, &c2); err != nil {
		t.Fatal(err)
	}
	if len(c2.Endpoints) == 0 || c2.Endpoints["stratz"] != c.Endpoints["stratz"] {
		t.Errorf("endpoints lost in round trip: %v", c2.Endpoints)
	}
	if len(c2.Builds.ProseContextItems) == 0 || len(c2.Builds.ProseGateStopWords) == 0 {
		t.Errorf("builds prose gate fields lost in round trip")
	}
	if !reflect.DeepEqual(c, &c2) {
		t.Error("round trip through Config is not lossless")
	}
}

func TestValidateRejectsNegativeReportMaxCells(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	c.Eval.ReportMaxCells = -1
	if err := c.Validate(); err == nil {
		t.Error("validate accepted a negative eval.reportMaxCells")
	}
}

func TestValidateRejectsBadExtensionCfg(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*EvalCfg)
	}{
		{"zero ensemble bags", func(e *EvalCfg) { e.Ensemble.Bags = 0 }},
		{"negative ensemble bags", func(e *EvalCfg) { e.Ensemble.Bags = -3 }},
		{"zero seq censusMinCount", func(e *EvalCfg) { e.Seq.CensusMinCount = 0 }},
		{"unit seq censusRatio", func(e *EvalCfg) { e.Seq.CensusRatio = 1 }},
		{"zero seq censusRatio", func(e *EvalCfg) { e.Seq.CensusRatio = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(&c.Eval)
			if err := c.Validate(); err == nil {
				t.Error("validate accepted a bad extension config")
			}
		})
	}
}

func TestSlugFromNPC(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"npc_dota_hero_nevermore":          "shadow-fiend",
		"npc_dota_hero_dragon-knight":      "dragon-knight",
		"npc_dota_hero_obsidian_destroyer": "outworld-destroyer",
	}
	for in, want := range cases {
		if got := c.SlugFromNPC(in); got != want {
			t.Errorf("SlugFromNPC(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortNPC(t *testing.T) {
	cases := map[string]string{
		// cdn names keep underscores and legacy names, no alias resolution
		"npc_dota_hero_windrunner":     "windrunner",
		"npc_dota_hero_necrolyte":      "necrolyte",
		"npc_dota_hero_furion":         "furion",
		"npc_dota_hero_phantom_lancer": "phantom_lancer",
		"axe":                          "axe",
		"npc_dota_hero_":               "npc_dota_hero_",
	}
	for in, want := range cases {
		if got := ShortNPC(in); got != want {
			t.Errorf("ShortNPC(%q) = %q, want %q", in, got, want)
		}
	}
}
