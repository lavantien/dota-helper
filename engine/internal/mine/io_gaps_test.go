package mine

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedRawTables fills all four raw tables with one valid row set each; the
// mine columns (win_count, synergy, wr) come from EnsureSchema.
func seedRawTables(t *testing.T) *sql.DB {
	t.Helper()
	db := openFixtureDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES
		(3, 'axe', 'Axe', 'axe', 'test'),
		(7, 'phantom-lancer', 'Phantom Lancer', 'phantom_lancer', 'test')`)
	exec(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, synergy, matches, source, scrape_date, confidence)
		VALUES ('axe', 'ursa', 2.0, 60, 'stratz_vs', '2026-09-24', 'high')`)
	exec(t, db, `INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
		VALUES ('axe', 'ursa', 50, 1.0, 'stratz')`)
	exec(t, db, `INSERT INTO hero_stats (slug, bracket, pick_count, win_count, source, scrape_date)
		VALUES ('axe', 'ALL', 100, 50, 'stratz', '2026-09-24')`)
	return db
}

// each raw table has a query-failure arm (missing table) and a scan-failure
// arm (a NOT NULL column relaxed so a corrupt row can land), and the loader
// must fail loudly on both rather than half-load
func TestLoadRawErrorArms(t *testing.T) {
	cases := []struct {
		name string
		brk  func(t *testing.T, db *sql.DB)
		want string
	}{
		{
			name: "roster table missing",
			brk:  func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE hero_roster`) },
			want: "hero_roster:",
		},
		{
			name: "roster row with null slug",
			brk: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE hero_roster ALTER COLUMN slug DROP NOT NULL`)
				exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source)
					VALUES (99, NULL, 'Corrupt', 'corrupt', 'test')`)
			},
			want: "Scan",
		},
		{
			name: "matchup table missing",
			brk:  func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE matchup_raw`) },
			want: "matchup_raw:",
		},
		{
			name: "matchup row with null matches",
			brk: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE matchup_raw ALTER COLUMN matches DROP NOT NULL`)
				exec(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, matches, source, scrape_date, confidence)
					VALUES ('axe', 'lina', NULL, 'stratz_vs', '2026-09-24', 'high')`)
			},
			want: "Scan",
		},
		{
			name: "synergy table missing",
			brk:  func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE synergy_raw`) },
			want: "synergy_raw:",
		},
		{
			name: "synergy row with null match count",
			brk: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE synergy_raw ALTER COLUMN match_count DROP NOT NULL`)
				exec(t, db, `INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
					VALUES ('axe', 'lina', NULL, 1.0, 'stratz')`)
			},
			want: "Scan",
		},
		{
			name: "stats table missing",
			brk:  func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE hero_stats`) },
			want: "hero_stats:",
		},
		{
			name: "stats row with null picks",
			brk: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE hero_stats ALTER COLUMN pick_count DROP NOT NULL`)
				exec(t, db, `INSERT INTO hero_stats (slug, bracket, pick_count, win_count, source, scrape_date)
					VALUES ('lina', 'ALL', NULL, 5, 'stratz', '2026-09-24')`)
			},
			want: "Scan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := seedRawTables(t)
			tc.brk(t, db)
			if _, err := loadRaw(db); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadRaw error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	db := seedRawTables(t)
	raw, err := loadRaw(db)
	if err != nil {
		t.Fatalf("unbroken seed: %v", err)
	}
	if len(raw.roster) != 2 || len(raw.matchups) != 1 || len(raw.synergies) != 1 || len(raw.stats) != 1 {
		t.Fatalf("seed loads incompletely: %+v", raw)
	}
}

func TestLoadTrendRowsArms(t *testing.T) {
	db := openFixtureDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	exec(t, db, `INSERT INTO hero_trend (snapshot_date, slug, pick_share, wr, source) VALUES
		('2026-09-20', 'axe', 0.10, 0.50, 'stratz_winday'),
		('2026-09-27', 'axe', 0.12, 0.51, 'stratz_winday')`)
	rows, err := loadTrendRows(db)
	if err != nil {
		t.Fatalf("load trend rows: %v", err)
	}
	if len(rows) != 2 || rows[0].Slug != "axe" {
		t.Fatalf("trend rows = %+v, want both axe snapshots", rows)
	}
	exec(t, db, `ALTER TABLE hero_trend ALTER COLUMN wr DROP NOT NULL`)
	exec(t, db, `INSERT INTO hero_trend VALUES ('2026-09-27', 'lina', 0.2, NULL, 'stratz_winday')`)
	if _, err := loadTrendRows(db); err == nil || !strings.Contains(err.Error(), "Scan") {
		t.Fatalf("null wr must fail the scan, got %v", err)
	}
	exec(t, db, `DROP TABLE hero_trend`)
	if _, err := loadTrendRows(db); err == nil || !strings.Contains(err.Error(), "hero_trend:") {
		t.Fatalf("missing trend table must fail the query, got %v", err)
	}
}

func TestLoadFallbackPopArms(t *testing.T) {
	cfg := testCfg(t)
	newDb := func(t *testing.T) *sql.DB {
		t.Helper()
		db := openFixtureDB(t)
		if err := EnsureSchema(db); err != nil {
			t.Fatalf("ensure schema: %v", err)
		}
		exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source)
			VALUES (3, 'axe', 'Axe', 'axe', 'test')`)
		return db
	}
	valid := `{"rows": [{"heroId": 3, "matchCount": 30, "winCount": 15}]}`
	cases := []struct {
		name string
		brk  func(t *testing.T, db *sql.DB, root string)
		want string
	}{
		{
			name: "popularity file is a directory",
			brk: func(t *testing.T, db *sql.DB, root string) {
				if err := os.Mkdir(filepath.Join(root, cfg.Paths.StratzRawDir, fallbackPopFile), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "popularity.json",
		},
		{
			name: "popularity file is not json",
			brk: func(t *testing.T, db *sql.DB, root string) {
				writeFallbackFile(t, cfg, root, `{"rows": [`)
			},
			want: "unexpected end of JSON input",
		},
		{
			name: "roster table missing",
			brk: func(t *testing.T, db *sql.DB, root string) {
				writeFallbackFile(t, cfg, root, valid)
				exec(t, db, `DROP TABLE hero_roster`)
			},
			want: "hero_roster",
		},
		{
			name: "roster row with null slug",
			brk: func(t *testing.T, db *sql.DB, root string) {
				writeFallbackFile(t, cfg, root, valid)
				exec(t, db, `ALTER TABLE hero_roster ALTER COLUMN slug DROP NOT NULL`)
				exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source)
					VALUES (99, NULL, 'Corrupt', 'corrupt', 'test')`)
			},
			want: "Scan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newDb(t)
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, cfg.Paths.StratzRawDir), 0o755); err != nil {
				t.Fatal(err)
			}
			tc.brk(t, db, root)
			_, err := loadFallbackPop(db, root, cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadFallbackPop error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	// every row resolving to an unknown hero id leaves no fallback at all
	root := t.TempDir()
	writeFallbackFile(t, cfg, root, `{"rows": [{"heroId": 99, "matchCount": 10}]}`)
	fallback, err := loadFallbackPop(newDb(t), root, cfg)
	if err != nil {
		t.Fatalf("unknown-id fallback: %v", err)
	}
	if len(fallback) != 0 {
		t.Fatalf("unknown ids must disable the fallback, got %v", fallback)
	}
}

func TestJoinSourcesPacksCanonicalLabels(t *testing.T) {
	cases := []struct {
		srcs []string
		want string
	}{
		{nil, ""},
		{[]string{"stratz"}, "stratz"},
		{[]string{"opendota_explorer", "stratz"}, "opendota_explorer+stratz"},
		{[]string{"a", "b", "c"}, "a+b+c"},
	}
	for _, c := range cases {
		if got := joinSources(c.srcs); got != c.want {
			t.Errorf("joinSources(%v) = %q, want %q", c.srcs, got, c.want)
		}
	}
}
