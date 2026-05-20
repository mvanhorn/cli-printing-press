// refresh.go: Cobra wrapper + library function for forcing/refreshing the
// stored session against the API's refresh endpoint.
//
// Transport choice: this file uses net/http with a Chrome User-Agent rather
// than the surf-based Chrome TLS fingerprint client. The generated CLIs
// (internal/generator/templates/client.go.tmpl) embed surf directly, but
// surf is not imported by any internal/* package today, only by templates.
// Adding it here would either pull the dependency permanently into the
// non-generator side of the binary or require a thin wrapper, neither of
// which buys much for press-auth — refresh endpoints accept normal HTTPS
// clients with a plausible UA. If a target site starts requiring a Chrome
// TLS fingerprint at the refresh layer, swap this transport for surf
// behind the Refresh signature with no test-visible change.
//
// HTTP method is GET-only in v1. POST-with-body refresh shapes are a future
// extension; the plan calls them out as deferred and the catalog hasn't yet
// captured any vendor that needs them.

package pressauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// chromeUserAgent is the UA sent on refresh requests. Matches a recent
// Chrome stable release; pinning a specific build keeps the request shape
// stable across rebuilds. Bump as needed.
const chromeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

// refreshHTTPTimeout caps the total round-trip. Real refresh endpoints
// answer in well under a second; this is a safety net for hung TLS.
const refreshHTTPTimeout = 15 * time.Second

// expiryRefreshWindow is the lead time before JWT expiry that triggers a
// lazy refresh in the cookies subcommand. Below this threshold a refresh
// is issued; above it the cached header is returned as-is.
const expiryRefreshWindow = 60 * time.Second

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
			st, err := Load(domain)
			if err != nil {
				if errors.Is(err, ErrStateNotFound) {
					return &ExitError{
						Code: ExitNotCaptured,
						Err:  fmt.Errorf("no captured session for %s — run: press-auth login %s", domain, domain),
					}
				}
				return &ExitError{Code: ExitUnknownError, Err: err}
			}
			updated, err := Refresh(cmd.Context(), st)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Refreshed session for %s; JWT now expires at %s\n",
				updated.Domain, updated.JWTExpiry.Format(time.RFC3339))
			return nil
		},
	}
	return cmd
}

// refreshClient is overridable in tests so the suite never depends on
// network access. Tests inject an httptest-server-backed client; production
// uses a stdlib client with refreshHTTPTimeout.
var refreshClient = &http.Client{Timeout: refreshHTTPTimeout}

// Refresh calls state.RefreshEndpoint, merges any Set-Cookie response into
// state.Cookies, re-decodes the JWT carrier cookie to recompute JWTExpiry,
// and persists the updated State. On any auth-required signal (401, 403,
// redirect to /login) the on-disk state is left untouched and an
// ExitRefreshFailed is returned with a "press-auth login" recovery hint.
//
// v1 always issues GET. POST-with-refresh-body is deferred.
func Refresh(ctx context.Context, state *State) (*State, error) {
	if state == nil {
		return nil, &ExitError{Code: ExitUnknownError, Err: errors.New("nil state")}
	}
	if state.RefreshEndpoint == "" {
		return nil, &ExitError{
			Code: ExitMissingEndpoint,
			Err:  fmt.Errorf("no refresh endpoint captured for %s — run: press-auth login %s --refresh-endpoint <path>", state.Domain, state.Domain),
		}
	}

	target, err := resolveRefreshURL(state.Domain, state.RefreshEndpoint)
	if err != nil {
		return nil, &ExitError{Code: ExitUnknownError, Err: err}
	}

	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("building refresh request: %w", err)}
	}
	req.Header.Set("User-Agent", chromeUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if header := formatCookieHeader(state.Cookies); header != "" {
		req.Header.Set("Cookie", header)
	}

	// Disable automatic redirect-following so a 302 to /login is observable
	// and not silently turned into a 200 from the login page.
	client := refreshClientCopy()

	resp, err := client.Do(req)
	if err != nil {
		return nil, &ExitError{Code: ExitRefreshFailed, Err: fmt.Errorf("refresh request failed: %w", err)}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if isAuthRequiredStatus(resp) {
		return nil, &ExitError{
			Code: ExitRefreshFailed,
			Err:  fmt.Errorf("refresh failed (HTTP %d) — run: press-auth login %s again", resp.StatusCode, state.Domain),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ExitError{
			Code: ExitRefreshFailed,
			Err:  fmt.Errorf("refresh failed: HTTP %d from %s", resp.StatusCode, target),
		}
	}

	setCookies := resp.Cookies()
	if len(setCookies) == 0 {
		return nil, &ExitError{
			Code: ExitNoNewCookies,
			Err:  fmt.Errorf("refresh returned no new cookies for %s", state.Domain),
		}
	}

	merged := mergeCookies(state.Cookies, setCookies)
	updated := &State{
		Domain:           state.Domain,
		CapturedAt:       state.CapturedAt,
		Cookies:          merged,
		RefreshEndpoint:  state.RefreshEndpoint,
		JWTCarrierCookie: state.JWTCarrierCookie,
		JWTExpiry:        state.JWTExpiry,
	}

	if carrier := merged[state.JWTCarrierCookie]; carrier != "" {
		if token, err := ExtractJWT(carrier); err == nil {
			if claims, err := DecodeJWT(token); err == nil {
				if exp, err := Exp(claims); err == nil && !exp.IsZero() {
					updated.JWTExpiry = exp
				}
			}
		}
	}

	if err := Save(updated); err != nil {
		return nil, &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("persisting refreshed state: %w", err)}
	}
	return updated, nil
}

// refreshClientCopy returns a shallow-cloned client with redirect-following
// disabled. The package-level refreshClient is preserved so tests can keep
// swapping it via SetRefreshClient.
func refreshClientCopy() *http.Client {
	c := *refreshClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
}

// isAuthRequiredStatus reports whether the response signals that the
// captured session is no longer valid. 401 and 403 are explicit; redirects
// to a login URL are inferred from the Location header.
func isAuthRequiredStatus(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return true
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := strings.ToLower(resp.Header.Get("Location"))
		if loc != "" && (strings.Contains(loc, "/login") || strings.Contains(loc, "signin")) {
			return true
		}
	}
	return false
}

// resolveRefreshURL builds an absolute URL from the captured refresh
// endpoint. Absolute URLs (including different hosts) pass through; relative
// paths are resolved against https://<domain>.
func resolveRefreshURL(domain, endpoint string) (string, error) {
	if endpoint == "" {
		return "", errors.New("empty refresh endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parsing refresh endpoint: %w", err)
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	base := &url.URL{Scheme: "https", Host: domain}
	return base.ResolveReference(u).String(), nil
}

// mergeCookies returns a fresh map with the existing cookies plus any
// updates from setCookies. New cookies are added, existing names are
// overwritten with the new value, and cookies the server did not send are
// preserved. The input map is never mutated.
func mergeCookies(existing map[string]string, setCookies []*http.Cookie) map[string]string {
	out := make(map[string]string, len(existing)+len(setCookies))
	maps.Copy(out, existing)
	for _, c := range setCookies {
		if c == nil || c.Name == "" {
			continue
		}
		out[c.Name] = c.Value
	}
	return out
}

// formatCookieHeader produces a "name1=value1; name2=value2" header value
// with cookie names in lexicographic order. Empty input returns the empty
// string so callers can use a length check before setting the header.
func formatCookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	// stdlib sort imported via state.go's transitive set; keep this file
	// minimal and use a manual insertion-style sort to avoid extra imports.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(cookies[name])
	}
	return b.String()
}
