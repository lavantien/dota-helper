package analytics

import "poolguide/internal/config"

// Prior scores each pool hero as its win rate z-score within the pool. Scores
// are data only: the tier label never enters the computation.
func Prior(wrs []float64, c *config.Config) []float64 {
	return Z(wrs, c.Normalize.ZSdFloor)
}
