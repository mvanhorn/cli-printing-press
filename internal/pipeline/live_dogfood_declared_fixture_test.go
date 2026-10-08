package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHappyArgsDeclareFixtureBlockedFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		annotation string
		want       bool
	}{
		{name: "cursor placeholder", annotation: "--cursor=example-value", want: true},
		{name: "path placeholder among real flags", annotation: "--limit=5;--path=example-value", want: true},
		{name: "url token placeholder", annotation: "--url=your-token-here", want: true},
		{name: "synthetic uuid on non-id flag", annotation: "--parent=550e8400-e29b-41d4-a716-446655440000", want: true},
		{name: "quoted placeholder", annotation: `--cursor="example-value"`, want: true},
		{name: "real value", annotation: "--path=/reports/q1.csv", want: false},
		{name: "bare boolean flag", annotation: "--recursive", want: false},
		{name: "positional placeholder is handled elsewhere", annotation: "cursor=example-value", want: false},
		{name: "empty", annotation: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, happyArgsDeclareFixtureBlockedFlag(parseHappyArgsAnnotation(tt.annotation)))
		})
	}
}

func TestHappyPathSyntheticParamFixtureSkipDeclaredFlagPlaceholder(t *testing.T) {
	t.Parallel()

	help := `Usage:
  cli items list-continue [flags]

Examples:
  cli items list-continue --cursor example-value

Flags:
      --cursor string   Continuation cursor
`
	tests := []struct {
		name      string
		happyArgs string
		mutating  bool
		want      string
	}{
		{
			// Example-derived placeholders stay name-gated: a cursor flag is
			// not id-shaped, so the row still runs.
			name: "example-derived placeholder on non-id flag still runs",
			want: "",
		},
		{
			name:      "declared placeholder on non-id flag is fixture-blocked",
			happyArgs: "--cursor=example-value",
			want:      reasonRequiredParamFixture,
		},
		{
			name:      "declared real value still runs",
			happyArgs: "--cursor=AAEcursor",
			want:      "",
		},
		{
			// Mutators preview with --dry-run, so a placeholder never reaches
			// the API and the row keeps running.
			name:      "declared placeholder on a dry-run mutator still runs",
			happyArgs: "--cursor=example-value",
			mutating:  true,
			want:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			annotations := map[string]string{mcpReadOnlyAnnotation: "true"}
			if tt.mutating {
				annotations = map[string]string{endpointMethodAnnotation: "POST"}
			}
			if tt.happyArgs != "" {
				annotations[happyArgsAnnotation] = tt.happyArgs
			}
			cmd := liveDogfoodCommand{
				Path:        []string{"items", "list-continue"},
				Help:        help,
				Annotations: annotations,
			}
			args, ok, parsed := liveDogfoodHappyArgsParsed(cmd)
			require.True(t, ok)
			assert.Equal(t, tt.want, happyPathSyntheticParamFixtureSkip(cmd, args, parsed, false))
		})
	}
}

func TestRunLiveDogfoodDeclaredFlagPlaceholderIsBlockedFixture(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)

	script := `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[
  {"name":"items","subcommands":[
    {"name":"list-continue","annotations":{"pp:method":"POST","mcp:read-only":"true","pp:happy-args":"--cursor=example-value"}},
    {"name":"get-link","annotations":{"pp:method":"POST","mcp:read-only":"true","pp:happy-args":"--path=/real/report.csv"}}
  ]}
]}
JSON
  exit 0
fi
if [ "${3:-}" = "--help" ]; then
  cat <<HELP
Items.

Usage:
  fixture-pp-cli items $2 [flags]

Examples:
  fixture-pp-cli items $2 --cursor abc

Flags:
      --cursor string   Continuation cursor
      --path string     Resource path
      --json            Output JSON
HELP
  exit 0
fi
echo 'HTTP 409: {"error_summary":"not_found/"}' >&2
exit 3
`
	writeStubBinary(t, dir, binaryName, script)

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:     dir,
		BinaryName: binaryName,
		Level:      "full",
		Timeout:    2 * time.Second,
	})
	require.NoError(t, err)

	for _, kind := range []LiveDogfoodTestKind{LiveDogfoodTestHappy, LiveDogfoodTestJSON} {
		blocked := findResultByCommandKind(report, "items list-continue", kind)
		require.NotNil(t, blocked, kind)
		assert.Equal(t, LiveDogfoodStatusSkip, blocked.Status, kind)
		assert.Equal(t, reasonRequiredParamFixture, blocked.Reason, kind)
	}

	// A real declared argument that the API rejects is still a CLI-visible
	// failure, not a blocked fixture.
	real := findResultByCommandKind(report, "items get-link", LiveDogfoodTestHappy)
	require.NotNil(t, real)
	assert.Equal(t, LiveDogfoodStatusFail, real.Status, real.Reason)
	assert.Contains(t, strings.Join(real.Args, " "), "/real/report.csv")
}
