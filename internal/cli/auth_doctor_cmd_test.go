package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthDoctorCmdJSONFlag(t *testing.T) {
	cmd := newAuthDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Output must be parseable JSON and contain a summary key.
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out.String())
	}
	if _, ok := payload["summary"]; !ok {
		t.Errorf("JSON output missing 'summary' key: %s", out.String())
	}
	if _, ok := payload["findings"]; !ok {
		t.Errorf("JSON output missing 'findings' key: %s", out.String())
	}
}

func TestAuthDoctorCmdTableFallback(t *testing.T) {
	cmd := newAuthDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	output := out.String()
	// Either the empty-library message or a table with a summary line.
	// Both are acceptable — depends on whether the developer has
	// ~/printing-press/library/ populated locally.
	if !strings.Contains(output, "No printed CLIs") && !strings.Contains(output, "Summary:") {
		t.Errorf("expected either empty message or summary line; got:\n%s", output)
	}
}

func TestAuthCmdHasDoctorSubcommand(t *testing.T) {
	cmd := newAuthCmd()
	var found bool
	for _, sub := range cmd.Commands() {
		if sub.Name() == "doctor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("auth command should have a 'doctor' subcommand")
	}
}
