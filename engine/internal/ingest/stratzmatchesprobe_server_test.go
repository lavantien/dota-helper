package ingest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"poolguide/internal/config"
)

// probeResponder answers the whole ProbeMatches surface: introspection, shape
// ladder, constants, leaderboard seasons, player browse arms (answer, empty,
// rejected), league listing filter ladder, league matches, pagination pages,
// and the by-ids round trip.
func probeResponder(mutate func(string) (stubReply, bool)) func(string, int, *http.Request) stubReply {
	return func(q string, _ int, _ *http.Request) stubReply {
		if mutate != nil {
			if rep, ok := mutate(q); ok {
				return rep
			}
		}
		switch {
		case strings.Contains(q, "__schema"):
			return dataReply(probeIntroSnapshot)
		case strings.Contains(q, "constants { heroes"):
			return dataReply(crawlRoster)
		case strings.Contains(q, "gameModes { id name"):
			return dataReply(`{"constants":{"gameModes":[{"id":22,"name":"ALL_PICK_RANKED"},{"id":2,"name":"CAPTAINS_MODE"}],` +
				`"lobbyTypes":[{"id":1,"name":"PRACTICE"},{"id":2,"name":"TOURNAMENT"},{"id":7,"name":"RANKED"}]}}`)
		case strings.Contains(q, "players(steamAccountIds"):
			return dataReply(`{"players":[{"matches":[` + probeRow(61, true) + `]}]}`)
		case strings.Contains(q, "leaderBoardDivision"):
			switch {
			case strings.Contains(q, "leaderBoardDivision: EUROPE"):
				return dataReply(`{"leaderboard":{"season":{"playerCount":2,"players":[{"steamAccountId":42},{"steamAccountId":43}]}}}`)
			case strings.Contains(q, "leaderBoardDivision: AMERICAS"):
				return errReply("leaderboard unavailable")
			case strings.Contains(q, "leaderBoardDivision: SE_ASIA"):
				return dataReply(`{"leaderboard":{"season":{"playerCount":0,"players":[]}}}`)
			}
			return dataReply(`{"leaderboard":{"season":{"playerCount":1,"players":[{"steamAccountId":50}]}}}`)
		case strings.Contains(q, "player(steamAccountId"):
			switch {
			case strings.Contains(q, "matchCount lastMatchDate"):
				count := 9
				if qInt(q, "player(steamAccountId: ") == 42 {
					count = 0
				}
				return dataReply(fmt.Sprintf(`{"player":{"matchCount":%d,"lastMatchDate":1759000000}}`, count))
			case strings.Contains(q, "after: 9000000000"):
				return errReply("cursor rejected")
			case strings.Contains(q, "matches(request: { take: 5 }) { id }"):
				return dataReply(`{"player":{"matches":[]}}`)
			case strings.Contains(q, "gameModeIds: [2]"):
				// the captains-mode browse arm degrades to an evidence print
				return stubReply{code: 403, body: "denied"}
			}
			return dataReply(`{"player":{"matches":[` + probeRow(62, true) + `]}}`)
		case strings.Contains(q, "leagues(request") && strings.Contains(q, "matches(request"):
			return dataReply(`{"leagues":[` +
				`{"id":11,"name":"","displayName":"The International Regional Qualifier Cup","matches":[` + probeRow(101, true) + `]},` +
				`{"id":12,"name":"Named Cup","displayName":"","matches":[` + probeRow(102, true) + `]}]}`)
		case strings.Contains(q, "match(id:"):
			return dataReply(`{"matches":[` + probeRow(71, true) + `]}`)
		case strings.Contains(q, "matches(ids:"):
			return dataReply(`{"matches":[` + probeRow(81, true) + `,` + probeRow(82, true) + `]}`)
		case strings.Contains(q, "league(id:"):
			if strings.Contains(q, "skip: 5") {
				return dataReply(`{"league":{"matches":[]}}`)
			}
			return dataReply(`{"league":{"matches":[` + probeRow(201, true) + `,` +
				matchJSON(202, probePreStart, 2400, pickBansJSON(21, 10)) + `]}}`)
		case strings.Contains(q, "matches(") || strings.Contains(q, "matches {"):
			return dataReply(`{"matches":[` + probeRow(8, false) + `]}`)
		case strings.Contains(q, "leagues(request"):
			switch {
			case strings.Contains(q, "leagueEnded: false"):
				return stubReply{code: 403, body: "denied"}
			case strings.Contains(q, "isFutureLeague: false"):
				return errReply("filter rejected")
			case strings.Contains(q, "betweenEndDateTime"):
				return okReply(`{"data":{"leagues":[{`)
			}
			return dataReply(`{"leagues":[{"id":11,"displayName":"Test Cup","lastMatchDate":1759000000},` +
				`{"id":12,"displayName":"A Very Long League Name Indeed","lastMatchDate":1759000100}]}`)
		}
		return dataReply(`{}`)
	}
}

func probeTestEnv(t *testing.T, mutate func(string) (stubReply, bool)) (*config.Config, *stratzServer) {
	t.Helper()
	cfg := serverTestConfig(t.TempDir())
	writeServerToken(t, cfg)
	s := newStratzServer(t, probeResponder(mutate))
	redirectStratzTraffic(t, s)
	return cfg, s
}

// the full probe walk answers every ladder arm: 36 requests covering
// introspection, shape discovery, constants, four divisions, the seed walk,
// the browse ladder with its empty and rejected arms, both mode windows, the
// eight league-listing filters, league matches with the singular match, both
// pagination pages (the second empty, one row outside the window), and the
// by-ids round trip
func TestProbeMatchesFullWalk(t *testing.T) {
	cfg, s := probeTestEnv(t, nil)
	if err := ProbeMatches(cfg); err != nil {
		t.Fatal(err)
	}
	if s.count() != 36 {
		t.Fatalf("probe made %d requests, want 36", s.count())
	}
	for _, site := range []string{
		"__schema",
		"matches(ids: [9016183525",
		"match(id: 9016183525",
		"leaderBoardDivision: AMERICAS",
		"player(steamAccountId: 43",
		"after: 9000000000",
		"playerList: ALL",
		"gameModeIds: [2]",
		"leagueEnded: false",
		"betweenEndDateTime",
		"matches(request: { take: 100",
		"league(id: 11",
	} {
		s.saw(t, site, "full walk")
	}
}

// no served leaderboard player with matches ends the probe early and clean;
// introspection and constants failing are evidence prints, never fatal
func TestProbeMatchesNoSeedEndsEarly(t *testing.T) {
	cfg, s := probeTestEnv(t, func(q string) (stubReply, bool) {
		switch {
		case strings.Contains(q, "__schema"), strings.Contains(q, "gameModes { id name"):
			return stubReply{code: 403, body: "denied"}, true
		case strings.Contains(q, "leaderBoardDivision"):
			return dataReply(`{"leaderboard":{"season":{"playerCount":0,"players":[]}}}`), true
		}
		return stubReply{}, false
	})
	if err := ProbeMatches(cfg); err != nil {
		t.Fatal(err)
	}
	if s.count() != 12 {
		t.Fatalf("probe made %d requests, want 12 (no seed walk, no browse)", s.count())
	}
}

// the by-ids tail failing is reported, not fatal: the probe still returns nil,
// and a dead pagination page degrades the same way
func TestProbeMatchesByIdsFailureIsNotFatal(t *testing.T) {
	cfg, s := probeTestEnv(t, func(q string) (stubReply, bool) {
		switch {
		case strings.Contains(q, "matches(ids: [9016183525"):
			return stubReply{code: 403, body: "admin wall"}, true
		case strings.Contains(q, "skip: 5"):
			return stubReply{code: 403, body: "denied"}, true
		}
		return stubReply{}, false
	})
	if err := ProbeMatches(cfg); err != nil {
		t.Fatal(err)
	}
	s.saw(t, "matches(ids: [9016183525", "by-ids attempt")
}

func TestProbeMatchesFailures(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		t.Setenv("STRATZ_TOKEN", "")
		cfg := serverTestConfig(t.TempDir())
		if err := ProbeMatches(cfg); err == nil || !strings.Contains(err.Error(), "token missing") {
			t.Fatalf("err = %v, want missing token", err)
		}
	})
	t.Run("unparsable patch epoch", func(t *testing.T) {
		cfg, s := probeTestEnv(t, nil)
		cfg.PatchEpoch = "not-a-date"
		if err := ProbeMatches(cfg); err == nil || !strings.Contains(err.Error(), "patchEpoch") {
			t.Fatalf("err = %v, want the epoch failure", err)
		}
		if s.count() != 0 {
			t.Fatalf("epoch failure must precede any request, saw %d", s.count())
		}
	})
}

// pickProbeSeed walks served ids in division order: a dead candidate is
// skipped, the candidate cap bounds the walk, and the first player with
// matches wins
func TestPickProbeSeedWalk(t *testing.T) {
	t.Run("cap bounds the walk when nobody has matches", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "matchCount lastMatchDate") {
				switch qInt(q, "player(steamAccountId: ") {
				case 1:
					return stubReply{code: 403, body: "player hidden"}
				case 2:
					return errReply("player hidden")
				}
				return dataReply(`{"player":{"matchCount":0,"lastMatchDate":0}}`)
			}
			return dataReply(`{}`)
		})
		got := pickProbeSeed(c, map[string][]int64{
			"EUROPE":   {1, 2, 3},
			"AMERICAS": {4, 5, 6, 7, 8},
		})
		if got != 0 {
			t.Fatalf("seed = %d, want 0 when no candidate carries matches", got)
		}
		if s.count() != 5 {
			t.Fatalf("walk queried %d candidates, want the cap of 5", s.count())
		}
	})
	t.Run("first candidate with matches wins", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			count := 9
			if qInt(q, "player(steamAccountId: ") <= 2 {
				count = 0
			}
			return dataReply(fmt.Sprintf(`{"player":{"matchCount":%d,"lastMatchDate":1759000000}}`, count))
		})
		got := pickProbeSeed(c, map[string][]int64{"EUROPE": {1, 2, 3}})
		if got != 3 {
			t.Fatalf("seed = %d, want 3", got)
		}
		if s.count() != 3 {
			t.Fatalf("walk queried %d candidates, want 3", s.count())
		}
	})
}

func TestProbeMatchesPaginationFailures(t *testing.T) {
	tests := []struct {
		name    string
		respond func(q string, _ int, _ *http.Request) stubReply
		wantErr string
	}{
		{
			name: "no leagues served",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "leagues(request") {
					return dataReply(`{"leagues":[]}`)
				}
				return dataReply(`{}`)
			},
			wantErr: "no leagues served",
		},
		{
			name: "league page rejected",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "league(id:") {
					return stubReply{code: 403, body: "denied"}
				}
				return dataReply(`{"leagues":[{"id":11}]}`)
			},
			wantErr: "stratz 403",
		},
		{
			name: "league page torn",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "league(id:") {
					return okReply(`{"data":{"league":{"mat`)
				}
				return dataReply(`{"leagues":[{"id":11}]}`)
			},
			wantErr: "unexpected end",
		},
		{
			name: "leagues listing rejected",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "leagues(request") {
					return stubReply{code: 403, body: "denied"}
				}
				return dataReply(`{}`)
			},
			wantErr: "stratz 403",
		},
		{
			name: "leagues listing torn",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "leagues(request") {
					return okReply(`{"data":{"leagues":[{`)
				}
				return dataReply(`{}`)
			},
			wantErr: "unexpected end",
		},
		{
			name: "second page rejected",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "league(id:") && strings.Contains(q, "skip: 5") {
					return stubReply{code: 403, body: "denied"}
				}
				if strings.Contains(q, "leagues(request") {
					return dataReply(`{"leagues":[{"id":11}]}`)
				}
				return dataReply(`{"league":{"matches":[{"id":201}]}}`)
			},
			wantErr: "stratz 403",
		},
		{
			name: "overlapping pages count their overlap",
			respond: func(q string, _ int, _ *http.Request) stubReply {
				if strings.Contains(q, "leagues(request") {
					return dataReply(`{"leagues":[{"id":11}]}`)
				}
				return dataReply(`{"league":{"matches":[{"id":201}]}}`)
			},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := stubClient(t, tt.respond)
			err := probeMatchesPagination(c, 1000, 2000)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// probeLeagueMatches degrades to evidence prints when the leagues browse is
// rejected or the singular match body tears
func TestProbeLeagueMatchesFailureModes(t *testing.T) {
	t.Run("leagues browse rejected", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "leagues(request") {
				return stubReply{code: 403, body: "denied"}
			}
			return dataReply(`{}`)
		})
		probeLeagueMatches(c, 1000, 2000)
		if s.count() != 1 {
			t.Fatalf("rejected browse must stop before the singular match, saw %d requests", s.count())
		}
	})
	t.Run("singular match torn", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "match(id:") {
				return okReply(`{"data":{"match":{"id":7`)
			}
			return dataReply(`{"leagues":[{"id":11,"name":"Cup","displayName":"","matches":[]}]}`)
		})
		probeLeagueMatches(c, 1000, 2000)
		if s.count() != 2 {
			t.Fatalf("torn singular match must still have walked the leagues, saw %d requests", s.count())
		}
	})
	t.Run("leagues body torn", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			return okReply(`{"data":{"leagues":[{"id":11,"matches":[`)
		})
		probeLeagueMatches(c, 1000, 2000)
		if s.count() != 1 {
			t.Fatalf("torn leagues body must stop the probe arm, saw %d requests", s.count())
		}
	})
}

func TestProbeMatchesConstantsAgainstStubServer(t *testing.T) {
	tests := []struct {
		name    string
		respond func(q string, _ int, _ *http.Request) stubReply
		wantErr string
	}{
		{"transport failure", func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		}, "stratz 403"},
		{"graphql failure", func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("constants hidden")
		}, "stratz graphql"},
		{"enum cross-check answers", func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"constants":{"gameModes":[{"id":22,"name":"ALL_PICK_RANKED"}],"lobbyTypes":[{"id":7,"name":"RANKED"}]}}`)
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := stubClient(t, tt.respond)
			err := probeMatchesConstants(c)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFetchSeasonPlayersAgainstStubServer(t *testing.T) {
	t.Run("serves ids and player count", func(t *testing.T) {
		c, _ := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "leaderBoardDivision: EUROPE") {
				return dataReply(`{"leaderboard":{"season":{"playerCount":2,"players":[{"steamAccountId":42},{"steamAccountId":43}]}}}`)
			}
			return dataReply(`{}`)
		})
		count, ids, err := fetchSeasonPlayers(c, "EUROPE")
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 || len(ids) != 2 || ids[0] != 42 || ids[1] != 43 {
			t.Fatalf("season = %d %v, want 2 [42 43]", count, ids)
		}
	})
	t.Run("transport and graphql failures", func(t *testing.T) {
		for _, rep := range []stubReply{
			{code: 403, body: "denied"},
			errReply("season hidden"),
		} {
			c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply { return rep })
			if _, _, err := fetchSeasonPlayers(c, "EUROPE"); err == nil {
				t.Fatalf("reply %+v must fail loudly", rep)
			}
		}
	})
}

func TestFetchPlayerMatchesShapesAgainstStubServer(t *testing.T) {
	t.Run("single player and plural players answer rows", func(t *testing.T) {
		c, _ := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "players(steamAccountIds") {
				return dataReply(`{"players":[{"matches":[{"id":61}]}]}`)
			}
			return dataReply(`{"player":{"matches":[{"id":62}]}}`)
		})
		rows, err := fetchPlayerMatches(c, "query { player(steamAccountId: 62) { matches { id } } }")
		if err != nil || len(rows) != 1 || rows[0].ID != 62 {
			t.Fatalf("single = %v %v", rows, err)
		}
		rows, err = fetchPlayersMatches(c, "query { players(steamAccountIds: [62]) { matches { id } } }")
		if err != nil || len(rows) != 1 || rows[0].ID != 61 {
			t.Fatalf("plural = %v %v", rows, err)
		}
	})
	t.Run("plural with no players is empty, not an error", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"players":[]}`)
		})
		rows, err := fetchPlayersMatches(c, "query { players(steamAccountIds: [62]) { matches { id } } }")
		if err != nil || rows != nil {
			t.Fatalf("empty plural = %v %v, want nil nil", rows, err)
		}
	})
	t.Run("graphql failure surfaces", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("admin wall")
		})
		if _, err := fetchPlayerMatches(c, "query { player(steamAccountId: 62) { matches { id } } }"); err == nil {
			t.Fatal("admin wall must fail loudly")
		}
	})
	t.Run("transport failures surface", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		if _, err := fetchPlayerMatches(c, "query { player(steamAccountId: 62) { matches { id } } }"); err == nil {
			t.Fatal("single-player transport failure must surface")
		}
		if _, err := fetchPlayersMatches(c, "query { players(steamAccountIds: [62]) { matches { id } } }"); err == nil {
			t.Fatal("plural transport failure must surface")
		}
	})
	t.Run("plural decode failure surfaces", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("admin wall")
		})
		if _, err := fetchPlayersMatches(c, "query { players(steamAccountIds: [62]) { matches { id } } }"); err == nil {
			t.Fatal("plural decode failure must surface")
		}
	})
}
