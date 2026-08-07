package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateOAuth2RefreshAuth(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth2-refresh")
	apiSpec.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		TokenURL: "https://auth.example.com/oauth/token",
		EnvVars: []string{
			"OAUTH_REFRESH_CLIENT_ID",
			"OAUTH_REFRESH_CLIENT_SECRET",
			"OAUTH_REFRESH_REFRESH_TOKEN",
		},
	}

	outputDir := filepath.Join(t.TempDir(), "oauth2-refresh-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	readme := readGeneratedFile(t, outputDir, "README.md")
	skill := readGeneratedFile(t, outputDir, "SKILL.md")

	assert.Contains(t, configSrc, `c.AuthSource = "oauth2_refresh"`)
	assert.Contains(t, configSrc, `return "Bearer " + c.AccessToken`)
	assert.NotContains(t, configSrc, `c.AuthSource = "bearer_refresh"`)
	assert.Contains(t, clientSrc, `"grant_type":    {"refresh_token"}`)
	assert.Contains(t, clientSrc, `OAuth2 client ID is required when a refresh token is configured`)
	assert.Contains(t, clientSrc, `tokenURL = "https://auth.example.com/oauth/token"`)
	assert.Contains(t, doctorSrc, `report["auth"] = "misconfigured (oauth2 refresh)"`)
	assert.Contains(t, doctorSrc, "cfg.AuthSource = \"oauth2_refresh\"\n					report[\"auth_source\"] = cfg.AuthSource\n					report[\"auth_hint\"] = \"refresh token is configured but OAuth2 client ID is missing")
	assert.Contains(t, doctorSrc, `refresh token is configured but OAuth2 client ID is missing`)
	assert.Contains(t, doctorSrc, `report["auth"] = "configured (oauth2 refresh)"`)
	assert.Contains(t, doctorSrc, `report["auth_source"] = cfg.AuthSource`)
	assert.Contains(t, doctorSrc, `report["auth_hint"] = "export OAUTH_REFRESH_REFRESH_TOKEN=<your-oauth-refresh-value>"`)
	assert.NotContains(t, doctorSrc, `report["auth_hint"] = "export OAUTH_REFRESH_CLIENT_ID=<your-oauth-refresh-value>"`)
	assert.Contains(t, readme, "This CLI uses OAuth2 with refresh-token rotation.")
	assert.Contains(t, skill, "This CLI uses OAuth2 with refresh-token rotation.")

	writeOAuth2RefreshRuntimeTest(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestOAuth2Refresh(TokenUsedBeforeRequest|AfterUnauthorizedPersistsRotatedTokens)$")
}

func TestOAuth2RefreshDefaultsEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth2-refresh-defaults")
	apiSpec.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		TokenURL: "https://auth.example.com/oauth/token",
	}

	require.NoError(t, apiSpec.Validate())
	require.Equal(t, []spec.AuthEnvVar{
		{
			Name:        "OAUTH2_REFRESH_DEFAULTS_CLIENT_ID",
			Kind:        spec.AuthEnvVarKindAuthFlowInput,
			Required:    true,
			Sensitive:   false,
			Description: "OAuth client ID.",
			Inferred:    true,
		},
		{
			Name:        "OAUTH2_REFRESH_DEFAULTS_CLIENT_SECRET",
			Kind:        spec.AuthEnvVarKindAuthFlowInput,
			Required:    false,
			Sensitive:   true,
			Description: "OAuth client secret.",
			Inferred:    true,
		},
		{
			Name:        "OAUTH2_REFRESH_DEFAULTS_REFRESH_TOKEN",
			Kind:        spec.AuthEnvVarKindAuthFlowInput,
			Required:    true,
			Sensitive:   true,
			Description: "OAuth refresh token.",
			Inferred:    true,
		},
	}, apiSpec.Auth.EnvVarSpecs)
}

func TestOAuth2RefreshExplicitEnvVarsKeepClientSecretOptional(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth2-refresh-explicit")
	apiSpec.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		TokenURL: "https://auth.example.com/oauth/token",
		EnvVars: []string{
			"OAUTH2_REFRESH_EXPLICIT_CLIENT_ID",
			"OAUTH2_REFRESH_EXPLICIT_CLIENT_SECRET",
			"OAUTH2_REFRESH_EXPLICIT_REFRESH_TOKEN",
		},
	}

	require.NoError(t, apiSpec.Validate())
	require.Equal(t, []spec.AuthEnvVar{
		{
			Name:      "OAUTH2_REFRESH_EXPLICIT_CLIENT_ID",
			Kind:      spec.AuthEnvVarKindAuthFlowInput,
			Required:  true,
			Sensitive: false,
			Inferred:  true,
		},
		{
			Name:      "OAUTH2_REFRESH_EXPLICIT_CLIENT_SECRET",
			Kind:      spec.AuthEnvVarKindAuthFlowInput,
			Required:  false,
			Sensitive: true,
			Inferred:  true,
		},
		{
			Name:      "OAUTH2_REFRESH_EXPLICIT_REFRESH_TOKEN",
			Kind:      spec.AuthEnvVarKindAuthFlowInput,
			Required:  true,
			Sensitive: true,
			Inferred:  true,
		},
	}, apiSpec.Auth.EnvVarSpecs)
	require.NotNil(t, apiSpec.Auth.OAuth2RefreshTokenEnvVar())
	assert.Equal(t, "OAUTH2_REFRESH_EXPLICIT_REFRESH_TOKEN", apiSpec.Auth.OAuth2RefreshTokenEnvVar().Name)
}

func TestOAuth2RefreshRequiresTokenURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth2-refresh-missing-token-url")
	apiSpec.Auth = spec.AuthConfig{
		Type:   spec.AuthTypeOAuth2Refresh,
		Header: "Authorization",
	}

	err := apiSpec.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `auth.token_url is required when auth.type is "oauth2_refresh"`)
}

func writeOAuth2RefreshRuntimeTest(t *testing.T, outputDir string) {
	t.Helper()

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	modulePath := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(goMod, "\n", 2)[0], "module "))
	require.NotEmpty(t, modulePath)

	runtimeTest := strings.ReplaceAll(`package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"__MODULE_PATH__/internal/cliutil"
	"__MODULE_PATH__/internal/config"
)

func TestOAuth2RefreshTokenUsedBeforeRequest(t *testing.T) {
	var gotGrant string
	var gotRefreshToken string
	var gotClientID string
	var gotClientSecret string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("token request method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type = %q, want application/x-www-form-urlencoded", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotGrant = r.Form.Get("grant_type")
		gotRefreshToken = r.Form.Get("refresh_token")
		gotClientID = r.Form.Get("client_id")
		gotClientSecret = r.Form.Get("client_secret")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `+"`"+`{"access_token":"access-123","refresh_token":"refresh-456","expires_in":3600}`+"`"+`)
	}))
	defer srv.Close()

	cfg := &config.Config{
		BaseURL:       "https://api.example.com",
		TokenURL:      srv.URL,
		RefreshToken:  "refresh-123",
		ClientID:      "client-123",
		ClientSecret:  "secret-123",
		TokenExpiry:   time.Now().Add(-time.Minute),
		Path:          filepath.Join(t.TempDir(), "config.toml"),
	}
	c := New(cfg, time.Second, 0)
	c.HTTPClient = srv.Client()

	header, err := c.authHeader(context.Background())
	if err != nil {
		t.Fatalf("authHeader: %v", err)
	}
	if header != "Bearer access-123" {
		t.Fatalf("auth header = %q, want Bearer access-123", header)
	}
	if gotGrant != "refresh_token" || gotRefreshToken != "refresh-123" || gotClientID != "client-123" || gotClientSecret != "secret-123" {
		t.Fatalf("refresh form = grant:%q refresh:%q client_id:%q client_secret:%q", gotGrant, gotRefreshToken, gotClientID, gotClientSecret)
	}
	if cfg.RefreshToken != "refresh-456" {
		t.Fatalf("rotated refresh token = %q, want refresh-456", cfg.RefreshToken)
	}
	if cfg.AuthSource != "oauth2_refresh" {
		t.Fatalf("auth source = %q, want oauth2_refresh", cfg.AuthSource)
	}
}

func TestOAuth2RefreshAfterUnauthorizedPersistsRotatedTokens(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("OAUTH2_REFRESH_DATA_DIR", dataDir)

	var apiCalls int
	var tokenCalls int
	handleToken := func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if r.Method != http.MethodPost {
			t.Fatalf("token request method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-123" {
			t.Fatalf("refresh_token = %q, want refresh-123", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `+"`"+`{"access_token":"access-after-401","refresh_token":"refresh-after-401","expires_in":3600}`+"`"+`)
	}
	handleResource := func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		switch apiCalls {
		case 1:
			if got := r.Header.Get("Authorization"); got != "Bearer stale-access" {
				t.Fatalf("first Authorization = %q, want Bearer stale-access", got)
			}
			http.Error(w, "expired token", http.StatusUnauthorized)
		case 2:
			if got := r.Header.Get("Authorization"); got != "Bearer access-after-401" {
				t.Fatalf("retry Authorization = %q, want Bearer access-after-401", got)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `+"`"+`{"ok":true}`+"`"+`)
		default:
			t.Fatalf("unexpected API call %d", apiCalls)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			handleToken(w, r)
		case "/resource":
			handleResource(w, r)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		BaseURL:      srv.URL,
		TokenURL:     srv.URL + "/token",
		AccessToken:  "stale-access",
		RefreshToken: "refresh-123",
		ClientID:     "client-123",
		ClientSecret: "secret-123",
		TokenExpiry:  time.Now().Add(time.Hour),
		Path:         filepath.Join(t.TempDir(), "config.toml"),
	}
	c := New(cfg, time.Second, 0)
	c.HTTPClient = srv.Client()

	if _, err := c.Get(context.Background(), "/resource", nil); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if apiCalls != 2 {
		t.Fatalf("api calls = %d, want 2", apiCalls)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}

	credentialsPath := filepath.Join(dataDir, "credentials.toml")
	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatalf("ReadFile(credentials.toml) error = %v", err)
	}
	credentialsText := string(credentialsData)
	for _, want := range []string{"access-after-401", "refresh-after-401", "token_expiry"} {
		if !strings.Contains(credentialsText, want) {
			t.Fatalf("credentials.toml missing %q after 401 refresh:\n%s", want, credentialsText)
		}
	}
	reloadedCreds, _, err := cliutil.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() after 401 refresh error = %v", err)
	}
	if reloadedCreds == nil || reloadedCreds.TokenExpiry.IsZero() {
		t.Fatalf("reloaded TokenExpiry is zero after 401 refresh")
	}
	if want := time.Now().Add(50 * time.Minute); reloadedCreds.TokenExpiry.Before(want) {
		t.Fatalf("reloaded TokenExpiry = %s is earlier than expected minimum %s", reloadedCreds.TokenExpiry, want)
	}
}
`, "__MODULE_PATH__", modulePath)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "oauth2_refresh_test.go"), []byte(runtimeTest), 0o644))
}

func TestOAuth2RefreshFailsBeforeNetworkWithoutClientID(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth2-refresh-missing-client")
	apiSpec.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		TokenURL: "https://auth.example.com/oauth/token",
	}

	outputDir := filepath.Join(t.TempDir(), "oauth2-refresh-missing-client-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	writeOAuth2RefreshMissingClientRuntimeTest(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestOAuth2RefreshFailsBeforeNetworkWithoutClientID$")
}

func writeOAuth2RefreshMissingClientRuntimeTest(t *testing.T, outputDir string) {
	t.Helper()

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	modulePath := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(goMod, "\n", 2)[0], "module "))
	require.NotEmpty(t, modulePath)

	runtimeTest := strings.ReplaceAll(`package client

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"__MODULE_PATH__/internal/config"
)

func TestOAuth2RefreshFailsBeforeNetworkWithoutClientID(t *testing.T) {
	called := false
	cfg := &config.Config{
		BaseURL:      "https://api.example.com",
		TokenURL:     "https://auth.example.com/oauth/token",
		RefreshToken: "refresh-123",
		TokenExpiry:  time.Now().Add(-time.Minute),
		Path:         filepath.Join(t.TempDir(), "config.toml"),
	}
	c := New(cfg, time.Second, 0)
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		t.Fatalf("refresh should fail before network when client ID is missing")
		return nil, nil
	})}

	_, err := c.authHeader(context.Background())
	if err == nil || !strings.Contains(err.Error(), "OAuth2 client ID is required") {
		t.Fatalf("authHeader err = %v", err)
	}
	if called {
		t.Fatalf("token endpoint was called")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
`, "__MODULE_PATH__", modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "oauth2_refresh_missing_client_test.go"), []byte(runtimeTest), 0o644))
}
