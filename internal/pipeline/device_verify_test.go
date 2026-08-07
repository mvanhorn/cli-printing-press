package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func writeStubFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceTier2DenominatorMatchesTypeAndDeadCodeScale(t *testing.T) {
	sc := &Scorecard{
		UnscoredDimensions: []string{
			DimPathValidity,
			DimAuthProtocol,
			DimDataPipelineIntegrity,
			DimSyncCorrectness,
			DimLiveAPIVerification,
		},
	}

	max := scorecardTierMax(sc, 60, DimLiveAPIVerification, DimPathValidity, DimAuthProtocol, DimSyncCorrectness, DimDataPipelineIntegrity)
	if max != 10 {
		t.Fatalf("device tier-2 max = %d, want 10 for type fidelity + dead code", max)
	}
}

// writeDeviceSpecGo writes a spec.go the canonical device detector recognizes:
// it carries the `Protocol = "ble"` constant the device generator emits.
func writeDeviceSpecGo(t *testing.T, dir string) {
	writeStubFile(t, filepath.Join(dir, "internal", "device", "spec.go"), "package device\n\nconst Protocol = \"ble\"\n")
}

func writeClientPkgGo(t *testing.T, dir string) {
	writeStubFile(t, filepath.Join(dir, "internal", "client", "client.go"), "package client\n")
}

func TestIsDeviceCLIDir(t *testing.T) {
	t.Run("device CLI: device pkg, no client pkg", func(t *testing.T) {
		dir := t.TempDir()
		writeDeviceSpecGo(t, dir)
		if !isDeviceCLIDir(dir) {
			t.Error("expected device CLI dir to be detected")
		}
	})
	t.Run("HTTP CLI: has client pkg", func(t *testing.T) {
		dir := t.TempDir()
		writeClientPkgGo(t, dir)
		if isDeviceCLIDir(dir) {
			t.Error("HTTP CLI must not be detected as device")
		}
	})
	t.Run("both packages present -> not device-only", func(t *testing.T) {
		dir := t.TempDir()
		writeDeviceSpecGo(t, dir)
		writeClientPkgGo(t, dir)
		if isDeviceCLIDir(dir) {
			t.Error("a CLI with an HTTP client is not a device-only CLI")
		}
	})
	t.Run("neither -> not device", func(t *testing.T) {
		if isDeviceCLIDir(t.TempDir()) {
			t.Error("empty dir must not be detected as device")
		}
	})
}

func TestScorecardDeviceMarksHTTPDimsUnscored(t *testing.T) {
	dir := t.TempDir()
	writeStubFile(t, filepath.Join(dir, "internal", "device", "spec.go"), "package device\n\nconst Protocol = \"ble\"\n")
	writeStubFile(t, filepath.Join(dir, "internal", "cli", "root.go"), "package cli\n")

	sc := &Scorecard{}
	scoreInfrastructureDimensions(sc, dir, true)
	scoreDomainDimensions(sc, dir, nil, nil, true)

	for _, dim := range []string{
		DimLocalCache, DimVision, DimWorkflows, DimInsight, DimAgentWorkflow,
		DimDataPipelineIntegrity, DimSyncCorrectness,
	} {
		if !sc.IsDimensionUnscored(dim) {
			t.Errorf("device CLI should mark %q N/A (unscored), got scored", dim)
		}
	}
}

func TestScoreDoctorDeviceCreditsBLEReachability(t *testing.T) {
	dir := t.TempDir()
	writeStubFile(t, filepath.Join(dir, "internal", "cli", "live_read.go"), `package cli
// newDoctorCmd builds the "doctor" command.
// It calls wpble.New(addr).Scan(ctx) and sets info["reachable"].
// Reports verify_env, dogfood_env, live_compiled; uses flags.address and wpble.ServiceUUID.
`)
	if got := scoreDoctorDevice(dir); got < 8 {
		t.Errorf("scoreDoctorDevice = %d, want >= 8 for a full device doctor", got)
	}
	if got := scoreDoctorDevice(t.TempDir()); got != 0 {
		t.Errorf("scoreDoctorDevice(no doctor) = %d, want 0", got)
	}
}

func TestScoreErrorHandlingDeviceCreditsActionableErrors(t *testing.T) {
	content := []string{
		`return fmt.Errorf("speed %.1f out of range: %w", kmh, err)`,
		`"not contacting the belt; pass --live to scan"`,
		`weight must be greater than 0`,
	}
	if got := scoreErrorHandlingDevice(content); got < 5 {
		t.Errorf("scoreErrorHandlingDevice = %d, want >= 5 for actionable device errors", got)
	}
	if got := scoreErrorHandlingDevice([]string{"package cli"}); got != 0 {
		t.Errorf("scoreErrorHandlingDevice(no signals) = %d, want 0", got)
	}
}

// TestDeviceDogfoodVerdictSuppression proves the HTTP-API-only checks
// (client.go auth protocol, sync data pipeline, agent-context example
// discovery) do not fail a device CLI's dogfood verdict, while the same
// findings still fail an HTTP CLI.
func TestDeviceDogfoodVerdictSuppression(t *testing.T) {
	base := func(device bool) *DogfoodReport {
		return &DogfoodReport{
			IsDeviceCLI:   device,
			AuthCheck:     AuthCheckResult{Match: false}, // would FAIL an HTTP CLI
			PipelineCheck: PipelineResult{SyncCallsDomain: false},
			ExampleCheck:  ExampleCheckResult{Skipped: true},
		}
	}
	if v := deriveDogfoodVerdict(base(true), true); v != "PASS" {
		t.Errorf("device CLI verdict = %s, want PASS (HTTP-only checks suppressed)", v)
	}
	if v := deriveDogfoodVerdict(base(false), true); v != "FAIL" {
		t.Errorf("HTTP CLI verdict = %s, want FAIL (auth mismatch still fails)", v)
	}

	deviceIssues := collectDogfoodIssues(base(true), true)
	for _, issue := range deviceIssues {
		if issue == "auth protocol mismatch" || issue == "sync uses generic Upsert only" {
			t.Errorf("device CLI should not surface HTTP-only issue: %q", issue)
		}
	}
}

func TestDeviceScorersCreditDeviceSurface(t *testing.T) {
	dir := t.TempDir()
	writeStubFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
// flags "json" "agent" "dry-run"; per-command writeJSON( and cmd.OutOrStdout()
// Short: a  Short: b  Short: c  Short: d   command "capabilities"
// rootCmd.AddCommand( x4
func wire() {
	rootCmd.AddCommand(newCapabilitiesCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(newScanCmd())
}
`)
	writeStubFile(t, filepath.Join(dir, "internal", "mcp", "tools.go"), "package mcp\n")
	writeStubFile(t, filepath.Join(dir, "internal", "device", "spec.go"), `package device
type CommandDefinition struct{ Parameters []string }
type StatusField struct{ SourceCharacteristicUUID string }
type CapabilitySummary struct{}
var cmds = []CommandDefinition{{Parameters: []string{"kmh"}}}
var fields = []StatusField{{SourceCharacteristicUUID: "1809"}}
`)
	writeStubFile(t, filepath.Join(dir, "internal", "device", "transport.go"), "package device\ntype CommandResult struct{}\n")
	writeStubFile(t, filepath.Join(dir, "README.md"), `# Device CLI

This CLI is device-native, generated from a BLE device spec.

## Commands
- status, capabilities, doctor, scan

## Live control
Build with -tags ble_live and pass --live to control the device. Use capabilities to inspect the surface.

## MCP server
A stdio MCP server mirrors the commands as agent tools, with extra prose so the README clears the length floor comfortably.
`)

	checks := []struct {
		name string
		got  int
		min  int
	}{
		{"output_modes", scoreOutputModesDevice(dir), 8},
		{"terminal_ux", scoreTerminalUXDevice(dir), 5},
		{"readme", scoreREADMEDevice(dir), 8},
		{"agent_native", scoreAgentNativeDevice(dir), 8},
		{"breadth", scoreBreadthDevice(dir), 4},
		{"type_fidelity", scoreTypeFidelityDevice(dir), 5},
	}
	for _, c := range checks {
		if c.got < c.min {
			t.Errorf("%s device score = %d, want >= %d", c.name, c.got, c.min)
		}
	}
}
