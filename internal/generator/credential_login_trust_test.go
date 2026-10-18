// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerate_ClientCredentialsWithoutTokenURLCompiles proves a supported
// client_credentials spec that omits token_url still emits a CLI whose tests
// compile. Config.TokenURL is absent in that shape, so the loose-config mint
// test must not read it.
func TestGenerate_ClientCredentialsWithoutTokenURLCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	apiSpec := clientCredentialsSpec("cc-no-token-url", "")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.NotContains(t, configSrc, "TokenURL")
	mintSrc := readGeneratedFile(t, outputDir, "internal", "client", "untrusted_mint_test.go")
	require.NotContains(t, mintSrc, "cfg.TokenURL")
	require.Contains(t, mintSrc, "TestLooseConfigDoesNotMintClientCredentials")

	runGoCommandRequired(t, outputDir, "test", "-run", "^$", "./...")
}

func TestGenerate_ClientCredentialsWithTokenURLKeepsMintRefusal(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	apiSpec := clientCredentialsSpec("cc-with-token-url", "https://api.example.com/oauth/token")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mintSrc := readGeneratedFile(t, outputDir, "internal", "client", "untrusted_mint_test.go")
	require.Contains(t, mintSrc, "cfg.TokenURL")
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestLooseConfigDoesNotMintClientCredentials", "-count=1", "-timeout", "180s")
}

func TestGenerate_BrowserLoginKeepsFreshCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "stdlib", source: ""},
		{name: "browser", source: "sniffed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiSpec := minimalSpec("browser-login-trust")
			apiSpec.BaseURL = "https://app.example.com"
			apiSpec.SpecSource = tc.source
			// Cookies wires the jar that attaches a saved session. An empty
			// list emits no jar, so login can store the token and still send nothing.
			apiSpec.Auth = spec.AuthConfig{
				Type:                           "cookie",
				Header:                         "Cookie",
				In:                             "cookie",
				CookieDomain:                   ".example.com",
				Cookies:                        []string{"session"},
				EnvVars:                        []string{"BROWSER_LOGIN_TRUST_COOKIES"},
				RequiresBrowserSession:         true,
				BrowserSessionValidationPath:   "/api/items",
				BrowserSessionValidationMethod: "GET",
			}
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			authSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
			require.Contains(t, authSrc, "cfg.CredentialRouteBlocked()")
			require.Contains(t, authSrc, "config.Load(cfg.Path)")
			trustSrc := readGeneratedFile(t, outputDir, "internal", "cli", "browser_login_trust_test.go")
			require.Contains(t, trustSrc, "TestBrowserLoginKeepsFreshCredentialOnLooseConfig")
			require.Contains(t, trustSrc, "httptest.NewTLSServer")
			require.Contains(t, trustSrc, "missing credential")
			if tc.source == "sniffed" {
				require.Contains(t, trustSrc, "SKIP_TLS_VERIFY")
			} else {
				require.NotContains(t, trustSrc, "SKIP_TLS_VERIFY")
			}

			runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestBrowserLoginKeepsFreshCredentialOnLooseConfig", "-count=1", "-timeout", "180s")
		})
	}
}

// TestGenerate_BrowserLoginCustomHeaderCarriesFreshCredential proves header-carried
// cookie auth, where the credential rides the spec header such as X-Session, still
// passes the login probe. The probe reads that header, so the fresh token has to
// be on the wire.
func TestGenerate_BrowserLoginCustomHeaderCarriesFreshCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	apiSpec := minimalSpec("browser-login-xsession")
	apiSpec.BaseURL = "https://app.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:                           "cookie",
		Header:                         "X-Session",
		In:                             "header",
		Format:                         "{token}",
		CookieDomain:                   ".example.com",
		EnvVars:                        []string{"BROWSER_LOGIN_XSESSION_TOKEN"},
		RequiresBrowserSession:         true,
		BrowserSessionValidationPath:   "/api/items",
		BrowserSessionValidationMethod: "GET",
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, `req.Header.Set("X-Session", authHeader)`)
	require.NotContains(t, clientSrc, "SeedCookieJar")
	trustSrc := readGeneratedFile(t, outputDir, "internal", "cli", "browser_login_trust_test.go")
	require.Contains(t, trustSrc, `r.Header.Get("X-Session")`)
	require.Contains(t, trustSrc, "missing credential")

	runGoCommandRequired(t, outputDir, "test", "-run", "^$", "./...")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestBrowserLoginKeepsFreshCredentialOnLooseConfig", "-count=1", "-timeout", "180s")
}

func clientCredentialsSpec(name, tokenURL string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{
		Type:        "bearer_token",
		Header:      "Authorization",
		Format:      "Bearer {token}",
		OAuth2Grant: spec.OAuth2GrantClientCredentials,
		TokenURL:    tokenURL,
		EnvVars:     []string{naming.EnvPrefix(name) + "_CLIENT_ID", naming.EnvPrefix(name) + "_CLIENT_SECRET"},
	}
	return apiSpec
}
