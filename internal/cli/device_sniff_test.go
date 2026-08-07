package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	for _, p := range []string{specPath, analysisPath, evidencePath} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "%s should not be world-readable: it embeds control payloads and device identity", p)
	}
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

func TestBluetoothSniffDoctorReportsAliasBinary(t *testing.T) {
	t.Parallel()

	cmd := newBluetoothSniffCmd()
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{"doctor"})

	require.NoError(t, cmd.Execute())
	var report struct {
		Binary        string   `json:"binary"`
		SmokeCommands []string `json:"smoke_commands"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	assert.Equal(t, "cli-printing-press bluetooth-sniff", report.Binary)
	for _, c := range report.SmokeCommands {
		assert.NotContains(t, c, "device-sniff ble", "alias smoke commands must use the bluetooth-sniff path, not the nested device-sniff path")
	}
}

func TestDeviceSniffBLEExposesProbeSubcommands(t *testing.T) {
	t.Parallel()

	cmd := newDeviceSniffCmd()
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{
		"ble",
		"scan",
		"--input", filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-events.json"),
	})

	require.NoError(t, cmd.Execute())
	var evidence ble.EvidenceInput
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &evidence))
	require.Len(t, evidence.Events, 1)
	assert.Equal(t, ble.EventAdvertisement, evidence.Events[0].Type)
}

func TestDeviceSniffBLEDoctorReportsNestedCommands(t *testing.T) {
	t.Parallel()

	cmd := newDeviceSniffCmd()
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{"ble", "doctor"})

	require.NoError(t, cmd.Execute())
	var report struct {
		SmokeCommands []string `json:"smoke_commands"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	assert.Contains(t, report.SmokeCommands, "cli-printing-press device-sniff ble scan --input testdata/device/fixtures/ble-events.json")
}

func TestDeviceSniffBLERedactTermFlagIsNotArchived(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	require.NoError(t, os.WriteFile(inputPath, []byte(`{
  "name": "owner-device",
  "identity": {"advertised_names": ["Owner Device"]},
  "events": [
    {"id": "adv", "type": "advertisement", "device_address": "AA:BB:CC:DD:EE:01", "device_name": "Owner Device"},
    {"id": "svc", "type": "service_discovery", "service_uuid": "ff00", "characteristic_uuid": "ff01", "properties": ["read"]}
  ]
}`), 0o600))
	evidencePath := filepath.Join(dir, "evidence.json")

	cmd := newDeviceSniffCmd()
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{
		"ble",
		"--input", inputPath,
		"--output", filepath.Join(dir, "device.yaml"),
		"--evidence-output", evidencePath,
		"--redact-term", "Owner",
	})

	require.NoError(t, cmd.Execute())
	data, err := os.ReadFile(evidencePath)
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "Owner")
	assert.NotContains(t, text, "redaction_terms")
	assert.Contains(t, text, "redacted Device")
	assert.Contains(t, stdout.String(), "supplied name terms redacted",
		"the archive note must report name redaction when --redact-term actually applied")
}

// TestDeviceSniffBLERedactionNoteReflectsEffectiveTerms guards the archive note
// against both ways it can lie: claiming name redaction when every supplied term
// was blank (over-claim), and staying silent when terms arrive from the evidence
// file rather than --redact-term (under-claim).
func TestDeviceSniffBLERedactionNoteReflectsEffectiveTerms(t *testing.T) {
	t.Parallel()

	const claim = "supplied name terms redacted"
	cases := []struct {
		name      string
		fileTerms string
		redactArg []string
		wantClaim bool
	}{
		{name: "no terms", fileTerms: "", redactArg: nil, wantClaim: false},
		{name: "blank flag term only", fileTerms: "", redactArg: []string{"   "}, wantClaim: false},
		{name: "file term, no flag", fileTerms: `"redaction_terms": ["Owner"],`, redactArg: nil, wantClaim: true},
		{name: "flag term", fileTerms: "", redactArg: []string{"Owner"}, wantClaim: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "input.json")
			require.NoError(t, os.WriteFile(inputPath, []byte(`{
  "name": "owner-device",
  `+tc.fileTerms+`
  "identity": {"advertised_names": ["Owner Device"]},
  "events": [
    {"id": "adv", "type": "advertisement", "device_address": "AA:BB:CC:DD:EE:01", "device_name": "Owner Device"},
    {"id": "svc", "type": "service_discovery", "service_uuid": "ff00", "characteristic_uuid": "ff01", "properties": ["read"]}
  ]
}`), 0o600))

			args := []string{
				"ble",
				"--input", inputPath,
				"--output", filepath.Join(dir, "device.yaml"),
				"--evidence-output", filepath.Join(dir, "evidence.json"),
			}
			for _, term := range tc.redactArg {
				args = append(args, "--redact-term", term)
			}

			cmd := newDeviceSniffCmd()
			stdout := new(bytes.Buffer)
			cmd.SetOut(stdout)
			cmd.SetArgs(args)

			require.NoError(t, cmd.Execute())
			if tc.wantClaim {
				assert.Contains(t, stdout.String(), claim)
			} else {
				assert.NotContains(t, stdout.String(), claim)
			}
		})
	}
}

func TestDeviceSniffBLECmdRequiresCapturedEvidenceInput(t *testing.T) {
	t.Parallel()

	cmd := newDeviceSniffCmd()
	cmd.SetArgs([]string{"ble"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"input\" not set")
}

func TestDefaultCLIBuildDoesNotLinkTinyGoBluetooth(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/cli-printing-press")
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	for dep := range strings.FieldsSeq(string(output)) {
		assert.False(t, strings.HasPrefix(dep, "tinygo.org/x/bluetooth"), "default cli-printing-press builds must not link the live BLE backend (got %s)", dep)
	}
}
