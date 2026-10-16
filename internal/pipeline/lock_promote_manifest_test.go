package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mcpManifestHandEdit   = "hand-edited-mcp-manifest-marker"
	toolsManifestHandEdit = "hand-edited-tools-manifest-marker"
	promoteManifestCLI    = "patchedmcp-pp-cli"
	promoteManifestMCP    = "patchedmcp-pp-mcp"
	promoteManifestAPI    = "patchedmcp"
)

func TestSnapshotPatchedMCPManifests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), []byte("root-mcp\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ToolsManifestFilename), []byte("root-tools\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", MCPBManifestFilename), []byte("nested\n"), 0o644))

	snaps, err := snapshotPatchedMCPManifests(dir)
	require.NoError(t, err)
	assert.Empty(t, snaps)

	writePatchRecordFile(t, filepath.Join(dir, PatchesDirName), "nested.json", PatchRecord{
		SchemaVersion: CurrentPatchesIndexSchemaVersion,
		ID:            "nested",
		Files:         []string{"internal/manifest.json"},
		CallSites:     []string{"nested"},
	})
	snaps, err = snapshotPatchedMCPManifests(dir)
	require.NoError(t, err)
	assert.Empty(t, snaps)

	writePatchRecordFile(t, filepath.Join(dir, PatchesDirName), "manifest.json", PatchRecord{
		SchemaVersion: CurrentPatchesIndexSchemaVersion,
		ID:            "manifest-hand-edit",
		Files:         []string{"./manifest.json"},
		CallSites:     []string{"root-mcp"},
	})
	snaps, err = snapshotPatchedMCPManifests(dir)
	require.NoError(t, err)
	assert.Equal(t, []byte("root-mcp\n"), snaps[MCPBManifestFilename])
	_, toolsClaimed := snaps[ToolsManifestFilename]
	assert.False(t, toolsClaimed)

	require.NoError(t, os.WriteFile(filepath.Join(dir, PatchesIndexFilename), []byte(`{
  "schema_version": 1,
  "patches": [
    {"id": "tools-hand-edit", "files": ["tools-manifest.json"], "call_sites": ["root-tools"]}
  ]
}`), 0o644))

	snaps, err = snapshotPatchedMCPManifests(dir)
	require.NoError(t, err)
	assert.Equal(t, []byte("root-mcp\n"), snaps[MCPBManifestFilename])
	assert.Equal(t, []byte("root-tools\n"), snaps[ToolsManifestFilename])

	require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), []byte("regenerated\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ToolsManifestFilename), []byte("regenerated\n"), 0o644))
	require.NoError(t, restorePatchedMCPManifests(dir, snaps))
	assert.Equal(t, "root-mcp\n", string(mustReadFile(t, filepath.Join(dir, MCPBManifestFilename))))
	assert.Equal(t, "root-tools\n", string(mustReadFile(t, filepath.Join(dir, ToolsManifestFilename))))
	assert.Equal(t, "nested\n", string(mustReadFile(t, filepath.Join(dir, "internal", MCPBManifestFilename))))
}

func TestPromoteWorkingCLI_PreservesPatchedMCPManifests(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", filepath.Join(root, "unused-home"))
	t.Setenv("PRINTING_PRESS_SCOPE", "promote-manifest")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", root)

	template := filepath.Join(root, "template", promoteManifestCLI)
	writePromoteMCPCLI(t, template)
	refreshed, err := refreshPromoteArtifacts(template, promoteManifestCLI)
	require.NoError(t, err)
	assert.True(t, refreshed)
	require.Contains(t, string(mustReadFile(t, filepath.Join(template, MCPBManifestFilename))), "TEST_API_TOKEN")

	t.Run("patched", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("PRINTING_PRESS_HOME", home)
		work := clonePromoteMCPCLI(t, template)
		stampPromoteManifestHandEdits(t, work)
		writePromoteManifestPatches(t, work)
		keepPromoteBinariesCurrent(t, work)

		beforeManifest := mustReadFile(t, filepath.Join(work, MCPBManifestFilename))
		beforeTools := mustReadFile(t, filepath.Join(work, ToolsManifestFilename))
		require.Contains(t, string(beforeManifest), mcpManifestHandEdit)
		require.Contains(t, string(beforeTools), toolsManifestHandEdit)

		require.NoError(t, PromoteWorkingCLI(promoteManifestCLI, work, NewMinimalState(promoteManifestCLI, work)))

		lib := filepath.Join(PublishedLibraryRoot(), promoteManifestAPI)
		assert.Equal(t, beforeManifest, mustReadFile(t, filepath.Join(lib, MCPBManifestFilename)))
		assert.Equal(t, beforeTools, mustReadFile(t, filepath.Join(lib, ToolsManifestFilename)))
		assert.Equal(t, beforeManifest, mustReadFile(t, filepath.Join(work, MCPBManifestFilename)))
		assert.Equal(t, beforeTools, mustReadFile(t, filepath.Join(work, ToolsManifestFilename)))
		assertPromoteBundleManifest(t, lib)
		assertPromoteBundleManifest(t, work)
	})

	t.Run("library-only manifest patch", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("PRINTING_PRESS_HOME", home)
		work := clonePromoteMCPCLI(t, template)
		stampPromoteManifestHandEdits(t, work)
		keepPromoteBinariesCurrent(t, work)

		lib := filepath.Join(PublishedLibraryRoot(), promoteManifestAPI)
		writePatchRecordFile(t, filepath.Join(lib, PatchesDirName), "manifest-hand-edit.json", PatchRecord{
			SchemaVersion: CurrentPatchesIndexSchemaVersion,
			ID:            "manifest-hand-edit",
			Files:         []string{MCPBManifestFilename},
			CallSites:     []string{mcpManifestHandEdit},
		})

		beforeManifest := mustReadFile(t, filepath.Join(work, MCPBManifestFilename))
		require.Contains(t, string(beforeManifest), mcpManifestHandEdit)
		require.Contains(t, string(beforeManifest), `"hand_edited_prompt"`)
		_, err := os.Stat(filepath.Join(work, PatchesDirName, "manifest-hand-edit.json"))
		require.True(t, os.IsNotExist(err))

		require.NoError(t, PromoteWorkingCLI(promoteManifestCLI, work, NewMinimalState(promoteManifestCLI, work)))

		assert.Equal(t, beforeManifest, mustReadFile(t, filepath.Join(lib, MCPBManifestFilename)))
		assert.Equal(t, beforeManifest, mustReadFile(t, filepath.Join(work, MCPBManifestFilename)))
		tools := string(mustReadFile(t, filepath.Join(lib, ToolsManifestFilename)))
		assert.NotContains(t, tools, toolsManifestHandEdit)
		assert.Contains(t, tools, "https://api.example.com")
		assert.FileExists(t, filepath.Join(lib, PatchesDirName, "manifest-hand-edit.json"))
		assertPromoteBundleManifest(t, lib)
		assertPromoteBundleManifest(t, work)
	})

	t.Run("tools patch still reconciles manifest", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("PRINTING_PRESS_HOME", home)
		work := clonePromoteMCPCLI(t, template)
		stampPromoteManifestHandEdits(t, work)
		writePatchRecordFile(t, filepath.Join(work, PatchesDirName), "tools-manifest-hand-edit.json", PatchRecord{
			SchemaVersion: CurrentPatchesIndexSchemaVersion,
			ID:            "tools-manifest-hand-edit",
			Files:         []string{ToolsManifestFilename},
			CallSites:     []string{toolsManifestHandEdit},
		})
		keepPromoteBinariesCurrent(t, work)
		beforeTools := mustReadFile(t, filepath.Join(work, ToolsManifestFilename))

		require.NoError(t, PromoteWorkingCLI(promoteManifestCLI, work, NewMinimalState(promoteManifestCLI, work)))

		lib := filepath.Join(PublishedLibraryRoot(), promoteManifestAPI)
		manifest := string(mustReadFile(t, filepath.Join(lib, MCPBManifestFilename)))
		assert.NotContains(t, manifest, mcpManifestHandEdit)
		assert.Contains(t, manifest, "TEST_API_TOKEN")
		assert.Equal(t, beforeTools, mustReadFile(t, filepath.Join(lib, ToolsManifestFilename)))
		assertPromoteBundleManifest(t, lib)
	})

	t.Run("unpatched", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("PRINTING_PRESS_HOME", home)
		work := clonePromoteMCPCLI(t, template)
		stampPromoteManifestHandEdits(t, work)
		keepPromoteBinariesCurrent(t, work)

		require.NoError(t, PromoteWorkingCLI(promoteManifestCLI, work, NewMinimalState(promoteManifestCLI, work)))

		lib := filepath.Join(PublishedLibraryRoot(), promoteManifestAPI)
		manifest := string(mustReadFile(t, filepath.Join(lib, MCPBManifestFilename)))
		tools := string(mustReadFile(t, filepath.Join(lib, ToolsManifestFilename)))
		assert.NotContains(t, manifest, mcpManifestHandEdit)
		assert.Contains(t, manifest, "TEST_API_TOKEN")
		assert.NotContains(t, tools, toolsManifestHandEdit)
		assert.Contains(t, tools, "https://api.example.com")
		assertPromoteBundleManifest(t, lib)
	})
}

func writePromoteMCPCLI(t *testing.T, cliDir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module example.com/patchedmcp\n\ngo 1.24\n"), 0o644))
	writePromoteMain(t, cliDir, promoteManifestCLI, `println("fresh cli")`)
	writePromoteMain(t, cliDir, promoteManifestMCP, `println("fresh mcp")`)
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "client"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "client", "env.go"), []byte("package client\n\nimport \"os\"\n\nfunc token() string {\n\treturn os.Getenv(\"TEST_API_TOKEN\")\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), []byte(`openapi: "3.0.3"
info:
  title: Patched MCP
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
`), 0o644))
	require.NoError(t, WriteCLIManifest(cliDir, CLIManifest{
		SchemaVersion: CurrentCLIManifestSchemaVersion,
		APIName:       promoteManifestAPI,
		CLIName:       promoteManifestCLI,
		MCPBinary:     promoteManifestMCP,
	}))
}

func clonePromoteMCPCLI(t *testing.T, template string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), promoteManifestCLI)
	require.NoError(t, CopyDir(template, work))
	return work
}

func stampPromoteManifestHandEdits(t *testing.T, cliDir string) {
	t.Helper()
	data := mustReadFile(t, filepath.Join(cliDir, MCPBManifestFilename))
	var manifest MCPBManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	if manifest.UserConfig == nil {
		manifest.UserConfig = map[string]MCPBVar{}
	}
	manifest.UserConfig["hand_edited_prompt"] = MCPBVar{
		Type:        "string",
		Title:       "Hand edited prompt",
		Description: mcpManifestHandEdit,
	}
	out, err := marshalMCPBManifest(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, MCPBManifestFilename), out, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, ToolsManifestFilename), []byte("{\n  \"api_name\": \"patchedmcp\",\n  \"description\": \""+toolsManifestHandEdit+"\",\n  \"tools\": []\n}\n"), 0o644))
}

func writePromoteManifestPatches(t *testing.T, cliDir string) {
	t.Helper()
	patches := filepath.Join(cliDir, PatchesDirName)
	writePatchRecordFile(t, patches, "manifest-hand-edit.json", PatchRecord{
		SchemaVersion: CurrentPatchesIndexSchemaVersion,
		ID:            "manifest-hand-edit",
		Files:         []string{MCPBManifestFilename},
		CallSites:     []string{mcpManifestHandEdit},
	})
	writePatchRecordFile(t, patches, "tools-manifest-hand-edit.json", PatchRecord{
		SchemaVersion: CurrentPatchesIndexSchemaVersion,
		ID:            "tools-manifest-hand-edit",
		Files:         []string{ToolsManifestFilename},
		CallSites:     []string{toolsManifestHandEdit},
	})
}

func keepPromoteBinariesCurrent(t *testing.T, cliDir string) {
	t.Helper()
	future := time.Now().Add(time.Hour)
	for _, name := range []string{promoteManifestCLI, promoteManifestMCP} {
		path := StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(name, runtime.GOOS))
		require.NoError(t, os.Chtimes(path, future, future))
	}
}

func assertPromoteBundleManifest(t *testing.T, cliDir string) {
	t.Helper()
	bundle := DefaultBundleOutputPath(cliDir, promoteManifestMCP, runtime.GOOS, runtime.GOARCH)
	assertBundleEntryEqualsFile(t, bundle, MCPBManifestFilename, filepath.Join(cliDir, MCPBManifestFilename))
}
