package pressauth

import (
	"errors"
	"fmt"
	"time"

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

			state, err := Load(domain)
			if err != nil {
				if errors.Is(err, ErrStateNotFound) {
					return &ExitError{
						Code: ExitNotCaptured,
						Err:  fmt.Errorf("no captured session for %s — run: press-auth login %s", domain, domain),
					}
				}
				return &ExitError{Code: ExitUnknownError, Err: err}
			}

			// Lazy refresh applies only when refresh metadata is complete.
			// Cookie-only captures, including JWT-cookie sessions without a
			// refresh endpoint, should still print the stored cookie header.
			if state.JWTCarrierCookie != "" && state.RefreshEndpoint != "" && (state.JWTExpiry.IsZero() || time.Until(state.JWTExpiry) <= expiryRefreshWindow) {
				refreshed, refErr := Refresh(cmd.Context(), state)
				if refErr != nil {
					return refErr
				}
				state = refreshed
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatCookieHeader(state.Cookies))
			return nil
		},
	}

	return cmd
}
