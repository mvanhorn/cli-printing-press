package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRewriteOwnerHappyPath pins the core flightgoat case: fresh files
// carry the wrong owner; rewrite swaps them to the destination's owner.
func TestRewriteOwnerHappyPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"),
		[]byte("// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.\npackage cli\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "auth.go"),
		[]byte("// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.\npackage cli\n"), 0o644))

	require.NoError(t, RewriteOwner(dir, "trevin-chow", "matt-van-horn"))

	for _, p := range []string{"internal/cli/root.go", "internal/cli/auth.go"} {
		data, err := os.ReadFile(filepath.Join(dir, p))
		require.NoError(t, err)
		assert.Contains(t, string(data), "Copyright 2026 matt-van-horn.")
		assert.NotContains(t, string(data), "trevin-chow")
	}
}

// TestRewriteOwnerLeavesProseAlone verifies we only touch the framework's
// emitted copyright header, not arbitrary mentions of the old owner. Without
// the literal-period anchor in the regex this would corrupt hand-written
// content like CHANGELOG mentions.
func TestRewriteOwnerLeavesProseAlone(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.
package cli

// Originally written by trevin-chow as part of the X experiment.
// Maintained at https://github.com/trevin-chow/something.
const owner = "trevin-chow"
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"), body, 0o644))

	require.NoError(t, RewriteOwner(dir, "trevin-chow", "matt-van-horn"))

	out, err := os.ReadFile(filepath.Join(dir, "x.go"))
	require.NoError(t, err)
	assert.Contains(t, string(out), "Copyright 2026 matt-van-horn.")
	// Hand-written prose mentions stay put.
	assert.Contains(t, string(out), "Originally written by trevin-chow")
	assert.Contains(t, string(out), "github.com/trevin-chow/something")
	assert.Contains(t, string(out), `const owner = "trevin-chow"`)
}

// TestRewriteOwnerIdempotent confirms a second pass is a no-op once the
// owner has been swapped — needed because Apply may run the rewrite step on
// a tempdir that already has the destination's owner if the operator's
// git config already matches.
func TestRewriteOwnerIdempotent(t *testing.T) {
	dir := t.TempDir()
	body := []byte("// Copyright 2026 matt-van-horn. Licensed under Apache-2.0.\npackage cli\n")
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, body, 0o644))

	require.NoError(t, RewriteOwner(dir, "trevin-chow", "matt-van-horn"))
	require.NoError(t, RewriteOwner(dir, "trevin-chow", "matt-van-horn"))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(body), string(out))
}

// TestRewriteOwnerNoOps confirms early-exit cases: empty owners, equal
// owners, missing dir.
func TestRewriteOwnerNoOps(t *testing.T) {
	tests := []struct {
		name     string
		oldOwner string
		newOwner string
	}{
		{"empty old", "", "matt-van-horn"},
		{"empty new", "matt-van-horn", ""},
		{"equal", "matt-van-horn", "matt-van-horn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			body := []byte("// Copyright 2026 matt-van-horn. Licensed.\npackage cli\n")
			path := filepath.Join(dir, "x.go")
			require.NoError(t, os.WriteFile(path, body, 0o644))
			require.NoError(t, RewriteOwner(dir, tt.oldOwner, tt.newOwner))
			out, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(body), string(out))
		})
	}
}

// TestRewriteOwnerSkipsUnrelatedOwner ensures we don't rewrite a file whose
// owner is something other than the explicit oldOwner. This matters when a
// CLI tree contains files from multiple authors (e.g., novel files
// hand-attributed to a different person).
func TestRewriteOwnerSkipsUnrelatedOwner(t *testing.T) {
	dir := t.TempDir()
	body := []byte("// Copyright 2026 someone-else. Licensed.\npackage cli\n")
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, body, 0o644))

	require.NoError(t, RewriteOwner(dir, "trevin-chow", "matt-van-horn"))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(body), string(out))
}
