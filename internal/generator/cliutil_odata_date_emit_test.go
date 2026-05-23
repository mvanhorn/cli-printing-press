package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseODataDateHelperEmitted covers issue #1578. Every generated CLI must
// ship cliutil.ParseODataDate so OData v3 CLIs decode /Date(ms)/ literals
// instead of re-implementing the regex per command. The emitted helper's own
// test must also pass.
func TestParseODataDateHelperEmitted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("odatahelper")
	outputDir := filepath.Join(t.TempDir(), "odatahelper-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	for _, name := range []string{"odata_date.go", "odata_date_test.go"} {
		_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", name))
		require.NoError(t, err, "expected internal/cliutil/%s to be emitted", name)
	}

	runGoCommand(t, outputDir, "test", "./internal/cliutil")
}
