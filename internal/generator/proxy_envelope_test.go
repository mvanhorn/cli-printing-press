package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProxyEnvelopeBuildPathHonorsExistingQueryString — when a proxy-
// envelope path already carries a query string (e.g. /api?op=list),
// buildProxyPath must use `&` to append additional params instead of `?`.
// Two `?` separators in one URL produce a path the upstream proxy rejects
// as malformed.
//
// We assert against the emitted client.go rather than calling the helper
// directly because buildProxyPath only exists inside the proxy-envelope
// template branch.
func TestProxyEnvelopeBuildPathHonorsExistingQueryString(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("proxy-joiner")
	apiSpec.ClientPattern = "proxy-envelope"

	outputDir := filepath.Join(t.TempDir(), "proxy-joiner-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	src := string(clientSrc)

	require.Contains(t, src, "func buildProxyPath(",
		"proxy-envelope client must emit buildProxyPath helper")
	require.Contains(t, src, `joiner := "?"`,
		"buildProxyPath must default to ? joiner")
	require.Contains(t, src, `strings.Contains(path, "?")`,
		"buildProxyPath must check for an existing query string before appending")
	require.Contains(t, src, `joiner = "&"`,
		"buildProxyPath must use & when path already contains ?")
}
