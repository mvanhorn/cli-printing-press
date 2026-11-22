package artifacts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSQLiteStoreFileName(t *testing.T) {
	for _, name := range []string{
		"listings.db",
		"listings.DB",
		"listings.db-wal",
		"listings.db-shm",
		"listings.db-journal",
		"cache.sqlite",
		"cache.SQLITE",
		"cache.sqlite-wal",
		"cache.sqlite-shm",
		"cache.sqlite-journal",
		"cache.sqlite3",
		"cache.sqlite3-wal",
		"cache.sqlite3-shm",
		"cache.sqlite3-journal",
		".db",
	} {
		assert.Truef(t, IsSQLiteStoreFileName(name), "%s", name)
	}
	for _, name := range []string{
		"notes.db.md",
		"notes.database",
		"review.md",
		"schema.db.txt",
		"db",
		"listings.db.bak",
		"shipcheck.md",
	} {
		assert.Falsef(t, IsSQLiteStoreFileName(name), "%s", name)
	}
}

func TestFileHasSQLiteStoreHeader(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "payload")
	require.NoError(t, os.WriteFile(db, append([]byte("SQLite format 3\x00"), []byte("page")...), 0o644))
	wal := filepath.Join(dir, "notes.md")
	require.NoError(t, os.WriteFile(wal, []byte{0x37, 0x7f, 0x06, 0x82, 0x00, 0x01}, 0o644))
	text := filepath.Join(dir, "shipcheck.md")
	require.NoError(t, os.WriteFile(text, []byte("SQLite format 3 is a sentence\n"), 0o644))

	assert.True(t, FileHasSQLiteStoreHeader(db))
	assert.True(t, FileHasSQLiteStoreHeader(wal))
	assert.False(t, FileHasSQLiteStoreHeader(text))
	assert.False(t, FileHasSQLiteStoreHeader(filepath.Join(dir, "missing")))
}
