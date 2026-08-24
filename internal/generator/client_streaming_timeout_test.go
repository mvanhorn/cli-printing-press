package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientStreamingTimeoutCarveOut(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("stream-timeout")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	client := string(clientSrc)

	assert.Contains(t, client, "func (c *Client) SetTimeoutExplicit(explicit bool)",
		"generated client must expose SetTimeoutExplicit so root --timeout Changed() can opt streams back into the whole-call bound")
	assert.Contains(t, client, "func StreamingHTTPClient(base *http.Client, headerTimeout time.Duration) *http.Client",
		"generated client must emit StreamingHTTPClient for binary/stream transfers")
	assert.Contains(t, client, "clone.Timeout = 0",
		"streaming client must drop the whole-call Timeout")
	assert.Contains(t, client, "tr.ResponseHeaderTimeout = headerTimeout",
		"streaming client must keep a header-stall bound")
	assert.Contains(t, client, "if binaryResponse && !c.timeoutExplicit {",
		"doInternal must swap to the streaming client only for unmarked binary transfers")
	assert.Contains(t, client, "httpClient = StreamingHTTPClient(c.HTTPClient, c.ConfiguredTimeout())",
		"binary transfers must reuse the configured --timeout as the header-stall bound")
	assert.Contains(t, client, "&http.Client{Timeout: timeout, Jar: jar, Transport: tr}",
		"ordinary JSON clients must keep the whole-call Timeout")

	rootSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	root := string(rootSrc)

	assert.Contains(t, root, `rootCmd.PersistentFlags().DurationVar(&flags.timeout, "timeout", 60*time.Second, "Request timeout")`,
		"root --timeout default must stay 60s for JSON calls")
	assert.Contains(t, root, `flags.timeoutExplicit = cmd.Flags().Changed("timeout")`,
		"root must record whether the operator set --timeout")
	assert.Contains(t, root, "c.SetTimeoutExplicit(true)",
		"newClient must mark an explicit --timeout so binary transfers still honor it")

	doStart := strings.Index(client, "func (c *Client) doInternal(")
	require.NotEqual(t, -1, doStart, "client.go must contain Client.doInternal")
	doRest := client[doStart:]
	nextFunc := strings.Index(doRest[1:], "\nfunc ")
	require.NotEqual(t, -1, nextFunc, "client.go should have a func after doInternal")
	doBody := doRest[:nextFunc+1]
	assert.Contains(t, doBody, "if binaryResponse && !c.timeoutExplicit {")
	assert.Contains(t, doBody, "httpClient.Do(req)")
	assert.NotContains(t, doBody, "c.HTTPClient.Do(req)",
		"binary-aware Do must go through the selected httpClient, not always HTTPClient")

	testSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client_test.go"))
	require.NoError(t, err)
	assert.Contains(t, string(testSrc), "func TestStreamingHTTPClientDropsWholeCallTimeout",
		"generated client tests must prove StreamingHTTPClient drops the whole-call Timeout")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "streaming_timeout_test.go"), []byte(streamingTimeoutBehaviorTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "StreamingHTTPClientDropsWholeCallTimeout|BinaryResponseIgnoresDefaultClientTimeout|JSONResponseStillHonorsClientTimeout|ExplicitTimeoutStillAbortsBinaryTransfer|BinaryResponseStillTimesOutWhenHeadersStall", "-count=1")
}

const streamingTimeoutBehaviorTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stream-timeout-pp-cli/internal/config"
)

func timeoutLike(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}

func flushThenPause(w http.ResponseWriter, delay time.Duration) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	time.Sleep(delay)
}

func TestBinaryResponseIgnoresDefaultClientTimeout(t *testing.T) {
	const clientTimeout = 150 * time.Millisecond
	const bodyDelay = 400 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x00})
		flushThenPause(w, bodyDelay)
		_, _ = w.Write([]byte{0x01, 0x02, 0x03})
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, clientTimeout, 0)
	c.NoCache = true

	start := time.Now()
	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, map[string]string{BinaryResponseHeader: "true"}); err != nil {
		t.Fatalf("binary transfer died under the default whole-client timeout: %v", err)
	}
	if elapsed := time.Since(start); elapsed < bodyDelay {
		t.Fatalf("binary transfer returned too quickly (%s), want at least %s of streamed body", elapsed, bodyDelay)
	}
}

func TestJSONResponseStillHonorsClientTimeout(t *testing.T) {
	const clientTimeout = 150 * time.Millisecond
	const bodyDelay = 400 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":` + "`" + `))
		flushThenPause(w, bodyDelay)
		_, _ = w.Write([]byte(` + "`" + `true}` + "`" + `))
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, clientTimeout, 0)
	c.NoCache = true

	if _, err := c.GetWithHeaders(context.Background(), "/items", nil, nil); err == nil || !timeoutLike(err) {
		t.Fatalf("JSON call should fail when the body outlives the whole-client timeout, got %v", err)
	}
}

func TestExplicitTimeoutStillAbortsBinaryTransfer(t *testing.T) {
	const clientTimeout = 150 * time.Millisecond
	const bodyDelay = 400 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x00})
		flushThenPause(w, bodyDelay)
		_, _ = w.Write([]byte{0x01, 0x02, 0x03})
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, clientTimeout, 0)
	c.SetTimeoutExplicit(true)
	c.NoCache = true

	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, map[string]string{BinaryResponseHeader: "true"}); err == nil || !timeoutLike(err) {
		t.Fatalf("explicit --timeout should still abort a slow binary transfer, got %v", err)
	}
}

func TestBinaryResponseStillTimesOutWhenHeadersStall(t *testing.T) {
	const clientTimeout = 150 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x00, 0x01})
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, clientTimeout, 0)
	c.NoCache = true

	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, map[string]string{BinaryResponseHeader: "true"}); err == nil || !timeoutLike(err) {
		t.Fatalf("binary transfer should still fail when response headers never arrive, got %v", err)
	}
}
`
