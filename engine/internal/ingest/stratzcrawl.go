package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"poolguide/internal/config"
)

// rawCache is the committed per-hero crawl artifact at paths.stratzRawDir/<slug>.json.
type rawCache struct {
	Slug      string       `json:"slug"`
	HeroID    int          `json:"heroId"`
	Name      string       `json:"name"`
	Npc       string       `json:"npc"`
	FetchedAt string       `json:"fetchedAt"`
	Bracket   string       `json:"bracket"`
	Week      int64        `json:"week"`
	Vs        []matchupRow `json:"vs"`
	With      []matchupRow `json:"with"`
}

// cacheWindowFresh is the resume gate for one hero cache: usable only when it
// crawled the week currently pinned. A legacy cache without a week stamp and a
// cache from an older week both force a refetch, so a plain fetch-stratz can
// never mix calendar weeks.
func cacheWindowFresh(c *rawCache, pin int64) bool {
	return c != nil && c.Week != 0 && c.Week == pin
}

type rosterEntry struct {
	ID   int    `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Npc  string `json:"npc"`
}

// rosterFile is the crawl roster artifact at paths.stratzRawDir/_roster.json.
type rosterFile struct {
	FetchedAt string        `json:"fetchedAt"`
	Bracket   string        `json:"bracket"`
	Source    string        `json:"source"`
	Heroes    []rosterEntry `json:"heroes"`
}

type crawlEntry struct {
	Slug      string `json:"slug"`
	HeroID    int    `json:"heroId"`
	FetchedAt string `json:"fetchedAt"`
	Cached    bool   `json:"cached"`
	Accepted  bool   `json:"accepted"`
}

type crawlFile struct {
	CrawledAt string       `json:"crawledAt"`
	Bracket   string       `json:"bracket"`
	Source    string       `json:"source"`
	Take      int          `json:"take"`
	Week      int64        `json:"week"`
	Heroes    []crawlEntry `json:"heroes"`
}

type gateResult struct {
	OK      bool
	Reasons []string
}

// rowSane is the per-row sanity filter: nonzero sample and a plausible raw
// synergy magnitude. Wild thin-sample rows are dropped, not fatal.
func rowSane(r matchupRow, cfg *config.Config) bool {
	return r.MatchCount > 0 &&
		r.Synergy < cfg.Stratz.MaxAbsSynergy && r.Synergy > -cfg.Stratz.MaxAbsSynergy
}

// checkGates applies the per-hero crawl acceptance rules from the config hub:
// row minimums on both sides, counted after the per-row sanity filter. Dropped
// rows are reported first and are informational only — a thin wild row never
// fails an otherwise healthy hero, it only shrinks the survivor count.
func checkGates(c *rawCache, cfg *config.Config) gateResult {
	var drops, counts []string
	vsN, withN := 0, 0
	for _, r := range c.Vs {
		if rowSane(r, cfg) {
			vsN++
			continue
		}
		drops = append(drops, dropReason(r, cfg))
	}
	for _, r := range c.With {
		if rowSane(r, cfg) {
			withN++
			continue
		}
		drops = append(drops, dropReason(r, cfg))
	}
	if withN < cfg.Stratz.MinWithRows {
		counts = append(counts, fmt.Sprintf("with rows %d < %d", withN, cfg.Stratz.MinWithRows))
	}
	if vsN < cfg.Stratz.MinVsRows {
		counts = append(counts, fmt.Sprintf("vs rows %d < %d", vsN, cfg.Stratz.MinVsRows))
	}
	return gateResult{
		OK:      len(counts) == 0,
		Reasons: append(drops, counts...),
	}
}

func dropReason(r matchupRow, cfg *config.Config) string {
	if r.MatchCount <= 0 {
		return fmt.Sprintf("matchCount %d on heroId2 %d", r.MatchCount, r.HeroID2)
	}
	return fmt.Sprintf("|synergy| %.2f on heroId2 %d exceeds %.0f", r.Synergy, r.HeroID2, cfg.Stratz.MaxAbsSynergy)
}

func cachePath(cfg *config.Config, slug string) string {
	return filepath.Join(cfg.Paths.StratzRawDir, slug+".json")
}

func rosterPath(cfg *config.Config) string {
	return filepath.Join(cfg.Paths.StratzRawDir, "_roster.json")
}

// readRosterFile loads the committed roster cache written by the stratz crawl.
func readRosterFile(cfg *config.Config) (*rosterFile, error) {
	b, err := os.ReadFile(rosterPath(cfg))
	if err != nil {
		return nil, err
	}
	var rf rosterFile
	if err := json.Unmarshal(b, &rf); err != nil {
		return nil, err
	}
	return &rf, nil
}

func writeJson(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func npcOf(shortName string) string {
	return "npc_dota_hero_" + shortName
}

func fetchDate(at string) string {
	if len(at) >= 10 {
		return at[:10]
	}
	return at
}

// fetchRosterCache pulls the roster once and commits it as the crawl anchor.
func fetchRosterCache(c *stratzClient, cfg *config.Config) (*rosterFile, error) {
	heroes, err := c.FetchRoster()
	if err != nil {
		return nil, err
	}
	rf := &rosterFile{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Bracket:   cfg.Scope.Bracket,
		Source:    "stratz",
	}
	for _, h := range heroes {
		name := h.DisplayName
		if name == "" {
			name = h.ShortName
		}
		rf.Heroes = append(rf.Heroes, rosterEntry{
			ID:   h.ID,
			Slug: cfg.SlugFromNPC(h.ShortName),
			Name: name,
			Npc:  npcOf(h.ShortName),
		})
	}
	if err := writeJson(rosterPath(cfg), rf); err != nil {
		return nil, err
	}
	return rf, nil
}

// loadCache returns the committed crawl artifact for one hero, or nil when the
// crawl has not reached it yet.
func loadCache(cfg *config.Config, slug string) *rawCache {
	b, err := os.ReadFile(cachePath(cfg, slug))
	if err != nil {
		return nil
	}
	var c rawCache
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

// FetchStratz crawls matchUp for every roster hero, resuming from the committed
// per-hero caches, then rewrites the crawl manifest from all cache states.
// refresh discards the caches and recrawls everything.
func FetchStratz(cfg *config.Config, refresh bool) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)

	rf, err := fetchRosterCache(client, cfg)
	if err != nil {
		return fmt.Errorf("stratz roster: %w", err)
	}
	fmt.Printf("stratz roster: %d heroes\n", len(rf.Heroes))

	manifest := crawlFile{
		CrawledAt: time.Now().UTC().Format(time.RFC3339),
		Bracket:   cfg.Scope.Bracket,
		Source:    "stratz completed-week crawl",
		Take:      cfg.Stratz.Take,
		Week:      LastCompletedWeekStart(time.Now()),
	}
	accepted, refetched := 0, 0
	for _, h := range rf.Heroes {
		entry := crawlEntry{Slug: h.Slug, HeroID: h.ID}
		c := loadCache(cfg, h.Slug)
		if refresh || !cacheWindowFresh(c, manifest.Week) {
			if c != nil {
				refetched++
			}
			c = nil
		}
		if c == nil {
			mu, err := client.FetchMatchUp(h.ID, cfg.Scope.Bracket, manifest.Week)
			if err != nil {
				return fmt.Errorf("stratz %s: %w", h.Slug, err)
			}
			c = &rawCache{
				Slug:      h.Slug,
				HeroID:    h.ID,
				Name:      h.Name,
				Npc:       h.Npc,
				FetchedAt: time.Now().UTC().Format(time.RFC3339),
				Bracket:   cfg.Scope.Bracket,
				Week:      manifest.Week,
				Vs:        mu.Vs,
				With:      mu.With,
			}
			if err := writeJson(cachePath(cfg, h.Slug), c); err != nil {
				return err
			}
		} else {
			entry.Cached = true
		}
		entry.FetchedAt = c.FetchedAt
		g := checkGates(c, cfg)
		entry.Accepted = g.OK
		if !g.OK {
			fmt.Printf("stratz %s: acceptance FAILED (%s)\n", h.Slug, strings.Join(g.Reasons, "; "))
		} else {
			accepted++
			if len(g.Reasons) > 0 {
				fmt.Printf("stratz %s: accepted with %d rows dropped (%s)\n", h.Slug, len(g.Reasons), strings.Join(g.Reasons, "; "))
			}
		}
		manifest.Heroes = append(manifest.Heroes, entry)
	}
	if err := writeJson(filepath.Join(cfg.Paths.StratzRawDir, "_crawl.json"), &manifest); err != nil {
		return err
	}
	fmt.Printf("stratz crawl done: %d/%d heroes pass acceptance, %d window-stale refetches, manifest rewritten (resumable)\n",
		accepted, len(rf.Heroes), refetched)
	return fetchPopularity(client, cfg)
}

type popularityFile struct {
	FetchedAt string        `json:"fetchedAt"`
	Bracket   string        `json:"bracket"`
	GameMode  int           `json:"gameMode"`
	Take      int           `json:"take"`
	Field     string        `json:"field"`
	Rows      []popRow      `json:"rows"`
	Note      string        `json:"note"`
}

// fetchPopularity writes the scoped per-hero aggregate. The old ladder
// (winBracket/winMonth/winWeek with bracketBasicIds, then all-bracket winDay)
// died with the schema: bracketBasicIds is invalid on the win* fields and
// winBracket no longer exists, which is how the cache silently fell to
// all-bracket data. winDay under bracketIds + gameModeIds is the scoped
// source and a failure is loud because no unscoped fallback is acceptable.
func fetchPopularity(c *stratzClient, cfg *config.Config) error {
	rows, err := c.fetchScopedAggregate()
	if err != nil {
		return fmt.Errorf("stratz popularity: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("stratz popularity: winDay answered no rows")
	}
	pf := popularityFile{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Bracket:   cfg.Scope.Bracket,
		GameMode:  cfg.Scope.GameMode,
		Take:      cfg.Stratz.Take,
		Field:     "winDay",
		Rows:      rows,
		Note: fmt.Sprintf("rolling %d-day aggregate, bracket- and mode-scoped, not patch pure",
			cfg.Stratz.Take),
	}
	if err := writeJson(filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), &pf); err != nil {
		return err
	}
	fmt.Printf("stratz popularity: %d rows via winDay scoped to %s + gameMode %d\n",
		len(rows), cfg.Scope.Bracket, cfg.Scope.GameMode)
	return nil
}

// ProbeStratz verifies token, headers, and per-hero acceptance with a handful
// of live calls against the first pool heroes.
func ProbeStratz(cfg *config.Config) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)

	heroes, err := client.FetchRoster()
	if err != nil {
		return fmt.Errorf("stratz probe roster: %w (browser headers required, token at %s)", err, cfg.Paths.TokenFile)
	}
	fmt.Printf("stratz probe: roster ok, %d heroes\n", len(heroes))
	bySlug := map[string]int{}
	for _, h := range heroes {
		bySlug[cfg.SlugFromNPC(h.ShortName)] = h.ID
	}
	probed := 0
	week := LastCompletedWeekStart(time.Now())
	for _, slug := range cfg.PoolSlugs() {
		if probed >= 3 {
			break
		}
		id, ok := bySlug[slug]
		if !ok {
			continue
		}
		mu, err := client.FetchMatchUp(id, cfg.Scope.Bracket, week)
		if err != nil {
			return fmt.Errorf("stratz probe %s: %w", slug, err)
		}
		g := checkGates(&rawCache{Slug: slug, Vs: mu.Vs, With: mu.With}, cfg)
		status := "PASS"
		if !g.OK {
			status = "FAIL: " + strings.Join(g.Reasons, "; ")
		}
		fmt.Printf("stratz probe %s: %d vs / %d with rows, acceptance %s\n", slug, len(mu.Vs), len(mu.With), status)
		probed++
	}
	return fetchPopularity(client, cfg)
}
