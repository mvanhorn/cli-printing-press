package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAPIKeyCookieInjectionUsesAddCookie(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Cookie Key API
  version: "1.0.0"
servers:
  - url: https://api.example.com
security:
  - cookieAuth: []
components:
  securitySchemes:
    cookieAuth:
      type: apiKey
      in: cookie
      name: __Secure-next-auth.session-token
paths:
  /me:
    get:
      operationId: getMe
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)
	apiSpec.Name = "cookiekey"
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/cookiekey-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), "cookiekey-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientSrc := string(clientBytes)

	assert.Contains(t, clientSrc, `req.AddCookie(&http.Cookie{Name: "__Secure-next-auth.session-token", Value: authHeader})`,
		"apiKey in:cookie must emit AddCookie on request construction")
	// The auth cookie is added by name exactly once, on the initial request.
	// The redirect handler must NOT re-add it on same-host hops (Go carries the
	// Cookie header verbatim, so a second AddCookie would duplicate it). The
	// cross-host branch may re-add OTHER cookies via a variable, so count only
	// the named-literal AddCookie.
	assert.Equal(t, 1, strings.Count(clientSrc, `AddCookie(&http.Cookie{Name: "__Secure-next-auth.session-token"`),
		"apiKey in:cookie must add the named auth cookie exactly once")
	assert.Contains(t, clientSrc, `req.Header.Del("Cookie")`,
		"cross-host redirect must strip the auth cookie so it cannot leak to a redirect target")
	assert.Contains(t, clientSrc, `if ck.Name != "__Secure-next-auth.session-token"`,
		"cross-host strip must remove only the auth cookie by name, preserving others")
	assert.NotContains(t, clientSrc, `req.Header.Set("__Secure-next-auth.session-token", authHeader)`,
		"apiKey in:cookie must not be emitted as a custom request header")

	// --dry-run must preview the credential in cookie wire format, not as a
	// bare request header.
	assert.Contains(t, clientSrc, `"  Cookie: %s=%s\n", "__Secure-next-auth.session-token"`,
		"dry-run must preview in:cookie auth as a Cookie header")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

func TestGenerateAPIKeyHeaderInjectionDoesNotUseAddCookie(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Header Key API
  version: "1.0.0"
servers:
  - url: https://api.example.com
security:
  - apiKeyAuth: []
components:
  securitySchemes:
    apiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /me:
    get:
      operationId: getMe
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)
	apiSpec.Name = "headerkey"
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/headerkey-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), "headerkey-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	clientSrc := string(clientBytes)

	assert.Contains(t, clientSrc, `req.Header.Set("X-API-Key", authHeader)`,
		"apiKey in:header must keep header injection")
	assert.Contains(t, clientSrc, `req.Header.Set("X-API-Key", h)`,
		"apiKey in:header redirect path must keep header injection")
	assert.NotContains(t, clientSrc, `req.AddCookie(&http.Cookie{Name: "X-API-Key", Value: authHeader})`,
		"apiKey in:header must not emit AddCookie")
}
