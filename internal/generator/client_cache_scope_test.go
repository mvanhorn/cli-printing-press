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
	body := clientCacheKeyBody(t, client)

	require.Contains(t, body, `"|base_url=" + c.BaseURL`, "cache keys must isolate staging/prod or per-tenant base URLs")
	require.Contains(t, body, `"|auth_source=" + c.Config.AuthSource`, "cache keys should distinguish env/config/profile auth sources")
	require.Contains(t, body, `authHeader := c.Config.AuthHeader()`, "cache keys should capture AuthHeader() once")
	require.Contains(t, body, `sha256.Sum256([]byte(authHeader))`, "cache keys should include an auth fingerprint without storing the raw token")
	require.NotContains(t, body, `sha256.Sum256([]byte(c.Config.AuthHeader()))`, "cache keys should reuse the captured authHeader, not call AuthHeader() twice")
	require.Contains(t, body, `sort.Strings(paramKeys)`, "cache keys should be deterministic for map params")
}

func clientCacheKeyBody(t *testing.T, content string) string {
	t.Helper()
	start := strings.Index(content, "func (c *Client) cacheKey(")
	require.NotEqual(t, -1, start, "cacheKey function must be emitted")
	body := content[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
