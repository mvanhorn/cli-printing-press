package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHeader_ClientCredentialsDoesNotUseSetupEnvVars pins that under
// OAuth2 client_credentials the setup inputs are never emitted as bearer
// headers. Only a minted AccessToken is usable for API requests.
func TestAuthHeader_ClientCredentialsDoesNotUseSetupEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cc-precedence")
	apiSpec.Auth = spec.AuthConfig{
		Type:   "bearer_token",
		Header: "Authorization",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "CC_AUTH_TEST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "CC_AUTH_TEST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
		},
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "https://example.com/token",
	}

	outputDir := filepath.Join(t.TempDir(), "cc-precedence-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(cfgSrc)

	envCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_ID") + ` != ""`
	envSecretCheck := "if c." + resolveEnvVarField("CC_AUTH_TEST_CLIENT_SECRET") + ` != ""`
	tokenCheck := `if c.AccessToken != ""`

	require.Contains(t, content, tokenCheck, "AuthHeader must check AccessToken")

	body := authHeaderBody(t, content)
	require.Contains(t, body, tokenCheck)
	require.NotContains(t, body, envCheck, "client ID must not be used as a bearer token")
	require.NotContains(t, body, envSecretCheck, "client secret must not be used as a bearer token")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientContent := string(clientSrc)
	verifyIdx := strings.Index(clientContent, `cliutil.IsVerifyEnv()`)
	mintIdx := strings.Index(clientContent, `c.mintClientCredentials(clientID, clientSecret)`)
	require.NotEqual(t, -1, verifyIdx, "mock verification should short-circuit before token minting")
	require.NotEqual(t, -1, mintIdx, "client_credentials mint path should still be emitted")
	assert.Less(t, verifyIdx, mintIdx, "mock verification must not dial the real token endpoint")
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
