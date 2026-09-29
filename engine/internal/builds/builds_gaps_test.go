package builds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"poolguide/internal/config"
	"poolguide/internal/ingest"
)

// writeManifest stamps the current completed week so the freshness gate
// passes; series is overridable per case.
func writeManifest(t *testing.T, dir, series string, week int64) {
	t.Helper()
	mb, err := json.Marshal(rawManifest{
		CrawledAt: "2026-09-25T00:00:00Z", Bracket: "DIVINE_IMMORTAL",
		Field: "itemFullPurchase", Series: series, Week: week,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_crawl.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeRawFixture lays down one floor-passing zeta@1 dump, the items anchor,
// and a current manifest; the content file is the caller's concern.
func writeRawFixture(t *testing.T, dir string) {
	t.Helper()
	mk := func(id int, short string, cost int) rawItem {
		it := rawItem{ID: id, ShortName: short}
		it.Stat.Cost = cost
		return it
	}
	ib, err := json.Marshal(rawItemsFile{Items: []rawItem{
		mk(12, "manta", 4650),
		mk(15, "heart", 5300),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_items.json"), ib, 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, "7.41", ingest.LastCompletedWeekStart(time.Now()))
	db, err := json.Marshal(rawEntry{Rows: []rawRow{
		{ItemID: 12, Time: 20, MatchCount: 800},
		{ItemID: 15, Time: 34, MatchCount: 300},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zeta@1.json"), db, 0o644); err != nil {
		t.Fatal(err)
	}
}

// runCfg wires a one-hero pool at the fixture dir.
func runCfg(dir string, contentName string) *config.Config {
	cfg := testCfg()
	cfg.Patch = "7.41f"
	cfg.Paths.BuildsRawDir = dir
	cfg.Paths.ContentPath = filepath.Join(dir, contentName)
	cfg.Pool = []config.PoolEntry{{Slug: "zeta", Name: "Zeta", Role: "1", Tier: "dedicated"}}
	return cfg
}

func TestRunFailureArms(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		content string // raw content.json bytes, "" for none
		want    string
	}{
		{
			name: "manifest missing",
			want: "builds manifest",
		},
		{
			name: "series mismatch",
			setup: func(t *testing.T, dir string) {
				writeManifest(t, dir, "6.88", ingest.LastCompletedWeekStart(time.Now()))
			},
			want: "does not match config series",
		},
		{
			name: "items anchor missing",
			setup: func(t *testing.T, dir string) {
				writeManifest(t, dir, "7.41", ingest.LastCompletedWeekStart(time.Now()))
			},
			want: "builds items anchor",
		},
		{
			name: "hero dump missing",
			setup: func(t *testing.T, dir string) {
				writeRawFixture(t, dir)
				if err := os.Remove(filepath.Join(dir, "zeta@1.json")); err != nil {
					t.Fatal(err)
				}
			},
			want: "builds dump missing for zeta@1",
		},
		{
			name: "every role below floors",
			setup: func(t *testing.T, dir string) {
				writeRawFixture(t, dir)
				thin, err := json.Marshal(rawEntry{Rows: []rawRow{{ItemID: 12, Time: 20, MatchCount: 5}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "zeta@1.json"), thin, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "no pooled role passed the floors",
		},
		{
			name: "sync path occupied by a directory",
			setup: func(t *testing.T, dir string) {
				writeRawFixture(t, dir)
				if err := os.Mkdir(filepath.Join(dir, "_sync.json"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			content: testContent,
			want:    "_sync.json",
		},
		{
			name:    "content file missing",
			setup:   func(t *testing.T, dir string) { writeRawFixture(t, dir) },
			content: "",
			want:    "content.json",
		},
		{
			name:    "content file not json",
			setup:   func(t *testing.T, dir string) { writeRawFixture(t, dir) },
			content: "not json at all",
			want:    "invalid character",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			contentName := "content.json"
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(dir, contentName), []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := Run(runCfg(dir, contentName))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// the second run over an already-synced content.json takes the no-change
// early return instead of rewriting the file
func TestRunSecondPassLeavesCurrentContentAlone(t *testing.T) {
	dir := t.TempDir()
	writeRawFixture(t, dir)
	contentPath := filepath.Join(dir, "content.json")
	if err := os.WriteFile(contentPath, []byte(testContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(runCfg(dir, "content.json")); err != nil {
		t.Fatal(err)
	}
	once, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(runCfg(dir, "content.json")); err != nil {
		t.Fatalf("second run: %v", err)
	}
	twice, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Fatalf("a current content.json was rewritten:\n%s\n---\n%s", once, twice)
	}
}

func TestDeriveEntryEdgeBranches(t *testing.T) {
	ix := testIndex()
	cases := []struct {
		name string
		cfg  func(c *config.Config)
		rows []rawRow
		want []string
	}{
		{
			// a zero-sample item survives only when the floor drops to 0, and
			// its average minute reads 0 rather than NaN
			name: "zero matches with zero floor",
			cfg:  func(c *config.Config) { c.Builds.MinMatches = 0 },
			rows: []rawRow{{ItemID: 12, Time: 20, MatchCount: 0}},
			want: []string{"manta"},
		},
		{
			// popularity ties rank id-ascending: manta 12 before heart 15,
			// then the chronological tiebreaks: equal averages order by
			// matches descending (blink 300 under the 500s), equal matches
			// order by id (manta before heart)
			name: "ties across both sorts",
			rows: []rawRow{
				{ItemID: 15, Time: 20, MatchCount: 500},
				{ItemID: 12, Time: 20, MatchCount: 500},
				{ItemID: 1, Time: 20, MatchCount: 300},
			},
			want: []string{"manta", "heart", "blink"},
		},
		{
			name: "topN truncation",
			cfg:  func(c *config.Config) { c.Builds.TopN = 3 },
			rows: []rawRow{
				{ItemID: 12, Time: 20, MatchCount: 800},
				{ItemID: 15, Time: 34, MatchCount: 700},
				{ItemID: 1, Time: 14, MatchCount: 600},
				{ItemID: 11, Time: 22, MatchCount: 500},
			},
			want: []string{"blink", "manta", "heart"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testCfg()
			if tc.cfg != nil {
				tc.cfg(cfg)
			}
			got, err := deriveEntry(tc.rows, ix, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got.Tokens, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("tokens = %v, want %v", got.Tokens, tc.want)
			}
		})
	}
	// the zero-sample entry's timing minute lands on 0, not a division result
	cfg := testCfg()
	cfg.Builds.MinMatches = 0
	got, err := deriveEntry([]rawRow{{ItemID: 12, Time: 20, MatchCount: 0}}, ix, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Timings[0].Minute != 0 {
		t.Errorf("zero-sample minute = %d, want 0", got.Timings[0].Minute)
	}
}
