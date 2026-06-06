package pipeline

import (
	"go/format"
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

	t.Run("rewrites README go install path", func(t *testing.T) {
		dir := t.TempDir()

		gomod := "module notion-pp-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		readme := "# notion-pp-cli\n\n## Install\n\n```\ngo install github.com/testowner/notion-pp-cli/cmd/notion-pp-cli@latest\n```\n\n### Binary\n\nDownload from [Releases](https://github.com/testowner/notion-pp-cli/releases).\n\n```bash\nnotion-pp-cli doctor\nnotion-pp-cli users list\n```\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644))

		err := RewriteModulePath(dir, "notion-pp-cli", "github.com/acme/library/notion-pp-cli")
		require.NoError(t, err)

		updatedReadme, err := os.ReadFile(filepath.Join(dir, "README.md"))
		require.NoError(t, err)
		content := string(updatedReadme)

		// go install path should be rewritten
		assert.Contains(t, content, "go install github.com/acme/library/notion-pp-cli/cmd/notion-pp-cli@latest")
		assert.Contains(t, content, "https://github.com/acme/library/releases")

		// Bare CLI name in usage examples must NOT be rewritten
		assert.Contains(t, content, "notion-pp-cli doctor")
		assert.Contains(t, content, "notion-pp-cli users list")
		assert.Contains(t, content, "# notion-pp-cli")
	})

	t.Run("rewrites goreleaser homepage to published repo", func(t *testing.T) {
		dir := t.TempDir()

		gomod := "module notion-pp-cli\n\ngo 1.23\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

		goreleaser := `version: 2
brews:
  - homepage: "https://github.com/testowner/notion-pp-cli"
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(goreleaser), 0o644))

		err := RewriteModulePath(dir, "notion-pp-cli", "github.com/acme/library/notion-pp-cli")
		require.NoError(t, err)

		updatedGR, err := os.ReadFile(filepath.Join(dir, ".goreleaser.yaml"))
		require.NoError(t, err)
		assert.Contains(t, string(updatedGR), `homepage: "https://github.com/acme/library"`)
		assert.NotContains(t, string(updatedGR), `homepage: "https://github.com/testowner/notion-pp-cli"`)
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

// TestRewriteModulePath_ProducesGofmtCleanImports guards the publish-time
// invariant that drove the library-wide gofmt drift: rewriting the module path
// via string replacement reorders the import block (the new path sorts
// differently than the old one), so without a reformat pass every published
// CLI's imports land out of gofmt order. The input here is gofmt-clean under
// the old short module path; the rewrite must leave it gofmt-clean under the
// new path too.
func TestRewriteModulePath_ProducesGofmtCleanImports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module acme-pp-cli\n\ngo 1.23\n"), 0o644))

	// Sorted under "acme-pp-cli": the local imports lead the group because
	// "acme" precedes the stdlib names. After the rewrite to a github.com
	// path they must move below "fmt", which only happens if the rewrite
	// reformats.
	src := `package cli

import (
	"acme-pp-cli/internal/cliutil"
	"acme-pp-cli/internal/config"
	"context"
	"fmt"
)

var _ = context.Background
var _ = fmt.Sprint
var _ = cliutil.X
var _ = config.Y
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	goPath := filepath.Join(dir, "internal", "cli", "root.go")
	require.NoError(t, os.WriteFile(goPath, []byte(src), 0o644))

	const newPath = "github.com/mvanhorn/printing-press-library/library/other/acme-pp-cli"
	require.NoError(t, RewriteModulePath(dir, "acme-pp-cli", newPath))

	out, err := os.ReadFile(goPath)
	require.NoError(t, err)

	formatted, err := format.Source(out)
	require.NoError(t, err)
	assert.Equal(t, string(formatted), string(out), "rewritten .go file must be gofmt-clean")
	assert.Contains(t, string(out), newPath+"/internal/cliutil")
}

func TestRewriteModulePathReferences_RewritesBareRootSelfImports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := `package cli

import (
	root "oldmod"
	store "oldmod/internal/store"
	extra "oldmod-extra/x"
	other "oldmodother"
)

var _ = root.X
var _ = store.X
var _ = extra.X
var _ = other.X
`
	goPath := filepath.Join(dir, "imports.go")
	require.NoError(t, os.WriteFile(goPath, []byte(src), 0o644))

	require.NoError(t, RewriteModulePathReferences(dir, "oldmod", "newmod"))
	out, err := os.ReadFile(goPath)
	require.NoError(t, err)
	content := string(out)

	assert.Contains(t, content, `root "newmod"`)
	assert.Contains(t, content, `store "newmod/internal/store"`)
	assert.Contains(t, content, `extra "oldmod-extra/x"`)
	assert.Contains(t, content, `other "oldmodother"`)
	assert.NotContains(t, content, `"oldmod"`)
	assert.NotContains(t, content, `"oldmod/internal/store"`)

	require.NoError(t, RewriteModulePathReferences(dir, "oldmod", "newmod"))
	again, err := os.ReadFile(goPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(again), "module path rewrite must be idempotent")
	assert.NotContains(t, string(again), "newmod/newmod")
}

func TestRewriteModulePathReferences_SkipsImportBlockCommentsAndSingleLineGroup(t *testing.T) {
	t.Parallel()

	// A comment inside an import block that quotes the old module path must
	// NOT be rewritten, while a real import on the next line is.
	dir := t.TempDir()
	src := `package cli

import (
	// "oldmod" is the self-reference package; this comment must stay verbatim
	root "oldmod"
)

var _ = root.X
`
	goPath := filepath.Join(dir, "imports.go")
	require.NoError(t, os.WriteFile(goPath, []byte(src), 0o644))
	require.NoError(t, RewriteModulePathReferences(dir, "oldmod", "newmod"))
	out, err := os.ReadFile(goPath)
	require.NoError(t, err)
	content := string(out)
	assert.Contains(t, content, `root "newmod"`, "real import must be rewritten")
	assert.Contains(t, content, `// "oldmod" is the self-reference package`, "import-block comment text must be left untouched")

	// A single-line grouped import carries the path on the import( line.
	dir2 := t.TempDir()
	src2 := "package cli\n\nimport(\"oldmod\")\n\nvar _ = X\n"
	goPath2 := filepath.Join(dir2, "single.go")
	require.NoError(t, os.WriteFile(goPath2, []byte(src2), 0o644))
	require.NoError(t, RewriteModulePathReferences(dir2, "oldmod", "newmod"))
	out2, err := os.ReadFile(goPath2)
	require.NoError(t, err)
	// The rewrite chain gofmts the file, so the single-line group is
	// reformatted; assert on the path token rather than the exact form.
	assert.Contains(t, string(out2), `"newmod"`, "single-line grouped import path must be rewritten")
	assert.NotContains(t, string(out2), `"oldmod"`, "old module path must not survive the rewrite")
}
