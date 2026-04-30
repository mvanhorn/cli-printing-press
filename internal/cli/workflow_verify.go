package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/spf13/cobra"
)

func newWorkflowVerifyCmd() *cobra.Command {
	var dir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "workflow-verify",
		Short: "Verify a generated CLI can complete its primary workflow",
		Long:  "Run the workflow verification manifest against a built CLI binary, testing that multi-step flows (e.g., ordering a pizza, creating a project) actually work end-to-end.",
		Example: `  # Verify a generated CLI's primary workflow
  printing-press workflow-verify --dir ./generated/dominos-pp-cli

  # Output as JSON for programmatic use
  printing-press workflow-verify --dir ./generated/dominos-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := pipeline.RunWorkflowVerification(dir)
			if err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("workflow verification: %w", err)}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}

			printWorkflowVerifyReport(report)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the generated CLI directory (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func printWorkflowVerifyReport(report *pipeline.WorkflowVerifyReport) {
	name := filepath.Base(report.Dir)

	fmt.Printf("Workflow Verification: %s\n", name)
	fmt.Println("================================")
	fmt.Println()

	for _, w := range report.Workflows {
		primary := ""
		if w.Primary {
			primary = " (primary)"
		}
		fmt.Printf("Workflow: %s%s\n", w.Name, primary)
		fmt.Printf("  Verdict: %s\n", w.Verdict)

		for i, step := range w.Steps {
			fmt.Printf("  Step %d: %s -> %s\n", i+1, step.Command, step.Status)
			if step.Error != "" {
				fmt.Printf("    Error: %s\n", step.Error)
			}
		}
		fmt.Println()
	}

	fmt.Printf("Overall Verdict: %s\n", report.Verdict)
	for _, issue := range report.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}
