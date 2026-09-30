package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestValidateRejectsBadRoles covers the role-table arms: duplicate ids first,
// then the fixed five-role count the pool grouping depends on.
func TestValidateRejectsBadRoles(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*Config)
		wantErr string
	}{
		{"duplicate role id", func(c *Config) { c.Roles = append(c.Roles, c.Roles[0]) },
			`duplicate role id "1"`},
		{"four roles", func(c *Config) { c.Roles = c.Roles[:4] },
			"want 5 roles, got 4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(c)
			err = c.Validate()
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidateRejectsBadPoolEntries covers the per-entry arms: duplicate
// slug@role pairs, roles outside the role table, and tiers outside the
// dedicated/flex vocabulary. the messages name the pool entry, so each want
// is built from the first entry the mutation lands on.
func TestValidateRejectsBadPoolEntries(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want func(slug, key string) string
	}{
		{"duplicate entry", func(c *Config) { c.Pool = append(c.Pool, c.Pool[0]) },
			func(slug, key string) string { return "duplicate pool entry " + key }},
		{"unknown role", func(c *Config) { c.Pool[0].Role = "6" },
			func(slug, key string) string { return `pool entry ` + slug + ` has unknown role "6"` }},
		{"unknown tier", func(c *Config) { c.Pool[0].Tier = "bench" },
			func(slug, key string) string { return `pool entry ` + slug + ` has unknown tier "bench"` }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			slug := c.Pool[0].Slug
			key := slug + "@" + c.Pool[0].Role
			tt.mut(c)
			err = c.Validate()
			if err == nil || err.Error() != tt.want(slug, key) {
				t.Fatalf("validate error = %v, want %q", err, tt.want(slug, key))
			}
		})
	}
}

// TestValidateRejectsBadFlatFloors covers the single-value floors across the
// hub sections: each error names its own key so a bad value is findable.
func TestValidateRejectsBadFlatFloors(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*Config)
		wantErr string
	}{
		{"missing gateDelta bonus", func(c *Config) { delete(c.GateDeltas, "bonus") },
			`gateDeltas missing "bonus"`},
		{"zero burst cap", func(c *Config) { c.Stratz.BurstCapPerSec = 0 },
			"stratz rate limits must be positive"},
		{"zero request interval", func(c *Config) { c.Stratz.RequestIntervalMs = 0 },
			"stratz rate limits must be positive"},
		{"zero maxAbsSynergy", func(c *Config) { c.Stratz.MaxAbsSynergy = 0 },
			"stratz.maxAbsSynergy must be positive, 0 would drop every row"},
		{"zero emit decimals", func(c *Config) { c.Emit.Decimals = 0 },
			"emit.decimals must be positive"},
		{"zero heatmap enemyCount", func(c *Config) { c.Heatmap.EnemyCount = 0 },
			"heatmap.enemyCount must be positive"},
		{"zero enemySlots", func(c *Config) { c.Score.EnemySlots = 0 },
			"score slot and divisor constants must be positive"},
		{"zero allySlots", func(c *Config) { c.Score.AllySlots = 0 },
			"score slot and divisor constants must be positive"},
		{"zero flexHalf", func(c *Config) { c.Score.FlexHalf = 0 },
			"score slot and divisor constants must be positive"},
		{"zero flexCap", func(c *Config) { c.Score.FlexCap = 0 },
			"score.flexCap must be >= 1"},
		{"zero midPct", func(c *Config) { c.Score.MidPct = 0 },
			"score.midPct must be in (0,1)"},
		{"unit midPct", func(c *Config) { c.Score.MidPct = 1 },
			"score.midPct must be in (0,1)"},
		{"zero zSdFloor", func(c *Config) { c.Normalize.ZSdFloor = 0 },
			"normalize.zSdFloor must be positive"},
		{"zero pivotFloor", func(c *Config) { c.Completion.PivotFloor = 0 },
			"completion.pivotFloor must be positive"},
		{"negative autoseedCap", func(c *Config) { c.Thresholds.AutoseedCap = -1 },
			"thresholds.autoseedCap must be >= 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(realConfig)
			if err != nil {
				t.Fatal(err)
			}
			tt.mut(c)
			err = c.Validate()
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoadErrorPaths pins the three load failures: unreadable file, malformed
// json, and a parseable hub that fails validation, each naming the path.
func TestLoadErrorPaths(t *testing.T) {
	dir := t.TempDir()

	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("load accepted a missing file")
	} else if !os.IsNotExist(err) {
		t.Fatalf("missing-file error = %v, want a not-exist error", err)
	}

	badJSON := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(badJSON, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(badJSON)
	if err == nil {
		t.Fatal("load accepted malformed json")
	}
	if !strings.HasPrefix(err.Error(), badJSON) {
		t.Fatalf("malformed-json error %q does not name the path", err)
	}

	badHub := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(badHub, []byte(`{"patch": "7.41f"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(badHub)
	if err == nil {
		t.Fatal("load accepted a hub that fails validation")
	}
	if !strings.HasPrefix(err.Error(), badHub) || !strings.Contains(err.Error(), "want 5 roles") {
		t.Fatalf("invalid-hub error %q does not wrap the path and the validate error", err)
	}
}

// TestHeroBySlug covers the merge lookup both ways: found returns the merged
// hero, unknown slugs return nil. the fixture picks a single-role entry so
// the merged roles pin exactly, independent of the dual-seat cores.
func TestHeroBySlug(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range c.Pool {
		counts[e.Slug]++
	}
	first := c.Pool[0]
	for _, e := range c.Pool {
		if counts[e.Slug] == 1 {
			first = e
			break
		}
	}
	h := c.HeroBySlug(first.Slug)
	if h == nil {
		t.Fatalf("HeroBySlug(%q) = nil, want the merged hero", first.Slug)
	}
	if h.Slug != first.Slug || h.Name != first.Name {
		t.Fatalf("HeroBySlug(%q) = %+v, want slug and name from the pool entry", first.Slug, h)
	}
	wantRoles := []string{first.Role}
	if !reflect.DeepEqual(h.Roles, wantRoles) {
		t.Errorf("HeroBySlug(%q).Roles = %v, want %v", first.Slug, h.Roles, wantRoles)
	}
	if c.HeroBySlug("not-a-pool-slug") != nil {
		t.Error("HeroBySlug on an unknown slug must return nil")
	}
}

// TestPoolSlugsUniqueAscending pins the dedup + sort contract the picker's
// parallel arrays rely on.
func TestPoolSlugsUniqueAscending(t *testing.T) {
	c, err := Load(realConfig)
	if err != nil {
		t.Fatal(err)
	}
	slugs := c.PoolSlugs()
	seen := map[string]bool{}
	for i, s := range slugs {
		if seen[s] {
			t.Errorf("PoolSlugs repeats %s", s)
		}
		seen[s] = true
		if i > 0 && s < slugs[i-1] {
			t.Errorf("PoolSlugs not ascending: %s after %s", s, slugs[i-1])
		}
	}
	if len(slugs) == 0 {
		t.Fatal("PoolSlugs is empty over the real hub")
	}
}

// TestPatchSeries covers the letter-suffix strip across patch shapes,
// including no-suffix and letters-only inputs.
func TestPatchSeries(t *testing.T) {
	cases := map[string]string{
		"7.41f":  "7.41",
		"7.41c":  "7.41",
		"7.41":   "7.41",
		"7.41bc": "7.41",
		"":       "",
		"abc":    "",
	}
	for in, want := range cases {
		if got := PatchSeries(in); got != want {
			t.Errorf("PatchSeries(%q) = %q, want %q", in, got, want)
		}
	}
}
