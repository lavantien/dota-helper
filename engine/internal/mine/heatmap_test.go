package mine

import "testing"

func TestHeatmapEnemiesRanksShareDescSlugTiebreak(t *testing.T) {
	pops := map[string]float64{
		"axe":              0.12,
		"centaur":          0.08,
		"phantom-assassin": 0.20,
		"phantom-lancer":   0.20,
		"sniper":           0.02,
	}
	got, err := HeatmapEnemies(pops, 3)
	if err != nil {
		t.Fatalf("HeatmapEnemies: %v", err)
	}
	// the 0.20 tie resolves slug-ascending
	want := []string{"phantom-assassin", "phantom-lancer", "axe"}
	if len(got) != len(want) {
		t.Fatalf("got %d columns, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("column %d = %s, want %s, full order %v", i, got[i], want[i], got)
		}
	}
}

func TestHeatmapEnemiesKeepsPoolMirrors(t *testing.T) {
	pops := map[string]float64{"phantom-lancer": 0.5, "axe": 0.3, "centaur": 0.2}
	got, err := HeatmapEnemies(pops, 3)
	if err != nil {
		t.Fatalf("HeatmapEnemies: %v", err)
	}
	if got[0] != "phantom-lancer" {
		t.Fatalf("a pool hero at the top share must stay column 1, got %v", got)
	}
}

func TestHeatmapEnemiesDeterministicAcrossMapIteration(t *testing.T) {
	pops := map[string]float64{"a": 0.30, "b": 0.10, "c": 0.25, "d": 0.15, "e": 0.20}
	want := []string{"a", "c", "e"}
	for i := 0; i < 25; i++ {
		got, err := HeatmapEnemies(pops, 3)
		if err != nil {
			t.Fatalf("HeatmapEnemies: %v", err)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("run %d column %d = %s, want %s", i, j, got[j], want[j])
			}
		}
	}
}

func TestHeatmapEnemiesRejectsShortCoverageAndBadCount(t *testing.T) {
	pops := map[string]float64{"axe": 0.6, "centaur": 0.4}
	if _, err := HeatmapEnemies(pops, 3); err == nil {
		t.Fatalf("expected an error when popularity covers fewer heroes than the column count")
	}
	if _, err := HeatmapEnemies(pops, 0); err == nil {
		t.Fatalf("expected an error for a zero column count")
	}
	if _, err := HeatmapEnemies(pops, -1); err == nil {
		t.Fatalf("expected an error for a negative column count")
	}
}
