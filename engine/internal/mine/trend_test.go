package mine

import (
	"math"
	"testing"
)

func TestTrendsLatestPairDeltas(t *testing.T) {
	rows := []TrendRow{
		{"2026-09-27", "marci", 0.10, 0.50},
		{"2026-09-27", "axe", 0.05, 0.52},
		{"2026-10-04", "marci", 0.12, 0.515},
		{"2026-10-04", "axe", 0.05, 0.52},
		{"2026-10-11", "marci", 0.09, 0.525},
		{"2026-10-11", "axe", 0.06, 0.51},
	}
	d := Trends([]string{"marci", "axe"}, rows)
	m, ok := d["marci"]
	if !ok {
		t.Fatal("marci missing a delta")
	}
	// only the latest pair compares: 2026-10-11 vs 2026-10-04
	// wr 0.515 -> 0.525 = +1pp, share 0.12 -> 0.09 = -3pp
	if math.Abs(m.WrDeltaPP-1.0) > 1e-9 || math.Abs(m.ShareDeltaPP+3.0) > 1e-9 {
		t.Fatalf("marci delta wrong: %+v", m)
	}
	if m.FromDate != "2026-10-04" || m.ToDate != "2026-10-11" {
		t.Fatalf("marci window wrong: %+v", m)
	}
	a := d["axe"]
	if math.Abs(a.WrDeltaPP+1.0) > 1e-9 || math.Abs(a.ShareDeltaPP-1.0) > 1e-9 {
		t.Fatalf("axe delta wrong: %+v", a)
	}
	// heroes with one snapshot carry no delta
	one := Trends([]string{"marci"}, rows[:2])
	if _, ok := one["marci"]; ok {
		t.Fatal("single snapshot must not produce a delta")
	}
	// heroes with no rows at all stay absent
	if _, ok := Trends([]string{"phantom-lancer"}, rows)["phantom-lancer"]; ok {
		t.Fatal("hero without snapshots must not produce a delta")
	}
}
