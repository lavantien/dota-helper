package analytics

import (
	"math"
	"testing"

	"poolguide/internal/config"
)

func testPriorCfg() *config.Config {
	return &config.Config{
		Normalize: config.NormalizeCfg{ZSdFloor: 0.05},
	}
}

func TestPriorRanksByWinRate(t *testing.T) {
	got := Prior([]float64{0.5, 0.6}, testPriorCfg())
	// two-point z: sd is exactly 0.05, so z = -1 and +1
	if math.Abs(got[0]-(-1)) > 1e-9 || math.Abs(got[1]-1) > 1e-9 {
		t.Errorf("Prior = %v, want [-1 1]", got)
	}
	if got[1] <= got[0] {
		t.Errorf("higher wr must give higher prior: %v", got)
	}
}

func TestPriorIgnoresTierLabels(t *testing.T) {
	// scoring is data only: the tier label never moves a prior
	got := Prior([]float64{0.5, 0.5, 0.5}, testPriorCfg())
	for i := range got {
		if math.Abs(got[i]) > 1e-12 {
			t.Errorf("equal win rates must give equal zero priors, got %v", got)
		}
	}
}

func TestPriorSingleHeroZeroZ(t *testing.T) {
	got := Prior([]float64{0.9}, testPriorCfg())
	if math.Abs(got[0]) > 1e-12 {
		t.Errorf("single hero prior = %v, want 0 (z=0, no tier term)", got[0])
	}
}
