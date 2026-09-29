package ingest

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
)

const buildsRoster = `{
	"fetchedAt": "2026-09-24T10:00:00Z",
	"bracket": "DIVINE_IMMORTAL",
	"source": "stratz",
	"heroes": [
		{"id": 8, "slug": "dark-seer", "name": "Dark Seer", "npc": "npc_dota_hero_dark_seer"},
		{"id": 14, "slug": "pudge", "name": "Pudge", "npc": "npc_dota_hero_pudge"}
	]
}`

// buildsResponder answers the item-build crawl surface from the committed
// parser fixtures so the wire shape stays the live-pinned one. mutate
// short-circuits a query for the failure cases.
func buildsResponder(items, purchases []byte, mutate func(string) (stubReply, bool)) func(string, int, *http.Request) stubReply {
	return func(q string, _ int, _ *http.Request) stubReply {
		if mutate != nil {
			if rep, ok := mutate(q); ok {
				return rep
			}
		}
		switch {
		case strings.Contains(q, "constants { items"):
			return okReply(string(items))
		case strings.Contains(q, "itemFullPurchase"):
			return okReply(string(purchases))
		}
		return dataReply(`{}`)
	}
}

func buildsTestEnv(t *testing.T, mutate func(string) (stubReply, bool)) (*config.Config, *stratzServer) {
	t.Helper()
	cfg := serverTestConfig(t.TempDir())
	writeServerToken(t, cfg)
	cfg.Pool = []config.PoolEntry{
		{Slug: "dark-seer", Name: "Dark Seer", Role: "3"},
		{Slug: "pudge", Name: "Pudge", Role: "4"},
		{Slug: "pudge", Name: "Pudge", Role: "4"}, // duplicate key, skipped
	}
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"), buildsRoster)
	s := newStratzServer(t, buildsResponder(
		readFixture(t, "stratz_builds_items.json"),
		readFixture(t, "stratz_builds_purchase.json"),
		mutate))
	redirectStratzTraffic(t, s)
	return cfg, s
}

func seedBuildsCache(t *testing.T, cfg *config.Config, slug, role, fetchedAt string, heroID, rows int) {
	t.Helper()
	c := &buildsCache{
		Slug: slug, Role: role, HeroID: heroID,
		FetchedAt: fetchedAt, Bracket: cfg.Scope.Bracket,
		Series: config.PatchSeries(cfg.Patch), Week: LastCompletedWeekStart(time.Now()),
		Rows: make([]itemPurchaseRow, rows),
	}
	for i := range c.Rows {
		c.Rows[i] = itemPurchaseRow{ItemID: i + 1, Time: 5, MatchCount: 40, WinCount: 20}
	}
	if err := writeJson(buildsCachePath(cfg, slug, role), c); err != nil {
		t.Fatal(err)
	}
}

func loadBuildsManifest(t *testing.T, cfg *config.Config) buildsCrawl {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfg.Paths.BuildsRawDir, "_crawl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m buildsCrawl
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func buildsEntryFor(m buildsCrawl, slug, role string) buildsEntry {
	for _, e := range m.Entries {
		if e.Slug == slug && e.Role == role {
			return e
		}
	}
	return buildsEntry{Slug: slug, Role: role + " (missing from manifest)"}
}

// the builds crawl lifecycle: items constants commit first, usable caches are
// resumed, refresh discards even usable caches, and a zero-row entry fails
// acceptance without failing the crawl and is refetched on the next run
func TestFetchBuildsLifecycle(t *testing.T) {
	t.Run("fresh crawl resumes around a usable cache", func(t *testing.T) {
		cfg, s := buildsTestEnv(t, nil)
		seedBuildsCache(t, cfg, "dark-seer", "3", "2026-09-24T11:00:00Z", 8, 2)
		if err := FetchBuilds(cfg, false); err != nil {
			t.Fatal(err)
		}
		if s.count() != 2 {
			t.Fatalf("crawl made %d requests, want 2 (items + one purchase, dark-seer resumed)", s.count())
		}
		if _, err := os.Stat(filepath.Join(cfg.Paths.BuildsRawDir, "_items.json")); err != nil {
			t.Fatal("items constants missing after crawl")
		}
		c := loadBuildsCache(cfg, "pudge", "4")
		if c == nil || c.Series != "7.41" || c.Bracket != cfg.Scope.Bracket || len(c.Rows) == 0 {
			t.Fatalf("pudge@4 cache = %+v, want a stamped non-empty series cache", c)
		}
		m := loadBuildsManifest(t, cfg)
		if len(m.Entries) != 2 {
			t.Fatalf("manifest carries %d entries, want 2 (duplicate pool key skipped)", len(m.Entries))
		}
		if e := buildsEntryFor(m, "dark-seer", "3"); !e.Cached || !e.Accepted {
			t.Errorf("resumed entry %+v, want cached and accepted", e)
		}
		if e := buildsEntryFor(m, "pudge", "4"); e.Cached || !e.Accepted || e.RowCount != len(c.Rows) {
			t.Errorf("fetched entry %+v, want fresh rows accepted", e)
		}
		if _, err := time.Parse(time.RFC3339, m.CrawledAt); err != nil {
			t.Fatalf("manifest crawledAt %q is not the data window bound: %v", m.CrawledAt, err)
		}
	})

	t.Run("refresh discards usable caches", func(t *testing.T) {
		cfg, s := buildsTestEnv(t, nil)
		seedBuildsCache(t, cfg, "dark-seer", "3", "2026-09-24T11:00:00Z", 8, 2)
		seedBuildsCache(t, cfg, "pudge", "4", "2026-09-24T11:00:00Z", 14, 2)
		if err := FetchBuilds(cfg, true); err != nil {
			t.Fatal(err)
		}
		if s.count() != 3 {
			t.Fatalf("refresh made %d requests, want 3 (items + both purchases)", s.count())
		}
		m := loadBuildsManifest(t, cfg)
		if buildsEntryFor(m, "dark-seer", "3").Cached || buildsEntryFor(m, "pudge", "4").Cached {
			t.Errorf("refresh must refetch usable caches: %+v", m.Entries)
		}
	})

	t.Run("zero-row entry fails acceptance and never caches as usable", func(t *testing.T) {
		cfg, s := buildsTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "itemFullPurchase") {
				return dataReply(`{"heroStats":{"itemFullPurchase":[]}}`), true
			}
			return stubReply{}, false
		})
		seedBuildsCache(t, cfg, "dark-seer", "3", "2026-09-24T11:00:00Z", 8, 2)
		for run := 1; run <= 2; run++ {
			if err := FetchBuilds(cfg, false); err != nil {
				t.Fatal(err)
			}
			if e := buildsEntryFor(loadBuildsManifest(t, cfg), "pudge", "4"); e.Accepted {
				t.Fatalf("run %d: zero-row entry accepted", run)
			}
		}
		if n := s.countMatching("itemFullPurchase"); n != 2 {
			t.Fatalf("zero-row cache refetched %d times, want 2 (an empty cache is never usable)", n)
		}
	})

	// a resumed crawl whose every entry carries an unparsable stamp falls
	// back to wall-clock for crawledAt: maxFetchedAt skips stamps it cannot
	// trust, never invents one
	t.Run("unparsable entry stamps fall back to wall clock", func(t *testing.T) {
		cfg, s := buildsTestEnv(t, nil)
		seedBuildsCache(t, cfg, "dark-seer", "3", "not-a-time", 8, 1)
		seedBuildsCache(t, cfg, "pudge", "4", "not-a-time", 14, 1)
		if err := FetchBuilds(cfg, false); err != nil {
			t.Fatal(err)
		}
		if s.count() != 1 {
			t.Fatalf("resumed crawl made %d requests, want 1 (items only)", s.count())
		}
		m := loadBuildsManifest(t, cfg)
		if _, err := time.Parse(time.RFC3339, m.CrawledAt); err != nil {
			t.Fatalf("manifest crawledAt %q is not a stamp: %v", m.CrawledAt, err)
		}
	})
}

// parser failure arms against torn bodies and the topPurchases truncation
func TestBuildsParsersAndTopPurchases(t *testing.T) {
	if _, err := parseItems([]byte(`{"data":{"constants":{"it`)); err == nil {
		t.Error("torn items body accepted")
	}
	if _, err := parsePurchases([]byte(`{"data":{"heroStats":{"itemFullPur`)); err == nil {
		t.Error("torn purchase body accepted")
	}
	rows := []itemPurchaseRow{
		{ItemID: 10, Instance: 0, MatchCount: 100},
		{ItemID: 11, Instance: 0, MatchCount: 90},
	}
	top := topPurchases(rows, 1)
	if len(top) != 1 || top[0].ItemID != 10 {
		t.Fatalf("topPurchases(1) = %+v, want only the head entry", top)
	}
}

// every loud failure on the builds path: token, items constants, roster
// cache, pool shape, wire shape, blocked writes
func TestFetchBuildsFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) (stubReply, bool)
		setup   func(t *testing.T, cfg *config.Config)
		pool    func(cfg *config.Config)
		noToken bool
		wantErr string
	}{
		{
			name:    "missing token",
			noToken: true,
			wantErr: "token missing",
		},
		{
			name: "items constants rejected",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "constants { items") {
					return stubReply{code: 403, body: "denied"}, true
				}
				return stubReply{}, false
			},
			wantErr: "stratz items constants:",
		},
		{
			name: "roster cache missing",
			setup: func(t *testing.T, cfg *config.Config) {
				os.Remove(filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"))
			},
			wantErr: "builds needs the stratz roster cache",
		},
		{
			name: "roster cache corrupt",
			setup: func(t *testing.T, cfg *config.Config) {
				wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"), "{torn")
			},
			wantErr: "builds needs the stratz roster cache",
		},
		{
			name:    "pool role is not a farm position",
			pool:    func(cfg *config.Config) { cfg.Pool[0].Role = "mid" },
			wantErr: "want a farm position 1-5",
		},
		{
			name:    "pool hero missing from roster",
			pool:    func(cfg *config.Config) { cfg.Pool[1].Slug = "axe" },
			wantErr: "missing from the roster cache",
		},
		{
			name: "items constants torn",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "constants { items") {
					return okReply(`{"data":{"constants":{"it`), true
				}
				return stubReply{}, false
			},
			wantErr: "stratz items constants:",
		},
		{
			name: "purchase query rejected",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "itemFullPurchase") && strings.Contains(q, "heroId: 14") {
					return stubReply{code: 403, body: "denied"}, true
				}
				return stubReply{}, false
			},
			wantErr: "stratz pudge@4:",
		},
		{
			name: "purchase shape drift",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "itemFullPurchase") {
					return dataReply(`{"heroStats":{"itemFullPurchase":{"rows":[]}}}`), true
				}
				return stubReply{}, false
			},
			wantErr: "no parseable purchase list",
		},
		{
			name: "items write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.BuildsRawDir, "_items.json"), 0o755)
			},
			wantErr: "_items.json",
		},
		{
			name: "entry cache write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.BuildsRawDir, "dark-seer@3.json"), 0o755)
			},
			wantErr: "dark-seer@3.json",
		},
		{
			name: "manifest write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.BuildsRawDir, "_crawl.json"), 0o755)
			},
			wantErr: "_crawl.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := buildsTestEnv(t, tt.mutate)
			if tt.noToken {
				t.Setenv("STRATZ_TOKEN", "")
				os.Remove(cfg.Paths.TokenFile)
			}
			if tt.setup != nil {
				tt.setup(t, cfg)
			}
			if tt.pool != nil {
				tt.pool(cfg)
			}
			err := FetchBuilds(cfg, false)
			if err == nil {
				t.Fatal("crawl succeeded, want a loud failure")
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// ProbeBuilds sanity-checks the items constants and one live pool entry; a
// zero-row entry fails acceptance without failing the probe
func TestProbeBuildsAgainstStubServer(t *testing.T) {
	t.Run("reports the first pool entry", func(t *testing.T) {
		cfg, s := buildsTestEnv(t, nil)
		if err := ProbeBuilds(cfg); err != nil {
			t.Fatal(err)
		}
		if s.count() != 2 {
			t.Fatalf("probe made %d requests, want 2 (items + one purchase)", s.count())
		}
		s.saw(t, "positionIds: [POSITION_3]", "probe purchase shape")
	})

	t.Run("zero rows fail acceptance without error", func(t *testing.T) {
		cfg, _ := buildsTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "itemFullPurchase") {
				return dataReply(`{"heroStats":{"itemFullPurchase":[]}}`), true
			}
			return stubReply{}, false
		})
		if err := ProbeBuilds(cfg); err != nil {
			t.Fatalf("zero rows must fail acceptance, not the probe: %v", err)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		cfg, _ := buildsTestEnv(t, nil)
		t.Setenv("STRATZ_TOKEN", "")
		os.Remove(cfg.Paths.TokenFile)
		if err := ProbeBuilds(cfg); err == nil || !strings.Contains(err.Error(), "token missing") {
			t.Fatalf("err = %v, want missing token", err)
		}
	})

	t.Run("roster cache missing", func(t *testing.T) {
		cfg, _ := buildsTestEnv(t, nil)
		os.Remove(filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"))
		if err := ProbeBuilds(cfg); err == nil || !strings.Contains(err.Error(), "builds needs the stratz roster cache") {
			t.Fatalf("err = %v, want the roster cache failure", err)
		}
	})

	t.Run("items failure", func(t *testing.T) {
		cfg, _ := buildsTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "constants { items") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		if err := ProbeBuilds(cfg); err == nil || !strings.Contains(err.Error(), "stratz probe items") {
			t.Fatalf("err = %v, want the items failure", err)
		}
	})

	t.Run("purchase failure names the entry", func(t *testing.T) {
		cfg, _ := buildsTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "itemFullPurchase") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		err := ProbeBuilds(cfg)
		if err == nil || !strings.Contains(err.Error(), "stratz probe dark-seer@3") {
			t.Fatalf("err = %v, want the per-entry probe failure", err)
		}
	})

	t.Run("purchase shape drift fails parse", func(t *testing.T) {
		cfg, _ := buildsTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "itemFullPurchase") {
				return dataReply(`{"heroStats":{"itemFullPurchase":{"rows":[]}}}`), true
			}
			return stubReply{}, false
		})
		if err := ProbeBuilds(cfg); err == nil || !strings.Contains(err.Error(), "no parseable purchase list") {
			t.Fatalf("err = %v, want the shape-drift failure", err)
		}
	})
}
