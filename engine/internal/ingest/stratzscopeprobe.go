package ingest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"poolguide/internal/config"
)

// Live scope probe for the heroStats aggregate family: which filter args the
// product queries can carry (gameModeIds, lobbyTypeIds, positionIds) so the
// crawl is scoped to divine-immortal ranked all pick by enforced filters, not
// by the scope.note prose. Evidence comes from a batched __schema read plus an
// accept/reject ladder with row counts, printed per arg, never fatal; the
// ladder tries numeric and enum token forms because the league path sends
// numeric ids while heroStats fields take enum names elsewhere.

type scopeAttempt struct {
	Label string
	Query string
	Rows  int
	Err   error
}

// scopeLadderQuery builds one heroStats call: base args, optionally one
// candidate extra appended, and the selection.
func scopeLadderQuery(field, base, extra, sel string) string {
	args := base
	if extra != "" {
		args += ", " + extra
	}
	return fmt.Sprintf("query { heroStats { %s(%s) { %s } } }", field, args, sel)
}

// countHeroStatsRows counts the first list under heroStats, mirroring how
// parsePopRows reads whichever aggregate field the query named. A graphql
// error envelope is a rejection, not a zero-row answer.
func countHeroStatsRows(body []byte) (int, error) {
	var d popData
	if err := decodeEnvelope(body, &d); err != nil {
		return 0, err
	}
	for _, raw := range d.HeroStats {
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err == nil {
			return len(rows), nil
		}
	}
	return 0, nil
}

// heroStatsLadder runs the base args, then each candidate extra appended to
// the base alone, returning one attempt per step with its row count or its
// rejection error.
func heroStatsLadder(c gqlFetcher, field, base, sel string, extras []string) []scopeAttempt {
	run := func(label, extra string) scopeAttempt {
		q := scopeLadderQuery(field, base, extra, sel)
		body, err := c.query(q)
		if err != nil {
			return scopeAttempt{Label: label, Query: q, Err: err}
		}
		rows, err := countHeroStatsRows(body)
		return scopeAttempt{Label: label, Query: q, Rows: rows, Err: err}
	}
	attempts := make([]scopeAttempt, 0, len(extras)+1)
	attempts = append(attempts, run("base", ""))
	for _, extra := range extras {
		attempts = append(attempts, run("+"+extra, extra))
	}
	return attempts
}

func printScopeAttempts(field string, attempts []scopeAttempt) {
	for _, a := range attempts {
		if a.Err != nil {
			fmt.Printf("probe scope %s %s: REJECTED %s\n", field, a.Label, shortErr(a.Err))
			continue
		}
		fmt.Printf("probe scope %s %s: ACCEPTED %d rows\n", field, a.Label, a.Rows)
	}
}

// introspectScope reads one batched __schema snapshot and hands out the
// HeroStatsQuery arg lists plus the raw envelope for type printers.
func introspectScope(c *stratzClient) (*introEnvelope, map[string][]string, error) {
	body, err := c.query(probeIntrospectionQuery)
	if err != nil {
		return nil, nil, err
	}
	var env introEnvelope
	if err := decodeEnvelope(body, &env); err != nil {
		return nil, nil, err
	}
	for _, t := range env.Sch.Types {
		if t.Name != "HeroStatsQuery" {
			continue
		}
		out := map[string][]string{}
		for _, f := range t.Fields {
			var args []string
			for _, a := range f.Args {
				args = append(args, a.Name)
			}
			sort.Strings(args)
			out[f.Name] = args
		}
		return &env, out, nil
	}
	return nil, nil, fmt.Errorf("HeroStatsQuery type not present in schema snapshot")
}

// scopeFilterExtras are the candidate filter args every ladder run tries, in
// both numeric and enum forms for the game mode. bracketIds candidates cover
// the enum-name and numeric spellings of divine-immortal because the win*
// aggregates dropped the old bracketBasicIds arg.
func scopeFilterExtras() []string {
	return []string{
		"gameModeIds: [22]",
		"gameModeIds: [ALL_PICK_RANKED]",
		"bracketIds: [DIVINE_IMMORTAL]",
		"bracketIds: [DIVINE, IMMORTAL]",
		"bracketIds: [7, 8]",
		"positionIds: [POSITION_1]",
		"gameModeIds: [ALL_PICK_RANKED], bracketIds: [DIVINE, IMMORTAL]",
	}
}

// popRowsByHero folds time-bucketed aggregate rows into per-hero sums so two
// arms of a probe (filtered vs unfiltered) compare on the same unit.
func popRowsByHero(rows []popRow) map[int]popRow {
	by := map[int]popRow{}
	for _, r := range rows {
		acc := by[r.HeroID]
		acc.HeroID = r.HeroID
		acc.MatchCount += r.MatchCount
		acc.WinCount += r.WinCount
		by[r.HeroID] = acc
	}
	return by
}

// diffPopRows reports how many heroes differ in match or win counts between
// two aggregate arms plus the max and median relative match-count gaps, so a
// zero diff is positive evidence the bracket aggregate is already mode-pure.
func diffPopRows(a, b []popRow) (heroes, differing int, maxGap, medianGap float64) {
	ab, bb := popRowsByHero(a), popRowsByHero(b)
	heroes = len(ab)
	var gaps []float64
	for id, ar := range ab {
		br, ok := bb[id]
		if !ok || ar.MatchCount != br.MatchCount || ar.WinCount != br.WinCount {
			differing++
		}
		if ok && ar.MatchCount > 0 {
			gap := float64(ar.MatchCount-br.MatchCount) / float64(ar.MatchCount)
			if gap < 0 {
				gap = -gap
			}
			gaps = append(gaps, gap)
			if gap > maxGap {
				maxGap = gap
			}
		}
	}
	sort.Float64s(gaps)
	if len(gaps) > 0 {
		medianGap = gaps[len(gaps)/2]
	}
	return heroes, differing, maxGap, medianGap
}

// ProbeScope prints the live evidence for scoping every heroStats query the
// crawl sends: the schema's own arg lists, the enum id cross-check, and the
// accept/reject ladder with row counts over matchUp, the popularity ladder
// aggregates, and itemFullPurchase.
func ProbeScope(cfg *config.Config) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)

	if env, args, err := introspectScope(client); err != nil {
		fmt.Printf("probe scope: introspection incomplete (%v), ladder carries on\n", err)
	} else {
		for _, field := range []string{"matchUp", "winMonth", "winWeek", "winDay", "itemFullPurchase"} {
			fmt.Printf("probe scope introspected %s args: %v\n", field, args[field])
		}
		printScopeTypes(env)
	}
	if err := probeMatchesConstants(client); err != nil {
		fmt.Printf("probe scope constants: %v\n", err)
	}

	heroes, err := client.FetchRoster()
	if err != nil {
		return fmt.Errorf("scope probe roster: %w", err)
	}
	bySlug := map[string]int{}
	for _, h := range heroes {
		bySlug[cfg.SlugFromNPC(h.ShortName)] = h.ID
	}
	var heroID int
	var heroSlug string
	for _, slug := range cfg.PoolSlugs() {
		if id, ok := bySlug[slug]; ok {
			heroID, heroSlug = id, slug
			break
		}
	}
	if heroID == 0 {
		return fmt.Errorf("scope probe: no pool hero found in roster")
	}
	bracket := cfg.Scope.Bracket
	take := cfg.Stratz.Take
	fmt.Printf("probe scope: per-hero fields probe %s (id %d), bracket %s, take %d\n", heroSlug, heroID, bracket, take)

	matchUpBase := fmt.Sprintf("heroId: %d, take: %d, bracketBasicIds: [%s]", heroID, take, bracket)
	printScopeAttempts("matchUp", heroStatsLadder(client, "matchUp", matchUpBase, "heroId vs { heroId2 }", scopeFilterExtras()))

	popSel := "heroId matchCount winCount"
	for _, field := range []string{"winMonth", "winWeek", "winDay"} {
		base := fmt.Sprintf("take: %d", take)
		printScopeAttempts(field, heroStatsLadder(client, field, base, popSel, scopeFilterExtras()))
	}

	// mode equivalence inside the bracket: if the divine-immortal aggregate
	// already is ranked all pick, adding the gameModeIds filter changes no
	// per-hero count, and the bracket-scoped pair tables (matchUp, which
	// takes no mode arg) are de facto mode-pure too
	if err := probeModeEquivalence(client, "winMonth", take,
		"bracketIds: [DIVINE, IMMORTAL]",
		"bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]"); err != nil {
		fmt.Printf("probe scope mode equivalence: %v\n", err)
	}
	// row counts came back identical on these two; the value diff decides
	// whether the filters bite or are silently ignored
	for _, field := range []string{"winWeek", "winDay"} {
		if err := probeModeEquivalence(client, field, take,
			"bracketIds: [DIVINE, IMMORTAL]",
			"bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]"); err != nil {
			fmt.Printf("probe scope mode equivalence %s: %v\n", field, err)
		}
	}
	// one live findMatchPlayer attempt: its groupBy enum carries POSITION and
	// GAME_MODE, so an ungated answer would open per-position and per-mode
	// aggregates beyond the heroStats family
	if body, err := client.query("query { findMatchPlayer(request: { groupBy: [POSITION], bracketBasicIds: [DIVINE_IMMORTAL], gameModeIds: [ALL_PICK_RANKED] }) { matchId } }"); err != nil {
		fmt.Printf("probe scope findMatchPlayer: REJECTED %s\n", shortErr(err))
	} else {
		fmt.Printf("probe scope findMatchPlayer: answers, %s\n", strings.TrimSpace(shortBody(body)))
	}

	// winGameVersion: per-patch rows under the same filter family, which
	// would pin popularity and wr to the exact patch window instead of a
	// rolling day count
	gvBase := fmt.Sprintf("take: %d, bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]", take)
	printScopeAttempts("winGameVersion", heroStatsLadder(client, "winGameVersion", gvBase, "heroId matchCount winCount",
		[]string{"positionIds: [POSITION_1]", "groupBy: HERO_ID"}))
	if body, err := client.query(scopeLadderQuery("winGameVersion", gvBase, "groupBy: HERO_ID",
		"heroId matchCount winCount gameVersionId")); err != nil {
		fmt.Printf("probe scope winGameVersion row shape: REJECTED %s\n", shortErr(err))
	} else {
		fmt.Printf("probe scope winGameVersion row shape: answers, %s\n", strings.TrimSpace(shortBody(body)))
	}

	// position slicing must change values, not just answer: two adjacent
	// positions on winDay under the same bracket+mode scope
	if err := probeModeEquivalence(client, "winDay", take,
		"bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED], positionIds: [POSITION_1]",
		"bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED], positionIds: [POSITION_2]"); err != nil {
		fmt.Printf("probe scope position slice equivalence: %v\n", err)
	}

	// groupBy shapes: HERO_ID collapses the month buckets to one row per
	// hero, HERO_ID_POSITION_BRACKET may serve the whole positions feature
	// in one request
	groupByBase := fmt.Sprintf("take: %d, bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED]", take)
	printScopeAttempts("winMonth grouped", heroStatsLadder(client, "winMonth", groupByBase, popSel,
		[]string{"groupBy: HERO_ID", "groupBy: ALL", "groupBy: HERO_ID_POSITION_BRACKET"}))
	if body, err := client.query(scopeLadderQuery("winMonth", groupByBase,
		"groupBy: HERO_ID_POSITION_BRACKET", "heroId matchCount winCount position bracket")); err != nil {
		fmt.Printf("probe scope grouped row shape: REJECTED %s\n", shortErr(err))
	} else {
		fmt.Printf("probe scope grouped row shape: answers, %s\n", strings.TrimSpace(shortBody(body)))
	}
	if body, err := client.query(scopeLadderQuery("winMonth", groupByBase,
		"groupBy: HERO_ID", "heroId matchCount winCount")); err != nil {
		fmt.Printf("probe scope grouped hero sample: REJECTED %s\n", shortErr(err))
	} else {
		fmt.Printf("probe scope grouped hero sample: answers, %s\n", strings.TrimSpace(shortBody(body)))
	}

	buildsBase := fmt.Sprintf("heroId: %d, bracketBasicIds: [%s], positionIds: [POSITION_1], matchLimit: 1", heroID, bracket)
	buildsExtras := []string{"gameModeIds: [22]", "gameModeIds: [ALL_PICK_RANKED]", "lobbyTypeIds: [7]"}
	printScopeAttempts("itemFullPurchase", heroStatsLadder(client, "itemFullPurchase", buildsBase, "itemId time instance matchCount winCount", buildsExtras))

	// position-tagged rows would let one query serve all positions; sliced
	// rows mean one request per position. Ask for the list form and print
	// whether the row shape carries a position field back.
	if body, err := client.query(scopeLadderQuery("winWeek",
		fmt.Sprintf("take: %d", take),
		"gameModeIds: [ALL_PICK_RANKED], positionIds: [POSITION_1, POSITION_2]",
		"heroId matchCount winCount position")); err != nil {
		fmt.Printf("probe scope position-list shape: REJECTED %s\n", shortErr(err))
	} else {
		fmt.Printf("probe scope position-list shape: answers, %s\n", strings.TrimSpace(shortBody(body)))
	}
	return nil
}

// probeModeEquivalence fetches one aggregate under two arg sets and diffs
// the per-hero counts, measuring whether the second filter changes anything
// inside the population the first filter already selected.
func probeModeEquivalence(c *stratzClient, field string, take int, baseExtra, filteredExtra string) error {
	fetch := func(extra string) ([]popRow, error) {
		body, err := c.query(scopeLadderQuery(field, fmt.Sprintf("take: %d", take), extra, "heroId matchCount winCount"))
		if err != nil {
			return nil, err
		}
		var d popData
		if err := decodeEnvelope(body, &d); err != nil {
			return nil, err
		}
		for _, raw := range d.HeroStats {
			var rows []popRow
			if err := json.Unmarshal(raw, &rows); err == nil {
				return rows, nil
			}
		}
		return nil, nil
	}
	base, err := fetch(baseExtra)
	if err != nil {
		return err
	}
	rap, err := fetch(filteredExtra)
	if err != nil {
		return err
	}
	heroes, differing, maxGap, medianGap := diffPopRows(base, rap)
	fmt.Printf("probe scope mode equivalence on %s (%s vs %s): %d heroes, %d differ, match gap max %.4f median %.4f (base rows %d, rap rows %d)\n",
		field, baseExtra, filteredExtra, heroes, differing, maxGap, medianGap, len(base), len(rap))
	return nil
}

// printScopeTypes prints the schema facts the scoping design consumes: every
// HeroStatsQuery field name, the win* row type fields (position attribution),
// the groupBy enums, and the findMatchPlayer surface.
func printScopeTypes(env *introEnvelope) {
	byName := map[string]*introType{}
	for _, t := range env.Sch.Types {
		byName[t.Name] = t
	}
	if hq := byName["HeroStatsQuery"]; hq != nil {
		var sigs []string
		for _, f := range hq.Fields {
			var args []string
			for _, a := range f.Args {
				args = append(args, a.Name)
			}
			sigs = append(sigs, fmt.Sprintf("%s(%s)", f.Name, strings.Join(args, ",")))
		}
		fmt.Printf("probe scope HeroStatsQuery fields: %v\n", sigs)
	}
	for _, n := range []string{"HeroWinDayType", "HeroWinWeekType", "HeroWinMonthType", "HeroWinGameVersionType", "HeroStatsType", "HeroStatsTakeType", "HeroVsHeroMatchupType"} {
		if t := byName[n]; t != nil {
			var names []string
			for _, f := range t.Fields {
				names = append(names, f.Name)
			}
			fmt.Printf("probe scope %s fields: %v\n", n, names)
		}
	}
	for _, t := range env.Sch.Types {
		if (strings.Contains(t.Name, "GroupBy") || strings.Contains(t.Name, "GroupByEnum")) && len(t.EnumValues) > 0 {
			var names []string
			for _, v := range t.EnumValues {
				names = append(names, v.Name)
			}
			fmt.Printf("probe scope enum %s: %v\n", t.Name, names)
		}
	}
	if root := env.Sch.QueryType; root != nil {
		for _, f := range root.Fields {
			if strings.Contains(strings.ToLower(f.Name), "matchplayer") || strings.Contains(strings.ToLower(f.Name), "herostats") {
				var args []string
				for _, a := range f.Args {
					args = append(args, a.Name)
				}
				fmt.Printf("probe scope root %s(%s): %s\n", f.Name, strings.Join(args, ","), f.Type.displayName())
			}
		}
	}
	for _, n := range []string{"FindMatchPlayerRequestType", "FilterHeroWinRequestType"} {
		if t := byName[n]; t != nil {
			var names []string
			for _, f := range t.InputFields {
				names = append(names, f.Name)
			}
			fmt.Printf("probe scope input %s: %v\n", n, names)
		}
	}
}
