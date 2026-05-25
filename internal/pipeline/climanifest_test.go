package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteCLIManifest(t *testing.T) {
	dir := t.TempDir()

	m := CLIManifest{
		SchemaVersion:        1,
		GeneratedAt:          time.Date(2026, 3, 28, 15, 4, 5, 0, time.UTC),
		PrintingPressVersion: "0.4.0",
		APIName:              "notion",
		CLIName:              "notion-pp-cli",
		SpecURL:              "https://example.com/spec.json",
		SpecPath:             "/tmp/spec.json",
		SpecFormat:           "openapi3",
		SpecChecksum:         "sha256:abc123",
		RunID:                "20260328T150405Z-abcd1234",
		CatalogEntry:         "notion",
		Category:             "productivity",
		Description:          "Notion workspace API",
	}

	err := WriteCLIManifest(dir, m)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "notion", got.APIName)
	assert.Equal(t, "notion-pp-cli", got.CLIName)
	assert.Equal(t, "0.4.0", got.PrintingPressVersion)
	assert.Equal(t, "https://example.com/spec.json", got.SpecURL)
	assert.Equal(t, "/tmp/spec.json", got.SpecPath)
	assert.Equal(t, "openapi3", got.SpecFormat)
	assert.Equal(t, "sha256:abc123", got.SpecChecksum)
	assert.Equal(t, "20260328T150405Z-abcd1234", got.RunID)
	assert.Equal(t, "notion", got.CatalogEntry)
	assert.Equal(t, "productivity", got.Category)
	assert.Equal(t, "Notion workspace API", got.Description)
	assert.Equal(t, m.GeneratedAt, got.GeneratedAt)
}

func TestWriteCLIManifestSchemaVersionAlwaysOne(t *testing.T) {
	dir := t.TempDir()
	m := CLIManifest{SchemaVersion: 1, APIName: "test"}

	err := WriteCLIManifest(dir, m)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 1, got.SchemaVersion)
}

func TestWriteCLIManifestOmitsEmptyOptionalFields(t *testing.T) {
	dir := t.TempDir()

	m := CLIManifest{
		SchemaVersion:        1,
		GeneratedAt:          time.Now().UTC(),
		PrintingPressVersion: "0.4.0",
		APIName:              "test",
		CLIName:              "test-pp-cli",
		SpecURL:              "https://example.com/spec.json",
		// SpecPath, CatalogEntry intentionally omitted
	}

	err := WriteCLIManifest(dir, m)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	// Verify optional fields are not present in JSON
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	_, hasCatalog := raw["catalog_entry"]
	assert.False(t, hasCatalog, "catalog_entry should be omitted when empty")

	_, hasSpecPath := raw["spec_path"]
	assert.False(t, hasSpecPath, "spec_path should be omitted when empty")

	_, hasCategory := raw["category"]
	assert.False(t, hasCategory, "category should be omitted when empty")

	_, hasDescription := raw["description"]
	assert.False(t, hasDescription, "description should be omitted when empty")
}

func TestWriteCLIManifestNonexistentDir(t *testing.T) {
	err := WriteCLIManifest("/nonexistent/path", CLIManifest{})
	assert.Error(t, err)
}

func TestWritePatchesIndex(t *testing.T) {
	dir := t.TempDir()

	err := WritePatchesIndex(dir, "20260517-091036", "4.8.0")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, PatchesIndexFilename))
	require.NoError(t, err)

	var idx PatchesIndex
	require.NoError(t, json.Unmarshal(data, &idx))

	assert.Equal(t, 1, idx.SchemaVersion)
	assert.Equal(t, "20260517-091036", idx.BaseRunID)
	assert.Equal(t, "4.8.0", idx.BasePrintingPressVersion)
	assert.NotNil(t, idx.Patches, "Patches should be non-nil (empty slice) so JSON serializes as [] not null")
	assert.Empty(t, idx.Patches)
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, idx.AppliedAt, "applied_at should be YYYY-MM-DD")

	// Verify the raw JSON serializes patches as [], not null — empty
	// arrays are what the library verifier expects, and []json.RawMessage{}
	// is what produces that shape (vs nil → null).
	assert.Contains(t, string(data), `"patches": []`)
}

func TestWritePatchesIndexPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, PatchesIndexFilename)

	// Pre-create a populated index (simulating an agent that already
	// applied an in-session patch to the generated CLI).
	preExisting := []byte(`{
  "schema_version": 1,
  "applied_at": "2026-01-15",
  "base_run_id": "20260115-000000",
  "base_printing_press_version": "3.0.0",
  "patches": [
    {"id": "test-patch", "summary": "preserved across regen"}
  ]
}
`)
	require.NoError(t, os.WriteFile(path, preExisting, 0o644))

	// Call WritePatchesIndex with different values — must NOT overwrite.
	err := WritePatchesIndex(dir, "20260517-091036", "4.8.0")
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, preExisting, got, "existing patches index must be preserved byte-for-byte on regen")
}

func TestWritePatchesIndexNonexistentDir(t *testing.T) {
	err := WritePatchesIndex("/nonexistent/path/that/does/not/exist", "run", "version")
	assert.Error(t, err)
}

func TestWriteManifestForGenerateEmitsPatchesIndex(t *testing.T) {
	dir := t.TempDir()

	// Place an OpenAPI spec so the manifest writer has format/checksum to populate.
	specContent := []byte(`{"openapi": "3.0.0", "info": {"title": "Test"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.json"), specContent, 0o644))

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "test-api",
		SpecSrcs:  []string{"https://example.com/openapi.json"},
		OutputDir: dir,
		RunID:     "20260517-091036",
	})
	require.NoError(t, err)

	// Both manifest siblings must land. (MCPB manifest.json is conditional
	// on populated MCP metadata, which this minimal test doesn't trigger;
	// the existing TestWriteManifestForGenerateWithSpecURL siblings cover
	// MCPB-emit cases.)
	for _, fname := range []string{CLIManifestFilename, PatchesIndexFilename} {
		_, err := os.Stat(filepath.Join(dir, fname))
		assert.NoError(t, err, "expected %s to exist after WriteManifestForGenerate", fname)
	}

	// Patches index has the right shape and the run-ID flows through.
	data, err := os.ReadFile(filepath.Join(dir, PatchesIndexFilename))
	require.NoError(t, err)
	var idx PatchesIndex
	require.NoError(t, json.Unmarshal(data, &idx))
	assert.Equal(t, "20260517-091036", idx.BaseRunID)
	assert.Equal(t, version.Version, idx.BasePrintingPressVersion)
}

func TestWriteManifestForGeneratePreservesPatchesIndexOnRegen(t *testing.T) {
	dir := t.TempDir()
	specContent := []byte(`{"openapi": "3.0.0", "info": {"title": "Test"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.json"), specContent, 0o644))

	// First generate.
	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "test-api",
		SpecSrcs:  []string{"https://example.com/openapi.json"},
		OutputDir: dir,
		RunID:     "20260517-091036",
	}))
	patchesPath := filepath.Join(dir, PatchesIndexFilename)
	firstContent, err := os.ReadFile(patchesPath)
	require.NoError(t, err)

	// Second generate (simulates `generate --force` regen) with different
	// run-ID — patches index must be preserved byte-for-byte.
	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "test-api",
		SpecSrcs:  []string{"https://example.com/openapi.json"},
		OutputDir: dir,
		RunID:     "20260601-120000",
	}))
	secondContent, err := os.ReadFile(patchesPath)
	require.NoError(t, err)

	assert.Equal(t, firstContent, secondContent, "patches index must survive regen unchanged")
}

func TestSyncCLIManifestNovelFeaturesPreservesManifestContract(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte(`{
  "schema_version": 1,
  "generated_at": "2026-05-09T17:28:02Z",
  "printing_press_version": "4.2.0",
  "api_name": "openrouter",
  "display_name": "OpenRouter",
  "cli_name": "openrouter-pp-cli",
  "printer": "rvdlaar",
  "printer_name": "Rick van de Laar",
  "spec_url": "https://example.com/openapi.json",
  "category": "ai",
  "description": "Access OpenRouter models.",
  "x_future_manifest_field": {
    "keep": true
  },
  "novel_features": [
    {
      "name": "Old",
      "command": "old",
      "description": "Old feature."
    }
  ]
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), manifest, 0o644))

	changed, err := SyncCLIManifestNovelFeatures(dir, []NovelFeature{
		{Name: "Model finder", Command: "models find", Description: "Find a model for a prompt."},
	})
	require.NoError(t, err)
	assert.True(t, changed)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, float64(1), got["schema_version"])
	assert.Equal(t, "4.2.0", got["printing_press_version"])
	assert.Equal(t, "openrouter", got["api_name"])
	assert.Equal(t, "openrouter-pp-cli", got["cli_name"])
	assert.Equal(t, "rvdlaar", got["printer"])
	assert.Equal(t, "Rick van de Laar", got["printer_name"])
	assert.Equal(t, "https://example.com/openapi.json", got["spec_url"])
	assert.Equal(t, "ai", got["category"])
	assert.Equal(t, "Access OpenRouter models.", got["description"])

	future, ok := got["x_future_manifest_field"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, future["keep"])

	features, ok := got["novel_features"].([]any)
	require.True(t, ok)
	require.Len(t, features, 1)
	feature, ok := features[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Model finder", feature["name"])
	assert.Equal(t, "models find", feature["command"])
	assert.Equal(t, "Find a model for a prompt.", feature["description"])
}

func TestSpecChecksum(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"openapi": "3.0.0"}`)
	specPath := filepath.Join(dir, "spec.json")
	require.NoError(t, os.WriteFile(specPath, content, 0o644))

	checksum, err := specChecksum(specPath)
	require.NoError(t, err)

	h := sha256.Sum256(content)
	expected := "sha256:" + hex.EncodeToString(h[:])
	assert.Equal(t, expected, checksum)
}

func TestSpecChecksumNonexistentFile(t *testing.T) {
	checksum, err := specChecksum("/nonexistent/file.json")
	require.NoError(t, err)
	assert.Empty(t, checksum)
}

func TestPublishWorkingCLIWritesManifest(t *testing.T) {
	home := setPressTestEnv(t)

	// Create a working directory with a minimal CLI structure and spec
	workingDir := filepath.Join(home, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"),
		0o644,
	))

	specContent := []byte(`{"openapi": "3.0.0", "info": {"title": "Test"}}`)
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "spec.json"),
		specContent,
		0o644,
	))

	// Create a PipelineState pointing to the working directory.
	// SpecURL is a real URL, SpecPath is a different local path —
	// both should appear in the manifest.
	state := NewState("test-api", workingDir)
	state.SpecURL = "https://example.com/spec.json"
	state.SpecPath = "/tmp/test-spec.json"

	// Ensure state directory exists so Save() works
	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	// Publish to a new directory
	publishDir := filepath.Join(home, "library", "test-pp-cli")
	finalDir, err := PublishWorkingCLI(state, publishDir)
	require.NoError(t, err)
	assert.Equal(t, publishDir, finalDir)

	// Verify .printing-press.json exists in published directory
	manifestPath := filepath.Join(finalDir, CLIManifestFilename)
	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "test-api", got.APIName)
	assert.Equal(t, "test-api-pp-cli", got.CLIName)
	assert.Equal(t, version.Version, got.PrintingPressVersion)
	assert.Equal(t, "https://example.com/spec.json", got.SpecURL)
	assert.Equal(t, "/tmp/test-spec.json", got.SpecPath)
	assert.Equal(t, "openapi3", got.SpecFormat)
	assert.NotEmpty(t, got.RunID)
	assert.False(t, got.GeneratedAt.IsZero())

	// Verify checksum matches independently computed value
	h := sha256.Sum256(specContent)
	expectedChecksum := "sha256:" + hex.EncodeToString(h[:])
	assert.Equal(t, expectedChecksum, got.SpecChecksum)
}

func TestPublishManifestNormalizesLocalPathInSpecURL(t *testing.T) {
	home := setPressTestEnv(t)

	workingDir := filepath.Join(home, "working", "local-spec-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"), 0o644))

	// Simulate the fullrun --spec /path/to/spec.json behavior:
	// SpecURL = local path, SpecPath = same local path
	state := NewState("local-test", workingDir)
	state.SpecURL = "/tmp/my-spec.yaml"
	state.SpecPath = "/tmp/my-spec.yaml"

	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	publishDir := filepath.Join(home, "library", "local-spec-pp-cli")
	finalDir, err := PublishWorkingCLI(state, publishDir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(finalDir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	// Local path should be in spec_path, NOT in spec_url
	assert.Empty(t, got.SpecURL, "local file path should not appear in spec_url")
	assert.Equal(t, "/tmp/my-spec.yaml", got.SpecPath)
}

func TestPublishManifestNormalizesURLDuplicatedInBothFields(t *testing.T) {
	home := setPressTestEnv(t)

	workingDir := filepath.Join(home, "working", "dup-url-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"), 0o644))

	// Simulate the fullrun --spec https://... behavior:
	// SpecURL = URL, SpecPath = same URL (duplicated)
	state := NewState("dup-url", workingDir)
	state.SpecURL = "https://example.com/spec.json"
	state.SpecPath = "https://example.com/spec.json"

	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	publishDir := filepath.Join(home, "library", "dup-url-pp-cli")
	finalDir, err := PublishWorkingCLI(state, publishDir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(finalDir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	// URL should be in spec_url only, not duplicated into spec_path
	assert.Equal(t, "https://example.com/spec.json", got.SpecURL)
	assert.Empty(t, got.SpecPath, "URL should not be duplicated in spec_path")
}

func TestPublishWorkingCLIWritesManifestForYAMLSpec(t *testing.T) {
	home := setPressTestEnv(t)

	workingDir := filepath.Join(home, "working", "yaml-spec-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"),
		0o644,
	))

	specContent := []byte("openapi: 3.0.0\ninfo:\n  title: YAML Test\n  version: 1.0.0\npaths: {}\n")
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "spec.yaml"),
		specContent,
		0o644,
	))

	state := NewState("yaml-api", workingDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	publishDir := filepath.Join(home, "library", "yaml-spec-pp-cli")
	finalDir, err := PublishWorkingCLI(state, publishDir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(finalDir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "openapi3", got.SpecFormat, "publish must detect format of YAML-archived specs")

	h := sha256.Sum256(specContent)
	expectedChecksum := "sha256:" + hex.EncodeToString(h[:])
	assert.Equal(t, expectedChecksum, got.SpecChecksum, "publish must checksum YAML-archived specs")
}

func TestPublishWorkingCLIManifestWithoutSpec(t *testing.T) {
	home := setPressTestEnv(t)

	// Working directory without spec.json
	workingDir := filepath.Join(home, "working", "no-spec-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"),
		0o644,
	))

	state := NewState("no-spec", workingDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	publishDir := filepath.Join(home, "library", "no-spec-pp-cli")
	finalDir, err := PublishWorkingCLI(state, publishDir)
	require.NoError(t, err)

	// Manifest should still be written with empty spec fields
	data, err := os.ReadFile(filepath.Join(finalDir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "no-spec", got.APIName)
	assert.Empty(t, got.SpecChecksum)
	assert.Empty(t, got.SpecFormat)
}

func TestPublishWorkingCLIWritesMCPMetadataForInternalSpec(t *testing.T) {
	home := setPressTestEnv(t)

	workingDir := filepath.Join(home, "working", "internal-spec-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "spec.json"),
		[]byte(`
name: internal-spec
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars:
    - INTERNAL_SPEC_TOKEN
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        no_auth: true
`),
		0o644,
	))

	state := NewState("internal-spec", workingDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	finalDir, err := PublishWorkingCLI(state, filepath.Join(home, "library", "internal-spec-pp-cli"))
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(finalDir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "internal", got.SpecFormat)
	assert.Equal(t, "internal-spec-pp-mcp", got.MCPBinary)
	assert.Equal(t, 1, got.MCPToolCount)
	assert.Equal(t, 1, got.MCPPublicToolCount)
	assert.Equal(t, "full", got.MCPReady)
	assert.Equal(t, "bearer_token", got.AuthType)
	assert.Equal(t, []string{"INTERNAL_SPEC_TOKEN"}, got.AuthEnvVars)
}

func TestWriteManifestForGenerateWithSpecURL(t *testing.T) {
	dir := t.TempDir()

	// Place an OpenAPI spec in the output dir so format/checksum are detected.
	specContent := []byte(`{"openapi": "3.0.0", "info": {"title": "Test"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.json"), specContent, 0o644))

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "test-api",
		SpecSrcs:  []string{"https://example.com/openapi.json"},
		OutputDir: dir,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "test-api", got.APIName)
	assert.Equal(t, "test-api-pp-cli", got.CLIName)
	assert.Equal(t, version.Version, got.PrintingPressVersion)
	assert.Equal(t, "https://example.com/openapi.json", got.SpecURL)
	assert.Empty(t, got.SpecPath)
	assert.Equal(t, "openapi3", got.SpecFormat)
	assert.NotEmpty(t, got.SpecChecksum)
	assert.False(t, got.GeneratedAt.IsZero())
}

func TestWriteManifestForGenerateWithLocalSpec(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "local-test",
		SpecSrcs:  []string{"/tmp/my-spec.yaml"},
		OutputDir: dir,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Empty(t, got.SpecURL, "local path should not appear in spec_url")
	assert.Equal(t, "/tmp/my-spec.yaml", got.SpecPath)
}

func TestWriteManifestForGenerateKeepsCatalogDisplayNameOverTitleFallback(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "producthunt",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name:                        "producthunt",
			DisplayName:                 "Producthunt",
			DisplayNameDerivedFromTitle: true,
			Auth:                        spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "Product Hunt", got.DisplayName)
}

func TestWriteManifestForGenerateKeepsGeneratedDisplayNameOverExplicitSpecName(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:     "synthetic-display-name",
		OutputDir:   dir,
		DisplayName: "Research Narrative Name",
		Spec: &spec.APISpec{
			Name:        "synthetic-display-name",
			DisplayName: "Explicit Spec Name",
			Auth:        spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "Research Narrative Name", got.DisplayName)
}

func TestWriteManifestForGenerateMatchesCatalogBySpecURLWhenSlugDiffers(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "cloud-run-admin",
		SpecURL:   "https://api.apis.guru/v2/specs/googleapis.com/run/v2/openapi.yaml",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name:                        "cloud-run-admin",
			DisplayName:                 "Cloud Run Admin",
			DisplayNameDerivedFromTitle: true,
			Auth:                        spec.AuthConfig{Type: "bearer_token"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "google-cloud-run", got.CatalogEntry)
	assert.Equal(t, "Google Cloud Run", got.DisplayName)
	assert.Equal(t, "cloud", got.Category)
}

func TestWriteManifestForGenerateWithDocsURL(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "docs-api",
		DocsURL:   "https://docs.example.com/api",
		OutputDir: dir,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "https://docs.example.com/api", got.SpecURL)
	assert.Equal(t, "docs", got.SpecFormat)
}

func TestWriteManifestForGenerateNoSpec(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "bare-api",
		OutputDir: dir,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "bare-api", got.APIName)
	assert.Empty(t, got.SpecURL)
	assert.Empty(t, got.SpecPath)
	assert.Empty(t, got.SpecChecksum)
}

func TestWriteManifestForGenerateStampsRunID(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "runid-test",
		RunID:     "20260504-190931",
		OutputDir: dir,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)

	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "20260504-190931", got.RunID)
}

func TestWriteManifestForGenerateAutoFillsEmptyRunID(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "norunid-test",
		OutputDir: dir,
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	// publish-validate requires run_id to be non-empty. When the caller has no
	// research-dir-derived run_id, the emitter falls back to a fresh
	// YYYYMMDD-HHMMSS so the manifest contract still holds.
	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Regexp(t, `^\d{8}-\d{6}$`, got.RunID)
}

func TestDeriveRunIDFromResearchDir(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"canonical run_id basename", "/tmp/runs/20260504-190931", "20260504-190931"},
		{"trailing slash", "/tmp/runs/20260504-190931/", "20260504-190931"},
		{"basename only", "20260101-000000", "20260101-000000"},
		{"empty input", "", ""},
		{"non-matching basename", "/tmp/runs/research", ""},
		{"partial match (date only)", "/tmp/runs/20260504", ""},
		{"longer suffix", "/tmp/runs/20260504-190931-x", ""},
		{"wrong shape (T separator)", "/tmp/runs/20260504T190931", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, DeriveRunIDFromResearchDir(tc.input))
		})
	}
}

func TestLoadAPINameFromResearchDir(t *testing.T) {
	t.Parallel()

	t.Run("returns api_name when state.json present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "state.json"),
			[]byte(`{"api_name":"canvas","run_id":"20260514-070718"}`),
			0o644,
		))
		assert.Equal(t, "canvas", LoadAPINameFromResearchDir(dir))
	})

	t.Run("trims whitespace from api_name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "state.json"),
			[]byte(`{"api_name":"  canvas  "}`),
			0o644,
		))
		assert.Equal(t, "canvas", LoadAPINameFromResearchDir(dir))
	})

	t.Run("empty string when researchDir empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", LoadAPINameFromResearchDir(""))
	})

	t.Run("empty string when state.json absent", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", LoadAPINameFromResearchDir(t.TempDir()))
	})

	t.Run("empty string when state.json malformed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`not json`), 0o644))
		assert.Equal(t, "", LoadAPINameFromResearchDir(dir))
	})

	t.Run("empty string when api_name field missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"run_id":"20260514-070718"}`), 0o644))
		assert.Equal(t, "", LoadAPINameFromResearchDir(dir))
	})
}

func TestArchiveRunArtifactsCopiesDiscovery(t *testing.T) {
	home := setPressTestEnv(t)

	workingDir := filepath.Join(home, "working", "disc-test-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"), 0o644))

	state := NewState("disc-test", workingDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	// Create research, proofs, and discovery dirs with test content
	require.NoError(t, os.MkdirAll(state.ResearchDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.ResearchDir(), "brief.md"), []byte("brief"), 0o644))

	require.NoError(t, os.MkdirAll(state.DiscoveryDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.DiscoveryDir(), "browser-sniff-report.md"), []byte("report"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(state.DiscoveryDir(), "browser-sniff-unique-paths.txt"), []byte("/api/v1\n/api/v2"), 0o644))

	archiveDir, err := ArchiveRunArtifacts(state)
	require.NoError(t, err)
	assert.DirExists(t, archiveDir)

	// Verify discovery/ was copied
	archivedDiscovery := ArchivedDiscoveryDir(state.APIName, state.RunID)
	assert.DirExists(t, archivedDiscovery)
	report, err := os.ReadFile(filepath.Join(archivedDiscovery, "browser-sniff-report.md"))
	require.NoError(t, err)
	assert.Equal(t, "report", string(report))
	paths, err := os.ReadFile(filepath.Join(archivedDiscovery, "browser-sniff-unique-paths.txt"))
	require.NoError(t, err)
	assert.Equal(t, "/api/v1\n/api/v2", string(paths))

	// Verify research/ was also copied
	assert.DirExists(t, ArchivedResearchDir(state.APIName, state.RunID))
}

func TestArchiveRunArtifactsSkipsMissingDiscovery(t *testing.T) {
	home := setPressTestEnv(t)

	workingDir := filepath.Join(home, "working", "no-disc-pp-cli")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "main.go"),
		[]byte("package main\nfunc main() {}"), 0o644))

	state := NewState("no-disc", workingDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(state.StatePath()), 0o755))
	require.NoError(t, state.Save())

	// Create only research/, no discovery/
	require.NoError(t, os.MkdirAll(state.ResearchDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state.ResearchDir(), "brief.md"), []byte("brief"), 0o644))

	archiveDir, err := ArchiveRunArtifacts(state)
	require.NoError(t, err)
	assert.DirExists(t, archiveDir)

	// Verify discovery/ was NOT created (silently skipped)
	archivedDiscovery := ArchivedDiscoveryDir(state.APIName, state.RunID)
	_, err = os.Stat(archivedDiscovery)
	assert.True(t, os.IsNotExist(err), "discovery/ should not exist when source is absent")

	// Research should still be archived
	assert.DirExists(t, ArchivedResearchDir(state.APIName, state.RunID))
}

func TestComputeMCPReady(t *testing.T) {
	// composed/cookie auth always reports "partial" — the older `if
	// publicTools > 0` gate produced false-negative "cli-only" labels for
	// CLIs whose spec authors hadn't yet tagged endpoints with `no_auth:
	// true`. Pagliacci-pizza is the canonical case: composed auth, 67
	// registered tools (account_register, account_login, store/menu
	// lookups all unauthenticated), but mcp_public_tool_count was 0 so
	// the readiness label was wrong and downstream manifest emission
	// was suppressed.
	tests := []struct {
		name     string
		authType string
		want     string
	}{
		{"none", "none", "full"},
		{"api_key", "api_key", "full"},
		{"bearer_token", "bearer_token", "full"},
		{"oauth2 defaults to full", "oauth2", "full"},
		{"cookie always partial", "cookie", "partial"},
		{"composed always partial", "composed", "partial"},
		{"empty auth type", "", "full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeMCPReady(tt.authType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteMCPBManifest(t *testing.T) {
	t.Run("no manifest file → no MCPB manifest written", func(t *testing.T) {
		dir := t.TempDir()
		err := WriteMCPBManifest(dir)
		require.NoError(t, err)
		_, statErr := os.Stat(filepath.Join(dir, MCPBManifestFilename))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("cli-only readiness still emits manifest", func(t *testing.T) {
		// Earlier behavior: cli-only readiness skipped manifest emission
		// entirely, on the theory that "the bundle won't work standalone."
		// In practice that suppressed bundles for composed/cookie-auth CLIs
		// with unauthenticated tools (registration, login, public lookups).
		// The manifest now ships regardless; user_config's required flag
		// communicates auth-required-or-optional. See Pagliacci-pizza.
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:   "test",
			MCPBinary: "test-pp-mcp",
			MCPReady:  "cli-only",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		_, statErr := os.Stat(filepath.Join(dir, MCPBManifestFilename))
		require.NoError(t, statErr, "cli-only readiness should NOT skip manifest emission")
	})

	t.Run("missing MCP binary → skipped", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{APIName: "no-mcp", MCPReady: "full"})

		require.NoError(t, WriteMCPBManifest(dir))
		_, statErr := os.Stat(filepath.Join(dir, MCPBManifestFilename))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("api_key auth emits required user_config fields", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "stripe",
			DisplayName: "Stripe",
			MCPBinary:   "stripe-pp-mcp",
			MCPReady:    "full",
			AuthType:    "api_key",
			AuthEnvVars: []string{"STRIPE_API_KEY"},
			AuthKeyURL:  "https://dashboard.stripe.com/apikeys",
			Description: "Stripe payments API",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Equal(t, MCPBManifestVersion, got.ManifestVersion)
		assert.Equal(t, "stripe-pp-mcp", got.Name)
		assert.Equal(t, "Stripe", got.DisplayName)
		assert.Equal(t, "Stripe payments API", got.Description)
		assert.Equal(t, "binary", got.Server.Type)
		assert.Equal(t, "bin/stripe-pp-mcp", got.Server.EntryPoint)
		assert.Equal(t, "${__dirname}/bin/stripe-pp-mcp", got.Server.MCPConfig.Command)
		assert.Equal(t, "${user_config.stripe_api_key}", got.Server.MCPConfig.Env["STRIPE_API_KEY"])

		key, ok := got.UserConfig["stripe_api_key"]
		require.True(t, ok, "user_config must include the env var key")
		assert.Equal(t, "STRIPE_API_KEY", key.Title)
		assert.True(t, key.Sensitive)
		assert.True(t, key.Required, "api_key auth must be required")
		assert.Contains(t, key.Description, "https://dashboard.stripe.com/apikeys")
	})

	t.Run("endpoint template vars emit required user_config fields", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:              "shopify",
			DisplayName:          "Shopify",
			MCPBinary:            "shopify-pp-mcp",
			MCPReady:             "full",
			APIVersion:           "2026-04",
			AuthType:             "api_key",
			AuthEnvVars:          []string{"SHOPIFY_ACCESS_TOKEN"},
			EndpointTemplateVars: []string{"shop", "api_version"},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Equal(t, "${user_config.shopify_shop}", got.Server.MCPConfig.Env["SHOPIFY_SHOP"])
		assert.Equal(t, "${user_config.shopify_api_version}", got.Server.MCPConfig.Env["SHOPIFY_API_VERSION"])

		shop, ok := got.UserConfig["shopify_shop"]
		require.True(t, ok)
		assert.Equal(t, "SHOPIFY_SHOP", shop.Title)
		assert.True(t, shop.Required, "{shop} has no spec-level default; user must supply it")
		assert.False(t, shop.Sensitive)
		assert.Contains(t, shop.Description, "{shop}")

		apiVersion, ok := got.UserConfig["shopify_api_version"]
		require.True(t, ok)
		assert.Equal(t, "SHOPIFY_API_VERSION", apiVersion.Title)
		assert.False(t, apiVersion.Required, "spec-defaulted vars are optional in MCPB user_config; presenting Required+Default together is contradictory and causes strict MCPB hosts to block install with the default pre-filled")
		assert.Equal(t, "2026-04", apiVersion.Default)
		assert.Contains(t, apiVersion.Description, "{api_version}")
	})

	t.Run("endpoint template var with spec-declared default is optional in user_config", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:                     "freshservice",
			DisplayName:                 "Freshservice",
			MCPBinary:                   "freshservice-pp-mcp",
			MCPReady:                    "full",
			AuthType:                    "api_key",
			AuthEnvVars:                 []string{"FRESHSERVICE_API_KEY"},
			EndpointTemplateVars:        []string{"domain"},
			EndpointTemplateVarDefaults: map[string]string{"domain": "yourcompany.freshservice.com"},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		domain, ok := got.UserConfig["freshservice_domain"]
		require.True(t, ok)
		assert.Equal(t, "yourcompany.freshservice.com", domain.Default,
			"spec-declared default flows through to MCPB user_config")
		assert.False(t, domain.Required,
			"Required: false avoids the install-blocking contradiction when MCPB hosts honor `required` strictly")
	})

	t.Run("composed auth emits optional user_config fields", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "pizza",
			MCPBinary:   "pizza-pp-mcp",
			MCPReady:    "partial",
			AuthType:    "composed",
			AuthEnvVars: []string{"PIZZA_AUTH"},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		key, ok := got.UserConfig["pizza_auth"]
		require.True(t, ok)
		assert.False(t, key.Required, "composed auth keeps user_config optional")
		assert.Contains(t, key.Description, "Optional.")
	})

	t.Run("api_key auth with auth_optional=true flips Required to false", func(t *testing.T) {
		// recipe-goat shape: USDA_FDC_API_KEY is api_key but only powers
		// opt-in `recipe get --nutrition`. spec.yaml's `auth.optional: true`
		// must override authRequiresCredential's per-type heuristic so the
		// MCPB Configure modal doesn't mark it Required.
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:      "recipe-goat",
			DisplayName:  "Recipe Goat",
			MCPBinary:    "recipe-goat-pp-mcp",
			MCPReady:     "full",
			AuthType:     "api_key",
			AuthEnvVars:  []string{"USDA_FDC_API_KEY"},
			AuthKeyURL:   "https://fdc.nal.usda.gov/api-key-signup",
			AuthOptional: true,
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		key, ok := got.UserConfig["usda_fdc_api_key"]
		require.True(t, ok)
		assert.False(t, key.Required, "auth_optional=true must flip Required to false even on api_key")
		assert.Contains(t, key.Description, "Optional.", "description prefix should reflect optional state")
		assert.Contains(t, key.Description, "https://fdc.nal.usda.gov/api-key-signup")
	})

	t.Run("rich auth user_config includes all declared env vars", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "rich-auth",
			DisplayName: "Rich Auth",
			MCPBinary:   "rich-auth-pp-mcp",
			MCPReady:    "full",
			AuthType:    "api_key",
			AuthKeyURL:  "https://rich-auth.example.com/oauth",
			AuthEnvVars: []string{"RICH_API_KEY", "RICH_CLIENT_SECRET", "RICH_SESSION"},
			AuthEnvVarSpecs: []spec.AuthEnvVar{
				{Name: "RICH_API_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Description: "Per-call API key."},
				{Name: "RICH_OPTIONAL", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: false, Description: "Optional public selector."},
				{Name: "RICH_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true, Description: "Sensitive setup secret."},
				{Name: "RICH_SESSION", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Equal(t, "${user_config.rich_api_key}", got.Server.MCPConfig.Env["RICH_API_KEY"])
		assert.Equal(t, "${user_config.rich_optional}", got.Server.MCPConfig.Env["RICH_OPTIONAL"])
		assert.Equal(t, "${user_config.rich_client_secret}", got.Server.MCPConfig.Env["RICH_CLIENT_SECRET"])
		assert.Equal(t, "${user_config.rich_session}", got.Server.MCPConfig.Env["RICH_SESSION"])

		required, ok := got.UserConfig["rich_api_key"]
		require.True(t, ok)
		assert.True(t, required.Required)
		assert.Equal(t, "Per-call API key.", required.Description)

		optional, ok := got.UserConfig["rich_optional"]
		require.True(t, ok)
		assert.False(t, optional.Required)
		assert.False(t, optional.Sensitive)
		assert.Equal(t, "Optional. Optional public selector.", optional.Description)

		secret, ok := got.UserConfig["rich_client_secret"]
		require.True(t, ok)
		assert.False(t, secret.Required)
		assert.True(t, secret.Sensitive)
		assert.Equal(t, "Optional. Sensitive setup secret.", secret.Description)

		session, ok := got.UserConfig["rich_session"]
		require.True(t, ok)
		assert.False(t, session.Required)
		assert.True(t, session.Sensitive)
		assert.Equal(t, "Optional. Stores RICH_SESSION after it is harvested by the auth setup flow for the Rich Auth MCP server.", session.Description)
		assert.NotContains(t, session.Description, "Get a credential from")
	})

	t.Run("composed apiKey + bearer surfaces sibling creds in user_config and env", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "stcompose",
			DisplayName: "ServiceTitan Compose",
			MCPBinary:   "stcompose-pp-mcp",
			MCPReady:    "full",
			AuthType:    "bearer_token",
			AuthEnvVars: []string{"ST_CLIENT_ID", "ST_CLIENT_SECRET"},
			AuthEnvVarSpecs: []spec.AuthEnvVar{
				{Name: "ST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
				{Name: "ST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
			},
			AuthAdditionalHeaders: []spec.AdditionalAuthHeader{
				{
					Header: "ST-App-Key",
					In:     "header",
					Scheme: "apiKeyHeader",
					EnvVar: spec.AuthEnvVar{
						Name:      "ST_APP_KEY",
						Kind:      spec.AuthEnvVarKindPerCall,
						Required:  true,
						Sensitive: true,
					},
				},
			},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Equal(t, "${user_config.st_app_key}", got.Server.MCPConfig.Env["ST_APP_KEY"],
			"sibling credential must forward through the launch env block")
		assert.Equal(t, "${user_config.st_client_id}", got.Server.MCPConfig.Env["ST_CLIENT_ID"])
		assert.Equal(t, "${user_config.st_client_secret}", got.Server.MCPConfig.Env["ST_CLIENT_SECRET"])
		uc, ok := got.UserConfig["st_app_key"]
		require.True(t, ok, "user_config must prompt for the sibling apiKey credential")
		assert.True(t, uc.Required)
		assert.True(t, uc.Sensitive)
		clientID, ok := got.UserConfig["st_client_id"]
		require.True(t, ok, "auth flow inputs must surface in user_config")
		assert.True(t, clientID.Required)
		assert.False(t, clientID.Sensitive)
		assert.Equal(t, "Collects ST_CLIENT_ID for the auth setup flow used by the ServiceTitan Compose MCP server.", clientID.Description)
		clientSecret, ok := got.UserConfig["st_client_secret"]
		require.True(t, ok, "auth flow secrets must surface in user_config")
		assert.True(t, clientSecret.Required)
		assert.True(t, clientSecret.Sensitive)
		assert.Equal(t, "Collects ST_CLIENT_SECRET for the auth setup flow used by the ServiceTitan Compose MCP server.", clientSecret.Description)
	})

	t.Run("sibling header credential surfaces even when primary env vars are absent", func(t *testing.T) {
		// Defensive: a manifest carrying AuthAdditionalHeaders but no primary
		// env vars (no AuthEnvVarSpecs, no AuthEnvVars) must still emit the
		// sibling credential into user_config and the env block. The generator
		// does not produce this combination today, but hand-crafted manifests
		// and future auth shapes (e.g. OAuth where the primary credential is
		// minted via a non-env code path) would otherwise silently 401.
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:   "siblings-only",
			MCPBinary: "siblings-only-pp-mcp",
			MCPReady:  "full",
			AuthType:  "bearer_token",
			AuthAdditionalHeaders: []spec.AdditionalAuthHeader{
				{
					Header: "X-Sibling-Key",
					In:     "header",
					Scheme: "apiKeyHeader",
					EnvVar: spec.AuthEnvVar{
						Name:      "SIBLINGS_ONLY_KEY",
						Kind:      spec.AuthEnvVarKindPerCall,
						Required:  true,
						Sensitive: true,
					},
				},
			},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Equal(t, "${user_config.siblings_only_key}", got.Server.MCPConfig.Env["SIBLINGS_ONLY_KEY"])
		uc, ok := got.UserConfig["siblings_only_key"]
		require.True(t, ok, "sibling credential must surface in user_config")
		assert.True(t, uc.Required)
		assert.True(t, uc.Sensitive)
	})

	t.Run("auth metadata overrides user_config title and description", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:         "flightgoat",
			DisplayName:     "Flight GOAT",
			MCPBinary:       "flightgoat-pp-mcp",
			MCPReady:        "full",
			AuthType:        "api_key",
			AuthEnvVars:     []string{"FLIGHTAWARE_API_KEY"},
			AuthOptional:    true,
			AuthKeyURL:      "https://flightaware.com/commercial/aeroapi/",
			AuthTitle:       "FlightAware AeroAPI Key",
			AuthDescription: "Optional FlightAware AeroAPI credential for enriched flight data.",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Equal(t, "${user_config.flightaware_api_key}", got.Server.MCPConfig.Env["FLIGHTAWARE_API_KEY"])
		key, ok := got.UserConfig["flightaware_api_key"]
		require.True(t, ok)
		assert.Equal(t, "FlightAware AeroAPI Key", key.Title)
		assert.Equal(t, "Optional FlightAware AeroAPI credential for enriched flight data.", key.Description)
		assert.False(t, key.Required)
		assert.True(t, key.Sensitive)
	})

	t.Run("multiple optional env vars (company-goat shape)", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "company-goat",
			DisplayName: "Company GOAT",
			MCPBinary:   "company-goat-pp-mcp",
			MCPReady:    "full",
			AuthType:    "none",
			AuthEnvVars: []string{"GITHUB_TOKEN", "COMPANIES_HOUSE_API_KEY"},
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		// Both env vars surface as user_config slots; auth_type "none" keeps
		// them optional even when env vars exist (sub-source credentials).
		assert.Len(t, got.UserConfig, 2)
		for _, key := range []string{"github_token", "companies_house_api_key"} {
			v, ok := got.UserConfig[key]
			require.True(t, ok, "user_config must include %q", key)
			assert.False(t, v.Required, "auth_type=none keeps env vars optional")
		}
	})

	t.Run("no auth env vars → no user_config or env block", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:   "espn",
			MCPBinary: "espn-pp-mcp",
			MCPReady:  "full",
			AuthType:  "none",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		got := readMCPBManifest(t, dir)

		assert.Empty(t, got.UserConfig)
		assert.Empty(t, got.Server.MCPConfig.Env)
	})
}

func TestWriteMCPBManifestPreservesExistingDisplayName(t *testing.T) {
	cases := []struct {
		name     string
		apiSlug  string
		existing string
		want     string
	}{
		{"single-word title-case brand (Wikipedia)", "wikipedia", "Wikipedia", "Wikipedia"},
		{"single-word title-case brand (Stripe)", "stripe", "Stripe", "Stripe"},
		{"all-caps brand (ESPN)", "espn", "ESPN", "ESPN"},
		{"branded with punctuation (Cal.com)", "cal-com", "Cal.com", "Cal.com"},
		{"multi-word brand (Company GOAT)", "company-goat", "Company GOAT", "Company GOAT"},
		{"lowercase slug treated as derived fallback", "espn", "espn", "espn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeMCPBManifest(t, dir, MCPBManifest{
				ManifestVersion: MCPBManifestVersion,
				Name:            tc.apiSlug + "-pp-mcp",
				DisplayName:     tc.existing,
				Description:     "stale description",
			})
			// CLIManifest WITHOUT DisplayName forces the chain to fall
			// through to the existing manifest.
			writeManifest(t, dir, CLIManifest{
				APIName:   tc.apiSlug,
				MCPBinary: tc.apiSlug + "-pp-mcp",
				MCPReady:  "full",
			})

			require.NoError(t, WriteMCPBManifest(dir))
			assert.Equal(t, tc.want, readMCPBManifest(t, dir).DisplayName)
		})
	}
}

func TestWriteMCPBManifestDescription(t *testing.T) {
	t.Run("hand-edited description preserved over canonical", func(t *testing.T) {
		dir := t.TempDir()
		handEdit := "Find the best version of any recipe across 37 trusted sites with trust-aware ranking weights real reader signal."
		writeMCPBManifest(t, dir, MCPBManifest{
			ManifestVersion: MCPBManifestVersion,
			Name:            "recipe-goat-pp-mcp",
			DisplayName:     "Recipe Goat",
			Description:     handEdit,
		})
		writeManifest(t, dir, CLIManifest{
			APIName:     "recipe-goat",
			DisplayName: "Recipe Goat",
			MCPBinary:   "recipe-goat-pp-mcp",
			MCPReady:    "full",
			Description: "Recipe GOAT aggregates recipes from canonical-description-source.json",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		assert.Equal(t, handEdit, readMCPBManifest(t, dir).Description)
	})

	t.Run("literal ellipsis description refreshed from canonical", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPBManifest(t, dir, MCPBManifest{
			ManifestVersion: MCPBManifestVersion,
			Name:            "recipe-goat-pp-mcp",
			DisplayName:     "Recipe Goat",
			Description:     "Recipe GOAT aggregates recipes from...",
		})
		writeManifest(t, dir, CLIManifest{
			APIName:     "recipe-goat",
			DisplayName: "Recipe Goat",
			MCPBinary:   "recipe-goat-pp-mcp",
			MCPReady:    "full",
			Description: "Recipe GOAT aggregates recipes from canonical-description-source.json",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		assert.Equal(t, "Recipe GOAT aggregates recipes from canonical-description-source.json", readMCPBManifest(t, dir).Description)
	})

	t.Run("derived-default existing description refreshed from canonical", func(t *testing.T) {
		dir := t.TempDir()
		writeMCPBManifest(t, dir, MCPBManifest{
			ManifestVersion: MCPBManifestVersion,
			Name:            "wikipedia-pp-mcp",
			DisplayName:     "Wikipedia",
			Description:     "Wikipedia API surface as MCP tools.",
		})
		writeManifest(t, dir, CLIManifest{
			APIName:     "wikipedia",
			DisplayName: "Wikipedia",
			MCPBinary:   "wikipedia-pp-mcp",
			MCPReady:    "full",
			Description: "Wikipedia REST API. Article summaries, search, related topics.",
		})

		require.NoError(t, WriteMCPBManifest(dir))
		assert.Equal(t, "Wikipedia REST API. Article summaries, search, related topics.", readMCPBManifest(t, dir).Description)
	})
}

func TestWriteMCPBManifest_DerivedDescriptionTrimsAPIFromDisplayName(t *testing.T) {
	// Spec authors commonly suffix " API" on info.title and
	// x-display-name. Without trimming, the derived description
	// concatenates "Stripe API" + " API surface as MCP tools." into
	// "Stripe API API surface as MCP tools." Single-source the trim
	// at concat sites; the manifest's display_name field still keeps
	// the unmodified spec value.
	tests := []struct {
		name        string
		displayName string
		want        string
	}{
		{"trailing API trimmed", "Stripe API", "Stripe API surface as MCP tools."},
		{"branded with punctuation+API trimmed", "Cal.com API", "Cal.com API surface as MCP tools."},
		{"no trailing API unchanged", "Stripe", "Stripe API surface as MCP tools."},
		{"embedded API not trimmed", "API Gateway", "API Gateway API surface as MCP tools."},
		{"trailing APIs (plural) not trimmed", "Stripe APIs", "Stripe APIs API surface as MCP tools."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, CLIManifest{
				APIName:     "test",
				DisplayName: tc.displayName,
				MCPBinary:   "test-pp-mcp",
				MCPReady:    "full",
			})
			require.NoError(t, WriteMCPBManifest(dir))
			assert.Equal(t, tc.want, readMCPBManifest(t, dir).Description)
			// display_name field itself stays unmodified — only concat
			// sites trim the redundant " API" suffix.
			assert.Equal(t, tc.displayName, readMCPBManifest(t, dir).DisplayName)
		})
	}
}

func TestWriteMCPBManifest_MigratesPriorDoubledAPIDescription(t *testing.T) {
	// Manifests written before this fix carry the doubled form
	// "Stripe API API surface as MCP tools." On the next mcp-sync,
	// the new derived default ("Stripe API surface as MCP tools.")
	// no longer matches; a naive "preserve when not matching"
	// preserves the doubled form forever. Recognize the prior form
	// too so the regen refreshes to the trimmed default.
	dir := t.TempDir()
	writeMCPBManifest(t, dir, MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            "stripe-pp-mcp",
		DisplayName:     "Stripe API",
		Description:     "Stripe API API surface as MCP tools.",
	})
	writeManifest(t, dir, CLIManifest{
		APIName:     "stripe",
		DisplayName: "Stripe API",
		MCPBinary:   "stripe-pp-mcp",
		MCPReady:    "full",
	})
	require.NoError(t, WriteMCPBManifest(dir))
	assert.Equal(t, "Stripe API surface as MCP tools.", readMCPBManifest(t, dir).Description)
}

func TestWriteMCPBManifest_EnvVarDescriptionTrimsAPIFromDisplayName(t *testing.T) {
	// The user_config env var description's "<displayName> MCP server"
	// substring uses the same trim so "Stripe API MCP server" reads
	// as "Stripe MCP server" — slightly more natural English and
	// keeps the surfaces consistent.
	dir := t.TempDir()
	writeManifest(t, dir, CLIManifest{
		APIName:     "stripe",
		DisplayName: "Stripe API",
		MCPBinary:   "stripe-pp-mcp",
		MCPReady:    "full",
		AuthType:    "bearer_token",
		AuthEnvVars: []string{"STRIPE_TOKEN"},
	})
	require.NoError(t, WriteMCPBManifest(dir))
	got := readMCPBManifest(t, dir)
	require.NotNil(t, got.UserConfig)
	stripe, ok := got.UserConfig["stripe_token"]
	require.True(t, ok, "stripe_token user_config entry must exist")
	assert.Equal(t, "Sets STRIPE_TOKEN for the Stripe MCP server.", stripe.Description)
}

func TestRefreshCLIManifestFromSpecRefreshesDisplayName(t *testing.T) {
	// RefreshCLIManifestFromSpec must overwrite an existing
	// DisplayName, not preserve it — otherwise stale slug-derived
	// values survive across mcp-sync cycles.
	dir := t.TempDir()
	writeManifest(t, dir, CLIManifest{
		APIName:     "cal-com",
		DisplayName: "Cal Com", // stale slug-derived value
		MCPBinary:   "cal-com-pp-mcp",
		MCPReady:    "full",
	})
	parsed := &spec.APISpec{
		Name:        "cal-com",
		DisplayName: "Cal.com", // authoritative value from upstream preservation
	}

	require.NoError(t, RefreshCLIManifestFromSpec(dir, parsed))

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var got CLIManifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "Cal.com", got.DisplayName)
}

func writeManifest(t *testing.T, dir string, m CLIManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), data, 0o644))
}

func writeMCPBManifest(t *testing.T, dir string, m MCPBManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), data, 0o644))
}

func readMCPBManifest(t *testing.T, dir string) MCPBManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, MCPBManifestFilename))
	require.NoError(t, err)
	var got MCPBManifest
	require.NoError(t, json.Unmarshal(data, &got))
	return got
}

func TestDetectSpecFormat(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "openapi json",
			data:     []byte(`{"openapi": "3.0.0", "info": {}}`),
			expected: "openapi3",
		},
		{
			name:     "openapi yaml",
			data:     []byte("openapi: 3.0.0\ninfo:\n  title: Test"),
			expected: "openapi3",
		},
		{
			name:     "swagger",
			data:     []byte(`{"swagger": "2.0"}`),
			expected: "openapi3",
		},
		{
			name:     "graphql",
			data:     []byte("type Query {\n  hello: String\n}"),
			expected: "graphql",
		},
		{
			name:     "internal spec",
			data:     []byte("name: test\nbase_url: https://api.example.com"),
			expected: "internal",
		},
		{
			name:     "empty",
			data:     []byte{},
			expected: "internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, detectSpecFormat(tt.data))
		})
	}
}

func TestPopulateMCPMetadata(t *testing.T) {
	var m CLIManifest
	populateMCPMetadata(&m, &spec.APISpec{
		Name: "test",
		Auth: spec.AuthConfig{
			Type:        "cookie",
			EnvVars:     []string{"TEST_AUTH"},
			KeyURL:      "https://auth.example.com",
			Optional:    true,
			Title:       "Test Auth",
			Description: "Use this test credential.",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/items", NoAuth: true},
					"create": {Method: "POST", Path: "/items"},
				},
			},
		},
	})

	assert.Equal(t, "test-pp-mcp", m.MCPBinary)
	assert.Equal(t, 2, m.MCPToolCount)
	assert.Equal(t, 1, m.MCPPublicToolCount)
	assert.Equal(t, "partial", m.MCPReady)
	assert.Equal(t, "cookie", m.AuthType)
	assert.Equal(t, []string{"TEST_AUTH"}, m.AuthEnvVars)
	assert.Empty(t, m.AuthEnvVarSpecs)
	assert.Equal(t, "https://auth.example.com", m.AuthKeyURL)
	assert.True(t, m.AuthOptional)
	assert.Equal(t, "Test Auth", m.AuthTitle)
	assert.Equal(t, "Use this test credential.", m.AuthDescription)
}

func TestPopulateMCPMetadataIncludesTierEnvVars(t *testing.T) {
	var m CLIManifest
	populateMCPMetadata(&m, &spec.APISpec{
		Name: "tiered",
		Auth: spec.AuthConfig{
			Type:    "bearer_token",
			EnvVars: []string{"GLOBAL_TOKEN"},
		},
		TierRouting: spec.TierRoutingConfig{
			Tiers: map[string]spec.TierConfig{
				"free": {Auth: spec.AuthConfig{Type: "none"}},
				"paid": {
					Auth: spec.AuthConfig{
						Type:    "api_key",
						EnvVars: []string{"PAID_KEY", "GLOBAL_TOKEN"},
					},
				},
			},
		},
		Resources: map[string]spec.Resource{
			"items": {
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items"},
				},
			},
		},
	})

	assert.Equal(t, []string{"GLOBAL_TOKEN", "PAID_KEY"}, m.AuthEnvVars)
}

func TestPopulateMCPMetadataMergesTierEnvVarSpecsWithTierOverride(t *testing.T) {
	var m CLIManifest
	populateMCPMetadata(&m, &spec.APISpec{
		Name: "tiered-rich",
		Auth: spec.AuthConfig{
			Type: "bearer_token",
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "SHARED_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Description: "Global shared token."},
				{Name: "GLOBAL_ONLY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			},
		},
		TierRouting: spec.TierRoutingConfig{
			Tiers: map[string]spec.TierConfig{
				"enterprise": {
					Auth: spec.AuthConfig{
						Type:    "api_key",
						EnvVars: []string{"LEGACY_TIER_ONLY"},
					},
				},
				"paid": {
					Auth: spec.AuthConfig{
						Type: "api_key",
						EnvVarSpecs: []spec.AuthEnvVar{
							{Name: "SHARED_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Tier shared token."},
						},
					},
				},
			},
		},
	})

	assert.Equal(t, []string{"SHARED_TOKEN", "GLOBAL_ONLY", "LEGACY_TIER_ONLY"}, m.AuthEnvVars)
	assert.Equal(t, []spec.AuthEnvVar{
		{Name: "SHARED_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Tier shared token."},
		{Name: "GLOBAL_ONLY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		{Name: "LEGACY_TIER_ONLY", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
	}, m.AuthEnvVarSpecs)
}

// TestPopulateMCPMetadataDisplayNamePrecedence pins:
//
//	spec.DisplayName (explicit) > existing m.DisplayName (catalog) > EffectiveDisplayName fallback
func TestPopulateMCPMetadataDisplayNamePrecedence(t *testing.T) {
	t.Run("spec explicit wins over existing catalog value", func(t *testing.T) {
		m := CLIManifest{DisplayName: "Catalog Name"}
		populateMCPMetadata(&m, &spec.APISpec{
			Name:        "test",
			DisplayName: "Spec Name",
			Auth:        spec.AuthConfig{Type: "none"},
		})
		assert.Equal(t, "Spec Name", m.DisplayName)
	})

	t.Run("existing catalog value preserved when spec is silent", func(t *testing.T) {
		m := CLIManifest{DisplayName: "Catalog Name"}
		populateMCPMetadata(&m, &spec.APISpec{
			Name: "twoword",
			Auth: spec.AuthConfig{Type: "none"},
		})
		assert.Equal(t, "Catalog Name", m.DisplayName)
	})

	t.Run("existing catalog value preserved over title-derived fallback", func(t *testing.T) {
		m := CLIManifest{DisplayName: "Catalog Name"}
		populateMCPMetadata(&m, &spec.APISpec{
			Name:                        "catalog-name",
			DisplayName:                 "Catalog Name API",
			DisplayNameDerivedFromTitle: true,
			Auth:                        spec.AuthConfig{Type: "none"},
		})
		assert.Equal(t, "Catalog Name", m.DisplayName)
	})

	t.Run("title-case fallback fires only when both spec and existing are empty", func(t *testing.T) {
		var m CLIManifest
		populateMCPMetadata(&m, &spec.APISpec{
			Name: "test-api",
			Auth: spec.AuthConfig{Type: "none"},
		})
		assert.Equal(t, "Test Api", m.DisplayName)
	})
}

// TestPopulateMCPMetadataCLIDescription pins that spec.cli_description
// overrides existing m.Description (catalog default).
func TestPopulateMCPMetadataCLIDescription(t *testing.T) {
	t.Run("cli_description overrides catalog description", func(t *testing.T) {
		m := CLIManifest{Description: "API-shaped catalog description."}
		populateMCPMetadata(&m, &spec.APISpec{
			Name:           "test",
			CLIDescription: "CLI-shaped description.",
			Auth:           spec.AuthConfig{Type: "none"},
		})
		assert.Equal(t, "CLI-shaped description.", m.Description)
	})

	t.Run("empty cli_description leaves catalog description in place", func(t *testing.T) {
		m := CLIManifest{Description: "Catalog description."}
		populateMCPMetadata(&m, &spec.APISpec{
			Name: "test",
			Auth: spec.AuthConfig{Type: "none"},
		})
		assert.Equal(t, "Catalog description.", m.Description)
	})
}

func TestRefreshCLIManifestFromSpecPreservesDurableDescription(t *testing.T) {
	dir := t.TempDir()
	existing := "Curated manifest description."
	writeManifest(t, dir, CLIManifest{
		APIName:     "asana",
		Description: existing,
	})

	err := RefreshCLIManifestFromSpec(dir, &spec.APISpec{
		Name:           "asana",
		CLIDescription: "Parsed spec fallback.",
		Auth:           spec.AuthConfig{Type: "none"},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, existing, got.Description)
}

func TestRefreshCLIManifestFromSpecReplacesLiteralEllipsisDescription(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, CLIManifest{
		APIName:     "asana",
		Description: "Legacy truncated manifest description...",
	})

	err := RefreshCLIManifestFromSpec(dir, &spec.APISpec{
		Name:           "asana",
		CLIDescription: "Parsed spec fallback.",
		Auth:           spec.AuthConfig{Type: "none"},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "Parsed spec fallback.", got.Description)
}

func TestWriteManifestForGenerateUsesExplicitCatalogDescription(t *testing.T) {
	dir := t.TempDir()
	rich := "Local-first CLI for the Roam HQ API (chat, On-Air events, transcripts, SCIM, webhooks) with offline FTS search and agent-friendly JSON output."

	err := WriteManifestForGenerate(GenerateManifestParams{
		// asana is in the embedded catalog; this proves the generate-time
		// description from the rendered project wins over catalog fallback
		// and the source spec's CLI-shaped copy.
		APIName:     "asana",
		OutputDir:   dir,
		Description: rich,
		Spec: &spec.APISpec{
			Name:           "asana",
			CLIDescription: "Manage Asana workspaces from the terminal.",
			Auth:           spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, rich, got.Description)
	assert.False(t, strings.HasSuffix(got.Description, "..."))
}

func TestWriteManifestForGenerateUsesExplicitCatalogDisplayName(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:     "alaska-airlines",
		OutputDir:   dir,
		DisplayName: "Alaska Airlines",
		Spec: &spec.APISpec{
			Name:                        "alaska-airlines",
			DisplayName:                 "Alaska Airlines API",
			DisplayNameDerivedFromTitle: true,
			Auth:                        spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "Alaska Airlines", got.DisplayName)
}

func TestWriteManifestForGeneratePreservesExistingDurableDescription(t *testing.T) {
	dir := t.TempDir()
	existing := "Curated CLI description that should survive force regeneration."
	writeManifest(t, dir, CLIManifest{
		APIName:     "asana",
		Description: existing,
	})

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:     "asana",
		OutputDir:   dir,
		Description: "Fresh generated fallback copy.",
		Spec: &spec.APISpec{
			Name: "asana",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, existing, got.Description)
}

func TestWriteManifestForGeneratePreservesExistingManifestExtras(t *testing.T) {
	dir := t.TempDir()
	existingRaw := `{
  "schema_version": 1,
  "api_name": "synthetic-polymarket",
  "cli_name": "synthetic-polymarket-pp-cli",
  "run_id": "20260523-171100",
  "owner": "original-owner",
  "printer": "original-printer",
  "printer_name": "Original Printer",
  "spec_url": "https://example.com/openapi.json",
  "api_version": "2026-05-23",
  "category": "other",
  "novel_features": [
    {
      "name": "Market scanner",
      "command": "markets scan",
      "description": "Finds active markets"
    }
  ],
  "operator_note": "published-library override"
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), []byte(existingRaw), 0o644))

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "synthetic-polymarket",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name: "synthetic-polymarket",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.JSONEq(t, `"published-library override"`, string(raw["operator_note"]))

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "20260523-171100", got.RunID)
	assert.Equal(t, "original-owner", got.Owner)
	assert.Equal(t, "original-printer", got.Printer)
	assert.Equal(t, "Original Printer", got.PrinterName)
	assert.Equal(t, "https://example.com/openapi.json", got.SpecURL)
	assert.Equal(t, "2026-05-23", got.APIVersion)
	assert.Equal(t, "other", got.Category)
	require.Len(t, got.NovelFeatures, 1)
	assert.Equal(t, "Market scanner", got.NovelFeatures[0].Name)
}

func TestWriteManifestForGenerateDoesNotPreserveCrossAPIManifestExtras(t *testing.T) {
	dir := t.TempDir()
	existingRaw := `{
  "schema_version": 1,
  "api_name": "old-api",
  "cli_name": "old-api-pp-cli",
  "run_id": "20260523-171100",
  "owner": "old-owner",
  "printer": "old-printer",
  "category": "other",
  "novel_features": [
    {
      "name": "Old scanner",
      "command": "old scan",
      "description": "Old feature"
    }
  ],
  "operator_note": "published-library override"
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), []byte(existingRaw), 0o644))

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "new-api",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name: "new-api",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "operator_note")

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "new-api", got.APIName)
	assert.NotEqual(t, "20260523-171100", got.RunID)
	assert.Empty(t, got.Owner)
	assert.Empty(t, got.Printer)
	assert.Empty(t, got.Category)
	assert.Empty(t, got.NovelFeatures)
}

func TestWriteManifestForGenerateDoesNotPreserveStaleSpecURLWhenFreshSourceIsPath(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.json")
	require.NoError(t, os.WriteFile(specPath, []byte(`{"openapi":"3.0.0","info":{"title":"Synthetic Polymarket","version":"1.0.0"},"paths":{}}`), 0o644))
	existingRaw := `{
  "schema_version": 1,
  "api_name": "synthetic-polymarket",
  "cli_name": "synthetic-polymarket-pp-cli",
  "spec_url": "https://example.com/old-openapi.json",
  "operator_note": "published-library override"
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), []byte(existingRaw), 0o644))

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "synthetic-polymarket",
		SpecSrcs:  []string{specPath},
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name: "synthetic-polymarket",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "operator_note")
	assert.NotContains(t, raw, "spec_url")

	got := readPublishedManifest(t, dir)
	assert.Empty(t, got.SpecURL)
	assert.Equal(t, specPath, got.SpecPath)
}

func TestWriteManifestForGenerateFreshValuesReplaceExistingManifestExtras(t *testing.T) {
	dir := t.TempDir()
	existingRaw := `{
  "schema_version": 1,
  "api_name": "synthetic-polymarket",
  "cli_name": "synthetic-polymarket-pp-cli",
  "category": "other",
  "display_name": "Stale Display",
  "owner": "stale-owner",
  "printer": "stale-printer",
  "printer_name": "Stale Printer",
  "novel_features": [
    {
      "name": "Stale scanner",
      "command": "stale scan",
      "description": "Old feature"
    }
  ],
  "operator_note": "published-library override"
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), []byte(existingRaw), 0o644))

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:     "synthetic-polymarket",
		OutputDir:   dir,
		DisplayName: "Fresh Display",
		Owner:       "fresh-owner",
		Printer:     "fresh-printer",
		PrinterName: "Fresh Printer",
		NovelFeatures: []NovelFeatureManifest{
			{
				Name:        "Fresh scanner",
				Command:     "fresh scan",
				Description: "Fresh feature",
			},
		},
		Spec: &spec.APISpec{
			Name:     "synthetic-polymarket",
			Category: "travel",
			Auth:     spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.JSONEq(t, `"published-library override"`, string(raw["operator_note"]))

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "travel", got.Category)
	assert.Equal(t, "Fresh Display", got.DisplayName)
	assert.Equal(t, "fresh-owner", got.Owner)
	assert.Equal(t, "fresh-printer", got.Printer)
	assert.Equal(t, "Fresh Printer", got.PrinterName)
	require.Len(t, got.NovelFeatures, 1)
	assert.Equal(t, "Fresh scanner", got.NovelFeatures[0].Name)
}

func TestWriteManifestForGenerateEmptyFreshNovelFeaturesClearExisting(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, CLIManifest{
		APIName:  "synthetic-polymarket",
		CLIName:  "synthetic-polymarket-pp-cli",
		Category: "other",
		NovelFeatures: []NovelFeatureManifest{
			{
				Name:        "Stale scanner",
				Command:     "stale scan",
				Description: "Old feature",
			},
		},
	})

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:       "synthetic-polymarket",
		OutputDir:     dir,
		NovelFeatures: []NovelFeatureManifest{},
		Spec: &spec.APISpec{
			Name: "synthetic-polymarket",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Empty(t, got.NovelFeatures)
}

func TestWriteManifestForGenerateReplacesLiteralEllipsisDescription(t *testing.T) {
	dir := t.TempDir()
	fresh := "Curated catalog description without a truncation marker."
	writeManifest(t, dir, CLIManifest{
		APIName:     "asana",
		Description: "Legacy truncated catalog copy...",
	})

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:     "asana",
		OutputDir:   dir,
		Description: fresh,
		Spec: &spec.APISpec{
			Name: "asana",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, fresh, got.Description)
}

// TestWriteManifestForGeneratePopulatesCategoryFromSpec pins the fallback
// that lets synthetic CLIs (not in the embedded catalog) carry their
// spec.Category through to .printing-press.json. Without this fallback,
// verify-skill's canonical-sections check expects the install URL to use
// "other" while the rendered SKILL (which reads category from the spec
// via the template's .Category) uses the real category — a structural
// drift that breaks publish for any synthetic-CLI category.
func TestWriteManifestForGeneratePopulatesCategoryFromSpec(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		// "synthetic-travel-cli" is not in the embedded catalog; the
		// catalog lookup will fail and the spec.Category fallback fires.
		APIName:   "synthetic-travel-cli",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name:     "synthetic-travel-cli",
			Category: "travel",
			Auth:     spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "travel", got.Category, "spec.Category should populate manifest.Category for synthetic CLIs")
}

// TestWriteManifestForGenerateCatalogCategoryWinsOverSpec pins precedence:
// when an API IS in the embedded catalog, the catalog's category wins.
// The spec.Category fallback only fires when the catalog lookup misses.
// Important because catalog-listed APIs may have richer category metadata
// (e.g., curated overrides) the spec doesn't reflect.
func TestWriteManifestForGenerateCatalogCategoryWinsOverSpec(t *testing.T) {
	dir := t.TempDir()

	// asana is in the embedded catalog with category=project-management.
	// The spec carries a different category to confirm the catalog wins.
	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "asana",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name:     "asana",
			Category: "developer-tools", // would-be override
			Auth:     spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Equal(t, "project-management", got.Category, "catalog category must win over spec category")
}

// TestWriteManifestForGenerateNoCategoryAnywhere pins that the manifest's
// category stays empty when neither the catalog nor the spec carries one.
// (verify-skill / install_section.go then default to "other" downstream;
// that fallback is the intended behavior for un-categorized CLIs.)
func TestWriteManifestForGenerateNoCategoryAnywhere(t *testing.T) {
	dir := t.TempDir()

	err := WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "synthetic-uncategorized",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Name: "synthetic-uncategorized",
			Auth: spec.AuthConfig{Type: "none"},
		},
	})
	require.NoError(t, err)

	got := readPublishedManifest(t, dir)
	assert.Empty(t, got.Category, "manifest.Category should stay empty when no source provides one")
}
