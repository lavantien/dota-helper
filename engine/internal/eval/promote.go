package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"poolguide/internal/config"
	"poolguide/internal/emit"
)

// DerivationEntry is one manifest row of ref/dota2/eval/derivations.json:
// how a scoring-affecting config key got its value.
type DerivationEntry struct {
	Key        string  `json:"key"`
	Value      float64 `json:"value"`
	Method     string  `json:"method"` // fitted | justified | authored
	Procedure  string  `json:"procedure"`
	Status     string  `json:"status,omitempty"` // pending | measured, fitted entries only
	MeasuredAt string  `json:"measuredAt,omitempty"`
}

// Derivations is the derivation manifest document.
type Derivations struct {
	Note    string            `json:"note"`
	Entries []DerivationEntry `json:"entries"`
}

const (
	weightsProcedure    = "coordinate ascent on train mean pick percentile over the sequential-replay term cache; promote guard: holdout gain >= eval.fit.minHoldoutGain with bootstrap CI low > 0"
	alphasProcedure     = "empirical-Bayes moment matching per family over the pooled raw aggregate tables (sigma2 = 2500 pp^2), holdout deviance cross-check over eval.alphaFit.grid; promote guard: bootstrap CI excludes the current value"
	completionProcedure = "masked-cell ALS cross-validation over eval.completionFit grids scored by Spearman against held-out shrunk values; promote guard: difference CI low > 0"
)

// Promote applies one fit section to the config hub behind its guard: the
// proposal is re-read from disk, the guard re-checked against the live
// config, the hub rewritten through unmarshal plus MarshalIndent and
// reloaded for equality, and the derivation manifest entries updated with
// the measured values. A failing guard leaves every file untouched.
func Promote(section string, d Deps) (string, error) {
	switch section {
	case "weights":
		return promoteWeights(d)
	case "alphas":
		return promoteAlphas(d)
	case "completion":
		return promoteCompletion(d)
	}
	return "", fmt.Errorf("eval promote: unknown section %q, want weights, alphas, or completion", section)
}

func promoteWeights(d Deps) (string, error) {
	var p WeightProposal
	if err := readProposal(d, "weights", &p); err != nil {
		return "", err
	}
	fc := d.Cfg.Eval.Fit
	weightKeys := []string{"knownMu", "prior", "genericFit", "exposure", "flexibility"}
	for _, k := range weightKeys {
		v, ok := p.Weights[k]
		if !ok {
			return "", fmt.Errorf("eval promote weights: proposal has no %s entry", k)
		}
		if math.IsNaN(v) || math.Abs(v) > fc.MaxWeight+1e-9 {
			return "", fmt.Errorf("eval promote weights: %s = %v outside the fit grid [-%g, %g]", k, v, fc.MaxWeight, fc.MaxWeight)
		}
	}
	for _, k := range []string{"bonus", "penalty"} {
		v, ok := p.GateDeltas[k]
		if !ok {
			return "", fmt.Errorf("eval promote weights: proposal has no gateDeltas.%s entry", k)
		}
		if math.IsNaN(v) || math.Abs(v) > fc.MaxWeight+1e-9 {
			return "", fmt.Errorf("eval promote weights: gateDeltas.%s = %v outside the fit grid", k, v)
		}
	}
	if p.Baseline == nil {
		return "", fmt.Errorf("eval promote weights: proposal records no baseline, rerun `engine eval fit weights`")
	}
	// the guard numbers were measured against the recorded vector; a hub
	// that moved since makes them stale. synByRole is authored, not fitted,
	// but the gain was measured under it, so it is baseline-pinned too
	if p.Baseline.Weights["knownMu"] != d.Cfg.Weights.KnownMu ||
		p.Baseline.Weights["prior"] != d.Cfg.Weights.Prior ||
		p.Baseline.Weights["genericFit"] != d.Cfg.Weights.GenericFit ||
		p.Baseline.Weights["exposure"] != d.Cfg.Weights.Exposure ||
		p.Baseline.Weights["flexibility"] != d.Cfg.Weights.Flexibility ||
		p.Baseline.GateDeltas["bonus"] != d.Cfg.GateDeltas["bonus"] ||
		p.Baseline.GateDeltas["penalty"] != d.Cfg.GateDeltas["penalty"] ||
		!reflect.DeepEqual(p.Baseline.SynByRole, d.Cfg.Weights.SynByRole) {
		return "", fmt.Errorf("eval promote weights: config changed since the fit measured its gain, rerun `engine eval fit weights`")
	}
	if p.Gain < fc.MinHoldoutGain {
		return "", fmt.Errorf("eval promote weights: guard failed, holdout gain %.4f below eval.fit.minHoldoutGain %.4f", p.Gain, fc.MinHoldoutGain)
	}
	if p.GainCiLow <= 0 {
		return "", fmt.Errorf("eval promote weights: guard failed, gain CI low %.4f not above 0", p.GainCiLow)
	}

	cfg := *d.Cfg
	cfg.Weights = config.Weights{
		KnownMu: p.Weights["knownMu"], SynByRole: synByRoleCopy(d.Cfg.Weights.SynByRole), Prior: p.Weights["prior"],
		GenericFit: p.Weights["genericFit"], Exposure: p.Weights["exposure"], Flexibility: p.Weights["flexibility"],
	}
	cfg.GateDeltas = map[string]float64{"bonus": p.GateDeltas["bonus"], "penalty": p.GateDeltas["penalty"]}
	if err := rewriteConfig(d, &cfg); err != nil {
		return "", err
	}
	entries := []DerivationEntry{
		{Key: "weights.knownMu", Value: cfg.Weights.KnownMu},
		{Key: "weights.prior", Value: cfg.Weights.Prior},
		{Key: "weights.genericFit", Value: cfg.Weights.GenericFit},
		{Key: "weights.exposure", Value: cfg.Weights.Exposure},
		{Key: "weights.flexibility", Value: cfg.Weights.Flexibility},
		{Key: "gateDeltas.bonus", Value: cfg.GateDeltas["bonus"]},
		{Key: "gateDeltas.penalty", Value: cfg.GateDeltas["penalty"]},
	}
	if err := updateDerivations(d, entries, weightsProcedure, p.MeasuredAt); err != nil {
		return "", err
	}
	return fmt.Sprintf("promoted weights: holdout pct %.4f (was %.4f), gain %+.4f [%.4f, %.4f]\n",
		p.HoldoutPct, p.CurrentHoldoutPct, p.Gain, p.GainCiLow, p.GainCiHi), nil
}

func promoteAlphas(d Deps) (string, error) {
	var p AlphaProposal
	if err := readProposal(d, "alphas", &p); err != nil {
		return "", err
	}
	byName := map[string]AlphaFamilyResult{}
	for _, f := range p.Families {
		byName[f.Family] = f
	}
	cfg := *d.Cfg
	want := []struct {
		field, family, key string
	}{
		{"matchupAlpha", "matchup", "shrink.matchupAlpha"},
		{"synergyAlpha", "synergy", "shrink.synergyAlpha"},
		{"overallWrAlpha", "overall", "shrink.overallWrAlpha"},
	}
	var entries []DerivationEntry
	var lines []string
	for _, w := range want {
		f, ok := byName[w.family]
		if !ok {
			return "", fmt.Errorf("eval promote alphas: proposal has no %s family", w.family)
		}
		if math.IsNaN(f.Chosen) || f.Chosen <= 0 {
			return "", fmt.Errorf("eval promote alphas: %s chose %v, want a positive alpha", w.family, f.Chosen)
		}
		var live float64
		switch w.field {
		case "matchupAlpha":
			live = d.Cfg.Shrink.MatchupAlpha
		case "synergyAlpha":
			live = d.Cfg.Shrink.SynergyAlpha
		case "overallWrAlpha":
			live = d.Cfg.Shrink.OverallWrAlpha
		}
		if f.Current != live {
			return "", fmt.Errorf("eval promote alphas: %s measured against current %g but the hub carries %g, rerun `engine eval fit alphas`",
				w.family, f.Current, live)
		}
		apply := f.Chosen
		if f.Chosen != f.Current {
			ci := f.Moment.CI
			if ci.Lo < f.Current && ci.Hi > f.Current {
				// the interval cannot distinguish the estimates: the family
				// keeps its current value and the measurement is record-only
				apply = f.Current
				lines = append(lines, fmt.Sprintf("%s: kept %g (chosen %g, CI [%.1f, %.1f] contains the current value)",
					w.family, f.Current, f.Chosen, ci.Lo, ci.Hi))
			}
		}
		switch w.field {
		case "matchupAlpha":
			cfg.Shrink.MatchupAlpha = apply
		case "synergyAlpha":
			cfg.Shrink.SynergyAlpha = apply
		case "overallWrAlpha":
			cfg.Shrink.OverallWrAlpha = apply
		}
		entries = append(entries, DerivationEntry{Key: w.key, Value: apply})
		if apply == f.Chosen {
			lines = append(lines, fmt.Sprintf("%s: %g -> %g (moment %.1f, %s)", w.family, f.Current, f.Chosen, f.Moment.Alpha, f.Reason))
		}
	}
	if err := rewriteConfig(d, &cfg); err != nil {
		return "", err
	}
	if err := updateDerivations(d, entries, alphasProcedure, p.MeasuredAt); err != nil {
		return "", err
	}
	out := "promoted alphas:\n"
	for _, l := range lines {
		out += "  " + l + "\n"
	}
	return out, nil
}

func promoteCompletion(d Deps) (string, error) {
	var p CompletionProposal
	if err := readProposal(d, "completion", &p); err != nil {
		return "", err
	}
	if p.Winner.Rank < 1 || math.IsNaN(p.Winner.Lambda) || p.Winner.Lambda <= 0 {
		return "", fmt.Errorf("eval promote completion: winner rank %d lambda %v is not a sane candidate", p.Winner.Rank, p.Winner.Lambda)
	}
	if p.Current.Rank != d.Cfg.Completion.Rank || p.Current.Lambda != d.Cfg.Completion.Lambda {
		return "", fmt.Errorf("eval promote completion: measured against rank %d lambda %g but the hub carries rank %d lambda %g, rerun `engine eval fit completion`",
			p.Current.Rank, p.Current.Lambda, d.Cfg.Completion.Rank, d.Cfg.Completion.Lambda)
	}
	changed := p.Winner.Rank != p.Current.Rank || p.Winner.Lambda != p.Current.Lambda
	if changed && p.DiffCiLow <= 0 {
		return "", fmt.Errorf("eval promote completion: guard failed, winner rank %d lambda %g difference CI low %.4f not above 0",
			p.Winner.Rank, p.Winner.Lambda, p.DiffCiLow)
	}
	cfg := *d.Cfg
	if changed {
		cfg.Completion.Rank, cfg.Completion.Lambda = p.Winner.Rank, p.Winner.Lambda
	}
	if err := rewriteConfig(d, &cfg); err != nil {
		return "", err
	}
	if err := updateDerivations(d, []DerivationEntry{
		{Key: "completion.rank", Value: float64(cfg.Completion.Rank)},
		{Key: "completion.lambda", Value: cfg.Completion.Lambda},
	}, completionProcedure, p.MeasuredAt); err != nil {
		return "", err
	}
	verb := "kept"
	if changed {
		verb = "promoted"
	}
	return fmt.Sprintf("%s completion: rank %d lambda %g (spearman %.4f, was %.4f at rank %d lambda %g)\n",
		verb, cfg.Completion.Rank, cfg.Completion.Lambda, p.Winner.Spearman, p.Current.Spearman, p.Current.Rank, p.Current.Lambda), nil
}

// readProposal loads one section's proposal: the weight fit writes
// paths.fitOut itself, the alpha and completion fits write derived siblings.
func readProposal(d Deps, section string, into any) error {
	name := d.Cfg.Paths.FitOut
	if section != "weights" {
		name = fitProposalPath(name, section)
	}
	path := filepath.Join(d.RepoRoot, name)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no %s proposal at %s: run `engine eval fit %s` first", section, path, section)
		}
		return err
	}
	return json.Unmarshal(b, into)
}

// rewriteConfig persists the mutated hub: MarshalIndent at 2 spaces, write,
// reload, and verify the rewrite is exactly what was intended. The round
// trip through config.Config is pinned lossless by the config tests.
func rewriteConfig(d Deps, cfg *config.Config) error {
	path := d.CfgPath
	if path == "" {
		path = filepath.Join(d.RepoRoot, "config.json")
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := emit.WriteFile(path, append(b, '\n')); err != nil {
		return fmt.Errorf("eval promote: rewrite config: %w", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("eval promote: reload after rewrite: %w", err)
	}
	if !reflect.DeepEqual(reloaded, cfg) {
		return fmt.Errorf("eval promote: reloaded config differs from the intended rewrite")
	}
	return nil
}

// updateDerivations upserts the manifest entries of one section as measured:
// value from the freshly written config, the fit procedure, and the
// proposal's measurement stamp. Entries sort by key so the file is stable.
func updateDerivations(d Deps, entries []DerivationEntry, procedure, measuredAt string) error {
	path := filepath.Join(d.RepoRoot, d.Cfg.Paths.DerivationsPath)
	doc := Derivations{Note: "how every scoring-affecting config key got its value; see ref/dota2/eval/README.md"}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("eval promote: parse derivations: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	upsert := map[string]DerivationEntry{}
	for _, e := range doc.Entries {
		upsert[e.Key] = e
	}
	for _, e := range entries {
		e.Method, e.Procedure, e.Status, e.MeasuredAt = "fitted", procedure, "measured", measuredAt
		upsert[e.Key] = e
	}
	doc.Entries = doc.Entries[:0]
	for _, e := range upsert {
		doc.Entries = append(doc.Entries, e)
	}
	sort.Slice(doc.Entries, func(i, j int) bool { return doc.Entries[i].Key < doc.Entries[j].Key })
	jb, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := emit.WriteFile(path, append(jb, '\n')); err != nil {
		return fmt.Errorf("eval promote: write derivations: %w", err)
	}
	return nil
}
