package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestCLITree creates a minimal CLI directory tree for rename testing.
// It includes files that should be renamed and files that should survive unchanged.
func writeTestCLITree(t *testing.T, dir string, cliName, apiName string) {
	t.Helper()
	mcpName := naming.MCP(naming.TrimCLISuffix(cliName))

	// cmd/<cli-name>/main.go
	cmdDir := filepath.Join(dir, "cmd", cliName)
	require.NoError(t, os.MkdirAll(cmdDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(`package main

import (
	"`+cliName+`/internal/cli"
)

func main() {
	cli.Execute()
}
`), 0o644))

	// cmd/<mcp-name>/main.go
	mcpDir := filepath.Join(dir, "cmd", mcpName)
	require.NoError(t, os.MkdirAll(mcpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "main.go"), []byte(`package main

func main() {
	serverName := "`+mcpName+`"
	_ = serverName
}
`), 0o644))

	// internal/cli/root.go — contains both import paths and runtime literals
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "root.go"), []byte(`package cli

import (
	"`+cliName+`/internal/client"
)

var version = "0.1.0"

func Execute() {
	rootCmd := &cobra.Command{
		Use:   "`+cliName+`",
		Short: "CLI for `+apiName+` API",
	}
	rootCmd.SetVersionTemplate("`+cliName+` {{ .Version }}\n")
}
`), 0o644))

	// internal/client/client.go — User-Agent
	clientDir := filepath.Join(dir, "internal", "client")
	require.NoError(t, os.MkdirAll(clientDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clientDir, "client.go"), []byte(`package client

func (c *Client) do() {
	req.Header.Set("User-Agent", "`+cliName+`/0.1.0")
}
`), 0o644))

	// .goreleaser.yaml
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(`version: 2
project_name: `+cliName+`
builds:
  - binary: `+cliName+`
  - binary: `+mcpName+`
    ldflags:
      - -s -w -X `+cliName+`/internal/cli.version={{ .Version }}
brews:
  - name: `+cliName+`
    install: |
      bin.install "`+cliName+`"
      bin.install "`+mcpName+`"
`), 0o644))

	// Makefile
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte(`build:
	go build -o `+cliName+` ./cmd/`+cliName+`
build-mcp:
	go build -o `+mcpName+` ./cmd/`+mcpName+`
`), 0o644))

	// README.md
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(`# `+cliName+`

CLI for the `+apiName+` API.

## Usage

`+"```"+`
`+cliName+` doctor
`+cliName+` users list
`+"```"+`

## MCP

`+"```"+`
claude mcp add `+apiName+` `+mcpName+`
`+"```"+`

Install the skill from `+"`cli-skills/pp-"+apiName+"`"+` and install with:

`+"```"+`
go install github.com/mvanhorn/printing-press-library/library/other/`+apiName+`/cmd/`+cliName+`@latest
`+"```"+`
`), 0o644))

	// SKILL.md
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: pp-`+apiName+`
metadata:
  openclaw:
    install:
      - kind: go
        module: github.com/mvanhorn/printing-press-library/library/other/`+apiName+`/cmd/`+cliName+`
---

# `+apiName+`

`+"```bash"+`
go install github.com/mvanhorn/printing-press-library/library/other/`+apiName+`/cmd/`+cliName+`@latest
`+"```"+`
`), 0o644))

	// go.mod (module path uses bare CLI name, as generated CLIs do)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module `+cliName+`

go 1.24
`), 0o644))

	// .manuscripts/ — should NOT be modified
	msDir := filepath.Join(dir, ".manuscripts", "20260329-100000", "research")
	require.NoError(t, os.MkdirAll(msDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(msDir, "brief.md"),
		[]byte("# Research Brief for "+cliName+"\n\nGenerated from "+cliName+" spec.\n"), 0o644))

	// .printing-press.json manifest
	m := CLIManifest{
		SchemaVersion: 1,
		APIName:       apiName,
		CLIName:       cliName,
		MCPBinary:     mcpName,
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), data, 0o644))

	mcpb := MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            mcpName,
		Version:         "1.0.0",
		Description:     apiName + " API surface as MCP tools.",
		Author:          MCPBAuthor{Name: "CLI Printing Press"},
		Server: MCPBServer{
			Type:       "binary",
			EntryPoint: "bin/" + mcpName,
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/" + mcpName,
				Args:    []string{},
			},
		},
	}
	mcpbData, err := json.MarshalIndent(mcpb, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mcpbData, 0o644))

	toolsManifest := ToolsManifest{
		APIName:     apiName,
		BaseURL:     "https://example.com",
		Description: "tools for " + apiName,
		MCPReady:    "full",
		Auth:        ManifestAuth{Type: "none"},
		Tools:       []ManifestTool{},
	}
	toolsData, err := json.MarshalIndent(toolsManifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ToolsManifestFilename), toolsData, 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(cliName+"\n"+mcpName+"\n"), 0o644))
}

func TestRenameCLI(t *testing.T) {
	t.Parallel()

	t.Run("happy path renames all references", func(t *testing.T) {
		root := t.TempDir()
		oldName := "notion-pp-cli"
		newName := "notion-alt-pp-cli"
		apiName := "notion"
		oldMCPName := naming.MCP(naming.TrimCLISuffix(oldName))
		newMCPName := naming.MCP(naming.TrimCLISuffix(newName))

		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		writeTestCLITree(t, cliDir, oldName, apiName)

		filesModified, err := RenameCLI(cliDir, oldName, newName, apiName)
		require.NoError(t, err)
		assert.Greater(t, filesModified, 0, "should modify at least one file")

		// Outer directory should stay slug-keyed.
		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		_, err = os.Stat(newDir)
		assert.NoError(t, err, "new directory should exist")
		_, err = os.Stat(cliDir)
		assert.ErrorIs(t, err, os.ErrNotExist, "old directory should not exist")

		// cmd/ directory should be renamed
		_, err = os.Stat(filepath.Join(newDir, "cmd", newName))
		assert.NoError(t, err, "new cmd directory should exist")
		_, err = os.Stat(filepath.Join(newDir, "cmd", oldName))
		assert.ErrorIs(t, err, os.ErrNotExist, "old cmd directory should not exist")
		_, err = os.Stat(filepath.Join(newDir, "cmd", newMCPName))
		assert.NoError(t, err, "new MCP cmd directory should exist")
		_, err = os.Stat(filepath.Join(newDir, "cmd", oldMCPName))
		assert.ErrorIs(t, err, os.ErrNotExist, "old MCP cmd directory should not exist")

		// root.go should have new name in Use and version template
		rootGo, err := os.ReadFile(filepath.Join(newDir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		assert.Contains(t, string(rootGo), `Use:   "`+newName+`"`)
		assert.Contains(t, string(rootGo), newName+` {{ .Version }}`)
		assert.NotContains(t, string(rootGo), oldName)

		// client.go should have new User-Agent
		clientGo, err := os.ReadFile(filepath.Join(newDir, "internal", "client", "client.go"))
		require.NoError(t, err)
		assert.Contains(t, string(clientGo), newName+`/0.1.0`)
		assert.NotContains(t, string(clientGo), oldName)

		// .goreleaser.yaml should have new project_name, binary, brew
		goreleaser, err := os.ReadFile(filepath.Join(newDir, ".goreleaser.yaml"))
		require.NoError(t, err)
		grContent := string(goreleaser)
		assert.Contains(t, grContent, "project_name: "+newName)
		assert.Contains(t, grContent, "binary: "+newName)
		assert.Contains(t, grContent, "binary: "+newMCPName)
		assert.Contains(t, grContent, `install "`+newName+`"`)
		assert.Contains(t, grContent, `install "`+newMCPName+`"`)
		assert.NotContains(t, grContent, oldName)
		assert.NotContains(t, grContent, oldMCPName)

		// Makefile should have new name
		makefile, err := os.ReadFile(filepath.Join(newDir, "Makefile"))
		require.NoError(t, err)
		assert.Contains(t, string(makefile), newName)
		assert.NotContains(t, string(makefile), oldName)
		assert.Contains(t, string(makefile), newMCPName)
		assert.NotContains(t, string(makefile), oldMCPName)

		// README should have new name
		readme, err := os.ReadFile(filepath.Join(newDir, "README.md"))
		require.NoError(t, err)
		assert.Contains(t, string(readme), "# "+newName)
		assert.NotContains(t, string(readme), oldName)
		assert.Contains(t, string(readme), newMCPName)
		assert.NotContains(t, string(readme), oldMCPName)
		assert.Contains(t, string(readme), "cli-skills/pp-"+naming.TrimCLISuffix(newName))
		assert.NotContains(t, string(readme), "`cli-skills/pp-"+apiName+"`")
		assert.Contains(t, string(readme), "library/other/"+naming.TrimCLISuffix(newName)+"/cmd/"+newName)
		assert.NotContains(t, string(readme), "library/other/"+apiName+"/cmd/"+newName)

		// SKILL should have new skill identity, install metadata, and Go fallback path.
		skill, err := os.ReadFile(filepath.Join(newDir, "SKILL.md"))
		require.NoError(t, err)
		assert.Contains(t, string(skill), "name: pp-"+naming.TrimCLISuffix(newName))
		assert.NotContains(t, string(skill), "name: pp-"+apiName+"\n")
		assert.Contains(t, string(skill), "library/other/"+naming.TrimCLISuffix(newName)+"/cmd/"+newName)
		assert.NotContains(t, string(skill), "library/other/"+apiName+"/cmd/"+newName)

		// Manifest should have the final public slug, CLI name, and MCP binary.
		mData, err := os.ReadFile(filepath.Join(newDir, CLIManifestFilename))
		require.NoError(t, err)
		var m CLIManifest
		require.NoError(t, json.Unmarshal(mData, &m))
		assert.Equal(t, newName, m.CLIName)
		assert.Equal(t, naming.TrimCLISuffix(newName), m.APIName)
		assert.Equal(t, newMCPName, m.MCPBinary)

		// MCPB manifest should launch the renamed MCP binary.
		mcpbData, err := os.ReadFile(filepath.Join(newDir, MCPBManifestFilename))
		require.NoError(t, err)
		var mcpb MCPBManifest
		require.NoError(t, json.Unmarshal(mcpbData, &mcpb))
		assert.Equal(t, newMCPName, mcpb.Name)
		assert.Equal(t, "bin/"+newMCPName, mcpb.Server.EntryPoint)
		assert.Equal(t, "${__dirname}/bin/"+newMCPName, mcpb.Server.MCPConfig.Command)
		assert.NotContains(t, string(mcpbData), oldMCPName)

		// Tools manifest should use the post-rename public API slug.
		toolsData, err := os.ReadFile(filepath.Join(newDir, ToolsManifestFilename))
		require.NoError(t, err)
		var tools ToolsManifest
		require.NoError(t, json.Unmarshal(toolsData, &tools))
		assert.Equal(t, naming.TrimCLISuffix(newName), tools.APIName)

		// Bare binary ignore patterns must be root-anchored so cmd/<binary> is tracked.
		gitignore, err := os.ReadFile(filepath.Join(newDir, ".gitignore"))
		require.NoError(t, err)
		assert.Contains(t, string(gitignore), "/"+newName)
		assert.Contains(t, string(gitignore), "/"+newMCPName)
		assert.NotContains(t, string(gitignore), "\n"+newName+"\n")
		assert.NotContains(t, string(gitignore), "\n"+newMCPName+"\n")
	})

	t.Run("numeric qualifier renames correctly", func(t *testing.T) {
		root := t.TempDir()
		oldName := "notion-pp-cli"
		newName := "notion-2-pp-cli"
		apiName := "notion"

		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		writeTestCLITree(t, cliDir, oldName, apiName)

		filesModified, err := RenameCLI(cliDir, oldName, newName, apiName)
		require.NoError(t, err)
		assert.Greater(t, filesModified, 0)

		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		rootGo, err := os.ReadFile(filepath.Join(newDir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		assert.Contains(t, string(rootGo), `Use:   "`+newName+`"`)
		assert.NotContains(t, string(rootGo), oldName)
	})

	t.Run("does not modify manuscripts", func(t *testing.T) {
		root := t.TempDir()
		oldName := "notion-pp-cli"
		newName := "notion-alt-pp-cli"
		apiName := "notion"

		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		writeTestCLITree(t, cliDir, oldName, apiName)

		_, err := RenameCLI(cliDir, oldName, newName, apiName)
		require.NoError(t, err)

		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		briefPath := filepath.Join(newDir, ".manuscripts", "20260329-100000", "research", "brief.md")
		brief, err := os.ReadFile(briefPath)
		require.NoError(t, err)
		// Manuscripts should still reference the OLD name
		assert.Contains(t, string(brief), oldName, "manuscripts should preserve original CLI name")
		assert.NotContains(t, string(brief), newName, "manuscripts should not contain new CLI name")
	})

	t.Run("does not replace bare API name", func(t *testing.T) {
		root := t.TempDir()
		oldName := "notion-pp-cli"
		newName := "notion-alt-pp-cli"
		apiName := "notion"

		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		writeTestCLITree(t, cliDir, oldName, apiName)

		_, err := RenameCLI(cliDir, oldName, newName, apiName)
		require.NoError(t, err)

		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		// root.go has "CLI for notion API" — the bare "notion" should survive
		rootGo, err := os.ReadFile(filepath.Join(newDir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		assert.Contains(t, string(rootGo), apiName+" API", "bare API name should not be replaced")
	})

	t.Run("gracefully handles missing cmd directory", func(t *testing.T) {
		root := t.TempDir()
		oldName := "simple-pp-cli"
		newName := "simple-alt-pp-cli"
		apiName := "simple"

		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))

		// Create a minimal tree without cmd/
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, "main.go"), []byte(`package main
func main() {}
`), 0o644))

		m := CLIManifest{SchemaVersion: 1, APIName: apiName, CLIName: oldName}
		data, _ := json.MarshalIndent(m, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), data, 0o644))

		_, err := RenameCLI(cliDir, oldName, newName, apiName)
		require.NoError(t, err)

		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		_, err = os.Stat(newDir)
		assert.NoError(t, err, "directory should still be renamed")
	})

	t.Run("rejects path traversal in new name", func(t *testing.T) {
		root := t.TempDir()
		cliDir := filepath.Join(root, "test-pp-cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))

		_, err := RenameCLI(cliDir, "test-pp-cli", "../evil-pp-cli", "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path traversal")
	})

	t.Run("rejects invalid CLI name format", func(t *testing.T) {
		root := t.TempDir()
		cliDir := filepath.Join(root, "test-pp-cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))

		_, err := RenameCLI(cliDir, "test-pp-cli", "not-a-valid-name", "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CLI name")
	})

	t.Run("rejects identical names", func(t *testing.T) {
		root := t.TempDir()
		cliDir := filepath.Join(root, "test-pp-cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))

		_, err := RenameCLI(cliDir, "test-pp-cli", "test-pp-cli", "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "identical")
	})

	t.Run("rejects directory base mismatch", func(t *testing.T) {
		root := t.TempDir()
		cliDir := filepath.Join(root, "other-pp-cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))

		_, err := RenameCLI(cliDir, "test-pp-cli", "test-alt-pp-cli", "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("works when dir base is slug but old-name is CLI name", func(t *testing.T) {
		root := t.TempDir()
		oldName := "dub-pp-cli"
		newName := "dub-alt-pp-cli"
		apiName := "dub"

		// Directory is slug-keyed ("dub"), not CLI-name-keyed ("dub-pp-cli")
		cliDir := filepath.Join(root, apiName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		writeTestCLITree(t, cliDir, oldName, apiName)

		filesModified, err := RenameCLI(cliDir, oldName, newName, apiName)
		require.NoError(t, err)
		assert.Greater(t, filesModified, 0, "should modify at least one file")

		// Outer directory should stay slug-keyed after rename.
		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		_, err = os.Stat(newDir)
		assert.NoError(t, err, "new directory should exist")
		_, err = os.Stat(cliDir)
		assert.ErrorIs(t, err, os.ErrNotExist, "old directory should not exist")

		// cmd/ subdirectories should be renamed
		_, err = os.Stat(filepath.Join(newDir, "cmd", newName))
		assert.NoError(t, err, "new cmd directory should exist")
		_, err = os.Stat(filepath.Join(newDir, "cmd", oldName))
		assert.ErrorIs(t, err, os.ErrNotExist, "old cmd directory should not exist")

		// File contents should have new name
		rootGo, err := os.ReadFile(filepath.Join(newDir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		assert.Contains(t, string(rootGo), `Use:   "`+newName+`"`)
		assert.NotContains(t, string(rootGo), oldName)

		// Manifest should have the final public slug and CLI name.
		mData, err := os.ReadFile(filepath.Join(newDir, CLIManifestFilename))
		require.NoError(t, err)
		var m CLIManifest
		require.NoError(t, json.Unmarshal(mData, &m))
		assert.Equal(t, newName, m.CLIName)
		assert.Equal(t, naming.TrimCLISuffix(newName), m.APIName)
	})

	t.Run("skips non-target file extensions", func(t *testing.T) {
		root := t.TempDir()
		oldName := "test-pp-cli"
		newName := "test-alt-pp-cli"

		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(cliDir, 0o755))

		// A .json file that isn't the manifest should NOT be touched
		otherJSON := filepath.Join(cliDir, "config.json")
		require.NoError(t, os.WriteFile(otherJSON, []byte(`{"name": "`+oldName+`"}`), 0o644))

		m := CLIManifest{SchemaVersion: 1, APIName: "test", CLIName: oldName}
		data, _ := json.MarshalIndent(m, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), data, 0o644))

		_, err := RenameCLI(cliDir, oldName, newName, "test")
		require.NoError(t, err)

		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		// config.json should still contain the old name (not walked for replacement)
		configData, err := os.ReadFile(filepath.Join(newDir, "config.json"))
		require.NoError(t, err)
		assert.Contains(t, string(configData), oldName, "non-target files should not be modified")
	})
}
