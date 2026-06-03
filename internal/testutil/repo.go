// Package testutil provides helpers shared across test packages.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// FindRepoRoot walks up from the calling test file's location until it
// finds go.mod. Works correctly under `go test ./...` regardless of the
// working directory the test runner is launched from, because each package
// compiles into its own directory and runtime.Caller(0) resolves to the
// compiled .go source path.
func FindRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file location")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod) starting from %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}
