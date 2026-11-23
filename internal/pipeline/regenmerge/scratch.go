package regenmerge

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// bakChildName is the single path element inside the exclusive bak directory
// that receives the published tree. A fixed name cannot escape that directory.
const bakChildName = "original"

// applyScratchHook, when set, runs after both scratch directories exist and
// before the published tree is renamed. Tests observe the paths; production
// leaves it nil.
var applyScratchHook atomic.Pointer[func(stageDir, bakDir string)]

var errScratchNotOwned = errors.New("refusing to remove a directory this run did not create")

// ownedDir is a directory this process created. remove refuses a path that
// no longer names that inode, so a swapped-in directory is not deleted.
type ownedDir struct {
	path string
	info os.FileInfo
}

func mkdirOwned(parent, pattern string) (*ownedDir, error) {
	path, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("statting %s: %w", path, err)
	}
	if !isPlainDir(info) {
		return nil, fmt.Errorf("%s is not a directory created by this run", path)
	}
	return &ownedDir{path: path, info: info}, nil
}

func (d *ownedDir) matches() bool {
	if d == nil || d.info == nil {
		return false
	}
	info, err := os.Lstat(d.path)
	if err != nil || !isPlainDir(info) {
		return false
	}
	return os.SameFile(d.info, info)
}

func (d *ownedDir) remove() error {
	if d == nil {
		return nil
	}
	if !d.matches() {
		return fmt.Errorf("%s: %w", d.path, errScratchNotOwned)
	}
	return os.RemoveAll(d.path)
}

func isPlainDir(info os.FileInfo) bool {
	return info != nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

// publishedDirMode is the mode to restore on the staged tree before it
// becomes the published directory. MkdirTemp creates that tree as 0700.
func publishedDirMode(info os.FileInfo) os.FileMode {
	mode := info.Mode()
	return mode.Perm() | (mode & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky))
}

// scratchPattern is a MkdirTemp pattern. Separators and '*' are flattened so
// a CLI basename cannot escape parent or steer the random suffix.
func scratchPattern(base, kind string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == os.PathSeparator || r == '*' {
			return '-'
		}
		return r
	}, base)
	cleaned = strings.Trim(cleaned, ".")
	if cleaned == "" {
		cleaned = "cli"
	}
	return cleaned + "." + kind
}

func chmodOwned(d *ownedDir, mode os.FileMode) error {
	if d == nil {
		return errors.New("no staging directory")
	}
	if !d.matches() {
		return fmt.Errorf("%s is no longer the directory this run created", d.path)
	}
	f, err := openDirNoFollow(d.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if !d.matches() {
		return fmt.Errorf("%s changed while updating permissions", d.path)
	}
	return nil
}
