package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"poolguide/internal/config"
)

// Per-match draft backfill probe. docs.stratz.com is Cloudflare-gated and
// named introspection serves null, so every shape was verified live and is
// pinned in stratzmatchesquery.go. The probe prints the evidence on every
// run: introspected schema, arg acceptance table, constants enum-id
// cross-check, per-player and league browse behavior with draft coverage per
// game mode, pagination, and the by-ids/admin walls.

// probeSeedCandidates bounds the seed walk; a probe knob, not a data tunable.
const probeSeedCandidates = 5

// probeConstantsQuery cross-checks the numeric ids in the config hub against
// the enum names the graphql filters take.
const probeConstantsQuery = "query { constants { gameModes { id name } lobbyTypes { id name } } }"

type probePickBan struct {
	IsPick    bool `json:"isPick"`
	HeroID    int  `json:"heroId"`
	Order     int  `json:"order"`
	IsRadiant bool `json:"isRadiant"`
}

// probePlayerRow captures lane/role fields verbatim: they are enums and the
// wire encoding (string vs int) is not part of the pin.
type probePlayerRow struct {
	HeroID    int             `json:"heroId"`
	IsRadiant bool            `json:"isRadiant"`
	Lane      json.RawMessage `json:"lane"`
	Position  json.RawMessage `json:"position"`
	Role      json.RawMessage `json:"role"`
	RoleBasic json.RawMessage `json:"roleBasic"`
}

// probeMatch carries raw mode/lobby tokens so the evidence print shows
// exactly what the API served.
type probeMatch struct {
	ID            int64             `json:"id"`
	StartDateTime int64             `json:"startDateTime"`
	Duration      int               `json:"durationSeconds"`
	DidRadiantWin *bool             `json:"didRadiantWin"`
	LobbyType     json.RawMessage   `json:"lobbyType"`
	GameMode      json.RawMessage   `json:"gameMode"`
	AverageRank   int               `json:"averageRank"`
	Bracket       int               `json:"bracket"`
	PickBans      []probePickBan    `json:"pickBans"`
	Players       []probePlayerRow  `json:"players"`
}

type playerMatchesData struct {
	Player struct {
		Matches []probeMatch `json:"matches"`
	} `json:"player"`
}

type seasonData struct {
	Leaderboard struct {
		Season struct {
			PlayerCount int `json:"playerCount"`
			Players     []struct {
				SteamAccountID int64 `json:"steamAccountId"`
			} `json:"players"`
		} `json:"season"`
	} `json:"leaderboard"`
}

type draftCoverage struct{ Rows, AnyDraft, Complete int }

// measureDraftCoverage counts rows carrying any pickBans and rows with a
// complete 10-pick draft: 5 picks per side, unique hero ids across the picks.
func measureDraftCoverage(rows []probeMatch) draftCoverage {
	var c draftCoverage
	for _, m := range rows {
		c.Rows++
		if len(m.PickBans) == 0 {
			continue
		}
		c.AnyDraft++
		var picks, radiant int
		heroes := map[int]bool{}
		ok := true
		for _, pb := range m.PickBans {
			if !pb.IsPick {
				continue
			}
			picks++
			if pb.IsRadiant {
				radiant++
			}
			if heroes[pb.HeroID] {
				ok = false
			}
			heroes[pb.HeroID] = true
		}
		if picks == 10 && radiant == 5 && ok {
			c.Complete++
		}
	}
	return c
}

func fetchPlayersMatches(c *stratzClient, q string) ([]probeMatch, error) {
	body, err := c.query(q)
	if err != nil {
		return nil, err
	}
	var d struct {
		Players []struct {
			Matches []probeMatch `json:"matches"`
		} `json:"players"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	if len(d.Players) == 0 {
		return nil, nil
	}
	return d.Players[0].Matches, nil
}

func fetchPlayerMatches(c *stratzClient, q string) ([]probeMatch, error) {
	body, err := c.query(q)
	if err != nil {
		return nil, err
	}
	var d playerMatchesData
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	return d.Player.Matches, nil
}

func fetchSeasonPlayers(c *stratzClient, division string) (int, []int64, error) {
	body, err := c.query(leaderboardSeasonQuery(division))
	if err != nil {
		return 0, nil, err
	}
	var d seasonData
	if err := decodeEnvelope(body, &d); err != nil {
		return 0, nil, err
	}
	ids := make([]int64, 0, len(d.Leaderboard.Season.Players))
	for _, p := range d.Leaderboard.Season.Players {
		ids = append(ids, p.SteamAccountID)
	}
	return d.Leaderboard.Season.PlayerCount, ids, nil
}

// probeLeagueListing compares listing-filter variants so the crawler can
// skip dead leagues instead of spending a request per empty one.
func probeLeagueListing(c *stratzClient, since, until int64) {
	for _, v := range []struct {
		label  string
		filter string
	}{
		{"unfiltered", ""},
		{"leagueEnded false", ", leagueEnded: false"},
		{"isFutureLeague false", ", isFutureLeague: false"},
		{"betweenStartDateTime", fmt.Sprintf(", betweenStartDateTime: %d", since)},
		{"betweenEndDateTime", fmt.Sprintf(", betweenEndDateTime: %d", since)},
		{"between pair", fmt.Sprintf(", betweenStartDateTime: %d, betweenEndDateTime: %d", since, until)},
		{"endDateTime", fmt.Sprintf(", endDateTime: %d", since)},
		{"startDateTime", fmt.Sprintf(", startDateTime: %d", since)},
	} {
		q := fmt.Sprintf("query { leagues(request: { take: 3, skip: 0%s }) { id displayName lastMatchDate } }", v.filter)
		body, err := c.query(q)
		if err != nil {
			fmt.Printf("probe matches league-listing %q: REJECTED %s\n", v.label, shortErr(err))
			continue
		}
		var d struct {
			Leagues []struct {
				ID            int64  `json:"id"`
				DisplayName   string `json:"displayName"`
				LastMatchDate int64  `json:"lastMatchDate"`
			} `json:"leagues"`
		}
		if err := decodeEnvelope(body, &d); err != nil {
			fmt.Printf("probe matches league-listing %q: %v\n", v.label, err)
			continue
		}
		var parts []string
		for _, lg := range d.Leagues {
			label := lg.DisplayName
			if len(label) > 24 {
				label = label[:24]
			}
			parts = append(parts, fmt.Sprintf("%d %q last %d", lg.ID, label, lg.LastMatchDate))
		}
		fmt.Printf("probe matches league-listing %q: %s\n", v.label, strings.Join(parts, " | "))
	}
}

// probeLeagueMatches checks whether league (pro) match rows with drafts are
// served to a default token: the accessible per-match scope, since pub match
// lists are token-gated (player.matches serves empty, matches(ids:) answers
// "User is not an admin").
func probeLeagueMatches(c *stratzClient, since, until int64) {
	q := fmt.Sprintf("query { leagues(request: { take: 5, skip: 0 }) { id name displayName matches(request: { take: 100, skip: 0, startDateTime: %d, endDateTime: %d }) { %s } } }",
		since, until, matchFieldsSel)
	body, err := c.query(q)
	if err != nil {
		fmt.Printf("probe matches leagues: REJECTED %s\n", shortErr(err))
		return
	}
	var d struct {
		Leagues []struct {
			ID          int64         `json:"id"`
			Name        string        `json:"name"`
			DisplayName string        `json:"displayName"`
			Matches     []probeMatch  `json:"matches"`
		} `json:"leagues"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		fmt.Printf("probe matches leagues: %v\n", err)
		return
	}
	for _, lg := range d.Leagues {
		cov := measureDraftCoverage(lg.Matches)
		label := lg.DisplayName
		if label == "" {
			label = lg.Name
		}
		var modes, lobbies string
		if len(lg.Matches) > 0 {
			modes, lobbies = string(lg.Matches[0].GameMode), string(lg.Matches[0].LobbyType)
		}
		fmt.Printf("probe matches leagues: league %d %q served %d in-window matches, %d complete drafts, sample gameMode=%s lobbyType=%s\n",
			lg.ID, label, cov.Rows, cov.Complete, modes, lobbies)
	}
	one, err := fetchRootMatches(c, fmt.Sprintf("query { match(id: %d) { id gameMode lobbyType pickBans { isPick heroId order isRadiant } } }", probeSampleMatchIDs[0]))
	if err != nil {
		fmt.Printf("probe matches singular match(id): %v\n", err)
		return
	}
	fmt.Printf("probe matches singular match(id): served %d rows\n", len(one))
}

// probePlayerMatchesShape runs the request-shape ladder on the per-player
// browse: the first variant that serves rows is the pinned shape.
func probePlayerMatchesShape(c *stratzClient, seed int64) {
	for _, v := range []struct {
		label string
		q     string
	}{
		{"no request arg", fmt.Sprintf("query { player(steamAccountId: %d) { matches { id } } }", seed)},
		{"take only", fmt.Sprintf("query { player(steamAccountId: %d) { matches(request: { take: 5 }) { id } } }", seed)},
		{"take+orderBy", fmt.Sprintf("query { player(steamAccountId: %d) { matches(request: { take: 5, orderBy: DESC }) { id } } }", seed)},
		{"playerList ALL", fmt.Sprintf("query { player(steamAccountId: %d) { matches(request: { take: 5, playerList: ALL }) { id } } }", seed)},
		{"cursor after recent id", fmt.Sprintf("query { player(steamAccountId: %d) { matches(request: { take: 5, orderBy: DESC, after: 9000000000 }) { id } } }", seed)},
		{"plural players root", fmt.Sprintf("query { players(steamAccountIds: [%d]) { matches(request: { take: 5 }) { id } } }", seed)},
	} {
		var rows []probeMatch
		var err error
		if strings.Contains(v.q, "players(") {
			rows, err = fetchPlayersMatches(c, v.q)
		} else {
			rows, err = fetchPlayerMatches(c, v.q)
		}
		switch {
		case err != nil:
			fmt.Printf("probe matches player-browse %q: REJECTED %s\n", v.label, shortErr(err))
		case len(rows) == 0:
			fmt.Printf("probe matches player-browse %q: 0 rows\n", v.label)
		default:
			fmt.Printf("probe matches player-browse %q: ANSWERS, %d rows, first id %d\n", v.label, len(rows), rows[0].ID)
		}
	}
}

// pickProbeSeed walks the served leaderboard ids until one carries matches,
// printing matchCount/lastMatchDate evidence per candidate.
func pickProbeSeed(c *stratzClient, divisionIDs map[string][]int64) int64 {
	tried := 0
	for _, div := range leaderboardDivisions {
		for _, id := range divisionIDs[div] {
			if tried >= probeSeedCandidates {
				break
			}
			tried++
			body, err := c.query(playerInfoQuery(id))
			if err != nil {
				fmt.Printf("probe matches seed %d (%s): %v\n", id, div, err)
				continue
			}
			var d struct {
				Player struct {
					MatchCount    int   `json:"matchCount"`
					LastMatchDate int64 `json:"lastMatchDate"`
				} `json:"player"`
			}
			if err := decodeEnvelope(body, &d); err != nil {
				fmt.Printf("probe matches seed %d (%s): %v\n", id, div, err)
				continue
			}
			fmt.Printf("probe matches seed %d (%s): matchCount %d, lastMatchDate %d\n",
				id, div, d.Player.MatchCount, d.Player.LastMatchDate)
			if d.Player.MatchCount > 0 {
				return id
			}
		}
	}
	return 0
}

func printSampleMatch(m probeMatch) {
	if b, err := json.MarshalIndent(m, "", "  "); err == nil {
		fmt.Println(string(b))
	}
}

// ProbeMatches verifies the per-match backfill path live and prints the
// evidence: schema facts, the pinned browse shape answering, draft coverage
// per game mode in scope.bracket, enum ids against constants and a known
// match, and take/skip pagination behavior.
func ProbeMatches(cfg *config.Config) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)
	since, err := time.Parse("2006-01-02", cfg.PatchEpoch)
	if err != nil {
		return fmt.Errorf("patchEpoch: %w", err)
	}
	until := time.Now().Unix()

	if _, err := introspectMatches(client); err != nil {
		fmt.Printf("probe matches: introspection incomplete (%v), pinned shapes carry on\n", err)
	}
	discoverMatchesShape(client, matchFieldsSel)
	if err := probeMatchesConstants(client); err != nil {
		fmt.Printf("probe matches constants: %v\n", err)
	}

	divisionIDs := map[string][]int64{}
	var seed int64
	for _, div := range leaderboardDivisions {
		count, ids, err := fetchSeasonPlayers(client, div)
		if err != nil {
			fmt.Printf("probe matches season %s: %v\n", div, err)
			continue
		}
		fmt.Printf("probe matches season %s: playerCount %d, %d ids served\n", div, count, len(ids))
		divisionIDs[div] = ids
	}
	seed = pickProbeSeed(client, divisionIDs)
	if seed == 0 {
		fmt.Println("probe matches: no leaderboard player with matches served, skipping the per-player browse probes")
		return nil
	}
	fmt.Printf("probe matches: browse seed steamAccountId %d\n", seed)

	fmt.Printf("probe matches: filter ids gameMode ranked=22 captains=2 lobby=7, bracket gate local from scope.bracket %s\n",
		bracketIdsFilter(cfg.Scope.Bracket))

	probePlayerMatchesShape(client, seed)
	take := clampTake(cfg.Stratz.Take)
	for _, mode := range []struct {
		label string
		ids   string
	}{
		{"ranked all pick (22)", "22"},
		{"captains mode (2)", "2"},
	} {
		q := playerMatchesQuery(seed, backfillFilterPayload(mode.ids, since.Unix(), until), take, 0)
		rows, err := fetchPlayerMatches(client, q)
		if err != nil {
			fmt.Printf("probe matches browse %s: %v\n", mode.label, err)
			continue
		}
		cov := measureDraftCoverage(rows)
		fmt.Printf("probe matches browse %s: %d rows, %d with any pickBans, %d with complete 10-pick drafts\n",
			mode.label, cov.Rows, cov.AnyDraft, cov.Complete)
	}

	probeLeagueListing(client, since.Unix(), until)
	probeLeagueMatches(client, since.Unix(), until)
	if err := probeMatchesPagination(client, since.Unix(), until); err != nil {
		fmt.Printf("probe matches pagination: %v\n", err)
	}
	byId, err := fetchRootMatches(client, matchesByIdsQuery(probeSampleMatchIDs))
	if err != nil {
		fmt.Printf("probe matches by-ids: %v\n", err)
		return nil
	}
	fmt.Printf("probe matches by-ids: %d/%d known-sample matches served\n", len(byId), len(probeSampleMatchIDs))
	cov := measureDraftCoverage(byId)
	fmt.Printf("probe matches by-ids coverage: %d rows, %d with any pickBans, %d with complete 10-pick drafts\n",
		cov.Rows, cov.AnyDraft, cov.Complete)
	for _, m := range byId {
		fmt.Printf("probe matches by-ids: match %d gameMode=%s lobbyType=%s bracket=%d averageRank=%d startDateTime=%d pickBans=%d\n",
			m.ID, m.GameMode, m.LobbyType, m.Bracket, m.AverageRank, m.StartDateTime, len(m.PickBans))
	}
	if len(byId) > 0 {
		fmt.Println("probe matches: sample by-ids match:")
		printSampleMatch(byId[0])
	}
	return nil
}

// probeMatchesConstants cross-checks the enum names the graphql filters take
// against the numeric ids the config hub stores.
func probeMatchesConstants(c *stratzClient) error {
	body, err := c.query(probeConstantsQuery)
	if err != nil {
		return err
	}
	var d struct {
		Constants struct {
			GameModes []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"gameModes"`
			LobbyTypes []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"lobbyTypes"`
		} `json:"constants"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		return err
	}
	for _, gm := range d.Constants.GameModes {
		if gm.ID == 22 || gm.ID == 2 {
			fmt.Printf("probe matches constants: gameMode id %d = %q\n", gm.ID, gm.Name)
		}
	}
	for _, lt := range d.Constants.LobbyTypes {
		if lt.ID == 1 || lt.ID == 2 || lt.ID == 7 {
			fmt.Printf("probe matches constants: lobbyType id %d = %q\n", lt.ID, lt.Name)
		}
	}
	return nil
}

// probeMatchesPagination proves take/skip walks distinct pages under the
// window filter inside the league browse (the accessible path).
func probeMatchesPagination(c *stratzClient, since, until int64) error {
	leagueID, err := firstLeagueID(c)
	if err != nil {
		return err
	}
	payload := fmt.Sprintf("startDateTime: %d, endDateTime: %d", since, until)
	p1, err := fetchLeagueMatches(c, leagueMatchesQuery(leagueID, payload, 5, 0))
	if err != nil {
		return err
	}
	p2, err := fetchLeagueMatches(c, leagueMatchesQuery(leagueID, payload, 5, 5))
	if err != nil {
		return err
	}
	ids := map[int64]bool{}
	for _, m := range p1 {
		ids[m.ID] = true
	}
	overlap := 0
	for _, m := range p2 {
		if ids[m.ID] {
			overlap++
		}
	}
	fmt.Printf("probe matches pagination (league %d): skip=0 n=%d [%d..%d], skip=5 n=%d [%d..%d], overlap=%d\n",
		leagueID, len(p1), edgeMatch(p1, false).ID, edgeMatch(p1, true).ID,
		len(p2), edgeMatch(p2, false).ID, edgeMatch(p2, true).ID, overlap)
	for _, m := range append(append([]probeMatch{}, p1...), p2...) {
		if m.StartDateTime < since || m.StartDateTime > until {
			fmt.Printf("probe matches pagination: match %d startDateTime %d outside the filter window\n", m.ID, m.StartDateTime)
		}
	}
	return nil
}

func firstLeagueID(c *stratzClient) (int64, error) {
	body, err := c.query(leaguesPageQuery(1, 0))
	if err != nil {
		return 0, err
	}
	var d struct {
		Leagues []struct {
			ID int64 `json:"id"`
		} `json:"leagues"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		return 0, err
	}
	if len(d.Leagues) == 0 {
		return 0, fmt.Errorf("no leagues served")
	}
	return d.Leagues[0].ID, nil
}

func fetchLeagueMatches(c *stratzClient, q string) ([]probeMatch, error) {
	body, err := c.query(q)
	if err != nil {
		return nil, err
	}
	var d struct {
		League struct {
			Matches []probeMatch `json:"matches"`
		} `json:"league"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	return d.League.Matches, nil
}

// edgeMatch returns the first (or last) row, zero value on an empty page.
func edgeMatch(rows []probeMatch, last bool) probeMatch {
	if len(rows) == 0 {
		return probeMatch{}
	}
	if last {
		return rows[len(rows)-1]
	}
	return rows[0]
}
