package ingest

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
)

const crawlRoster = `{"constants":{"heroes":[
	{"id":1,"name":"npc_dota_hero_antimage","displayName":"Anti-Mage","shortName":"antimage"},
	{"id":8,"name":"npc_dota_hero_dark_seer","displayName":"Dark Seer","shortName":"dark_seer"},
	{"id":14,"name":"npc_dota_hero_pudge","shortName":"pudge"}]}}`

const crawlRosterFour = `{"constants":{"heroes":[
	{"id":1,"name":"npc_dota_hero_antimage","displayName":"Anti-Mage","shortName":"antimage"},
	{"id":2,"name":"npc_dota_hero_axe","displayName":"Axe","shortName":"axe"},
	{"id":8,"name":"npc_dota_hero_dark_seer","displayName":"Dark Seer","shortName":"dark_seer"},
	{"id":14,"name":"npc_dota_hero_pudge","displayName":"Pudge","shortName":"pudge"}]}}`

const popEnvelope = `{"heroStats":{"winDay":[{"heroId":1,"matchCount":600,"winCount":300}]}}`

// crawlResponder answers the pair-crawl surface: roster, matchUp pinned to the
// queried hero, and the winDay aggregate. mutate short-circuits a query so
// failure cases bend exactly one route.
func crawlResponder(mutate func(q string) (stubReply, bool)) func(string, int, *http.Request) stubReply {
	return func(q string, _ int, _ *http.Request) stubReply {
		if mutate != nil {
			if rep, ok := mutate(q); ok {
				return rep
			}
		}
		switch {
		case strings.Contains(q, "constants { heroes"):
			return dataReply(crawlRoster)
		case strings.Contains(q, "matchUp("):
			id := qInt(q, "matchUp(heroId: ")
			return dataReply(matchUpEnvelope(id, rowsJSON(100, 101), rowsJSON(102, 103)))
		case strings.Contains(q, "winDay("):
			return dataReply(popEnvelope)
		}
		return dataReply(`{}`)
	}
}

func stubClient(t *testing.T, respond func(q string, hit int, req *http.Request) stubReply) (*stratzClient, *stratzServer) {
	t.Helper()
	s := newStratzServer(t, respond)
	c := newStratzClient(serverTestConfig(t.TempDir()), "test-token")
	if c.endpoint != stratzEndpoint {
		t.Fatalf("default endpoint = %q, want the product endpoint const", c.endpoint)
	}
	c.endpoint = s.url()
	return c, s
}

// the client core against a live loopback server: success, auth headers, the
// 429/5xx backoff ladder (Retry-After 0, never a real sleep value), immediate
// 4xx failure, transport and mid-body read failures retried, and decode
// failures surfaced from the graphql envelope
func TestStratzClientQueryAgainstStubServer(t *testing.T) {
	tests := []struct {
		name    string
		respond func(q string, hit int, req *http.Request) stubReply
		query   string
		decode  bool // run the roster fetch so the envelope decode is in scope
		wantErr string
		wantSub string
		wantHit int
	}{
		{
			name: "posts the query and returns the body",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				return dataReply(`{"constants":{"heroes":[]}}`)
			},
			query:   rosterQuery,
			wantSub: "heroes",
		},
		{
			name: "authorization and browser headers ride along",
			respond: func(_ string, _ int, req *http.Request) stubReply {
				if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("Authorization = %q", got)
				}
				if got := req.Header.Get("User-Agent"); got != "poolguide-test" {
					t.Errorf("User-Agent = %q, want the configured browser header", got)
				}
				return dataReply(`{}`)
			},
			query: rosterQuery,
		},
		{
			name: "429 with Retry-After 0 backs off once then succeeds",
			respond: func(_ string, hit int, _ *http.Request) stubReply {
				if hit == 1 {
					return stubReply{code: 429, body: "slow down", retryAfter: "0"}
				}
				return dataReply(`{"constants":{"heroes":[]}}`)
			},
			query:   rosterQuery,
			wantSub: "heroes",
			wantHit: 2,
		},
		{
			name: "persistent 429 exhausts the retry budget",
			respond: func(_ string, _ int, _ *http.Request) stubReply {
				return stubReply{code: 429, body: "slow down", retryAfter: "0"}
			},
			query:   rosterQuery,
			wantErr: "failed after 4 attempts",
			wantHit: 4,
		},
		{
			name: "persistent 500 without Retry-After exhausts the budget",
			respond: func(_ string, _ int, _ *http.Request) stubReply {
				return stubReply{code: 500, body: "boom"}
			},
			query:   rosterQuery,
			wantErr: "stratz 500",
			wantHit: 4,
		},
		{
			name: "403 fails immediately without burning retries",
			respond: func(_ string, _ int, _ *http.Request) stubReply {
				return stubReply{code: 403, body: "cloudflare"}
			},
			query:   rosterQuery,
			wantErr: "stratz 403",
			wantHit: 1,
		},
		{
			name: "dead connection is retried on the next attempt",
			respond: func(_ string, hit int, _ *http.Request) stubReply {
				if hit == 1 {
					return stubReply{hijack: true}
				}
				return dataReply(`{"constants":{"heroes":[]}}`)
			},
			query:   rosterQuery,
			wantSub: "heroes",
			wantHit: 2,
		},
		{
			name: "body torn mid-stream is retried on the next attempt",
			respond: func(_ string, hit int, _ *http.Request) stubReply {
				if hit == 1 {
					return stubReply{code: 200, body: `{"data":{"constants":{"heroes":[{`, abort: true}
				}
				return dataReply(`{"constants":{"heroes":[]}}`)
			},
			query:   rosterQuery,
			wantSub: "heroes",
			wantHit: 2,
		},
		{
			name: "torn json body fails at decode",
			respond: func(_ string, _ int, _ *http.Request) stubReply {
				return okReply(`{"data":{"constants":{"her`)
			},
			query:   rosterQuery,
			decode:  true,
			wantErr: "unexpected end",
			wantHit: 1,
		},
		{
			name: "graphql error envelope fails at decode",
			respond: func(_ string, _ int, _ *http.Request) stubReply {
				return errReply("User is not an admin")
			},
			query:   rosterQuery,
			decode:  true,
			wantErr: "stratz graphql",
			wantHit: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, s := stubClient(t, tt.respond)
			var body []byte
			var err error
			if tt.decode {
				_, err = c.FetchRoster()
			} else {
				body, err = c.query(tt.query)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("query err = %v, want one containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tt.wantSub != "" && !strings.Contains(string(body), tt.wantSub) {
				t.Fatalf("body %q missing %q", body, tt.wantSub)
			}
			if tt.wantHit != 0 && s.count() != tt.wantHit {
				t.Fatalf("server saw %d requests, want %d", s.count(), tt.wantHit)
			}
		})
	}
}

func crawlTestEnv(t *testing.T, mutate func(string) (stubReply, bool)) (*config.Config, *stratzServer) {
	t.Helper()
	cfg := serverTestConfig(t.TempDir())
	writeServerToken(t, cfg)
	s := newStratzServer(t, crawlResponder(mutate))
	redirectStratzTraffic(t, s)
	return cfg, s
}

func seedHeroCache(t *testing.T, cfg *config.Config, slug string, heroID int, week int64) {
	t.Helper()
	c := &rawCache{
		Slug: slug, HeroID: heroID, Name: slug, Npc: npcOf(slug),
		FetchedAt: "2026-09-24T10:05:00Z", Bracket: cfg.Scope.Bracket, Week: week,
		Vs:   []matchupRow{{HeroID2: 100, MatchCount: 500, WinCount: 250, Synergy: 1.5}},
		With: []matchupRow{{HeroID2: 102, MatchCount: 500, WinCount: 250, Synergy: 1.5}},
	}
	if err := writeJson(cachePath(cfg, slug), c); err != nil {
		t.Fatal(err)
	}
}

// the crawl lifecycle against the stub wire: fresh crawl writes every cache
// and the manifest, a fresh same-week cache is resumed instead of refetched,
// refresh discards even fresh caches, a stale-week cache is refetched and
// restamped, and acceptance verdicts land in the manifest without failing
// the crawl
func TestFetchStratzCrawlLifecycle(t *testing.T) {
	t.Run("fresh crawl", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, nil)
		if err := FetchStratz(cfg, false); err != nil {
			t.Fatal(err)
		}
		if s.count() != 5 {
			t.Fatalf("crawl made %d requests, want 5 (roster + 3 matchups + popularity)", s.count())
		}
		week := LastCompletedWeekStart(time.Now())
		for _, slug := range []string{"anti-mage", "dark-seer", "pudge"} {
			c := loadCache(cfg, slug)
			if c == nil {
				t.Fatalf("%s cache missing", slug)
			}
			if c.Week != week {
				t.Errorf("%s cache week = %d, want the pinned %d", slug, c.Week, week)
			}
			if len(c.Vs) != 2 || len(c.With) != 2 {
				t.Errorf("%s cache rows = %d/%d, want 2/2", slug, len(c.Vs), len(c.With))
			}
		}
		m := loadCrawlManifest(t, cfg)
		if len(m.Heroes) != 3 || m.Week != week {
			t.Fatalf("manifest = %d heroes on week %d, want 3 on %d", len(m.Heroes), m.Week, week)
		}
		for _, e := range m.Heroes {
			if e.Cached || !e.Accepted {
				t.Errorf("fresh entry %+v, want fetched and accepted", e)
			}
		}
		if _, err := os.Stat(filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json")); err != nil {
			t.Fatal("popularity cache missing after crawl")
		}
	})

	t.Run("resume skips fresh same-week caches", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, nil)
		seedHeroCache(t, cfg, "dark-seer", 8, LastCompletedWeekStart(time.Now()))
		seedHeroCache(t, cfg, "pudge", 14, LastCompletedWeekStart(time.Now()))
		// a torn cache reads as no cache: the hero is refetched and rewritten
		wf(t, cachePath(cfg, "anti-mage"), `{"slug": "anti-mage", "wee`)
		if err := FetchStratz(cfg, false); err != nil {
			t.Fatal(err)
		}
		if s.count() != 3 {
			t.Fatalf("resumed crawl made %d requests, want 3 (roster + 1 stale matchup + popularity)", s.count())
		}
		if c := loadCache(cfg, "anti-mage"); c == nil || c.Week != LastCompletedWeekStart(time.Now()) {
			t.Fatalf("torn anti-mage cache = %+v, want rewritten on the current pin", c)
		}
		m := loadCrawlManifest(t, cfg)
		if !manifestEntry(m, "dark-seer").Cached || !manifestEntry(m, "pudge").Cached {
			t.Errorf("fresh caches were refetched: %+v", m.Heroes)
		}
		if manifestEntry(m, "anti-mage").Cached {
			t.Error("uncached hero reported as cached")
		}
	})

	t.Run("refresh discards fresh caches", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, nil)
		seedHeroCache(t, cfg, "dark-seer", 8, LastCompletedWeekStart(time.Now()))
		if err := FetchStratz(cfg, true); err != nil {
			t.Fatal(err)
		}
		if s.count() != 5 {
			t.Fatalf("refresh made %d requests, want the full 5", s.count())
		}
		if manifestEntry(loadCrawlManifest(t, cfg), "dark-seer").Cached {
			t.Error("refresh must refetch even a fresh cache")
		}
	})

	t.Run("stale-week cache is refetched and restamped", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, nil)
		seedHeroCache(t, cfg, "pudge", 14, LastCompletedWeekStart(time.Now())-7*86400)
		if err := FetchStratz(cfg, false); err != nil {
			t.Fatal(err)
		}
		if s.countMatching("matchUp(heroId: 14") != 1 {
			t.Fatalf("stale-week hero not refetched (%d requests)", s.count())
		}
		if c := loadCache(cfg, "pudge"); c == nil || c.Week != LastCompletedWeekStart(time.Now()) {
			t.Fatalf("pudge cache = %+v, want restamped to the current pin", c)
		}
	})

	t.Run("thin hero fails acceptance without failing the crawl", func(t *testing.T) {
		cfg, _ := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "matchUp(heroId: 8") {
				return dataReply(matchUpEnvelope(8, "", "")), true
			}
			return stubReply{}, false
		})
		if err := FetchStratz(cfg, false); err != nil {
			t.Fatal(err)
		}
		m := loadCrawlManifest(t, cfg)
		if manifestEntry(m, "dark-seer").Accepted {
			t.Error("zero-row hero accepted")
		}
		if !manifestEntry(m, "pudge").Accepted {
			t.Error("healthy hero rejected beside a thin one")
		}
	})

	t.Run("wild rows drop without failing the hero", func(t *testing.T) {
		cfg, _ := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "matchUp(heroId: 8") {
				wild := "," + jsonRow(101, 500, 99)
				return dataReply(matchUpEnvelope(8, rowsJSON(100)+wild, rowsJSON(102)+wild)), true
			}
			return stubReply{}, false
		})
		if err := FetchStratz(cfg, false); err != nil {
			t.Fatal(err)
		}
		e := manifestEntry(loadCrawlManifest(t, cfg), "dark-seer")
		if !e.Accepted {
			t.Fatalf("hero with one sane row per side rejected: %+v", e)
		}
		if c := loadCache(cfg, "dark-seer"); len(c.Vs) != 2 || len(c.With) != 2 {
			t.Errorf("cache rows = %d/%d, want all kept (drops are gate-time only)", len(c.Vs), len(c.With))
		}
	})
}

// every loud failure on the crawl path: token, roster transport, hostile
// roster entries, blocked writes, empty wire shapes
func TestFetchStratzCrawlFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) (stubReply, bool)
		setup   func(t *testing.T, cfg *config.Config)
		noToken bool
		wantErr string
	}{
		{
			name:    "missing token",
			noToken: true,
			wantErr: "token missing",
		},
		{
			name: "roster rejected",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "constants { heroes") {
					return stubReply{code: 403, body: "denied"}, true
				}
				return stubReply{}, false
			},
			wantErr: "stratz roster:",
		},
		{
			name: "roster graphql error",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "constants { heroes") {
					return errReply("bad token"), true
				}
				return stubReply{}, false
			},
			wantErr: "stratz graphql",
		},
		{
			name: "hostile roster shortName aborts",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "constants { heroes") {
					return dataReply(`{"constants":{"heroes":[{"id":1,"shortName":"../../etc/passwd"}]}}`), true
				}
				return stubReply{}, false
			},
			wantErr: "invalid slug",
		},
		{
			name: "roster write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"), 0o755)
			},
			wantErr: "stratz roster:",
		},
		{
			name: "matchUp answers zero results",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "matchUp(") {
					return dataReply(`{"heroStats":{"matchUp":[]}}`), true
				}
				return stubReply{}, false
			},
			wantErr: "returned 0 results",
		},
		{
			name: "hero cache write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.StratzRawDir, "anti-mage.json"), 0o755)
			},
			wantErr: "anti-mage",
		},
		{
			name: "popularity query rejected",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "winDay(") {
					return stubReply{code: 403, body: "denied"}, true
				}
				return stubReply{}, false
			},
			wantErr: "stratz popularity:",
		},
		{
			name: "popularity answers no rows",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "winDay(") {
					return dataReply(`{"heroStats":{"winDay":[]}}`), true
				}
				return stubReply{}, false
			},
			wantErr: "answered no rows",
		},
		{
			name: "manifest write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.StratzRawDir, "_crawl.json"), 0o755)
			},
			wantErr: "_crawl.json",
		},
		{
			name: "popularity write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				os.MkdirAll(filepath.Join(cfg.Paths.StratzRawDir, "_popularity.json"), 0o755)
			},
			wantErr: "_popularity.json",
		},
		{
			name: "raw dir blocked by a file",
			setup: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				p := filepath.Join(t.TempDir(), "stratz-raw-blocker")
				os.WriteFile(p, nil, 0o644)
				cfg.Paths.StratzRawDir = p
			},
			wantErr: "not a directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := crawlTestEnv(t, tt.mutate)
			if tt.noToken {
				t.Setenv("STRATZ_TOKEN", "")
				os.Remove(cfg.Paths.TokenFile)
			}
			if tt.setup != nil {
				tt.setup(t, cfg)
			}
			err := FetchStratz(cfg, false)
			if err == nil {
				t.Fatal("crawl succeeded, want a loud failure")
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}
