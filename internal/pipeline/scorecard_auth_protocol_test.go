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
		assert.Equal(t, 10, authProtocolScore(t, dir, specPath), "composed still stamps Authorization from authHeader")

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

func TestCookieCredentialAttached(t *testing.T) {
	t.Parallel()

	jar := func(passJar bool) string {
		jarArg := "nil"
		if passJar {
			jarArg = "cookieJar"
		}
		return `package client
func New(cfg *Config) *Client {
	cookieJar := LoadCookieJar()
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
	_, _ = newHTTPClient(0, ` + jarArg + `, nil)
	return nil
}
`
	}
	seedOutsideNew := `package client
func New(cfg *Config) *Client {
	cookieJar := LoadCookieJar()
	_, _ = newHTTPClient(0, cookieJar, nil)
	return nil
}
func seed(cfg *Config, cookieJar interface{}) {
	SeedCookieJarForDomain(cookieJar, "https://example.test", cfg.CookieCredential(), ".example.test")
}
`
	headerSet := `package client
func do(req *http.Request, authHeader string) {
	req.Header.Set("Authorization", authHeader)
}
`
	headerAdd := `package client
func do(req *http.Request, authHeader string) {
	req.Header.Add("X-Session", authHeader)
}
`
	wrongValue := `package client
func do(req *http.Request) {
	req.Header.Set("Authorization", "unused")
}
`
	addCookie := `package client
func do(req *http.Request, authHeader string) {
	req.AddCookie(&http.Cookie{Name: "__Secure-next-auth.session-token", Value: authHeader})
}
`
	wrongCookie := `package client
func do(req *http.Request, authHeader string) {
	req.AddCookie(&http.Cookie{Name: "other", Value: authHeader})
}
`

	cases := []struct {
		name   string
		client string
		header string
		want   bool
	}{
		{name: "jar seeded into newHTTPClient", client: jar(true), header: "Cookie", want: true},
		{name: "missing jar wiring", client: jar(false), header: "Cookie", want: false},
		{name: "seed outside New", client: seedOutsideNew, header: "Cookie", want: false},
		{name: "jar does not credit Authorization", client: jar(true), header: "Authorization", want: false},
		{name: "header-borne cookie stamps Authorization", client: headerSet, header: "Authorization", want: true},
		{name: "composed stamps the expected header", client: headerAdd, header: "X-Session", want: true},
		{name: "wrong header", client: headerSet, header: "X-Session", want: false},
		{name: "header value is not authHeader", client: wrongValue, header: "Authorization", want: false},
		{name: "openapi apiKey cookie", client: addCookie, header: "__Secure-next-auth.session-token", want: true},
		{name: "wrong cookie name", client: wrongCookie, header: "session", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, cookieCredentialAttached(tc.client, tc.header))
		})
	}
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
