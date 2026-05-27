package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, dir, json string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(json), 0o644))
}

func writeRootGo(t *testing.T, dir, header string) {
	t.Helper()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "root.go"), []byte(header+"\npackage cli\n"), 0o644))
}

// The manifest creator object is the authority and wins over every other tier.
func TestResolveCreatorForExisting_ManifestCreatorWins(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"creator": {"handle": "trevin-chow", "name": "Trevin Chow"},
		"printer": "someone-else",
		"owner": "yet-another"
	}`)
	assert.Equal(t, spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"}, resolveCreatorForExisting(dir))
}

// A pre-transition manifest (no creator) falls back to the legacy printer
// fields, then owner fields.
func TestResolveCreatorForExisting_LegacyFallback(t *testing.T) {
	t.Run("printer", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, `{"printer": "mvanhorn", "printer_name": "Matt Van Horn", "owner": "ignored"}`)
		assert.Equal(t, spec.Person{Handle: "mvanhorn", Name: "Matt Van Horn"}, resolveCreatorForExisting(dir))
	})
	t.Run("owner when no printer", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, `{"owner": "hiten-shah", "owner_name": "Hiten Shah"}`)
		assert.Equal(t, spec.Person{Handle: "hiten-shah", Name: "Hiten Shah"}, resolveCreatorForExisting(dir))
	})
}

// With no manifest, the copyright header recovers the creator: the current
// "<name> and contributors." header yields the display name; a legacy
// "<slug>." header yields the slug as the handle.
func TestResolveCreatorForExisting_CopyrightHeaderParse(t *testing.T) {
	t.Run("current format -> name", func(t *testing.T) {
		dir := t.TempDir()
		writeRootGo(t, dir, "// Copyright 2026 Trevin Chow and contributors. Licensed under Apache-2.0. See LICENSE.")
		assert.Equal(t, spec.Person{Name: "Trevin Chow"}, resolveCreatorForExisting(dir))
	})
	t.Run("legacy format -> handle", func(t *testing.T) {
		dir := t.TempDir()
		writeRootGo(t, dir, "// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.")
		assert.Equal(t, spec.Person{Handle: "trevin-chow"}, resolveCreatorForExisting(dir))
	})
}

// The current "<name> and contributors." header must not be misread by the
// legacy slug regex (which would capture nothing useful), and the creator
// regex must not swallow the " and contributors" suffix into the name.
func TestCopyrightCreatorRe_DoesNotSwallowSuffix(t *testing.T) {
	m := copyrightCreatorRe.FindStringSubmatch("// Copyright 2026 Trevin Chow and contributors. Licensed under Apache-2.0.")
	require.Len(t, m, 2)
	assert.Equal(t, "Trevin Chow", m[1])
}

func TestResolveContributorsForExisting(t *testing.T) {
	t.Run("reads list", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, `{"contributors": [{"handle": "jane-doe", "name": "Jane Doe"}, {"handle": "mvanhorn", "name": "Matt Van Horn"}]}`)
		got := resolveContributorsForExisting(dir)
		require.Len(t, got, 2)
		assert.Equal(t, "jane-doe", got[0].Handle)
		assert.Equal(t, "Matt Van Horn", got[1].Name)
	})
	t.Run("empty when absent", func(t *testing.T) {
		assert.Empty(t, resolveContributorsForExisting(t.TempDir()))
	})
}
