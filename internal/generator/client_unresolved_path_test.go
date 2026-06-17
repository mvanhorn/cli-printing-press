package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientRejectsUnresolvedPathParamsBeforeHTTP(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("pathguard")
	apiSpec.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/widgets",
					Description: "List widgets",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), "func rejectUnresolvedPathParams(path string) error")

	behaviorTest := `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cfgpkg "pathguard-pp-cli/internal/config"
)

func TestPathParamGuardRejectsUnresolvedPathBeforeHTTP(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	c := New(&cfgpkg.Config{BaseURL: srv.URL}, time.Second, 0)
	c.HTTPClient = srv.Client()
	c.NoCache = true

	_, err := c.Get(context.Background(), "/widgets/{widget_id}", nil)
	if err == nil {
		t.Fatal("expected unresolved path parameter error")
	}
	if !strings.Contains(err.Error(), "unresolved path parameter {widget_id}") {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("server was called before unresolved path parameter was rejected")
	}
}

func TestPathParamGuardAllowsResolvedPath(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/widgets/widget_123" {
			t.Fatalf("path = %q, want /widgets/widget_123", r.URL.Path)
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := New(&cfgpkg.Config{BaseURL: srv.URL}, time.Second, 0)
	c.HTTPClient = srv.Client()
	c.NoCache = true

	if _, err := c.Get(context.Background(), "/widgets/widget_123", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("server was not called for resolved path")
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "client", "path_guard_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestPathParamGuard", "-count=1")
}
