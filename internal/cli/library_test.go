package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setLibraryTestEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", home)
	return home
}

func writeTestManifest(t *testing.T, dir string, m pipeline.CLIManifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), data, 0o644))
}

func TestLibraryListJSONWithManifests(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// Create two CLI directories with manifests
	cli1Dir := filepath.Join(libDir, "notion-pp-cli")
	require.NoError(t, os.MkdirAll(cli1Dir, 0o755))
	writeTestManifest(t, cli1Dir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "notion",
		CLIName:       "notion-pp-cli",
		Category:      "productivity",
		CatalogEntry:  "notion",
		Description:   "Notion workspace API",
	})

	cli2Dir := filepath.Join(libDir, "stripe-pp-cli")
	require.NoError(t, os.MkdirAll(cli2Dir, 0o755))
	writeTestManifest(t, cli2Dir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "stripe",
		CLIName:       "stripe-pp-cli",
		Category:      "payments",
		CatalogEntry:  "stripe",
		Description:   "Stripe payment processing API",
	})

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 2)

	// Verify fields are populated from manifest
	names := map[string]bool{}
	for _, e := range entries {
		names[e.CLIName] = true
		assert.NotEmpty(t, e.Dir)
		assert.NotEmpty(t, e.APIName)
		assert.NotEmpty(t, e.Category)
		assert.False(t, e.Modified.IsZero())
	}
	assert.True(t, names["notion-pp-cli"])
	assert.True(t, names["stripe-pp-cli"])
}

func TestLibraryListEmptyLibrary(t *testing.T) {
	setLibraryTestEnv(t)
	// Library directory doesn't exist yet

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Empty(t, entries)
}

func TestLibraryListMissingManifest(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// CLI directory exists but no manifest
	cliDir := filepath.Join(libDir, "test-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, "test-pp-cli", entries[0].CLIName)
	assert.Empty(t, entries[0].APIName)
	assert.Empty(t, entries[0].Category)
}

func TestLibraryListMalformedManifest(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	cliDir := filepath.Join(libDir, "bad-pp-cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cliDir, pipeline.CLIManifestFilename),
		[]byte("not valid json{{{"),
		0o644,
	))

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, "bad-pp-cli", entries[0].CLIName)
	assert.Empty(t, entries[0].APIName)
}

func TestLibraryListClaimedRerunSuffix(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// A claimed rerun directory with -2 suffix
	cliDir := filepath.Join(libDir, "test-pp-cli-2")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, "test-pp-cli-2", entries[0].CLIName)
}

func TestLibraryListIgnoresNonCLIDirectories(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// A directory that's not a CLI and has no manifest
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "random-dir"), 0o755))
	// A real CLI directory
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "test-pp-cli"), 0o755))

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 1)
	assert.Equal(t, "test-pp-cli", entries[0].CLIName)
}
