package artifacts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CleanupOptions controls which generated artifacts to remove.
type CleanupOptions struct {
	RemoveCache              bool
	RemoveRuntimeBinary      bool
	RemoveValidationBinaries bool
	RemoveDogfoodBinaries    bool
	RemoveRecursiveCopies    bool
	RemoveFinderMetadata     bool
}

// CleanupGeneratedCLI removes reproducible artifacts from a generated CLI tree.
func CleanupGeneratedCLI(dir string, opts CleanupOptions) error {
	var errs []error

	if opts.RemoveRuntimeBinary {
		name := filepath.Base(filepath.Clean(dir))
		if name != "." && name != string(filepath.Separator) {
			errs = append(errs, removeFileIfExists(filepath.Join(dir, name)))
			errs = append(errs, removeFileIfExists(filepath.Join(dir, name+".exe")))
		}
	}

	if opts.RemoveValidationBinaries || opts.RemoveDogfoodBinaries {
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			switch {
			case opts.RemoveValidationBinaries && (strings.HasSuffix(name, "-validation") || strings.HasSuffix(name, "-validation.exe")):
				errs = append(errs, removeFileIfExists(filepath.Join(dir, name)))
			case opts.RemoveDogfoodBinaries && (strings.HasSuffix(name, "-dogfood") || strings.HasSuffix(name, "-dogfood.exe")):
				errs = append(errs, removeFileIfExists(filepath.Join(dir, name)))
			}
		}
	}

	if opts.RemoveRecursiveCopies {
		errs = append(errs, removeDirIfExists(filepath.Join(dir, "cmd", "library")))
	}

	if opts.RemoveFinderMetadata {
		errs = append(errs, removeFinderMetadata(dir))
	}

	if opts.RemoveCache {
		errs = append(errs, removeDirIfExists(filepath.Join(dir, ".cache")))
	}

	return errors.Join(errs...)
}

func removeFinderMetadata(root string) error {
	var errs []error
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if d.IsDir() || d.Name() != ".DS_Store" {
			return nil
		}
		errs = append(errs, removeFileIfExists(path))
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	return errors.Join(errs...)
}

func removeFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeDirIfExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(path)
}
