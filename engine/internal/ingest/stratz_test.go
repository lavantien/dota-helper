package ingest

import (
	"os"
	"strings"
	"testing"

	"poolguide/internal/config"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mkRows(n int, mc int64, syn float64) []matchupRow {
	rows := make([]matchupRow, n)
	for i := range rows {
		rows[i] = matchupRow{HeroID2: i + 1, MatchCount: mc, WinCount: mc / 2, Synergy: syn}
	}
	return rows
}

// the scoped aggregate query is the product's popularity and overall-count
// source: winDay under the bracket and mode filters the scope hub pins
func TestScopedAggregateQueryGolden(t *testing.T) {
	c := &config.Config{}
	c.Stratz.Take = 7
	c.Scope.Bracket = "DIVINE_IMMORTAL"
	c.Scope.GameMode = 22
	want := "query { heroStats { winDay(take: 7, bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]) { heroId matchCount winCount } } }"
	got, err := aggregateQuery(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// the pair query pins the completed calendar week and pins pagination above
// the enemy roster; week 0 omits the arg for the probe evidence arms
func TestMatchUpQueryGolden(t *testing.T) {
	want := "query { heroStats { matchUp(heroId: 67, take: 200, bracketBasicIds: [DIVINE_IMMORTAL], week: 1789948800) { heroId vs { heroId2 matchCount winCount synergy } with { heroId2 matchCount winCount synergy } } } }"
	if got := matchUpQuery(67, matchUpRowCap, "DIVINE_IMMORTAL", 1789948800); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	bare := matchUpQuery(67, matchUpRowCap, "DIVINE_IMMORTAL", 0)
	if strings.Contains(bare, "week") {
		t.Fatalf("week 0 must omit the window arg, got %s", bare)
	}
}

// a cache is resumable only when it crawled the week currently pinned:
// legacy unstamped caches and older weeks both force a refetch
func TestCacheWindowFresh(t *testing.T) {
	const pin = int64(1789948800)
	if cacheWindowFresh(nil, pin) {
		t.Fatal("nil cache is never fresh")
	}
	if cacheWindowFresh(&rawCache{}, pin) {
		t.Fatal("unstamped legacy cache must refetch")
	}
	if cacheWindowFresh(&rawCache{Week: pin - 7*86400}, pin) {
		t.Fatal("previous-week cache must refetch")
	}
	if !cacheWindowFresh(&rawCache{Week: pin}, pin) {
		t.Fatal("current-week cache must be resumable")
	}
}

// an unmapped scope id must never render an empty filter list: the api reads
// gameModeIds: [] as no filter, the exact silent unscoped crawl this guard kills
func TestScopedAggregateQueryRejectsUnknownScope(t *testing.T) {
	c := &config.Config{}
	c.Stratz.Take = 200
	c.Scope.Bracket = "DIVINE_IMMORTAL"
	c.Scope.GameMode = 21
	if _, err := aggregateQuery(c); err == nil || !strings.Contains(err.Error(), "gameMode") {
		t.Fatalf("unmapped gameMode must fail loudly, got %v", err)
	}
	c.Scope.GameMode = 22
	c.Scope.Bracket = "HERALD"
	if _, err := aggregateQuery(c); err == nil || !strings.Contains(err.Error(), "bracket") {
		t.Fatalf("unmapped bracket must fail loudly, got %v", err)
	}
}

func TestScopeEnumMappings(t *testing.T) {
	if tok, ok := gameModeEnumToken(22); !ok || tok != "ALL_PICK_RANKED" {
		t.Fatalf("gameMode 22 = %q %v, want ALL_PICK_RANKED", tok, ok)
	}
	if _, ok := gameModeEnumToken(21); ok {
		t.Fatal("unmapped gameMode must not resolve")
	}
	if ids, ok := winBracketIds("DIVINE_IMMORTAL"); !ok || ids != "DIVINE, IMMORTAL" {
		t.Fatalf("DIVINE_IMMORTAL = %q %v, want DIVINE, IMMORTAL", ids, ok)
	}
	if _, ok := winBracketIds("HERALD"); ok {
		t.Fatal("unmapped bracket must not resolve")
	}
}

func TestParseMatchUpSkipsZeroAndSelfRows(t *testing.T) {
	mu, err := parseMatchUp(readFixture(t, "stratz_matchup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mu.HeroID != 8 {
		t.Errorf("heroId = %d, want 8", mu.HeroID)
	}
	if len(mu.Vs) != 2 {
		t.Fatalf("vs rows = %d, want 2 (zero sentinel and self rows dropped)", len(mu.Vs))
	}
	if mu.Vs[0].HeroID2 != 1 || mu.Vs[0].MatchCount != 61234 || mu.Vs[0].Synergy != -0.42 {
		t.Errorf("vs[0] = %+v", mu.Vs[0])
	}
	if mu.Vs[1].HeroID2 != 14 || mu.Vs[1].MatchCount != 5120 {
		t.Errorf("vs[1] = %+v", mu.Vs[1])
	}
	if len(mu.With) != 2 {
		t.Fatalf("with rows = %d, want 2", len(mu.With))
	}
	if mu.With[0].HeroID2 != 2 || mu.With[0].WinCount != 4920 {
		t.Errorf("with[0] = %+v", mu.With[0])
	}
}

func TestParseRosterSortsByID(t *testing.T) {
	heroes, err := parseRoster(readFixture(t, "stratz_roster.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Aliases: map[string]string{"antimage": "anti-mage"}}
	wantIDs := []int{1, 8, 14}
	wantSlugs := []string{"anti-mage", "dark-seer", "pudge"}
	for i, h := range heroes {
		if h.ID != wantIDs[i] {
			t.Errorf("heroes[%d].id = %d, want %d", i, h.ID, wantIDs[i])
		}
		if got := cfg.SlugFromNPC(h.ShortName); got != wantSlugs[i] {
			t.Errorf("slug(%s) = %s, want %s", h.ShortName, got, wantSlugs[i])
		}
	}
}

func TestBuildRosterFileRejectsHostileShortNames(t *testing.T) {
	cfg := &config.Config{Aliases: map[string]string{"antimage": "anti-mage"}}
	heroes := []rosterHero{
		{ID: 1, Name: "npc_dota_hero_antimage", DisplayName: "Anti-Mage", ShortName: "antimage"},
		{ID: 2, Name: "npc_dota_hero_axe", DisplayName: "Axe", ShortName: "axe"},
	}
	for _, hostile := range []string{"../../picker/gates", "<img src=x onerror=alert(1)>", "a b", ""} {
		heroes[1].ShortName = hostile
		_, err := buildRosterFile(cfg, heroes)
		if err == nil || !strings.Contains(err.Error(), "invalid slug") {
			t.Errorf("shortName %q must fail loudly, got %v", hostile, err)
		}
	}
	heroes[1].ShortName = "axe"
	rf, err := buildRosterFile(cfg, heroes)
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.Heroes) != 2 || rf.Heroes[0].Slug != "anti-mage" || rf.Heroes[1].Slug != "axe" {
		t.Fatalf("roster entries = %+v", rf.Heroes)
	}
	if rf.Heroes[1].Name != "Axe" || rf.Heroes[1].Npc != "npc_dota_hero_axe" {
		t.Fatalf("roster entry fields = %+v", rf.Heroes[1])
	}
}

func TestAcceptanceGates(t *testing.T) {
	cfg := &config.Config{Stratz: config.StratzCfg{MinWithRows: 120, MinVsRows: 120, MaxAbsSynergy: 20}}
	tests := []struct {
		name    string
		vs      []matchupRow
		with    []matchupRow
		wantOK  bool
		wantSub string
	}{
		{"ok", mkRows(130, 100, 5), mkRows(125, 100, 5), true, ""},
		{"too few with rows", mkRows(130, 100, 5), mkRows(119, 100, 5), false, "with rows"},
		{"too few vs rows", mkRows(119, 100, 5), mkRows(125, 100, 5), false, "vs rows"},
		{"zero match count", mkRows(130, 0, 5), mkRows(125, 100, 5), false, "matchCount"},
		{"synergy at cap", mkRows(130, 100, 20), mkRows(125, 100, 5), false, "|synergy|"},
		{"synergy below floor", mkRows(130, 100, -20.5), mkRows(125, 100, 5), false, "|synergy|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkGates(&rawCache{Slug: "x", Vs: tt.vs, With: tt.with}, cfg)
			if got.OK != tt.wantOK {
				t.Fatalf("ok = %v, want %v (%v)", got.OK, tt.wantOK, got.Reasons)
			}
			if tt.wantSub != "" {
				if len(got.Reasons) == 0 {
					t.Fatalf("reasons empty, want one containing %q", tt.wantSub)
				}
				if !strings.Contains(got.Reasons[0], tt.wantSub) {
					t.Errorf("reason %q, want substring %q", got.Reasons[0], tt.wantSub)
				}
			}
		})
	}
}
