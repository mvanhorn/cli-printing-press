package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthTokenHTTPClientOmittedWithoutTokenURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("no-token-url")
	outputDir := filepath.Join(t.TempDir(), "no-token-url-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", "oauth_token.go"))
	require.ErrorIs(t, err, os.ErrNotExist)
	client := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.NotContains(t, client, "OAuthTokenHTTPClient")
}

func TestOAuthTokenHTTPClientEmittedForAuthorizationURLOnly(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("implicit-oauth")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		AuthorizationURL: "https://petstore.example/oauth/authorize",
		EnvVars:          []string{"IMPLICIT_OAUTH_TOKEN"},
	}
	outputDir := filepath.Join(t.TempDir(), "implicit-oauth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	helper := readGeneratedFile(t, outputDir, "internal", "cliutil", "oauth_token.go")
	assert.Contains(t, helper, "func OAuthTokenHTTPClient")
	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, auth, "cliutil.OAuthTokenHTTPClient")
}

func TestOAuthTokenHTTPClientEmittedForClientCredentialsWithoutTokenURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cc-no-token-url")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "CC_NO_TOKEN_URL_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "CC_NO_TOKEN_URL_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "cc-no-token-url-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	helper := readGeneratedFile(t, outputDir, "internal", "cliutil", "oauth_token.go")
	assert.Contains(t, helper, "func OAuthTokenHTTPClient")
	assert.Contains(t, helper, "func oauthTokenCanonicalPort")
	client := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, client, "cliutil.OAuthTokenHTTPClient")
}

func TestOAuthTokenExchangeSameOriginRedirectPolicy(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tokredir")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "oauth2",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://auth.example.com/oauth/token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "TOKREDIR_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: false},
			{Name: "TOKREDIR_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "tokredir-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	helper := readGeneratedFile(t, outputDir, "internal", "cliutil", "oauth_token.go")
	assert.Contains(t, helper, "func OAuthTokenHTTPClient(base *http.Client) *http.Client")
	assert.Contains(t, helper, "c.CheckRedirect = oauthTokenSameOriginRedirect")
	assert.Contains(t, helper, "func oauthTokenCanonicalPort")
	assert.NotContains(t, helper, "CheckRedirect = nil")
	assert.NotContains(t, helper, "clone := *base")

	client := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	mintBlock := generatedSourceBlock(t, client, "func (c *Client) mintClientCredentials", "func (c *Client) refreshAccessToken")
	refreshBlock := generatedSourceBlock(t, client, "func (c *Client) refreshAccessToken", "type binaryResponseEnvelope")
	assert.Contains(t, mintBlock, "cliutil.OAuthTokenHTTPClient(c.HTTPClient).Do(req)")
	assert.NotContains(t, mintBlock, "c.HTTPClient.Do(req)")
	assert.Contains(t, refreshBlock, "cliutil.OAuthTokenHTTPClient(c.HTTPClient).Do(req)")
	assert.NotContains(t, refreshBlock, "c.HTTPClient.Do(req)")
	assert.Contains(t, client, "httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {",
		"shared API CheckRedirect must remain so nonce-bound API redirects still work")

	auth := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, auth, "cliutil.OAuthTokenHTTPClient(httpClient).Do(req)")

	const runtimeTest = `package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tokredir-pp-cli/internal/config"
)

func TestRefreshAndMintRefuseCrossHostRedirects(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusFound, http.StatusSeeOther} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			const secret = "body-borne-client-secret"
			var hits atomic.Int64
			var leaked atomic.Bool
			evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), secret) {
					leaked.Store(true)
				}
				if _, pass, ok := r.BasicAuth(); ok && pass == secret {
					leaked.Store(true)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer evil.Close()

			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", evil.URL+"/stolen")
				w.WriteHeader(status)
			}))
			defer origin.Close()

			cfg := &config.Config{
				TokenURL:     origin.URL + "/token",
				RefreshToken: "rt-1",
				ClientID:     "client-id",
				ClientSecret: secret,
				Path:         filepath.Join(t.TempDir(), "config.toml"),
			}
			c := &Client{Config: cfg, HTTPClient: origin.Client()}

			if err := c.refreshAccessToken(context.Background()); err == nil {
				t.Fatal("refreshAccessToken followed a cross-host redirect")
			}
			if hits.Load() != 0 || leaked.Load() {
				t.Fatalf("refresh leaked client_secret: hits=%d leaked=%v", hits.Load(), leaked.Load())
			}

			if err := c.mintClientCredentials(context.Background(), "client-id", secret); err == nil {
				t.Fatal("mintClientCredentials followed a cross-host redirect")
			}
			if hits.Load() != 0 || leaked.Load() {
				t.Fatalf("mint leaked client secret: hits=%d leaked=%v", hits.Load(), leaked.Load())
			}
		})
	}
}

func TestRefreshFollowsSameOrigin307(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/token-real")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/token-real", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_secret") != "same-origin-secret" {
			t.Errorf("client_secret = %q", r.Form.Get("client_secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"access_token":"refreshed","expires_in":3600}` + "`" + `)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &config.Config{
		TokenURL:     srv.URL + "/token",
		RefreshToken: "rt-1",
		ClientID:     "client-id",
		ClientSecret: "same-origin-secret",
		Path:         filepath.Join(t.TempDir(), "config.toml"),
	}
	c := &Client{Config: cfg, HTTPClient: srv.Client()}
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatalf("same-origin refresh: %v", err)
	}
	if cfg.AccessToken != "refreshed" {
		t.Fatalf("AccessToken = %q", cfg.AccessToken)
	}
}

func TestSharedAPICheckRedirectStillFollowsCrossHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cfg := &config.Config{
		BaseURL: "https://api.example.com",
		Path:    filepath.Join(t.TempDir(), "config.toml"),
	}
	c := New(cfg, time.Second, 0)
	viaURL, _ := url.Parse("https://api.example.com/start")
	targetURL, _ := url.Parse("https://evil.example.net/done")
	via := &http.Request{URL: viaURL}
	req := &http.Request{URL: targetURL, Header: http.Header{"Authorization": {"Bearer tok"}}}
	if err := c.HTTPClient.CheckRedirect(req, []*http.Request{via}); err != nil {
		t.Fatalf("shared API CheckRedirect must still allow cross-host hops: %v", err)
	}
}
`

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "oauth_token_redirect_runtime_test.go"),
		[]byte(runtimeTest),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "./internal/client", "-count=1")
}
