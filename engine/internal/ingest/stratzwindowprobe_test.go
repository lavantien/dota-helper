package ingest

import (
	"errors"
	"testing"
	"time"
)

// the winDay family is honored at a sub-30 hub take only when the row count
// equals the fixed day-bucket universe and the totals strictly shrink below
// the saturated 30-day arm
func TestWinDayVerdictHonoredWhenShrunk(t *testing.T) {
	hub := windowArmStat{Rows: 889, MatchSum: 900_000}
	sat := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	ok, reason := winDayVerdict(hub, sat, 127, 7)
	if !ok {
		t.Fatalf("shrunk arm must be honored, reason: %s", reason)
	}
}

func TestWinDayVerdictRefusesSaturation(t *testing.T) {
	hub := windowArmStat{Rows: 889, MatchSum: 3_800_000}
	sat := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	if ok, reason := winDayVerdict(hub, sat, 127, 7); ok {
		t.Fatal("totals equal to the 30-day arm are silent saturation")
	} else if reason == "" {
		t.Fatal("refusal must carry a reason")
	}
}

func TestWinDayVerdictRefusesUniverseMismatch(t *testing.T) {
	sat := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	// take ignored server-side: 30 buckets per hero still served
	overflow := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	if ok, _ := winDayVerdict(overflow, sat, 127, 7); ok {
		t.Fatal("rows above the roster x take universe mean take is not capping buckets")
	}
	// server shrinks the window under the claimed take: fewer buckets served
	shrunk := windowArmStat{Rows: 635, MatchSum: 700_000}
	if ok, _ := winDayVerdict(shrunk, sat, 127, 7); ok {
		t.Fatal("rows below the roster x take universe mean the window silently shrank")
	}
}

func TestWinDayVerdictRefusesUnpinnedCap(t *testing.T) {
	hub := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	sat := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	if ok, _ := winDayVerdict(hub, sat, 127, 200); ok {
		t.Fatal("a hub take at or above the 30-day cap pins nothing, the exact pre-pin defect")
	}
}

func TestWinDayVerdictPropagatesArmError(t *testing.T) {
	hub := windowArmStat{Err: errors.New("boom")}
	sat := windowArmStat{Rows: 3810, MatchSum: 3_800_000}
	if ok, _ := winDayVerdict(hub, sat, 127, 7); ok {
		t.Fatal("an errored arm must refuse")
	}
}

// the completed-week lever is honored when the arm answers over the floor,
// differs from the ignored in-progress-week shape, and its volume sits in
// the single-week band against the 7-day calibration
func TestCalendarWeekVerdictHonored(t *testing.T) {
	arm := windowArmStat{Rows: 124, MatchSum: 32_585}
	base := windowArmStat{Rows: 126, MatchSum: 21_905}
	calib := windowArmStat{Rows: 7, MatchSum: 3_639}
	if ok, reason := calendarWeekVerdict(arm, base, calib, 5, 100); !ok {
		t.Fatalf("a full answered week in band must be honored, reason: %s", reason)
	}
}

func TestCalendarWeekVerdictRefusesIgnoredInProgressWeek(t *testing.T) {
	calib := windowArmStat{Rows: 7, MatchSum: 3_639}
	base := windowArmStat{Rows: 126, MatchSum: 21_905}
	arm := windowArmStat{Rows: 126, MatchSum: 21_905}
	if ok, _ := calendarWeekVerdict(arm, base, calib, 5, 100); ok {
		t.Fatal("identical to the default means the server ignored the week value")
	}
	// a match landing between the two live calls must not defeat detection
	drifted := windowArmStat{Rows: 126, MatchSum: 21_908}
	if ok, _ := calendarWeekVerdict(drifted, base, calib, 5, 100); ok {
		t.Fatal("volume within drift tolerance of the default still reads as ignored")
	}
}

func TestCalendarWeekVerdictRefusesStarvedAndBloatedWeeks(t *testing.T) {
	base := windowArmStat{Rows: 126, MatchSum: 21_905}
	calib := windowArmStat{Rows: 7, MatchSum: 3_639}
	starved := windowArmStat{Rows: 124, MatchSum: 4_000}
	if ok, _ := calendarWeekVerdict(starved, base, calib, 5, 100); ok {
		t.Fatal("a volume ratio under half a week must refuse")
	}
	bloated := windowArmStat{Rows: 126, MatchSum: 200_000}
	if ok, _ := calendarWeekVerdict(bloated, base, calib, 5, 100); ok {
		t.Fatal("a volume ratio over two and a half weeks must refuse")
	}
}

func TestCalendarWeekVerdictRefusesFloorCollapse(t *testing.T) {
	arm := windowArmStat{Rows: 12, MatchSum: 30_000}
	base := windowArmStat{Rows: 126, MatchSum: 21_905}
	calib := windowArmStat{Rows: 7, MatchSum: 3_639}
	if ok, _ := calendarWeekVerdict(arm, base, calib, 5, 100); ok {
		t.Fatal("rows below the acceptance floor must refuse")
	}
}

func TestWeekStartsAreConsecutiveMondays(t *testing.T) {
	// 2026-09-29 is a Tuesday: the in-progress week started Monday 2026-09-28
	thisW, lastW, beforeW := weekStarts(time.Date(2026, 9, 29, 15, 4, 5, 0, time.UTC))
	if thisW != time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("this week must start Monday, got %d", thisW)
	}
	if lastW != thisW-7*86400 || beforeW != lastW-7*86400 {
		t.Fatalf("weeks must be 7 days apart: %d %d %d", thisW, lastW, beforeW)
	}
	// mid-week anchor: Thursday 2026-10-01 still resolves the same Monday
	this2, _, _ := weekStarts(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC))
	if this2 != thisW {
		t.Fatalf("mid-week resolution must find the same Monday: %d vs %d", this2, thisW)
	}
}

func TestCellsSubset(t *testing.T) {
	nested := map[int]int64{1: 10, 2: 20}
	outer := map[int]int64{1: 30, 2: 20, 3: 5}
	aInB, bInA := cellsSubset(nested, outer)
	if !aInB || bInA {
		t.Fatalf("nested containment must read one way: %v %v", aInB, bInA)
	}
	sliceA := map[int]int64{1: 10, 2: 0}
	sliceB := map[int]int64{1: 5, 3: 7}
	aInB, bInA = cellsSubset(sliceA, sliceB)
	if aInB || bInA {
		t.Fatalf("overlapping calendar slices are incomparable: %v %v", aInB, bInA)
	}
	if aInB, bInA := cellsSubset(nested, nested); !aInB || !bInA {
		t.Fatalf("equal maps are subsets both ways: %v %v", aInB, bInA)
	}
}

func TestImpliedWindowDays(t *testing.T) {
	// the most-purchased cell covers ~4x the hub-window position volume: the
	// server aggregate spans roughly 4x the pinned window, labeled a heuristic
	if d := impliedWindowDays(400_000, 100_000, 7); d < 27.9 || d > 28.1 {
		t.Fatalf("implied days = %v, want 28", d)
	}
	if d := impliedWindowDays(400_000, 0, 7); d != 0 {
		t.Fatalf("a zero reference volume carries no estimate, got %v", d)
	}
}

func TestWindowTakeArmsDedupSorted(t *testing.T) {
	if got := windowTakeArms(7); len(got) != 3 || got[0] != 7 || got[1] != 30 || got[2] != 200 {
		t.Fatalf("arms(7) = %v, want [7 30 200]", got)
	}
	if got := windowTakeArms(30); len(got) != 2 || got[0] != 30 || got[1] != 200 {
		t.Fatalf("arms(30) = %v, want [30 200]", got)
	}
	if got := windowTakeArms(200); len(got) != 2 || got[0] != 30 || got[1] != 200 {
		t.Fatalf("arms(200) = %v, want [30 200]", got)
	}
}
