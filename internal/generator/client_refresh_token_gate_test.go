package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guards #1200: refreshAccessToken() and its call site must only be emitted
// when the spec actually carries an OAuth token-refresh endpoint
// (Auth.TokenURL != ""). For bearer-only CLIs (api_key, bearer_token with
// no TokenURL) the method becomes a guaranteed no-op that silently swallows
// every refresh call, leaving users to hit a 1-hour token expiry wall with
// no error and `auth status` cheerfully reporting `has_refresh_token: ...`.
//
// Both prongs must hold: the method definition and the do()-path call site
// share the same gate, so dropping one without the other leaves either an
// orphan symbol or an undefined-symbol compile error.
func TestRefreshAccessToken_GatedByTokenURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth spec.AuthConfig
		want bool
	}{
		{
			name: "bearer_only_omits_refresh",
			auth: spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"BEARER_ONLY_TOKEN"},
			},
			want: false,
		},
		{
			name: "api_key_omits_refresh",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"API_KEY_TOKEN"},
			},
			want: false,
		},
		{
			name: "oauth2_authorization_code_emits_refresh",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				EnvVars:          []string{"AUTHCODE_TOKEN"},
				AuthorizationURL: "https://example.com/oauth/authorize",
				TokenURL:         "https://example.com/oauth/token",
			},
			want: true,
		},
		{
			// Pins the gate on TokenURL specifically — an oauth2 spec
			// with an AuthorizationURL but no TokenURL must still drop
			// refresh plumbing. Without this case the gate could be
			// silently re-keyed on Auth.Type ("oauth2") and the
			// authorization_code-emits-refresh case alone would still pass.
			name: "oauth2_without_token_url_omits_refresh",
			auth: spec.AuthConfig{
				Type:             "oauth2",
				Header:           "Authorization",
				Format:           "Bearer {token}",
				EnvVars:          []string{"NO_TOKEN_URL_TOKEN"},
				AuthorizationURL: "https://example.com/oauth/authorize",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = tt.auth

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")

			methodPresent := strings.Contains(clientSrc, "func (c *Client) refreshAccessToken(ctx context.Context)")
			callPresent := strings.Contains(clientSrc, "c.refreshAccessToken(ctx)")
			assert.Equal(t, tt.want, methodPresent,
				"refreshAccessToken method definition emission must follow Auth.TokenURL non-empty")
			assert.Equal(t, tt.want, callPresent,
				"refreshAccessToken call-site emission must follow Auth.TokenURL non-empty")

			// Catch import-gating mistakes: an orphan reference to a gated
			// symbol would pass the substring asserts but produce
			// non-buildable generated source. Skipped under -short.
			runGoCommand(t, outputDir, "build", "./...")
		})
	}
}
