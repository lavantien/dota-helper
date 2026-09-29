package builds

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// heroFieldOrder is the canonical key order for hero blocks in content.json.
var heroFieldOrder = []string{"identity", "why", "how", "when", "build", "timings", "tech", "mechanics"}

type contentDoc struct {
	Patch      json.RawMessage                       `json:"patch"`
	Heroes     map[string]map[string]json.RawMessage `json:"heroes"`
	Principles json.RawMessage                       `json:"principles"`
	Practice   json.RawMessage                       `json:"practice"`
	Picker     json.RawMessage                       `json:"picker"`
}

// renderContent rewrites exactly build, timings, and buildsMeta; every other
// value rides through verbatim, and the output is validated value-for-value
// against the input before anything is written.
func renderContent(orig []byte, updates map[string]heroUpdate, meta string) ([]byte, error) {
	var doc contentDoc
	if err := json.Unmarshal(orig, &doc); err != nil {
		return nil, err
	}
	if doc.Heroes == nil {
		return nil, fmt.Errorf("content.json has no heroes object")
	}
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString("  \"patch\": ")
	sb.Write(doc.Patch)
	sb.WriteString(",\n")
	sb.WriteString("  \"heroes\": {")
	slugs := make([]string, 0, len(doc.Heroes))
	for s := range doc.Heroes {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for i, slug := range slugs {
		fields := doc.Heroes[slug]
		sb.WriteString("\n    ")
		sb.Write(strconv.AppendQuote(nil, slug))
		sb.WriteString(": {")
		keys := make([]string, 0, len(fields))
		known := map[string]bool{}
		for _, k := range heroFieldOrder {
			if _, ok := fields[k]; ok {
				keys = append(keys, k)
				known[k] = true
			}
		}
		var unknown []string
		for k := range fields {
			if !known[k] {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
		keys = append(keys, unknown...)
		for j, k := range keys {
			sb.WriteString("\n      ")
			sb.Write(strconv.AppendQuote(nil, k))
			sb.WriteString(": ")
			u, isUpdate := updates[slug]
			switch {
			case isUpdate && k == "build":
				b, _ := json.Marshal(u.Build)
				sb.Write(b)
			case isUpdate && k == "timings":
				sb.WriteString(u.TimingsJSON)
			default:
				sb.Write(fields[k])
			}
			if j < len(keys)-1 {
				sb.WriteString(",")
			}
		}
		sb.WriteString("\n    }")
		if i < len(slugs)-1 {
			sb.WriteString(",")
		}
	}
	sb.WriteString("\n  },\n")
	sb.WriteString("  \"principles\": ")
	sb.Write(doc.Principles)
	sb.WriteString(",\n")
	sb.WriteString("  \"practice\": ")
	sb.Write(doc.Practice)
	sb.WriteString(",\n")
	if len(doc.Picker) > 0 {
		sb.WriteString("  \"picker\": ")
		sb.Write(doc.Picker)
		sb.WriteString(",\n")
	}
	fmt.Fprintf(&sb, "  \"buildsMeta\": %s\n", strconv.Quote(meta))
	sb.WriteString("}")
	out := []byte(sb.String())

	// value assertion: apart from the owned fields, the parsed document is
	// unchanged in both directions. catches any writer bug that would eat
	// authored prose or inject keys.
	var before, after map[string]any
	if err := json.Unmarshal(orig, &before); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(out, &after); err != nil {
		return nil, fmt.Errorf("rendered content does not parse: %w", err)
	}
	if err := assertSyncPreserved(before, after, updates); err != nil {
		return nil, err
	}
	return out, nil
}

// assertSyncPreserved compares the parsed documents value-for-value, ignoring
// exactly the owned fields (build, timings) on updated heroes and buildsMeta.
func assertSyncPreserved(before, after map[string]any, updates map[string]heroUpdate) error {
	heroesBefore := before["heroes"].(map[string]any)
	heroesAfter := after["heroes"].(map[string]any)
	if len(heroesBefore) != len(heroesAfter) {
		return fmt.Errorf("rendered content changed the hero set")
	}
	for slug, fb := range heroesBefore {
		fa, ok := heroesAfter[slug]
		if !ok {
			return fmt.Errorf("rendered content dropped hero %s", slug)
		}
		mb := fb.(map[string]any)
		ma := fa.(map[string]any)
		_, owned := updates[slug]
		isOwnedField := func(k string) bool {
			return owned && (k == "build" || k == "timings")
		}
		for k, vb := range mb {
			if isOwnedField(k) {
				continue
			}
			if !reflect.DeepEqual(vb, ma[k]) {
				return fmt.Errorf("rendered content changed %s.%s", slug, k)
			}
		}
		for k := range ma {
			if _, wasThere := mb[k]; !wasThere && !isOwnedField(k) {
				return fmt.Errorf("rendered content added %s.%s", slug, k)
			}
		}
	}
	for k, vb := range before {
		if k == "heroes" || k == "buildsMeta" {
			continue
		}
		if !reflect.DeepEqual(vb, after[k]) {
			return fmt.Errorf("rendered content changed top-level %s", k)
		}
	}
	for k := range after {
		if _, wasThere := before[k]; !wasThere && k != "buildsMeta" {
			return fmt.Errorf("rendered content added top-level %s", k)
		}
	}
	return nil
}

type heroUpdate struct {
	Build       string
	TimingsJSON string
}

func timingsJSON(ts []TimingPair) string {
	if len(ts) == 0 {
		return "[]"
	}
	var parts []string
	for _, t := range ts {
		l, _ := json.Marshal(t.Label)
		parts = append(parts, fmt.Sprintf("[%s, %d]", l, t.Minute))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
