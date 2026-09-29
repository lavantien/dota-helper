package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"poolguide/internal/config"
)

// Stratz item-build crawl. The graphql field and row shape were pinned by
// live introspection against api.stratz.com (recorded in ref/dota2/README.md):
// heroStats.itemFullPurchase returns per (itemId, minute, instance) purchase
// counts, matchLimit is a server-side per-row sample floor (1 = no floor, the
// derive step applies the config hub floor instead), and positionIds slices
// the aggregate by farm position POSITION_1..POSITION_5.

const buildsItemsQuery = "query { constants { items { id name displayName shortName components { componentId } stat { cost isRecipe } } } }"

const buildsPurchaseShape = "itemId time instance matchCount winCount"

// buildsMatchLimit is transport semantics ("return every row"), not a tunable:
// the data-quality floor lives in builds.minMatches.
const buildsMatchLimit = 1

var positionRx = regexp.MustCompile(`^[1-5]$`)

// buildsPurchaseQuery builds the item crawl. week 0 omits the window arg
// (probe evidence arms); the product crawl pins the completed calendar week,
// the only window control this field has (minTime/maxTime filter purchase
// minutes inside a match, live-proven by make probe-window).
func buildsPurchaseQuery(heroID int, bracket, position string, week int64) string {
	weekArg := ""
	if week != 0 {
		weekArg = fmt.Sprintf(", week: %d", week)
	}
	return fmt.Sprintf(
		"query { heroStats { itemFullPurchase(heroId: %d, bracketBasicIds: [%s], positionIds: [%s], matchLimit: %d%s) { %s } } }",
		heroID, bracket, position, buildsMatchLimit, weekArg, buildsPurchaseShape)
}

type itemStat struct {
	Cost     int  `json:"cost"`
	IsRecipe bool `json:"isRecipe"`
}

type itemComponent struct {
	ComponentID int `json:"componentId"`
}

type itemConstant struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	DisplayName string          `json:"displayName"`
	ShortName   string          `json:"shortName"`
	Components  []itemComponent `json:"components"`
	Stat        itemStat        `json:"stat"`
}

type itemsData struct {
	Constants struct {
		Items []itemConstant `json:"items"`
	} `json:"constants"`
}

// parseItems sorts by id so the dump is deterministic. Recipes stay in: the
// derive step reads recipe shortNames ("recipe_x" upgrades "x") and their
// component lists to suppress components of popular final items.
func parseItems(body []byte) ([]itemConstant, error) {
	var d itemsData
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	out := make([]itemConstant, len(d.Constants.Items))
	copy(out, d.Constants.Items)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type itemPurchaseRow struct {
	ItemID     int   `json:"itemId"`
	Time       int64 `json:"time"` // purchase minute bucket
	Instance   int   `json:"instance"`
	MatchCount int64 `json:"matchCount"`
	WinCount   int64 `json:"winCount"`
}

type purchaseData struct {
	HeroStats map[string]json.RawMessage `json:"heroStats"`
}

// parsePurchases pulls the row list out of the heroStats object; the response
// key mirrors whichever field the query asked for. A heroStats value that no
// longer parses as a row list is API shape drift and must fail loudly, not
// read as empty data.
func parsePurchases(body []byte) ([]itemPurchaseRow, error) {
	var d purchaseData
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	for _, raw := range d.HeroStats {
		var rows []itemPurchaseRow
		if err := json.Unmarshal(raw, &rows); err == nil {
			return rows, nil
		}
	}
	return nil, fmt.Errorf("heroStats carried no parseable purchase list (api shape changed?)")
}

type itemsFile struct {
	FetchedAt string        `json:"fetchedAt"`
	Source    string        `json:"source"`
	Items     []itemConstant `json:"items"`
}

type buildsCache struct {
	Slug      string            `json:"slug"`
	Role      string            `json:"role"`
	HeroID    int               `json:"heroId"`
	FetchedAt string            `json:"fetchedAt"`
	Bracket   string            `json:"bracket"`
	Series    string            `json:"series"`
	Week      int64             `json:"week"`
	Rows      []itemPurchaseRow `json:"rows"`
}

// buildsCacheUsable gates resume: a cache from another patch series, bracket,
// or calendar week is stale data that would ride into a fresh manifest, and a
// zero-row cache carries no information worth keeping.
func buildsCacheUsable(c *buildsCache, series, bracket string, week int64) bool {
	return c != nil && c.Series == series && c.Bracket == bracket && c.Week == week && len(c.Rows) > 0
}

type buildsEntry struct {
	Slug      string `json:"slug"`
	Role      string `json:"role"`
	HeroID    int    `json:"heroId"`
	FetchedAt string `json:"fetchedAt"`
	Cached    bool   `json:"cached"`
	Accepted  bool   `json:"accepted"`
	RowCount  int    `json:"rowCount"`
}

type buildsCrawl struct {
	CrawledAt  string        `json:"crawledAt"`
	Bracket    string        `json:"bracket"`
	Source     string        `json:"source"`
	Field      string        `json:"field"`
	Patch      string        `json:"patch"`
	Series     string        `json:"series"`
	Week       int64         `json:"week"`
	WindowNote string        `json:"windowNote"`
	Entries    []buildsEntry `json:"entries"`
}

func buildsDir(cfg *config.Config) string {
	return cfg.Paths.BuildsRawDir
}

func buildsCachePath(cfg *config.Config, slug, role string) string {
	return filepath.Join(buildsDir(cfg), slug+"@"+role+".json")
}

// maxFetchedAt returns the newest entry stamp, the honest crawledAt for a
// partially-resumed crawl. Unparsable stamps are skipped, not trusted.
func maxFetchedAt(entries []buildsEntry) string {
	var best time.Time
	for _, e := range entries {
		t, err := time.Parse(time.RFC3339, e.FetchedAt)
		if err != nil {
			continue
		}
		if t.After(best) {
			best = t
		}
	}
	if best.IsZero() {
		return ""
	}
	return best.UTC().Format(time.RFC3339)
}

func buildsRosterIDs(cfg *config.Config) (map[string]int, error) {
	rf, err := readRosterFile(cfg)
	if err != nil {
		return nil, fmt.Errorf("builds needs the stratz roster cache, run make fetch-stratz first: %w", err)
	}
	ids := make(map[string]int, len(rf.Heroes))
	for _, h := range rf.Heroes {
		ids[h.Slug] = h.ID
	}
	return ids, nil
}

func fetchItemsConstants(c *stratzClient) ([]itemConstant, error) {
	body, err := c.query(buildsItemsQuery)
	if err != nil {
		return nil, err
	}
	return parseItems(body)
}

// loadBuildsCache returns the committed per-entry artifact, or nil when the
// crawl has not reached it yet.
func loadBuildsCache(cfg *config.Config, slug, role string) *buildsCache {
	b, err := os.ReadFile(buildsCachePath(cfg, slug, role))
	if err != nil {
		return nil
	}
	var c buildsCache
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

// FetchBuilds crawls itemFullPurchase for every pool entry (hero, role),
// resuming from the committed per-entry caches, then rewrites the manifest.
func FetchBuilds(cfg *config.Config, refresh bool) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)

	items, err := fetchItemsConstants(client)
	if err != nil {
		return fmt.Errorf("stratz items constants: %w", err)
	}
	if err := writeJson(filepath.Join(buildsDir(cfg), "_items.json"), &itemsFile{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    "stratz",
		Items:     items,
	}); err != nil {
		return err
	}
	fmt.Printf("builds items: %d constants\n", len(items))

	ids, err := buildsRosterIDs(cfg)
	if err != nil {
		return err
	}

	week := LastCompletedWeekStart(time.Now())
	manifest := buildsCrawl{
		Bracket:    cfg.Scope.Bracket,
		Source:     "stratz completed-week crawl",
		Field:      "itemFullPurchase",
		Patch:      cfg.Patch,
		Series:     config.PatchSeries(cfg.Patch),
		Week:       week,
		WindowNote: fmt.Sprintf("completed calendar week %d, per (item, minute, instance) purchase counts", week),
	}
	seen := map[string]bool{}
	accepted := 0
	for _, e := range cfg.Pool {
		key := e.Slug + "@" + e.Role
		if seen[key] {
			continue
		}
		seen[key] = true
		if !positionRx.MatchString(e.Role) {
			return fmt.Errorf("builds: pool entry %s has role %q, want a farm position 1-5 for the stratz positionIds filter", key, e.Role)
		}
		id, ok := ids[e.Slug]
		if !ok {
			return fmt.Errorf("builds: pool hero %s missing from the roster cache", e.Slug)
		}
		entry := buildsEntry{Slug: e.Slug, Role: e.Role, HeroID: id}
		c := loadBuildsCache(cfg, e.Slug, e.Role)
		if refresh || !buildsCacheUsable(c, manifest.Series, cfg.Scope.Bracket, week) {
			rows, err := client.query(buildsPurchaseQuery(id, cfg.Scope.Bracket, "POSITION_"+e.Role, week))
			if err != nil {
				return fmt.Errorf("stratz %s: %w", key, err)
			}
			parsed, err := parsePurchases(rows)
			if err != nil {
				return fmt.Errorf("stratz %s: %w", key, err)
			}
			c = &buildsCache{
				Slug:      e.Slug,
				Role:      e.Role,
				HeroID:    id,
				FetchedAt: time.Now().UTC().Format(time.RFC3339),
				Bracket:   cfg.Scope.Bracket,
				Series:    manifest.Series,
				Week:      week,
				Rows:      parsed,
			}
			if err := writeJson(buildsCachePath(cfg, e.Slug, e.Role), c); err != nil {
				return err
			}
		} else {
			entry.Cached = true
		}
		entry.FetchedAt = c.FetchedAt
		entry.RowCount = len(c.Rows)
		entry.Accepted = entry.RowCount > 0
		if !entry.Accepted {
			fmt.Printf("builds %s: acceptance FAILED (no purchase rows)\n", key)
		} else {
			accepted++
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	// crawledAt is the data window's upper bound (the newest entry actually
	// fetched), never the moment the crawl command ran over warm caches
	manifest.CrawledAt = maxFetchedAt(manifest.Entries)
	if manifest.CrawledAt == "" {
		manifest.CrawledAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := writeJson(filepath.Join(buildsDir(cfg), "_crawl.json"), &manifest); err != nil {
		return err
	}
	fmt.Printf("builds crawl done: %d/%d pool entries pass acceptance, manifest rewritten (resumable)\n",
		accepted, len(manifest.Entries))
	return nil
}

// topPurchases aggregates instance-0 rows per item for probe output.
func topPurchases(rows []itemPurchaseRow, n int) []struct {
	ItemID int
	Count  int64
} {
	tot := map[int]int64{}
	for _, r := range rows {
		if r.Instance == 0 {
			tot[r.ItemID] += r.MatchCount
		}
	}
	out := make([]struct {
		ItemID int
		Count  int64
	}, 0, len(tot))
	for id, c := range tot {
		out = append(out, struct {
			ItemID int
			Count  int64
		}{id, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].ItemID < out[j].ItemID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// ProbeBuilds sanity-checks the items constants and one live pool entry.
func ProbeBuilds(cfg *config.Config) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)

	items, err := fetchItemsConstants(client)
	if err != nil {
		return fmt.Errorf("stratz probe items: %w", err)
	}
	byID := make(map[int]itemConstant, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	fmt.Printf("stratz probe builds: %d item constants\n", len(items))

	ids, err := buildsRosterIDs(cfg)
	if err != nil {
		return err
	}
	e := cfg.Pool[0]
	id := ids[e.Slug]
	body, err := client.query(buildsPurchaseQuery(id, cfg.Scope.Bracket, "POSITION_"+e.Role, LastCompletedWeekStart(time.Now())))
	if err != nil {
		return fmt.Errorf("stratz probe %s@%s: %w", e.Slug, e.Role, err)
	}
	rows, err := parsePurchases(body)
	if err != nil {
		return err
	}
	var parts []string
	for _, t := range topPurchases(rows, 3) {
		nm := fmt.Sprintf("item %d", t.ItemID)
		if it, ok := byID[t.ItemID]; ok {
			nm = it.ShortName
		}
		parts = append(parts, fmt.Sprintf("%s n=%d", nm, t.Count))
	}
	status := "PASS"
	if len(rows) == 0 {
		status = "FAIL: no rows"
	}
	fmt.Printf("stratz probe %s@%s: %d rows, top: %s, acceptance %s\n",
		e.Slug, e.Role, len(rows), strings.Join(parts, ", "), status)
	return nil
}
