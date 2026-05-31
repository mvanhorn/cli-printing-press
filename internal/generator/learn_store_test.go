package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestGenerateStoreSchemaVersion_DisabledAdvancesToV3(t *testing.T) {
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
	require.Contains(t, src, "const StoreSchemaVersion = 3")
	require.NotContains(t, src, "const StoreSchemaVersion = 6")
	for _, table := range []string{"search_learnings", "search_patterns", "entity_lookups"} {
		require.NotContains(t, src, table, "learn-disabled spec must not emit %s migration", table)
	}
}

func TestGenerateStoreSchemaVersion_EnabledAdvancesToV6WithLearnTables(t *testing.T) {
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
	require.Contains(t, src, "const StoreSchemaVersion = 6")
	require.NotContains(t, src, "const StoreSchemaVersion = 2")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS search_learnings",
		"CREATE TABLE IF NOT EXISTS search_patterns",
		"CREATE TABLE IF NOT EXISTS entity_lookups",
		"CREATE INDEX IF NOT EXISTS idx_learn_query",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_learn_unique",
		"CREATE INDEX IF NOT EXISTS idx_entity_lookup_canonical",
	} {
		require.Contains(t, src, want, "learn-enabled spec must emit %q", want)
	}
	// Divergent tables removed in the canonical-restore pass.
	for _, gone := range []string{"teach_log_metadata", "search_learnings_fts"} {
		require.NotContains(t, src, gone, "canonical schema must not emit divergent table %s", gone)
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
	for _, table := range []string{"search_learnings", "search_patterns", "entity_lookups"} {
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
	} {
		require.Contains(t, src, want, "learn-enabled spec must emit %q", want)
	}
}

// TestGenerateStoreLearnRenamedFromRecipes verifies the schema rename landed.
// Per the predecessor plan, the prediction-goat `search_recipes` table is
// renamed to `search_patterns` everywhere in the emitted store; a
// `search_recipes` leak would mean a partial rename slipped past the
// template. The unrelated `Recipes`/`recipe` identifiers from the workflow
// recipe system stay; this check is scoped to the learn-pattern rename.
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
	require.NotContains(t, src, "search_recipes", "the legacy search_recipes table name must not appear in the generated store")
}

// TestGenerateStoreCompilesUnderLearnEnabled drives the emitted store package
// through `go test -c` to catch any template error that produces valid-shaped
// but uncompilable Go (mis-placed comma in the migrations slice, malformed
// string literal, etc.). The -c flag compiles tests without running them, so
// the schema_version_test template is exercised at type-check time without
// paying the runtime cost.
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
