package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
)

// LibraryEntry represents a CLI in the local library for listing purposes.
type LibraryEntry struct {
	CLIName      string    `json:"cli_name"`
	Dir          string    `json:"dir"`
	APIName      string    `json:"api_name,omitempty"`
	Category     string    `json:"category,omitempty"`
	CatalogEntry string    `json:"catalog_entry,omitempty"`
	Description  string    `json:"description,omitempty"`
	Modified     time.Time `json:"modified"`
}

func newLibraryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage CLIs in the local library",
		Example: `  # List all CLIs in the library
  printing-press library list

  # List as JSON for tooling
  printing-press library list --json`,
	}

	cmd.AddCommand(newLibraryListCmd())
	cmd.AddCommand(newLibraryMigrateCmd())

	return cmd
}

func newLibraryListCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all CLIs in the local library",
		Example: `  printing-press library list
  printing-press library list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := scanLibrary()
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("scanning library: %w", err)}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			if len(entries) == 0 {
				fmt.Fprintln(os.Stderr, "No CLIs found in library.")
				return nil
			}

			fmt.Fprintf(os.Stderr, "Found %d CLIs in library:\n\n", len(entries))
			for _, e := range entries {
				cat := e.Category
				if cat == "" {
					cat = "-"
				}
				fmt.Printf("  %-30s %-20s %s\n", e.CLIName, cat, e.Description)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newLibraryMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Rename legacy -pp-cli library directories to slug-keyed names",
		Long: `Scans ~/printing-press/library/ for directories using the old naming
convention ({slug}-pp-cli) and renames them to the new slug-keyed format ({slug}).

Examples:
  dub-pp-cli/     → dub/
  dub-pp-cli-2/   → dub-2/
  cal-com-pp-cli/ → cal-com/

Directories that already have the target name are skipped (idempotent).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			libRoot := pipeline.PublishedLibraryRoot()
			renamed, skipped, err := migrateLibrary(libRoot)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("migrating library: %w", err)}
			}
			for _, msg := range renamed {
				fmt.Fprintln(cmd.OutOrStdout(), msg)
			}
			for _, msg := range skipped {
				fmt.Fprintln(cmd.ErrOrStderr(), msg)
			}
			if len(renamed) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "Nothing to migrate.")
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "\nMigrated %d directories.\n", len(renamed))
			}
			return nil
		},
	}
}

// migrateLibraryDirName converts a legacy CLI directory name to its slug-keyed
// equivalent by removing the "-pp-cli" infix. Unlike TrimCLISuffix, this
// preserves numeric rerun suffixes: "dub-pp-cli-2" → "dub-2", not "dub".
func migrateLibraryDirName(name string) string {
	return naming.LibraryDirName(name)
}

// migrateLibrary scans libRoot for directories matching the old IsCLIDirName()
// pattern and renames them to their slug-keyed equivalents. Returns lists of
// renamed and skipped messages for display.
func migrateLibrary(libRoot string) (renamed []string, skipped []string, err error) {
	dirEntries, err := os.ReadDir(libRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading library: %w", err)
	}

	absRoot, err := filepath.Abs(libRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving library root: %w", err)
	}

	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		dirName := de.Name()

		// Only migrate directories that match the old CLI naming convention
		if !naming.IsCLIDirName(dirName) {
			continue
		}

		slugName := migrateLibraryDirName(dirName)
		if slugName == "" || slugName == dirName {
			continue
		}

		targetPath := filepath.Join(libRoot, slugName)

		// Layer 2 containment: verify derived target resolves under library root
		absTarget, err := filepath.Abs(targetPath)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("skip %s: cannot resolve target path: %v", dirName, err))
			continue
		}
		if !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
			skipped = append(skipped, fmt.Sprintf("skip %s: target %q escapes library root", dirName, slugName))
			continue
		}

		// Skip if target already exists (idempotent)
		if _, err := os.Stat(targetPath); err == nil {
			skipped = append(skipped, fmt.Sprintf("skip %s: target %s already exists", dirName, slugName))
			continue
		}

		srcPath := filepath.Join(libRoot, dirName)
		if err := os.Rename(srcPath, targetPath); err != nil {
			return renamed, skipped, fmt.Errorf("renaming %s to %s: %w", dirName, slugName, err)
		}
		renamed = append(renamed, fmt.Sprintf("renamed %s → %s", dirName, slugName))
	}

	return renamed, skipped, nil
}

func scanLibrary() ([]LibraryEntry, error) {
	libRoot := pipeline.PublishedLibraryRoot()

	dirEntries, err := os.ReadDir(libRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []LibraryEntry{}, nil
		}
		return nil, fmt.Errorf("reading library: %w", err)
	}

	var entries []LibraryEntry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		dirName := de.Name()

		// Accept directories that look like CLI names or contain a manifest
		dirPath := filepath.Join(libRoot, dirName)
		manifestPath := filepath.Join(dirPath, pipeline.CLIManifestFilename)

		entry := LibraryEntry{
			CLIName: dirName,
			Dir:     dirPath,
		}

		// Get modification time
		if info, err := de.Info(); err == nil {
			entry.Modified = info.ModTime()
		}

		// Try to read the manifest for metadata
		if data, err := os.ReadFile(manifestPath); err == nil {
			var m pipeline.CLIManifest
			if json.Unmarshal(data, &m) == nil {
				if m.CLIName != "" {
					entry.CLIName = m.CLIName
				}
				entry.APIName = m.APIName
				entry.Category = m.Category
				entry.CatalogEntry = m.CatalogEntry
				entry.Description = m.Description
			}
		}

		// Only include directories with a valid manifest or a valid library dir name
		if entry.APIName != "" || naming.IsValidLibraryDirName(dirName) {
			entries = append(entries, entry)
		}
	}

	// Sort by modification time, most recent first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Modified.After(entries[j].Modified)
	})

	return entries, nil
}
