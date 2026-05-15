package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClearTokensClearsEmittedCredentialFields pins that the generated
// (*Config).ClearTokens body zeroes every credential-bearing field the
// template emits for an api_key CLI, not only the OAuth trio. AuthHeader()
// falls back to the env-var-derived field for api_key auth, so leaving
// it set after logout would silently re-authenticate the next command.
func TestClearTokensClearsEmittedCredentialFields(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("clear-tokens")
	apiSpec.Auth.EnvVars = []string{"PRINTING_PRESS_APIKEY"}

	outputDir := filepath.Join(t.TempDir(), "clear-tokens-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	body := extractClearTokensBody(t, configSrc)

	assert.Contains(t, body, "c.AccessToken = \"\"", "AccessToken should be cleared")
	assert.Contains(t, body, "c.RefreshToken = \"\"", "RefreshToken should be cleared")
	assert.Contains(t, body, "c.TokenExpiry = time.Time{}", "TokenExpiry should be cleared")
	assert.Contains(t, body, "c.AuthHeaderVal = \"\"", "AuthHeaderVal should be cleared (cached header could re-authenticate)")
	assert.Contains(t, body, "c.PrintingPressApikey = \"\"", "env-var-derived API key field should be cleared")
}

// TestClearTokensSkipsBuiltinCollisions pins that env vars whose
// placeholder collides with a builtin Config tag (e.g. *_ACCESS_TOKEN
// resolves to AccessToken) don't produce a duplicate clear: the OAuth-trio
// line already covers them and a second `c.AccessToken = ""` would be
// dead-code duplication.
func TestClearTokensSkipsBuiltinCollisions(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("builtin-collision")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"PRINTING_PRESS_ACCESS_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "builtin-collision-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	body := extractClearTokensBody(t, configSrc)

	count := strings.Count(body, "c.AccessToken = \"\"")
	assert.Equal(t, 1, count, "AccessToken should be cleared exactly once; env-var loop must skip the builtin collision")
}

func extractClearTokensBody(t *testing.T, src string) string {
	t.Helper()
	const decl = "func (c *Config) ClearTokens() error {"
	start := strings.Index(src, decl)
	require.GreaterOrEqual(t, start, 0, "ClearTokens declaration not found in generated config.go")
	body := src[start+len(decl):]
	end := strings.Index(body, "\nfunc ")
	if end < 0 {
		end = len(body)
	}
	return body[:end]
}
