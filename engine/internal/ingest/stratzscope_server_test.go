package ingest

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"poolguide/internal/config"
)

// scopeIntroSnapshot carries the HeroStatsQuery arg surface both probes read:
// introspectScope's per-field arg lists plus the argTypeNames type names
// (minTime and maxTime Int so the window probe's minute-filter arm fires),
// the win row types, the groupBy enum, and the root/input surfaces
// printScopeTypes walks.
const scopeIntroSnapshot = `{"sch":{"queryType":{"name":"StratzQuery","fields":[` +
	`{"name":"heroStats","args":[{"name":"take"}],"type":{"kind":"OBJECT","name":"HeroStatsQuery"}},` +
	`{"name":"findMatchPlayer","args":[{"name":"request"}],"type":{"kind":"OBJECT","name":"MatchType"}}]},` +
	`"types":[` +
	`{"name":"HeroStatsQuery","fields":[` +
	`{"name":"matchUp","args":[{"name":"heroId"},{"name":"take","type":{"kind":"SCALAR","name":"Long"}},{"name":"bracketBasicIds"},{"name":"week","type":{"kind":"SCALAR","name":"Long"}}],"type":{"kind":"OBJECT","name":"HeroVsHeroMatchupType"}},` +
	`{"name":"winDay","args":[{"name":"take","type":{"kind":"SCALAR","name":"Long"}}],"type":{"kind":"OBJECT","name":"HeroWinDayType"}},` +
	`{"name":"winMonth","args":[{"name":"take"}],"type":{"kind":"OBJECT","name":"HeroWinMonthType"}},` +
	`{"name":"winWeek","args":[{"name":"take"}],"type":{"kind":"OBJECT","name":"HeroWinWeekType"}},` +
	`{"name":"itemFullPurchase","args":[{"name":"heroId"},{"name":"matchLimit"},{"name":"week","type":{"kind":"SCALAR","name":"Long"}},{"name":"minTime","type":{"kind":"SCALAR","name":"Int"}},{"name":"maxTime","type":{"kind":"SCALAR","name":"Int"}}],"type":{"kind":"OBJECT","name":"HeroItemFullPurchaseType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroWinDayType","fields":[{"name":"heroId"},{"name":"matchCount"},{"name":"winCount"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroWinWeekType","fields":[{"name":"heroId"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroWinMonthType","fields":[{"name":"heroId"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroWinGameVersionType","fields":[{"name":"gameVersionId"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroStatsType","fields":[{"name":"matchUp"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroStatsTakeType","fields":[{"name":"take"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroVsHeroMatchupType","fields":[{"name":"vs"}],"inputFields":[],"enumValues":[]},` +
	`{"name":"HeroStatsGroupByEnum","fields":[],"inputFields":[],"enumValues":[{"name":"HERO_ID"},{"name":"ALL"},{"name":"HERO_ID_POSITION_BRACKET"}]},` +
	`{"name":"FindMatchPlayerRequestType","fields":[],"inputFields":[{"name":"groupBy"},{"name":"bracketBasicIds"},{"name":"gameModeIds"}],"enumValues":[]},` +
	`{"name":"FilterHeroWinRequestType","fields":[],"inputFields":[{"name":"take"},{"name":"bracketIds"}],"enumValues":[]}]}}`

// scopeResponder answers the ProbeScope surface: the schema snapshot, the
// constants cross-check, the roster, and one single-row heroStats aggregate
// for whichever field a ladder step named. The numeric gameModeIds ladder arm
// is rejected so every ladder carries one REJECTED evidence line.
func scopeResponder(mutate func(string) (stubReply, bool)) func(string, int, *http.Request) stubReply {
	return func(q string, _ int, _ *http.Request) stubReply {
		if mutate != nil {
			if rep, ok := mutate(q); ok {
				return rep
			}
		}
		switch {
		case strings.Contains(q, "__schema"):
			return dataReply(scopeIntroSnapshot)
		case strings.Contains(q, "gameModes { id name"):
			return dataReply(`{"constants":{"gameModes":[{"id":22,"name":"ALL_PICK_RANKED"},{"id":2,"name":"CAPTAINS_MODE"}],` +
				`"lobbyTypes":[{"id":7,"name":"RANKED"}]}}`)
		case strings.Contains(q, "constants { heroes"):
			return dataReply(crawlRoster)
		case strings.Contains(q, "gameModeIds: [22]"):
			return errReply("Expected type GameModeEnumType, found 22")
		case strings.Contains(q, "heroStats { "):
			field := q[strings.Index(q, "heroStats { ")+len("heroStats { "):]
			if i := strings.IndexByte(field, '('); i >= 0 {
				field = field[:i]
			}
			return dataReply(fmt.Sprintf(`{"heroStats":{"%s":[{"heroId":1,"matchCount":10,"winCount":5}]}}`, field))
		case strings.Contains(q, "findMatchPlayer"):
			return dataReply(`{}`)
		}
		return dataReply(`{}`)
	}
}

func scopeTestEnv(t *testing.T, mutate func(string) (stubReply, bool)) (*config.Config, *stratzServer) {
	t.Helper()
	cfg := serverTestConfig(t.TempDir())
	writeServerToken(t, cfg)
	cfg.Pool = []config.PoolEntry{{Slug: "anti-mage", Name: "Anti-Mage", Role: "1"}}
	s := newStratzServer(t, scopeResponder(mutate))
	redirectStratzTraffic(t, s)
	return cfg, s
}

// the full probe walk answers every ladder and equivalence arm: 59 requests
// covering the schema snapshot, the constants cross-check, the roster, the
// matchUp plus three win* ladders with their seven filter extras each, the
// three mode-equivalence pairs, findMatchPlayer, the winGameVersion ladder
// and row shape, the position slice, the grouped shapes, the builds ladder,
// and the position-list shape
func TestProbeScopeFullWalk(t *testing.T) {
	cfg, s := scopeTestEnv(t, nil)
	if err := ProbeScope(cfg); err != nil {
		t.Fatal(err)
	}
	if s.count() != 59 {
		t.Fatalf("probe made %d requests, want 59", s.count())
	}
	for _, site := range []string{
		"__schema",
		"matchUp(heroId: 1",
		"gameModeIds: [22]",
		"bracketIds: [7, 8]",
		"positionIds: [POSITION_1]",
		"findMatchPlayer",
		"winGameVersion",
		"groupBy: HERO_ID_POSITION_BRACKET",
		"itemFullPurchase(heroId: 1",
		"positionIds: [POSITION_1, POSITION_2]",
	} {
		s.saw(t, site, "full walk")
	}
}

// every loud failure on the scope path: token, roster, pool shape
func TestProbeScopeFailures(t *testing.T) {
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
			wantErr: "scope probe roster",
		},
		{
			name: "no pool hero in roster",
			setup: func(t *testing.T, cfg *config.Config) {
				cfg.Pool = []config.PoolEntry{{Slug: "axe", Name: "Axe", Role: "1"}}
			},
			wantErr: "no pool hero found in roster",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := scopeTestEnv(t, tt.mutate)
			if tt.noToken {
				t.Setenv("STRATZ_TOKEN", "")
				os.Remove(cfg.Paths.TokenFile)
			}
			if tt.setup != nil {
				tt.setup(t, cfg)
			}
			err := ProbeScope(cfg)
			if err == nil {
				t.Fatal("probe succeeded, want a loud failure")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// the evidence arms degrade to prints, never fail the probe: a refused
// snapshot, a refused constants cross-check, a refused equivalence pair, a
// refused findMatchPlayer
func TestProbeScopeDegradedArms(t *testing.T) {
	t.Run("introspection refused", func(t *testing.T) {
		cfg, s := scopeTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "__schema") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		if err := ProbeScope(cfg); err != nil {
			t.Fatal(err)
		}
		if s.count() != 59 {
			t.Fatalf("probe made %d requests, want 59 (no request hangs off the snapshot)", s.count())
		}
	})
	t.Run("constants refused", func(t *testing.T) {
		cfg, _ := scopeTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "gameModes { id name") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		if err := ProbeScope(cfg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("mode equivalence refused", func(t *testing.T) {
		cfg, _ := scopeTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		if err := ProbeScope(cfg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("findMatchPlayer refused", func(t *testing.T) {
		cfg, _ := scopeTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "findMatchPlayer") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		if err := ProbeScope(cfg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("position-list shape refused", func(t *testing.T) {
		cfg, _ := scopeTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "positionIds: [POSITION_1, POSITION_2]") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		if err := ProbeScope(cfg); err != nil {
			t.Fatal(err)
		}
	})
}

// a heroStats value that is not a row list counts as zero rows, not a
// rejection: only the graphql error envelope rejects
func TestCountHeroStatsRowsSkipsNonLists(t *testing.T) {
	n, err := countHeroStatsRows([]byte(`{"data":{"heroStats":{"winWeek":{"nested":true}}}}`))
	if err != nil || n != 0 {
		t.Fatalf("non-list value = %d %v, want 0 nil", n, err)
	}
}

// introspectScope: the arg map off the snapshot, loud on every failure shape
func TestIntrospectScopeAgainstStubServer(t *testing.T) {
	tests := []struct {
		name    string
		respond func(_ string, _ int, _ *http.Request) stubReply
		wantErr string
	}{
		{"serves the arg lists", func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "__schema") {
				return dataReply(scopeIntroSnapshot)
			}
			return dataReply(`{}`)
		}, ""},
		{"transport failure", func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		}, "stratz 403"},
		{"torn snapshot", func(_ string, _ int, _ *http.Request) stubReply {
			return okReply(`{"sch":{"types":[{`)
		}, "unexpected end"},
		{"no HeroStatsQuery type", func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"sch":{"types":[{"name":"Other","fields":[]}]}}`)
		}, "HeroStatsQuery type not present"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := stubClient(t, tt.respond)
			env, args, err := introspectScope(c)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if env == nil || len(args["matchUp"]) == 0 || len(args["itemFullPurchase"]) == 0 {
				t.Fatalf("args = %v, want the matchUp and itemFullPurchase lists", args)
			}
		})
	}
}

// probeModeEquivalence: identical arms read as zero diff, both fetch failures
// surface
func TestProbeModeEquivalenceAgainstStubServer(t *testing.T) {
	t.Run("identical arms differ nowhere", func(t *testing.T) {
		c, _ := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "heroStats { winDay(") {
				return dataReply(`{"heroStats":{"winDay":[{"heroId":1,"matchCount":10,"winCount":5}]}}`)
			}
			return dataReply(`{}`)
		})
		if err := probeModeEquivalence(c, "winDay", 7, "bracketIds: [DIVINE, IMMORTAL]", "bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("base fetch failure surfaces", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		err := probeModeEquivalence(c, "winDay", 7, "bracketIds: [DIVINE, IMMORTAL]", "")
		if err == nil || !strings.Contains(err.Error(), "stratz 403") {
			t.Fatalf("err = %v, want the base fetch failure", err)
		}
	})
	t.Run("filtered fetch failure surfaces", func(t *testing.T) {
		c, _ := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "gameModeIds") {
				return stubReply{code: 403, body: "denied"}
			}
			return dataReply(`{"heroStats":{"winDay":[{"heroId":1,"matchCount":10,"winCount":5}]}}`)
		})
		err := probeModeEquivalence(c, "winDay", 7, "", "gameModeIds: [ALL_PICK_RANKED]")
		if err == nil || !strings.Contains(err.Error(), "stratz 403") {
			t.Fatalf("err = %v, want the filtered fetch failure", err)
		}
	})
	t.Run("filtered fetch graphql failure surfaces at decode", func(t *testing.T) {
		c, _ := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "gameModeIds") {
				return errReply("filter rejected")
			}
			return dataReply(`{"heroStats":{"winDay":[{"heroId":1,"matchCount":10,"winCount":5}]}}`)
		})
		err := probeModeEquivalence(c, "winDay", 7, "", "gameModeIds: [ALL_PICK_RANKED]")
		if err == nil || !strings.Contains(err.Error(), "stratz graphql") {
			t.Fatalf("err = %v, want the filtered decode failure", err)
		}
	})
	t.Run("rows that are not popRow lists read as an empty diff", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"heroStats":{"winDay":[1,2]}}`)
		})
		if err := probeModeEquivalence(c, "winDay", 7, "", "gameModeIds: [ALL_PICK_RANKED]"); err != nil {
			t.Fatalf("unparsable rows must fold to an empty diff, got %v", err)
		}
	})
	t.Run("a heavier filtered arm folds its negative gap to the magnitude", func(t *testing.T) {
		c, _ := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "gameModeIds") {
				return dataReply(`{"heroStats":{"winDay":[{"heroId":1,"matchCount":100,"winCount":50}]}}`)
			}
			return dataReply(`{"heroStats":{"winDay":[{"heroId":1,"matchCount":90,"winCount":45}]}}`)
		})
		if err := probeModeEquivalence(c, "winDay", 7, "", "gameModeIds: [ALL_PICK_RANKED]"); err != nil {
			t.Fatal(err)
		}
	})
}
