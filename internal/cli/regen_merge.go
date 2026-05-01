package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline/regenmerge"
	"github.com/spf13/cobra"
)

func newRegenMergeCmd() *cobra.Command {
	var (
		freshDir string
		apply    bool
		asJSON   bool
		force    bool
	)
	cmd := &cobra.Command{
		Use:           "regen-merge <cli-dir>",
		Short:         "Merge a fresh-generated CLI tree into a published library CLI without losing hand-edits",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Classify each Go file under <cli-dir>/internal and <cli-dir>/cmd by
comparing its top-level decl-set against a fresh-generated tree (--fresh).
Apply safe templated overwrites, restore lost AddCommand registrations in
root.go and resource-parents, and merge go.mod while preserving the
published module path.

Files with hand-edited additions (decls present in published but absent
from fresh) are flagged TEMPLATED-WITH-ADDITIONS and left untouched —
the human reviews via 'git diff' and merges by hand.

Default mode is --dry-run (classify and report only). Pass --apply to
write changes via stage-and-swap-with-recovery: changes stage to a
sibling tempdir, then a two-step rename atomically replaces the published
tree. Failure mid-rename surfaces a recovery path so original data is
never lost.

Supported on macOS and Linux. Windows is not supported (rename semantics
differ when files are held by editors). --apply requires a clean git
tree at <cli-dir> by default; --force overrides.`,
		Example: `  # Dry-run classification report against a fresh-generated tree:
  printing-press regen-merge ~/library/postman-explore --fresh /tmp/fresh-postman

  # Apply the safe changes:
  printing-press regen-merge ~/library/postman-explore --fresh /tmp/fresh-postman --apply

  # JSON output for piping into other tools:
  printing-press regen-merge ~/library/postman-explore --fresh /tmp/fresh-postman --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if freshDir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--fresh is required")}
			}
			cliDir, err := filepath.Abs(args[0])
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("resolving cli dir: %w", err)}
			}
			freshAbs, err := filepath.Abs(freshDir)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("resolving fresh dir: %w", err)}
			}

			report, err := regenmerge.Classify(cliDir, freshAbs, regenmerge.Options{Force: force})
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}

			if apply {
				if err := regenmerge.Apply(report, regenmerge.Options{Force: force}); err != nil {
					return &ExitError{Code: ExitPublishError, Err: err}
				}
			}

			if asJSON {
				return printJSONReport(cmd.OutOrStdout(), report)
			}
			return printHumanRegenReport(cmd.OutOrStderr(), report, apply)
		},
	}
	cmd.Flags().StringVar(&freshDir, "fresh", "", "Path to the fresh-generated CLI tree (required)")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply safe changes (default: dry-run)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit machine-readable JSON instead of the human report")
	cmd.Flags().BoolVar(&force, "force", false, "Override path-containment and dirty-tree safety checks")
	return cmd
}

func printJSONReport(w io.Writer, report *regenmerge.MergeReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printHumanRegenReport(w io.Writer, report *regenmerge.MergeReport, applied bool) error {
	mode := "DRY-RUN"
	if applied {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "regen-merge %s\n", mode)
	fmt.Fprintf(w, "  cli:   %s\n", report.CLIDir)
	fmt.Fprintf(w, "  fresh: %s\n\n", report.FreshDir)

	counts := map[regenmerge.Verdict]int{}
	for _, fc := range report.Files {
		counts[fc.Verdict]++
	}
	fmt.Fprintln(w, "verdicts:")
	for _, v := range []regenmerge.Verdict{
		regenmerge.VerdictTemplatedClean,
		regenmerge.VerdictNewTemplateEmission,
		regenmerge.VerdictPublishedOnlyTemplated,
		regenmerge.VerdictNovel,
		regenmerge.VerdictTemplatedWithAdditions,
		regenmerge.VerdictNovelCollision,
	} {
		fmt.Fprintf(w, "  %-26s %d\n", v, counts[v])
	}
	fmt.Fprintln(w)

	// Files needing human review.
	var needsReview []regenmerge.FileClassification
	for _, fc := range report.Files {
		if fc.Verdict == regenmerge.VerdictTemplatedWithAdditions || fc.Verdict == regenmerge.VerdictNovelCollision {
			needsReview = append(needsReview, fc)
		}
	}
	if len(needsReview) > 0 {
		fmt.Fprintln(w, "files needing human review:")
		for _, fc := range needsReview {
			fmt.Fprintf(w, "  %s [%s]\n", fc.Path, fc.Verdict)
			if fc.DeclSetDelta != nil {
				if len(fc.DeclSetDelta.InPublishedNotFresh) > 0 {
					fmt.Fprintf(w, "    in_published_not_fresh: %v\n", fc.DeclSetDelta.InPublishedNotFresh)
				}
				if len(fc.DeclSetDelta.InFreshNotPublished) > 0 {
					fmt.Fprintf(w, "    in_fresh_not_published: %v\n", fc.DeclSetDelta.InFreshNotPublished)
				}
			}
		}
		fmt.Fprintln(w)
	}

	// Lost registrations.
	if len(report.LostRegistrations) > 0 {
		fmt.Fprintln(w, "lost registrations:")
		for _, lr := range report.LostRegistrations {
			fmt.Fprintf(w, "  %s: %d call(s)\n", lr.HostFile, len(lr.Calls))
			for _, c := range lr.Calls {
				fmt.Fprintf(w, "    %s\n", c)
			}
			if len(lr.SkippedForMissingReferent) > 0 {
				fmt.Fprintf(w, "    skipped (referent missing in fresh+novels): %v\n", lr.SkippedForMissingReferent)
			}
		}
		fmt.Fprintln(w)
	}

	// go.mod summary.
	if report.GoMod != nil {
		fmt.Fprintln(w, "go.mod:")
		fmt.Fprintf(w, "  preserved module: %s\n", report.GoMod.PreservedModulePath)
		if len(report.GoMod.AddedRequires) > 0 {
			fmt.Fprintf(w, "  added requires:   %v\n", report.GoMod.AddedRequires)
		}
		if len(report.GoMod.PreservedReplaces) > 0 {
			fmt.Fprintf(w, "  preserved local replaces: %v\n", report.GoMod.PreservedReplaces)
		}
	}

	return nil
}
