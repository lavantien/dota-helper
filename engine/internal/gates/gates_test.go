package gates

import (
	"testing"

	"poolguide/internal/config"
)

const realGates = "../../../picker/gates.json"

func testCfg() *config.Config {
	return &config.Config{
		GateDeltas:     map[string]float64{"bonus": 0.25, "penalty": -0.25},
		AoEClearHeroes: []string{"axe", "invoker"},
		Thresholds:     config.Thresholds{DisCut: 1.5, AutoseedCap: 3},
	}
}

func containsHero(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestConditionTruthTable(t *testing.T) {
	cfg := testCfg()
	state := DraftState{
		VisibleEnemies:  []string{"axe", "sniper"},
		Allies:          []string{"io"},
		Role:            "2",
		CandidatesGated: 2,
	}
	cases := []struct {
		name string
		c    Condition
		want bool
	}{
		{"enemyVisible hit", Condition{Kind: KindEnemyVisible, Heroes: []string{"axe"}}, true},
		{"enemyVisible miss", Condition{Kind: KindEnemyVisible, Heroes: []string{"puck"}}, false},
		{"enemyVisibleAny one of", Condition{Kind: KindEnemyVisibleAny, Heroes: []string{"sniper", "puck"}}, true},
		{"enemyVisibleAny none", Condition{Kind: KindEnemyVisibleAny, Heroes: []string{"puck", "invoker"}}, false},
		{"enemyVisibleNone true", Condition{Kind: KindEnemyVisibleNone, Heroes: []string{"puck"}}, true},
		{"enemyVisibleNone false", Condition{Kind: KindEnemyVisibleNone, Heroes: []string{"axe"}}, false},
		{"anyCountMin met", Condition{Kind: KindEnemyVisibleAnyCountMin, Heroes: []string{"axe", "sniper", "puck"}, Count: 2}, true},
		{"anyCountMin unmet", Condition{Kind: KindEnemyVisibleAnyCountMin, Heroes: []string{"axe", "sniper", "puck"}, Count: 3}, false},
		{"allyVisibleAny hit", Condition{Kind: KindAllyVisibleAny, Heroes: []string{"io"}}, true},
		{"allyVisibleAny miss", Condition{Kind: KindAllyVisibleAny, Heroes: []string{"chen"}}, false},
		{"enemyCountMin met", Condition{Kind: KindEnemyCountMin, Count: 2}, true},
		{"enemyCountMin unmet", Condition{Kind: KindEnemyCountMin, Count: 3}, false},
		{"enemyCountMax met", Condition{Kind: KindEnemyCountMax, Count: 2}, true},
		{"enemyCountMax unmet", Condition{Kind: KindEnemyCountMax, Count: 1}, false},
		{"enemyAoEMax met", Condition{Kind: KindEnemyAoEMax, Max: 2}, true},
		{"enemyAoEMax unmet", Condition{Kind: KindEnemyAoEMax, Max: 0}, false},
		{"enemyAoEMin met", Condition{Kind: KindEnemyAoEMin, Min: 1}, true},
		{"enemyAoEMin unmet", Condition{Kind: KindEnemyAoEMin, Min: 2}, false},
		{"roleCandidatesGated met", Condition{Kind: KindRoleCandidatesGated, Count: 2}, true},
		{"roleCandidatesGated unmet", Condition{Kind: KindRoleCandidatesGated, Count: 3}, false},
		{"minEnemyCount met", Condition{Kind: KindMinEnemyCount, MinEnemyCount: 2}, true},
		{"minEnemyCount unmet", Condition{Kind: KindMinEnemyCount, MinEnemyCount: 3}, false},
	}
	for _, tc := range cases {
		if got := condHolds(tc.c, state, cfg); got != tc.want {
			t.Errorf("%s: condHolds = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAndSemantics(t *testing.T) {
	cfg := testCfg()
	state := DraftState{VisibleEnemies: []string{"axe"}, Role: "2"}
	rule := Rule{
		Id: "r", Target: "spectre", Action: ActionNoteOnly, Note: "fired",
		When: []Condition{
			{Kind: KindEnemyVisible, Heroes: []string{"axe"}},
			{Kind: KindEnemyCountMin, Count: 5},
		},
	}
	if res := Evaluate([]Rule{rule}, "spectre", state, cfg); len(res.FiredRuleIds) != 0 {
		t.Errorf("AND must reject when one condition fails: %v", res.FiredRuleIds)
	}
	state.Allies = []string{"io"}
	rule.When[1] = Condition{Kind: KindAllyVisibleAny, Heroes: []string{"io"}}
	res := Evaluate([]Rule{rule}, "spectre", state, cfg)
	if len(res.FiredRuleIds) != 1 || res.FiredRuleIds[0] != "r" || len(res.Notes) != 1 || res.Notes[0] != "fired" {
		t.Errorf("AND must fire when all hold: %+v", res)
	}
}

func TestRoleScoping(t *testing.T) {
	cfg := testCfg()
	state := DraftState{Role: "2"}
	rule := Rule{
		Id: "r", Target: "spectre", Roles: []string{"5"}, Action: ActionNoteOnly,
		When: []Condition{{Kind: KindEnemyCountMin, Count: 0}},
	}
	if res := Evaluate([]Rule{rule}, "spectre", state, cfg); len(res.FiredRuleIds) != 0 {
		t.Errorf("role mismatch must not fire: %v", res.FiredRuleIds)
	}
	state.Role = "5"
	if res := Evaluate([]Rule{rule}, "spectre", state, cfg); len(res.FiredRuleIds) != 1 {
		t.Errorf("role match must fire: %+v", res)
	}
	wild := rule
	wild.Roles = nil
	state.Role = "3"
	if res := Evaluate([]Rule{wild}, "spectre", state, cfg); len(res.FiredRuleIds) != 1 {
		t.Errorf("empty roles must be a wildcard: %+v", res)
	}
}

func TestActionEffects(t *testing.T) {
	cfg := testCfg()
	state := DraftState{Role: "2", VisibleEnemies: []string{"axe"}}
	always := []Condition{{Kind: KindEnemyCountMin, Count: 0}}
	rules := []Rule{
		{Id: "h", Target: "spectre", Action: ActionHardGate, Note: "hard", When: []Condition{{Kind: KindEnemyVisible, Heroes: []string{"axe"}}}},
		{Id: "b", Target: "spectre", Action: ActionBonus, When: always},
		{Id: "p1", Target: "spectre", Action: ActionPenalty, When: always},
		{Id: "p2", Target: "spectre", Action: ActionPenalty, When: always},
		{Id: "n", Target: "spectre", Action: ActionNoteOnly, Note: "watch out", When: always},
	}
	res := Evaluate(rules, "spectre", state, cfg)
	if !res.HardGated {
		t.Error("hard_gate must set HardGated")
	}
	if res.Eligible() {
		t.Error("hard-gated candidate must be ineligible")
	}
	if want, got := -0.25, res.Delta; want != got {
		t.Errorf("Delta = %v, want %v (+0.25 bonus, two -0.25 penalties)", got, want)
	}
	if len(res.Notes) != 2 || res.Notes[0] != "hard" || res.Notes[1] != "watch out" {
		t.Errorf("Notes = %v, want [hard watch out]", res.Notes)
	}
	if len(res.FiredRuleIds) != 5 {
		t.Errorf("FiredRuleIds = %v, want all 5", res.FiredRuleIds)
	}
}

func TestLoadRealGates(t *testing.T) {
	doc, err := Load(realGates)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(doc.Rules) == 0 {
		t.Fatal("no rules in real gates.json")
	}
	seen := map[string]bool{}
	for _, r := range doc.Rules {
		if r.Id == "" || r.Target == "" {
			t.Fatalf("rule with empty id or target: %+v", r)
		}
		if seen[r.Id] {
			t.Fatalf("duplicate rule id %s", r.Id)
		}
		seen[r.Id] = true
	}
	found := false
	for _, r := range doc.Rules {
		if r.Target != "spectre" || r.Action != ActionHardGate {
			continue
		}
		for _, c := range r.When {
			if c.Kind == KindEnemyVisibleAny && containsHero(c.Heroes, "undying") {
				found = true
			}
		}
	}
	if !found {
		t.Error("spectre hard gate on undying not found in real gates.json")
	}
	if len(doc.FallbackOrder["5"]) == 0 {
		t.Error("fallbackOrder must cover role 5")
	}
	if len(doc.RoleNotes) == 0 {
		t.Error("roleNotes must be populated")
	}
	if len(doc.Autogenerated) != 0 {
		t.Errorf("autogenerated should start empty, got %d rules", len(doc.Autogenerated))
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []Rule{
		{Id: "", Target: "x", Action: ActionNoteOnly},
		{Id: "r", Target: "", Action: ActionNoteOnly},
		{Id: "r", Target: "x", Action: "explode"},
		{Id: "r", Target: "x", Action: ActionNoteOnly, When: []Condition{{Kind: "nope"}}},
		{Id: "r", Target: "x", Action: ActionNoteOnly, When: []Condition{{Kind: KindEnemyVisible}}},
	}
	for i, r := range cases {
		doc := &Doc{Rules: []Rule{r}}
		if err := doc.Validate(); err == nil {
			t.Errorf("case %d must fail validation: %+v", i, r)
		}
	}
}

func TestAutoseed(t *testing.T) {
	rows := []VsRow{
		{"axe", 1.4}, {"sniper", 5.4}, {"puck", 2.3}, {"zeus", 9.1}, {"io", 3}, {"meepo", 2}, {"chen", 1.6},
	}
	got := Autoseed("spectre", rows, testCfg())
	if len(got) != 3 {
		t.Fatalf("got %d rules, want 3", len(got))
	}
	wantEnemies := []string{"zeus", "sniper", "io"}
	for i, r := range got {
		if r.Id != "autoseed-spectre-"+wantEnemies[i] {
			t.Errorf("rule %d id = %q, want autoseed-spectre-%s", i, r.Id, wantEnemies[i])
		}
		if r.Target != "spectre" || r.Action != ActionHardGate || r.Origin != "autogenerated" {
			t.Errorf("rule %d shape wrong: %+v", i, r)
		}
		if r.Roles != nil {
			t.Errorf("autoseed rules must be role wildcards: %v", r.Roles)
		}
		if len(r.When) != 1 || r.When[0].Kind != KindEnemyVisibleAny ||
			len(r.When[0].Heroes) != 1 || r.When[0].Heroes[0] != wantEnemies[i] {
			t.Errorf("rule %d when wrong: %+v", i, r.When)
		}
	}
	if want, got := "autogenerated: io dis 3", got[2].Note; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestAutoseedTieBreak(t *testing.T) {
	rows := []VsRow{{"b", 2}, {"a", 2}, {"c", 9}}
	got := Autoseed("io", rows, testCfg())
	if len(got) != 3 || got[0].Id != "autoseed-io-c" || got[1].Id != "autoseed-io-a" || got[2].Id != "autoseed-io-b" {
		t.Errorf("tie order wrong: %v %v %v", got[0].Id, got[1].Id, got[2].Id)
	}
}

func TestAutoseedBelowCutEmpty(t *testing.T) {
	got := Autoseed("io", []VsRow{{"axe", 1.4}}, testCfg())
	if len(got) != 0 {
		t.Errorf("rows below cut must produce nothing, got %d", len(got))
	}
}
