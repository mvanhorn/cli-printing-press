package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RewriteOwner must swap only the creator token in the current
// "<name> and contributors." header, preserving the suffix and period.
func TestRewriteOwnerCurrentFormatPreservesSuffix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 Matt Van Horn and contributors. Licensed under Apache-2.0. See LICENSE.\npackage cli\n"), 0o644))

	require.NoError(t, RewriteOwner(dir, "Matt Van Horn", "Trevin Chow"))

	data, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Copyright 2026 Trevin Chow and contributors.")
	assert.NotContains(t, string(data), "Matt Van Horn")
}

// A display name containing `$` must be inserted literally — not interpreted as
// a regexp backreference in the replacement template.
func TestRewriteOwnerLiteralDollarInName(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 Old Name and contributors. Licensed under Apache-2.0.\npackage cli\n"), 0o644))

	require.NoError(t, RewriteOwner(dir, "Old Name", "A$AP $1 Worldwide"))

	data, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Copyright 2026 A$AP $1 Worldwide and contributors.")
}

// The legacy slug header still rewrites with no spurious suffix.
func TestRewriteOwnerLegacyFormatUnchangedShape(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 matt-van-horn. Licensed under Apache-2.0. See LICENSE.\npackage cli\n"), 0o644))

	require.NoError(t, RewriteOwner(dir, "matt-van-horn", "trevin-chow"))

	data, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Copyright 2026 trevin-chow.")
	assert.NotContains(t, string(data), "and contributors")
}
