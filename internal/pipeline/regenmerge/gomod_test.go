package regenmerge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

// TestPlanGoModMergePostmanExplore verifies the plan reports the published
// module path preserved and fresh's added require visible.
func TestPlanGoModMergePostmanExplore(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := postmanFixture(t)

	plan, err := planGoModMerge(pubDir, freshDir)
	require.NoError(t, err)
	require.NotNil(t, plan)

	assert.Equal(t,
		"github.com/mvanhorn/printing-press-library/library/developer-tools/postman-explore",
		plan.PreservedModulePath)
}

// TestRenderMergedGoModPreservesPublishedModule confirms the rendered bytes
// have the published module line, the fresh require versions, and parse
// cleanly.
func TestRenderMergedGoModPreservesPublishedModule(t *testing.T) {
	t.Parallel()

	pubDir, freshDir := postmanFixture(t)

	bytes, err := renderMergedGoMod(pubDir, freshDir)
	require.NoError(t, err)

	parsed, err := modfile.Parse("merged-go.mod", bytes, nil)
	require.NoError(t, err)

	// Module path: published.
	assert.Equal(t,
		"github.com/mvanhorn/printing-press-library/library/developer-tools/postman-explore",
		parsed.Module.Mod.Path)

	// Require version: fresh's (1.8.1, not 1.8.0).
	var cobraVersion string
	for _, r := range parsed.Require {
		if r.Mod.Path == "github.com/spf13/cobra" {
			cobraVersion = r.Mod.Version
			break
		}
	}
	assert.Equal(t, "v1.8.1", cobraVersion, "should pick up fresh's pinned cobra version")
}

// TestRenderMergedGoModLocalReplaceWins verifies the smart-replace rule:
// a local-path replace in published wins over a version-replace in fresh.
func TestRenderMergedGoModLocalReplaceWins(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	pubDir := filepath.Join(tmp, "pub")
	freshDir := filepath.Join(tmp, "fresh")
	require.NoError(t, os.MkdirAll(pubDir, 0o755))
	require.NoError(t, os.MkdirAll(freshDir, 0o755))

	pubGoMod := []byte(`module github.com/example/monorepo/library/foo

go 1.23.0

require github.com/x/y v1.0.0

replace github.com/x/y => ./local-fork
`)
	freshGoMod := []byte(`module foo-pp-cli

go 1.23.0

require github.com/x/y v1.2.3

replace github.com/x/y => github.com/upstream/fork v9.9.9
`)
	require.NoError(t, writeFileAtomic(filepath.Join(pubDir, "go.mod"), pubGoMod))
	require.NoError(t, writeFileAtomic(filepath.Join(freshDir, "go.mod"), freshGoMod))

	bytes, err := renderMergedGoMod(pubDir, freshDir)
	require.NoError(t, err)

	parsed, err := modfile.Parse("merged-go.mod", bytes, nil)
	require.NoError(t, err)

	require.Len(t, parsed.Replace, 1, "exactly one replace should survive — published's local-path version")
	r := parsed.Replace[0]
	assert.Equal(t, "github.com/x/y", r.Old.Path)
	assert.Equal(t, "./local-fork", r.New.Path, "published's local-path replace wins over fresh's version-replace")
}
