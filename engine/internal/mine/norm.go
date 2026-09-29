package mine

import (
	"poolguide/internal/analytics"
	"poolguide/internal/config"
)

// Cell kinds, the pair_shrunk / norm_cell vocabulary: enemy matchups and
// ally synergies.
const (
	KindEnemy = "enemy"
	KindAlly  = "ally"
)

// NormCell is one normalized pair value for a pool hero.
type NormCell struct {
	Hero, Other, Kind string
	Pct, Z            float64
	Completed         bool
}

// NormalizeMatrix completes one shrunk matrix and normalizes the merged
// result. Rows are the pool heroes (slug ascending), columns the roster in
// hero_id order. Cells that are missing or observed too thinly
// (thresholds.thinCellMatches) are refilled by ALS completion and flagged;
// observed cells pass through unchanged. After the merge every pool hero's
// full row is re-normalized: pct is the midrank percentile across the whole
// row and z its standardization.
func NormalizeMatrix(pool, roster []string, cells []Cell, kind string, cfg *config.Config) []NormCell {
	lookup := map[string]map[string]Cell{}
	for _, c := range cells {
		if c.Hero != "" && c.Other != "" {
			if lookup[c.Hero] == nil {
				lookup[c.Hero] = map[string]Cell{}
			}
			lookup[c.Hero][c.Other] = c
		}
	}
	m := analytics.Matrix{
		Values:   make([][]float64, len(pool)),
		Observed: make([][]bool, len(pool)),
	}
	for i, hero := range pool {
		m.Values[i] = make([]float64, len(roster))
		m.Observed[i] = make([]bool, len(roster))
		for j, other := range roster {
			c, ok := lookup[hero][other]
			if !ok || c.N < int64(cfg.Thresholds.ThinCellMatches) {
				continue
			}
			m.Values[i][j] = c.Value
			m.Observed[i][j] = true
		}
	}
	merged := analytics.Complete(m, cfg.Completion)

	out := make([]NormCell, 0, len(pool)*len(roster))
	for i, hero := range pool {
		pcts := analytics.Midrank(merged.Values[i])
		zs := analytics.Z(pcts, cfg.Normalize.ZSdFloor)
		for j, other := range roster {
			out = append(out, NormCell{
				Hero: hero, Other: other, Kind: kind,
				Pct:       pcts[j],
				Z:         zs[j],
				Completed: !m.Observed[i][j],
			})
		}
	}
	return out
}
