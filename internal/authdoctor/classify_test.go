package authdoctor

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
)

func envFrom(m map[string]string) getEnv {
	return func(k string) string {
		return m[k]
	}
}

func TestClassifyNilManifest(t *testing.T) {
	findings := Classify("hubspot", nil, envFrom(nil))
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].Status != StatusUnknown {
		t.Errorf("want StatusUnknown, got %q", findings[0].Status)
	}
}

func TestClassifyNoAuth(t *testing.T) {
	cases := []struct {
		name string
		auth pipeline.ManifestAuth
	}{
		{"type=none", pipeline.ManifestAuth{Type: "none"}},
		{"type empty", pipeline.ManifestAuth{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &pipeline.ToolsManifest{Auth: tc.auth}
			findings := Classify("hackernews", m, envFrom(nil))
			if len(findings) != 1 {
				t.Fatalf("want 1 finding, got %d", len(findings))
			}
			if findings[0].Status != StatusNoAuth {
				t.Errorf("want StatusNoAuth, got %q", findings[0].Status)
			}
		})
	}
}

func TestClassifyAuthTypeWithoutEnvVars(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{Type: "api_key"},
	}
	findings := Classify("mystery", m, envFrom(nil))
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].Status != StatusUnknown {
		t.Errorf("want StatusUnknown when type is declared but env_vars empty, got %q", findings[0].Status)
	}
}

func TestClassifyEnvVarSetOK(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"HUBSPOT_ACCESS_TOKEN"},
		},
	}
	env := envFrom(map[string]string{"HUBSPOT_ACCESS_TOKEN": "pat-xxxxxxxxxxxx"})
	findings := Classify("hubspot", m, env)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Status != StatusOK {
		t.Errorf("want StatusOK, got %q (reason=%q)", f.Status, f.Reason)
	}
	if f.Fingerprint != "pat-..." {
		t.Errorf("want fingerprint %q, got %q", "pat-...", f.Fingerprint)
	}
	if f.EnvVar != "HUBSPOT_ACCESS_TOKEN" {
		t.Errorf("env var not carried through: %q", f.EnvVar)
	}
}

func TestClassifyEnvVarUnset(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"ESPN_KEY"},
		},
	}
	findings := Classify("espn", m, envFrom(nil))
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].Status != StatusNotSet {
		t.Errorf("want StatusNotSet, got %q", findings[0].Status)
	}
	if findings[0].Fingerprint != "" {
		t.Errorf("fingerprint should be empty for unset, got %q", findings[0].Fingerprint)
	}
}

func TestClassifyEnvVarSuspiciousShortAPIKey(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"ESPN_KEY"},
		},
	}
	env := envFrom(map[string]string{"ESPN_KEY": "abc"})
	findings := Classify("espn", m, env)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Status != StatusSuspicious {
		t.Errorf("want StatusSuspicious, got %q", f.Status)
	}
	if f.Reason == "" {
		t.Error("suspicious finding should carry a reason")
	}
	if f.Fingerprint == "" {
		t.Error("suspicious finding should still carry a fingerprint")
	}
}

func TestClassifyEnvVarSuspiciousShortBearerToken(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "bearer_token",
			EnvVars: []string{"DUB_TOKEN"},
		},
	}
	// 12 chars, min for bearer_token is 20
	env := envFrom(map[string]string{"DUB_TOKEN": "short_value1"})
	findings := Classify("dub", m, env)
	if findings[0].Status != StatusSuspicious {
		t.Errorf("want StatusSuspicious for short bearer token, got %q", findings[0].Status)
	}
}

func TestClassifyEnvVarSuspiciousSurroundingWhitespace(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"HUBSPOT_ACCESS_TOKEN"},
		},
	}
	env := envFrom(map[string]string{"HUBSPOT_ACCESS_TOKEN": "  pat-well-formed-value  "})
	findings := Classify("hubspot", m, env)
	f := findings[0]
	if f.Status != StatusSuspicious {
		t.Errorf("want StatusSuspicious for wrapped whitespace, got %q", f.Status)
	}
	if f.Reason == "" {
		t.Error("whitespace finding should carry a reason")
	}
}

func TestClassifyUnknownAuthTypePassesLengthCheck(t *testing.T) {
	// Unknown types are not length-gated; any non-empty value is OK.
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "composed",
			EnvVars: []string{"DOMINOS_TOKEN"},
		},
	}
	env := envFrom(map[string]string{"DOMINOS_TOKEN": "xy"})
	findings := Classify("dominos", m, env)
	if findings[0].Status != StatusOK {
		t.Errorf("unknown auth types should not be length-gated, got %q", findings[0].Status)
	}
}

func TestClassifyComposedMultipleEnvVarsMixed(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "composed",
			EnvVars: []string{"COOKIE_A", "COOKIE_B"},
		},
	}
	env := envFrom(map[string]string{"COOKIE_A": "abcdef12345"})
	findings := Classify("pagliacci", m, env)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per env var), got %d", len(findings))
	}
	// Findings are in manifest order
	if findings[0].Status != StatusOK {
		t.Errorf("COOKIE_A should be OK, got %q", findings[0].Status)
	}
	if findings[1].Status != StatusNotSet {
		t.Errorf("COOKIE_B should be NotSet, got %q", findings[1].Status)
	}
}

func TestClassifyBrowserSessionAlsoReportsEnvVars(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:                   "cookie",
			EnvVars:                []string{"PRODUCT_SESSION"},
			RequiresBrowserSession: true,
		},
	}
	findings := Classify("product", m, envFrom(map[string]string{"PRODUCT_SESSION": "session=value"}))
	if len(findings) != 2 {
		t.Fatalf("want env var finding plus browser-session proof finding, got %d", len(findings))
	}
	if findings[0].EnvVar != "PRODUCT_SESSION" || findings[0].Status != StatusOK {
		t.Fatalf("want first finding to report env var status, got %+v", findings[0])
	}
	if findings[1].Status != StatusUnknown {
		t.Fatalf("want browser-session proof finding to remain unknown, got %+v", findings[1])
	}
	if findings[1].Reason == "" {
		t.Fatal("browser-session proof finding should explain the required doctor check")
	}
}
