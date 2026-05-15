package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBinaryDir creates a temp dir with fake `git` and `gh` shell scripts and
// replaces PATH entirely with it. ghScript is the body emitted by the `gh`
// stub (must include a shebang); pass an empty string to omit gh so
// exec.LookPath fails. Note: replacing PATH (rather than prepending) means
// any other binary called transitively from the code under test will also
// fail to resolve — keep scope to functions that only exec git/gh.
func stubBinaryDir(t *testing.T, gitScript, ghScript string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell scripts are Unix-only")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(gitScript), 0o755))
	if ghScript != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(ghScript), 0o755))
	}
	t.Setenv("PATH", dir)
	return dir
}

// TestResolvePrinterForExistingManifestWins pins the regen-preservation tier.
func TestResolvePrinterForExistingManifestWins(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer":"mvanhorn","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	assert.Equal(t, "mvanhorn", resolvePrinterForExisting(dir))
}

// TestResolvePrinterForExistingFallsThroughOnAbsentPrinter exercises the git-config tier.
func TestResolvePrinterForExistingFallsThroughOnAbsentPrinter(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	// We do not assert the value (depends on runner's git config), only
	// that the fall-through path is exercised without panicking.
	_ = resolvePrinterForExisting(dir)
}

// TestResolvePrinterForExistingFallsThroughOnEmptyPrinter treats empty as absent.
func TestResolvePrinterForExistingFallsThroughOnEmptyPrinter(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer":"","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	_ = resolvePrinterForExisting(dir)
}

// TestReadManifestPrinterHandlesMissingFile keeps missing manifests non-fatal.
func TestReadManifestPrinterHandlesMissingFile(t *testing.T) {
	assert.Equal(t, "", readManifestPrinter(t.TempDir()))
}

// TestReadManifestPrinterHandlesMalformed keeps invalid JSON non-fatal.
func TestReadManifestPrinterHandlesMalformed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte("not json"), 0o644))
	assert.Equal(t, "", readManifestPrinter(dir))
}

// TestReadManifestPrinterHandlesNonStringValue treats non-string values as missing.
func TestReadManifestPrinterHandlesNonStringValue(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer":42,"cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))
	assert.Equal(t, "", readManifestPrinter(dir))
}

// TestResolvePrinterNameForExistingManifestWins pins display-name preservation.
func TestResolvePrinterNameForExistingManifestWins(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer_name":"Matt Van Horn","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	assert.Equal(t, "Matt Van Horn", resolvePrinterNameForExisting(dir))
}

// TestResolvePrinterNameForExistingFallsThroughOnAbsentName exercises git config.
func TestResolvePrinterNameForExistingFallsThroughOnAbsentName(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	// Returned value depends on runner's git config; just exercise the path.
	_ = resolvePrinterNameForExisting(dir)
}

// TestReadManifestPrinterNameHandlesMissingFile keeps missing manifests non-fatal.
func TestReadManifestPrinterNameHandlesMissingFile(t *testing.T) {
	assert.Equal(t, "", readManifestPrinterName(t.TempDir()))
}

// TestGenerateLeavesPrinterEmptyWhenGitHandleUnset prevents owner-slug false positives.
func TestGenerateLeavesPrinterEmptyWhenGitHandleUnset(t *testing.T) {
	// Stub both git (returns empty for `git config github.user`) and gh
	// (exits non-zero — simulates unauthenticated/missing gh) so resolution
	// falls through to "". Without the gh stub a logged-in test runner
	// would now pick up the gh fallback and flip the assertion.
	stubBinaryDir(t,
		"#!/bin/sh\nexit 0\n",
		"#!/bin/sh\necho 'not authenticated' >&2\nexit 1\n",
	)

	apiSpec := minimalSpec("self-owned")
	outputDir := filepath.Join(t.TempDir(), "self-owned-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	assert.Empty(t, gen.Spec.Printer)
}

// TestResolvePrinterForNewFallsBackToGhWhenGitUnset pins the gh fallback.
// Covers the common case where `git config github.user` is unset but `gh`
// is logged in — without this fallback, every fresh print on such a machine
// emits an empty printer and fails publish-validate's manifest contract.
func TestResolvePrinterForNewFallsBackToGhWhenGitUnset(t *testing.T) {
	stubBinaryDir(t,
		"#!/bin/sh\nexit 0\n",
		"#!/bin/sh\necho ghfallback-user\n",
	)
	assert.Equal(t, "ghfallback-user", resolvePrinterForNew())
}

// TestResolvePrinterForNewPrefersGitConfigOverGh pins resolution order.
func TestResolvePrinterForNewPrefersGitConfigOverGh(t *testing.T) {
	stubBinaryDir(t,
		"#!/bin/sh\necho git-handle\n",
		"#!/bin/sh\necho gh-handle\n",
	)
	assert.Equal(t, "git-handle", resolvePrinterForNew())
}

// TestResolvePrinterForNewReturnsEmptyWhenBothUnset pins the empty fallthrough.
func TestResolvePrinterForNewReturnsEmptyWhenBothUnset(t *testing.T) {
	stubBinaryDir(t,
		"#!/bin/sh\nexit 1\n",
		"#!/bin/sh\nexit 1\n",
	)
	assert.Empty(t, resolvePrinterForNew())
}
