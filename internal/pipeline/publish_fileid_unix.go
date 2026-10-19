//go:build unix

package pipeline

import (
	"io/fs"
	"syscall"
)

func fileIdentityOf(_ string, info fs.FileInfo) (fileIdentity, bool) {
	if info == nil || !info.Mode().IsRegular() {
		return fileIdentity{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return fileIdentity{}, false
	}
	return fileIdentity{dev: uint64(stat.Dev), ino: uint64(stat.Ino)}, true
}
