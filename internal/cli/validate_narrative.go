package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/mvanhorn/cli-printing-press/v4/internal/narrativecheck"
	"github.com/spf13/cobra"
)

func newValidateNarrativeCmd() *cobra.Command {
	var (
		researchPath string
		binaryPath   string
		strict       bool
		fullExamples bool
		asJSON       bool
	)

	cmd := &cobra.Command{
		Use:           "validate-narrative",
		Short:         "Verify research.json narrative commands against a built CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Walks every narrative.quickstart[].command and narrative.recipes[].command
in research.json and runs '<binary> <words> --help' to confirm each command
path exists. With --full-examples, also runs each complete example under
PRINTING_PRESS_VERIFY=1, appending --dry-run when the command advertises it.

Without this check, broken commands ship to the README's Quick Start and
the SKILL's recipes; users hit "unknown command" on copy-paste.`,
		Example: `  # Default: warn-only, exits 0 even when commands are missing
  printing-press validate-narrative \
    --research $API_RUN_DIR/research.json \
    --binary $CLI_WORK_DIR/myapi-pp-cli

  # Strict: exits non-zero on missing commands or empty narrative
  printing-press validate-narrative --strict \
    --research $API_RUN_DIR/research.json \
    --binary $CLI_WORK_DIR/myapi-pp-cli

  # Stronger check: also dry-run full examples to catch bad flags/args
  printing-press validate-narrative --strict --full-examples \
    --research $API_RUN_DIR/research.json \
    --binary $CLI_WORK_DIR/myapi-pp-cli

  # JSON output for downstream tooling
  printing-press validate-narrative --json \
    --research $API_RUN_DIR/research.json \
    --binary $CLI_WORK_DIR/myapi-pp-cli`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if researchPath == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--research is required")}
			}
			if binaryPath == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--binary is required")}
			}

			// Honor SIGINT so a stuck `<binary> --help` (e.g., a CLI
			// that itself spawns a child) doesn't block forever.
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			report, err := narrativecheck.ValidateWithOptions(ctx, researchPath, binaryPath, narrativecheck.Options{
				FullExamples: fullExamples,
			})
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}

			// Human report goes to stderr so --json on stdout pipes cleanly.
			if asJSON {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
					return err
				}
			} else {
				printHumanReport(cmd.OutOrStderr(), report)
			}

			if strict && (report.HasFailures() || report.ResearchEmpty) {
				return &ExitError{Code: ExitInputError, Err: errors.New("narrative validation failed")}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&researchPath, "research", "", "Path to research.json (required)")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Path to the built CLI binary to walk (required)")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero on any missing command or empty narrative (default: warn-only)")
	cmd.Flags().BoolVar(&fullExamples, "full-examples", false, "Also run full narrative examples safely with PRINTING_PRESS_VERIFY=1 and --dry-run where supported")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit machine-readable JSON instead of the human report")
	return cmd
}

func printHumanReport(w io.Writer, report *narrativecheck.Report) {
	if report.ResearchEmpty {
		fmt.Fprintln(w, "WARNING: research.json has no narrative.quickstart or narrative.recipes entries")
	}
	for _, r := range report.Results {
		switch r.Status {
		case narrativecheck.StatusMissing:
			fmt.Fprintf(w, "MISSING [%s]: %s → %s\n", r.Section, r.Command, r.Words)
		case narrativecheck.StatusEmptyWords:
			fmt.Fprintf(w, "EMPTY [%s]: %s has no subcommand words to verify\n", r.Section, r.Command)
		case narrativecheck.StatusExampleFailed:
			fmt.Fprintf(w, "FAILED [%s]: %s → %s\n", r.Section, r.Command, r.Error)
		case narrativecheck.StatusUnsupported:
			fmt.Fprintf(w, "UNSUPPORTED [%s]: %s → %s\n", r.Section, r.Command, r.Error)
		}
	}
	if !report.HasFailures() && !report.ResearchEmpty {
		if report.FullExamples {
			fmt.Fprintf(w, "OK: %d narrative commands resolved and full examples passed\n", report.Walked)
			return
		}
		fmt.Fprintf(w, "OK: %d narrative commands resolved against the CLI tree\n", report.Walked)
		return
	}
	fmt.Fprintf(w, "DONE: %d ok, %d missing, %d empty-words, %d failed-examples, %d unsupported\n",
		report.Walked, report.Missing, report.Empty, report.ExampleFailed, report.Unsupported)
}
