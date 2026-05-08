package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoAuthSpecOmitsTokenScaffolding asserts that an APISpec with
// Auth.Type == "none" does not emit OAuth-shaped token scaffolding into
// config.go and client.go. Issue #695 reported that no-auth CLIs ship with
// AccessToken / RefreshToken / ClientID / ClientSecret / TokenExpiry fields,
// SaveTokens / ClearTokens methods, and a refreshAccessToken() function that
// nothing on the CLI surface can populate (because no `auth` subcommand is
// emitted in this case). Those symbols are dead code for no-auth CLIs and the
// `OAuth-shaped config` framing has misled triage in the past.
func TestNoAuthSpecOmitsTokenScaffolding(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("no-auth-dead-code")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}

	outputDir := filepath.Join(t.TempDir(), "no-auth-dead-code-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	for _, sym := range []string{
		"AccessToken",
		"RefreshToken",
		"TokenExpiry",
		"ClientID",
		"ClientSecret",
		"SaveTokens",
		"ClearTokens",
	} {
		assert.NotContains(t, configSrc, sym,
			"config.go must not reference %q for Auth.Type=none specs", sym)
	}

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	for _, sym := range []string{
		"refreshAccessToken",
		"c.Config.AccessToken",
		"c.Config.RefreshToken",
		"c.Config.TokenExpiry",
	} {
		assert.NotContains(t, clientSrc, sym,
			"client.go must not reference %q for Auth.Type=none specs", sym)
	}
}

// TestApiKeySpecKeepsTokenScaffolding pins the positive control: the gating
// for issue #695 must not strip token scaffolding from CLIs that actually
// have an auth surface. A bearer/api_key CLI still needs SaveTokens (called
// from the auth subcommand templates) and AccessToken (read by AuthHeader()).
func TestApiKeySpecKeepsTokenScaffolding(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("api-key-keeps-tokens")

	outputDir := filepath.Join(t.TempDir(), "api-key-keeps-tokens-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	for _, sym := range []string{
		"AccessToken",
		"RefreshToken",
		"TokenExpiry",
		"ClientID",
		"ClientSecret",
		"SaveTokens",
		"ClearTokens",
	} {
		assert.Contains(t, configSrc, sym,
			"config.go must keep %q for non-none auth specs", sym)
	}

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, "refreshAccessToken")
}
