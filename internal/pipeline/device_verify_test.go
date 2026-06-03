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
