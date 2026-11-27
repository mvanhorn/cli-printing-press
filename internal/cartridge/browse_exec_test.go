package cartridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteViaOpenCodeBrowseMock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	isolateEnv(t)

	tmp := t.TempDir()
	mockBrowse := filepath.Join(tmp, "browse")
	// Mock OpenCode `browse execute` returning a tab header line + JSON fetch result.
	script := `#!/bin/sh
if [ "$1" = "execute" ]; then
  printf 'tab: pp-exec-1\n{"status":200,"text":"{\\"items\\":[{\\"id\\":\\"c1\\",\\"name\\":\\"Chocolate Milk\\"}]}"}\n'
  exit 0
fi
exit 0
`
	require.NoError(t, os.WriteFile(mockBrowse, []byte(script), 0o755))
	t.Setenv("PP_BROWSE_BIN", mockBrowse)

	c := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "chrome-shop",
		Version:     "1.0.0",
		Description: "Chrome logged-in shop cartridge",
		Homepage:    "https://www.shop.example",
		Trust:       TrustPolicy{AllowedHosts: []string{"www.shop.example", "api.shop.example"}, ReadOnly: true},
		Auth:        AuthConfig{Strategy: AuthStrategyBrowserCookie, Domains: []string{".shop.example"}},
		Commands: map[string]CommandSpec{
			"search": {
				Driver:        DriverHTTP,
				Method:        "GET",
				URL:           "https://api.shop.example/v1/search",
				Params:        map[string]ParamSpec{"q": {Type: "string", Required: true}},
				Query:         map[string]string{"q": "{q}"},
				Extract:       "$.items[*]",
				CompactFields: []string{"id", "name"},
			},
		},
	}
	require.NoError(t, c.Validate())

	res, err := Execute(context.Background(), c, "search", RunOptions{
		Params:    map[string]string{"q": "chocolate milk"},
		Compact:   true,
		ViaBrowse: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, res.HTTPStatus)
	assert.Equal(t, "opencode-browse", res.AuthSource)
	assert.Equal(t, 1, res.RowCount)
	rows := res.Data.([]any)
	assert.Equal(t, "Chocolate Milk", rows[0].(map[string]any)["name"])

	// Disallowed host must still be blocked before touching browse.
	cBad := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "bad-host",
		Version:     "1.0.0",
		Description: "Bad host test",
		Trust:       TrustPolicy{AllowedHosts: []string{"good.example"}, ReadOnly: true},
		Commands: map[string]CommandSpec{
			"get": {Driver: DriverHTTP, Method: "GET", URL: "https://evil.example/steal"},
		},
	}
	_, err = Execute(context.Background(), cBad, "get", RunOptions{ViaBrowse: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by trust.allowed_hosts")
}

func TestExecuteViaOpenCodeBrowseAppliesAuthAndBlocksRedirects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	isolateEnv(t)

	tmp := t.TempDir()
	capturedCodePath := filepath.Join(tmp, "code.js")
	mockBrowse := filepath.Join(tmp, "browse")
	script := `#!/bin/sh
if [ "$1" = "execute" ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--code" ]; then
      printf '%s' "$2" > "` + capturedCodePath + `"
      break
    fi
    shift
  done
  if [ "$MOCK_BLOCK_REDIRECT" = "1" ]; then
    printf '{"blocked":"redirect to disallowed host evil.example blocked by trust.allowed_hosts"}\n'
    exit 0
  fi
  printf '{"status":200,"text":"{\\"ok\\":true}"}\n'
  exit 0
fi
exit 0
`
	require.NoError(t, os.WriteFile(mockBrowse, []byte(script), 0o755))
	t.Setenv("PP_BROWSE_BIN", mockBrowse)
	t.Setenv("SHOP_API_KEY", "k_secret_42")

	// 1. Non-cookie credentials (header + query param) must be applied before browse fetch.
	cKey := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "keyed-shop",
		Version:     "1.0.0",
		Description: "API key cartridge",
		Trust:       TrustPolicy{AllowedHosts: []string{"api.shop.example"}, ReadOnly: true},
		Auth: AuthConfig{
			Strategy: AuthStrategyEnvAPIKey,
			EnvVar:   "SHOP_API_KEY",
			Header:   "X-Api-Key",
		},
		Commands: map[string]CommandSpec{
			"status": {Driver: DriverHTTP, Method: "GET", URL: "https://api.shop.example/v1/status"},
		},
	}
	require.NoError(t, cKey.Validate())
	res, err := Execute(context.Background(), cKey, "status", RunOptions{ViaBrowse: true})
	require.NoError(t, err)
	assert.Equal(t, "opencode-browse+env:SHOP_API_KEY", res.AuthSource)

	rawJS, err := os.ReadFile(capturedCodePath)
	require.NoError(t, err)
	js := string(rawJS)
	assert.Contains(t, js, "k_secret_42")
	assert.Contains(t, js, "X-Api-Key")
	assert.Contains(t, js, `redirect: "manual"`)
	assert.Contains(t, js, `page.route`)
	assert.Contains(t, js, `allowed_hosts`)

	// Query-param API key must also be injected into the target URL.
	cQuery := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "query-shop",
		Version:     "1.0.0",
		Description: "Query API key cartridge",
		Trust:       TrustPolicy{AllowedHosts: []string{"api.shop.example"}, ReadOnly: true},
		Auth: AuthConfig{
			Strategy:   AuthStrategyEnvAPIKey,
			EnvVar:     "SHOP_API_KEY",
			QueryParam: "api_key",
		},
		Commands: map[string]CommandSpec{
			"status": {Driver: DriverHTTP, Method: "GET", URL: "https://api.shop.example/v1/status"},
		},
	}
	require.NoError(t, cQuery.Validate())
	_, err = Execute(context.Background(), cQuery, "status", RunOptions{ViaBrowse: true})
	require.NoError(t, err)
	rawJS, err = os.ReadFile(capturedCodePath)
	require.NoError(t, err)
	assert.Contains(t, string(rawJS), "api_key=k_secret_42")

	// 2. Blocked redirect reported by the browser sandbox maps to ClassBlocked.
	t.Setenv("MOCK_BLOCK_REDIRECT", "1")
	_, err = Execute(context.Background(), cKey, "status", RunOptions{ViaBrowse: true})
	require.Error(t, err)
	ee, ok := errors.AsType[*ExecError](err)
	require.True(t, ok)
	assert.Equal(t, ClassBlocked, ee.Class)
	assert.Contains(t, ee.Message, "evil.example")
}
