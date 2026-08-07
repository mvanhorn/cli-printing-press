package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFindAllDeadFunctions_CountsTestFileReferences pins that a function whose
// only callers live in _test.go files is NOT reported dead.
//
// listGoFiles skips *_test.go, and findAllDeadFunctions built both its
// definition set and its usage corpus from that same list, so test-only callers
// were invisible. On a real printed CLI that made RootCmd — the entry point the
// generator's own scaffold tests call, 25 times in one case — look unreferenced.
// Removing it leaves a tree that still passes `go build` and fails `go test`.
func TestFindAllDeadFunctions_CountsTestFileReferences(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.go"), []byte(`package cli

// RootCmd is constructed by main and by every scaffold test.
func RootCmd() string {
	return "root"
}

func trulyDead() string {
	return "nobody calls me, not even a test"
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "root_test.go"), []byte(`package cli

import "testing"

func TestRootWires(t *testing.T) {
	if RootCmd() != "root" {
		t.Fatal("boom")
	}
}
`), 0o644))

	dead := findAllDeadFunctions(dir)

	require.NotContains(t, dead, "RootCmd",
		"RootCmd is called from root_test.go; deleting it would break the suite while still passing go build")
	require.Contains(t, dead, "trulyDead",
		"a function with no caller anywhere must still be reported dead")
}

// TestFindAllDeadFunctions_TestOnlyHelperIsLive covers the same rule for an
// unexported helper used only by a table test.
func TestFindAllDeadFunctions_TestOnlyHelperIsLive(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(`package cli

func normalizeKey(s string) string {
	return s
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers_test.go"), []byte(`package cli

import "testing"

func TestNormalizeKey(t *testing.T) {
	if normalizeKey("a") != "a" {
		t.Fatal("boom")
	}
}
`), 0o644))

	require.NotContains(t, findAllDeadFunctions(dir), "normalizeKey")
}
