package cartridge

import (
	"context"
	"encoding/json"
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

func TestExecuteViaOpenCodeBrowseRejectsDirectSecretsAndMatchesHostPatterns(t *testing.T) {
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
    if [ "$1" = "--json" ]; then
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

	// 1. Direct-credential auth strategies must be rejected cleanly so secrets
	// never leak into subprocess CLI arguments (--code).
	for _, strat := range []string{AuthStrategyEnvAPIKey, AuthStrategyEnvBearer, AuthStrategyHeader, AuthStrategyOnePassword} {
		cSecret := &Cartridge{
			Schema:      SchemaVersionV1,
			Name:        "secret-shop",
			Version:     "1.0.0",
			Description: "Direct secret cartridge",
			Trust:       TrustPolicy{AllowedHosts: []string{"api.shop.example"}, ReadOnly: true},
			Auth:        AuthConfig{Strategy: strat, EnvVar: "SHOP_API_KEY"},
			Commands: map[string]CommandSpec{
				"status": {Driver: DriverHTTP, Method: "GET", URL: "https://api.shop.example/v1/status"},
			},
		}
		_, err := Execute(context.Background(), cSecret, "status", RunOptions{ViaBrowse: true})
		require.Error(t, err, "strategy %s must not be allowed with --via browse", strat)
		ee, ok := errors.AsType[*ExecError](err)
		require.True(t, ok)
		assert.Equal(t, ClassUsage, ee.Class)
		assert.Contains(t, ee.Message, "not supported with --via browse")
	}
	_, statErr := os.Stat(capturedCodePath)
	assert.True(t, os.IsNotExist(statErr), "browse must never be invoked for direct-credential auth")

	// 2. Leading-dot and wildcard allowed_hosts patterns work and JS helper
	// matches Go's Cartridge.HostAllowed across all pattern shapes.
	cCookie := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "cookie-shop",
		Version:     "1.0.0",
		Description: "Browser cookie cartridge",
		Trust:       TrustPolicy{AllowedHosts: []string{".shop.example", "*.cdn.example"}, ReadOnly: true},
		Auth:        AuthConfig{Strategy: AuthStrategyBrowserCookie, Domains: []string{".shop.example"}},
		Commands: map[string]CommandSpec{
			"status": {Driver: DriverHTTP, Method: "GET", URL: "https://api.shop.example/v1/status"},
		},
	}
	require.NoError(t, cCookie.Validate())
	res, err := Execute(context.Background(), cCookie, "status", RunOptions{ViaBrowse: true})
	require.NoError(t, err)
	assert.Equal(t, "opencode-browse", res.AuthSource)

	rawPayload, err := os.ReadFile(capturedCodePath)
	require.NoError(t, err)
	var execPayload struct {
		Tab  string `json:"tab"`
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rawPayload, &execPayload))
	js := execPayload.Code
	assert.Contains(t, js, `redirect: "manual"`)
	assert.Contains(t, js, `parseHTTPURLParts`)
	assert.Contains(t, js, `allowed_hosts`)
	assert.Contains(t, js, `a.startsWith(".")`)
	assert.Contains(t, js, `a.startsWith("*.")`)
	assert.Contains(t, js, `h === apex || h.endsWith(a)`)
	assert.Contains(t, js, `h === apex || h.endsWith("." + apex)`)
	assert.NotContains(t, js, `page.route`, "OpenCode browse execute throws on page.route()")

	// Verify Cartridge.HostAllowed parity across leading-dot, wildcard, and exact patterns
	// without executing any host binaries from PATH.
	for _, tc := range []struct {
		host    string
		allowed []string
		want    bool
	}{
		{"api.example.com", []string{".example.com"}, true},
		{"example.com", []string{".example.com"}, true},
		{"notexample.com", []string{".example.com"}, false},
		{"api.example.com", []string{"*.example.com"}, true},
		{"example.com", []string{"*.example.com"}, true},
		{"notexample.com", []string{"*.example.com"}, false},
		{"api.example.com", []string{"api.example.com"}, true},
		{"other.example.com", []string{"api.example.com"}, false},
	} {
		cTest := &Cartridge{Trust: TrustPolicy{AllowedHosts: tc.allowed}}
		assert.Equal(t, tc.want, cTest.HostAllowed(tc.host),
			"HostAllowed mismatch for host=%q allowed=%v", tc.host, tc.allowed)
	}

	// 3. Blocked redirect reported by the browser sandbox maps to ClassBlocked.
	t.Setenv("MOCK_BLOCK_REDIRECT", "1")
	_, err = Execute(context.Background(), cCookie, "status", RunOptions{ViaBrowse: true})
	require.Error(t, err)
	ee, ok := errors.AsType[*ExecError](err)
	require.True(t, ok)
	assert.Equal(t, ClassBlocked, ee.Class)
	assert.Contains(t, ee.Message, "evil.example")

	// 4. browseAllowedOrigins normalizes leading-dot and wildcard patterns.
	assert.Equal(t, []string{"example.com", "*.example.com", "api.other.test"},
		browseAllowedOrigins([]string{".example.com", "*.example.com", "api.other.test"}))
}

func TestExecuteViaOpenCodeBrowseSpillsLargeResponseToBodyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	isolateEnv(t)

	tmp := t.TempDir()
	mockBrowse := filepath.Join(tmp, "browse")
	// Mock `browse execute --json ...`: extract body_file from the embedded req
	// JSON, write a valid JSON payload to it, and return {status:200, body_file:true}.
	script := `#!/bin/sh
if [ "$1" = "execute" ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--json" ]; then
      BODY_FILE=$(printf '%s' "$2" | grep -oE '/[^"\\]*pp-browse-[^"\\]*/body\.txt' | head -n 1)
      if [ -n "$BODY_FILE" ]; then
        printf '{"items":[{"id":"big1","name":"Spilled OK"}]}' > "$BODY_FILE"
        printf 'tab: 99\n\n{"status":200,"body_file":true}\n'
        exit 0
      fi
      break
    fi
    shift
  done
fi
exit 0
`
	require.NoError(t, os.WriteFile(mockBrowse, []byte(script), 0o755))
	t.Setenv("PP_BROWSE_BIN", mockBrowse)

	c := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "big-shop",
		Version:     "1.0.0",
		Description: "Large response spill test",
		Trust:       TrustPolicy{AllowedHosts: []string{"api.big.example"}, ReadOnly: true},
		Commands: map[string]CommandSpec{
			"list": {Driver: DriverHTTP, Method: "GET", URL: "https://api.big.example/v1/items", Extract: "$.items[*]"},
		},
	}
	require.NoError(t, c.Validate())

	res, err := Execute(context.Background(), c, "list", RunOptions{ViaBrowse: true})
	require.NoError(t, err)
	assert.Equal(t, 200, res.HTTPStatus)
	assert.Equal(t, 1, res.RowCount)
	rows := res.Data.([]any)
	assert.Equal(t, "Spilled OK", rows[0].(map[string]any)["name"])

	// readBoundedRegularFile must error explicitly rather than silently truncating
	// when a response exceeds maxBytes.
	oversize := filepath.Join(tmp, "oversize.txt")
	require.NoError(t, os.WriteFile(oversize, []byte("0123456789"), 0o600))
	_, err = readBoundedRegularFile(oversize, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds 5-byte limit")
}
