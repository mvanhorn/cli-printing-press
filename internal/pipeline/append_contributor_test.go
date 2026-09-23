package pipeline

import (
	"encoding/json"
	"errors"
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

	t.Run("missing creator matching printer becomes creator", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestJSON(t, dir, `{"cli_name":"acme-pp-cli","printer":"qazmataz","printer_name":"qazmataz"}`)

		added, err := AppendContributor(dir, spec.Person{Handle: "qazmataz", Name: "qazmataz"}, false)
		require.NoError(t, err)
		assert.False(t, added, "publisher matching printer must become creator, not a contributor")

		m := readManifest(t, dir)
		require.NotNil(t, m.Creator)
		assert.Equal(t, "qazmataz", m.Creator.Handle)
		assert.Empty(t, m.Contributors)
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
func TestRecordContributorLeavesFilesUnchangedWhenNoticeMissing(t *testing.T) {
	dir := t.TempDir()
	body := `{"cli_name":"acme-pp-cli","creator":{"handle":"trevin-chow","name":"Trevin Chow"}}`
	writeManifestJSON(t, dir, body)
	readme := []byte("Created by [@trevin-chow](https://github.com/trevin-chow) (Trevin Chow).\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), readme, 0o644))

	added, synced, err := RecordContributor(dir, spec.Person{Handle: "jane-doe", Name: "Jane Doe"}, false)
	require.Error(t, err)
	assert.False(t, added)
	assert.False(t, synced)
	assert.Contains(t, err.Error(), "NOTICE is missing")

	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	require.NoError(t, err)
	assert.JSONEq(t, body, string(data))
	got, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, string(readme), string(got))
}

func TestCommitContributorFilesRestoresManifestAndReadmeWhenNoticeWriteFails(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, CLIManifestFilename)
	originalManifest := []byte("{\"cli_name\":\"acme\"}\n")
	plannedManifest := []byte("{\"cli_name\":\"acme\",\"contributors\":[{\"handle\":\"h\"}]}\n")
	require.NoError(t, os.WriteFile(manifestPath, originalManifest, 0o644))
	readmePath := filepath.Join(dir, "README.md")
	noticePath := filepath.Join(dir, "NOTICE")
	require.NoError(t, os.WriteFile(readmePath, []byte("old readme\n"), 0o644))
	require.NoError(t, os.WriteFile(noticePath, []byte("old notice\n"), 0o644))

	write := func(path string, data []byte, perm os.FileMode) error {
		if filepath.Base(path) == "NOTICE" {
			return errors.New("notice write failed")
		}
		return os.WriteFile(path, data, perm)
	}
	err := commitContributorFiles(write, manifestPath, originalManifest, plannedManifest, true,
		plannedSurface{
			path:     readmePath,
			original: []byte("old readme\n"),
			next:     []byte("new readme\n"),
			mode:     0o644,
			changed:  true,
		},
		plannedSurface{
			path:     noticePath,
			original: []byte("old notice\n"),
			next:     []byte("new notice\n"),
			mode:     0o644,
			changed:  true,
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notice write failed")

	gotManifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, string(originalManifest), string(gotManifest))
	gotReadme, err := os.ReadFile(readmePath)
	require.NoError(t, err)
	assert.Equal(t, "old readme\n", string(gotReadme))
	gotNotice, err := os.ReadFile(noticePath)
	require.NoError(t, err)
	assert.Equal(t, "old notice\n", string(gotNotice))
}

func TestCommitContributorFilesRestoresSurfaceAfterPartialWrite(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, CLIManifestFilename)
	originalManifest := []byte("{\"cli_name\":\"acme\"}\n")
	plannedManifest := []byte("{\"cli_name\":\"acme\",\"contributors\":[{\"handle\":\"h\"}]}\n")
	require.NoError(t, os.WriteFile(manifestPath, originalManifest, 0o644))
	readmePath := filepath.Join(dir, "README.md")
	noticePath := filepath.Join(dir, "NOTICE")
	originalReadme := []byte("old readme\n")
	originalNotice := []byte("old notice\n")
	require.NoError(t, os.WriteFile(readmePath, originalReadme, 0o644))
	require.NoError(t, os.WriteFile(noticePath, originalNotice, 0o644))

	t.Run("first surface truncates then errors", func(t *testing.T) {
		require.NoError(t, os.WriteFile(manifestPath, plannedManifest, 0o644))
		require.NoError(t, os.WriteFile(readmePath, originalReadme, 0o644))
		require.NoError(t, os.WriteFile(noticePath, originalNotice, 0o644))

		var readmeWrites int
		write := func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "README.md" {
				readmeWrites++
				if readmeWrites == 1 {
					if err := os.WriteFile(path, []byte("PARTIAL"), perm); err != nil {
						return err
					}
					return errors.New("short write")
				}
			}
			return writeFileAtomic(path, data, perm)
		}
		err := commitContributorFiles(write, manifestPath, originalManifest, plannedManifest, true,
			plannedSurface{
				path:     readmePath,
				original: originalReadme,
				next:     []byte("new readme\n"),
				mode:     0o644,
				changed:  true,
			},
			plannedSurface{
				path:     noticePath,
				original: originalNotice,
				next:     []byte("new notice\n"),
				mode:     0o644,
				changed:  true,
			},
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "short write")
		assert.NotContains(t, err.Error(), "also failed to restore")
		assertContributorFiles(t, manifestPath, originalManifest, readmePath, originalReadme, noticePath, originalNotice)
	})

	t.Run("later surface truncates then errors", func(t *testing.T) {
		require.NoError(t, os.WriteFile(manifestPath, plannedManifest, 0o644))
		require.NoError(t, os.WriteFile(readmePath, []byte("new readme\n"), 0o644))
		require.NoError(t, os.WriteFile(noticePath, originalNotice, 0o644))

		var noticeWrites int
		write := func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == "NOTICE" {
				noticeWrites++
				if noticeWrites == 1 {
					if err := os.WriteFile(path, []byte("PARTIAL"), perm); err != nil {
						return err
					}
					return errors.New("short write")
				}
			}
			return writeFileAtomic(path, data, perm)
		}
		err := commitContributorFiles(write, manifestPath, originalManifest, plannedManifest, true,
			plannedSurface{
				path:     readmePath,
				original: originalReadme,
				next:     []byte("new readme\n"),
				mode:     0o644,
				changed:  true,
			},
			plannedSurface{
				path:     noticePath,
				original: originalNotice,
				next:     []byte("new notice\n"),
				mode:     0o644,
				changed:  true,
			},
		)
		require.Error(t, err)
		assert.ErrorContains(t, err, "short write")
		assert.NotContains(t, err.Error(), "also failed to restore")
		assertContributorFiles(t, manifestPath, originalManifest, readmePath, originalReadme, noticePath, originalNotice)
	})
}

func TestCommitContributorFilesAtomicFailureLeavesDestinationUntouched(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, CLIManifestFilename)
	originalManifest := []byte("{\"cli_name\":\"acme\"}\n")
	plannedManifest := []byte("{\"cli_name\":\"acme\",\"contributors\":[{\"handle\":\"h\"}]}\n")
	require.NoError(t, os.WriteFile(manifestPath, originalManifest, 0o644))
	readmePath := filepath.Join(dir, "README.md")
	originalReadme := []byte("old readme\n")
	require.NoError(t, os.WriteFile(readmePath, originalReadme, 0o644))

	// Replacing a directory fails at rename, after the temp file is written.
	// The destination tree must still be the pre-write contents.
	noticePath := filepath.Join(dir, "NOTICE")
	require.NoError(t, os.Mkdir(noticePath, 0o755))
	sentinel := filepath.Join(noticePath, "keep.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o644))

	err := commitContributorFiles(writeFileAtomic, manifestPath, originalManifest, plannedManifest, true,
		plannedSurface{
			path:     readmePath,
			original: originalReadme,
			next:     []byte("new readme\n"),
			mode:     0o644,
			changed:  true,
		},
		plannedSurface{
			path:     noticePath,
			original: []byte("old notice\n"),
			next:     []byte("new notice\n"),
			mode:     0o644,
			changed:  true,
		},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "also failed to restore")

	gotManifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, string(originalManifest), string(gotManifest))
	gotReadme, err := os.ReadFile(readmePath)
	require.NoError(t, err)
	assert.Equal(t, string(originalReadme), string(gotReadme))
	gotSentinel, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(gotSentinel))
	leftover, err := filepath.Glob(filepath.Join(dir, ".NOTICE.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftover)
	leftover, err = filepath.Glob(filepath.Join(dir, ".README.md.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftover)
}

func assertContributorFiles(t *testing.T, manifestPath string, manifest []byte, readmePath string, readme []byte, noticePath string, notice []byte) {
	t.Helper()
	gotManifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, string(manifest), string(gotManifest))
	gotReadme, err := os.ReadFile(readmePath)
	require.NoError(t, err)
	assert.Equal(t, string(readme), string(gotReadme))
	gotNotice, err := os.ReadFile(noticePath)
	require.NoError(t, err)
	assert.Equal(t, string(notice), string(gotNotice))
}

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
