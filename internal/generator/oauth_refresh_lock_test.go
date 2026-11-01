package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestOAuthRefreshFileLockGenerated(t *testing.T) {
	apiSpec := minimalSpec("oauth-refresh-lock")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		OAuth2Grant:      spec.OAuth2GrantAuthorizationCode,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		EnvVars:          []string{"OAUTH_REFRESH_LOCK_TOKEN"},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile("testdata/oauth_refresh_lock/client_test.go")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "oauth_refresh_lock_test.go"), src, 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestOAuthRefreshFileLock$", "-count=1")
	requireGeneratedCompiles(t, outputDir)

	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			env := []string{"GOOS=" + goos, "GOARCH=amd64", "CGO_ENABLED=0"}
			runGoCommandWithEnv(t, outputDir, env, "vet", "-mod=mod", "./internal/client", "./internal/config")
			runGoCommandWithEnv(t, outputDir, env, "build", "-mod=mod", "./internal/client", "./internal/config")
		})
	}
}

func TestOAuth2RefreshEmptyAccessUsesEarlyWindow(t *testing.T) {
	apiSpec := minimalSpec("oauth-refresh-empty")
	apiSpec.Auth = spec.AuthConfig{
		Type:     "oauth2_refresh",
		Header:   "Authorization",
		Format:   "Bearer {token}",
		TokenURL: "https://example.com/oauth/token",
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "empty_access_refresh_test.go"), []byte(oauth2RefreshEmptyAccessTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestOAuth2RefreshEmptyAccess$", "-count=1")
}

const oauth2RefreshEmptyAccessTest = `package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"oauth-refresh-empty-pp-cli/internal/cliutil"
	"oauth-refresh-empty-pp-cli/internal/cliutil/testenv"
	"oauth-refresh-empty-pp-cli/internal/config"
)

func TestOAuth2RefreshEmptyAccess(t *testing.T) {
	t.Run("empty access refreshes", func(t *testing.T) {
		assertEmptyAccessRefresh(t, "", time.Time{}, 1, "Bearer access-new")
	})
	t.Run("current access is left alone", func(t *testing.T) {
		assertEmptyAccessRefresh(t, "access-old", time.Now().Add(2*time.Minute), 0, "Bearer access-old")
	})
}

func assertEmptyAccessRefresh(t *testing.T, access string, expiry time.Time, wantCalls int32, wantAuth string) {
	t.Helper()
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	restore, err := cliutil.SetHomeOverride("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	home := testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir, cliutil.StateDir, cliutil.CacheDir)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localapp"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))

	var calls atomic.Int32
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, ` + "`" + `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}` + "`" + `)
			return
		}
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `{"ok":true}` + "`" + `)
	}))
	t.Cleanup(srv.Close)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = srv.URL
	cfg.TokenURL = srv.URL + "/token"
	if err := cfg.SaveTokens("client-id", "client-secret-value", access, "refresh-old", expiry); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	loaded.BaseURL = srv.URL
	loaded.TokenURL = srv.URL + "/token"
	c := New(loaded, 5*time.Second, 0)
	c.NoCache = true
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != wantCalls {
		t.Fatalf("token calls=%d, want %d", calls.Load(), wantCalls)
	}
	if got := gotAuth.Load(); got != wantAuth {
		t.Fatalf("authorization=%v, want %s", got, wantAuth)
	}
}
`
