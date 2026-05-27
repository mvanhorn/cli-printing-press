package regenmerge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRootHeader(t *testing.T, dir, header string) {
	t.Helper()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "root.go"), []byte(header+"\npackage cli\n"), 0o644))
}

// parseCopyrightOwner recovers the creator token from both header forms: the
// display name from the current "<name> and contributors." header, and the
// slug from the legacy header. This must stay aligned with the generator
// package's copyrightCreatorRe / copyrightOwnerRe pair.
func TestRegenmergeParseCopyrightOwnerBothFormats(t *testing.T) {
	t.Run("current format", func(t *testing.T) {
		dir := t.TempDir()
		writeRootHeader(t, dir, "// Copyright 2026 Trevin Chow and contributors. Licensed under Apache-2.0.")
		assert.Equal(t, "Trevin Chow", parseCopyrightOwner(dir))
	})
	t.Run("legacy format", func(t *testing.T) {
		dir := t.TempDir()
		writeRootHeader(t, dir, "// Copyright 2026 trevin-chow. Licensed under Apache-2.0.")
		assert.Equal(t, "trevin-chow", parseCopyrightOwner(dir))
	})
}

// The manifest owner still wins over the header parse.
func TestRegenmergeResolveOwnerForTreeManifestWins(t *testing.T) {
	dir := t.TempDir()
	writeRootHeader(t, dir, "// Copyright 2026 Trevin Chow and contributors.")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"),
		[]byte(`{"owner": "manifest-owner"}`), 0o644))
	assert.Equal(t, "manifest-owner", resolveOwnerForTree(dir))
}
