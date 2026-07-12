package ble

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeEvidenceCorrelatesActionWriteAndNotification(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-events.json")
	result, err := AnalyzeEvidence(input)
	require.NoError(t, err)

	require.Len(t, result.Spec.Capabilities.Commands, 1)
	cmd := result.Spec.Capabilities.Commands[0]
	assert.Equal(t, "start", cmd.Name)
	assert.Equal(t, "fd01", cmd.CharacteristicUUID)
	assert.Equal(t, devicespec.SafetyPhysicalEffect, cmd.Safety)
	assert.Equal(t, devicespec.ValidationStatusObserved, cmd.ValidationStatus)
	assert.Equal(t, []byte{0xa0, 0x01}, cmd.Payload.Bytes)
	assert.Equal(t, []string{"action-start", "write-start", "notify-running"}, cmd.EvidenceRefs)

	assert.Contains(t, result.Report.CommandCandidates[0].Summary, "action-start")
	assert.Equal(t, CommandDiscoveryGuidedCandidates, result.Report.CommandDiscovery.Status)
	assert.Equal(t, []string{GuidanceSourceActionJournal}, result.Report.CommandDiscovery.GuidanceSources)
	assert.True(t, result.Report.CommandDiscovery.ReplayValidationNext)
	assert.Empty(t, result.Report.Ambiguities)
}

func TestAnalyzeEvidenceDoesNotInferCommandsFromSurfaceOnlyDiscovery(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "unguided-device",
		Events: []Event{
			{ID: "adv", Type: EventAdvertisement, DeviceAddress: "device-1", DeviceName: "Unguided Device"},
			{ID: "svc-control", Type: EventServiceDiscovery, ServiceUUID: "fd00", CharacteristicUUID: "fd01", Properties: []string{"write"}},
			{ID: "svc-status", Type: EventServiceDiscovery, ServiceUUID: "fd00", CharacteristicUUID: "fd02", Properties: []string{"notify"}},
			{ID: "notify-1", Type: EventNotification, ServiceUUID: "fd00", CharacteristicUUID: "fd02", ValueHex: "01"},
			{ID: "notify-2", Type: EventNotification, ServiceUUID: "fd00", CharacteristicUUID: "fd02", ValueHex: "02"},
		},
	})
	require.NoError(t, err)

	assert.Empty(t, result.Spec.Capabilities.Commands)
	require.Len(t, result.Spec.Capabilities.Telemetry, 1)
	assert.Equal(t, CommandDiscoveryInsufficientGuidance, result.Report.CommandDiscovery.Status)
	assert.Equal(t, []string{"fd01"}, result.Report.CommandDiscovery.WritableTargets)
	assert.Empty(t, result.Report.CommandDiscovery.GuidanceSources)
	assert.Contains(t, result.Report.CommandDiscovery.Message, "does not advertise command semantics")
	assert.Contains(t, result.Report.Warnings, "writable BLE characteristics found, but no commands emitted without guidance or replay validation")
}

func TestAnalyzeEvidenceInfersTelemetryFromRepeatedNotifications(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-events.json")
	result, err := AnalyzeEvidence(input)
	require.NoError(t, err)

	require.Len(t, result.Spec.Capabilities.Telemetry, 1)
	field := result.Spec.Capabilities.Telemetry[0]
	assert.Equal(t, "fd02_notification", field.Name)
	assert.Equal(t, "fd02", field.SourceCharacteristicUUID)
	assert.Equal(t, "notification", field.SampleCadence)
	assert.True(t, field.Store)
	assert.Contains(t, result.Report.TelemetryCandidates[0].Summary, "fd02")
}

func TestAnalyzeEvidencePreservesAmbiguousWrites(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-ambiguous-scan.json")
	result, err := AnalyzeEvidence(input)
	require.NoError(t, err)

	assert.Empty(t, result.Spec.Capabilities.Commands)
	assert.Equal(t, CommandDiscoveryAmbiguousGuidance, result.Report.CommandDiscovery.Status)
	assert.Equal(t, []string{GuidanceSourceActionJournal}, result.Report.CommandDiscovery.GuidanceSources)
	require.Len(t, result.Report.Ambiguities, 1)
	assert.Contains(t, result.Report.Ambiguities[0], "action-toggle")
	assert.Contains(t, result.Report.Ambiguities[0], "write-a")
	assert.Contains(t, result.Report.Ambiguities[0], "write-b")
}

func TestAnalyzeEvidenceSkipsMalformedCorrelationTimestamps(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
			{ID: "write-bad-time", Type: EventWrite, At: "not-a-time", CharacteristicUUID: "aa01", ValueHex: "0101"},
			{ID: "write-good", Type: EventWrite, At: "2026-06-01T12:00:06Z", CharacteristicUUID: "aa01", ValueHex: "0202"},
		},
		Actions: []ActionMarker{
			{ID: "action-bad-time", Label: "bad time", At: "not-a-time", Safety: devicespec.SafetyLowRiskWrite},
			{ID: "action-good", Label: "power-on", At: "2026-06-01T12:00:05Z", Safety: devicespec.SafetyLowRiskWrite},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Spec.Capabilities.Commands, 1)
	assert.Equal(t, "power-on", result.Spec.Capabilities.Commands[0].Name)
	require.Len(t, result.Report.Ambiguities, 2)
	assert.Contains(t, result.Report.Ambiguities[0], `action-bad-time has invalid action timestamp "not-a-time"; skipped`)
	assert.Contains(t, result.Report.Ambiguities[1], `write-bad-time has invalid write timestamp "not-a-time"; skipped`)
}

func TestAnalyzeEvidenceSkipsMalformedWriteHex(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
			{ID: "write-good", Type: EventWrite, At: "2026-06-01T12:00:06Z", CharacteristicUUID: "aa01", ValueHex: "0101"},
			{ID: "write-bad-hex", Type: EventWrite, At: "2026-06-01T12:01:06Z", CharacteristicUUID: "aa01", ValueHex: "not-hex"},
		},
		Actions: []ActionMarker{
			{ID: "action-good", Label: "power-on", At: "2026-06-01T12:00:05Z", Safety: devicespec.SafetyLowRiskWrite},
			{ID: "action-bad-hex", Label: "power-off", At: "2026-06-01T12:01:05Z", Safety: devicespec.SafetyLowRiskWrite},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Spec.Capabilities.Commands, 1)
	assert.Equal(t, "power-on", result.Spec.Capabilities.Commands[0].Name)
	assert.Equal(t, []byte{0x01, 0x01}, result.Spec.Capabilities.Commands[0].Payload.Bytes)
	require.NotEmpty(t, result.Report.Ambiguities)
	assert.Contains(t, result.Report.Ambiguities[0], `write-bad-hex has invalid value_hex "not-hex"; skipped`)
}

func TestAnalyzeEvidenceUsesCommunityReferenceAsEvidence(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-community-reference.json")
	result, err := AnalyzeEvidence(input)
	require.NoError(t, err)

	require.Len(t, result.Spec.Capabilities.Commands, 1)
	cmd := result.Spec.Capabilities.Commands[0]
	assert.Equal(t, "vendor-action", cmd.Name)
	assert.Equal(t, devicespec.ValidationStatusInferred, cmd.ValidationStatus)
	assert.Equal(t, []string{"community-vendor-action"}, cmd.EvidenceRefs)
	assert.Equal(t, []byte{0xf7, 0xa5, 0x01, 0x00, 0xfd}, cmd.Payload.Bytes)
	assert.Equal(t, CommandDiscoveryGuidedCandidates, result.Report.CommandDiscovery.Status)
	assert.Equal(t, []string{GuidanceSourceCommunityReference}, result.Report.CommandDiscovery.GuidanceSources)
}

func TestAnalyzeEvidenceSkipsMalformedCommunityPayloadHex(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
		},
		CommunityReferences: []CommunityReference{
			{
				ID:                 "ref-good",
				CommandName:        "good-command",
				CharacteristicUUID: "aa01",
				PayloadHex:         "aabb",
				Safety:             devicespec.SafetyLowRiskWrite,
			},
			{
				ID:                 "ref-bad",
				CommandName:        "bad-command",
				CharacteristicUUID: "aa01",
				PayloadHex:         "not-hex",
				Safety:             devicespec.SafetyLowRiskWrite,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Spec.Capabilities.Commands, 1)
	assert.Equal(t, "good-command", result.Spec.Capabilities.Commands[0].Name)
	assert.Equal(t, []byte{0xaa, 0xbb}, result.Spec.Capabilities.Commands[0].Payload.Bytes)
	require.NotEmpty(t, result.Report.Ambiguities)
	assert.Contains(t, result.Report.Ambiguities[0], `ref-bad has invalid payload_hex "not-hex"; skipped`)
}

func TestAnalyzeEvidenceReportsAmbiguousDeviceDiscovery(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-ambiguous-scan.json")
	result, err := AnalyzeEvidence(input)
	require.NoError(t, err)

	require.Len(t, result.Report.DeviceCandidates, 2)
	assert.True(t, result.Report.RequiresOperatorSelection)
	assert.Contains(t, result.Report.Warnings, "multiple matching BLE devices; require explicit operator selection")
}

func TestRedactEvidencePseudonymizesStableIdentifiers(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-events.json")
	redacted := RedactEvidence(input)
	data, err := json.Marshal(redacted)
	require.NoError(t, err)
	text := string(data)

	assert.NotContains(t, text, "AA:BB:CC:DD:EE:01")
	assert.NotContains(t, text, "Trevin")
	assert.NotContains(t, text, "redaction_terms")
	assert.Contains(t, text, "device-1")
	assert.Contains(t, text, "fd01")
	assert.Contains(t, text, "a001")
}

func TestRedactEvidenceDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	input := readFixture(t, "ble-events.json")
	originalAddress := input.Events[0].DeviceAddress
	originalName := input.Events[0].DeviceName
	originalTerms := append([]string(nil), input.RedactionTerms...)

	redacted := RedactEvidence(input)

	assert.NotEqual(t, originalAddress, redacted.Events[0].DeviceAddress)
	assert.Equal(t, originalAddress, input.Events[0].DeviceAddress)
	assert.Equal(t, originalName, input.Events[0].DeviceName)
	assert.Equal(t, originalTerms, input.RedactionTerms)
}

func TestAnalyzeEvidenceDefaultsSafetyUnknownWhenOmitted(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
			{ID: "write-1", Type: EventWrite, At: "2026-06-01T12:00:06Z", CharacteristicUUID: "aa01", ValueHex: "0101"},
		},
		Actions: []ActionMarker{
			// Safety intentionally omitted.
			{ID: "action-1", Label: "power-on", At: "2026-06-01T12:00:05Z"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Spec.Capabilities.Commands, 1)
	assert.Equal(t, devicespec.SafetyUnknown, result.Spec.Capabilities.Commands[0].Safety)
}

func TestAnalyzeEvidenceFallsBackToIDSlugWhenLabelUnusable(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
			{ID: "write-1", Type: EventWrite, At: "2026-06-01T12:00:06Z", CharacteristicUUID: "aa01", ValueHex: "0101"},
		},
		Actions: []ActionMarker{
			// Label is all punctuation — Slug("!!!") == "".
			// ID "action-press" has a usable slug.
			{ID: "action-press", Label: "!!!", At: "2026-06-01T12:00:05Z", Safety: devicespec.SafetyLowRiskWrite},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Spec.Capabilities.Commands, 1)
	assert.Equal(t, "action-press", result.Spec.Capabilities.Commands[0].Name)
}

func TestAnalyzeEvidenceSkipsCommandWhenBothLabelAndIDUnusable(t *testing.T) {
	t.Parallel()

	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
			{ID: "write-1", Type: EventWrite, At: "2026-06-01T12:00:06Z", CharacteristicUUID: "aa01", ValueHex: "0101"},
		},
		Actions: []ActionMarker{
			// Both label and ID produce empty slugs.
			{ID: "!!!", Label: "🔥", At: "2026-06-01T12:00:05Z", Safety: devicespec.SafetyLowRiskWrite},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, result.Spec.Capabilities.Commands)
	require.NotEmpty(t, result.Report.Ambiguities)
	assert.Contains(t, result.Report.Ambiguities[0], "no usable command name")
}

func TestAnalyzeEvidenceDropsCommandWithUndiscoveredCharacteristic(t *testing.T) {
	t.Parallel()

	// The community reference points to "ee01" which has no corresponding
	// service_discovery event, so the characteristic is not in the BLE surface.
	result, err := AnalyzeEvidence(EvidenceInput{
		Name: "test-device",
		Events: []Event{
			// Only "aa01" is service-discovered, not "ee01".
			{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "aa00", CharacteristicUUID: "aa01", Properties: []string{"write"}},
		},
		CommunityReferences: []CommunityReference{
			{
				ID:                 "ref-1",
				CommandName:        "ghost-cmd",
				CharacteristicUUID: "ee01", // undiscovered
				PayloadHex:         "aabb",
				Safety:             devicespec.SafetyLowRiskWrite,
			},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, result.Spec.Capabilities.Commands)
	require.NotEmpty(t, result.Report.Ambiguities)
	assert.Contains(t, result.Report.Ambiguities[0], "ee01")
}

func readFixture(t *testing.T, name string) EvidenceInput {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "device", "fixtures", name))
	require.NoError(t, err)
	var input EvidenceInput
	require.NoError(t, json.Unmarshal(data, &input))
	return input
}
