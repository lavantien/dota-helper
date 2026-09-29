package ingest

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"poolguide/internal/config"
)

// windowArmClient builds a client around a caller-owned cfg so the arm tests
// can bend the scope hub, pointed at the stub wire.
func windowArmClient(t *testing.T, cfg *config.Config, respond func(string, int, *http.Request) stubReply) (*stratzClient, *stratzServer) {
	t.Helper()
	s := newStratzServer(t, respond)
	c := newStratzClient(cfg, "test-token")
	c.endpoint = s.url()
	return c, s
}

const armPopRows = `{"heroStats":{"winDay":[` +
	`{"heroId":1,"matchCount":7,"winCount":3},` +
	`{"heroId":8,"matchCount":9,"winCount":4}]}}`

// every measurement arm folds its wire answer into a windowArmStat or fails
// with the production wrap: wire failures land in the stat, scope-id failures
// abort before the wire
func TestWindowArmsAgainstStubServer(t *testing.T) {
	t.Run("winDayArm folds rows and volume", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		a := winDayArm(c, cfg, 7)
		if a.Err != nil || a.Rows != 2 || a.MatchSum != 16 || a.Label != "take 7" {
			t.Fatalf("arm = %+v, want 2 rows summing 16 at take 7", a)
		}
	})
	t.Run("winDayArm carries wire and scope failures", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		if a := winDayArm(c, cfg, 7); a.Err == nil || !strings.Contains(a.Err.Error(), "stratz 403") {
			t.Fatalf("transport failure arm = %+v", a)
		}
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("schema drifted")
		})
		if a := winDayArm(c2, cfg, 7); a.Err == nil || !strings.Contains(a.Err.Error(), "stratz graphql") {
			t.Fatalf("graphql failure arm = %+v", a)
		}
		bad := serverTestConfig(t.TempDir())
		bad.Scope.GameMode = 21
		c3, _ := windowArmClient(t, bad, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		if a := winDayArm(c3, bad, 7); a.Err == nil || !strings.Contains(a.Err.Error(), "no known enum token") {
			t.Fatalf("scope failure arm = %+v", a)
		}
	})
	t.Run("winDayHeroArm folds one hero only", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		a := winDayHeroArm(c, cfg, 1, 7)
		if a.Err != nil || a.Rows != 1 || a.MatchSum != 7 || a.Label != "hero 1 take 7" {
			t.Fatalf("hero arm = %+v, want hero 1 alone", a)
		}
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		if a := winDayHeroArm(c2, cfg, 1, 7); a.Err == nil {
			t.Fatal("transport failure must land in the hero arm")
		}
		c3, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("schema drifted")
		})
		if a := winDayHeroArm(c3, cfg, 1, 7); a.Err == nil || !strings.Contains(a.Err.Error(), "stratz graphql") {
			t.Fatalf("graphql failure arm = %+v", a)
		}
		bad := serverTestConfig(t.TempDir())
		bad.Scope.GameMode = 21
		c4, _ := windowArmClient(t, bad, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		if a := winDayHeroArm(c4, bad, 1, 7); a.Err == nil || !strings.Contains(a.Err.Error(), "no known enum token") {
			t.Fatalf("scope failure arm = %+v", a)
		}
	})
	t.Run("positionArm sums the farm-position slice", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, s := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		a, err := positionArm(c, cfg, 7)
		if err != nil || a.Err != nil || a.Rows != 2 || a.MatchSum != 16 {
			t.Fatalf("arm = %+v %v, want the folded slice", a, err)
		}
		s.saw(t, "positionIds: [POSITION_1]", "position arm")
	})
	t.Run("positionArm aborts on unmapped scope ids", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		cfg.Scope.GameMode = 21
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		if _, err := positionArm(c, cfg, 7); err == nil || !strings.Contains(err.Error(), "gameMode 21") {
			t.Fatalf("err = %v, want the gameMode failure", err)
		}
		cfg.Scope.GameMode = 22
		cfg.Scope.Bracket = "HERALD"
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(armPopRows)
		})
		if _, err := positionArm(c2, cfg, 7); err == nil || !strings.Contains(err.Error(), "HERALD") {
			t.Fatalf("err = %v, want the bracket failure", err)
		}
	})
	t.Run("positionArm carries wire failures in the stat", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		a, err := positionArm(c, cfg, 7)
		if err != nil || a.Err == nil || !strings.Contains(a.Err.Error(), "stratz 403") {
			t.Fatalf("arm = %+v %v, want the failure in the stat", a, err)
		}
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("schema drifted")
		})
		a, err = positionArm(c2, cfg, 7)
		if err != nil || a.Err == nil || !strings.Contains(a.Err.Error(), "stratz graphql") {
			t.Fatalf("arm = %+v %v, want the graphql failure in the stat", a, err)
		}
	})
	t.Run("matchUpArm folds both sides with the cell map", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(matchUpEnvelope(1, windowVsRows(2, 500), windowVsRows(1, 300)))
		})
		vs, with, cells := matchUpArm(c, cfg, 1, 200)
		if vs.Err != nil || vs.Rows != 2 || vs.MatchSum != 1000 {
			t.Fatalf("vs = %+v, want 2 rows summing 1000", vs)
		}
		if with.Rows != 1 || with.MatchSum != 300 {
			t.Fatalf("with = %+v, want 1 row summing 300", with)
		}
		if cells[100] != 500 || cells[101] != 500 || len(cells) != 2 {
			t.Fatalf("cells = %v, want the per-enemy vs volumes", cells)
		}
	})
	t.Run("matchUpArm carries wire failures to both sides", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		vs, with, cells := matchUpArm(c, cfg, 1, 200)
		if vs.Err == nil || with.Err == nil || cells != nil {
			t.Fatalf("arms = %+v %+v %v, want the failure everywhere", vs, with, cells)
		}
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"heroStats":{"matchUp":[]}}`)
		})
		vs, _, _ = matchUpArm(c2, cfg, 1, 200)
		if vs.Err == nil || !strings.Contains(vs.Err.Error(), "returned 0 results") {
			t.Fatalf("zero-result arm = %+v", vs)
		}
	})
	t.Run("matchUpWeekArm pins the week stamp", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, s := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(matchUpEnvelope(1, windowVsRows(2, 500), ""))
		})
		a, cells := matchUpWeekArm(c, cfg, 1, 1789948800)
		if a.Err != nil || a.Rows != 2 || a.MatchSum != 1000 || a.Label != "week-start 1789948800" {
			t.Fatalf("week arm = %+v, want the pinned pair volume", a)
		}
		if len(cells) != 2 {
			t.Fatalf("cells = %v, want the per-enemy map", cells)
		}
		s.saw(t, "week: 1789948800", "week arm")
	})
	t.Run("matchUpWeekArm carries wire failures", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		a, cells := matchUpWeekArm(c, cfg, 1, 1789948800)
		if a.Err == nil || cells != nil || !strings.Contains(a.Err.Error(), "stratz 403") {
			t.Fatalf("week arm = %+v %v, want the transport failure", a, cells)
		}
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return errReply("week rejected")
		})
		a, cells = matchUpWeekArm(c2, cfg, 1, 1789948800)
		if a.Err == nil || cells != nil || !strings.Contains(a.Err.Error(), "stratz graphql") {
			t.Fatalf("week arm = %+v %v, want the graphql failure", a, cells)
		}
	})
	t.Run("buildsArmAt folds rows, volume, and the max cell", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, s := windowArmClient(t, cfg, func(q string, _ int, _ *http.Request) stubReply {
			if strings.Contains(q, "minTime: 0, maxTime: 10") {
				return dataReply(windowBuildsMinute)
			}
			return dataReply(windowBuildsBaseline)
		})
		a := buildsArmAt(c, cfg, 1, "", "product")
		if a.Err != nil || a.Rows != 4 || a.MatchSum != 400 || a.MaxCell != 150 {
			t.Fatalf("builds arm = %+v, want 4 rows summing 400 peaking at 150", a)
		}
		minute := buildsArmAt(c, cfg, 1, "minTime: 0, maxTime: 10", "minute filter")
		if minute.Err != nil || minute.Rows != 2 {
			t.Fatalf("minute arm = %+v, want the narrower slice", minute)
		}
		s.saw(t, "matchLimit: 1, minTime: 0, maxTime: 10", "minute arm args")
	})
	t.Run("buildsArmAt carries wire failures and shape drift", func(t *testing.T) {
		cfg := serverTestConfig(t.TempDir())
		c, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return stubReply{code: 403, body: "denied"}
		})
		if a := buildsArmAt(c, cfg, 1, "", "product"); a.Err == nil {
			t.Fatal("transport failure must land in the builds arm")
		}
		c2, _ := windowArmClient(t, cfg, func(_ string, _ int, _ *http.Request) stubReply {
			return dataReply(`{"heroStats":{"itemFullPurchase":{"rows":[]}}}`)
		})
		a := buildsArmAt(c2, cfg, 1, "", "product")
		if a.Err == nil || !strings.Contains(a.Err.Error(), "no parseable purchase list") {
			t.Fatalf("shape drift arm = %+v", a)
		}
	})
}

// the pure folding and printing helpers at the edges the walks cannot reach
func TestWindowPureHelpers(t *testing.T) {
	if n, sum := sumPopRows(nil); n != 0 || sum != 0 {
		t.Fatalf("sumPopRows(nil) = %d %d", n, sum)
	}
	rows := []popRow{{HeroID: 1, MatchCount: 3}, {HeroID: 1, MatchCount: 4}}
	if n, sum := sumPopRows(rows); n != 2 || sum != 7 {
		t.Fatalf("sumPopRows = %d %d, want 2 rows summing 7", n, sum)
	}
	if prefixComma("") != "" || prefixComma("minTime: 0") != ", minTime: 0" {
		t.Fatal("prefixComma must render the graphql arg separator only for args")
	}
	if verdictWord(true) != "HONORED" || verdictWord(false) != "REFUSED" {
		t.Fatal("verdictWord must name both verdicts")
	}
	for _, n := range []string{"Int", "Long", "Int!", "Long!"} {
		if !isNumericTypeName(n) {
			t.Fatalf("isNumericTypeName(%q) must hold", n)
		}
	}
	for _, n := range []string{"", "String", "Week"} {
		if isNumericTypeName(n) {
			t.Fatalf("isNumericTypeName(%q) must not hold", n)
		}
	}
}

// the week-verdict refusal arms the walks cannot stage: an errored week arm,
// an empty week arm, a missing calibration volume
func TestCalendarWeekVerdictRefusalEdges(t *testing.T) {
	base := windowArmStat{Rows: 126, MatchSum: 21_905}
	calib := windowArmStat{Rows: 7, MatchSum: 3_639}
	if ok, _ := calendarWeekVerdict(windowArmStat{Err: errors.New("boom")}, base, calib, 5, 100); ok {
		t.Fatal("an errored week arm must refuse")
	}
	if ok, _ := calendarWeekVerdict(windowArmStat{}, base, calib, 5, 100); ok {
		t.Fatal("an empty week arm must refuse")
	}
	overFloor := windowArmStat{Rows: 126, MatchSum: 32_585}
	if ok, _ := calendarWeekVerdict(overFloor, base, windowArmStat{}, 5, 100); ok {
		t.Fatal("a missing calibration volume must refuse the band check")
	}
}

// argTypeNames reads the schema snapshot's HeroStatsQuery arg types and stays
// empty without a snapshot or without the type
func TestArgTypeNames(t *testing.T) {
	if got := argTypeNames(nil, "itemFullPurchase"); len(got) != 0 {
		t.Fatalf("nil snapshot args = %v, want none", got)
	}
	var env introEnvelope
	if err := json.Unmarshal([]byte(`{"sch":{"types":[{"name":"Other","fields":[]}]}}`), &env); err != nil {
		t.Fatal(err)
	}
	if got := argTypeNames(&env, "itemFullPurchase"); len(got) != 0 {
		t.Fatalf("args without the type = %v, want none", got)
	}
	var rich introEnvelope
	if err := json.Unmarshal([]byte(scopeIntroSnapshot), &rich); err != nil {
		t.Fatal(err)
	}
	types := argTypeNames(&rich, "itemFullPurchase")
	if types["minTime"] != "Int" || types["maxTime"] != "Int" || types["week"] != "Long" {
		t.Fatalf("arg types = %v, want Int/Int/Long on the builds window args", types)
	}
	if got := argTypeNames(&rich, "matchUp"); got["take"] != "Long" || got["heroId"] != "" {
		t.Fatalf("matchUp arg types = %v, want the typed take and no untyped heroId", got)
	}
}
