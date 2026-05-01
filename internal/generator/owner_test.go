package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveOwnerForExistingManifestWins pins tier 1 of the precedence
// chain: a `.printing-press.json` with an `owner` field is the highest-
// priority signal short of the explicit --owner flag (which the caller
// sets via spec.Owner before resolveOwnerForExisting runs).
func TestResolveOwnerForExistingManifestWins(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"owner":"hiten-shah","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))

	// Even with a copyright header that disagrees, the manifest wins.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 someone-else. Licensed under Apache-2.0.\npackage cli\n"), 0o644))

	assert.Equal(t, "hiten-shah", resolveOwnerForExisting(dir))
}

// TestResolveOwnerForExistingCopyrightFallback pins tier 2: when the
// manifest is absent or the owner field is empty, parse the copyright
// header in internal/cli/root.go.
func TestResolveOwnerForExistingCopyrightFallback(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 hiten-shah. Licensed under Apache-2.0. See LICENSE.\npackage cli\n"), 0o644))

	assert.Equal(t, "hiten-shah", resolveOwnerForExisting(dir))
}

// TestResolveOwnerForExistingFallsThroughOnAbsentOwner pins that a
// manifest without an `owner` field doesn't short-circuit the chain —
// copyright parse gets a chance.
func TestResolveOwnerForExistingFallsThroughOnAbsentOwner(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"cli_name":"foo-pp-cli","api_name":"foo"}` // no owner field
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 hiten-shah.\npackage cli\n"), 0o644))

	assert.Equal(t, "hiten-shah", resolveOwnerForExisting(dir))
}

// TestResolveOwnerForExistingFallsThroughOnEmptyOwner pins the explicit
// empty-string case: `{"owner":""}` is treated identically to the
// absent-field case — both fall through to copyright parse. Without this
// guarantee, a corrupted or partially-populated manifest would short-
// circuit to an empty owner string.
func TestResolveOwnerForExistingFallsThroughOnEmptyOwner(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"schema_version":1,"owner":"","cli_name":"foo-pp-cli","api_name":"foo"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 hiten-shah.\npackage cli\n"), 0o644))

	assert.Equal(t, "hiten-shah", resolveOwnerForExisting(dir))
}

// TestResolveOwnerForNew pins the brand-new project path: no existing
// tree, falls through git config to USER. Result depends on the runner's
// git config; we just check it returns a non-empty value.
func TestResolveOwnerForNew(t *testing.T) {
	assert.NotEmpty(t, resolveOwnerForNew())
}

// TestParseCopyrightOwnerHandlesMissingFile confirms the function returns
// "" rather than panicking when root.go isn't there.
func TestParseCopyrightOwnerHandlesMissingFile(t *testing.T) {
	assert.Equal(t, "", parseCopyrightOwner(t.TempDir()))
}

// TestReadManifestOwnerHandlesMissingFile confirms the function returns
// "" rather than panicking when the manifest isn't there.
func TestReadManifestOwnerHandlesMissingFile(t *testing.T) {
	assert.Equal(t, "", readManifestOwner(t.TempDir()))
}

// TestReadManifestOwnerHandlesMalformed confirms invalid JSON returns ""
// rather than crashing the resolver.
func TestReadManifestOwnerHandlesMalformed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"),
		[]byte("not json at all{{{"), 0o644))
	assert.Equal(t, "", readManifestOwner(dir))
}
