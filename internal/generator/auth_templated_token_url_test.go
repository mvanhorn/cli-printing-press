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

func TestTemplatedAuthTokenURLEscapesPathSegment(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("path-oauth")
	apiSpec.BaseURL = "https://api.example.net/tenants/{tenant}/api"
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://api.example.net/tenants/{tenant}/token",
		EnvVars:     []string{"PATH_OAUTH_CLIENT_ID", "PATH_OAUTH_CLIENT_SECRET"},
	}
	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), "path-oauth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"path-oauth-pp-cli/internal/config"
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

func TestMintEscapesPathSegmentTemplateValue(t *testing.T) {
	rt := &captureRoundTripper{}
	cfg := &config.Config{
		Path: filepath.Join(t.TempDir(), "config.toml"),
		TemplateVars: map[string]string{
			"tenant": "acme/evil",
		},
	}
	c := &Client{Config: cfg, HTTPClient: &http.Client{Transport: rt}}
	if err := c.mintClientCredentials(context.Background(), "client-id", "client-secret"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if rt.got == nil {
		t.Fatal("token endpoint was not called")
	}
	if rt.got.URL.Host != "api.example.net" || rt.got.URL.EscapedPath() != "/tenants/acme%2Fevil/token" {
		t.Fatalf("token URL = %s escaped path %s, want host api.example.net path /tenants/acme%%2Fevil/token", rt.got.URL.String(), rt.got.URL.EscapedPath())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "templated_path_escape_test.go"), []byte(clientTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestMintEscapesPathSegmentTemplateValue")
}

func TestTemplatedAuthTokenURLEscapesGlobalPathSegmentOnce(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("global-path-oauth")
	apiSpec.BaseURL = "https://api.example.net/tenants/{tenant}/api"
	apiSpec.GlobalPathTemplateVars = []string{"tenant"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Manage items",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/tenants/{tenant}/items", Description: "List items"},
		},
	}
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://api.example.net/tenants/{tenant}/token",
		EnvVars:     []string{"GLOBAL_PATH_OAUTH_CLIENT_ID", "GLOBAL_PATH_OAUTH_CLIENT_SECRET"},
	}
	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), "global-path-oauth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	urlSrc := readGeneratedFile(t, outputDir, "internal", "client", "url.go")
	require.Contains(t, urlSrc, "globalPathTemplateVars[key]")

	const clientTest = `package client

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"global-path-oauth-pp-cli/internal/config"
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

func TestMintEscapesGlobalPathSegmentOnce(t *testing.T) {
	rt := &captureRoundTripper{}
	cfg := &config.Config{
		Path: filepath.Join(t.TempDir(), "config.toml"),
		TemplateVars: map[string]string{
			"tenant": "acme/evil",
		},
	}
	c := &Client{Config: cfg, HTTPClient: &http.Client{Transport: rt}}
	if err := c.mintClientCredentials(context.Background(), "client-id", "client-secret"); err != nil {
		t.Fatalf("mintClientCredentials() error = %v", err)
	}
	if rt.got == nil {
		t.Fatal("token endpoint was not called")
	}
	if strings.Contains(rt.got.URL.EscapedPath(), "%252F") {
		t.Fatalf("token path was escaped twice: %s", rt.got.URL.EscapedPath())
	}
	if rt.got.URL.Host != "api.example.net" || rt.got.URL.EscapedPath() != "/tenants/acme%2Fevil/token" {
		t.Fatalf("token URL = %s escaped path %s, want host api.example.net path /tenants/acme%%2Fevil/token", rt.got.URL.String(), rt.got.URL.EscapedPath())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "templated_global_path_escape_test.go"), []byte(clientTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestMintEscapesGlobalPathSegmentOnce")
}

func TestTemplatedOAuth2RefreshAuthCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("refresh-oauth")
	apiSpec.BaseURL = "https://{tenant}.{domain}/api"
	apiSpec.EndpointTemplateVarDefaults = map[string]string{
		"tenant": "demo",
		"domain": "example.com",
	}
	apiSpec.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		Format:   "Bearer {token}",
		TokenURL: "https://{tenant}.{domain}/auth/token",
		EnvVars:  []string{"REFRESH_OAUTH_CLIENT_ID", "REFRESH_OAUTH_CLIENT_SECRET", "REFRESH_OAUTH_REFRESH_TOKEN"},
	}
	require.NoError(t, apiSpec.Validate())
	outputDir := filepath.Join(t.TempDir(), "refresh-oauth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "test", "./internal/cli", "./internal/client", "-run", "^$")
}

func TestTemplatedDeviceAuthURLsResolveAtRuntime(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-device")
	apiSpec.BaseURL = "https://{tenant}.{domain}/api"
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

	const runtimeTest = `package cli

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
	body := ` + "`" + `{"access_token":"minted","expires_in":3600,"device_code":"device-code","user_code":"ABCD-EFGH","verification_uri":"https://example.com/device","expires_in":600,"interval":5}` + "`" + `
	if strings.Contains(r.URL.Path, "/auth/device") {
		body = ` + "`" + `{"device_code":"device-code","user_code":"ABCD-EFGH","verification_uri":"https://example.com/device","expires_in":600,"interval":5}` + "`" + `
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

func useCapture(t *testing.T) *captureRoundTripper {
	t.Helper()
	rt := &captureRoundTripper{}
	prev := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: rt}
	t.Cleanup(func() { http.DefaultClient = prev })
	return rt
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return configPath
}

func assertConcreteHost(t *testing.T, rt *captureRoundTripper, path string) {
	t.Helper()
	if rt.got == nil {
		t.Fatal("endpoint was not called")
	}
	if strings.Contains(rt.got.URL.String(), "{") {
		t.Fatalf("URL still contains a placeholder: %s", rt.got.URL.String())
	}
	if rt.got.URL.Host != "acme.example.com" || rt.got.URL.Path != path {
		t.Fatalf("URL = %s, want host acme.example.com path %s", rt.got.URL.String(), path)
	}
}

func TestDeviceLoginResolvesTemplatedAuthorizationURL(t *testing.T) {
	rt := useCapture(t)
	t.Setenv("TENANT_DEVICE_TENANT", "acme")
	t.Setenv("TENANT_DEVICE_DOMAIN", "example.com")
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", writeConfig(t, ""), "auth", "login", "--device-code", "--poll=false", "--client-id", "client-id"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth login error = %v; output:\n%s", err, out.String())
	}
	assertConcreteHost(t, rt, "/auth/device")
}

func TestDeviceLoginRejectsMissingTemplateValues(t *testing.T) {
	rt := useCapture(t)
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", writeConfig(t, ""), "auth", "login", "--device-code", "--poll=false", "--client-id", "client-id"})
	if err := root.Execute(); err == nil {
		t.Fatalf("auth login error = nil, want missing template var; output:\n%s", out.String())
	}
	if rt.got != nil {
		t.Fatalf("device endpoint was called with %s", rt.got.URL.String())
	}
}

func TestDevicePollResolvesTemplatedTokenURL(t *testing.T) {
	rt := useCapture(t)
	t.Setenv("TENANT_DEVICE_TENANT", "acme")
	t.Setenv("TENANT_DEVICE_DOMAIN", "example.com")
	configPath := writeConfig(t, "")
	pending := configPath + ".device-code.json"
	state := ` + "`" + `{"device_code":"device-code","client_id":"client-id","token_url":"https://{tenant}.{domain}/auth/token","expires_at":"2099-01-01T00:00:00Z"}` + "`" + `
	if err := os.WriteFile(pending, []byte(state), 0o600); err != nil {
		t.Fatalf("writing pending state: %v", err)
	}
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "poll", "--timeout", "5s"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth poll error = %v; output:\n%s", err, out.String())
	}
	assertConcreteHost(t, rt, "/auth/token")
}

func TestDevicePollRejectsMissingTemplateValues(t *testing.T) {
	rt := useCapture(t)
	configPath := writeConfig(t, "")
	pending := configPath + ".device-code.json"
	state := ` + "`" + `{"device_code":"device-code","client_id":"client-id","token_url":"https://{tenant}.{domain}/auth/token","expires_at":"2099-01-01T00:00:00Z"}` + "`" + `
	if err := os.WriteFile(pending, []byte(state), 0o600); err != nil {
		t.Fatalf("writing pending state: %v", err)
	}
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "poll", "--timeout", "5s"})
	if err := root.Execute(); err == nil {
		t.Fatalf("auth poll error = nil, want missing template var; output:\n%s", out.String())
	}
	if rt.got != nil {
		t.Fatalf("token endpoint was called with %s", rt.got.URL.String())
	}
}

func TestDeviceRefreshResolvesTemplatedTokenURL(t *testing.T) {
	rt := useCapture(t)
	t.Setenv("TENANT_DEVICE_TENANT", "acme")
	t.Setenv("TENANT_DEVICE_DOMAIN", "example.com")
	configPath := writeConfig(t, "client_id = \"client-id\"\nrefresh_token = \"refresh-token\"\n")
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "refresh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth refresh error = %v; output:\n%s", err, out.String())
	}
	assertConcreteHost(t, rt, "/auth/token")
}

func TestDeviceRefreshRejectsMissingTemplateValues(t *testing.T) {
	rt := useCapture(t)
	configPath := writeConfig(t, "client_id = \"client-id\"\nrefresh_token = \"refresh-token\"\n")
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "refresh"})
	if err := root.Execute(); err == nil {
		t.Fatalf("auth refresh error = nil, want missing template var; output:\n%s", out.String())
	}
	if rt.got != nil {
		t.Fatalf("token endpoint was called with %s", rt.got.URL.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "templated_device_url_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDevice")
}

func TestTemplatedAuthorizationCodeLoginResolvesURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tenant-authcode")
	apiSpec.BaseURL = "https://{tenant}.{domain}/api"
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

	const loginTest = `package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthLoginResolvesTemplatedAuthorizationURL(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("TENANT_AUTHCODE_CLIENT_ID", "client-id")
	t.Setenv("TENANT_AUTHCODE_TENANT", "acme")
	t.Setenv("TENANT_AUTHCODE_DOMAIN", "example.com")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	root := RootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", configPath, "auth", "login"})
	if err := root.Execute(); err != nil {
		t.Fatalf("auth login error = %v; output:\n%s", err, out.String())
	}
	got := out.String()
	if strings.Contains(got, "{") {
		t.Fatalf("authorize URL still contains a placeholder:\n%s", got)
	}
	if !strings.Contains(got, "would launch: https://acme.example.com/oauth/authorize?") {
		t.Fatalf("authorize URL = %q, want concrete host acme.example.com", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "templated_auth_url_test.go"), []byte(loginTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestAuthLogin")
}
