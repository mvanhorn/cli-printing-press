package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteModulePath(t *testing.T) {
	t.Parallel()

	t.Run("rewrites go.mod and go imports", func(t *testing.T) {
		dir := t.TempDir()

		// Write a go.mod with the old module path
		gomod := "module notion-pp-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		// Write a .go file with import paths
		goFile := `package main

import (
	"notion-pp-cli/internal/cli"
	"notion-pp-cli/internal/config"
)

func main() {}
`
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "notion-pp-cli"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "notion-pp-cli", "main.go"), []byte(goFile), 0o644))

		err := RewriteModulePath(dir, "notion-pp-cli", "github.com/mvanhorn/printing-press-library/library/productivity/notion-pp-cli")
		require.NoError(t, err)

		// Check go.mod
		updatedMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		require.NoError(t, err)
		assert.Contains(t, string(updatedMod), "module github.com/mvanhorn/printing-press-library/library/productivity/notion-pp-cli")
		assert.NotContains(t, string(updatedMod), "module notion-pp-cli\n")

		// Check .go file imports
		updatedGo, err := os.ReadFile(filepath.Join(dir, "cmd", "notion-pp-cli", "main.go"))
		require.NoError(t, err)
		assert.Contains(t, string(updatedGo), `"github.com/mvanhorn/printing-press-library/library/productivity/notion-pp-cli/internal/cli"`)
		assert.Contains(t, string(updatedGo), `"github.com/mvanhorn/printing-press-library/library/productivity/notion-pp-cli/internal/config"`)
	})

	t.Run("does not corrupt non-import strings", func(t *testing.T) {
		dir := t.TempDir()

		gomod := "module notion-pp-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		// Simulate a generated root.go with both imports AND runtime literals
		goFile := `package cli

import (
	"notion-pp-cli/internal/client"
	"notion-pp-cli/internal/config"
)

var version = "0.1.0"

func Execute() {
	rootCmd := &cobra.Command{
		Use:   "notion-pp-cli",
		Short: "CLI for Notion API",
	}
	rootCmd.SetVersionTemplate("notion-pp-cli {{ .Version }}\n")
}
`
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte(goFile), 0o644))

		// Simulate a client.go with User-Agent
		clientFile := `package client

func (c *Client) do() {
	req.Header.Set("User-Agent", "notion-pp-cli/0.1.0")
}
`
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "client", "client.go"), []byte(clientFile), 0o644))

		newPath := "github.com/acme/library/notion-pp-cli"
		err := RewriteModulePath(dir, "notion-pp-cli", newPath)
		require.NoError(t, err)

		// Imports should be rewritten
		updatedRoot, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		rootContent := string(updatedRoot)
		assert.Contains(t, rootContent, `"github.com/acme/library/notion-pp-cli/internal/client"`)
		assert.Contains(t, rootContent, `"github.com/acme/library/notion-pp-cli/internal/config"`)

		// Command Use string must NOT be rewritten
		assert.Contains(t, rootContent, `Use:   "notion-pp-cli"`)
		assert.NotContains(t, rootContent, `Use:   "github.com/acme/library/notion-pp-cli"`)

		// Version template must NOT be rewritten
		assert.Contains(t, rootContent, `"notion-pp-cli {{ .Version }}\n"`)

		// User-Agent must NOT be rewritten
		updatedClient, err := os.ReadFile(filepath.Join(dir, "internal", "client", "client.go"))
		require.NoError(t, err)
		assert.Contains(t, string(updatedClient), `"notion-pp-cli/0.1.0"`)
	})

	t.Run("rewrites goreleaser ldflags", func(t *testing.T) {
		dir := t.TempDir()

		gomod := "module notion-pp-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		goreleaser := `version: 2
builds:
  - ldflags:
      - -s -w -X notion-pp-cli/internal/cli.version={{ .Version }}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(goreleaser), 0o644))

		err := RewriteModulePath(dir, "notion-pp-cli", "github.com/acme/library/notion-pp-cli")
		require.NoError(t, err)

		updatedGR, err := os.ReadFile(filepath.Join(dir, ".goreleaser.yaml"))
		require.NoError(t, err)
		assert.Contains(t, string(updatedGR), "-X github.com/acme/library/notion-pp-cli/internal/cli.version=")
		assert.NotContains(t, string(updatedGR), "-X notion-pp-cli/internal/cli.version=")
	})

	t.Run("noop when paths are equal", func(t *testing.T) {
		dir := t.TempDir()
		gomod := "module notion-pp-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		err := RewriteModulePath(dir, "notion-pp-cli", "notion-pp-cli")
		require.NoError(t, err)
	})

	t.Run("error when go.mod missing old path", func(t *testing.T) {
		dir := t.TempDir()
		gomod := "module other-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		err := RewriteModulePath(dir, "notion-pp-cli", "github.com/org/repo/notion-pp-cli")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not contain expected module path")
	})
}
