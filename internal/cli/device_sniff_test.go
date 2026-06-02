package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicesniff/ble"
	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceSniffBLECmdWritesArtifactsAndJSONSummary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "device.yaml")
	analysisPath := filepath.Join(dir, "analysis.json")
	evidencePath := filepath.Join(dir, "evidence.json")
	cmd := newDeviceSniffCmd()
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{
		"ble",
		"--input", filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-events.json"),
		"--output", specPath,
		"--analysis-output", analysisPath,
		"--evidence-output", evidencePath,
		"--json",
	})

	require.NoError(t, cmd.Execute())

	var summary map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &summary))
	assert.Equal(t, specPath, summary["device_spec_path"])
	assert.Equal(t, analysisPath, summary["analysis_path"])
	assert.Equal(t, evidencePath, summary["evidence_path"])
	assert.EqualValues(t, 1, summary["commands"])
	assert.EqualValues(t, 1, summary["telemetry"])
	assert.Equal(t, false, summary["requires_operator_selection"])

	parsed, err := devicespec.Parse(specPath)
	require.NoError(t, err)
	require.Len(t, parsed.Capabilities.Commands, 1)
	assert.Equal(t, "start", parsed.Capabilities.Commands[0].Name)

	reportData, err := os.ReadFile(analysisPath)
	require.NoError(t, err)
	var report ble.Report
	require.NoError(t, json.Unmarshal(reportData, &report))
	assert.Len(t, report.CommandCandidates, 1)

	evidenceData, err := os.ReadFile(evidencePath)
	require.NoError(t, err)
	assert.Contains(t, string(evidenceData), "device-1")
	assert.NotContains(t, string(evidenceData), "AA:BB:CC:DD:EE:01")
	assert.NotContains(t, string(evidenceData), "Trevin")

	info, err := os.Stat(evidencePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "redacted evidence should not be world-readable")
}

func TestBluetoothSniffAliasUsesBLEBackend(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "device.yaml")
	cmd := newBluetoothSniffCmd()
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{
		"--input", filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-community-reference.json"),
		"--output", specPath,
		"--json",
	})

	require.NoError(t, cmd.Execute())

	parsed, err := devicespec.Parse(specPath)
	require.NoError(t, err)
	require.Len(t, parsed.Capabilities.Commands, 1)
	assert.Equal(t, "vendor-action", parsed.Capabilities.Commands[0].Name)
}

func TestDeviceSniffBLECmdRequiresCapturedEvidenceInput(t *testing.T) {
	t.Parallel()

	cmd := newDeviceSniffCmd()
	cmd.SetArgs([]string{"ble"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"input\" not set")
}
