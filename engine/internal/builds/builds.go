// Package builds derives hero item builds from the committed stratz builds
// crawl and syncs them into the authored prose hub (content.json). It owns
// exactly two fields per hero (build, timings) plus the deterministic
// buildsMeta stamp; every other field rides through byte-identical.
package builds

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"poolguide/internal/config"
	"poolguide/internal/ingest"
)

// mirrors of the committed ref/dota2/builds/raw artifacts (see ingest
// stratzbuilds.go for the crawl that writes them)
type rawItem struct {
	ID          int    `json:"id"`
	ShortName   string `json:"shortName"`
	Components  []struct {
		ComponentID int `json:"componentId"`
	} `json:"components"`
	Stat struct {
		Cost     int  `json:"cost"`
		IsRecipe bool `json:"isRecipe"`
	} `json:"stat"`
}

type rawItemsFile struct {
	Items []rawItem `json:"items"`
}

type rawRow struct {
	ItemID     int   `json:"itemId"`
	Time       int64 `json:"time"`
	Instance   int   `json:"instance"`
	MatchCount int64 `json:"matchCount"`
	WinCount   int64 `json:"winCount"`
}

type rawEntry struct {
	Rows []rawRow `json:"rows"`
}

type rawManifest struct {
	CrawledAt string `json:"crawledAt"`
	Bracket   string `json:"bracket"`
	Field     string `json:"field"`
	Series    string `json:"series"`
	Week      int64  `json:"week"`
}

// syncFile records which roles each hero's synced build actually covers and
// which pooled roles were dropped for falling under the floors, so check.ps1
// can gate silent collapses (a role pooled in config but absent everywhere).
type syncFile struct {
	Series string                    `json:"series"`
	Heroes map[string]syncHeroEntry  `json:"heroes"`
}

type syncHeroEntry struct {
	Roles  []string `json:"roles"`
	Dropped []string `json:"dropped"`
}

type itemIndex struct {
	byID    map[int]rawItem
	parents map[int]map[string]bool // component item id -> upgrade result short names
}

func readJsonFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// loadIndex reads the _items.json anchor and derives the component->upgrade
// map from recipe shortNames ("recipe_x" upgrades "x") and their component
// lists, so components of popular final items can be suppressed.
func loadIndex(dir string) (*itemIndex, error) {
	var f rawItemsFile
	if err := readJsonFile(filepath.Join(dir, "_items.json"), &f); err != nil {
		return nil, fmt.Errorf("builds items anchor: %w (run make fetch-builds)", err)
	}
	ix := &itemIndex{
		byID:    make(map[int]rawItem, len(f.Items)),
		parents: map[int]map[string]bool{},
	}
	for _, it := range f.Items {
		ix.byID[it.ID] = it
		// the recipe_ prefix is the reliable signal: live constants carry
		// upgrade recipes with isRecipe=false (all cost 0 so far, but the
		// flag has already drifted once)
		if !strings.HasPrefix(it.ShortName, "recipe_") {
			continue
		}
		result := strings.TrimPrefix(it.ShortName, "recipe_")
		for _, c := range it.Components {
			if ix.parents[c.ComponentID] == nil {
				ix.parents[c.ComponentID] = map[string]bool{}
			}
			ix.parents[c.ComponentID][result] = true
		}
	}
	return ix, nil
}

func (ix *itemIndex) token(shortName string, cfg *config.Config) string {
	if v, ok := cfg.Builds.ItemAliases[shortName]; ok {
		return v
	}
	return strings.ReplaceAll(shortName, "_", " ")
}

type itemAgg struct {
	Matches int64
	Moment  int64 // sum(purchase minute * matches)
}

func (a itemAgg) avgMinute() float64 {
	if a.Matches == 0 {
		return 0
	}
	return float64(a.Moment) / float64(a.Matches)
}

type TimingPair struct {
	Label  string
	Minute int
}

type entryBuild struct {
	Tokens  []string // display tokens, purchase order
	Timings []TimingPair
	Total   int64 // top item sample, the role's weight for timing choice
}

// deriveEntry aggregates instance-0 purchase rows per item, filters by the
// config hub floors (cost, sample), suppresses components whose upgrade is
// also a popular candidate, and keeps the topN survivors.
func deriveEntry(rows []rawRow, ix *itemIndex, cfg *config.Config) (entryBuild, error) {
	agg := map[int]itemAgg{}
	for _, r := range rows {
		if r.Instance != 0 {
			continue
		}
		a := agg[r.ItemID]
		a.Matches += r.MatchCount
		a.Moment += r.Time * r.MatchCount
		agg[r.ItemID] = a
	}
	type cand struct {
		id   int
		name string
		agg  itemAgg
	}
	var cands []cand
	for id, a := range agg {
		it, ok := ix.byID[id]
		if !ok || it.Stat.IsRecipe || strings.HasPrefix(it.ShortName, "recipe_") {
			continue
		}
		if it.Stat.Cost < cfg.Builds.MinCost || a.Matches < int64(cfg.Builds.MinMatches) {
			continue
		}
		cands = append(cands, cand{id: id, name: it.ShortName, agg: a})
	}
	if len(cands) == 0 {
		return entryBuild{}, fmt.Errorf("no items passed the builds floors (minCost %d, minMatches %d)", cfg.Builds.MinCost, cfg.Builds.MinMatches)
	}
	rank := func(i, j int) bool {
		if cands[i].agg.Matches != cands[j].agg.Matches {
			return cands[i].agg.Matches > cands[j].agg.Matches
		}
		return cands[i].id < cands[j].id
	}
	sort.Slice(cands, rank)
	// candidate short names at any popularity: a component whose upgrade is
	// also bought at floor-passing rates is an intermediate, not a build item
	shorts := map[string]bool{}
	for _, c := range cands {
		shorts[c.name] = true
	}
	var kept []cand
	for _, c := range cands {
		skip := false
		for parent := range ix.parents[c.id] {
			if shorts[parent] {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, c)
		}
	}
	if len(kept) > cfg.Builds.TopN {
		kept = kept[:cfg.Builds.TopN]
	}
	// the role's weight is its top item's sample: capture it while kept is
	// still in popularity order, before the chronological display sort
	out := entryBuild{Total: kept[0].agg.Matches}
	// display in purchase order, the way a build path reads
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].agg.avgMinute() != kept[j].agg.avgMinute() {
			return kept[i].agg.avgMinute() < kept[j].agg.avgMinute()
		}
		if kept[i].agg.Matches != kept[j].agg.Matches {
			return kept[i].agg.Matches > kept[j].agg.Matches
		}
		return kept[i].id < kept[j].id
	})
	for _, c := range kept {
		out.Tokens = append(out.Tokens, ix.token(c.name, cfg))
		if len(out.Timings) < cfg.Builds.TimingTopN {
			out.Timings = append(out.Timings, TimingPair{
				Label:  ix.token(c.name, cfg),
				Minute: int(c.agg.avgMinute() + 0.5),
			})
		}
	}
	return out, nil
}

// deriveHero renders the content.json fields: single-role heroes get a plain
// list, multi-role heroes get one segment per role (identical lists merge
// their labels), timings come from the heaviest role.
func deriveHero(roles []string, byRole map[string]entryBuild) (string, []TimingPair) {
	if len(roles) == 1 {
		b := byRole[roles[0]]
		return strings.Join(b.Tokens, ", "), b.Timings
	}
	// group roles sharing an identical token list, ordered by first role
	type seg struct {
		roles  []string
		tokens []string
	}
	var segs []seg
	for _, r := range roles { // roles arrive ascending
		t := byRole[r].Tokens
		if len(segs) > 0 && reflect.DeepEqual(segs[len(segs)-1].tokens, t) {
			segs[len(segs)-1].roles = append(segs[len(segs)-1].roles, r)
			continue
		}
		segs = append(segs, seg{roles: []string{r}, tokens: t})
	}
	var parts []string
	for _, s := range segs {
		parts = append(parts, fmt.Sprintf("pos %s: %s", strings.Join(s.roles, "/"), strings.Join(s.tokens, ", ")))
	}
	best := roles[0]
	for _, r := range roles[1:] {
		if byRole[r].Total > byRole[best].Total {
			best = r
		}
	}
	return strings.Join(parts, ". "), byRole[best].Timings
}

// Run derives builds for every pool hero and syncs content.json.
func Run(cfg *config.Config) error {
	dir := cfg.Paths.BuildsRawDir
	var mf rawManifest
	if err := readJsonFile(filepath.Join(dir, "_crawl.json"), &mf); err != nil {
		return fmt.Errorf("builds manifest: %w (run make fetch-builds)", err)
	}
	if mf.Series != config.PatchSeries(cfg.Patch) {
		return fmt.Errorf("builds crawl series %s does not match config series %s, rerun make fetch-builds", mf.Series, config.PatchSeries(cfg.Patch))
	}
	// the purchase counts pin the completed calendar week: a manifest from an
	// older week under the same patch series would silently sync stale builds
	if mf.Week == 0 {
		return fmt.Errorf("builds manifest carries no week stamp, rerun make fetch-builds")
	}
	if cur := ingest.LastCompletedWeekStart(time.Now()); mf.Week != cur {
		return fmt.Errorf("builds crawl pins completed week %d but the last completed week is %d, rerun make fetch-builds", mf.Week, cur)
	}
	ix, err := loadIndex(dir)
	if err != nil {
		return err
	}

	updates := map[string]heroUpdate{}
	sync := syncFile{Series: mf.Series, Heroes: map[string]syncHeroEntry{}}
	for _, h := range cfg.Heroes() {
		byRole := map[string]entryBuild{}
		var thin []string
		for _, role := range h.Roles {
			var e rawEntry
			path := filepath.Join(dir, h.Slug+"@"+role+".json")
			if err := readJsonFile(path, &e); err != nil {
				return fmt.Errorf("builds dump missing for %s@%s: %w (run make fetch-builds)", h.Slug, role, err)
			}
			b, err := deriveEntry(e.Rows, ix, cfg)
			if err != nil {
				// a position slice too thin for the floors is absent data, not
				// stale data: drop the segment loudly, keep the healthy roles
				thin = append(thin, role)
				continue
			}
			byRole[role] = b
		}
		if len(byRole) == 0 {
			return fmt.Errorf("builds derive %s: no pooled role passed the floors (%v)", h.Slug, h.Roles)
		}
		roles := make([]string, 0, len(byRole))
		for r := range byRole {
			roles = append(roles, r)
		}
		sort.Strings(roles)
		if len(thin) > 0 {
			fmt.Printf("builds %s: pos %s below floors, segment dropped\n", h.Slug, strings.Join(thin, "/"))
		}
		sort.Strings(thin)
		sync.Heroes[h.Slug] = syncHeroEntry{Roles: roles, Dropped: thin}
		buildStr, timings := deriveHero(roles, byRole)
		updates[h.Slug] = heroUpdate{Build: buildStr, TimingsJSON: timingsJSON(timings)}
	}
	syncBytes, err := json.MarshalIndent(sync, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "_sync.json"), append(syncBytes, '\n'), 0o644); err != nil {
		return err
	}

	contentPath := cfg.Paths.ContentPath
	orig, err := os.ReadFile(contentPath)
	if err != nil {
		return err
	}
	fetched := mf.CrawledAt
	if len(fetched) > 10 {
		fetched = fetched[:10]
	}
	meta := fmt.Sprintf("stratz %s completed week %d, %s, fetched %s, series %s",
		mf.Field, mf.Week, strings.ToLower(mf.Bracket), fetched, mf.Series)
	out, err := renderContent(orig, updates, meta)
	if err != nil {
		return err
	}
	if string(out) == string(orig) {
		fmt.Printf("builds sync: %d heroes, content.json already current\n", len(updates))
		return nil
	}
	if err := os.WriteFile(contentPath, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("builds sync: %d heroes updated in content.json (meta: %s)\n", len(updates), meta)
	return nil
}
