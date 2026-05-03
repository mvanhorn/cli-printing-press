package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHeader_BearerTokenAccessTokenWinsOverEnv pins that a cached
// AccessToken wins over the env-var-as-bearer fallback for bearer_token.
// Env-var-as-bearer remains the fallback for the simple case where the
// env var IS a usable bearer JWT (personal access tokens, GitHub PATs).
func TestAuthHeader_BearerTokenAccessTokenWinsOverEnv(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-precedence")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"BEARER_AUTH_TEST_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-precedence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	envCheck := "if c." + resolveEnvVarField("BEARER_AUTH_TEST_TOKEN") + ` != ""`
	tokenCheck := `if c.AccessToken != ""`

	require.Contains(t, content, envCheck, "AuthHeader must keep the env-var fallback")
	require.Contains(t, content, tokenCheck, "AuthHeader must check AccessToken")

	body := authHeaderBody(t, content)
	envIdx := strings.Index(body, envCheck)
	tokenIdx := strings.Index(body, tokenCheck)
	require.NotEqual(t, -1, envIdx)
	require.NotEqual(t, -1, tokenIdx)
	assert.Less(t, tokenIdx, envIdx,
		"AccessToken check must appear BEFORE env-var fallback for bearer_token")
}

// TestAuthHeader_EnvVarWinsOverFileTokenForCookieComposed pins that
// cookie and composed types still check env-var FIRST (unchanged from
// the env > config convention; only bearer_token's precedence flipped).
func TestAuthHeader_EnvVarWinsOverFileTokenForCookieComposed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		authType string
		envVar   string
	}{
		{"cookie", "cookie", "COOKIE_AUTH_TEST_TOKEN"},
		{"composed", "composed", "COMPOSED_AUTH_TEST_TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tc.name + "-precedence")
			apiSpec.Auth = spec.AuthConfig{
				Type:    tc.authType,
				Header:  "Authorization",
				EnvVars: []string{tc.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tc.name+"-precedence-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			envCheck := "if c." + resolveEnvVarField(tc.envVar) + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			require.Contains(t, content, envCheck)
			require.Contains(t, content, tokenCheck)

			body := authHeaderBody(t, content)
			envIdx := strings.Index(body, envCheck)
			tokenIdx := strings.Index(body, tokenCheck)
			assert.Less(t, envIdx, tokenIdx,
				"env-var check must appear BEFORE AccessToken check for type %q", tc.authType)
		})
	}
}

// authHeaderBody slices out just the AuthHeader function body so precedence
// assertions can't be tricked by a matching pattern in unrelated code
// further down the file.
func authHeaderBody(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, "func (c *Config) AuthHeader() string {")
	require.NotEqual(t, -1, start, "AuthHeader function must be emitted")
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
