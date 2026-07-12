package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

// TestGeneratePostOnlyLearnEnabledPromotesStore pins the R4 soft-validation
// posture: a learn-enabled spec whose vision profile skipped Store (post-only,
// zero syncable resources) generates successfully with the store and learn
// packages, while sync/search stay stripped because there is nothing to sync.
func TestGeneratePostOnlyLearnEnabledPromotesStore(t *testing.T) {
	t.Parallel()

	apiSpec := postOnlyOutputSpec("post-only-learn")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "learn", "recall.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "search.go"))

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.NotContains(t, rootSrc, "newSyncCmd(flags)")
	require.NotContains(t, rootSrc, "newSearchCmd(flags)")
	require.NotContains(t, doctorSrc, "collectCacheReport")

	requireGeneratedCompiles(t, outputDir)
}

// TestGeneratePostOnlyLearnDisabledSkipsStore pins that the learn opt-out
// keeps a post-only spec exactly as it is today: no store, no learn package.
func TestGeneratePostOnlyLearnDisabledSkipsStore(t *testing.T) {
	t.Parallel()

	apiSpec := postOnlyOutputSpec("post-only-learn-off")
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.NoFileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "learn"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
}

// TestGenerateLearnEnabledForcedVisionWithoutStorePromotes exercises the old
// hard-error path: a caller force-sets a VisionSet with Store=false while the
// spec enables learn. Constrain now promotes Store (and Sync, because this
// spec has syncable resources) instead of erroring.
func TestGenerateLearnEnabledForcedVisionWithoutStorePromotes(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("forced-no-store-learn")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	require.True(t, gen.VisionSet.Store, "constrain must promote Store for learn-enabled specs")
	require.True(t, gen.VisionSet.Sync, "Store-forces-Sync must be re-derived for syncable specs")
	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))

	requireGeneratedCompiles(t, outputDir)
}

// TestGenerateSyncableLearnEnabledProfileSkippedStorePromotesStoreAndSync
// covers the profile-derived path: SelectVisionTemplates runs (VisionSet left
// zero), the profile skips Store, and learn promotion inside constrain adds
// Store plus the re-derived Sync.
func TestGenerateSyncableLearnEnabledProfileSkippedStorePromotesStoreAndSync(t *testing.T) {
	t.Parallel()

	apiSpec := smallReadWriteSyncableOutputSpec("syncable-learn-promote")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	require.True(t, gen.VisionSet.Store)
	require.True(t, gen.VisionSet.Sync)
	require.FileExists(t, filepath.Join(outputDir, "internal", "store", "store.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "learn", "recall.go"))
}
