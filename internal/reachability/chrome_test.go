package reachability

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServerCertPool(t *testing.T, servers ...*httptest.Server) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	for _, srv := range servers {
		pool.AddCert(srv.Certificate())
	}
	return pool
}

func TestChromeClient_TLSHTTP2AndHeaders(t *testing.T) {
	var gotProtoMajor int
	var gotHeaders http.Header
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProtoMajor = r.ProtoMajor
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	client, err := newChromeClientWithRoots(5*time.Second, testServerCertPool(t, srv))
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"ok":true}`, string(body))
	assert.Equal(t, 2, gotProtoMajor, "Chrome transport should negotiate HTTP/2 when server supports it")
	assert.Equal(t, chromeUserAgent, gotHeaders.Get("User-Agent"))
	assert.Equal(t, chromeAccept, gotHeaders.Get("Accept"))
	assert.Equal(t, chromeAcceptLanguage, gotHeaders.Get("Accept-Language"))
	assert.Equal(t, chromeSecCHUA, gotHeaders.Get("sec-ch-ua"))
	assert.Equal(t, chromeSecCHUAMobile, gotHeaders.Get("sec-ch-ua-mobile"))
	assert.Equal(t, chromeSecCHUAPlatform, gotHeaders.Get("sec-ch-ua-platform"))
	assert.Empty(t, req.Header.Get("User-Agent"), "chromeHeaderTripper must not mutate caller request")
}

func TestChromeClient_TLSHTTP1Fallback(t *testing.T) {
	var gotProtoMajors []int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProtoMajors = append(gotProtoMajors, r.ProtoMajor)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	client, err := newChromeClientWithRoots(5*time.Second, testServerCertPool(t, srv))
	require.NoError(t, err)

	for range 2 {
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}
	assert.Equal(t, []int{1, 1}, gotProtoMajors, "Chrome transport should fall back to HTTP/1.1 when server does not negotiate h2")
}

func TestChromeClient_PlainHTTP(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain"))
	}))
	defer srv.Close()

	client, err := buildClient(TransportSurfChrome, Options{Timeout: 5 * time.Second})
	require.NoError(t, err)

	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, chromeUserAgent, gotUA)
}

func TestFormatProxyAddr(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "http hostname default port", raw: "http://proxy.example", want: "proxy.example:80"},
		{name: "https hostname default port", raw: "https://proxy.example", want: "proxy.example:443"},
		{name: "explicit port", raw: "http://proxy.example:8080", want: "proxy.example:8080"},
		{name: "http portless ipv6", raw: "http://[2001:db8::1]", want: "[2001:db8::1]:80"},
		{name: "https portless ipv6", raw: "https://[2001:db8::1]", want: "[2001:db8::1]:443"},
		{name: "ipv6 explicit port", raw: "http://[::1]:3128", want: "[::1]:3128"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, formatProxyAddr(u))
		})
	}
}

// startTestConnectProxy runs a local HTTP or HTTPS CONNECT proxy that tunnels
// requests to targetAddr (bypassing DNS so non-loopback hostnames like
// example.com route through httpproxy.FromEnvironment() hermetically).
func startTestConnectProxy(t *testing.T, useTLS bool, targetAddr, expectedAuth, rejectStatus string) (*httptest.Server, chan string) {
	t.Helper()
	gotAuth := make(chan string, 4)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		gotAuth <- r.Header.Get("Proxy-Authorization")
		if rejectStatus != "" {
			w.WriteHeader(http.StatusProxyAuthRequired)
			_, _ = w.Write([]byte(rejectStatus))
			return
		}
		if expectedAuth != "" && r.Header.Get("Proxy-Authorization") != expectedAuth {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		dialAddr := targetAddr
		if dialAddr == "" {
			dialAddr = r.Host
		}
		upstream, err := net.DialTimeout("tcp", dialAddr, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			upstream.Close()
			http.Error(w, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		clientConn, buf, err := hj.Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() {
			defer upstream.Close()
			defer clientConn.Close()
			_, _ = io.Copy(upstream, buf)
		}()
		go func() {
			defer upstream.Close()
			defer clientConn.Close()
			_, _ = io.Copy(clientConn, upstream)
		}()
	})
	var srv *httptest.Server
	if useTLS {
		srv = httptest.NewTLSServer(handler)
	} else {
		srv = httptest.NewServer(handler)
	}
	t.Cleanup(srv.Close)
	return srv, gotAuth
}

func TestChromeClient_HTTPProxyCONNECTWithAuth(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("via-http-proxy"))
	}))
	origin.EnableHTTP2 = true
	origin.StartTLS()
	defer origin.Close()

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	proxySrv, gotAuth := startTestConnectProxy(t, false, origin.Listener.Addr().String(), wantAuth, "")

	proxyURL, err := url.Parse(proxySrv.URL)
	require.NoError(t, err)
	proxyURL.User = url.UserPassword("alice", "s3cret")
	t.Setenv("HTTPS_PROXY", proxyURL.String())
	t.Setenv("https_proxy", proxyURL.String())
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	client, err := newChromeClientWithRoots(5*time.Second, testServerCertPool(t, origin))
	require.NoError(t, err)

	// httptest's default TLS certificate includes example.com, which is non-loopback
	// so httpproxy.FromEnvironment() routes it through HTTPS_PROXY.
	resp, err := client.Get("https://example.com/")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "via-http-proxy", string(body))
	select {
	case auth := <-gotAuth:
		assert.Equal(t, wantAuth, auth)
	default:
		t.Fatal("expected request to tunnel through HTTP CONNECT proxy")
	}
}

func TestChromeClient_HTTPSProxyCONNECT(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("via-https-proxy"))
	}))
	defer origin.Close()

	proxySrv, gotAuth := startTestConnectProxy(t, true, origin.Listener.Addr().String(), "", "")
	t.Setenv("HTTPS_PROXY", proxySrv.URL)
	t.Setenv("https_proxy", proxySrv.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	client, err := newChromeClientWithRoots(5*time.Second, testServerCertPool(t, origin, proxySrv))
	require.NoError(t, err)

	resp, err := client.Get("https://example.com/")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "via-https-proxy", string(body))
	select {
	case <-gotAuth:
	default:
		t.Fatal("expected request to tunnel through HTTPS CONNECT proxy")
	}
}

func TestChromeClient_ProxyErrors(t *testing.T) {
	t.Run("non-200 CONNECT rejected", func(t *testing.T) {
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer origin.Close()

		proxySrv, _ := startTestConnectProxy(t, false, origin.Listener.Addr().String(), "", "auth required")
		t.Setenv("HTTPS_PROXY", proxySrv.URL)
		t.Setenv("https_proxy", proxySrv.URL)
		t.Setenv("NO_PROXY", "")
		t.Setenv("no_proxy", "")

		client, err := newChromeClientWithRoots(5*time.Second, testServerCertPool(t, origin))
		require.NoError(t, err)

		_, err = client.Get("https://example.com/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "proxy CONNECT returned 407")
	})

	t.Run("unsupported proxy scheme rejected", func(t *testing.T) {
		t.Setenv("HTTPS_PROXY", "socks5://127.0.0.1:1080")
		t.Setenv("https_proxy", "socks5://127.0.0.1:1080")
		t.Setenv("NO_PROXY", "")
		t.Setenv("no_proxy", "")

		client, err := newChromeClientWithRoots(5*time.Second, nil)
		require.NoError(t, err)

		_, err = client.Get("https://example.invalid/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unsupported proxy scheme "socks5"`)
	})

	t.Run("untrusted HTTPS proxy certificate rejected", func(t *testing.T) {
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer origin.Close()

		proxySrv, _ := startTestConnectProxy(t, true, origin.Listener.Addr().String(), "", "")
		t.Setenv("HTTPS_PROXY", proxySrv.URL)
		t.Setenv("https_proxy", proxySrv.URL)
		t.Setenv("NO_PROXY", "")
		t.Setenv("no_proxy", "")

		// Empty cert pool trusts neither the proxy nor the origin: TLS handshake to proxy must fail.
		client, err := newChromeClientWithRoots(5*time.Second, x509.NewCertPool())
		require.NoError(t, err)

		_, err = client.Get("https://example.com/")
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "unknown authority"), "unexpected error: %v", err)
	})
}

func TestChromeProxyCONNECT_PreservesBufferedTrailingBytes(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()

	// Write the CONNECT response and immediate post-tunnel payload in one write
	// so bufio.Reader buffers trailing bytes past the HTTP response headers.
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(serverSide))
		if err != nil {
			return
		}
		_ = req.Body.Close()
		_, _ = serverSide.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\ntrailing-tls-bytes"))
	}()

	proxyURL, err := url.Parse("http://proxy.example:8080")
	require.NoError(t, err)

	br, err := chromeProxyCONNECT(context.Background(), clientSide, "example.com:443", proxyURL)
	require.NoError(t, err)

	prefixed := &chromePrefixedConn{Conn: clientSide, r: br}
	buf := make([]byte, len("trailing-tls-bytes"))
	_, err = io.ReadFull(prefixed, buf)
	require.NoError(t, err)
	assert.Equal(t, "trailing-tls-bytes", string(buf))
}

func TestChromeProxyCONNECT_RespectsContextCancellation(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()

	// Drain the CONNECT request on the proxy side, then stall without replying
	// so chromeProxyCONNECT must unblock via context cancellation.
	reqRead := make(chan struct{})
	go func() {
		defer close(reqRead)
		req, err := http.ReadRequest(bufio.NewReader(serverSide))
		if err != nil {
			return
		}
		_ = req.Body.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-reqRead
		cancel()
	}()

	proxyURL, err := url.Parse("http://proxy.example:8080")
	require.NoError(t, err)

	start := time.Now()
	_, err = chromeProxyCONNECT(ctx, clientSide, "example.com:443", proxyURL)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "cancelled context should unblock stalled CONNECT read")
}
