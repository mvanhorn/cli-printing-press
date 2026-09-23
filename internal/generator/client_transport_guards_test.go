package generator

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientDecodesDeclaredAcceptEncoding(t *testing.T) {
	t.Parallel()

	var gzipped bytes.Buffer
	zw := gzip.NewWriter(&gzipped)
	_, err := zw.Write([]byte(htmlTableSimpleFixture))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	body := append([]byte(nil), gzipped.Bytes()...)

	var acceptEncoding atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding.Store(r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	apiSpec := htmlTableExtractSpec("gziphtml", 0)
	apiSpec.BaseURL = server.URL
	apiSpec.RequiredHeaders = []spec.RequiredHeader{
		{Name: "Accept-Encoding", Value: "gzip, deflate"},
	}
	apiSpec.Resources["report"] = spec.Resource{
		Description: "HTML tables",
		Endpoints: map[string]spec.Endpoint{
			"sheet": {
				Method:         "GET",
				Path:           "/sheet",
				Description:    "Gzipped salary table",
				ResponseFormat: spec.ResponseFormatHTML,
				HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModeTable},
				Response:       spec.ResponseDef{Type: "array", Item: "html_table_row"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, `req.Header.Set("Accept-Encoding", "gzip, deflate")`)
	require.Contains(t, clientSrc, "decodeContentEncoding(resp.Header.Get(\"Content-Encoding\"), respBody)")
	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	cmd := exec.Command(binaryPath, "report", "sheet", "--json")
	cmd.Env = append(os.Environ(),
		"GZIPHTML_BASE_URL="+server.URL,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "gzip, deflate", acceptEncoding.Load(),
		"spec Accept-Encoding must stay on the request; net/http must not replace it")
	// Without decodeContentEncoding the HTML parser finds no <table>, the
	// command still exits 0, and this length check is what fails.
	rows := decodeHTMLTableRows(t, out)
	require.Len(t, rows, 1, "gzipped HTML under HTTP 200 must decode to the table row; removing decode yields an empty table and exit 0\n%s", out)
	require.Equal(t, "Ada Lovelace", rows[0]["Player"])
	require.Equal(t, "$100", rows[0]["Salary"])

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "content_encoding_redirect_test.go"),
		[]byte(contentEncodingRedirectTestSource(modulePath)),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestDecodeContentEncoding|TestRedirectDestinationRefused|TestClientGzipJSONAndRedirectGuards", "-count=1")
}

func contentEncodingRedirectTestSource(modulePath string) string {
	return `package client

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"` + modulePath + `/internal/cliutil"
	"` + modulePath + `/internal/cliutil/testenv"
	"` + modulePath + `/internal/config"
)

func TestDecodeContentEncoding(t *testing.T) {
	plain := []byte("{\"id\":\"ada\",\"salary\":\"$100\"}")

	gzipped := mustGzip(t, plain)
	got, err := decodeContentEncoding("gzip", gzipped)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("gzip decoded %q, want %q", got, plain)
	}
	got, err = decodeContentEncoding("x-gzip", gzipped)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("x-gzip: %v %q", err, got)
	}
	got, err = decodeContentEncoding("GZIP", gzipped)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("GZIP: %v %q", err, got)
	}

	zlibbed := mustZlib(t, plain)
	got, err = decodeContentEncoding("deflate", zlibbed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("zlib deflate: %v %q", err, got)
	}
	raw := mustFlate(t, plain)
	got, err = decodeContentEncoding("deflate", raw)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("raw deflate: %v %q", err, got)
	}

	stacked := mustGzip(t, mustFlate(t, plain))
	got, err = decodeContentEncoding("deflate, gzip", stacked)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("stacked deflate, gzip: %v %q", err, got)
	}

	got, err = decodeContentEncoding("", plain)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("empty encoding: %v %q", err, got)
	}
	got, err = decodeContentEncoding("identity", plain)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("identity: %v %q", err, got)
	}
	got, err = decodeContentEncoding("gzip", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty body: %v %q", err, got)
	}
	if _, err := decodeContentEncoding("br", []byte("not-brotli")); err == nil {
		t.Fatal("br must error; unknown encodings are not passed through")
	}
}

func TestDecodeContentEncodingRejectsExpansionPastLimit(t *testing.T) {
	over := bytes.Repeat([]byte("A"), maxDecodedBodyBytes+1)
	if _, err := decodeContentEncoding("gzip", mustGzip(t, over)); !errors.Is(err, ErrDecodedBodyTooLarge) {
		t.Fatalf("gzip over limit: %v", err)
	}
	if _, err := decodeContentEncoding("deflate", mustZlib(t, over)); !errors.Is(err, ErrDecodedBodyTooLarge) {
		t.Fatalf("zlib deflate over limit: %v", err)
	}
	if _, err := decodeContentEncoding("deflate", mustFlate(t, over)); !errors.Is(err, ErrDecodedBodyTooLarge) {
		t.Fatalf("raw deflate over limit: %v", err)
	}
	exact := bytes.Repeat([]byte("B"), maxDecodedBodyBytes)
	got, err := decodeContentEncoding("gzip", mustGzip(t, exact))
	if err != nil {
		t.Fatalf("gzip at limit: %v", err)
	}
	if len(got) != maxDecodedBodyBytes {
		t.Fatalf("gzip at limit len=%d, want %d", len(got), maxDecodedBodyBytes)
	}
}

func TestRedirectDestinationRefused(t *testing.T) {
	cases := []struct {
		name    string
		prior   []string
		next    string
		wantErr error
	}{
		{name: "same origin", prior: []string{"https://api.example.com/a"}, next: "https://api.example.com/b"},
		{name: "public cross origin", prior: []string{"https://api.example.com/a"}, next: "https://cdn.example.com/b"},
		{name: "http upgrade keeps going", prior: []string{"http://api.example.com/a"}, next: "https://api.example.com/b"},
		{name: "https to http downgrade", prior: []string{"https://api.example.com/a"}, next: "http://api.example.com/b", wantErr: ErrRedirectProtocolDowngrade},
		{name: "downgrade after upgrade", prior: []string{"http://api.example.com/a", "https://api.example.com/b"}, next: "http://api.example.com/c", wantErr: ErrRedirectProtocolDowngrade},
		{name: "downgrade beats private", prior: []string{"https://example.com/a"}, next: "http://127.0.0.1/b", wantErr: ErrRedirectProtocolDowngrade},
		{name: "off origin loopback", prior: []string{"http://example.com/a"}, next: "http://127.0.0.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "mapped loopback", prior: []string{"http://example.com/a"}, next: "http://[::ffff:127.0.0.1]/b", wantErr: ErrRedirectPrivateDestination},
		{name: "different loopback port", prior: []string{"http://127.0.0.1:9/a"}, next: "http://127.0.0.1:10/b", wantErr: ErrRedirectPrivateDestination},
		{name: "same loopback origin", prior: []string{"http://127.0.0.1:9/a"}, next: "http://127.0.0.1:9/b"},
		{name: "implicit http port is same loopback origin", prior: []string{"http://127.0.0.1/a"}, next: "http://127.0.0.1:80/b"},
		{name: "explicit http port is same loopback origin", prior: []string{"http://127.0.0.1:80/a"}, next: "http://127.0.0.1/b"},
		{name: "leading zero port is same loopback origin", prior: []string{"http://127.0.0.1/a"}, next: "http://127.0.0.1:080/b"},
		{name: "implicit https port is same loopback origin", prior: []string{"https://127.0.0.1/a"}, next: "https://127.0.0.1:443/b"},
		{name: "ipv6 implicit port is same origin", prior: []string{"http://[::1]/a"}, next: "http://[::1]:80/b"},
		{name: "host case and default port are same origin", prior: []string{"https://API.Example.COM/a"}, next: "https://api.example.com:443/b"},
		{name: "scheme case is same origin", prior: []string{"HTTP://127.0.0.1/a"}, next: "http://127.0.0.1:80/b"},
		{name: "scheme change on loopback is off origin", prior: []string{"http://127.0.0.1/a"}, next: "https://127.0.0.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "http port 443 is not https origin", prior: []string{"http://127.0.0.1:443/a"}, next: "https://127.0.0.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "private", prior: []string{"http://example.com/a"}, next: "http://10.1.2.3/b", wantErr: ErrRedirectPrivateDestination},
		{name: "private 172", prior: []string{"http://example.com/a"}, next: "http://172.16.0.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "private 192", prior: []string{"http://example.com/a"}, next: "http://192.168.1.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "link local", prior: []string{"http://example.com/a"}, next: "http://169.254.169.254/b", wantErr: ErrRedirectPrivateDestination},
		{name: "link local v6", prior: []string{"http://example.com/a"}, next: "http://[fe80::1]/b", wantErr: ErrRedirectPrivateDestination},
		{name: "link local multicast", prior: []string{"http://example.com/a"}, next: "http://224.0.0.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "unspecified", prior: []string{"http://example.com/a"}, next: "http://0.0.0.0/b", wantErr: ErrRedirectPrivateDestination},
		{name: "unspecified v6", prior: []string{"http://example.com/a"}, next: "http://[::]/b", wantErr: ErrRedirectPrivateDestination},
		{name: "https loopback off origin", prior: []string{"https://example.com/a"}, next: "https://127.0.0.1/b", wantErr: ErrRedirectPrivateDestination},
		{name: "file scheme", prior: []string{"https://api.example.com/a"}, next: "file:///etc/passwd", wantErr: ErrRedirectUnsupportedScheme},
		{name: "empty target", prior: []string{"https://api.example.com/a"}, wantErr: ErrRedirectUnsupportedScheme},
		{name: "hostname localhost is not a literal", prior: []string{"https://api.example.com/a"}, next: "https://localhost/b"},
		{name: "metadata hostname is not a literal", prior: []string{"https://api.example.com/a"}, next: "https://metadata.google.internal/b"},
		{name: "cgnat is not private", prior: []string{"http://example.com/a"}, next: "http://100.64.1.1/b"},
		{name: "decimal obfuscation is not a literal", prior: []string{"http://example.com/a"}, next: "http://2130706433/b"},
		{name: "empty via fails closed on a literal", next: "http://127.0.0.1/b", wantErr: ErrRedirectPrivateDestination},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var via []*http.Request
			for _, raw := range tc.prior {
				parsed, err := url.Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				via = append(via, &http.Request{URL: parsed})
			}
			var next *url.URL
			if tc.next != "" {
				parsed, err := url.Parse(tc.next)
				if err != nil {
					t.Fatal(err)
				}
				next = parsed
			}
			err := redirectDestinationRefused(next, via)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("redirectDestinationRefused() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("redirectDestinationRefused() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestClientGzipJSONAndRedirectGuards(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	testenv.Isolate(t, cliutil.CacheDir)

	plain := []byte("[{\"id\":\"ada\"}]")
	var acceptEncoding atomic.Value
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding.Store(r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(mustGzip(t, plain))
	}))
	t.Cleanup(jsonSrv.Close)

	c := newGuardClient(t, jsonSrv.URL)
	got, err := c.Get(context.Background(), "/items", nil)
	if err != nil {
		t.Fatalf("gzip Get: %v", err)
	}
	if acceptEncoding.Load() != "gzip, deflate" {
		t.Fatalf("Accept-Encoding = %v, want gzip, deflate", acceptEncoding.Load())
	}
	var rows []map[string]string
	if err := json.Unmarshal(got, &rows); err != nil {
		t.Fatalf("gzip body is not JSON (compressed bytes fail this way): %v body=%q", err, got)
	}
	if len(rows) != 1 || rows[0]["id"] != "ada" {
		t.Fatalf("rows = %#v", rows)
	}

	var sameSrv *httptest.Server
	sameSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, sameSrv.URL+"/done", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[{\"id\":\"row\"}]"))
	}))
	t.Cleanup(sameSrv.Close)
	same := newGuardClient(t, sameSrv.URL)
	got, err = same.Get(context.Background(), "/start", nil)
	if err != nil {
		t.Fatalf("same-origin redirect: %v", err)
	}
	if err := json.Unmarshal(got, &rows); err != nil || len(rows) != 1 || rows[0]["id"] != "row" {
		t.Fatalf("same-origin body = %q err=%v", got, err)
	}

	var victimHits atomic.Int32
	victim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		victimHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(victim.Close)
	hopper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, victim.URL+"/secret", http.StatusFound)
	}))
	t.Cleanup(hopper.Close)
	off := newGuardClient(t, hopper.URL)
	_, err = off.Get(context.Background(), "/start", nil)
	if !errors.Is(err, ErrRedirectPrivateDestination) {
		t.Fatalf("off-origin loopback = %v, want ErrRedirectPrivateDestination", err)
	}
	if victimHits.Load() != 0 {
		t.Fatalf("victim was hit %d times", victimHits.Load())
	}

	for _, loc := range []string{
		"http://10.1.2.3/",
		"http://169.254.169.254/",
		"http://0.0.0.0/",
		"http://[::1]/",
	} {
		loc := loc
		t.Run(loc, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", loc)
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(srv.Close)
			client := newGuardClient(t, srv.URL)
			_, err := client.Get(context.Background(), "/hop", nil)
			if !errors.Is(err, ErrRedirectPrivateDestination) {
				t.Fatalf("Location %s = %v, want ErrRedirectPrivateDestination", loc, err)
			}
		})
	}

	var plainHits atomic.Int32
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(plainSrv.Close)
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plainSrv.URL+"/downgrade", http.StatusFound)
	}))
	t.Cleanup(tlsSrv.Close)
	tlsClient := newGuardClient(t, tlsSrv.URL)
	installTestTLS(t, tlsClient, tlsSrv)
	_, err = tlsClient.Get(context.Background(), "/start", nil)
	if !errors.Is(err, ErrRedirectProtocolDowngrade) {
		t.Fatalf("https to http = %v, want ErrRedirectProtocolDowngrade", err)
	}
	if plainHits.Load() != 0 {
		t.Fatalf("downgrade target was hit %d times", plainHits.Load())
	}

	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "file:///etc/passwd")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(fileSrv.Close)
	fileClient := newGuardClient(t, fileSrv.URL)
	_, err = fileClient.Get(context.Background(), "/start", nil)
	if !errors.Is(err, ErrRedirectUnsupportedScheme) {
		t.Fatalf("file redirect = %v, want ErrRedirectUnsupportedScheme", err)
	}
}

func newGuardClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c := New(&config.Config{BaseURL: baseURL}, 5*time.Second, 0)
	c.NoCache = true
	return c
}

func installTestTLS(t *testing.T, c *Client, srv *httptest.Server) {
	t.Helper()
	base, ok := srv.Client().Transport.(*http.Transport)
	if !ok || base.TLSClientConfig == nil {
		t.Fatal("httptest TLS transport missing")
	}
	tr, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport %T", c.HTTPClient.Transport)
	}
	tr.TLSClientConfig = base.TLSClientConfig.Clone()
}

func mustGzip(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustZlib(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustFlate(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
`
}
