package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoBuildCacheDirIsShared(t *testing.T) {
	t.Setenv("GOCACHE", "")

	// Two different project directories should get the same cache dir.
	// This is critical for CI performance because the shared cache avoids each
	// parallel test recompiling the Go standard library from scratch.
	dir1, err := goBuildCacheDir("/tmp/project-a")
	require.NoError(t, err)

	dir2, err := goBuildCacheDir("/tmp/project-b")
	require.NoError(t, err)

	assert.Equal(t, dir1, dir2, "different projects should share the same build cache")
}

func TestGoBuildCacheDirPath(t *testing.T) {
	t.Setenv("GOCACHE", "")

	dir, err := goBuildCacheDir("/tmp/any-project")
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	expected := filepath.Join(home, ".cache", "printing-press", "go-build")
	assert.Equal(t, expected, dir)
}

func TestGoBuildCacheDirHonorsExplicitGOCACHE(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "go-build")
	t.Setenv("GOCACHE", cacheDir)

	dir, err := goBuildCacheDir("/tmp/any-project")
	require.NoError(t, err)

	assert.Equal(t, cacheDir, dir)
	assert.DirExists(t, cacheDir)
}

func TestValidateRunsPinnedDefaultGovulncheckGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell go binary is Unix-only")
	}
	outputDir := filepath.Join(t.TempDir(), "validate-pp-cli")
	gen := New(minimalSpec("validate"), outputDir)
	require.NoError(t, gen.Generate())

	fakeBin := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "go-calls.txt")
	fakeGo := filepath.Join(fakeBin, "go")
	require.NoError(t, os.WriteFile(fakeGo, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GO_CALLS"
if [ "$1" = "run" ]; then
  echo "fake govulncheck failure" >&2
  exit 42
fi
exit 0
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	err := gen.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `gate "govulncheck ./..." failed`)

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "mod tidy\n")
	assert.Contains(t, string(calls), "run "+govulncheck.ToolModule+" ./...\n")
	assert.NotContains(t, string(calls), "-show")
	assert.NotContains(t, string(calls), "verbose")
}
