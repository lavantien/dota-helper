package builds

import (
	"encoding/json"
	"strings"
	"testing"
)

// renderContent must reject a document it cannot parse, one without a
// heroes object, and one whose values parse into the raw-shape struct but
// not into the value map the preservation assertion needs (1e999 overflows
// float64 only on the second unmarshal route).
func TestRenderContentRejectsMalformedDocs(t *testing.T) {
	cases := []struct {
		name string
		orig string
		want string
	}{
		{"broken json", `{"patch": `, "unexpected end of JSON input"},
		{"no heroes object", `{}`, "no heroes object"},
		{
			"unparsable values",
			`{"patch": [1e999], "heroes": {"zeta": {"build": "old"}}}`,
			"cannot unmarshal number 1e999",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := renderContent([]byte(tc.orig), nil, "meta")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// a timings blob that does not parse must never reach content.json: the
// post-render parse check throws the whole render away
func TestRenderContentRejectsBadTimingsBlob(t *testing.T) {
	updates := map[string]heroUpdate{"zeta": {Build: "b", TimingsJSON: "trailing junk"}}
	_, err := renderContent([]byte(testContent), updates, "meta")
	if err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("error = %v, want the rendered-parse rejection", err)
	}
}

// hero fields outside the canonical order ride through verbatim and land
// after the known keys, so nothing authored is dropped or reordered
func TestRenderContentUnknownHeroFieldsRideLast(t *testing.T) {
	orig := strings.Replace(testContent, `"mechanics": [["tip", "kept"]]`,
		`"mechanics": [["tip", "kept"]], "extra": {"nested": true}`, 1)
	out, err := renderContent([]byte(orig), nil, "meta line")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rendered doc does not parse: %v\n%s", err, out)
	}
	h := doc["heroes"].(map[string]any)["zeta"].(map[string]any)
	if h["extra"].(map[string]any)["nested"] != true {
		t.Errorf("unknown field value changed: %v", h["extra"])
	}
	if strings.Index(string(out), `"mechanics"`) > strings.Index(string(out), `"extra"`) {
		t.Errorf("unknown field must sort after the canonical keys:\n%s", out)
	}
}

// the preservation assertion itself catches every drift class: a shrunken
// hero set, a swapped slug, a changed authored field, a changed top-level
// section
func TestAssertSyncPreservedCatchesDrift(t *testing.T) {
	base := map[string]any{
		"heroes": map[string]any{
			"zeta":  map[string]any{"identity": "a"},
			"alpha": map[string]any{"identity": "b"},
		},
		"patch": map[string]any{"headline": "h"},
	}
	cases := []struct {
		name   string
		before map[string]any
		after  map[string]any
		want   string
	}{
		{
			"hero set shrank",
			base,
			map[string]any{
				"heroes": map[string]any{"zeta": map[string]any{"identity": "a"}},
				"patch":  base["patch"],
			},
			"changed the hero set",
		},
		{
			"hero swapped",
			base,
			map[string]any{
				"heroes": map[string]any{
					"zeta": map[string]any{"identity": "a"},
					"beta": map[string]any{"identity": "b"},
				},
				"patch": base["patch"],
			},
			"dropped hero alpha",
		},
		{
			"authored field changed",
			base,
			map[string]any{
				"heroes": map[string]any{
					"zeta":  map[string]any{"identity": "tampered"},
					"alpha": map[string]any{"identity": "b"},
				},
				"patch": base["patch"],
			},
			"changed zeta.identity",
		},
		{
			"top level changed",
			base,
			map[string]any{
				"heroes": base["heroes"],
				"patch":  map[string]any{"headline": "other"},
			},
			"changed top-level patch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertSyncPreserved(tc.before, tc.after, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestTimingsJSONEmpty(t *testing.T) {
	if got := timingsJSON(nil); got != "[]" {
		t.Errorf("timingsJSON(nil) = %s, want []", got)
	}
}
