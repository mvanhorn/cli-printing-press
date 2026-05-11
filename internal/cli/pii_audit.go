package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/spf13/cobra"
)

// newPIIAuditCmd inspects a printed CLI's high-risk files for PII-shape
// matches. Mirrors `tools-audit` semantics: detection is purely
// mechanical, agent judgment runs through the polish skill's pii-polish
// playbook, and the ledger at <cli-dir>/.printing-press-pii-polish.json
// persists agent decisions across runs.
//
// Default exit is 0 (diagnostic). The `--strict` flag exits non-zero
// when pending findings or gate failures remain — for external CI
// callers that shell out to `printing-press pii-audit`. The in-process
// promote/publish gates call artifacts.RunPIIAudit directly and apply
// equivalent enforcement.
func newPIIAuditCmd() *cobra.Command {
	var asJSON bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "pii-audit <cli-dir>",
		Short: "Mechanically audit a printed CLI's high-risk files for customer-PII shapes",
		Long: `Walks <cli-dir>'s high-risk file scope (JSON, YAML, Markdown,
*_test.go, .manuscripts/**, testdata/**) and reports per-line findings
that signal PII-shape leaks. Detection is purely mechanical: card
last-4 with context tokens, email addresses, US-shaped phone numbers,
ZIP+4, and street-address-line shapes. The agent layer
(skills/printing-press-polish/references/pii-polish.md) takes these
findings and applies judgment per item — fix in source or accept with
pre-decision fields.

Exit 0 by default (diagnostic). With --strict, exits non-zero when
pending findings or gate failures remain.`,
		Example: `  printing-press pii-audit ~/printing-press/library/dub
  printing-press pii-audit ~/printing-press/library/dub --json
  printing-press pii-audit ~/printing-press/library/dub --strict`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			// No mcp:read-only — RunPIIAudit writes a ledger file under
			// <cli-dir>, which is user-visible mutation outside the
			// local cache. Per AGENTS.md, a false readOnlyHint on a
			// mutating tool is a real bug.
			"pp:typed-exit-codes": "0,3",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cliDir := args[0]
			info, err := os.Stat(cliDir)
			if err != nil {
				return fmt.Errorf("cli-dir %q: %w", cliDir, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("cli-dir %q is not a directory", cliDir)
			}

			var result artifacts.PIIAuditResult
			if asJSON {
				// --json is a read-only probe; do not write the ledger.
				result, err = artifacts.ScanPII(cliDir)
			} else {
				result, err = artifacts.RunPIIAudit(cliDir)
			}
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(result.Findings); err != nil {
					return err
				}
			} else {
				renderPIIAuditTable(cmd.OutOrStdout(), result.Findings, result.Delta, result.Completion)
			}

			if strict && (artifacts.PIIPendingCount(result.Findings) > 0 || result.Completion.HasGateFailure()) {
				return &ExitError{
					Code: ExitGenerationError,
					Err: fmt.Errorf("pii-audit: %d pending finding(s), %d gate failure(s)",
						artifacts.PIIPendingCount(result.Findings), result.Completion.GateFailureCount()),
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable table")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero when pending findings or gate failures remain")
	return cmd
}

func renderPIIAuditTable(w io.Writer, findings []artifacts.PIIFinding, delta artifacts.PIILedgerDelta, completion artifacts.PIICompletionStatus) {
	pending := artifacts.PIIPendingCount(findings)
	accepted := len(findings) - pending
	gateFired := completion.HasGateFailure()

	switch {
	case pending == 0 && !gateFired:
		if accepted > 0 {
			fmt.Fprintf(w, "pii-audit: no pending findings (%d accepted) — phase-1 scope (card/email/phone/zip/postal); order-IDs, ASINs, and standalone names are a future detector class\n", accepted)
		} else {
			fmt.Fprintln(w, "pii-audit: no findings — phase-1 scope (card/email/phone/zip/postal); order-IDs, ASINs, and standalone names are a future detector class")
		}
	case pending == 0 && gateFired:
		fmt.Fprintf(w, "pii-audit: incomplete (%d accepted, %d gate failure(s))\n",
			accepted, completion.GateFailureCount())
	default:
		fmt.Fprintf(w, "pii-audit: %d pending finding(s)", pending)
		if accepted > 0 {
			fmt.Fprintf(w, " (%d accepted)", accepted)
		}
		if gateFired {
			fmt.Fprintf(w, ", %d gate failure(s)", completion.GateFailureCount())
		}
		fmt.Fprintln(w)
	}

	if delta.HasPrevious {
		fmt.Fprintf(w, "since last run: %d resolved, %d new\n", len(delta.Resolved), len(delta.Added))
	}

	if gateFired {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "incomplete: the run is not done yet")
		fmt.Fprintln(w, artifacts.FormatPIIGateFailures(completion))
	}

	if pending > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%-16s  %-6s  %-12s  %-40s  %s\n", "KIND", "LINE", "ID", "FILE", "MATCH")
		for _, f := range findings {
			if f.Status == artifacts.PIIStatusAccepted {
				continue
			}
			fmt.Fprintf(w, "%-16s  %-6d  %-12s  %-40s  %s\n",
				f.Kind, f.Line, artifacts.PIIFindingID(f),
				truncate(f.File, 40), truncate(f.MatchedSpan, 60))
		}
	}

	if completion.NextPending != nil {
		f := completion.NextPending
		fmt.Fprintln(w)
		fmt.Fprintf(w, "next: %s [%s] at %s:%d\n", f.Kind, artifacts.PIIFindingID(*f), f.File, f.Line)
	}
}
