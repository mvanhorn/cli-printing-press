package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientRejectsPlaceholderConfigTokenBeforeRequest(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("placeholder-auth")
	apiSpec.Auth.Type = "bearer_token"
	apiSpec.Auth.EnvVars = []string{"PLACEHOLDER_AUTH_TOKEN"}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"placeholder-auth-pp-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestPlaceholderAccessTokenIsRejectedBeforeRequest(t *testing.T) {
	for _, token := range []string{"<PAT>", "<your-token>", "YOUR_TOKEN_HERE"} {
		t.Run(token, func(t *testing.T) {
			var calls int
			cfg := &config.Config{
				BaseURL:     "https://api.example.invalid",
				AccessToken: token,
				Path:        "/tmp/placeholder-auth-config.toml",
			}
			c := New(cfg, time.Second, 0)
			c.NoCache = true
			c.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				t.Fatal("placeholder auth should be rejected before transport")
				return nil, nil
			})}

			_, err := c.Get("/items", nil)
			if !errors.Is(err, ErrPlaceholderCredential) {
				t.Fatalf("expected placeholder auth error for %q, got %v", token, err)
			}
			if calls != 0 {
				t.Fatalf("transport called %d time(s), want 0", calls)
			}
			msg := err.Error()
			for _, want := range []string{"placeholder credential", "PLACEHOLDER_AUTH_TOKEN", "auth set-token"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("placeholder auth error %q missing %q", msg, want)
				}
			}
		})
	}
}

func TestPlaceholderEnvTokenIsRejectedBeforeRequest(t *testing.T) {
	t.Setenv("PLACEHOLDER_AUTH_TOKEN", "<PAT>")
	var calls int
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.BaseURL = "https://api.example.invalid"
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("placeholder auth should be rejected before transport")
		return nil, nil
	})}

	_, err = c.Get("/items", nil)
	if !errors.Is(err, ErrPlaceholderCredential) {
		t.Fatalf("expected placeholder auth error, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("transport called %d time(s), want 0", calls)
	}
}

func TestPlaceholderAccessTokenFailsBeforeCacheHit(t *testing.T) {
	cfg := &config.Config{
		BaseURL:     "https://api.example.invalid",
		AccessToken: "<PAT>",
		Path:        "/tmp/placeholder-auth-config.toml",
	}
	c := New(cfg, time.Second, 0)
	c.cacheDir = t.TempDir()
	c.writeCache("/items", nil, []byte(` + "`" + `{"cached":true}` + "`" + `))

	_, err := c.Get("/items", nil)
	if !errors.Is(err, ErrPlaceholderCredential) {
		t.Fatalf("expected placeholder auth error before cache hit, got %v", err)
	}
}

func TestRealTokensReachTransport(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
	}{
		{
			name: "saved-token",
			cfg: &config.Config{
				BaseURL:     "https://api.example.invalid",
				AccessToken: "real-saved-token",
			},
		},
		{
			name: "env-token",
			cfg: &config.Config{
				BaseURL:              "https://api.example.invalid",
				PlaceholderAuthToken: "real-env-token",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			c := New(tt.cfg, time.Second, 0)
			c.NoCache = true
			c.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if got := req.Header.Get("Authorization"); !strings.Contains(got, "real-") {
					t.Fatalf("Authorization = %q, want real token", got)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(` + "`" + `{"ok":true}` + "`" + `)),
					Request:    req,
				}, nil
			})}

			if _, err := c.Get("/items", nil); err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if calls != 1 {
				t.Fatalf("transport called %d time(s), want 1", calls)
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "placeholder_auth_test.go"), []byte(clientTest), 0o644))

	const cliTest = `package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"placeholder-auth-pp-cli/internal/client"
)

func TestPlaceholderAuthErrorClassifiesAsExitCode4(t *testing.T) {
	err := classifyAPIError(fmt.Errorf("%w configured in /tmp/config.toml; set a real token with: export PLACEHOLDER_AUTH_TOKEN=<your-token> or placeholder-auth-pp-cli auth set-token <token>", client.ErrPlaceholderCredential), &rootFlags{})
	if ExitCode(err) != 4 {
		t.Fatalf("placeholder auth error exit code = %d, want 4", ExitCode(err))
	}
	if !errors.Is(err, client.ErrPlaceholderCredential) {
		t.Fatalf("classified error lost placeholder sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "PLACEHOLDER_AUTH_TOKEN") || !strings.Contains(err.Error(), "auth set-token") {
		t.Fatalf("classified error lost setup guidance: %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "placeholder_auth_test.go"), []byte(cliTest), 0o644))

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	require.Contains(t, string(syncSrc), "var firstPlaceholderErr error")
	require.Contains(t, string(syncSrc), "errors.Is(res.Err, client.ErrPlaceholderCredential)")
	require.Contains(t, string(syncSrc), "return classifyAPIError(firstPlaceholderErr, flags)")

	runGoCommand(t, outputDir, "test", "./internal/client", "./internal/cli", "-run", "TestPlaceholder", "-count=1")
}

func TestGeneratedClientRejectsBasicAuthPlaceholdersBeforeRequest(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("placeholder-basic")
	apiSpec.Auth.Type = "api_key"
	apiSpec.Auth.Format = "Basic {username}:{password}"
	apiSpec.Auth.EnvVarSpecs = []spec.AuthEnvVar{
		{Name: "PLACEHOLDER_BASIC_USERNAME", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
		{Name: "PLACEHOLDER_BASIC_PASSWORD", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"errors"
	"testing"
	"time"

	"placeholder-basic-pp-cli/internal/config"
)

func TestBasicAuthPlaceholderIsRejectedBeforeRequest(t *testing.T) {
	cfg := &config.Config{
		BaseURL:                  "https://api.example.invalid",
		PlaceholderBasicUsername: "<PAT>",
		PlaceholderBasicPassword: "secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true

	_, err := c.Get("/items", nil)
	if !errors.Is(err, ErrPlaceholderCredential) {
		t.Fatalf("expected placeholder auth error, got %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "placeholder_basic_test.go"), []byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestBasicAuthPlaceholder", "-count=1")
}

func TestGeneratedClientRejectsClientCredentialsPlaceholdersBeforeMint(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("placeholder-cc")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "PLACEHOLDER_CC_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "PLACEHOLDER_CC_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
		},
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://api.example.invalid/token",
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"placeholder-cc-pp-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestClientCredentialsPlaceholdersRejectedBeforeMint(t *testing.T) {
	var calls int
	cfg := &config.Config{
		BaseURL:      "https://api.example.invalid",
		ClientID:     "<PAT>",
		ClientSecret: "secret",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("placeholder client credentials should be rejected before token mint")
		return nil, nil
	})}

	_, err := c.Get("/items", nil)
	if !errors.Is(err, ErrPlaceholderCredential) {
		t.Fatalf("expected placeholder auth error, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("transport called %d time(s), want 0", calls)
	}
}

func TestClientCredentialsPlaceholdersRejectedBeforeCacheHit(t *testing.T) {
	cfg := &config.Config{
		BaseURL:      "https://api.example.invalid",
		ClientID:     "<PAT>",
		ClientSecret: "secret",
	}
	c := New(cfg, time.Second, 0)
	c.cacheDir = t.TempDir()
	c.writeCache("/items", nil, []byte(` + "`" + `{"cached":true}` + "`" + `))

	_, err := c.Get("/items", nil)
	if !errors.Is(err, ErrPlaceholderCredential) {
		t.Fatalf("expected placeholder auth error before cache hit, got %v", err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "placeholder_cc_test.go"), []byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestClientCredentialsPlaceholders", "-count=1")
}

func TestGeneratedClientRejectsRefreshPlaceholdersBeforeRefresh(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("placeholder-oauth-refresh")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		AuthorizationURL: "https://api.example.invalid/auth",
		TokenURL:         "https://api.example.invalid/token",
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"placeholder-oauth-refresh-pp-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestRefreshPlaceholdersRejectedBeforeRefresh(t *testing.T) {
	var calls int
	cfg := &config.Config{
		BaseURL:      "https://api.example.invalid",
		AccessToken:  "expired-real-token",
		RefreshToken: "<PAT>",
		TokenExpiry:  time.Now().Add(-time.Minute),
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		t.Fatal("placeholder refresh token should be rejected before token refresh")
		return nil, nil
	})}

	_, err := c.Get("/items", nil)
	if !errors.Is(err, ErrPlaceholderCredential) {
		t.Fatalf("expected placeholder auth error, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("transport called %d time(s), want 0", calls)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "placeholder_refresh_test.go"), []byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestRefreshPlaceholders", "-count=1")
}
