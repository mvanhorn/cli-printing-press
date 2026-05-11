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

// TestAuthHeader_OAuth2DoesNotUseSetupEnvVars pins that for every OAuth2
// grant (authorization_code via the default, client_credentials via explicit
// OAuth2Grant) the configured env vars (e.g. CLIENT_ID / CLIENT_SECRET) are
// never emitted as bearer headers. The minted AccessToken is the only usable
// bearer; sending CLIENT_ID as `Authorization: Bearer` surfaces as
// token_rejected at the API.
func TestAuthHeader_OAuth2DoesNotUseSetupEnvVars(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		grant string
	}{
		{"authorization_code", ""},
		{"client_credentials", spec.OAuth2GrantClientCredentials},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("oauth-precedence-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:   "oauth2",
				Header: "Authorization",
				Format: "Bearer {token}",
				EnvVarSpecs: []spec.AuthEnvVar{
					{Name: "OAUTH_AUTH_TEST_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
					{Name: "OAUTH_AUTH_TEST_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
				},
				AuthorizationURL: "https://example.com/auth",
				TokenURL:         "https://example.com/token",
				OAuth2Grant:      tc.grant,
			}

			outputDir := filepath.Join(t.TempDir(), "oauth-precedence-"+tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(cfgSrc)

			clientIDCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_CLIENT_ID") + ` != ""`
			clientSecretCheck := "if c." + resolveEnvVarField("OAUTH_AUTH_TEST_CLIENT_SECRET") + ` != ""`
			tokenCheck := `if c.AccessToken != ""`

			body := authHeaderBody(t, content)
			require.Contains(t, body, tokenCheck, "AuthHeader must check AccessToken")
			require.Contains(t, body, `applyAuthFormat("Bearer {token}", map[string]string{"access_token": c.AccessToken, "token": c.AccessToken})`,
				"AuthHeader must return the AccessToken via applyAuthFormat")
			require.NotContains(t, body, clientIDCheck, "client ID must not be used as a bearer token")
			require.NotContains(t, body, clientSecretCheck, "client secret must not be used as a bearer token")
		})
	}
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

// TestAuthHeader_BearerTokenPrefixOverride pins that a bearer_token spec
// declaring auth.prefix changes the rendered Authorization scheme word
// across both the env-var and AccessToken branches. APIs that require a
// non-Bearer scheme (e.g., "Token", "PRIVATE-TOKEN", lowercase "token")
// otherwise force operators to hand-edit generated config. When auth.prefix
// is unset, "Bearer" remains the default.
func TestAuthHeader_BearerTokenPrefixOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		prefix   string
		expected string
	}{
		{"default", "", "Bearer"},
		{"token", "Token", "Token"},
		{"lowercase", "token", "token"},
		{"private_token", "PRIVATE-TOKEN", "PRIVATE-TOKEN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("prefix-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Prefix:  tc.prefix,
				EnvVars: []string{"PREFIX_TEST_TOKEN"},
			}

			outputDir := filepath.Join(t.TempDir(), "prefix-"+tc.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			body := authHeaderBody(t, string(cfgSrc))

			envField := resolveEnvVarField("PREFIX_TEST_TOKEN")
			require.Contains(t, body, `return "`+tc.expected+` " + c.`+envField,
				"env-var branch must render configured prefix")
			require.Contains(t, body, `return "`+tc.expected+` " + c.AccessToken`,
				"AccessToken branch must render configured prefix")

			if tc.prefix != "" && tc.expected != "Bearer" {
				assert.NotContains(t, body, `return "Bearer " + c.`+envField,
					"default Bearer literal must not leak when prefix is overridden")
				assert.NotContains(t, body, `return "Bearer " + c.AccessToken`,
					"default Bearer literal must not leak when prefix is overridden")
			}
		})
	}
}

// TestAuthHeader_BearerTokenPrefixFormatPrecedence pins that Auth.Format
// wins over Auth.Prefix at the same call sites, so the documented "Ignored
// when Format is set" contract survives template restructuring.
func TestAuthHeader_BearerTokenPrefixFormatPrecedence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("prefix-format")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Prefix:  "Token",
		Format:  "Bearer {token}",
		EnvVars: []string{"PREFIX_FORMAT_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "prefix-format-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	body := authHeaderBody(t, string(cfgSrc))

	envField := resolveEnvVarField("PREFIX_FORMAT_TOKEN")
	require.Contains(t, body, `applyAuthFormat("Bearer {token}"`,
		"Format must render via applyAuthFormat, not via the prefix literal")
	assert.NotContains(t, body, `return "Token " + c.`+envField,
		"Prefix must not leak into the env-var branch when Format is set")
	assert.NotContains(t, body, `return "Token " + c.AccessToken`,
		"Prefix must not leak into the AccessToken branch when Format is set")
}

// TestTierRouting_BearerPrefix pins that the per-tier bearer scheme in
// client.go.tmpl honors auth.prefix on the tier's auth config, matching
// the default-tier AuthHeader() behavior.
func TestTierRouting_BearerPrefix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tier-prefix")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"TIER_PREFIX_TOKEN"},
	}
	apiSpec.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "primary",
		Tiers: map[string]spec.TierConfig{
			"primary": {
				Auth: spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					Prefix:  "Token",
					EnvVars: []string{"TIER_PRIMARY_TOKEN"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "tier-prefix-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	content := string(clientSrc)

	require.Contains(t, content, `value := "Token " + tierValue0`,
		"per-tier bearer auth must honor configured prefix")
	assert.NotContains(t, content, `value := "Bearer " + tierValue0`,
		"default Bearer literal must not leak when tier prefix is overridden")
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
