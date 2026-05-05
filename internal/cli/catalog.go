package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	catalogfs "github.com/mvanhorn/cli-printing-press/v3/catalog"
	"github.com/mvanhorn/cli-printing-press/v3/internal/catalog"
	"github.com/spf13/cobra"
)

func newCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse the embedded API catalog",
		Example: `  # List all catalog entries
  printing-press catalog list

  # Show a single entry
  printing-press catalog show stripe

  # Search the catalog
  printing-press catalog search auth`,
	}

	cmd.AddCommand(newCatalogListCmd())
	cmd.AddCommand(newCatalogShowCmd())
	cmd.AddCommand(newCatalogSearchCmd())

	return cmd
}

func newCatalogListCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all catalog entries",
		Example: `  printing-press catalog list
  printing-press catalog list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := catalog.ParseFS(catalogfs.FS)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading catalog: %w", err)}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			// Group by category
			grouped := map[string][]catalog.Entry{}
			for _, e := range entries {
				grouped[e.Category] = append(grouped[e.Category], e)
			}

			categories := make([]string, 0, len(grouped))
			for cat := range grouped {
				categories = append(categories, cat)
			}
			sort.Strings(categories)

			for _, cat := range categories {
				fmt.Printf("%s:\n", cat)
				for _, e := range grouped[cat] {
					fmt.Printf("  %-20s %s\n", e.Name, e.Description)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newCatalogShowCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details for a catalog entry",
		Example: `  printing-press catalog show stripe
  printing-press catalog show stripe --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entry, err := catalog.LookupFS(catalogfs.FS, args[0])
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(entry)
			}

			fmt.Printf("Name:           %s\n", entry.Name)
			fmt.Printf("Display Name:   %s\n", entry.DisplayName)
			fmt.Printf("Description:    %s\n", entry.Description)
			fmt.Printf("Category:       %s\n", entry.Category)
			fmt.Printf("Tier:           %s\n", entry.Tier)
			if entry.IsWrapperOnly() {
				fmt.Printf("Mode:           wrapper-only (no official spec)\n")
			} else {
				fmt.Printf("Spec URL:       %s\n", entry.SpecURL)
				fmt.Printf("Spec Format:    %s\n", entry.SpecFormat)
			}
			if entry.OpenAPIVersion != "" {
				fmt.Printf("OpenAPI:        %s\n", entry.OpenAPIVersion)
			}
			if entry.Homepage != "" {
				fmt.Printf("Homepage:       %s\n", entry.Homepage)
			}
			if entry.SpecSource != "" {
				fmt.Printf("Spec Source:    %s\n", entry.SpecSource)
			}
			if entry.ClientPattern != "" {
				fmt.Printf("Client Pattern: %s\n", entry.ClientPattern)
			}
			if entry.HTTPTransport != "" {
				fmt.Printf("HTTP Transport: %s\n", entry.HTTPTransport)
			}
			if entry.AuthRequired != nil {
				fmt.Printf("Auth Required:  %v\n", *entry.AuthRequired)
			}
			if entry.BearerRefresh.BundleURL != "" {
				fmt.Printf("Bearer Refresh: %s\n", entry.BearerRefresh.BundleURL)
			}
			if entry.Notes != "" {
				fmt.Printf("Notes:          %s\n", entry.Notes)
			}
			if entry.VerifiedDate != "" {
				fmt.Printf("Verified:       %s\n", entry.VerifiedDate)
			}
			if len(entry.WrapperLibraries) > 0 {
				fmt.Printf("\nWrapper Libraries:\n")
				for _, w := range entry.WrapperLibraries {
					fmt.Printf("  - %s (%s, %s)\n", w.Name, w.Language, w.IntegrationMode)
					fmt.Printf("    %s\n", w.URL)
					if w.License != "" {
						fmt.Printf("    License: %s\n", w.License)
					}
					if w.Notes != "" {
						fmt.Printf("    Notes: %s\n", strings.TrimSpace(w.Notes))
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newCatalogSearchCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search catalog entries by name, description, or category",
		Example: `  printing-press catalog search auth
  printing-press catalog search payments --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := catalog.ParseFS(catalogfs.FS)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading catalog: %w", err)}
			}

			query := strings.ToLower(args[0])
			matches := make([]catalog.Entry, 0)
			for _, e := range entries {
				if matchesCatalogQuery(e, query) {
					matches = append(matches, e)
				}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(matches)
			}

			if len(matches) == 0 {
				fmt.Printf("No entries matching %q\n", args[0])
				return nil
			}

			fmt.Printf("Found %d matching entries:\n\n", len(matches))
			for _, e := range matches {
				fmt.Printf("  %-20s %-15s %s\n", e.Name, e.Category, e.Description)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func matchesCatalogQuery(e catalog.Entry, query string) bool {
	fields := []string{
		e.Name,
		e.DisplayName,
		e.Description,
		e.Category,
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}
