package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v2/internal/pipeline"
	"github.com/spf13/cobra"
)

func newScorecardCmd() *cobra.Command {
	var dir string
	var researchDir string
	var specPath string
	var asJSON bool
	var liveCheck bool
	var liveCheckTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "scorecard",
		Short: "Score a generated CLI against the Steinberger bar",
		Example: `  # Score a generated CLI directory
  printing-press scorecard --dir ./generated/stripe-pp-cli

  # Include a live behavioral sample (runs novel-feature examples against real targets)
  printing-press scorecard --dir ./generated/stripe-pp-cli --live-check

  # Live-check a CLI whose research.json lives in the run state, not the CLI dir
  printing-press scorecard --dir ./working/foo-pp-cli --research-dir ./runs/<id> --live-check

  # Output as JSON
  printing-press scorecard --dir ./generated/stripe-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			// Use a temp pipeline dir for the scorecard output
			pipelineDir, err := os.MkdirTemp("", "scorecard-*")
			if err != nil {
				return fmt.Errorf("creating temp dir: %w", err)
			}
			defer func() { _ = os.RemoveAll(pipelineDir) }()

			sc, err := pipeline.RunScorecard(dir, pipelineDir, specPath, nil)
			if err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("running scorecard: %w", err)}
			}

			var live *pipeline.LiveCheckResult
			if liveCheck {
				live = pipeline.RunLiveCheck(pipeline.LiveCheckOptions{
					CLIDir:      dir,
					ResearchDir: researchDir,
					Timeout:     liveCheckTimeout,
				})
				if insightCap := pipeline.InsightCapFromLiveCheck(live); insightCap != nil && sc.Steinberger.Insight > *insightCap {
					sc.Steinberger.Insight = *insightCap
				}
				pipeline.ApplyLiveCheckToScorecard(sc, live)
			}

			if asJSON {
				payload := map[string]any{"scorecard": sc}
				if live != nil {
					payload["live_check"] = live
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}

			// Human-readable output
			renderHumanScorecard(os.Stdout, sc)

			if live != nil {
				fmt.Printf("\nLive Check (behavioral sample)\n")
				if live.Unable {
					fmt.Printf("  Unable to run: %s\n", live.Reason)
				} else {
					fmt.Printf("  Passed: %d/%d  (%d%% pass rate)\n", live.Passed, live.Checked(), int(live.PassRate*100+0.5))
					if live.Failed > 0 {
						fmt.Println("  Failures:")
						for _, f := range live.Features {
							if f.Status == "fail" {
								fmt.Printf("    - %s: %s\n", f.Name, f.Reason)
							}
						}
					}
					// Wave B output-quality warnings are surfaced here so a
					// developer running scorecard without --json sees them.
					// Wave A review flagged the "human sees less than agent"
					// gap as an agent-native parity concern.
					warnCount := 0
					for _, f := range live.Features {
						warnCount += len(f.Warnings)
					}
					if warnCount > 0 {
						fmt.Printf("  Warnings (%d): not blocking in Wave B — flip to failures in Wave C after calibration\n", warnCount)
						for _, f := range live.Features {
							for _, w := range f.Warnings {
								fmt.Printf("    - %s: %s\n", f.Name, w)
							}
						}
					}
				}
			}

			if len(sc.GapReport) > 0 {
				fmt.Printf("\nGaps:\n")
				for _, g := range sc.GapReport {
					fmt.Printf("  - %s\n", g)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to generated CLI directory")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Directory containing research.json (defaults to --dir; useful when the CLI working dir and the run's research.json live in different directories)")
	cmd.Flags().StringVar(&specPath, "spec", "", "Path to OpenAPI spec JSON for semantic validation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&liveCheck, "live-check", false, "Sample novel-feature examples against real targets and cap Insight when flagships return broken output")
	cmd.Flags().DurationVar(&liveCheckTimeout, "live-check-timeout", 10*time.Second, "Per-feature timeout for live check invocations")

	return cmd
}

// renderHumanScorecard writes the human-readable scorecard table to w.
// Optional dimensions (those that may land in sc.UnscoredDimensions
// when the spec hasn't opted into the feature being measured) render
// as "N/A" so the scorer's "didn't measure" signal isn't flattened
// into a 0/10 that reads as a defect.
func renderHumanScorecard(w io.Writer, sc *pipeline.Scorecard) {
	s := sc.Steinberger
	render := func(name string, score, max int) string {
		if sc.IsDimensionUnscored(name) {
			return "N/A"
		}
		return fmt.Sprintf("%d/%d", score, max)
	}

	fmt.Fprintf(w, "Quality Scorecard: %s\n\n", sc.APIName)
	fmt.Fprintf(w, "  Output Modes         %d/10\n", s.OutputModes)
	fmt.Fprintf(w, "  Auth                 %d/10\n", s.Auth)
	fmt.Fprintf(w, "  Error Handling       %d/10\n", s.ErrorHandling)
	fmt.Fprintf(w, "  Terminal UX          %d/10\n", s.TerminalUX)
	fmt.Fprintf(w, "  README               %d/10\n", s.README)
	fmt.Fprintf(w, "  Doctor               %d/10\n", s.Doctor)
	fmt.Fprintf(w, "  Agent Native         %d/10\n", s.AgentNative)
	fmt.Fprintf(w, "  MCP Quality          %d/10\n", s.MCPQuality)
	fmt.Fprintf(w, "  MCP Desc Quality     %s\n", render(pipeline.DimMCPDescriptionQuality, s.MCPDescriptionQuality, 10))
	fmt.Fprintf(w, "  MCP Token Efficiency %s\n", render(pipeline.DimMCPTokenEfficiency, s.MCPTokenEff, 10))
	fmt.Fprintf(w, "  MCP Remote Transport %s\n", render(pipeline.DimMCPRemoteTransport, s.MCPRemoteTransport, 10))
	fmt.Fprintf(w, "  MCP Tool Design      %s\n", render(pipeline.DimMCPToolDesign, s.MCPToolDesign, 10))
	fmt.Fprintf(w, "  MCP Surface Strategy %s\n", render(pipeline.DimMCPSurfaceStrategy, s.MCPSurfaceStrategy, 10))
	fmt.Fprintf(w, "  Local Cache          %d/10\n", s.LocalCache)
	fmt.Fprintf(w, "  Cache Freshness      %s\n", render(pipeline.DimCacheFreshness, s.CacheFreshness, 10))
	fmt.Fprintf(w, "  Breadth              %d/10\n", s.Breadth)
	fmt.Fprintf(w, "  Vision               %d/10\n", s.Vision)
	fmt.Fprintf(w, "  Workflows            %d/10\n", s.Workflows)
	fmt.Fprintf(w, "  Insight              %d/10\n", s.Insight)
	fmt.Fprintf(w, "  Agent Workflow       %d/10\n", s.AgentWorkflow)
	fmt.Fprintf(w, "\n  Domain Correctness\n")
	fmt.Fprintf(w, "  Path Validity           %s\n", render(pipeline.DimPathValidity, s.PathValidity, 10))
	fmt.Fprintf(w, "  Auth Protocol           %s\n", render(pipeline.DimAuthProtocol, s.AuthProtocol, 10))
	fmt.Fprintf(w, "  Data Pipeline Integrity %d/10\n", s.DataPipelineIntegrity)
	fmt.Fprintf(w, "  Sync Correctness        %d/10\n", s.SyncCorrectness)
	fmt.Fprintf(w, "  Live API Verification   %s\n", render(pipeline.DimLiveAPIVerification, s.LiveAPIVerification, 10))
	fmt.Fprintf(w, "  Type Fidelity           %d/5\n", s.TypeFidelity)
	fmt.Fprintf(w, "  Dead Code               %d/5\n", s.DeadCode)
	fmt.Fprintf(w, "\n  Total: %d/100 - Grade %s\n", s.Total, sc.OverallGrade)
	if len(sc.UnscoredDimensions) > 0 {
		fmt.Fprintf(w, "  Note: omitted from denominator: %s\n", strings.Join(sc.UnscoredDimensions, ", "))
	}
}
