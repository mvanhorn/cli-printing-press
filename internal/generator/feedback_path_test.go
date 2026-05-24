package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeedbackPath_UsesLocalShareDir(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("feedback-path")
	outputDir := filepath.Join(t.TempDir(), "feedback-path-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	feedbackPath := filepath.Join(outputDir, "internal", "cli", "feedback.go")
	feedbackContent, err := os.ReadFile(feedbackPath)
	require.NoError(t, err)
	feedbackSrc := string(feedbackContent)

	require.Contains(t, feedbackSrc,
		`dir := filepath.Join(home, ".local", "share", "feedback-path-pp-cli")`,
		"feedbackFilePath should use ~/.local/share/<name> like defaultDBPath")
	require.NotContains(t, feedbackSrc,
		`dir := filepath.Join(home, ".feedback-path-pp-cli")`,
		"feedbackFilePath must not use the legacy ~/.<name> dotdir")
	require.Contains(t, feedbackSrc,
		"Feedback is captured locally first at ~/.local/share/feedback-path-pp-cli/feedback.jsonl.",
		"feedback command help text should reference the new local ledger path")

	skillPath := filepath.Join(outputDir, "SKILL.md")
	skillContent, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.Contains(t, string(skillContent),
		"Entries are stored locally at `~/.local/share/feedback-path-pp-cli/feedback.jsonl`.",
		"generated SKILL.md should reference the new local ledger path")
}
