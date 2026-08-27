package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

Install via:

`+"```"+`
npx -y @mvanhorn/printing-press-library install `+apiName+` --cli-only
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
npx -y @mvanhorn/printing-press-library install `+apiName+` --cli-only
go install github.com/mvanhorn/printing-press-library/library/other/`+apiName+`/cmd/`+cliName+`@latest
`+"```"+`
`), 0o644))

	// go.mod (module path uses bare CLI name, as generated CLIs do)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module `+cliName+`

go 1.24
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "NOTICE"), []byte(cliName+`
Copyright 2026 Example
`), 0o644))

	pathsDir := filepath.Join(dir, "internal", "cliutil")
	require.NoError(t, os.MkdirAll(pathsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pathsDir, "paths.go"), []byte(`package cliutil

const envPrefix = "`+naming.EnvPrefix(apiName)+`"

func envName(suffix string) string {
	return envPrefix + "_" + suffix
}
`), 0o644))

	// .manuscripts/ — should NOT be modified
	msDir := filepath.Join(dir, ".manuscripts", "20260329-100000", "research")
	require.NoError(t, os.MkdirAll(msDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(msDir, "brief.md"),
		[]byte("# Research Brief for "+cliName+"\n\nGenerated from "+cliName+" spec.\n"), 0o644))
	require.NoError(t, writeResearchJSON(&ResearchResult{
		APIName: apiName,
		Narrative: &ReadmeNarrative{
			AuthNarrative: "Export the API token for " + cliName + ".",
		},
	}, filepath.Join(dir, ".manuscripts", "20260329-100000")))
	require.NoError(t, writeResearchJSON(&ResearchResult{
		APIName: apiName,
		Narrative: &ReadmeNarrative{
			AuthNarrative: "Export the API token for " + cliName + ".",
		},
	}, dir))

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

		// go.mod module path must move with the CLI name so the tree still builds.
		gomod, err := os.ReadFile(filepath.Join(newDir, "go.mod"))
		require.NoError(t, err)
		assert.Contains(t, string(gomod), "module "+newName)
		assert.NotContains(t, string(gomod), oldName)

		notice, err := os.ReadFile(filepath.Join(newDir, "NOTICE"))
		require.NoError(t, err)
		assert.Contains(t, string(notice), newName)
		assert.NotContains(t, string(notice), oldName)

		pathsGo, err := os.ReadFile(filepath.Join(newDir, "internal", "cliutil", "paths.go"))
		require.NoError(t, err)
		assert.Contains(t, string(pathsGo), `const envPrefix = "`+naming.EnvPrefix(naming.TrimCLISuffix(newName))+`"`)
		assert.NotContains(t, string(pathsGo), naming.EnvPrefix(apiName)+`"`)

		assert.Contains(t, string(readme), "install "+naming.TrimCLISuffix(newName)+" --cli-only")
		assert.NotContains(t, string(readme), "install "+apiName+" --cli-only")
		assert.Contains(t, string(skill), "install "+naming.TrimCLISuffix(newName)+" --cli-only")
		assert.NotContains(t, string(skill), "install "+apiName+" --cli-only")

		rootResearch, err := os.ReadFile(filepath.Join(newDir, "research.json"))
		require.NoError(t, err)
		var rootResearchResult ResearchResult
		require.NoError(t, json.Unmarshal(rootResearch, &rootResearchResult))
		assert.Equal(t, naming.TrimCLISuffix(newName), rootResearchResult.APIName)
		assert.Contains(t, rootResearchResult.Narrative.AuthNarrative, newName)
		assert.NotContains(t, rootResearchResult.Narrative.AuthNarrative, oldName)

		msResearch, err := os.ReadFile(filepath.Join(newDir, ".manuscripts", "20260329-100000", "research.json"))
		require.NoError(t, err)
		var msResearchResult ResearchResult
		require.NoError(t, json.Unmarshal(msResearch, &msResearchResult))
		assert.Equal(t, naming.TrimCLISuffix(newName), msResearchResult.APIName)

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

	t.Run("rejects stray old or new slug directories", func(t *testing.T) {
		tests := []struct {
			name     string
			strayDir string
		}{
			{name: "old slug", strayDir: "home-health"},
			{name: "new slug", strayDir: "home-air-health"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				root := t.TempDir()
				oldName := "home-health-pp-cli"
				newName := "home-air-health-pp-cli"
				cliDir := filepath.Join(root, "home-health")
				require.NoError(t, os.MkdirAll(cliDir, 0o755))
				writeTestCLITree(t, cliDir, oldName, "home-health")
				require.NoError(t, os.MkdirAll(filepath.Join(cliDir, tt.strayDir), 0o755))

				_, err := RenameCLI(cliDir, oldName, newName, "home-health")
				require.Error(t, err)
				assert.Contains(t, err.Error(), "stray top-level directory")
				assert.Contains(t, err.Error(), tt.strayDir)
			})
		}
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

	t.Run("rewrites packaged module path slug", func(t *testing.T) {
		root := t.TempDir()
		oldName := "subject-pp-cli"
		newName := "overpass-pp-cli"
		cliDir := filepath.Join(root, oldName)
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))

		oldMod := "github.com/mvanhorn/printing-press-library/library/other/subject"
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module "+oldMod+"\n\ngo 1.24\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "cli", "root.go"), []byte(`package cli

import "`+oldMod+`/internal/client"
`), 0o644))
		m := CLIManifest{SchemaVersion: 1, APIName: "subject", CLIName: oldName}
		data, err := json.MarshalIndent(m, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), data, 0o644))

		_, err = RenameCLI(cliDir, oldName, newName, "subject")
		require.NoError(t, err)

		newDir := filepath.Join(root, naming.LibraryDirName(newName))
		gomod, err := os.ReadFile(filepath.Join(newDir, "go.mod"))
		require.NoError(t, err)
		assert.Contains(t, string(gomod), "module github.com/mvanhorn/printing-press-library/library/other/overpass")
		assert.NotContains(t, string(gomod), oldMod)
		assert.NotContains(t, string(gomod), "/subject")

		rootGo, err := os.ReadFile(filepath.Join(newDir, "internal", "cli", "root.go"))
		require.NoError(t, err)
		assert.Contains(t, string(rootGo), "github.com/mvanhorn/printing-press-library/library/other/overpass/internal/client")
		assert.NotContains(t, string(rootGo), oldMod)
	})
}

func TestRenameCLIContentIdentityTokens(t *testing.T) {
	t.Parallel()

	got := renameCLIContent(
		`npx -y @mvanhorn/printing-press-library install subject --cli-only
const envPrefix = "SUBJECT"
req := os.Getenv("SUBJECT_NO_LEARN")
import "github.com/acme/library/other/subject/internal/cli"
`,
		"subject-pp-cli", "overpass-pp-cli",
		"subject-pp-mcp", "overpass-pp-mcp",
		"subject", "overpass",
	)
	assert.Contains(t, got, "install overpass --cli-only")
	assert.NotContains(t, got, "install subject --cli-only")
	assert.Contains(t, got, `const envPrefix = "OVERPASS"`)
	assert.Contains(t, got, `os.Getenv("OVERPASS_NO_LEARN")`)
	assert.NotContains(t, got, "SUBJECT")
	assert.Contains(t, got, "/other/overpass/internal/cli")
	assert.NotContains(t, got, "/other/subject/internal/cli")

	// Prefix-extending rename must not double a name that already uses
	// the destination prefix (NOTION_ALT_SHOP stays put when notion → notion-alt).
	got = renameEnvPrefix("os.Getenv(\"NOTION_ALT_SHOP\")\nos.Getenv(\"NOTION_SHOP\")", "notion", "notion-alt")
	assert.Equal(t, 2, strings.Count(got, `os.Getenv("NOTION_ALT_SHOP")`))
	assert.NotContains(t, got, "NOTION_ALT_ALT_SHOP")
	assert.NotContains(t, got, `os.Getenv("NOTION_SHOP")`)

	m := CLIManifest{EndpointTemplateEnvOverrides: map[string]string{
		"shop":     "NOTION_ALT_SHOP",
		"fallback": "NOTION_SHOP",
	}}
	rewriteCLIManifestEnvPrefixes(&m, "notion", "notion-alt")
	assert.Equal(t, "NOTION_ALT_SHOP", m.EndpointTemplateEnvOverrides["shop"])
	assert.Equal(t, "NOTION_ALT_SHOP", m.EndpointTemplateEnvOverrides["fallback"])

	// Shortening must rewrite a bare prefix override (no _SHOP suffix).
	// File-content rename only touches OLD_… and quoted "OLD"; the
	// unquoted metadata value would otherwise stay NOTION_ALT.
	bare := CLIManifest{EndpointTemplateEnvOverrides: map[string]string{
		"workspace": "NOTION_ALT",
		"shop":      "NOTION_ALT_SHOP",
	}}
	rewriteCLIManifestEnvPrefixes(&bare, "notion-alt", "notion")
	assert.Equal(t, "NOTION", bare.EndpointTemplateEnvOverrides["workspace"])
	assert.Equal(t, "NOTION_SHOP", bare.EndpointTemplateEnvOverrides["shop"])

	// Installer slug must not clip a longer token that only shares a prefix.
	got = renameInstallSlug("npx install subject-extra --cli-only", "subject", "overpass")
	assert.Equal(t, "npx install subject-extra --cli-only", got)

	got = renameGoModModuleSegment("module github.com/acme/library/other/subject\n", "subject", "overpass")
	assert.Equal(t, "module github.com/acme/library/other/overpass\n", got)

	// Earlier path segments that happen to equal the slug must stay put.
	got = renameGoModModuleSegment("module github.com/subject/library/other/subject\n", "subject", "overpass")
	assert.Equal(t, "module github.com/subject/library/other/overpass\n", got)

	got = renameResearchAPIName(`{"api_name": "subject", "novelty_score": 8}`, "subject", "overpass")
	assert.Equal(t, `{"api_name": "overpass", "novelty_score": 8}`, got)
	got = renameResearchAPIName("{\n  \"api_name\" :\t\"subject\"\n}", "subject", "overpass")
	assert.Equal(t, "{\n  \"api_name\" :\t\"overpass\"\n}", got)
}

func TestRenameCLIRewritesEndpointOverrideMatchingOldPrefix(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldName := "shopify-pp-cli"
	newName := "shopify-alt-pp-cli"
	cliDir := filepath.Join(root, oldName)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "config", "config.go"), []byte(`package config

import "os"

func Load() {
	_ = os.Getenv("SHOPIFY_SHOP")
	_ = os.Getenv("SHOPIFY_ACCESS_TOKEN")
}
`), 0o644))

	m := CLIManifest{
		SchemaVersion:                1,
		APIName:                      "shopify",
		CLIName:                      oldName,
		MCPBinary:                    naming.MCP("shopify"),
		AuthType:                     "api_key",
		AuthEnvVars:                  []string{"SHOPIFY_ACCESS_TOKEN"},
		EndpointTemplateVars:         []string{"shop"},
		EndpointTemplateEnvOverrides: map[string]string{"shop": "SHOPIFY_SHOP"},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), data, 0o644))

	_, err = RenameCLI(cliDir, oldName, newName, "shopify")
	require.NoError(t, err)

	newDir := filepath.Join(root, naming.LibraryDirName(newName))
	configSrc, err := os.ReadFile(filepath.Join(newDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), `os.Getenv("SHOPIFY_ALT_SHOP")`)
	require.NotContains(t, string(configSrc), `os.Getenv("SHOPIFY_SHOP")`)

	got := readMCPBManifest(t, newDir)
	assert.Equal(t, "${user_config.shopify_alt_shop}", got.Server.MCPConfig.Env["SHOPIFY_ALT_SHOP"])
	_, stale := got.Server.MCPConfig.Env["SHOPIFY_SHOP"]
	assert.False(t, stale)
}

func TestRenameCLIRewritesBarePrefixOverrideOnShortening(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldName := "notion-alt-pp-cli"
	newName := "notion-pp-cli"
	cliDir := filepath.Join(root, oldName)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "config", "config.go"), []byte(`package config

import "os"

func Load() {
	_ = os.Getenv("NOTION_ALT")
	_ = os.Getenv("NOTION_ALT_ACCESS_TOKEN")
}
`), 0o644))

	m := CLIManifest{
		SchemaVersion:                1,
		APIName:                      "notion-alt",
		CLIName:                      oldName,
		MCPBinary:                    naming.MCP("notion-alt"),
		AuthType:                     "api_key",
		AuthEnvVars:                  []string{"NOTION_ALT_ACCESS_TOKEN"},
		EndpointTemplateVars:         []string{"workspace"},
		EndpointTemplateEnvOverrides: map[string]string{"workspace": "NOTION_ALT"},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), data, 0o644))

	_, err = RenameCLI(cliDir, oldName, newName, "notion-alt")
	require.NoError(t, err)

	newDir := filepath.Join(root, naming.LibraryDirName(newName))
	configSrc, err := os.ReadFile(filepath.Join(newDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), `os.Getenv("NOTION")`)
	require.NotContains(t, string(configSrc), `os.Getenv("NOTION_ALT")`)

	cliData, err := os.ReadFile(filepath.Join(newDir, CLIManifestFilename))
	require.NoError(t, err)
	var cli CLIManifest
	require.NoError(t, json.Unmarshal(cliData, &cli))
	assert.Equal(t, "NOTION", cli.EndpointTemplateEnvOverrides["workspace"],
		"bare prefix override must match the rewritten Getenv after a shortening rename")

	got := readMCPBManifest(t, newDir)
	assert.Equal(t, "${user_config.notion}", got.Server.MCPConfig.Env["NOTION"])
	_, stale := got.Server.MCPConfig.Env["NOTION_ALT"]
	assert.False(t, stale, "stale bare NOTION_ALT must not remain on the installer prompt")
}

func TestRenameCLISkipsResearchJSONSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldName := "subject-pp-cli"
	newName := "overpass-pp-cli"
	cliDir := filepath.Join(root, oldName)
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	outside := filepath.Join(root, "outside-research.json")
	require.NoError(t, os.WriteFile(outside, []byte(`{"api_name": "subject"}`+"\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(cliDir, "research.json")))

	m := CLIManifest{SchemaVersion: 1, APIName: "subject", CLIName: oldName}
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, CLIManifestFilename), data, 0o644))

	_, err = RenameCLI(cliDir, oldName, newName, "subject")
	require.NoError(t, err)

	outsideData, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, `{"api_name": "subject"}`+"\n", string(outsideData), "symlink target outside the CLI tree must stay untouched")
}
