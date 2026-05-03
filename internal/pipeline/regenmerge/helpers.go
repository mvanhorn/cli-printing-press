package regenmerge

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// writeFileAtomic writes data to path via a tmp file + rename. Copied from
// internal/pipeline/mcpsync/sync.go (unexported there). Same shape; the rename
// is the atomic operation, so concurrent readers either see the old file or
// the new file, never partial.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temporary %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// validateInputPath rejects raw user-supplied paths containing ".." segments.
// Runs BEFORE filepath.Abs so the segment is detectable. Force overrides for
// unusual sweep workflows where the user provides an explicit out-of-tree
// path.
func validateInputPath(input string, force bool) error {
	if force {
		return nil
	}
	if slices.Contains(strings.Split(input, string(filepath.Separator)), "..") {
		return fmt.Errorf("path %q contains '..' segments; refusing (use --force to override)", input)
	}
	return nil
}

// validatePathAgainstCWD rejects an absolute path that isn't under the
// current working directory's prefix. Mitigates filepath.Join traversal per
// docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md.
func validatePathAgainstCWD(absPath string, force bool) error {
	if force {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving cwd: %w", err)
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("absolutizing cwd: %w", err)
	}
	if !strings.HasPrefix(absPath, cwdAbs+string(filepath.Separator)) && absPath != cwdAbs {
		return fmt.Errorf("path %q is outside the current working directory %q (use --force to override)", absPath, cwdAbs)
	}
	return nil
}

// shouldWalk returns true when the directory is one regen-merge cares about.
// Skips build artifacts, vendor dirs, hidden directories, and obvious
// non-source roots.
func shouldWalkDir(name string) bool {
	switch name {
	case "build", "dist", "vendor", ".git", "node_modules", ".gotmp":
		return false
	}
	return !strings.HasPrefix(name, ".")
}

// shouldClassifyFile returns true for files that participate in classification.
// Includes .go and a small allowlist of root-level generator-owned files.
// spec.yaml / spec.json are generator-owned (downstream tools — mcp-sync,
// dogfood, scorecard — re-parse them at runtime), so source-spec changes
// must propagate via Apply's TEMPLATED-CLEAN path.
func shouldClassifyFile(rel string) bool {
	if strings.HasSuffix(rel, ".go") {
		return true
	}
	switch filepath.Base(rel) {
	case "go.mod", "go.sum", "spec.yaml", "spec.json":
		return true
	}
	return false
}
