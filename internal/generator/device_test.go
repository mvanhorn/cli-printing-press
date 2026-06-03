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
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
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
	root := string(rootSrc)
	assert.Contains(t, root, "device.Transport")
	assert.Contains(t, generatedFunction(t, root, "newCapabilitiesCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.Contains(t, generatedFunction(t, root, "newStatusCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)

	transportSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "transport.go"))
	require.NoError(t, err)
	assert.Contains(t, string(transportSrc), "cliutil.IsVerifyEnv()")
	assert.NotContains(t, string(transportSrc), `os.Getenv("PRINTING_PRESS_VERIFY")`)

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBLESkillEmitsCanonicalInstallSection(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	skillSrc, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)

	// Device CLIs carry no catalog category, so the canonical install block uses
	// the category-agnostic installer path. verify-skill's canonical-sections
	// check requires this exact block once the printed CLI has a manifest, so the
	// device SKILL template must emit it just like the HTTP skill.md.tmpl does.
	want := CanonicalSkillInstallSection(ds.Name, "")
	got, ok := ExtractSkillInstallSection(string(skillSrc))
	require.True(t, ok, "device SKILL.md must contain the canonical install section")
	assert.Equal(t, want, got, "device SKILL install section must match the canonical generator output")
}

func TestGeneratedBLEDeviceEmitsMCPSurface(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	// MCP entrypoint + the API-agnostic cobratree walker that mirrors the Cobra
	// tree (honoring mcp:read-only / mcp:hidden per command).
	assert.FileExists(t, filepath.Join(outputDir, "cmd", naming.MCP(ds.Name), "main.go"))
	assert.FileExists(t, filepath.Join(outputDir, "internal", "mcp", "cobratree", "walker.go"))

	toolsSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	tools := string(toolsSrc)
	assert.Contains(t, tools, "cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath)")
	assert.Contains(t, tools, `mcplib.NewTool("context"`)
	assert.Contains(t, tools, "device.Capabilities()")
	// The device MCP binary has no typed HTTP endpoint tools — it must not import
	// an HTTP client/config/store the device CLI does not have.
	assert.NotContains(t, tools, "internal/client")
	assert.NotContains(t, tools, "internal/store")

	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(goMod), "github.com/mark3labs/mcp-go")

	requireGeneratedCompiles(t, outputDir) // builds ./... including the MCP binary
}

func TestGeneratedBLEEmitsNovelCommandHook(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootSrc)
	// A nil-guarded function-variable hook: hand-authored commands attach via an
	// operator-owned file that sets novelCommands, with no edit to generated
	// files. The default build is a no-op (nil hook).
	assert.Contains(t, root, "var novelCommands func(root *cobra.Command, flags *rootFlags)")
	assert.Contains(t, root, "if novelCommands != nil {")
	assert.Contains(t, root, "novelCommands(rootCmd, flags)")

	// The generated CLI compiles with the hook unset (no operator file present).
	requireGeneratedCompiles(t, outputDir)

	// An operator file that wires the hook builds and adds a command. This mirrors
	// how regenmerge preserves snapshot-only (NOVEL) files verbatim across regen.
	operatorFile := filepath.Join(outputDir, "internal", "cli", "novel_ops.go")
	require.NoError(t, os.WriteFile(operatorFile, []byte(`package cli

import "github.com/spf13/cobra"

func init() {
	novelCommands = func(root *cobra.Command, flags *rootFlags) {
		_ = flags
		root.AddCommand(&cobra.Command{Use: "ping", RunE: func(c *cobra.Command, a []string) error { return nil }})
	}
}
`), 0o644))
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBLEDeviceEmitsLiveBackendSeam(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(outputDir, rel))
		require.NoError(t, err)
		return string(b)
	}

	// Device-neutral seam, always compiled.
	seam := read(filepath.Join("internal", "device", "ble.go"))
	assert.Contains(t, seam, "type bleBackend interface")
	assert.Contains(t, seam, "type bleLink interface")
	assert.Contains(t, seam, "func LiveAvailable() bool")
	assert.Contains(t, seam, "Write(characteristicUUID string, payload []byte) error")
	assert.NotContains(t, seam, "tinygo.org/x/bluetooth") // the seam itself stays BLE-library-free

	// tinygo live driver behind the ble_live build tag (CGO).
	live := read(filepath.Join("internal", "device", "ble_live.go"))
	assert.Contains(t, live, "//go:build ble_live")
	assert.Contains(t, live, "tinygo.org/x/bluetooth")
	assert.Contains(t, live, "const liveCompiled = true")

	// Pure-Go stub for the default build (no BLE stack, no CGO).
	stub := read(filepath.Join("internal", "device", "ble_stub.go"))
	assert.Contains(t, stub, "//go:build !ble_live")
	assert.Contains(t, stub, "const liveCompiled = false")
	assert.Contains(t, stub, "return nil, ErrLiveUnavailable")

	// tinygo is required in go.mod (retained by go mod tidy via the tag-gated
	// import) so -tags ble_live resolves.
	assert.Contains(t, read("go.mod"), "tinygo.org/x/bluetooth")

	// Default build (no tag) compiles with no BLE stack linked.
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBLEDeviceEmitsLiveTransportAndDoctor(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(outputDir, rel))
		require.NoError(t, err)
		return string(b)
	}

	// --live/--address/--timeout flags + run-time transport selection (selection
	// can't happen at construction time because persistent flags are unparsed).
	root := read(filepath.Join("internal", "cli", "root.go"))
	assert.Contains(t, root, `"live"`)
	assert.Contains(t, root, `"address"`)
	assert.Contains(t, root, `"timeout"`)
	assert.Contains(t, root, "func deviceTransport(flags *rootFlags) device.Transport")
	assert.Contains(t, root, "device.NewLiveTransport(flags.address, flags.timeout)")
	assert.Contains(t, root, "func newDoctorCmd(")

	// LiveTransport implements the Transport interface over the BLE seam.
	live := read(filepath.Join("internal", "device", "live.go"))
	assert.Contains(t, live, "type LiveTransport struct")
	assert.Contains(t, live, "func (t *LiveTransport) Status(")
	assert.Contains(t, live, "func (t *LiveTransport) ExecuteCommand(")
	assert.Contains(t, live, "newBLEBackend()")

	// Service UUIDs surfaced for discovery/connect.
	assert.Contains(t, read(filepath.Join("internal", "device", "spec.go")), "var ServiceUUIDs = []string{")

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

func TestGeneratedBLELiveCommandShortCircuitsUnderVerifyEnv(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-session-appliance")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	// --live under verify must NOT dial: LiveTransport short-circuits before it
	// touches the BLE backend (not even compiled in the default build), so the
	// floor catches an actuation the verifier's classifier might miss.
	cmd := exec.Command("go", "run", "-mod=mod", "./cmd/ble-session-appliance-pp-cli", "start", "--live", "--json")
	cmd.Dir = outputDir
	cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var result map[string]any
	require.NoError(t, json.Unmarshal(output, &result))
	assert.Equal(t, true, result["verify_noop"])
	assert.Equal(t, "verify-live-noop", result["transport"])
}

func TestGeneratedBLEPhysicalCommandRequiresConfirmation(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-session-appliance")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	blocked := exec.Command("go", "run", "-mod=mod", "./cmd/ble-session-appliance-pp-cli", "start", "--json")
	blocked.Dir = outputDir
	blockedOutput, err := blocked.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(blockedOutput), "has safety class physical-effect")
	assert.Contains(t, string(blockedOutput), "--confirm-physical-effect")

	confirmed := exec.Command("go", "run", "-mod=mod", "./cmd/ble-session-appliance-pp-cli", "start", "--json", "--confirm-physical-effect")
	confirmed.Dir = outputDir
	confirmedOutput, err := confirmed.CombinedOutput()
	require.NoError(t, err, string(confirmedOutput))
	var result map[string]any
	require.NoError(t, json.Unmarshal(confirmedOutput, &result))
	assert.Equal(t, "start", result["command"])
	assert.Equal(t, "physical-effect", result["safety"])
	assert.Equal(t, false, result["dry_run"])
	assert.Equal(t, "replay", result["transport"])
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
	assert.Contains(t, root, "telemetryCmd.AddCommand(newTelemetrySessionsCmd")
	assert.Contains(t, root, "captureSessionSummary(flags, status)")
	assert.Contains(t, root, `PayloadHex: "a001"`)
	assert.Contains(t, root, `device.CommandDefinition{Name: "start"`)
	assert.Contains(t, root, `"confirm-physical-effect"`)
	assert.Contains(t, root, `func requiresPhysicalConfirmation(definition device.CommandDefinition) bool`)
	assert.Contains(t, generatedFunction(t, root, "newCapabilitiesCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.Contains(t, generatedFunction(t, root, "newStatusCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.Contains(t, generatedFunction(t, root, "newTelemetryLatestCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.Contains(t, generatedFunction(t, root, "newTelemetrySessionsCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.Contains(t, generatedFunction(t, root, "newSessionStatusCmd"), `Annotations: map[string]string{"mcp:read-only": "true"}`)
	assert.NotContains(t, generatedFunction(t, root, "newDeviceCommandCmd"), `"mcp:read-only"`)
	assert.NotContains(t, generatedFunction(t, root, "newTelemetryCaptureCmd"), `"mcp:read-only"`)
	assert.NotContains(t, generatedFunction(t, root, "newSessionStartCmd"), `"mcp:read-only"`)
	assert.NotContains(t, generatedFunction(t, root, "newSessionStopCmd"), `"mcp:read-only"`)

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
	assert.Contains(t, store, `type SessionSummary struct`)
	assert.Contains(t, store, `func (s *TelemetryStore) CaptureStatus(snapshot StatusSnapshot)`)
	assert.Contains(t, store, `func (s *TelemetryStore) CaptureSession(status SessionStatus)`)
	assert.Contains(t, store, `func (s *TelemetryStore) SessionSummaries()`)
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

	skillSrc, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skillSrc), "ble-session-appliance-pp-cli start --dry-run --json")
	assert.Contains(t, string(skillSrc), "--confirm-physical-effect")

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
	sessionSummaryPath := filepath.Join(filepath.Dir(runtimeDir), "session-summaries.jsonl")
	assert.FileExists(t, sessionSummaryPath)
	assertSessionFileMode(t, runtimeDir, 0o700)
	assertSessionFileMode(t, filepath.Join(runtimeDir, "session.lock"), 0o600)
	assertSessionFileMode(t, filepath.Join(runtimeDir, "capability.token"), 0o600)
	assertSessionFileMode(t, filepath.Join(runtimeDir, "state.json"), 0o600)
	assertSessionFileMode(t, sessionSummaryPath, 0o600)

	secondStart := runGeneratedSessionCommand(t, outputDir, homeDir, "start")
	assert.Equal(t, "running", secondStart["state"])
	assert.Equal(t, true, secondStart["token_present"])

	stopped := runGeneratedSessionCommand(t, outputDir, homeDir, "stop")
	assert.Equal(t, "stopped", stopped["state"])
	assert.Equal(t, false, stopped["token_present"])
	assert.NoFileExists(t, filepath.Join(runtimeDir, "session.lock"))
	assert.NoFileExists(t, filepath.Join(runtimeDir, "capability.token"))
	assert.NoFileExists(t, filepath.Join(runtimeDir, "state.json"))

	summaries := runGeneratedTelemetrySessionsCommand(t, outputDir, homeDir)
	require.Len(t, summaries, 3)
	assert.Equal(t, "running", summaries[0]["state"])
	assert.Equal(t, "running", summaries[1]["state"])
	assert.Equal(t, "stopped", summaries[2]["state"])
	wantEndpointKind := "unix-socket"
	if runtime.GOOS == "windows" {
		wantEndpointKind = "windows-named-pipe"
	}
	assert.Equal(t, wantEndpointKind, summaries[0]["endpoint_kind"])
}

func runGeneratedSessionCommand(t *testing.T, outputDir, homeDir, action string) map[string]any {
	t.Helper()

	result, ok := runGeneratedJSONCommand(t, outputDir, homeDir, "session", action).(map[string]any)
	require.True(t, ok)
	return result
}

func runGeneratedTelemetrySessionsCommand(t *testing.T, outputDir, homeDir string) []map[string]any {
	t.Helper()

	raw, ok := runGeneratedJSONCommand(t, outputDir, homeDir, "telemetry", "sessions").([]any)
	require.True(t, ok)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		summary, ok := item.(map[string]any)
		require.True(t, ok)
		out = append(out, summary)
	}
	return out
}

func runGeneratedJSONCommand(t *testing.T, outputDir, homeDir string, args ...string) any {
	t.Helper()

	cmdArgs := append([]string{"run", "-mod=mod", "./cmd/ble-session-appliance-pp-cli", "--json"}, args...)
	cmd := exec.Command("go", cmdArgs...)
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
	var result any
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
	assert.NotContains(t, generatedFunction(t, string(rootSrc), "newDeviceCommandCmd"), `"mcp:read-only"`)

	specSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "device", "spec.go"))
	require.NoError(t, err)
	assert.Contains(t, string(specSrc), `Name: "toggle"`)
	assert.Contains(t, string(specSrc), `Safety: "low-risk-write"`)
	assert.Contains(t, string(specSrc), `Callable: true`)

	requireGeneratedCompiles(t, outputDir)
}

func generatedFunction(t *testing.T, src, name string) string {
	t.Helper()

	start := strings.Index(src, "func "+name+"(")
	require.NotEqual(t, -1, start, "function %s not found", name)
	rest := src[start:]
	next := strings.Index(rest[len("func "):], "\nfunc ")
	if next == -1 {
		return rest
	}
	return rest[:len("func ")+next]
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

	skillSrc, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	skill := string(skillSrc)
	assert.Contains(t, skill, "capabilities --json")
	assert.NotContains(t, skill, "--dry-run --json", "withheld commands must not advertise a replay preview in SKILL.md")
	assert.NotContains(t, skill, "--confirm-physical-effect", "withheld commands must not advertise the confirmation flag in SKILL.md")

	requireGeneratedCompiles(t, outputDir)
}
