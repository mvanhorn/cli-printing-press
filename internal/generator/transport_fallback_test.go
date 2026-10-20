package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestCommunityDefaultTransportReachesHTTP1(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("community-h1")
	apiSpec.SpecSource = "community"
	apiSpec.BaseURL = "https://example.com"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	chromeGo := readGeneratedFile(t, outputDir, "internal", "client", "chrome.go")
	require.Contains(t, chromeGo, `chromeALPN = []string{"h2", "http/1.1"}`)

	clientTest := `package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCommunityClientReachesHTTP1OnlyOrigin(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("request proto = %d, want 1", r.ProtoMajor)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = false
	server.StartTLS()
	defer server.Close()

	httpClient, err := newHTTPClient(time.Second, nil, tlsSettings{Insecure: true})
	if err != nil {
		t.Fatalf("newHTTPClient: %v", err)
	}
	resp, err := httpClient.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP/1.1 origin: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if resp.ProtoMajor != 1 {
		t.Fatalf("response proto = %d, want 1", resp.ProtoMajor)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "community_h1_runtime_test.go"), []byte(clientTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestCommunityClientReachesHTTP1OnlyOrigin", "-count=1")
}
