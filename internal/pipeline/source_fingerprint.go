package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceFingerprint is the source snapshot recorded with a Phase 5 proof.
// The per-file hashes let publish report the paths that drifted; Digest is the
// compact binding used for the unchanged-tree fast path.
type SourceFingerprint struct {
	Digest string
	Files  map[string]string
}

// CaptureSourceFingerprint records the relevant source files under root.
func CaptureSourceFingerprint(root string) (SourceFingerprint, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return SourceFingerprint{}, fmt.Errorf("CLI source directory is empty")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return SourceFingerprint{}, fmt.Errorf("stat CLI source directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return SourceFingerprint{}, fmt.Errorf("CLI source path must not be a symlink: %s", root)
	}
	if !info.IsDir() {
		return SourceFingerprint{}, fmt.Errorf("CLI source path is not a directory: %s", root)
	}

	files := make(map[string]string)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && sourceFingerprintSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve source path %q: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		if !isSourceFingerprintFile(rel) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read source file %q: %w", rel, err)
		}
		digest := sha256.Sum256(data)
		files[rel] = hex.EncodeToString(digest[:])
		return nil
	})
	if err != nil {
		return SourceFingerprint{}, fmt.Errorf("walk CLI source directory: %w", err)
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, path := range paths {
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(files[path]))
		_, _ = hash.Write([]byte{'\n'})
	}

	return SourceFingerprint{
		Digest: hex.EncodeToString(hash.Sum(nil)),
		Files:  files,
	}, nil
}

func sourceFingerprintSkipDir(name string) bool {
	switch name {
	case ".git", ".manuscripts", ".printing-press":
		return true
	default:
		return false
	}
}

func isSourceFingerprintFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(strings.ToLower(path), ".go") {
		return true
	}
	switch base {
	case "go.mod", "go.sum", "spec.json", "spec.yaml", "spec.yml":
		return true
	default:
		return false
	}
}

func changedSourceFingerprintFiles(expected, current map[string]string) []string {
	seen := make(map[string]struct{}, len(expected)+len(current))
	for path := range expected {
		seen[path] = struct{}{}
	}
	for path := range current {
		seen[path] = struct{}{}
	}

	changed := make([]string, 0)
	for path := range seen {
		if expected[path] != current[path] {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}
