package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/spf13/cobra"
)

func newDogfoodCmd() *cobra.Command {
	var dir string
	var specPath string
	var researchDir string
	var asJSON bool
	var live bool
	var level string
	var timeout time.Duration
	var writeAcceptance string
	var authEnv string
	var allowDestructive bool

	cmd := &cobra.Command{
		Use:   "dogfood",
		Short: "Validate a generated CLI against its source spec",
		Long:  "Mechanically verify that a generated CLI's commands hit valid API paths, auth matches the spec protocol, no dead flags/functions exist, and the data pipeline is wired correctly.",
		Example: `  # Evaluate a generated CLI directory
  printing-press dogfood --dir ./generated/stripe-pp-cli

  # Output as JSON for programmatic use
  printing-press dogfood --dir ./generated/stripe-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if live {
				report, err := pipeline.RunLiveDogfood(pipeline.LiveDogfoodOptions{
					CLIDir:              dir,
					Level:               level,
					Timeout:             timeout,
					WriteAcceptancePath: writeAcceptance,
					AuthEnv:             authEnv,
					AllowDestructive:    allowDestructive,
				})
				if err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("running live dogfood: %w", err)}
				}
				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					if err := enc.Encode(report); err != nil {
						return err
					}
				} else {
					printLiveDogfoodReport(report)
				}
				if report.Verdict != "PASS" {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("live dogfood failed: %d/%d tests failed", report.Failed, report.MatrixSize)}
				}
				return nil
			}

			var opts []pipeline.DogfoodOption
			if researchDir != "" {
				opts = append(opts, pipeline.WithResearchDir(researchDir))
			}
			report, err := pipeline.RunDogfood(dir, specPath, opts...)
			if err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("running dogfood: %w", err)}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}

			printDogfoodReport(report)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the generated CLI directory (required)")
	cmd.Flags().StringVar(&specPath, "spec", "", "Path to the OpenAPI spec file")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Pipeline directory containing research.json for novel features validation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&live, "live", false, "Run the Phase 5 live command-tree dogfood matrix")
	cmd.Flags().StringVar(&level, "level", "full", "Live dogfood depth: quick or full")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Timeout for each live dogfood test")
	cmd.Flags().StringVar(&writeAcceptance, "write-acceptance", "", "Write phase5-acceptance.json to this path when live dogfood passes")
	cmd.Flags().StringVar(&authEnv, "auth-env", "", "Environment variable that proves an API credential was available for the acceptance marker")
	cmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Re-enable testing of endpoints classified as destructive-at-auth. Default skips them to prevent runner-credential rotation.")
	_ = cmd.MarkFlagRequired("dir")
	return cmd
}

func printLiveDogfoodReport(report *pipeline.LiveDogfoodReport) {
	fmt.Printf("Live Dogfood Report: %s\n", filepath.Base(report.Dir))
	fmt.Println("================================")
	fmt.Println()
	fmt.Printf("Level:      %s\n", report.Level)
	fmt.Printf("Verdict:    %s%s\n", report.Verdict, liveDogfoodVerdictQualifier(report))
	fmt.Printf("Commands:   %d\n", len(report.Commands))
	fmt.Printf("Tests:      %d passed, %d failed, %d skipped\n", report.Passed, report.Failed, report.Skipped)
	fmt.Println()
	for _, result := range report.Tests {
		status := strings.ToUpper(string(result.Status))
		fmt.Printf("[%s] %s %s", status, result.Command, result.Kind)
		if result.Reason != "" {
			fmt.Printf(": %s", result.Reason)
		}
		fmt.Println()
	}
}

func liveDogfoodVerdictQualifier(report *pipeline.LiveDogfoodReport) string {
	if report == nil || report.Verdict != "PASS" {
		return ""
	}
	if report.Skipped > 0 {
		return " (with skips)"
	}
	return " (all tests run)"
}

func printDogfoodReport(report *pipeline.DogfoodReport) {
	name := filepath.Base(report.Dir)

	fmt.Printf("Dogfood Report: %s\n", name)
	fmt.Println("================================")
	fmt.Println()

	pathStatus := "SKIP"
	if report.SpecPath != "" && !report.PathCheck.Skipped {
		pathStatus = "PASS"
		if report.PathCheck.Pct < 70 {
			pathStatus = "FAIL"
		}
	}
	fmt.Printf("Path Validity:     %d/%d valid (%s)\n", report.PathCheck.Valid, report.PathCheck.Tested, pathStatus)
	if report.PathCheck.Skipped && report.PathCheck.Detail != "" {
		fmt.Printf("  Detail: %s\n", report.PathCheck.Detail)
	}
	for _, path := range report.PathCheck.Invalid {
		fmt.Printf("  - %s: not in spec\n", path)
	}
	fmt.Println()

	authStatus := "SKIP"
	if report.SpecPath != "" {
		authStatus = "MATCH"
		if !report.AuthCheck.Match {
			authStatus = "MISMATCH"
		}
	}
	fmt.Printf("Auth Protocol:     %s\n", authStatus)
	if report.AuthCheck.SpecScheme != "" {
		fmt.Printf("  Spec: %s\n", report.AuthCheck.SpecScheme)
	}
	if report.AuthCheck.GeneratedFmt != "" {
		fmt.Printf("  Generated: Uses %q prefix\n", trimSpaceOrUnknown(report.AuthCheck.GeneratedFmt))
	}
	if report.AuthCheck.Detail != "" {
		fmt.Printf("  Detail: %s\n", report.AuthCheck.Detail)
	}
	fmt.Println()

	flagsStatus := "PASS"
	if report.DeadFlags.Dead >= 3 {
		flagsStatus = "FAIL"
	} else if report.DeadFlags.Dead > 0 {
		flagsStatus = "WARN"
	}
	fmt.Printf("Dead Flags:        %d dead (%s)\n", report.DeadFlags.Dead, flagsStatus)
	for _, item := range report.DeadFlags.Items {
		fmt.Printf("  - %s (declared, never read)\n", item)
	}
	fmt.Println()

	funcsStatus := "PASS"
	if report.DeadFuncs.Dead > 0 {
		funcsStatus = "WARN"
	}
	fmt.Printf("Dead Functions:    %d dead (%s)\n", report.DeadFuncs.Dead, funcsStatus)
	for _, item := range report.DeadFuncs.Items {
		fmt.Printf("  - %s (defined, never called)\n", item)
	}
	fmt.Println()

	pipelineStatus := "GOOD"
	if !report.PipelineCheck.SyncCallsDomain || !report.PipelineCheck.SearchCallsDomain || report.PipelineCheck.DomainTables == 0 {
		pipelineStatus = "PARTIAL"
	}
	fmt.Printf("Data Pipeline:     %s\n", pipelineStatus)
	if report.PipelineCheck.SyncCallsDomain {
		fmt.Println("  Sync: calls domain-specific Upsert methods (GOOD)")
	} else {
		fmt.Println("  Sync: uses generic Upsert only")
	}
	if report.PipelineCheck.SearchCallsDomain {
		fmt.Println("  Search: calls domain-specific Search methods (GOOD)")
	} else {
		fmt.Println("  Search: uses generic Search only or direct SQL")
	}
	fmt.Printf("  Domain tables: %d\n", report.PipelineCheck.DomainTables)
	fmt.Println()

	if report.ExampleCheck.Skipped {
		fmt.Printf("Examples:          SKIP (%s)\n", report.ExampleCheck.Detail)
	} else {
		exampleStatus := "PASS"
		if report.ExampleCheck.Tested > 0 && (report.ExampleCheck.WithExamples*100/report.ExampleCheck.Tested) < 50 {
			exampleStatus = "FAIL"
		} else if len(report.ExampleCheck.InvalidFlags) > 0 {
			exampleStatus = "WARN"
		} else if report.ExampleCheck.Tested == 0 {
			exampleStatus = "SKIP"
		}
		fmt.Printf("Examples:          %d/%d commands have examples", report.ExampleCheck.WithExamples, report.ExampleCheck.Tested)
		if len(report.ExampleCheck.InvalidFlags) > 0 {
			fmt.Printf(" (%d invalid flags: %s)", len(report.ExampleCheck.InvalidFlags), strings.Join(report.ExampleCheck.InvalidFlags, ", "))
		}
		fmt.Printf(" (%s)\n", exampleStatus)
		for _, cmd := range report.ExampleCheck.Missing {
			fmt.Printf("  - %s: missing example\n", cmd)
		}
	}
	fmt.Println()

	nfc := report.NovelFeaturesCheck
	if nfc.Skipped {
		fmt.Println("Novel Features:    SKIP (no research.json)")
	} else if nfc.Planned == 0 {
		fmt.Println("Novel Features:    SKIP (none planned)")
	} else {
		nfStatus := "PASS"
		if len(nfc.Missing) > 0 {
			nfStatus = "WARN"
		}
		fmt.Printf("Novel Features:    %d/%d survived (%s)\n", nfc.Found, nfc.Planned, nfStatus)
		for _, cmd := range nfc.Missing {
			fmt.Printf("  - %s: planned but not found\n", cmd)
		}
	}
	fmt.Println()

	mcp := report.MCPSurfaceParityCheck
	if mcp.Skipped {
		fmt.Printf("MCP Surface:       SKIP (%s)\n", mcp.Detail)
	} else {
		mcpStatus := "PASS"
		if mcp.HandEdited {
			mcpStatus = "WARN"
		} else if !mcp.Pass {
			mcpStatus = "FAIL"
		}
		fmt.Printf("MCP Surface:       %s (%s)\n", mcpStatus, mcp.Detail)
	}
	fmt.Println()

	fmt.Printf("Verdict: %s\n", report.Verdict)
	for _, issue := range report.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}

func trimSpaceOrUnknown(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
