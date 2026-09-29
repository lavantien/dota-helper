package ingest

import (
	"encoding/json"
	"io"
	"os"
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

// probe body excerpts are raw hostile bytes: shortBody must neutralize them
// and keep its 600-byte cap
func TestShortBodyNeutralizesAndCaps(t *testing.T) {
	got := shortBody([]byte("\x1b[2Khead"))
	if want := `\x1b[2Khead`; got != want {
		t.Fatalf("shortBody = %q, want %q", got, want)
	}
	long := shortBody([]byte(strings.Repeat("y", 650)))
	if !strings.HasPrefix(long, strings.Repeat("y", 600)) || !strings.HasSuffix(long, "...") {
		t.Fatalf("cap missing: %d bytes, tail %q", len(long), long[len(long)-3:])
	}
	for _, r := range long {
		if isControlRune(r) {
			t.Fatalf("control rune %q survived: %q", r, long)
		}
	}
}

// the introspection snapshot prints every schema name with %v/%s: names are
// remote text, so decoding a hostile 200 snapshot must leave no control rune
// in any of them (json \uXXXX escapes decode into real C0/C1 bytes)
func TestIntrospectionNamesNeutralizedAtDecode(t *testing.T) {
	body := []byte(`{"sch":{"queryType":{"name":"Q\u0000t","fields":[{"name":"matches\u001b[2K",` +
		`"args":[{"name":"take\n","type":{"kind":"LIST","name":null,"ofType":{"kind":"SCALAR","name":"Int\u009b"}}}],` +
		`"type":{"kind":"OBJECT","name":"Match\u0007Type"}}]},"types":[{"name":"T\u0007","fields":[],"inputFields":[],` +
		`"enumValues":[{"name":"A\u001b"}]}]}}`)
	var env introEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	qt := env.Sch.QueryType
	names := map[string]string{
		"queryType name":  qt.Name,
		"field name":      qt.Fields[0].Name,
		"arg name":        qt.Fields[0].Args[0].Name,
		"ofType name":     qt.Fields[0].Args[0].Type.OfType.Name,
		"ofType kind":     qt.Fields[0].Args[0].Type.OfType.Kind,
		"field type name": qt.Fields[0].Type.Name,
		"type name":       env.Sch.Types[0].Name,
		"enum value":      env.Sch.Types[0].EnumValues[0].Name,
		"displayName()":   qt.Fields[0].Args[0].Type.OfType.displayName(),
	}
	for where, s := range names {
		for _, r := range s {
			if isControlRune(r) {
				t.Fatalf("%s kept control rune %q: %q", where, r, s)
			}
		}
	}
}

// a json.RawMessage field can carry a C1 control as valid UTF-8 (0xc2 0x9b),
// which survives MarshalIndent: the sample-match print must neutralize it
// while keeping MarshalIndent's own structural newlines
func TestPrintSampleMatchNeutralizesControls(t *testing.T) {
	m := probeMatch{ID: 7, GameMode: json.RawMessage(`"GAME_MODE_` + "\u009b" + `"`)}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printSampleMatch(m)
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `GAME_MODE_\u009b`) {
		t.Fatalf("C1 control was not escaped in the sample print: %q", string(out))
	}
	for _, c := range string(out) {
		if c != '\n' && isControlRune(c) {
			t.Fatalf("control rune %q survived the sample print: %q", c, string(out))
		}
	}
}
