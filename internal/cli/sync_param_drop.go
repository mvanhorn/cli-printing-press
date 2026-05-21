package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/spf13/cobra"
)

// newSyncParamDropCmd exposes the hand-authored-sync param-drop gate as
// a Phase 4.x check the printing-press skill can shell out to. The gate
// compares the param-key set each `client.<Method>(path, params)` call
// passes against the captured key set on the same endpoint in a
// browser-sniff traffic-analysis.json. Captured-superset call sites are
// reported as findings; the user either widens the call to match the
// live site or annotates it with `// pp:sync-params-intentional-subset`.
//
// Diagnostic by default. Pass `--strict` to make the command exit
// non-zero when findings remain.
func newSyncParamDropCmd() *cobra.Command {
	var cliDir string
	var trafficAnalysisPath string
	var asJSON bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "sync-param-drop",
		Short: "Flag sync/transcendence calls that pass fewer params than the site captured",
		Long: `Walks every .go file under <cli-dir>/internal/syncer/ (and parallel
hand-authored sync directories) and finds calls of the shape
client.<HTTPMethod>(path, params). For each call, looks up the same path
in the supplied browser-sniff traffic-analysis.json and reports the call
as a finding when the captured query/body key set is a strict superset of
what the code passes. Suppress one call site with a
// pp:sync-params-intentional-subset reason=... comment immediately above.

Diagnostic by default; pass --strict for a non-zero exit on findings.`,
		Example: `  printing-press sync-param-drop --dir ./factor75-pp-cli --traffic-analysis ./pipeline/factor75-traffic-analysis.json
  printing-press sync-param-drop --dir . --traffic-analysis ./traffic-analysis.json --strict --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliDir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}
			if trafficAnalysisPath == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--traffic-analysis is required")}
			}

			result := pipeline.CheckSyncParamDrop(cliDir, trafficAnalysisPath)

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(result); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			} else {
				renderSyncParamDrop(cmd.OutOrStdout(), result)
			}

			if strict && len(result.Findings) > 0 {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("sync-param-drop has %d finding(s)", len(result.Findings))}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cliDir, "dir", "", "Path to a printed-CLI work directory (required)")
	cmd.Flags().StringVar(&trafficAnalysisPath, "traffic-analysis", "", "Path to a traffic-analysis.json file (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit JSON instead of a human-readable summary")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero when findings remain")
	return cmd
}

func renderSyncParamDrop(out interface{ Write([]byte) (int, error) }, result pipeline.SyncParamDropResult) {
	if result.Skipped {
		fmt.Fprintln(out, "sync-param-drop: skipped (no traffic-analysis capture or no syncer sources)")
		return
	}
	fmt.Fprintf(out, "sync-param-drop: %d call(s) checked, %d finding(s), %d suppressed\n",
		result.Checked, len(result.Findings), result.Suppressed,
	)
	for _, f := range result.Findings {
		fmt.Fprintln(out, "- "+pipeline.FormatSyncParamDropFinding(f))
	}
}
