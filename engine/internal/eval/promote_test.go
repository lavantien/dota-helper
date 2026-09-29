package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/config"
)

// load fills a Deps with the hub config of the root under test.
func (d *Deps) load() error {
	path := filepath.Join(d.RepoRoot, "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	d.Cfg, d.CfgPath = cfg, path
	return nil
}

func readHubCfg(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload hub: %v", err)
	}
	return cfg
}

// promoteRoot copies the real hub config into a temp root with a minimal
// derivation manifest and hands back everything promote touches.
func promoteRoot(t *testing.T) (root, cfgPath, derivPath string) {
	t.Helper()
	t.Chdir(repoRoot)
	root = t.TempDir()
	cfgPath = filepath.Join(root, "config.json")
	src, err := os.ReadFile(filepath.Join(repoRoot, "config.json"))
	if err != nil {
		t.Fatalf("read hub config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cfgPath, src, 0o644); err != nil {
		t.Fatalf("write hub copy: %v", err)
	}
	derivPath = filepath.Join(root, "ref/dota2/eval/derivations.json")
	if err := os.MkdirAll(filepath.Dir(derivPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := Derivations{
		Note: "test manifest",
		Entries: []DerivationEntry{
			{Key: "weights.knownMu", Value: 1, Method: "fitted", Procedure: "seeded", Status: "pending"},
			{Key: "gateDeltas.bonus", Value: 0.25, Method: "fitted", Procedure: "seeded", Status: "pending"},
		},
	}
	b, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(derivPath, append(b, '\n'), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return root, cfgPath, derivPath
}

func writeProposalFile(t *testing.T, root, section string, v any) {
	t.Helper()
	name := "var/eval-fit.json"
	if section != "weights" {
		name = fitProposalPath(name, section)
	}
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// passingWeightProposal builds a guard-passing proposal measured against
// the live hub copy the test root carries.
func passingWeightProposal(cfg *config.Config) WeightProposal {
	return WeightProposal{
		Weights:    map[string]float64{"knownMu": 1.2, "prior": 0.4, "genericFit": 0.5, "exposure": 0.6, "flexibility": 0.2},
		GateDeltas: map[string]float64{"bonus": 0.3, "penalty": -0.2},
		TrainPct:   0.6, HoldoutPct: 0.58, CurrentHoldoutPct: 0.54,
		Gain: 0.04, GainCiLow: 0.01, GainCiHi: 0.07,
		MeasuredAt: "2026-09-26T00:00:00Z",
		Ablation:   []AblationRow{},
		Baseline: &WeightBaseline{
			Weights: map[string]float64{
				"knownMu": cfg.Weights.KnownMu, "prior": cfg.Weights.Prior,
				"genericFit": cfg.Weights.GenericFit, "exposure": cfg.Weights.Exposure, "flexibility": cfg.Weights.Flexibility,
			},
			GateDeltas: map[string]float64{"bonus": cfg.GateDeltas["bonus"], "penalty": cfg.GateDeltas["penalty"]},
			SynByRole:  synByRoleCopy(cfg.Weights.SynByRole),
		},
	}
}

func TestPromoteWeightsRejectsBelowThreshold(t *testing.T) {
	root, cfgPath, derivPath := promoteRoot(t)
	before, _ := os.ReadFile(cfgPath)
	deps := Deps{RepoRoot: root}
	if err := deps.load(); err != nil {
		t.Fatal(err)
	}
	p := passingWeightProposal(deps.Cfg)
	p.Gain, p.GainCiLow = 0.001, 0.0002
	writeProposalFile(t, root, "weights", p)
	_, err := Promote("weights", deps)
	if err == nil || !strings.Contains(err.Error(), "guard failed") {
		t.Fatalf("err = %v, want the guard to reject a sub-threshold gain", err)
	}
	// gain fine but the interval straddles zero: still rejected
	p.Gain, p.GainCiLow = 0.02, -0.001
	writeProposalFile(t, root, "weights", p)
	if _, err := Promote("weights", deps); err == nil || !strings.Contains(err.Error(), "guard failed") {
		t.Fatalf("err = %v, want the guard to reject a CI straddling zero", err)
	}
	// five weight entries with typo'd names must not pass as silent zeros
	typo := passingWeightProposal(deps.Cfg)
	typo.Weights = map[string]float64{
		"knownmu": 1.2, "prior": 0.4, "genericFit": 0.5, "exposure": 0.6, "flexibilityX": 0.2,
	}
	writeProposalFile(t, root, "weights", typo)
	if _, err := Promote("weights", deps); err == nil || !strings.Contains(err.Error(), "no knownMu entry") {
		t.Fatalf("err = %v, want the missing-key rejection", err)
	}
	// a baseline that no longer matches the live hub is a stale measurement
	p.Gain, p.GainCiLow = 0.02, 0.01
	p.Baseline.Weights["prior"] += 0.1
	writeProposalFile(t, root, "weights", p)
	if _, err := Promote("weights", deps); err == nil || !strings.Contains(err.Error(), "rerun") {
		t.Fatalf("err = %v, want the stale-baseline rejection", err)
	}
	after, _ := os.ReadFile(cfgPath)
	if string(after) != string(before) {
		t.Fatal("a failed guard touched the config")
	}
	d, _ := os.ReadFile(derivPath)
	if !strings.Contains(string(d), `"pending"`) {
		t.Fatal("a failed guard touched the manifest")
	}
}

func TestPromoteWeightsAppliesAboveThreshold(t *testing.T) {
	root, cfgPath, derivPath := promoteRoot(t)
	deps := Deps{RepoRoot: root}
	if err := deps.load(); err != nil {
		t.Fatal(err)
	}
	writeProposalFile(t, root, "weights", passingWeightProposal(deps.Cfg))
	msg, err := Promote("weights", deps)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !strings.Contains(msg, "promoted weights") {
		t.Fatalf("message %q", msg)
	}
	hub := readHubCfg(t, cfgPath)
	p := passingWeightProposal(deps.Cfg)
	if hub.Weights.KnownMu != p.Weights["knownMu"] || hub.Weights.Flexibility != p.Weights["flexibility"] {
		t.Fatalf("weights not applied: %+v", hub.Weights)
	}
	if hub.GateDeltas["bonus"] != 0.3 || hub.GateDeltas["penalty"] != -0.2 {
		t.Fatalf("gate deltas not applied: %+v", hub.GateDeltas)
	}
	var doc Derivations
	b, _ := os.ReadFile(derivPath)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	byKey := map[string]DerivationEntry{}
	for _, e := range doc.Entries {
		byKey[e.Key] = e
	}
	e := byKey["weights.knownMu"]
	if e.Value != 1.2 || e.Method != "fitted" || e.Status != "measured" || e.MeasuredAt == "" {
		t.Fatalf("manifest entry not updated: %+v", e)
	}
	if byKey["gateDeltas.bonus"].Value != 0.3 {
		t.Fatalf("gate delta manifest entry not updated: %+v", byKey["gateDeltas.bonus"])
	}
	// a missing key is appended, so the manifest stays complete
	if _, ok := byKey["weights.prior"]; !ok {
		t.Fatal("promote did not append the missing prior entry")
	}
}

func TestPromoteMissingProposalIsAClearError(t *testing.T) {
	root, _, _ := promoteRoot(t)
	deps := Deps{RepoRoot: root}
	if err := deps.load(); err != nil {
		t.Fatal(err)
	}
	_, err := Promote("weights", deps)
	if err == nil || !strings.Contains(err.Error(), "run `engine eval fit weights` first") {
		t.Fatalf("err = %v, want a pointer at the missing fit", err)
	}
	if _, err := Promote("bogus", deps); err == nil || !strings.Contains(err.Error(), "unknown section") {
		t.Fatalf("err = %v, want the unknown-section error", err)
	}
}

func TestPromoteAlphasGuardAndApply(t *testing.T) {
	root, cfgPath, _ := promoteRoot(t)
	deps := Deps{RepoRoot: root}
	if err := deps.load(); err != nil {
		t.Fatal(err)
	}
	cur := deps.Cfg.Shrink
	family := func(name string, current, chosen, lo, hi float64) AlphaFamilyResult {
		return AlphaFamilyResult{
			Family: name, Current: current, Chosen: chosen,
			Moment: MomentFit{Alpha: chosen, CI: CI{Lo: lo, Hi: hi}},
			Reason: "test",
		}
	}
	// matchup CI contains its current value: that family keeps it, the
	// families with excluding intervals still move
	writeProposalFile(t, root, "alphas", AlphaProposal{
		Families: []AlphaFamilyResult{
			family("matchup", cur.MatchupAlpha, cur.MatchupAlpha+10, cur.MatchupAlpha-10, cur.MatchupAlpha+20),
			family("synergy", cur.SynergyAlpha, cur.SynergyAlpha, 0, 0),
			family("overall", cur.OverallWrAlpha, cur.OverallWrAlpha+300, cur.OverallWrAlpha+200, cur.OverallWrAlpha+400),
		},
		MeasuredAt: "2026-09-26T00:00:00Z",
	})
	msg, err := Promote("alphas", deps)
	if err != nil {
		t.Fatalf("promote alphas: %v", err)
	}
	if !strings.Contains(msg, "kept") {
		t.Fatalf("message %q does not record the kept matchup family", msg)
	}
	hub := readHubCfg(t, cfgPath)
	if hub.Shrink.MatchupAlpha != cur.MatchupAlpha || hub.Shrink.SynergyAlpha != cur.SynergyAlpha ||
		hub.Shrink.OverallWrAlpha != cur.OverallWrAlpha+300 {
		t.Fatalf("shrink section wrong: %+v, want matchup kept and overall moved", hub.Shrink)
	}
	writeProposalFile(t, root, "alphas", AlphaProposal{
		Families: []AlphaFamilyResult{
			family("matchup", cur.MatchupAlpha, cur.MatchupAlpha+37.7, cur.MatchupAlpha+20, cur.MatchupAlpha+55),
			family("synergy", cur.SynergyAlpha, cur.SynergyAlpha, 0, 0),
			family("overall", cur.OverallWrAlpha+300, cur.OverallWrAlpha+300, 0, 0),
		},
		MeasuredAt: "2026-09-26T00:00:00Z",
	})
	// promote rewrote the hub, so the second promote runs against a freshly
	// loaded config, exactly like a fresh process would
	if err := deps.load(); err != nil {
		t.Fatal(err)
	}
	if _, err := Promote("alphas", deps); err != nil {
		t.Fatalf("promote alphas: %v", err)
	}
	if hub := readHubCfg(t, cfgPath); hub.Shrink.MatchupAlpha != cur.MatchupAlpha+37.7 {
		t.Fatalf("matchup alpha not applied: %+v", hub.Shrink)
	}
	// a family measured against a stale current is a stale measurement
	writeProposalFile(t, root, "alphas", AlphaProposal{
		Families: []AlphaFamilyResult{
			family("matchup", 1, 2, 0, 0),
			family("synergy", cur.SynergyAlpha, cur.SynergyAlpha, 0, 0),
			family("overall", cur.OverallWrAlpha+300, cur.OverallWrAlpha+300, 0, 0),
		},
		MeasuredAt: "2026-09-26T00:00:00Z",
	})
	if _, err := Promote("alphas", deps); err == nil || !strings.Contains(err.Error(), "rerun") {
		t.Fatalf("err = %v, want the stale-current rejection", err)
	}
}

func TestPromoteCompletionGuardAndApply(t *testing.T) {
	root, cfgPath, _ := promoteRoot(t)
	deps := Deps{RepoRoot: root}
	if err := deps.load(); err != nil {
		t.Fatal(err)
	}
	base := CompletionProposal{
		Kinds: []string{"enemy", "ally"}, Masked: 300, MaskFrac: 0.2,
		Current: CompletionCandidate{
			Rank: deps.Cfg.Completion.Rank, Lambda: deps.Cfg.Completion.Lambda, Spearman: 0.9,
		},
		Winner:    CompletionCandidate{Rank: 12, Lambda: 1.0, Spearman: 0.95},
		DiffCiLow: -0.01, DiffCiHi: 0.02,
		MeasuredAt: "2026-09-26T00:00:00Z",
	}
	writeProposalFile(t, root, "completion", base)
	before := readHubCfg(t, cfgPath)
	if _, err := Promote("completion", deps); err == nil || !strings.Contains(err.Error(), "guard failed") {
		t.Fatalf("err = %v, want the difference CI guard to fail", err)
	}
	if after := readHubCfg(t, cfgPath); after.Completion != before.Completion {
		t.Fatal("a failed guard touched the completion section")
	}
	base.DiffCiLow = 0.01
	writeProposalFile(t, root, "completion", base)
	if _, err := Promote("completion", deps); err != nil {
		t.Fatalf("promote completion: %v", err)
	}
	if hub := readHubCfg(t, cfgPath); hub.Completion.Rank != 12 || hub.Completion.Lambda != 1.0 {
		t.Fatalf("completion section not applied: %+v", hub.Completion)
	}
}
