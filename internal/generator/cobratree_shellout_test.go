package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedWindowsShelloutLargeOutputUsesPowerShell(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("shellout-large-output")
	outputDir := filepath.Join(t.TempDir(), "shellout-large-output-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	shelloutTest, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "shellout_test.go"))
	require.NoError(t, err)
	source := string(shelloutTest)
	assert.Contains(t, source, `[Console]::Error.Write((New-Object string([char]0x78,70000)))`)
	assert.Contains(t, source, `[Console]::Out.Write((New-Object string([char]0x78,70000)))`)
	assert.NotContains(t, source, "for /L %%i in (1,1,70000)")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "^Test(ShellOutBoundsFinalError|ShellOutSurfacesSuccessStderrHintsSeparateFromJSON|RunCLICommandBoundsFailureOutput|RunCLICommandFiltersSuccessStderrHints)$", "-count=1")
}
