package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientMasksCredentialValuesInDisplayedURLs(t *testing.T) {
	apiSpec := minimalSpec("url-redaction")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		In:      "query",
		Header:  "api_key",
		EnvVars: []string{"URL_REDACTION_API_KEY"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"url-redaction-pp-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = original
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("closing stderr pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading stderr pipe: %v", err)
	}
	return string(out)
}

func newRedactionClient(secret string) *Client {
	cfg := &config.Config{
		BaseURL:       "https://api.example.invalid",
		AuthHeaderVal: secret,
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	return c
}

func TestCredentialMaskingHandlesPrefixOverlaps(t *testing.T) {
	const short = "a2d16a0d"
	const long = short + "/8e81+e7fb9e6437ac=="
	c := newRedactionClient(long)

	got := c.maskCredentialText("raw="+long+"&escaped="+url.QueryEscape(long)+"&short="+short, short)

	assertCredentialMasked(t, "prefix-overlap text", got, long, "****ac==", "****6a0d")
	if strings.Contains(got, "/8e81") || strings.Contains(got, "%2F8e81") {
		t.Fatalf("prefix-overlap text leaked long credential suffix: %s", got)
	}
}

func forbiddenTransport() http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Request:    req,
		}, nil
	})
}

func forbiddenTransportWithBody(body string) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
}

func assertCredentialMasked(t *testing.T, label, text, secret string, wantParts ...string) {
	t.Helper()
	if strings.Contains(text, secret) {
		t.Fatalf("%s leaked credential %q: %s", label, secret, text)
	}
	for _, part := range wantParts {
		if !strings.Contains(text, part) {
			t.Fatalf("%s did not contain masked fragment %q: %s", label, part, text)
		}
	}
}

func TestDryRunMasksCredentialEmbeddedInURLPath(t *testing.T) {
	const secret = "a2d16a0d/8e81+e7fb9e6437ac=="
	c := newRedactionClient(secret)
	c.DryRun = true

	stderr := captureStderr(t, func() {
		params := map[string]string{"echo": secret}
		if _, err := c.Get(context.Background(), "/v6/"+url.PathEscape(secret)+"/codes", params); err != nil {
			t.Fatalf("Get() dry-run error = %v", err)
		}
	})

	assertCredentialMasked(t, "dry-run stderr", stderr, secret, "/v6/****ac==/codes", "echo=****ac==", "api_key=****ac==")
}

func TestAPIErrorMasksCredentialEmbeddedInURLPath(t *testing.T) {
	const secret = "a2d16a0d/8e81+e7fb9e6437ac=="
	c := newRedactionClient(secret)
	c.HTTPClient = &http.Client{Transport: forbiddenTransportWithBody("forbidden for token " + secret)}

	_, err := c.Get(context.Background(), "/v6/"+url.PathEscape(secret)+"/codes", nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	assertCredentialMasked(t, "APIError", err.Error(), secret, "/v6/****ac==/codes", "forbidden for token ****ac==")
}

func TestTransportErrorMasksCredentialInWrappedURLError(t *testing.T) {
	const secret = "a2d16a0d/8e81+e7fb9e6437ac=="
	c := newRedactionClient(secret)
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: errors.New("dial tcp: lookup api.example.invalid: no such host")}
	})}

	_, err := c.Get(context.Background(), "/v6/"+url.PathEscape(secret)+"/pair/USD/EUR", nil)
	if err == nil {
		t.Fatal("expected transport error")
	}
	assertCredentialMasked(t, "transport error", err.Error(), secret, "/v6/****ac==/pair/USD/EUR", "api_key=****ac==")
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		t.Fatalf("transport error exposed unredacted URL error through unwrap chain: %v", urlErr)
	}
}

func TestHeaderAuthDoesNotChangeUnrelatedAPIErrorText(t *testing.T) {
	const secret = "header-only-token"
	c := newRedactionClient(secret)
	c.HTTPClient = &http.Client{Transport: forbiddenTransport()}

	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	if got, want := err.Error(), "GET /items returned HTTP 403: forbidden"; got != want {
		t.Fatalf("APIError = %q, want %q", got, want)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "url_redaction_test.go"), []byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "Test.*Credential|TestHeaderAuth", "-count=1")
}

func TestGeneratedClientLeavesHeaderAuthDisplayUnchanged(t *testing.T) {
	apiSpec := minimalSpec("header-redaction")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "api_key",
		In:      "header",
		Header:  "X-Api-Key",
		EnvVars: []string{"HEADER_REDACTION_API_KEY"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"header-redaction-pp-cli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHeaderAuthDoesNotChangeUnrelatedAPIErrorText(t *testing.T) {
	cfg := &config.Config{
		BaseURL:       "https://api.example.invalid",
		AuthHeaderVal: "header-only-token",
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Request:    req,
		}, nil
	})}

	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	if got, want := err.Error(), "GET /items returned HTTP 403: forbidden"; got != want {
		t.Fatalf("APIError = %q, want %q", got, want)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "header_redaction_test.go"), []byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestHeaderAuth", "-count=1")
}
