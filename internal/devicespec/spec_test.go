package devicespec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseMinimalBLESpecDefaultsOneShot(t *testing.T) {
	t.Parallel()

	ds, err := Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	assert.Equal(t, 1, ds.Version)
	assert.Equal(t, "ble-temperature-sensor", ds.Name)
	assert.Equal(t, ProtocolBLE, ds.Protocol)
	assert.Equal(t, SessionModeOneShot, ds.Session.Mode)
	assert.Equal(t, MatchStrengthMedium, ds.Identity.MatchStrength)
	require.Len(t, ds.BLE.Services, 1)
	require.Len(t, ds.BLE.Services[0].Characteristics, 1)
	assert.Equal(t, "2a6e", ds.BLE.Services[0].Characteristics[0].UUID)

	require.Len(t, ds.Capabilities.Telemetry, 1)
	assert.Equal(t, "temperature", ds.Capabilities.Telemetry[0].Name)
	assert.Equal(t, "2a6e", ds.Capabilities.Telemetry[0].SourceCharacteristicUUID)
}

func TestLooksLikeDeviceSpecRequiresBLEProtocolAndSurface(t *testing.T) {
	t.Parallel()

	deviceData, err := os.ReadFile(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)
	assert.True(t, LooksLikeDeviceSpec(deviceData))

	assert.False(t, LooksLikeDeviceSpec([]byte(`
name: http-api
base_url: https://api.example.com
resources:
  things:
    endpoints: []
`)))
	assert.False(t, LooksLikeDeviceSpec([]byte(`
version: 1
name: empty-device
protocol: ble
ble:
  services: []
`)))
}

func TestParseSimpleActuatorNoSessionRequirement(t *testing.T) {
	t.Parallel()

	ds, err := Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-simple-actuator.yaml"))
	require.NoError(t, err)

	assert.Equal(t, SessionModeOneShot, ds.Session.Mode)
	require.Len(t, ds.Capabilities.Commands, 1)
	cmd := ds.Capabilities.Commands[0]
	assert.Equal(t, "toggle", cmd.Name)
	assert.Equal(t, SafetyLowRiskWrite, cmd.Safety)
	assert.Equal(t, ValidationStatusObserved, cmd.ValidationStatus)
	assert.Equal(t, "ff01", cmd.CharacteristicUUID)
	assert.Equal(t, []byte{0x01}, cmd.Payload.Bytes)
}

func TestParseSessionTelemetryPreservesEvidenceAndSafety(t *testing.T) {
	t.Parallel()

	ds, err := Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)

	assert.Equal(t, SessionModeOptional, ds.Session.Mode)
	assert.Equal(t, []string{SessionReasonLowLatencyControls, SessionReasonNotificationStream, SessionReasonTelemetrySampling, SessionReasonReconnect}, ds.Session.Reasons)
	assert.True(t, ds.Session.OneShotFallback)
	assert.True(t, ds.Session.Reconnect)
	assert.True(t, ds.Session.NotificationStream)
	require.Len(t, ds.Capabilities.Commands, 1)
	assert.Equal(t, []string{"write-start", "notify-running"}, ds.Capabilities.Commands[0].EvidenceRefs)
	assert.Equal(t, SafetyPhysicalEffect, ds.Capabilities.Commands[0].Safety)
	require.Len(t, ds.Capabilities.Telemetry, 1)
	assert.True(t, ds.Capabilities.Telemetry[0].Store)
}

func TestParseRequiredSessionDisallowsOneShotFallback(t *testing.T) {
	t.Parallel()

	ds, err := ParseBytes([]byte(`
version: 1
name: session-required-device
protocol: ble
ble:
  services:
    - uuid: "fd00"
      characteristics:
        - uuid: "fd01"
          properties: [write]
session:
  mode: required
  reasons: [unsafe_one_shot]
  one_shot_fallback: true
`))
	require.Error(t, err)
	require.Nil(t, ds)
	assert.Contains(t, err.Error(), `session.one_shot_fallback can only be true when session.mode is "optional"`)
}

func TestParseLegacyPersistentSessionNormalizesToOptional(t *testing.T) {
	t.Parallel()

	ds, err := ParseBytes([]byte(`
version: 1
name: legacy-session-device
protocol: ble
ble:
  services: []
session:
  mode: persistent
  reasons: [notification_stream]
`))
	require.NoError(t, err)
	assert.Equal(t, SessionModeOptional, ds.Session.Mode)
	assert.True(t, ds.Session.OneShotFallback)
}

func TestParseOpaqueBinaryPreservesUnknownPayloadFields(t *testing.T) {
	t.Parallel()

	ds, err := Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-opaque-binary.yaml"))
	require.NoError(t, err)

	require.Len(t, ds.Capabilities.Commands, 1)
	payload := ds.Capabilities.Commands[0].Payload
	assert.Equal(t, PayloadEncodingHex, payload.Encoding)
	assert.Equal(t, []byte{0xf7, 0xa5, 0x01, 0x00, 0xfd}, payload.Bytes)
	assert.Equal(t, []UnknownPayloadField{{Name: "counter", ByteRange: "2..3", Confidence: ConfidenceLow}}, payload.UnknownFields)
	assert.Equal(t, "sum bytes 1..3 modulo 256", payload.Checksum)
}

func TestValidateRejectsMissingCommandCharacteristic(t *testing.T) {
	t.Parallel()

	ds, err := ParseBytes([]byte(`
version: 1
name: invalid-missing-characteristic
protocol: ble
ble:
  services:
    - uuid: "180f"
      characteristics:
        - uuid: "2a19"
          properties: [read]
capabilities:
  commands:
    - name: write-missing
      characteristic_uuid: "ffff"
      safety: low-risk-write
      payload:
        encoding: hex
        bytes: "01"
`))
	require.Error(t, err)
	require.Nil(t, ds)
	assert.Contains(t, err.Error(), `commands[0] references missing characteristic "ffff"`)
}

func TestValidateRejectsPhysicalEffectWithoutSafety(t *testing.T) {
	t.Parallel()

	ds, err := ParseBytes([]byte(`
version: 1
name: invalid-missing-safety
protocol: ble
ble:
  services:
    - uuid: "ff00"
      characteristics:
        - uuid: "ff01"
          properties: [write]
capabilities:
  commands:
    - name: start-motion
      characteristic_uuid: "ff01"
      payload:
        encoding: hex
        bytes: "01"
`))
	require.Error(t, err)
	require.Nil(t, ds)
	assert.Contains(t, err.Error(), "commands[0].safety is required")
}

func TestParseRejectsUnsupportedVersionWithMigrationError(t *testing.T) {
	t.Parallel()

	_, err := ParseBytes([]byte(`
version: 99
name: future-device
protocol: ble
ble:
  services: []
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported device spec version 99")
	assert.Contains(t, err.Error(), "regenerate or migrate")
}

func TestDeviceSpecRoundTripYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	ds, err := ParseBytes(data)
	require.NoError(t, err)

	out, err := yaml.Marshal(ds)
	require.NoError(t, err)

	roundTrip, err := ParseBytes(out)
	require.NoError(t, err)
	assert.Equal(t, ds, roundTrip)
}
