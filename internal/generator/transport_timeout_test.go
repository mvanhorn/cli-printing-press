package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestBrowserTransport_OverridesResponseHeaderTimeout asserts the generator
// emits the surf transport's ResponseHeaderTimeout override in every CLI
// that uses the browser-impersonate transport (SpecSource="sniffed" triggers
// it). Without the override, the user-facing --timeout flag flows into
// httpClient.Timeout but never reaches the underlying *http.Transport's
// per-stage ResponseHeaderTimeout, which surf sets to its 10s package
// default. Slow-streaming endpoints (RAG queries, LLM completions) fail
// with "net/http: timeout awaiting response headers" at the surf default
// regardless of how --timeout is set.
//
// This canary asserts the structural fix: surfClient.GetTransport() is
// type-asserted to *http.Transport and ResponseHeaderTimeout is set to
// the requested timeout, before the wrapping Std() client is built.
func TestBrowserTransport_OverridesResponseHeaderTimeout(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:       "transport-timeout-canary",
		Version:    "0.1.0",
		BaseURL:    "https://www.example.com",
		SpecSource: "sniffed", // triggers UsesBrowserHTTPTransport
		Owner:      "test-owner",
		OwnerName:  "Test Author",
		Auth:       spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/transport-timeout-canary-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"posts": {
				Description: "Browse posts",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/", Description: "List posts"},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	// Sanity: the test fixture must actually exercise the browser path.
	require.Contains(t, src, "Impersonate()",
		"test fixture must trigger UsesBrowserHTTPTransport — Impersonate() should be in the emitted client")

	require.Contains(t, src, `enetxhttp "github.com/enetx/http"`,
		"client.go must import enetx/http aliased so the transport type assertion has a name")
	require.Contains(t, src, "surfClient.GetTransport().(*enetxhttp.Transport)",
		"surfClient.GetTransport() must be type-asserted to *enetxhttp.Transport — surf returns the enetx HTTP fork's RoundTripper, not stdlib's, so a stdlib type-assertion is impossible")
	require.Contains(t, src, "t.ResponseHeaderTimeout = timeout",
		"ResponseHeaderTimeout must be set to the user-supplied timeout so --timeout reaches the transport layer")
}

func TestBrowserTransport_DropsChromeImpersonationWhenTrafficAnalysisMarksUnsafe(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:       "transport-content-negotiation-canary",
		Version:    "0.1.0",
		BaseURL:    "https://www.example.com",
		SpecSource: "sniffed",
		Owner:      "test-owner",
		OwnerName:  "Test Author",
		Auth:       spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/transport-content-negotiation-canary-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"stores": {
				Description: "Browse stores",
				Endpoints: map[string]spec.Endpoint{
					"get": {Method: "GET", Path: "/api/Stores/{id}", Description: "Get store"},
				},
			},
		},
	}
	impersonationSafe := false
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.TrafficAnalysis = &browsersniff.TrafficAnalysis{
		Reachability: &browsersniff.ReachabilityAnalysis{
			Mode:              "standard_http",
			Confidence:        0.95,
			ImpersonationSafe: &impersonationSafe,
		},
	}
	require.NoError(t, gen.Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	require.Contains(t, src, `"github.com/enetx/surf"`,
		"sniffed CLIs still use surf transport when the spec defaults to browser transport")
	require.NotContains(t, src, "Impersonate()",
		"content-type flip evidence must suppress Chrome impersonation")
	require.NotContains(t, src, "Chrome()",
		"content-type flip evidence must suppress Chrome Accept-header impersonation")
	require.Contains(t, src, "ForceHTTP2()",
		"the existing sniffed browser transport default should still force HTTP/2")
}

// TestNonBrowserTransport_DoesNotEmitOverride asserts the override only
// fires inside the browser-transport branch. Vanilla *http.Client CLIs
// already honor --timeout via http.Client.Timeout — they have no surf
// middleware overriding ResponseHeaderTimeout, so emitting the override
// there would be dead code (and would fail compilation since the surf
// package isn't imported in non-browser branches).
func TestNonBrowserTransport_DoesNotEmitOverride(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain-transport-canary")
	// Default SpecSource ("") does NOT trigger UsesBrowserHTTPTransport.
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	require.NotContains(t, src, "Impersonate()",
		"sanity: plain transport CLIs must not emit Impersonate()")
	require.NotContains(t, src, "ResponseHeaderTimeout",
		"plain transport CLIs must not emit the surf-specific ResponseHeaderTimeout override")
}
