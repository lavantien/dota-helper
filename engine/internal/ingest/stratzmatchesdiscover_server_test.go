package ingest

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// matchesIntroSnapshot walks every introspectMatches print arm: a matches root
// field with args, the MatchType wants with resolvable sub-types, the
// match-carrying account/player/leaderboard/league surfaces, the page and
// request inputs, the season leaderboard shape, and the enum cross-checks.
const matchesIntroSnapshot = `{"sch":{"queryType":{"name":"StratzQuery","fields":[` +
	`{"name":"matches","args":[{"name":"ids"},{"name":"take"}],"type":{"kind":"OBJECT","name":"MatchType"}},` +
	`{"name":"player","args":[],"type":{"kind":"OBJECT","name":"SteamAccountType"}}]},` +
	`"types":[` +
	`{"name":"MatchType","fields":[` +
	`{"name":"id","args":[],"type":{"kind":"SCALAR","name":"Long"}},` +
	`{"name":"pickBans","args":[],"type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"PickBansType"}}},` +
	`{"name":"bracket","args":[],"type":{"kind":"SCALAR","name":"Int"}},` +
	`{"name":"lobbyType","args":[],"type":{"kind":"OBJECT","name":"LobbyTypeEnum"}},` +
	`{"name":"gameMode","args":[],"type":{"kind":"OBJECT","name":"GameModeEnumType"}},` +
	`{"name":"averageRank","args":[],"type":{"kind":"SCALAR","name":"Int"}},` +
	`{"name":"players","args":[],"type":{"kind":"OBJECT","name":"PlayerType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"PickBansType","fields":[{"name":"heroId","args":[],"type":{"kind":"SCALAR","name":"Int"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"GameModeEnumType","fields":[],"inputFields":[],"enumValues":[{"name":"ALL_PICK_RANKED"},{"name":"CAPTAINS_MODE"}]},` +
	`{"name":"LobbyTypeEnum","fields":[],"inputFields":[],"enumValues":[{"name":"RANKED"}]},` +
	`{"name":"SteamAccountType","fields":[{"name":"matches","args":[{"name":"request"}],"type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"MatchType"}}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"PlayerType","fields":[{"name":"matchPlayers","args":[{"name":"take"}],"type":{"kind":"OBJECT","name":"MatchType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"LeaderboardQuery","fields":[{"name":"season","args":[{"name":"request"}],"type":{"kind":"OBJECT","name":"LeaderboardType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"PlayerMatchesRequestType","fields":[],"inputFields":[{"name":"take"},{"name":"skip"},{"name":"orderBy"}],"enumValues":[]},` +
	`{"name":"PlayerPerformanceMatchesRequestType","fields":[],"inputFields":[{"name":"take"}],"enumValues":[]},` +
	`{"name":"PagePlayerQuery","fields":[],"inputFields":[{"name":"skip"}],"enumValues":[]},` +
	`{"name":"PageMatchesQuery","fields":[],"inputFields":[{"name":"take"}],"enumValues":[]},` +
	`{"name":"FilterSeasonLeaderboardRequestType","fields":[],"inputFields":[{"name":"leaderBoardDivision"}],"enumValues":[]},` +
	`{"name":"SteamAccountSeasonActiveLeaderboardType","fields":[` +
	`{"name":"rank","args":[],"type":{"kind":"SCALAR","name":"Int"}},` +
	`{"name":"players","args":[],"type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"SteamAccountSeasonActivePlayerType"}}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"SteamAccountSeasonActivePlayerType","fields":[{"name":"steamAccountId","args":[],"type":{"kind":"SCALAR","name":"Long"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"LeaderboardDivision","fields":[],"inputFields":[],"enumValues":[{"name":"EUROPE"},{"name":"AMERICAS"}]},` +
	`{"name":"FindMatchPlayerOrderBy","fields":[],"inputFields":[],"enumValues":[{"name":"DESC"}]},` +
	`{"name":"FindMatchPlayerList","fields":[],"inputFields":[],"enumValues":[{"name":"ALL"}]},` +
	`{"name":"RankBracket","fields":[],"inputFields":[],"enumValues":[{"name":"DIVINE"},{"name":"IMMORTAL"}]},` +
	`{"name":"RankBracketBasicEnum","fields":[],"inputFields":[{"name":"DIVINE_IMMORTAL"}],"enumValues":[]},` +
	`{"name":"StratzQuery","fields":[{"name":"matches","args":[],"type":{"kind":"OBJECT","name":"MatchType"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"LiveType","fields":[{"name":"id","args":[],"type":{"kind":"SCALAR","name":"Long"}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"LeagueType","fields":[` +
	`{"name":"id","args":[],"type":{"kind":"SCALAR","name":"Long"}},` +
	`{"name":"name","args":[],"type":{"kind":"SCALAR","name":"String"}},` +
	`{"name":"matches","args":[{"name":"request"}],"type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"MatchType"}}}],"inputFields":[],"enumValues":[]},` +
	`{"name":"LeagueMatchesRequestType","fields":[],"inputFields":[{"name":"take"},{"name":"skip"},{"name":"startDateTime"}],"enumValues":[]},` +
	`{"name":"LeagueRequestType","fields":[],"inputFields":[{"name":"take"}],"enumValues":[]}]}}`

// the schema reader: one snapshot request resolving the root args, the
// MatchType field set, and the mode/lobby enums; every failure shape is loud
func TestIntrospectMatchesAgainstStubServer(t *testing.T) {
	t.Run("walks every schema surface", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "__schema") {
				return dataReply(matchesIntroSnapshot)
			}
			return dataReply(`{}`)
		})
		sch, err := introspectMatches(c)
		if err != nil {
			t.Fatal(err)
		}
		if !sch.args["ids"] || !sch.args["take"] {
			t.Fatalf("root args = %v, want ids and take", sortedKeys(sch.args))
		}
		if !sch.fields["pickBans"] || !sch.fields["players"] {
			t.Fatal("MatchType field set not harvested")
		}
		if len(sch.enums["gameMode"]) == 0 || len(sch.enums["lobbyType"]) == 0 {
			t.Fatalf("enums = %v, want mode and lobby values", sch.enums)
		}
		if s.count() != 1 {
			t.Fatalf("introspection made %d requests, want 1", s.count())
		}
	})
	t.Run("transport failure", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		if _, err := introspectMatches(c); err == nil || !strings.Contains(err.Error(), "stratz 403") {
			t.Fatalf("err = %v, want the transport failure", err)
		}
	})
	t.Run("torn snapshot", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return okReply(`{"sch":{"queryType":{"na`)
		})
		if _, err := introspectMatches(c); err == nil || !strings.Contains(err.Error(), "unexpected end") {
			t.Fatalf("err = %v, want the decode failure", err)
		}
	})
	t.Run("no matches root args introspected", func(t *testing.T) {
		c, _ := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"sch":{"types":[]}}`)
		})
		_, err := introspectMatches(c)
		if err == nil || !strings.Contains(err.Error(), "no matches root field args introspected") {
			t.Fatalf("err = %v, want the empty-args failure", err)
		}
	})
}

// the root decoder behind the ladder and the by-ids round trip
func TestFetchRootMatchesAgainstStubServer(t *testing.T) {
	tests := []struct {
		name    string
		respond func(_ string, _ int, _ *http.Request) stubReply
		want    int
		wantErr string
	}{
		{"serves the matches list", func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"matches":[{"id":8},{"id":9}]}`)
		}, 2, ""},
		{"transport failure", func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		}, 0, "stratz 403"},
		{"graphql failure", func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("User is not an admin")
		}, 0, "stratz graphql"},
		{"torn body", func(_ string, _ int, _ *http.Request) stubReply {
			return okReply(`{"data":{"matches":[{`)
		}, 0, "unexpected end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := stubClient(t, tt.respond)
			rows, err := fetchRootMatches(c, "query { matches { id } }")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tt.want {
				t.Fatalf("rows = %d, want %d", len(rows), tt.want)
			}
		})
	}
}

// the acceptance ladder: a seeded browse tries every candidate argument, an
// empty browse skips the ids arm, and rejected arms are prints, never fatal
func TestDiscoverMatchesShapeAgainstStubServer(t *testing.T) {
	t.Run("browse answers and every arg is tried", func(t *testing.T) {
		c, s := stubClient(t, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"matches":[{"id":8}]}`)
		})
		rows := discoverMatchesShape(c, "id")
		if len(rows) != 1 {
			t.Fatalf("browse rows = %d, want the served sample", len(rows))
		}
		if s.count() != 6 {
			t.Fatalf("ladder made %d requests, want 6 (browse + 5 args)", s.count())
		}
		s.saw(t, "matches(ids: [8]", "seeded ids arm")
	})
	t.Run("empty browse skips the ids arm", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "matches {") {
				return dataReply(`{"matches":[]}`)
			}
			return dataReply(`{"matches":[{"id":8}]}`)
		})
		rows := discoverMatchesShape(c, "id")
		if len(rows) != 0 {
			t.Fatalf("browse rows = %d, want empty", len(rows))
		}
		if s.count() != 5 {
			t.Fatalf("ladder made %d requests, want 5 (browse + 4 args, ids skipped)", s.count())
		}
		if s.countMatching("ids:") != 0 {
			t.Fatal("the ids arm must be skipped without a seed")
		}
	})
	t.Run("rejected browse still ladders the args", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "matches {") {
				return stubReply{code: 403, body: "denied"}
			}
			return dataReply(`{"matches":[{"id":8}]}`)
		})
		if rows := discoverMatchesShape(c, "id"); rows != nil {
			t.Fatalf("rows = %d, want nil on a rejected browse", len(rows))
		}
		if s.count() != 5 {
			t.Fatalf("ladder made %d requests, want 5 (rejected browse + 4 args)", s.count())
		}
	})
	t.Run("rejected ladder arg is a print", func(t *testing.T) {
		c, s := stubClient(t, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "matches(limit: 5") {
				return stubReply{code: 403, body: "denied"}
			}
			return dataReply(`{"matches":[{"id":8}]}`)
		})
		discoverMatchesShape(c, "id")
		if s.countMatching("matches(limit: 5") != 1 {
			t.Fatal("the rejected limit arm must still have been tried")
		}
	})
}

// the decode-time neutralizers reject schema values of the wrong json type
// instead of half-decoding them
func TestIntroDecodeRejectsWrongTypes(t *testing.T) {
	var f introField
	if err := json.Unmarshal([]byte(`{"name":5}`), &f); err == nil {
		t.Error("numeric name accepted for introField")
	}
	var r introTypeRef
	if err := json.Unmarshal([]byte(`{"kind":1}`), &r); err == nil {
		t.Error("numeric kind accepted for introTypeRef")
	}
	var ff introFieldFull
	if err := json.Unmarshal([]byte(`{"name":[]}`), &ff); err == nil {
		t.Error("array name accepted for introFieldFull")
	}
	var it introType
	if err := json.Unmarshal([]byte(`{"name":{}}`), &it); err == nil {
		t.Error("object name accepted for introType")
	}
}

func TestIntroTypeRefDisplayName(t *testing.T) {
	var nilRef *introTypeRef
	if got := nilRef.displayName(); got != "?" {
		t.Fatalf("nil displayName = %q, want ?", got)
	}
	var wrapped introTypeRef
	if err := json.Unmarshal([]byte(`{"kind":"NON_NULL","ofType":{"kind":"LIST","ofType":{"kind":"OBJECT","name":"X"}}}`), &wrapped); err != nil {
		t.Fatal(err)
	}
	if got := wrapped.displayName(); got != "X" {
		t.Fatalf("wrapped displayName = %q, want the innermost name X", got)
	}
}

func TestIntersectSorted(t *testing.T) {
	m := map[string]bool{"take": true, "week": true, "skip": true}
	got := intersectSorted(m, []string{"take", "ids", "week"})
	if len(got) != 2 || got[0] != "take" || got[1] != "week" {
		t.Fatalf("intersect = %v, want [take week] in want order", got)
	}
	if got := intersectSorted(m, nil); got != nil {
		t.Fatalf("intersect against nothing = %v, want nil", got)
	}
}
