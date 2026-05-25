package cli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrintingPressSkillSideEffectNarrativeGuidance(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "Step 1 of `quickstart` should usually be verify-safe")
	require.Contains(t, content, "Use `<cli> doctor --dry-run` as step 1")
	require.Contains(t, content, "reports each as an `UNSUPPORTED` warning instead of executing it")
	require.Contains(t, content, "These warnings do not fail strict aggregation")
	require.Contains(t, content, "Non-side-effect unsupported examples still fail strict mode")
}

func TestPrintingPressSkillMCPEnrichmentGate(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../skills/printing-press/SKILL.md")
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, "Mandatory >50 endpoint-tools gate")
	require.Contains(t, content, "spec exposes <N> MCP endpoint tools (>50 threshold)")
	require.Contains(t, content, "actual numeric count printed by the generator")
	require.Contains(t, content, "AskUserQuestion")
	require.Contains(t, content, "Apply Cloudflare MCP pattern + regenerate (recommended)")
	require.Contains(t, content, "OpenAPI input: write or update a root `x-mcp:` block")
	require.Contains(t, content, "Internal YAML input: write or update the root `mcp:` block")
	require.Contains(t, content, "If the runtime cannot ask a blocking question, stop")
}
