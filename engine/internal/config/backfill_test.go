package config

import "testing"

// The backfill and eval sections drive the per-match draft pipeline; every
// rule here guards a value whose misuse would silently corrupt a crawl or a
// fitted model, so they fail config load loudly.

func TestValidateRejectsBadBackfillCfg(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*BackfillCfg)
	}{
		{"zero take", func(b *BackfillCfg) { b.Take = 0 }},
		{"zero maxRequests", func(b *BackfillCfg) { b.MaxRequests = 0 }},
		{"zero targetMatches", func(b *BackfillCfg) { b.TargetMatches = 0 }},
		{"zero minDurationSec", func(b *BackfillCfg) { b.MinDurationSec = 0 }},
		{"zero gameMode", func(b *BackfillCfg) { b.GameMode = 0 }},
		{"zero lobbyType", func(b *BackfillCfg) { b.LobbyType = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(&c.Backfill)
			if err := c.Validate(); err == nil {
				t.Error("validate accepted a bad backfill config")
			}
		})
	}
}

func TestValidateRejectsBadEvalCfg(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*EvalCfg)
	}{
		{"empty topK", func(e *EvalCfg) { e.TopK = nil }},
		{"unsorted topK", func(e *EvalCfg) { e.TopK = []int{3, 1} }},
		{"zero topK entry", func(e *EvalCfg) { e.TopK = []int{0, 3} }},
		{"zero minSidePool", func(e *EvalCfg) { e.MinSidePool = 0 }},
		{"trainFrac at zero", func(e *EvalCfg) { e.Split.TrainFrac = 0 }},
		{"trainFrac at one", func(e *EvalCfg) { e.Split.TrainFrac = 1 }},
		{"zero resamples", func(e *EvalCfg) { e.Bootstrap.Resamples = 0 }},
		{"one calibration bin", func(e *EvalCfg) { e.CalibrationBins = 1 }},
		{"empty alpha grid", func(e *EvalCfg) { e.AlphaFit.Grid = nil }},
		{"unsorted alpha grid", func(e *EvalCfg) { e.AlphaFit.Grid = []float64{10, 5} }},
		{"zero minHoldoutN", func(e *EvalCfg) { e.AlphaFit.MinHoldoutN = 0 }},
		{"empty completion ranks", func(e *EvalCfg) { e.CompletionFit.Ranks = nil }},
		{"unsorted completion ranks", func(e *EvalCfg) { e.CompletionFit.Ranks = []int{8, 4} }},
		{"empty lambdas", func(e *EvalCfg) { e.CompletionFit.Lambdas = nil }},
		{"zero lambda entry", func(e *EvalCfg) { e.CompletionFit.Lambdas = []float64{0.01, 0} }},
		{"maskFrac at zero", func(e *EvalCfg) { e.CompletionFit.MaskFrac = 0 }},
		{"maskFrac at one", func(e *EvalCfg) { e.CompletionFit.MaskFrac = 1 }},
		{"zero gridStep", func(e *EvalCfg) { e.Fit.GridStep = 0 }},
		{"zero maxWeight", func(e *EvalCfg) { e.Fit.MaxWeight = 0 }},
		{"zero maxPasses", func(e *EvalCfg) { e.Fit.MaxPasses = 0 }},
		{"negative minHoldoutGain", func(e *EvalCfg) { e.Fit.MinHoldoutGain = -0.1 }},
		{"screen q at zero", func(e *EvalCfg) { e.Screen.Q = 0 }},
		{"screen q at one", func(e *EvalCfg) { e.Screen.Q = 1 }},
		{"zero screen minMatches", func(e *EvalCfg) { e.Screen.MinMatches = 0 }},
		{"zero chiSqMinExpected", func(e *EvalCfg) { e.Screen.ChiSqMinExpected = 0 }},
		{"failAlphaMult below one", func(e *EvalCfg) { e.Screen.FailAlphaMult = 0.5 }},
		{"zero naiveBayes alpha", func(e *EvalCfg) { e.NaiveBayes.Alpha = 0 }},
		{"zero knn k", func(e *EvalCfg) { e.Knn.K = 0 }},
		{"zero triples minSupport", func(e *EvalCfg) { e.Triples.MinSupport = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(&c.Eval)
			if err := c.Validate(); err == nil {
				t.Error("validate accepted a bad eval config")
			}
		})
	}
}

func TestLoadRealBackfillEval(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Backfill.Take != 100 || c.Backfill.MaxRequests != 1500 || c.Backfill.TargetMatches != 20000 {
		t.Errorf("backfill = %+v", c.Backfill)
	}
	if c.Backfill.GameMode != 2 || c.Backfill.LobbyType != 1 {
		t.Errorf("backfill ids = mode %d lobby %d, want the probe-verified captains/practice pair", c.Backfill.GameMode, c.Backfill.LobbyType)
	}
	if len(c.Eval.TopK) != 2 || c.Eval.TopK[0] != 1 || c.Eval.TopK[1] != 3 {
		t.Errorf("eval.topK = %v", c.Eval.TopK)
	}
	if c.Paths.MatchesRawDir != "ref/dota2/matches/raw" {
		t.Errorf("paths.matchesRawDir = %q", c.Paths.MatchesRawDir)
	}
	if c.Paths.EvalOut == "" || c.Paths.FitOut == "" || c.Paths.DerivationsPath == "" {
		t.Errorf("eval paths incomplete: %+v", c.Paths)
	}
}
