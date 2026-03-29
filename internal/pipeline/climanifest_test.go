package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/version"
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
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))

	_, hasCatalog := raw["catalog_entry"]
	assert.False(t, hasCatalog, "catalog_entry should be omitted when empty")

	_, hasSpecPath := raw["spec_path"]
	assert.False(t, hasSpecPath, "spec_path should be omitted when empty")
}

func TestWriteCLIManifestNonexistentDir(t *testing.T) {
	err := WriteCLIManifest("/nonexistent/path", CLIManifest{})
	assert.Error(t, err)
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
