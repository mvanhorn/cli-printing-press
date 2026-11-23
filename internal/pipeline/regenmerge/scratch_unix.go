//go:build unix

package regenmerge

import (
	"os"
	"syscall"
)

// openDirNoFollow opens path itself. O_NOFOLLOW fails if a symlink was swapped
// onto the path; O_DIRECTORY fails if the path is not a directory. Chmod on
// the returned file uses fchmod, so it does not follow a symlink.
func openDirNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
}
