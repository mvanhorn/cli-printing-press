package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoveDeadCode_KeepsTheSuiteCompiling is the end-to-end guard for
// --remove-dead-code: after a pass, the tree must still build AND its test
// binaries must still compile.
//
// `go build` never compiles _test.go, so verification on build alone cannot
// notice that a removal broke the suite. Verification must compile test
// binaries without executing package initialization or TestMain setup.
func TestRemoveDeadCode_KeepsTheSuiteCompiling(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "probe"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module probe-pp-cli\n\ngo 1.22\n"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "root.go"), []byte(`package cli

// RootCmd is referenced only from root_test.go.
func RootCmd() string {
	return "root"
}

func trulyDead() string {
	return "no caller anywhere"
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "root_test.go"), []byte(`package cli

import (
	"os"
	"testing"
)

func TestMain(*testing.M) {
	os.Exit(23)
}

func TestRootWires(t *testing.T) {
	if RootCmd() != "root" {
		t.Fatal("boom")
	}
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "probe", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644))

	result, err := RemoveDeadCode(dir, false)
	require.NoError(t, err)

	require.True(t, result.BuildVerified,
		"test binaries must compile without executing TestMain; build_error=%q", result.BuildError)
	require.NotContains(t, result.Removed, "RootCmd",
		"RootCmd is referenced from a _test.go and must survive")

	// The surviving source must still contain RootCmd, and the test binary must
	// still compile — the whole point of the verification change.
	src, err := os.ReadFile(filepath.Join(cliDir, "root.go"))
	require.NoError(t, err)
	require.Contains(t, string(src), "func RootCmd(")
}
