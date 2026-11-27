package cartridge

import (
	"context"
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
