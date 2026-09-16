package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestBrowserTransport_TimeoutReachesTransport(t *testing.T) {
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

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, "return chromeClient(timeout, jar, skipTLSVerify)",
		"test fixture must trigger UsesBrowserHTTPTransport")
	require.NotContains(t, clientSrc, "github.com/enetx/")

	chromeSrc := readGeneratedFile(t, outputDir, "internal", "client", "chrome.go")
	require.Contains(t, chromeSrc, "&http.Client{Timeout: timeout, Jar: jar, Transport: rt}",
		"--timeout must bound the whole request on the chrome client")
	require.NotContains(t, chromeSrc, "github.com/enetx/")

	bare := *apiSpec
	bare.Name = "transport-timeout-bare"
	bare.HTTPTransport = spec.HTTPTransportBrowserChrome
	bareDir := filepath.Join(t.TempDir(), naming.CLI(bare.Name))
	require.NoError(t, New(&bare, bareDir).Generate())
	bareChrome := readGeneratedFile(t, bareDir, "internal", "client", "chrome.go")
	require.Contains(t, bareChrome, "h1.ResponseHeaderTimeout = timeout",
		"the HTTP/1.1 fallback transport must inherit --timeout as its header timeout")
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

	src := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, src, "return chromeClient(timeout, jar, skipTLSVerify)",
		"sniffed CLIs keep the Chrome TLS transport when impersonation is unsafe")
	require.NotContains(t, src, "github.com/enetx/")
	require.Contains(t, src, `req.Header.Set("User-Agent", "transport-content-negotiation-canary-pp-cli/0.1.0")`,
		"with the header overlay off, the request path must own the User-Agent default")
	require.Contains(t, src, `req.Header.Set("Accept", "application/json")`,
		"with the header overlay off, the request path must own the Accept default")

	chromeSrc := readGeneratedFile(t, outputDir, "internal", "client", "chrome.go")
	require.NotContains(t, chromeSrc, "chromeHeaderTripper",
		"content-type flip evidence must suppress the Chrome header overlay")
	require.Contains(t, chromeSrc, `chromeALPN = []string{"h2"}`,
		"the sniffed browser transport default should still force HTTP/2")

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "test", "./internal/client")
}

func TestNonBrowserTransport_DoesNotEmitChromeClient(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("plain-transport-canary")
	// Default SpecSource ("") does NOT trigger UsesBrowserHTTPTransport.
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.NotContains(t, src, "chromeClient(")
	require.NotContains(t, src, "t.ResponseHeaderTimeout = timeout",
		"plain transport CLIs must not emit the chrome-family timeout assignment")
	require.Contains(t, src, "tr.ResponseHeaderTimeout = headerTimeout",
		"plain transport streaming clients must set ResponseHeaderTimeout so header stalls still die")
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "client", "chrome.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "client", "chrome_profile.go"))
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "client", "chrome_test.go"))
}
