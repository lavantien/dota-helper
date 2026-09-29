package builds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
	"poolguide/internal/ingest"
)

func testIndex() *itemIndex {
	mk := func(id int, short string, cost int) rawItem {
		it := rawItem{ID: id, ShortName: short}
		it.Stat.Cost = cost
		return it
	}
	items := []rawItem{
		mk(1, "blink", 2250),
		mk(10, "oblivion_staff", 1625),
		mk(11, "orchid", 3275),
		mk(12, "manta", 4650),
		mk(13, "yasha", 2100),
		mk(14, "magic_wand", 460),
		mk(15, "heart", 5300),
		{ID: 20, ShortName: "recipe_orchid", Components: []struct {
			ComponentID int `json:"componentId"`
		}{{ComponentID: 10}}},
		{ID: 21, ShortName: "recipe_manta", Components: []struct {
			ComponentID int `json:"componentId"`
		}{{ComponentID: 13}}},
	}
	for i := range items {
		if strings.HasPrefix(items[i].ShortName, "recipe_") {
			items[i].Stat.IsRecipe = true
		}
	}
	ix := &itemIndex{byID: map[int]rawItem{}, parents: map[int]map[string]bool{}}
	for _, it := range items {
		ix.byID[it.ID] = it
	}
	ix.parents[10] = map[string]bool{"orchid": true}
	ix.parents[13] = map[string]bool{"manta": true}
	return ix
}

func testCfg() *config.Config {
	c := &config.Config{}
	c.Builds = config.BuildsCfg{MinMatches: 100, TopN: 4, TimingTopN: 2, MinCost: 1000}
	return c
}

func TestDeriveEntryFloorsAndSuppression(t *testing.T) {
	ix := testIndex()
	rows := []rawRow{
		// yasha: most bought, component of manta (manta also candidate -> suppressed)
		{ItemID: 13, Time: 9, MatchCount: 900},
		{ItemID: 13, Time: 11, MatchCount: 100},
		// manta: later purchase, kept
		{ItemID: 12, Time: 20, MatchCount: 800},
		// oblivion_staff: passes floors but orchid is a candidate -> suppressed
		{ItemID: 10, Time: 15, MatchCount: 700},
		{ItemID: 11, Time: 22, MatchCount: 650},
		// magic_wand: cost 460 under minCost -> dropped
		{ItemID: 14, Time: 2, MatchCount: 5000},
		// blink: below minMatches -> dropped
		{ItemID: 1, Time: 14, MatchCount: 99},
		// heart, instance 1 only: ignored entirely
		{ItemID: 15, Time: 35, Instance: 1, MatchCount: 5000},
		{ItemID: 15, Time: 34, MatchCount: 300},
	}
	got, err := deriveEntry(rows, ix, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	// survivors ranked by matches: manta 800, orchid 650, heart 300 -> topN 4 keeps all 3
	// chronological order: orchid 22.0, manta 20.0... avg minutes: manta 20, orchid 22, heart 34
	wantTokens := []string{"manta", "orchid", "heart"}
	if !reflect.DeepEqual(got.Tokens, wantTokens) {
		t.Errorf("tokens = %v, want %v (yasha suppressed for manta, oblivion for orchid, wand cost, blink sample)", got.Tokens, wantTokens)
	}
	if len(got.Timings) != 2 || got.Timings[0].Label != "manta" || got.Timings[0].Minute != 20 || got.Timings[1].Label != "orchid" {
		t.Errorf("timings = %+v, want manta 20 then orchid 22", got.Timings)
	}
	if got.Timings[1].Minute != 22 {
		t.Errorf("orchid minute = %d, want 22", got.Timings[1].Minute)
	}
	if got.Total != 800 {
		t.Errorf("total = %d, want 800 (top survivor)", got.Total)
	}
}

func TestDeriveEntryFailsLoudlyWhenEmpty(t *testing.T) {
	rows := []rawRow{{ItemID: 1, Time: 14, MatchCount: 5}}
	if _, err := deriveEntry(rows, testIndex(), testCfg()); err == nil {
		t.Fatal("derive accepted a floor-empty entry, stale builds would survive silently")
	}
}

func TestDeriveEntryAliasToken(t *testing.T) {
	ix := testIndex()
	cfg := testCfg()
	cfg.Builds.ItemAliases = map[string]string{"manta": "manta style?"}
	rows := []rawRow{{ItemID: 12, Time: 20, MatchCount: 500}}
	got, err := deriveEntry(rows, ix, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tokens[0] != "manta style?" {
		t.Errorf("token = %q, want the alias", got.Tokens[0])
	}
}

func TestDeriveHeroSingleAndSegments(t *testing.T) {
	single := entryBuild{Tokens: []string{"manta", "heart"}, Timings: []TimingPair{{"manta", 20}}}
	build, ts := deriveHero([]string{"1"}, map[string]entryBuild{"1": single})
	if build != "manta, heart" || len(ts) != 1 {
		t.Fatalf("single-role build = %q ts %+v", build, ts)
	}

	core := entryBuild{Tokens: []string{"radiance", "blink"}, Total: 200, Timings: []TimingPair{{"radiance", 18}}}
	soft := entryBuild{Tokens: []string{"glimmer", "force"}, Total: 900, Timings: []TimingPair{{"glimmer", 14}}}
	hard := entryBuild{Tokens: []string{"glimmer", "force"}, Total: 800, Timings: []TimingPair{{"glimmer", 15}}}
	build, ts = deriveHero([]string{"1", "4", "5"}, map[string]entryBuild{"1": core, "4": soft, "5": hard})
	wantBuild := "pos 1: radiance, blink. pos 4/5: glimmer, force"
	if build != wantBuild {
		t.Errorf("build = %q, want %q (identical lists merge labels)", build, wantBuild)
	}
	// timings from the heaviest role: soft 900 beats hard 800 and core 200
	if len(ts) != 1 || ts[0].Label != "glimmer" || ts[0].Minute != 14 {
		t.Errorf("timings = %+v, want glimmer 14 from the heaviest role", ts)
	}
}

const testContent = `{
  "patch": {
    "headline": "test patch"
  },
  "heroes": {
    "zeta": {
      "identity": "test hero",
      "why": "because",
      "how": "like this",
      "when": "always",
      "build": "old stale items",
      "timings": [["diffusal", 16]],
      "tech": [
        "line one",
        "line two"
      ],
      "mechanics": [["tip", "kept"]]
    }
  },
  "principles": ["p1"],
  "practice": {"drills": [["zeta", "drill"]]},
  "picker": {"intro": "pick here", "cards": [{"name": "n", "body": "b"}]}
}`

func TestRenderContentPreservesAuthoredFields(t *testing.T) {
	updates := map[string]heroUpdate{
		"zeta": {Build: "manta, heart", TimingsJSON: `[["manta", 20], ["heart", 34]]`},
	}
	out, err := renderContent([]byte(testContent), updates, "meta line")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rendered doc does not parse: %v\n%s", err, out)
	}
	h := doc["heroes"].(map[string]any)["zeta"].(map[string]any)
	if h["identity"] != "test hero" || h["why"] != "because" || h["how"] != "like this" || h["when"] != "always" {
		t.Errorf("scalar prose changed: %v", h)
	}
	if !reflect.DeepEqual(h["tech"], []any{"line one", "line two"}) {
		t.Errorf("tech changed: %v", h["tech"])
	}
	if !reflect.DeepEqual(h["mechanics"], []any{[]any{"tip", "kept"}}) {
		t.Errorf("mechanics changed: %v", h["mechanics"])
	}
	if h["build"] != "manta, heart" {
		t.Errorf("build = %v", h["build"])
	}
	if !reflect.DeepEqual(h["timings"], []any{[]any{"manta", float64(20)}, []any{"heart", float64(34)}}) {
		t.Errorf("timings = %v", h["timings"])
	}
	if doc["buildsMeta"] != "meta line" {
		t.Errorf("buildsMeta = %v", doc["buildsMeta"])
	}
	if !strings.Contains(string(out), `"timings": [["manta", 20], ["heart", 34]]`) {
		t.Errorf("timings not rendered compact:\n%s", out)
	}
	// practice drills ride through untouched
	pr := doc["practice"].(map[string]any)["drills"]
	if !reflect.DeepEqual(pr, []any{[]any{"zeta", "drill"}}) {
		t.Errorf("drills changed: %v", pr)
	}
	// the top-level picker section rides through untouched too
	pk := doc["picker"].(map[string]any)
	if pk["intro"] != "pick here" {
		t.Errorf("picker changed: %v", pk)
	}
}

func TestRenderContentIdempotent(t *testing.T) {
	updates := map[string]heroUpdate{
		"zeta": {Build: "manta, heart", TimingsJSON: `[["manta", 20]]`},
	}
	once, err := renderContent([]byte(testContent), updates, "meta line")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := renderContent(once, updates, "meta line")
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Errorf("render is not byte-idempotent:\n%s\n---\n%s", once, twice)
	}
}

func TestDeriveEntryTotalIsTopSampleNotEarliest(t *testing.T) {
	ix := testIndex()
	rows := []rawRow{
		{ItemID: 1, Time: 8, MatchCount: 400},  // blink: earliest purchase, smaller sample
		{ItemID: 12, Time: 20, MatchCount: 900}, // manta: the role's real weight
	}
	got, err := deriveEntry(rows, ix, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 900 {
		t.Errorf("total = %d, want 900 (top item sample, not the earliest item's 400)", got.Total)
	}
	if len(got.Timings) == 0 || got.Timings[0].Label != "blink" {
		t.Errorf("timings = %+v, want blink first (display order stays chronological)", got.Timings)
	}
}

func TestDeriveHeroTimingRoleIsHeaviestByTopSample(t *testing.T) {
	// dragon-knight shape: pos 2's earliest item outsamples pos 3's earliest,
	// but pos 3 is the heavier role by its top item and must own the timings
	pos2 := entryBuild{Tokens: []string{"radiance", "bkb"}, Total: 994, Timings: []TimingPair{{"radiance", 19}, {"bkb", 26}}}
	pos3 := entryBuild{Tokens: []string{"radiance", "shard"}, Total: 6842, Timings: []TimingPair{{"radiance", 21}, {"shard", 22}}}
	_, ts := deriveHero([]string{"2", "3"}, map[string]entryBuild{"2": pos2, "3": pos3})
	if len(ts) != 2 || ts[0].Minute != 21 || ts[1].Label != "shard" {
		t.Errorf("timings = %+v, want radiance 21 + shard 22 from the heavier pos 3", ts)
	}
}

func TestLoadIndexRegistersUnflaggedRecipes(t *testing.T) {
	dir := t.TempDir()
	mk := func(id int, short string, cost int) rawItem {
		it := rawItem{ID: id, ShortName: short}
		it.Stat.Cost = cost
		return it
	}
	// live constants carry upgrade recipes with isRecipe=false and cost 0
	unflagged := mk(3, "recipe_y", 0)
	unflagged.Components = []struct {
		ComponentID int `json:"componentId"`
	}{{ComponentID: 2}}
	f := rawItemsFile{Items: []rawItem{
		mk(1, "blink", 2250),
		mk(2, "x", 2000),
		unflagged,
		mk(5, "y", 2000),
		mk(4, "recipe_special", 2000),
	}}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_items.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := loadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ix.parents[2]["y"] {
		t.Errorf("recipe_y edge missing: parents = %v (prefix must register regardless of the isRecipe flag)", ix.parents)
	}
	rows := []rawRow{
		{ItemID: 2, Time: 12, MatchCount: 700},
		{ItemID: 4, Time: 5, MatchCount: 500}, // floor-passing by cost and sample
		{ItemID: 5, Time: 25, MatchCount: 50},  // y under the sample floor, so x stays
	}
	got, err := deriveEntry(rows, ix, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tokens, []string{"x"}) {
		t.Errorf("tokens = %v, want [x] (recipe_-prefixed rows are never build items)", got.Tokens)
	}
	if got.Total != 700 {
		t.Errorf("total = %d, want 700", got.Total)
	}
}

func TestAssertSyncPreservedRejectsAddedKeys(t *testing.T) {
	before := map[string]any{
		"heroes": map[string]any{"zeta": map[string]any{"identity": "a"}},
		"patch":  map[string]any{"headline": "h"},
	}
	addedHeroField := map[string]any{
		"heroes": map[string]any{"zeta": map[string]any{"identity": "a", "injected": nil}},
		"patch":  before["patch"],
	}
	if err := assertSyncPreserved(before, addedHeroField, nil); err == nil {
		t.Error("assertion missed an added hero field")
	}
	addedTopLevel := map[string]any{
		"heroes": before["heroes"],
		"patch":  before["patch"],
		"extra":  nil,
	}
	if err := assertSyncPreserved(before, addedTopLevel, nil); err == nil {
		t.Error("assertion missed an added top-level key")
	}
	owned := map[string]any{
		"heroes": map[string]any{"zeta": map[string]any{"identity": "a", "build": "new"}},
		"patch":  before["patch"],
	}
	if err := assertSyncPreserved(before, owned, map[string]heroUpdate{"zeta": {}}); err != nil {
		t.Errorf("owned build change rejected: %v", err)
	}
	if err := assertSyncPreserved(before, owned, nil); err == nil {
		t.Error("non-owned build change accepted")
	}
}

// the derive refuses a manifest that is not stamped with the current
// completed week: an older week under the same patch series would silently
// sync stale purchase counts
func TestRunRefusesStaleWeekManifest(t *testing.T) {
	dir := t.TempDir()
	cur := ingest.LastCompletedWeekStart(time.Now())
	for _, tt := range []struct {
		name string
		week int64
		want string
	}{
		{"unstamped", 0, "no week stamp"},
		{"previous week", cur - 7*86400, "last completed week is"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mb, err := json.Marshal(rawManifest{CrawledAt: "2026-09-25T00:00:00Z", Bracket: "DIVINE_IMMORTAL", Field: "itemFullPurchase", Series: "7.41", Week: tt.week})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "_crawl.json"), mb, 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := testCfg()
			cfg.Patch = "7.41f"
			cfg.Paths.BuildsRawDir = dir
			cfg.Paths.ContentPath = filepath.Join(dir, "content.json")
			err = Run(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("stale manifest must fail loudly, got %v", err)
			}
		})
	}
}

func TestRunRecordsDropAccounting(t *testing.T) {
	dir := t.TempDir()
	mk := func(id int, short string, cost int) rawItem {
		it := rawItem{ID: id, ShortName: short}
		it.Stat.Cost = cost
		return it
	}
	items := rawItemsFile{Items: []rawItem{
		mk(1, "blink", 2250),
		mk(12, "manta", 4650),
		mk(15, "heart", 5300),
	}}
	ib, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_items.json"), ib, 0o644); err != nil {
		t.Fatal(err)
	}
	mb, err := json.Marshal(rawManifest{CrawledAt: "2026-09-25T00:00:00Z", Bracket: "DIVINE_IMMORTAL", Field: "itemFullPurchase", Series: "7.41", Week: ingest.LastCompletedWeekStart(time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_crawl.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
	writeDump := func(name string, rows []rawRow) {
		t.Helper()
		b, err := json.Marshal(rawEntry{Rows: rows})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDump("zeta@1.json", []rawRow{
		{ItemID: 12, Time: 20, MatchCount: 800},
		{ItemID: 15, Time: 34, MatchCount: 300},
	})
	writeDump("zeta@4.json", []rawRow{{ItemID: 1, Time: 14, MatchCount: 5}})

	contentPath := filepath.Join(dir, "content.json")
	if err := os.WriteFile(contentPath, []byte(testContent), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testCfg()
	cfg.Patch = "7.41f"
	cfg.Paths.BuildsRawDir = dir
	cfg.Paths.ContentPath = contentPath
	cfg.Pool = []config.PoolEntry{{Slug: "zeta", Name: "Zeta", Role: "1", Tier: "dedicated"}, {Slug: "zeta", Name: "Zeta", Role: "4", Tier: "flex"}}

	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	var sf syncFile
	if err := readJsonFile(filepath.Join(dir, "_sync.json"), &sf); err != nil {
		t.Fatalf("sync drop accounting missing: %v", err)
	}
	e, ok := sf.Heroes["zeta"]
	if !ok {
		t.Fatalf("zeta absent from _sync.json: %+v", sf)
	}
	if !reflect.DeepEqual(e.Roles, []string{"1"}) || !reflect.DeepEqual(e.Dropped, []string{"4"}) {
		t.Errorf("zeta roles/dropped = %v/%v, want [1]/[4] (thin role tracked, not silent)", e.Roles, e.Dropped)
	}
	var doc map[string]any
	cb, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(cb, &doc); err != nil {
		t.Fatal(err)
	}
	z := doc["heroes"].(map[string]any)["zeta"].(map[string]any)
	if z["build"] != "manta, heart" {
		t.Errorf("build = %v, want the surviving role's plain list", z["build"])
	}
}

func TestRenderContentLeavesNonUpdatedHeroesAlone(t *testing.T) {
	two := strings.Replace(testContent, `"mechanics": [["tip", "kept"]]`,
		`"mechanics": [["tip", "kept"]]
    },
    "alpha": {
      "identity": "other",
      "build": "alpha keeps its authored build",
      "timings": [["blink", 13]]`, 1)
	updates := map[string]heroUpdate{
		"zeta": {Build: "manta, heart", TimingsJSON: `[["manta", 20]]`},
	}
	out, err := renderContent([]byte(two), updates, "meta line")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	a := doc["heroes"].(map[string]any)["alpha"].(map[string]any)
	if a["build"] != "alpha keeps its authored build" {
		t.Errorf("non-updated hero build touched: %v", a["build"])
	}
	if !reflect.DeepEqual(a["timings"], []any{[]any{"blink", float64(13)}}) {
		t.Errorf("non-updated hero timings touched: %v", a["timings"])
	}
	if len(doc["heroes"].(map[string]any)) != 2 {
		t.Errorf("hero set changed: %v", doc["heroes"])
	}
}
