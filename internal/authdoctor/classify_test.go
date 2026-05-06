package authdoctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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

func TestClassifyMixedVersionManifestUsesEnvVarSpecsOnce(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"RICH_TOKEN"},
			EnvVarSpecs: []spec.AuthEnvVar{{
				Name:      "RICH_TOKEN",
				Kind:      spec.AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
			}},
		},
	}

	findings := Classify("rich", m, envFrom(map[string]string{"RICH_TOKEN": "well-formed-token"}))
	if len(findings) != 1 {
		t.Fatalf("want one rich-path finding with no legacy duplicate, got %d: %+v", len(findings), findings)
	}
	if findings[0].EnvVar != "RICH_TOKEN" || findings[0].Status != StatusOK {
		t.Fatalf("want single RICH_TOKEN OK finding, got %+v", findings[0])
	}
}

func TestSameAuthEnvVarNamesIgnoresOrder(t *testing.T) {
	if !sameAuthEnvVarNames([]string{"B", "A"}, []spec.AuthEnvVar{
		{Name: "A"},
		{Name: "B"},
	}) {
		t.Fatal("expected auth env var names with different order to match")
	}
}

func TestClassifyEnvVarSpecsKindAwareReporting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type:    "composed",
			EnvVars: []string{"PER_CALL_REQUIRED", "OAUTH_CLIENT_SECRET", "SESSION_COOKIE"},
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "PER_CALL_REQUIRED", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
				{Name: "PER_CALL_OPTIONAL", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
				{Name: "OAUTH_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
				{Name: "SESSION_COOKIE", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			},
		},
	}

	findings := Classify("rich", m, envFrom(nil))
	if len(findings) != 4 {
		t.Fatalf("want one finding per rich env var, got %d: %+v", len(findings), findings)
	}
	if findings[0].EnvVar != "PER_CALL_REQUIRED" || findings[0].Status != StatusNotSet {
		t.Fatalf("required per-call missing should be not_set, got %+v", findings[0])
	}
	for _, idx := range []int{1, 2, 3} {
		if findings[idx].Status != StatusInfo {
			t.Fatalf("finding %d should be informational, got %+v", idx, findings[idx])
		}
	}
	if findings[3].Reason != "populated by auth login; run auth login --chrome" {
		t.Fatalf("composed harvested env var should keep chrome login hint, got %q", findings[3].Reason)
	}
}

func TestClassifyHarvestedBearerEnvVarDoesNotSuggestChrome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type: "bearer_token",
			EnvVarSpecs: []spec.AuthEnvVar{{
				Name:      "BEARER_ACCESS_TOKEN",
				Kind:      spec.AuthEnvVarKindHarvested,
				Required:  false,
				Sensitive: true,
			}},
		},
	}

	findings := Classify("bearer-api", m, envFrom(nil))
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d", len(findings))
	}
	if findings[0].Status != StatusInfo {
		t.Fatalf("want missing harvested bearer token to report info, got %+v", findings[0])
	}
	if findings[0].Reason != "populated by auth login; run the printed CLI's auth command" {
		t.Fatalf("bearer harvested env var should not suggest chrome login, got %q", findings[0].Reason)
	}
}

func TestClassifyHarvestedEnvVarUsesAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "cookie-api-pp-cli")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("session = 'ok'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{
			Type: "cookie",
			EnvVarSpecs: []spec.AuthEnvVar{{
				Name:      "COOKIE_API_SESSION",
				Kind:      spec.AuthEnvVarKindHarvested,
				Required:  false,
				Sensitive: true,
			}},
		},
	}

	findings := Classify("cookie-api", m, envFrom(nil))
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d", len(findings))
	}
	if findings[0].Status != StatusOK || findings[0].Reason != "auth file present" {
		t.Fatalf("want harvested auth file to report ok, got %+v", findings[0])
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

func TestClassifyTierRoutingEnvVars(t *testing.T) {
	m := &pipeline.ToolsManifest{
		Auth: pipeline.ManifestAuth{Type: "none"},
		TierRouting: &pipeline.ManifestTiers{
			DefaultTier: "free",
			Tiers: map[string]pipeline.ManifestTier{
				"free": {Auth: pipeline.ManifestAuth{Type: "none"}},
				"paid": {
					Auth: pipeline.ManifestAuth{
						Type:    "api_key",
						EnvVars: []string{"PAID_KEY"},
					},
				},
			},
		},
	}

	findings := Classify("tiered", m, envFrom(map[string]string{"PAID_KEY": "paid-secret-value"}))
	if len(findings) != 1 {
		t.Fatalf("want paid tier env finding, got %d", len(findings))
	}
	if findings[0].Type != "tier:paid/api_key" {
		t.Fatalf("want scoped tier auth type, got %q", findings[0].Type)
	}
	if findings[0].EnvVar != "PAID_KEY" || findings[0].Status != StatusOK {
		t.Fatalf("want paid tier env var OK, got %+v", findings[0])
	}
}
