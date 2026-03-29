package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
