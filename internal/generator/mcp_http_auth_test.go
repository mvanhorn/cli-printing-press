package generator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedHTTPMCPRequiresCallerAuthAndTLS(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("http-auth-proof")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"HTTP_AUTH_PROOF_TOKEN"},
	}
	apiSpec.MCP = spec.MCPConfig{
		Transport: []string{"stdio", "http"},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mainSrc := readGeneratedMCPMain(t, outputDir, apiSpec.Name)
	for _, want := range []string{
		`httpTokenEnvVar = "HTTP_AUTH_PROOF_MCP_HTTP_TOKEN"`,
		"func requireHTTPCallerToken()",
		"func httpBindIsLoopback(",
		"func requireTLSForNonLoopback(",
		"func requireBearerAuth(",
		"func bearerTokenMatches(",
		`flag.String("tls-cert"`,
		`flag.String("tls-key"`,
		"requireBearerAuth(token, inner)",
		"httpSrv.ListenAndServe()",
		"httpSrv.ListenAndServeTLS(",
	} {
		require.Contains(t, mainSrc, want)
	}
	require.NotContains(t, mainSrc, "httpSrv.Start(")

	testSrc := readGeneratedFile(t, outputDir, "cmd", naming.MCP(apiSpec.Name), "http_auth_test.go")
	require.Contains(t, testSrc, "TestRequireBearerAuthRejectsMissingAndWrongTokens")
	require.Contains(t, testSrc, "httptest.NewServer")

	runGoCommandRequired(t, outputDir, "test", "./cmd/"+naming.MCP(apiSpec.Name))

	binPath := filepath.Join(outputDir, naming.MCP(apiSpec.Name))
	runGoCommandRequired(t, outputDir, "build", "-o", binPath, "./cmd/"+naming.MCP(apiSpec.Name))

	missingCtx, missingCancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(missingCancel)
	missingToken := exec.CommandContext(missingCtx, binPath, "--transport", "http", "--addr", "127.0.0.1:0")
	missingToken.Env = append(os.Environ(), "HTTP_AUTH_PROOF_MCP_HTTP_TOKEN=")
	out, err := missingToken.CombinedOutput()
	require.Error(t, err, "HTTP transport without caller token must refuse: %s", out)
	require.Contains(t, string(out), "HTTP_AUTH_PROOF_MCP_HTTP_TOKEN")

	openCtx, openCancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(openCancel)
	openDoor := exec.CommandContext(openCtx, binPath, "--transport", "http", "--addr", "0.0.0.0:0")
	openDoor.Env = append(os.Environ(), "HTTP_AUTH_PROOF_MCP_HTTP_TOKEN=shared-secret")
	out, err = openDoor.CombinedOutput()
	require.Error(t, err, "non-loopback HTTP without TLS must refuse: %s", out)
	require.Contains(t, string(out), "--tls-cert")
	require.True(t,
		strings.Contains(string(out), "non-loopback") || strings.Contains(string(out), "loopback"),
		"refusal should name the loopback/TLS rule: %s", out)
}

func TestGeneratedHTTPMCPStdioOnlyOmitsCallerAuthHelpers(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("stdio-only-proof")
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio"}}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mainSrc := readGeneratedMCPMain(t, outputDir, apiSpec.Name)
	require.Contains(t, mainSrc, "server.ServeStdio(s)")
	require.NotContains(t, mainSrc, "requireHTTPCallerToken")
	require.NotContains(t, mainSrc, "requireBearerAuth")
	_, err := os.Stat(filepath.Join(outputDir, "cmd", naming.MCP(apiSpec.Name), "http_auth_test.go"))
	require.True(t, os.IsNotExist(err))
}
