package emit

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"poolguide/internal/config"
)

// The committed artifacts and the hub inputs sit at fixed offsets from the
// package directory: engine/internal/emit -> repo root.
const (
	hubPath    = "../../../config.json"
	prosePath  = "../../../content.json"
	dataJSPath = "../../../guide/data.js"
)

// committedEnemies parses the heatmapEnemies block out of the committed
// data.js, so the tests pin formatting against the real file shape without
// depending on live var/ state.
func committedEnemies(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(dataJSPath)
	if err != nil {
		t.Fatalf("read committed data.js: %v", err)
	}
	s := string(raw)
	start := strings.Index(s, "  heatmapEnemies: [\n")
	if start < 0 {
		t.Fatalf("committed data.js has no heatmapEnemies block")
	}
	block := s[start:]
	end := strings.Index(block, "\n  ],")
	if end < 0 {
		t.Fatalf("committed data.js heatmapEnemies block never closes")
	}
	var out []string
	for _, line := range strings.Split(block[:end], "\n")[1:] {
		e := strings.TrimSuffix(strings.TrimSpace(line), ",")
		out = append(out, strings.Trim(e, "'"))
	}
	return out
}

func TestDataJSMatchesCommittedFile(t *testing.T) {
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	got, err := DataJS(cfg, prose, committedEnemies(t))
	if err != nil {
		t.Fatalf("DataJS: %v", err)
	}
	want, err := os.ReadFile(dataJSPath)
	if err != nil {
		t.Fatalf("read committed data.js: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("DataJS differs from committed data.js\nwant %d bytes, got %d bytes\nfirst divergence:\n%s",
			len(want), len(got), firstDivergence(string(want), string(got)))
	}
}

// firstDivergence reports the first differing line pair of two rendered docs.
func firstDivergence(want, got string) string {
	wl := splitLines(want)
	gl := splitLines(got)
	n := max(len(wl), len(gl))
	for i := 0; i < n; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return "line " + strconv.Itoa(i+1) + "\n  want: " + strconv.Quote(w) + "\n  got:  " + strconv.Quote(g)
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func TestDataJSEmitsPickerSection(t *testing.T) {
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	if prose.Picker.Intro == "" {
		t.Fatalf("content.json picker.intro is empty")
	}
	if len(prose.Picker.Cards) == 0 {
		t.Fatalf("content.json picker.cards is empty")
	}
	for _, c := range prose.Picker.Cards {
		if c.Name == "" || c.Body == "" {
			t.Fatalf("picker card with empty name or body")
		}
	}
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got, err := DataJS(cfg, prose, committedEnemies(t))
	if err != nil {
		t.Fatalf("DataJS: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "\n  picker: {\n") {
		t.Fatalf("DataJS output has no picker section")
	}
	for _, c := range prose.Picker.Cards {
		if !strings.Contains(s, quoteJS(c.Name)) {
			t.Fatalf("picker card %q missing from output", c.Name)
		}
	}
}

func TestDataJSDeterministic(t *testing.T) {
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	enemies := committedEnemies(t)
	a, err := DataJS(cfg, prose, enemies)
	if err != nil {
		t.Fatalf("DataJS first run: %v", err)
	}
	b, err := DataJS(cfg, prose, enemies)
	if err != nil {
		t.Fatalf("DataJS second run: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("DataJS is not deterministic across runs")
	}
}

func TestDataJSRejectsBadEnemyList(t *testing.T) {
	cfg, err := config.Load(hubPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	prose, err := LoadProse(prosePath)
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	enemies := committedEnemies(t)
	if _, err := DataJS(cfg, prose, nil); err == nil {
		t.Fatalf("expected an error for an empty enemies list")
	}
	short := enemies[:len(enemies)-1]
	if _, err := DataJS(cfg, prose, short); err == nil {
		t.Fatalf("expected an error when the enemies list is shorter than hub enemyCount")
	}
}

func TestQuoteJSEscapes(t *testing.T) {
	cases := map[string]string{
		"plain":       "'plain'",
		"it's":        `'it\'s'`,
		"back\\slash": `'back\\slash'`,
		"tab\there":   "'tab\\there'",
	}
	for in, want := range cases {
		if got := quoteJS(in); got != want {
			t.Errorf("quoteJS(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFmtNumbers(t *testing.T) {
	if got := fmtShort(45.0); got != "45" {
		t.Errorf("fmtShort(45) = %s", got)
	}
	if got := fmtShort(51.86); got != "51.86" {
		t.Errorf("fmtShort(51.86) = %s", got)
	}
	if got := fmtFixed(0.5, 4); got != "0.5000" {
		t.Errorf("fmtFixed(0.5, 4) = %s", got)
	}
}
