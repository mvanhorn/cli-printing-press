package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v3/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	var dir string
	var specPath string
	var apiKey string
	var envVar string
	var threshold int
	var fix bool
	var maxIterations int
	var asJSON bool
	var cleanup bool
	var noSpec bool

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Runtime-test a generated CLI against real API or mock server",
		Long: `Build the generated CLI, then run every command against the real API
(read-only GETs) or a spec-derived mock server. Produces a PASS/WARN/FAIL
verdict with per-command scores and a data pipeline integrity check.

If --api-key is provided, tests run against the real API (read-only only).
Otherwise, a mock server is started from the OpenAPI spec.

Use --fix to auto-patch common failures and re-test (max 3 iterations).`,
		Example: `  # Test against real API (read-only GETs only)
  printing-press verify --dir ./github-pp-cli --spec /tmp/spec.json --api-key $GITHUB_TOKEN

  # Test against mock server (no API key needed)
  printing-press verify --dir ./github-pp-cli --spec /tmp/spec.json

  # Auto-fix failures and re-test
  printing-press verify --dir ./github-pp-cli --spec /tmp/spec.json --fix

  # Remove transient build artifacts after the final verification pass
  printing-press verify --dir ./github-pp-cli --spec /tmp/spec.json --cleanup

  # Set pass threshold and output JSON
  printing-press verify --dir ./github-pp-cli --spec /tmp/spec.json --threshold 70 --json

  # Structural verification without an API spec (plan-driven CLIs)
  printing-press verify --dir ./agent-capture-pp-cli --no-spec`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := pipeline.VerifyConfig{
				Dir:       dir,
				SpecPath:  specPath,
				APIKey:    apiKey,
				EnvVar:    envVar,
				Threshold: threshold,
				NoSpec:    noSpec,
			}

			report, err := pipeline.RunVerify(cfg)
			if err != nil {
				return fmt.Errorf("running verify: %w", err)
			}

			// Run fix loop if requested and score is below threshold
			var fixReport *pipeline.FixLoopReport
			if fix && shouldRunFixLoop(report) {
				fmt.Printf("\nVerification verdict %s (pass rate %.0f%%, threshold %d%%). Running fix loop (max %d iterations)...\n\n",
					report.Verdict, report.PassRate, threshold, maxIterations)
				fixReport, err = pipeline.RunFixLoop(cfg, report, maxIterations)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Fix loop error: %v\n", err)
				} else if fixReport.FinalReport != nil {
					report = fixReport.FinalReport
				}
			}

			if err := cleanupVerifyArtifacts(dir, cleanup); err != nil {
				return err
			}

			if asJSON {
				output := map[string]any{"verify": report}
				if fixReport != nil {
					output["fix_loop"] = fixReport
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			printVerifyReport(report)

			if fixReport != nil {
				fmt.Printf("\nFix Loop: %d iterations, improved: %v\n", len(fixReport.Iterations), fixReport.Improved)
				for _, iter := range fixReport.Iterations {
					fmt.Printf("  Iteration %d: %.0f%% -> %.0f%% (%+.0f%%), %d fixes applied\n",
						iter.Number, iter.BeforeRate, iter.AfterRate, iter.Delta, len(iter.Fixes))
				}
			}

			if report.Verdict == "FAIL" {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the generated CLI directory (required)")
	cmd.Flags().StringVar(&specPath, "spec", "", "Path to the OpenAPI spec file")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for live testing (read-only GETs only)")
	cmd.Flags().StringVar(&envVar, "env-var", "", "Environment variable name for the API key (e.g., GITHUB_TOKEN)")
	cmd.Flags().IntVar(&threshold, "threshold", 80, "Minimum pass rate percentage")
	cmd.Flags().BoolVar(&fix, "fix", false, "Auto-fix common failures and re-test")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 3, "Maximum fix loop iterations")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "Remove transient build artifacts after verification")
	cmd.Flags().BoolVar(&noSpec, "no-spec", false, "Structural verification only (no API spec required)")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func shouldRunFixLoop(report *pipeline.VerifyReport) bool {
	if report == nil {
		return false
	}
	return report.Verdict != "PASS"
}

func printVerifyReport(report *pipeline.VerifyReport) {
	fmt.Printf("Runtime Verification: %s\n", report.Binary)
	fmt.Printf("Mode: %s\n\n", report.Mode)

	// Per-command results
	fmt.Printf("%-30s %-12s %-6s %-8s %-8s %s\n", "COMMAND", "KIND", "HELP", "DRY-RUN", "EXEC", "SCORE")
	for _, r := range report.Results {
		fmt.Printf("%-30s %-12s %-6s %-8s %-8s %d/3\n",
			truncStr(r.Command, 30),
			r.Kind,
			passFail(r.Help),
			passFail(r.DryRun),
			passFail(r.Execute),
			r.Score)
	}

	fmt.Println()
	if report.DataPipelineDetail != "" {
		fmt.Printf("Data Pipeline: %s\n", report.DataPipelineDetail)
	} else {
		fmt.Printf("Data Pipeline: %s\n", passFail(report.DataPipeline))
	}
	if report.BrowserSessionRequired {
		fmt.Printf("Browser Session Proof: %s", report.BrowserSessionProof)
		if report.BrowserSessionDetail != "" {
			fmt.Printf(" (%s)", report.BrowserSessionDetail)
		}
		fmt.Println()
	}
	fmt.Printf("Pass Rate: %.0f%% (%d/%d passed, %d critical)\n",
		report.PassRate, report.Passed, report.Total, report.Critical)
	fmt.Printf("Verdict: %s\n", report.Verdict)
}

func cleanupVerifyArtifacts(dir string, cleanup bool) error {
	if !cleanup {
		return nil
	}
	if dir == "" {
		return fmt.Errorf("cleaning generated artifacts: empty dir")
	}
	if !filepath.IsAbs(dir) {
		if absDir, err := filepath.Abs(dir); err == nil {
			dir = absDir
		}
	}
	if err := artifacts.CleanupGeneratedCLI(dir, artifacts.CleanupOptions{
		RemoveCache:              true,
		RemoveRuntimeBinary:      true,
		RemoveValidationBinaries: true,
		RemoveDogfoodBinaries:    true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return fmt.Errorf("cleaning generated artifacts: %w", err)
	}
	return nil
}

func passFail(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
