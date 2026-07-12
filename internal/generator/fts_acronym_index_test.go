package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateStoreFTSIndexesAcronyms verifies the generated store package
// indexes 3-letter all-caps acronyms (API, SQL, AWS, ...) into resources_fts
// so they are searchable. A prior blanket exclusion of every 3-char all-caps
// token (intended for currency/country codes) made common technical acronyms
// unsearchable on every CLI using the generic FTS index.
func TestGenerateStoreFTSIndexesAcronyms(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("fts-acronyms")
	outputDir := filepath.Join(t.TempDir(), "fts-acronyms-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "store", "fts_acronym_index_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package store

import "testing"

func TestShouldIndexSearchStringAcronyms(t *testing.T) {
	for _, acr := range []string{"API", "SQL", "AWS", "XML", "GPU"} {
		if !shouldIndexSearchString("description", acr) {
			t.Errorf("shouldIndexSearchString(%q) = false, want true (3-letter acronyms must be searchable)", acr)
		}
	}
	// Sanity: genuinely non-indexable values stay excluded.
	if shouldIndexSearchString("id", "abc-123") {
		t.Errorf("identifier-key values must stay unindexed")
	}
	if shouldIndexSearchString("x", "a") {
		t.Errorf("single-character values must stay unindexed")
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestShouldIndexSearchStringAcronyms", "-count=1")
}
