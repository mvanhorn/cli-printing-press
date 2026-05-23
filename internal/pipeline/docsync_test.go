package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncReadmeAuthNarrativeRemovesStaleAuthenticationWhenOptionalExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join([]string{
		"# Example",
		"",
		"## Optional: API Key",
		"",
		"Old optional setup.",
		"",
		"## Authentication",
		"",
		"Old required setup.",
		"",
		"## Quick Start",
		"",
		"Run the CLI.",
		"",
	}, "\n")), 0o600))

	changed, err := syncReadmeAuthNarrative(path, "Use `example-pp-cli oauth-token` for protected calls.")
	require.NoError(t, err)
	require.True(t, changed)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	readme := string(data)
	assert.Contains(t, readme, "## Optional: API Key\n\n**All core commands work without setup.**")
	assert.Contains(t, readme, "Use `example-pp-cli oauth-token` for protected calls.")
	assert.NotContains(t, readme, "## Authentication")
	assert.NotContains(t, readme, "Old required setup.")
	assert.Contains(t, readme, "## Quick Start")
}

func TestRenderSkillAuthSetupSectionDoesNotDuplicateDoctorInstruction(t *testing.T) {
	section := renderSkillAuthSetupSection(
		"test",
		"Use `test-pp-cli oauth-token` before protected calls.\n\nRun `test-pp-cli doctor` to verify setup.",
	)

	assert.Equal(t, 1, strings.Count(section, "Run `test-pp-cli doctor` to verify setup."))
}

func TestMarkdownHeadingsRequiresMatchingFenceLength(t *testing.T) {
	content := strings.Join([]string{
		"````",
		"## Fenced",
		"```",
		"## Still fenced",
		"````",
		"## Real",
		"",
	}, "\n")

	assert.Equal(t, -1, findMarkdownHeading(content, "## Fenced"))
	assert.Equal(t, -1, findMarkdownHeading(content, "## Still fenced"))
	assert.GreaterOrEqual(t, findMarkdownHeading(content, "## Real"), 0)
}
