package cli

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVisionCmdDefaultsToScopedResearchDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", home)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", filepath.Join(home, "repo"))

	state := pipeline.NewStateWithRun("stripe", filepath.Join(home, "work", "stripe-pp-cli"), "run-123", "test-scope")
	require.NoError(t, state.Save())

	cmd := newVisionCmd()
	cmd.SetArgs([]string{"--api", "stripe", "--json"})

	require.NoError(t, cmd.Execute())

	expectedFile := filepath.Join(state.ResearchDir(), "visionary-research.md")
	assert.FileExists(t, expectedFile)
}
