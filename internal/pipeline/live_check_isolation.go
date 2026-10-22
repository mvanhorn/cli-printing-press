package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// liveCheckSample is one sample's private cwd and home. Writes stay here
// and are never copied back, and samples do not share a database file.
type liveCheckSample struct {
	dir     string
	args    []string
	env     []string
	cleanup func()
}

func prepareLiveCheckSample(cliDir string, args []string) (*liveCheckSample, error) {
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
	rewritten, err := rewriteLiveCheckDBArgs(cliDir, sample.dir, args)
	if err != nil {
		return fail(err)
	}
	sample.args = rewritten
	if err := copyCLIDirFixtures(args, 0, cliDir, sample.dir); err != nil {
		return fail(fmt.Errorf("isolating sample store: %w", err))
	}
	if err := copyLiveCheckCWDStores(cliDir, sample.dir); err != nil {
		return fail(err)
	}
	if err := seedLiveCheckDataDirs(cliDir, home); err != nil {
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

func rewriteLiveCheckDBArgs(cliDir, scratch string, args []string) ([]string, error) {
	out := append([]string(nil), args...)
	n := 0
	for i := 0; i < len(out); i++ {
		if out[i] == "--" {
			break
		}
		raw, inline, ok := liveCheckDBArg(out, i)
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
		dst := filepath.Join(scratch, "isolated-db", fmt.Sprintf("%d-%s", n, filepath.Base(src)))
		n++
		if err := stageSQLiteFile(src, dst); err != nil {
			return nil, fmt.Errorf("isolating sample store: %w", err)
		}
		if inline {
			out[i] = "--db=" + dst
		} else {
			out[i+1] = dst
			i++
		}
	}
	return out, nil
}

func liveCheckDBArg(args []string, i int) (value string, inline bool, ok bool) {
	arg := args[i]
	if arg == "--db" {
		if i+1 >= len(args) {
			return "", false, false
		}
		return args[i+1], false, true
	}
	value, found := strings.CutPrefix(arg, "--db=")
	if !found || value == "" {
		return "", false, false
	}
	return value, true, true
}

func copyLiveCheckCWDStores(cliDir, scratch string) error {
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return fmt.Errorf("isolating sample store: %w", err)
	}
	for _, entry := range entries {
		if !isLiveCheckDefaultStoreName(entry.Name()) {
			continue
		}
		src := filepath.Join(cliDir, entry.Name())
		dst := filepath.Join(scratch, entry.Name())
		if err := stageSQLiteFile(src, dst); err != nil {
			return fmt.Errorf("isolating sample store: %w", err)
		}
	}
	return nil
}

func seedLiveCheckDataDirs(cliDir, home string) error {
	for _, name := range findCLINames(cliDir) {
		dst := filepath.Join(home, ".local", "share", name)
		src := liveDogfoodOperatorDataDir(name)
		if err := copyLiveCheckSQLiteDir(src, dst); err != nil {
			return fmt.Errorf("isolating sample store: %w", err)
		}
	}
	return nil
}

func copyLiveCheckSQLiteDir(src, dst string) error {
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
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !isLiveCheckSQLiteName(entry.Name()) {
			continue
		}
		if err := stageSQLiteFile(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func stageSQLiteFile(src, dst string) error {
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
	if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
		return err
	}
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
		if err := copyFile(sideSrc, dst+suffix, sideInfo.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func isLiveCheckDefaultStoreName(name string) bool {
	stem := name
	switch {
	case strings.HasSuffix(name, "-wal"):
		stem = strings.TrimSuffix(name, "-wal")
	case strings.HasSuffix(name, "-shm"):
		stem = strings.TrimSuffix(name, "-shm")
	case strings.HasSuffix(name, "-journal"):
		stem = strings.TrimSuffix(name, "-journal")
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
