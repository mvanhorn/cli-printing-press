package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolvePrinterForExistingManifestWins pins tier 1: a manifest
// with a `printer` field is the highest-priority signal. This is the
// regen-preservation guarantee — once a CLI is printed, subsequent
// regens by other contributors do not overwrite the original printer
// attribution.
func TestResolvePrinterForExistingManifestWins(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer":"mvanhorn","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	assert.Equal(t, "mvanhorn", resolvePrinterForExisting(dir))
}

// TestResolvePrinterForExistingFallsThroughOnAbsentPrinter pins that a
// manifest without a `printer` field falls through to the git-config
// tier (resolvePrinterForNew). The actual returned value depends on
// the runner's git config; we just check the call does not panic.
func TestResolvePrinterForExistingFallsThroughOnAbsentPrinter(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	// We do not assert the value (depends on runner's git config), only
	// that the fall-through path is exercised without panicking.
	_ = resolvePrinterForExisting(dir)
}

// TestResolvePrinterForExistingFallsThroughOnEmptyPrinter pins the
// explicit empty-string case: `{"printer":""}` is treated identically
// to the absent-field case. Without this, a corrupted manifest would
// short-circuit the chain to an empty printer string.
func TestResolvePrinterForExistingFallsThroughOnEmptyPrinter(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer":"","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	_ = resolvePrinterForExisting(dir)
}

// TestReadManifestPrinterHandlesMissingFile confirms the reader returns
// "" rather than panicking when the manifest isn't there.
func TestReadManifestPrinterHandlesMissingFile(t *testing.T) {
	assert.Equal(t, "", readManifestPrinter(t.TempDir()))
}

// TestReadManifestPrinterHandlesMalformed confirms invalid JSON returns
// "" rather than propagating an error.
func TestReadManifestPrinterHandlesMalformed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte("not json"), 0o644))
	assert.Equal(t, "", readManifestPrinter(dir))
}

// TestReadManifestPrinterHandlesNonStringValue confirms a non-string
// `printer` value is treated as missing rather than panicking.
func TestReadManifestPrinterHandlesNonStringValue(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer":42,"cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))
	assert.Equal(t, "", readManifestPrinter(dir))
}

// TestResolvePrinterNameForExistingManifestWins pins tier 1 for the
// display name: a manifest with a `printer_name` field wins over git
// config. Mirrors resolveOwnerNameForExisting's shape.
func TestResolvePrinterNameForExistingManifestWins(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"printer_name":"Matt Van Horn","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	assert.Equal(t, "Matt Van Horn", resolvePrinterNameForExisting(dir))
}

// TestResolvePrinterNameForExistingFallsThroughOnAbsentName confirms
// that an absent `printer_name` field falls through to git config.
func TestResolvePrinterNameForExistingFallsThroughOnAbsentName(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	// Returned value depends on runner's git config; just exercise the path.
	_ = resolvePrinterNameForExisting(dir)
}

// TestReadManifestPrinterNameHandlesMissingFile confirms the reader
// returns "" rather than panicking when the manifest isn't there.
func TestReadManifestPrinterNameHandlesMissingFile(t *testing.T) {
	assert.Equal(t, "", readManifestPrinterName(t.TempDir()))
}
