// internal/cli/env.go
package cli

import (
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables for the project",
	}
	// Subcommands (check, set) will be added in P1.
	return cmd
}
