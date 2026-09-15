package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateStoreTrigramFTS_EmitsTokenizerAndVersionPin(t *testing.T) {
	t.Parallel()

	enabled, enabledDir := generateLearnStore(t, "fts-trigram-enabled", true)
	require.Contains(t, enabled, "tokenize='trigram'")
	require.NotContains(t, enabled, "tokenize='porter unicode61'")
	require.Contains(t, enabled, "const StoreSchemaVersion = 11")
	require.Contains(t, enabled, "const resourcesFTSTokenizerSchemaVersion = 11")
	require.Contains(t, enabled, "ftsNeedsLikeFallback")
	require.Contains(t, enabled, "current < resourcesFTSTokenizerSchemaVersion")
	requireGeneratedCompiles(t, enabledDir)

	disabled, disabledDir := generateLearnStore(t, "fts-trigram-disabled", false)
	require.Contains(t, disabled, "tokenize='trigram'")
	require.NotContains(t, disabled, "tokenize='porter unicode61'")
	require.Contains(t, disabled, "const StoreSchemaVersion = 6")
	require.Contains(t, disabled, "const resourcesFTSTokenizerSchemaVersion = 6")
	requireGeneratedCompiles(t, disabledDir)
}

func TestGenerateStoreTrigramFTS_EmittedCJKSearchPasses(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		name := "fts-trigram-cjk-disabled"
		if enabled {
			name = "fts-trigram-cjk-enabled"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, outputDir := generateLearnStore(t, name, enabled)
			runGoCommandRequired(t, outputDir, "test", "-c", "-o", filepath.Join(t.TempDir(), "store.test"), "./internal/store/...")
			runGoCommandRequired(t, outputDir, "test", "./internal/store",
				"-run", "^(TestSearch_CJKSubstringAndASCII|TestSearch_LikeEscapesWildcards|TestSearch_LikeFallbackPreservesTokenBoundaries|TestMigrate_TokenizerRebuildsCJKSearch)$",
				"-count=1")
		})
	}
}
