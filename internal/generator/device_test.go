package generator

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cliutil", "verifyenv.go"))
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "session.go"))
	assert.NoFileExists(t, filepath.Join(outputDir, "internal", "device", "store.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "cli", "root.go"))

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), "device.Transport")

	transportSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "transport.go"))
	require.NoError(t, err)
	assert.Contains(t, string(transportSrc), "cliutil.IsVerifyEnv()")
	assert.NotContains(t, string(transportSrc), `os.Getenv("PRINTING_PRESS_VERIFY")`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBLECommandShortCircuitsUnderVerifyEnv(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-session-appliance")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	cmd := exec.Command("go", "run", "-mod=mod", "./cmd/ble-session-appliance-pp-cli", "start", "--json")
	cmd.Dir = outputDir
	cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var result map[string]any
	require.NoError(t, json.Unmarshal(output, &result))
	assert.Equal(t, "start", result["command"])
	assert.Equal(t, "physical-effect", result["safety"])
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, true, result["verify_noop"])
	assert.Equal(t, "verify_short_circuit", result["reason"])
	assert.Equal(t, "verify-replay", result["transport"])
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
	assert.Contains(t, root, "rootCmd.AddCommand(newCapabilitiesCmd")
	assert.Contains(t, root, "rootCmd.AddCommand(newSessionCmd")
	assert.Contains(t, root, "rootCmd.AddCommand(newTelemetryCmd")
	assert.Contains(t, root, "device.NewReplaySession()")
	assert.Contains(t, root, `PayloadHex: "a001"`)
	assert.Contains(t, root, `device.CommandDefinition{Name: "start"`)

	sessionSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "session.go"))
	require.NoError(t, err)
	session := string(sessionSrc)
	assert.Contains(t, session, `type SessionEndpoint struct`)
	assert.Contains(t, session, `session.lock`)
	assert.Contains(t, session, `capability.token`)
	assert.Contains(t, session, `state.json`)
	assert.Contains(t, session, `windows-named-pipe`)
	assert.Contains(t, session, `unix-socket`)
	assert.Contains(t, session, `existing replay session lock is active`)

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
	assert.Contains(t, spec, `Name: "start"`)
	assert.Contains(t, spec, `Safety: "physical-effect"`)
	assert.Contains(t, spec, `EvidenceRefs: []string{"write-start", "notify-running"`)
	assert.Contains(t, spec, `Callable: true`)
	assert.Contains(t, spec, `WithheldReason: ""`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBLESessionRuntimeTracksLockAndToken(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-session-appliance")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	homeDir := t.TempDir()
	status := runGeneratedSessionCommand(t, outputDir, homeDir, "status")
	assert.Equal(t, "not-running", status["state"])
	assert.Equal(t, false, status["token_present"])

	started := runGeneratedSessionCommand(t, outputDir, homeDir, "start")
	assert.Equal(t, "running", started["state"])
	assert.Equal(t, true, started["token_present"])
	assert.NotEmpty(t, started["runtime_dir"])
	endpoint, ok := started["endpoint"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, endpoint["kind"])
	assert.NotEmpty(t, endpoint["path"])

	runtimeDir, ok := started["runtime_dir"].(string)
	require.True(t, ok)
	assert.FileExists(t, filepath.Join(runtimeDir, "session.lock"))
	assert.FileExists(t, filepath.Join(runtimeDir, "capability.token"))
	assert.FileExists(t, filepath.Join(runtimeDir, "state.json"))
	assertSessionFileMode(t, runtimeDir, 0o700)
	assertSessionFileMode(t, filepath.Join(runtimeDir, "session.lock"), 0o600)
	assertSessionFileMode(t, filepath.Join(runtimeDir, "capability.token"), 0o600)
	assertSessionFileMode(t, filepath.Join(runtimeDir, "state.json"), 0o600)

	secondStart := runGeneratedSessionCommand(t, outputDir, homeDir, "start")
	assert.Equal(t, "running", secondStart["state"])
	assert.Equal(t, true, secondStart["token_present"])

	stopped := runGeneratedSessionCommand(t, outputDir, homeDir, "stop")
	assert.Equal(t, "stopped", stopped["state"])
	assert.Equal(t, false, stopped["token_present"])
	assert.NoFileExists(t, filepath.Join(runtimeDir, "session.lock"))
	assert.NoFileExists(t, filepath.Join(runtimeDir, "capability.token"))
	assert.NoFileExists(t, filepath.Join(runtimeDir, "state.json"))
}

func runGeneratedSessionCommand(t *testing.T, outputDir, homeDir, action string) map[string]any {
	t.Helper()

	cmd := exec.Command("go", "run", "-mod=mod", "./cmd/ble-session-appliance-pp-cli", "--json", "session", action)
	cmd.Dir = outputDir
	cacheDir, err := goBuildCacheDir(outputDir)
	require.NoError(t, err)
	modCacheDir := os.Getenv("GOMODCACHE")
	if modCacheDir == "" {
		output, err := exec.Command("go", "env", "GOMODCACHE").Output()
		require.NoError(t, err)
		modCacheDir = strings.TrimSpace(string(output))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		"GOCACHE="+cacheDir,
		"GOMODCACHE="+modCacheDir,
		"HOME="+homeDir,
		"XDG_CACHE_HOME="+filepath.Join(homeDir, ".cache"),
	)
	output, err := cmd.Output()
	require.NoError(t, err, stderr.String())
	var result map[string]any
	require.NoError(t, json.Unmarshal(output, &result))
	return result
}

func assertSessionFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm())
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
	assert.Contains(t, string(rootSrc), "newCapabilitiesCmd")
	assert.Contains(t, string(rootSrc), `PayloadHex: "01"`)

	specSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "spec.go"))
	require.NoError(t, err)
	assert.Contains(t, string(specSrc), `Name: "toggle"`)
	assert.Contains(t, string(specSrc), `Safety: "low-risk-write"`)
	assert.Contains(t, string(specSrc), `Callable: true`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateUnknownBLECommandAsMetadataOnly(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-opaque-binary.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-opaque-binary")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootSrc)
	assert.Contains(t, root, "newCapabilitiesCmd")
	assert.NotContains(t, root, `device.CommandDefinition{Name: "vendor-action"`)
	assert.NotContains(t, root, `PayloadHex: "f7a50100fd"`)

	specSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "spec.go"))
	require.NoError(t, err)
	spec := string(specSrc)
	assert.Contains(t, spec, `Name: "vendor-action"`)
	assert.Contains(t, spec, `Safety: "unknown"`)
	assert.Contains(t, spec, `ValidationStatus: "inferred"`)
	assert.Contains(t, spec, `Callable: false`)
	assert.Contains(t, spec, `WithheldReason: "withheld: command is not observed or replay-validated"`)

	requireGeneratedCompiles(t, outputDir)
}
