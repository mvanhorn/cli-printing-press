package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifestVersion(t *testing.T, dir, version string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, pipeline.CLIManifestFilename),
		[]byte(`{"schema_version":1,"printing_press_version":"`+version+`"}`),
		0o644,
	))
}

func TestEnsureVersionNotOlderThanCLIManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    string
		generated  string
		wantErr    bool
		wantErrSub string
	}{
		{name: "same version runs", current: "4.19.0", generated: "4.19.0"},
		{name: "newer version runs", current: "4.22.1", generated: "4.19.0"},
		{name: "v-prefixed current version runs", current: "v4.22.1", generated: "4.19.0"},
		{
			name:       "older version refuses",
			current:    "4.2.0",
			generated:  "4.19.0",
			wantErr:    true,
			wantErrSub: "mcp-sync refused: cli-printing-press 4.2.0 is older than this CLI's generating version 4.19.0",
		},
		{
			name:       "invalid version refuses",
			current:    "not-a-version",
			generated:  "4.19.0",
			wantErr:    true,
			wantErrSub: "cannot compare cli-printing-press version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeManifestVersion(t, dir, tt.generated)

			err := ensureVersionNotOlderThanCLIManifest(dir, "mcp-sync", tt.current)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestEnsureVersionNotOlderThanCLIManifestSkipsMissingOrLegacyManifest(t *testing.T) {
	t.Parallel()

	require.NoError(t, ensureVersionNotOlderThanCLIManifest(t.TempDir(), "mcp-sync", "4.22.1"))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), []byte(`{"schema_version":1}`), 0o644))
	require.NoError(t, ensureVersionNotOlderThanCLIManifest(dir, "mcp-sync", "4.22.1"))
}

func TestEnsureMCPVersionCompatibleWithCLIManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    string
		generated  string
		wantErr    bool
		wantErrSub string
	}{
		{name: "same version runs", current: "4.30.2", generated: "4.30.2"},
		{name: "same minor patch drift runs", current: "4.30.3", generated: "4.30.2"},
		{
			name:       "newer minor refuses",
			current:    "4.31.0",
			generated:  "4.30.2",
			wantErr:    true,
			wantErrSub: "mcp-sync refused: cli-printing-press 4.31.0 does not match the target CLI's generating minor version 4.30; reprint required",
		},
		{
			name:       "older minor refuses",
			current:    "4.29.9",
			generated:  "4.30.2",
			wantErr:    true,
			wantErrSub: "mcp-sync refused: cli-printing-press 4.29.9 does not match the target CLI's generating minor version 4.30; reprint required",
		},
		{
			name:       "invalid version refuses",
			current:    "not-a-version",
			generated:  "4.30.2",
			wantErr:    true,
			wantErrSub: "cannot compare cli-printing-press version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeManifestVersion(t, dir, tt.generated)

			err := ensureMCPVersionCompatibleWithCLIManifest(dir, tt.current)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestEnsureMCPVersionCompatibleWithCLIManifestSkipsMissingOrLegacyManifest(t *testing.T) {
	t.Parallel()

	require.NoError(t, ensureMCPVersionCompatibleWithCLIManifest(t.TempDir(), "4.31.0"))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), []byte(`{"schema_version":1}`), 0o644))
	require.NoError(t, ensureMCPVersionCompatibleWithCLIManifest(dir, "4.31.0"))
}

func TestRegenMergeCommandRefusesStaleBinaryBeforeClassify(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	freshDir := t.TempDir()
	writeManifestVersion(t, cliDir, "999.0.0")

	cmd := newRegenMergeCmd()
	cmd.SetArgs([]string{cliDir, "--fresh", freshDir})
	err := cmd.Execute()
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitInputError, exitErr.Code)
	assert.Contains(t, err.Error(), "regen-merge refused")
}

func TestMCPSyncCommandRefusesStaleBinaryBeforeSync(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	writeManifestVersion(t, cliDir, "999.0.0")

	cmd := newMCPSyncCmd()
	cmd.SetArgs([]string{cliDir})
	err := cmd.Execute()
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitInputError, exitErr.Code)
	assert.Contains(t, err.Error(), "mcp-sync refused")
}

func TestMCPSyncCommandRefusesDifferentMinorVersionBeforeSync(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	writeManifestVersion(t, cliDir, "4.29.0")

	cmd := newMCPSyncCmd()
	cmd.SetArgs([]string{cliDir})
	err := cmd.Execute()
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitInputError, exitErr.Code)
	assert.Contains(t, err.Error(), "reprint required")
}
