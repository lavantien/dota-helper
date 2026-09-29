package store

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"hero_roster", "matchup_raw", "synergy_raw", "hero_stats",
		"provenance", "pair_shrunk", "norm_cell", "popularity"} {
		var n int
		if err := db.QueryRow(
			"SELECT count(*) FROM duckdb_tables() WHERE table_name = ?", table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("table %s missing", table)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.duckdb")
	db1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db1.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
}
