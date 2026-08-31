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
	require.Contains(t, client, "os.Chmod(resourceDir, 0o700)",
		"cache resource dirs must be owner-only even when a leftover 0755 dir is reused")
	require.Contains(t, client, "os.Chmod(cacheFile, 0o600)",
		"rewriting an existing cache file must chmod 0600; WriteFile ignores perm on an extant file")
	require.Contains(t, clientFuncBody(t, client, "func (c *Client) readCacheWithHeaders("), "ensureCachePerms(",
		"a cache-hit read of a leftover 0644 file must tighten perms before returning content")
	writeBody := clientFuncBody(t, client, "func (c *Client) writeCacheWithHeaders(")
	preChmod := strings.Index(writeBody, "ensureCachePerms(")
	removeCall := strings.Index(writeBody, "os.Remove(cacheFile)")
	writeCall := strings.Index(writeBody, "os.WriteFile(cacheFile")
	postChmod := strings.LastIndex(writeBody, "ensureCachePerms(")
	require.NotEqual(t, -1, preChmod, "write path must call ensureCachePerms")
	require.NotEqual(t, -1, removeCall, "write path must unlink a leftover cache inode before rewrite")
	require.NotEqual(t, -1, writeCall, "write path must WriteFile the cache file")
	require.Less(t, preChmod, removeCall,
		"restrict a leftover 0644 file and its dir before replacing the inode")
	require.Less(t, removeCall, writeCall,
		"unlink the leftover inode so an open FD cannot see the new body")
	require.Greater(t, postChmod, writeCall,
		"WriteFile perm applies only on create; chmod the new inode and dir")
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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
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

func TestWriteCacheWithHeadersReplacesOpenInode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix inode replacement is not preserved on Windows")
	}

	c := New(&config.Config{BaseURL: "https://api.example.invalid"}, time.Second, 0)
	c.cacheDir = t.TempDir()

	old := json.RawMessage(` + "`" + `{"ok":"old"}` + "`" + `)
	c.writeCacheWithHeaders("/items", nil, nil, old)
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
	held, err := os.Open(cacheFile)
	if err != nil {
		t.Fatalf("open leftover cache file: %v", err)
	}
	defer held.Close()
	oldInfo, err := held.Stat()
	if err != nil {
		t.Fatalf("stat open leftover inode: %v", err)
	}

	c.writeCacheWithHeaders("/items", nil, nil, json.RawMessage(` + "`" + `{"ok":"new"}` + "`" + `))

	got, err := io.ReadAll(held)
	if err != nil {
		t.Fatalf("read open leftover FD: %v", err)
	}
	if string(got) != string(old) {
		t.Fatalf("open FD saw %s, want leftover body %s", got, old)
	}
	fresh, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read replaced cache file: %v", err)
	}
	if string(fresh) != ` + "`" + `{"ok":"new"}` + "`" + ` {
		t.Fatalf("path data = %s, want new body", fresh)
	}
	newInfo, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("stat replaced cache file: %v", err)
	}
	if mode := newInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("replaced cache mode = %04o, want 0600", mode)
	}
	oldSys, oldOK := oldInfo.Sys().(*syscall.Stat_t)
	newSys, newOK := newInfo.Sys().(*syscall.Stat_t)
	if !oldOK || !newOK {
		t.Fatal("expected syscall.Stat_t for inode comparison")
	}
	if oldSys.Ino == newSys.Ino {
		t.Fatal("rewrite reused inode; an open leftover FD would see the new body")
	}
}

func TestReadCacheWithHeadersRechmodsFreshLegacyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not preserved on Windows")
	}

	c := New(&config.Config{BaseURL: "https://api.example.invalid"}, time.Second, 0)
	c.cacheDir = t.TempDir()

	want := json.RawMessage(` + "`" + `{"ok":true}` + "`" + `)
	c.writeCacheWithHeaders("/items", nil, nil, want)
	matches, err := filepath.Glob(filepath.Join(c.cacheDir, "resources", "*", "*.json"))
	if err != nil {
		t.Fatalf("glob cache files: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("wrote %d cache files, want 1: %v", len(matches), matches)
	}
	cacheFile := matches[0]
	resourceDir := filepath.Dir(cacheFile)
	if err := os.Chmod(cacheFile, 0o644); err != nil {
		t.Fatalf("chmod leftover 0644: %v", err)
	}
	if err := os.Chmod(resourceDir, 0o755); err != nil {
		t.Fatalf("chmod leftover 0755 dir: %v", err)
	}

	got, ok := c.readCacheWithHeaders("/items", nil, nil)
	if !ok {
		t.Fatal("expected cache hit for a fresh leftover file")
	}
	if string(got) != string(want) {
		t.Fatalf("cache hit data = %s, want %s", got, want)
	}

	info, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("stat cache-hit file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("cache-hit file mode = %04o, want 0600", mode)
	}
	dirInfo, err := os.Stat(resourceDir)
	if err != nil {
		t.Fatalf("stat cache-hit resource dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("cache-hit resource dir mode = %04o, want 0700", mode)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "cache_chmod_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^Test(WriteCacheWithHeadersRechmodsExistingFile|WriteCacheWithHeadersReplacesOpenInode|ReadCacheWithHeadersRechmodsFreshLegacyFile)$", "-count=1")
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
	return clientFuncBody(t, content, "func (c *Client) cacheKeyFor(")
}

func clientFuncBody(t *testing.T, content, signature string) string {
	t.Helper()
	start := strings.Index(content, signature)
	require.NotEqual(t, -1, start, "%s must be emitted", signature)
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
