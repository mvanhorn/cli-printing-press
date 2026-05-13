package pressauth

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCookiesCmd(_ *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cookies <domain>",
		Short: "Print the cookie header for <domain> on stdout",
		Long: `Reads the captured session for <domain>, refreshes the JWT if it is
within 60 seconds of expiry, and prints the Cookie header value on stdout
in the form "name1=value1; name2=value2".

This is the command printed CLIs shell out to. The output is suitable for
piping directly into HTTP clients.

Prerequisite: run "press-auth login <domain>" first. If no state exists
for <domain>, this command exits with code 2 (not captured) and points
the user at the login subcommand.`,
		Example: `  press-auth cookies example.com
  curl -H "Cookie: $(press-auth cookies example.com)" https://example.com/api/me`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := args[0]
			if domain == "" {
				return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
			}
			return &ExitError{
				Code: ExitNotCaptured,
				Err:  fmt.Errorf("no captured session for %s — run: press-auth login %s (not implemented)", domain, domain),
			}
		},
	}

	return cmd
}
