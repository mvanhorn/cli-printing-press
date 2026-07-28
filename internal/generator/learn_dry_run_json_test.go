package generator

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

// learnDryRunCommands are the learn-loop commands whose --dry-run branch
// short-circuits before any store work, paired with the action name the
// emitted envelope must report.
var learnDryRunCommands = []struct {
	args   []string
	action string
}{
	{args: []string{"teach"}, action: "teach"},
	{args: []string{"teach-lookup"}, action: "teach-lookup"},
	{args: []string{"teach-pattern"}, action: "teach-pattern"},
	{args: []string{"teach-playbook"}, action: "teach-playbook"},
	{args: []string{"playbook", "amend"}, action: "playbook amend"},
	{args: []string{"playbook", "list"}, action: "playbook list"},
	{args: []string{"recall", "example query"}, action: "recall"},
	{args: []string{"learnings", "list"}, action: "learnings list"},
	{args: []string{"learnings", "stats"}, action: "learnings stats"},
	{args: []string{"learnings", "candidates"}, action: "learnings candidates"},
	{args: []string{"learnings", "confirm", "1"}, action: "learnings confirm"},
	{args: []string{"learnings", "reject", "1"}, action: "learnings reject"},
	{args: []string{"learnings", "purge"}, action: "learnings purge"},
	{args: []string{"learnings", "forget", "example query"}, action: "learnings forget"},
}

// TestLearnDryRunEmitsJSONEnvelope pins that a --dry-run short-circuit stays
// machine-legible. Returning nil silently left --json callers with empty
// stdout, which the live-dogfood json_fidelity check reads as a broken
// command rather than a deliberate no-op.
func TestLearnDryRunEmitsJSONEnvelope(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("learn-dry-run")
	apiSpec.Learn.Enabled = true
	_, binaryPath := buildGeneratedBinary(t, apiSpec)

	for _, tc := range learnDryRunCommands {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append(append([]string{}, tc.args...), "--dry-run", "--json")
			stdout := requireLearnDryRunOutput(t, binaryPath, args)

			var envelope struct {
				DryRun bool   `json:"dry_run"`
				Action string `json:"action"`
				Would  string `json:"would"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout: %q", stdout)
			require.True(t, envelope.DryRun)
			require.Equal(t, tc.action, envelope.Action)
			require.NotEmpty(t, envelope.Would)
		})
	}

	// The human path must report the skipped action too, as prose rather than
	// the machine envelope.
	t.Run("human mode", func(t *testing.T) {
		stdout := requireLearnDryRunOutput(t, binaryPath, []string{"teach", "--dry-run"})
		require.Contains(t, stdout, "dry-run")
		require.Contains(t, stdout, "teach")
		require.False(t, json.Valid([]byte(stdout)), "human mode must not emit the JSON envelope")
	})
}

// TestLearnTemplatesHaveNoSilentDryRunReturns guards the emitted call sites:
// every learn-loop --dry-run guard must hand off to writeDryRun, so a new
// command cannot reintroduce the silent short-circuit.
func TestLearnTemplatesHaveNoSilentDryRunReturns(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("learn-dry-run-sites")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpers, "func writeDryRun(w io.Writer, flags *rootFlags, action string) error")
	require.Contains(t, helpers, `json:"dry_run"`)

	for _, name := range []string{"teach.go", "teach_playbook.go", "learnings_candidates.go", "learnings_stats.go"} {
		src := readGeneratedFile(t, outputDir, "internal", "cli", name)
		require.Contains(t, src, "return writeDryRun(cmd.OutOrStdout(), flags,", "%s must report its dry-run short-circuit", name)
		require.NotContains(t, src, "if dryRunOK(flags) {\n\t\t\t\treturn nil\n", "%s still has a silent dry-run return", name)
	}
}

func requireLearnDryRunOutput(t *testing.T, binaryPath string, args []string) string {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = sandboxHomeEnv(t)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return strings.TrimSpace(stdout.String())
}
