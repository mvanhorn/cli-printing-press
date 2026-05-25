package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestGenerateStoreSchemaVersion_DisabledStaysV2(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-version-disabled")
	apiSpec.Learn.Enabled = false
	outputDir := filepath.Join(t.TempDir(), "learn-version-disabled-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeGo)
	require.Contains(t, src, "const StoreSchemaVersion = 2")
	require.NotContains(t, src, "const StoreSchemaVersion = 3")
	for _, table := range []string{"search_learnings", "search_patterns", "entity_lookups", "teach_log_metadata", "search_learnings_fts"} {
		require.NotContains(t, src, table, "learn-disabled spec must not emit %s migration", table)
	}
}

func TestGenerateStoreSchemaVersion_EnabledAdvancesToV3WithLearnTables(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-version-enabled")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-version-enabled-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeGo)
	require.Contains(t, src, "const StoreSchemaVersion = 3")
	require.NotContains(t, src, "const StoreSchemaVersion = 2")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS search_learnings",
		"CREATE TABLE IF NOT EXISTS search_patterns",
		"CREATE TABLE IF NOT EXISTS entity_lookups",
		"CREATE TABLE IF NOT EXISTS teach_log_metadata",
		"CREATE VIRTUAL TABLE IF NOT EXISTS search_learnings_fts",
	} {
		require.Contains(t, src, want, "learn-enabled spec must emit %q", want)
	}
}

// TestGenerateStoreLearnMigrationsGated verifies the learn-table migrations
// only emit when Spec.Learn.Enabled is true.
func TestGenerateStoreLearnMigrationsGated(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-disabled")
	apiSpec.Learn.Enabled = false
	outputDir := filepath.Join(t.TempDir(), "learn-disabled-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeGo)
	for _, table := range []string{"search_learnings", "search_patterns", "entity_lookups", "teach_log_metadata", "search_learnings_fts"} {
		require.NotContains(t, src, table, "learn-disabled spec must not emit %s migration", table)
	}
}

// TestGenerateStoreLearnMigrationsPresent verifies that with Learn.Enabled the
// additive table-creates land in the emitted migrations slice with the anchor
// comment the library-side sweep tool will search for.
func TestGenerateStoreLearnMigrationsPresent(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-enabled")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-enabled-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := string(storeGo)

	require.Contains(t, src, "// CLI Printing Press: learn migrations", "the sweep-tool anchor comment must be present in the emitted store")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS search_learnings",
		"CREATE TABLE IF NOT EXISTS search_patterns",
		"CREATE TABLE IF NOT EXISTS entity_lookups",
		"CREATE TABLE IF NOT EXISTS teach_log_metadata",
		"CREATE VIRTUAL TABLE IF NOT EXISTS search_learnings_fts",
	} {
		require.Contains(t, src, want, "learn-enabled spec must emit %q", want)
	}
	require.Contains(t, src, "tokenize='porter unicode61'", "FTS5 must use the porter/unicode61 tokenizer mirroring resources_fts")
}

// TestGenerateStoreLearnRenamedFromRecipes verifies the schema rename landed.
// Per the plan, the prediction-goat `learn_recipes` table is renamed to
// `search_patterns` everywhere in the emitted store; a `learn_recipes` leak
// would mean a partial rename slipped past the template.
func TestGenerateStoreLearnRenamedFromRecipes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-rename")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-rename-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	src := strings.ToLower(string(storeGo))
	require.NotContains(t, src, "learn_recipes", "the legacy learn_recipes table name must not appear in the generated store")
	require.NotContains(t, src, "recipe ", "the legacy Recipe identifier must not appear in the generated store")
}

// TestGenerateStoreCompilesUnderLearnEnabled drives the emitted store package
// through `go test -c` to catch any template error that produces valid-shaped
// but uncompilable Go (mis-placed comma in the migrations slice, malformed
// string literal, etc.). The -c flag compiles tests without running them, so
// the new v2->v3 additive-migration assertion in the schema_version_test
// template is exercised at type-check time without paying the runtime cost.
func TestGenerateStoreCompilesUnderLearnEnabled(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-compile")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-compile-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "test", "-c", "-o", filepath.Join(t.TempDir(), "store.test"), "./internal/store/...")
}

func TestGenerateLearnEnabledRequiresStoreVision(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-no-store")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-no-store-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: false, Export: true}

	err := gen.Generate()
	require.ErrorContains(t, err, "learn.enabled requires VisionSet.Store=true; the learn package depends on internal/store")
}

// TestLearnConfigIsZeroValueByDefault pins the LearnConfig default-disabled
// contract: a spec without an explicit `learn:` block emits a Learn field
// whose Enabled bit is false, so the migrations slice gate stays cold and
// no learn-table SQL emits.
func TestLearnConfigIsZeroValueByDefault(t *testing.T) {
	t.Parallel()

	var s spec.APISpec
	require.False(t, s.Learn.Enabled, "zero-value APISpec must leave Learn disabled")
}
