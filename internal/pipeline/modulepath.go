package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rewriteExtensions lists file extensions that may contain Go import paths
// or module-path references (e.g., goreleaser ldflags).
var rewriteExtensions = []string{".go", ".yaml", ".yml"}

// RewriteModulePath replaces the Go module path in a CLI directory.
// It rewrites the module declaration in go.mod and import paths
// (oldPath/internal/...) in .go and .yaml files from oldPath to newPath.
//
// Only import-style references (oldPath + "/internal/") are replaced in
// source files. Bare CLI name occurrences in command strings, User-Agent
// headers, and config paths are intentionally left untouched.
func RewriteModulePath(dir, oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}

	// Rewrite go.mod module line
	gomodPath := filepath.Join(dir, "go.mod")
	gomod, err := os.ReadFile(gomodPath)
	if err != nil {
		return fmt.Errorf("reading go.mod: %w", err)
	}

	oldModule := "module " + oldPath
	newModule := "module " + newPath
	updated := strings.Replace(string(gomod), oldModule, newModule, 1)
	if updated == string(gomod) {
		return fmt.Errorf("go.mod does not contain expected module path %q", oldPath)
	}
	if err := os.WriteFile(gomodPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing go.mod: %w", err)
	}

	// Only replace import-path references: oldPath/internal/...
	// This avoids corrupting command Use strings, User-Agent headers,
	// config paths, and other runtime literals that contain the CLI name.
	oldImport := oldPath + "/internal/"
	newImport := newPath + "/internal/"

	return filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		if !hasRewriteExtension(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		replaced := strings.ReplaceAll(string(content), oldImport, newImport)
		if replaced == string(content) {
			return nil // no changes needed
		}

		return os.WriteFile(path, []byte(replaced), 0o644)
	})
}

func hasRewriteExtension(path string) bool {
	for _, ext := range rewriteExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
