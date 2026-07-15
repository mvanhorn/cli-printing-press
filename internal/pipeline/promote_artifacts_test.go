package pipeline

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromoteWorkingCLI_RebuildsStaleBinariesAndBundle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", tmp)
	cliDir := filepath.Join(tmp, "working", "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	cliName := "test-pp-cli"
	mcpName := "test-pp-mcp"

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))
	writePromoteMain(t, cliDir, cliName, `println("fresh cli")`)
	writePromoteMain(t, cliDir, mcpName, `println("fresh mcp")`)
	require.NoError(t, WriteCLIManifest(cliDir, CLIManifest{
		SchemaVersion: CurrentCLIManifestSchemaVersion,
		APIName:       "test",
		CLIName:       cliName,
		MCPBinary:     mcpName,
	}))
	require.NoError(t, WriteMCPBManifest(cliDir))

	cliBinary := StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(cliName, runtime.GOOS))
	mcpBinary := StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(mcpName, runtime.GOOS))
	require.NoError(t, os.MkdirAll(filepath.Dir(cliBinary), 0o755))
	require.NoError(t, os.WriteFile(cliBinary, []byte("stale cli"), 0o755))
	require.NoError(t, os.WriteFile(mcpBinary, []byte("stale mcp"), 0o755))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(cliBinary, old, old))
	require.NoError(t, os.Chtimes(mcpBinary, old, old))

	state := NewMinimalState(cliName, cliDir)
	require.NoError(t, PromoteWorkingCLI(cliName, cliDir, state))
	workBundlePath := DefaultBundleOutputPath(cliDir, mcpName, runtime.GOOS, runtime.GOARCH)
	before := fileDigest(t, workBundlePath)
	infoBefore, err := os.Stat(workBundlePath)
	require.NoError(t, err)

	state.WorkingDir = cliDir
	state.OutputDir = cliDir
	writePhase5PassForState(t, state, "none")
	require.NoError(t, PromoteWorkingCLI(cliName, cliDir, state))
	assert.Equal(t, before, fileDigest(t, workBundlePath))
	infoAfter, err := os.Stat(workBundlePath)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime())

	cliDir = filepath.Join(PublishedLibraryRoot(), "test")
	cliBinary = StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(cliName, runtime.GOOS))
	mcpBinary = StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(mcpName, runtime.GOOS))

	output, err := exec.Command(cliBinary).CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "fresh cli")
	output, err = exec.Command(mcpBinary).CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "fresh mcp")

	bundlePath := DefaultBundleOutputPath(cliDir, mcpName, runtime.GOOS, runtime.GOARCH)
	assertBundleEntryEqualsFile(t, bundlePath, MCPBManifestFilename, filepath.Join(cliDir, MCPBManifestFilename))
	assertBundleEntryEqualsFile(t, bundlePath, "bin/"+platform.ExecutablePathForGOOS(cliName, runtime.GOOS), cliBinary)
	assertBundleEntryEqualsFile(t, bundlePath, "bin/"+platform.ExecutablePathForGOOS(mcpName, runtime.GOOS), mcpBinary)
}

func TestRefreshPromoteArtifacts_CreatesMissingStageDirectory(t *testing.T) {
	cliDir := filepath.Join(t.TempDir(), "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	cliName := "test-pp-cli"
	mcpName := "test-pp-mcp"
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))
	writePromoteMain(t, cliDir, cliName, `println("fresh cli")`)
	writePromoteMain(t, cliDir, mcpName, `println("fresh mcp")`)
	require.NoError(t, WriteCLIManifest(cliDir, CLIManifest{
		SchemaVersion: CurrentCLIManifestSchemaVersion,
		APIName:       "test",
		CLIName:       cliName,
		MCPBinary:     mcpName,
	}))

	refreshed, err := refreshPromoteArtifacts(cliDir, cliName)
	require.NoError(t, err)
	assert.True(t, refreshed)
	cliBinary := StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(cliName, runtime.GOOS))
	assert.FileExists(t, cliBinary)
	assert.FileExists(t, StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(mcpName, runtime.GOOS)))

	otherArch := "amd64"
	if runtime.GOARCH == otherArch {
		otherArch = "arm64"
	}
	require.NoError(t, BuildMCPBBinary(cliDir, cliName, cliBinary, runtime.GOOS, otherArch))
	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(cliBinary, future, future))
	refreshed, err = refreshPromoteArtifacts(cliDir, cliName)
	require.NoError(t, err)
	assert.True(t, refreshed)
	assert.True(t, fileCurrentAt(cliBinary, time.Time{}, runtime.GOOS, runtime.GOARCH))

	require.NoError(t, os.WriteFile(cliBinary, nil, 0o755))
	require.NoError(t, os.Chtimes(cliBinary, future, future))
	refreshed, err = refreshPromoteArtifacts(cliDir, cliName)
	require.NoError(t, err)
	assert.True(t, refreshed)
	info, err := os.Stat(cliBinary)
	require.NoError(t, err)
	assert.Positive(t, info.Size())

	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n\n// dependency input changed\n"), 0o644))
	require.NoError(t, os.Chtimes(filepath.Join(cliDir, "go.mod"), future.Add(time.Hour), future.Add(time.Hour)))
	refreshed, err = refreshPromoteArtifacts(cliDir, cliName)
	require.NoError(t, err)
	assert.True(t, refreshed)
}

func writePromoteMain(t *testing.T, cliDir, name, body string) {
	t.Helper()
	dir := filepath.Join(cliDir, "cmd", name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	source := "package main\nfunc main() { " + body + " }\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0o644))
}

func assertBundleEntryEqualsFile(t *testing.T, bundlePath, entryName, filePath string) {
	t.Helper()
	zr, err := zip.OpenReader(bundlePath)
	require.NoError(t, err)
	defer func() { require.NoError(t, zr.Close()) }()

	want, err := os.ReadFile(filePath)
	require.NoError(t, err)
	for _, entry := range zr.File {
		if entry.Name != entryName {
			continue
		}
		r, err := entry.Open()
		require.NoError(t, err)
		var got bytes.Buffer
		_, err = got.ReadFrom(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		assert.Equal(t, want, got.Bytes())
		return
	}
	t.Fatalf("bundle entry %q not found", entryName)
}

func fileDigest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return sha256.Sum256(data)
}
