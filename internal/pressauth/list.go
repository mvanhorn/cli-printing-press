package pressauth

import (
	"github.com/spf13/cobra"
)

func newListCmd(_ *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every captured domain with its capture time and JWT expiry",
		Long: `Walks the state directory (default ~/.press-auth/) and prints one row
per captured domain. Output includes the domain, when it was captured,
and when its stored JWT expires.

Use --json for machine-readable output suitable for piping into other
tools (e.g. a status dashboard).`,
		Example: `  press-auth list
  press-auth list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplementedExit("list")
		},
	}

	return cmd
}
