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

// TestAuthHeader_ClientCredentialsAccessTokenWinsOverEnv pins that under
// OAuth2 client_credentials the cached AccessToken wins over the env-var
// fallback. The env var is the Client ID, not a usable bearer JWT.
func TestAuthHeader_ClientCredentialsAccessTokenWinsOverEnv(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cc-precedence")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "bearer_token",
		Header:      "Authorization",
		EnvVars:     []string{"CC_AUTH_TEST_CLIENT_ID"},
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "cc-precedence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	envCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_ID") + ` != ""`
	tokenCheck := `if c.AccessToken != ""`

	require.Contains(t, content, envCheck, "AuthHeader must keep the env-var fallback")
	require.Contains(t, content, tokenCheck, "AuthHeader must check AccessToken")

	body := authHeaderBody(t, content)
	envIdx := strings.Index(body, envCheck)
	tokenIdx := strings.Index(body, tokenCheck)
	require.NotEqual(t, -1, envIdx)
	require.NotEqual(t, -1, tokenIdx)
	assert.Less(t, tokenIdx, envIdx,
		"AccessToken check must appear BEFORE env-var fallback under OAuth2 client_credentials")
}

func TestAuthHeader_OAuth2AuthorizationCodeUsesToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-precedence")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		EnvVars:          []string{"OAUTH_AUTH_TEST_TOKEN"},
		AuthorizationURL: "https://example.com/auth",
		TokenURL:         "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-precedence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	envCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_TOKEN") + ` != ""`
	tokenCheck := `if c.AccessToken != ""`

	require.Contains(t, content, envCheck)
	require.Contains(t, content, tokenCheck)

	body := authHeaderBody(t, content)
	envIdx := strings.Index(body, envCheck)
	tokenIdx := strings.Index(body, tokenCheck)
	assert.Less(t, envIdx, tokenIdx,
		"env-var bearer fallback should win over file token for OAuth2 authorization_code")
}

func TestAuthLoginEnvVarsUseShellSafePrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("hyphen-api")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		AuthorizationURL: "https://example.com/auth",
		TokenURL:         "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "hyphen-api-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	require.Contains(t, content, `os.Getenv("HYPHEN_API_CLIENT_ID")`)
	require.Contains(t, content, `os.Getenv("HYPHEN_API_CLIENT_SECRET")`)
	require.NotContains(t, content, `HYPHEN-API_CLIENT_ID`)
}

// TestAuthHeader_EnvVarWinsOverFileToken pins env-first precedence for
// the non-client_credentials cases — plain bearer_token (PAT-style),
// cookie, and composed all follow the env > config convention so a
// freshly-rotated env var wins over a stale on-disk AccessToken.
func TestAuthHeader_EnvVarWinsOverFileToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		authType string
		envVar   string
	}{
		{"bearer_token", "bearer_token", "BEARER_AUTH_TEST_TOKEN"},
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
