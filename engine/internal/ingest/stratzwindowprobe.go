package ingest

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"poolguide/internal/config"
)

// Live window probe for the heroStats families: winDay takes days
// (server-capped at 30), matchUp's take is pagination and its Long week arg
// is a calendar-week stamp, itemFullPurchase shares the week arg while its
// minTime/maxTime filter purchase minutes. Verdicts are pure functions over
// measured arms; any family whose window cannot be pinned is a hard error,
// the crawl must never serve a window the hub does not control.

// windowArmStat is one family's measured evidence at one arm: served row
// count, summed match volume, largest raw cell, or the arm's request error.
type windowArmStat struct {
	Label    string
	Rows     int
	MatchSum int64
	MaxCell  int64
	Err      error
}

// windowSaturationTake is the known server cap on the win* day buckets and
// the reference arm every sub-30 hub take must shrink below.
const windowSaturationTake = 30

// windowOldHubTake is the pre-2026-09-29 hub ceiling, live-identical to the
// saturation arm on winDay; the ladder keeps it so a future semantics change
// that breaks that identity surfaces in the same run. On matchUp it is the
// return-all pagination value (every enemy row), never a window.
const windowOldHubTake = 200

// windowCalibrationTake is the take the week ladder reads its volumes
// against: one true 7-day winDay slice.
const windowCalibrationTake = 7

// windowTakeArms is the deduped, sorted take ladder: the hub value plus the
// two reference arms.
func windowTakeArms(hub int) []int {
	set := map[int]bool{hub: true, windowSaturationTake: true, windowOldHubTake: true}
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// winDayVerdict decides the win* family: honored at a sub-saturation hub take
// iff the rows equal the roster x take bucket universe (the universe is fixed
// by the api, fewer rows mean a silently shrunk window) and the volume
// strictly shrinks below the saturation arm. At or above the cap nothing is
// pinned, which is the exact pre-pin defect, so it fails.
func winDayVerdict(hub, sat windowArmStat, rosterCount, hubTake int) (bool, string) {
	if hub.Err != nil {
		return false, fmt.Sprintf("hub arm errored: %v", hub.Err)
	}
	if sat.Err != nil {
		return false, fmt.Sprintf("30-day arm errored: %v", sat.Err)
	}
	if hubTake >= windowSaturationTake {
		return false, fmt.Sprintf("hub take %d is at or above the %d-day server cap, the window is unpinned, lower stratz.take",
			hubTake, windowSaturationTake)
	}
	if universe := rosterCount * hubTake; hub.Rows != universe {
		return false, fmt.Sprintf("rows %d do not equal the fixed %d-hero x %d-day bucket universe %d, the window is not what take claims",
			hub.Rows, rosterCount, hubTake, universe)
	}
	if hub.MatchSum >= sat.MatchSum {
		return false, fmt.Sprintf("summed volume %d did not shrink below the 30-day arm %d, silent saturation",
			hub.MatchSum, sat.MatchSum)
	}
	return true, fmt.Sprintf("rows %d equal the bucket universe, volume %d strictly under the 30-day arm %d",
		hub.Rows, hub.MatchSum, sat.MatchSum)
}

// weekIgnoreTolerance is the relative drift allowed between the week arm and
// the no-week default before the pair still reads as identical: the two live
// calls land seconds apart, so a match arriving in between must not defeat
// ignore detection. A genuinely different window sits tens of percent away.
const weekIgnoreTolerance = 0.001

// calendarWeekVerdict decides the completed-week lever: honored when the arm
// answers over the floor, is not the ignored in-progress-week shape (rows
// equal and volume within drift tolerance of the no-week default), and its
// volume sits in the band one week explains against the calibration (pair
// sums count each match once per enemy slot; the band absorbs weekly variance
// and the mode mix).
func calendarWeekVerdict(arm, baseline, calib windowArmStat, enemySlots float64, floor int) (bool, string) {
	if arm.Err != nil {
		return false, fmt.Sprintf("week arm errored: %v", arm.Err)
	}
	if baseline.Err != nil {
		return false, fmt.Sprintf("no-week baseline errored: %v", baseline.Err)
	}
	if arm.Rows == 0 {
		return false, "week arm answered empty"
	}
	if floor > 0 && arm.Rows < floor {
		return false, fmt.Sprintf("rows %d fell below the %d-row acceptance floor", arm.Rows, floor)
	}
	if arm.Rows == baseline.Rows &&
		math.Abs(float64(arm.MatchSum-baseline.MatchSum)) <= weekIgnoreTolerance*float64(baseline.MatchSum) {
		return false, "week arm is volume-identical to the no-week default within drift, the server ignored the week value"
	}
	if calib.MatchSum <= 0 {
		return false, "calibration volume missing, cannot band-check the week arm"
	}
	ratio := float64(arm.MatchSum) / (enemySlots * float64(calib.MatchSum))
	if ratio < 0.5 {
		return false, fmt.Sprintf("volume ratio %.2f starved for a full week", ratio)
	}
	if ratio > 2.5 {
		return false, fmt.Sprintf("volume ratio %.2f looks like a multi-week or unfiltered window", ratio)
	}
	return true, fmt.Sprintf("rows %d, volume ratio %.2f within the single-week band", arm.Rows, ratio)
}

// weekStarts lives in stratz.go beside the query that pins it.

// impliedWindowDays estimates a server-windowed aggregate's span from its
// largest raw cell against the hub-windowed winDay volume for the same hero:
// the ratio times the reference take. A heuristic (the max cell never covers
// every match), labeled as one wherever printed.
func impliedWindowDays(maxCell, refMatch int64, refTake int) float64 {
	if refMatch <= 0 {
		return 0
	}
	return float64(maxCell) / float64(refMatch) * float64(refTake)
}

// sumPopRows folds day-bucket rows into (rows, volume).
func sumPopRows(rows []popRow) (int, int64) {
	var sum int64
	for _, r := range rows {
		sum += r.MatchCount
	}
	return len(rows), sum
}

// winDayArm runs the product popularity query at one take and folds the raw
// day-bucket rows into the arm stat.
func winDayArm(c gqlFetcher, cfg *config.Config, take int) windowArmStat {
	label := fmt.Sprintf("take %d", take)
	q, err := aggregateQueryAt(cfg, take)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	body, err := c.query(q)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	rows, err := parsePopRows(body)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	n, sum := sumPopRows(rows)
	return windowArmStat{Label: label, Rows: n, MatchSum: sum}
}

// winDayHeroArm folds one hero's rows out of the popularity aggregate at one
// take: the per-hero 7-day calibration volume.
func winDayHeroArm(c gqlFetcher, cfg *config.Config, heroID, take int) windowArmStat {
	label := fmt.Sprintf("hero %d take %d", heroID, take)
	q, err := aggregateQueryAt(cfg, take)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	body, err := c.query(q)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	rows, err := parsePopRows(body)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	var sum int64
	n := 0
	for _, r := range rows {
		if r.HeroID != heroID {
			continue
		}
		n++
		sum += r.MatchCount
	}
	return windowArmStat{Label: label, Rows: n, MatchSum: sum}
}

// positionArm runs the product positions query for farm position 1 at one
// take and returns the summed volume only: it feeds the builds estimate.
func positionArm(c gqlFetcher, cfg *config.Config, take int) (windowArmStat, error) {
	mode, ok := gameModeEnumToken(cfg.Scope.GameMode)
	if !ok {
		return windowArmStat{}, fmt.Errorf("window probe: gameMode %d has no known enum token", cfg.Scope.GameMode)
	}
	brackets, ok := winBracketIds(cfg.Scope.Bracket)
	if !ok {
		return windowArmStat{}, fmt.Errorf("window probe: bracket %q has no win* bracketIds mapping", cfg.Scope.Bracket)
	}
	label := fmt.Sprintf("take %d", take)
	body, err := c.query(positionQuery(1, take, brackets, mode))
	if err != nil {
		return windowArmStat{Label: label, Err: err}, nil
	}
	rows, err := parsePopRows(body)
	if err != nil {
		return windowArmStat{Label: label, Err: err}, nil
	}
	n, sum := sumPopRows(rows)
	return windowArmStat{Label: label, Rows: n, MatchSum: sum}, nil
}

// matchUpArm runs the product pair query at one take and folds the cleaned
// vs/with rows into two arm stats, with the per-enemy vs cell map alongside
// for the subset evidence (the no-week arm at the pagination take doubles as
// the default baseline).
func matchUpArm(c gqlFetcher, cfg *config.Config, heroID, take int) (vs, with windowArmStat, vsCells map[int]int64) {
	label := fmt.Sprintf("take %d", take)
	stat := func(rows []matchupRow) windowArmStat {
		var sum int64
		for _, r := range rows {
			sum += r.MatchCount
		}
		return windowArmStat{Label: label, Rows: len(rows), MatchSum: sum}
	}
	body, err := c.query(matchUpQuery(heroID, take, cfg.Scope.Bracket, 0))
	if err != nil {
		e := windowArmStat{Label: label, Err: err}
		return e, e, nil
	}
	mu, err := parseMatchUp(body)
	if err != nil {
		e := windowArmStat{Label: label, Err: err}
		return e, e, nil
	}
	cells := map[int]int64{}
	for _, r := range mu.Vs {
		cells[r.HeroID2] = r.MatchCount
	}
	return stat(mu.Vs), stat(mu.With), cells
}

// matchUpWeekArm runs the pair query pinned to one week value and returns the
// arm stat plus the per-enemy cell map (subset evidence between arms).
func matchUpWeekArm(c gqlFetcher, cfg *config.Config, heroID int, week int64) (windowArmStat, map[int]int64) {
	label := fmt.Sprintf("week-start %d", week)
	body, err := c.query(matchUpQuery(heroID, windowOldHubTake, cfg.Scope.Bracket, week))
	if err != nil {
		return windowArmStat{Label: label, Err: err}, nil
	}
	mu, err := parseMatchUp(body)
	if err != nil {
		return windowArmStat{Label: label, Err: err}, nil
	}
	cells := map[int]int64{}
	var sum int64
	for _, r := range mu.Vs {
		cells[r.HeroID2] = r.MatchCount
		sum += r.MatchCount
	}
	return windowArmStat{Label: label, Rows: len(mu.Vs), MatchSum: sum}, cells
}

// cellsSubset reports both directions: aInB is true when every cell of a is
// <= the matching cell of b (a's window is contained in b's), which
// distinguishes calendar slices (incomparable) from cumulative bounds
// (nested).
func cellsSubset(a, b map[int]int64) (aInB, bInA bool) {
	aInB, bInA = true, true
	for k, v := range a {
		if v > b[k] {
			aInB = false
		}
	}
	for k, v := range b {
		if v > a[k] {
			bInA = false
		}
	}
	return
}

// buildsArmAt runs the product builds query plus optional extra args and
// folds the purchase rows into an arm stat. label names the arm in output,
// separate from args because args carry the graphql syntax.
func buildsArmAt(c gqlFetcher, cfg *config.Config, heroID int, args, label string) windowArmStat {
	q := fmt.Sprintf("query { heroStats { itemFullPurchase(heroId: %d, bracketBasicIds: [%s], positionIds: [POSITION_1], matchLimit: %d%s) { %s } } }",
		heroID, cfg.Scope.Bracket, buildsMatchLimit, prefixComma(args), buildsPurchaseShape)
	body, err := c.query(q)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	rows, err := parsePurchases(body)
	if err != nil {
		return windowArmStat{Label: label, Err: err}
	}
	var sum, maxCell int64
	for _, r := range rows {
		sum += r.MatchCount
		if r.MatchCount > maxCell {
			maxCell = r.MatchCount
		}
	}
	return windowArmStat{Label: label, Rows: len(rows), MatchSum: sum, MaxCell: maxCell}
}

func prefixComma(args string) string {
	if args == "" {
		return ""
	}
	return ", " + args
}

// argTypeNames maps a field's args to their display type names from the
// schema snapshot.
func argTypeNames(env *introEnvelope, field string) map[string]string {
	out := map[string]string{}
	if env == nil {
		return out
	}
	for _, t := range env.Sch.Types {
		if t.Name != "HeroStatsQuery" {
			continue
		}
		for _, f := range t.Fields {
			if f.Name != field {
				continue
			}
			for _, a := range f.Args {
				if a.Type != nil {
					out[a.Name] = a.Type.displayName()
				}
			}
		}
	}
	return out
}

// ProbeWindow prints the live window evidence per family and returns a hard
// error naming every family whose window cannot be pinned to the hub's.
func ProbeWindow(cfg *config.Config) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)

	var env *introEnvelope
	if e, args, err := introspectScope(client); err != nil {
		fmt.Printf("probe window: introspection incomplete (%v), arg lists unknown\n", err)
	} else {
		env = e
		for _, f := range []string{"matchUp", "winDay", "itemFullPurchase"} {
			fmt.Printf("probe window introspected %s args: %v\n", f, args[f])
		}
		for _, f := range []string{"matchUp", "itemFullPurchase"} {
			types := argTypeNames(env, f)
			for _, a := range []string{"take", "week", "minTime", "maxTime"} {
				if types[a] != "" {
					fmt.Printf("probe window %s arg %s: %s\n", f, a, types[a])
				}
			}
		}
	}

	heroes, err := client.FetchRoster()
	if err != nil {
		return fmt.Errorf("window probe roster: %w", err)
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
		return fmt.Errorf("window probe: no pool hero found in roster")
	}
	fmt.Printf("probe window: laddering %s (id %d), bracket %s, hub take %d\n",
		heroSlug, heroID, cfg.Scope.Bracket, cfg.Stratz.Take)

	var failed []string

	// winDay family: take is days, capped at 30 server-side
	arms := windowTakeArms(cfg.Stratz.Take)
	winDay := map[int]windowArmStat{}
	for _, tk := range arms {
		a := winDayArm(client, cfg, tk)
		winDay[tk] = a
		printWindowArm("winDay", a)
	}
	if _, ok := winDay[windowCalibrationTake]; !ok {
		a := winDayArm(client, cfg, windowCalibrationTake)
		winDay[windowCalibrationTake] = a
		printWindowArm("winDay", a)
	}
	for _, tk := range arms {
		a, err := positionArm(client, cfg, tk)
		if err != nil {
			return err
		}
		printWindowArm("winDay+POSITION_1", a)
	}
	sat := windowSaturationTake
	printSaturationIdentity("winDay", winDay[sat], winDay[windowOldHubTake])
	ok, reason := winDayVerdict(winDay[cfg.Stratz.Take], winDay[sat], len(heroes), cfg.Stratz.Take)
	fmt.Printf("probe window winDay (popularity, positions, trends source): %s, %s\n", verdictWord(ok), reason)
	if !ok {
		failed = append(failed, "winDay (popularity, positions, trends source)")
	}

	// matchUp family: take is pagination (row cap); the week arg is a
	// calendar-week stamp and the completed week is the pinnable window
	vsArms := map[int]windowArmStat{}
	vsCells := map[int]map[int]int64{}
	for _, tk := range arms {
		vs, with, cells := matchUpArm(client, cfg, heroID, tk)
		vsArms[tk], vsCells[tk] = vs, cells
		printWindowArm("matchUp vs", vs)
		printWindowArm("matchUp with", with)
	}
	printSaturationIdentity("matchUp vs", vsArms[sat], vsArms[windowOldHubTake])
	fmt.Printf("probe window matchUp take semantics: vs rows at take 30 = %d, at take %d = %d (pagination, not days)\n",
		vsArms[sat].Rows, windowOldHubTake, vsArms[windowOldHubTake].Rows)

	calib := winDayHeroArm(client, cfg, heroID, windowCalibrationTake)
	printWindowArm("winDay hero calibration", calib)

	thisWeek, lastWeek, weekBefore := weekStarts(time.Now())
	baseCells := vsCells[windowOldHubTake]
	baseArm := vsArms[windowOldHubTake]
	baseArm.Label = "no-week default"
	printWindowArm("matchUp vs", baseArm)
	weekArms := map[int64]windowArmStat{}
	weekCells := map[int64]map[int]int64{}
	for _, w := range []int64{thisWeek, lastWeek, weekBefore} {
		a, cells := matchUpWeekArm(client, cfg, heroID, w)
		weekArms[w], weekCells[w] = a, cells
		printWindowArm("matchUp vs", a)
	}
	if baseCells != nil {
		for _, w := range []int64{thisWeek, lastWeek, weekBefore} {
			aInB, bInA := cellsSubset(weekCells[w], baseCells)
			fmt.Printf("probe window matchUp cells %s vs default: weekInDefault %v, defaultInWeek %v\n",
				weekArms[w].Label, aInB, bInA)
		}
		aInB, bInA := cellsSubset(weekCells[lastWeek], weekCells[weekBefore])
		fmt.Printf("probe window matchUp cells lastWeek vs weekBefore: lastInBefore %v, beforeInLast %v\n", aInB, bInA)
	}

	ok, reason = calendarWeekVerdict(weekArms[lastWeek], baseArm, calib, float64(cfg.Score.EnemySlots), cfg.Stratz.MinVsRows)
	fmt.Printf("probe window matchUp completed week (matchup cells, hero_prior overall wr): %s, %s\n", verdictWord(ok), reason)
	if !ok {
		failed = append(failed, "matchUp week (matchup cells, hero_prior overall wr)")
	}

	// builds family: minTime/maxTime filter purchase minutes inside a match
	// (minute bounds narrow rows, epoch bounds answer empty), week is the
	// window lever with the same calendar semantics as matchUp
	buildsBaseline := buildsArmAt(client, cfg, heroID, "", "product")
	printWindowArm("itemFullPurchase", buildsBaseline)
	buildsTypes := argTypeNames(env, "itemFullPurchase")
	if isNumericTypeName(buildsTypes["minTime"]) && isNumericTypeName(buildsTypes["maxTime"]) {
		minuteArm := buildsArmAt(client, cfg, heroID, "minTime: 0, maxTime: 10", "minTime 0, maxTime 10 (minute filter check)")
		printWindowArm("itemFullPurchase", minuteArm)
		if minuteArm.Err == nil && minuteArm.Rows > 0 && minuteArm.Rows < buildsBaseline.Rows {
			fmt.Printf("probe window itemFullPurchase minTime/maxTime: purchase-minute filters (rows %d of %d), not a window control\n",
				minuteArm.Rows, buildsBaseline.Rows)
		}
	}
	buildsWeek := buildsArmAt(client, cfg, heroID, fmt.Sprintf("week: %d", lastWeek), fmt.Sprintf("week-start %d", lastWeek))
	printWindowArm("itemFullPurchase", buildsWeek)
	// builds rows have no acceptance floor of their own; the volume band runs
	// against the no-week default, same single-week expectation
	// builds rows carry no per-hero 7d calibration of their own, so the band
	// runs directly against the no-week default (enemySlots 1)
	ok, reason = calendarWeekVerdict(buildsWeek, buildsBaseline, buildsBaseline, 1, 0)
	fmt.Printf("probe window itemFullPurchase completed week: %s, %s\n", verdictWord(ok), reason)
	if !ok {
		days := impliedWindowDays(buildsBaseline.MaxCell, calib.MatchSum, windowCalibrationTake)
		fmt.Printf("probe window itemFullPurchase fallback evidence: implied default span ~%.0f days (heuristic)\n", days)
		failed = append(failed, "itemFullPurchase week")
	}

	if len(failed) > 0 {
		return fmt.Errorf("probe window: families whose window cannot be pinned: %s",
			strings.Join(failed, "; "))
	}
	return nil
}

func isNumericTypeName(name string) bool {
	return name == "Int" || name == "Long" || name == "Int!" || name == "Long!"
}

func verdictWord(ok bool) string {
	if ok {
		return "HONORED"
	}
	return "REFUSED"
}

func printWindowArm(family string, a windowArmStat) {
	if a.Err != nil {
		fmt.Printf("probe window %s %s: ERROR %s\n", family, a.Label, shortErr(a.Err))
		return
	}
	fmt.Printf("probe window %s %s: %d rows, volume %d\n", family, a.Label, a.Rows, a.MatchSum)
}

func printSaturationIdentity(family string, at30, at200 windowArmStat) {
	if at30.Err != nil || at200.Err != nil {
		fmt.Printf("probe window %s saturation identity: arm error, unchecked\n", family)
		return
	}
	same := at30.Rows == at200.Rows && at30.MatchSum == at200.MatchSum
	fmt.Printf("probe window %s saturation identity take30==take200: %v (%d/%d vs %d/%d)\n",
		family, same, at30.Rows, at30.MatchSum, at200.Rows, at200.MatchSum)
}
