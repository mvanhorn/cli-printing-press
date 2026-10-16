package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestDryRunSkipsClientCredentialsMint(t *testing.T) {
	t.Parallel()

	apiSpec := oauthDryRunSpec("ccdry", spec.OAuth2GrantClientCredentials, "")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, "!c.DryRun && c.needsClientCredentialsMint()")

	writeOAuthDryRunTest(t, outputDir, naming.CLI(apiSpec.Name), "TestDryRunSkipsClientCredentialsMint", false)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestDryRunSkipsClientCredentialsMint", "-count=1")
}

func TestDryRunSkipsAccessTokenRefresh(t *testing.T) {
	t.Parallel()

	apiSpec := oauthDryRunSpec("acdry", "", "https://api.example.com/oauth/authorize")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, "!c.DryRun && c.Config.AccessToken")

	writeOAuthDryRunTest(t, outputDir, naming.CLI(apiSpec.Name), "TestDryRunSkipsAccessTokenRefresh", true)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestDryRunSkipsAccessTokenRefresh", "-count=1")
}

func oauthDryRunSpec(name, grant, authorizationURL string) *spec.APISpec {
	return &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:             "bearer_token",
			Header:           "Authorization",
			Format:           "Bearer {token}",
			OAuth2Grant:      grant,
			AuthorizationURL: authorizationURL,
			TokenURL:         "https://api.example.com/oauth/token",
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/" + name + "-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}}},
		},
	}
}

func writeOAuthDryRunTest(t *testing.T, outputDir, module, testName string, refresh bool) {
	t.Helper()
	accessToken := ""
	refreshToken := ""
	if refresh {
		accessToken = "stale"
		refreshToken = "refresh-me"
	}
	body := fmt.Sprintf(`package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"%s/internal/config"
)

func %s(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	tokenHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			tokenHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(%q))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	credsDir := filepath.Join(home, ".local", "share", "%s")
	if err := os.MkdirAll(credsDir, 0o700); err != nil {
		t.Fatalf("mkdir creds: %%v", err)
	}
	credsPath := filepath.Join(credsDir, "credentials.toml")
	sentinel := []byte("sentinel-credentials")
	if err := os.WriteFile(credsPath, sentinel, 0o600); err != nil {
		t.Fatalf("write sentinel: %%v", err)
	}
	configPath := filepath.Join(home, "config.toml")

	cfg := &config.Config{
		BaseURL:      srv.URL,
		TokenURL:     srv.URL + "/oauth/token",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		AccessToken:  %q,
		RefreshToken: %q,
		Path:         configPath,
	}
	if %t {
		cfg.TokenExpiry = time.Now().Add(-time.Minute)
	}
	c := New(cfg, 5*time.Second, 0)
	c.DryRun = true
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatalf("dry-run get: %%v", err)
	}
	if tokenHits != 0 {
		t.Fatalf("dry-run token requests = %%d, want 0", tokenHits)
	}
	got, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("read credentials: %%v", err)
	}
	if string(got) != string(sentinel) {
		t.Fatalf("dry-run rewrote credentials: %%q", got)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote config: %%v", err)
	}

	c.DryRun = false
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatalf("live get: %%v", err)
	}
	if tokenHits < 1 {
		t.Fatalf("live token requests = %%d, want at least 1", tokenHits)
	}
	got, err = os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("read live credentials: %%v", err)
	}
	if string(got) == string(sentinel) {
		t.Fatal("live call left the credentials sentinel in place")
	}
}
`, module, testName, `{"access_token":"minted","refresh_token":"rotated","expires_in":3600}`, module, accessToken, refreshToken, refresh)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "dry_run_oauth_runtime_test.go"), []byte(body), 0o644))
}
