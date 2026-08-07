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

func TestGeneratedBLEDeviceEscapesDisplayNameInRootCommand(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)
	ds.DisplayName = `BLE "Kitchen" Sensor`

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootSrc), `Short:        "Control BLE \"Kitchen\" Sensor over BLE"`)
	assert.Contains(t, string(rootSrc), `Long:         "Control BLE \"Kitchen\" Sensor over BLE using a generated device-native CLI surface."`)
	requireGeneratedCompiles(t, outputDir)
}

// TestGeneratedBLEDeviceEmitsPublishArtifacts verifies the device generator
// emits the four standard publish artifacts the public library's
// completeness verifier expects (AGENTS.md, LICENSE, NOTICE, .goreleaser.yaml).
// A device generate previously dropped all four — the "fork-and-drop" gap the
// shared version.go template fixed for the version command.
func TestGeneratedBLEDeviceEmitsPublishArtifacts(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "LICENSE", "NOTICE", ".goreleaser.yaml"} {
		assert.FileExists(t, filepath.Join(outputDir, name))
	}

	// Claude Code auto-loads CLAUDE.md, not AGENTS.md; the device variant emits a
	// CLAUDE.md that imports the contract.
	assert.Equal(t, "@AGENTS.md", strings.TrimSpace(readFileString(t, filepath.Join(outputDir, "CLAUDE.md"))))

	// None of the four may contain an unrendered Go-template directive. The
	// goreleaser file legitimately carries goreleaser's own `{{ .Version }}`
	// (note the leading space), so assert specifically on the Go-template form
	// `{{.` / `{{range`-style openers that would mean a field went unrendered.
	for _, name := range []string{"AGENTS.md", "LICENSE", "NOTICE", ".goreleaser.yaml"} {
		body := readFileString(t, filepath.Join(outputDir, name))
		assert.NotContains(t, body, "{{.", "%s contains an unrendered Go template directive", name)
		assert.NotEmpty(t, strings.TrimSpace(body), "%s is empty", name)
	}

	// LICENSE is the Apache-2.0 text with the device "and contributors" holder.
	license := readFileString(t, filepath.Join(outputDir, "LICENSE"))
	assert.Contains(t, license, "Apache License")
	assert.Contains(t, license, "contributors")

	// NOTICE attributes the printed CLI; the creator block is skipped (device
	// specs carry no creator handle), leaving the Press generation credit.
	notice := readFileString(t, filepath.Join(outputDir, "NOTICE"))
	assert.Contains(t, notice, naming.CLI(ds.Name))
	assert.Contains(t, notice, "CLI Printing Press")
	assert.NotContains(t, notice, "Created by", "device NOTICE has no creator handle, so the byline block is skipped")

	// .goreleaser.yaml builds both the CLI and the companion MCP binary, wires
	// the version ldflag to the module's internal/cli package, and carries a
	// non-empty homebrew description.
	goreleaser := readFileString(t, filepath.Join(outputDir, ".goreleaser.yaml"))
	assert.Contains(t, goreleaser, "project_name: "+naming.CLI(ds.Name))
	assert.Contains(t, goreleaser, "main: ./cmd/"+naming.CLI(ds.Name))
	assert.Contains(t, goreleaser, "main: ./cmd/"+naming.MCP(ds.Name))
	assert.Contains(t, goreleaser, naming.CLI(ds.Name)+"/internal/cli.version=")
	assert.Contains(t, goreleaser, "-X main.version={{ .Version }}")
	assert.Contains(t, goreleaser, `description: "`)

	// AGENTS.md is the device-aware variant: it uses BLE/replay concepts and the
	// codec/novelCommands customization model, never HTTP auth/sync/SQL.
	agents := readFileString(t, filepath.Join(outputDir, "AGENTS.md"))
	assert.Contains(t, agents, naming.CLI(ds.Name))
	assert.Contains(t, agents, "ble_live")
	assert.Contains(t, agents, "replay-backed by default")
	assert.Contains(t, agents, "DeviceCodec")
	assert.NotContains(t, agents, "agent-context", "device AGENTS.md must not reference the HTTP agent-context command")
	assert.NotContains(t, agents, "Self-Learning Loop", "device AGENTS.md must not reference the HTTP learn loop")
}

func TestGeneratedBLESkillEmitsCanonicalInstallSection(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	skillSrc, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)

	// Device CLIs carry no public-library category, so the canonical install block uses
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

	mcpMainSrc, err := os.ReadFile(filepath.Join(outputDir, "cmd", naming.MCP(ds.Name), "main.go"))
	require.NoError(t, err)
	mcpMain := string(mcpMainSrc)
	// The MCP server version must be an ldflag-overridable var, not a hardcoded
	// literal — the device .goreleaser injects -X main.version at release time, so
	// a bare "1.0.0" in NewMCPServer would make that injection a silent no-op.
	assert.Contains(t, mcpMain, `var version = "0.0.0-dev"`)
	assert.NotContains(t, mcpMain, "\n\t\t\"1.0.0\",\n\t\tserver.WithToolCapabilities(false),")

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
	assert.Contains(t, string(goMod), "\ngo "+currentGoDirectiveVersion()+"\n")

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
	// An additive hook registry lets independently preserved command files
	// coexist without overwriting each other's registration.
	assert.Contains(t, root, "var novelCommandHooks []func(root *cobra.Command, flags *rootFlags)")
	assert.Contains(t, root, "func registerNovelCommand(hook func(root *cobra.Command, flags *rootFlags))")
	assert.Contains(t, root, "novelCommandHooks = append(novelCommandHooks, hook)")
	assert.Contains(t, root, "for _, hook := range novelCommandHooks {")

	// The generated CLI compiles with the hook unset (no operator file present).
	requireGeneratedCompiles(t, outputDir)

	// Two independent operator files both register commands. This mirrors how
	// regenmerge preserves snapshot-only (NOVEL) files verbatim across regen.
	operatorFile := filepath.Join(outputDir, "internal", "cli", "novel_ping.go")
	require.NoError(t, os.WriteFile(operatorFile, []byte(`package cli

import "github.com/spf13/cobra"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		_ = flags
		root.AddCommand(&cobra.Command{Use: "ping", RunE: func(c *cobra.Command, a []string) error { return nil }})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_echo.go"), []byte(`package cli

import "github.com/spf13/cobra"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		_ = flags
		root.AddCommand(&cobra.Command{Use: "echo", RunE: func(c *cobra.Command, a []string) error { return nil }})
	})
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "novel_hooks_test.go"), []byte(`package cli

import "testing"

func TestNovelHooksCompose(t *testing.T) {
	root := RootCmd()
	for _, name := range []string{"ping", "echo"} {
		if cmd, _, err := root.Find([]string{name}); err != nil || cmd == root {
			t.Fatalf("registered command %q unavailable: %v", name, err)
		}
	}
}
`), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestNovelHooksCompose")
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
	assert.Contains(t, seam, "type Link interface")
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

// TestGeneratedBLETransportContractDrivesCodegen verifies the transport contract
// fields drive emitted code: command_spacing_ms emits a paced writer, write_mode
// flips the write preference, and the contract + quirks + workflows surface in
// doctor. A device with no transport block is unaffected (acknowledged-first, no
// pacing) and surfaces neither quirks nor workflows.
func TestGeneratedBLETransportContractDrivesCodegen(t *testing.T) {
	t.Parallel()

	gen := func(mut func(*devicespec.DeviceSpec)) (bleLive, root string) {
		ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-simple-actuator.yaml"))
		require.NoError(t, err)
		if mut != nil {
			mut(ds)
		}
		dir := filepath.Join(t.TempDir(), "dev")
		require.NoError(t, NewDevice(ds, dir).Generate())
		bl, err := os.ReadFile(filepath.Join(dir, "internal", "device", "ble_live.go"))
		require.NoError(t, err)
		rt, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		return string(bl), string(rt)
	}

	// Baseline: no transport contract -> acknowledged-first, no pacing, doctor
	// reports the effective contract but no quirks.
	plainLive, plainRoot := gen(nil)
	assert.NotContains(t, plainLive, "const commandSpacing")
	assert.NotContains(t, plainLive, "l.lastWrite")
	assert.Contains(t, plainLive, "if _, err := c.Write(payload); err == nil")
	assert.Contains(t, plainRoot, `"write_mode"`)
	assert.NotContains(t, plainRoot, `info["quirks"]`)
	assert.NotContains(t, plainRoot, `info["workflows"]`)

	// command_spacing_ms -> paced writer.
	pacedLive, _ := gen(func(ds *devicespec.DeviceSpec) { ds.Transport.CommandSpacingMS = 690 })
	assert.Contains(t, pacedLive, "const commandSpacing = 690 * time.Millisecond")
	assert.Contains(t, pacedLive, "time.Sleep(wait)")

	// write_mode: without-response -> without-response-first.
	woLive, _ := gen(func(ds *devicespec.DeviceSpec) { ds.Transport.WriteMode = devicespec.WriteModeWithoutResponse })
	assert.Contains(t, woLive, "if _, err := c.WriteWithoutResponse(payload); err == nil")

	// quirks -> doctor surfaces them for the operator/agent.
	_, quirkRoot := gen(func(ds *devicespec.DeviceSpec) {
		ds.Quirks = []devicespec.DeviceQuirk{{Category: devicespec.QuirkCategoryInit, Summary: "Dummy read before first write."}}
	})
	assert.Contains(t, quirkRoot, `info["quirks"]`)
	assert.Contains(t, quirkRoot, "Dummy read before first write.")

	// workflows -> doctor surfaces the proven spine (name, goal, ordered steps) so
	// the implemented control flow can be checked against it.
	_, wfRoot := gen(func(ds *devicespec.DeviceSpec) {
		ds.Workflows = []devicespec.DeviceWorkflow{{
			Name:  "start-walk",
			Goal:  "Hold the belt running at a set speed.",
			Steps: []string{"Subscribe to notify.", "Run the handshake.", "Wait for running, then set speed once."},
			Notes: "A speed sent before the running state is ignored.",
		}}
	})
	assert.Contains(t, wfRoot, `info["workflows"]`)
	assert.Contains(t, wfRoot, "start-walk")
	assert.Contains(t, wfRoot, "Wait for running, then set speed once.")
	assert.Contains(t, wfRoot, "A speed sent before the running state is ignored.")
	assert.Contains(t, wfRoot, "proven workflows:")
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
	assert.Contains(t, root, "func newScanCmd(")
	// Shared version command: device CLIs emit internal/cli/version.go and register
	// it, so they carry the version command + --version flag like HTTP CLIs (and
	// pass the version quality gate).
	assert.Contains(t, root, "rootCmd.AddCommand(newVersionCmd())")
	assert.Contains(t, root, "Version:      version,")
	assert.Contains(t, read(filepath.Join("internal", "cli", "version.go")), "func newVersionCmd()")

	// LiveTransport implements the Transport interface over the BLE seam.
	live := read(filepath.Join("internal", "device", "live.go"))
	assert.Contains(t, live, "type LiveTransport struct")
	assert.Contains(t, live, "func (t *LiveTransport) Status(")
	assert.Contains(t, live, "func (t *LiveTransport) ExecuteCommand(")
	assert.Contains(t, live, "bleBackendFactory()")
	// Exported connection API for hand-authored Tier-2 commands, plus the codec
	// decode hook for the generated status command.
	assert.Contains(t, live, "func Dial(ctx context.Context, address string, timeout time.Duration) (Link, error)")
	seam := read(filepath.Join("internal", "device", "ble.go"))
	assert.Contains(t, seam, "type DeviceCodec interface")
	assert.Contains(t, seam, "type Link interface")

	// Service UUIDs surfaced for discovery/connect.
	assert.Contains(t, read(filepath.Join("internal", "device", "spec.go")), "var ServiceUUIDs = []string{")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedBLEDeviceLiveTransportTestsPass(t *testing.T) {
	t.Parallel()

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	// Run the emitted device-package tests: the Tier-1 live path (write payload,
	// dry-run, scan) against the injected fake backend — no hardware, no tag.
	runGoCommandRequired(t, outputDir, "test", "./internal/device/")
}

func TestGeneratedBLEDeviceLiveBuildCompiles(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping ble_live compile in -short")
	}
	// The default build never compiles ble_live.go (the tinygo driver), so prove
	// it separately. On Linux the live backend is pure-Go (D-Bus) and compiles
	// toolchain-free in CI; macOS needs CGO/CoreBluetooth and Windows WinRT, so
	// gate the automated proof to Linux. Other platforms are covered manually.
	if runtime.GOOS != "linux" {
		t.Skipf("ble_live compile-proof runs on linux (pure-Go BLE backend); GOOS=%s", runtime.GOOS)
	}

	ds, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-minimal.yaml"))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), "ble-temperature-sensor")
	require.NoError(t, NewDevice(ds, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir) // tidy + default (stub) build
	runGoCommandRequired(t, outputDir, "build", "-tags", "ble_live", "./...")
}

func TestGeneratedBLEDeviceEmitsParameterizedCommand(t *testing.T) {
	t.Parallel()

	ds := &devicespec.DeviceSpec{
		Version:  1,
		Name:     "ble-param-device",
		Protocol: "ble",
		Session:  devicespec.SessionProfile{Mode: devicespec.SessionModeOneShot},
		BLE: devicespec.BLESurface{Services: []devicespec.BLEService{{
			UUID:            "0000fe00-0000-1000-8000-00805f9b34fb",
			Characteristics: []devicespec.BLECharacteristic{{UUID: "0000fe01-0000-1000-8000-00805f9b34fb", Properties: []string{"write"}}},
		}}},
		Capabilities: devicespec.DeviceCapabilities{Commands: []devicespec.DeviceCommand{{
			Name:               "set-level",
			CharacteristicUUID: "0000fe01-0000-1000-8000-00805f9b34fb",
			Safety:             devicespec.SafetyPhysicalEffect,
			ValidationStatus:   devicespec.ValidationStatusObserved,
			Payload:            devicespec.DevicePayload{Encoding: devicespec.PayloadEncodingHex, Bytes: []byte{0x01}},
			Parameters:         []devicespec.CommandParameter{{Name: "level", Type: devicespec.ParamTypeInt}},
		}}},
	}
	require.NoError(t, ds.Validate())

	outputDir := filepath.Join(t.TempDir(), "ble-param-device")
	require.NoError(t, NewDevice(ds, outputDir).Generate())

	// Parameter names flow into the emitted CommandDefinition; the generated
	// command builds its Use string and ExactArgs from them.
	assert.Contains(t, readFileString(t, filepath.Join(outputDir, "internal", "device", "spec.go")), `Parameters: []string{"level"}`)
	root := readFileString(t, filepath.Join(outputDir, "internal", "cli", "root.go"))
	assert.Contains(t, root, `use += " <" + param + ">"`)
	assert.Contains(t, root, "cobra.ExactArgs(len(definition.Parameters))")
	// The parameter names must reach the command at construction time: the
	// AddCommand call must pass Parameters, not just the CommandDefinitions var.
	assert.Contains(t, root, `Parameters: []string{"level"}`)

	requireGeneratedCompiles(t, outputDir)

	// End-to-end: the built command must accept its positional arg (replay mode),
	// not reject it with "accepts 0 arg(s)".
	bin := filepath.Join(outputDir, "ble-param-device-pp-cli")
	runGoCommandRequired(t, outputDir, "build", "-o", bin, "./cmd/ble-param-device-pp-cli")
	stdout, _ := runGeneratedBinary(t, bin, "set-level", "5", "--dry-run", "--json")
	assert.Contains(t, stdout, "set-level")
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
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

// TestGeneratedBLEHidesControlFromMCPWhenSessionRequired verifies that a
// held-connection device (session.mode == required) hides its mutating one-shot
// control commands from MCP — a one-shot tool cannot drive such a device — while
// a one-shot device keeps them exposed.
func TestGeneratedBLEHidesControlFromMCPWhenSessionRequired(t *testing.T) {
	t.Parallel()

	const hide = `command.Annotations = map[string]string{"mcp:hidden": "true"}`

	// session.mode == required: mutating control is held-connection-only.
	required, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-session-telemetry.yaml"))
	require.NoError(t, err)
	required.Session.Mode = devicespec.SessionModeRequired
	required.Session.OneShotFallback = false // only valid in "optional" mode
	reqDir := filepath.Join(t.TempDir(), "ble-required")
	require.NoError(t, NewDevice(required, reqDir).Generate())
	reqRoot, err := os.ReadFile(filepath.Join(reqDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	deviceCmd := generatedFunction(t, string(reqRoot), "newDeviceCommandCmd")
	assert.Contains(t, deviceCmd, hide, "required-session device should hide mutating control from MCP")
	assert.Contains(t, deviceCmd, "requiresPhysicalConfirmation(definition)", "hiding must be gated on the mutating-command predicate")
	requireGeneratedCompiles(t, reqDir)

	// session.mode == one-shot: control stays on MCP.
	oneShot, err := devicespec.Parse(filepath.Join("..", "..", "testdata", "device", "fixtures", "ble-simple-actuator.yaml"))
	require.NoError(t, err)
	osDir := filepath.Join(t.TempDir(), "ble-oneshot")
	require.NoError(t, NewDevice(oneShot, osDir).Generate())
	osRoot, err := os.ReadFile(filepath.Join(osDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.NotContains(t, generatedFunction(t, string(osRoot), "newDeviceCommandCmd"), hide,
		"one-shot device should keep control commands on MCP")
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
	// The command's Use string is built from its name plus any parameters.
	assert.Contains(t, string(rootSrc), `use := definition.Name`)
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
