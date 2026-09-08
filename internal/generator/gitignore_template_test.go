package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedGitignoreAnchorsRootBinaries(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("seats-aero")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	cliName := naming.CLI(apiSpec.Name)
	mcpName := naming.MCP(apiSpec.Name)
	gitignore := readGeneratedFile(t, outputDir, ".gitignore")

	assert.Contains(t, gitignore, "/"+cliName+"\n")
	assert.Contains(t, gitignore, "/"+mcpName+"\n")
	assert.Contains(t, gitignore, "/"+cliName+".exe\n")
	assert.Contains(t, gitignore, "/"+mcpName+".exe\n")
	assert.Contains(t, gitignore, "/build/\n")
	assert.Contains(t, gitignore, "/dist/\n")
	assert.NotContains(t, gitignore, "\n"+cliName+"\n")
	assert.NotContains(t, gitignore, "\n"+mcpName+"\n")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, cliName), []byte("fake-binary\n"), 0o644))
	initGitRepo(t, outputDir)

	ignored, output := gitCheckIgnore(t, outputDir, cliName)
	require.True(t, ignored, "root binary must be ignored: %s", output)

	cmdFile := filepath.Join("cmd", cliName, "x_test.go")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, cmdFile), []byte("package main\n"), 0o644))
	ignored, output = gitCheckIgnore(t, outputDir, cmdFile)
	require.False(t, ignored, "cmd/%s/ source must stay tracked: %s", cliName, output)

	goreleaser := readGeneratedFile(t, outputDir, ".goreleaser.yaml")
	assert.Contains(t, goreleaser, "flags:")
	assert.GreaterOrEqual(t, strings.Count(goreleaser, "-trimpath"), 2,
		"CLI and MCP goreleaser builds must both set -trimpath")
}

func TestGeneratedGitignoreUsesCanonicalBinaryNames(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("widget-pp")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliName := naming.CLI(apiSpec.Name)
	mcpName := naming.MCP(apiSpec.Name)
	require.Equal(t, "widget-pp-cli", cliName)
	require.Equal(t, "widget-pp-mcp", mcpName)

	gitignore := readGeneratedFile(t, outputDir, ".gitignore")
	assert.Contains(t, gitignore, "/"+cliName+"\n")
	assert.Contains(t, gitignore, "/"+mcpName+"\n")
	assert.NotContains(t, gitignore, "widget-pp-pp-cli")
	assert.NotContains(t, gitignore, "widget-pp-pp-mcp")
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	runGit(t, dir, "config", "core.excludesFile", "")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func gitCheckIgnore(t *testing.T, dir, path string) (ignored bool, output string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "core.excludesFile=", "check-ignore", "-v", "--", path)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output = string(out)
	if err == nil {
		return true, output
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return false, output
	}
	require.NoError(t, err, "git check-ignore %s: %s", path, output)
	return false, output
}
