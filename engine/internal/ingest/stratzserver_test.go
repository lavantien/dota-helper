package ingest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"poolguide/internal/config"
)

// stubReply is one canned stratz response. hijack closes the connection before
// any response (transport error arm), abort writes a partial body then kills
// the stream (mid-body read error arm).
type stubReply struct {
	code       int
	body       string
	retryAfter string
	hijack     bool
	abort      bool
}

func okReply(body string) stubReply { return stubReply{code: http.StatusOK, body: body} }

// dataReply wraps a graphql data payload in the transport envelope.
func dataReply(data string) stubReply {
	return okReply(`{"data":` + data + `}`)
}

// errReply is a 200 graphql error envelope, the shape the api answers schema
// violations with.
func errReply(msg string) stubReply {
	return okReply(`{"errors":[{"message":` + strconv.Quote(msg) + `}]}`)
}

// stratzServer is an httptest graphql endpoint answering from a responder.
// hit is the 1-based request index so sequencing tests need no counters of
// their own. Tests reach it two ways: a directly built stratzClient carries
// the server url in its endpoint field, and the exported entry points (which
// construct their own client around the product endpoint const) ride the
// redirectStratzTraffic transport swap. Nothing ever leaves loopback.
type stratzServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func newStratzServer(t *testing.T, respond func(q string, hit int, req *http.Request) stubReply) *stratzServer {
	t.Helper()
	s := &stratzServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("stub server: read request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(b, &payload); err != nil {
			t.Errorf("stub server: request body is not a graphql envelope: %q", b)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.seen = append(s.seen, payload.Query)
		hit := len(s.seen)
		s.mu.Unlock()
		rep := respond(payload.Query, hit, r)
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
		if rep.retryAfter != "" {
			w.Header().Set("Retry-After", rep.retryAfter)
		}
		w.WriteHeader(rep.code)
		if rep.abort {
			// flush a partial chunk then kill the stream: the client must
			// see a mid-body read error, not a clean empty 200
			w.Write([]byte(rep.body[:len(rep.body)/2]))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		w.Write([]byte(rep.body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stratzServer) url() string { return s.srv.URL }

func (s *stratzServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

// countMatching reports how many queries received carried sub.
func (s *stratzServer) countMatching(sub string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, q := range s.seen {
		if strings.Contains(q, sub) {
			n++
		}
	}
	return n
}

// saw asserts some query carried sub and names the site on failure.
func (s *stratzServer) saw(t *testing.T, sub, site string) {
	t.Helper()
	if s.countMatching(sub) == 0 {
		t.Errorf("%s: no query carried %q", site, sub)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// redirectStratzTraffic points every request that falls through to the
// default transport at srv, rewriting only scheme and host. The exported
// crawl entry points build their own client around the endpoint const, so
// this is the seam that keeps their code path untouched while the suite owns
// the wire.
func redirectStratzTraffic(t *testing.T, s *stratzServer) {
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

// qInt extracts the first integer after marker in a rendered graphql query.
func qInt(q, marker string) int64 {
	i := strings.Index(q, marker)
	if i < 0 {
		return 0
	}
	rest := q[i+len(marker):]
	if end := strings.IndexAny(rest, ",)] }"); end >= 0 {
		rest = rest[:end]
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	return n
}

// serverTestConfig builds the cfg the httptest suites share: token from a
// temp file so the host STRATZ_TOKEN never leaks in, zero request spacing,
// and a burst bucket the suites never drain, so no test sleeps on the limiter.
func serverTestConfig(base string) *config.Config {
	return &config.Config{
		Patch:      "7.41f",
		PatchEpoch: "2026-09-15",
		Scope:      config.Scope{Bracket: "DIVINE_IMMORTAL", GameMode: 22},
		Stratz: config.StratzCfg{
			Take: 7, MinWithRows: 1, MinVsRows: 1, MaxAbsSynergy: 20,
			BurstCapPerSec: 1000,
			Headers:        map[string]string{"User-Agent": "poolguide-test"},
		},
		Aliases: map[string]string{"antimage": "anti-mage"},
		Paths: config.Paths{
			TokenFile:     filepath.Join(base, "token.txt"),
			StratzRawDir:  filepath.Join(base, "stratz", "raw"),
			BuildsRawDir:  filepath.Join(base, "builds", "raw"),
			MatchesRawDir: filepath.Join(base, "matches", "raw"),
		},
	}
}

func writeServerToken(t *testing.T, cfg *config.Config) {
	t.Helper()
	wf(t, cfg.Paths.TokenFile, "test-token")
}

func loadCrawlManifest(t *testing.T, cfg *config.Config) crawlFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cfg.Paths.StratzRawDir, "_crawl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m crawlFile
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func manifestEntry(m crawlFile, slug string) crawlEntry {
	for _, e := range m.Heroes {
		if e.Slug == slug {
			return e
		}
	}
	return crawlEntry{Slug: slug + " (missing from manifest)"}
}

func rowsJSON(ids ...int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = jsonRow(id, 500, 1.5)
	}
	return strings.Join(parts, ", ")
}

func jsonRow(id, matches int64, synergy float64) string {
	return fmt.Sprintf(`{"heroId2":%d,"matchCount":%d,"winCount":%d,"synergy":%g}`, id, matches, matches/2, synergy)
}

func matchUpEnvelope(heroID int64, vs, with string) string {
	return fmt.Sprintf(`{"heroStats":{"matchUp":[{"heroId":%d,"vs":[%s],"with":[%s]}]}}`, heroID, vs, with)
}

// probeWinStart sits inside the cfg.PatchEpoch window (2026-09-15 .. now),
// probePreStart one day before it, so the probe's window-outside print arm
// has a row to complain about.
const (
	probeWinStart = int64(1758326400) // 2026-09-20
	probePreStart = int64(1757808000) // 2026-09-14
)

// probeRow is one canned probeMatch row, with or without a complete draft.
func probeRow(id int64, draft bool) string {
	pb := ""
	if draft {
		pb = pickBansJSON(int(id)%7, 10)
	}
	return matchJSON(id, probeWinStart, 2400, pb)
}

func TestFetchDate(t *testing.T) {
	if got := fetchDate("2026-09-24T10:05:00Z"); got != "2026-09-24" {
		t.Errorf("fetchDate(rfc3339) = %q, want the date prefix", got)
	}
	if got := fetchDate("short"); got != "short" {
		t.Errorf("fetchDate(short) = %q, want it verbatim", got)
	}
}

// probeIntroSnapshot is the minimal __schema body the matches probe's
// introspection walk resolves: a matches root field with args plus the types
// it prints.
const probeIntroSnapshot = `{"sch":{"queryType":{"name":"StratzQuery","fields":[` +
	`{"name":"matches","args":[{"name":"ids"},{"name":"take"}],"type":{"kind":"OBJECT","name":"MatchType"}},` +
	`{"name":"player","args":[],"type":{"kind":"OBJECT","name":"SteamAccountType"}}]},` +
	`"types":[` +
	`{"name":"MatchType","fields":[{"name":"id","type":{"kind":"SCALAR","name":"Long"}},` +
	`{"name":"pickBans","type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"PickBansType"}}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"SteamAccountType","fields":[{"name":"matches","args":[{"name":"request"}],"type":{"kind":"OBJECT","name":"MatchType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"PlayerType","fields":[],"inputFields":[],"enumValues":[]},` +
	`{"name":"LeaderboardQuery","fields":[],"inputFields":[],"enumValues":[]},` +
	`{"name":"FilterSeasonLeaderboardRequestType","fields":[],"inputFields":[{"name":"leaderBoardDivision","type":{"kind":"SCALAR","name":"LeaderboardDivision"}}],"enumValues":[]},` +
	`{"name":"LeaderboardDivision","fields":[],"inputFields":[],"enumValues":[{"name":"EUROPE"}]},` +
	`{"name":"RankBracket","fields":[],"inputFields":[],"enumValues":[{"name":"DIVINE"},{"name":"IMMORTAL"}]},` +
	`{"name":"GameModeEnumType","fields":[],"inputFields":[],"enumValues":[{"name":"ALL_PICK_RANKED"},{"name":"CAPTAINS_MODE"}]},` +
	`{"name":"LobbyTypeEnum","fields":[],"inputFields":[],"enumValues":[{"name":"RANKED"}]},` +
	`{"name":"LeagueType","fields":[{"name":"matches","args":[{"name":"request"}],"type":{"kind":"OBJECT","name":"MatchType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"LeagueMatchesRequestType","fields":[],"inputFields":[{"name":"take"},{"name":"skip"}],"enumValues":[]},` +
	`{"name":"LiveType","fields":[],"inputFields":[],"enumValues":[]}]}}`
