package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "github.user")
	t.Setenv("GIT_CONFIG_VALUE_0", "")

	apiSpec := minimalSpec("self-owned")
	outputDir := filepath.Join(t.TempDir(), "self-owned-pp-cli")
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	assert.Empty(t, gen.Spec.Printer)
}
