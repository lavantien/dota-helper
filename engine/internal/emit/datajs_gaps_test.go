package emit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/config"
)

func TestLoadProseErrorPaths(t *testing.T) {
	if _, err := LoadProse(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("load prose must fail on a missing file")
	}
	bad := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProse(bad)
	if err == nil || !strings.HasPrefix(err.Error(), bad) || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("load prose must wrap the path around the parse error, got %v", err)
	}
}

// TestTimingUnmarshalArms covers the compact [label, minute] pair contract:
// exactly two fields, string label, numeric minute.
func TestTimingUnmarshalArms(t *testing.T) {
	var ok []Timing
	if err := json.Unmarshal([]byte(`[["power spike", 6]]`), &ok); err != nil {
		t.Fatalf("valid timing pair rejected: %v", err)
	}
	if ok[0].Label != "power spike" || ok[0].At != 6 {
		t.Fatalf("timing = %+v", ok[0])
	}
	tests := []struct {
		raw  string
		want string
	}{
		{`[["a"]]`, "timing pair has 1 fields, want 2"},
		{`[["a", 1, 2]]`, "timing pair has 3 fields, want 2"},
		{`[[5, 6]]`, "timing label 5 is not a string"},
		{`[["a", "b"]]`, "timing minute b is not a number"},
		{`[["a", true]]`, "timing minute true is not a number"},
	}
	for _, tt := range tests {
		var ts []Timing
		err := json.Unmarshal([]byte(tt.raw), &ts)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("unmarshal %s = %v, want %q", tt.raw, err, tt.want)
		}
	}
	// a non-array element reaches the pair unmarshal itself and fails there
	var broken []Timing
	if err := json.Unmarshal([]byte("[3]"), &broken); err == nil {
		t.Error("timing element that is not an array must fail the pair unmarshal")
	}
}

// TestMechanicUnmarshalArms covers the compact [tag, text] pair contract:
// exactly two fields, tag strictly tip or counter, string text.
func TestMechanicUnmarshalArms(t *testing.T) {
	var ok []Mechanic
	if err := json.Unmarshal([]byte(`[["tip", "stack camps"], ["counter", "burst him"]]`), &ok); err != nil {
		t.Fatalf("valid mechanic pairs rejected: %v", err)
	}
	if ok[0].Tag != "tip" || ok[0].Text != "stack camps" || ok[1].Tag != "counter" {
		t.Fatalf("mechanics = %+v", ok)
	}
	tests := []struct {
		raw  string
		want string
	}{
		{`[["tip"]]`, "mechanic pair has 1 fields, want 2"},
		{`[[3, "x"]]`, "mechanic tag 3 is not a string"},
		{`[["note", "x"]]`, `mechanic tag "note" is not tip or counter`},
		{`[["tip", 3]]`, "mechanic text 3 is not a string"},
	}
	for _, tt := range tests {
		var ms []Mechanic
		err := json.Unmarshal([]byte(tt.raw), &ms)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("unmarshal %s = %v, want %q", tt.raw, err, tt.want)
		}
	}
	var broken []Mechanic
	if err := json.Unmarshal([]byte("[3]"), &broken); err == nil {
		t.Error("mechanic element that is not an array must fail the pair unmarshal")
	}
}

// the picker orientation block is mandatory: an authored content.json without
// it must fail the emit rather than ship an empty section.
func TestDataJSRequiresPickerOrientation(t *testing.T) {
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	prose.Picker = ProsePicker{}
	if _, err := DataJS(cfg, prose, committedEnemies(t)); err == nil ||
		!strings.Contains(err.Error(), "picker orientation block missing") {
		t.Fatalf("data js must fail without the picker orientation block, got %v", err)
	}
}

// a pool hero the authored prose never covers fails the pool section by name.
func TestDataJSRequiresProseForEachPoolHero(t *testing.T) {
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	ghost := "ghost-hero"
	cfg.Pool[0].Slug = ghost
	if _, err := DataJS(cfg, prose, committedEnemies(t)); err == nil ||
		!strings.Contains(err.Error(), "no prose for pool hero "+ghost) {
		t.Fatalf("data js must fail on a pool hero without prose, got %v", err)
	}
}

// the live pool carries multi-role heroes (the dual-seat cores), so the
// fixture starts from a single-role hero and the merge contract must still
// sort the synthetic pair ascending.
func TestDataJSMultiRoleHeroRolesSorted(t *testing.T) {
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	counts := map[string]int{}
	for _, e := range cfg.Pool {
		counts[e.Slug]++
	}
	origIdx := -1
	for i, e := range cfg.Pool {
		if counts[e.Slug] == 1 {
			origIdx = i
			break
		}
	}
	if origIdx < 0 {
		t.Fatal("no single-role pool hero to build the fixture from")
	}
	orig := cfg.Pool[origIdx]
	second := "5"
	if orig.Role == "5" {
		second = "1"
	}
	dup := orig
	dup.Role = second
	// whichever way keeps the greater role first in pool order, the merge must
	// swap the pair while sorting
	if second > orig.Role {
		cfg.Pool = append([]config.PoolEntry{dup}, cfg.Pool...)
	} else {
		cfg.Pool = append(cfg.Pool, dup)
	}
	out, err := DataJS(cfg, prose, committedEnemies(t))
	if err != nil {
		t.Fatalf("data js over a multi-role hero: %v", err)
	}
	low, high := orig.Role, second
	if low > high {
		low, high = high, low
	}
	want := "roles: ['" + low + "', '" + high + "']"
	if !strings.Contains(string(out), want) {
		t.Fatalf("multi-role hero roles must render sorted ascending, want %s", want)
	}
}
