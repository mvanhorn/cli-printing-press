package pipeline

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveDogfoodRotatingRefreshEnvVars(t *testing.T) {
	t.Parallel()

	assert.Empty(t, liveDogfoodRotatingRefreshEnvVars(CLIManifest{AuthType: "api_key", AuthEnvVars: []string{"FOO_API_KEY"}}, "FOO_API_KEY"))
	assert.Empty(t, liveDogfoodRotatingRefreshEnvVars(CLIManifest{AuthType: "bearer_token"}, "TOKEN"))

	got := liveDogfoodRotatingRefreshEnvVars(CLIManifest{
		AuthType:    spec.AuthTypeOAuth2Refresh,
		AuthEnvVars: []string{"CASDOOR_CLIENT_ID", "CASDOOR_CLIENT_SECRET", "CASDOOR_REFRESH_TOKEN"},
	}, "CASDOOR_REFRESH_TOKEN")
	assert.Equal(t, []string{"CASDOOR_REFRESH_TOKEN"}, got)

	got = liveDogfoodRotatingRefreshEnvVars(CLIManifest{
		AuthType: spec.AuthTypeOAuth2Refresh,
		AuthEnvVarSpecs: []spec.AuthEnvVar{
			{Name: "VENDOR_REFRESH_TOKEN", Kind: spec.AuthEnvVarKindAuthFlowInput},
		},
	}, "MY_CUSTOM_TOKEN")
	assert.Equal(t, []string{"VENDOR_REFRESH_TOKEN", "MY_CUSTOM_TOKEN"}, got)
}

func TestLiveDogfoodFileHasRefreshToken(t *testing.T) {
	t.Parallel()

	assert.False(t, liveDogfoodFileHasRefreshToken(nil))
	assert.False(t, liveDogfoodFileHasRefreshToken([]byte("client_id = \"abc\"\n")))
	assert.False(t, liveDogfoodFileHasRefreshToken([]byte("refresh_token = \"\"\n")))
	assert.True(t, liveDogfoodFileHasRefreshToken([]byte("refresh_token = \"abc\"\n")))
	assert.True(t, liveDogfoodFileHasRefreshToken([]byte("access_token = \"a\"\nrefresh_token = \"b\"\n")))
}

func TestTomlQuotedStringEscapes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `"plain"`, tomlQuotedString("plain"))
	assert.Equal(t, `"say \"hi\""`, tomlQuotedString(`say "hi"`))
	assert.Equal(t, `"a\\b"`, tomlQuotedString(`a\b`))
	assert.Equal(t, `"line\nnext"`, tomlQuotedString("line\nnext"))
}

func TestInvalidGrantCascadeTracker(t *testing.T) {
	t.Parallel()

	tracker := invalidGrantCascadeTracker{}
	assert.False(t, tracker.observe([]LiveDogfoodTestResult{{
		Kind: LiveDogfoodTestHelp, Status: LiveDogfoodStatusPass,
	}}))
	assert.False(t, tracker.observe([]LiveDogfoodTestResult{{
		Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass,
	}}))
	assert.True(t, tracker.observe([]LiveDogfoodTestResult{{
		Kind:         LiveDogfoodTestHappy,
		Status:       LiveDogfoodStatusFail,
		OutputSample: `Error: refreshing access token: HTTP 400: {"error":"invalid_grant"}`,
	}}))
}

func TestRunLiveDogfoodAuthEnvRefreshTokenUsesSharedCredentialFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const (
		binaryName = "fixture-pp-cli"
		authEnv    = "FIXTURE_REFRESH_TOKEN"
		original   = "env-refresh-token"
		rotated    = "rotated-refresh-token"
	)

	operatorHome := t.TempDir()
	t.Setenv("HOME", operatorHome)
	t.Setenv(authEnv, original)

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      spec.AuthTypeOAuth2Refresh,
		AuthEnvVars:   []string{"FIXTURE_CLIENT_ID", "FIXTURE_CLIENT_SECRET", authEnv},
		SpecFormat:    "openapi3",
	}))
	writeLiveDogfoodRotatingRefreshStub(t, dir, binaryName, original, rotated)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
		AuthEnv:    authEnv,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)

	got, err := os.ReadFile(filepath.Join(operatorHome, ".local", "share", binaryName, "credentials.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(got), rotated)
	assert.NotContains(t, string(got), original)
}

func TestRunLiveDogfoodSyncsOAuth2RefreshCredentialsTomlBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const (
		binaryName = "fixture-pp-cli"
		original   = "old-refresh"
		rotated    = "new-refresh"
	)

	operatorHome := t.TempDir()
	t.Setenv("HOME", operatorHome)
	credsPath := filepath.Join(operatorHome, ".local", "share", binaryName, "credentials.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(credsPath), 0o700))
	require.NoError(t, os.WriteFile(credsPath, []byte("refresh_token = \""+original+"\"\n"), 0o600))
	t.Setenv("FIXTURE_REFRESH_TOKEN", original)

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      spec.AuthTypeOAuth2Refresh,
		AuthEnvVars:   []string{"FIXTURE_REFRESH_TOKEN"},
		SpecFormat:    "openapi3",
	}))
	writeLiveDogfoodRotatingRefreshStub(t, dir, binaryName, original, rotated)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
		AuthEnv:    "FIXTURE_REFRESH_TOKEN",
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)

	got, err := os.ReadFile(credsPath)
	require.NoError(t, err)
	assert.Contains(t, string(got), rotated)
	assert.NotContains(t, string(got), original)
}

func TestRunLiveDogfoodAPIKeyAuthEnvUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const (
		binaryName = "fixture-pp-cli"
		authEnv    = "FIXTURE_API_KEY"
	)

	t.Setenv("HOME", t.TempDir())
	t.Setenv(authEnv, "static-api-key")

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      "api_key",
		AuthEnvVars:   []string{authEnv},
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
  if [ -z "${FIXTURE_API_KEY:-}" ]; then
    echo "api key env missing from subprocess" >&2
    exit 1
  fi
  if [ "$FIXTURE_API_KEY" != "static-api-key" ]; then
    echo "api key env mutated" >&2
    exit 1
  fi
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
		AuthEnv:    authEnv,
	})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict, report.Tests)
}

func TestRunLiveDogfoodAbortsInvalidGrantCascade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}

	const binaryName = "fixture-pp-cli"

	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       binaryName,
		RunID:         "run-live-dogfood",
		AuthType:      spec.AuthTypeOAuth2Refresh,
		SpecFormat:    "openapi3",
	}))
	writeStubBinary(t, dir, binaryName, `set -u

if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{
  "commands": [
    {"name":"account","subcommands":[{"name":"show"}]},
    {"name":"billing","subcommands":[{"name":"list"}]},
    {"name":"catalog","subcommands":[{"name":"list"}]}
  ]
}
JSON
  exit 0
fi

if [ "${2:-}" = "--help" ] || [ "${3:-}" = "--help" ]; then
  cat <<HELP
Show $1 $2.

Usage:
  fixture-pp-cli $1 $2 [flags]

Examples:
  fixture-pp-cli $1 $2

Flags:
      --json    Output JSON
HELP
  exit 0
fi

if [ "$1" = "account" ] && [ "$2" = "show" ]; then
  if [ "${3:-}" = "--json" ]; then
    echo '{"ok":true}'
    exit 0
  fi
  echo 'ok'
  exit 0
fi

if [ "$1" = "billing" ] && [ "$2" = "list" ]; then
  echo 'Error: refreshing access token: HTTP 400: {"error":"invalid_grant","error_description":"refresh token is invalid, expired or revoked"}' >&2
  exit 4
fi

if [ "$1" = "catalog" ]; then
  echo "should have aborted before catalog" >&2
  exit 99
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
	assert.Equal(t, "FAIL", report.Verdict)

	diag := findResultByCommandKind(report, "live-dogfood", LiveDogfoodTestHappy)
	require.NotNil(t, diag)
	assert.Equal(t, LiveDogfoodStatusFail, diag.Status)
	assert.Equal(t, reasonRefreshTokenRotationCascade, diag.Reason)

	catalogHelp := findResultByCommandKind(report, "catalog list", LiveDogfoodTestHelp)
	require.NotNil(t, catalogHelp)
	assert.Equal(t, LiveDogfoodStatusSkip, catalogHelp.Status)
	assert.Equal(t, reasonRefreshTokenRotationCascade, catalogHelp.Reason)

	for _, result := range report.Tests {
		assert.NotEqual(t, 99, result.ExitCode, "catalog must not execute after invalid_grant cascade")
	}
}

func TestWriteLiveDogfoodCredentialFileIfUnchangedCreatesMissingSrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "operator", "credentials.toml")
	dst := filepath.Join(dir, "scoped", "credentials.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o700))
	require.NoError(t, os.WriteFile(dst, []byte("refresh_token = \"rotated\"\n"), 0o600))

	err := writeLiveDogfoodCredentialFileIfUnchanged(liveDogfoodCredentialMirror{
		src:         src,
		dst:         dst,
		mode:        0o600,
		allowCreate: true,
	}, []byte("refresh_token = \"rotated\"\n"))
	require.NoError(t, err)

	got, err := os.ReadFile(src)
	require.NoError(t, err)
	assert.Equal(t, "refresh_token = \"rotated\"\n", string(got))
}

func writeLiveDogfoodRotatingRefreshStub(t *testing.T, dir, binaryName, original, rotated string) {
	t.Helper()
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
  if [ -n "${FIXTURE_REFRESH_TOKEN:-}" ]; then
    echo "rotating refresh token leaked via env" >&2
    exit 1
  fi
  creds="$HOME/.local/share/fixture-pp-cli/credentials.toml"
  if [ ! -f "$creds" ]; then
    echo "missing sandbox credentials.toml" >&2
    exit 1
  fi
  if grep -q '`+original+`' "$creds"; then
    printf 'refresh_token = "%s"\naccess_token = "fresh-access"\n' '`+rotated+`' > "$creds"
  elif ! grep -q '`+rotated+`' "$creds"; then
    echo "unexpected credential file" >&2
    cat "$creds" >&2
    exit 1
  fi
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
}
