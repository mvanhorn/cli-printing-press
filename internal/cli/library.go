package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
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
				desc := e.Description
				if desc == "" {
					desc = ""
				}
				fmt.Printf("  %-30s %-20s %s\n", e.CLIName, cat, desc)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
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

		// Only include directories that look like CLIs or have a manifest
		if naming.IsCLIDirName(dirName) || entry.APIName != "" {
			entries = append(entries, entry)
		}
	}

	// Sort by modification time, most recent first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Modified.After(entries[j].Modified)
	})

	return entries, nil
}
