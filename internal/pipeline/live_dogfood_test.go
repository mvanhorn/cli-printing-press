package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunLiveDogfoodDetectsJSONParseFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, false)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Greater(t, report.MatrixSize, 0)
	assert.Greater(t, report.Failed, 0)

	var jsonFailure *LiveDogfoodTestResult
	for i := range report.Tests {
		if report.Tests[i].Command == "widgets broken" && report.Tests[i].Kind == LiveDogfoodTestJSON {
			jsonFailure = &report.Tests[i]
			break
		}
	}
	require.NotNil(t, jsonFailure)
	assert.Equal(t, LiveDogfoodStatusFail, jsonFailure.Status)
	assert.Contains(t, jsonFailure.Reason, "invalid JSON")
}

func TestRunLiveDogfoodDetectsTruncatedJSONOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodLargeJSONFixture(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    30 * time.Second,
	})
	require.NoError(t, err)

	result := findResultByCommandKind(report, "widgets large", LiveDogfoodTestJSON)
	require.NotNil(t, result)
	assert.Equal(t, LiveDogfoodStatusFail, result.Status)
	assert.Equal(t, "output exceeded capture cap", result.Reason)
}

func TestRunLiveDogfoodWritesAcceptanceMarkerOnPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, true)
	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "PASS", report.Verdict, report.Tests)

	// widgets get happy_path must exercise the resolve-success chain
	// (companion widgets list returns parseable JSON, resolver substitutes the
	// id, get probe runs and passes), not silently skip on companion-parse
	// failure as it did before the fixture fix.
	widgetsGetHappy := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, widgetsGetHappy, "expected widgets get happy_path test result in report")
	assert.Equal(t, LiveDogfoodStatusPass, widgetsGetHappy.Status, widgetsGetHappy.Reason)

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "pass", marker.Status)
	assert.Equal(t, "full", marker.Level)
	assert.Equal(t, report.MatrixSize, marker.MatrixSize)
	assert.Equal(t, report.Passed, marker.TestsPassed)
	assert.Equal(t, 0, marker.TestsFailed)

	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{APIName: marker.APIName, RunID: marker.RunID, AuthType: "none"})
	assert.True(t, validation.Passed, validation.Detail)
}

// TestRunLiveDogfoodWritesFailMarkerOnFail covers the inverted contract from
// issue #1384: --write-acceptance must emit a marker on every outcome, so the
// Phase 5.6 gate has something to read (pass → promote, fail → hold-path)
// instead of forcing operators to hand-author the FAIL marker.
func TestRunLiveDogfoodWritesFailMarkerOnFail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, false)
	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "FAIL", report.Verdict, report.Tests)
	assert.Greater(t, report.Failed, 0)

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err, "failed live dogfood must still write an acceptance marker for Phase 5.6")
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "fail", marker.Status)
	assert.Equal(t, report.Failed, marker.TestsFailed)
	require.NotNil(t, marker.FailureSummary, "fail markers must carry a failure_summary block")
	// The fixture's failing branch produces exit-nonzero results; the
	// classifier may also bucket some as http_4xx/5xx depending on the
	// fixture's emitted reason text. Either way, the aggregate failure
	// count across all buckets must match report.Failed so no failure is
	// silently dropped.
	total := marker.FailureSummary.TransportError + marker.FailureSummary.HTTP4xx +
		marker.FailureSummary.HTTP5xx + marker.FailureSummary.ExitNonzero +
		marker.FailureSummary.OutputMismatch + marker.FailureSummary.Other
	assert.Equal(t, report.Failed, total, "failure_summary buckets must account for every failed test")
	assert.NotEmpty(t, marker.FailureSummary.Commands, "failure_summary must list at least one failing command")

	// The Phase 5 gate must route this marker to the hold path, not pass it.
	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{APIName: marker.APIName, RunID: marker.RunID, AuthType: "none"})
	assert.False(t, validation.Passed)
	assert.Equal(t, "fail", validation.Status)
}

// TestClassifyLiveDogfoodFailure covers the per-test bucket assignment. The
// classifier feeds failure_summary triage hints; missing a bucket silently
// downgrades the operator signal, so each branch needs an explicit fixture.
// HTTP ordering and the JSON-mismatch fall-through were Greptile findings on
// the PR introducing this function and are pinned here as regressions.
func TestClassifyLiveDogfoodFailure(t *testing.T) {
	cases := []struct {
		name string
		in   LiveDogfoodTestResult
		want string
	}{
		{
			name: "http_4xx from reason",
			in:   LiveDogfoodTestResult{Reason: "got HTTP 404 from upstream", ExitCode: 1},
			want: "http_4xx",
		},
		{
			name: "http_5xx from reason",
			in:   LiveDogfoodTestResult{Reason: "got HTTP 503 from upstream", ExitCode: 1},
			want: "http_5xx",
		},
		{
			name: "4xx wins when both appear (retry log shadowing case)",
			in:   LiveDogfoodTestResult{Reason: "retried http 5 times, status http 404", ExitCode: 1},
			want: "http_4xx",
		},
		{
			name: "transport_error on connection refused",
			in:   LiveDogfoodTestResult{Reason: "dial tcp: connection refused", ExitCode: 1},
			want: "transport_error",
		},
		{
			name: "output_mismatch from bare 'invalid JSON' reason",
			in:   LiveDogfoodTestResult{Reason: "invalid JSON", OutputSample: "<<not json>>", ExitCode: 1},
			want: "output_mismatch",
		},
		{
			name: "output_mismatch from 'not json' reason without 'output' word",
			in:   LiveDogfoodTestResult{Reason: "response was not JSON", ExitCode: 1},
			want: "output_mismatch",
		},
		{
			name: "output_mismatch from output+mismatch conjunction",
			in:   LiveDogfoodTestResult{Reason: "output mismatch vs schema", ExitCode: 1},
			want: "output_mismatch",
		},
		{
			name: "exit_nonzero fall-through",
			in:   LiveDogfoodTestResult{Reason: "unknown failure", ExitCode: 2},
			want: "exit_nonzero",
		},
		{
			name: "other when nothing matches and exit code is zero",
			in:   LiveDogfoodTestResult{Reason: "weird thing happened", ExitCode: 0},
			want: "other",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyLiveDogfoodFailure(tc.in))
		})
	}
}

// TestDogfoodEnvVarMatchesEmittedTemplate guards against the runner-side
// const and the emitted-CLI helper drifting apart. They live in
// separate Go modules so a shared import is impossible; this test reads
// the template as text and asserts the literal matches dogfoodEnvVar.
// Without it, a typo on either side would silently break every
// IsDogfoodEnv() short-circuit in printed CLIs.
func TestDogfoodEnvVarMatchesEmittedTemplate(t *testing.T) {
	content, err := generator.TemplateFS.ReadFile("templates/cliutil_verifyenv.go.tmpl")
	require.NoError(t, err)

	re := regexp.MustCompile(`const\s+DogfoodEnvVar\s*=\s*"([^"]+)"`)
	match := re.FindStringSubmatch(string(content))
	require.Len(t, match, 2, "DogfoodEnvVar const not found in cliutil_verifyenv.go.tmpl")
	assert.Equal(t, dogfoodEnvVar, match[1], "runner-side dogfoodEnvVar must match template-side DogfoodEnvVar literal")
}

// TestRunLiveDogfoodProcessSetsDogfoodEnvVar asserts the live-dogfood
// subprocess inherits PRINTING_PRESS_DOGFOOD=1 so long-running commands
// can short-circuit via cliutil.IsDogfoodEnv() to fit inside the
// matrix's per-command timeout.
func TestRunLiveDogfoodProcessSetsDogfoodEnvVar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	// Unset before the call so a CI runner that happens to have
	// PRINTING_PRESS_DOGFOOD pre-set in its environment can't make the
	// assertion pass via inheritance — the test must prove the runner's
	// own append line is what gets the var into the subprocess.
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "echo-env")
	script := "#!/bin/sh\nprintf '%s' \"${PRINTING_PRESS_DOGFOOD:-}\"\n"
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	run := runLiveDogfoodProcess(binPath, dir, nil, 30*time.Second)
	require.NoError(t, run.err, "fixture: %s", run.stderr)
	assert.Equal(t, "1", run.stdout, "live-dogfood subprocess should see PRINTING_PRESS_DOGFOOD=1")
}

func TestRunLiveDogfoodLocalDatastoreManifestPreservesOperatorHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	home := t.TempDir()
	cacheHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	require.NoError(t, os.WriteFile(filepath.Join(home, "source.db"), []byte("fixture"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheHome, "snapshot.db"), []byte("fixture"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "source.conf"), []byte("fixture"), 0o600))

	dir, binaryName := writeLiveDogfoodHomeProbeFixture(t, `if [ ! -f "$HOME/source.db" ]; then echo "missing local source" >&2; exit 3; fi
  if [ ! -f "$XDG_CACHE_HOME/snapshot.db" ]; then echo "missing local cache" >&2; exit 3; fi
  if [ ! -f "$XDG_CONFIG_HOME/source.conf" ]; then echo "missing local config" >&2; exit 3; fi`)
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      "none",
		SpecFormat:    "sqlite",
	}))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)
}

func TestRunLiveDogfoodAPICLIRetainsScopedHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	home := t.TempDir()
	cacheHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	require.NoError(t, os.WriteFile(filepath.Join(home, "source.db"), []byte("fixture"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheHome, "snapshot.db"), []byte("fixture"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "source.conf"), []byte("fixture"), 0o600))

	dir, binaryName := writeLiveDogfoodHomeProbeFixture(t, `if [ -f "$HOME/source.db" ]; then echo "operator home leaked" >&2; exit 3; fi
  if [ -f "$XDG_CACHE_HOME/snapshot.db" ]; then echo "operator cache leaked" >&2; exit 3; fi
  if [ -f "$XDG_CONFIG_HOME/source.conf" ]; then echo "operator config leaked" >&2; exit 3; fi`)
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      "none",
		SpecFormat:    "openapi3",
	}))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)
}

func TestRunLiveDogfoodAuthenticatedLocalDatastoreRetainsScopedHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	home := t.TempDir()
	cacheHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	require.NoError(t, os.WriteFile(filepath.Join(home, "source.db"), []byte("fixture"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheHome, "snapshot.db"), []byte("fixture"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "source.conf"), []byte("fixture"), 0o600))

	dir, binaryName := writeLiveDogfoodHomeProbeFixture(t, `if [ -f "$HOME/source.db" ]; then echo "operator home leaked" >&2; exit 3; fi
  if [ -f "$XDG_CACHE_HOME/snapshot.db" ]; then echo "operator cache leaked" >&2; exit 3; fi
  if [ -f "$XDG_CONFIG_HOME/source.conf" ]; then echo "operator config leaked" >&2; exit 3; fi`)
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      "api_key",
		SpecFormat:    "sqlite",
	}))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)
}

func TestRunLiveDogfoodMirrorsConfigAndCookieCredentialsIntoScopedHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const (
		binaryName         = "fixture-pp-cli"
		configCredential   = "real-config-token"
		cookieCredential   = "real-cookie-token"
		configOriginalBody = "auth_header = \"" + configCredential + "\"\n"
		cookieOriginalBody = `[{"name":"session","value":"` + cookieCredential + `","domain":".example.test","path":"/","secure":true}]`
	)

	operatorHome := t.TempDir()
	t.Setenv("HOME", operatorHome)
	configPath := filepath.Join(operatorHome, ".config", binaryName, "config.toml")
	cookiePath := filepath.Join(operatorHome, ".local", "share", binaryName, "cookies.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(cookiePath), 0o700))
	require.NoError(t, os.WriteFile(configPath, []byte(configOriginalBody), 0o600))
	require.NoError(t, os.WriteFile(cookiePath, []byte(cookieOriginalBody), 0o600))

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		RunID:         "run-live-dogfood",
		AuthType:      "composed",
		SpecFormat:    "openapi3",
	}))
	writeStubBinary(t, dir, binaryName, `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"account","subcommands":[{"name":"show"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show the authenticated account.

Usage:
  fixture-pp-cli account show [flags]

Examples:
  fixture-pp-cli account show

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ]; then
  config_path="$HOME/.config/fixture-pp-cli/config.toml"
  cookie_path="$HOME/.local/share/fixture-pp-cli/cookies.json"
  if ! grep -q 'real-config-token' "$config_path"; then
    echo "missing mirrored config credential" >&2
    exit 1
  fi
  if ! grep -q 'real-cookie-token' "$cookie_path"; then
    echo "missing mirrored cookie credential" >&2
    exit 1
  fi
  printf 'auth_header = "real-config-token-mutated"\n' > "$config_path"
  printf '[{"name":"session","value":"real-cookie-token-mutated","domain":".example.test","path":"/","secure":true}]' > "$cookie_path"
  if [ "${3:-}" = "--json" ]; then
    echo '{"ok":true}'
    exit 0
  fi
  echo 'ok'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)

	happy := findResultByCommandKind(report, "account show", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusPass, happy.Status, happy.Reason)
	jsonResult := findResultByCommandKind(report, "account show", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult)
	assert.Equal(t, LiveDogfoodStatusPass, jsonResult.Status, jsonResult.Reason)

	gotConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, configOriginalBody, string(gotConfig), "live dogfood must not mutate the operator's real config")
	gotCookies, err := os.ReadFile(cookiePath)
	require.NoError(t, err)
	assert.Equal(t, cookieOriginalBody, string(gotCookies), "live dogfood must not mutate the operator's real cookies")
}

// TestRunLiveDogfoodCookieAuthNoSessionSkips covers issue #3104: a cookie-auth
// CLI run in the sandboxed dogfood HOME (no captured session) 401s every
// command. Those 401s are a harness artifact, not a CLI defect, so the runner
// records a clean skip verdict (CLI exits 0) and writes a phase5-skip.json the
// promote gate accepts — instead of counting the 401s as failures.
func TestRunLiveDogfoodCookieAuthNoSessionSkips(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const binaryName = "fixture-pp-cli"

	// Sandboxed HOME so the cookie jar is absent — mirror the real harness.
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		RunID:         "run-cookie-no-session",
		AuthType:      "cookie",
		SpecFormat:    "openapi3",
	}))
	writeStubBinary(t, dir, binaryName, `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"account","subcommands":[{"name":"show"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show the authenticated account.

Usage:
  fixture-pp-cli account show [flags]

Examples:
  fixture-pp-cli account show

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ]; then
  echo "HTTP 401: couldn't authenticate; login required" >&2
  exit 1
fi

echo "unexpected args: $*" >&2
exit 99
`)

	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	assert.Equal(t, liveDogfoodVerdictCookieAuthNoSession, report.Verdict, report.Tests)
	assert.Equal(t, 0, report.Failed, "cookie-auth 401s must not count as failures")

	// The runner writes the skip marker, not the fail acceptance marker.
	_, err = os.Stat(markerPath)
	assert.True(t, os.IsNotExist(err), "no fail acceptance marker should be written for the cookie-auth skip")
	skipPath := filepath.Join(filepath.Dir(markerPath), Phase5SkipFilename)
	data, err := os.ReadFile(skipPath)
	require.NoError(t, err, "cookie-auth skip must write a phase5-skip.json marker")
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "skip", marker.Status)
	assert.Equal(t, phase5SkipReasonCookieAuthNoHarnessSession, marker.SkipReason)
	assert.Equal(t, "cookie", marker.AuthContext.Type)

	// The promote gate accepts the skip marker.
	validation := ValidatePhase5Gate(filepath.Dir(skipPath), CLIManifest{
		APIName: marker.APIName, RunID: marker.RunID, AuthType: "cookie",
	})
	assert.True(t, validation.Passed, validation.Detail)
	assert.Equal(t, "skip", validation.Status)
}

func TestRunLiveDogfoodSyncsOAuth2RefreshConfigBackToOperatorHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const (
		binaryName         = "fixture-pp-cli"
		configOriginalBody = "access_token = \"expired-access\"\nrefresh_token = \"old-refresh\"\n"
		configRotatedBody  = "access_token = \"fresh-access\"\nrefresh_token = \"new-refresh\"\n"
	)

	operatorHome := t.TempDir()
	t.Setenv("HOME", operatorHome)
	configPath := filepath.Join(operatorHome, ".config", binaryName, "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	require.NoError(t, os.WriteFile(configPath, []byte(configOriginalBody), 0o600))

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		RunID:         "run-live-dogfood",
		AuthType:      "oauth2_refresh",
		SpecFormat:    "openapi3",
	}))
	writeStubBinary(t, dir, binaryName, `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"account","subcommands":[{"name":"show"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show the authenticated account.

Usage:
  fixture-pp-cli account show [flags]

Examples:
  fixture-pp-cli account show

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ]; then
  config_path="$HOME/.config/fixture-pp-cli/config.toml"
  if ! grep -Eq 'old-refresh|new-refresh' "$config_path"; then
    echo "missing mirrored refresh token" >&2
    exit 1
  fi
  printf 'access_token = "fresh-access"\nrefresh_token = "new-refresh"\n' > "$config_path"
  if [ "${3:-}" = "--json" ]; then
    echo '{"ok":true}'
    exit 0
  fi
  echo 'ok'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)

	gotConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, configRotatedBody, string(gotConfig), "rotated OAuth refresh tokens must survive live dogfood cleanup")
}

func TestRunLiveDogfoodSyncBackConflictStillReturnsReportAndWritesAcceptance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const (
		binaryName         = "fixture-pp-cli"
		configOriginalBody = "access_token = \"expired-access\"\nrefresh_token = \"old-refresh\"\n"
		configRotatedBody  = "access_token = \"fresh-access\"\nrefresh_token = \"new-refresh\"\n"
		configConflictBody = "access_token = \"operator-edit\"\nrefresh_token = \"operator-refresh\"\n"
	)

	operatorHome := t.TempDir()
	t.Setenv("HOME", operatorHome)
	configPath := filepath.Join(operatorHome, ".config", binaryName, "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	require.NoError(t, os.WriteFile(configPath, []byte(configOriginalBody), 0o600))
	t.Setenv("OPERATOR_CONFIG_PATH", configPath)

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		RunID:         "run-live-dogfood",
		AuthType:      "oauth2_refresh",
		SpecFormat:    "openapi3",
	}))
	writeStubBinary(t, dir, binaryName, `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"account","subcommands":[{"name":"show"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show the authenticated account.

Usage:
  fixture-pp-cli account show [flags]

Examples:
  fixture-pp-cli account show

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ]; then
  config_path="$HOME/.config/fixture-pp-cli/config.toml"
  if ! grep -Eq 'old-refresh|new-refresh' "$config_path"; then
    echo "missing mirrored refresh token" >&2
    exit 1
  fi
  printf 'access_token = "operator-edit"\nrefresh_token = "operator-refresh"\n' > "$OPERATOR_CONFIG_PATH"
  printf 'access_token = "fresh-access"\nrefresh_token = "new-refresh"\n' > "$config_path"
  if [ "${3:-}" = "--json" ]; then
    echo '{"ok":true}'
    exit 0
  fi
  echo 'ok'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`)

	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.Error(t, err)
	require.NotNil(t, report, "sync-back errors must not discard the completed live dogfood report")
	assert.Contains(t, err.Error(), "operator config changed during dogfood")
	assert.Equal(t, "PASS", report.Verdict, report.Tests)

	gotConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, configConflictBody, string(gotConfig), "sync-back conflict must preserve the operator edit")

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err, "sync-back errors must not suppress the Phase 5 acceptance marker")
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "pass", marker.Status)
	assert.Equal(t, report.MatrixSize, marker.MatrixSize)
	assert.Equal(t, report.Passed, marker.TestsPassed)
}

func TestSyncLiveDogfoodCredentialMirrorsRejectsOperatorConfigConflict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "operator", "config.toml")
	dst := filepath.Join(dir, "scoped", "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o700))
	require.NoError(t, os.WriteFile(src, []byte("refresh_token = \"old\"\n"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("refresh_token = \"new\"\n"), 0o600))

	mirror := liveDogfoodCredentialMirror{
		src:      src,
		dst:      dst,
		original: []byte("refresh_token = \"original\"\n"),
		mode:     0o600,
	}

	err := syncLiveDogfoodCredentialMirrors([]liveDogfoodCredentialMirror{mirror})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operator config changed during dogfood")

	got, readErr := os.ReadFile(src)
	require.NoError(t, readErr)
	assert.Equal(t, "refresh_token = \"old\"\n", string(got))
}

func TestRunLiveDogfoodProcessRetriesTransientAuth401(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	binPath := writeStubBinary(t, dir, "flaky-auth", `count_file="count"
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s' "$count" > "$count_file"
if [ "$count" -eq 1 ]; then
  echo 'Error: GET /api/v2/account/settings returned HTTP 401: {"error":"Couldn'\''t authenticate you"}' >&2
  exit 1
fi
printf '{"ok":true}'
`)

	run := runLiveDogfoodProcess(binPath, dir, nil, 5*time.Second)
	require.NoError(t, run.err, "fixture: %s", run.stderr)
	assert.Equal(t, 0, run.exitCode)
	assert.Equal(t, `{"ok":true}`, run.stdout)

	count, err := os.ReadFile(countPath)
	require.NoError(t, err)
	assert.Equal(t, "2", string(count), "auth-shaped 401 should be retried once")
}

func TestRunLiveDogfoodSkipsPersistentAuth401AsRunnerCredentialUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	writeStubBinary(t, dir, binaryName, `if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"account","subcommands":[{"name":"show-settings"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show-settings" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show account settings.

Usage:
  fixture-pp-cli account show-settings [flags]

Examples:
  fixture-pp-cli account show-settings

Flags:
      --json    Output JSON
HELP
  exit 0
fi

count_file="count"
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s' "$count" > "$count_file"
echo 'Error: GET /api/v2/account/settings returned HTTP 401: {"error":"Could not authenticate you"}' >&2
exit 1
`)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    5 * time.Second,
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "account show-settings", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected account show-settings happy_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Equal(t, reasonUnavailableRunnerCredentials, happy.Reason)

	jsonResult := findResultByCommandKind(report, "account show-settings", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult, "expected account show-settings json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, jsonResult.Status)
	assert.Equal(t, reasonUnavailableRunnerCredentials, jsonResult.Reason)

	count, err := os.ReadFile(filepath.Join(dir, "count"))
	require.NoError(t, err)
	assert.Equal(t, "2", string(count), "persistent auth-shaped 401 should retry once before skip classification")
}

func TestRunLiveDogfoodRefreshesStageBinaryBeforeResolving(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the stale staged binary; skip on Windows")
	}

	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/live-dogfood-test\n\ngo 1.23\n"), 0o644))
	cmdDir := filepath.Join(dir, "cmd", binaryName)
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))
	mainPath := filepath.Join(cmdDir, "main.go")
	agentContext := `{"commands":[{"name":"widgets","subcommands":[{"name":"list"}]}]}`
	help := `List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list

Flags:
      --json    Output JSON
`
	mainSource := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	switch strings.Join(args, " ") {
	case "agent-context":
		fmt.Print(%q)
	case "widgets list --help":
		fmt.Print(%q)
	case "widgets list", "widgets list --json":
		fmt.Print(%q)
	default:
		fmt.Fprintf(os.Stderr, "unexpected args: %%v\n", args)
		os.Exit(2)
	}
}
`, agentContext, help, `{"ok":true}`)
	require.NoError(t, os.WriteFile(mainPath, []byte(mainSource), 0o644))

	stagedBinDir := filepath.Join(dir, "build", "stage", "bin")
	require.NoError(t, os.MkdirAll(stagedBinDir, 0o755))
	stagedPath := filepath.Join(stagedBinDir, binaryName)
	require.NoError(t, os.WriteFile(stagedPath, []byte("#!/bin/sh\necho 'stale staged binary' >&2\nexit 2\n"), 0o755))
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(stagedPath, oldTime, oldTime))
	require.NoError(t, os.Chtimes(mainPath, newTime, newTime))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    5 * time.Second,
	})
	require.NoError(t, err)
	require.Zero(t, report.Failed)
	wantBinary, err := filepath.Abs(stagedPath)
	require.NoError(t, err)
	assert.Equal(t, wantBinary, report.Binary)
	happy := findResultByCommandKind(report, "widgets list", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusPass, happy.Status)
}

func TestRunLiveDogfoodSkipsRequiresTierMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName, argvLog := writeLiveDogfoodTierFixture(t, true, false, true)
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLog)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "PASS", report.Verdict, report.Tests)
	happy := findResultByCommandKind(report, "administration get", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Equal(t, `blocked-fixture: requires auth tier "accountant"`, happy.Reason)

	jsonResult := findResultByCommandKind(report, "administration get", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult)
	assert.Equal(t, LiveDogfoodStatusSkip, jsonResult.Status)
	assert.Equal(t, happy.Reason, jsonResult.Reason)

	errorResult := findResultByCommandKind(report, "administration get", LiveDogfoodTestError)
	require.NotNil(t, errorResult)
	assert.Equal(t, LiveDogfoodStatusSkip, errorResult.Status)
	assert.Equal(t, happy.Reason, errorResult.Reason)

	lines := readArgvLog(t, argvLog)
	assert.Equal(t, 0, countArgvLines(lines, "administration get", "--json"),
		"json_fidelity must not invoke tier-gated endpoints when active auth tier mismatches")
	assert.Equal(t, 0, countArgvLines(lines, "administration get")-countArgvLines(lines, "administration get --help"),
		"happy_path must not invoke tier-gated endpoints when active auth tier mismatches")
	assert.Equal(t, 0, countArgvLines(lines, "administration get", "__printing_press_invalid__"),
		"error_path must not invoke tier-gated endpoints when active auth tier mismatches")
}

func TestRunLiveDogfoodSkipsRequiresTierMismatchWithoutRunnableExample(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName, _ := writeLiveDogfoodTierFixture(t, true, false, false)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "administration get", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Equal(t, `blocked-fixture: requires auth tier "accountant"`, happy.Reason)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)
}

func TestRunLiveDogfoodRunsRequiresTierMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName, _ := writeLiveDogfoodTierFixture(t, true, true, true)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
		AuthTier:   "accountant",
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "administration get", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusPass, happy.Status, happy.Reason)
	jsonResult := findResultByCommandKind(report, "administration get", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult)
	assert.Equal(t, LiveDogfoodStatusPass, jsonResult.Status, jsonResult.Reason)
}

func TestRunLiveDogfoodRunsRequiresTierMatchFromEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName, _ := writeLiveDogfoodTierFixture(t, true, true, true)
	t.Setenv("PP_AUTH_TIER", "accountant")
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "administration get", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusPass, happy.Status, happy.Reason)
	jsonResult := findResultByCommandKind(report, "administration get", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult)
	assert.Equal(t, LiveDogfoodStatusPass, jsonResult.Status, jsonResult.Reason)
}

func TestRunLiveDogfoodRequiresTierAbsentDoesNotSkip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName, _ := writeLiveDogfoodTierFixture(t, false, false, true)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "quick",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "administration get", LiveDogfoodTestHappy)
	require.NotNil(t, happy)
	assert.Equal(t, LiveDogfoodStatusFail, happy.Status)
	assert.Contains(t, happy.OutputSample, "EP_001")
}

func TestRunLiveDogfoodProcessPreservesLargeJSONUnderCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	binPath := filepath.Join(dir, "large-json")
	payloadBytes := liveDogfoodMaxOutputBytes / 5
	script := `#!/bin/sh
printf '{"data":"'
head -c ` + fmt.Sprint(payloadBytes) + ` /dev/zero | tr '\0' 'x'
printf '"}'
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o700))

	run := runLiveDogfoodProcess(binPath, dir, nil, 5*time.Second)
	require.NoError(t, run.err)
	assert.False(t, run.stdoutTruncated)
	assert.True(t, validLiveDogfoodJSONOutput(run.stdout))
}

func TestRunLiveDogfoodProcessTracksOutputTruncation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	binPath := filepath.Join(dir, "huge-json")
	script := fmt.Sprintf(`#!/bin/sh
printf '{"data":"'
head -c %d /dev/zero | tr '\0' 'x'
printf '"}'
`, liveDogfoodMaxOutputBytes+1024)
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o700))

	run := runLiveDogfoodProcess(binPath, dir, nil, 30*time.Second)
	require.NoError(t, run.err)
	assert.True(t, run.stdoutTruncated)
	assert.False(t, validLiveDogfoodJSONOutput(run.stdout))
	assert.Equal(t, "output exceeded capture cap", liveDogfoodInvalidJSONReason(run, "invalid JSON"))
}

func TestLiveDogfoodResultRedactsOutputSamplePII(t *testing.T) {
	run := liveDogfoodRun{
		stdout:   "{\"name\":\"Jane Doe\"}\n",
		stderr:   "{\"email\":\"jane@example.com\"}",
		exitCode: 0,
	}

	result := liveDogfoodResult("widgets list", LiveDogfoodTestHappy, []string{"widgets", "list"}, run)

	require.NotContains(t, result.OutputSample, "Jane Doe")
	require.NotContains(t, result.OutputSample, "jane@example.com")
	require.Contains(t, result.OutputSample, `"name":"<redacted>"`)
	require.Contains(t, result.OutputSample, `"email":"<redacted>"`)
}

func TestRunLiveDogfoodErrorPathAcceptsExpectedNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFixture(t, true)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	var errorPath *LiveDogfoodTestResult
	for i := range report.Tests {
		if report.Tests[i].Command == "widgets get" && report.Tests[i].Kind == LiveDogfoodTestError {
			errorPath = &report.Tests[i]
			break
		}
	}
	require.NotNil(t, errorPath)
	assert.Equal(t, LiveDogfoodStatusPass, errorPath.Status)
	assert.Equal(t, 2, errorPath.ExitCode)
}

func TestLiveDogfoodMutatingLeafDetectionTokenizesCommandNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"create-customer",
		"post-agent-ledger-templates",
		"request-send-money",
		"transfer",
		"set-token",
	} {
		assert.True(t, isMutatingLeaf(name), "%s should be treated as mutating", name)
	}
	assert.False(t, isMutatingLeaf("get-agent-ledger-templates"))
	assert.False(t, isMutatingLeaf("list"))
}

func TestLiveDogfoodCommandMutatesPrefersEndpointMethod(t *testing.T) {
	t.Parallel()

	assert.False(t, liveDogfoodCommandMutates(liveDogfoodCommand{
		Path: []string{"search", "query"},
		Annotations: map[string]string{
			"pp:method":     "POST",
			"mcp:read-only": "true",
		},
	}))
	assert.False(t, liveDogfoodCommandMutates(liveDogfoodCommand{
		Path:        []string{"request-send-money", "list-send-money-approval-requests"},
		Annotations: map[string]string{"pp:method": "GET"},
	}))
	assert.True(t, liveDogfoodCommandMutates(liveDogfoodCommand{
		Path:        []string{"accounts", "plain-name"},
		Annotations: map[string]string{"pp:method": "POST"},
	}))
}

func TestHappyPathFileFixtureSkip(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	existing := filepath.Join(cliDir, "fixture.csv")
	require.NoError(t, os.WriteFile(existing, []byte("header\nvalue\n"), 0o600))

	cases := []struct {
		name       string
		args       []string
		wantSkip   bool
		wantPrefix string
	}{
		{
			name:     "no file flag",
			args:     []string{"sync", "--limit", "5"},
			wantSkip: false,
		},
		{
			name:       "missing csv fixture",
			args:       []string{"vet", "--csv", "prospects.csv"},
			wantSkip:   true,
			wantPrefix: "file fixture required: --csv prospects.csv",
		},
		{
			name:       "missing file fixture",
			args:       []string{"import-csv", "--file", "accounts.csv"},
			wantSkip:   true,
			wantPrefix: "file fixture required: --file accounts.csv",
		},
		{
			name:       "missing fixture via --flag=value form",
			args:       []string{"import-csv", "--file=accounts.csv"},
			wantSkip:   true,
			wantPrefix: "file fixture required: --file accounts.csv",
		},
		{
			name:     "existing fixture in cliDir",
			args:     []string{"vet", "--csv", "fixture.csv"},
			wantSkip: false,
		},
		{
			name:     "URL value does not trigger skip",
			args:     []string{"upload", "--file", "https://example.com/data.csv"},
			wantSkip: false,
		},
		{
			name:     "unrelated flag name ignored",
			args:     []string{"resolve", "--query", "anything.csv"},
			wantSkip: false,
		},
		{
			name:     "case-insensitive flag match",
			args:     []string{"upload", "--CSV", "missing.csv"},
			wantSkip: true,
		},
		{
			// --profile contains "file" as a substring but is not a file
			// flag; spurious skips here would silently drop test signal.
			name:     "profile flag does not trigger skip",
			args:     []string{"deploy", "--profile", "staging"},
			wantSkip: false,
		},
		{
			name:     "hyphenated suffix matches",
			args:     []string{"upload", "--input-file", "missing.txt"},
			wantSkip: true,
		},
		{
			name:     "underscore suffix matches",
			args:     []string{"upload", "--output_file", "missing.txt"},
			wantSkip: true,
		},
		{
			name:     "csv suffix matches",
			args:     []string{"import", "--import-csv", "missing.csv"},
			wantSkip: true,
		},
		{
			// --file-format takes a format identifier (csv/json), not a path.
			// Greptile's suggestion correctly excludes the prefix shape.
			name:     "file prefix without suffix anchor does not match",
			args:     []string{"export", "--file-format", "csv"},
			wantSkip: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := happyPathFileFixtureSkip(tc.args, cliDir)
			if tc.wantSkip {
				assert.NotEmpty(t, got, "expected skip reason")
				if tc.wantPrefix != "" {
					assert.Equal(t, tc.wantPrefix, got)
				}
			} else {
				assert.Empty(t, got, "expected no skip")
			}
		})
	}
}

func TestRunLiveDogfoodHappyPathHandlesShellCommentInExample(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodShellCommentScript(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "sync", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected sync happy_path result")
	assert.Equal(t, LiveDogfoodStatusPass, happy.Status,
		"trailing '# comment' in Cobra Example must not bleed into happy_path argv (reason=%q)", happy.Reason)
	assert.Equal(t, []string{"sync"}, happy.Args,
		"happy_path argv must contain only the subcommand path, not the comment text")
}

func writeLiveDogfoodShellCommentScript(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"sync"}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "sync" ] && [ "${2:-}" = "--help" ]; then
  cat <<'HELP'
Refresh local cache.

Usage:
  fixture-pp-cli sync [flags]

Examples:
  fixture-pp-cli sync                       # full schema + records refresh

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "sync" ]; then
  # Anything past 'sync' means the example's trailing comment leaked into
  # argv — fail loudly so the test catches the regression.
  if [ "$#" -gt 1 ] && [ "${2}" != "--json" ]; then
    echo "unexpected sync args: $*" >&2
    exit 4
  fi
  if [ "${2:-}" = "--json" ]; then
    echo '{"synced":true}'
    exit 0
  fi
  echo 'synced'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func TestRunLiveDogfoodSkipsHappyPathOnMissingFileFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodFileFixtureScript(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	happy := findResultByCommandKind(report, "import-csv", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected import-csv happy_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Contains(t, happy.Reason, reasonFileFixtureRequired)
	assert.Contains(t, happy.Reason, "accounts.csv")

	json := findResultByCommandKind(report, "import-csv", LiveDogfoodTestJSON)
	require.NotNil(t, json, "expected import-csv json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, json.Status)
	assert.Contains(t, json.Reason, reasonFileFixtureRequired)

	help := findResultByCommandKind(report, "import-csv", LiveDogfoodTestHelp)
	require.NotNil(t, help, "expected import-csv help result")
	assert.Equal(t, LiveDogfoodStatusPass, help.Status, "help check must still pass when the only failure is a missing fixture")
}

func TestRunLiveDogfoodSkipsHappyPathOnRequiredParam4xx(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodRequiredParamFixture(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "PASS", report.Verdict)

	happy := findResultByCommandKind(report, "reports prospects", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected reports prospects happy_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Equal(t, reasonRequiredParamFixture, happy.Reason)

	json := findResultByCommandKind(report, "reports prospects", LiveDogfoodTestJSON)
	require.NotNil(t, json, "expected reports prospects json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, json.Status)
	assert.Equal(t, happy.Reason, json.Reason)

	help := findResultByCommandKind(report, "reports prospects", LiveDogfoodTestHelp)
	require.NotNil(t, help, "expected reports prospects help result")
	assert.Equal(t, LiveDogfoodStatusPass, help.Status, "help check must still pass when the only failure is an unsupplied required API parameter")

	summaryHappy := findResultByCommandKind(report, "reports summary", LiveDogfoodTestHappy)
	require.NotNil(t, summaryHappy, "expected reports summary happy_path result")
	assert.Equal(t, LiveDogfoodStatusPass, summaryHappy.Status)

	summaryJSON := findResultByCommandKind(report, "reports summary", LiveDogfoodTestJSON)
	require.NotNil(t, summaryJSON, "expected reports summary json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, summaryJSON.Status)
	assert.Equal(t, reasonRequiredParamFixture, summaryJSON.Reason)

	eventsHappy := findResultByCommandKind(report, "events list", LiveDogfoodTestHappy)
	require.NotNil(t, eventsHappy, "expected events list happy_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, eventsHappy.Status)
	assert.Equal(t, reasonRequiredParamFixture, eventsHappy.Reason)

	eventsJSON := findResultByCommandKind(report, "events list", LiveDogfoodTestJSON)
	require.NotNil(t, eventsJSON, "expected events list json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, eventsJSON.Status)
	assert.Equal(t, reasonRequiredParamFixture, eventsJSON.Reason)
}

func TestRunLiveDogfoodSkipsMutatingCommandsWithoutRunnableExample(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodMissingRunnableExampleFixture(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	updateHelp := findResultByCommandKind(report, "templates update", LiveDogfoodTestHelp)
	require.NotNil(t, updateHelp, "expected templates update help result")
	assert.Equal(t, LiveDogfoodStatusPass, updateHelp.Status)

	updateHappy := findResultByCommandKind(report, "templates update", LiveDogfoodTestHappy)
	require.NotNil(t, updateHappy, "expected templates update happy_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, updateHappy.Status)
	assert.Equal(t, reasonMutatingRunnableFixture, updateHappy.Reason)

	updateJSON := findResultByCommandKind(report, "templates update", LiveDogfoodTestJSON)
	require.NotNil(t, updateJSON, "expected templates update json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, updateJSON.Status)
	assert.Equal(t, reasonMutatingRunnableFixture, updateJSON.Reason)

	updateError := findResultByCommandKind(report, "templates update", LiveDogfoodTestError)
	require.NotNil(t, updateError, "expected templates update error_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, updateError.Status)
	assert.Equal(t, reasonMutatingRunnableFixture, updateError.Reason)

	detailsHappy := findResultByCommandKind(report, "reports details", LiveDogfoodTestHappy)
	require.NotNil(t, detailsHappy, "expected reports details happy_path result")
	assert.Equal(t, LiveDogfoodStatusFail, detailsHappy.Status)
	assert.Equal(t, "missing runnable example", detailsHappy.Reason)
}

func TestRunLiveDogfoodKeepsOrdinary4xxFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodOrdinary4xxFixture(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)

	happy := findResultByCommandKind(report, "reports broken-filter", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected reports broken-filter happy_path result")
	assert.Equal(t, LiveDogfoodStatusFail, happy.Status)
	assert.Contains(t, happy.OutputSample, "invalid filter")
}

func writeLiveDogfoodFileFixtureScript(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"import-csv"}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "import-csv" ] && [ "${2:-}" = "--help" ]; then
  cat <<'HELP'
Import accounts from a CSV file.

Usage:
  fixture-pp-cli import-csv [flags]

Examples:
  fixture-pp-cli import-csv --file accounts.csv

Flags:
      --file string   Path to the CSV file
      --json          Output JSON
HELP
  exit 0
fi

if [ "$1" = "import-csv" ]; then
  # Without a real fixture, this would error out. The test exercises the
  # skip path; the actual subprocess should never be invoked for happy_path.
  echo 'open accounts.csv: no such file or directory' >&2
  exit 1
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeLiveDogfoodRequiredParamFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	script := `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"reports","subcommands":[
      {"name":"prospects"},
      {"name":"summary"}
    ]},
    {"name":"events","subcommands":[
      {"name":"list","annotations":{"pp:method":"GET","mcp:read-only":"true"}}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "reports" ] && [ "$2" = "prospects" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List report prospects.

Usage:
  fixture-pp-cli reports prospects [flags]

Examples:
  fixture-pp-cli reports prospects

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "reports" ] && [ "$2" = "prospects" ] && [ "${3:-}" = "--json" ]; then
  echo 'unexpected reports prospects --json invocation' >&2
  exit 9
fi

if [ "$1" = "reports" ] && [ "$2" = "prospects" ]; then
  echo 'HTTP 400: {"error":"Please provide email"}' >&2
  exit 1
fi

if [ "$1" = "reports" ] && [ "$2" = "summary" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show report summary.

Usage:
  fixture-pp-cli reports summary [flags]

Examples:
  fixture-pp-cli reports summary

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "reports" ] && [ "$2" = "summary" ] && [ "${3:-}" = "--json" ]; then
  echo 'HTTP 400: {"error":"missing required parameter: email"}' >&2
  exit 1
fi

if [ "$1" = "reports" ] && [ "$2" = "summary" ]; then
  echo 'summary'
  exit 0
fi

if [ "$1" = "events" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List calendar events.

Usage:
  fixture-pp-cli events list [flags]

Examples:
  fixture-pp-cli events list --account-id 550e8400-e29b-41d4-a716-446655440000 --calendar-ids example-value --start 2026-01-15T09:00:00Z --end 2026-01-15T10:00:00Z

Flags:
      --account-id string     Account id
      --calendar-ids string   Calendar ids
      --end string            End time
      --json                  Output JSON
      --start string          Start time
HELP
  exit 0
fi

if [ "$1" = "events" ] && [ "$2" = "list" ]; then
  echo 'unexpected events list invocation with synthetic required query params' >&2
  exit 9
fi

echo "unexpected args: $*" >&2
exit 99
`
	writeStubBinary(t, dir, binaryName, script)
	return dir, binaryName
}

func writeLiveDogfoodMissingRunnableExampleFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	script := `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"templates","subcommands":[
      {"name":"update","annotations":{"pp:method":"PUT"}}
    ]},
    {"name":"reports","subcommands":[
      {"name":"details","annotations":{"pp:method":"GET"}}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "templates" ] && [ "$2" = "update" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Update a template.

Usage:
  fixture-pp-cli templates update [flags]

Examples:
  # Supply a real template id before running this write command.

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "reports" ] && [ "$2" = "details" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Read report details.

Usage:
  fixture-pp-cli reports details [flags]

Examples:
  # No runnable example is documented for this read command.

Flags:
      --json    Output JSON
HELP
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	writeStubBinary(t, dir, binaryName, script)
	return dir, binaryName
}

func writeLiveDogfoodOrdinary4xxFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	script := `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"reports","subcommands":[
      {"name":"broken-filter"}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "reports" ] && [ "$2" = "broken-filter" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Run a report with an invalid upstream filter.

Usage:
  fixture-pp-cli reports broken-filter [flags]

Examples:
  fixture-pp-cli reports broken-filter

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "reports" ] && [ "$2" = "broken-filter" ]; then
  echo 'HTTP 400: {"error":"invalid filter"}' >&2
  exit 1
fi

echo "unexpected args: $*" >&2
exit 99
`
	writeStubBinary(t, dir, binaryName, script)
	return dir, binaryName
}

func TestValidLiveDogfoodJSONOutputAcceptsNDJSON(t *testing.T) {
	t.Parallel()

	assert.True(t, validLiveDogfoodJSONOutput(`{"event":"start"}`))
	assert.True(t, validLiveDogfoodJSONOutput("{\"event\":\"start\"}\n{\"event\":\"done\"}\n"))
	assert.False(t, validLiveDogfoodJSONOutput("{\"event\":\"start\"}\nwarning: skipped\n"))
	assert.False(t, validLiveDogfoodJSONOutput(""))
}

func TestLiveDogfoodUnavailableForRunnerDoesNotHideNotFound(t *testing.T) {
	t.Parallel()

	assert.True(t, liveDogfoodUnavailableForRunner(liveDogfoodRun{stderr: "HTTP 403 permission denied"}))
	assert.True(t, liveDogfoodUnavailableForRunner(liveDogfoodRun{stderr: "your credentials are valid but lack access"}))
	assert.True(t, liveDogfoodUnavailableForRunner(liveDogfoodRun{stderr: `HTTP 401: {"error":"Couldn't authenticate you"}`}))
	assert.False(t, liveDogfoodUnavailableForRunner(liveDogfoodRun{stderr: "HTTP 404 NotFound"}))
}

func TestLiveDogfoodAuth401OutputMatchesGooglePhrases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name: "login required",
			output: `Error: GET /youtube/v3/videoAbuseReportReasons returned HTTP 401: {
  "error": {"message": "Login Required."}
}`,
			want: true,
		},
		{
			name: "missing required authentication credential",
			output: `Error: GET /youtube/v3/videoAbuseReportReasons returned HTTP 401: {
  "error": {"message": "Request is missing required authentication credential."}
}`,
			want: true,
		},
		{
			name:   "non 401",
			output: `Error: GET /widgets returned HTTP 404: {"error":"not found"}`,
			want:   false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, liveDogfoodAuth401Output(strings.ToLower(tt.output)))
		})
	}
}

func TestLiveDogfoodRequiredParamFixtureReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  liveDogfoodRun
		want string
	}{
		{
			name: "missing required parameter 4xx is blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 400: {"error":"missing required parameter: email"}`, exitCode: 1},
			want: reasonRequiredParamFixture,
		},
		{
			name: "please provide 4xx is blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 400: {"error":"Please provide email"}`, exitCode: 1},
			want: reasonRequiredParamFixture,
		},
		{
			name: "ordinary 4xx remains a failure",
			run:  liveDogfoodRun{stderr: "HTTP 404 NotFound", exitCode: 1},
		},
		{
			name: "401 auth requirement is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 401: {"error":"api key is required"}`, exitCode: 1},
		},
		{
			name: "403 subscription requirement is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 403: {"error":"subscription is required"}`, exitCode: 1},
		},
		{
			name: "404 not found requirement is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 404: {"error":"widget id is required"}`, exitCode: 1},
		},
		{
			name: "generic 400 requirement is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 400: {"error":"subscription is required"}`, exitCode: 1},
		},
		{
			name: "generic please provide 400 is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 400: {"error":"Please provide subscription"}`, exitCode: 1},
		},
		{
			name: "422 missing field validation is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 422: {"error":"missing field: status"}`, exitCode: 1},
		},
		{
			name: "successful output is not a blocked fixture",
			run:  liveDogfoodRun{stderr: `HTTP 400: {"error":"missing required parameter: email"}`, exitCode: 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, liveDogfoodRequiredParamFixtureReason(tc.run))
		})
	}
}

func TestRunLiveDogfoodSkipsDestructiveByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodDestructiveFixture(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	var destructiveSkips int
	for _, result := range report.Tests {
		if result.Command == "api-keys refresh" {
			assert.Equal(t, LiveDogfoodStatusSkip, result.Status)
			assert.Equal(t, reasonDestructiveAtAuth, result.Reason)
			destructiveSkips++
		}
	}
	assert.Equal(t, 4, destructiveSkips)

	widgetsHelp := findResultByCommandKind(report, "widgets list", LiveDogfoodTestHelp)
	require.NotNil(t, widgetsHelp)
	assert.Equal(t, LiveDogfoodStatusPass, widgetsHelp.Status)
}

func TestRunLiveDogfoodAllowDestructiveBypass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir, binaryName := writeLiveDogfoodDestructiveFixture(t)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:           dir,
		BinaryName:       binaryName,
		Level:            "full",
		Timeout:          2 * time.Second,
		AllowDestructive: true,
	})
	require.NoError(t, err)

	assert.Equal(t, 0, countResultsWithReason(report.Tests, reasonDestructiveAtAuth))
	for _, kind := range []LiveDogfoodTestKind{LiveDogfoodTestHelp, LiveDogfoodTestHappy, LiveDogfoodTestJSON} {
		result := findResultByCommandKind(report, "api-keys refresh", kind)
		require.NotNil(t, result)
		assert.Equal(t, LiveDogfoodStatusPass, result.Status, result.Reason)
	}
}

func TestRunLiveDogfoodExplicitBinaryNameMustExist(t *testing.T) {
	dir := t.TempDir()

	_, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: "missing-pp-cli",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-pp-cli")
}

func TestRunLiveDogfoodAcceptanceWithoutManifestEmitsMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	// Phase 5 dogfood runs before `lock promote` writes the manifest. With
	// no .printing-press.json on disk and no runstate matching this temp
	// fixture, --write-acceptance must still emit a marker carrying the
	// dogfood run's own state. Identity stays empty; the gate cross-check
	// in validatePhase5Marker only enforces identity when the manifest
	// supplies it.
	dir, binaryName := writeLiveDogfoodFixture(t, true)
	require.NoError(t, os.Remove(filepath.Join(dir, CLIManifestFilename)))

	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "PASS", report.Verdict, report.Tests)

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "pass", marker.Status)
	assert.Equal(t, "full", marker.Level)
	assert.Equal(t, report.MatrixSize, marker.MatrixSize)
	assert.Equal(t, report.Passed, marker.TestsPassed)
	assert.Empty(t, marker.APIName, "marker should not invent identity when neither manifest nor runstate supplies it")
	assert.Empty(t, marker.RunID, "marker should not invent identity when neither manifest nor runstate supplies it")
	assert.Equal(t, "none", marker.AuthContext.Type)

	// Validation passes against an unidentified manifest because the
	// cross-check has nothing to enforce.
	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{AuthType: "none"})
	assert.True(t, validation.Passed, validation.Detail)
}

func TestRunLiveDogfoodAcceptanceFallsBackToRunstateIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	// Pre-promote scenario from issue #963: working dir has no manifest but
	// runstate identifies the CLI. The marker must record state's
	// api_name/run_id so the gate cross-check at promote time matches the
	// manifest lock promote will write.
	setPressTestEnv(t)

	dir, binaryName := writeLiveDogfoodFixture(t, true)
	require.NoError(t, os.Remove(filepath.Join(dir, CLIManifestFilename)))

	state := NewState("fixture", dir)
	require.NoError(t, state.Save())

	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		WriteAcceptancePath: markerPath,
	})
	require.NoError(t, err)
	require.Equal(t, "PASS", report.Verdict, report.Tests)

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, "fixture", marker.APIName, "marker should record runstate api_name when manifest is absent")
	assert.Equal(t, state.RunID, marker.RunID, "marker should record runstate run_id when manifest is absent")

	validation := ValidatePhase5Gate(filepath.Dir(markerPath), CLIManifest{APIName: "fixture", RunID: state.RunID, AuthType: "none"})
	assert.True(t, validation.Passed, validation.Detail)
}

// TestFinalizeLiveDogfoodReportVerdictGate exercises the quick-level verdict
// switch directly against synthesized reports. The new gate must accept
// skip-with-reason as a non-failure (Passed + Skipped >= 5) with a MatrixSize
// floor of 4, while preserving Failed-dominance and full-level semantics.
func TestFinalizeLiveDogfoodReportVerdictGate(t *testing.T) {
	mkResult := func(status LiveDogfoodStatus) LiveDogfoodTestResult {
		return LiveDogfoodTestResult{Status: status}
	}

	tests := []struct {
		name     string
		level    string
		authType string
		results  []LiveDogfoodTestResult
		want     string
	}{
		{
			name:  "quick all pass classic",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
			},
			want: "PASS",
		},
		{
			name:  "quick 5 pass + 1 skip — companion missing",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusSkip),
			},
			want: "PASS",
		},
		{
			name:  "quick 4 pass + 2 skip — multi-positional skip + no-companion skip",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip),
			},
			want: "PASS",
		},
		{
			name:  "quick 3 pass + 3 skip - skips do not satisfy the signal floor",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusSkip),
				mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip),
			},
			want: "FAIL",
		},
		{
			name:  "quick 1-command all pass — 4 entries should PASS via min(5, M) threshold",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
			},
			want: "PASS",
		},
		{
			name:  "quick 4 pass + 1 fail — Failed dominates",
			level: "quick",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusFail),
			},
			want: "FAIL",
		},
		{
			name:    "quick all skip — MatrixSize 0",
			level:   "quick",
			results: []LiveDogfoodTestResult{mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip)},
			want:    "FAIL",
		},
		{
			name:    "full all skip — MatrixSize 0 still blocks acceptance",
			level:   "full",
			results: []LiveDogfoodTestResult{mkResult(LiveDogfoodStatusSkip), mkResult(LiveDogfoodStatusSkip)},
			want:    "FAIL",
		},
		{
			name:  "full all pass — full-level PASS preserved (verdict default)",
			level: "full",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass),
			},
			want: "PASS",
		},
		{
			name:  "full credential-unavailable skips need a live signal",
			level: "full",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusPass),
				{Status: LiveDogfoodStatusSkip, Reason: reasonUnavailableRunnerCredentials},
			},
			want: "FAIL",
		},
		{
			name:  "full credential-unavailable skips pass with one real happy path",
			level: "full",
			results: []LiveDogfoodTestResult{
				{Status: LiveDogfoodStatusPass, Kind: LiveDogfoodTestHappy, Args: []string{"widgets", "get"}},
				{Status: LiveDogfoodStatusSkip, Reason: reasonUnavailableRunnerCredentials},
			},
			want: "PASS",
		},
		{
			name:  "full one fail — Failed dominates at full level",
			level: "full",
			results: []LiveDogfoodTestResult{
				mkResult(LiveDogfoodStatusPass), mkResult(LiveDogfoodStatusPass),
				mkResult(LiveDogfoodStatusFail),
			},
			want: "FAIL",
		},
		{
			// Cookie auth with no captured session: the sandboxed HOME 401s
			// every command. That is a harness artifact, so the no-live-signal
			// outcome becomes a clean skip verdict, not a FAIL.
			name:     "cookie auth credential-unavailable skips become a clean skip",
			level:    "full",
			authType: "cookie",
			results: []LiveDogfoodTestResult{
				{Status: LiveDogfoodStatusSkip, Reason: reasonUnavailableRunnerCredentials},
			},
			want: liveDogfoodVerdictCookieAuthNoSession,
		},
		{
			// A cookie-auth CLI that did get one real live pass (session
			// injected) certifies normally, not as a skip.
			name:     "cookie auth with a live pass certifies normally",
			level:    "full",
			authType: "cookie",
			results: []LiveDogfoodTestResult{
				{Status: LiveDogfoodStatusPass, Kind: LiveDogfoodTestHappy, Args: []string{"account", "show"}},
				{Status: LiveDogfoodStatusSkip, Reason: reasonUnavailableRunnerCredentials},
			},
			want: "PASS",
		},
		{
			// A genuine non-auth failure (e.g. a crashing --help) alongside the
			// session-less 401 skips must NOT be masked by the cookie-auth
			// clean-skip path: report.Failed > 0 keeps the verdict FAIL so the
			// gate still sees the real defect.
			name:     "cookie auth with a genuine failure stays FAIL, not a clean skip",
			level:    "full",
			authType: "cookie",
			results: []LiveDogfoodTestResult{
				{Status: LiveDogfoodStatusFail, Kind: LiveDogfoodTestHappy, Args: []string{"widgets", "list"}},
				{Status: LiveDogfoodStatusSkip, Reason: reasonUnavailableRunnerCredentials},
			},
			want: "FAIL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &LiveDogfoodReport{
				Level:   tt.level,
				Verdict: "PASS",
				Tests:   tt.results,
			}
			finalizeLiveDogfoodReport(report, tt.authType)
			assert.Equal(t, tt.want, report.Verdict, "Passed=%d Failed=%d Skipped=%d MatrixSize=%d",
				report.Passed, report.Failed, report.Skipped, report.MatrixSize)
		})
	}
}

func TestLiveDogfoodQuickCommandsSamplesAcrossFamilies(t *testing.T) {
	commands := []liveDogfoodCommand{
		{Path: []string{"across-admins"}},
		{Path: []string{"administration", "get"}},
		{Path: []string{"administration", "list"}},
		{Path: []string{"contacts", "get"}},
		{Path: []string{"contacts", "list"}},
		{Path: []string{"invoices", "get"}},
		{Path: []string{"ledger", "get"}},
		{Path: []string{"mutations", "create"}},
		{Path: []string{"relations", "get"}},
		{Path: []string{"reports", "list"}},
		{Path: []string{"transactions", "get"}},
		{Path: []string{"transactions", "list"}},
	}

	got := liveDogfoodQuickCommands(commands)

	require.Len(t, got, 6)
	families := map[string]bool{}
	for _, command := range got {
		families[liveDogfoodCommandFamily(command)] = true
	}
	assert.GreaterOrEqual(t, len(families), 3)
	assert.NotEqual(t, commands[:2], got,
		"quick sampling should not collapse to the first two sorted commands")
}

func TestLiveDogfoodQuickCommandsDoesNotDeduplicateEmptyFamily(t *testing.T) {
	commands := []liveDogfoodCommand{
		{Path: nil},
		{Path: []string{}},
		{Path: []string{"administration", "get"}},
		{Path: []string{"administration", "list"}},
		{Path: []string{"contacts", "get"}},
		{Path: []string{"contacts", "list"}},
		{Path: []string{"invoices", "get"}},
		{Path: []string{"invoices", "list"}},
	}

	got := liveDogfoodQuickCommands(commands)

	require.Len(t, got, 6)
	assert.Equal(t, []liveDogfoodCommand{
		commands[0],
		commands[1],
		commands[2],
		commands[4],
		commands[6],
		commands[3],
	}, got)
}

func TestExtractFirstIDFromJSON(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   string
		ok     bool
	}{
		{
			name:   "TMDb results shape",
			stdout: `{"results":[{"id":"42"}],"total_results":1}`,
			want:   "42", ok: true,
		},
		{
			name:   "top-level array",
			stdout: `[{"id":"first"},{"id":"second"}]`,
			want:   "first", ok: true,
		},
		{
			name:   "items shape (GitHub REST)",
			stdout: `{"items":[{"id":"abc"}],"total_count":1}`,
			want:   "abc", ok: true,
		},
		{
			name:   "data array (Stripe)",
			stdout: `{"object":"list","data":[{"id":"cus_xyz"}],"has_more":false}`,
			want:   "cus_xyz", ok: true,
		},
		{
			name:   "list shape (long-tail)",
			stdout: `{"list":[{"id":"L1"}]}`,
			want:   "L1", ok: true,
		},
		{
			name:   "GraphQL nodes (Shopify)",
			stdout: `{"data":{"products":{"nodes":[{"id":"gid://shopify/Product/42"}]}}}`,
			want:   "gid://shopify/Product/42", ok: true,
		},
		{
			name:   "GraphQL edges (Relay-style)",
			stdout: `{"data":{"viewer":{"repos":{"edges":[{"node":{"id":"R_kgABC123"}}]}}}}`,
			want:   "R_kgABC123", ok: true,
		},
		{
			name:   "numeric id preserved as string",
			stdout: `{"results":[{"id":12345}]}`,
			want:   "12345", ok: true,
		},
		{
			name:   "snowflake-size numeric id (no scientific notation)",
			stdout: `{"results":[{"id":1234567890123456789}]}`,
			want:   "1234567890123456789", ok: true,
		},
		{
			name:   "empty results — no id",
			stdout: `{"results":[]}`,
			want:   "", ok: false,
		},
		{
			name:   "results without id field",
			stdout: `{"results":[{"name":"thing"}]}`,
			want:   "", ok: false,
		},
		{
			name:   "invalid JSON",
			stdout: `not json at all`,
			want:   "", ok: false,
		},
		{
			name:   "matches REST results before GraphQL — REST wins",
			stdout: `{"results":[{"id":"REST"}],"data":{"x":{"nodes":[{"id":"GQL"}]}}}`,
			want:   "REST", ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractFirstIDFromJSON(tt.stdout)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildSiblingMap(t *testing.T) {
	commands := []liveDogfoodCommand{
		{Path: []string{"projects", "list"}},
		{Path: []string{"projects", "get"}},
		{Path: []string{"projects", "tasks", "list"}},
		{Path: []string{"projects", "tasks", "update"}},
		{Path: []string{"users", "get"}},
	}
	siblings := buildSiblingMap(commands)

	// Top-level commands keyed by "" (parent path).
	assert.Len(t, siblings["projects"], 2, "projects subcommands")
	assert.Len(t, siblings["projects tasks"], 2, "projects tasks subcommands")
	assert.Len(t, siblings["users"], 1, "users subcommands")
}

func TestFindListCompanion(t *testing.T) {
	candidates := []liveDogfoodCommand{
		{Path: []string{"widgets", "get"}},
		{Path: []string{"widgets", "list"}},
		{Path: []string{"widgets", "delete"}},
	}
	got := findListCompanion(candidates)
	if assert.NotNil(t, got) {
		assert.Equal(t, []string{"widgets", "list"}, got.Path)
	}

	// Cinema verb fallback.
	cinema := []liveDogfoodCommand{
		{Path: []string{"movies", "get"}},
		{Path: []string{"movies", "popular"}},
	}
	got = findListCompanion(cinema)
	if assert.NotNil(t, got) {
		assert.Equal(t, []string{"movies", "popular"}, got.Path)
	}

	// No allowlisted leaf.
	none := []liveDogfoodCommand{
		{Path: []string{"x", "delete"}},
		{Path: []string{"x", "update"}},
	}
	assert.Nil(t, findListCompanion(none))
}

func TestSubstitutePositionals(t *testing.T) {
	tests := []struct {
		name        string
		happyArgs   []string
		commandPath []string
		resolved    []string
		want        []string
	}{
		{
			name:        "single positional",
			happyArgs:   []string{"widgets", "get", "example-value"},
			commandPath: []string{"widgets", "get"},
			resolved:    []string{"42"},
			want:        []string{"widgets", "get", "42"},
		},
		{
			name:        "two positionals",
			happyArgs:   []string{"projects", "tasks", "update", "ph1", "ph2"},
			commandPath: []string{"projects", "tasks", "update"},
			resolved:    []string{"P1", "T1"},
			want:        []string{"projects", "tasks", "update", "P1", "T1"},
		},
		{
			name:        "positional before flag",
			happyArgs:   []string{"widgets", "update", "ph1", "--name", "thing"},
			commandPath: []string{"widgets", "update"},
			resolved:    []string{"abc"},
			want:        []string{"widgets", "update", "abc", "--name", "thing"},
		},
		{
			name:        "no positionals (resolved empty)",
			happyArgs:   []string{"widgets", "list", "--limit", "5"},
			commandPath: []string{"widgets", "list"},
			resolved:    nil,
			want:        []string{"widgets", "list", "--limit", "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substitutePositionals(tt.happyArgs, tt.commandPath, tt.resolved)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveCommandPositionalsSkipPaths(t *testing.T) {
	ctx := resolveCtx{
		siblings: map[string][]liveDogfoodCommand{},
		cache:    newCompanionCache(),
		timeout:  time.Second,
	}

	// No positionals → not-skipped, happyArgs unchanged.
	cmd := liveDogfoodCommand{
		Path: []string{"widgets", "list"},
		Help: "Usage:\n  cli widgets list [flags]\n",
	}
	args, skipped, _, _ := resolveCommandPositionals(cmd, []string{"widgets", "list"}, ctx)
	assert.False(t, skipped)
	assert.Equal(t, []string{"widgets", "list"}, args)

	// Non-id-shape positional (<query>) at depth 0 → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"widgets", "search"},
		Help: "Usage:\n  cli widgets search <query> [flags]\n",
	}
	_, skipped, reason, _ := resolveCommandPositionals(cmd, []string{"widgets", "search", "x"}, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "non-id positional")

	// id-shape positional (bare `id`) but no companion → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"widgets", "get"},
		Help: "Usage:\n  cli widgets get <id> [flags]\n",
	}
	_, skipped, reason, _ = resolveCommandPositionals(cmd, []string{"widgets", "get", "x"}, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "no list companion")

	// camelCase id-shape positional (movieId) but no companion → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"movies", "get"},
		Help: "Usage:\n  cli movies get <movieId> [flags]\n",
	}
	_, skipped, reason, _ = resolveCommandPositionals(cmd, []string{"movies", "get", "x"}, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "no list companion")

	// Path shorter than placeholders + 1 → skip.
	cmd = liveDogfoodCommand{
		Path: []string{"get"},
		Help: "Usage:\n  cli get <id> <name> [flags]\n",
	}
	_, skipped, _, _ = resolveCommandPositionals(cmd, []string{"get", "x", "y"}, ctx)
	assert.True(t, skipped)
}

func TestResolveCommandPositionalsMixedStoreAndCompanionSourceIsUntagged(t *testing.T) {
	requireSQLite3(t)
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "fixture-pp-cli")
	script := `#!/bin/sh
set -u
if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "$3" = "list" ] && [ "$4" = "real-project-1" ] && [ "$5" = "--json" ]; then
  echo '{"results":[{"id":"real-task-1"}]}'
  exit 0
fi
echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binaryPath, []byte(script), 0o755))

	dbPath := filepath.Join(t.TempDir(), "data.db")
	createResources := "CREATE TABLE resources (id TEXT NOT NULL, resource_type TEXT NOT NULL, data JSON NOT NULL, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (resource_type, id)); INSERT INTO resources(resource_type, id, data) VALUES('projects', 'real-project-1', '{}')"
	require.NoError(t, exec.Command("sqlite3", dbPath, createResources).Run())

	cmd := liveDogfoodCommand{
		Path: []string{"projects", "tasks", "get"},
		Help: "Usage:\n  fixture-pp-cli projects tasks get <project-id> <task-id> [flags]\n",
	}
	listCmd := liveDogfoodCommand{Path: []string{"projects", "tasks", "list"}}
	ctx := resolveCtx{
		binaryPath:  binaryPath,
		cliDir:      dir,
		siblings:    map[string][]liveDogfoodCommand{"projects tasks": {listCmd}},
		cache:       newCompanionCache(),
		timeout:     time.Second,
		storeDBPath: dbPath,
	}

	args, skipped, reason, source := resolveCommandPositionals(cmd, []string{"projects", "tasks", "get", "example-project", "example-task"}, ctx)
	require.False(t, skipped, reason)
	assert.Equal(t, []string{"projects", "tasks", "get", "real-project-1", "real-task-1"}, args)
	assert.Empty(t, source, "mixed store and companion resolution should not be counted as store-backed")
}

func TestLiveDogfoodPreSyncTimeoutCapsLongTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Second, liveDogfoodPreSyncTimeout(30*time.Second))
	assert.Equal(t, 2*time.Second, liveDogfoodPreSyncTimeout(2*time.Second))
	assert.Equal(t, 5*time.Second, liveDogfoodPreSyncTimeout(0))
}

func TestCommandSupportsSearch(t *testing.T) {
	tests := []struct {
		name string
		help string
		want bool
	}{
		{
			name: "search via --query flag",
			help: `Usage:
  fixture-pp-cli widgets search [flags]

Flags:
      --query string   Search query
      --json           Output JSON
`,
			want: true,
		},
		{
			name: "search via positional <query>",
			help: `Usage:
  fixture-pp-cli widgets search <query> [flags]

Flags:
      --json   Output JSON
`,
			want: true,
		},
		{
			name: "non-search list command — no query signal",
			help: `Usage:
  fixture-pp-cli widgets list [flags]

Flags:
      --limit int   Max items
      --json        Output JSON
`,
			want: false,
		},
		{
			name: "exact-match flag — --queue must not match --query",
			help: `Usage:
  fixture-pp-cli widgets dispatch [flags]

Flags:
      --queue string   Job queue name
`,
			want: false,
		},
		{
			name: "Examples block mentioning --query does NOT trigger search-shape (Flags-section scoping)",
			help: `Usage:
  fixture-pp-cli widgets delete <id> [flags]

Examples:
  fixture-pp-cli widgets delete 42
  # related: fixture-pp-cli widgets list --query=foo

Flags:
      --yes   Confirm
`,
			want: false,
		},
		{
			name: "Long block mentioning --query does NOT trigger search-shape",
			help: `Long: To delete by filter, see the related --query syntax in widgets list.

Usage:
  fixture-pp-cli widgets purge <id> [flags]

Flags:
      --force   Skip confirmation
`,
			want: false,
		},
		{
			name: "mutation command — no query signal",
			help: `Usage:
  fixture-pp-cli widgets delete <id> [flags]

Flags:
      --yes   Confirm
`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, commandSupportsSearch(tt.help))
		})
	}
}

func TestRunLiveDogfoodJSONFlagDetectionIsExact(t *testing.T) {
	help := `Usage:
  fixture-pp-cli widgets list [flags]

Flags:
      --json-output string   Write JSON to a file
`

	assert.False(t, commandSupportsJSON(help))
	assert.True(t, commandSupportsJSON(help+"\n      --json   Output JSON\n"))
}

func TestAppendDryRunArg(t *testing.T) {
	t.Run("appends when missing", func(t *testing.T) {
		got := appendDryRunArg([]string{"widgets", "create"})
		assert.Equal(t, []string{"widgets", "create", "--dry-run"}, got)
	})

	t.Run("idempotent on bare --dry-run", func(t *testing.T) {
		got := appendDryRunArg([]string{"widgets", "create", "--dry-run"})
		assert.Equal(t, []string{"widgets", "create", "--dry-run"}, got)
	})

	t.Run("idempotent on --dry-run= form", func(t *testing.T) {
		got := appendDryRunArg([]string{"widgets", "create", "--dry-run=true"})
		assert.Equal(t, []string{"widgets", "create", "--dry-run=true"}, got)
	})

	t.Run("does not collide with --dry-run-output near-miss", func(t *testing.T) {
		got := appendDryRunArg([]string{"widgets", "create", "--dry-run-output", "preview.json"})
		assert.Equal(t, []string{"widgets", "create", "--dry-run-output", "preview.json", "--dry-run"}, got)
	})
}

func TestCommandSupportsDryRun(t *testing.T) {
	tests := []struct {
		name string
		help string
		want bool
	}{
		{
			name: "advertised under Global Flags (generated-CLI shape)",
			help: `Usage:
  fixture-pp-cli widgets create [flags]

Flags:
      --json   Output JSON

Global Flags:
      --dry-run   Show request without sending
`,
			want: true,
		},
		{
			name: "absent — hand-written novel command without --dry-run wiring",
			help: `Usage:
  fixture-pp-cli widgets purge <id> [flags]

Flags:
      --force   Skip confirmation
`,
			want: false,
		},
		{
			name: "mention only in Examples — still matches (mirrors commandSupportsJSON's unscoped scan)",
			help: `Usage:
  fixture-pp-cli widgets create [flags]

Examples:
  fixture-pp-cli widgets create --dry-run

Flags:
      --json   Output JSON
`,
			want: true,
		},
		{
			name: "empty help",
			help: ``,
			want: false,
		},
		{
			name: "near-miss: --dry-run-output is not --dry-run",
			help: `Usage:
  fixture-pp-cli widgets create [flags]

Flags:
      --dry-run-output string   Write dry-run preview to a file
`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, commandSupportsDryRun(tt.help))
		})
	}
}

func writeLiveDogfoodFixture(t *testing.T, brokenJSONFixed bool) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	brokenJSON := "{not-json"
	if brokenJSONFixed {
		brokenJSON = `{"ok":true}`
	}
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"list"},
      {"name":"get"},
      {"name":"broken"}
    ]},
    {"name":"completion","subcommands":[{"name":"bash"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a widget.

Usage:
  fixture-pp-cli widgets get <id> [flags]

Examples:
  fixture-pp-cli widgets get 123

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "broken" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Return malformed JSON.

Usage:
  fixture-pp-cli widgets broken [flags]

Examples:
  fixture-pp-cli widgets broken

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"123"}]}'
    exit 0
  fi
  echo 'widget 1'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"123"}'
    exit 0
  fi
  echo 'widget 123'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "broken" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '` + brokenJSON + `'
    exit 0
  fi
  echo 'broken'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeLiveDogfoodTierFixture(t *testing.T, annotate bool, adminPass bool, runnableExample bool) (dir string, binaryName string, argvLog string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	argvLog = filepath.Join(t.TempDir(), "argv.log")
	writeTestManifestForLiveDogfood(t, dir)

	annotation := ""
	if annotate {
		annotation = `,"annotations":{"pp:requires-tier":"accountant"}`
	}
	adminExample := "  fixture-pp-cli administration get adm_1"
	if !runnableExample {
		adminExample = "  fixture-pp-cli administration list --json"
	}
	adminBody := `echo 'Error: GET /v1/administration returned HTTP 400: {"type":"badrequest","code":"EP_001","title":"This endpoint is only available to accountants.","status":400}' >&2
exit 5`
	if adminPass {
		adminBody = `if [ "${3:-}" = "__printing_press_invalid__" ]; then
  echo 'invalid id' >&2
  exit 2
fi
if [ "${4:-}" = "--json" ]; then
  echo '{"id":"administration"}'
  exit 0
fi
echo 'administration'
exit 0`
	}

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

if [ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ]; then
  printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
fi

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
	"commands": [
	    {"name":"administration","subcommands":[
	      {"name":"get"` + annotation + `},
	      {"name":"list"}
	    ]},
	    {"name":"public","subcommands":[
	      {"name":"list"}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "administration" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get administration details.

Usage:
  fixture-pp-cli administration get <id> [flags]

Examples:
` + adminExample + `

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "administration" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List administration records.

Usage:
  fixture-pp-cli administration list [flags]

Examples:
  fixture-pp-cli administration list --json

Flags:
      --json       Output JSON
      --limit int  Limit results
HELP
  exit 0
fi

if [ "$1" = "public" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List public records.

Usage:
  fixture-pp-cli public list [flags]

Examples:
  fixture-pp-cli public list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "administration" ] && [ "$2" = "get" ]; then
` + adminBody + `
fi

if [ "$1" = "administration" ] && [ "$2" = "list" ]; then
  echo '{"results":[{"id":"adm_1"}]}'
  exit 0
fi

if [ "$1" = "public" ] && [ "$2" = "list" ]; then
  echo '{"results":[{"id":"pub_1"}]}'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName, argvLog
}

func writeLiveDogfoodHomeProbeFixture(t *testing.T, probe string) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

if [ "${1:-}" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"list"},
      {"name":"recent"}
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
  fixture-pp-cli widgets list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "${1:-}" = "widgets" ] && [ "${2:-}" = "recent" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List recent widgets.

Usage:
  fixture-pp-cli widgets recent [flags]

Examples:
  fixture-pp-cli widgets recent --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "${1:-}" = "widgets" ] && { [ "${2:-}" = "list" ] || [ "${2:-}" = "recent" ]; }; then
  ` + probe + `
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"123"}]}'
    exit 0
  fi
  echo 'widget 1'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeLiveDogfoodLargeJSONFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := fmt.Sprintf(`#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[{"name":"large"}]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "large" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Large widgets.

Usage:
  fixture-pp-cli widgets large [flags]

Examples:
  fixture-pp-cli widgets large --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "large" ]; then
  if [ "${3:-}" = "--json" ]; then
    printf '{"id":"first"}\n'
    printf '{"data":"'
    head -c %d /dev/zero | tr '\0' 'x'
    printf '"}\n'
    exit 0
  fi
  echo 'large widgets'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`, liveDogfoodMaxOutputBytes+1024)
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeLiveDogfoodSoftFailureFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

if [ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ]; then
  printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
fi

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"list"},
      {"name":"soft-lookup","annotations":{"pp:no-error-path-probe":"true"}},
      {"name":"status","annotations":{"pp:no-error-path-probe":"true"}}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"42"}]}'
    exit 0
  fi
  echo 'widget 1'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "soft-lookup" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Lookup a widget through a soft-failure endpoint.

Usage:
  fixture-pp-cli widgets soft-lookup <id> [flags]

Examples:
  fixture-pp-cli widgets soft-lookup 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "soft-lookup" ]; then
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","results":[]}'
    exit 0
  fi
  echo "soft lookup $3"
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "status" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Show widget service status.

Usage:
  fixture-pp-cli widgets status [flags]

Examples:
  fixture-pp-cli widgets status

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "status" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"ok":true}'
    exit 0
  fi
  echo 'ok'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeLiveDogfoodDestructiveFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"api-keys","subcommands":[
      {"name":"refresh","annotations":{"pp:endpoint":"api-keys.keys-refresh"}}
    ]},
    {"name":"widgets","subcommands":[
      {"name":"list"}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "api-keys" ] && [ "$2" = "refresh" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Refresh the current API key.

Usage:
  fixture-pp-cli api-keys refresh [flags]

Examples:
  fixture-pp-cli api-keys refresh

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "api-keys" ] && [ "$2" = "refresh" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"refreshed":true}'
    exit 0
  fi
  echo 'refreshed'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[]}'
    exit 0
  fi
  echo 'widgets'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func writeTestManifestForLiveDogfood(t *testing.T, dir string) {
	t.Helper()
	writeTestManifestForLiveDogfoodCLIName(t, dir, "fixture-pp-cli")
}

func writeTestManifestForLiveDogfoodCLIName(t *testing.T, dir, cliName string) {
	t.Helper()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       cliName,
		RunID:         "run-live-dogfood",
		AuthType:      "none",
	}))
}

// writeLiveDogfoodRichFixture builds a fake binary with multi-resource
// command families (widgets, gizmos, projects/tasks, failing-resource) plus
// search/delete commands. Each family's purpose is named in its section
// header below; the test names that consume them follow the same naming.
//
// IMPORTANT: companionSupportsLimit operates on RAW help text (not
// flags-section-scoped), so any --limit token anywhere in a companion's
// help — Examples included — makes the resolver append --limit 1.
// Companions where the test expects a bare call must keep --limit out of
// the help entirely; companions where the test expects --limit 1 must
// declare --limit in Flags.
func writeLiveDogfoodRichFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

# Argv logging side channel. Every invocation appends its argv (space-joined)
# when PRINTING_PRESS_TEST_ARGV_LOG is set. Defaults to no-op so tests that
# don't care about argv tracking work unchanged.
if [ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ]; then
  printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
fi

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"list"},
      {"name":"get"},
      {"name":"describe"},
      {"name":"search"},
      {"name":"search-no-json"},
      {"name":"search-positional"},
      {"name":"delete"}
    ]},
    {"name":"gizmos","subcommands":[
      {"name":"list"},
      {"name":"get"}
    ]},
    {"name":"projects","subcommands":[
      {"name":"list"},
      {"name":"tasks","subcommands":[
        {"name":"list"},
        {"name":"update"}
      ]}
    ]},
    {"name":"failing-resource","subcommands":[
      {"name":"list"},
      {"name":"get"},
      {"name":"describe"}
    ]},
    {"name":"completion","subcommands":[{"name":"bash"}]}
  ]
}
JSON
  exit 0
fi

# ---------- widgets family ----------

if [ "$1" = "widgets" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List widgets.

Usage:
  fixture-pp-cli widgets list [flags]

Examples:
  fixture-pp-cli widgets list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"42"}]}'
    exit 0
  fi
  echo 'widget 1'
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a widget.

Usage:
  fixture-pp-cli widgets get <id> [flags]

Examples:
  fixture-pp-cli widgets get 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42"}'
    exit 0
  fi
  echo "widget $3"
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "describe" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Describe a widget.

Usage:
  fixture-pp-cli widgets describe <id> [flags]

Examples:
  fixture-pp-cli widgets describe 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "describe" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","description":"a widget"}'
    exit 0
  fi
  echo "description of widget $3"
  exit 0
fi

# ---------- gizmos family (companion-supports-limit) ----------

if [ "$1" = "gizmos" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List gizmos.

Usage:
  fixture-pp-cli gizmos list [flags]

Examples:
  fixture-pp-cli gizmos list --json

Flags:
      --json     Output JSON
      --limit    Maximum results to return
HELP
  exit 0
fi

if [ "$1" = "gizmos" ] && [ "$2" = "list" ]; then
  # Resolver appends --limit 1 because --limit is declared in Flags. Match
  # only when both --json AND --limit are present, so a regression that
  # stops appending --limit (bare --json) falls through to the failure
  # branch instead of being silently accepted.
  case "$*" in
    *"--json"*"--limit"*|*"--limit"*"--json"*)
      echo '{"results":[{"id":"42"}]}'
      exit 0
      ;;
  esac
  echo 'gizmo 1'
  exit 0
fi

if [ "$1" = "gizmos" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a gizmo.

Usage:
  fixture-pp-cli gizmos get <id> [flags]

Examples:
  fixture-pp-cli gizmos get 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "gizmos" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42"}'
    exit 0
  fi
  echo "gizmo $3"
  exit 0
fi

# ---------- projects/tasks family (chained walk) ----------

if [ "$1" = "projects" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List projects.

Usage:
  fixture-pp-cli projects list [flags]

Examples:
  fixture-pp-cli projects list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "list" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"results":[{"id":"P1"}]}'
    exit 0
  fi
  echo 'project 1'
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "list" ] && [ "${4:-}" = "--help" ]; then
  cat <<'HELP'
List tasks within a project.

Usage:
  fixture-pp-cli projects tasks list <project-id> [flags]

Examples:
  fixture-pp-cli projects tasks list P1 --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "list" ]; then
  # ${4:-} is the resolved project-id (or the matrix walker's invalid-token
  # sentinel for error_path). ${5:-} is --json when supplied. We also handle
  # the self-companion case (4-arg "... list --json" with no project-id),
  # which fires when the resolver's findListCompanion picks "projects tasks
  # list" as the companion for itself; without this branch the matrix walker
  # would silently skip the bare list probe.
  if [ "${4:-}" = "__printing_press_invalid__" ]; then
    echo 'invalid project' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ] || [ "${5:-}" = "--json" ]; then
    echo '{"results":[{"id":"T7"}]}'
    exit 0
  fi
  echo 'task 1'
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "update" ] && [ "${4:-}" = "--help" ]; then
  cat <<'HELP'
Update a task within a project.

Usage:
  fixture-pp-cli projects tasks update <project-id> <task-id> [flags]

Examples:
  fixture-pp-cli projects tasks update P1 T7

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "projects" ] && [ "$2" = "tasks" ] && [ "${3:-}" = "update" ]; then
  # ${4:-} is project-id (or __printing_press_invalid__ for error_path).
  # ${5:-} is task-id (or --json for malformed error_path argv).
  if [ "${4:-}" = "__printing_press_invalid__" ]; then
    echo 'invalid project' >&2
    exit 2
  fi
  if [ "${6:-}" = "--json" ]; then
    echo '{"id":"T7","status":"updated"}'
    exit 0
  fi
  echo 'updated'
  exit 0
fi

# ---------- failing-resource family (negative cache) ----------

if [ "$1" = "failing-resource" ] && [ "$2" = "list" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
List failing-resource items.

Usage:
  fixture-pp-cli failing-resource list [flags]

Examples:
  fixture-pp-cli failing-resource list --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "list" ]; then
  # Always fail on the actual --json call so the resolver caches a sentinel.
  if [ "${3:-}" = "--json" ]; then
    echo 'upstream service unavailable' >&2
    exit 2
  fi
  echo 'failing-resource 1'
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a failing-resource item.

Usage:
  fixture-pp-cli failing-resource get <id> [flags]

Examples:
  fixture-pp-cli failing-resource get 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "get" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42"}'
    exit 0
  fi
  echo "failing-resource $3"
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "describe" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Describe a failing-resource item.

Usage:
  fixture-pp-cli failing-resource describe <id> [flags]

Examples:
  fixture-pp-cli failing-resource describe 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "failing-resource" ] && [ "$2" = "describe" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'not found' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","description":"a thing"}'
    exit 0
  fi
  echo "description of failing-resource $3"
  exit 0
fi

# ---------- widgets search family (U3 — search-shape error_path) ----------

# widgets search: --query flag + --json flag.
# Mode dispatch via PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE only affects the
# error_path probe (query == __printing_press_invalid__). Walker's happy_path
# and json_fidelity probes use a different query and always return valid JSON
# so they don't pollute test signal.
if [ "$1" = "widgets" ] && [ "$2" = "search" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search widgets.

Usage:
  fixture-pp-cli widgets search <query> [flags]

Examples:
  fixture-pp-cli widgets search --query foo --json

Flags:
      --json     Output JSON
      --query    Search query
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "search" ]; then
  # Args shape: widgets search --query <q> [--json]
  query="${4:-}"
  if [ "$query" = "__printing_press_invalid__" ]; then
    case "${PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE:-empty}" in
      fallback)
        echo '{"results":[{"id":"recent-1"},{"id":"recent-2"}]}'
        exit 0
        ;;
      nonzero)
        exit 4
        ;;
      invalid)
        echo '{not-json'
        exit 0
        ;;
      *)
        echo '{"results":[]}'
        exit 0
        ;;
    esac
  fi
  echo '{"results":[]}'
  exit 0
fi

# widgets search-no-json: --query flag, NO --json flag. Used to verify that
# search-shape error_path passes on exit 0 even without --json.
if [ "$1" = "widgets" ] && [ "$2" = "search-no-json" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search widgets without JSON support.

Usage:
  fixture-pp-cli widgets search-no-json <query> [flags]

Examples:
  fixture-pp-cli widgets search-no-json --query foo

Flags:
      --query    Search query
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "search-no-json" ]; then
  echo '0 results found.'
  exit 0
fi

# widgets search-positional: positional <query>, no --query flag. Used to
# verify error_path constructs the positional argv shape (no --query flag).
if [ "$1" = "widgets" ] && [ "$2" = "search-positional" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Search widgets via a positional query.

Usage:
  fixture-pp-cli widgets search-positional <query> [flags]

Examples:
  fixture-pp-cli widgets search-positional foo --json

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "search-positional" ]; then
  # Args shape: widgets search-positional <query> [--json]
  echo '{"results":[]}'
  exit 0
fi

# widgets delete: mutation-shape (no --query flag, no <query> positional;
# accepts <id>). error_path uses non-zero-required strategy (mutating-leaf
# deny-list overrides search-shape detection).
if [ "$1" = "widgets" ] && [ "$2" = "delete" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Delete a widget.

Usage:
  fixture-pp-cli widgets delete <id> [flags]

Examples:
  fixture-pp-cli widgets delete 42

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "delete" ]; then
  if [ "${3:-}" = "__printing_press_invalid__" ]; then
    echo 'invalid id' >&2
    exit 2
  fi
  if [ "${4:-}" = "--json" ]; then
    echo '{"id":"42","status":"deleted"}'
    exit 0
  fi
  echo 'deleted'
  exit 0
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

// readArgvLog returns the lines from the argv log file, with empty lines
// filtered out. Used by resolve-success and search/error_path tests to
// assert on subprocess invocation count and content.
func readArgvLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// countArgvLines returns the number of argv-log lines whose content contains
// every substring in `must`. Tests filter on `--json` (the actual companion
// call) to exclude `--help` invocations from companionSupportsLimit, which
// would otherwise inflate companion-call counts.
func countArgvLines(lines []string, must ...string) int {
	count := 0
outer:
	for _, line := range lines {
		for _, m := range must {
			if !strings.Contains(line, m) {
				continue outer
			}
		}
		count++
	}
	return count
}

// findResultByCommandKind locates a single matrix-walker result by command
// path and test kind. Used by resolve-success tests to assert on the
// post-resolution status of a specific probe.
func findResultByCommandKind(report *LiveDogfoodReport, command string, kind LiveDogfoodTestKind) *LiveDogfoodTestResult {
	for i := range report.Tests {
		if report.Tests[i].Command == command && report.Tests[i].Kind == kind {
			return &report.Tests[i]
		}
	}
	return nil
}

func countResultsWithReason(results []LiveDogfoodTestResult, reason string) int {
	count := 0
	for _, result := range results {
		if result.Reason == reason {
			count++
		}
	}
	return count
}

// setupRichFixture is the shared preamble for U2/U3 tests: skip on Windows,
// build the rich fixture, and enable the argv-log side channel via a unique
// per-test tempfile path. Returns the fixture dir, binary name, and the
// argv-log path (tests that don't read the log can ignore it).
func setupRichFixture(t *testing.T) (dir, binaryName, argvLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName = writeLiveDogfoodRichFixture(t)
	argvLog = filepath.Join(t.TempDir(), "argv.log")
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLog)
	return
}

// runRichFixtureMatrix runs the standard full-level matrix walk against the
// rich fixture with the same options every U2/U3 test uses. Fails the test on
// any RunLiveDogfood error.
func runRichFixtureMatrix(t *testing.T, dir, binaryName string) *LiveDogfoodReport {
	t.Helper()
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	return report
}

func requireSQLite3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for live dogfood store fixture tests")
	}
}

func writeLiveDogfoodStoreFixture(t *testing.T, seedTask bool) (dir string, binaryName string) {
	t.Helper()
	requireSQLite3(t)
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	dir = t.TempDir()
	binaryName = liveDogfoodStoreFixtureBinaryName(t.Name())
	writeTestManifestForLiveDogfoodCLIName(t, dir, binaryName)

	seedLine := ":"
	if seedTask {
		seedLine = `sqlite3 "$db" "INSERT OR REPLACE INTO resources(resource_type, id, data) VALUES('tasks', 'real-task-1', '{}')"`
	}
	binPath := filepath.Join(dir, binaryName)
	script := fmt.Sprintf(`#!/bin/sh
set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"sync"},
    {"name":"tasks","subcommands":[
      {"name":"get-task"}
    ]}
  ]
}
JSON
  exit 0
fi

if [ "$1" = "sync" ] && [ "${2:-}" = "--help" ]; then
  cat <<'HELP'
Sync records.

Usage:
  %[1]s sync [flags]

Examples:
  %[1]s sync

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "sync" ]; then
  db="$HOME/.local/share/%[1]s/data.db"
  mkdir -p "$(dirname "$db")"
  sqlite3 "$db" "CREATE TABLE IF NOT EXISTS resources (id TEXT NOT NULL, resource_type TEXT NOT NULL, data JSON NOT NULL, synced_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (resource_type, id))"
  %[2]s
  echo '{"synced":true}'
  exit 0
fi

if [ "$1" = "tasks" ] && [ "$2" = "get-task" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a task.

Usage:
  %[1]s tasks get-task <task-id> [flags]

Examples:
  %[1]s tasks get-task example-id

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "tasks" ] && [ "$2" = "get-task" ]; then
  if [ "${3:-}" = "real-task-1" ]; then
    if [ "${4:-}" = "--json" ]; then
      echo '{"id":"real-task-1"}'
    else
      echo 'real-task-1'
    fi
    exit 0
  fi
  echo 'HTTP 400: Invalid argument value' >&2
  exit 1
fi

echo "unexpected args: $*" >&2
exit 99
`, binaryName, seedLine)
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func liveDogfoodStoreFixtureBinaryName(testName string) string {
	name := strings.ToLower(testName)
	name = strings.NewReplacer("/", "-", "_", "-").Replace(name)
	return "fixture-" + name + "-pp-cli"
}

func TestRunLiveDogfoodStoreFixtureSourceUsesSyncedResourceID(t *testing.T) {
	dir, binaryName := writeLiveDogfoodStoreFixture(t, true)
	report := runRichFixtureMatrix(t, dir, binaryName)

	happy := findResultByCommandKind(report, "tasks get-task", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected tasks get-task happy_path result")
	assert.Equal(t, LiveDogfoodStatusPass, happy.Status, happy.Reason)
	assert.Equal(t, []string{"tasks", "get-task", "real-task-1"}, happy.Args)
	assert.Equal(t, "store", happy.FixtureSource)

	jsonResult := findResultByCommandKind(report, "tasks get-task", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult, "expected tasks get-task json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusPass, jsonResult.Status, jsonResult.Reason)
	assert.Equal(t, []string{"tasks", "get-task", "real-task-1", "--json"}, jsonResult.Args)
	assert.Equal(t, "store", jsonResult.FixtureSource)
}

func TestRunLiveDogfoodStoreFixtureSourceSkipsWhenStoreEmpty(t *testing.T) {
	dir, binaryName := writeLiveDogfoodStoreFixture(t, false)
	report := runRichFixtureMatrix(t, dir, binaryName)

	happy := findResultByCommandKind(report, "tasks get-task", LiveDogfoodTestHappy)
	require.NotNil(t, happy, "expected tasks get-task happy_path result")
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Equal(t, reasonRequiredParamFixture, happy.Reason)

	jsonResult := findResultByCommandKind(report, "tasks get-task", LiveDogfoodTestJSON)
	require.NotNil(t, jsonResult, "expected tasks get-task json_fidelity result")
	assert.Equal(t, LiveDogfoodStatusSkip, jsonResult.Status)
	assert.Equal(t, reasonRequiredParamFixture, jsonResult.Reason)
}

func TestRunLiveDogfoodResolveSuccessSinglePositional(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// The resolver substituted the id from companion widgets list --json
	// (which returned {"results":[{"id":"42"}]}) into widgets get,
	// producing argv = `widgets get 42`. The probe ran and returned exit 0.
	got := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, got, "expected widgets get happy_path in report")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	// Companion-leaf invariant: the resolver picked widgets list, not some
	// other allowlisted sibling (widgets search is also in crossAPIListVerbs
	// but sorts later alphabetically). Pin both directions: widgets list
	// must appear AS a companion call, AND widgets search must NOT appear
	// as one. The bare-companion shape `widgets search --json` (path +
	// --json, nothing else) only fires when findListCompanion picks search;
	// the walker's own probe of widgets search uses --query foo --json from
	// Examples, which doesn't match the bare companion shape.
	lines := readArgvLog(t, argvLog)
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets list", "--json"), 1,
		"expected widgets list --json to appear in argv log as the chosen companion")
	bareSearchCompanion := 0
	for _, line := range lines {
		if line == "widgets search --json" {
			bareSearchCompanion++
		}
	}
	assert.Equal(t, 0, bareSearchCompanion,
		"widgets search must NOT be picked as companion when widgets list is available; saw bare `widgets search --json` in argv log")
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets get 42"), 1,
		"expected widgets get 42 (post-substitution probe) to appear in argv log")
}

func TestRunLiveDogfoodResolveSuccessChainedMultiPositional(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// The chain walks projects list → projects tasks list P1, threading the
	// resolved P1 into the second list call. The final probe is
	// `projects tasks update P1 T7`.
	got := findResultByCommandKind(report, "projects tasks update", LiveDogfoodTestHappy)
	require.NotNil(t, got, "expected projects tasks update happy_path in report")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	lines := readArgvLog(t, argvLog)
	assert.GreaterOrEqual(t, countArgvLines(lines, "projects list", "--json"), 1,
		"expected projects list --json (depth-0 companion) in argv log")
	assert.GreaterOrEqual(t, countArgvLines(lines, "projects tasks list", "P1", "--json"), 1,
		"expected projects tasks list P1 --json (depth-1 companion threading P1) in argv log")
	assert.GreaterOrEqual(t, countArgvLines(lines, "projects tasks update P1 T7"), 1,
		"expected projects tasks update P1 T7 (post-chain probe) in argv log")
}

func TestRunLiveDogfoodResolveSuccessCacheHit(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Both siblings successfully resolve and run their probes.
	getResult := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, getResult)
	assert.Equal(t, LiveDogfoodStatusPass, getResult.Status, getResult.Reason)
	descResult := findResultByCommandKind(report, "widgets describe", LiveDogfoodTestHappy)
	require.NotNil(t, descResult)
	assert.Equal(t, LiveDogfoodStatusPass, descResult.Status, descResult.Reason)

	// Cache hit: one cached id serves every widgets-family id-shape sibling.
	// The walker probes `widgets list` itself for both happy_path and
	// json_fidelity, both of which invoke argv `widgets list --json`
	// (Examples already has --json, so appendJSONArg dedups and json_fidelity
	// reuses the happy argv). The resolver invokes the companion exactly
	// once for the first id-shape sibling probed; subsequent siblings hit
	// the cache and add 0.
	const walkerProbes, resolverCallsWithCacheHit = 2, 1
	const expectedTotal = walkerProbes + resolverCallsWithCacheHit
	lines := readArgvLog(t, argvLog)
	companionCalls := countArgvLines(lines, "widgets list", "--json")
	// Equality assertion (not <=) catches both directions: cache miss inflates
	// to expectedTotal+1, walker-side dedup deflates to expectedTotal-1. Bare
	// upper bound would silently pass on the second case.
	assert.Equal(t, expectedTotal, companionCalls,
		"expected exactly %d widgets list --json invocations (%d walker + %d resolver with cache hit); got %d", expectedTotal, walkerProbes, resolverCallsWithCacheHit, companionCalls)

	// Both probe argvs landed in the log post-substitution.
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets get 42"), 1)
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets describe 42"), 1)
}

func TestRunLiveDogfoodResolveSuccessCompanionLimit(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// gizmos list declares --limit in its Flags section, so the resolver
	// appends --limit 1 to the companion call before invoking it.
	got := findResultByCommandKind(report, "gizmos get", LiveDogfoodTestHappy)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	lines := readArgvLog(t, argvLog)
	// The resolver's gizmos list call must include both --json and --limit 1.
	// Order is resolver-dependent (currently --json --limit 1) so we assert
	// substring presence rather than exact ordering.
	limitCalls := countArgvLines(lines, "gizmos list", "--json", "--limit 1")
	assert.GreaterOrEqual(t, limitCalls, 1,
		"expected gizmos list call with both --json and --limit 1; got 0 such lines")
}

func TestRunLiveDogfoodResolveSuccessNegativeCacheSentinel(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// failing-resource list returns exit non-zero for --json. The walker
	// probes commands in alphabetical order: `failing-resource describe`
	// runs first and hits the FRESH failure (caches sentinel);
	// `failing-resource get` runs second and hits the cached sentinel.
	descResult := findResultByCommandKind(report, "failing-resource describe", LiveDogfoodTestHappy)
	require.NotNil(t, descResult)
	assert.Equal(t, LiveDogfoodStatusSkip, descResult.Status,
		"first sibling (describe) should skip with fresh companion-failure reason")
	assert.Contains(t, descResult.Reason, "list companion failed at depth",
		"first sibling reason should reference the actual depth-keyed failure, not the cached sentinel")

	getResult := findResultByCommandKind(report, "failing-resource get", LiveDogfoodTestHappy)
	require.NotNil(t, getResult)
	assert.Equal(t, LiveDogfoodStatusSkip, getResult.Status,
		"second sibling (get) should skip via the cached negative-cache sentinel")
	assert.Contains(t, getResult.Reason, "list companion previously failed at depth",
		"second sibling reason should reference the cached sentinel, not re-fail the companion")

	// Filter to the actual companion call (--json excludes --help). The
	// walker probes `failing-resource list` for both happy_path and
	// json_fidelity (both use argv `failing-resource list --json` since
	// Examples already includes --json). The resolver invokes the companion
	// once for the first sibling (describe), caches the sentinel, and the
	// second sibling (get) hits the sentinel without re-invoking.
	const walkerProbes, resolverCallsWithSentinel = 2, 1
	const expectedTotal = walkerProbes + resolverCallsWithSentinel
	lines := readArgvLog(t, argvLog)
	companionCalls := countArgvLines(lines, "failing-resource list", "--json")
	// Equality assertion (not <=) catches sentinel-bypass (4 calls) AND any
	// future walker-side dedup that would collapse to 2 calls without going
	// through the sentinel path.
	assert.Equal(t, expectedTotal, companionCalls,
		"expected exactly %d failing-resource list --json invocations (%d walker + %d resolver with sentinel); got %d", expectedTotal, walkerProbes, resolverCallsWithSentinel, companionCalls)
}

// ----- U3: search-aware error_path integration tests -----

func TestRunLiveDogfoodSearchErrorPathEmptyResults(t *testing.T) {
	// Mode unset → fixture returns exit 0 + {"results":[]} for the
	// __printing_press_invalid__ probe.
	dir, binaryName, _ := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got, "expected widgets search error_path in report")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
}

func TestRunLiveDogfoodErrorPathSkipsAnnotatedSoftFailureCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodSoftFailureFixture(t)
	argvLog := filepath.Join(t.TempDir(), "argv.log")
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLog)
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets soft-lookup", LiveDogfoodTestError)
	require.NotNil(t, got, "expected widgets soft-lookup error_path in report")
	assert.Equal(t, LiveDogfoodStatusSkip, got.Status, got.Reason)
	assert.Equal(t, reasonNoErrorPathProbeAnnotation, got.Reason)
	assert.Empty(t, got.Args, "skipped error_path must not include executable args")

	for _, kind := range []LiveDogfoodTestKind{LiveDogfoodTestHelp, LiveDogfoodTestHappy, LiveDogfoodTestJSON} {
		result := findResultByCommandKind(report, "widgets soft-lookup", kind)
		require.NotNil(t, result, "expected widgets soft-lookup %s in report", kind)
		assert.Equal(t, LiveDogfoodStatusPass, result.Status, result.Reason)
	}

	lines := readArgvLog(t, argvLog)
	assert.Equal(t, 0, countArgvLines(lines, "widgets soft-lookup", "__printing_press_invalid__"),
		"error_path probe must not invoke the binary for annotated soft-failure commands")
}

func TestRunLiveDogfoodErrorPathAnnotationPreservesNoPositionalSkip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodSoftFailureFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets status", LiveDogfoodTestError)
	require.NotNil(t, got, "expected widgets status error_path skip in report")
	assert.Equal(t, LiveDogfoodStatusSkip, got.Status, got.Reason)
	assert.Equal(t, "no positional argument", got.Reason)
}

func TestRunLiveDogfoodSearchErrorPathFallbackResults(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "fallback")
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Recency-fallback APIs return content under unmatched queries — exit 0
	// with non-empty results is a valid "no match" signal, not a failure.
	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
}

func TestRunLiveDogfoodSearchErrorPathNoJSONSupport(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Search-shape command without --json flag declared. Exit 0 alone is
	// sufficient when --json wasn't supplied — no JSON validation possible.
	got := findResultByCommandKind(report, "widgets search-no-json", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	// argv-log assertion: the error_path probe ran without --json.
	lines := readArgvLog(t, argvLog)
	assert.GreaterOrEqual(t, countArgvLines(lines, "widgets search-no-json", "--query", "__printing_press_invalid__"), 1,
		"expected error_path probe to use --query for the no-json search command")
}

func TestRunLiveDogfoodSearchErrorPathPositionalQuery(t *testing.T) {
	dir, binaryName, argvLog := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets search-positional", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)

	// argv-log assertion: probe used the positional argv shape, not --query
	// (the command has <query> in Usage but no --query flag in Flags).
	lines := readArgvLog(t, argvLog)
	positionalCalls := countArgvLines(lines, "widgets search-positional __printing_press_invalid__", "--json")
	assert.GreaterOrEqual(t, positionalCalls, 1,
		"expected error_path probe to use positional <query>, not --query flag")
	flagCalls := countArgvLines(lines, "widgets search-positional", "--query")
	assert.Equal(t, 0, flagCalls,
		"expected --query flag NOT to appear in error_path argv when command uses positional <query>")
}

func TestRunLiveDogfoodSearchErrorPathNonZeroExit(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "nonzero")
	report := runRichFixtureMatrix(t, dir, binaryName)

	// Non-zero exit is also a valid "no match" signal for some APIs;
	// search-shape error_path treats it as Pass, consistent with mutation.
	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
}

// TestRunLiveDogfoodSearchErrorPathInvalidJSON exercises the only fail mode
// currently produced by the search-shape error_path code in
// live_dogfood.go:693-711. INVARIANT: if the production code adds a new
// search-shape Fail branch (timeout, empty stdout under --json, schema
// mismatch), add a corresponding integration test in the same change.
func TestRunLiveDogfoodSearchErrorPathInvalidJSON(t *testing.T) {
	dir, binaryName, _ := setupRichFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "invalid")
	report := runRichFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets search", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusFail, got.Status,
		"search + invalid JSON under --json is the only search-shape error_path Fail mode")
	assert.Contains(t, got.Reason, "invalid JSON")
}

func TestRunLiveDogfoodSearchErrorPathMutationFallthrough(t *testing.T) {
	dir, binaryName, argvLogPath := setupRichFixture(t)
	report := runRichFixtureMatrix(t, dir, binaryName)

	// widgets delete is a mutating leaf (in mutatingVerbs). The error_path
	// probe is skipped without invoking the binary so that APIs which would
	// accept __printing_press_invalid__ as a real id (and queue or perform
	// the deletion) cannot mutate live data.
	got := findResultByCommandKind(report, "widgets delete", LiveDogfoodTestError)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusSkip, got.Status, got.Reason)
	assert.Equal(t, reasonMutatingErrorPath, got.Reason)
	assert.Empty(t, got.Args, "skipped error_path must not include executable mutation args")
	assert.Equal(t, 0, got.ExitCode, "skipped error_path must not record a real exit code")

	// Defense-in-depth: the binary must not have been invoked with the
	// invalid-id sentinel. Status=Skip alone is structurally distinct from
	// a Pass that ran the probe, but a direct argv-log check makes the
	// "no live invocation" invariant explicit.
	lines := readArgvLog(t, argvLogPath)
	assert.Equal(t, 0, countArgvLines(lines, "delete", "__printing_press_invalid__"),
		"error_path probe must not invoke the binary for a mutating command")
}

// writeLiveDogfoodDryRunFixture builds a CLI binary that exposes three
// commands so the matrix-level dry-run behavior can be exercised end-to-end:
//
//   - widgets create — mutator that advertises --dry-run. Exits 0 with a
//     valid JSON envelope only when --dry-run is present in argv. Without
//     --dry-run, exits 1 (modeling the live-API placeholder-body rejection
//     class). When PRINTING_PRESS_TEST_DRY_RUN_BROKEN=1, exits 1 even with
//     --dry-run, modeling a broken dry-run preview (R3).
//   - widgets destroy — mutator (leaf in mutatingVerbs) that does NOT
//     advertise --dry-run (hand-written novel command shape). The matrix
//     should fail the gate's second leg and keep today behavior: no
//     --dry-run injection and no error_path_real entry.
//   - widgets get — non-mutator (read). Advertises --dry-run via the
//     persistent root flag, but the matrix must not inject because the leaf
//     is not in mutatingVerbs.
func writeLiveDogfoodDryRunFixture(t *testing.T) (dir string, binaryName string) {
	t.Helper()

	dir = t.TempDir()
	binaryName = "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	binPath := filepath.Join(dir, binaryName)
	script := `#!/bin/sh
set -u

# Argv logging side channel — same convention as setupRichFixture. Tests
# that don't set PRINTING_PRESS_TEST_ARGV_LOG see no behavior change.
if [ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ]; then
  printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"
fi

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"widgets","subcommands":[
      {"name":"create"},
      {"name":"destroy"},
      {"name":"get"},
      {"name":"update"}
    ]}
  ]
}
JSON
  exit 0
fi

# argv contains --dry-run (anywhere)?
has_dry_run=0
for a in "$@"; do
  case "$a" in
    --dry-run|--dry-run=*) has_dry_run=1 ;;
  esac
done

# ---------- widgets create: mutator with --dry-run advertised ----------
if [ "$1" = "widgets" ] && [ "$2" = "create" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Create a widget.

Usage:
  fixture-pp-cli widgets create [flags]

Examples:
  fixture-pp-cli widgets create --name=demo

Flags:
      --json   Output JSON

Global Flags:
      --dry-run   Show request without sending
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "create" ]; then
  if [ "${PRINTING_PRESS_TEST_DRY_RUN_BROKEN:-0}" = "1" ]; then
    echo 'broken dry-run preview' >&2
    exit 1
  fi
  if [ "$has_dry_run" = "1" ]; then
    # Dry-run preview envelope shape mirrors what the generated CLI emits.
    echo '{"action":"post","resource":"widgets","path":"/widgets","status":0,"success":false,"dry_run":true}'
    exit 0
  fi
  if [ "${PRINTING_PRESS_TEST_PERMISSIVE_API:-0}" = "1" ]; then
    # Models an over-permissive API that quietly accepts placeholder bodies;
    # error_path_real should surface this as a Fail.
    echo '{"id":"created"}'
    exit 0
  fi
  echo 'API rejected: missing required body field' >&2
  exit 1
fi

# ---------- widgets destroy: mutator (leaf in mutatingVerbs) WITHOUT --dry-run advertised ----------
if [ "$1" = "widgets" ] && [ "$2" = "destroy" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Destroy widgets matching a filter (hand-written novel command).

Usage:
  fixture-pp-cli widgets destroy [flags]

Examples:
  fixture-pp-cli widgets destroy --filter=stale

Flags:
      --filter string   Filter expression
      --json            Output JSON
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "destroy" ]; then
  if [ "$has_dry_run" = "1" ]; then
    # If the matrix injects --dry-run despite this command not advertising it,
    # the fixture surfaces it as a failure so the test catches the regression.
    echo 'unexpected --dry-run on a command that does not support it' >&2
    exit 99
  fi
  echo '{"destroyed":[]}'
  exit 0
fi

# ---------- widgets get: read command with --dry-run advertised globally ----------
if [ "$1" = "widgets" ] && [ "$2" = "get" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Get a widget.

Usage:
  fixture-pp-cli widgets get [flags]

Examples:
  fixture-pp-cli widgets get

Flags:
      --json   Output JSON

Global Flags:
      --dry-run   Show request without sending
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "get" ]; then
  if [ "$has_dry_run" = "1" ]; then
    # Read commands must never receive --dry-run from the matrix; surface as fail.
    echo 'unexpected --dry-run on read command' >&2
    exit 99
  fi
  echo '{"id":"42"}'
  exit 0
fi

# ---------- widgets update <id>: mutator with positional + no list companion ----------
# Used to exercise resolveCommandPositionals skip path: the agent-context
# above does NOT expose a list-shape sibling in the widgets/* parent, so the
# chain walker can't source a real id, and both happy_path and
# error_path_real should be skipped with the same reason.
if [ "$1" = "widgets" ] && [ "$2" = "update" ] && [ "${3:-}" = "--help" ]; then
  cat <<'HELP'
Update a widget.

Usage:
  fixture-pp-cli widgets update <id> [flags]

Examples:
  fixture-pp-cli widgets update 42 --name=demo

Flags:
      --json   Output JSON

Global Flags:
      --dry-run   Show request without sending
HELP
  exit 0
fi

if [ "$1" = "widgets" ] && [ "$2" = "update" ]; then
  echo 'unexpected widgets update invocation; matrix should have skipped' >&2
  exit 99
fi

echo "unexpected args: $*" >&2
exit 99
`
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))
	return dir, binaryName
}

func runDryRunFixtureMatrix(t *testing.T, dir, binaryName string) *LiveDogfoodReport {
	t.Helper()
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)
	return report
}

func TestRunLiveDogfoodInjectsDryRunForMutator(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets create", LiveDogfoodTestHappy)
	require.NotNil(t, got, "expected widgets create happy_path result in matrix")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	assert.Contains(t, got.Args, "--dry-run",
		"expected matrix to inject --dry-run into happy_path on a mutator that advertises it")
}

func TestRunLiveDogfoodJSONFidelityInheritsDryRunForMutator(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets create", LiveDogfoodTestJSON)
	require.NotNil(t, got, "expected widgets create json_fidelity result in matrix")
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	assert.Contains(t, got.Args, "--dry-run",
		"expected json_fidelity to inherit --dry-run from happy_path injection")
	assert.Contains(t, got.Args, "--json",
		"expected json_fidelity to add --json on top of the dry-run base")
}

func TestRunLiveDogfoodHappyPathFailsOnBrokenDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	t.Setenv("PRINTING_PRESS_TEST_DRY_RUN_BROKEN", "1")
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	// R3: A CLI whose generated --dry-run is broken still surfaces a
	// happy_path failure — the dry-run injection must not mask binary-level
	// failures.
	got := findResultByCommandKind(report, "widgets create", LiveDogfoodTestHappy)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusFail, got.Status,
		"broken --dry-run preview must surface as happy_path failure (R3)")
}

func TestRunLiveDogfoodSkipsDryRunInjectionForMutatorWithoutFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	// widgets destroy has a leaf in mutatingVerbs but its help does NOT
	// advertise --dry-run. The gate's second leg falsifies, so the matrix
	// must keep today-behavior. The fixture surfaces a regression as exit 99.
	got := findResultByCommandKind(report, "widgets destroy", LiveDogfoodTestHappy)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	assert.NotContains(t, got.Args, "--dry-run",
		"expected matrix to skip --dry-run injection for mutators without the flag advertised")
}

func TestRunLiveDogfoodSkipsDryRunInjectionForReadCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	// widgets get is a read command. Even though --dry-run is advertised
	// (as a global flag), the leaf is not in mutatingVerbs, so injection
	// must not happen. Fixture surfaces a regression as exit 99.
	got := findResultByCommandKind(report, "widgets get", LiveDogfoodTestHappy)
	require.NotNil(t, got)
	assert.Equal(t, LiveDogfoodStatusPass, got.Status, got.Reason)
	assert.NotContains(t, got.Args, "--dry-run",
		"expected matrix to skip --dry-run injection on non-mutator read commands")
}

func TestRunLiveDogfoodSkipsErrorPathRealForMutatorWithDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets create", LiveDogfoodTestErrorReal)
	require.NotNil(t, got, "expected error_path_real entry for mutator advertising --dry-run")
	assert.Equal(t, LiveDogfoodStatusSkip, got.Status, got.Reason)
	assert.Equal(t, reasonMutatingDryRunOnly, got.Reason)
	assert.Empty(t, got.Args, "skipped error_path_real must not include executable mutation args")
}

func TestRunLiveDogfoodSkipsErrorPathRealForReadCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets get", LiveDogfoodTestErrorReal)
	assert.Nil(t, got, "non-mutator commands must not produce an error_path_real entry")
}

func TestRunLiveDogfoodSkipsErrorPathRealForMutatorWithoutDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	got := findResultByCommandKind(report, "widgets destroy", LiveDogfoodTestErrorReal)
	assert.Nil(t, got, "mutators without --dry-run advertised must not produce an error_path_real entry")
}

func TestRunLiveDogfoodErrorPathRealSkipMatchesHappyPathReason(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	// widgets update <id> is a mutator advertising --dry-run, but the fixture
	// has no list companion under widgets/*, so positional resolution skips.
	// happy_path and error_path_real must skip with the same reason for
	// triage parity.
	happy := findResultByCommandKind(report, "widgets update", LiveDogfoodTestHappy)
	errorReal := findResultByCommandKind(report, "widgets update", LiveDogfoodTestErrorReal)
	require.NotNil(t, happy)
	require.NotNil(t, errorReal)
	assert.Equal(t, LiveDogfoodStatusSkip, happy.Status)
	assert.Equal(t, LiveDogfoodStatusSkip, errorReal.Status)
	assert.Equal(t, happy.Reason, errorReal.Reason,
		"error_path_real skip reason must match happy_path skip reason")
	assert.Contains(t, errorReal.Reason, "no list companion",
		"resolve-skipped reason should surface the list-companion gap")
}

func TestRunLiveDogfoodSkipsErrorPathForMutatorWithDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	argvLogPath := filepath.Join(t.TempDir(), "argv.log")
	t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", argvLogPath)
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	// widgets update <id> is a mutator that advertises --dry-run and takes a
	// positional argument. The fixture is wired to exit 99 ("matrix should
	// have skipped") if invoked. With the fix the error_path probe must
	// skip outright instead of running `widgets update __printing_press_invalid__`
	// against the live API — even though --dry-run could be injected, the
	// error_path's invalid-argument semantics are not compatible with a
	// dry-run preview, so the safe action is to skip.
	got := findResultByCommandKind(report, "widgets update", LiveDogfoodTestError)
	require.NotNil(t, got, "expected widgets update error_path result in matrix")
	assert.Equal(t, LiveDogfoodStatusSkip, got.Status, got.Reason)
	assert.Equal(t, reasonMutatingErrorPath, got.Reason)
	assert.Empty(t, got.Args, "skipped error_path must not include executable mutation args")

	// Defense-in-depth: assert the binary was never invoked with the
	// invalid-id sentinel. The exit-99 sentinel in the fixture already
	// catches regression via the Status/Args/ExitCode assertions, but
	// stating the "no live invocation" invariant directly is clearer.
	lines := readArgvLog(t, argvLogPath)
	assert.Equal(t, 0, countArgvLines(lines, "update", "__printing_press_invalid__"),
		"error_path probe must not invoke the binary for a mutating command")
}

// TestRunLiveDogfoodErrorPathRealReportContribution locks in the matrix
// counters so the new test kind threads through finalizeLiveDogfoodReport
// without weakening the verdict math (which counts by Status, not by Kind).
func TestRunLiveDogfoodErrorPathRealReportContribution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	dir, binaryName := writeLiveDogfoodDryRunFixture(t)
	report := runDryRunFixtureMatrix(t, dir, binaryName)

	// Sanity: report is non-FAIL even though widgets update is fully skipped.
	// Counters should sum to MatrixSize and the verdict should be PASS.
	assert.Equal(t, "PASS", report.Verdict, report.Tests)
	assert.Equal(t, report.Passed+report.Failed, report.MatrixSize,
		"MatrixSize should equal Passed + Failed (skipped entries do not contribute)")
	assert.Equal(t, 0, report.Failed)
}

func TestLiveDogfoodHappyArgsHonorsPPHappyArgs(t *testing.T) {
	// pp:happy-args supplies a real value, overriding the Example placeholder
	// ("example-value") that strict upstream validators reject with HTTP 400.
	flagCmd := liveDogfoodCommand{
		Path:        []string{"users", "get-by-ids"},
		Help:        "Usage:\n  cli users get-by-ids [flags]\n\nExamples:\n  cli users get-by-ids --ids example-value\n",
		Annotations: map[string]string{happyArgsAnnotation: "--ids=12"},
	}
	args, ok := liveDogfoodHappyArgs(flagCmd)
	require.True(t, ok)
	assert.Equal(t, []string{"users", "get-by-ids", "--ids", "12"}, args,
		"flag-form pp:happy-args must override the Example placeholder")

	// Positional form: <name>=value contributes the value as a positional arg.
	// resolveCommandPositionals must NOT re-resolve (or skip) it even when the
	// Usage line carries an <id> placeholder and no list companion is reachable.
	posCmd := liveDogfoodCommand{
		Path:        []string{"tweets", "get"},
		Help:        "Usage:\n  cli tweets get <id> [flags]\n",
		Annotations: map[string]string{happyArgsAnnotation: "<id>=1750000000000000000"},
	}
	args, ok = liveDogfoodHappyArgs(posCmd)
	require.True(t, ok)
	assert.Equal(t, []string{"tweets", "get", "1750000000000000000"}, args)
	resolved, skipped, reason, _ := resolveCommandPositionals(posCmd, args, resolveCtx{})
	assert.False(t, skipped, "pp:happy-args positional must not be skipped: %s", reason)
	assert.Equal(t, []string{"tweets", "get", "1750000000000000000"}, resolved,
		"resolveCommandPositionals must preserve the pp:happy-args positional value")

	// Empty annotation falls through to the Example-derivation path.
	emptyCmd := liveDogfoodCommand{
		Path:        []string{"users", "get-by-ids"},
		Help:        "Usage:\n  cli users get-by-ids [flags]\n\nExamples:\n  cli users get-by-ids --ids example-value\n",
		Annotations: map[string]string{happyArgsAnnotation: ""},
	}
	args, ok = liveDogfoodHappyArgs(emptyCmd)
	require.True(t, ok)
	assert.Equal(t, []string{"users", "get-by-ids", "--ids", "example-value"}, args,
		"empty pp:happy-args must fall through to Example derivation")
}

func TestHappyArgsContainSyntheticFlagPlaceholder(t *testing.T) {
	assert.True(t, happyArgsContainSyntheticFlagPlaceholder(
		[]string{"events", "list", "--account-id", "550e8400-e29b-41d4-a716-446655440000", "--calendar-ids", "example-value"},
		[]string{"events", "list"},
	))
	assert.True(t, happyArgsContainSyntheticFlagPlaceholder(
		[]string{"users", "get-by-ids", "--ids=example-value"},
		[]string{"users", "get-by-ids"},
	))
	assert.True(t, happyArgsContainSyntheticFlagPlaceholder(
		[]string{"keys", "list", "--api-key", "your-token-here"},
		[]string{"keys", "list"},
	))
	assert.False(t, happyArgsContainSyntheticFlagPlaceholder(
		[]string{"widgets", "search", "--query", "example-value"},
		[]string{"widgets", "search"},
	))
}

// TestLiveDogfoodSkipsInteractiveAuthCommands locks in that the live matrix
// skips promoted login/logout (interactive OAuth lifecycle commands) the same
// way it already skips the "auth" parent. Probing their happy path would bind a
// local callback port and block on a browser redirect until the per-command
// timeout, so they must never enter the command list.
func TestLiveDogfoodSkipsInteractiveAuthCommands(t *testing.T) {
	t.Parallel()

	roots := []dogfoodAgentCommand{
		{Name: "login"},
		{Name: "logout"},
		{Name: "auth", Subcommands: []dogfoodAgentCommand{{Name: "login"}}},
		{Name: "tasks", Subcommands: []dogfoodAgentCommand{{Name: "list"}}},
	}
	var cmds []liveDogfoodCommand
	for _, root := range roots {
		collectLiveDogfoodCommands(nil, root, &cmds)
	}

	var names []string
	for _, c := range cmds {
		names = append(names, strings.Join(c.Path, " "))
	}

	assert.NotContains(t, names, "login")
	assert.NotContains(t, names, "logout")
	assert.NotContains(t, names, "auth login")
	assert.Contains(t, names, "tasks list", "non-auth commands must still be collected")
}
