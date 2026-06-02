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
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "root.go"))

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "device.Transport")

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
