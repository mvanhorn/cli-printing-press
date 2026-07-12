package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// Regression for #1410: the generated client.go must wire CheckRedirect on
// the http.Client so nonce-bound auth (OAuth 1.0a PLAINTEXT, SigV4, Hawk)
// gets a fresh authHeader on each redirect hop instead of replaying the
// original Authorization header. Go's default replays headers verbatim,
// which trips replay-detection on the second hop (Schoology 303 -> 401).
func TestClientCheckRedirectReappliesAuth(t *testing.T) {
	t.Parallel()

	t.Run("bearer in Authorization header re-sets Authorization", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-bearer")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "bearer_token",
			EnvVars: []string{"REDIRECT_BEARER_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, "if len(via) >= 10 {",
			"CheckRedirect must cap depth at 10 to match Go's default policy")
		require.Contains(t, closure, `return errors.New("stopped after 10 redirects")`,
			"depth cap must return a plain error so Do() propagates it; ErrUseLastResponse would silently surface the 3xx body as a successful response")
		require.Contains(t, closure, `c.authHeader(req.Context())`,
			"CheckRedirect must call c.authHeader with the redirect request context so nonce-bound schemes get a fresh cancellable signature")
		require.Contains(t, closure, `req.Header.Set("Authorization", h)`,
			"bearer auth must re-set Authorization on redirect to refresh nonce-bound headers")
		require.Contains(t, closure, "req.URL.Host == via[0].URL.Host",
			"auth re-stamp must be gated on same-host so a cross-domain 3xx (open redirect or partner handoff) does not leak the credential — Go's automatic Authorization stripping has already run by the time CheckRedirect is called, and any header set here is sent verbatim")
	})

	t.Run("api_key in custom header re-sets that header, not Authorization", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-apikey-header")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-API-Key",
			EnvVars: []string{"REDIRECT_APIKEY_HEADER_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, `req.Header.Set("X-API-Key", h)`,
			"api_key in custom header must re-set that header on redirect, not Authorization")
		require.NotContains(t, closure, `req.Header.Set("Authorization", h)`,
			"must not also stamp Authorization when the spec uses a custom header")
		require.Contains(t, closure, "req.URL.Host == via[0].URL.Host",
			"custom-header auth must also be same-host gated to avoid leaking credentials across domains")
	})

	t.Run("api_key in query parameter skips header re-set", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-apikey-query")
		apiSpec.Auth = spec.AuthConfig{
			Type:    "api_key",
			In:      "query",
			Header:  "api_key",
			EnvVars: []string{"REDIRECT_APIKEY_QUERY_TOKEN"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, "if len(via) >= 10 {",
			"CheckRedirect must still cap redirect depth for query-param auth")
		require.NotContains(t, closure, `c.authHeader()`,
			"query-param auth must not re-derive a header inside the closure; Go preserves the URL query on redirects")
		require.NotContains(t, closure, "req.Header.Set(",
			"query-param auth must not stamp any auth header on redirect")
	})

	t.Run("browser cookie auth uses jar redirect path", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("redirect-cookie")
		apiSpec.Auth = spec.AuthConfig{
			Type:         "cookie",
			Header:       "Cookie",
			In:           "cookie",
			CookieDomain: ".example.com",
			Cookies:      []string{"session_id", "csrf_token"},
			EnvVars:      []string{"REDIRECT_COOKIE_SESSION"},
		}
		client := generateClientSource(t, apiSpec)
		closure := checkRedirectClosureBody(t, client)

		require.Contains(t, closure, "Cookie-auth redirects: Go's http.Client + http.CookieJar handle",
			"browser cookie auth must use the jar redirect branch, not the named in:cookie branch")
		require.NotContains(t, closure, `ck.Name != "Cookie"`,
			"browser cookie auth must not treat the Cookie header name as a cookie name when stripping redirects")
		require.NotContains(t, closure, `req.AddCookie(ck)`,
			"browser cookie auth redirects must not re-add preserved real cookies to cross-host redirects")
	})
}

// checkRedirectClosureBody extracts the body of the CheckRedirect closure
// from the generated New() function so substring assertions don't match the
// docstring above the closure (which mentions c.authHeader for context).
func checkRedirectClosureBody(t *testing.T, content string) string {
	t.Helper()
	marker := "httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {"
	start := strings.Index(content, marker)
	require.NotEqual(t, -1, start, "CheckRedirect closure must be emitted in New()")
	body := content[start+len(marker):]
	end := strings.Index(body, "\n\t}\n")
	require.NotEqual(t, -1, end, "CheckRedirect closure must be properly closed")
	return body[:end]
}

func generateClientSource(t *testing.T, apiSpec *spec.APISpec) string {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), apiSpec.Name+"-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	return string(src)
}
