package config

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

const realConfig = "../../../config.json"

func TestLoadReal(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Patch != "7.41f" {
		t.Errorf("patch = %q", c.Patch)
	}
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
	if !reflect.DeepEqual(c.SharedRolePools, [][]string{{"1", "2", "3"}, {"4", "5"}}) {
		t.Errorf("sharedRolePools = %v, want the core field and the support field", c.SharedRolePools)
	}
}

func TestSharedRolePools(t *testing.T) {
	ids := map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true}
	c := &Config{SharedRolePools: [][]string{{"1", "2"}}}
	if err := c.validateSharedRolePools(ids); err != nil {
		t.Fatalf("valid group rejected: %v", err)
	}
	if got := c.RoleField("1"); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Errorf("RoleField(1) = %v, want [1 2]", got)
	}
	if got := c.RoleField("3"); !reflect.DeepEqual(got, []string{"3"}) {
		t.Errorf("RoleField(3) = %v, want [3]", got)
	}
	if !c.PlaysRole([]string{"2"}, "1") {
		t.Error("PlaysRole([2], 1) = false, want the shared field")
	}
	if c.PlaysRole([]string{"3"}, "1") {
		t.Error("PlaysRole([3], 1) = true, want exact seats only")
	}
	for name, groups := range map[string][][]string{
		"unknown role": {{"1", "6"}},
		"overlap":      {{"1", "2"}, {"2", "3"}},
		"singleton":    {{"1"}},
	} {
		bad := &Config{SharedRolePools: groups}
		if err := bad.validateSharedRolePools(ids); err == nil {
			t.Errorf("%s: group accepted", name)
		}
	}
}

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

func TestPoolHeroesPinnedTiersAndSeats(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	multi := map[string][]string{}
	for _, h := range c.Heroes() {
		for _, r := range h.Roles {
			if h.Tier[r] != "dedicated" {
				t.Errorf("%s tier[%s] = %q, want dedicated", h.Slug, r, h.Tier[r])
			}
		}
		if len(h.Roles) > 1 {
			multi[h.Slug] = h.Roles
		}
	}
	if len(multi) != 0 {
		t.Errorf("multi-role heroes = %v, want none under the frozen single-seat lineup", multi)
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
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

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"anti-mage", "dark-seer", "pudge", "hoodwink", "obsidian-destroyer-9", "axe"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "../..", "a/b", `a\b`, "<img>", "x y", "Pudge", "a.b", "never more"} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}
