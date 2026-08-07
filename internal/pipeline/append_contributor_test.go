package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifestJSON(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, CLIManifestFilename), []byte(body), 0o644))
}

func TestAppendContributor(t *testing.T) {
	t.Run("appends to empty list", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"}}`)

		added, err := AppendContributor(dir, spec.Person{Handle: "jane-doe", Name: "Jane Doe"}, false)
		require.NoError(t, err)
		assert.True(t, added)

		m := readManifest(t, dir)
		require.Len(t, m.Contributors, 1)
		assert.Equal(t, "jane-doe", m.Contributors[0].Handle)
	})

	t.Run("skips the creator", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"}}`)

		added, err := AppendContributor(dir, spec.Person{Handle: "Trevin-Chow", Name: "Trevin Chow"}, false)
		require.NoError(t, err)
		assert.False(t, added, "creator must not be added as a contributor (case-insensitive)")
		assert.Empty(t, readManifest(t, dir).Contributors)
	})

	t.Run("idempotent on an existing contributor", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"},"contributors":[{"handle":"jane-doe","name":"Jane Doe"}]}`)

		added, err := AppendContributor(dir, spec.Person{Handle: "JANE-DOE", Name: "Jane Doe"}, false)
		require.NoError(t, err)
		assert.False(t, added)
		assert.Len(t, readManifest(t, dir).Contributors, 1)
	})

	t.Run("front prepends (reprinter first)", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"},"contributors":[{"handle":"jane-doe","name":"Jane Doe"}]}`)

		added, err := AppendContributor(dir, spec.Person{Handle: "mvanhorn", Name: "Matt Van Horn"}, true)
		require.NoError(t, err)
		assert.True(t, added)

		got := readManifest(t, dir).Contributors
		require.Len(t, got, 2)
		assert.Equal(t, "mvanhorn", got[0].Handle, "front=true must prepend")
		assert.Equal(t, "jane-doe", got[1].Handle)
	})

	t.Run("preserves unknown manifest fields", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"},"x_future_field":{"keep":true}}`)

		_, err := AppendContributor(dir, spec.Person{Handle: "jane-doe", Name: "Jane Doe"}, false)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
		require.NoError(t, err)
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &raw))
		assert.Contains(t, raw, "x_future_field", "unknown fields must survive the append")
		assert.Contains(t, raw, "contributors")
	})
}

// A contributor recorded with only a display name (no handle) must still
// dedupe by name, instead of re-appending on every call.
func TestAppendContributorNameOnlyDedupes(t *testing.T) {
	t.Run("repeat name-only add is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"}}`)

		added, err := AppendContributor(dir, spec.Person{Name: "Jane Doe"}, false)
		require.NoError(t, err)
		assert.True(t, added)

		added, err = AppendContributor(dir, spec.Person{Name: "jane doe"}, false) // case-insensitive
		require.NoError(t, err)
		assert.False(t, added, "a name-only contributor must dedupe by name")
		assert.Len(t, readManifest(t, dir).Contributors, 1)
	})

	t.Run("name-only matching the creator name is skipped", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","creator":{"name":"Solo Dev"}}`)

		added, err := AppendContributor(dir, spec.Person{Name: "Solo Dev"}, false)
		require.NoError(t, err)
		assert.False(t, added, "the creator must not be re-added as a name-only contributor")
		assert.Empty(t, readManifest(t, dir).Contributors)
	})
}
