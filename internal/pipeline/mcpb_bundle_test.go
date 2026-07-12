package pipeline

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMCPBBundle(t *testing.T) {
	t.Run("packages manifest and binary into ZIP", func(t *testing.T) {
		dir := t.TempDir()

		manifest := MCPBManifest{
			ManifestVersion: MCPBManifestVersion,
			Name:            "demo-pp-mcp",
			Version:         "1.0.0",
			Description:     "demo",
			Author:          MCPBAuthor{Name: "Test"},
			Server: MCPBServer{
				Type:       "binary",
				EntryPoint: "bin/demo-pp-mcp",
				MCPConfig: MCPBLaunchSpec{
					Command: "${__dirname}/bin/demo-pp-mcp",
					Args:    []string{},
				},
			},
		}
		mData, _ := json.Marshal(manifest)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mData, 0o644))

		binPath := filepath.Join(dir, "fake-binary")
		require.NoError(t, os.WriteFile(binPath, []byte("#!/bin/sh\necho fake\n"), 0o755))

		out := filepath.Join(dir, "demo.mcpb")
		err := BuildMCPBBundle(BundleParams{
			CLIDir:     dir,
			BinaryPath: binPath,
			OutputPath: out,
		})
		require.NoError(t, err)

		entries := readZipEntries(t, out)
		assert.Contains(t, entries, MCPBManifestFilename, "bundle must include manifest.json at root")
		assert.Contains(t, entries, "bin/demo-pp-mcp", "bundle must place binary at server.entry_point")
	})

	t.Run("packages companion CLI binary under provided archive name", func(t *testing.T) {
		dir := t.TempDir()

		manifest := MCPBManifest{
			ManifestVersion: MCPBManifestVersion,
			Name:            "demo-pp-mcp",
			Server: MCPBServer{
				Type:       "binary",
				EntryPoint: "bin/demo-pp-mcp",
				MCPConfig:  MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
			},
		}
		mData, _ := json.Marshal(manifest)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mData, 0o644))

		mcpPath := filepath.Join(dir, "fake-mcp")
		cliPath := filepath.Join(dir, "fake-cli.exe")
		require.NoError(t, os.WriteFile(mcpPath, []byte("#!/bin/sh\necho mcp\n"), 0o755))
		require.NoError(t, os.WriteFile(cliPath, []byte("cli"), 0o755))

		out := filepath.Join(dir, "demo.mcpb")
		err := BuildMCPBBundle(BundleParams{
			CLIDir:        dir,
			BinaryPath:    mcpPath,
			CLIBinaryName: "demo-pp-cli.exe",
			CLIBinaryPath: cliPath,
			OutputPath:    out,
		})
		require.NoError(t, err)

		entries := readZipEntries(t, out)
		assert.Contains(t, entries, "bin/demo-pp-cli.exe")
	})

	t.Run("uses provided MCP archive name in ZIP and bundled manifest", func(t *testing.T) {
		dir := t.TempDir()

		mData := []byte(`{
  "manifest_version": "0.3",
  "name": "demo-pp-mcp",
  "server": {
    "type": "binary",
    "entry_point": "bin/demo-pp-mcp",
    "mcp_config": {
      "command": "${__dirname}/bin/demo-pp-mcp",
      "custom_launch_field": "preserve-me"
    },
    "custom_server_field": {"nested": true}
  },
  "custom_top_level_field": ["preserve-me"]
}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mData, 0o644))

		mcpPath := filepath.Join(dir, "fake-mcp.exe")
		require.NoError(t, os.WriteFile(mcpPath, []byte("mcp"), 0o755))

		out := filepath.Join(dir, "demo.mcpb")
		err := BuildMCPBBundle(BundleParams{
			CLIDir:     dir,
			BinaryPath: mcpPath,
			BinaryName: "demo-pp-mcp.exe",
			OutputPath: out,
		})
		require.NoError(t, err)

		entries := readZipEntries(t, out)
		assert.Contains(t, entries, "bin/demo-pp-mcp.exe")
		assert.NotContains(t, entries, "bin/demo-pp-mcp")

		bundledManifest := readZipManifest(t, out)
		assert.Equal(t, "bin/demo-pp-mcp.exe", bundledManifest.Server.EntryPoint)
		assert.Equal(t, "${__dirname}/bin/demo-pp-mcp.exe", bundledManifest.Server.MCPConfig.Command)
		bundledDoc := readZipJSONMap(t, out, MCPBManifestFilename)
		server := bundledDoc["server"].(map[string]any)
		mcpConfig := server["mcp_config"].(map[string]any)
		assert.Equal(t, []any{"preserve-me"}, bundledDoc["custom_top_level_field"])
		assert.Equal(t, map[string]any{"nested": true}, server["custom_server_field"])
		assert.Equal(t, "preserve-me", mcpConfig["custom_launch_field"])

		diskManifest := readManifestFile(t, filepath.Join(dir, MCPBManifestFilename))
		assert.Equal(t, "bin/demo-pp-mcp", diskManifest.Server.EntryPoint)
		assert.Equal(t, "${__dirname}/bin/demo-pp-mcp", diskManifest.Server.MCPConfig.Command)
	})

	t.Run("keeps original manifest bytes when MCP archive name is unchanged", func(t *testing.T) {
		dir := t.TempDir()

		mData := []byte(`{"manifest_version":"0.3","name":"demo-pp-mcp","server":{"type":"binary","entry_point":"bin/demo-pp-mcp","mcp_config":{"command":"${__dirname}/bin/demo-pp-mcp","custom_launch_field":"preserve-me"}},"custom_top_level_field":["preserve-me"]}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mData, 0o644))

		mcpPath := filepath.Join(dir, "fake-mcp")
		require.NoError(t, os.WriteFile(mcpPath, []byte("mcp"), 0o755))

		out := filepath.Join(dir, "demo.mcpb")
		err := BuildMCPBBundle(BundleParams{
			CLIDir:     dir,
			BinaryPath: mcpPath,
			BinaryName: "demo-pp-mcp",
			OutputPath: out,
		})
		require.NoError(t, err)

		assert.Equal(t, mData, readZipEntryBytes(t, out, MCPBManifestFilename))
	})

	t.Run("stamps release version only in bundled manifest", func(t *testing.T) {
		dir := t.TempDir()

		mData := []byte(`{
  "manifest_version": "0.3",
  "name": "demo-pp-mcp",
  "version": "0.0.0",
  "server": {
    "type": "binary",
    "entry_point": "bin/demo-pp-mcp",
    "mcp_config": {
      "command": "${__dirname}/bin/demo-pp-mcp",
      "custom_launch_field": "preserve-me"
    }
  },
  "custom_top_level_field": ["preserve-me"]
}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mData, 0o644))

		mcpPath := filepath.Join(dir, "fake-mcp")
		require.NoError(t, os.WriteFile(mcpPath, []byte("mcp"), 0o755))

		out := filepath.Join(dir, "demo.mcpb")
		err := BuildMCPBBundle(BundleParams{
			CLIDir:     dir,
			BinaryPath: mcpPath,
			OutputPath: out,
			Version:    "0.1.4",
		})
		require.NoError(t, err)

		bundledDoc := readZipJSONMap(t, out, MCPBManifestFilename)
		assert.Equal(t, "0.1.4", bundledDoc["version"])
		assert.Equal(t, []any{"preserve-me"}, bundledDoc["custom_top_level_field"])
		server := bundledDoc["server"].(map[string]any)
		mcpConfig := server["mcp_config"].(map[string]any)
		assert.Equal(t, "preserve-me", mcpConfig["custom_launch_field"])
		assert.Equal(t, mData, readManifestFileBytes(t, filepath.Join(dir, MCPBManifestFilename)))
	})

	t.Run("missing manifest skips silently", func(t *testing.T) {
		dir := t.TempDir()
		// No manifest.json written; bundle call should no-op.
		out := filepath.Join(dir, "missing.mcpb")
		err := BuildMCPBBundle(BundleParams{
			CLIDir:     dir,
			BinaryPath: "/nonexistent",
			OutputPath: out,
		})
		require.NoError(t, err)

		_, statErr := os.Stat(out)
		assert.True(t, os.IsNotExist(statErr), "no bundle should be written")
	})

	t.Run("manifest with empty entry_point is rejected", func(t *testing.T) {
		dir := t.TempDir()
		bad := MCPBManifest{Name: "demo-pp-mcp", Server: MCPBServer{Type: "binary"}}
		data, _ := json.Marshal(bad)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), data, 0o644))

		err := BuildMCPBBundle(BundleParams{
			CLIDir:     dir,
			BinaryPath: filepath.Join(dir, "anything"),
			OutputPath: filepath.Join(dir, "out.mcpb"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entry_point")
	})

	t.Run("binary executable bit is preserved", func(t *testing.T) {
		// Skip on Windows: zip mode bits semantics differ and this is a
		// macOS/Linux-targeted assertion (those hosts launch the binary
		// directly, where the +x bit matters).
		if runtime.GOOS == "windows" {
			t.Skip("executable bit semantics are POSIX-specific")
		}

		dir := t.TempDir()
		manifest := MCPBManifest{
			ManifestVersion: MCPBManifestVersion,
			Name:            "demo-pp-mcp",
			Server: MCPBServer{
				Type:       "binary",
				EntryPoint: "bin/demo-pp-mcp",
				MCPConfig:  MCPBLaunchSpec{Command: "${__dirname}/bin/demo-pp-mcp"},
			},
		}
		mData, _ := json.Marshal(manifest)
		require.NoError(t, os.WriteFile(filepath.Join(dir, MCPBManifestFilename), mData, 0o644))

		binPath := filepath.Join(dir, "fake-binary")
		require.NoError(t, os.WriteFile(binPath, []byte("payload"), 0o755))

		out := filepath.Join(dir, "demo.mcpb")
		require.NoError(t, BuildMCPBBundle(BundleParams{CLIDir: dir, BinaryPath: binPath, OutputPath: out}))

		mode := readZipEntryMode(t, out, "bin/demo-pp-mcp")
		assert.NotZero(t, mode&0o111, "binary entry must keep at least one execute bit set")
	})
}

func TestDefaultBundleOutputPath(t *testing.T) {
	got := DefaultBundleOutputPath("/tmp/cli", "demo-pp-mcp", "darwin", "arm64")
	assert.Equal(t, filepath.Join("/tmp/cli", "build", "demo-pp-mcp-darwin-arm64.mcpb"), got)

	hostFallback := DefaultBundleOutputPath("/tmp/cli", "demo-pp-mcp", "", "")
	assert.True(t, strings.HasPrefix(filepath.Base(hostFallback), "demo-pp-mcp-"+runtime.GOOS+"-"+runtime.GOARCH))
}

func readZipEntries(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func readZipEntryMode(t *testing.T, path, entry string) os.FileMode {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	for _, f := range zr.File {
		if f.Name == entry {
			return f.Mode()
		}
	}
	t.Fatalf("entry %q not found in %s", entry, path)
	return 0
}

func readZipManifest(t *testing.T, path string) MCPBManifest {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	for _, f := range zr.File {
		if f.Name != MCPBManifestFilename {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
		var manifest MCPBManifest
		require.NoError(t, json.NewDecoder(rc).Decode(&manifest))
		return manifest
	}
	t.Fatalf("%s not found in %s", MCPBManifestFilename, path)
	return MCPBManifest{}
}

func readManifestFile(t *testing.T, path string) MCPBManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var manifest MCPBManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	return manifest
}

func readManifestFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func readZipJSONMap(t *testing.T, path, entry string) map[string]any {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	for _, f := range zr.File {
		if f.Name != entry {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
		var doc map[string]any
		require.NoError(t, json.NewDecoder(rc).Decode(&doc))
		return doc
	}
	t.Fatalf("entry %q not found in %s", entry, path)
	return nil
}

func readZipEntryBytes(t *testing.T, path, entry string) []byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = zr.Close() })
	for _, f := range zr.File {
		if f.Name != entry {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		return data
	}
	t.Fatalf("entry %q not found in %s", entry, path)
	return nil
}
