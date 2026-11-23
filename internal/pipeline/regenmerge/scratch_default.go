//go:build !unix

package regenmerge

import (
	"errors"
	"os"
)

func openDirNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !isPlainDir(info) {
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("not a directory")}
	}
	return os.Open(path)
}
