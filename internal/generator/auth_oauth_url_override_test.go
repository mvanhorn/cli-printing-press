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

// Pins the runtime override path for the OAuth2 OIDC URLs reported in #952.
// The generator-baked URL was treated as the only source of truth, leaving
// users no way to point a printed CLI at a non-default deployment without
// regenerating. The fix: emit AuthorizationURL/TokenURL as Config fields
// (with env-var overrides) and prefer cfg.* over the spec literal.
func TestOAuth2URLs_RuntimeOverrideEmittedForAuthCodeGrant(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("oauth-url-override")
	apiSpec.Auth = spec.AuthConfig{
		Type:             "oauth2",
		Header:           "Authorization",
		Format:           "Bearer {token}",
		EnvVars:          []string{"OAUTH_URL_OVERRIDE_TOKEN"},
		AuthorizationURL: "http://localhost:9001/oidc/auth",
		TokenURL:         "http://localhost:9001/oidc/token",
	}

	outputDir := filepath.Join(t.TempDir(), "oauth-url-override-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	cfg := string(cfgSrc)

	require.Regexp(t, `\bAuthorizationURL\s+string\b`, cfg, "Config must expose AuthorizationURL override field")
	require.Regexp(t, `\bTokenURL\s+string\b`, cfg, "Config must expose TokenURL override field")
	require.Contains(t, cfg, `os.Getenv("OAUTH_URL_OVERRIDE_AUTHORIZATION_URL")`,
		"Load() must read AuthorizationURL env override")
	require.Contains(t, cfg, `os.Getenv("OAUTH_URL_OVERRIDE_TOKEN_URL")`,
		"Load() must read TokenURL env override")

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	auth := string(authSrc)

	require.Contains(t, auth, "authURL = cfg.AuthorizationURL",
		"login flow must read cfg.AuthorizationURL before falling back to spec default")
	require.Contains(t, auth, "tokenURL = cfg.TokenURL",
		"login flow must read cfg.TokenURL before falling back to spec default")

	// The expiry calc must guard against ExpiresIn==0 so a non-conformant
	// server doesn't make every subsequent call think the token has expired.
	require.Contains(t, auth, "if tokenResp.ExpiresIn > 0 {",
		"login flow must guard expiry calc against ExpiresIn==0")
	expiryIdx := strings.Index(auth, "if tokenResp.ExpiresIn > 0 {")
	saveIdx := strings.Index(auth, "cfg.SaveTokens(clientID, clientSecret, tokenResp.AccessToken")
	assert.Less(t, expiryIdx, saveIdx, "guard must precede SaveTokens call")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)
	require.Contains(t, client, "tokenURL := c.Config.TokenURL",
		"refreshAccessToken must read c.Config.TokenURL before falling back to spec default")
	require.Contains(t, client, "func (c *Client) refreshAccessToken(ctx context.Context) error",
		"refreshAccessToken must accept the caller context")
	require.Contains(t, client, "http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(params.Encode()))",
		"refreshAccessToken must attach the caller context to the token request")
}

func TestOAuth2URLs_RuntimeOverrideEmittedForClientCredentialsGrant(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cc-url-override")
	apiSpec.Auth = spec.AuthConfig{
		Type:        "bearer_token",
		Header:      "Authorization",
		EnvVars:     []string{"CC_URL_OVERRIDE_CLIENT_ID", "CC_URL_OVERRIDE_CLIENT_SECRET"},
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    "http://localhost:9001/oidc/token",
	}

	outputDir := filepath.Join(t.TempDir(), "cc-url-override-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	cfg := string(cfgSrc)
	require.Regexp(t, `\bTokenURL\s+string\b`, cfg)
	require.NotRegexp(t, `\bAuthorizationURL\s+string\b`, cfg,
		"client_credentials grant has no authorization URL, so the field must not be emitted")
	require.Contains(t, cfg, `os.Getenv("CC_URL_OVERRIDE_TOKEN_URL")`)

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	auth := string(authSrc)
	require.Contains(t, auth, "tokenURL := cfg.TokenURL",
		"client_credentials login must read cfg.TokenURL before mint")
	require.Contains(t, auth, "if tok.ExpiresIn > 0 {",
		"client_credentials login must guard expiry calc against ExpiresIn==0")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)
	require.Contains(t, client, "tokenURL = c.Config.TokenURL",
		"mintClientCredentials must read c.Config.TokenURL via the c.Config != nil guard")
	require.Contains(t, client, "func (c *Client) mintClientCredentials(ctx context.Context, clientID, clientSecret string) error",
		"mintClientCredentials must accept the caller context")
	require.Contains(t, client, "http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))",
		"mintClientCredentials must attach the caller context to the token request")

	// Pin the auto-refresh expiry guard: without it a server returning
	// expires_in: 0 makes every request re-trigger mintClientCredentials in
	// a loop. mintClientCredentials is the runtime auto-refresh path called
	// from authHeader(), so this guard matters more than the login-path one.
	mintIdx := strings.Index(client, "func (c *Client) mintClientCredentials")
	require.NotEqual(t, -1, mintIdx, "mintClientCredentials must be emitted")
	mintBody := client[mintIdx:]
	if next := strings.Index(mintBody[1:], "\nfunc "); next != -1 {
		mintBody = mintBody[:next+1]
	}
	require.Contains(t, mintBody, "if tokenResp.ExpiresIn > 0 {",
		"mintClientCredentials must guard expiry calc against ExpiresIn==0")
}

// Pins that bearer_token / api_key specs (no OAuth2 URLs) still build cleanly:
// the cfg.TokenURL access in client.go must be gated so the field isn't
// referenced when it's not emitted.
func TestOAuth2URLs_NoFieldsForBearerTokenSpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-no-oauth")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"BEARER_NO_OAUTH_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-no-oauth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cfgSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	cfg := string(cfgSrc)
	require.NotRegexp(t, `\bAuthorizationURL\s+string\b`, cfg,
		"plain bearer_token has no OAuth2 URLs; field must not be emitted")
	require.NotRegexp(t, `\bTokenURL\s+string\b`, cfg,
		"plain bearer_token has no OAuth2 URLs; field must not be emitted")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.NotContains(t, string(clientSrc), "c.Config.TokenURL",
		"client.go must not reference c.Config.TokenURL when the field isn't emitted")
}
