package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateStoreFTSRowIDContract verifies the generated store package keeps
// ftsRowID deterministic, non-negative, and domain-separated across the
// scope/id boundary.
func TestGenerateStoreFTSRowIDContract(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("fts-rowid")
	outputDir := filepath.Join(t.TempDir(), "fts-rowid-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "store", "fts_rowid_contract_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package store

import "testing"

func TestFTSRowIDContract(t *testing.T) {
	a := ftsRowID("resource", "123")
	b := ftsRowID("resource", "123")
	if a != b {
		t.Fatalf("ftsRowID must be deterministic: %d != %d", a, b)
	}
	if a < 0 {
		t.Fatalf("ftsRowID must be non-negative, got %d", a)
	}
	if ftsRowID("ab", "c") == ftsRowID("a", "bc") {
		t.Fatalf("ftsRowID must separate scope/id boundary")
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestFTSRowIDContract", "-count=1")
}

func TestGenerateStoreFTSRowIDMigration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		learnEnabled bool
	}{
		{name: "learn-disabled"},
		{name: "learn-enabled", learnEnabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("fts-rowid-migration-" + tc.name)
			apiSpec.Learn.Enabled = tc.learnEnabled
			outputDir := filepath.Join(t.TempDir(), "fts-rowid-migration-"+tc.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			gen.VisionSet = VisionTemplateSet{Store: true}
			require.NoError(t, gen.Generate())

			runGoCommand(t, outputDir, "test", "./internal/store", "-run", "^Test(SchemaVersion_StampedOnFreshDB|Migrate_ResourcesCompositeKeyUpgrade|Migrate_V2ResourcesFTSRowIDUpgrade|Migrate_V3ResourcesFTSNoRebuild)$", "-count=1")
		})
	}
}
