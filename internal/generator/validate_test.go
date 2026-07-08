package generator

import (
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
	assert.Contains(t, string(calls), "toolchain=go1.26.5\n")
	assert.NotContains(t, string(calls), "-show")
	assert.NotContains(t, string(calls), "verbose")
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
