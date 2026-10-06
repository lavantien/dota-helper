// Package order re-sorts the authored pool order from mined per-position win
// rates (hero_position, the scoped divine-immortal ranked all pick winDay
// crawl). It owns exactly ordering in two files, the config.json pool array
// and the gates.json fallbackOrder arrays; membership is never touched and
// every other byte rides through verbatim.
package order

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"poolguide/internal/config"
	"poolguide/internal/gates"
)

// Deps carries the live db handle, the loaded hub, and the authored config
// path to rewrite in place.
type Deps struct {
	DB      *sql.DB
	Cfg     *config.Config
	CfgPath string
}

// posStat is one hero's mined aggregate at one farm position.
type posStat struct {
	WR        float64
	PickCount int64
}

func loadPositionWR(db *sql.DB) (map[string]map[int]posStat, string, error) {
	rows, err := db.Query(`SELECT slug, position, pick_count, win_count, scrape_date
		FROM hero_position WHERE source = 'stratz_winday'`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	stats := map[string]map[int]posStat{}
	scrape := ""
	for rows.Next() {
		var slug string
		var position int
		var picks, wins int64
		var date string
		if err := rows.Scan(&slug, &position, &picks, &wins, &date); err != nil {
			return nil, "", err
		}
		if stats[slug] == nil {
			stats[slug] = map[int]posStat{}
		}
		if picks > 0 {
			stats[slug][position] = posStat{WR: float64(wins) / float64(picks), PickCount: picks}
		}
		if date > scrape {
			scrape = date
		}
	}
	return stats, scrape, rows.Err()
}

// roleOrders sorts each role's pool entries by win rate at that position
// descending, pick count descending, slug ascending. A hero without a row at
// its pooled position is absent data, not a sort-last fact: every miss is
// reported at once.
func roleOrders(cfg *config.Config, stats map[string]map[int]posStat) (map[string][]config.PoolEntry, error) {
	type roleEntry struct {
		entry config.PoolEntry
		stat  posStat
	}
	var missing []string
	grouped := map[string][]roleEntry{}
	for _, e := range cfg.Pool {
		pos, err := strconv.Atoi(e.Role)
		if err != nil {
			return nil, fmt.Errorf("order: pool entry %s has non-numeric role %q", e.Slug, e.Role)
		}
		st, ok := stats[e.Slug][pos]
		if !ok || st.PickCount <= 0 {
			missing = append(missing, e.Slug+"@"+e.Role)
			continue
		}
		grouped[e.Role] = append(grouped[e.Role], roleEntry{entry: e, stat: st})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("order: no hero_position row for %s (run make fetch-positions && make ingest)", strings.Join(missing, ", "))
	}
	ordered := map[string][]config.PoolEntry{}
	for role, entries := range grouped {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].stat.WR != entries[j].stat.WR {
				return entries[i].stat.WR > entries[j].stat.WR
			}
			if entries[i].stat.PickCount != entries[j].stat.PickCount {
				return entries[i].stat.PickCount > entries[j].stat.PickCount
			}
			return entries[i].entry.Slug < entries[j].entry.Slug
		})
		out := make([]config.PoolEntry, len(entries))
		for i, re := range entries {
			out[i] = re.entry
		}
		ordered[role] = out
	}
	return ordered, nil
}

// Run sorts each role's pool entries and the gates fallbackOrder by win rate
// at that position and rewrites both authored files in place.
func Run(deps Deps) error {
	stats, scrape, err := loadPositionWR(deps.DB)
	if err != nil {
		return fmt.Errorf("order: load hero_position: %w", err)
	}
	ordered, err := roleOrders(deps.Cfg, stats)
	if err != nil {
		return err
	}
	gatesPath := filepath.Join(deps.Cfg.Paths.PickerDir, "gates.json")
	cfgOrig, err := os.ReadFile(deps.CfgPath)
	if err != nil {
		return err
	}
	gatesOrig, err := os.ReadFile(gatesPath)
	if err != nil {
		return err
	}
	doc, err := gates.Load(gatesPath)
	if err != nil {
		return err
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	if err := assertGatesInSync(doc, deps.Cfg); err != nil {
		return err
	}

	orderedFlat := make([]config.PoolEntry, 0, len(deps.Cfg.Pool))
	for _, r := range deps.Cfg.Roles {
		if len(ordered[r.ID]) == 0 {
			return fmt.Errorf("order: role %s has no pool entries", r.ID)
		}
		orderedFlat = append(orderedFlat, ordered[r.ID]...)
	}
	cfgOut, err := renderConfigPool(cfgOrig, orderedFlat)
	if err != nil {
		return err
	}
	gatesOut, err := renderGatesFallback(gatesOrig, deps.Cfg.Roles, ordered)
	if err != nil {
		return err
	}
	if err := assertConfigPreserved(cfgOrig, cfgOut, orderedFlat); err != nil {
		return err
	}
	if err := assertGatesPreserved(gatesOrig, gatesOut, deps.Cfg.Roles, ordered); err != nil {
		return err
	}
	if err := validateRenderedConfig(cfgOut, orderedFlat); err != nil {
		return err
	}
	if err := validateRenderedGates(gatesOut, deps.Cfg.Roles, ordered); err != nil {
		return err
	}

	for _, r := range deps.Cfg.Roles {
		slugs := make([]string, 0, len(ordered[r.ID]))
		for _, e := range ordered[r.ID] {
			slugs = append(slugs, e.Slug)
		}
		fmt.Printf("order pos %s: %s\n", r.ID, strings.Join(slugs, ", "))
	}
	if bytes.Equal(cfgOut, cfgOrig) && bytes.Equal(gatesOut, gatesOrig) {
		fmt.Printf("order: pool already current (positions scrape %s)\n", scrape)
		return nil
	}
	if err := os.WriteFile(deps.CfgPath, cfgOut, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(gatesPath, gatesOut, 0o644); err != nil {
		return err
	}
	fmt.Printf("order: rewrote pool order in %s and %s (positions scrape %s)\n", deps.CfgPath, gatesPath, scrape)
	return nil
}

// assertGatesInSync refuses to order anything while the authored fallback
// sets and the config pool disagree; ordering never repairs membership.
func assertGatesInSync(doc *gates.Doc, cfg *config.Config) error {
	poolSets := map[string]map[string]bool{}
	for _, r := range cfg.Roles {
		poolSets[r.ID] = map[string]bool{}
	}
	for _, e := range cfg.Pool {
		poolSets[e.Role][e.Slug] = true
	}
	var problems []string
	for rid, fb := range doc.FallbackOrder {
		set, ok := poolSets[rid]
		if !ok {
			problems = append(problems, fmt.Sprintf("role %s is not a config role", rid))
			continue
		}
		seen := map[string]bool{}
		for _, s := range fb {
			if !set[s] {
				problems = append(problems, fmt.Sprintf("role %s fallback lists %s outside the pool", rid, s))
			}
			seen[s] = true
		}
		if len(seen) != len(set) {
			problems = append(problems, fmt.Sprintf("role %s fallback covers %d of %d pool heroes", rid, len(seen), len(set)))
		}
	}
	for rid := range poolSets {
		if _, ok := doc.FallbackOrder[rid]; !ok {
			problems = append(problems, fmt.Sprintf("role %s missing from fallbackOrder", rid))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("order: gates fallbackOrder disagrees with the config pool: %s", strings.Join(problems, "; "))
	}
	return nil
}

// findTopLevelValue locates `\n  "<key>":` (both authored hub files keep
// top-level keys at two-space indent) and returns the index of the value's
// first byte.
func findTopLevelValue(src []byte, key string) (int, error) {
	anchor := "\n  \"" + key + "\":"
	at := bytes.Index(src, []byte(anchor))
	if at < 0 {
		return 0, fmt.Errorf("order: top-level key %q not found", key)
	}
	if bytes.Contains(src[at+len(anchor):], []byte(anchor)) {
		return 0, fmt.Errorf("order: top-level key %q is ambiguous", key)
	}
	i := at + len(anchor)
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i, nil
}

// matchBracket finds the closing bracket for the opener at src[open],
// honoring string literals so prose brackets cannot fool the scan.
func matchBracket(src []byte, open int) (int, error) {
	var openCh, closeCh byte
	switch src[open] {
	case '[':
		openCh, closeCh = '[', ']'
	case '{':
		openCh, closeCh = '{', '}'
	default:
		return 0, fmt.Errorf("order: expected a bracket at %d, found %q", open, src[open])
	}
	depth, inStr := 0, false
	for i := open; i < len(src); i++ {
		c := src[i]
		if inStr {
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("order: unmatched %q at %d", openCh, open)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func renderPoolEntry(e config.PoolEntry) string {
	return fmt.Sprintf("\n    {\n      \"slug\": %s,\n      \"name\": %s,\n      \"role\": %s,\n      \"tier\": %s\n    }",
		jsonString(e.Slug), jsonString(e.Name), jsonString(e.Role), jsonString(e.Tier))
}

func renderFallbackRole(role string, slugs []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n    %s: [", jsonString(role))
	for _, s := range slugs {
		fmt.Fprintf(&sb, "\n      %s,", jsonString(s))
	}
	return strings.TrimSuffix(sb.String(), ",") + "\n    ]"
}

// renderConfigPool splices the pool array body in place; every byte outside
// the array rides through verbatim.
func renderConfigPool(src []byte, ordered []config.PoolEntry) ([]byte, error) {
	valAt, err := findTopLevelValue(src, "pool")
	if err != nil {
		return nil, err
	}
	closeAt, err := matchBracket(src, valAt)
	if err != nil {
		return nil, err
	}
	parts := make([]string, 0, len(ordered))
	for _, e := range ordered {
		parts = append(parts, renderPoolEntry(e))
	}
	out := make([]byte, 0, len(src)+256)
	out = append(out, src[:valAt+1]...)
	out = append(out, []byte(strings.Join(parts, ",")+"\n  ")...)
	out = append(out, src[closeAt:]...)
	return out, nil
}

// renderGatesFallback splices the fallbackOrder object body in place.
func renderGatesFallback(src []byte, roles []config.RoleDef, ordered map[string][]config.PoolEntry) ([]byte, error) {
	valAt, err := findTopLevelValue(src, "fallbackOrder")
	if err != nil {
		return nil, err
	}
	closeAt, err := matchBracket(src, valAt)
	if err != nil {
		return nil, err
	}
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		slugs := make([]string, 0, len(ordered[r.ID]))
		for _, e := range ordered[r.ID] {
			slugs = append(slugs, e.Slug)
		}
		parts = append(parts, renderFallbackRole(r.ID, slugs))
	}
	out := make([]byte, 0, len(src)+256)
	out = append(out, src[:valAt+1]...)
	out = append(out, []byte(strings.Join(parts, ",")+"\n  ")...)
	out = append(out, src[closeAt:]...)
	return out, nil
}

// assertConfigPreserved compares the parsed documents value-for-value: apart
// from the pool ordering, nothing changes in either direction.
func assertConfigPreserved(orig, out []byte, ordered []config.PoolEntry) error {
	var before, after map[string]any
	if err := json.Unmarshal(orig, &before); err != nil {
		return err
	}
	if err := json.Unmarshal(out, &after); err != nil {
		return fmt.Errorf("order: rendered config does not parse: %w", err)
	}
	for k, vb := range before {
		if k == "pool" {
			continue
		}
		if !reflect.DeepEqual(vb, after[k]) {
			return fmt.Errorf("order: rendered config changed top-level %s", k)
		}
	}
	for k := range after {
		if _, wasThere := before[k]; !wasThere {
			return fmt.Errorf("order: rendered config added top-level %s", k)
		}
	}
	if !multisetEqual(entryKeys(before["pool"]), entryKeys(after["pool"])) {
		return fmt.Errorf("order: rendered config changed the pool membership")
	}
	got := entryKeys(after["pool"])
	want := make([]string, 0, len(ordered))
	for _, e := range ordered {
		want = append(want, e.Slug+"@"+e.Role)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		return fmt.Errorf("order: rendered pool order %s does not match the computed order", strings.Join(got, " "))
	}
	return nil
}

// assertGatesPreserved compares the parsed documents value-for-value: apart
// from the fallbackOrder ordering, nothing changes in either direction.
func assertGatesPreserved(orig, out []byte, roles []config.RoleDef, ordered map[string][]config.PoolEntry) error {
	var before, after map[string]any
	if err := json.Unmarshal(orig, &before); err != nil {
		return err
	}
	if err := json.Unmarshal(out, &after); err != nil {
		return fmt.Errorf("order: rendered gates does not parse: %w", err)
	}
	for k, vb := range before {
		if k == "fallbackOrder" {
			continue
		}
		if !reflect.DeepEqual(vb, after[k]) {
			return fmt.Errorf("order: rendered gates changed top-level %s", k)
		}
	}
	for k := range after {
		if _, wasThere := before[k]; !wasThere {
			return fmt.Errorf("order: rendered gates added top-level %s", k)
		}
	}
	fbBefore := before["fallbackOrder"].(map[string]any)
	fbAfter := after["fallbackOrder"].(map[string]any)
	for _, r := range roles {
		sorted := func(v any) string {
			list := make([]string, 0)
			for _, s := range v.([]any) {
				list = append(list, s.(string))
			}
			sort.Strings(list)
			return strings.Join(list, " ")
		}
		if sorted(fbBefore[r.ID]) != sorted(fbAfter[r.ID]) {
			return fmt.Errorf("order: rendered fallbackOrder role %s changed its set", r.ID)
		}
		want := make([]string, 0, len(ordered[r.ID]))
		for _, e := range ordered[r.ID] {
			want = append(want, e.Slug)
		}
		var got []string
		for _, s := range fbAfter[r.ID].([]any) {
			got = append(got, s.(string))
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			return fmt.Errorf("order: rendered fallbackOrder role %s does not match the computed order", r.ID)
		}
	}
	return nil
}

// entryKeys renders each pool entry as slug@role in document order.
func entryKeys(v any) []string {
	arr := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		m := e.(map[string]any)
		out = append(out, m["slug"].(string)+"@"+m["role"].(string))
	}
	return out
}

func multisetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, k := range a {
		counts[k]++
	}
	for _, k := range b {
		counts[k]--
		if counts[k] < 0 {
			return false
		}
	}
	return true
}

// validateRenderedConfig reloads the rendered bytes through the full hub
// loader; a splice that breaks any validation rule fails here, before write.
func validateRenderedConfig(out []byte, ordered []config.PoolEntry) error {
	tmp, err := os.CreateTemp("", "order-config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	cfg, err := config.Load(tmp.Name())
	if err != nil {
		return fmt.Errorf("order: rendered config fails hub validation: %w", err)
	}
	if len(cfg.Pool) != len(ordered) {
		return fmt.Errorf("order: rendered config pool has %d entries, want %d", len(cfg.Pool), len(ordered))
	}
	for i := range ordered {
		if !reflect.DeepEqual(cfg.Pool[i], ordered[i]) {
			return fmt.Errorf("order: rendered config pool entry %d is %+v, want %+v", i, cfg.Pool[i], ordered[i])
		}
	}
	return nil
}

func validateRenderedGates(out []byte, roles []config.RoleDef, ordered map[string][]config.PoolEntry) error {
	var doc gates.Doc
	if err := json.Unmarshal(out, &doc); err != nil {
		return fmt.Errorf("order: rendered gates does not parse: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("order: rendered gates fails validation: %w", err)
	}
	for _, r := range roles {
		want := make([]string, 0, len(ordered[r.ID]))
		for _, e := range ordered[r.ID] {
			want = append(want, e.Slug)
		}
		if !reflect.DeepEqual(doc.FallbackOrder[r.ID], want) {
			return fmt.Errorf("order: rendered gates fallbackOrder role %s does not match the computed order", r.ID)
		}
	}
	return nil
}
