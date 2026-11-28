package reachability

import (
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServerCertPool(t *testing.T, srv *httptest.Server) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
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
