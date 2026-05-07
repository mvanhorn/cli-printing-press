package regenmerge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

// TestApplyPostmanExploreFixture exercises the full Apply flow against the
// postman-explore fixture. After Apply:
//   - novel files (canonical.go, novels.go, novel_helpers.go) are preserved
//   - templated files (helpers.go, root.go, category.go, templated_stubs.go)
//     match fresh's content
//   - import.go (NEW-TEMPLATE-EMISSION) is added
//   - root.go has the lost AddCommand calls re-injected
//   - category.go has the landscape sub-cmd registration re-injected
//   - go.mod has the published module path
func TestApplyPostmanExploreFixture(t *testing.T) {
	t.Parallel()

	stagedPubDir := stageFixture(t, "testdata/postman-explore/published")
	freshDir := absFixturePath(t, "testdata/postman-explore/fresh")

	report, err := Classify(stagedPubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	require.NoError(t, Apply(report, Options{Force: true}))
	require.True(t, report.Applied)

	// Novel files preserved.
	assertFileExists(t, stagedPubDir, "internal/cli/canonical.go", "novel preserved")
	assertFileExists(t, stagedPubDir, "internal/cli/novels.go", "novel preserved")
	assertFileExists(t, stagedPubDir, "internal/cli/novel_helpers.go", "novel preserved")

	// NEW-TEMPLATE-EMISSION: import.go added.
	assertFileExists(t, stagedPubDir, "internal/cli/import.go", "fresh-emitted file added")

	// Templated overwritten with fresh content (post module-path rewrite).
	helpersPath := filepath.Join(stagedPubDir, "internal/cli/helpers.go")
	freshHelpers, _ := os.ReadFile(filepath.Join(freshDir, "internal/cli/helpers.go"))
	merged, _ := os.ReadFile(helpersPath)
	// helpers.go has no module-path imports so should match fresh exactly.
	assert.Equal(t, string(freshHelpers), string(merged), "helpers.go matches fresh")

	// root.go has the lost registrations restored.
	rootSrc, err := os.ReadFile(filepath.Join(stagedPubDir, "internal/cli/root.go"))
	require.NoError(t, err)
	rootContent := string(rootSrc)
	for _, expectedCtor := range []string{
		"newCanonicalCmd", "newTopCmd", "newPublishersCmd", "newDriftCmd",
		"newSimilarCmd", "newVelocityCmd", "newBrowseCmd",
	} {
		assert.True(t, strings.Contains(rootContent, expectedCtor),
			"root.go should have %s registered after restoration", expectedCtor)
	}

	// category.go has the landscape sub-cmd restored.
	catSrc, err := os.ReadFile(filepath.Join(stagedPubDir, "internal/cli/category.go"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(catSrc), "newCategoryLandscapeCmd"),
		"category.go should have newCategoryLandscapeCmd registered after restoration")

	// go.mod has the published module path.
	gomod, err := os.ReadFile(filepath.Join(stagedPubDir, "go.mod"))
	require.NoError(t, err)
	assert.True(t, bytes.Contains(gomod, []byte("github.com/mvanhorn/printing-press-library")),
		"go.mod preserves published module path")
	assert.False(t, bytes.Contains(gomod, []byte("module postman-explore-pp-cli")),
		"go.mod does not retain fresh's standalone module path")
}

// TestApplyEbayAuthFixturePreservesAuth confirms the canary preservation
// case: auth.go has hand-added decls; Apply must NOT overwrite it.
func TestApplyEbayAuthFixturePreservesAuth(t *testing.T) {
	t.Parallel()

	stagedPubDir := stageFixture(t, "testdata/ebay-auth/published")
	freshDir := absFixturePath(t, "testdata/ebay-auth/fresh")

	// Snapshot auth.go before Apply.
	authPath := filepath.Join(stagedPubDir, "internal/cli/auth.go")
	authBefore, err := os.ReadFile(authPath)
	require.NoError(t, err)

	report, err := Classify(stagedPubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	require.NoError(t, Apply(report, Options{Force: true}))

	// auth.go must be byte-equal to its pre-Apply state (preserved).
	authAfter, err := os.ReadFile(authPath)
	require.NoError(t, err)
	assert.Equal(t, string(authBefore), string(authAfter),
		"ebay auth.go must be preserved byte-for-byte (5+ added OAuth functions intact)")
}

// TestApplyIdempotency runs Apply twice on the same staged fixture; the
// second run should be safe and produce a tree byte-equal to the first.
func TestApplyIdempotency(t *testing.T) {
	t.Parallel()

	stagedPubDir := stageFixture(t, "testdata/postman-explore/published")
	freshDir := absFixturePath(t, "testdata/postman-explore/fresh")

	// First Apply.
	report1, err := Classify(stagedPubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	require.NoError(t, Apply(report1, Options{Force: true}))

	// Snapshot tree state.
	snap1 := snapshotTree(t, stagedPubDir)

	// Second Apply.
	report2, err := Classify(stagedPubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	require.NoError(t, Apply(report2, Options{Force: true}))

	// Tree state should be byte-equal.
	snap2 := snapshotTree(t, stagedPubDir)
	assert.Equal(t, snap1, snap2, "second Apply should produce no diff")
}

// --- helpers ---

// stageFixture copies a fixture published-tree into a fresh tempdir under
// the test's CWD prefix so validatePathAgainstCWD passes without --force,
// returning the staged directory path. The original testdata/ tree is
// never touched.
func stageFixture(t *testing.T, relSrc string) string {
	t.Helper()
	src, err := filepath.Abs(relSrc)
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	staged := filepath.Join(cwd, "_staged", t.Name())
	require.NoError(t, os.MkdirAll(staged, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(staged) })

	require.NoError(t, pipeline.CopyDir(src, staged))
	return staged
}

func absFixturePath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	require.NoError(t, err)
	return abs
}

func assertFileExists(t *testing.T, dir, rel, msg string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	assert.NoError(t, err, "%s: %s should exist", msg, rel)
}

// snapshotTree returns a map of relative-path → content-bytes for diffing.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, _ := os.ReadFile(path)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	require.NoError(t, err)
	return out
}
