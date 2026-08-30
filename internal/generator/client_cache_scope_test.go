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
	require.Contains(t, client, "os.Chmod(cacheFile, 0o600)",
		"rewriting an existing cache file must chmod 0600; WriteFile ignores perm on an extant file")
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

func TestWriteCacheWithHeadersRechmodsExistingFile(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("cache-chmod")
	outputDir := filepath.Join(t.TempDir(), "cache-chmod-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"cache-chmod-pp-cli/internal/config"
)

func TestWriteCacheWithHeadersRechmodsExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not preserved on Windows")
	}

	c := New(&config.Config{BaseURL: "https://api.example.invalid"}, time.Second, 0)
	c.cacheDir = t.TempDir()

	c.writeCacheWithHeaders("/items", nil, nil, json.RawMessage(` + "`" + `{"ok":true}` + "`" + `))
	matches, err := filepath.Glob(filepath.Join(c.cacheDir, "resources", "*", "*.json"))
	if err != nil {
		t.Fatalf("glob cache files: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("wrote %d cache files, want 1: %v", len(matches), matches)
	}
	cacheFile := matches[0]
	if err := os.Chmod(cacheFile, 0o644); err != nil {
		t.Fatalf("chmod leftover 0644: %v", err)
	}

	c.writeCacheWithHeaders("/items", nil, nil, json.RawMessage(` + "`" + `{"ok":true}` + "`" + `))
	info, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("stat rewritten cache file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("rewritten cache mode = %04o, want 0600", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "cache_chmod_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestWriteCacheWithHeadersRechmodsExistingFile$", "-count=1")
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
