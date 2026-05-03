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

// TestRenderMergedGoModPreservesPublishedOnlyRequires pins the contract that
// requires present in published but absent from fresh survive the merge.
// Typical case: agent ran `go get modernc.org/sqlite` after generation to
// build a hand-coded local store; the dep isn't in the spec and won't be in
// the fresh tree's go.mod. Without preservation, the merged go.mod drops the
// dep and `go build` fails on the next sweep.
func TestRenderMergedGoModPreservesPublishedOnlyRequires(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	pubDir := filepath.Join(tmp, "pub")
	freshDir := filepath.Join(tmp, "fresh")
	require.NoError(t, os.MkdirAll(pubDir, 0o755))
	require.NoError(t, os.MkdirAll(freshDir, 0o755))

	// Published has a hand-added sqlite dep on top of fresh's baseline.
	pubGoMod := []byte(`module github.com/example/monorepo/library/foo

go 1.23.0

require (
	github.com/spf13/cobra v1.9.1
	modernc.org/sqlite v1.50.0
)
`)
	freshGoMod := []byte(`module foo-pp-cli

go 1.23.0

require github.com/spf13/cobra v1.9.1
`)
	require.NoError(t, writeFileAtomic(filepath.Join(pubDir, "go.mod"), pubGoMod))
	require.NoError(t, writeFileAtomic(filepath.Join(freshDir, "go.mod"), freshGoMod))

	// Plan: published-only require lands in PreservedRequires.
	plan, err := planGoModMerge(pubDir, freshDir)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.PreservedRequires, 1)
	assert.Contains(t, plan.PreservedRequires[0], "modernc.org/sqlite",
		"sqlite dep must be reported as preserved so operators can see hand-additions survive")

	// Render: merged go.mod still requires sqlite.
	bytes, err := renderMergedGoMod(pubDir, freshDir)
	require.NoError(t, err)
	parsed, err := modfile.Parse("merged-go.mod", bytes, nil)
	require.NoError(t, err)

	gotPaths := map[string]string{}
	for _, req := range parsed.Require {
		gotPaths[req.Mod.Path] = req.Mod.Version
	}
	assert.Equal(t, "v1.50.0", gotPaths["modernc.org/sqlite"],
		"hand-added sqlite must survive the merge with its published version")
	assert.Equal(t, "v1.9.1", gotPaths["github.com/spf13/cobra"],
		"shared deps stay at fresh's version")
}
