package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCookieAuthScoreHeaderPreservesStampedHeader(t *testing.T) {
	assert.Equal(t, "Cookie", cookieAuthScoreHeader(spec.AuthConfig{Type: "cookie"}))
	assert.Equal(t, "Cookie", cookieAuthScoreHeader(spec.AuthConfig{Type: "cookie", Header: "Cookie"}))
	assert.Equal(t, "Authorization", cookieAuthScoreHeader(spec.AuthConfig{Type: "composed"}))
	assert.Equal(t, "X-Session", cookieAuthScoreHeader(spec.AuthConfig{Type: "cookie", Header: " X-Session "}))

	info := internalSpecToOpenAPISpecInfo(&spec.APISpec{Auth: spec.AuthConfig{Type: "composed"}})
	scheme := info.SecuritySchemes["composed"]
	assert.Equal(t, "apikey", scheme.Type)
	assert.Equal(t, "cookie", scheme.In)
	assert.Equal(t, "Authorization", scheme.HeaderName)
}

func TestAuthProtocol_GeneratedCookieAndComposed(t *testing.T) {
	t.Parallel()

	t.Run("cookie jar", func(t *testing.T) {
		t.Parallel()
		dir, specPath := generateInternalCLI(t, `name: scorecookie
version: "0.1.0"
description: Cookie auth scorecard fixture
base_url: https://api.example.com
auth:
  type: cookie
  header: Cookie
  in: cookie
  format: "Cookie {session}"
  cookie_domain: .example.com
  cookies:
    - session
    - csrf
  env_vars:
    - SCORECOOKIE_SESSION
config:
  format: toml
  path: ~/.config/scorecookie-pp-cli/config.toml
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
        description: List users
`)
		assertGeneratedAuthProtocol(t, dir, specPath, spec.AuthConfig{
			Type:   "cookie",
			Format: "Cookie {session}",
		}, 10)
		rewriteClient(t, dir, func(src string) string {
			require.Contains(t, src, "SeedCookieJarForDomain(")
			src = strings.ReplaceAll(src, "SeedCookieJarForDomain(", "skipSeed(")
			src = strings.ReplaceAll(src, "SeedCookieJar(", "skipSeed(")
			return src
		})
		assert.False(t, cookieCredentialAttached(readClient(t, dir), "Cookie"))
		assert.Equal(t, 2, authProtocolScore(t, dir, specPath))
	})

	t.Run("composed header and jar", func(t *testing.T) {
		t.Parallel()
		dir, specPath := generateInternalCLI(t, `name: scorecomposed
version: "0.1.0"
description: Composed auth scorecard fixture
base_url: https://api.example.com
auth:
  type: composed
  header: Authorization
  format: "Bearer {token}"
  cookie_domain: .example.com
  cookies:
    - session
  env_vars:
    - SCORECOMPOSED_TOKEN
config:
  format: toml
  path: ~/.config/scorecomposed-pp-cli/config.toml
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
        description: List users
`)
		original := readClient(t, dir)
		assertGeneratedAuthProtocol(t, dir, specPath, spec.AuthConfig{
			Type:   "composed",
			Format: "Bearer {token}",
		}, 10)
		require.Contains(t, original, `req.Header.Set("Authorization", authHeader)`)

		rewriteClient(t, dir, func(src string) string {
			src = strings.ReplaceAll(src, "SeedCookieJarForDomain(", "skipSeed(")
			src = strings.ReplaceAll(src, "SeedCookieJar(", "skipSeed(")
			return src
		})
		assert.Equal(t, 10, authProtocolScore(t, dir, specPath), "composed still stamps Authorization on the request that is Do'd")

		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "client", "client.go"), []byte(original), 0o644))
		rewriteClient(t, dir, func(src string) string {
			replaced := strings.ReplaceAll(src, `req.Header.Set("Authorization", authHeader)`, `req.Header.Set("Authorization", "unused")`)
			require.NotEqual(t, src, replaced)
			require.Contains(t, replaced, "SeedCookieJarForDomain(")
			return replaced
		})
		assert.False(t, cookieCredentialAttached(readClient(t, dir), "Authorization"))
		assert.Equal(t, 2, authProtocolScore(t, dir, specPath), "a seeded jar does not stand in for a missing Authorization credential")

		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "client", "client.go"), []byte(original), 0o644))
		rewriteClient(t, dir, func(src string) string {
			src = strings.ReplaceAll(src, "SeedCookieJarForDomain(", "skipSeed(")
			src = strings.ReplaceAll(src, "SeedCookieJar(", "skipSeed(")
			replaced := strings.ReplaceAll(src, `req.Header.Set("Authorization", authHeader)`, `req.Header.Set("Authorization", "unused")`)
			require.NotEqual(t, src, replaced)
			return replaced
		})
		assert.False(t, cookieCredentialAttached(readClient(t, dir), "Authorization"))
		assert.Equal(t, 2, authProtocolScore(t, dir, specPath))
	})

	t.Run("header-carried cookie", func(t *testing.T) {
		t.Parallel()
		dir, specPath := generateInternalCLI(t, `name: scoreheadercookie
version: "0.1.0"
description: Header-carried cookie scorecard fixture
base_url: https://api.example.com
auth:
  type: cookie
  header: Authorization
  in: header
  format: "Bearer {token}"
  env_vars:
    - SCOREHEADER_COOKIE
config:
  format: toml
  path: ~/.config/scoreheadercookie-pp-cli/config.toml
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
        description: List users
`)
		client := readClient(t, dir)
		require.NotContains(t, client, "SeedCookieJarForDomain(")
		require.Contains(t, client, `req.Header.Set("Authorization", authHeader)`)
		assertGeneratedAuthProtocol(t, dir, specPath, spec.AuthConfig{
			Type:   "cookie",
			Format: "Bearer {token}",
		}, 10)
		rewriteClient(t, dir, func(src string) string {
			replaced := strings.ReplaceAll(src, `req.Header.Set("Authorization", authHeader)`, `req.Header.Set("Authorization", "unused")`)
			require.NotEqual(t, src, replaced)
			return replaced
		})
		assert.Equal(t, 2, authProtocolScore(t, dir, specPath))
	})

	t.Run("openapi apiKey cookie", func(t *testing.T) {
		t.Parallel()
		const specYAML = `openapi: "3.0.3"
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
`
		api, err := openapi.Parse([]byte(specYAML))
		require.NoError(t, err)
		api.Name = "cookiekey"
		api.Owner = "test-owner"
		api.OwnerName = "Test Author"
		api.Creator = spec.Person{Handle: "tester", Name: "Test Author"}
		api.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/cookiekey-pp-cli/config.toml"}
		dir := filepath.Join(t.TempDir(), "cookiekey-pp-cli")
		require.NoError(t, generator.New(api, dir).Generate())
		requireGeneratedCompiles(t, dir)
		specPath := filepath.Join(t.TempDir(), "spec.yaml")
		require.NoError(t, os.WriteFile(specPath, []byte(specYAML), 0o644))

		client := readClient(t, dir)
		const addCookie = `req.AddCookie(&http.Cookie{Name: "__Secure-next-auth.session-token", Value: authHeader})`
		require.Contains(t, client, addCookie)
		require.NotContains(t, client, "SeedCookieJarForDomain(")
		assert.GreaterOrEqual(t, authProtocolScore(t, dir, specPath), 8)

		rewriteClient(t, dir, func(src string) string {
			replaced := strings.ReplaceAll(src, addCookie, `req.AddCookie(&http.Cookie{Name: "__Secure-next-auth.session-token", Value: "unused"})`)
			require.NotEqual(t, src, replaced)
			return replaced
		})
		assert.Less(t, authProtocolScore(t, dir, specPath), 8)
	})
}

func TestScoreAuthScheme_CookieAttachmentRejectsCosmeticFakes(t *testing.T) {
	const chrome = "func detectCookieTool() {}\n"
	jarBacked := cookieJarClient(false)
	cases := []struct {
		name   string
		client string
		config string
		auth   string
		scheme openAPISecurityScheme
		want   int
	}{
		{
			name:   "jar installed on the client",
			client: jarBacked,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name:   "jar plus browser cookie tooling",
			client: jarBacked,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   10,
		},
		{
			name:   "external chrome transport still installs the jar",
			client: cookieJarClient(true),
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "local chrome transport installs the jar",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	c := &Client{}
	c.HTTPClient = httpClient
	return c
}
func newHTTPClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return chromeClient(timeout, jar, trust)
}
func chromeClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return &http.Client{Jar: jar}, nil
}
`,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "seed helper calls itself",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
func SeedCookieJarForDomain(jar interface{}, base, cred, domain string) {
	SeedCookieJarForDomain(jar, base, cred, domain)
}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "newHTTPClient calls itself",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
func newHTTPClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return newHTTPClient(timeout, jar, trust)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name:   "local seed helper calls SetCookies",
			client: jarBacked + "\nfunc SeedCookieJarForDomain(jar interface{ SetCookies(any, any) }, base, cred, domain string) { jar.SetCookies(nil, nil) }\n",
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "executed cookie header",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, err := http.NewRequest("GET", "https://example.test", nil)
	if err != nil {
		return
	}
	req.Header.Set("Cookie", authHeader)
	_, _ = httpClient.Do(req)
}
`,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "guardOutbound and WithContext keep the composed header",
			client: `package client
func (c *Client) do(ctx context.Context, authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://example.test", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", authHeader)
	guarded, err := guardOutbound(req, nil, authHeader, nil)
	if err != nil {
		return
	}
	req = guarded
	req = req.WithContext(ctx)
	_, _ = httpClient.Do(req)
}
`,
			scheme: cookieScheme("Authorization"),
			want:   8,
		},
		{
			name: "AddCookie of the scheme cookie",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, err := http.NewRequest("GET", "https://example.test", nil)
	req.AddCookie(&http.Cookie{Name: "__Secure-next-auth.session-token", Value: authHeader})
	_, _ = httpClient.Do(req)
}
`,
			scheme: cookieScheme("__Secure-next-auth.session-token"),
			want:   8,
		},
		{
			name: "header value may be a credential call",
			client: `package client
func (c *Client) do(cfg *config.Config, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Cookie", cfg.CookieCredential())
	_, _ = httpClient.Do(req)
}
`,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name:   "unused const does not attach",
			client: "package client\nconst leftover = `SeedCookieJarForDomain(cookieJar, cfg.CookieCredential())`\nconst also = `req.Header.Set(\"Cookie\", authHeader)`\n",
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "dead helper is not New and never Dos",
			client: `package client
func decorate(cfg *config.Config) {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	req.Header.Set("Cookie", authHeader)
	req.AddCookie(&http.Cookie{Name: "Cookie", Value: authHeader})
	_ = httpClient
}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "New wires a client it does not store",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	_ = httpClient
	return &Client{}
}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "seed is replaced before the client is built",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	cookieJar = NewCookieJar()
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "credential argument is a string, not a call",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", "cfg.CookieCredential()", ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "local seed helper does not call SetCookies",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
func SeedCookieJarForDomain(jar, base, cred, domain interface{}) {}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "local seed delegates to a helper that also does nothing",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
func SeedCookieJarForDomain(jar interface{}) { seedCookieJar(jar) }
func seedCookieJar(jar interface{}) {}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "newHTTPClient drops the jar",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
func newHTTPClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return &http.Client{Timeout: timeout}, nil
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "local chrome transport drops the jar",
			client: `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
func newHTTPClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return chromeClient(timeout, jar, trust)
}
func chromeClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return &http.Client{Timeout: timeout}, nil
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "seeded jar is not the one passed to newHTTPClient",
			client: `package client
func New(cfg *config.Config) *Client {
	other := LoadCookieJar()
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(other, "https://example.test", cfg.CookieCredential(), ".example.test")
	httpClient, _ := newHTTPClient(0, cookieJar, tlsSettings{})
	return &Client{HTTPClient: httpClient}
}
` + newHTTPClientInstallsJar,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name:   "malformed client",
			client: "package client\nfunc (\n",
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "literal header value",
			client: `package client
func (c *Client) do(httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Authorization", "Bearer x")
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Authorization"),
			want:   2,
		},
		{
			name: "header set with no request and no Do",
			client: `package client
func (c *Client) do(authHeader string) {
	req.Header.Set("Cookie", authHeader)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "header is set on a request that is replaced",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test/a", nil)
	req.Header.Set("Cookie", authHeader)
	req, _ = http.NewRequest("GET", "https://example.test/b", nil)
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "returned Do still carries the header",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) (*http.Response, error) {
	req, err := http.NewRequest("GET", "https://example.test", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)
	return httpClient.Do(req)
}
`,
			scheme: cookieScheme("Authorization"),
			want:   8,
		},
		{
			name: "header overwritten before Do",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Authorization", "unused")
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Authorization"),
			want:   2,
		},
		{
			name: "header deleted before Do",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Del("Authorization")
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Authorization"),
			want:   2,
		},
		{
			name: "header set again after delete",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Del("Authorization")
	req.Header.Add("Authorization", authHeader)
	_, _ = httpClient.Do(req)
}
`,
			scheme: cookieScheme("Authorization"),
			want:   8,
		},
		{
			name: "inner req does not credit the outer request",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	{
		req, _ := http.NewRequest("GET", "https://example.test/inner", nil)
		req.Header.Set("Cookie", authHeader)
	}
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "outer req keeps its header when an inner req shadows it",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Cookie", authHeader)
	{
		req, _ := http.NewRequest("GET", "https://example.test/inner", nil)
		req.Header.Set("Cookie", "unused")
	}
	_, _ = httpClient.Do(req)
}
`,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "alias sees a header set on the original request",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	other := req
	req.Header.Set("Cookie", authHeader)
	_, _ = httpClient.Do(other)
}
`,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "alias sees a later delete",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	other := req
	req.Header.Set("Authorization", authHeader)
	req.Header.Del("Authorization")
	_, _ = httpClient.Do(other)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Authorization"),
			want:   2,
		},
		{
			name: "clone keeps the header copied before a later overwrite",
			client: `package client
func (c *Client) do(ctx context.Context, authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.Header.Set("Cookie", authHeader)
	other := req.Clone(ctx)
	req.Header.Set("Cookie", "unused")
	_, _ = httpClient.Do(other)
}
`,
			scheme: cookieScheme("Cookie"),
			want:   8,
		},
		{
			name: "header is set after Do",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	_, _ = httpClient.Do(req)
	req.Header.Set("Cookie", authHeader)
}
`,
			auth:   chrome,
			scheme: cookieScheme("Cookie"),
			want:   2,
		},
		{
			name: "Del Cookie drops a cookie added earlier",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: authHeader})
	req.Header.Del("Cookie")
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("session"),
			want:   2,
		},
		{
			name: "AddCookie uses the wrong name",
			client: `package client
func (c *Client) do(authHeader string, httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.AddCookie(&http.Cookie{Name: "other", Value: authHeader})
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("session"),
			want:   2,
		},
		{
			name: "AddCookie value is a literal",
			client: `package client
func (c *Client) do(httpClient interface{ Do(any) (any, error) }) {
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "unused"})
	_, _ = httpClient.Do(req)
}
`,
			auth:   chrome,
			scheme: cookieScheme("session"),
			want:   2,
		},
		{
			name:   "seeded jar does not satisfy Authorization",
			client: jarBacked,
			auth:   chrome,
			scheme: cookieScheme("Authorization"),
			want:   2,
		},
		{
			name:   "cookie scheme named bearerSession uses the jar",
			client: jarBacked,
			scheme: openAPISecurityScheme{Key: "bearerSession", Type: "apikey", In: "cookie", HeaderName: "Cookie"},
			want:   8,
		},
		{
			name:   "cookie scheme named botSession uses the jar",
			client: jarBacked,
			scheme: openAPISecurityScheme{Key: "botSession", Type: "apikey", In: "cookie", HeaderName: "Cookie"},
			want:   8,
		},
		{
			name:   "cookie scheme named basicSession uses the jar",
			client: jarBacked,
			scheme: openAPISecurityScheme{Key: "basicSession", Type: "apikey", In: "cookie", HeaderName: "Cookie"},
			want:   8,
		},
		{
			name:   "bearer literal does not credit a cookie scheme named bearerSession",
			client: "package client\nconst leftover = \"Bearer \"\n",
			auth:   chrome,
			scheme: openAPISecurityScheme{Key: "bearerSession", Type: "apikey", In: "cookie", HeaderName: "Cookie"},
			want:   2,
		},
		{
			name:   "bearer is not credited for cookie jar wiring",
			client: jarBacked,
			auth:   chrome,
			scheme: openAPISecurityScheme{Key: "bearer", Type: "http", Scheme: "bearer"},
			want:   2,
		},
		{
			name:   "api key header is not credited for cookie jar wiring",
			client: jarBacked,
			scheme: openAPISecurityScheme{Key: "x-api-key", Type: "apikey", In: "header", HeaderName: "X-API-Key"},
			want:   0,
		},
		{
			name:   "api key query is not credited for cookie jar wiring",
			client: jarBacked,
			scheme: openAPISecurityScheme{Key: "api_key", Type: "apikey", In: "query", HeaderName: "api_key"},
			want:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score, ok := scoreAuthScheme(tc.client, tc.config, tc.auth, false, tc.scheme)
			assert.True(t, ok)
			assert.Equal(t, tc.want, score)
		})
	}
}

const newHTTPClientInstallsJar = `
func newHTTPClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	return &http.Client{Timeout: timeout, Jar: jar}, nil
}
`

func cookieJarClient(viaChrome bool) string {
	transport := "return &http.Client{Timeout: timeout, Jar: jar}, nil"
	if viaChrome {
		transport = "return chromeClient(timeout, jar, trust)"
	}
	return `package client
func New(cfg *config.Config) *Client {
	cookieJar := LoadCookieJar()
	if cfg != nil && !cfg.UsePersistedCookieJar() {
		cookieJar = NewCookieJar()
	}
	if cfg != nil {
		SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	}
	httpClient, tlsErr := newHTTPClient(0, cookieJar, tlsSettings{})
	c := &Client{HTTPClient: httpClient, tlsConfigErr: tlsErr}
	return c
}
func newHTTPClient(timeout int, jar interface{}, trust interface{}) (*http.Client, error) {
	` + transport + `
}
`
}

func cookieScheme(header string) openAPISecurityScheme {
	return openAPISecurityScheme{Key: "cookie", Type: "apikey", In: "cookie", HeaderName: header}
}

func generateInternalCLI(t *testing.T, yamlSpec string) (string, string) {
	t.Helper()
	api, err := spec.ParseBytes([]byte(yamlSpec))
	require.NoError(t, err)
	api.Owner = "test-owner"
	api.OwnerName = "Test Author"
	api.Creator = spec.Person{Handle: "tester", Name: "Test Author"}
	dir := filepath.Join(t.TempDir(), api.Name+"-pp-cli")
	require.NoError(t, generator.New(api, dir).Generate())
	requireGeneratedCompiles(t, dir)
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(yamlSpec), 0o644))
	return dir, specPath
}

func requireGeneratedCompiles(t *testing.T, dir string) {
	t.Helper()
	if testing.Short() {
		// The lint lane runs -short. Scoring still runs there; the full suite compiles.
		return
	}
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"GOCACHE=" + filepath.Join(home, "gocache"),
	}
	// A replaced HOME would otherwise make Go download modules into that
	// temp dir. The module cache is read-only, so the test cleanup fails.
	for _, name := range []string{"GOPATH", "GOMODCACHE"} {
		if value := goEnv(t, name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func goEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func assertGeneratedAuthProtocol(t *testing.T, dir, specPath string, auth spec.AuthConfig, want int) {
	t.Helper()
	assert.Equal(t, want, authProtocolScore(t, dir, specPath))
	result := checkAuth(dir, auth)
	assert.True(t, result.Match, result.Detail)
}

func authProtocolScore(t *testing.T, dir, specPath string) int {
	t.Helper()
	sc, err := RunScorecard(dir, t.TempDir(), specPath, nil)
	require.NoError(t, err)
	require.NotContains(t, sc.UnscoredDimensions, DimAuthProtocol)
	return sc.Steinberger.AuthProtocol
}

func readClient(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "internal", "client", "client.go"))
	require.NoError(t, err)
	return string(data)
}

func rewriteClient(t *testing.T, dir string, fn func(string) string) {
	t.Helper()
	path := filepath.Join(dir, "internal", "client", "client.go")
	updated := fn(readClient(t, dir))
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
}
