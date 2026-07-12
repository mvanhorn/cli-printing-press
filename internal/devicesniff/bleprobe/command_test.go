package bleprobe

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

func TestScanReplayEmitsEvidenceInput(t *testing.T) {
	t.Parallel()

	output := executeProbe(t, "scan", "--input", fixturePath("ble-events.json"))
	evidence, err := ble.ParseEvidence(output)
	require.NoError(t, err)

	require.Len(t, evidence.Events, 1)
	assert.Equal(t, ble.EventAdvertisement, evidence.Events[0].Type)
	assert.Equal(t, "AA:BB:CC:DD:EE:01", evidence.Events[0].DeviceAddress)
	assert.Equal(t, "Trevin SessionThing", evidence.Events[0].DeviceName)
	assert.Equal(t, []string{"fd00"}, evidence.Events[0].ServiceUUIDs)
}

func TestInspectReplayOutputRoundTripsThroughAnalyzer(t *testing.T) {
	t.Parallel()

	output := executeProbe(t,
		"inspect",
		"--input", fixturePath("ble-events.json"),
		"--address", "AA:BB:CC:DD:EE:01",
	)
	evidence, err := ble.ParseEvidence(output)
	require.NoError(t, err)
	assert.Len(t, evidence.Actions, 1, "inspect should preserve action markers from replay evidence")

	analysis, err := ble.AnalyzeEvidence(evidence)
	require.NoError(t, err)
	require.Len(t, analysis.Spec.Capabilities.Commands, 1)
	assert.Equal(t, "start", analysis.Spec.Capabilities.Commands[0].Name)
}

func TestReadWriteAndSubscribeReplayEmitEvidenceInput(t *testing.T) {
	t.Parallel()

	inputPath := writeEvidenceFixture(t, ble.EvidenceInput{
		Name: "probe-device",
		Events: []ble.Event{
			{ID: "adv", Type: ble.EventAdvertisement, DeviceAddress: "device-1", DeviceName: "Demo"},
			{ID: "read-battery", Type: ble.EventRead, ServiceUUID: "180f", CharacteristicUUID: "2a19", ValueHex: "64"},
			{ID: "write-mode", Type: ble.EventWrite, ServiceUUID: "ff00", CharacteristicUUID: "ff01", ValueHex: "0102"},
			{ID: "notify-1", Type: ble.EventNotification, ServiceUUID: "180f", CharacteristicUUID: "2a19", ValueHex: "63"},
			{ID: "notify-2", Type: ble.EventNotification, ServiceUUID: "180f", CharacteristicUUID: "2a19", ValueHex: "62"},
		},
	})

	read := parseProbeEvidence(t, executeProbe(t,
		"read", "--input", inputPath, "--address", "device-1", "--characteristic", "2A19",
	))
	require.Len(t, read.Events, 1)
	assert.Equal(t, ble.EventRead, read.Events[0].Type)
	assert.Equal(t, "64", read.Events[0].ValueHex)

	write := parseProbeEvidence(t, executeProbe(t,
		"write", "--input", inputPath, "--address", "device-1", "--characteristic", "ff01", "--value-hex", "0102",
	))
	require.Len(t, write.Events, 1)
	assert.Equal(t, ble.EventWrite, write.Events[0].Type)
	assert.Equal(t, "0102", write.Events[0].ValueHex)

	notifications := parseProbeEvidence(t, executeProbe(t,
		"subscribe", "--input", inputPath, "--address", "device-1", "--characteristic", "2a19",
	))
	require.Len(t, notifications.Events, 2)
	assert.Equal(t, "notify-1", notifications.Events[0].ID)
	assert.Equal(t, "notify-2", notifications.Events[1].ID)
}

func TestMergeEvidenceCombinesProbeOutputsForAnalyzer(t *testing.T) {
	t.Parallel()

	firstPath := writeEvidenceFixture(t, ble.EvidenceInput{
		Name:           "merged-device",
		DisplayName:    "Merged Device",
		RedactionTerms: []string{"Owner"},
		Identity: devicespec.DeviceIdentity{
			AdvertisedNames: []string{"Owner Device"},
			AddressPolicy:   "stable",
			MatchStrength:   "strong",
		},
		Events: []ble.Event{
			{ID: "adv", Type: ble.EventAdvertisement, DeviceAddress: "device-1", DeviceName: "Owner Device"},
			{ID: "svc", Type: ble.EventServiceDiscovery, ServiceUUID: "ff00", CharacteristicUUID: "ff01", Properties: []string{"write"}},
		},
		Actions: []ble.ActionMarker{
			{ID: "action-toggle", Label: "toggle", At: "2026-06-01T12:00:00Z", Safety: "low-risk-write"},
		},
	})
	secondPath := writeEvidenceFixture(t, ble.EvidenceInput{
		Name:           "ignored-name",
		RedactionTerms: []string{"Owner"},
		Events: []ble.Event{
			{ID: "svc", Type: ble.EventServiceDiscovery, ServiceUUID: "ff00", CharacteristicUUID: "ff01", Properties: []string{"write"}},
			{ID: "write-toggle", Type: ble.EventWrite, At: "2026-06-01T12:00:01Z", ServiceUUID: "ff00", CharacteristicUUID: "ff01", ValueHex: "01"},
		},
	})

	output := executeProbe(t, "merge", "--redact-term", "Desk", firstPath, secondPath)
	evidence, err := ble.ParseEvidence(output)
	require.NoError(t, err)

	assert.Equal(t, "merged-device", evidence.Name)
	assert.Equal(t, "Merged Device", evidence.DisplayName)
	assert.Equal(t, []string{"Owner", "Desk"}, evidence.RedactionTerms)
	assert.Equal(t, []string{"Owner Device"}, evidence.Identity.AdvertisedNames)
	require.Len(t, evidence.Events, 3)
	assert.Equal(t, "write-toggle", evidence.Events[2].ID)

	analysis, err := ble.AnalyzeEvidence(evidence)
	require.NoError(t, err)
	require.Len(t, analysis.Spec.Capabilities.Commands, 1)
	assert.Equal(t, "toggle", analysis.Spec.Capabilities.Commands[0].Name)
}

func TestMergeEvidenceKeepsDistinctLiveCharacteristicEvents(t *testing.T) {
	t.Parallel()

	firstPath := writeEvidenceFixture(t, ble.EvidenceInput{
		Name: "merged-device",
		Events: []ble.Event{
			{ID: "live-read-180a-2a29-100", Type: ble.EventRead, ServiceUUID: "180a", CharacteristicUUID: "2a29", ValueHex: "01"},
		},
	})
	secondPath := writeEvidenceFixture(t, ble.EvidenceInput{
		Name: "merged-device",
		Events: []ble.Event{
			{ID: "live-read-180a-2a25-200", Type: ble.EventRead, ServiceUUID: "180a", CharacteristicUUID: "2a25", ValueHex: "02"},
		},
	})

	output := executeProbe(t, "merge", firstPath, secondPath)
	evidence, err := ble.ParseEvidence(output)
	require.NoError(t, err)

	require.Len(t, evidence.Events, 2)
	assert.Equal(t, "2a29", evidence.Events[0].CharacteristicUUID)
	assert.Equal(t, "2a25", evidence.Events[1].CharacteristicUUID)
}

func TestProbeRequiresReplayInputUnlessLive(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand("ble-probe")
	cmd.SetArgs([]string{"scan"})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pass --input for replay evidence or --live for live BLE")
}

func TestProbeReportsLiveUnavailableWhenBuildTagIsAbsent(t *testing.T) {
	t.Parallel()
	if ble.LiveSupport().Compiled {
		t.Skip("replay-only assertion does not apply to live-capable builds")
	}

	cmd := NewRootCommand("ble-probe")
	cmd.SetArgs([]string{"scan", "--live"})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live BLE support is not compiled in")
}

func TestDoctorReportsReplayOnlyBuildReadiness(t *testing.T) {
	t.Parallel()
	if ble.LiveSupport().Compiled {
		t.Skip("replay-only assertion does not apply to live-capable builds")
	}

	output := executeProbe(t, "doctor")
	var report DoctorReport
	require.NoError(t, json.Unmarshal(output, &report))

	assert.Equal(t, "ble-probe", report.Binary)
	assert.True(t, report.ReplaySupported)
	assert.False(t, report.Live.Compiled)
	assert.Contains(t, report.Live.Message, "live BLE support is not compiled in")
	assert.Contains(t, report.SmokeCommands, "ble-probe scan --input testdata/device/fixtures/ble-events.json")
	assert.Contains(t, report.SmokeCommands, "ble-probe merge scan.json inspect.json read.json notify.json > evidence.json")
	assert.Contains(t, report.HardwareCommands, "ble-probe scan --live --duration-ms 10000")
}

func executeProbe(t *testing.T, args ...string) []byte {
	t.Helper()

	cmd := NewRootCommand("ble-probe")
	stdout := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return stdout.Bytes()
}

func parseProbeEvidence(t *testing.T, data []byte) ble.EvidenceInput {
	t.Helper()

	evidence, err := ble.ParseEvidence(data)
	require.NoError(t, err)
	return evidence
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "..", "testdata", "device", "fixtures", name)
}

func writeEvidenceFixture(t *testing.T, evidence ble.EvidenceInput) string {
	t.Helper()

	data, err := FormatEvidence(evidence)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "evidence.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}
