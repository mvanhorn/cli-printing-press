package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeSpecChecksumNormalizesOnlyTextSpecLineEndings(t *testing.T) {
	lf := []byte("name: checksum-test\ndescription: platform independent\n")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))

	assert.Equal(t, hashSpecBytes(lf), ComputeSpecChecksum(lf, "internal"))
	assert.Equal(t, hashSpecBytes(lf), ComputeSpecChecksum(crlf, "internal"))

	withLoneCR := []byte("name: before\rafter\r\ndescription: preserved\r\n")
	normalizedLoneCR := []byte("name: before\rafter\ndescription: preserved\n")
	assert.Equal(t, hashSpecBytes(normalizedLoneCR), ComputeSpecChecksum(withLoneCR, "internal"),
		"normalization must preserve carriage returns that are not part of CRLF")

	assert.Equal(t, hashSpecBytes(crlf), ComputeSpecChecksum(crlf, "binary"),
		"unknown or non-text formats must retain byte-for-byte checksum semantics")
}

func TestSpecChecksumMatchesLegacyTextLineEndings(t *testing.T) {
	lf := []byte("name: checksum-test\ndescription: platform independent\n")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))

	assert.True(t, SpecChecksumMatches(hashSpecBytes(crlf), lf, "internal"),
		"an LF checkout must accept a manifest written from CRLF bytes")
	assert.True(t, SpecChecksumMatches(hashSpecBytes(lf), crlf, "internal"),
		"a CRLF checkout must accept a manifest written from LF bytes")
	assert.False(t, SpecChecksumMatches(hashSpecBytes(crlf), lf, "binary"),
		"transition matching must remain gated to text spec formats")
	assert.False(t, SpecChecksumMatches(hashSpecBytes([]byte("different\n")), lf, "internal"))
}

func TestWriteManifestForGenerateRewritesLegacyCRLFChecksumOnLineageMatch(t *testing.T) {
	outputDir := t.TempDir()
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	lf := []byte(`name: checksum-transition
description: Checksum transition fixture
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
resources: {}
`)
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	require.NoError(t, os.WriteFile(specPath, lf, 0o644))

	existing := fmt.Sprintf(`{
  "api_name": "checksum-transition",
  "cli_name": "checksum-transition-pp-cli",
  "spec_format": "internal",
  "spec_checksum": %q,
  "operator_note": "preserve me"
}`, hashSpecBytes(crlf))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, CLIManifestFilename), []byte(existing), 0o644))

	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "checksum-transition",
		SpecSrcs:  []string{specPath},
		OutputDir: outputDir,
		RunID:     "20260819-000000",
	}))

	manifest, err := ReadCLIManifest(outputDir)
	require.NoError(t, err)
	assert.Equal(t, ComputeSpecChecksum(lf, "internal"), manifest.SpecChecksum)
	assert.NotEqual(t, hashSpecBytes(crlf), manifest.SpecChecksum,
		"a matching legacy manifest must be rewritten to the canonical checksum")

	data, err := os.ReadFile(filepath.Join(outputDir, CLIManifestFilename))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.JSONEq(t, `"preserve me"`, string(raw["operator_note"]),
		"same-lineage unknown fields must survive the checksum transition")
}
