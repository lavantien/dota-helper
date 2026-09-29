package ingest

import (
	"strings"
	"testing"
)

func TestBracketIdsFilterExpandsBasicEnum(t *testing.T) {
	cases := map[string]string{
		"DIVINE_IMMORTAL": "DIVINE, IMMORTAL",
		"LEGEND_ANCIENT":  "LEGEND, ANCIENT",
		"HERALD_GUARDIAN": "HERALD, GUARDIAN",
		"IMMORTAL":        "IMMORTAL",
		"UNCALIBRATED":    "UNCALIBRATED",
	}
	for in, want := range cases {
		if got := bracketIdsFilter(in); got != want {
			t.Errorf("bracketIdsFilter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBackfillFilterPayload(t *testing.T) {
	got := backfillFilterPayload("22", 1000, 2000)
	want := "gameModeIds: [22], lobbyTypeIds: [7], startDateTime: 1000, endDateTime: 2000"
	if got != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
}

func TestPlayerMatchesQueryShape(t *testing.T) {
	q := playerMatchesQuery(42, backfillFilterPayload("22", 1000, 2000), 100, 300)
	for _, want := range []string{
		"player(steamAccountId: 42)",
		"matches(request: { gameModeIds: [22]",
		"lobbyTypeIds: [7]",
		"startDateTime: 1000, endDateTime: 2000",
		"take: 100, skip: 300, orderBy: DESC",
		"id didRadiantWin durationSeconds",
		"pickBans { isPick heroId order isRadiant }",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
}

func TestLeaderboardSeasonQueryShape(t *testing.T) {
	q := leaderboardSeasonQuery("EUROPE")
	for _, want := range []string{
		"leaderboard { season(request: { leaderBoardDivision: EUROPE })",
		"playerCount players { steamAccountId }",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q: %s", want, q)
		}
	}
}

func TestMatchesByIdsQueryShape(t *testing.T) {
	q := matchesByIdsQuery([]int64{7, 8})
	if !strings.Contains(q, "matches(ids: [7, 8])") || !strings.Contains(q, "pickBans { isPick heroId order isRadiant }") {
		t.Errorf("by-ids query malformed: %s", q)
	}
}

func TestMeasureDraftCoverage(t *testing.T) {
	good := func() probeMatch {
		m := probeMatch{ID: 1}
		for i := 0; i < 5; i++ {
			m.PickBans = append(m.PickBans, probePickBan{HeroID: 100 + i, Order: i, IsRadiant: true, IsPick: true})
		}
		for i := 0; i < 5; i++ {
			m.PickBans = append(m.PickBans, probePickBan{HeroID: 200 + i, Order: 5 + i, IsRadiant: false, IsPick: true})
		}
		return m
	}
	nine := good()
	nine.PickBans = nine.PickBans[:9]
	unbalanced := good()
	unbalanced.PickBans[5].IsRadiant = true // 6 radiant picks
	dup := good()
	dup.PickBans[0].HeroID = dup.PickBans[1].HeroID
	bans := good()
	bans.PickBans = append([]probePickBan{{HeroID: 1, Order: -1, IsPick: false}}, bans.PickBans...)

	cov := measureDraftCoverage([]probeMatch{good(), nine, unbalanced, dup, bans, {ID: 2}})
	if cov.Rows != 6 || cov.Complete != 2 || cov.AnyDraft != 5 {
		t.Errorf("coverage = %+v, want 6 rows / 2 complete / 5 with any draft", cov)
	}
}
