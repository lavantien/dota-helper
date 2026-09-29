package emit

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGoldens rewrites the testdata golden files instead of comparing, so
// pool or hub changes regenerate pins with `go test ./internal/emit -update`.
var updateGoldens = flag.Bool("update", false, "rewrite golden files under testdata")

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run go test ./internal/emit -update to generate)", err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s drifted from testdata/%s:\n%s", name, name, firstDivergence(string(want), string(got)))
	}
}
