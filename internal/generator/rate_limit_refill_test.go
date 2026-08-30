package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientWaitsForRateLimitRefill(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("ratelimitwait")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.Contains(t, clientSrc, "func (c *Client) waitForRateLimitRefill(ctx context.Context, wait time.Duration) error",
		"generated client must wait for the politeness budget after retries exhaust")
	assert.Contains(t, clientSrc, "timed out waiting %s for rate-limit budget to refill",
		"short --timeout must surface a refill-wait timeout, not RateLimitedError")
	assert.Contains(t, clientSrc, "if canRetryAmbiguousFailure && attempt >= maxRetries {",
		"safe requests must keep waiting after the 429 retry policy is exhausted")

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, `"rate-limit", client.RateLimitAuto,`)
	assert.Contains(t, rootSrc, `f.DefValue = "auto"`)

	const runtimeTest = `package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ratelimitwait-pp-cli/internal/config"
	"ratelimitwait-pp-cli/internal/platform"
)

type refillRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn refillRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func refillResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(` + "`{}`" + `)),
		Request:    req,
	}
}

func newRefillClient(t *testing.T, timeout time.Duration, rateLimit float64, transport http.RoundTripper) *Client {
	t.Helper()
	c := New(&config.Config{
		BaseURL: "https://api.example.invalid",
		Path:    filepath.Join(t.TempDir(), "config.toml"),
	}, timeout, rateLimit)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: transport, Timeout: timeout}
	return c
}

func TestRateLimitRefill_ThreeSequentialReadsPaceWithoutError(t *testing.T) {
	var calls int
	c := newRefillClient(t, 5*time.Second, 2, refillRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return refillResponse(req, http.StatusOK), nil
	}))

	started := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "/items", nil); err != nil {
			t.Fatalf("Get(%d) = %v; want success without --rate-limit 0", i+1, err)
		}
	}
	elapsed := time.Since(started)
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("three paced reads elapsed %s, want the third visibly delayed", elapsed)
	}
}

func TestRateLimitRefill_WaitsAfterRetriesInsteadOfRateLimitedError(t *testing.T) {
	var calls int
	c := newRefillClient(t, 5*time.Second, 0, refillRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls <= 4 {
			resp := refillResponse(req, http.StatusTooManyRequests)
			resp.Header.Set("Retry-After", "0")
			return resp, nil
		}
		return refillResponse(req, http.StatusOK), nil
	}))

	started := time.Now()
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatalf("Get() after exhausted 429 retries = %v; want wait-for-refill then success", err)
	}
	elapsed := time.Since(started)
	if calls != 5 {
		t.Fatalf("calls = %d, want 4 exhausted 429s then 1 success", calls)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("refill wait elapsed %s, want ~1s fallback after Retry-After: 0", elapsed)
	}
}

func TestRateLimitRefill_ShortTimeoutIsTimeoutNotRateLimitedError(t *testing.T) {
	var calls int
	c := newRefillClient(t, 80*time.Millisecond, 0, refillRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		resp := refillResponse(req, http.StatusTooManyRequests)
		resp.Header.Set("Retry-After", "0")
		return resp, nil
	}))

	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil {
		t.Fatal("Get() = nil; want timeout while waiting for refill")
	}
	var limited *platform.RateLimitedError
	if errors.As(err, &limited) {
		t.Fatalf("Get() = %v; want timeout, not RateLimitedError", err)
	}
	if !strings.Contains(err.Error(), "timed out waiting") {
		t.Fatalf("Get() = %v; want a clear rate-limit wait timeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get() = %v; want context deadline exceeded", err)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 4 (retry policy) before the refill wait times out", calls)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "rate_limit_refill_test.go"), []byte(runtimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestRateLimitRefill_", "-count=1")
}
