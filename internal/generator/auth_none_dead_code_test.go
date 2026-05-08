package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
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

// TestTokenScaffoldingFollowsAuthSurface pins all three branches of
// shouldEmitAuth(): Auth.Type != "none", Auth.AuthorizationURL != "", and a
// graphql_persisted_query traffic-analysis hint each independently keep the
// OAuth-shape token scaffolding emitting; only when all three are absent do
// the symbols disappear (otherwise they would be dead code with no caller on
// the CLI surface). The cases also verify that the generated CLI still
// compiles with the gating in place — gated imports falling out of sync with
// gated function bodies would otherwise pass the symbol-absence assertions
// but ship a non-buildable CLI.
func TestTokenScaffoldingFollowsAuthSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		auth                 spec.AuthConfig
		trafficAnalysisHints []string
		expect               func(t *testing.T, src, sym string)
	}{
		{
			name:   "no_auth_omits_scaffolding",
			auth:   spec.AuthConfig{Type: "none"},
			expect: func(t *testing.T, src, sym string) { assert.NotContains(t, src, sym) },
		},
		{
			name: "api_key_keeps_scaffolding",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"MYAPI_TOKEN"},
			},
			expect: func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) },
		},
		{
			name: "none_with_authorization_url_keeps_scaffolding",
			auth: spec.AuthConfig{
				Type:             "none",
				AuthorizationURL: "https://example.com/oauth/authorize",
			},
			expect: func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) },
		},
		{
			name:                 "none_with_graphql_persisted_query_keeps_scaffolding",
			auth:                 spec.AuthConfig{Type: "none"},
			trafficAnalysisHints: []string{"graphql_persisted_query"},
			expect:               func(t *testing.T, src, sym string) { assert.Contains(t, src, sym) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = tt.auth

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			gen := New(apiSpec, outputDir)
			if len(tt.trafficAnalysisHints) > 0 {
				gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{GenerationHints: tt.trafficAnalysisHints}
			}
			require.NoError(t, gen.Generate())

			configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
			for _, sym := range configTokenScaffolding {
				tt.expect(t, configSrc, sym)
			}

			clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
			for _, sym := range clientTokenScaffolding {
				tt.expect(t, clientSrc, sym)
			}

			// Catch import-gating mistakes: an orphan reference to a gated
			// symbol (e.g., a stray time.Time{} after TokenExpiry is removed)
			// would pass the substring asserts but produce non-buildable
			// generated source. Skipped under -short / unit lane via runGoCommand.
			runGoCommand(t, outputDir, "mod", "tidy")
			runGoCommand(t, outputDir, "build", "./...")
		})
	}
}
