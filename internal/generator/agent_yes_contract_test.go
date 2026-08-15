package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAgentModeDoesNotGrantYes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agent-yes")
	outputDir := filepath.Join(t.TempDir(), "agent-yes-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	testPath := filepath.Join(outputDir, "internal", "cli", "agent_yes_contract_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestAgentModeDoesNotSetYes(t *testing.T) {
	got := runFlagProbe(t, "--agent")

	if !got.agent {
		t.Fatalf("--agent should record agent mode")
	}
	if !got.asJSON || !got.compact || !got.noInput {
		t.Fatalf("--agent should set output and interaction defaults, got asJSON=%v compact=%v noInput=%v", got.asJSON, got.compact, got.noInput)
	}
	if got.yes {
		t.Fatalf("--agent must not grant explicit confirmation")
	}
}

func TestExplicitYesStillSetsYes(t *testing.T) {
	got := runFlagProbe(t, "--yes")

	if got.agent {
		t.Fatalf("--yes alone must not imply --agent")
	}
	if !got.yes {
		t.Fatalf("explicit --yes should still grant confirmation")
	}
}

func runFlagProbe(t *testing.T, args ...string) rootFlags {
	t.Helper()

	oldNoColor, oldHumanFriendly := noColor, humanFriendly
	noColor, humanFriendly = false, false
	t.Cleanup(func() {
		noColor, humanFriendly = oldNoColor, oldHumanFriendly
	})

	var flags rootFlags
	var captured rootFlags
	root := newRootCmd(&flags)
	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			captured = flags
			return nil
		},
	})
	root.SetArgs(append([]string{"probe"}, args...))

	if err := root.Execute(); err != nil {
		t.Fatalf("root Execute returned error: %v", err)
	}
	return captured
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestAgentModeDoesNotSetYes|TestExplicitYesStillSetsYes", "-count=1")
}

func TestGeneratedDocsKeepAgentAndYesContractsSeparate(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agent-docs")
	items := apiSpec.Resources["items"]
	items.Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create item",
	}
	apiSpec.Resources["items"] = items
	outputDir := filepath.Join(t.TempDir(), "agent-docs-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootGo := readAgentContractGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootGo, "Set agent-friendly output defaults (--json --compact --no-input --no-color)")
	assert.NotContains(t, rootGo, "Set all agent-friendly defaults (--json --compact --no-input --no-color --yes)")
	assert.NotContains(t, rootGo, `flags.yes = true`)

	readme := readAgentContractGeneratedFile(t, outputDir, "README.md")
	assert.Contains(t, readme, "`--agent` does not imply `--yes`; pass `--yes` separately")
	assert.NotContains(t, readme, "**Confirmable** - `--yes` for explicit confirmation of destructive actions")

	skill := readAgentContractGeneratedFile(t, outputDir, "SKILL.md")
	assert.Contains(t, skill, "Expands to: `--json --compact --no-input --no-color`.")
	assert.Contains(t, skill, "`--agent` does not imply `--yes`; pass `--yes` separately")
	assert.NotContains(t, skill, "Expands to: `--json --compact --no-input --no-color --yes`.")

	agents := readAgentContractGeneratedFile(t, outputDir, "AGENTS.md")
	assert.Contains(t, agents, "`--agent` does not imply `--yes`.")
	assert.NotContains(t, agents, "confirmation-safe scripting")
}

func readAgentContractGeneratedFile(t *testing.T, outputDir string, parts ...string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(append([]string{outputDir}, parts...)...))
	require.NoError(t, err)
	return string(body)
}
