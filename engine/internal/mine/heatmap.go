package mine

import (
	"fmt"
	"sort"
)

// HeatmapEnemies ranks the roster by pick share descending, slug ascending
// tiebreak, and returns the top count as the guide heatmap's enemy columns.
// Pool heroes are eligible: a popular pool hero is a common enemy.
func HeatmapEnemies(pops map[string]float64, count int) ([]string, error) {
	if count <= 0 {
		return nil, fmt.Errorf("mine: heatmap enemy count must be positive, got %d", count)
	}
	if len(pops) < count {
		return nil, fmt.Errorf("mine: popularity covers %d heroes, want %d heatmap enemies", len(pops), count)
	}
	slugs := make([]string, 0, len(pops))
	for slug := range pops {
		slugs = append(slugs, slug)
	}
	sort.Slice(slugs, func(i, j int) bool {
		if pops[slugs[i]] != pops[slugs[j]] {
			return pops[slugs[i]] > pops[slugs[j]]
		}
		return slugs[i] < slugs[j]
	})
	return slugs[:count], nil
}
