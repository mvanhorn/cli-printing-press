package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateStoreUpsertBatchWarningUsesNotCachedLocallyWording(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("store-warning-wording")
	outputDir := filepath.Join(t.TempDir(), "store-warning-wording-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeSrc := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	require.Contains(t, storeSrc, "not cached locally")
	require.NotContains(t, storeSrc, "items skipped")
}
