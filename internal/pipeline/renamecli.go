package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// renameExtensions lists file extensions walked during CLI rename.
// Makefile is handled separately by base-name check in shouldRenameFile.
var renameExtensions = []string{".go", ".yaml", ".yml", ".md"}

// RenameCLI renames all user-visible CLI name references in a staged CLI
// directory. It handles:
//   - Filesystem: outer directory rename to the slug-keyed directory derived
//     from newCLIName, and cmd/oldCLIName/ → cmd/newCLIName/
//   - File content: replaces occurrences of oldCLIName with newCLIName in
//     .go, .yaml, .yml, .md files and Makefiles (skips .manuscripts/)
//   - Manifest: updates cli_name to newCLIName, preserves api_name as
//     originalAPIName
//
// This function does NOT call RewriteModulePath — that handles import
// paths and is run separately during packaging. RenameCLI handles exactly
// the user-visible references that RewriteModulePath intentionally skips.
func RenameCLI(dir, oldCLIName, newCLIName, originalAPIName string) (int, error) {
	if err := validateRenameInputs(oldCLIName, newCLIName); err != nil {
		return 0, err
	}
	oldMCPName := naming.MCP(naming.TrimCLISuffix(oldCLIName))
	newMCPName := naming.MCP(naming.TrimCLISuffix(newCLIName))

	// Path traversal protection: verify the directory and new name resolve
	// within the expected parent.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return 0, fmt.Errorf("resolving directory: %w", err)
	}
	parent := filepath.Dir(absDir)
	newDir := filepath.Join(parent, naming.LibraryDirName(newCLIName))
	absNew, err := filepath.Abs(newDir)
	if err != nil {
		return 0, fmt.Errorf("resolving new directory: %w", err)
	}
	if !strings.HasPrefix(absNew, parent+string(filepath.Separator)) {
		return 0, fmt.Errorf("new CLI name resolves outside parent directory: %s", absNew)
	}

	// Verify old directory exists and base matches old name.
	// After slug-keyed directories, the dir base may be the slug (e.g., "dub")
	// while oldCLIName is "dub-pp-cli". Accept either.
	dirBase := filepath.Base(absDir)
	if dirBase != oldCLIName && dirBase != naming.LibraryDirName(oldCLIName) {
		return 0, fmt.Errorf("directory base %q does not match old CLI name %q", dirBase, oldCLIName)
	}

	filesModified := 0

	// 1. Replace file contents (walk before directory renames so paths are stable).
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip .manuscripts subtree — archival provenance records
			// should preserve original names.
			if d.Name() == ".manuscripts" {
				return filepath.SkipDir
			}
			return nil
		}

		if !shouldRenameFile(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		result := strings.ReplaceAll(string(content), oldCLIName, newCLIName)
		result = strings.ReplaceAll(result, oldMCPName, newMCPName)
		if result == string(content) {
			return nil
		}

		if err := os.WriteFile(path, []byte(result), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		filesModified++
		return nil
	})
	if err != nil {
		return filesModified, fmt.Errorf("walking directory: %w", err)
	}

	// 2. Update manifest: set cli_name, preserve api_name.
	manifestPath := filepath.Join(absDir, CLIManifestFilename)
	if manifestData, readErr := os.ReadFile(manifestPath); readErr == nil {
		var m CLIManifest
		if jsonErr := json.Unmarshal(manifestData, &m); jsonErr == nil {
			m.CLIName = newCLIName
			m.APIName = originalAPIName
			if m.MCPBinary != "" {
				m.MCPBinary = newMCPName
			}
			if writeErr := WriteCLIManifest(absDir, m); writeErr != nil {
				return filesModified, fmt.Errorf("updating manifest: %w", writeErr)
			}
			filesModified++
		}
	}

	// 3. Rename cmd/ subdirectory if it exists.
	oldCmdDir := filepath.Join(absDir, "cmd", oldCLIName)
	newCmdDir := filepath.Join(absDir, "cmd", newCLIName)
	if _, err := os.Stat(oldCmdDir); err == nil {
		if err := os.Rename(oldCmdDir, newCmdDir); err != nil {
			return filesModified, fmt.Errorf("renaming cmd directory: %w", err)
		}
	}

	// 3b. Rename cmd/ MCP subdirectory if it exists.
	oldMCPDir := filepath.Join(absDir, "cmd", oldMCPName)
	newMCPDir := filepath.Join(absDir, "cmd", newMCPName)
	if _, err := os.Stat(oldMCPDir); err == nil {
		if err := os.Rename(oldMCPDir, newMCPDir); err != nil {
			return filesModified, fmt.Errorf("renaming MCP cmd directory: %w", err)
		}
	}

	// 4. Rename outer directory last (changes the path for the caller).
	if err := os.Rename(absDir, absNew); err != nil {
		return filesModified, fmt.Errorf("renaming CLI directory: %w", err)
	}

	return filesModified, nil
}

// shouldRenameFile returns true if a file should be processed during rename.
// Checks extension (.go, .yaml, .yml, .md) and base name (Makefile).
func shouldRenameFile(path string) bool {
	base := filepath.Base(path)
	if base == "Makefile" {
		return true
	}
	for _, ext := range renameExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// validateRenameInputs checks that both CLI names are valid and safe.
func validateRenameInputs(oldName, newName string) error {
	if oldName == newName {
		return fmt.Errorf("old and new CLI names are identical: %q", oldName)
	}

	for _, name := range []string{oldName, newName} {
		if !naming.IsCLIDirName(name) {
			return fmt.Errorf("invalid CLI name (must end with %s): %q", naming.CurrentCLISuffix, name)
		}
		// Path traversal protection: reject dangerous characters.
		if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
			return fmt.Errorf("CLI name contains path traversal characters: %q", name)
		}
	}

	return nil
}
