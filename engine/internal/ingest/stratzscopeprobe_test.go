package ingest

import (
	"fmt"
	"strings"
	"testing"
)

// the ladder builds one heroStats call per (field, args) pair so each filter
// arg gets its own accept/reject evidence with a row count
func TestScopeLadderQueryGolden(t *testing.T) {
	got := scopeLadderQuery("matchUp", "heroId: 1, take: 200, bracketBasicIds: [DIVINE_IMMORTAL]", "gameModeIds: [22]", "heroId vs { heroId2 }")
	want := "query { heroStats { matchUp(heroId: 1, take: 200, bracketBasicIds: [DIVINE_IMMORTAL], gameModeIds: [22]) { heroId vs { heroId2 } } } }"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := scopeLadderQuery("winWeek", "take: 200, bracketBasicIds: [DIVINE_IMMORTAL]", "", "heroId matchCount winCount"); got !=
		"query { heroStats { winWeek(take: 200, bracketBasicIds: [DIVINE_IMMORTAL]) { heroId matchCount winCount } } }" {
		t.Fatalf("bare aggregate: got %q", got)
	}
}

func TestCountHeroStatsRows(t *testing.T) {
	body := []byte(`{"data":{"heroStats":{"winWeek":[{"heroId":1},{"heroId":2}]}}}`)
	n, err := countHeroStatsRows(body)
	if err != nil || n != 2 {
		t.Fatalf("got %d %v want 2", n, err)
	}
	// graphql errors are rejections, not zero-row answers
	if _, err := countHeroStatsRows([]byte(`{"errors":[{"message":"Unknown argument"}]}`)); err == nil {
		t.Fatal("error envelope must not count as rows")
	}
	if n, err := countHeroStatsRows([]byte(`{"data":{"heroStats":{"winWeek":[]}}}`)); err != nil || n != 0 {
		t.Fatalf("empty list: got %d %v", n, err)
	}
}

func TestHeroStatsLadderRunsEveryStep(t *testing.T) {
	calls := 0
	fake := fakeScopeClient{fn: func(q string) ([]byte, error) {
		calls++
		if strings.Contains(q, "lobbyTypeIds") {
			return nil, fmt.Errorf("stratz graphql: Unknown argument 'lobbyTypeIds'")
		}
		return []byte(`{"data":{"heroStats":{"x":[{"a":1},{"a":2},{"a":3}]}}}`), nil
	}}
	attempts := heroStatsLadder(fake, "winWeek", "take: 200", "heroId", []string{"gameModeIds: [22]", "lobbyTypeIds: [7]"})
	if calls != 3 {
		t.Fatalf("base plus each extra must run: got %d calls", calls)
	}
	if attempts[0].Label != "base" || attempts[0].Rows != 3 || attempts[0].Err != nil {
		t.Fatalf("base attempt wrong: %+v", attempts[0])
	}
	if attempts[1].Label != "+gameModeIds: [22]" || attempts[1].Rows != 3 {
		t.Fatalf("accepted extra wrong: %+v", attempts[1])
	}
	if attempts[2].Err == nil || attempts[2].Rows != 0 {
		t.Fatalf("rejected extra must carry the error: %+v", attempts[2])
	}
}

// fake gqlFetcher for ladder tests; *stratzClient satisfies the same seam
type fakeScopeClient struct {
	fn func(q string) ([]byte, error)
}

func (f fakeScopeClient) query(q string) ([]byte, error) { return f.fn(q) }

func TestPopRowsByHeroFoldsBuckets(t *testing.T) {
	rows := []popRow{
		{HeroID: 1, MatchCount: 10, WinCount: 5},
		{HeroID: 1, MatchCount: 8, WinCount: 4},
		{HeroID: 2, MatchCount: 6, WinCount: 3},
	}
	by := popRowsByHero(rows)
	if len(by) != 2 || by[1].MatchCount != 18 || by[1].WinCount != 9 || by[2].MatchCount != 6 {
		t.Fatalf("fold wrong: %+v", by)
	}
}

func TestDiffPopRowsCountsDifferingHeroes(t *testing.T) {
	a := []popRow{{HeroID: 1, MatchCount: 100, WinCount: 50}, {HeroID: 2, MatchCount: 100, WinCount: 50}}
	b := []popRow{{HeroID: 1, MatchCount: 100, WinCount: 50}, {HeroID: 2, MatchCount: 90, WinCount: 50}}
	heroes, differing, gap, median := diffPopRows(a, b)
	// upper middle median: gaps [0, 0.1] -> 0.1
	if heroes != 2 || differing != 1 || gap < 0.099 || gap > 0.101 || median < 0.099 || median > 0.101 {
		t.Fatalf("got %d %d %.4f %.4f want 2 1 0.1 0.1", heroes, differing, gap, median)
	}
	if _, d, _, _ := diffPopRows(a, a); d != 0 {
		t.Fatalf("identical arms must not differ: %d", d)
	}
}
