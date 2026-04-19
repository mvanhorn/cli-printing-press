package authdoctor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTable(&buf, nil); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No printed CLIs") {
		t.Errorf("want empty message, got %q", out)
	}
}

func TestRenderTableRows(t *testing.T) {
	findings := []Finding{
		{API: "dub", Type: "bearer_token", EnvVar: "DUB_TOKEN", Status: StatusSuspicious, Fingerprint: "abc", Reason: "too short"},
		{API: "hubspot", Type: "api_key", EnvVar: "HUBSPOT_ACCESS_TOKEN", Status: StatusOK, Fingerprint: "pat-..."},
		{API: "hackernews", Type: "none", Status: StatusNoAuth},
	}
	var buf bytes.Buffer
	if err := RenderTable(&buf, findings); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	out := buf.String()

	// Header present
	if !strings.Contains(out, "API") || !strings.Contains(out, "Env Var") {
		t.Errorf("missing header in output:\n%s", out)
	}
	// Row data present
	if !strings.Contains(out, "dub") || !strings.Contains(out, "DUB_TOKEN") {
		t.Errorf("missing dub row:\n%s", out)
	}
	// Summary line present
	if !strings.Contains(out, "Summary:") {
		t.Errorf("missing summary line:\n%s", out)
	}
	if !strings.Contains(out, "1 ok") || !strings.Contains(out, "1 suspicious") || !strings.Contains(out, "1 no auth") {
		t.Errorf("summary counts wrong:\n%s", out)
	}
}

func TestRenderJSONShape(t *testing.T) {
	findings := []Finding{
		{API: "dub", Type: "bearer_token", EnvVar: "DUB_TOKEN", Status: StatusSuspicious, Fingerprint: "abc", Reason: "too short"},
		{API: "hackernews", Type: "none", Status: StatusNoAuth},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, findings); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var payload struct {
		Summary  Summary   `json:"summary"`
		Findings []Finding `json:"findings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("parse JSON output: %v\noutput: %s", err, buf.String())
	}
	if payload.Summary.Suspicious != 1 || payload.Summary.NoAuth != 1 {
		t.Errorf("summary wrong: %+v", payload.Summary)
	}
	if len(payload.Findings) != 2 {
		t.Errorf("findings count wrong: %d", len(payload.Findings))
	}
}
