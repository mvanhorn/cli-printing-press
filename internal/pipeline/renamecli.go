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
//   - Metadata: updates .printing-press.json, manifest.json, and
//     tools-manifest.json to the final public slug/binary names
//
// This function does NOT call RewriteModulePath — that handles import
// paths and is run separately during packaging. RenameCLI handles exactly
// the user-visible references that RewriteModulePath intentionally skips.
func RenameCLI(dir, oldCLIName, newCLIName, _ string) (int, error) {
	if err := validateRenameInputs(oldCLIName, newCLIName); err != nil {
		return 0, err
	}
	oldSlug := naming.TrimCLISuffix(oldCLIName)
	newSlug := naming.TrimCLISuffix(newCLIName)
	oldMCPName := naming.MCP(oldSlug)
	newMCPName := naming.MCP(newSlug)

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

		result := renameCLIContent(string(content), oldCLIName, newCLIName, oldMCPName, newMCPName, oldSlug, newSlug)
		if filepath.Base(path) == ".gitignore" {
			result = anchorRenamedGitignorePatterns(result, newCLIName, newMCPName)
		}
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

	// 2. Update metadata files from the final public slug/binary names.
	manifestPath := filepath.Join(absDir, CLIManifestFilename)
	if manifestData, readErr := os.ReadFile(manifestPath); readErr == nil {
		var m CLIManifest
		if jsonErr := json.Unmarshal(manifestData, &m); jsonErr == nil {
			m.CLIName = newCLIName
			m.APIName = newSlug
			if m.MCPBinary != "" {
				m.MCPBinary = newMCPName
			}
			if writeErr := WriteCLIManifest(absDir, m); writeErr != nil {
				return filesModified, fmt.Errorf("updating manifest: %w", writeErr)
			}
			if writeErr := WriteMCPBManifestFromStruct(absDir, m); writeErr != nil {
				return filesModified, fmt.Errorf("updating MCPB manifest: %w", writeErr)
			}
			filesModified++
		}
	}
	if modified, err := updateToolsManifestAPIName(absDir, newSlug); err != nil {
		return filesModified, err
	} else if modified {
		filesModified++
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

func renameCLIContent(content, oldCLIName, newCLIName, oldMCPName, newMCPName, oldSlug, newSlug string) string {
	result := strings.ReplaceAll(content, oldCLIName, newCLIName)
	result = strings.ReplaceAll(result, oldMCPName, newMCPName)
	result = strings.ReplaceAll(result, "pp-"+oldSlug, "pp-"+newSlug)
	result = strings.ReplaceAll(result, "/"+oldSlug+"/cmd/", "/"+newSlug+"/cmd/")
	return result
}

func anchorRenamedGitignorePatterns(content, cliName, mcpName string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == cliName || line == mcpName {
			lines[i] = "/" + line
		}
	}
	return strings.Join(lines, "\n")
}

func updateToolsManifestAPIName(dir, apiName string) (bool, error) {
	path := filepath.Join(dir, ToolsManifestFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading tools manifest: %w", err)
	}
	var m ToolsManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return false, fmt.Errorf("parsing tools manifest: %w", err)
	}
	if m.APIName == apiName {
		return false, nil
	}
	m.APIName = apiName
	updated, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshaling tools manifest: %w", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return false, fmt.Errorf("writing tools manifest: %w", err)
	}
	return true, nil
}

// shouldRenameFile returns true if a file should be processed during rename.
// Checks extension (.go, .yaml, .yml, .md) and base name (Makefile).
func shouldRenameFile(path string) bool {
	base := filepath.Base(path)
	if base == "Makefile" || base == ".gitignore" {
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
