package pipeline

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // registers the pure-Go driver used for VACUUM INTO snapshots
)

// liveCheckStoreCopyMaxBytes bounds bytes staged for one sample. Staging runs
// before the command timeout, so an uncapped tree can fill the disk before
// the probe's own limit applies.
const liveCheckStoreCopyMaxBytes int64 = 256 << 20

const sqliteDatabaseHeader = "SQLite format 3\x00"

// liveCheckSample is one sample's private cwd and home. Writes stay here
// and are never copied back, and samples do not share a database file.
type liveCheckSample struct {
	dir     string
	args    []string
	env     []string
	cleanup func()
}

// liveCheckCopyBudget counts bytes that one sample is allowed to stage.
// A nil budget is unlimited so unit tests can exercise staging without a cap.
type liveCheckCopyBudget struct {
	max  int64
	used int64
}

func (b *liveCheckCopyBudget) consume(n int64) error {
	if b == nil {
		return nil
	}
	if n < 0 {
		return fmt.Errorf("store copy size is negative")
	}
	if b.used > b.max || n > b.max-b.used {
		return fmt.Errorf("store copy exceeds %d MiB", b.max>>20)
	}
	b.used += n
	return nil
}

func prepareLiveCheckSample(cliDir string, args []string) (*liveCheckSample, error) {
	return prepareLiveCheckSampleBudget(cliDir, args, liveCheckStoreCopyMaxBytes)
}

func prepareLiveCheckSampleBudget(cliDir string, args []string, maxBytes int64) (*liveCheckSample, error) {
	root, err := os.MkdirTemp("", "printing-press-live-check-sample-")
	if err != nil {
		return nil, fmt.Errorf("isolating sample store: %w", err)
	}
	sample := &liveCheckSample{
		dir:     filepath.Join(root, "cwd"),
		cleanup: func() { _ = os.RemoveAll(root) },
	}
	fail := func(err error) (*liveCheckSample, error) {
		sample.cleanup()
		return nil, err
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(sample.dir, 0o755); err != nil {
		return fail(fmt.Errorf("isolating sample store: %w", err))
	}
	for _, sub := range []string{".config", ".cache", filepath.Join(".local", "share"), filepath.Join(".local", "state")} {
		if err := os.MkdirAll(filepath.Join(home, sub), 0o700); err != nil {
			return fail(fmt.Errorf("isolating sample store: %w", err))
		}
	}
	budget := &liveCheckCopyBudget{max: maxBytes}
	// --home outranks the scratch data dir in the printed CLI, so both the
	// database flag and the home flag have to point into this sample.
	rewritten, err := rewriteLiveCheckDBArgs(cliDir, sample.dir, args, budget)
	if err != nil {
		return fail(err)
	}
	rewritten, err = rewriteLiveCheckHomeArgs(cliDir, sample.dir, rewritten, budget)
	if err != nil {
		return fail(err)
	}
	sample.args = rewritten
	if err := copyCLIDirFixtures(args, 0, cliDir, sample.dir); err != nil {
		return fail(fmt.Errorf("isolating sample store: %w", err))
	}
	if err := copyLiveCheckCWDStores(cliDir, sample.dir, budget); err != nil {
		return fail(err)
	}
	if err := seedLiveCheckDataDirs(cliDir, home, budget); err != nil {
		return fail(err)
	}
	env := subprocessEnv()
	if env == nil {
		env = make([]string, 0)
	}
	for key, value := range scopedConfigHomeOverrides(home) {
		env = putEnv(env, key, value)
	}
	sample.env = env
	return sample, nil
}

func rewriteLiveCheckDBArgs(cliDir, scratch string, args []string, budget *liveCheckCopyBudget) ([]string, error) {
	return rewriteLiveCheckPathArgs(cliDir, args, "--db", func(src string, n int) (string, error) {
		dst := filepath.Join(scratch, "isolated-db", fmt.Sprintf("%d-%s", n, filepath.Base(src)))
		if err := stageSQLiteFile(src, dst, budget); err != nil {
			return "", err
		}
		return dst, nil
	})
}

func rewriteLiveCheckHomeArgs(cliDir, scratch string, args []string, budget *liveCheckCopyBudget) ([]string, error) {
	return rewriteLiveCheckPathArgs(cliDir, args, "--home", func(src string, n int) (string, error) {
		dst := filepath.Join(scratch, "isolated-home", fmt.Sprintf("%d", n))
		if err := stageLiveCheckHome(src, dst, budget); err != nil {
			return "", err
		}
		return dst, nil
	})
}

func rewriteLiveCheckPathArgs(cliDir string, args []string, flag string, stage func(src string, n int) (string, error)) ([]string, error) {
	out := append([]string(nil), args...)
	n := 0
	for i := 0; i < len(out); i++ {
		if out[i] == "--" {
			break
		}
		raw, inline, ok := liveCheckFlagArg(out, i, flag)
		if !ok {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		src := raw
		if !filepath.IsAbs(src) {
			src = filepath.Join(cliDir, src)
		}
		src = filepath.Clean(src)
		dst, err := stage(src, n)
		if err != nil {
			return nil, fmt.Errorf("isolating sample store: %w", err)
		}
		n++
		if inline {
			out[i] = flag + "=" + dst
		} else {
			out[i+1] = dst
			i++
		}
	}
	return out, nil
}

func liveCheckFlagArg(args []string, i int, flag string) (value string, inline bool, ok bool) {
	arg := args[i]
	if arg == flag {
		if i+1 >= len(args) {
			return "", false, false
		}
		return args[i+1], false, true
	}
	value, found := strings.CutPrefix(arg, flag+"=")
	if !found || value == "" {
		return "", false, false
	}
	return value, true, true
}

func stageLiveCheckHome(src, dst string, budget *liveCheckCopyBudget) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			// The sample still needs a data dir to write into. Do not create src.
			return os.MkdirAll(filepath.Join(dst, "data"), 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	return copyLiveCheckSQLiteDir(filepath.Join(src, "data"), filepath.Join(dst, "data"), budget)
}

func copyLiveCheckCWDStores(cliDir, scratch string, budget *liveCheckCopyBudget) error {
	if err := stageMatchingSQLiteEntries(cliDir, scratch, isLiveCheckDefaultStoreName, budget); err != nil {
		return fmt.Errorf("isolating sample store: %w", err)
	}
	return nil
}

func seedLiveCheckDataDirs(cliDir, home string, budget *liveCheckCopyBudget) error {
	for _, name := range findCLINames(cliDir) {
		dst := filepath.Join(home, ".local", "share", name)
		src := liveDogfoodOperatorDataDir(name)
		if err := copyLiveCheckSQLiteDir(src, dst, budget); err != nil {
			return fmt.Errorf("isolating sample store: %w", err)
		}
	}
	return nil
}

func copyLiveCheckSQLiteDir(src, dst string, budget *liveCheckCopyBudget) error {
	if strings.TrimSpace(src) == "" {
		return os.MkdirAll(dst, 0o755)
	}
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dst, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return stageMatchingSQLiteEntries(src, dst, isLiveCheckSQLiteName, budget)
}

func stageMatchingSQLiteEntries(src, dst string, match func(string) bool, budget *liveCheckCopyBudget) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !match(name) || liveCheckMainOwnsSidecar(src, name) {
			continue
		}
		if err := stageSQLiteFile(filepath.Join(src, name), filepath.Join(dst, name), budget); err != nil {
			return err
		}
	}
	return nil
}

// liveCheckMainOwnsSidecar reports whether name is a wal, shm, or journal
// whose main database is a regular file. That main file's staging copies a
// non-SQLite sidecar itself. A SQLite snapshot must not gain the live wal:
// SQLite would apply a mismatched generation and report the copy malformed.
func liveCheckMainOwnsSidecar(dir, name string) bool {
	main, ok := liveCheckSidecarMain(name)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, main))
	return err == nil && info.Mode().IsRegular()
}

func liveCheckSidecarMain(name string) (string, bool) {
	switch {
	case strings.HasSuffix(name, "-wal"):
		return strings.TrimSuffix(name, "-wal"), true
	case strings.HasSuffix(name, "-shm"):
		return strings.TrimSuffix(name, "-shm"), true
	case strings.HasSuffix(name, "-journal"):
		return strings.TrimSuffix(name, "-journal"), true
	default:
		return "", false
	}
}

func stageSQLiteFile(src, dst string, budget *liveCheckCopyBudget) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(filepath.Dir(dst), 0o755)
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	if hasSQLiteHeader(src) {
		return snapshotSQLiteFile(src, dst, info, budget)
	}
	size := info.Size()
	type sidecar struct {
		src  string
		dst  string
		mode os.FileMode
	}
	var sidecars []sidecar
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sideSrc := src + suffix
		sideInfo, err := os.Stat(sideSrc)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !sideInfo.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", sideSrc)
		}
		size += sideInfo.Size()
		sidecars = append(sidecars, sidecar{sideSrc, dst + suffix, sideInfo.Mode().Perm()})
	}
	if err := budget.consume(size); err != nil {
		return err
	}
	if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
		return err
	}
	for _, side := range sidecars {
		if err := copyFile(side.src, side.dst, side.mode); err != nil {
			return err
		}
	}
	return nil
}

// snapshotSQLiteFile copies one committed generation. A byte copy of the
// database and its wal can mix two generations when a writer checkpoints
// between the two reads.
func snapshotSQLiteFile(src, dst string, info os.FileInfo, budget *liveCheckCopyBudget) error {
	size := info.Size()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sideInfo, err := os.Stat(src + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !sideInfo.Mode().IsRegular() {
			continue
		}
		size += sideInfo.Size()
	}
	if err := budget.consume(size); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := vacuumSQLiteInto(src, dst); err != nil {
		removeSQLiteFamily(dst)
		return err
	}
	return nil
}

func vacuumSQLiteInto(src, dst string) error {
	// modernc honors mode and the pragmas only when the DSN has a file: prefix.
	// The read can create a wal and shm beside the source. Leave them. The
	// connection is closed before any later check, so a size or existence test
	// is not a lock: removing those files can delete a commit another
	// connection has already written.
	db, err := openLiveCheckSnapshot(src)
	if err != nil {
		return fmt.Errorf("snapshot sqlite database: %w", err)
	}
	defer func() { _ = db.Close() }()
	query := "VACUUM INTO " + quoteSQLiteString(filepath.ToSlash(dst))
	if _, err := db.Exec(query); err != nil {
		removeSQLiteFamily(dst)
		return fmt.Errorf("snapshot sqlite database: %w", err)
	}
	return nil
}

// mode=ro takes the shared lock or WAL read mark that VACUUM INTO holds for
// its read transaction. immutable=1 skips that lock, so a commit or checkpoint
// can rewrite pages under the copy when the wal is absent or empty.
func sqliteSnapshotDSN(src string) string {
	return sqliteFileURI(src) + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=mmap_size(0)"
}

// immutable=1 is only for a WAL database whose wal has no frames when the
// directory cannot create the wal index. Rollback journals stay on the locking
// open: skipping the shared lock mixes pages. A wal that still has frames
// stays there too: an immutable read would drop commits the CLI can see.
func openLiveCheckSnapshot(src string) (*sql.DB, error) {
	dsn := sqliteSnapshotDSN(src)
	db, err := openLiveCheckSnapshotDSN(dsn)
	if err == nil || !snapshotOpenNeedsImmutableFallback(err) || !snapshotWALHasNoFrames(src) {
		return db, err
	}
	return openLiveCheckSnapshotDSN(dsn + "&immutable=1")
}

func openLiveCheckSnapshotDSN(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// VACUUM INTO has to run on the connection that applied the DSN pragmas.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func snapshotOpenNeedsImmutableFallback(err error) bool {
	if err == nil || snapshotErrorIsBusy(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "malformed") || strings.Contains(msg, "sqlite_corrupt") || strings.Contains(msg, "not a database") {
		return false
	}
	return strings.Contains(msg, "sqlite_cantopen") ||
		strings.Contains(msg, "unable to open") ||
		strings.Contains(msg, "sqlite_readonly") ||
		strings.Contains(msg, "readonly") ||
		strings.Contains(msg, "read-only")
}

func snapshotErrorIsBusy(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "database is locked")
}

// snapshotWALHasNoFrames reports whether an immutable read would still see
// every committed row. Bytes 18 and 19 are the SQLite write and read versions;
// both are 2 only while the file is in WAL mode.
func snapshotWALHasNoFrames(src string) bool {
	if !sqliteHeaderIsWAL(src) {
		return false
	}
	info, err := os.Stat(src + "-wal")
	if err != nil {
		return os.IsNotExist(err)
	}
	return !info.Mode().IsRegular() || info.Size() == 0
}

func sqliteHeaderIsWAL(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var hdr [20]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	return string(hdr[:len(sqliteDatabaseHeader)]) == sqliteDatabaseHeader && hdr[18] == 2 && hdr[19] == 2
}

// SQLite ends a file: filename at a raw # or ? and decodes %HH, so a path
// such as client#1 or data%2E.db would open a different file. / and : stay
// literal so separators and a Windows drive letter do not depend on a
// decoder that accepts %2F. file:// is avoided because that form is parsed
// as an authority.
func sqliteFileURI(path string) string {
	slash := filepath.ToSlash(path)
	var b strings.Builder
	b.Grow(len("file:") + len(slash))
	b.WriteString("file:")
	for i := 0; i < len(slash); i++ {
		c := slash[i]
		if sqliteURIPathUnescaped(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func sqliteURIPathUnescaped(c byte) bool {
	switch c {
	case '-', '_', '.', '~', '/', ':',
		'!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', '@':
		return true
	default:
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
	}
}

func quoteSQLiteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func removeSQLiteFamily(path string) {
	_ = os.Remove(path)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}

func hasSQLiteHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, len(sqliteDatabaseHeader))
	n, err := io.ReadFull(f, buf)
	if err != nil || n != len(sqliteDatabaseHeader) {
		return false
	}
	return string(buf) == sqliteDatabaseHeader
}

func isLiveCheckDefaultStoreName(name string) bool {
	stem := name
	if main, ok := liveCheckSidecarMain(name); ok {
		stem = main
	}
	if !strings.HasSuffix(stem, ".db") {
		return false
	}
	base := strings.TrimSuffix(stem, ".db")
	return base == "data" || strings.HasPrefix(base, "data-")
}

func isLiveCheckSQLiteName(name string) bool {
	return strings.HasSuffix(name, ".db") ||
		strings.HasSuffix(name, ".db-wal") ||
		strings.HasSuffix(name, ".db-shm") ||
		strings.HasSuffix(name, ".db-journal")
}
