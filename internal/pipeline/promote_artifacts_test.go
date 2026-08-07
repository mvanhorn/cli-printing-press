package pipeline

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
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

func TestRefreshPromoteArtifacts_FailedMCPBuildPreservesBothStagedBinaries(t *testing.T) {
	cliDir := filepath.Join(t.TempDir(), "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	cliName := "test-pp-cli"
	mcpName := "test-pp-mcp"
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))
	writePromoteMain(t, cliDir, cliName, `println("fresh cli")`)
	writePromoteMain(t, cliDir, mcpName, `this is not valid Go`)
	require.NoError(t, WriteCLIManifest(cliDir, CLIManifest{
		SchemaVersion: CurrentCLIManifestSchemaVersion,
		APIName:       "test",
		CLIName:       cliName,
		MCPBinary:     mcpName,
	}))

	cliBinary := StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(cliName, runtime.GOOS))
	mcpBinary := StagedMCPBinaryPath(cliDir, platform.ExecutablePathForGOOS(mcpName, runtime.GOOS))
	require.NoError(t, os.MkdirAll(filepath.Dir(cliBinary), 0o755))
	require.NoError(t, os.WriteFile(cliBinary, []byte("original cli"), 0o755))
	require.NoError(t, os.WriteFile(mcpBinary, []byte("original mcp"), 0o755))

	refreshed, err := refreshPromoteArtifacts(cliDir, cliName)
	require.Error(t, err)
	assert.False(t, refreshed)
	assert.Equal(t, "original cli", string(mustReadFile(t, cliBinary)))
	assert.Equal(t, "original mcp", string(mustReadFile(t, mcpBinary)))
}

func TestReplacePromoteBinaryPair_SecondReplacementFailureRestoresBothOriginals(t *testing.T) {
	dir := t.TempDir()
	cliOutputPath := filepath.Join(dir, "cli")
	mcpOutputPath := filepath.Join(dir, "mcp")
	cliTmpPath := filepath.Join(dir, "new-cli")
	mcpTmpPath := filepath.Join(dir, "new-mcp")
	require.NoError(t, os.WriteFile(cliOutputPath, []byte("original cli"), 0o755))
	require.NoError(t, os.WriteFile(mcpOutputPath, []byte("original mcp"), 0o755))
	require.NoError(t, os.WriteFile(cliTmpPath, []byte("new cli"), 0o755))
	require.NoError(t, os.WriteFile(mcpTmpPath, []byte("new mcp"), 0o755))

	replacements := 0
	err := replacePromoteBinaryPair(cliTmpPath, cliOutputPath, mcpTmpPath, mcpOutputPath, func(src, dst string) error {
		replacements++
		if replacements == 2 {
			return errors.New("injected MCP replacement failure")
		}
		return replaceLiveCheckBinary(src, dst)
	})
	require.ErrorContains(t, err, "injected MCP replacement failure")
	assert.Equal(t, "original cli", string(mustReadFile(t, cliOutputPath)))
	assert.Equal(t, "original mcp", string(mustReadFile(t, mcpOutputPath)))
}

func TestPromoteBundleMatches_RequiresExactEntrySet(t *testing.T) {
	cliDir := t.TempDir()
	mcpArchiveName := "test-pp-mcp"
	cliArchiveName := "test-pp-cli"
	mcpBinary := filepath.Join(cliDir, "staged-mcp")
	cliBinary := filepath.Join(cliDir, "staged-cli")
	require.NoError(t, os.WriteFile(mcpBinary, []byte("mcp bytes"), 0o755))
	require.NoError(t, os.WriteFile(cliBinary, []byte("cli bytes"), 0o755))
	manifest := []byte(`{"name":"test-pp-mcp","server":{"entry_point":"bin/test-pp-mcp"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, MCPBManifestFilename), manifest, 0o644))

	expectedEntries := []zipTestEntry{
		{name: MCPBManifestFilename, data: manifest},
		{name: "bin/" + mcpArchiveName, data: []byte("mcp bytes")},
		{name: "bin/" + cliArchiveName, data: []byte("cli bytes")},
	}
	tests := []struct {
		name    string
		entries []zipTestEntry
	}{
		{name: "unexpected entry", entries: append(append([]zipTestEntry{}, expectedEntries...), zipTestEntry{name: "extra.txt", data: []byte("extra")})},
		{name: "duplicate expected entry", entries: append(append([]zipTestEntry{}, expectedEntries...), expectedEntries[1])},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundlePath := filepath.Join(t.TempDir(), "bundle.mcpb")
			writeTestZIP(t, bundlePath, tt.entries)
			matches, err := promoteBundleMatches(bundlePath, cliDir, mcpArchiveName, mcpBinary, cliArchiveName, cliBinary)
			require.NoError(t, err)
			assert.False(t, matches)
		})
	}
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

type zipTestEntry struct {
	name string
	data []byte
}

func writeTestZIP(t *testing.T, path string, entries []zipTestEntry) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		require.NoError(t, err)
		_, err = w.Write(entry.data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
