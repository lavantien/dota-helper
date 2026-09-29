package ingest

import (
	"fmt"
	"strings"
	"testing"

	"poolguide/internal/config"
)

func TestPositionQueryGolden(t *testing.T) {
	got := positionQuery(3, 200, "DIVINE, IMMORTAL", "ALL_PICK_RANKED")
	want := "query { heroStats { winDay(take: 200, bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED], positionIds: [POSITION_3]) { heroId matchCount winCount } } }"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// the crawl queries each position once, folds day buckets per hero, and
// orders rows by hero id so the cache is deterministic
func TestFetchPositionsFromFoldsAndSorts(t *testing.T) {
	var queries []string
	fake := fakeScopeClient{fn: func(q string) ([]byte, error) {
		queries = append(queries, q)
		return []byte(`{"data":{"heroStats":{"winDay":[
			{"heroId":7,"matchCount":10,"winCount":5},
			{"heroId":7,"matchCount":8,"winCount":4},
			{"heroId":2,"matchCount":6,"winCount":3}]}}}`), nil
	}}
	cfg := &config.Config{}
	cfg.Stratz.Take = 200
	cfg.Scope.Bracket = "DIVINE_IMMORTAL"
	cfg.Scope.GameMode = 22
	pf, err := fetchPositionsFrom(fake, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 5 {
		t.Fatalf("got %d queries, want one per position", len(queries))
	}
	for k := 1; k <= 5; k++ {
		if !strings.Contains(queries[k-1], fmt.Sprintf("positionIds: [POSITION_%d]", k)) {
			t.Fatalf("query %d missing position %d: %s", k, k, queries[k-1])
		}
	}
	if len(pf.Positions) != 5 || pf.Positions[0].Position != 1 {
		t.Fatalf("positions wrong: %+v", pf.Positions)
	}
	first := pf.Positions[0]
	if len(first.Rows) != 2 || first.Rows[0].HeroID != 2 || first.Rows[1].HeroID != 7 {
		t.Fatalf("rows not folded+sorted: %+v", first.Rows)
	}
	if first.Rows[1].MatchCount != 18 || first.Rows[1].WinCount != 9 {
		t.Fatalf("hero 7 fold wrong: %+v", first.Rows[1])
	}
	if pf.Field != "winDay" || pf.GameMode != 22 {
		t.Fatalf("echo fields wrong: %+v", pf)
	}
}
