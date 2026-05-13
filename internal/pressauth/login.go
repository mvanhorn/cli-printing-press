package pressauth

import (
	"fmt"

	"github.com/spf13/cobra"
)

// LoginFlags holds the login subcommand's flag values. Kept as a struct
// (rather than closure-captured locals) so U3 can extract a launch helper
// without reshuffling Cobra wiring.
type LoginFlags struct {
	LoginURL         string
	CompleteSelector string
	RefreshEndpoint  string
	JWTCarrierCookie string
	Force            bool
}

func newLoginCmd(_ *GlobalFlags) *cobra.Command {
	lf := &LoginFlags{}

	cmd := &cobra.Command{
		Use:   "login <domain>",
		Short: "Open a controlled Chrome window and capture the cookie session for <domain>",
		Long: `One-time interactive login flow.

press-auth launches its own controlled Chrome window (separate from your
daily Chrome profile, so your everyday browsing is untouched) and
navigates to the login URL. After you complete the login in that window,
press-auth captures the resulting cookies for <domain>, writes them to
encrypted on-disk state, and closes the window.

The captured cookies are then served to any printed CLI that calls
"press-auth cookies <domain>".

Provide --refresh-endpoint and --jwt-carrier-cookie when the API issues a
JWT session you want press-auth to refresh lazily on expiry.`,
		Example: `  press-auth login example.com --login-url https://example.com/login
  press-auth login example.com --login-url https://example.com/login \
      --refresh-endpoint /account/token --jwt-carrier-cookie session
  press-auth login example.com --login-url https://example.com/login \
      --complete-selector "a[href*=signout]"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := args[0]
			if domain == "" {
				return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
			}
			return notImplementedExit("login")
		},
	}

	cmd.Flags().StringVar(&lf.LoginURL, "login-url", "", "URL press-auth opens in the controlled Chrome window for you to log in")
	cmd.Flags().StringVar(&lf.CompleteSelector, "complete-selector", "", "Optional CSS selector that signals login is complete (e.g. \"a[href*=signout]\")")
	cmd.Flags().StringVar(&lf.RefreshEndpoint, "refresh-endpoint", "", "API path press-auth POSTs to when refreshing an expiring JWT (e.g. /account/token)")
	cmd.Flags().StringVar(&lf.JWTCarrierCookie, "jwt-carrier-cookie", "", "Name of the cookie that holds the JWT; its exp claim drives refresh timing")
	cmd.Flags().BoolVar(&lf.Force, "force", false, "Overwrite existing state for <domain> without prompting")

	return cmd
}
