package pressauth

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ForgetFlags holds the forget subcommand's flag values.
type ForgetFlags struct {
	All bool
	Yes bool
}

func newForgetCmd(_ *GlobalFlags) *cobra.Command {
	ff := &ForgetFlags{}

	cmd := &cobra.Command{
		Use:   "forget [domain]",
		Short: "Remove captured state and keychain entries for a domain",
		Long: `Deletes the on-disk state file for <domain> and removes the matching
keychain entry that held its encryption key.

Use --all to forget every captured domain at once; --all requires --yes
unless run from a TTY, where it will prompt for interactive confirmation.

forget is idempotent: forgetting a domain that has no state succeeds and
prints a one-line "nothing to forget" notice.`,
		Example: `  press-auth forget example.com
  press-auth forget --all --yes`,
		Args: func(cmd *cobra.Command, args []string) error {
			if ff.All {
				if len(args) > 0 {
					return fmt.Errorf("--all cannot be combined with a domain argument")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ff.All {
				domain := args[0]
				if domain == "" {
					return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
				}
			}
			return notImplementedExit("forget")
		},
	}

	cmd.Flags().BoolVar(&ff.All, "all", false, "Forget every captured domain")
	cmd.Flags().BoolVar(&ff.Yes, "yes", false, "Skip the interactive confirmation prompt (required with --all in non-TTY contexts)")

	return cmd
}
