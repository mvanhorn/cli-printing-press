package generator

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplacePathParamPercentEncodesValue pins the helpers.go template so the
// emitted replacePathParam routes the user-supplied value through
// url.PathEscape before substituting it into the URL path. Without the escape,
// values that contain path-reserved characters (e.g. "src/main.go" for the
// GitHub Contents API or "https://example.com/" for Search Console's siteUrl)
// silently produce malformed request URLs that 404 against the live API.
//
// We assert at the template-output level (helpers.go contains the escape call
// and imports net/url) so the contract is checked once and every printed CLI
// inherits the fix.
func TestReplacePathParamPercentEncodesValue(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "encpath",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/encpath-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/v1/items/{itemId}",
						Description: "Get an item",
						Params: []spec.Param{
							{Name: "itemId", Type: "string", Required: true, Positional: true},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersPath := filepath.Join(outputDir, "internal", "cli", "helpers.go")
	helpersGo, err := os.ReadFile(helpersPath)
	require.NoError(t, err)
	src := string(helpersGo)

	assert.Contains(t, src, `"net/url"`,
		"helpers.go must import net/url when replacePathParam is emitted")
	assert.Contains(t, src,
		`return strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(value))`,
		"replacePathParam must percent-encode the value via url.PathEscape so "+
			"path-reserved characters (/, :, ?, #, space, %) don't produce a malformed URL")
}

// TestURLPathEscapeBehaviorPinsContract is a stdlib-behavior pin: if Go ever
// changed url.PathEscape's encoding shape, every printed CLI's request URLs
// would change too. We document the inputs the issue cited (opaque IDs round-
// trip; slashes/colons encode) so a future stdlib regression is caught here
// rather than in a live-API 404.
func TestURLPathEscapeBehaviorPinsContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"abc-123-def", "abc-123-def"},
		{"2026-01-15", "2026-01-15"},
		{"src/cli/main.go", "src%2Fcli%2Fmain.go"},
		{"https://example.com/", "https:%2F%2Fexample.com%2F"},
		{"sc-domain:example.com", "sc-domain:example.com"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, url.PathEscape(c.in))
		})
	}
}
