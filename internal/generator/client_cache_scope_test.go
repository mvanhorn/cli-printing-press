package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestClientCacheKeyScopesByBaseURLAndAuthIdentity(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cache-scope")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"CACHE_SCOPE_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "cache-scope-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)
	body := clientCacheKeyForBody(t, client)

	require.Contains(t, body, `"|base_url=" + c.BaseURL`, "cache keys must isolate staging/prod or per-tenant base URLs")
	require.Contains(t, body, `"|auth_source=" + c.Config.AuthSource`, "cache keys should distinguish env/config/profile auth sources")
	require.Contains(t, body, `authHeader := c.Config.AuthHeader()`, "cache keys should capture AuthHeader() once")
	require.Contains(t, body, `sha256.Sum256([]byte(authHeader))`, "cache keys should include an auth fingerprint without storing the raw token")
	require.NotContains(t, body, `sha256.Sum256([]byte(c.Config.AuthHeader()))`, "cache keys should reuse the captured authHeader, not call AuthHeader() twice")
	require.Contains(t, body, `query := url.Values{}`, "cache keys should encode query params with structured delimiters")
	require.Contains(t, body, `key += "|query=" + query.Encode()`, "cache keys should use url.Values.Encode for deterministic query boundaries")
}

func TestGeneratedCacheWritesUsePrivatePermissions(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cache-perms")
	outputDir := filepath.Join(t.TempDir(), "cache-perms-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cacheSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cache", "cache.go"))
	require.NoError(t, err)
	cache := string(cacheSrc)
	require.Contains(t, cache, "os.MkdirAll(s.Dir, 0o700)")
	require.Contains(t, cache, "os.WriteFile(s.path(key), []byte(value), 0o600)")
	require.NotContains(t, cache, "os.MkdirAll(s.Dir, 0o755)")
	require.NotContains(t, cache, "os.WriteFile(s.path(key), []byte(value), 0o644)")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)
	require.Contains(t, client, "os.MkdirAll(resourceDir, 0o700)")
	require.Contains(t, client, "os.WriteFile(cacheFile, []byte(data), 0o600)")
	require.NotContains(t, client, "os.MkdirAll(resourceDir, 0o755)")
	require.NotContains(t, client, "os.WriteFile(cacheFile, []byte(data), 0o644)")

	// minimalSpec does not enable HTML extraction, so the writeCacheContentType
	// ".meta.json" 0o600 write is not emitted here; that path's permission is
	// covered by the golden suite's HTML-extraction fixtures. This test guards
	// the always-emitted cache/client/config perms.
	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	config := string(configSrc)
	require.Contains(t, config, "cliutil.AtomicWritePrivateFile(c.Path, data, 0o600, 0o700)")
	require.NotContains(t, config, "os.WriteFile(c.Path, data, 0o644)")
	require.NotContains(t, config, "os.MkdirAll(dir, 0o755)")
}

func TestGeneratedClientQueryParamContractsPass(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("query-param-contracts")
	outputDir := filepath.Join(t.TempDir(), "query-param-contracts-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "Test(CacheKeyDelimitsSortedQueryParams|GetWithHeadersValuesPreservesRepeatedQueryParams)", "-count=1")
}

func clientCacheKeyForBody(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, "func (c *Client) cacheKeyFor(")
	require.NotEqual(t, -1, start, "cacheKeyFor function must be emitted")
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
