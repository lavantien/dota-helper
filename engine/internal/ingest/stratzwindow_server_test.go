package ingest

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
)

// windowBuildsRows are the itemFullPurchase arms: the no-week baseline (4 rows
// summing 400), the minute-filter arm (2 rows, narrower than the baseline),
// and the completed-week arm (3 rows summing 300, inside the single-week band
// against the baseline volume).
const windowBuildsBaseline = `{"heroStats":{"itemFullPurchase":[` +
	`{"itemId":1,"time":5,"instance":0,"matchCount":150,"winCount":80},` +
	`{"itemId":2,"time":6,"instance":0,"matchCount":100,"winCount":50},` +
	`{"itemId":3,"time":7,"instance":0,"matchCount":90,"winCount":45},` +
	`{"itemId":4,"time":8,"instance":0,"matchCount":60,"winCount":30}]}}`

const windowBuildsMinute = `{"heroStats":{"itemFullPurchase":[` +
	`{"itemId":1,"time":0,"instance":0,"matchCount":60,"winCount":30},` +
	`{"itemId":4,"time":9,"instance":0,"matchCount":40,"winCount":20}]}}`

const windowBuildsWeek = `{"heroStats":{"itemFullPurchase":[` +
	`{"itemId":1,"time":5,"instance":0,"matchCount":100,"winCount":50},` +
	`{"itemId":2,"time":6,"instance":0,"matchCount":100,"winCount":50},` +
	`{"itemId":3,"time":7,"instance":0,"matchCount":100,"winCount":50}]}}`

// winDayRows renders the roster x day-bucket universe at one take, 1000
// matches per hero-day: the server caps the take at 30 days, so both the
// saturation and the old-hub arm see the 30-day universe.
func winDayRows(take int64) string {
	days := take
	if days > 30 {
		days = 30
	}
	var b strings.Builder
	for _, hero := range []int64{1, 8, 14} {
		for d := int64(0); d < days; d++ {
			fmt.Fprintf(&b, `{"heroId":%d,"matchCount":1000,"winCount":500},`, hero)
		}
	}
	return strings.TrimSuffix(b.String(), ",")
}

// windowVsRows renders n cleaned vs rows at one fixed volume per row.
func windowVsRows(n int, matches int64) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = jsonRow(int64(100+i), matches, 1.5)
	}
	return strings.Join(parts, ", ")
}

// windowResponder answers the whole ProbeWindow surface with volumes that pin
// every verdict: the winDay universe per take (21 rows at the hub take 7, 90 at
// the cap), pair pagination arms at 400 a row, the completed-week pair arm at
// 35000 (ratio 1.0 against the 7000 hero calibration x 5 enemy slots), and
// the builds arms above.
func windowResponder(mutate func(string) (stubReply, bool)) func(string, int, *http.Request) stubReply {
	return func(q string, _ int, _ *http.Request) stubReply {
		if mutate != nil {
			if rep, ok := mutate(q); ok {
				return rep
			}
		}
		switch {
		case strings.Contains(q, "__schema"):
			return dataReply(scopeIntroSnapshot)
		case strings.Contains(q, "constants { heroes"):
			return dataReply(crawlRoster)
		case strings.Contains(q, "matchUp(heroId:"):
			if w := qInt(q, "week: "); w != 0 {
				thisW, lastW, beforeW := weekStarts(time.Now())
				switch w {
				case lastW:
					return dataReply(matchUpEnvelope(1, windowVsRows(70, 500), ""))
				case thisW:
					return dataReply(matchUpEnvelope(1, windowVsRows(12, 100), ""))
				case beforeW:
					return dataReply(matchUpEnvelope(1, windowVsRows(50, 300), ""))
				}
				return dataReply(matchUpEnvelope(1, windowVsRows(10, 100), ""))
			}
			return dataReply(matchUpEnvelope(1, windowVsRows(int(qInt(q, "take: ")), 400), ""))
		case strings.Contains(q, "itemFullPurchase"):
			switch {
			case strings.Contains(q, "minTime: 0, maxTime: 10"):
				return dataReply(windowBuildsMinute)
			case qInt(q, "week: ") != 0:
				return dataReply(windowBuildsWeek)
			}
			return dataReply(windowBuildsBaseline)
		case strings.Contains(q, "winDay(take: "):
			return dataReply(`{"heroStats":{"winDay":[` + winDayRows(qInt(q, "winDay(take: ")) + `]}}`)
		}
		return dataReply(`{}`)
	}
}

func windowTestEnv(t *testing.T, mutate func(string) (stubReply, bool)) (*config.Config, *stratzServer) {
	t.Helper()
	cfg := serverTestConfig(t.TempDir())
	writeServerToken(t, cfg)
	cfg.Score = config.ScoreConsts{EnemySlots: 5}
	cfg.Pool = []config.PoolEntry{{Slug: "anti-mage", Name: "Anti-Mage", Role: "1"}}
	s := newStratzServer(t, windowResponder(mutate))
	redirectStratzTraffic(t, s)
	return cfg, s
}

// the full probe walk pins every family: 18 requests covering the schema
// snapshot, the roster, three winDay ladder arms plus the position arms, three
// matchUp pagination arms, the hero calibration, three week arms, and the
// three builds arms, ending with no failed family
func TestProbeWindowFullWalk(t *testing.T) {
	cfg, s := windowTestEnv(t, nil)
	if err := ProbeWindow(cfg); err != nil {
		t.Fatal(err)
	}
	if s.count() != 18 {
		t.Fatalf("probe made %d requests, want 18", s.count())
	}
	for _, site := range []string{
		"winDay(take: 7, bracketIds",
		"winDay(take: 30, bracketIds",
		"positionIds: [POSITION_1]",
		"matchUp(heroId: 1, take: 200",
		"minTime: 0, maxTime: 10",
	} {
		s.saw(t, site, "full walk")
	}
	if n := s.countMatching("winDay(take: 7,"); n != 3 {
		t.Fatalf("take-7 aggregate ran %d times, want 3 (ladder + position slice + hero calibration)", n)
	}
}

// a hub take beside the calibration arm forces the extra 7-day fetch: 19
// requests, the ladder at 14/30/200 plus the separate calibration fetch
func TestProbeWindowCalibrationArmBesideLadder(t *testing.T) {
	cfg, s := windowTestEnv(t, nil)
	cfg.Stratz.Take = 14
	if err := ProbeWindow(cfg); err != nil {
		t.Fatal(err)
	}
	if s.count() != 19 {
		t.Fatalf("probe made %d requests, want 19 (calibration outside the ladder)", s.count())
	}
	if n := s.countMatching("winDay(take: 7,"); n != 2 {
		t.Fatalf("take-7 aggregate ran %d times, want 2 (extra calibration + hero calibration)", n)
	}
}

// every loud failure on the window path: token, roster, pool shape, unmapped
// scope ids, an unpinned hub take, a failed saturation arm, an ignored week
// value, a starved builds week, a dead pair family
func TestProbeWindowFailures(t *testing.T) {
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
			wantErr: "window probe roster",
		},
		{
			name: "no pool hero in roster",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Pool = []config.PoolEntry{{Slug: "axe", Name: "Axe", Role: "1"}}
			},
			wantErr: "no pool hero found in roster",
		},
		{
			name: "unmapped game mode aborts at the position arm",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Scope.GameMode = 21
			},
			wantErr: "window probe: gameMode 21 has no known enum token",
		},
		{
			name: "unmapped bracket aborts at the position arm",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Scope.Bracket = "HERALD"
			},
			wantErr: `window probe: bracket "HERALD" has no win* bracketIds mapping`,
		},
		{
			name: "hub take at the server cap pins nothing",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Stratz.Take = 30
			},
			wantErr: "families whose window cannot be pinned: winDay",
		},
		{
			name: "failed saturation arm refuses winDay",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "winDay(take: 30,") {
					return stubReply{code: 403, body: "denied"}, true
				}
				return stubReply{}, false
			},
			wantErr: "families whose window cannot be pinned: winDay",
		},
		{
			name: "week value ignored by the server refuses matchUp",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "matchUp(heroId:") && qInt(q, "week: ") != 0 {
					return dataReply(matchUpEnvelope(1, windowVsRows(200, 400), "")), true
				}
				return stubReply{}, false
			},
			wantErr: "families whose window cannot be pinned: matchUp week",
		},
		{
			name: "starved builds week falls to the implied-span print",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "itemFullPurchase") && qInt(q, "week: ") != 0 {
					return dataReply(`{"heroStats":{"itemFullPurchase":[{"itemId":1,"time":5,"instance":0,"matchCount":10,"winCount":5}]}}`), true
				}
				return stubReply{}, false
			},
			wantErr: "itemFullPurchase week",
		},
		{
			name: "dead pair family refuses the week verdict",
			mutate: func(q string) (stubReply, bool) {
				if strings.Contains(q, "matchUp(heroId:") && qInt(q, "week: ") == 0 {
					return stubReply{code: 403, body: "denied"}, true
				}
				return stubReply{}, false
			},
			wantErr: "families whose window cannot be pinned: matchUp week",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := windowTestEnv(t, tt.mutate)
			if tt.noToken {
				t.Setenv("STRATZ_TOKEN", "")
				os.Remove(cfg.Paths.TokenFile)
			}
			if tt.setup != nil {
				tt.setup(t, cfg)
			}
			err := ProbeWindow(cfg)
			if err == nil {
				t.Fatal("probe succeeded, want a loud failure")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// a refused snapshot is evidence, not fatal: the walk continues without the
// arg-type prints and the minute-filter arm stays closed
func TestProbeWindowIntrospectionRefused(t *testing.T) {
	cfg, s := windowTestEnv(t, func(q string) (stubReply, bool) {
		if strings.Contains(q, "__schema") {
			return stubReply{code: 403, body: "denied"}, true
		}
		return stubReply{}, false
	})
	if err := ProbeWindow(cfg); err != nil {
		t.Fatal(err)
	}
	if s.countMatching("minTime: 0, maxTime: 10") != 0 {
		t.Fatal("the minute-filter arm must stay closed without arg types")
	}
	if s.count() != 17 {
		t.Fatalf("probe made %d requests, want 17 (refused snapshot still asked, minute arm skipped)", s.count())
	}
}
