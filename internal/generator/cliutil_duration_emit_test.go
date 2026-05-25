package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDurationLooseHelperEmitted covers issue #1862. Every generated CLI
// must ship cliutil.ParseDurationLoose so hand-coded time-window flags accept
// the same 7d/4w shorthand the framework's `sync --since` advertises. The
// emitted helper's own test must also pass.
func TestParseDurationLooseHelperEmitted(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("durhelper")
	outputDir := filepath.Join(t.TempDir(), "durhelper-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	for _, name := range []string{"duration.go", "duration_test.go"} {
		_, err := os.Stat(filepath.Join(outputDir, "internal", "cliutil", name))
		require.NoError(t, err, "expected internal/cliutil/%s to be emitted", name)
	}

	// Run the emitted helper's table test to validate the parsing logic.
	runGoCommand(t, outputDir, "test", "./internal/cliutil")
}
