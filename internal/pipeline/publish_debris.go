package pipeline

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RemoveUnshippablePackageFiles drops scorecard probe directories and compiled
// binaries from a staged publish tree. Publish force-adds the CLI directory, so
// gitignore cannot keep a leftover inside that tree out of the public library.
func RemoveUnshippablePackageFiles(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if isLiveCheckStagingDirName(d.Name()) {
			if rmErr := removeAllRetry(path); rmErr != nil {
				return rmErr
			}
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		drop, err := isStrayPackageExecutable(path, d)
		if err != nil {
			return err
		}
		if !drop {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}

func isStrayPackageExecutable(path string, d fs.DirEntry) (bool, error) {
	if strings.EqualFold(filepath.Ext(d.Name()), ".exe") {
		return true, nil
	}
	// Java class files share the fat Mach-O magic. They are not probe binaries.
	if strings.EqualFold(filepath.Ext(d.Name()), ".class") {
		return false, nil
	}
	if d.Type()&os.ModeSymlink != 0 {
		return false, nil
	}
	info, err := d.Info()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	return regularFileHasExecutableMagic(path)
}

func regularFileHasExecutableMagic(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = f.Close() }()

	var buf [4]byte
	n, err := f.Read(buf[:])
	if err != nil && err != io.EOF {
		return false, err
	}
	return hasExecutableMagic(buf[:n]), nil
}

func hasExecutableMagic(b []byte) bool {
	if bytes.HasPrefix(b, []byte{0x7f, 'E', 'L', 'F'}) || bytes.HasPrefix(b, []byte{'M', 'Z'}) {
		return true
	}
	if len(b) < 4 {
		return false
	}
	switch binary.BigEndian.Uint32(b) {
	case 0xFEEDFACE, 0xFEEDFACF, 0xCEFAEDFE, 0xCFFAEDFE, 0xCAFEBABE, 0xBEBAFECA:
		return true
	default:
		return false
	}
}
