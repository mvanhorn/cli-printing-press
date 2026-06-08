package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
)

func newApifyActorAuditCmd() *cobra.Command {
	var dir string
	var researchDir string
	var asJSON bool
	var baseURL string

	cmd := &cobra.Command{
		Use:   "apify-audit",
		Short: "Verify referenced Apify actors are reachable",
		Long: `Scan a generated CLI and optional research directory for Apify actor IDs
used with actor run endpoints, then probe GET /v2/acts/{actor}. The audit checks
actor reachability only and never starts paid actor runs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := pipeline.RunApifyActorAudit(context.Background(), pipeline.ApifyActorAuditOptions{
				Dir:         dir,
				ResearchDir: researchDir,
				BaseURL:     baseURL,
			})
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("apify actor audit: %w", err)}
			}
			if err := pipeline.WriteApifyActorAuditReport(dir, report); err != nil {
				return &ExitError{Code: ExitGenerationError, Err: err}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return fmt.Errorf("rendering Apify actor audit JSON: %w", err)
				}
			} else {
				printApifyActorAuditReport(report)
			}

			if report.Verdict == pipeline.ApifyActorAuditFail {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("apify actor audit failed: %d issue(s)", len(report.Issues))}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the generated CLI directory (required)")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Pipeline directory containing research.json")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Override Apify API base URL (for tests)")
	_ = cmd.Flags().MarkHidden("base-url")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func printApifyActorAuditReport(report *pipeline.ApifyActorAuditReport) {
	fmt.Println("Apify Actor Audit")
	fmt.Println("=================")
	fmt.Println()
	for _, actor := range report.Actors {
		fmt.Printf("%s: %s\n", actor.ID, actor.Status)
		if actor.Detail != "" {
			fmt.Printf("  %s\n", actor.Detail)
		}
		for _, source := range actor.Sources {
			fmt.Printf("  - %s\n", source)
		}
	}
	if len(report.Actors) == 0 {
		fmt.Println("No Apify actor references found.")
	}
	fmt.Println()
	fmt.Printf("Verdict: %s\n", report.Verdict)
	for _, issue := range report.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}
