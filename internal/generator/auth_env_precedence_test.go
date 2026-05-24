package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/catalogmeta"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHeader_ClientCredentialsDoesNotUseSetupEnvVars pins that under
// OAuth2 client_credentials the setup inputs are never emitted as bearer
// headers. Only a minted AccessToken is usable for API requests.
func TestAuthHeader_ClientCredentialsDoesNotUseSetupEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cc-precedence")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "CC_AUTH_TEST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "CC_AUTH_TEST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
		},
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "cc-precedence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	envCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_ID") + ` != ""`
	envSecretCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_SECRET") + ` != ""`
	tokenCheck := `if c.AccessToken != ""`

	require.Contains(t, content, tokenCheck, "AuthHeader must check AccessToken")

	body := authHeaderBody(t, content)
	require.Contains(t, body, tokenCheck)
	require.NotContains(t, body, envCheck, "client ID must not be used as a bearer token")
	require.NotContains(t, body, envSecretCheck, "client secret must not be used as a bearer token")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	verifyIdx := strings.Index(clientContent, `cliutil.IsVerifyEnv()`)
	mintIdx := strings.Index(clientContent, `c.mintClientCredentials(ctx, clientID, clientSecret)`)
	require.NotEqual(t, -1, verifyIdx, "mock verification should short-circuit before token minting")
	require.NotEqual(t, -1, mintIdx, "client_credentials mint path should still be emitted")
	assert.Less(t, verifyIdx, mintIdx, "mock verification must not dial the real token endpoint")
}

func TestClientCredentialsEnvVarsSkipTenantSetupInput(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("entra-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/COMMON/oauth2/v2.0/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "ENTRA_CC_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "ENTRA_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "ENTRA_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "entra-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = os.Getenv("ENTRA_CC_CLIENT_ID")`)
	require.Contains(t, authContent, `clientSecret = os.Getenv("ENTRA_CC_CLIENT_SECRET")`)
	require.NotContains(t, authContent, `clientID = os.Getenv("ENTRA_CC_TENANT_ID")`)
	require.Contains(t, authContent, `resolveClientCredentialsTokenURL(tokenURL, cfg.`+resolveEnvVarField("ENTRA_CC_TENANT_ID")+`)`)
	require.Contains(t, authContent, `os.Getenv("ENTRA_CC_OAUTH_SCOPE")`)
	require.Contains(t, authContent, `strings.ReplaceAll("api://{client_id}/.default", "{client_id}", clientID)`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("ENTRA_CC_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("ENTRA_CC_CLIENT_SECRET")`)
	require.NotContains(t, clientContent, `id = os.Getenv("ENTRA_CC_TENANT_ID")`)
	require.Contains(t, clientContent, `tenant = c.Config.`+resolveEnvVarField("ENTRA_CC_TENANT_ID"))
	require.Contains(t, clientContent, `resolveClientCredentialsTokenURL(tokenURL, tenant)`)
	require.Contains(t, clientContent, `form.Set("scope", scope)`)

	const runtimeTest = `package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"entra-cc-pp-cli/internal/config"
)

func TestClientCredentialsRuntimeEntraTenantAndScope(t *testing.T) {
	var gotPath string
	var gotScope string
	var gotClientID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		gotScope = r.Form.Get("scope")
		gotClientID = r.Form.Get("client_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"access_token":"minted-token","expires_in":3600}` + "`" + `)
	}))
	defer server.Close()

	cfg := &config.Config{TokenURL: server.URL + "/COMMON/oauth2/v2.0/token", Path: filepath.Join(t.TempDir(), "config.toml")}
	cfg.{{TENANT_FIELD}} = "contoso"
	c := &Client{Config: cfg, HTTPClient: server.Client()}
	if err := c.mintClientCredentials(context.Background(), "client-123", "secret-456"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if gotPath != "/contoso/oauth2/v2.0/token" {
		t.Fatalf("token request path = %q, want /contoso/oauth2/v2.0/token", gotPath)
	}
	if gotScope != "api://client-123/.default" {
		t.Fatalf("scope = %q, want api://client-123/.default", gotScope)
	}
	if gotClientID != "client-123" {
		t.Fatalf("client_id = %q, want client-123", gotClientID)
	}

	t.Setenv("ENTRA_CC_OAUTH_SCOPE", "https://override.example/.default")
	if err := c.mintClientCredentials(context.Background(), "client-789", "secret-456"); err != nil {
		t.Fatalf("mintClientCredentials() with scope override error = %v", err)
	}
	if gotScope != "https://override.example/.default" {
		t.Fatalf("override scope = %q, want https://override.example/.default", gotScope)
	}
}

func TestClientCredentialsRuntimeEntraRequiresTenant(t *testing.T) {
	cfg := &config.Config{TokenURL: "https://login.microsoftonline.com/COMMON/oauth2/v2.0/token"}
	c := &Client{Config: cfg, HTTPClient: http.DefaultClient}
	if err := c.mintClientCredentials(context.Background(), "client-123", "secret-456"); err == nil {
		t.Fatalf("mintClientCredentials() error = nil, want tenant-required error")
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "entra_runtime_test.go"),
		[]byte(strings.ReplaceAll(runtimeTest, "{{TENANT_FIELD}}", resolveEnvVarField("ENTRA_CC_TENANT_ID"))),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestClientCredentialsRuntimeEntra")
}

func TestClientCredentialsStaticTokenURLDoesNotEmitTenantSubstitution(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("static-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "STATIC_CC_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "STATIC_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "STATIC_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "static-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.NotContains(t, string(authSrc), `resolveClientCredentialsTokenURL(tokenURL`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.NotContains(t, string(clientSrc), `resolveClientCredentialsTokenURL(tokenURL`)

	const runtimeTest = `package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"static-cc-pp-cli/internal/config"
)

func TestClientCredentialsRuntimeStaticTokenURLDoesNotSendDefaultScope(t *testing.T) {
	var hasScope bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		_, hasScope = r.Form["scope"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"access_token":"minted-token","expires_in":3600}` + "`" + `)
	}))
	defer server.Close()

	cfg := &config.Config{TokenURL: server.URL, Path: filepath.Join(t.TempDir(), "config.toml")}
	c := &Client{Config: cfg, HTTPClient: server.Client()}
	if err := c.mintClientCredentials(context.Background(), "client-123", "secret-456"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if hasScope {
		t.Fatalf("unexpected scope form value for static non-Microsoft token URL")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "static_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestClientCredentialsRuntimeStatic")
}

func TestClientCredentialsLegacyEnvVarsSubstituteTenantTokenURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("legacy-entra-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		EnvVars: []string{
			"LEGACY_ENTRA_TENANT_ID",
			"LEGACY_ENTRA_CLIENT_ID",
			"LEGACY_ENTRA_CLIENT_SECRET",
		},
	}
	ccEnvVars := clientCredentialsEnvVars(apiSpec.Auth)
	require.Len(t, ccEnvVars, 2)
	require.Equal(t, "LEGACY_ENTRA_CLIENT_ID", ccEnvVars[0].Name)
	require.False(t, ccEnvVars[0].Sensitive, "legacy client ID must not be synthesized as sensitive")
	require.Equal(t, "LEGACY_ENTRA_CLIENT_SECRET", ccEnvVars[1].Name)
	require.True(t, ccEnvVars[1].Sensitive, "legacy client secret must stay sensitive")

	outputDir := filepath.Join(t.TempDir(), "legacy-entra-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = os.Getenv("LEGACY_ENTRA_CLIENT_ID")`)
	require.Contains(t, authContent, `clientSecret = os.Getenv("LEGACY_ENTRA_CLIENT_SECRET")`)
	require.Contains(t, authContent, `resolveClientCredentialsTokenURL(tokenURL, cfg.`+resolveEnvVarField("LEGACY_ENTRA_TENANT_ID")+`)`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("LEGACY_ENTRA_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("LEGACY_ENTRA_CLIENT_SECRET")`)
	require.Contains(t, clientContent, `tenant = c.Config.`+resolveEnvVarField("LEGACY_ENTRA_TENANT_ID"))
	require.Contains(t, clientContent, `resolveClientCredentialsTokenURL(tokenURL, tenant)`)
}

func TestClientCredentialsTenantPrefixDoesNotHideClientCredentials(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("multitenant-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MULTITENANT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "MULTITENANT_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "multitenant-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = os.Getenv("MULTITENANT_CLIENT_ID")`)
	require.Contains(t, authContent, `clientSecret = os.Getenv("MULTITENANT_CLIENT_SECRET")`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("MULTITENANT_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("MULTITENANT_CLIENT_SECRET")`)
}

func TestClientCredentialsPerCallTenantIDDoesNotTriggerConfigBackedTenantSubstitution(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("per-call-tenant-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "PER_CALL_TENANT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "PER_CALL_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "PER_CALL_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "per-call-tenant-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	require.NotContains(t, string(authSrc), `resolveClientCredentialsTokenURL(tokenURL`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.NotContains(t, string(clientSrc), `resolveClientCredentialsTokenURL(tokenURL`)
}

func TestClientCredentialsDeclaredScopesSuppressMicrosoftDefaultScope(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("scoped-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://login.microsoftonline.com/COMMON/oauth2/v2.0/token",
		Scopes:      []string{"https://graph.microsoft.com/.default"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SCOPED_CC_TENANT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "SCOPED_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "SCOPED_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "scoped-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `return "https://graph.microsoft.com/.default"`)
	require.NotContains(t, clientContent, `api://{client_id}/.default`)
}

func TestClientCredentialsMixedKindEnvVarsPreserveClientRoles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mixed-kind-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MIXED_KIND_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "MIXED_KIND_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "mixed-kind-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	authContent := string(authSrc)
	require.Contains(t, authContent, `clientID = os.Getenv("MIXED_KIND_CLIENT_ID")`)
	require.Contains(t, authContent, `clientSecret = os.Getenv("MIXED_KIND_CLIENT_SECRET")`)

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	require.Contains(t, clientContent, `id = os.Getenv("MIXED_KIND_CLIENT_ID")`)
	require.Contains(t, clientContent, `secret = os.Getenv("MIXED_KIND_CLIENT_SECRET")`)
}

func TestClientCredentialsHostileScopeEmitsValidGo(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hostile-scope-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		Scopes:      []string{"read\"with\\escapes\nwrite"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "HOSTILE_SCOPE_CLIENT_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "HOSTILE_SCOPE_CLIENT_SECRET", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "hostile-scope-cc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "test", "./internal/client")
	runGoCommand(t, outputDir, "test", "./internal/cli")
}

// TestAuthHeader_OAuth2DoesNotUseSetupEnvVars pins that for every OAuth2
// grant (authorization_code via the default, client_credentials via explicit
// OAuth2Grant) the configured env vars (e.g. CLIENT_ID / CLIENT_SECRET) are
// never emitted as bearer headers. The minted AccessToken is the only usable
// bearer; sending CLIENT_ID as `Authorization: Bearer` surfaces as
// token_rejected at the API.
func TestAuthHeader_OAuth2DoesNotUseSetupEnvVars(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		grant string
	}{
		{"authorization_code", ""},
		{"client_credentials", spec.OAuth2GrantClientCredentials},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("oauth-precedence-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:   "oauth2",
				Header: "Authorization",
				Format: "Bearer {token}",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "OAUTH_AUTH_TEST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
					{Name: "OAUTH_AUTH_TEST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
				},
				AuthorizationURL: "https://example.com/auth",
				TokenURL:         "https://example.com/token",
				OAuth2Grant:      tc.grant,
			}

			outputDir := filepath.Join(t.TempDir(), "oauth-precedence-"+tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			clientIDCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_CLIENT_ID") + ` != ""`
			clientSecretCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_CLIENT_SECRET") + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			body := authHeaderBody(t, content)
			require.Contains(t, body, tokenCheck, "AuthHeader must check AccessToken")
			require.Contains(t, body, `applyAuthFormat("Bearer {token}", map[string]string{"access_token": c.AccessToken, "token": c.AccessToken})`,
				"AuthHeader must return the AccessToken via applyAuthFormat")
			require.NotContains(t, body, clientIDCheck, "client ID must not be used as a bearer token")
			require.NotContains(t, body, clientSecretCheck, "client secret must not be used as a bearer token")
		})
	}
}

func TestAuthLoginEnvVarsUseShellSafePrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hyphen-api")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		AuthorizationURL: "https://example.com/auth",
		TokenURL:         "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "hyphen-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	require.Contains(t, content, `os.Getenv("HYPHEN_API_CLIENT_ID")`)
	require.Contains(t, content, `os.Getenv("HYPHEN_API_CLIENT_SECRET")`)
	require.NotContains(t, content, `HYPHEN-API_CLIENT_ID`)
}

func TestAuthHeader_LegacyEnvVarsList_OrSemantics(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("legacy-env-or")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"FOO_ACCESS_TOKEN", "FOO_API_KEY", "FOO_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "legacy-env-or-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const testSrc = `package config

import "testing"

func TestAuthHeaderUsesLegacyAliasEnvVar(t *testing.T) {
	t.Setenv("FOO_ACCESS_TOKEN", "")
	t.Setenv("FOO_API_KEY", "alias-token")
	t.Setenv("FOO_TOKEN", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.AuthHeader(); got != "Bearer alias-token" {
		t.Fatalf("AuthHeader() = %q, want %q", got, "Bearer alias-token")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "auth_header_legacy_envvars_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestAuthHeaderUsesLegacyAliasEnvVar")
}

// TestAuthHeader_EnvVarWinsOverFileToken pins env-first precedence for
// the non-client_credentials cases — plain bearer_token (PAT-style),
// cookie, and composed all follow the env > config convention so a
// freshly-rotated env var wins over a stale on-disk AccessToken.
func TestAuthHeader_EnvVarWinsOverFileToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		authType string
		envVar   string
	}{
		{"bearer_token", "bearer_token", "BEARER_AUTH_TEST_TOKEN"},
		{"cookie", "cookie", "COOKIE_AUTH_TEST_TOKEN"},
		{"composed", "composed", "COMPOSED_AUTH_TEST_TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.name + "-precedence")
			apiSpec.Auth = spec.AuthConfig{
				Type:    tc.authType,
				Header:  "Authorization",
				EnvVars: []string{tc.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tc.name+"-precedence-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			envCheck := "if c." + resolveEnvVarField(tc.envVar) + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			require.Contains(t, content, envCheck)
			require.Contains(t, content, tokenCheck)

			body := authHeaderBody(t, content)
			envIdx := strings.Index(body, envCheck)
			tokenIdx := strings.Index(body, tokenCheck)
			assert.Less(t, envIdx, tokenIdx,
				"env-var check must appear BEFORE AccessToken check for type %q", tc.authType)
		})
	}
}

// TestAuthHeader_BearerTokenPrefixOverride pins that a bearer_token spec
// declaring auth.prefix changes the rendered Authorization scheme word
// across both the env-var and AccessToken branches. APIs that require a
// non-Bearer scheme (e.g., "Token", "PRIVATE-TOKEN", lowercase "token")
// otherwise force operators to hand-edit generated config. When auth.prefix
// is unset, "Bearer" remains the default.
func TestAuthHeader_BearerTokenPrefixOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		prefix   string
		expected string
	}{
		{"default", "", "Bearer"},
		{"token", "Token", "Token"},
		{"lowercase", "token", "token"},
		{"private_token", "PRIVATE-TOKEN", "PRIVATE-TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("prefix-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Prefix:  tc.prefix,
				EnvVars: []string{"PREFIX_TEST_TOKEN"},
			}

			outputDir := filepath.Join(t.TempDir(), "prefix-"+tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			body := authHeaderBody(t, string(cfgSrc))

			envField := resolveEnvVarField("PREFIX_TEST_TOKEN")
			require.Contains(t, body, `return "`+tc.expected+` " + c.`+envField,
				"env-var branch must render configured prefix")
			require.Contains(t, body, `return "`+tc.expected+` " + c.AccessToken`,
				"AccessToken branch must render configured prefix")

			if tc.prefix != "" && tc.expected != "Bearer" {
				assert.NotContains(t, body, `return "Bearer " + c.`+envField,
					"default Bearer literal must not leak when prefix is overridden")
				assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
					"default Bearer literal must not leak when prefix is overridden")
			}
		})
	}
}

// TestAuthHeader_BearerTokenPrefixFormatPrecedence pins that Auth.Format
// wins over Auth.Prefix at the same call sites, so the documented "Ignored
// when Format is set" contract survives template restructuring.
func TestAuthHeader_BearerTokenPrefixFormatPrecedence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("prefix-format")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Prefix:  "Token",
		Format:  "Bearer {token}",
		EnvVars: []string{"PREFIX_FORMAT_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "prefix-format-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	body := authHeaderBody(t, string(cfgSrc))

	envField := resolveEnvVarField("PREFIX_FORMAT_TOKEN")
	require.Contains(t, body, `applyAuthFormat("Bearer {token}"`,
		"Format must render via applyAuthFormat, not via the prefix literal")
	assert.NotContains(t, body, `return "Token " + c.`+envField,
		"Prefix must not leak into the env-var branch when Format is set")
	assert.NotContains(t, body, `return "Token " + c.AccessToken`,
		"Prefix must not leak into the AccessToken branch when Format is set")
}

// TestAuthHeader_BearerTokenPrefixMissedSites exercises the three
// non-default code paths in config.go.tmpl that the main override table
// does not reach: oauth2/client_credentials, BearerRefresh.Enabled, and
// the $isAuthEnvVarORCase branch. Without these cases a revert of any of
// those template sites back to a "Bearer " literal would ship undetected.
func TestAuthHeader_BearerTokenPrefixMissedSites(t *testing.T) {
	t.Parallel()

	t.Run("oauth2_client_credentials", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("prefix-oauth2-cc")
		apiSpec.Auth = spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Prefix:      "Token",
			EnvVarSpecs: []spec.AuthEnvVar{{Name: "CC_PREFIX_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput}, {Name: "CC_PREFIX_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Sensitive: true}},
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://example.com/token",
		}

		outputDir := filepath.Join(t.TempDir(), "prefix-oauth2-cc-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		body := authHeaderBody(t, string(cfgSrc))

		require.Contains(t, body, `return "Token " + c.AccessToken`,
			"oauth2/client_credentials AccessToken branch must honor configured prefix")
		assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
			"default Bearer literal must not leak in the oauth2/cc branch when prefix is overridden")
	})

	t.Run("bearer_refresh_enabled", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("prefix-bearer-refresh")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "bearer_token",
			Header:  "Authorization",
			Prefix:  "Token",
			EnvVars: []string{"REFRESH_PREFIX_TOKEN"},
		}
		apiSpec.BearerRefresh = spec.BearerRefreshConfig{
			BundleURL: "https://cdn.example.com/main.js",
			Pattern:   `"(AAAAAAAA[^"]+)"`,
		}

		outputDir := filepath.Join(t.TempDir(), "prefix-bearer-refresh-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		body := authHeaderBody(t, string(cfgSrc))

		require.Contains(t, body, `return "Token " + c.AccessToken`,
			"BearerRefresh AccessToken branch must honor configured prefix")
		assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
			"default Bearer literal must not leak in the bearer_refresh branch when prefix is overridden")
	})

	t.Run("env_var_or_case", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("prefix-or-case")
		apiSpec.Auth = spec.AuthConfig{
			Type:   "bearer_token",
			Header: "Authorization",
			Prefix: "Token",
			EnvVarSpecs: []spec.AuthEnvVar{
				{Name: "OR_PREFIX_A", Kind: spec.AuthEnvVarKindPerCall, Required: false},
				{Name: "OR_PREFIX_B", Kind: spec.AuthEnvVarKindPerCall, Required: false},
			},
		}

		outputDir := filepath.Join(t.TempDir(), "prefix-or-case-pp-cli")
		require.NoError(t, New(apiSpec, outputDir).Generate())

		cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
		require.NoError(t, err)
		body := authHeaderBody(t, string(cfgSrc))

		fieldA := resolveEnvVarField("OR_PREFIX_A")
		fieldB := resolveEnvVarField("OR_PREFIX_B")
		require.Contains(t, body, `return "Token " + c.`+fieldA,
			"OR-case env-var branch must honor configured prefix (first env var)")
		require.Contains(t, body, `return "Token " + c.`+fieldB,
			"OR-case env-var branch must honor configured prefix (second env var)")
		assert.NotContains(t, body, `return "Bearer " + c.`+fieldA,
			"default Bearer literal must not leak in the OR-case branch when prefix is overridden")
	})
}

// TestTierRouting_BearerPrefix pins that the per-tier bearer scheme in
// client.go.tmpl honors auth.prefix on the tier's auth config, matching
// the default-tier AuthHeader() behavior.
func TestTierRouting_BearerPrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tier-prefix")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"TIER_PREFIX_TOKEN"},
	}
	apiSpec.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "primary",
		Tiers: map[string]spec.TierConfig{
			"primary": {
				Auth: spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					Prefix:  "Token",
					EnvVars: []string{"TIER_PRIMARY_TOKEN"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "tier-prefix-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	content := string(clientSrc)

	require.Contains(t, content, `value := "Token " + tierValue0`,
		"per-tier bearer auth must honor configured prefix")
	assert.NotContains(t, content, `value := "Bearer " + tierValue0`,
		"default Bearer literal must not leak when tier prefix is overridden")
}

// TestCatalogAuthEnvVars_GenerateReadsCatalogNamesFirst pins the issue #1482
// acceptance criterion: when a catalog entry declares auth_env_vars, the
// generator emits config.go reading the catalog-declared names first, in
// order, with the parser's name-derived default trailing as a fallback so
// operators who already export the legacy name keep working without a
// migration.
func TestCatalogAuthEnvVars_GenerateReadsCatalogNamesFirst(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("stripe")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"STRIPE_BEARER_AUTH"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "STRIPE_BEARER_AUTH", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
		},
	}

	catalogmeta.ApplyCatalogAuthEnvVars(&apiSpec.Auth, []string{"STRIPE_SECRET_KEY", "STRIPE_API_KEY"})

	outputDir := filepath.Join(t.TempDir(), "stripe-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	for _, name := range []string{"STRIPE_SECRET_KEY", "STRIPE_API_KEY", "STRIPE_BEARER_AUTH"} {
		field := resolveEnvVarField(name)
		assert.Contains(t, content, "if v := os.Getenv(\""+name+"\"); v != \"\" {",
			"Load() must read env var %s", name)
		assert.Contains(t, content, "cfg."+field, "Config struct must carry field for %s", name)
	}

	body := authHeaderBody(t, content)
	secretIdx := strings.Index(body, "if c."+resolveEnvVarField("STRIPE_SECRET_KEY")+` != ""`)
	apiIdx := strings.Index(body, "if c."+resolveEnvVarField("STRIPE_API_KEY")+` != ""`)
	bearerIdx := strings.Index(body, "if c."+resolveEnvVarField("STRIPE_BEARER_AUTH")+` != ""`)

	require.NotEqual(t, -1, secretIdx, "AuthHeader must check STRIPE_SECRET_KEY first")
	require.NotEqual(t, -1, apiIdx, "AuthHeader must check STRIPE_API_KEY")
	require.NotEqual(t, -1, bearerIdx, "AuthHeader must retain STRIPE_BEARER_AUTH fallback")
	assert.Less(t, secretIdx, apiIdx, "STRIPE_SECRET_KEY must be tried before STRIPE_API_KEY")
	assert.Less(t, apiIdx, bearerIdx, "STRIPE_API_KEY must be tried before legacy STRIPE_BEARER_AUTH fallback")
}

// TestCatalogAuthEnvVars_GenerateUnchangedWithoutCatalogList pins the
// negative acceptance criterion: an API without catalog auth_env_vars
// continues to emit only the parser's name-derived default env var, so
// existing CLIs regenerate to byte-equivalent config.go.
func TestCatalogAuthEnvVars_GenerateUnchangedWithoutCatalogList(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("legacy")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"LEGACY_BEARER_AUTH"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "LEGACY_BEARER_AUTH", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true, Inferred: true},
		},
	}

	catalogmeta.ApplyCatalogAuthEnvVars(&apiSpec.Auth, nil)

	outputDir := filepath.Join(t.TempDir(), "legacy-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	assert.Contains(t, content, "if v := os.Getenv(\"LEGACY_BEARER_AUTH\"); v != \"\"")
	assert.NotContains(t, content, "STRIPE_SECRET_KEY")
}

// authHeaderBody slices out just the AuthHeader function body so precedence
// assertions can't be tricked by a matching pattern in unrelated code
// further down the file.
func authHeaderBody(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, "func (c *Config) AuthHeader() string {")
	require.NotEqual(t, -1, start, "AuthHeader function must be emitted")
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
