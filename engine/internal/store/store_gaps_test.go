package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The parent dir path runs through an existing regular file, so MkdirAll
// cannot create it and Open must surface the os error instead of panicking
// on a half-open db.
func TestOpenRejectsParentUnderRegularFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file, not dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(blocker, "t.duckdb"))
	if err == nil {
		db.Close()
		t.Fatal("Open accepted a path whose parent is a regular file")
	}
	if !strings.Contains(err.Error(), blocker) {
		t.Errorf("error %v does not name the blocking path", err)
	}
}

// duckdb-go parses the path as a URL DSN eagerly inside sql.Open, so a path
// carrying a URL control character fails there, before any connect attempt.
func TestOpenRejectsUnparsablePath(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bad\nname.duckdb"))
	if err == nil {
		db.Close()
		t.Fatal("Open accepted a path the driver cannot parse as a DSN")
	}
	if !strings.Contains(err.Error(), "parse DSN") {
		t.Errorf("error %v is not the driver DSN parse failure", err)
	}
}

// A read-only reopen keeps Open usable for inspection but must refuse to
// apply the schema: the exec arm closes the db and wraps the DDL failure.
func TestOpenRejectsSchemaOnReadOnlyDb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.duckdb")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	ro, err := Open(path + "?access_mode=read_only")
	if err == nil {
		ro.Close()
		t.Fatal("Open applied DDL to a read-only database")
	}
	if !strings.Contains(err.Error(), "schema:") {
		t.Errorf("error %v is not the schema exec failure", err)
	}
}
