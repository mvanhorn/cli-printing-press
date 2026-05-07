package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedAgentsGuideRendersPortableAgentContract(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("finance")
	outputDir := filepath.Join(t.TempDir(), "finance-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	agents, err := os.ReadFile(filepath.Join(outputDir, "AGENTS.md"))
	require.NoError(t, err)
	content := string(agents)

	assert.Contains(t, content, "# Finance Printed CLI Agent Guide")
	assert.Contains(t, content, "generated `finance-pp-cli` printed CLI")
	assert.Contains(t, content, "finance-pp-cli doctor --json")
	assert.Contains(t, content, "finance-pp-cli agent-context --pretty")
	assert.Contains(t, content, `finance-pp-cli which "<capability>" --json`)
	assert.Contains(t, content, "finance-pp-cli <command> --help")
	assert.Contains(t, content, "finance-pp-cli <command> --agent")
	assert.Contains(t, content, "finance-pp-cli <command> --dry-run --agent")
	assert.Contains(t, content, "README.md")
	assert.Contains(t, content, "SKILL.md")

	assert.NotContains(t, content, "## Command Reference")
	assert.NotContains(t, content, "npx -y @mvanhorn/printing-press install")
	assert.NotContains(t, content, "export ")
	assert.NotContains(t, content, "<cli>")
	assert.NotContains(t, content, "Claude Code")
	assertASCII(t, content)
}

func assertASCII(t *testing.T, content string) {
	t.Helper()
	for _, r := range content {
		require.LessOrEqualf(t, r, rune(127), "generated AGENTS.md should stay ASCII-only; found %q", r)
	}
}
