package generator

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateGoogleServiceAccountAuthScaffold(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("google-service-account")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "bearer_token",
		Subtype:     spec.AuthSubtypeGoogleServiceAccount,
		Header:      "Authorization",
		TokenURL:    "https://oauth2.googleapis.com/token",
		Scopes:      []string{"https://www.googleapis.com/auth/cloud-platform"},
		EnvVars:     []string{"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_OAUTH_ACCESS_TOKEN"},
		EnvVarSpecs: spec.NewORCaseEnvVarSpecs([]string{"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_OAUTH_ACCESS_TOKEN"}),
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, authSrc, `Use:   "service-account"`)
	assert.Contains(t, authSrc, `google.JWTConfigFromJSON`)
	assert.Contains(t, authSrc, `--impersonate`)
	assert.Contains(t, authSrc, `--scopes`)
	assert.Contains(t, authSrc, "flags.oauthHTTPClient(cfg)")
	assert.Contains(t, authSrc, "oauth2.HTTPClient")

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	assert.Contains(t, configSrc, "GoogleImpersonate")
	assert.Contains(t, configSrc, "GoogleScopes")
	assert.Contains(t, configSrc, "GOOGLE_OAUTH_ACCESS_TOKEN")

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, "context.WithTimeout")
	assert.Contains(t, clientSrc, "googleServiceAccountAuthHeader")
	assert.Contains(t, clientSrc, `return "Bearer " + token.AccessToken`)
	assert.Contains(t, clientSrc, "oauth2.HTTPClient")
	assert.Contains(t, clientSrc, "cliutil.OAuthTokenHTTPClient(c.HTTPClient)")

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	assert.Contains(t, doctorSrc, "Google service account")

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.Contains(t, goMod, "golang.org/x/oauth2 v0.36.0")

	requireGeneratedCompiles(t, outputDir)

	noTokenURLSpec := minimalSpec("google-service-account-no-token-url")
	noTokenURLSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Subtype: spec.AuthSubtypeGoogleServiceAccount,
		Header:  "Authorization",
		Scopes:  []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	noTokenURLDir := filepath.Join(t.TempDir(), naming.CLI(noTokenURLSpec.Name))
	require.NoError(t, New(noTokenURLSpec, noTokenURLDir).Generate())
	noTokenAuth := readGeneratedFile(t, noTokenURLDir, "internal", "cli", "auth.go")
	noTokenClient := readGeneratedFile(t, noTokenURLDir, "internal", "client", "client.go")
	assert.Contains(t, noTokenAuth, "flags.oauthHTTPClient(cfg)")
	assert.Contains(t, noTokenAuth, "oauth2.HTTPClient")
	assert.NotContains(t, noTokenAuth, "cliutil.OAuthTokenHTTPClient",
		"service-account login uses NewPlainHTTPClient; its redirect callback does not call authHeader")
	assert.Contains(t, noTokenClient, "oauth2.HTTPClient")
	assert.Contains(t, noTokenClient, "cliutil.OAuthTokenHTTPClient(c.HTTPClient)")
	_, statErr := os.Stat(filepath.Join(noTokenURLDir, "internal", "cliutil", "oauth_token.go"))
	require.NoError(t, statErr)
	requireGeneratedCompiles(t, noTokenURLDir)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	keyJSON, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "service-account@example.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})),
		"token_uri":    "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)
	keyPath := filepath.Join(outputDir, "service-account.json")
	require.NoError(t, os.WriteFile(keyPath, keyJSON, 0o600))

	runtimeTest := fmt.Sprintf(`package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestMintGoogleServiceAccountToken(t *testing.T) {
	var gotGrantType, gotAssertion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request: %%v", err)
		}
		gotGrantType = r.Form.Get("grant_type")
		gotAssertion = r.Form.Get("assertion")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`+"`"+`{"access_token":"runtime-token","token_type":"Bearer","expires_in":3600}`+"`"+`))
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())
	token, expiry, err := mintGoogleServiceAccountToken(ctx, %q, server.URL, "delegated@example.com", []string{"scope-one", "scope-two"})
	if err != nil {
		t.Fatalf("mint token: %%v", err)
	}
	if token != "runtime-token" {
		t.Fatalf("token = %%q, want runtime-token", token)
	}
	if expiry.Before(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("expiry = %%s, want at least 30 minutes from now", expiry)
	}
	if gotGrantType != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("grant_type = %%q", gotGrantType)
	}
	parts := strings.Split(gotAssertion, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT assertion has %%d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %%v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("decode JWT claims: %%v", err)
	}
	if claims["sub"] != "delegated@example.com" {
		t.Fatalf("sub claim = %%v", claims["sub"])
	}
	if claims["scope"] != "scope-one scope-two" {
		t.Fatalf("scope claim = %%v", claims["scope"])
	}
	if claims["aud"] != server.URL {
		t.Fatalf("aud claim = %%v, want %%s", claims["aud"], server.URL)
	}
}

func TestServiceAccountLoginRejectsBadCA(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.pem")
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "--ca-cert", missing, "auth", "service-account", "--key", %q})
	err := root.Execute()
	if ExitCode(err) != 10 || !strings.Contains(err.Error(), missing) {
		t.Fatalf("exit=%%d err=%%v out=%%s", ExitCode(err), err, out.String())
	}
}

`, keyPath, keyPath)
	runtimeTest = strings.ReplaceAll(runtimeTest, "{{MODULE}}", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "google_service_account_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestMintGoogleServiceAccountToken|TestServiceAccountLoginRejectsBadCA", "-count=1")

	clientRuntimeTest := fmt.Sprintf(`package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"{{MODULE}}/internal/config"
	"golang.org/x/oauth2"
)

func TestGoogleServiceAccountAuthHeader(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request: %%v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(%q))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		GoogleApplicationCredentials: %q,
		GoogleScopes: []string{"scope-one"},
		TokenURL: server.URL,
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())
	c := New(cfg, 5*time.Second, 0)
	header, err := c.googleServiceAccountAuthHeader(ctx)
	if err != nil {
		t.Fatalf("first auth header: %%v", err)
	}
	if header != "Bearer runtime-token" {
		t.Fatalf("first header = %%q", header)
	}
	header, err = c.googleServiceAccountAuthHeader(ctx)
	if err != nil {
		t.Fatalf("second auth header: %%v", err)
	}
	if header != "Bearer runtime-token" {
		t.Fatalf("second header = %%q", header)
	}
	if requests != 1 {
		t.Fatalf("token endpoint requests = %%d, want 1", requests)
	}

	// An expired persisted token must not be returned by the authHeader fast
	// path; it should fall through to the expiry-aware service-account exchange.
	c.Config.AuthHeaderVal = "Bearer stale-token"
	c.Config.TokenExpiry = time.Now().Add(-time.Minute)
	header, err = c.authHeader(ctx)
	if err != nil {
		t.Fatalf("expired auth header: %%v", err)
	}
	if header != "Bearer runtime-token" {
		t.Fatalf("expired header = %%q", header)
	}
	if requests != 2 {
		t.Fatalf("token endpoint requests after expiry = %%d, want 2", requests)
	}

	c.DryRun = true
	c.Config.TokenExpiry = time.Now().Add(-time.Minute)
	before := requests
	if _, err = c.authHeader(ctx); err != nil {
		t.Fatalf("dry-run auth header: %%v", err)
	}
	if requests != before {
		t.Fatalf("dry-run exchanged a service-account token: requests = %%d, want %%d", requests, before)
	}
}

func TestGoogleServiceAccountAuthHeaderUsesAPIClient(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(%q))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL: server.URL,
		GoogleApplicationCredentials: %q,
		GoogleScopes: []string{"scope-one"},
		TokenURL: server.URL,
	}
	c := New(cfg, 5*time.Second, 0)
	c.HTTPClient = server.Client()
	header, err := c.googleServiceAccountAuthHeader(context.Background())
	if err != nil {
		t.Fatalf("auth header: %%v", err)
	}
	if header != "Bearer runtime-token" {
		t.Fatalf("header = %%q", header)
	}
	if requests != 1 {
		t.Fatalf("token endpoint requests = %%d, want 1", requests)
	}
}

`, `{"access_token":"runtime-token","token_type":"Bearer","expires_in":3600}`, keyPath, `{"access_token":"runtime-token","token_type":"Bearer","expires_in":3600}`, keyPath)
	clientRuntimeTest = strings.ReplaceAll(clientRuntimeTest, "{{MODULE}}", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "google_service_account_runtime_test.go"), []byte(clientRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestGoogleServiceAccountAuthHeader", "-count=1")

	configRuntimeTest := `package config

import (
	"path/filepath"
	"testing"
)

func TestGoogleServiceAccountSettingsPersist(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	cfg.SetGoogleServiceAccount("/tmp/service-account.json", "delegated@example.com", []string{"scope-one", "scope-two"})
	if err := cfg.save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if loaded.GoogleImpersonate != "delegated@example.com" {
		t.Fatalf("impersonate = %q", loaded.GoogleImpersonate)
	}
	if len(loaded.GoogleScopes) != 2 || loaded.GoogleScopes[1] != "scope-two" {
		t.Fatalf("scopes = %v", loaded.GoogleScopes)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "google_service_account_runtime_test.go"), []byte(configRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "TestGoogleServiceAccountSettingsPersist", "-count=1")

	// Spec TokenURL is empty, so the exchange targets the key's token_uri.
	// New() installs the API CheckRedirect, which calls authHeader on a
	// same-host hop. That hop must return a token instead of taking googleMu twice.
	hangTest := fmt.Sprintf(`package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"{{MODULE}}/internal/config"
)

func TestGoogleServiceAccountSameHostTokenRedirectDoesNotHang(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("GOOGLE_OAUTH_ACCESS_TOKEN", "")

	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", "/token-real")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/token-real", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token request: %%v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || r.Form.Get("assertion") == "" {
			t.Errorf("grant_type=%%q assertion empty=%%v", r.Form.Get("grant_type"), r.Form.Get("assertion") == "")
			http.Error(w, "bad assertion", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `+"`"+`{"access_token":"runtime-token","token_type":"Bearer","expires_in":3600}`+"`"+`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	key["token_uri"] = srv.URL + "/token"
	rewritten, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "service-account.json")
	if err := os.WriteFile(keyPath, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		BaseURL:                      srv.URL,
		GoogleApplicationCredentials: keyPath,
		GoogleScopes:                 []string{"scope-one"},
		Path:                         filepath.Join(t.TempDir(), "config.toml"),
	}
	c := New(cfg, 5*time.Second, 0)
	if err := c.TLSConfigError(); err != nil {
		t.Fatal(err)
	}

	type result struct {
		header string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		header, err := c.authHeader(context.Background())
		done <- result{header, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("auth header: %%v", got.err)
		}
		if got.header != "Bearer runtime-token" {
			t.Fatalf("header = %%q", got.header)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("same-host token redirect hung while exchanging a service-account token")
	}
	if hits.Load() != 2 {
		t.Fatalf("token endpoint hits = %%d, want 2", hits.Load())
	}
}
`, keyPath)
	hangTest = strings.ReplaceAll(hangTest, "{{MODULE}}", naming.CLI(noTokenURLSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(noTokenURLDir, "internal", "client", "google_service_account_redirect_test.go"), []byte(hangTest), 0o644))
	runGoCommand(t, noTokenURLDir, "test", "./internal/client", "-run", "TestGoogleServiceAccountSameHostTokenRedirectDoesNotHang", "-count=1")
}
