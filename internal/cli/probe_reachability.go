package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/reachability"
	"github.com/spf13/cobra"
)

// newProbeReachabilityCmd classifies a URL's reachability without opening
// a browser. Used by the printing-press skill (Phase 1.7) to decide
// whether browser-sniff needs to escalate to a real Chrome attach for
// clearance-cookie capture, or whether Surf alone is enough.
//
// Diagnostic only — exit 0 regardless of classification. Exit non-zero
// only on input-validation failure.
func newProbeReachabilityCmd() *cobra.Command {
	var (
		asJSON    bool
		timeout   time.Duration
		probeOnly string
	)

	cmd := &cobra.Command{
		Use:   "probe-reachability <url>",
		Short: "Classify a URL's reachability with no-browser probes",
		Long: `Runs stdlib HTTP and Surf (Chrome TLS fingerprint) probes against the URL
and emits a reachability mode matching internal/browsersniff vocabulary:
standard_http, browser_http, browser_clearance_http, or unknown.

Diagnostic only. Exit 0 regardless of result; exit non-zero only on
input-validation failure.`,
		Example: `  printing-press probe-reachability https://food52.com/
  printing-press probe-reachability https://api.github.com/zen --json
  printing-press probe-reachability https://example.com --probe-only surf --timeout 5s`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			only := reachability.ProbeOnly(probeOnly)
			switch only {
			case reachability.ProbeOnlyNone, reachability.ProbeOnlyStdlib, reachability.ProbeOnlySurf:
			default:
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("invalid --probe-only value: %q (want stdlib or surf)", probeOnly)}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout*2+5*time.Second)
			defer cancel()

			result, err := reachability.Probe(ctx, args[0], reachability.Options{
				Timeout:   timeout,
				ProbeOnly: only,
			})
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}

			if asJSON {
				return reachability.RenderJSON(cmd.OutOrStdout(), result)
			}
			return reachability.RenderTable(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable table")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "per-probe timeout")
	cmd.Flags().StringVar(&probeOnly, "probe-only", "", "restrict the ladder to one rung (stdlib|surf); debug only")
	return cmd
}
