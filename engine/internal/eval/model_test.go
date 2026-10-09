package eval

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/emit"
	"poolguide/internal/gates"
	"poolguide/internal/mine"
	"poolguide/internal/store"
)

var repoRoot = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Join(file, "..", "..", ".."))
}()

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(repoRoot, "config.json"))
	if err != nil {
		t.Fatalf("load hub config: %v", err)
	}
	return cfg
}

func seedRoster(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "ref", "dota2", "synergy", "raw", "_roster.json"))
	if err != nil {
		t.Fatalf("read roster cache: %v", err)
	}
	var doc struct {
		Heroes []struct{ Slug string } `json:"heroes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse roster cache: %v", err)
	}
	inPool := make(map[string]bool, len(cfg.Pool))
	for _, e := range cfg.Pool {
		inPool[e.Slug] = true
	}
	slugs := append([]string(nil), cfg.PoolSlugs()...)
	for _, h := range doc.Heroes {
		if inPool[h.Slug] {
			continue
		}
		slugs = append(slugs, h.Slug)
		if len(slugs) == len(cfg.PoolSlugs())+2 {
			break
		}
	}
	return slugs
}

func seedModelDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := testConfig(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "model.duckdb"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := mine.EnsureSchema(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	slugs := seedRoster(t, cfg)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %s: %v", q, err)
		}
	}
	for i, slug := range slugs {
		exec(`INSERT INTO hero_roster (hero_id, slug, name, npc, source) VALUES (?, ?, ?, ?, 'test')`,
			100-i, slug, strings.ReplaceAll(slug, "-", " "), "npc_dota_hero_"+strings.ReplaceAll(slug, "-", "_"))
		exec(`INSERT INTO popularity (slug, pick_share, source) VALUES (?, ?, 'test')`,
			slug, float64(i+1)/float64(len(slugs)*len(slugs)))
	}
	pool := cfg.PoolSlugs()
	for i, slug := range pool {
		exec(`INSERT INTO hero_prior (slug, wr, tier, value) VALUES (?, ?, 'dedicated', ?)`,
			slug, 0.4+0.01*float64(i), -1+0.05*float64(i))
	}
	for i, hero := range pool {
		for j, other := range slugs {
			for _, kind := range []string{mine.KindEnemy, mine.KindAlly} {
				exec(`INSERT INTO norm_cell (hero, other, kind, pct, z, completed) VALUES (?, ?, ?, ?, ?, ?)`,
					hero, other, kind, float64(j+1)/float64(len(slugs)+1)+0.000049, 0.1*float64(j), j%5 == 0)
				exec(`INSERT INTO pair_shrunk (hero, other, kind, value, n, post_var, source, confidence)
					VALUES (?, ?, ?, ?, ?, 1.5, 'stratz', 'high')`,
					hero, other, kind, float64(j)-3, int64(120+j))
			}
		}
		_ = i
	}
	return db
}

func arrayInner(t *testing.T, artifact, key string) string {
	t.Helper()
	marker := "  " + key + ": ["
	start := strings.Index(artifact, marker)
	if start < 0 {
		t.Fatalf("artifact has no %s line", key)
	}
	rest := artifact[start+len(marker):]
	end := strings.Index(rest, "],\n")
	if end < 0 {
		t.Fatalf("artifact %s line does not close", key)
	}
	return rest[:end]
}

func packedRowMap(t *testing.T, row string) map[int]float64 {
	t.Helper()
	row = strings.Trim(row, "'")
	out := map[int]float64{}
	for _, cell := range strings.Split(row, ",") {
		if cell == "" {
			continue
		}
		parts := strings.Split(cell, ":")
		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			t.Fatalf("packed cell %q has a bad index", cell)
		}
		pct, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			t.Fatalf("packed cell %q has a bad pct", cell)
		}
		out[idx] = pct
	}
	return out
}

func floatArray(t *testing.T, inner string) []float64 {
	t.Helper()
	inner = strings.TrimSpace(inner)
	inner = strings.TrimPrefix(inner, "[")
	inner = strings.TrimSuffix(inner, "]")
	var out []float64
	for _, cell := range strings.Split(inner, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(cell), 64)
		if err != nil {
			t.Fatalf("float cell %q: %v", cell, err)
		}
		out = append(out, v)
	}
	return out
}

func TestBuildModelMatchesEmittedPicker(t *testing.T) {
	t.Chdir(repoRoot)
	cfg := testConfig(t)
	db := seedModelDB(t)
	m, err := BuildModel(db, cfg)
	if err != nil {
		t.Fatalf("build model: %v", err)
	}
	doc, err := gates.Load(filepath.Join(repoRoot, "picker", "gates.json"))
	if err != nil {
		t.Fatalf("load gates: %v", err)
	}
	prose, err := emit.LoadProse(filepath.Join(repoRoot, "content.json"))
	if err != nil {
		t.Fatalf("load prose: %v", err)
	}
	in, err := emit.LoadPicker(db, cfg, doc)
	if err != nil {
		t.Fatalf("load picker: %v", err)
	}
	in.Prose = prose.Heroes
	in.Date = "2026-09-26"
	out, err := emit.FormatPicker(in, cfg)
	if err != nil {
		t.Fatalf("format picker: %v", err)
	}
	artifact := string(out)

	popVals := floatArray(t, arrayInner(t, artifact, "pop"))
	if len(popVals) != m.RosterSize {
		t.Fatalf("pop length %d, want roster size %d", len(popVals), m.RosterSize)
	}
	for idx, v := range popVals {
		if m.Pop[idx] != v {
			t.Fatalf("pop[%d] = %v, want the emitted %v", idx, m.Pop[idx], v)
		}
	}
	priorVals := floatArray(t, arrayInner(t, artifact, "prior"))
	if len(priorVals) != len(m.Prior) {
		t.Fatalf("prior length %d, want %d", len(priorVals), len(m.Prior))
	}
	for pi, v := range priorVals {
		if m.Prior[pi] != v {
			t.Fatalf("prior[%d] = %v, want the emitted %v", pi, m.Prior[pi], v)
		}
	}
	for _, key := range []string{"mu", "syn"} {
		rows := strings.Split(arrayInner(t, artifact, key), "', '")
		if len(rows) != len(m.PoolIdx) {
			t.Fatalf("%s rows %d, want pool size %d", key, len(rows), len(m.PoolIdx))
		}
		table := m.Mu
		if key == "syn" {
			table = m.Syn
		}
		for pi, row := range rows {
			want := packedRowMap(t, row)
			if len(table[pi]) != len(want) {
				t.Fatalf("%s row %d covers %d indices, want %d", key, pi, len(table[pi]), len(want))
			}
			for idx, v := range want {
				if table[pi][idx] != v {
					t.Fatalf("%s[%d][%d] = %v, want the emitted %v", key, pi, idx, table[pi][idx], v)
				}
			}
		}
	}
	if m.Gates == nil {
		t.Fatalf("model carries no gates document")
	}
}
