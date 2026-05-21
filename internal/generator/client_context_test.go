package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientThreadsCallContextThroughHTTPRequestsAndRetryWaits(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("client-context")
	outputDir := filepath.Join(t.TempDir(), "client-context-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	emitted := string(src)

	assert.Contains(t, emitted, `"context"`,
		"client.go should import context for per-call cancellation")
	assert.Contains(t, emitted, "func sleepContext(ctx context.Context, wait time.Duration) error",
		"client.go should emit a context-aware retry sleep helper")

	for _, signature := range []string{
		"func (c *Client) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error)",
		"func (c *Client) GetWithHeaders(ctx context.Context, path string, params map[string]string, headers map[string]string) (json.RawMessage, error)",
		"func (c *Client) Post(ctx context.Context, path string, body any) (json.RawMessage, int, error)",
		"func (c *Client) DeleteWithParams(ctx context.Context, path string, params map[string]string) (json.RawMessage, int, error)",
		"func (c *Client) Put(ctx context.Context, path string, body any) (json.RawMessage, int, error)",
		"func (c *Client) Patch(ctx context.Context, path string, body any) (json.RawMessage, int, error)",
	} {
		assert.Contains(t, emitted, signature)
	}

	assert.Contains(t, emitted, "func (c *Client) do(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error)")
	assert.Contains(t, emitted, "func (c *Client) doRead(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error)")
	assert.Contains(t, emitted, "func (c *Client) doInternal(ctx context.Context, method, path string, params map[string]string, body any, headerOverrides map[string]string, readOnlyIntent bool) (json.RawMessage, int, error)")

	doStart := strings.Index(emitted, "func (c *Client) doInternal(")
	require.NotEqual(t, -1, doStart, "client.go must contain Client.doInternal function")
	doRest := emitted[doStart:]
	nextFunc := strings.Index(doRest[1:], "\nfunc ")
	require.NotEqual(t, -1, nextFunc, "client.go should have at least one func after doInternal")
	doBody := doRest[:nextFunc+1]

	assert.Contains(t, doBody, "http.NewRequestWithContext(ctx, method, targetURL, bodyReader)",
		"regular client requests should use the caller's context")
	assert.Contains(t, doBody, "if ctxErr := ctx.Err(); ctxErr != nil {\n\t\t\t\treturn nil, 0, ctxErr\n\t\t\t}",
		"request cancellation should not burn through retry attempts")
	assert.Contains(t, doBody, "if err := sleepContext(ctx, wait); err != nil {\n\t\t\t\treturn nil, 0, err\n\t\t\t}",
		"retry sleeps should return promptly when the caller context is cancelled")
	assert.NotContains(t, doBody, "http.NewRequest(method, targetURL, bodyReader)")
	assert.NotContains(t, doBody, "time.Sleep(wait)")
}
