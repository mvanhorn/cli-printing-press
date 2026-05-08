package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Symbols that exist solely to support an `auth` subcommand. They must
// disappear from generated config.go / client.go for `auth.type: "none"`
// CLIs (no caller can populate them) and stay for any non-none auth flow.
var (
	configTokenScaffolding = []string{
		"AccessToken",
		"RefreshToken",
		"TokenExpiry",
		"ClientID",
		"ClientSecret",
		"SaveTokens",
		"ClearTokens",
	}
	clientTokenScaffolding = []string{
		"refreshAccessToken",
		"c.Config.AccessToken",
		"c.Config.RefreshToken",
		"c.Config.TokenExpiry",
	}
)

// TestTokenScaffoldingFollowsAuthSurface pins both directions: no-auth specs
// drop the OAuth-shape token scaffolding entirely (otherwise the symbols are
// dead code), and api_key (or any non-none) specs keep it (the auth
// subcommand templates depend on SaveTokens / AccessToken).
func TestTokenScaffoldingFollowsAuthSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		auth   spec.AuthConfig
		expect func(t *testing.T, src, sym string)
	}{
		{
			name:   "no_auth_omits_scaffolding",
			auth:   spec.AuthConfig{Type: "none"},
			expect: func(t *testing.T, src, sym string) { assert.NotContains(t, src, sym) },
		},
		{
			name:   "api_key_keeps_scaffolding",
			auth:   spec.AuthConfig{}, // minimalSpec defaults to api_key
			expect: func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			if tt.auth.Type != "" {
				apiSpec.Auth = tt.auth
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
			for _, sym := range configTokenScaffolding {
				tt.expect(t, configSrc, sym)
			}

			clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
			for _, sym := range clientTokenScaffolding {
				tt.expect(t, clientSrc, sym)
			}
		})
	}
}
