package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mvanhorn/cli-printing-press/v4/internal/patch"
	"github.com/spf13/cobra"
)

func newPatchCmd() *cobra.Command {
	var (
		dryRun    bool
		force     bool
		skipBuild bool
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "patch <cli-dir>",
		Short: "Apply PR #218 features (profile/deliver/feedback) to an already-published CLI without reprint",
		Long: `Patch AST-injects --profile / --deliver / feedback wiring into an
already-generated CLI's root.go and drops in the three companion files.
It does not read the source spec, does not touch per-endpoint command
files, and does not change the CLI's module path — novel and synthetic
commands are preserved.

See docs/plans/2026-04-18-001-feat-patch-library-clis-v2-plan.md.`,
		Example: `  # Dry-run against a library CLI
  printing-press patch ~/Code/printing-press-library/library/productivity/cal-com --dry-run

  # Apply (writes root.go + creates profile.go/deliver.go/feedback.go)
  printing-press patch ~/Code/printing-press-library/library/productivity/cal-com

  # Refuse to run if Pagliacci has a feedback resource collision
  printing-press patch ~/Code/printing-press-library/library/food-and-dining/pagliacci-pizza

  # Force past a resource-level collision (skips the colliding drop-in)
  printing-press patch .../pagliacci-pizza --force

  # JSON output for batch scripting
  printing-press patch . --dry-run --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := patch.Patch(patch.Options{
				Dir:       args[0],
				DryRun:    dryRun,
				Force:     force,
				SkipBuild: skipBuild,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			printPatchSummary(cmd.ErrOrStderr(), report)
			if len(report.Collisions) > 0 && !force {
				return fmt.Errorf("patch refused: %d resource-level collision(s) — resolve manually or re-run with --force", len(report.Collisions))
			}
			if !skipBuild && !dryRun && !report.BuildOK {
				return fmt.Errorf("post-patch build failed; see build_output in the report")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the file list and collisions without writing anything")
	cmd.Flags().BoolVar(&force, "force", false, "Proceed even when resource-level collisions are detected (skips colliding drop-ins)")
	cmd.Flags().BoolVar(&skipBuild, "skip-build", false, "Skip the post-patch `go build ./...` gate")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the full Report as JSON on stdout (for batch scripting)")
	return cmd
}

func printPatchSummary(w io.Writer, r *patch.Report) {
	fmt.Fprintf(w, "CLI:        %s\n", r.Name)
	fmt.Fprintf(w, "Dir:        %s\n", r.Dir)
	if r.DryRun {
		fmt.Fprintf(w, "Mode:       dry-run (no writes)\n")
	}
	if r.Idempotent {
		fmt.Fprintf(w, "Status:     already patched — no-op\n")
		return
	}
	if len(r.FilesCreated) > 0 {
		fmt.Fprintf(w, "Created:    %d\n", len(r.FilesCreated))
		for _, f := range r.FilesCreated {
			fmt.Fprintf(w, "  + %s\n", f)
		}
	}
	if len(r.FilesModified) > 0 {
		fmt.Fprintf(w, "Modified:   %d\n", len(r.FilesModified))
		for _, f := range r.FilesModified {
			fmt.Fprintf(w, "  ~ %s\n", f)
		}
	}
	if len(r.Collisions) > 0 {
		fmt.Fprintf(w, "Collisions: %d\n", len(r.Collisions))
		for _, c := range r.Collisions {
			fmt.Fprintf(w, "  ! [%s] %s\n", c.Kind, c.Message)
		}
	}
	if !r.DryRun {
		if r.BuildOK {
			fmt.Fprintf(w, "Build:      PASS\n")
		} else {
			fmt.Fprintf(w, "Build:      FAIL\n%s\n", r.BuildOutput)
		}
	}
}
