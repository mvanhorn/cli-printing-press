//go:build windows

package pipeline

import (
	"io/fs"
	"syscall"
)

func fileIdentityOf(path string, info fs.FileInfo) (fileIdentity, bool) {
	if info == nil || !info.Mode().IsRegular() {
		return fileIdentity{}, false
	}
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fileIdentity{}, false
	}
	// Share flags so a file already open for the copy can still be identified.
	// FILE_FLAG_OPEN_REPARSE_POINT keeps the identity of this name, not a symlink target.
	h, err := syscall.CreateFile(
		pathp,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fileIdentity{}, false
	}
	defer syscall.CloseHandle(h)
	var out syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &out); err != nil {
		return fileIdentity{}, false
	}
	return fileIdentity{
		dev: uint64(out.VolumeSerialNumber),
		ino: uint64(out.FileIndexHigh)<<32 | uint64(out.FileIndexLow),
	}, true
}
