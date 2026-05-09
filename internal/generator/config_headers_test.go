package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientAppliesConfigHeadersBeforeRequestOverrides(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("static-headers")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const behaviorTest = `package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"static-headers-pp-cli/internal/config"
)

func TestConfigHeadersAreAppliedBeforeRequestOverrides(t *testing.T) {
	var seen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if got := r.Header.Get("Authorization"); got != "Bearer config-token" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer config-token")
		}
		if got := r.Header.Get("apikey"); got != "config-key" {
			t.Fatalf("apikey header = %q, want %q", got, "config-key")
		}
		if got := r.Header.Get("x-api-version"); got != "override-version" {
			t.Fatalf("x-api-version header = %q, want request override to win", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:       server.URL,
		AuthHeaderVal: "Bearer config-token",
		Headers: map[string]string{
			"apikey":        "config-key",
			"x-api-version": "config-version",
		},
	}
	c := New(cfg, time.Second, 0)
	c.NoCache = true

	if _, err := c.GetWithHeaders("/items", nil, map[string]string{"x-api-version": "override-version"}); err != nil {
		t.Fatalf("GetWithHeaders returned error: %v", err)
	}
	if seen != 1 {
		t.Fatalf("server saw %d requests, want 1", seen)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "config_headers_test.go"), []byte(behaviorTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client")
}
