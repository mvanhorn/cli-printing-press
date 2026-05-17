package pressauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// LoginFlags holds the login subcommand's flag values. Kept as a struct
// (rather than closure-captured locals) so the launch helper can take a
// pointer without reshuffling Cobra wiring.
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
			return runLogin(cmd.Context(), domain, lf)
		},
	}

	cmd.Flags().StringVar(&lf.LoginURL, "login-url", "", "URL press-auth opens in the controlled Chrome window for you to log in")
	cmd.Flags().StringVar(&lf.CompleteSelector, "complete-selector", "", "Optional CSS selector that signals login is complete (e.g. \"a[href*=signout]\")")
	cmd.Flags().StringVar(&lf.RefreshEndpoint, "refresh-endpoint", "", "API path press-auth GETs to refresh an expiring JWT (e.g. /account/token)")
	cmd.Flags().StringVar(&lf.JWTCarrierCookie, "jwt-carrier-cookie", "", "Name of the cookie that holds the JWT; its exp claim drives refresh timing")
	cmd.Flags().BoolVar(&lf.Force, "force", false, "Overwrite existing state for <domain> without prompting")

	return cmd
}

// runLogin runs the body of `press-auth login`: flag validation, an
// existing-state precondition check, the chromedp capture, and the
// Save call. The Cobra layer wraps any returned error in the standard
// exit-code envelope.
func runLogin(ctx context.Context, domain string, lf *LoginFlags) error {
	if lf.LoginURL == "" {
		return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("--login-url is required")}
	}
	if err := validateLoginURL(lf.LoginURL); err != nil {
		return &ExitError{Code: ExitUsageError, Err: err}
	}

	// Refuse to clobber an existing captured session unless --force is set.
	if existing, err := Load(domain); err == nil && existing != nil && !lf.Force {
		return &ExitError{
			Code: ExitUsageError,
			Err:  fmt.Errorf("state already exists for %s — re-run with: press-auth login %s --force", domain, domain),
		}
	} else if err != nil && !errors.Is(err, ErrStateNotFound) {
		return &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("checking existing state: %w", err)}
	}

	if ctx == nil {
		ctx = context.Background()
	}
	state, err := Capture(ctx, CaptureOptions{
		Domain:           domain,
		LoginURL:         lf.LoginURL,
		CompleteSelector: lf.CompleteSelector,
		RefreshEndpoint:  lf.RefreshEndpoint,
		JWTCarrierCookie: lf.JWTCarrierCookie,
		Timeout:          defaultCaptureTimeout,
	})
	if err != nil {
		return &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("capture failed: %w", err)}
	}
	if len(state.Cookies) == 0 {
		return &ExitError{
			Code: ExitNoNewCookies,
			Err:  fmt.Errorf("no cookies matched domain %s — check the login flow really set cookies for that host", domain),
		}
	}
	// Defensive: never persist a state with a zero CapturedAt.
	if state.CapturedAt.IsZero() {
		state.CapturedAt = time.Now().UTC()
	}
	if err := Save(state); err != nil {
		return &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("saving state: %w", err)}
	}

	carrier := lf.JWTCarrierCookie
	if carrier == "" {
		carrier = "(none configured)"
	}
	fmt.Fprintf(os.Stdout, "captured %d cookies for %s; JWT carrier: %s (expiry will be set on first refresh)\n", len(state.Cookies), domain, carrier)
	return nil
}

// validateLoginURL rejects anything that is not http://localhost,
// http://127.0.0.1, or https://<host>. http:// elsewhere would silently
// leak any captured cookie to a network sniffer and the user is unlikely
// to intend it. We do not validate the path; that is the API's problem.
func validateLoginURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("--login-url is not a valid URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" {
			return nil
		}
		return fmt.Errorf("--login-url uses http://; only https:// is allowed (except for localhost/127.0.0.1)")
	default:
		return fmt.Errorf("--login-url must use http or https, got scheme %q", u.Scheme)
	}
}
