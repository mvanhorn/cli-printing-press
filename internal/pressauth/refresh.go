package pressauth

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRefreshCmd(_ *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh <domain>",
		Short: "Force a JWT refresh for <domain>",
		Long: `Calls the refresh endpoint stored alongside the captured session,
parses Set-Cookie headers from the response, merges them into state, and
persists the updated expiry.

The "cookies" subcommand performs a refresh automatically when the JWT is
within 60 seconds of expiring; this command is the explicit form for
debugging or for use cases where you want a long-lived session warmed
before it would normally refresh.

Prerequisite: "press-auth login <domain>" must have captured a refresh
endpoint via --refresh-endpoint, otherwise this exits with code 6.`,
		Example: `  press-auth refresh example.com
  press-auth refresh example.com --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := args[0]
			if domain == "" {
				return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
			}
			return notImplementedExit("refresh")
		},
	}

	return cmd
}
