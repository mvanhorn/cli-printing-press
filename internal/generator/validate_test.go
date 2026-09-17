package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpGateTimeout(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want time.Duration
	}{
		{
			name: "windows",
			goos: "windows",
			want: 30 * time.Second,
		},
		{
			name: "linux",
			goos: "linux",
			want: 15 * time.Second,
		},
		{
			name: "darwin",
			goos: "darwin",
			want: 15 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, helpGateTimeout(tt.goos))
		})
	}
}

func isolateBuildCacheHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GOCACHE", "")
	return home
}

func TestGoBuildCacheDirIsShared(t *testing.T) {
	isolateBuildCacheHome(t)

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
	home := isolateBuildCacheHome(t)

	dir, err := goBuildCacheDir("/tmp/any-project")
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

func TestBoundBuildCacheWipesWhenOverMax(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.bin")
	require.NoError(t, os.WriteFile(stale, bytes.Repeat([]byte("x"), 2000), 0o644))

	require.NoError(t, boundBuildCache(dir, 1000))

	assert.NoFileExists(t, stale)
	assert.DirExists(t, dir)
}

func TestBoundBuildCacheKeepsFilesUnderMax(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.bin")
	require.NoError(t, os.WriteFile(keep, []byte("hello"), 0o644))

	require.NoError(t, boundBuildCache(dir, 1000))

	assert.FileExists(t, keep)
}

func TestBoundBuildCacheMissingDirIsNoop(t *testing.T) {
	require.NoError(t, boundBuildCache(filepath.Join(t.TempDir(), "missing"), 1000))
}

type dirEntryStatError struct {
	err error
}

func (e dirEntryStatError) Name() string               { return "x" }
func (e dirEntryStatError) IsDir() bool                { return false }
func (e dirEntryStatError) Type() os.FileMode          { return 0 }
func (e dirEntryStatError) Info() (os.FileInfo, error) { return nil, e.err }

func TestCacheEntrySizeIgnoresMissingFiles(t *testing.T) {
	size, err := cacheEntrySize(dirEntryStatError{err: os.ErrNotExist})
	require.NoError(t, err)
	assert.Equal(t, int64(0), size)
}

func TestCacheEntrySizeOtherStatErrorsAreVisible(t *testing.T) {
	size, err := cacheEntrySize(dirEntryStatError{err: os.ErrPermission})
	require.Error(t, err)
	assert.Equal(t, int64(0), size)
	assert.True(t, scanRequiresTrim(false, err))
}

func TestScanRequiresTrimOnWalkError(t *testing.T) {
	assert.False(t, scanRequiresTrim(false, nil))
	assert.True(t, scanRequiresTrim(true, nil))
	assert.True(t, scanRequiresTrim(false, os.ErrPermission))
}

func TestBuildCacheExceedsUnreadableDirReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not lock directories on Windows")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	over, err := buildCacheExceeds(dir, 1<<30)
	require.Error(t, err)
	assert.False(t, over)
	assert.True(t, scanRequiresTrim(over, err))
}

func TestBoundBuildCacheReportsRemoveErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not lock directories on Windows")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "x"), bytes.Repeat([]byte("x"), 2000), 0o644))
	require.NoError(t, os.Chmod(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	require.Error(t, boundBuildCache(dir, 1000))
}

func TestWithGoBuildCacheLimitedSurfacesWipeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not lock directories on Windows")
	}
	home := isolateBuildCacheHome(t)
	cacheDir := filepath.Join(home, ".cache", "printing-press", "go-build")
	locked := filepath.Join(cacheDir, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "x"), bytes.Repeat([]byte("x"), 2000), 0o644))
	require.NoError(t, os.Chmod(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	called := false
	err := withGoBuildCacheLimited("/tmp/any-project", 1000, func(string) error {
		called = true
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bounding isolated GOCACHE")
	assert.False(t, called)
}

func TestGoBuildCacheDirWipesManagedCacheOverMax(t *testing.T) {
	home := isolateBuildCacheHome(t)
	cacheDir := filepath.Join(home, ".cache", "printing-press", "go-build")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	stale := filepath.Join(cacheDir, "aa", "stale.bin")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
	require.NoError(t, os.WriteFile(stale, bytes.Repeat([]byte("x"), 2000), 0o644))

	err := withGoBuildCacheLimited("/tmp/any-project", 1000, func(got string) error {
		assert.Equal(t, cacheDir, got)
		assert.NoFileExists(t, stale)
		return nil
	})
	require.NoError(t, err)
	assert.DirExists(t, cacheDir)
}

func TestGoBuildCacheDirDoesNotWipeExplicitGOCACHE(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "go-build")
	t.Setenv("GOCACHE", cacheDir)
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	keep := filepath.Join(cacheDir, "keep.bin")
	require.NoError(t, os.WriteFile(keep, bytes.Repeat([]byte("x"), 2000), 0o644))

	err := withGoBuildCacheLimited("/tmp/any-project", 1000, func(got string) error {
		assert.Equal(t, cacheDir, got)
		assert.FileExists(t, keep)
		return nil
	})
	require.NoError(t, err)
	assert.FileExists(t, keep)
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
  printf 'toolchain=%s\n' "$GOTOOLCHAIN" >> "$FAKE_GO_CALLS"
  echo "fake govulncheck failure" >&2
  exit 42
fi
exit 0
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)
	t.Setenv("GOTOOLCHAIN", "auto")

	err := gen.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `gate "govulncheck ./..." failed`)

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "mod tidy\n")
	assert.Contains(t, string(calls), "run "+govulncheck.ToolModule+" ./...\n")
	assert.Contains(t, string(calls), "toolchain="+currentGoToolchainVersion()+"\n")
	assert.NotContains(t, string(calls), "-show")
	assert.NotContains(t, string(calls), "verbose")
}

func TestValidateRunsGeneratedUnitTests(t *testing.T) {
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
if [ "$1" = "test" ]; then
  if [ "$2" != "-count=1" ]; then
    # Simulate a stale cached green result. A fresh run exposes the failure.
    exit 0
  fi
  echo "fresh generated test failure" >&2
  exit 43
fi
exit 0
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	err := gen.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fresh generated test failure")

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "test -count=1 ./...\n")
}

func TestValidateFailsWhenGeneratedUnitTestsFail(t *testing.T) {
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
if [ "$1" = "test" ]; then
  echo "broken generated test" >&2
  exit 42
fi
exit 0
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	err := gen.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `gate "go test ./..." failed`)
	assert.Contains(t, err.Error(), "broken generated test")

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "test -count=1 ./...\n")
	assert.NotContains(t, string(calls), "vet ./...\n")
	assert.NotContains(t, string(calls), "build ")
}

func TestDeviceValidateRunsGeneratedUnitTests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell go binary is Unix-only")
	}
	outputDir := t.TempDir()
	gen := &DeviceGenerator{OutputDir: outputDir}

	fakeBin := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "go-calls.txt")
	fakeGo := filepath.Join(fakeBin, "go")
	require.NoError(t, os.WriteFile(fakeGo, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GO_CALLS"
if [ "$1" = "test" ]; then
  echo "broken generated device test" >&2
  exit 42
fi
exit 0
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	err := gen.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go test ./...")
	assert.Contains(t, err.Error(), "broken generated device test")

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "test -count=1 ./...\n")
	assert.NotContains(t, string(calls), "build ")
}

func TestValidateBuildRunnableBinaryUsesReproducibleBuildFlags(t *testing.T) {
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
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift || true
done
if [ -n "$out" ]; then
  mkdir -p "$(dirname "$out")"
  cat > "$out" <<'SCRIPT'
#!/bin/sh
echo ok
exit 0
SCRIPT
  chmod 755 "$out"
fi
exit 0
`), 0o755))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GO_CALLS", callsPath)

	require.NoError(t, gen.Validate())

	calls, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "build -trimpath -ldflags=-buildid= -o ")
}
