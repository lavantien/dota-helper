package ingest

import (
	"fmt"
	"strings"
)

// Pinned, live-verified graphql shapes for the per-match draft backfill
// (evidence recorded by make probe-matches):
//   - the root matches field takes ids only; per-player match lists and
//     matches(ids:) are token-gated on this tier, so the accessible
//     per-match source is league matches, which carry full captains-mode
//     pickBans in the patch window
//   - request filters take numeric ids: gameModeIds [Byte] (2 = CAPTAINS_MODE,
//     22 = ALL_PICK_RANKED), lobbyTypeIds [Byte] (7 = RANKED), startDateTime
//     and endDateTime are single Longs
//   - take is capped server-side at 100

// matchFieldsSel is the pinned MatchType selection the crawler fetches. The
// players subfields (lane, position, role, roleBasic, verified present by
// introspection) ride along for role-conditioned metrics.
const matchFieldsSel = "id didRadiantWin durationSeconds startDateTime lobbyType gameMode averageRank bracket pickBans { isPick heroId order isRadiant } players { heroId isRadiant lane position role roleBasic }"

// matchesTakeMax is the server-side take cap on the paginated match requests
// (verified live: "You have surpassed the maximum take value of: 100"). API
// semantics, not a tunable: the backfill take in the config hub is clamped to
// it.
const matchesTakeMax = 100

// leaderboardDivisions are the pinned LeaderboardDivision enum values, probe
// order by player pool size.
var leaderboardDivisions = []string{"EUROPE", "AMERICAS", "SE_ASIA", "CHINA"}

// probeSampleMatchIDs pins known recent divine/immortal pub match ids for the
// by-ids verification (manual opendota publicMatches lookup, 2026-09-26;
// ranks 63, 61, 71). Historical matches stay served, so the pin ages well.
var probeSampleMatchIDs = []int64{9016183525, 9016183069, 9016177652}

// bracketIdsFilter expands a RankBracketBasicEnum value (scope.bracket) into
// the per-bracket RankBracket list: DIVINE_IMMORTAL -> DIVINE, IMMORTAL.
func bracketIdsFilter(basic string) string {
	return strings.ReplaceAll(basic, "_", ", ")
}

// backfillFilterPayload is the pinned PlayerMatchesRequestType filter body:
// numeric ids for modes and lobby, single-Long window. bracketIds is left out
// on purpose: its Int numbering is undocumented, rows carry bracket and
// averageRank so the acceptance gates enforce scope locally.
func backfillFilterPayload(gameModeIDs string, since, until int64) string {
	return fmt.Sprintf("gameModeIds: [%s], lobbyTypeIds: [7], startDateTime: %d, endDateTime: %d",
		gameModeIDs, since, until)
}

func playerMatchesQuery(steamID int64, payload string, take, skip int) string {
	if payload != "" {
		payload += ", "
	}
	return fmt.Sprintf("query { player(steamAccountId: %d) { matches(request: { %stake: %d, skip: %d, orderBy: DESC }) { %s } } }",
		steamID, payload, take, skip, matchFieldsSel)
}

func playerInfoQuery(steamID int64) string {
	return fmt.Sprintf("query { player(steamAccountId: %d) { matchCount lastMatchDate } }", steamID)
}

func leaderboardSeasonQuery(division string) string {
	return fmt.Sprintf("query { leaderboard { season(request: { leaderBoardDivision: %s }) { playerCount players { steamAccountId } } } }", division)
}

// leaguesPageQuery pages the leagues list (default order: lastMatchDate
// descending, verified live); skip is required by the server, and
// lastMatchDate lets the crawler cut the listing at the patch window.
func leaguesPageQuery(take, skip int) string {
	return fmt.Sprintf("query { leagues(request: { take: %d, skip: %d }) { id displayName lastMatchDate } }", take, skip)
}

// leagueMatchesQuery pages one league's matches; payload carries the window
// and any id filters (LeagueMatchesRequestType).
func leagueMatchesQuery(leagueID int64, payload string, take, skip int) string {
	if payload != "" {
		payload += ", "
	}
	return fmt.Sprintf("query { league(id: %d) { matches(request: { %stake: %d, skip: %d }) { %s } } }",
		leagueID, payload, take, skip, matchFieldsSel)
}

func matchesByIdsQuery(ids []int64) string {
	var parts []string
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	return fmt.Sprintf("query { matches(ids: [%s]) { %s } }", strings.Join(parts, ", "), matchFieldsSel)
}

// matchesWindowPayload is the league-matches window filter shared by the
// crawler and the pagination probe. gameModeIDs is the numeric filter from
// the config hub (empty string filters by window only).
func matchesWindowPayload(gameModeIDs string, since, until int64) string {
	mode := ""
	if gameModeIDs != "" {
		mode = fmt.Sprintf("gameModeIds: [%s], ", gameModeIDs)
	}
	return fmt.Sprintf("%sstartDateTime: %d, endDateTime: %d", mode, since, until)
}

// clampTake keeps configured take values inside the server cap.
func clampTake(take int) int {
	return min(take, matchesTakeMax)
}
