package reachability

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/http2"
)

// Chrome identity literals match internal/generator/templates/chrome_profile.go.tmpl
// so reachability probes exercise the same Chrome TLS/HTTP stack that printed
// browser-chrome CLIs ship.
const (
	chromeMajor = "145"

	chromeUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + chromeMajor + ".0.0.0 Safari/537.36"
	chromeAccept          = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	chromeAcceptLanguage  = "en-US,en;q=0.9"
	chromeSecCHUA         = `"Not:A-Brand";v="99", "Google Chrome";v="` + chromeMajor + `", "Chromium";v="` + chromeMajor + `"`
	chromeSecCHUAMobile   = "?0"
	chromeSecCHUAPlatform = `"Windows"`

	chromeHeaderTableSize = 65536
	chromeIdleConnTimeout = 90 * time.Second
)

var (
	chromeHelloID = utls.HelloChrome_Auto
	chromeALPN    = []string{"h2", "http/1.1"}

	errChromeNotH2 = errors.New("chrome transport: server did not negotiate h2")
)

type chromeHeader struct {
	name  string
	value string
}

var chromeHeaders = []chromeHeader{
	{name: "User-Agent", value: chromeUserAgent},
	{name: "Accept", value: chromeAccept},
	{name: "Accept-Language", value: chromeAcceptLanguage},
	{name: "sec-ch-ua", value: chromeSecCHUA},
	{name: "sec-ch-ua-mobile", value: chromeSecCHUAMobile},
	{name: "sec-ch-ua-platform", value: chromeSecCHUAPlatform},
}

func newChromeClient(timeout time.Duration) (*http.Client, error) {
	return newChromeClientWithRoots(timeout, nil)
}

func newChromeClientWithRoots(timeout time.Duration, rootCAs *x509.CertPool) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	rt := newChromeTransport(timeout, rootCAs)
	return &http.Client{
		Timeout:   timeout,
		Jar:       jar,
		Transport: chromeHeaderTripper{next: rt},
	}, nil
}

// chromeHeaderTripper clones the request because RoundTrippers must not mutate
// their input.
type chromeHeaderTripper struct {
	next http.RoundTripper
}

func (t chromeHeaderTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if req.Header == nil {
		req.Header = http.Header{}
	}
	for _, h := range chromeHeaders {
		if req.Header.Get(h.name) == "" {
			req.Header.Set(h.name, h.value)
		}
	}
	return t.next.RoundTrip(req)
}

type chromeTransport struct {
	h2 *http2.Transport
	h1 *http.Transport

	mu      sync.Mutex
	h1Hosts map[string]bool
}

func newChromeTransport(timeout time.Duration, rootCAs *x509.CertPool) http.RoundTripper {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}

	var h1 *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		h1 = base.Clone()
	} else {
		h1 = &http.Transport{}
	}
	// chromeDialTCP tunnels HTTPS CONNECT itself so the Chrome ClientHello, not
	// crypto/tls, reaches the origin through a proxy. Plain HTTP still honors
	// standard proxy environment variables.
	h1.Proxy = func(req *http.Request) (*url.URL, error) {
		if req.URL.Scheme == "https" {
			return nil, nil
		}
		return http.ProxyFromEnvironment(req)
	}
	h1.ForceAttemptHTTP2 = false
	h1.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	h1.ResponseHeaderTimeout = timeout
	h1.DialContext = dialer.DialContext
	h1.DialTLSContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return chromeDialTLS(ctx, dialer, addr, []string{"http/1.1"}, rootCAs)
	}

	h2 := &http2.Transport{
		DialTLSContext: func(ctx context.Context, _, addr string, _ *tls.Config) (net.Conn, error) {
			return chromeDialH2(ctx, dialer, addr, rootCAs)
		},
		MaxEncoderHeaderTableSize: chromeHeaderTableSize,
		MaxDecoderHeaderTableSize: chromeHeaderTableSize,
		IdleConnTimeout:           chromeIdleConnTimeout,
	}

	return &chromeTransport{
		h2:      h2,
		h1:      h1,
		h1Hosts: map[string]bool{},
	}
}

func (t *chromeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return t.h1.RoundTrip(req)
	}
	host := req.URL.Host
	t.mu.Lock()
	useH1 := t.h1Hosts[host]
	t.mu.Unlock()
	if useH1 {
		return t.h1.RoundTrip(req)
	}
	resp, err := t.h2.RoundTrip(req)
	if !errors.Is(err, errChromeNotH2) {
		return resp, err
	}
	t.mu.Lock()
	t.h1Hosts[host] = true
	t.mu.Unlock()
	return t.h1.RoundTrip(req)
}

func chromeDialH2(ctx context.Context, dialer *net.Dialer, addr string, rootCAs *x509.CertPool) (net.Conn, error) {
	conn, err := chromeDialTLS(ctx, dialer, addr, chromeALPN, rootCAs)
	if err != nil {
		return nil, err
	}
	if proto := conn.ConnectionState().NegotiatedProtocol; proto != "h2" {
		_ = conn.Close()
		return nil, fmt.Errorf("%w (got %q)", errChromeNotH2, proto)
	}
	return conn, nil
}

func chromeDialTLS(ctx context.Context, dialer *net.Dialer, addr string, alpn []string, rootCAs *x509.CertPool) (*utls.UConn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	raw, err := chromeDialTCP(ctx, dialer, addr, rootCAs)
	if err != nil {
		return nil, err
	}
	spec, err := utls.UTLSIdToSpec(chromeHelloID)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	for _, ext := range spec.Extensions {
		if a, ok := ext.(*utls.ALPNExtension); ok {
			a.AlpnProtocols = alpn
		}
	}
	conn := utls.UClient(raw, &utls.Config{ServerName: host, RootCAs: rootCAs}, utls.HelloCustom)
	if err := conn.ApplyPreset(&spec); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return conn, nil
}

func chromeDialTCP(ctx context.Context, dialer *net.Dialer, addr string, rootCAs *x509.CertPool) (net.Conn, error) {
	proxyURL, err := httpproxy.FromEnvironment().ProxyFunc()(&url.URL{Scheme: "https", Host: addr})
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return dialer.DialContext(ctx, "tcp", addr)
	}
	switch proxyURL.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("chrome transport: unsupported proxy scheme %q", proxyURL.Scheme)
	}
	proxyAddr := proxyURL.Host
	if _, _, err := net.SplitHostPort(proxyAddr); err != nil {
		port := "80"
		if proxyURL.Scheme == "https" {
			port = "443"
		}
		proxyAddr = net.JoinHostPort(proxyAddr, port)
	}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if proxyURL.Scheme == "https" {
		host, _, splitErr := net.SplitHostPort(proxyAddr)
		if splitErr != nil {
			_ = conn.Close()
			return nil, splitErr
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, RootCAs: rootCAs})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		conn = tlsConn
	}
	br, err := chromeProxyCONNECT(ctx, conn, addr, proxyURL)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &chromePrefixedConn{Conn: conn, r: br}, nil
}

func chromeProxyCONNECT(ctx context.Context, conn net.Conn, addr string, proxyURL *url.URL) (*bufio.Reader, error) {
	var mu sync.Mutex
	armed := true
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if armed {
			_ = conn.SetDeadline(time.Now())
		}
	})
	defer func() {
		stop()
		mu.Lock()
		armed = false
		_ = conn.SetDeadline(time.Time{})
		mu.Unlock()
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	connect := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: http.Header{},
	}
	if user := proxyURL.User; user != nil {
		password, _ := user.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user.Username()+":"+password)))
	}
	if err := connect.Write(conn); err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connect)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chrome transport: proxy CONNECT returned %s", resp.Status)
	}
	return br, nil
}

// chromePrefixedConn keeps bytes the CONNECT response reader already buffered,
// which would otherwise be dropped if we returned the raw TCP conn.
type chromePrefixedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *chromePrefixedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}
