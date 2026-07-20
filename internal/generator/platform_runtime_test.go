package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPlatformRuntimeContract(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("platform-runtime")
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio"}}
	outputDir := filepath.Join(t.TempDir(), "platform-runtime-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	for _, name := range []string{
		"profile.go",
		"gate.go",
		"metadata.go",
		"ratelimit.go",
		"receipt.go",
		"doctor.go",
	} {
		path := filepath.Join(outputDir, "internal", "platform", name)
		_, err := os.Stat(path)
		require.NoErrorf(t, err, "generated platform runtime must include %s", name)
	}

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/platform", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestPlatformCLIConformance", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestPlatformCacheKeyContract", "-count=1")
}
