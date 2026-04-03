package websniff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyze(t *testing.T) {
	t.Parallel()

	apiSpec, err := Analyze(filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"))
	require.NoError(t, err)
	require.NotNil(t, apiSpec)

	assert.Equal(t, "hn-algolia", apiSpec.Name)
	require.NotEmpty(t, apiSpec.Resources)

	foundEndpointWithParams := false
	for _, resource := range apiSpec.Resources {
		require.NotEmpty(t, resource.Endpoints)
		for _, endpoint := range resource.Endpoints {
			if len(endpoint.Params) > 0 {
				foundEndpointWithParams = true
			}
		}
	}

	assert.True(t, foundEndpointWithParams)
	assert.NoError(t, apiSpec.Validate())
}

func TestWriteSpec(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:        "example",
		Description: "Example API",
		Version:     "0.1.0",
		BaseURL:     "https://api.example.com",
		Auth:        spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/example-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"widgets": {
				Description: "Operations on widgets",
				Endpoints: map[string]spec.Endpoint{
					"list_widgets": {
						Method: "GET",
						Path:   "/widgets",
						Response: spec.ResponseDef{
							Type: "object",
							Item: "widgets",
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}

	outputPath := filepath.Join(t.TempDir(), "nested", "spec.yaml")
	err := WriteSpec(apiSpec, outputPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	parsed, err := spec.ParseBytes(data)
	require.NoError(t, err)
	assert.Equal(t, apiSpec.Name, parsed.Name)
	assert.Equal(t, apiSpec.BaseURL, parsed.BaseURL)
}

func TestDeriveNameFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips www and tld",
			raw:  "https://www.youtube.com",
			want: "youtube",
		},
		{
			name: "keeps meaningful subdomain",
			raw:  "https://hn.algolia.com",
			want: "hn-algolia",
		},
		{
			name: "drops generic api prefix",
			raw:  "https://api.example.com",
			want: "example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, deriveNameFromURL(tt.raw))
		})
	}
}

func TestAnalyzeCapture_UsesCapturedBearerAuth(t *testing.T) {
	t.Parallel()

	capture, err := ParseEnriched(filepath.Join("..", "..", "testdata", "sniff", "sample-auth-capture.json"))
	require.NoError(t, err)

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Equal(t, "bearer_token", apiSpec.Auth.Type)
	assert.Equal(t, "Authorization", apiSpec.Auth.Header)
}

func TestAnalyzeCapture_UsesCapturedCookieAuth(t *testing.T) {
	t.Parallel()

	capture := &EnrichedCapture{
		TargetURL: "https://api.spotify.com",
		Auth: &AuthCapture{
			Cookies:     []string{"_session=abc"},
			Type:        "cookie",
			BoundDomain: "spotify.com",
		},
		Entries: []EnrichedEntry{
			{
				Method:         "GET",
				URL:            "https://api.spotify.com/v1/me",
				RequestHeaders: map[string]string{"Content-Type": "application/json"},
			},
		},
	}

	apiSpec, err := AnalyzeCapture(capture)
	require.NoError(t, err)

	assert.Equal(t, "cookie", apiSpec.Auth.Type)
	assert.Equal(t, "Cookie", apiSpec.Auth.Header)
	assert.Equal(t, "cookie", apiSpec.Auth.In)
	assert.Equal(t, "spotify.com", apiSpec.Auth.CookieDomain)
	assert.Equal(t, []string{"SPOTIFY_COOKIES"}, apiSpec.Auth.EnvVars)
}

func TestDetectAuth_PrefersCapturedAuthOverHeaders(t *testing.T) {
	t.Parallel()

	auth := detectAuth(&EnrichedCapture{
		Auth: &AuthCapture{
			Headers: map[string]string{"X-API-Key": "key-123"},
			Type:    "api_key",
		},
	}, []EnrichedEntry{
		{
			RequestHeaders: map[string]string{"Authorization": "Bearer tok123"},
		},
	}, "spotify")

	assert.Equal(t, "api_key", auth.Type)
	assert.Equal(t, "X-API-Key", auth.Header)
	assert.Equal(t, "header", auth.In)
}

func TestDetectAuth_FallsBackToHeaderInference(t *testing.T) {
	t.Parallel()

	auth := detectAuth(nil, []EnrichedEntry{
		{
			RequestHeaders: map[string]string{"Authorization": "Bearer tok123"},
		},
	}, "spotify")

	assert.Equal(t, "bearer_token", auth.Type)
	assert.Equal(t, "Authorization", auth.Header)
}

func TestWriteEnrichedCaptureUsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "capture.json")
	err := WriteEnrichedCapture(&EnrichedCapture{
		TargetURL: "https://api.spotify.com",
		Auth: &AuthCapture{
			Headers: map[string]string{"Authorization": "Bearer tok123"},
			Type:    "bearer",
		},
	}, outputPath)
	require.NoError(t, err)

	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
