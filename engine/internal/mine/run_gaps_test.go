package mine

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/store"
)

// seedRunFixture builds the smallest db a full Run derives from: a 3-hero
// roster, one matchup pair, one single-direction synergy, in-bracket stats
// for every roster hero, and a two-date trend for the first pool hero.
func seedRunFixture(t *testing.T, cfg *config.Config) *sql.DB {
	t.Helper()
	cfg.Heatmap.EnemyCount = 3
	db := openFixtureDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	exec(t, db, `INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES
		(2, 'spectre', 'Spectre', 'spectre', 'test'),
		(3, 'axe', 'Axe', 'axe', 'test'),
		(7, 'phantom-lancer', 'Phantom Lancer', 'phantom_lancer', 'test')`)
	exec(t, db, `INSERT INTO matchup_raw (pool_slug, enemy_slug, synergy, matches, source, scrape_date, confidence)
		VALUES ('spectre', 'axe', -3.0, 40, 'stratz_vs', '2026-09-24', 'high')`)
	exec(t, db, `INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
		VALUES ('spectre', 'phantom-lancer', 100, 3.0, 'stratz')`)
	exec(t, db, `INSERT INTO hero_stats (slug, bracket, pick_count, win_count, source, scrape_date)
		VALUES ('spectre', ?, 1000, 550, 'stratz', '2026-09-24'),
		('axe', ?, 500, 250, 'stratz', '2026-09-24'),
		('phantom-lancer', ?, 200, 90, 'stratz', '2026-09-24')`,
		cfg.Scope.Bracket, cfg.Scope.Bracket, cfg.Scope.Bracket)
	exec(t, db, `INSERT INTO hero_trend (snapshot_date, slug, pick_share, wr, source) VALUES
		('2026-09-20', ?, 0.10, 0.50, 'stratz_winday'),
		('2026-09-27', ?, 0.12, 0.51, 'stratz_winday')`, cfg.PoolSlugs()[0], cfg.PoolSlugs()[0])
	return db
}

// writeFallbackFile plants the raw aggregate popularity file for a repo root.
func writeFallbackFile(t *testing.T, cfg *config.Config, root string, body string) {
	t.Helper()
	dir := filepath.Join(root, cfg.Paths.StratzRawDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fallbackPopFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunDefaultsClockWithoutNow(t *testing.T) {
	cfg := testCfg(t)
	db := seedRunFixture(t, cfg)
	if err := Run(Deps{DB: db, Cfg: cfg, RepoRoot: t.TempDir()}); err != nil {
		t.Fatalf("run without Now: %v", err)
	}
	assertCount(t, db, "SELECT count(*) FROM hero_prior", len(cfg.PoolSlugs()))
}

// a read-only db must fail the mine schema step loudly, both when EnsureSchema
// is called directly and through Run
func TestEnsureSchemaRejectsReadOnlyDb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.duckdb")
	rw, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rw.Close()
	ro, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close() })
	if err := EnsureSchema(ro); err == nil {
		t.Fatal("EnsureSchema accepted a read-only database")
	}
	cfg := testCfg(t)
	err = Run(Deps{DB: ro, Cfg: cfg, RepoRoot: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "mine schema") {
		t.Fatalf("Run on a read-only db = %v, want the mine schema failure", err)
	}
}

func TestRunFailureArms(t *testing.T) {
	cases := []struct {
		name string
		prep func(t *testing.T, cfg *config.Config, db *sql.DB, root string)
		want string
	}{
		{
			name: "raw roster table missing",
			prep: func(t *testing.T, cfg *config.Config, db *sql.DB, root string) {
				exec(t, db, `DROP TABLE hero_roster`)
			},
			want: "hero_roster:",
		},
		{
			name: "fallback file corrupt",
			prep: func(t *testing.T, cfg *config.Config, db *sql.DB, root string) {
				writeFallbackFile(t, cfg, root, `{"rows": [`)
			},
			want: "unexpected end of JSON input",
		},
		{
			name: "heatmap coverage short of column count",
			prep: func(t *testing.T, cfg *config.Config, db *sql.DB, root string) {
				cfg.Heatmap.EnemyCount = 99
			},
			want: "popularity covers",
		},
		{
			name: "trend table missing",
			prep: func(t *testing.T, cfg *config.Config, db *sql.DB, root string) {
				exec(t, db, `DROP TABLE hero_trend`)
			},
			want: "hero_trend:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testCfg(t)
			db := seedRunFixture(t, cfg)
			root := t.TempDir()
			tc.prep(t, cfg, db, root)
			err := Run(Deps{DB: db, Cfg: cfg, RepoRoot: root, Now: fixedNow})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// every persist write has its own error arm: the clear loop, each prepared
// insert (binder failures via dropped columns), each row insert (constraint
// failures via checks), and the two provenance writes
func TestRunPersistFailureArms(t *testing.T) {
	withCheck := func(table, ddl string) func(t *testing.T, db *sql.DB) {
		return func(t *testing.T, db *sql.DB) {
			exec(t, db, "DROP TABLE "+table)
			exec(t, db, ddl)
		}
	}
	cases := []struct {
		name    string
		corrupt func(t *testing.T, db *sql.DB)
		want    string
	}{
		{
			name:    "clear loop",
			corrupt: func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE pair_shrunk`) },
			want:    "clear pair_shrunk",
		},
		{
			name: "pair insert binder",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE pair_shrunk DROP COLUMN value`)
			},
			want: "pair_shrunk:",
		},
		{
			name: "pair insert constraint",
			corrupt: withCheck("pair_shrunk", `CREATE TABLE pair_shrunk (
				hero VARCHAR NOT NULL, other VARCHAR NOT NULL, kind VARCHAR NOT NULL,
				value DOUBLE NOT NULL, n BIGINT NOT NULL, post_var DOUBLE NOT NULL,
				source VARCHAR NOT NULL, confidence VARCHAR NOT NULL,
				PRIMARY KEY (hero, other, kind), CHECK (kind = 'nope'))`),
			want: "pair_shrunk spectre/axe",
		},
		{
			// the enemy pass runs first, so the ally write only surfaces when
			// the enemy family is empty and the ally cells hit the break alone
			name: "ally pair insert constraint",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `DELETE FROM matchup_raw`)
				exec(t, db, "DROP TABLE pair_shrunk")
				exec(t, db, `CREATE TABLE pair_shrunk (
					hero VARCHAR NOT NULL, other VARCHAR NOT NULL, kind VARCHAR NOT NULL,
					value DOUBLE NOT NULL, n BIGINT NOT NULL, post_var DOUBLE NOT NULL,
					source VARCHAR NOT NULL, confidence VARCHAR NOT NULL,
					PRIMARY KEY (hero, other, kind), CHECK (kind = 'nope'))`)
			},
			want: "pair_shrunk spectre/phantom-lancer",
		},
		{
			name: "norm insert binder",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE norm_cell DROP COLUMN pct`)
			},
			want: "norm_cell:",
		},
		{
			name: "norm insert constraint",
			corrupt: withCheck("norm_cell", `CREATE TABLE norm_cell (
				hero VARCHAR NOT NULL, other VARCHAR NOT NULL, kind VARCHAR NOT NULL,
				pct DOUBLE NOT NULL, z DOUBLE NOT NULL, completed BOOLEAN NOT NULL,
				PRIMARY KEY (hero, other, kind), CHECK (kind = 'nope'))`),
			want: "norm_cell ",
		},
		{
			name: "popularity insert binder",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE popularity DROP COLUMN pick_share`)
			},
			want: "popularity:",
		},
		{
			name: "popularity insert constraint",
			corrupt: withCheck("popularity", `CREATE TABLE popularity (
				slug VARCHAR NOT NULL PRIMARY KEY, pick_share DOUBLE NOT NULL,
				source VARCHAR NOT NULL, CHECK (slug = 'nope'))`),
			want: "popularity axe",
		},
		{
			name: "prior insert binder",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE hero_prior DROP COLUMN tier`)
			},
			want: "hero_prior:",
		},
		{
			name: "prior insert constraint",
			corrupt: withCheck("hero_prior", `CREATE TABLE hero_prior (
				slug VARCHAR NOT NULL PRIMARY KEY, wr DOUBLE NOT NULL,
				tier VARCHAR NOT NULL, value DOUBLE NOT NULL, CHECK (slug = 'nope'))`),
			want: "hero_prior ",
		},
		{
			name: "trend insert binder",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE hero_trend_delta DROP COLUMN from_date`)
			},
			want: "hero_trend_delta:",
		},
		{
			name: "trend insert constraint",
			corrupt: withCheck("hero_trend_delta", `CREATE TABLE hero_trend_delta (
				slug VARCHAR NOT NULL PRIMARY KEY, wr_delta_pp DOUBLE NOT NULL,
				share_delta_pp DOUBLE NOT NULL, from_date VARCHAR NOT NULL,
				to_date VARCHAR NOT NULL, CHECK (slug = 'nope'))`),
			want: "hero_trend_delta ",
		},
		{
			name: "heatmap insert binder",
			corrupt: func(t *testing.T, db *sql.DB) {
				exec(t, db, `ALTER TABLE heatmap_enemies DROP COLUMN slug`)
			},
			want: "heatmap_enemies:",
		},
		{
			name: "heatmap insert constraint",
			corrupt: withCheck("heatmap_enemies", `CREATE TABLE heatmap_enemies (
				rank INTEGER NOT NULL PRIMARY KEY, slug VARCHAR NOT NULL,
				CHECK (slug = 'nope'))`),
			want: "heatmap_enemies spectre",
		},
		{
			name:    "acceptance provenance write",
			corrupt: func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE provenance`) },
			want:    "provenance",
		},
		{
			name:    "fallback provenance write",
			corrupt: func(t *testing.T, db *sql.DB) { exec(t, db, `DROP TABLE provenance`) },
			want:    "provenance",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testCfg(t)
			db := seedRunFixture(t, cfg)
			if tc.name == "acceptance provenance write" {
				// a divergent synergy pair is what triggers the acceptance rows
				exec(t, db, `INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
					VALUES ('phantom-lancer', 'spectre', 25, 1.0, 'stratz')`)
			}
			if tc.name == "fallback provenance write" {
				// a roster hero with no stats plus the aggregate file flips the
				// fallback flag; the seed has no divergence so the acceptance
				// rows stay empty
				exec(t, db, `DELETE FROM hero_stats WHERE slug = 'phantom-lancer'`)
			}
			tc.corrupt(t, db)
			root := t.TempDir()
			if tc.name == "fallback provenance write" {
				writeFallbackFile(t, cfg, root, `{"rows": [{"heroId": 7, "matchCount": 75, "winCount": 40}]}`)
			}
			err := Run(Deps{DB: db, Cfg: cfg, RepoRoot: root, Now: fixedNow})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
