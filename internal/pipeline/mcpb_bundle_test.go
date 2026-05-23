package pipeline

import (
	"archive/zip"
	"encoding/json"
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
