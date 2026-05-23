package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimOrForce_DefaultAutoIncrements(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "myapi-pp-cli")

	// First call claims the base
	got, _, err := claimOrForce(base, false, false, false)
	require.NoError(t, err)
	assert.Equal(t, base, got)

	// Second call auto-increments to -2
	got, _, err = claimOrForce(base, false, false, false)
	require.NoError(t, err)
	assert.Equal(t, base+"-2", got)

	// Third call auto-increments to -3
	got, _, err = claimOrForce(base, false, false, false)
	require.NoError(t, err)
	assert.Equal(t, base+"-3", got)
}

func TestClaimOrForce_ForceOverwrites(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "myapi-pp-cli")

	// Create base with a file in it
	require.NoError(t, os.MkdirAll(base, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0o644))

	// Force should snapshot the existing dir to a sibling preserve path
	// and recreate absOut empty for Generate() to populate. The pre-emit
	// guard fires only on hand-authored evidence; a bare `main.go` (no
	// manifest, not in internal/{cli,syncer,store}/) doesn't trip it.
	got, snapshotDir, err := claimOrForce(base, true, false, false)
	require.NoError(t, err)
	assert.Equal(t, base, got)

	// New absOut must exist and be empty.
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	assert.Empty(t, entries, "absOut must be empty after force snapshot")

	// Snapshot must hold the prior content for the caller to merge back in.
	require.NotEmpty(t, snapshotDir, "snapshot path must be returned when prior absOut had content")
	preservedFile := filepath.Join(snapshotDir, "main.go")
	got2, err := os.ReadFile(preservedFile)
	require.NoError(t, err)
	assert.Equal(t, "package main", string(got2), "prior absOut content survives in snapshot")
}

func TestClaimOrForce_ExplicitOutputErrorsOnCollision(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "myapi-pp-cli")

	// Create base with content
	require.NoError(t, os.MkdirAll(base, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0o644))

	// Explicit output without force should error
	_, _, err := claimOrForce(base, false, false, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestClaimOrForce_ExplicitOutputWithForce(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "myapi-pp-cli")

	// Create base with content
	require.NoError(t, os.MkdirAll(base, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0o644))

	// Explicit output with force should succeed
	got, _, err := claimOrForce(base, true, false, true)
	require.NoError(t, err)
	assert.Equal(t, base, got)
}
