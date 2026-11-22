package artifacts

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// sqliteDatabaseMagic is the 16-byte header at the start of a SQLite database.
// The last byte is NUL, which is why a store never survives a text secret scan.
var sqliteDatabaseMagic = []byte("SQLite format 3\x00")

// sqliteStoreSuffixes are database names and the rollback/WAL sidecars SQLite
// writes beside them.
var sqliteStoreSuffixes = []string{
	".sqlite3-journal",
	".sqlite3-wal",
	".sqlite3-shm",
	".sqlite-journal",
	".sqlite-wal",
	".sqlite-shm",
	".db-journal",
	".db-wal",
	".db-shm",
	".sqlite3",
	".sqlite",
	".db",
}

// IsSQLiteStoreFileName reports whether name is a SQLite database or one of
// its -wal, -shm, or -journal sidecars. The comparison is case-insensitive.
func IsSQLiteStoreFileName(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range sqliteStoreSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// HasSQLiteStoreHeader reports whether prefix begins with a SQLite database
// header or a WAL header (0x377f0682 / 0x377f0683).
func HasSQLiteStoreHeader(prefix []byte) bool {
	if bytes.HasPrefix(prefix, sqliteDatabaseMagic) {
		return true
	}
	return len(prefix) >= 4 && prefix[0] == 0x37 && prefix[1] == 0x7f && prefix[2] == 0x06 && (prefix[3] == 0x82 || prefix[3] == 0x83)
}

// FileHasSQLiteStoreHeader reads the start of path and reports whether it is
// a SQLite database or WAL file. A missing or unreadable file is not one.
func FileHasSQLiteStoreHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, len(sqliteDatabaseMagic))
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false
	}
	return HasSQLiteStoreHeader(buf[:n])
}
