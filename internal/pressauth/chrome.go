package pressauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// headlessEnv lets tests and CI flip the controlled Chrome window off so a
// real browser never appears on the user's desktop. Production callers
// leave this unset; the launcher then runs headed.
const headlessEnv = "PRESSAUTH_HEADLESS"

const noSandboxEnv = "PRESSAUTH_CHROME_NO_SANDBOX"

// defaultCaptureTimeout is the upper bound on the interactive login flow.
// Ten minutes is the documented contract for the user: enough room for
// multi-factor prompts, password manager dances, and Bitwarden unlocks,
// short enough that an abandoned terminal session does not hold a
// controlled Chrome window open indefinitely.
const defaultCaptureTimeout = 10 * time.Minute

// CaptureOptions carries the user-supplied configuration into the
// chromedp launcher. Domain is the target the captured cookies will be
// stored under; LoginURL is what the controlled window first navigates to.
type CaptureOptions struct {
	Domain           string
	LoginURL         string
	CompleteSelector string
	RefreshEndpoint  string
	JWTCarrierCookie string
	Timeout          time.Duration
}

// Capture launches a controlled Chrome window via chromedp, navigates to
// opts.LoginURL, waits for the login flow to complete (either an explicit
// CompleteSelector becoming visible or the built-in heuristic firing),
// snapshots every cookie in the browser, filters them to the target
// domain, and returns the resulting *State. The temp user-data-dir is
// deleted on both success and error paths so the user's daily Chrome
// profile is never touched.
//
// Capture does not write to disk. The caller is responsible for handing
// the returned State to Save once it confirms the capture succeeded.
func Capture(ctx context.Context, opts CaptureOptions) (*State, error) {
	if opts.Domain == "" {
		return nil, errors.New("capture: empty domain")
	}
	if opts.LoginURL == "" {
		return nil, errors.New("capture: empty login URL")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultCaptureTimeout
	}

	userDataDir, err := os.MkdirTemp("", "press-auth-"+sanitizeForTempName(opts.Domain)+"-")
	if err != nil {
		return nil, fmt.Errorf("creating temp user-data-dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(userDataDir) }()

	// Start from chromedp's recommended defaults (Puppeteer-style
	// stability flags) and tack on the press-auth specifics: pinned
	// temp UserDataDir so we never touch the user's daily Chrome
	// profile, and a Flag("headless", ...) that the env var can flip
	// for CI. The Flag override comes after the defaults so it wins.
	allocOpts := make([]chromedp.ExecAllocatorOption, 0, len(chromedp.DefaultExecAllocatorOptions)+4)
	allocOpts = append(allocOpts, chromedp.DefaultExecAllocatorOptions[:]...)
	allocOpts = append(allocOpts,
		chromedp.UserDataDir(userDataDir),
		chromedp.Flag("headless", isHeadless()),
	)
	if shouldDisableChromeSandbox() {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)

	timedCtx, cancelTimeout := context.WithTimeout(browserCtx, timeout)
	defer func() {
		cancelTimeout()
		shutdownCtx, cancelShutdown := context.WithTimeout(browserCtx, 5*time.Second)
		defer cancelShutdown()
		_ = chromedp.Cancel(shutdownCtx)
		cancelBrowser()
		cancelAlloc()
	}()

	if err := chromedp.Run(timedCtx, chromedp.Navigate(opts.LoginURL)); err != nil {
		return nil, classifyChromeErr("navigate", err)
	}

	if err := waitForCompletion(timedCtx, opts); err != nil {
		return nil, err
	}

	cookies, err := snapshotCookies(timedCtx)
	if err != nil {
		return nil, classifyChromeErr("snapshot cookies", err)
	}

	filtered := filterCookies(cookies, opts.Domain)
	var jwtExpiry time.Time
	if opts.JWTCarrierCookie != "" {
		if token, err := ExtractJWT(filtered[opts.JWTCarrierCookie]); err == nil {
			if claims, err := DecodeJWT(token); err == nil {
				if exp, err := Exp(claims); err == nil && !exp.IsZero() {
					jwtExpiry = exp
				}
			}
		}
	}
	state := &State{
		Domain:           opts.Domain,
		CapturedAt:       time.Now().UTC(),
		Cookies:          filtered,
		RefreshEndpoint:  opts.RefreshEndpoint,
		JWTCarrierCookie: opts.JWTCarrierCookie,
		JWTExpiry:        jwtExpiry,
	}
	return state, nil
}

// waitForCompletion blocks until the login flow has finished. The
// explicit-selector branch is preferred when the caller passes one; the
// heuristic branch is the fallback. Both surface the underlying timeout
// from timedCtx so the caller sees a uniform deadline error.
func waitForCompletion(ctx context.Context, opts CaptureOptions) error {
	if opts.CompleteSelector != "" {
		if err := chromedp.Run(ctx, chromedp.WaitVisible(opts.CompleteSelector, chromedp.ByQuery)); err != nil {
			return classifyChromeErr("waiting for completion selector", err)
		}
		return nil
	}
	return waitHeuristic(ctx, opts.LoginURL)
}

// waitHeuristic polls the current page every second and declares login
// complete when (a) the URL has moved off the login page, (b) no
// password input is visible, and (c) a "sign out" / "logout" link is
// visible. The heuristic gives up at the timeout encoded in ctx.
func waitHeuristic(ctx context.Context, loginURL string) error {
	parsed, err := url.Parse(loginURL)
	if err != nil {
		return fmt.Errorf("parsing login URL: %w", err)
	}
	loginPath := parsed.Path
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for login completion: %w", ctx.Err())
		case <-ticker.C:
			done, err := heuristicTick(ctx, loginPath)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// heuristicTick runs one round of the heuristic checks against the live
// page. Returning (true, nil) means the caller should stop polling.
func heuristicTick(ctx context.Context, loginPath string) (bool, error) {
	var (
		currentURL   string
		hasPassword  bool
		hasSignoutEl bool
	)
	const probeScript = `({
  href: location.href,
  hasPassword: !!document.querySelector('input[type=password]'),
  hasSignout: (function () {
    var links = Array.from(document.querySelectorAll('a, button'));
    return links.some(function (el) {
      var t = (el.textContent || '').toLowerCase();
      var h = (el.getAttribute('href') || '').toLowerCase();
      return t.includes('sign out') || t.includes('signout') ||
             t.includes('log out') || t.includes('logout') ||
             h.includes('signout') || h.includes('logout');
    });
  })(),
})`
	var probe struct {
		Href        string `json:"href"`
		HasPassword bool   `json:"hasPassword"`
		HasSignout  bool   `json:"hasSignout"`
	}
	err := chromedp.Run(ctx, chromedp.Evaluate(probeScript, &probe))
	if err != nil {
		// Treat transient evaluation errors during navigation as
		// not-yet-ready, not a hard failure. A deadline error from the
		// surrounding context will surface in the next select round.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return false, fmt.Errorf("waiting for login completion: %w", err)
		}
		return false, nil
	}
	currentURL = probe.Href
	hasPassword = probe.HasPassword
	hasSignoutEl = probe.HasSignout

	if hasPassword {
		return false, nil
	}
	if !hasSignoutEl {
		return false, nil
	}
	if loginPath != "" {
		curParsed, err := url.Parse(currentURL)
		if err != nil {
			return false, nil
		}
		if curParsed.Path == loginPath {
			return false, nil
		}
	}
	return true, nil
}

// snapshotCookies returns every cookie the controlled browser knows
// about. We use storage.GetCookies (whole-browser) rather than
// network.GetCookies (current-URL only) because login flows commonly
// span multiple subdomains; the suffix-match filter below decides which
// of them belong to the target.
func snapshotCookies(ctx context.Context) ([]*network.Cookie, error) {
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		got, err := storage.GetCookies().Do(c)
		if err != nil {
			return err
		}
		cookies = got
		return nil
	}))
	return cookies, err
}

// filterCookies reduces the full browser cookie list to a name -> value
// map for the cookies whose Domain attribute matches the target. Values
// are never logged; the only place they should reach is the encrypted
// state blob.
func filterCookies(all []*network.Cookie, target string) map[string]string {
	out := make(map[string]string, len(all))
	for _, c := range all {
		if !cookieDomainMatches(c.Domain, target) {
			continue
		}
		out[c.Name] = c.Value
	}
	return out
}

// cookieDomainMatches applies RFC 6265 §5.1.3 domain matching:
//
//   - A cookie with Domain=".example.com" matches both "example.com"
//     and any subdomain (www.example.com, api.example.com, ...).
//   - A cookie with Domain="example.com" (no leading dot) only matches
//     the exact host "example.com".
//   - An empty cookie Domain (the Set-Cookie header omitted the Domain
//     attribute) is a host-only cookie for the origin that set it; press-
//     auth treats it as a match for the target because the controlled
//     browser only navigates to the target's pages.
//
// All comparisons are case-insensitive per RFC 6265 §5.1.2.
func cookieDomainMatches(cookieDomain, target string) bool {
	cookieDomain = strings.ToLower(strings.TrimSpace(cookieDomain))
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	if cookieDomain == "" {
		return true
	}
	if bare, ok := strings.CutPrefix(cookieDomain, "."); ok {
		if target == bare {
			return true
		}
		return strings.HasSuffix(target, "."+bare)
	}
	return cookieDomain == target
}

// isHeadless reads the PRESSAUTH_HEADLESS env var. "1", "true", "yes"
// (case-insensitive) all turn the controlled window into a headless
// Chrome process; anything else leaves it visible.
func isHeadless() bool {
	return truthyEnv(headlessEnv)
}

func shouldDisableChromeSandbox() bool {
	return truthyEnv(noSandboxEnv) || truthyEnv("CI")
}

func truthyEnv(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes"
}

// sanitizeForTempName makes a domain safe to embed in a temp-dir name.
// We only allow letters, digits, '.', '-', and '_' through; everything
// else collapses to '_' so a hostile domain string cannot escape the
// MkdirTemp template.
func sanitizeForTempName(s string) string {
	if s == "" {
		return "domain"
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// classifyChromeErr wraps a chromedp/cdproto error with stage context so
// the user can tell which phase of the capture failed. The wrapper
// avoids surfacing any cookie bytes that might have appeared in a more
// detailed underlying error.
func classifyChromeErr(stage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", stage, err)
}
