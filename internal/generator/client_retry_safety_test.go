package generator

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
	"encoding/json"
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

func TestRetrySafety_ReadOnlyMutatingVerbHelpersRetryAmbiguousFailures(t *testing.T) {
	requests := []struct {
		name string
		do   func(*Client) (json.RawMessage, int, error)
	}{
		{name: http.MethodPost, do: func(c *Client) (json.RawMessage, int, error) {
			return c.PostQueryWithParams(context.Background(), "/search", nil, map[string]string{"query": "one"})
		}},
		{name: http.MethodPut, do: func(c *Client) (json.RawMessage, int, error) {
			return c.PutQueryWithParams(context.Background(), "/search", nil, map[string]string{"query": "one"})
		}},
		{name: http.MethodPatch, do: func(c *Client) (json.RawMessage, int, error) {
			return c.PatchQueryWithParams(context.Background(), "/search", nil, map[string]string{"query": "one"})
		}},
	}

	for _, request := range requests {
		t.Run(request.name+" server error", func(t *testing.T) {
			var calls int
			c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return retryResponse(req, http.StatusBadGateway), nil
				}
				return retryResponse(req, http.StatusOK), nil
			}))

			_, status, err := request.do(c)
			if err != nil || status != http.StatusOK {
				t.Fatalf("read-only %s = status %d, error %v; want HTTP 200", request.name, status, err)
			}
			if calls != 2 {
				t.Fatalf("read-only %s calls = %d, want 2", request.name, calls)
			}
		})

		t.Run(request.name+" transport error", func(t *testing.T) {
			var calls int
			c := newRetrySafetyClient(t, retryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return nil, errors.New("connection reset")
				}
				return retryResponse(req, http.StatusOK), nil
			}))

			_, status, err := request.do(c)
			if err != nil || status != http.StatusOK {
				t.Fatalf("read-only %s = status %d, error %v; want HTTP 200", request.name, status, err)
			}
			if calls != 2 {
				t.Fatalf("read-only %s calls = %d, want 2", request.name, calls)
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "retry_safety_test.go"), []byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestRetrySafety_", "-count=1")
}

func TestGeneratedReadOnlyPutPatchDispatchUsesQueryHelpers(t *testing.T) {
	t.Parallel()

	readOnlyEndpoint := func(method, contentType string, headers bool) spec.Endpoint {
		endpoint := spec.Endpoint{
			Method:             method,
			Path:               "/query",
			Description:        "Read query results",
			RequestContentType: contentType,
			Meta:               map[string]string{"mcp:read-only": "true"},
			Body:               []spec.Param{{Name: "filter", Type: "string"}},
		}
		if contentType == "multipart/form-data" {
			endpoint.Body = append(endpoint.Body, spec.Param{Name: "document", Type: "string", Format: "binary"})
		}
		if headers {
			endpoint.HeaderOverrides = []spec.RequiredHeader{{Name: "Accept", Value: "application/json"}}
		}
		return endpoint
	}

	endpoints := map[string]spec.Endpoint{
		"readPutJSON":        readOnlyEndpoint(http.MethodPut, "application/json", false),
		"readPatchJSON":      readOnlyEndpoint(http.MethodPatch, "application/json", true),
		"readPutForm":        readOnlyEndpoint(http.MethodPut, "application/x-www-form-urlencoded", true),
		"readPatchForm":      readOnlyEndpoint(http.MethodPatch, "application/x-www-form-urlencoded", false),
		"readPutMultipart":   readOnlyEndpoint(http.MethodPut, "multipart/form-data", false),
		"readPatchMultipart": readOnlyEndpoint(http.MethodPatch, "multipart/form-data", true),
		"updateWidget": {
			Method: http.MethodPut, Path: "/widgets/{id}", Description: "Update a widget",
			Body: []spec.Param{{Name: "name", Type: "string"}},
		},
		"patchWidget": {
			Method: http.MethodPatch, Path: "/widgets/{id}", Description: "Patch a widget",
			Body: []spec.Param{{Name: "name", Type: "string"}},
		},
	}

	apiSpec := minimalSpec("readonly-put-patch")
	apiSpec.Resources = map[string]spec.Resource{
		"typed": {Description: "Typed endpoints", Endpoints: endpoints},
	}
	for name, endpoint := range endpoints {
		if name == "updateWidget" || name == "patchWidget" {
			continue
		}
		apiSpec.Resources["promoted"+name] = spec.Resource{
			Description: "Promoted endpoint",
			Endpoints:   map[string]spec.Endpoint{name: endpoint},
		}
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	for _, want := range []string{
		`func (c *Client) PutQueryWithParams(`,
		`func (c *Client) PutQueryFormWithParams(`,
		`func (c *Client) PutQueryMultipartWithParams(`,
		`func (c *Client) PatchQueryWithParams(`,
		`func (c *Client) PatchQueryFormWithParams(`,
		`func (c *Client) PatchQueryMultipartWithParams(`,
		`return c.doRead(ctx, "PUT"`,
		`return c.doRead(ctx, "PATCH"`,
	} {
		require.Contains(t, clientSrc, want)
	}

	var typedSrc, promotedSrc strings.Builder
	entries, err := os.ReadDir(filepath.Join(outputDir, "internal", "cli"))
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		src := readGeneratedFile(t, outputDir, "internal", "cli", entry.Name())
		if strings.HasPrefix(entry.Name(), "promoted_") {
			promotedSrc.WriteString(src)
		} else if strings.Contains(src, `"pp:endpoint": "typed.`) {
			typedSrc.WriteString(src)
		}
	}

	for surface, src := range map[string]string{
		"typed endpoint":   typedSrc.String(),
		"promoted command": promotedSrc.String(),
	} {
		for _, want := range []string{
			"c.PutQueryWithParams(",
			"c.PatchQueryWithParamsAndHeaders(",
			"c.PutQueryFormWithParamsAndHeaders(",
			"c.PatchQueryFormWithParams(",
			"c.PutQueryMultipartWithParams(",
			"c.PatchQueryMultipartWithParamsAndHeaders(",
		} {
			require.Contains(t, src, want, "%s must route through %s", surface, want)
		}
	}
	require.Contains(t, typedSrc.String(), "c.PutWithParams(")
	require.Contains(t, typedSrc.String(), "c.PatchWithParams(")

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	for _, want := range []string{
		"c.PutQueryWithParams(ctx, path, params, bodyArgs)",
		"c.PatchQueryWithParamsAndHeaders(ctx, path, params, bodyArgs, headers)",
		"c.PutQueryFormWithParams(ctx, path, params, formFields)",
		"c.PatchQueryFormWithParamsAndHeaders(ctx, path, params, formFields, headers)",
		"c.PutQueryMultipartWithParams(ctx, path, params, multipartFields, multipartFileFields)",
		"c.PatchQueryMultipartWithParamsAndHeaders(ctx, path, params, multipartFields, multipartFileFields, headers)",
	} {
		require.Contains(t, mcpSrc, want)
	}
	require.Contains(t, mcpSrc, "data, _, err = c.PutWithParams(ctx, path, params, bodyArgs)")
	require.Contains(t, mcpSrc, "data, _, err = c.PatchWithParams(ctx, path, params, bodyArgs)")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedSessionInvalidationRetrySafety(t *testing.T) {
	t.Parallel()

	apiSpec := canonicalSessionHandshakeSpec()
	apiSpec.Name = "retry-safety-session"
	apiSpec.Auth.InvalidateOnStatus = []int{http.StatusUnauthorized, http.StatusInternalServerError}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package client

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retry-safety-session-pp-cli/internal/config"
)

type sessionRetryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn sessionRetryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func sessionRetryResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func newSessionRetrySafetyClient(t *testing.T, transport http.RoundTripper) (*Client, *int) {
	t.Helper()
	c := New(&config.Config{
		BaseURL: "https://api.example.invalid",
		Path:    filepath.Join(t.TempDir(), "config.toml"),
	}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: transport}
	c.Session.token = "token-one"

	handshakeCalls := 0
	c.Session.client = &http.Client{Transport: sessionRetryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		handshakeCalls++
		return sessionRetryResponse(req, http.StatusOK, "token-two"), nil
	})}
	return c, &handshakeCalls
}

func TestSessionRetrySafety_MutatingMethodDoesNotInvalidateAndRetryServerError(t *testing.T) {
	var calls int
	c, handshakeCalls := newSessionRetrySafetyClient(t, sessionRetryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return sessionRetryResponse(req, http.StatusInternalServerError, "{}"), nil
	}))

	_, status, err := c.do(context.Background(), http.MethodPost, "/items", nil, map[string]string{"name": "one"}, nil)
	if err == nil || status != http.StatusInternalServerError {
		t.Fatalf("do(POST) = status %d, error %v; want HTTP 500 error", status, err)
	}
	if calls != 1 {
		t.Fatalf("POST calls = %d, want 1", calls)
	}
	if *handshakeCalls != 0 {
		t.Fatalf("handshake calls = %d, want 0", *handshakeCalls)
	}
	if token := c.Session.Token(); token != "token-one" {
		t.Fatalf("session token = %q, want original token", token)
	}
}

func TestSessionRetrySafety_ReplaySafeRequestsInvalidateAndRetryServerError(t *testing.T) {
	tests := []struct {
		name string
		do   func(*Client) (int, error)
	}{
		{
			name: "GET",
			do: func(c *Client) (int, error) {
				_, status, err := c.do(context.Background(), http.MethodGet, "/items", nil, nil, nil)
				return status, err
			},
		},
		{
			name: "read-only POST",
			do: func(c *Client) (int, error) {
				_, status, err := c.doRead(context.Background(), http.MethodPost, "/search", nil, map[string]string{"query": "one"}, nil)
				return status, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			c, handshakeCalls := newSessionRetrySafetyClient(t, sessionRetryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return sessionRetryResponse(req, http.StatusInternalServerError, "{}"), nil
				}
				return sessionRetryResponse(req, http.StatusOK, "{}"), nil
			}))

			status, err := tt.do(c)
			if err != nil || status != http.StatusOK {
				t.Fatalf("request = status %d, error %v; want HTTP 200", status, err)
			}
			if calls != 2 {
				t.Fatalf("request calls = %d, want 2", calls)
			}
			if *handshakeCalls != 2 {
				t.Fatalf("handshake calls = %d, want bootstrap and token fetch", *handshakeCalls)
			}
		})
	}
}

func TestSessionRetrySafety_MutatingMethodRetriesAuthRejection(t *testing.T) {
	var calls int
	c, handshakeCalls := newSessionRetrySafetyClient(t, sessionRetryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return sessionRetryResponse(req, http.StatusUnauthorized, "{}"), nil
		}
		return sessionRetryResponse(req, http.StatusCreated, "{}"), nil
	}))

	_, status, err := c.do(context.Background(), http.MethodPost, "/items", nil, map[string]string{"name": "one"}, nil)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("do(POST) = status %d, error %v; want HTTP 201", status, err)
	}
	if calls != 2 {
		t.Fatalf("POST calls = %d, want 2", calls)
	}
	if *handshakeCalls != 2 {
		t.Fatalf("handshake calls = %d, want bootstrap and token fetch", *handshakeCalls)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "session_retry_safety_test.go"), []byte(runtimeTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestSessionRetrySafety_", "-count=1")
}
