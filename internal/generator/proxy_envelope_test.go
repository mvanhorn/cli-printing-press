package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProxyEnvelopeBuildPathEmittedAsCliutil — the proxy-envelope path
// helper now lives in cliutil.BuildPath so it can be unit-tested
// directly. The behavioral assertions on URL encoding, ordering, and
// trailing-separator handling live in cliutil_proxypath_test.go.tmpl
// (which the generator emits alongside the helper). This canary just
// proves the structural contract: the helper file is emitted, the
// client wires through the cliutil call, and the old inline helper is
// gone from client.go.
func TestProxyEnvelopeBuildPathEmittedAsCliutil(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("proxy-joiner")
	apiSpec.ClientPattern = "proxy-envelope"

	outputDir := filepath.Join(t.TempDir(), "proxy-joiner-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// 1. The helper is emitted into the cliutil package.
	proxyPathSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "proxypath.go"))
	require.NoError(t, err, "cliutil/proxypath.go must be emitted for proxy-envelope clients")
	require.Contains(t, string(proxyPathSrc), "func BuildPath(",
		"cliutil.BuildPath must be exported so client.go can call it")
	require.Contains(t, string(proxyPathSrc), `"net/url"`,
		"BuildPath must use net/url for correct query encoding")

	// 2. Its test file is emitted next to it.
	_, err = os.ReadFile(filepath.Join(outputDir, "internal", "cliutil", "proxypath_test.go"))
	require.NoError(t, err, "cliutil/proxypath_test.go must ship into the generated CLI")

	// 3. The client routes through cliutil and no longer carries an
	// inline helper that could drift from the cliutil implementation.
	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	require.Contains(t, string(clientSrc), "cliutil.BuildPath(path, params)",
		"client.go must call cliutil.BuildPath in the proxy-envelope branch")
	require.NotContains(t, string(clientSrc), "func buildProxyPath(",
		"client.go must not carry an inline buildProxyPath; the helper moved to cliutil")
}

// TestCliutilProxyPath_NotEmittedForRESTClients keeps the helper
// scoped — REST CLIs would carry it as dead weight.
func TestCliutilProxyPath_NotEmittedForRESTClients(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("rest-only")
	// Default ClientPattern is REST (empty string) — no override.

	outputDir := filepath.Join(t.TempDir(), "rest-only-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", "proxypath.go"))
	require.Error(t, err, "cliutil/proxypath.go must NOT be emitted for non-proxy-envelope clients")
	require.True(t, os.IsNotExist(err), "expected file-not-exist error, got: %v", err)
}
