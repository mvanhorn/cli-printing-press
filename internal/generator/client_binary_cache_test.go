package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientBinaryResponseHeaderBypassesJSONCache(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("binary-cache")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const behaviorTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"binary-cache-pp-cli/internal/config"
)

func TestBinaryResponseHeaderBypassesJSONCache(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x1f, 0x8b, 0x08, 0x00})
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.cacheDir = t.TempDir()

	headers := map[string]string{BinaryResponseHeader: "true"}
	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, headers); err != nil {
		t.Fatalf("first binary GetWithHeaders returned error: %v", err)
	}
	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, headers); err != nil {
		t.Fatalf("second binary GetWithHeaders returned error: %v", err)
	}
	if _, err := c.GetWithHeadersNoCache(context.Background(), "/blob", nil, headers); err != nil {
		t.Fatalf("binary GetWithHeadersNoCache returned error: %v", err)
	}
	if seen != 3 {
		t.Fatalf("server saw %d requests, want 3 because binary responses bypass cache reads and writes", seen)
	}
	if matches, err := filepath.Glob(filepath.Join(c.cacheDir, "resources", "*", "*.json")); err != nil {
		t.Fatalf("glob cache files: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("binary response wrote JSON cache files: %v", matches)
	}
}

func TestConfigBinaryResponseHeaderBypassesJSONCache(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x1f, 0x8b, 0x08, 0x00})
	}))
	defer server.Close()

	c := New(&config.Config{
		BaseURL: server.URL,
		Headers: map[string]string{
			BinaryResponseHeader: "true",
		},
	}, time.Second, 0)
	c.cacheDir = t.TempDir()

	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, nil); err != nil {
		t.Fatalf("first config-header binary GetWithHeaders returned error: %v", err)
	}
	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, nil); err != nil {
		t.Fatalf("second config-header binary GetWithHeaders returned error: %v", err)
	}
	if _, err := c.GetWithHeadersNoCache(context.Background(), "/blob", nil, nil); err != nil {
		t.Fatalf("config-header binary GetWithHeadersNoCache returned error: %v", err)
	}
	if seen != 3 {
		t.Fatalf("server saw %d requests, want 3 because config binary responses bypass cache", seen)
	}
	if matches, err := filepath.Glob(filepath.Join(c.cacheDir, "resources", "*", "*.json")); err != nil {
		t.Fatalf("glob cache files: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("config binary response wrote JSON cache files: %v", matches)
	}
}

func TestCaseVariantBinaryResponseHeadersAreDeterministic(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x1f, 0x8b, 0x08, 0x00})
	}))
	defer server.Close()

	c := New(&config.Config{
		BaseURL: server.URL,
		Headers: map[string]string{
			BinaryResponseHeader:               "false",
			"x-printing-press-binary-response": "true",
		},
	}, time.Second, 0)
	c.cacheDir = t.TempDir()

	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, nil); err != nil {
		t.Fatalf("case-variant config binary GetWithHeaders returned error: %v", err)
	}
	if _, err := c.GetWithHeaders(context.Background(), "/blob", nil, map[string]string{
		BinaryResponseHeader:               "false",
		"x-printing-press-binary-response": "true",
	}); err != nil {
		t.Fatalf("case-variant call binary GetWithHeaders returned error: %v", err)
	}
	if seen != 2 {
		t.Fatalf("server saw %d requests, want 2 because case-variant binary headers bypass cache", seen)
	}
	if matches, err := filepath.Glob(filepath.Join(c.cacheDir, "resources", "*", "*.json")); err != nil {
		t.Fatalf("glob cache files: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("case-variant binary response wrote JSON cache files: %v", matches)
	}
}

func TestJSONResponseStillUsesJSONCache(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.cacheDir = t.TempDir()

	if _, err := c.GetWithHeaders(context.Background(), "/items", nil, nil); err != nil {
		t.Fatalf("first JSON GetWithHeaders returned error: %v", err)
	}
	if _, err := c.GetWithHeaders(context.Background(), "/items", nil, nil); err != nil {
		t.Fatalf("second JSON GetWithHeaders returned error: %v", err)
	}
	if seen != 1 {
		t.Fatalf("server saw %d requests, want 1 because JSON response should be cached", seen)
	}
	if matches, err := filepath.Glob(filepath.Join(c.cacheDir, "resources", "*", "*.json")); err != nil {
		t.Fatalf("glob cache files: %v", err)
	} else if len(matches) != 1 {
		t.Fatalf("JSON response wrote %d cache files, want 1: %v", len(matches), matches)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "binary_cache_test.go"), []byte(behaviorTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client")
}
