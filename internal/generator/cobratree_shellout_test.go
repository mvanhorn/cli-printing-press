package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMCPJoinsFlagValuesAndSplitsVariadicRawArgs(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-argv-contract")
	outputDir := filepath.Join(t.TempDir(), "mcp-argv-contract-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	shellout := readGenerated(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	assert.Contains(t, shellout, `out = append(out, "--"+k+"="+tv)`)
	assert.Contains(t, shellout, `out = append(out, "--"+k+"="+strconv.FormatFloat(tv, 'f', -1, 64))`)
	assert.Contains(t, shellout, `out = append(out, "--"+k+"="+strings.Join(parts, ","))`)
	assert.Contains(t, shellout, `out = append(out, "--"+k+"="+fmt.Sprintf("%v", v))`)
	assert.NotContains(t, shellout, `out = append(out, "--"+k, tv)`)
	assert.NotContains(t, shellout, `out = append(out, "--"+k, strconv.FormatFloat`)
	assert.Contains(t, shellout, `!positionals[0].Variadic`)

	typemap := readGenerated(t, outputDir, "internal", "mcp", "cobratree", "typemap.go")
	assert.Contains(t, typemap, "Variadic  bool")

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "^Test(CliArgsFromMCP_ValueCannotSmuggleBlockedFlag|PositionalArgsFromRawArgsField|ShellOutVariadicRawArgsSplits|ShellOutStructuredThenRawOverflowPreservesOrder|ShellOutSinglePositionalArgsFieldPreservesWhitespace)$", "-count=1")
}

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
