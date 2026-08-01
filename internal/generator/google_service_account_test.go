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

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	assert.Contains(t, configSrc, "GoogleImpersonate")
	assert.Contains(t, configSrc, "GoogleScopes")
	assert.Contains(t, configSrc, "GOOGLE_OAUTH_ACCESS_TOKEN")

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, "context.WithTimeout")
	assert.Contains(t, clientSrc, "googleServiceAccountAuthHeader")
	assert.Contains(t, clientSrc, `return "Bearer " + token.AccessToken`)

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
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

`, keyPath)
	runtimeTest = strings.ReplaceAll(runtimeTest, "{{MODULE}}", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "google_service_account_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestMintGoogleServiceAccountToken", "-count=1")

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
}

`, `{"access_token":"runtime-token","token_type":"Bearer","expires_in":3600}`, keyPath)
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
}
