package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientRetrySafety(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("retry-safety")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

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

	"retry-safety-pp-cli/internal/config"
)

type retryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn retryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func retryResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(` + "`{}`" + `)),
		Request:    req,
	}
}

func newRetrySafetyClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	c := New(&config.Config{
		BaseURL: "https://api.example.invalid",
		Path:    filepath.Join(t.TempDir(), "config.toml"),
	}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: transport}
	return c
}

func TestRetrySafety_MutatingMethodsDoNotRetryServerError(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var calls int
			c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return retryResponse(req, http.StatusInternalServerError), nil
			}))

			_, status, err := c.do(context.Background(), method, "/items", nil, map[string]string{"name": "one"}, nil)
			if err == nil || status != http.StatusInternalServerError {
				t.Fatalf("do(%s) = status %d, error %v; want HTTP 500 error", method, status, err)
			}
			if calls != 1 {
				t.Fatalf("%s calls = %d, want 1", method, calls)
			}
		})
	}
}

func TestRetrySafety_SafeMethodsRetryServerError(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			var calls int
			c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return retryResponse(req, http.StatusBadGateway), nil
				}
				return retryResponse(req, http.StatusOK), nil
			}))

			_, status, err := c.do(context.Background(), method, "/items", nil, nil, nil)
			if err != nil || status != http.StatusOK {
				t.Fatalf("do(%s) = status %d, error %v; want HTTP 200", method, status, err)
			}
			if calls != 2 {
				t.Fatalf("%s calls = %d, want 2", method, calls)
			}
		})
	}
}

func TestRetrySafety_POSTRetriesRateLimit(t *testing.T) {
	var calls int
	c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := retryResponse(req, http.StatusTooManyRequests)
			resp.Header.Set("Retry-After", "0")
			return resp, nil
		}
		return retryResponse(req, http.StatusCreated), nil
	}))

	_, status, err := c.Post(context.Background(), "/items", map[string]string{"name": "one"})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("Post() = status %d, error %v; want HTTP 201", status, err)
	}
	if calls != 2 {
		t.Fatalf("POST calls = %d, want 2", calls)
	}
}

func TestRetrySafety_TransportErrorsRespectMethodSafety(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method+" retries with backoff", func(t *testing.T) {
			var calls int
			c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, errors.New("connection reset")
				}
				return retryResponse(req, http.StatusOK), nil
			}))

			started := time.Now()
			_, status, err := c.do(context.Background(), method, "/items", nil, nil, nil)
			if err != nil || status != http.StatusOK {
				t.Fatalf("do(%s) = status %d, error %v; want HTTP 200", method, status, err)
			}
			if calls != 2 {
				t.Fatalf("%s calls = %d, want 2", method, calls)
			}
			if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
				t.Fatalf("%s transport retry elapsed %s, want backoff near 1s", method, elapsed)
			}
		})
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method+" does not retry", func(t *testing.T) {
			var calls int
			c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("connection reset")
			}))

			_, _, err := c.do(context.Background(), method, "/items", nil, map[string]string{"name": "one"}, nil)
			if err == nil {
				t.Fatalf("do(%s) error = nil, want transport error", method)
			}
			if calls != 1 {
				t.Fatalf("%s calls = %d, want 1", method, calls)
			}
		})
	}
}

func TestRetrySafety_ReadOnlyPOSTRetriesAmbiguousFailures(t *testing.T) {
	t.Run("server error", func(t *testing.T) {
		var calls int
		c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return retryResponse(req, http.StatusBadGateway), nil
			}
			return retryResponse(req, http.StatusOK), nil
		}))

		_, status, err := c.doRead(context.Background(), http.MethodPost, "/search", nil, map[string]string{"query": "one"}, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("doRead(POST) = status %d, error %v; want HTTP 200", status, err)
		}
		if calls != 2 {
			t.Fatalf("read-only POST calls = %d, want 2", calls)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		var calls int
		c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("connection reset")
			}
			return retryResponse(req, http.StatusOK), nil
		}))

		_, status, err := c.doRead(context.Background(), http.MethodPost, "/search", nil, map[string]string{"query": "one"}, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("doRead(POST) = status %d, error %v; want HTTP 200", status, err)
		}
		if calls != 2 {
			t.Fatalf("read-only POST calls = %d, want 2", calls)
		}
	})
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "retry_safety_test.go"), []byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestRetrySafety_", "-count=1")
}
