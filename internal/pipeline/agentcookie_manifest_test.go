package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestWriteAgentcookieManifest_BearerToken(t *testing.T) {
	dir := t.TempDir()
	p := GenerateManifestParams{
		APIName:     "stripe",
		OutputDir:   dir,
		DisplayName: "Stripe",
		Description: "Payment processing and financial infrastructure API",
		Spec: &spec.APISpec{
			Auth: spec.AuthConfig{
				Type:    "bearer_token",
				EnvVars: []string{"STRIPE_SECRET_KEY"},
			},
		},
	}
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("WriteAgentcookieManifest: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, AgentcookieManifestFilename))
	if err != nil {
		t.Fatalf("reading emitted manifest: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		`schema_version = 2`,
		`name = "stripe-pp-cli"`,
		`display_name = "Stripe"`,
		`project_kind = "cli"`,
		`[secrets.file]`,
		`path = "~/.config/stripe-pp-cli/config.toml"`,
		`[sync]`,
		`default = false`,
		`[sync.keys]`,
		`STRIPE_SECRET_KEY = true`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected manifest to contain %q; got:\n%s", want, s)
		}
	}
}

func TestWriteAgentcookieManifest_EnvVarSpecsRespectsSensitivity(t *testing.T) {
	dir := t.TempDir()
	p := GenerateManifestParams{
		APIName:   "example",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Auth: spec.AuthConfig{
				Type: "oauth2",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "EXAMPLE_CLIENT_ID", Sensitive: false},
					{Name: "EXAMPLE_CLIENT_SECRET", Sensitive: true},
				},
			},
		},
	}
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("WriteAgentcookieManifest: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, AgentcookieManifestFilename))
	s := string(body)
	if !strings.Contains(s, "EXAMPLE_CLIENT_ID = false") {
		t.Errorf("expected non-sensitive client_id; got:\n%s", s)
	}
	if !strings.Contains(s, "EXAMPLE_CLIENT_SECRET = true") {
		t.Errorf("expected sensitive client_secret; got:\n%s", s)
	}
}

func TestWriteAgentcookieManifest_SkipsCookieOnly(t *testing.T) {
	dir := t.TempDir()
	p := GenerateManifestParams{
		APIName:   "instacart",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Auth: spec.AuthConfig{
				Type: "cookie",
			},
		},
	}
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("WriteAgentcookieManifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentcookieManifestFilename)); err == nil {
		t.Error("expected no manifest for cookie-only CLI; file was created")
	}
}

func TestWriteAgentcookieManifest_SkipsNilSpec(t *testing.T) {
	dir := t.TempDir()
	p := GenerateManifestParams{
		APIName:   "no-spec",
		OutputDir: dir,
	}
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("WriteAgentcookieManifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentcookieManifestFilename)); err == nil {
		t.Error("expected no manifest when spec is nil")
	}
}

func TestWriteAgentcookieManifest_RespectsOverrideMarker(t *testing.T) {
	dir := t.TempDir()
	override := "# agentcookie-manual-override\nschema_version = 2\nname = \"example-pp-cli\"\n"
	if err := os.WriteFile(filepath.Join(dir, AgentcookieManifestFilename), []byte(override), 0o644); err != nil {
		t.Fatalf("seeding override: %v", err)
	}
	p := GenerateManifestParams{
		APIName:   "example",
		OutputDir: dir,
		Spec: &spec.APISpec{
			Auth: spec.AuthConfig{
				Type:    "bearer_token",
				EnvVars: []string{"EXAMPLE_TOKEN"},
			},
		},
	}
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("WriteAgentcookieManifest: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, AgentcookieManifestFilename))
	if !strings.HasPrefix(string(body), AgentcookieOverrideMarker) {
		t.Error("override marker file was overwritten")
	}
	if strings.Contains(string(body), "EXAMPLE_TOKEN") {
		t.Error("generated content leaked into overridden file")
	}
}

func TestWriteAgentcookieManifest_Idempotent(t *testing.T) {
	dir := t.TempDir()
	p := GenerateManifestParams{
		APIName:     "stripe",
		OutputDir:   dir,
		DisplayName: "Stripe",
		Spec: &spec.APISpec{
			Auth: spec.AuthConfig{
				Type:    "bearer_token",
				EnvVars: []string{"STRIPE_SECRET_KEY"},
			},
		},
	}
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, AgentcookieManifestFilename))
	if err := WriteAgentcookieManifest(p); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, AgentcookieManifestFilename))
	if string(first) != string(second) {
		t.Errorf("second emit produced different bytes:\n--- first ---\n%s\n--- second ---\n%s", string(first), string(second))
	}
}
