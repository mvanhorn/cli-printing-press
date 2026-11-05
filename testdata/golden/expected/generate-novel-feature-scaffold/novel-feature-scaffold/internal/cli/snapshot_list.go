// Copyright 2026 printing-press-golden and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newNovelSnapshotListCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List snapshots.",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:data-source": "auto", "pp:novel-scaffold": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "snapshot list")
			}
			// validate required flags here
			return fmt.Errorf("TODO: implement novel feature %q", "snapshot list")
		},
	}
	return cmd
}
