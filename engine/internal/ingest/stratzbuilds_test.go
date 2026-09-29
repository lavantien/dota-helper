package ingest

import (
	"strings"
	"testing"

	"poolguide/internal/config"
)

func TestParseItemsSortsByIDAndKeepsRecipes(t *testing.T) {
	items, err := parseItems(readFixture(t, "stratz_builds_items.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}
	wantIDs := []int{1, 35, 36, 63, 116, 147}
	for i, id := range wantIDs {
		if items[i].ID != id {
			t.Errorf("items[%d].id = %d, want %d", i, items[i].ID, id)
		}
	}
	var rec *itemConstant
	for i := range items {
		if items[i].ShortName == "recipe_magic_wand" {
			rec = &items[i]
		}
	}
	if rec == nil {
		t.Fatal("recipe item dropped, derive needs it for component suppression")
	}
	if len(rec.Components) != 3 || rec.Components[0].ComponentID != 34 {
		t.Errorf("recipe components = %+v", rec.Components)
	}
}

func TestParsePurchases(t *testing.T) {
	rows, err := parsePurchases(readFixture(t, "stratz_builds_purchase.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows parsed")
	}
	sawInstance := false
	for _, r := range rows {
		if r.ItemID <= 0 || r.MatchCount < 0 {
			t.Errorf("bad row %+v", r)
		}
		if r.Instance != 0 {
			sawInstance = true
		}
	}
	if !sawInstance {
		t.Error("fixture lost the instance rows, topPurchases filtering is untested")
	}
}

func TestTopPurchasesInstanceZeroOnly(t *testing.T) {
	rows := []itemPurchaseRow{
		{ItemID: 10, Instance: 0, Time: 5, MatchCount: 100},
		{ItemID: 10, Instance: 0, Time: 9, MatchCount: 50},
		{ItemID: 11, Instance: 0, Time: 2, MatchCount: 120},
		{ItemID: 10, Instance: 1, Time: 12, MatchCount: 999},
	}
	top := topPurchases(rows, 2)
	if len(top) != 2 {
		t.Fatalf("top = %d entries, want 2", len(top))
	}
	if top[0].ItemID != 10 || top[0].Count != 150 {
		t.Errorf("top[0] = %+v, want item 10 n=150 (minutes summed, instance 1 excluded)", top[0])
	}
	if top[1].ItemID != 11 || top[1].Count != 120 {
		t.Errorf("top[1] = %+v, want item 11 n=120", top[1])
	}
}

func TestTopPurchasesTieBreakByItemID(t *testing.T) {
	top := topPurchases([]itemPurchaseRow{
		{ItemID: 9, Instance: 0, MatchCount: 70},
		{ItemID: 4, Instance: 0, MatchCount: 70},
	}, 2)
	if top[0].ItemID != 4 || top[1].ItemID != 9 {
		t.Errorf("ties must break by item id ascending, got %+v", top)
	}
}

func TestPatchSeries(t *testing.T) {
	tests := []struct{ in, want string }{
		{"7.41f", "7.41"},
		{"7.41", "7.41"},
		{"8.0ab", "8.0"},
	}
	for _, tt := range tests {
		if got := config.PatchSeries(tt.in); got != tt.want {
			t.Errorf("PatchSeries(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParsePurchasesRejectsUnknownShape(t *testing.T) {
	if _, err := parsePurchases([]byte(`{"data":{"heroStats":{"itemFullPurchase":{"rows":[]}}}}`)); err == nil {
		t.Error("unparsable heroStats accepted, API shape drift would read as empty data")
	}
	rows, err := parsePurchases([]byte(`{"data":{"heroStats":{"itemFullPurchase":[]}}}`))
	if err != nil || len(rows) != 0 {
		t.Errorf("legit empty list: rows=%v err=%v, want empty and no error", rows, err)
	}
}

func TestBuildsCacheUsable(t *testing.T) {
	const week = int64(1789948800)
	if buildsCacheUsable(nil, "7.41", "DIVINE_IMMORTAL", week) {
		t.Error("nil cache usable")
	}
	if buildsCacheUsable(&buildsCache{Series: "7.40", Rows: []itemPurchaseRow{{}}}, "7.41", "DIVINE_IMMORTAL", week) {
		t.Error("cross-series cache usable, stale patches would resume into a fresh manifest")
	}
	if buildsCacheUsable(&buildsCache{Series: "7.41"}, "7.41", "DIVINE_IMMORTAL", week) {
		t.Error("zero-row cache usable, empty results would cache forever")
	}
	if buildsCacheUsable(&buildsCache{Series: "7.41", Bracket: "IMMORTAL", Rows: []itemPurchaseRow{{}}}, "7.41", "DIVINE_IMMORTAL", week) {
		t.Error("cross-bracket cache usable, a scope change would resume stale-bracket dumps under the same series")
	}
	if buildsCacheUsable(&buildsCache{Series: "7.41", Bracket: "DIVINE_IMMORTAL", Rows: []itemPurchaseRow{{}}}, "7.41", "DIVINE_IMMORTAL", week) {
		t.Error("unstamped or wrong-week cache usable, a week rollover would resume stale-window dumps")
	}
	if !buildsCacheUsable(&buildsCache{Series: "7.41", Bracket: "DIVINE_IMMORTAL", Week: week, Rows: []itemPurchaseRow{{}}}, "7.41", "DIVINE_IMMORTAL", week) {
		t.Error("healthy cache rejected")
	}
}

func TestMaxFetchedAt(t *testing.T) {
	got := maxFetchedAt([]buildsEntry{
		{FetchedAt: "2026-09-24T10:00:00Z"},
		{FetchedAt: "2026-09-25T09:00:00Z"},
		{FetchedAt: "not-a-time"},
	})
	if got != "2026-09-25T09:00:00Z" {
		t.Errorf("maxFetchedAt = %q, want the newest entry stamp", got)
	}
	if maxFetchedAt(nil) != "" {
		t.Error("empty entries must yield an empty stamp")
	}
}

func TestBuildsPurchaseQueryShape(t *testing.T) {
	q := buildsPurchaseQuery(12, "DIVINE_IMMORTAL", "POSITION_1", 1789948800)
	for _, want := range []string{
		"heroId: 12", "bracketBasicIds: [DIVINE_IMMORTAL]",
		"positionIds: [POSITION_1]", "matchLimit: 1", "week: 1789948800",
		"itemId time instance matchCount winCount",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}
	if strings.Contains(buildsPurchaseQuery(12, "DIVINE_IMMORTAL", "POSITION_1", 0), "week") {
		t.Error("week 0 must omit the window arg for the probe evidence arms")
	}
}
