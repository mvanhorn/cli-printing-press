package govulncheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolModuleIsPinned(t *testing.T) {
	assert.NotContains(t, ToolModule, "@latest")
	assert.True(t, strings.HasPrefix(ToolVersion, "v"))
	assert.Equal(t, "golang.org/x/vuln/cmd/govulncheck@"+ToolVersion, ToolModule)
}

func TestGoRunArgsUsesDefaultMode(t *testing.T) {
	args := GoRunArgs("./...")
	assert.Equal(t, []string{"run", ToolModule, "./..."}, args)
	assert.NotContains(t, args, "-show")
	assert.NotContains(t, args, "verbose")
}

func TestToolchainEnvPrefersToolchainDirective(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.25.0\ntoolchain go1.26.5\n"), 0o644))

	assert.Equal(t, []string{"GOTOOLCHAIN=go1.26.5"}, ToolchainEnv(dir))
}

func TestToolchainEnvFallsBackToPatchLevelGoDirective(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.26.5\n"), 0o644))

	assert.Equal(t, []string{"GOTOOLCHAIN=go1.26.5"}, ToolchainEnv(dir))
}

func TestToolchainEnvIgnoresLanguageOnlyGoDirective(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))

	assert.Nil(t, ToolchainEnv(dir))
}

func TestToolchainEnvMissingGoMod(t *testing.T) {
	assert.Nil(t, ToolchainEnv(t.TempDir()))
}
