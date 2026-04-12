package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedREADMEHasNoPlaceholderMarkers asserts that no <!-- *_OUTPUT -->
// HTML-comment markers ship in the rendered README. These markers were left
// over from an abandoned post-generate augmentation flow; the machine never
// populated them, so they leaked into every printed CLI as visible artifacts.
// Regression guard: if anyone re-introduces a marker without wiring up a
// fill path, this test fails.
func TestGeneratedREADMEHasNoPlaceholderMarkers(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "markerless",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"MARKERLESS_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/markerless-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/items",
						Description: "List items",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "markerless-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	for _, marker := range []string{
		"<!-- HELP_OUTPUT -->",
		"<!-- DOCTOR_OUTPUT -->",
		"<!-- VERSION_OUTPUT -->",
	} {
		assert.False(t, strings.Contains(content, marker),
			"rendered README still contains placeholder marker %q — no machine code replaces it", marker)
	}
}

// TestGeneratedREADMEHasNoHallucinatedCookbook asserts that the printed
// README does not advertise commands that the CLI may not implement. The
// old ## Cookbook block hard-coded sync/search/export examples that most
// specs don't produce; removing it prevents users from trying commands
// that error out.
func TestGeneratedREADMEHasNoHallucinatedCookbook(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "cookbookless",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"COOKBOOKLESS_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/cookbookless-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "cookbookless-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.False(t, strings.Contains(content, "## Cookbook"),
		"README should not include a Cookbook section (hard-coded sync/search/export commands are hallucinated for most specs)")
	assert.False(t, strings.Contains(content, "cookbookless-pp-cli sync"),
		"README should not reference an unimplemented sync command")
	assert.False(t, strings.Contains(content, "cookbookless-pp-cli export --format jsonl"),
		"README should not reference an unimplemented export command")
}

// TestOutputFormatsUsesRealCommandExample asserts the Output Formats block
// renders a resource+endpoint pair that actually exists in the spec. The
// previous template hard-coded `{firstResource} list`, which produced
// nonsense like "autocomplete list" when autocomplete had no list endpoint.
func TestOutputFormatsUsesRealCommandExample(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "realexample",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"REALEXAMPLE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/realexample-pp-cli/config.toml",
		},
		// Intentionally: a resource whose only endpoint is NOT "list".
		// Previous template would have produced "autocomplete list"; the
		// fixed template should render "autocomplete get" instead.
		Resources: map[string]spec.Resource{
			"autocomplete": {
				Description: "Autocomplete",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/autocomplete", Description: "Autocomplete symbols"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "realexample-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	content := string(readme)

	assert.True(t, strings.Contains(content, "realexample-pp-cli autocomplete get"),
		"Output Formats should reference a real resource+endpoint pair from the spec")
	assert.False(t, strings.Contains(content, "realexample-pp-cli autocomplete list"),
		"Output Formats should not hallucinate a 'list' endpoint that doesn't exist in the spec")
}
