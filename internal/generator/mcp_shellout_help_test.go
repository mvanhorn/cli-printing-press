package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillArglessCommandsRunOnBareCall(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("..", "..", "skills", "printing-press", "phases", "11-build-the-goat.md"))
	require.NoError(t, err)
	text := string(body)

	require.NotContains(t, text, "keep the help-only plus dry-run")
	require.NotContains(t, text, "omit the usageErr branch entirely and keep the help-only")
	require.Contains(t, text, "A read-only command with no required inputs runs its default action on a bare call")
	require.Contains(t, text, "For commands with no required inputs, omit both the help-only branch and the `usageErr(...)` branch.")
	require.Contains(t, text, "A bare `<cli> mycommand` prints the report and exits 0.")

	noInput := strings.Index(text, "Commands with no required inputs:")
	require.NotEqual(t, -1, noInput)
	shape := text[noInput:]
	end := strings.Index(shape, "Omit both the help-only branch")
	require.NotEqual(t, -1, end)
	require.NotContains(t, shape[:end], "return cmd.Help()")
	require.Contains(t, shape[:end], "dryRunOK(flags)")
}

func TestGeneratedMCPMirrorHelpAndCLIPath(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mirrorhelp")
	outputDir := filepath.Join(t.TempDir(), "mirrorhelp-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	shellout := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	require.Contains(t, shellout, "func cliStdoutIsCobraHelp(")
	require.Contains(t, shellout, "command printed help instead of running")
	cliPath := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "cli_path.go")
	require.Contains(t, cliPath, "func CachedSiblingCLIPath()")
	require.Contains(t, cliPath, "func ResetSiblingCLIPathCache()")
	require.Contains(t, cliPath, `os.Getenv("MIRRORHELP_CLI_PATH")`)
	require.Contains(t, cliPath, "exec.LookPath")
	require.Contains(t, cliPath, "lookPath(cliExecutableName(goos))")
	walker := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "walker.go")
	require.Contains(t, walker, "cliPath = shareCLIPath(cliPath)")
	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, tools, "cobratree.RegisterAll(s, cli.RootCmd(), cobratree.CachedSiblingCLIPath)")

	requireGeneratedCompiles(t, outputDir)
	requireGoTestPassed(t, outputDir, []string{
		"./internal/mcp/cobratree",
		"-count=1",
		"-run",
		"^Test(CLIStdoutIsCobraHelp|MirroredBareCallHelpIsNotSuccess|RegisterAllResolvesCompanionCLIOnce|CachedSiblingCLIPathMemoAndReset|CompanionCLIResolutionOrder)$",
	},
		"--- PASS: TestCLIStdoutIsCobraHelp",
		"--- PASS: TestMirroredBareCallHelpIsNotSuccess",
		"--- PASS: TestRegisterAllResolvesCompanionCLIOnce",
		"--- PASS: TestCachedSiblingCLIPathMemoAndReset",
		"--- PASS: TestCompanionCLIResolutionOrder",
	)
}
