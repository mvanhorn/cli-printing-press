package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	catalogfs "github.com/mvanhorn/cli-printing-press/v4/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/spf13/cobra"
)

var catalogRegionFilterPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func newCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse the embedded API catalog",
		Example: `  # List all catalog entries
  cli-printing-press catalog list

  # Show a single entry
  cli-printing-press catalog show stripe

  # Search the catalog
  cli-printing-press catalog search auth`,
	}

	cmd.AddCommand(newCatalogListCmd())
	cmd.AddCommand(newCatalogShowCmd())
	cmd.AddCommand(newCatalogSearchCmd())

	return cmd
}

func newCatalogListCmd() *cobra.Command {
	var asJSON bool
	var region string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all catalog entries",
		Example: `  cli-printing-press catalog list
  cli-printing-press catalog list --json
  cli-printing-press catalog list --region NL`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCatalogRegionFilter(region); err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}
			entries, err := catalog.ParseFS(catalogfs.FS)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading catalog: %w", err)}
			}
			entries = filterCatalogEntriesByRegion(entries, region)

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
	cmd.Flags().StringVar(&region, "region", "", "Filter to catalog entries explicitly scoped to a region token (for example NL, EU, or *)")

	return cmd
}

func newCatalogShowCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details for a catalog entry",
		Example: `  cli-printing-press catalog show stripe
  cli-printing-press catalog show stripe --json`,
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

			printCatalogEntryPlainText(*entry)

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func printCatalogEntryPlainText(entry catalog.Entry) {
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
	if entry.BaseURL != "" {
		fmt.Printf("Base URL:       %s\n", entry.BaseURL)
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
	if len(entry.Regions) > 0 {
		fmt.Printf("Regions:        %s\n", strings.Join(entry.Regions, ", "))
	}
	if entry.APILanguage != "" {
		fmt.Printf("API Language:   %s\n", entry.APILanguage)
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
}

func filterCatalogEntriesByRegion(entries []catalog.Entry, region string) []catalog.Entry {
	region = strings.ToUpper(strings.TrimSpace(region))
	if region == "" {
		return entries
	}
	out := make([]catalog.Entry, 0, len(entries))
	for _, entry := range entries {
		if catalogEntryMatchesRegion(entry, region) {
			out = append(out, entry)
		}
	}
	return out
}

func catalogEntryMatchesRegion(entry catalog.Entry, region string) bool {
	for _, candidate := range entry.Regions {
		candidate = strings.ToUpper(strings.TrimSpace(candidate))
		if candidate == region || candidate == "*" {
			return true
		}
	}
	return false
}

func validateCatalogRegionFilter(region string) error {
	region = strings.ToUpper(strings.TrimSpace(region))
	if region == "" || region == "*" || catalogRegionFilterPattern.MatchString(region) {
		return nil
	}
	return fmt.Errorf("--region must be a two-letter region token such as NL, EU, or *")
}

func newCatalogSearchCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search catalog entries by name, description, or category",
		Example: `  cli-printing-press catalog search auth
  cli-printing-press catalog search payments --json`,
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
		strings.Join(e.Regions, " "),
		e.APILanguage,
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}
