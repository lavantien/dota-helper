package ingest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"poolguide/internal/config"
)

// odReply is one canned opendota response. hijack closes the connection before
// any response (transport error arm), abort writes a partial body then kills
// the stream (mid-body read error arm).
type odReply struct {
	code   int
	body   string
	hijack bool
	abort  bool
}

const odHeroesBody = `[{"id":1},{"id":8},{"id":14}]`

// odExplorerBody mirrors testdata/opendota_explorer.json: one ally and one
// enemy row above a 30-match floor.
const odExplorerBody = `{"rows":[` +
	`{"kind":"ally","other_id":14,"matches":120,"pool_wins":70},` +
	`{"kind":"enemy","other_id":8,"matches":300,"pool_wins":160}]}`

func odHealthy(path string, _ int) odReply {
	switch path {
	case "/api/heroes":
		return odReply{code: http.StatusOK, body: odHeroesBody}
	case "/api/explorer":
		return odReply{code: http.StatusOK, body: odExplorerBody}
	}
	return odReply{code: http.StatusNotFound, body: "no such route"}
}

// opendotaServer is an httptest endpoint answering plain GETs by path with a
// 1-based per-path hit index so sequencing tests need no counters of their
// own. The opendota entry points construct their own http.Client around the
// package base const, so tests ride the default-transport swap, the same seam
// the stratz side uses. Nothing ever leaves loopback.
type opendotaServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func newOpenDotaServer(t *testing.T, respond func(path string, hit int) odReply) *opendotaServer {
	t.Helper()
	s := &opendotaServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		hit := 0
		for _, p := range s.seen {
			if p == r.URL.Path {
				hit++
			}
		}
		s.seen = append(s.seen, r.URL.Path)
		s.mu.Unlock()
		rep := respond(r.URL.Path, hit+1)
		if rep.hijack {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("stub server: response writer cannot hijack")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("stub server: hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		w.WriteHeader(rep.code)
		if rep.abort {
			w.Write([]byte(rep.body[:len(rep.body)/2]))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		w.Write([]byte(rep.body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *opendotaServer) url() string { return s.srv.URL }

func (s *opendotaServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

// countMatching reports how many GETs landed on a path carrying sub.
func (s *opendotaServer) countMatching(sub string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.seen {
		if strings.Contains(p, sub) {
			n++
		}
	}
	return n
}

// redirectOpenDotaTraffic points every request that falls through to the
// default transport at srv, rewriting only scheme and host, mirroring
// redirectStratzTraffic for the opendota client builders.
func redirectOpenDotaTraffic(t *testing.T, s *opendotaServer) {
	t.Helper()
	target, err := url.Parse(s.url())
	if err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rr := r.Clone(r.Context())
		u := *r.URL
		u.Scheme, u.Host = target.Scheme, target.Host
		rr.URL, rr.Host = &u, u.Host
		return old.RoundTrip(rr)
	})
	t.Cleanup(func() { http.DefaultTransport = old })
}

// opendotaTestEnv wires the shared config (limiter wide open so no test
// sleeps), the roster cache the explorer paths need, and the transport swap.
func opendotaTestEnv(t *testing.T, respond func(path string, hit int) odReply) (*config.Config, *opendotaServer) {
	t.Helper()
	cfg := serverTestConfig(t.TempDir())
	cfg.OpenDota = config.OpenDotaCfg{RequestsPerMin: 60000, ExplorerMinMatches: 30}
	base := filepath.Dir(cfg.Paths.TokenFile)
	cfg.Paths.OpenDotaRawDir = filepath.Join(base, "opendota", "raw")
	cfg.Pool = []config.PoolEntry{
		{Slug: "dark-seer", Name: "Dark Seer", Role: "3"},
		{Slug: "pudge", Name: "Pudge", Role: "4"},
	}
	wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"), buildsRoster)
	s := newOpenDotaServer(t, respond)
	redirectOpenDotaTraffic(t, s)
	return cfg, s
}

// the client core against a loopback server: body and status passthrough, the
// dead-connection transport arm, the torn-body read arm
func TestOpenDotaClientGetAgainstStubServer(t *testing.T) {
	t.Run("returns the body with its status", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(_ string, _ int) odReply {
			return odReply{code: http.StatusTeapot, body: odHeroesBody}
		})
		body, status, err := newOpenDotaClient(cfg).get("/api/heroes")
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusTeapot || string(body) != odHeroesBody {
			t.Fatalf("get = %q %d, want the teapot body", body, status)
		}
	})
	t.Run("dead connection fails the request", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(_ string, _ int) odReply {
			return odReply{hijack: true}
		})
		body, _, err := newOpenDotaClient(cfg).get("/api/heroes")
		if err == nil || body != nil {
			t.Fatalf("get = %q %v, want a loud transport failure", body, err)
		}
	})
	t.Run("body torn mid-stream fails the read", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, func(path string, _ int) odReply {
			if path == "/api/heroes" {
				return odReply{code: http.StatusOK, body: strings.Repeat("h", 40), abort: true}
			}
			return odHealthy(path, 1)
		})
		body, status, err := newOpenDotaClient(cfg).get("/api/heroes")
		if err == nil || body != nil {
			t.Fatalf("get = %q %v, want a loud read failure", body, err)
		}
		if status != http.StatusOK {
			t.Fatalf("status = %d, want the 200 the server sent", status)
		}
		if s.count() != 1 {
			t.Fatalf("server saw %d requests, want 1", s.count())
		}
	})
}

// ProbeOpenDota treats outages as data: every down shape is a clean pass, the
// heroes body must parse when the api is up
func TestProbeOpenDotaAgainstStubServer(t *testing.T) {
	t.Run("up walks the explorer arm", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, odHealthy)
		if err := ProbeOpenDota(cfg); err != nil {
			t.Fatal(err)
		}
		if s.countMatching("/api/explorer") != 1 {
			t.Fatalf("explorer hit %d times, want 1", s.countMatching("/api/explorer"))
		}
	})
	t.Run("down status is a clean pass", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(_ string, _ int) odReply {
			return odReply{code: http.StatusInternalServerError, body: "outage"}
		})
		if err := ProbeOpenDota(cfg); err != nil {
			t.Fatalf("clean down report must pass, got %v", err)
		}
	})
	t.Run("dead connection is a clean pass", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(_ string, _ int) odReply {
			return odReply{hijack: true}
		})
		if err := ProbeOpenDota(cfg); err != nil {
			t.Fatalf("clean down report must pass, got %v", err)
		}
	})
	t.Run("torn heroes body is a clean pass", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(path string, _ int) odReply {
			if path == "/api/heroes" {
				return odReply{code: http.StatusOK, body: strings.Repeat("h", 40), abort: true}
			}
			return odHealthy(path, 1)
		})
		if err := ProbeOpenDota(cfg); err != nil {
			t.Fatalf("clean down report must pass, got %v", err)
		}
	})
	t.Run("heroes body that is not a list fails the parse", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(path string, _ int) odReply {
			if path == "/api/heroes" {
				return odReply{code: http.StatusOK, body: "not-json"}
			}
			return odHealthy(path, 1)
		})
		if err := ProbeOpenDota(cfg); err == nil || !strings.Contains(err.Error(), "opendota heroes parse") {
			t.Fatalf("err = %v, want the heroes parse failure", err)
		}
	})
}

// the explorer arm needs the stratz roster cache, skips heroes it cannot map,
// and degrades to prints on every failure shape
func TestProbeOpenDotaExplorerArms(t *testing.T) {
	t.Run("no roster cache skips", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, odHealthy)
		os.Remove(filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"))
		probeOpenDotaExplorer(newOpenDotaClient(cfg), cfg)
		if s.countMatching("/api/explorer") != 0 {
			t.Fatal("explorer must not fire without the roster cache")
		}
	})
	t.Run("empty roster skips", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, odHealthy)
		wf(t, filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"), `{"heroes":[]}`)
		probeOpenDotaExplorer(newOpenDotaClient(cfg), cfg)
		if s.countMatching("/api/explorer") != 0 {
			t.Fatal("explorer must not fire on an empty roster")
		}
	})
	t.Run("pool slug missing from the roster skips", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, odHealthy)
		cfg.Pool = []config.PoolEntry{{Slug: "axe", Name: "Axe", Role: "1"}}
		probeOpenDotaExplorer(newOpenDotaClient(cfg), cfg)
		if s.countMatching("/api/explorer") != 0 {
			t.Fatal("explorer must not fire for an unmapped slug")
		}
	})
	t.Run("explorer down degrades to a print", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, func(path string, _ int) odReply {
			if path == "/api/explorer" {
				return odReply{code: http.StatusForbidden, body: "denied"}
			}
			return odHealthy(path, 1)
		})
		probeOpenDotaExplorer(newOpenDotaClient(cfg), cfg)
		if s.countMatching("/api/explorer") != 1 {
			t.Fatal("the down explorer arm must still have been queried")
		}
	})
	t.Run("explorer body that is not rows degrades to a print", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, func(path string, _ int) odReply {
			if path == "/api/explorer" {
				return odReply{code: http.StatusOK, body: "[]"}
			}
			return odHealthy(path, 1)
		})
		probeOpenDotaExplorer(newOpenDotaClient(cfg), cfg)
		if s.countMatching("/api/explorer") != 1 {
			t.Fatal("the unparsable explorer arm must still have been queried")
		}
	})
}

// FetchOpenDota: loud failures for the roster cache, the health gate, the
// epoch format, and blocked writes; happy path caches every rostered pool hero
// and skips the rest
func TestFetchOpenDotaAgainstStubServer(t *testing.T) {
	tests := []struct {
		name    string
		respond func(path string, hit int) odReply
		setup   func(t *testing.T, cfg *config.Config)
		wantErr string
	}{
		{
			name: "roster cache missing",
			setup: func(t *testing.T, cfg *config.Config) {
				os.Remove(filepath.Join(cfg.Paths.StratzRawDir, "_roster.json"))
			},
			wantErr: "stratz roster cache missing",
		},
		{
			name:    "api down",
			respond: func(_ string, _ int) odReply { return odReply{code: 500, body: "outage"} },
			wantErr: "opendota api down",
		},
		{
			name: "heroes body torn counts as down",
			respond: func(path string, _ int) odReply {
				if path == "/api/heroes" {
					return odReply{code: http.StatusOK, body: strings.Repeat("h", 40), abort: true}
				}
				return odHealthy(path, 1)
			},
			wantErr: "opendota api down",
		},
		{
			name: "unparsable patch epoch",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.PatchEpoch = "09/15/2026"
			},
			wantErr: "is not YYYY-MM-DD",
		},
		{
			name: "raw dir blocked by a file",
			setup: func(t *testing.T, cfg *config.Config) {
				p := filepath.Join(t.TempDir(), "opendota-raw-blocker")
				wf(t, p, "")
				cfg.Paths.OpenDotaRawDir = p
			},
			wantErr: "not a directory",
		},
		{
			name: "hero cache write blocked",
			setup: func(t *testing.T, cfg *config.Config) {
				os.MkdirAll(filepath.Join(cfg.Paths.OpenDotaRawDir, "dark-seer.json"), 0o755)
			},
			wantErr: "dark-seer.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respond := tt.respond
			if respond == nil {
				respond = odHealthy
			}
			cfg, _ := opendotaTestEnv(t, respond)
			if tt.setup != nil {
				tt.setup(t, cfg)
			}
			err := FetchOpenDota(cfg)
			if err == nil {
				t.Fatal("fetch succeeded, want a loud failure")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}

	t.Run("caches every rostered pool hero", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, odHealthy)
		if err := FetchOpenDota(cfg); err != nil {
			t.Fatal(err)
		}
		for _, slug := range []string{"dark-seer", "pudge"} {
			b, err := os.ReadFile(filepath.Join(cfg.Paths.OpenDotaRawDir, slug+".json"))
			if err != nil {
				t.Fatalf("%s cache missing: %v", slug, err)
			}
			if string(b) != odExplorerBody {
				t.Fatalf("%s cache = %q, want the raw explorer body", slug, b)
			}
		}
		if s.countMatching("/api/explorer") != 2 {
			t.Fatalf("explorer hit %d times, want 2", s.countMatching("/api/explorer"))
		}
	})

	t.Run("unrostered hero and failed query are skipped", func(t *testing.T) {
		cfg, s := opendotaTestEnv(t, func(path string, hit int) odReply {
			if path == "/api/explorer" && hit == 2 {
				return odReply{code: http.StatusForbidden, body: "denied"}
			}
			return odHealthy(path, hit)
		})
		cfg.Pool = []config.PoolEntry{
			{Slug: "axe", Name: "Axe", Role: "1"},
			{Slug: "dark-seer", Name: "Dark Seer", Role: "3"},
			{Slug: "pudge", Name: "Pudge", Role: "4"},
		}
		if err := FetchOpenDota(cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(cfg.Paths.OpenDotaRawDir, "dark-seer.json")); err != nil {
			t.Fatal("healthy hero was not cached")
		}
		if _, err := os.Stat(filepath.Join(cfg.Paths.OpenDotaRawDir, "pudge.json")); err == nil {
			t.Fatal("failed query must not cache")
		}
		if _, err := os.Stat(filepath.Join(cfg.Paths.OpenDotaRawDir, "axe.json")); err == nil {
			t.Fatal("unrostered hero must not cache")
		}
		if s.count() != 3 {
			t.Fatalf("fetch made %d requests, want 3 (heroes + one ok + one denied explorer)", s.count())
		}
	})

	t.Run("unparsable rows still cache the raw body", func(t *testing.T) {
		cfg, _ := opendotaTestEnv(t, func(path string, _ int) odReply {
			if path == "/api/explorer" {
				return odReply{code: http.StatusOK, body: "[]"}
			}
			return odHealthy(path, 1)
		})
		if err := FetchOpenDota(cfg); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(cfg.Paths.OpenDotaRawDir, "pudge.json"))
		if err != nil || string(b) != "[]" {
			t.Fatalf("pudge cache = %q %v, want the raw body kept", b, err)
		}
	})
}
