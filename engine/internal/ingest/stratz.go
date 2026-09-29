package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"poolguide/internal/config"
)

const (
	stratzEndpoint = "https://api.stratz.com/graphql"
	tokenEnvVar    = "STRATZ_TOKEN"
	// transport and error-path guards, not data tunables
	httpTimeout            = 30 * time.Second
	maxRetries             = 3
	retryAfterCapSeconds   = 60
)

type matchupRow struct {
	HeroID2    int     `json:"heroId2"`
	MatchCount int64   `json:"matchCount"`
	WinCount   int64   `json:"winCount"`
	Synergy    float64 `json:"synergy"`
}

type matchUpResult struct {
	HeroID int
	Vs     []matchupRow
	With   []matchupRow
}

type rosterHero struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`        // npc-derived internal name
	DisplayName string `json:"displayName"` // localized name
	ShortName   string `json:"shortName"`
}

// loadToken reads the stratz token from the token file, falling back to the
// environment. The value is never logged.
func loadToken(cfg *config.Config) (string, error) {
	if b, err := os.ReadFile(cfg.Paths.TokenFile); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	if t := strings.TrimSpace(os.Getenv(tokenEnvVar)); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("stratz token missing: %s or %s", cfg.Paths.TokenFile, tokenEnvVar)
}

type stratzClient struct {
	http  *http.Client
	cfg   *config.Config
	token string
	rl    *rateLimiter
	// endpoint defaults to stratzEndpoint; the field exists so tests point
	// the client at an httptest server instead of the live api
	endpoint string
}

func newStratzClient(cfg *config.Config, token string) *stratzClient {
	return &stratzClient{
		http:     &http.Client{Timeout: httpTimeout},
		cfg:      cfg,
		token:    token,
		rl:       newRateLimiter(cfg.Stratz.RequestIntervalMs, cfg.Stratz.BurstCapPerSec, realClock{}),
		endpoint: stratzEndpoint,
	}
}

// query posts one graphql query, retrying 429/5xx responses with Retry-After
// backoff. Plain clients get cloudflare 403, so the browser-like headers from
// the config hub ride along on every attempt.
func (s *stratzClient) query(q string) ([]byte, error) {
	payload, err := json.Marshal(map[string]string{"query": q})
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		s.rl.Wait()
		req, err := http.NewRequest(http.MethodPost, s.endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+s.token)
		for k, v := range s.cfg.Stratz.Headers {
			req.Header.Set(k, v)
		}
		resp, err := s.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			ra, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
			if ra <= 0 {
				ra = 1
			}
			// cap the backoff: the api's hour/day buckets (1500/hour,
			// 15000/day, served as long Retry-Afters) must fail visibly
			// after the retries, not hang a crawl for hours
			if ra > retryAfterCapSeconds {
				fmt.Printf("stratz: %d with Retry-After %ds, capping backoff at %ds (attempt %d/%d)\n",
					resp.StatusCode, ra, retryAfterCapSeconds, attempt+1, maxRetries+1)
				ra = retryAfterCapSeconds
			}
			lastErr = fmt.Errorf("stratz %d", resp.StatusCode)
			time.Sleep(time.Duration(ra) * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("stratz %d: %s", resp.StatusCode, truncateBody(body))
		}
		return body, nil
	}
	return nil, fmt.Errorf("stratz query failed after %d attempts: %w", maxRetries+1, lastErr)
}

// truncateBody bounds the remote body excerpt and neutralizes it: every
// remote-controlled byte that reaches an error line passes through here.
func truncateBody(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return sanitizeControls(string(b))
}

// sanitizeControls escapes terminal-significant control runes (C0 except
// tab, DEL, and the C1 range carrying the 8-bit CSI/OSC forms) as \xNN or
// \u00XX, so a hostile response can never rewrite the run log with escape
// sequences or inject forged log lines. Invalid UTF-8 bytes are escaped too:
// a lone 0x9b steers an 8-bit terminal exactly like an ESC-prefixed CSI.
func sanitizeControls(s string) string {
	return escapeControls(s, isControlRune)
}

// escapeControls rewrites s with every rune the hostile predicate flags
// escaped as \xNN or \u00XX; clean text passes through untouched.
func escapeControls(s string, hostile func(rune) bool) string {
	escaped := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if hostile(r) || (r == utf8.RuneError && size == 1) {
			escaped = true
			break
		}
		i += size
	}
	if !escaped {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, "\\x%02x", s[i])
		case !hostile(r):
			b.WriteString(s[i : i+size])
		case r < 0x80:
			fmt.Fprintf(&b, "\\x%02x", byte(r))
		default:
			fmt.Fprintf(&b, "\\u%04x", r)
		}
		i += size
	}
	return b.String()
}

func isControlRune(r rune) bool {
	return r != '\t' && (r < 0x20 || (r >= 0x7f && r < 0xa0))
}

type gqlEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func decodeEnvelope(body []byte, out any) error {
	var env gqlEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		// the graphql message is remote text riding into the run log via
		// error prints, so it gets the same cap and neutralization as a
		// non-200 body
		return fmt.Errorf("stratz graphql: %s", truncateBody([]byte(env.Errors[0].Message)))
	}
	return json.Unmarshal(env.Data, out)
}

const rosterQuery = "query { constants { heroes { id name displayName shortName } } }"

type rosterData struct {
	Constants struct {
		Heroes []rosterHero `json:"heroes"`
	} `json:"constants"`
}

// parseRoster sorts by id so the crawl order is deterministic.
func parseRoster(body []byte) ([]rosterHero, error) {
	var d rosterData
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	sort.Slice(d.Constants.Heroes, func(i, j int) bool {
		return d.Constants.Heroes[i].ID < d.Constants.Heroes[j].ID
	})
	return d.Constants.Heroes, nil
}

func (s *stratzClient) FetchRoster() ([]rosterHero, error) {
	body, err := s.query(rosterQuery)
	if err != nil {
		return nil, err
	}
	return parseRoster(body)
}

const matchUpShape = "heroId vs { heroId2 matchCount winCount synergy } with { heroId2 matchCount winCount synergy }"

// matchUpRowCap is PAGINATION, never a window: matchUp's take caps the enemy
// row list (live-proven by make probe-window: take 30 serves exactly 30 rows),
// so the crawl pins it above the 126-enemy roster to return every row. The
// pair window is the week arg alone.
const matchUpRowCap = 200

// weekStarts returns the week-start unix seconds (Monday 00:00 UTC) for the
// in-progress week and the two completed weeks before it. The pair and item
// tables pin to the last completed week: the in-progress week value is
// ignored server-side and silently serves the undocumented default.
func weekStarts(now time.Time) (thisWeek, lastWeek, weekBefore int64) {
	u := now.UTC()
	daysSinceMonday := (int(u.Weekday()) + 6) % 7
	this := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceMonday)
	return this.Unix(), this.AddDate(0, 0, -7).Unix(), this.AddDate(0, 0, -14).Unix()
}

// LastCompletedWeekStart is the week stamp the product crawls pin. Exported
// because the builds derive guards against the same pin.
func LastCompletedWeekStart(now time.Time) int64 {
	_, last, _ := weekStarts(now)
	return last
}

// matchUpQuery builds the pair query. take is pagination; week 0 omits the
// window arg (probe evidence arms), any other value pins the calendar week.
func matchUpQuery(heroID, take int, bracket string, week int64) string {
	weekArg := ""
	if week != 0 {
		weekArg = fmt.Sprintf(", week: %d", week)
	}
	return fmt.Sprintf(
		"query { heroStats { matchUp(heroId: %d, take: %d, bracketBasicIds: [%s]%s) { %s } } }",
		heroID, take, bracket, weekArg, matchUpShape)
}

type matchUpData struct {
	HeroStats struct {
		MatchUp []matchUpResult `json:"matchUp"`
	} `json:"heroStats"`
}

func parseMatchUp(body []byte) (*matchUpResult, error) {
	var d matchUpData
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	if len(d.HeroStats.MatchUp) != 1 {
		return nil, fmt.Errorf("matchUp returned %d results, want 1", len(d.HeroStats.MatchUp))
	}
	mu := &d.HeroStats.MatchUp[0]
	mu.Vs = cleanRows(mu.Vs, mu.HeroID)
	mu.With = cleanRows(mu.With, mu.HeroID)
	return mu, nil
}

// cleanRows drops the zero sentinel and self rows stratz returns.
func cleanRows(rows []matchupRow, selfID int) []matchupRow {
	out := make([]matchupRow, 0, len(rows))
	for _, r := range rows {
		if r.HeroID2 > 0 && r.HeroID2 != selfID {
			out = append(out, r)
		}
	}
	return out
}

// FetchMatchUp takes the week stamp from its caller: the crawl computes the
// pin once per run so every cache stamps the week actually fetched, never a
// per-call recompute that could cross Monday 00:00 UTC mid-crawl.
func (s *stratzClient) FetchMatchUp(heroID int, bracket string, week int64) (*matchUpResult, error) {
	body, err := s.query(matchUpQuery(heroID, matchUpRowCap, bracket, week))
	if err != nil {
		return nil, err
	}
	return parseMatchUp(body)
}

type popRow struct {
	HeroID     int   `json:"heroId"`
	MatchCount int64 `json:"matchCount"`
	WinCount   int64 `json:"winCount"`
}

type popData struct {
	HeroStats map[string]json.RawMessage `json:"heroStats"`
}

// parsePopRows pulls the first aggregate list out of the heroStats object:
// the response keys mirror whichever aggregate field the query asked for.
func parsePopRows(body []byte) ([]popRow, error) {
	var d popData
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	for _, raw := range d.HeroStats {
		var rows []popRow
		if err := json.Unmarshal(raw, &rows); err == nil && len(rows) > 0 {
			return rows, nil
		}
	}
	return nil, nil
}

// gameModeEnumToken maps the numeric game-mode id the config hub stores to
// the graphql enum token the win* aggregates require. Numeric ids are
// rejected by the api ("Expected type GameModeEnumType"); the mapping is
// verified against constants by make probe-scope (ref/dota2/README.md scope
// conventions).
func gameModeEnumToken(id int) (string, bool) {
	tokens := map[int]string{
		2:  "CAPTAINS_MODE",
		22: "ALL_PICK_RANKED",
	}
	tok, ok := tokens[id]
	return tok, ok
}

// winBracketIds maps the basic bracket the scope hub stores to the bracketIds
// values the win* aggregates take: DIVINE_IMMORTAL is one basic bracket but
// the win* family wants the two rank brackets spelled out.
func winBracketIds(bracket string) (string, bool) {
	ids := map[string]string{
		"DIVINE_IMMORTAL": "DIVINE, IMMORTAL",
	}
	v, ok := ids[bracket]
	return v, ok
}

// aggregateQuery is the scoped per-hero source for popularity and overall
// counts: winDay (rolling take-day buckets, summed per hero at ingest) under
// the bracket and game-mode filters. Only the win* family accepts
// gameModeIds, and winBracket is gone from the schema. An unmapped scope id
// fails loudly: rendering an empty filter list would read as "no filter"
// server-side and silently crawl unscoped, so no fallback exists.
func aggregateQuery(cfg *config.Config) (string, error) {
	return aggregateQueryAt(cfg, cfg.Stratz.Take)
}

// aggregateQueryAt renders the product popularity query at an arbitrary take
// so the window probe ladders the exact product shape, never a hand copy.
func aggregateQueryAt(cfg *config.Config, take int) (string, error) {
	mode, ok := gameModeEnumToken(cfg.Scope.GameMode)
	if !ok {
		return "", fmt.Errorf("popularity: gameMode %d has no known enum token", cfg.Scope.GameMode)
	}
	brackets, ok := winBracketIds(cfg.Scope.Bracket)
	if !ok {
		return "", fmt.Errorf("popularity: bracket %q has no win* bracketIds mapping", cfg.Scope.Bracket)
	}
	return fmt.Sprintf(
		"query { heroStats { winDay(take: %d, bracketIds: [%s], gameModeIds: [%s]) { heroId matchCount winCount } } }",
		take, brackets, mode), nil
}

func (s *stratzClient) fetchScopedAggregate() ([]popRow, error) {
	q, err := aggregateQuery(s.cfg)
	if err != nil {
		return nil, err
	}
	body, err := s.query(q)
	if err != nil {
		return nil, err
	}
	return parsePopRows(body)
}
