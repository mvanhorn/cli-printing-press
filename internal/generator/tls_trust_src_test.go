package generator

import (
	"strconv"
	"strings"
)

func tlsTrustClientTest(mod, prefix string, browser, h1Only bool) string {
	return tlsTrustReplace(`package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	__MOD__
)

const tlsTrustPrefix = __PREFIX__
const tlsTrustH1Only = __H1__
const tlsTrustBrowser = __BROWSER__

func TestTLSTrustPrivateCA(t *testing.T) {
	caPEM, ca, caKey := tlsTrustMintCA(t)
	leaf := tlsTrustMintLeaf(t, ca, caKey, "", net.IPv4(127, 0, 0, 1), 2)
	srv, n := tlsTrustStart(t, leaf, false)
	caPath := tlsTrustWrite(t, caPEM)

	bare := tlsTrustClient(t, srv.URL, "", "")
	if _, err := bare.Get(context.Background(), "/", nil); err == nil {
		t.Fatal("default client trusted a private CA")
	} else if kind, fix, ok := ClassifyTLSVerification(err); !ok || kind != "chain" || !tlsTrustFix(fix, "ca") {
		t.Fatalf("chain classification: kind=%q fix=%q ok=%v err=%v", kind, fix, ok, err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("private CA failure accept count = %d, want 1 (verification failures are not retried)", got)
	}

	trusted := tlsTrustClient(t, srv.URL, caPath, "")
	body, err := trusted.Get(context.Background(), "/", nil)
	if err != nil {
		t.Fatalf("ca-cert client: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("body = %s", body)
	}
	if hc, err := newHTTPClient(time.Second, nil, tlsSettings{CACert: caPath}); err != nil {
		t.Fatal(err)
	} else if tr, ok := hc.Transport.(*http.Transport); ok {
		if tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs == nil {
			t.Fatal("ca-cert did not set RootCAs")
		}
	}
}

func TestTLSTrustHostname(t *testing.T) {
	caPEM, ca, caKey := tlsTrustMintCA(t)
	leaf := tlsTrustMintLeaf(t, ca, caKey, "api.example.com", nil, 3)
	srv, n := tlsTrustStart(t, leaf, false)
	caPath := tlsTrustWrite(t, caPEM)

	mismatch := tlsTrustClient(t, srv.URL, caPath, "")
	if _, err := mismatch.Get(context.Background(), "/", nil); err == nil {
		t.Fatal("IP dial matched a DNS-only certificate")
	} else if kind, fix, ok := ClassifyTLSVerification(err); !ok || kind != "hostname" || !tlsTrustFix(fix, "name") {
		t.Fatalf("hostname classification: kind=%q fix=%q ok=%v err=%v", kind, fix, ok, err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("hostname failure accept count = %d, want 1", got)
	}

	named := tlsTrustClient(t, srv.URL, caPath, "api.example.com")
	if _, err := named.Get(context.Background(), "/", nil); err != nil {
		t.Fatalf("tls-server-name client: %v", err)
	}
	if hc, err := newHTTPClient(time.Second, nil, tlsSettings{ServerName: "api.example.com"}); err != nil {
		t.Fatal(err)
	} else if tr, ok := hc.Transport.(*http.Transport); ok {
		if tr.TLSClientConfig == nil || tr.TLSClientConfig.ServerName != "api.example.com" {
			t.Fatal("tls-server-name did not set ServerName")
		}
		if tr.TLSClientConfig.RootCAs != nil {
			t.Fatal("server name alone set RootCAs")
		}
	}
}

func TestTLSTrustBadBundle(t *testing.T) {
	caPEM, ca, caKey := tlsTrustMintCA(t)
	leaf := tlsTrustMintLeaf(t, ca, caKey, "", net.IPv4(127, 0, 0, 1), 4)
	srv, n := tlsTrustStart(t, leaf, false)
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	trunc := filepath.Join(dir, "trunc.pem")
	if err := os.WriteFile(trunc, []byte("-----BEGIN CERTIFICATE-----\nAAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.pem")
	for _, path := range []string{empty, keyPath, trunc, missing, dir} {
		before := n.Load()
		c := tlsTrustClient(t, srv.URL, path, "")
		if !errors.Is(c.TLSConfigError(), ErrTLSConfig) {
			t.Fatalf("%s: TLSConfigError = %v", path, c.TLSConfigError())
		}
		if _, err := c.Get(context.Background(), "/", nil); !errors.Is(err, ErrTLSConfig) {
			t.Fatalf("%s: Get = %v", path, err)
		}
		if n.Load() != before {
			t.Fatalf("%s: bad bundle sent a request (%d accepts)", path, n.Load()-before)
		}
	}
	extra := append([]byte("commentary\n"), caPEM...)
	extra = append(extra, []byte("\nmore\n")...)
	extraPath := tlsTrustWrite(t, extra)
	if _, err := tlsTrustClient(t, srv.URL, extraPath, "").Get(context.Background(), "/", nil); err != nil {
		t.Fatalf("pem with surrounding text: %v", err)
	}
	unreadable := tlsTrustWrite(t, caPEM)
	if err := os.Chmod(unreadable, 0); err == nil {
		if _, rerr := os.ReadFile(unreadable); rerr != nil {
			before := n.Load()
			c := tlsTrustClient(t, srv.URL, unreadable, "")
			if !errors.Is(c.TLSConfigError(), ErrTLSConfig) {
				t.Fatalf("unreadable bundle: %v", c.TLSConfigError())
			}
			if _, err := c.Get(context.Background(), "/", nil); !errors.Is(err, ErrTLSConfig) || n.Load() != before {
				t.Fatalf("unreadable bundle dialed or returned %v", err)
			}
		}
	}
}

func TestTLSTrustSystemRootsUnavailable(t *testing.T) {
	orig := systemCertPool
	t.Cleanup(func() { systemCertPool = orig })
	path := tlsTrustWrite(t, []byte("-----BEGIN CERTIFICATE-----\nAAAA\n"))
	systemCertPool = func() (*x509.CertPool, error) {
		return nil, errors.New("roots unavailable")
	}
	_, err := newHTTPClient(time.Second, nil, tlsSettings{CACert: path})
	if !errors.Is(err, ErrTLSConfig) || !strings.Contains(err.Error(), "system roots") {
		t.Fatalf("system pool error: %v", err)
	}
	systemCertPool = func() (*x509.CertPool, error) { return nil, nil }
	if _, err := newHTTPClient(time.Second, nil, tlsSettings{CACert: path}); !errors.Is(err, ErrTLSConfig) {
		t.Fatalf("nil system pool: %v", err)
	}
	systemCertPool = func() (*x509.CertPool, error) {
		t.Fatal("empty ca-cert loaded system roots")
		return nil, errors.New("unused")
	}
	if _, err := newHTTPClient(time.Second, nil, tlsSettings{}); err != nil {
		t.Fatal(err)
	}
	systemCertPool = func() (*x509.CertPool, error) {
		t.Fatal("insecure loaded system roots")
		return nil, errors.New("unused")
	}
	if _, err := newHTTPClient(time.Second, nil, tlsSettings{Insecure: true, CACert: path}); err != nil {
		t.Fatalf("insecure: %v", err)
	}
}

func TestTLSTrustOtherNetworkErrorsStillRetry(t *testing.T) {
	caPEM, ca, caKey := tlsTrustMintCA(t)
	leaf := tlsTrustMintLeaf(t, ca, caKey, "", net.IPv4(127, 0, 0, 1), 5)
	srv, n := tlsTrustStart(t, leaf, true)
	if _, err := tlsTrustClient(t, srv.URL, tlsTrustWrite(t, caPEM), "").Get(context.Background(), "/", nil); err != nil {
		t.Fatalf("retry after a closed connection: %v", err)
	}
	if n.Load() < 2 {
		t.Fatalf("non-certificate network error was not retried: accepts=%d", n.Load())
	}
}

func TestTLSTrustDefaults(t *testing.T) {
	hc, err := newHTTPClient(time.Second, nil, tlsSettings{})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := hc.Transport.(*http.Transport)
	if tlsTrustBrowser {
		if ok {
			t.Fatal("browser transport exposed *http.Transport")
		}
		return
	}
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if tlsTrustH1Only {
		if tr.TLSClientConfig == nil || len(tr.TLSClientConfig.NextProtos) != 1 || tr.TLSClientConfig.NextProtos[0] != "http/1.1" {
			t.Fatalf("http/1.1 ALPN = %#v", tr.TLSClientConfig)
		}
		if tr.TLSClientConfig.RootCAs != nil || tr.TLSClientConfig.ServerName != "" || tr.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("empty trust changed verification settings")
		}
		return
	}
	base, _ := http.DefaultTransport.(*http.Transport)
	if base != nil && base.TLSClientConfig == nil && tr.TLSClientConfig != nil {
		t.Fatal("empty trust initialized TLSClientConfig")
	}
	if tr.TLSClientConfig != nil && (tr.TLSClientConfig.RootCAs != nil || tr.TLSClientConfig.ServerName != "") {
		t.Fatal("empty trust set RootCAs or ServerName")
	}
}

func TestTLSTrustNonHTTPTransport(t *testing.T) {
	orig := http.DefaultTransport
	rt := &tlsTrustDenyTripper{}
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = orig })
	for _, trust := range []tlsSettings{{}, {Insecure: true}, {Insecure: true, CACert: t.TempDir(), ServerName: "api.example.com"}} {
		_, err := newHTTPClient(time.Second, nil, trust)
		if !errors.Is(err, ErrTLSConfig) {
			t.Fatalf("trust=%+v err=%v", trust, err)
		}
	}
	c := New(&config.Config{BaseURL: "https://127.0.0.1:1"}, time.Second, 0)
	if _, err := c.Get(context.Background(), "/", nil); !errors.Is(err, ErrTLSConfig) {
		t.Fatalf("Get = %v", err)
	}
	if rt.n.Load() != 0 {
		t.Fatalf("non-*http.Transport handled %d requests", rt.n.Load())
	}
}

func TestTLSTrustInsecureWins(t *testing.T) {
	tlsTrustResetWarn()
	var warned strings.Builder
	orig := writeTLSWarning
	writeTLSWarning = func(format string, args ...any) { fmt.Fprintf(&warned, format, args...) }
	t.Cleanup(func() { writeTLSWarning = orig })

	if _, err := newHTTPClient(time.Second, nil, tlsSettings{Insecure: true}); err != nil {
		t.Fatal(err)
	}
	if warned.Len() != 0 {
		t.Fatalf("insecure without trust settings warned: %s", warned.String())
	}
	tlsTrustResetWarn()
	warned.Reset()
	hc, err := newHTTPClient(time.Second, nil, tlsSettings{Insecure: true, CACert: t.TempDir(), ServerName: "api.example.com"})
	if err != nil {
		t.Fatalf("insecure client read the bundle: %v", err)
	}
	if !strings.Contains(warned.String(), "overrides") {
		t.Fatalf("warning = %q", warned.String())
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	resp, err := hc.Get(srv.URL)
	if err != nil {
		t.Fatalf("insecure client rejected test certificate: %v", err)
	}
	resp.Body.Close()
}

func TestTLSTrustClassifySynthetic(t *testing.T) {
	cert := &x509.Certificate{}
	host := x509.HostnameError{Host: "127.0.0.1", Certificate: cert}
	kind, fix, ok := ClassifyTLSVerification(host)
	if !ok || kind != "hostname" || !tlsTrustFix(fix, "name") {
		t.Fatalf("hostname: kind=%q fix=%q ok=%v", kind, fix, ok)
	}
	wrapped := fmt.Errorf("dial: %w", &tls.CertificateVerificationError{Err: host})
	kind, fix, ok = ClassifyTLSVerification(wrapped)
	if !ok || kind != "hostname" || !tlsTrustFix(fix, "name") {
		t.Fatalf("wrapped hostname: kind=%q fix=%q ok=%v", kind, fix, ok)
	}
	unknown := x509.UnknownAuthorityError{Cert: cert}
	kind, fix, ok = ClassifyTLSVerification(&url.Error{Op: "Get", URL: "https://127.0.0.1/", Err: unknown})
	if !ok || kind != "chain" || !tlsTrustFix(fix, "ca") {
		t.Fatalf("unknown authority: kind=%q fix=%q ok=%v", kind, fix, ok)
	}
	kind, _, ok = ClassifyTLSVerification(x509.SystemRootsError{Err: errors.New("roots")})
	if !ok || kind != "chain" {
		t.Fatalf("system roots: kind=%q ok=%v", kind, ok)
	}
	expired := x509.CertificateInvalidError{Cert: cert, Reason: x509.Expired}
	if _, _, ok := ClassifyTLSVerification(expired); ok {
		t.Fatal("expiry classified as a trust setting")
	}
	if !isCertificateVerificationError(expired) {
		t.Fatal("expiry would be retried")
	}
	usage := x509.CertificateInvalidError{Cert: cert, Reason: x509.IncompatibleUsage}
	if _, _, ok := ClassifyTLSVerification(usage); ok || !isCertificateVerificationError(usage) {
		t.Fatal("incompatible usage classified or would be retried")
	}
}

func TestTLSTrustSymlinkAndSnapshot(t *testing.T) {
	caPEM, ca, caKey := tlsTrustMintCA(t)
	leaf := tlsTrustMintLeaf(t, ca, caKey, "", net.IPv4(127, 0, 0, 1), 6)
	srv, n := tlsTrustStart(t, leaf, false)
	caPath := tlsTrustWrite(t, caPEM)
	link := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.Symlink(caPath, link); err != nil {
		t.Skip(err)
	}
	if _, err := tlsTrustClient(t, srv.URL, link, "").Get(context.Background(), "/", nil); err != nil {
		t.Fatalf("symlink to CA bundle: %v", err)
	}
	empty := tlsTrustWrite(t, nil)
	badLink := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.Symlink(empty, badLink); err != nil {
		t.Fatal(err)
	}
	before := n.Load()
	if err := tlsTrustClient(t, srv.URL, badLink, "").TLSConfigError(); !errors.Is(err, ErrTLSConfig) || n.Load() != before {
		t.Fatalf("symlink to empty bundle: %v accepts=%d", err, n.Load()-before)
	}
	live := tlsTrustClient(t, srv.URL, caPath, "")
	if err := os.WriteFile(caPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := live.Get(context.Background(), "/", nil); err != nil {
		t.Fatalf("client reread the CA bundle: %v", err)
	}
	before = n.Load()
	if err := tlsTrustClient(t, srv.URL, caPath, "").TLSConfigError(); !errors.Is(err, ErrTLSConfig) || n.Load() != before {
		t.Fatalf("fresh client ignored the emptied bundle: %v", err)
	}
}

func tlsTrustFix(fix, kind string) bool {
	switch kind {
	case "ca":
		return strings.Contains(fix, "--ca-cert") && strings.Contains(fix, tlsTrustPrefix+"_CA_CERT") && strings.Contains(fix, "ca_cert")
	case "name":
		return strings.Contains(fix, "--tls-server-name") && strings.Contains(fix, tlsTrustPrefix+"_TLS_SERVER_NAME") && strings.Contains(fix, "tls_server_name")
	default:
		return false
	}
}

func tlsTrustClient(t *testing.T, baseURL, caPath, serverName string) *Client {
	t.Helper()
	c := New(&config.Config{BaseURL: baseURL, CACert: caPath, TLSServerName: serverName}, 5*time.Second, 0)
	c.NoCache = true
	return c
}

func tlsTrustResetWarn() {
	tlsWarnMu.Lock()
	lastTLSWarn = ""
	tlsWarnMu.Unlock()
}

func tlsTrustWrite(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tlsTrustMintCA(t *testing.T) ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tls-trust-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert, key
}

func tlsTrustMintLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dns string, ip net.IP, serial int64) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if dns != "" {
		tmpl.DNSNames = []string{dns}
	}
	if ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

type acceptCounter struct {
	net.Listener
	n *atomic.Int32
}

func (l *acceptCounter) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.n.Add(1)
	return c, nil
}

type eofFirstListener struct {
	net.Listener
	n *atomic.Int32
}

func (l *eofFirstListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if l.n.Add(1) == 1 {
		return &eofConn{Conn: c}, nil
	}
	return c, nil
}

type eofConn struct{ net.Conn }

func (c *eofConn) Read([]byte) (int, error) {
	c.Conn.Close()
	return 0, io.EOF
}

func (c *eofConn) Write([]byte) (int, error) { return 0, io.EOF }

type tlsTrustDenyTripper struct{ n atomic.Int32 }

func (d *tlsTrustDenyTripper) RoundTrip(*http.Request) (*http.Response, error) {
	d.n.Add(1)
	return nil, errors.New("should not dial")
}

func tlsTrustStart(t *testing.T, cert tls.Certificate, firstEOF bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	n := &atomic.Int32{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true}")
	}))
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if firstEOF {
		srv.Listener = &eofFirstListener{Listener: ln, n: n}
	} else {
		srv.Listener = &acceptCounter{Listener: ln, n: n}
	}
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, n
}
`, mod+"/internal/config", prefix, h1Only, browser)
}

func tlsTrustReplace(src, mod, prefix string, h1Only, browser bool) string {
	r := strings.NewReplacer(
		"__MOD__", strconv.Quote(mod),
		"__PREFIX__", strconv.Quote(prefix),
		"__ENVKEY__", prefix+"_",
		"__H1__", strconv.FormatBool(h1Only),
		"__BROWSER__", strconv.FormatBool(browser),
	)
	return r.Replace(src)
}

func tlsTrustConstructTest(prefix string) string {
	return tlsTrustReplace(`package client

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const tlsTrustPrefix = __PREFIX__

func TestTLSTrustConstruct(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, empty, filepath.Join(dir, "missing.pem")} {
		_, err := newHTTPClient(time.Second, nil, tlsSettings{CACert: path})
		if !errors.Is(err, ErrTLSConfig) {
			t.Fatalf("%s: %v", path, err)
		}
	}
	tlsTrustResetWarn()
	var warned strings.Builder
	orig := writeTLSWarning
	writeTLSWarning = func(format string, args ...any) { fmt.Fprintf(&warned, format, args...) }
	t.Cleanup(func() { writeTLSWarning = orig })
	hc, err := newHTTPClient(time.Second, nil, tlsSettings{Insecure: true, CACert: dir, ServerName: "api.example.com"})
	if err != nil || hc == nil {
		t.Fatalf("insecure client = %v, %v", hc, err)
	}
	if !strings.Contains(warned.String(), "overrides") {
		t.Fatalf("warning = %q", warned.String())
	}
	tlsTrustResetWarn()
	warned.Reset()
	if _, err := newHTTPClient(time.Second, nil, tlsSettings{Insecure: true}); err != nil || warned.Len() != 0 {
		t.Fatalf("bare insecure err=%v warning=%q", err, warned.String())
	}
	cert := &x509.Certificate{}
	kind, fix, ok := ClassifyTLSVerification(x509.HostnameError{Host: "127.0.0.1", Certificate: cert})
	if !ok || kind != "hostname" || !strings.Contains(fix, "--tls-server-name") || !strings.Contains(fix, tlsTrustPrefix+"_TLS_SERVER_NAME") {
		t.Fatalf("hostname fix: kind=%q fix=%q ok=%v", kind, fix, ok)
	}
	kind, fix, ok = ClassifyTLSVerification(x509.UnknownAuthorityError{Cert: cert})
	if !ok || kind != "chain" || !strings.Contains(fix, "--ca-cert") || !strings.Contains(fix, tlsTrustPrefix+"_CA_CERT") {
		t.Fatalf("chain fix: kind=%q fix=%q ok=%v", kind, fix, ok)
	}
	expired := x509.CertificateInvalidError{Cert: cert, Reason: x509.Expired}
	if _, _, ok := ClassifyTLSVerification(expired); ok || !isCertificateVerificationError(expired) {
		t.Fatal("expiry classification")
	}
}

func tlsTrustResetWarn() {
	tlsWarnMu.Lock()
	lastTLSWarn = ""
	tlsWarnMu.Unlock()
}
`, "", prefix, false, false)
}

func tlsTrustConfigTest(mod, prefix string) string {
	return tlsTrustReplace(`package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	__MOD__
)

func TestTLSTrustConfig(t *testing.T) {
	home := tlsTrustIsolate(t)
	dir, err := cliutil.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, home) {
		t.Fatalf("config dir %s is outside %s", dir, home)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "ca_cert = \"/file/ca.pem\"\ntls_server_name = \"file.example\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tlsTrustLock(t, path)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CACert != "/file/ca.pem" || cfg.TLSServerName != "file.example" {
		t.Fatalf("file settings = %#v", cfg)
	}
	if saved := cfg.configForSave(); saved.CACert != "/file/ca.pem" || saved.TLSServerName != "file.example" {
		t.Fatalf("file settings would not persist: %#v", saved)
	}

	t.Setenv("__ENVKEY__CA_CERT", "/env/ca.pem")
	t.Setenv("__ENVKEY__TLS_SERVER_NAME", "env.example")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CACert != "/env/ca.pem" || cfg.TLSServerName != "env.example" {
		t.Fatalf("env did not win: ca=%q name=%q", cfg.CACert, cfg.TLSServerName)
	}
	saved := cfg.configForSave()
	if saved.CACert != "/file/ca.pem" || saved.TLSServerName != "file.example" {
		t.Fatalf("env would persist: ca=%q name=%q", saved.CACert, saved.TLSServerName)
	}
	if err := cfg.save(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	if !strings.Contains(text, "/file/ca.pem") || strings.Contains(text, "/env/ca.pem") || !strings.Contains(text, "file.example") || strings.Contains(text, "env.example") {
		t.Fatalf("saved config = %s", text)
	}

	t.Setenv("__ENVKEY__CA_CERT", "")
	t.Setenv("__ENVKEY__TLS_SERVER_NAME", "")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tlsTrustLock(t, path)
	tlsTrustLoosen(t, path)
	stderr := tlsTrustStderr(t, func() {
		cfg, err = Load(path)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CACert != "" || cfg.TLSServerName != "" {
		t.Fatalf("loose config applied ca=%q name=%q", cfg.CACert, cfg.TLSServerName)
	}
	if !strings.Contains(stderr, "ignoring ca_cert and tls_server_name") {
		t.Fatalf("stderr = %q", stderr)
	}
	t.Setenv("__ENVKEY__CA_CERT", "/env/ca.pem")
	t.Setenv("__ENVKEY__TLS_SERVER_NAME", "env.example")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CACert != "/env/ca.pem" || cfg.TLSServerName != "env.example" {
		t.Fatalf("env did not override a loose file: ca=%q name=%q", cfg.CACert, cfg.TLSServerName)
	}
	if saved := cfg.configForSave(); saved.CACert != "" || saved.TLSServerName != "" {
		t.Fatalf("env over a loose file would persist: %#v", saved)
	}

	t.Setenv("__ENVKEY__CA_CERT", "")
	t.Setenv("__ENVKEY__TLS_SERVER_NAME", "")
	empty := filepath.Join(t.TempDir(), "empty.toml")
	if err := os.WriteFile(empty, []byte("base_url = \"https://api.example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tlsTrustLoosen(t, empty)
	stderr = tlsTrustStderr(t, func() {
		cfg, err = Load(empty)
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "ignoring ca_cert") {
		t.Fatalf("empty trust settings warned: %s", stderr)
	}

	if runtime.GOOS != "windows" {
		t.Setenv(strings.ToLower("__ENVKEY__CA_CERT"), "/lower/ca.pem")
		cfg, err = Load(filepath.Join(t.TempDir(), "missing.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CACert != "" {
			t.Fatalf("lowercase env applied %q", cfg.CACert)
		}
	}

	t.Run("symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "target.toml")
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		tlsTrustLoosen(t, target)
		link := filepath.Join(t.TempDir(), "link.toml")
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		cfg, err := Load(link)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CACert != "" || cfg.TLSServerName != "" {
			t.Fatal("symlink to a loose config applied trust settings")
		}
		private := filepath.Join(t.TempDir(), "private.toml")
		if err := os.WriteFile(private, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		tlsTrustLock(t, private)
		privateLink := filepath.Join(t.TempDir(), "private-link.toml")
		if err := os.Symlink(private, privateLink); err != nil {
			t.Fatal(err)
		}
		cfg, err = Load(privateLink)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CACert != "/file/ca.pem" || cfg.TLSServerName != "file.example" {
			t.Fatalf("symlink to a private config dropped settings: %#v", cfg)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		legacyHome := t.TempDir()
		t.Setenv("HOME", legacyHome)
		t.Setenv("USERPROFILE", legacyHome)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(legacyHome, "xdg"))
		legacy, err := LegacyConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		tlsTrustLock(t, legacy)
		cfg, err := Load("")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CACert != "/file/ca.pem" || cfg.TLSServerName != "file.example" {
			t.Fatalf("private legacy config dropped settings: %#v", cfg)
		}
		tlsTrustLoosen(t, legacy)
		cfg, err = Load("")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CACert != "" || cfg.TLSServerName != "" {
			t.Fatalf("loose legacy config applied ca=%q name=%q", cfg.CACert, cfg.TLSServerName)
		}
	})
}

func tlsTrustIsolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, key := range []string{"HOME", "CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR", "CONFIG", "CA_CERT", "TLS_SERVER_NAME", "BASE_URL"} {
		t.Setenv("__ENVKEY__"+key, "")
	}
	return home
}

func tlsTrustLock(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		user := os.Getenv("USERNAME")
		if user == "" {
			t.Skip("USERNAME unset")
		}
		if err := exec.Command("icacls", path, "/inheritance:r", "/grant:r", user+":F").Run(); err != nil {
			t.Skip(err)
		}
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func tlsTrustLoosen(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if err := exec.Command("icacls", path, "/grant", "*S-1-5-32-545:R").Run(); err != nil {
			t.Skip(err)
		}
		return
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
}

func tlsTrustStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}
`, mod+"/internal/cliutil", prefix, false, false)
}

func tlsTrustCLITest(mod, prefix string, browser bool) string {
	extra := ""
	if browser {
		extra = tlsTrustBrowserDoctor()
	}
	return strings.NewReplacer(
		"__CLIENT__", strconv.Quote(mod+"/internal/client"),
		"__CONFIG__", strconv.Quote(mod+"/internal/config"),
		"__ENVKEY__", prefix+"_",
		"__BROWSER_TEST__", extra,
	).Replace(tlsTrustCLISource)
}

const tlsTrustCLISource = `package cli

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	__CLIENT__
	__CONFIG__
)

func TestTLSTrustDoctor(t *testing.T) {
	tlsTrustIsolate(t)
	out, err := tlsTrustDoctor(t)
	if err != nil {
		t.Fatal(err)
	}
	report := tlsTrustReport(t, out)
	tlsVal, _ := report["tls"].(string)
	apiVal, _ := report["api"].(string)
	if tlsVal != "system roots" {
		t.Fatalf("default trust = %q", tlsVal)
	}
	if !strings.Contains(apiVal, "placeholder") {
		t.Fatalf("api = %q", apiVal)
	}
	if strings.Contains(tlsVal, "ca-cert") || strings.Contains(tlsVal, "tls-server-name") {
		t.Fatalf("default summary names a trust override: %q", tlsVal)
	}

	caPEM, ca, caKey := tlsTrustMintCA(t)
	ipLeaf := tlsTrustMintLeaf(t, ca, caKey, "", net.IPv4(127, 0, 0, 1), 2)
	srv, n := tlsTrustStart(t, ipLeaf)
	caPath := tlsTrustWrite(t, caPEM)
	t.Setenv("__ENVKEY__BASE_URL", srv.URL)
	before := n.Load()
	out, err = tlsTrustDoctor(t, "--ca-cert", filepath.Join(t.TempDir(), "missing.pem"))
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) {
		t.Fatalf("missing bundle exit=%d err=%v", ExitCode(err), err)
	}
	if n.Load() != before {
		t.Fatal("missing bundle dialed")
	}
	empty := tlsTrustWrite(t, nil)
	out, err = tlsTrustDoctor(t, "--ca-cert", empty)
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || n.Load() != before {
		t.Fatalf("empty bundle exit=%d err=%v accepts=%d", ExitCode(err), err, n.Load()-before)
	}
	out, err = tlsTrustDoctor(t, "--ca-cert", t.TempDir())
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || n.Load() != before {
		t.Fatalf("directory bundle exit=%d err=%v", ExitCode(err), err)
	}
	keyDER, keyErr := x509.MarshalECPrivateKey(caKey)
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	keyPath := tlsTrustWrite(t, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	out, err = tlsTrustDoctor(t, "--ca-cert", keyPath)
	if ExitCode(err) != 10 || !errors.Is(err, client.ErrTLSConfig) || n.Load() != before {
		t.Fatalf("key bundle exit=%d err=%v", ExitCode(err), err)
	}
	_ = out

	before = n.Load()
	out, err = tlsTrustDoctor(t)
	if err != nil {
		t.Fatal(err)
	}
	report = tlsTrustReport(t, out)
	apiVal, _ = report["api"].(string)
	if !strings.Contains(apiVal, "chain") || !strings.Contains(apiVal, "--ca-cert") || !strings.Contains(apiVal, "__ENVKEY__CA_CERT") || !strings.Contains(apiVal, "ca_cert") {
		t.Fatalf("chain report = %q", apiVal)
	}
	if n.Load()-before != 1 {
		t.Fatalf("chain failure accepts = %d, want 1", n.Load()-before)
	}

	dnsSrv, dnsN := tlsTrustStart(t, tlsTrustMintLeaf(t, ca, caKey, "api.example.com", nil, 3))
	t.Setenv("__ENVKEY__BASE_URL", dnsSrv.URL)
	before = dnsN.Load()
	out, err = tlsTrustDoctor(t, "--ca-cert", caPath)
	if err != nil {
		t.Fatal(err)
	}
	report = tlsTrustReport(t, out)
	apiVal, _ = report["api"].(string)
	tlsVal, _ = report["tls"].(string)
	if !strings.Contains(apiVal, "hostname") || !strings.Contains(apiVal, "--tls-server-name") || !strings.Contains(apiVal, "__ENVKEY__TLS_SERVER_NAME") || !strings.Contains(apiVal, "tls_server_name") {
		t.Fatalf("hostname report = %q", apiVal)
	}
	if !strings.Contains(tlsVal, "ca-cert") {
		t.Fatalf("tls summary = %q", tlsVal)
	}
	if dnsN.Load()-before != 1 {
		t.Fatalf("hostname failure accepts = %d, want 1", dnsN.Load()-before)
	}
	out, err = tlsTrustDoctor(t, "--ca-cert", caPath, "--tls-server-name", "api.example.com")
	if err != nil {
		t.Fatalf("named doctor: %v\n%s", err, out)
	}
	report = tlsTrustReport(t, out)
	apiVal, _ = report["api"].(string)
	tlsVal, _ = report["tls"].(string)
	if !strings.Contains(apiVal, "reachable") {
		t.Fatalf("api = %q", apiVal)
	}
	if !strings.Contains(tlsVal, "ca-cert "+caPath) || !strings.Contains(tlsVal, "tls-server-name api.example.com") {
		t.Fatalf("tls summary = %q", tlsVal)
	}

	__BROWSER_TEST__
}

func TestTLSTrustFlagPrecedence(t *testing.T) {
	tlsTrustIsolate(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "ca_cert = \"/file/ca.pem\"\ntls_server_name = \"file.example\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tlsTrustLock(t, path)
	t.Setenv("__ENVKEY__CA_CERT", "/env/ca.pem")
	t.Setenv("__ENVKEY__TLS_SERVER_NAME", "env.example")

	flags := rootFlags{configPath: path}
	cmd := newRootCmd(&flags)
	if err := cmd.PersistentFlags().Parse([]string{"--ca-cert", "/flag/ca.pem", "--tls-server-name", "flag.example"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	flags.applyTLSFlags(cfg)
	if cfg.CACert != "/flag/ca.pem" || cfg.TLSServerName != "flag.example" {
		t.Fatalf("flag did not win: ca=%q name=%q", cfg.CACert, cfg.TLSServerName)
	}

	flags = rootFlags{configPath: path}
	cmd = newRootCmd(&flags)
	if err := cmd.PersistentFlags().Parse([]string{"--ca-cert", "", "--tls-server-name", "  "}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	flags.applyTLSFlags(cfg)
	if cfg.CACert != "/env/ca.pem" || cfg.TLSServerName != "env.example" {
		t.Fatalf("empty flag overrode env: ca=%q name=%q", cfg.CACert, cfg.TLSServerName)
	}
}

func tlsTrustDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	flags := &rootFlags{}
	cmd := newRootCmd(flags)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append(append([]string{}, args...), "doctor", "--json"))
	err := cmd.Execute()
	return buf.String(), err
}

func tlsTrustReport(t *testing.T, out string) map[string]any {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor json: %v\n%s", err, out)
	}
	return report
}

func tlsTrustIsolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, key := range []string{"HOME", "CONFIG_DIR", "DATA_DIR", "STATE_DIR", "CACHE_DIR", "CONFIG", "CA_CERT", "TLS_SERVER_NAME", "BASE_URL"} {
		t.Setenv("__ENVKEY__"+key, "")
	}
}

func tlsTrustLock(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		user := os.Getenv("USERNAME")
		if user == "" {
			t.Skip("USERNAME unset")
		}
		if err := exec.Command("icacls", path, "/inheritance:r", "/grant:r", user+":F").Run(); err != nil {
			t.Skip(err)
		}
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func tlsTrustWrite(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bundle.pem")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tlsTrustMintCA(t *testing.T) ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tls-trust-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert, key
}

func tlsTrustMintLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dns string, ip net.IP, serial int64) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if dns != "" {
		tmpl.DNSNames = []string{dns}
	}
	if ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

type tlsTrustCounter struct {
	net.Listener
	n *atomic.Int32
}

func (l *tlsTrustCounter) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.n.Add(1)
	return c, nil
}

func tlsTrustStart(t *testing.T, cert tls.Certificate) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	n := &atomic.Int32{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ok\":true}")
	}))
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = &tlsTrustCounter{Listener: ln, n: n}
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, n
}
`

func tlsTrustBrowserDoctor() string {
	return `
	plain := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{\"ok\":true}")
	}))
	t.Cleanup(plain.Close)
	t.Setenv("__ENVKEY__BASE_URL", plain.URL)
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	old := os.Stderr
	os.Stderr = w
	out, err = tlsTrustDoctor(t, "--insecure", "--ca-cert", t.TempDir(), "--tls-server-name", "api.example.com")
	w.Close()
	os.Stderr = old
	warned, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("insecure doctor: %v\n%s", err, out)
	}
	if !strings.Contains(string(warned), "overrides") {
		t.Fatalf("stderr = %q", warned)
	}
	report = tlsTrustReport(t, out)
	apiVal, _ = report["api"].(string)
	tlsVal, _ = report["tls"].(string)
	if !strings.Contains(apiVal, "reachable") {
		t.Fatalf("insecure api = %q", apiVal)
	}
	if !strings.Contains(tlsVal, "verification disabled") {
		t.Fatalf("insecure tls summary = %q", tlsVal)
	}
`
}
