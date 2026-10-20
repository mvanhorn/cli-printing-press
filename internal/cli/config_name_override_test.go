package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCmdNameOverrideRefreshesOmittedConfigPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	outputDir := filepath.Join(dir, "out")
	require.NoError(t, os.WriteFile(specPath, []byte(`name: old
description: Omitted config renamed by --name
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--name", "new",
		"--output", outputDir,
		"--validate=false",
	})
	require.NoError(t, cmd.Execute())

	readme := readFileString(t, filepath.Join(outputDir, "README.md"))
	configGo := readFileString(t, filepath.Join(outputDir, "internal", "config", "config.go"))
	const want = "~/.config/new-pp-cli/config.toml"
	assert.Contains(t, readme, want)
	assert.NotContains(t, readme, "old-pp-cli")
	assert.Contains(t, configGo, `"new-pp-cli", "config.toml"`)
	assert.NotContains(t, configGo, "old-pp-cli")
}

func TestGenerateCmdNameOverrideKeepsExplicitConfigPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	outputDir := filepath.Join(dir, "out")
	const explicit = "~/.config/custom/settings.toml"
	require.NoError(t, os.WriteFile(specPath, []byte(`name: old
description: Explicit config path survives --name
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: `+explicit+`
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--name", "new",
		"--output", outputDir,
		"--validate=false",
	})
	require.NoError(t, cmd.Execute())

	readme := readFileString(t, filepath.Join(outputDir, "README.md"))
	configGo := readFileString(t, filepath.Join(outputDir, "internal", "config", "config.go"))
	assert.Contains(t, readme, "The platform-default config path is `"+explicit+"` on macOS/Linux")
	assert.NotContains(t, readme, "~/.config/old-pp-cli/config.toml")
	assert.NotContains(t, readme, "~/.config/new-pp-cli/config.toml")
	assert.Contains(t, configGo, `"new-pp-cli", "config.toml"`)
	assert.NotContains(t, configGo, "old-pp-cli")
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
