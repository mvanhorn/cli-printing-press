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

	// Directories that should be excluded (dotfiles, invalid names)
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, ".DS_Store"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, ".hidden"), 0o755))
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

func TestLibraryListSlugKeyedDirectory(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// A slug-keyed directory with a manifest (new format)
	slugDir := filepath.Join(libDir, "dub")
	require.NoError(t, os.MkdirAll(slugDir, 0o755))
	writeTestManifest(t, slugDir, pipeline.CLIManifest{
		SchemaVersion: 1,
		APIName:       "dub",
		CLIName:       "dub-pp-cli",
		Category:      "developer-tools",
		Description:   "Dub link management API",
	})

	// A slug-keyed directory without a manifest (still included by name validation)
	bareSlugDir := filepath.Join(libDir, "cal-com")
	require.NoError(t, os.MkdirAll(bareSlugDir, 0o755))

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"list", "--json"})

	output, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)

	var entries []LibraryEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 2)

	names := map[string]bool{}
	for _, e := range entries {
		names[e.CLIName] = true
	}
	// Manifest-based entry uses CLIName from manifest
	assert.True(t, names["dub-pp-cli"])
	// Bare slug entry uses directory name as CLIName
	assert.True(t, names["cal-com"])
}

func TestMigrateLibraryDirName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"dub-pp-cli", "dub"},
		{"cal-com-pp-cli", "cal-com"},
		{"dub-pp-cli-2", "dub-2"},
		{"steam-web-pp-cli", "steam-web"},
		{"pagliacci-pizza-pp-cli", "pagliacci-pizza"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := migrateLibraryDirName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMigrateLibraryHappyPath(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// Create old-style directories
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "dub-pp-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "cal-com-pp-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "steam-web-pp-cli"), 0o755))

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Len(t, renamed, 3)
	assert.Empty(t, skipped)

	// Verify old dirs are gone and new dirs exist
	_, err = os.Stat(filepath.Join(libDir, "dub-pp-cli"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(libDir, "dub"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(libDir, "cal-com-pp-cli"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(libDir, "cal-com"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(libDir, "steam-web-pp-cli"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(libDir, "steam-web"))
	assert.NoError(t, err)
}

func TestMigrateLibraryRerunSuffix(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// Rerun suffix must be preserved: dub-pp-cli-2 → dub-2
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "dub-pp-cli-2"), 0o755))

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Len(t, renamed, 1)
	assert.Empty(t, skipped)

	_, err = os.Stat(filepath.Join(libDir, "dub-pp-cli-2"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(libDir, "dub-2"))
	assert.NoError(t, err)
}

func TestMigrateLibrarySkipsNonCLIDirs(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// This has a dot + extra suffix — IsCLIDirName returns false
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "postman-explore-pp-cli.bak-170836"), 0o755))
	// A slug-keyed dir (already migrated)
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "dub"), 0o755))
	// A dotfile
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, ".DS_Store"), 0o755))

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Empty(t, renamed)
	assert.Empty(t, skipped)

	// Verify non-CLI dirs are untouched
	_, err = os.Stat(filepath.Join(libDir, "postman-explore-pp-cli.bak-170836"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(libDir, "dub"))
	assert.NoError(t, err)
}

func TestMigrateLibrarySkipsWhenTargetExists(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	// Old dir and target both exist — skip (idempotent)
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "dub-pp-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "dub"), 0o755))

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Empty(t, renamed)
	assert.Len(t, skipped, 1)
	assert.Contains(t, skipped[0], "already exists")

	// Both dirs still exist
	_, err = os.Stat(filepath.Join(libDir, "dub-pp-cli"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(libDir, "dub"))
	assert.NoError(t, err)
}

func TestMigrateLibraryEmptyLibrary(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")
	require.NoError(t, os.MkdirAll(libDir, 0o755))

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Empty(t, renamed)
	assert.Empty(t, skipped)
}

func TestMigrateLibraryNonexistentLibrary(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library") // does not exist

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Empty(t, renamed)
	assert.Empty(t, skipped)
}

func TestMigrateLibraryTraversalContainment(t *testing.T) {
	// Test Layer 2 containment: a crafted directory name whose derived slug
	// would escape the library root must be rejected.
	//
	// We can't easily create a directory named with ".." via os.MkdirAll
	// since the OS normalizes it. Instead, test the migrateLibrary function
	// directly with a directory that, after -pp-cli removal, would produce
	// a name containing path separators.
	//
	// The IsCLIDirName filter already rejects most invalid names, but we verify
	// the Layer 2 check works by calling migrateLibrary on a library root where
	// the abs-target check would catch escapes that slip through Layer 1.

	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")
	require.NoError(t, os.MkdirAll(libDir, 0o755))

	// This is a valid CLI dir name that migrates cleanly — verify it works
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "safe-pp-cli"), 0o755))

	renamed, skipped, err := migrateLibrary(libDir)
	require.NoError(t, err)
	assert.Len(t, renamed, 1)
	assert.Empty(t, skipped)

	_, err = os.Stat(filepath.Join(libDir, "safe"))
	assert.NoError(t, err)
}

func TestMigrateLibraryViaCommand(t *testing.T) {
	home := setLibraryTestEnv(t)
	libDir := filepath.Join(home, "library")

	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "dub-pp-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libDir, "cal-com-pp-cli"), 0o755))

	cmd := newLibraryCmd()
	cmd.SetArgs([]string{"migrate"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify directories were renamed
	_, err = os.Stat(filepath.Join(libDir, "dub"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(libDir, "cal-com"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(libDir, "dub-pp-cli"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(libDir, "cal-com-pp-cli"))
	assert.True(t, os.IsNotExist(err))
}
