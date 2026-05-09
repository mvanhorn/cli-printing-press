package artifacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupGeneratedCLI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "library", "sample-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cache", "go-build"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0o755))

	writeArtifactFile(t, filepath.Join(dir, "sample-cli"))
	writeArtifactFile(t, filepath.Join(dir, "sample-cli.exe"))
	writeArtifactFile(t, filepath.Join(dir, "sample-cli-validation"))
	writeArtifactFile(t, filepath.Join(dir, "sample-cli-validation.exe"))
	writeArtifactFile(t, filepath.Join(dir, "sample-cli-dogfood"))
	writeArtifactFile(t, filepath.Join(dir, "sample-cli-dogfood.exe"))
	writeArtifactFile(t, filepath.Join(dir, ".DS_Store"))
	writeArtifactFile(t, filepath.Join(dir, "nested", ".DS_Store"))
	writeArtifactFile(t, filepath.Join(dir, ".cache", "go-build", "index"))
	writeArtifactFile(t, filepath.Join(dir, "cmd", "library", "sample-cli", "main.go"))

	err := CleanupGeneratedCLI(dir, CleanupOptions{
		RemoveRuntimeBinary:      true,
		RemoveValidationBinaries: true,
		RemoveDogfoodBinaries:    true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(dir, "sample-cli"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli.exe"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-validation"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-validation.exe"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-dogfood"))
	assert.NoFileExists(t, filepath.Join(dir, "sample-cli-dogfood.exe"))
	assert.NoFileExists(t, filepath.Join(dir, ".DS_Store"))
	assert.NoFileExists(t, filepath.Join(dir, "nested", ".DS_Store"))
	assert.NoDirExists(t, filepath.Join(dir, "cmd", "library"))
	assert.DirExists(t, filepath.Join(dir, ".cache"))

	err = CleanupGeneratedCLI(dir, CleanupOptions{RemoveCache: true})
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(dir, ".cache"))
}

func writeArtifactFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("artifact"), 0o644))
}
