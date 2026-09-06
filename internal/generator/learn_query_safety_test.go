package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func generateLearnQuerySafetyCLI(t *testing.T, name string) string {
	t.Helper()
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), name+"-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	return outputDir
}

// TestGenerateLearnQuerySafety_AuditOmitsRawQuery pins the emitted
// teach/playbook/log surfaces: audit and teach.log persist a hash (and
// a redacted normalized form), never the raw query, and forget scrubs
// matching log lines. Behavioral proof is the emitted CLI tests.
func TestGenerateLearnQuerySafety_AuditOmitsRawQuery(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnQuerySafetyCLI(t, "qsafe")

	teach := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	for _, want := range []string{
		"func queryHashRef(",
		"func scrubLearningsLogs(",
		"delete(entry, \"query\")",
		"learn.QueryHash(query)",
		"learn.RedactPII(",
		"cliutil.WithFileLock(",
		"textLogHasQueryHash(",
		"textLogHasLegacyQuery(",
	} {
		require.Contains(t, teach, want, "emitted cli/teach.go must contain %q", want)
	}
	require.NotContains(t, teach, "query=%q",
		"teach.log lines must not format the raw query")
	require.NotContains(t, teach, `strings.Contains(line, rawQuery)`,
		"scrub must not match log lines by raw-query substring")
	require.NotContains(t, teach, `strings.Contains(line, queryHash)`,
		"scrub must not match log lines by hash substring")

	playbook := readEmitted(t, outputDir, "internal", "cli", "teach_playbook.go")
	require.Contains(t, playbook, "learn.QueryHash(query)")
	require.NotContains(t, playbook, "query=%q",
		"playbook teach.log lines must not format the raw query")

	teachLog := readEmitted(t, outputDir, "internal", "learn", "teach_log.go")
	require.Contains(t, teachLog, "QueryHash: QueryHash(query)")
	require.NotContains(t, teachLog, "Query:     query",
		"structured teach.log must not persist the raw query field")

	if testing.Short() {
		t.Skip("compile-and-test of emitted query-safety CLI skipped in -short mode")
	}
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-race", "./internal/cli",
		"-run", "TestTeachCommand_AuditOmitsRawQuery|TestTeachCommand_TeachLogOmitsRawQuery|TestLearningsForget_ScrubsAuditLogs|TestLearningsForget_DoesNotScrubUnrelatedHistory|TestLogLineMatchesQuery_ExactIdentityOnly|TestLearningsAudit_RotatesWhenOverCap|TestLearningsAudit_ConcurrentRotateKeepsRecords|TestLearningsAudit_ConcurrentAppendKeepsRecords|TestTeachCommand_PIIEmailWarnsAndSucceeds",
		"-count=1")
	runGoCommand(t, outputDir, "test", "./internal/learn",
		"-run", "TestQueryHash_|TestRedactPII_|TestAppendTeachLogWarning_JSONShape",
		"-count=1")
}

// TestGenerateLearnQuerySafety_ShellSafeQueryGuidance pins that the
// agent-facing surfaces instruct argv/MCP or file-then-$QUERY passing
// and no longer interpolate user-controlled text into a shell command.
func TestGenerateLearnQuerySafety_ShellSafeQueryGuidance(t *testing.T) {
	t.Parallel()

	outputDir := generateLearnQuerySafetyCLI(t, "qshell")

	unsafe := []string{
		`recall "<question>"`,
		`recall "<user's question>"`,
		`--query "<question>"`,
		`--query "<user's question>"`,
		`--query "<exact recall query`,
		`forget "<question>"`,
		`--add-note "<your concrete correction>"`,
	}

	proto := readEmitted(t, outputDir, "internal", "learn", "protocol.go")
	require.Contains(t, proto, "QUERY=$(cat /path/to/question.txt)")
	require.Contains(t, proto, `recall "$QUERY" --agent`)
	require.Contains(t, proto, "never interpolate user-controlled text")
	for _, s := range unsafe {
		require.NotContains(t, proto, s, "protocol must not instruct %q", s)
	}

	teach := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	require.Contains(t, teach, `QUERY="$(cat /path/to/question.txt)"`)
	require.Contains(t, teach, `--query "$QUERY"`)

	agents := readEmitted(t, outputDir, "AGENTS.md")
	require.Contains(t, agents, "QUERY=$(cat /path/to/question.txt)")
	require.Contains(t, agents, `--query "$QUERY"`)
	for _, s := range unsafe {
		require.NotContains(t, agents, s, "AGENTS.md must not instruct %q", s)
	}

	skill := readEmitted(t, outputDir, "SKILL.md")
	require.Contains(t, skill, "QUERY=$(cat /path/to/question.txt)")
	require.Contains(t, skill, "NOTE=$(cat /path/to/note.txt)")
	require.Contains(t, skill, "Command substitution on a file only ever yields data")
	for _, s := range []string{
		`recall "<user's question>"`,
		`--query "<user's question>"`,
		`--query "<exact recall query string>"`,
		`--add-note "<your concrete correction>"`,
	} {
		require.NotContains(t, skill, s, "SKILL.md must not instruct %q", s)
	}
	require.True(t, strings.Contains(skill, `recall "$QUERY" --agent`),
		"SKILL.md must show the file-mediated recall invocation")

	mcp := readEmitted(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcp, "learn.RecallFirstProtocol")

	if testing.Short() {
		t.Skip("compile check of emitted query-safety CLI skipped in -short mode")
	}
	requireGeneratedCompiles(t, outputDir)
}
