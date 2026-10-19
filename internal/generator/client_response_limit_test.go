package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientBoundsResponseBodies(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("resp-limit")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	envName := naming.EnvPrefix(apiSpec.Name) + "_MAX_RESPONSE_BYTES"
	require.Contains(t, clientSrc, envName)
	require.NotContains(t, clientSrc, "io.ReadAll(resp.Body)")
	require.Equal(t, 1, strings.Count(clientSrc, "streamIdle ="),
		"the idle window is armed only for unmarked binary transfers")
	idleIf := strings.Index(clientSrc, "if binaryResponse && !c.timeoutExplicit {")
	require.NotEqual(t, -1, idleIf)
	idleBlock := clientSrc[idleIf:]
	idleEnd := strings.Index(idleBlock, "\n\t\t}")
	require.NotEqual(t, -1, idleEnd)
	require.Contains(t, idleBlock[:idleEnd], "streamIdle = c.ConfiguredTimeout()")
	require.Contains(t, clientSrc, "readLimitedResponse(resp, streamIdle, cancelAttempt)")

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "response_limit_test.go"),
		[]byte(responseLimitBehaviorTest(modulePath)),
		0o644,
	))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "ResponseLimit", "-count=1")
}

func TestGeneratedOAuthResponseReadsUseBodyLimit(t *testing.T) {
	t.Parallel()

	ccSpec := &spec.APISpec{
		Name:    "resplimitcc",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:        "bearer_token",
			Header:      "Authorization",
			Format:      "Bearer {token}",
			OAuth2Grant: spec.OAuth2GrantClientCredentials,
			TokenURL:    "https://api.example.com/oauth/token",
			EnvVars:     []string{"RESPLIMITCC_API_KEY", "RESPLIMITCC_SECRET_KEY"},
		},
		Config:    spec.ConfigSpec{Format: "toml", Path: "~/.config/resplimitcc-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{"items": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}}}},
	}
	ccDir := filepath.Join(t.TempDir(), naming.CLI(ccSpec.Name))
	require.NoError(t, New(ccSpec, ccDir).Generate())
	ccSrc := readGeneratedFile(t, ccDir, "internal", "client", "client.go")
	mint := generatedFuncBody(ccSrc, "func (c *Client) mintClientCredentials(")
	require.NotEmpty(t, mint)
	require.Contains(t, mint, "readResponseBody(resp.Body)")
	require.NotContains(t, mint, "io.ReadAll(resp.Body)")

	acSpec := &spec.APISpec{
		Name:    "resplimitac",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth: spec.AuthConfig{
			Type:             "bearer_token",
			Header:           "Authorization",
			Format:           "Bearer {token}",
			AuthorizationURL: "https://api.example.com/oauth/authorize",
			TokenURL:         "https://api.example.com/oauth/token",
			EnvVars:          []string{"RESPLIMITAC_TOKEN"},
		},
		Config:    spec.ConfigSpec{Format: "toml", Path: "~/.config/resplimitac-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{"items": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/items"}}}},
	}
	acDir := filepath.Join(t.TempDir(), naming.CLI(acSpec.Name))
	require.NoError(t, New(acSpec, acDir).Generate())
	acSrc := readGeneratedFile(t, acDir, "internal", "client", "client.go")
	refresh := generatedFuncBody(acSrc, "func (c *Client) refreshAccessToken(")
	require.NotEmpty(t, refresh)
	require.Contains(t, refresh, "readResponseBody(resp.Body)")
	require.NotContains(t, refresh, "io.ReadAll(resp.Body)")
	require.NotContains(t, refresh, "json.NewDecoder(resp.Body)")
	requireGeneratedCompiles(t, ccDir)
	requireGeneratedCompiles(t, acDir)
}

func generatedFuncBody(src, sig string) string {
	start := strings.Index(src, sig)
	if start < 0 {
		return ""
	}
	rest := src[start:]
	next := strings.Index(rest[1:], "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:next+1]
}

func responseLimitBehaviorTest(modulePath string) string {
	return `package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"` + modulePath + `/internal/config"
)

func TestResponseLimitOverride(t *testing.T) {
	if got := maxResponseBytes(); got != defaultMaxResponseBytes {
		t.Fatalf("default = %d, want %d", got, defaultMaxResponseBytes)
	}
	t.Setenv(maxResponseBytesEnv, "  65536  ")
	if got := maxResponseBytes(); got != 65536 {
		t.Fatalf("override = %d", got)
	}
	t.Setenv(maxResponseBytesEnv, "0")
	if got := maxResponseBytes(); got != defaultMaxResponseBytes {
		t.Fatalf("zero = %d", got)
	}
	t.Setenv(maxResponseBytesEnv, "-5")
	if got := maxResponseBytes(); got != defaultMaxResponseBytes {
		t.Fatalf("negative = %d", got)
	}
	t.Setenv(maxResponseBytesEnv, "nope")
	if got := maxResponseBytes(); got != defaultMaxResponseBytes {
		t.Fatalf("invalid = %d", got)
	}
	t.Setenv(maxResponseBytesEnv, "${UNRESOLVED}")
	if got := maxResponseBytes(); got != defaultMaxResponseBytes {
		t.Fatalf("placeholder = %d", got)
	}
	t.Setenv(maxResponseBytesEnv, strconv.FormatInt(math.MaxInt64, 10))
	if got := maxResponseBytes(); got != maxResponseBytesCeiling {
		t.Fatalf("ceiling = %d, want %d", got, maxResponseBytesCeiling)
	}
	if maxResponseBytesCeiling >= math.MaxInt64 {
		t.Fatalf("ceiling %d allows limit+1 to overflow", maxResponseBytesCeiling)
	}
	t.Setenv(maxResponseBytesEnv, "9223372036854775808")
	if got := maxResponseBytes(); got != defaultMaxResponseBytes {
		t.Fatalf("overflow parse = %d", got)
	}
}

func TestResponseLimitTooLarge(t *testing.T) {
	t.Setenv(maxResponseBytesEnv, strconv.Itoa(64<<10))
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		wrote := 0
		for wrote < 8<<20 {
			n, err := w.Write(chunk)
			wrote += n
			if err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, 30*time.Second, 0)
	c.NoCache = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.Get(ctx, "/items", nil)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("got %v after %s", err, elapsed)
	}
	if !strings.Contains(err.Error(), maxResponseBytesEnv) {
		t.Fatalf("error %q does not name %s", err, maxResponseBytesEnv)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took %s; the limit should fail once the ceiling is crossed", elapsed)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestResponseLimitExact(t *testing.T) {
	const limit = 64 << 10
	t.Setenv(maxResponseBytesEnv, strconv.Itoa(limit))
	body := bytes.Repeat([]byte("a"), limit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
	c.NoCache = true
	got, err := c.Get(context.Background(), "/items", nil)
	if err != nil {
		t.Fatalf("body at the limit: %v", err)
	}
	if int64(len(got)) != limit {
		t.Fatalf("len = %d, want %d", len(got), limit)
	}
}

func TestResponseLimitTransparentGzip(t *testing.T) {
	t.Setenv(maxResponseBytesEnv, strconv.Itoa(64<<10))
	var plain bytes.Buffer
	zw := gzip.NewWriter(&plain)
	if _, err := zw.Write(bytes.Repeat([]byte("A"), 8<<20)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte(nil), plain.Bytes()...)
	var enc atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc.Store(r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, 30*time.Second, 0)
	c.NoCache = true
	_, err := c.Get(context.Background(), "/items", nil)
	gotEnc, _ := enc.Load().(string)
	if !strings.Contains(strings.ToLower(gotEnc), "gzip") {
		t.Fatalf("Accept-Encoding = %q, want the transport to request transparent gzip", gotEnc)
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrDecodedBodyTooLarge) {
		t.Fatalf("manual decode cap fired: %v", err)
	}
}

func TestResponseLimitBinaryStall(t *testing.T) {
	const idle = 200 * time.Millisecond
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte{0x01}); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, idle, 0)
	c.NoCache = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.GetWithHeaders(ctx, "/blob", nil, map[string]string{BinaryResponseHeader: "true"})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrResponseStalled) {
		t.Fatalf("got %v after %s", err, elapsed)
	}
	if elapsed < idle/2 {
		t.Fatalf("returned in %s, before the idle window", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("stall took %s", elapsed)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestResponseLimitBinaryProgress(t *testing.T) {
	const idle = 500 * time.Millisecond
	const tick = 20 * time.Millisecond
	const ticks = 30
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher, _ := w.(http.Flusher)
		for i := 0; i < ticks; i++ {
			if _, err := w.Write([]byte{byte(i)}); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			if i+1 < ticks {
				time.Sleep(tick)
			}
		}
	}))
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, idle, 0)
	c.NoCache = true
	start := time.Now()
	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, map[string]string{BinaryResponseHeader: "true"}); err != nil {
		t.Fatalf("progressing download: %v", err)
	}
	if elapsed := time.Since(start); elapsed < idle {
		t.Fatalf("finished in %s, want longer than the idle window %s", elapsed, idle)
	}
}

func TestResponseLimitExplicitZero(t *testing.T) {
	t.Setenv(maxResponseBytesEnv, strconv.Itoa(64<<10))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/huge") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(bytes.Repeat([]byte("y"), 8<<20))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if _, err := w.Write([]byte{0x01}); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte{0x02})
	}))
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, 0, 0)
	c.SetTimeoutExplicit(true)
	c.NoCache = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.GetWithHeaders(ctx, "/blob", nil, map[string]string{BinaryResponseHeader: "true"}); err != nil {
		t.Fatalf("explicit timeout 0: %v", err)
	}
	_, err := c.Get(context.Background(), "/huge", nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("explicit timeout 0 size limit: %v", err)
	}
}

type responseLimitCountListener struct {
	net.Listener
	n *atomic.Int32
}

func (l *responseLimitCountListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.n.Add(1)
	}
	return c, err
}

func TestResponseLimitKeepAlive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	srv.Listener.Close()
	srv.Listener = &responseLimitCountListener{Listener: ln, n: &conns}
	srv.Start()
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
	c.NoCache = true
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "/items", nil); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("accepted %d connections, want 1", got)
	}
}

// responseLimitSharedTransport hides *http.Transport so StreamingHTTPClient
// does not clone it. Production binary calls each get a fresh pool; this
// wrapper keeps one pool so cancel-after-read is observable.
type responseLimitSharedTransport struct {
	http.RoundTripper
}

func (t responseLimitSharedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.RoundTripper.RoundTrip(r)
}

func TestResponseLimitBinaryKeepAlive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x01, 0x02, 0x03})
	}))
	srv.Listener.Close()
	srv.Listener = &responseLimitCountListener{Listener: ln, n: &conns}
	srv.Start()
	defer srv.Close()

	c := New(&config.Config{BaseURL: srv.URL}, 2*time.Second, 0)
	c.NoCache = true
	c.HTTPClient.Transport = responseLimitSharedTransport{RoundTripper: c.HTTPClient.Transport}
	for i := 0; i < 3; i++ {
		if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, map[string]string{BinaryResponseHeader: "true"}); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("accepted %d connections, want 1", got)
	}
}
`
}
