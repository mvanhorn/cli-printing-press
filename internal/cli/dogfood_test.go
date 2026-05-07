package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintDogfoodReportRespectsSkippedPathCheck(t *testing.T) {
	report := &pipeline.DogfoodReport{
		Dir:      t.TempDir(),
		SpecPath: "synthetic.yaml",
		PathCheck: pipeline.PathCheckResult{
			Skipped: true,
			Detail:  "synthetic spec: path validity not applicable",
		},
	}

	out := captureStdout(t, func() {
		printDogfoodReport(report)
	})

	assert.Contains(t, out, "Path Validity:     0/0 valid (SKIP)")
	assert.Contains(t, out, "synthetic spec: path validity not applicable")
	assert.NotContains(t, out, "Path Validity:     0/0 valid (FAIL)")
}

func TestDogfoodHelpIncludesLiveFlags(t *testing.T) {
	cmd := newDogfoodCmd()
	cmd.SetArgs([]string{"--help"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	assert.Contains(t, output, "--live")
	assert.Contains(t, output, "--level")
	assert.Contains(t, output, "--write-acceptance")
}

func TestPrintLiveDogfoodReportDistinguishesPassWithSkips(t *testing.T) {
	clean := captureStdout(t, func() {
		printLiveDogfoodReport(&pipeline.LiveDogfoodReport{
			Dir:      t.TempDir(),
			Level:    "quick",
			Verdict:  "PASS",
			Passed:   4,
			Failed:   0,
			Skipped:  0,
			Commands: []string{"widgets list"},
		})
	})
	assert.Contains(t, clean, "Verdict:    PASS (all tests run)")
	assert.NotContains(t, clean, "PASS (with skips)")

	withSkips := captureStdout(t, func() {
		printLiveDogfoodReport(&pipeline.LiveDogfoodReport{
			Dir:      t.TempDir(),
			Level:    "quick",
			Verdict:  "PASS",
			Passed:   2,
			Failed:   0,
			Skipped:  2,
			Commands: []string{"widgets list"},
		})
	})
	assert.Contains(t, withSkips, "Verdict:    PASS (with skips)")
	assert.NotContains(t, withSkips, "PASS (all tests run)")
}

func TestDogfoodLiveQuickPassWithSkipsWritesAcceptance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := writeDogfoodQuickSkipFixture(t)
	markerPath := filepath.Join(t.TempDir(), pipeline.Phase5AcceptanceFilename)
	cmd := newDogfoodCmd()
	cmd.SetArgs([]string{
		"--dir", dir,
		"--live",
		"--level", "quick",
		"--timeout", "2s",
		"--write-acceptance", markerPath,
	})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	assert.Contains(t, output, "Verdict:    PASS (with skips)")
	assert.Contains(t, output, "Tests:      4 passed, 0 failed, 4 skipped")

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker pipeline.Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "pass", marker.Status)
	assert.Equal(t, "quick", marker.Level)
	assert.Equal(t, 4, marker.TestsPassed)
	assert.Equal(t, 4, marker.TestsSkipped)
	assert.Equal(t, 0, marker.TestsFailed)

	validation := pipeline.ValidatePhase5Gate(filepath.Dir(markerPath), pipeline.CLIManifest{
		APIName:  marker.APIName,
		RunID:    marker.RunID,
		AuthType: "none",
	})
	assert.True(t, validation.Passed, validation.Detail)
}

func writeDogfoodQuickSkipFixture(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "fixture")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, pipeline.WriteCLIManifest(dir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       "fixture-pp-cli",
		RunID:         "run-live-dogfood",
		AuthType:      "none",
	}))

	binPath := filepath.Join(dir, "fixture-pp-cli")
	script := `#!/bin/sh
set -u

if [ "${1:-}" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"alpha","subcommands":[
      {"name":"list"}
    ]},
    {"name":"widgets","subcommands":[
      {"name":"list"}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "${1:-}" = "widgets" ] && [ "${2:-}" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list
HELP
  exit 0
fi

if [ "${1:-}" = "widgets" ] && [ "${2:-}" = "list" ]; then
  echo 'widgets'
  exit 0
fi

if [ "${1:-}" = "alpha" ] && [ "${2:-}" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List alpha records.

Usage:
  fixture-pp-cli alpha list [flags]

Examples:
  fixture-pp-cli alpha list
HELP
  exit 0
fi

if [ "${1:-}" = "alpha" ] && [ "${2:-}" = "list" ]; then
  echo 'alpha'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir
}
