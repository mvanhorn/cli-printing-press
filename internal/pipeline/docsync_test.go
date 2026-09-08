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

func TestReplaceReadmeIntroNarrativeStopsAtHeadingBeforeInstall(t *testing.T) {
	content := strings.Join([]string{
		"# Example CLI",
		"",
		"Old headline.",
		"",
		"## Authentication",
		"",
		"Keep this auth section.",
		"",
		"## Install",
		"",
		"Install instructions.",
		"",
	}, "\n")

	updated := replaceReadmeIntroNarrative(content, &ReadmeNarrative{
		Headline:  "New headline",
		ValueProp: "New value proposition.",
	})

	assert.Contains(t, updated, "**New headline**")
	assert.Contains(t, updated, "New value proposition.")
	assert.Contains(t, updated, "## Authentication\n\nKeep this auth section.")
	assert.Contains(t, updated, "## Install\n\nInstall instructions.")
	assert.NotContains(t, updated, "Old headline.")
	requireBefore(t, updated, "New value proposition.", "## Authentication")
	requireBefore(t, updated, "## Authentication", "## Install")
}

func TestRenderSkillAuthSetupSectionDoesNotDuplicateDoctorInstruction(t *testing.T) {
	section := renderSkillAuthSetupSection(
		"test",
		"Use `test-pp-cli oauth-token` before protected calls.\n\nRun `test-pp-cli doctor` to verify setup.",
	)

	assert.Equal(t, 1, strings.Count(section, "Run `test-pp-cli doctor` to verify setup."))
}

func TestSyncCLINarrativeDocsRefreshesReadmeAndSkillRecipes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(strings.Join([]string{
		"# Example",
		"",
		"## Quick Start",
		"",
		"Start here.",
		"",
		"## Recipes",
		"",
		"### Old recipe",
		"",
		"```bash",
		"example-pp-cli provisionar --dry-run",
		"```",
		"",
		"## Usage",
		"",
		"Use commands.",
		"",
	}, "\n")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(strings.Join([]string{
		"# Example",
		"",
		"## Command Reference",
		"",
		"Commands.",
		"",
		"## Recipes",
		"",
		"### Old recipe",
		"",
		"```bash",
		"example-pp-cli provisionar --dry-run",
		"```",
		"",
		"## Auth Setup",
		"",
		"No authentication required.",
		"",
	}, "\n")), 0o600))

	synced, err := SyncCLINarrativeDocs(dir, "example", &ReadmeNarrative{
		Recipes: []Recipe{{
			Title:       "Provision safely",
			Command:     "example-pp-cli provisionar --simular",
			Explanation: "Preview provisioning with the current flag name.",
		}},
	})
	require.NoError(t, err)

	assert.Contains(t, synced, syncedArtifact{Path: "README.md", Detail: "Recipes"})
	assert.Contains(t, synced, syncedArtifact{Path: "SKILL.md", Detail: "Recipes"})
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	skill, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	require.NoError(t, err)
	for _, content := range []string{string(readme), string(skill)} {
		assert.Contains(t, content, "example-pp-cli provisionar --simular")
		assert.NotContains(t, content, "--dry-run")
	}
	requireBefore(t, string(readme), "## Recipes", "## Usage")
	requireBefore(t, string(skill), "## Recipes", "## Auth Setup")
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

func TestSyncWhichIndexPreservesPromotedEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "internal", "cli", "which.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`package cli

var whichIndex = []whichEntry{
	{Command: "old-novel", Description: "stale novel", Group: "", WhyItMatters: ""},
	{Command: "awards", Description: "Search award availability", Group: "awards", WhyItMatters: "Search award availability"}, // pp:which-promoted
}
`), 0o644))

	changed, err := syncWhichIndex(path, []NovelFeature{{
		Command:      "digest",
		Description:  "Fresh novel",
		Group:        "Analysis",
		WhyItMatters: "Hero path",
	}})
	require.NoError(t, err)
	require.True(t, changed)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(data)
	assert.Contains(t, got, `Command: "digest"`)
	assert.Contains(t, got, `Command: "awards"`)
	assert.Contains(t, got, "pp:which-promoted")
	assert.NotContains(t, got, "old-novel")
	assert.NotContains(t, got, "stale novel")
	requireBefore(t, got, `Command: "digest"`, `Command: "awards"`)
}

func TestSyncWhichIndexIgnoresLabelsInsideDescriptionProse(t *testing.T) {
	line := `	{Command: "awards", Description: "See Group: awards in WhyItMatters: docs", Group: "real-group", WhyItMatters: "real-why"}, // pp:which-promoted`
	entry, ok := parseWhichEntryLine(line)
	require.True(t, ok)
	assert.Equal(t, "awards", entry.Command)
	assert.Equal(t, "See Group: awards in WhyItMatters: docs", entry.Description)
	assert.Equal(t, "real-group", entry.Group)
	assert.Equal(t, "real-why", entry.WhyItMatters)

	dir := t.TempDir()
	path := filepath.Join(dir, "internal", "cli", "which.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`package cli

var whichIndex = []whichEntry{
`+line+`
}
`), 0o644))

	changed, err := syncWhichIndex(path, []NovelFeature{{
		Command:      "digest",
		Description:  "Fresh novel",
		Group:        "Analysis",
		WhyItMatters: "Hero path",
	}})
	require.NoError(t, err)
	require.True(t, changed)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(data)
	assert.Contains(t, got, `Group: "real-group"`)
	assert.Contains(t, got, `WhyItMatters: "real-why"`)
	assert.Contains(t, got, `Description: "See Group: awards in WhyItMatters: docs"`)
	assert.NotContains(t, got, `Group: "awards"`)
	assert.NotContains(t, got, `WhyItMatters: "docs"`)
}
