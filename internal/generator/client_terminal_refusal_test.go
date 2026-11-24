package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientHookRefusalIsNotRetried(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "0")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "0")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "0")

	apiSpec := minimalSpec("hookrefuse")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "terminal_refusal_test.go"),
		[]byte(terminalRefusalTestSource(modulePath)),
		0o644,
	))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestTerminal|TestPlainTransport|TestTransientNetwork", "-count=1")
}

func terminalRefusalTestSource(modulePath string) string {
	return `package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"` + modulePath + `/internal/config"
)

func clearHarness(t *testing.T) {
	t.Helper()
	t.Setenv("PRINTING_PRESS_VERIFY", "0")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "0")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "0")
}

type countingTripper struct {
	hits     atomic.Int32
	err      error
	cancel   context.CancelFunc
	cancelOn int32
}

func (c *countingTripper) RoundTrip(*http.Request) (*http.Response, error) {
	n := c.hits.Add(1)
	if c.cancel != nil && c.cancelOn > 0 && n >= c.cancelOn {
		c.cancel()
	}
	return nil, c.err
}

func refusalClient(cfg *config.Config, trip http.RoundTripper) *Client {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1"
	}
	c := New(cfg, 5*time.Second, 0)
	c.NoCache = true
	c.HTTPClient.Transport = trip
	return c
}

func isolate(t *testing.T) (string, bool) {
	t.Helper()
	if os.Getenv("ISOLATED_TEST") == t.Name() {
		return "", false
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "ISOLATED_TEST="+t.Name())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("child: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return stderr.String(), true
}

func assertNotTransientNet(t *testing.T, err error) {
	t.Helper()
	var netErr net.Error
	if errors.As(err, &netErr) {
		t.Fatalf("error satisfies net.Error: %v", err)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		t.Fatalf("error still wraps *url.Error: %v", err)
	}
}

func chainHas(err error, needle string) bool {
	type multi interface{ Unwrap() []error }
	var walk func(error, int) bool
	walk = func(err error, depth int) bool {
		if err == nil || depth > 20 {
			return false
		}
		if strings.Contains(err.Error(), needle) {
			return true
		}
		if joined, ok := err.(multi); ok {
			for _, child := range joined.Unwrap() {
				if walk(child, depth+1) {
					return true
				}
			}
			return false
		}
		return walk(errors.Unwrap(err), depth+1)
	}
	return walk(err, 0)
}

type tempNet struct{ msg string }

func (e tempNet) Error() string   { return e.msg }
func (e tempNet) Timeout() bool   { return true }
func (e tempNet) Temporary() bool { return true }

type markerNet struct{}

func (markerNet) Error() string   { return "fake refused" }
func (markerNet) Timeout() bool   { return true }
func (markerNet) Temporary() bool { return true }
func (markerNet) Is(target error) bool {
	return target == ErrTerminalRefusal
}

type hiddenSecret struct{ secret string }

func (h hiddenSecret) Error() string { return "refused" }
func (h hiddenSecret) Unwrap() error { return errors.New("leaked " + h.secret) }

type wrapErr struct {
	msg string
	err error
}

func (w wrapErr) Error() string { return w.msg }
func (w wrapErr) Unwrap() error { return w.err }

type quietJoin struct {
	msg  string
	kids []error
}

func (q quietJoin) Error() string     { return q.msg }
func (q quietJoin) Unwrap() []error { return q.kids }

func TestTerminalErrorNil(t *testing.T) {
	if TerminalError(nil) != nil {
		t.Fatal("TerminalError(nil) must stay nil")
	}
	plain := errors.New("refused")
	if errors.Is(plain, ErrTerminalRefusal) {
		t.Fatal("plain error matched ErrTerminalRefusal")
	}
	wrapped := TerminalError(tempNet{msg: "dial timeout"})
	if wrapped.Error() != "dial timeout" {
		t.Fatalf("Error() = %q", wrapped.Error())
	}
	assertNotTransientNet(t, wrapped)
	if !errors.Is(wrapped, ErrTerminalRefusal) {
		t.Fatalf("missing sentinel: %v", wrapped)
	}
}

func TestTerminalRefusalOnce(t *testing.T) {
	if stderr, parent := isolate(t); parent {
		if strings.Contains(stderr, "network error (") || strings.Contains(stderr, "retrying") {
			t.Fatalf("refusal reported as a network error:\n%s", stderr)
		}
		return
	}
	clearHarness(t)

	sentinel := errors.New("unknown value")
	cases := []struct {
		name string
		err  error
		want []string
		is   error
	}{
		{name: "plain marker", err: TerminalError(errors.New("refused")), want: []string{"refused"}},
		{name: "wrapped sentinel", err: TerminalError(fmt.Errorf("taxonomy: %w", sentinel)), want: []string{"taxonomy", "unknown value"}, is: sentinel},
		{name: "join", err: errors.Join(TerminalError(errors.New("refused")), errors.New("other")), want: []string{"refused", "other"}},
		{name: "url wrap", err: &url.Error{Op: "Get", URL: "http://127.0.0.1/widgets", Err: TerminalError(errors.New("refused"))}, want: []string{"refused"}},
		{name: "net marker", err: TerminalError(tempNet{msg: "dial timeout"}), want: []string{"dial timeout"}},
		{name: "sentinel-only net error", err: markerNet{}, want: []string{"fake refused"}},
	}
	ran := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran++
			trip := &countingTripper{err: tc.err}
			c := refusalClient(nil, trip)
			_, err := c.Get(context.Background(), "/widgets", nil)
			if err == nil {
				t.Fatal("expected refusal")
			}
			if trip.hits.Load() != 1 {
				t.Fatalf("attempts = %d, want 1", trip.hits.Load())
			}
			for _, needle := range tc.want {
				if !strings.Contains(err.Error(), needle) {
					t.Fatalf("error %q does not contain %q", err.Error(), needle)
				}
			}
			if !errors.Is(err, ErrTerminalRefusal) {
				t.Fatalf("missing ErrTerminalRefusal: %v", err)
			}
			var refusal *TerminalRefusal
			if tc.name != "sentinel-only net error" && !errors.As(err, &refusal) {
				t.Fatalf("errors.As TerminalRefusal failed: %v", err)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("missing hook sentinel: %v", err)
			}
			assertNotTransientNet(t, err)
		})
	}
	if ran != len(cases) {
		t.Fatalf("ran %d refusal cases, want %d", ran, len(cases))
	}
}

func TestTerminalRefusalMasksCredential(t *testing.T) {
	clearHarness(t)
	const secret = "super-secret-token"
	trip := &countingTripper{err: TerminalError(fmt.Errorf("blocked %s and %s", secret, strings.ToUpper(secret)))}
	c := refusalClient(&config.Config{AuthHeaderVal: "Bearer " + secret}, trip)
	_, err := c.Get(context.Background(), "/widgets", nil)
	if err == nil {
		t.Fatal("expected refusal")
	}
	if trip.hits.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", trip.hits.Load())
	}
	if chainHas(err, secret) || chainHas(err, strings.ToUpper(secret)) {
		t.Fatalf("credential leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "****") {
		t.Fatalf("error %q was not redacted", err.Error())
	}
	if !errors.Is(err, ErrTerminalRefusal) {
		t.Fatalf("missing sentinel: %v", err)
	}
	assertNotTransientNet(t, err)

	hidden := &countingTripper{err: TerminalError(hiddenSecret{secret: secret})}
	c = refusalClient(&config.Config{AuthHeaderVal: "Bearer " + secret}, hidden)
	_, err = c.Get(context.Background(), "/widgets", nil)
	if err == nil {
		t.Fatal("expected hidden-secret refusal")
	}
	if hidden.hits.Load() != 1 {
		t.Fatalf("hidden attempts = %d, want 1", hidden.hits.Load())
	}
	if chainHas(err, secret) || strings.Contains(fmt.Sprint(err), secret) {
		t.Fatalf("unwrapped credential leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Fatalf("hook message missing: %v", err)
	}
	assertNotTransientNet(t, err)

	slashSecret := "super/secret-token"
	encoded := url.QueryEscape(slashSecret)
	if encoded == slashSecret || !strings.Contains(encoded, "%") {
		t.Fatal("credential fixture did not encode")
	}
	joined := &countingTripper{err: TerminalError(quietJoin{
		msg:  "refused",
		kids: []error{errors.New("leaked " + encoded)},
	})}
	c = refusalClient(&config.Config{AuthHeaderVal: "Bearer " + slashSecret}, joined)
	_, err = c.Get(context.Background(), "/widgets", nil)
	if err == nil {
		t.Fatal("expected encoded-join refusal")
	}
	if joined.hits.Load() != 1 {
		t.Fatalf("encoded-join attempts = %d, want 1", joined.hits.Load())
	}
	if chainHas(err, encoded) || strings.Contains(fmt.Sprint(err), encoded) || strings.Contains(fmt.Sprint(err), slashSecret) {
		t.Fatalf("encoded credential leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Fatalf("hook message missing: %v", err)
	}
	assertNotTransientNet(t, err)

	deep := error(errors.New("leaked " + secret))
	for i := 0; i < 12; i++ {
		deep = wrapErr{msg: "refused", err: deep}
	}
	buried := &countingTripper{err: TerminalError(deep)}
	c = refusalClient(&config.Config{AuthHeaderVal: "Bearer " + secret}, buried)
	_, err = c.Get(context.Background(), "/widgets", nil)
	if err == nil {
		t.Fatal("expected deep-chain refusal")
	}
	if buried.hits.Load() != 1 {
		t.Fatalf("deep attempts = %d, want 1", buried.hits.Load())
	}
	if chainHas(err, secret) || strings.Contains(fmt.Sprint(err), secret) {
		t.Fatalf("deep credential leaked: %v", err)
	}
	if !errors.Is(err, ErrTerminalRefusal) {
		t.Fatalf("missing sentinel: %v", err)
	}
	assertNotTransientNet(t, err)
}

func TestPlainTransportErrorStillRetries(t *testing.T) {
	if stderr, parent := isolate(t); parent {
		if !strings.Contains(stderr, "network error (") || !strings.Contains(stderr, "retrying") {
			t.Fatalf("plain error was not retried as a network error:\n%s", stderr)
		}
		return
	}
	clearHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trip := &countingTripper{err: errors.New("refused"), cancel: cancel, cancelOn: 2}
	c := refusalClient(nil, trip)
	_, err := c.Get(ctx, "/widgets", nil)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if trip.hits.Load() < 2 {
		t.Fatalf("attempts = %d, want at least 2", trip.hits.Load())
	}
	if errors.Is(err, ErrTerminalRefusal) {
		t.Fatalf("plain error became a terminal refusal: %v", err)
	}
}

func TestTransientNetworkErrorStillRetries(t *testing.T) {
	if stderr, parent := isolate(t); parent {
		if !strings.Contains(stderr, "network error (") || !strings.Contains(stderr, "retrying") {
			t.Fatalf("transient network error was not retried:\n%s", stderr)
		}
		return
	}
	clearHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trip := &countingTripper{
		err:      &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection reset")},
		cancel:   cancel,
		cancelOn: 2,
	}
	c := refusalClient(nil, trip)
	_, err := c.Get(ctx, "/widgets", nil)
	if err == nil {
		t.Fatal("expected network error")
	}
	if trip.hits.Load() < 2 {
		t.Fatalf("attempts = %d, want at least 2", trip.hits.Load())
	}
	if errors.Is(err, ErrTerminalRefusal) {
		t.Fatalf("network error became a terminal refusal: %v", err)
	}
}
`
}
