package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestTemplatedAuthTokenURLResolvesThroughBuildURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-oauth")
	apiSpec.BaseURL = "https://{tenant}.{domain}/api"
	apiSpec.EndpointTemplateVarDefaults = map[string]string{
		"tenant": "demo",
		"domain": "example.com",
	}
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://{tenant}.{domain}/auth/token",
		EnvVars:     []string{"TENANT_OAUTH_CLIENT_ID", "TENANT_OAUTH_CLIENT_SECRET"},
	}

	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), "tenant-oauth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"tenant-oauth-pp-cli/internal/config"
)

type captureRoundTripper struct {
	got *http.Request
}

func (c *captureRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	c.got = r.Clone(r.Context())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(` + "`" + `{"access_token":"minted","expires_in":3600}` + "`" + `)),
		Request:    r,
	}, nil
}

func TestMintResolvesTemplatedTokenURL(t *testing.T) {
	rt := &captureRoundTripper{}
	cfg := &config.Config{
		Path: filepath.Join(t.TempDir(), "config.toml"),
		TemplateVars: map[string]string{
			"tenant": "acme",
			"domain": "example.com",
		},
	}
	c := &Client{Config: cfg, HTTPClient: &http.Client{Transport: rt}}
	if err := c.mintClientCredentials(context.Background(), "client-id", "client-secret"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if rt.got == nil {
		t.Fatal("token endpoint was not called")
	}
	if strings.Contains(rt.got.URL.String(), "{") {
		t.Fatalf("token URL still contains a placeholder: %s", rt.got.URL.String())
	}
	if rt.got.URL.Host != "acme.example.com" || rt.got.URL.Path != "/auth/token" {
		t.Fatalf("token URL = %s, want https://acme.example.com/auth/token", rt.got.URL.String())
	}
}

func TestMintRejectsUnresolvedTemplatedTokenURL(t *testing.T) {
	rt := &captureRoundTripper{}
	cfg := &config.Config{
		Path:         filepath.Join(t.TempDir(), "config.toml"),
		TemplateVars: map[string]string{},
	}
	c := &Client{Config: cfg, HTTPClient: &http.Client{Transport: rt}}
	if err := c.mintClientCredentials(context.Background(), "client-id", "client-secret"); err == nil {
		t.Fatal("mintClientCredentials() error = nil, want unresolved template var")
	}
	if rt.got != nil {
		t.Fatalf("token endpoint was called with %s", rt.got.URL.String())
	}
}

func TestRefreshResolvesTemplatedTokenURL(t *testing.T) {
	rt := &captureRoundTripper{}
	cfg := &config.Config{
		Path:         filepath.Join(t.TempDir(), "config.toml"),
		ClientID:     "client-id",
		RefreshToken: "refresh-token",
		TemplateVars: map[string]string{
			"tenant": "acme",
			"domain": "example.com",
		},
	}
	c := &Client{Config: cfg, HTTPClient: &http.Client{Transport: rt}}
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatalf("refreshAccessToken() error = %v", err)
	}
	if rt.got == nil {
		t.Fatal("token endpoint was not called")
	}
	if strings.Contains(rt.got.URL.String(), "{") {
		t.Fatalf("token URL still contains a placeholder: %s", rt.got.URL.String())
	}
	if rt.got.URL.Host != "acme.example.com" || rt.got.URL.Path != "/auth/token" {
		t.Fatalf("token URL = %s, want https://acme.example.com/auth/token", rt.got.URL.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "templated_token_url_test.go"), []byte(clientTest), 0o644))

	const loginTest = `package cli

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type captureRoundTripper struct {
	got *http.Request
}

func (c *captureRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	c.got = r.Clone(r.Context())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(` + "`" + `{"access_token":"minted","expires_in":3600}` + "`" + `)),
		Request:    r,
	}, nil
}

func TestAuthLoginResolvesTemplatedTokenURL(t *testing.T) {
	rt := &captureRoundTripper{}
	prev := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: rt}
	t.Cleanup(func() { http.DefaultClient = prev })

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	t.Setenv("TENANT_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("TENANT_OAUTH_CLIENT_SECRET", "client-secret")
	t.Setenv("TENANT_OAUTH_TENANT", "acme")
	t.Setenv("TENANT_OAUTH_DOMAIN", "example.com")

	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "login"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth login error = %v; output:\n%s", err, out.String())
	}
	if rt.got == nil {
		t.Fatal("token endpoint was not called")
	}
	if strings.Contains(rt.got.URL.String(), "{") {
		t.Fatalf("token URL still contains a placeholder: %s", rt.got.URL.String())
	}
	if rt.got.URL.Host != "acme.example.com" || rt.got.URL.Path != "/auth/token" {
		t.Fatalf("token URL = %s, want https://acme.example.com/auth/token", rt.got.URL.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "templated_token_url_test.go"), []byte(loginTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "./internal/cli", "-run", "TemplatedTokenURL")
}

func TestTemplatedDeviceAuthURLsCompile(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-device")
	apiSpec.BaseURL = "https://{tenant}.{domain}/api"
	apiSpec.EndpointTemplateVarDefaults = map[string]string{
		"tenant": "demo",
		"domain": "example.com",
	}
	apiSpec.Auth = spec.AuthConfig{
		Type:                   "oauth2",
		Header:                 "Authorization",
		Format:                 "Bearer {token}",
		OAuth2Grant:            spec.OAuth2GrantDeviceCode,
		DeviceAuthorizationURL: "https://{tenant}.{domain}/auth/device",
		TokenURL:               "https://{tenant}.{domain}/auth/token",
		Scopes:                 []string{"read"},
		EnvVars:                []string{"TENANT_DEVICE_CLIENT_ID"},
	}
	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), "tenant-device-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^$")
}

func TestTemplatedAuthorizationCodeTokenURLCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-authcode")
	apiSpec.BaseURL = "https://{tenant}.{domain}/api"
	apiSpec.EndpointTemplateVarDefaults = map[string]string{
		"tenant": "demo",
		"domain": "example.com",
	}
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://{tenant}.{domain}/oauth/authorize",
		TokenURL:         "https://{tenant}.{domain}/auth/token",
		EnvVars:          []string{"TENANT_AUTHCODE_CLIENT_ID", "TENANT_AUTHCODE_CLIENT_SECRET"},
	}
	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), "tenant-authcode-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "test", "./internal/cli", "./internal/client", "-run", "^$")
}
