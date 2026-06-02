package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMinimalBLEDeviceCLICompiles(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	assert.FileExists(t, filepath.Join(outputDir, "go.mod"))
	assert.FileExists(t, filepath.Join(outputDir, "cmd", "ble-temperature-sensor-pp-cli", "main.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "device", "transport.go"))
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "session.go"))
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "store.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "root.go"))

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "device.Transport")

	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateOptionalBLESessionScaffoldCompiles(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-session-appliance")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	assert.FileExists(t, filepath.Join(outputDir, "internal", "device", "session.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "device", "store.go"))

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootSrc)
	assert.Contains(t, root, "rootCmd.AddCommand(newSessionCmd")
	assert.Contains(t, root, "rootCmd.AddCommand(newTelemetryCmd")
	assert.Contains(t, root, "device.NewReplaySession()")
	assert.NotContains(t, root, `PayloadHex: "a001"`)
	assert.NotContains(t, root, `device.CommandDefinition{Name: "start"`)

	sessionSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "session.go"))
	require.NoError(t, err)
	session := string(sessionSrc)
	assert.Contains(t, session, `State:              state`)
	assert.Contains(t, session, `Detail:             "replay session scaffold only; live BLE IPC is not enabled in this generated CLI yet"`)

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "store.go"))
	require.NoError(t, err)
	store := string(storeSrc)
	assert.Contains(t, store, `type TelemetrySample struct`)
	assert.Contains(t, store, `func (s *TelemetryStore) CaptureStatus(snapshot StatusSnapshot)`)
	assert.Contains(t, store, `func (s *TelemetryStore) Latest()`)

	specSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "spec.go"))
	require.NoError(t, err)
	spec := string(specSrc)
	assert.Contains(t, spec, `SessionMode`)
	assert.Contains(t, spec, `= "optional"`)
	assert.Contains(t, spec, `SessionOneShotFallback`)
	assert.Contains(t, spec, `= true`)
	assert.Contains(t, spec, `"notification_stream"`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateLowRiskBLEDeviceCommandCompiles(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-simple-actuator.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-desk-lamp")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), `Use:   definition.Name`)
	assert.Contains(t, string(rootSrc), `PayloadHex: "01"`)

	specSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "spec.go"))
	require.NoError(t, err)
	assert.Contains(t, string(specSrc), `Name: "toggle"`)
	assert.Contains(t, string(specSrc), `Safety: "low-risk-write"`)

	requireGeneratedCompiles(t, outputDir)
}
