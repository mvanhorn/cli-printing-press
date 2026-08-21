package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCmdEmitsPlatformIndependentSpecChecksum(t *testing.T) {
	lf := []byte(`name: checksum-output
description: Generated checksum output fixture
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/checksum-output-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`)
	inputs := map[string][]byte{
		"lf":   lf,
		"crlf": bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n")),
	}

	checksums := make(map[string]string, len(inputs))
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			specPath := filepath.Join(dir, "spec.yaml")
			outputDir := filepath.Join(dir, "generated")
			require.NoError(t, os.WriteFile(specPath, input, 0o644))

			cmd := newGenerateCmd()
			cmd.SetArgs([]string{
				"--spec", specPath,
				"--output", outputDir,
				"--validate=false",
			})
			require.NoError(t, cmd.Execute())

			manifest, err := pipeline.ReadCLIManifest(outputDir)
			require.NoError(t, err)
			checksums[name] = manifest.SpecChecksum
		})
	}

	assert.Equal(t, checksums["lf"], checksums["crlf"])
	assert.Equal(t, pipeline.ComputeSpecChecksum(lf, "internal"), checksums["lf"])
}

func TestForceRegenSpecHashMatchesLegacyTextLineEndings(t *testing.T) {
	lf := []byte("name: checksum-force\ndescription: force transition\n")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))

	tests := []struct {
		name             string
		manifestSpec     []byte
		currentSpecBytes []byte
	}{
		{name: "CRLF manifest with LF checkout", manifestSpec: crlf, currentSpecBytes: lf},
		{name: "LF manifest with CRLF checkout", manifestSpec: lf, currentSpecBytes: crlf},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshotDir := t.TempDir()
			require.NoError(t, pipeline.WriteCLIManifest(snapshotDir, pipeline.CLIManifest{
				APIName:      "checksum-force",
				SpecFormat:   "internal",
				SpecChecksum: rawSpecChecksumForTest(tt.manifestSpec),
			}))

			assert.True(t, forceRegenSpecHashMatches(snapshotDir, tt.currentSpecBytes))
			assert.False(t, forceRegenSpecHashMatches(snapshotDir, []byte("name: different\n")))
		})
	}
}

func rawSpecChecksumForTest(specBytes []byte) string {
	sum := sha256.Sum256(specBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}
