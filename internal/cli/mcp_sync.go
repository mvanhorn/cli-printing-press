package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline/mcpsync"
	"github.com/spf13/cobra"
)

func newMCPSyncCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "mcp-sync <cli-dir>",
		Short: "Migrate a printed CLI to the runtime Cobra-tree MCP surface",
		Long: `Regenerates the MCP surface (tools.go + tools-manifest.json + manifest.json)
from the spec, current generator templates, and any mcp-descriptions.json
overrides. Refreshes spec-derived fields in .printing-press.json so the
chain spec.yaml → .printing-press.json → manifest.json stays consistent.

Honors PRINTING_PRESS_LIBRARY_PUBLIC: when set to a public-library
clone, mcp-sync consults that clone's registry.json as a final fallback
for display_name when the spec and existing manifest.json don't carry
a usable brand name.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliDir, err := filepath.Abs(args[0])
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("resolving cli dir: %w", err)}
			}
			result, err := mcpsync.Sync(cliDir, mcpsync.Options{Force: force})
			if err != nil {
				if errors.Is(err, mcpsync.ErrHandEdited) {
					return &ExitError{Code: ExitUnknownError, Err: err}
				}
				return &ExitError{Code: ExitPublishError, Err: err}
			}
			for _, name := range result.UnmatchedOverrideKeys {
				// Surface override-file keys that didn't match any endpoint.
				// Common causes: typo in mcp-descriptions.json, stale key
				// after a resource/endpoint rename. Stderr-warn so the user
				// notices but the sync still succeeds — tools-audit will
				// flag any thin description that wasn't overridden.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: mcp-descriptions.json key %q does not match any tool in the spec\n", name)
			}
			if result.Changed {
				fmt.Fprintf(cmd.OutOrStdout(), "migrated MCP surface in %s\n", cliDir)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", result.Detail)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite generated MCP files even when tools.go lacks the generated marker")
	return cmd
}
