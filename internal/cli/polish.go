package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
)

func newPolishCmd() *cobra.Command {
	var dir string
	var removeDeadCode bool
	var asJSON bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "polish",
		Short: "Deterministic post-generation quality fixes",
		Long:  "Run deterministic quality fixes on a generated CLI. Unlike LLM instructions, these are mechanical transformations that produce the same result every time.",
		Example: `  # Preview dead code that would be removed
  printing-press polish --remove-dead-code --dir ./steam-web-pp-cli --dry-run

  # Remove dead functions and verify build
  printing-press polish --remove-dead-code --dir ./steam-web-pp-cli

  # JSON output for programmatic use
  printing-press polish --remove-dead-code --dir ./steam-web-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !removeDeadCode {
				return fmt.Errorf("specify at least one polish action (e.g., --remove-dead-code)")
			}

			if removeDeadCode {
				result, err := pipeline.RemoveDeadCode(dir, dryRun)
				if err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("polish: %w", err)}
				}

				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}

				printPolishResult(result)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the generated CLI directory (required)")
	cmd.Flags().BoolVar(&removeDeadCode, "remove-dead-code", false, "Remove dead functions identified by dogfood")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be removed without modifying files")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func printPolishResult(r *pipeline.PolishResult) {
	name := filepath.Base(r.Dir)

	if r.DryRun {
		fmt.Printf("Polish (dry run): %s\n", name)
	} else {
		fmt.Printf("Polish: %s\n", name)
	}
	fmt.Println("================================")

	if len(r.DeadFunctions) == 0 {
		fmt.Println("\nNo dead functions found. Nothing to remove.")
		return
	}

	fmt.Printf("\nDead functions found: %d\n", len(r.DeadFunctions))
	for _, fn := range r.DeadFunctions {
		fmt.Printf("  - %s\n", fn)
	}

	if r.DryRun {
		fmt.Println("\nDry run — no files modified.")
		return
	}

	if len(r.Removed) > 0 {
		fmt.Printf("\nRemoved: %d functions\n", len(r.Removed))
		for _, fn := range r.Removed {
			fmt.Printf("  - %s\n", fn)
		}
	}

	if len(r.Restored) > 0 {
		fmt.Printf("\nRestored (build failed after removal): %d functions\n", len(r.Restored))
		for _, fn := range r.Restored {
			fmt.Printf("  - %s\n", fn)
		}
		fmt.Printf("\nBuild error:\n%s\n", r.BuildError)
	}

	if r.BuildVerified {
		fmt.Println("\nBuild verified: PASS")
	}

	fmt.Printf("\nVerdict: ")
	if len(r.Removed) > 0 && r.BuildVerified {
		fmt.Printf("CLEANED (%d dead functions removed)\n", len(r.Removed))
	} else if len(r.Restored) > 0 {
		fmt.Println("REVERTED (build failed, all changes restored)")
	} else {
		fmt.Println("CLEAN (no dead code)")
	}
}
