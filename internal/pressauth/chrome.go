package pressauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/cdproto/target"
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

// pageTargetType is the CDP target type for a tab's document. "tab" wrappers,
// iframes, and workers are not pages and are not completion candidates.
const pageTargetType = "page"

// errTabUnreadable marks one page tab whose probe failed for a reason other
// than the capture deadline. The completion check skips that tab and keeps
// evaluating the rest.
var errTabUnreadable = errors.New("tab unreadable")

// CaptureOptions carries the user-supplied configuration into the
// chromedp launcher. Domain is the target the captured cookies will be
// stored under; LoginURL is what the controlled window first navigates to.
type CaptureOptions struct {
	Domain            string
	LoginURL          string
	CompleteSelector  string
	CompleteCookie    string
	IncludeSubdomains bool
	RefreshEndpoint   string
	JWTCarrierCookie  string
	Timeout           time.Duration
}

// Capture launches a controlled Chrome window via chromedp, navigates to
// opts.LoginURL, waits for the login flow to complete (a named
// complete-cookie, a complete-selector on any page tab, or, when neither
// flag is set, the sign-out heuristic on any page tab), snapshots every
// cookie in the browser,
// filters them to the target domain, and returns the resulting *State.
// The temp user-data-dir is deleted on both success and error paths so
// the user's daily Chrome profile is never touched.
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
	defer removeTempDirEventually(userDataDir, 5*time.Second)

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

	filtered := filterCookies(cookies, opts.Domain, opts.IncludeSubdomains)
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

// loginWaitConfig is the subset of CaptureOptions the completion check reads.
type loginWaitConfig struct {
	LoginURL         string
	CompleteSelector string
	CompleteCookie   string
}

// pageSignal is one page tab's completion probe, independent of CDP.
type pageSignal struct {
	Href        string
	HasPassword bool
	HasSignout  bool
	SelectorHit bool
}

// targetRef is a browser target reduced to the fields the completion check
// needs, so tests can feed a target list without a Chrome session.
type targetRef struct {
	ID   string
	Type string
}

// completionView is the capture browser surface the login check reads.
// Production uses chromedp; tests substitute a scripted view.
type completionView interface {
	pageSignals(ctx context.Context, selector string) ([]pageSignal, error)
	cookieNames(ctx context.Context) (map[string]struct{}, error)
}

// waitForCompletion blocks until the login flow has finished or ctx ends.
func waitForCompletion(ctx context.Context, opts CaptureOptions) error {
	return waitForLogin(ctx, loginWaitConfig{
		LoginURL:         opts.LoginURL,
		CompleteSelector: opts.CompleteSelector,
		CompleteCookie:   strings.TrimSpace(opts.CompleteCookie),
	}, newChromedpCompletionView())
}

// waitForLogin polls view until loginComplete reports done. The first check
// runs immediately so a cookie or selector that is already present does not
// wait out the interval.
func waitForLogin(ctx context.Context, cfg loginWaitConfig, view completionView) error {
	return waitForLoginEvery(ctx, cfg, view, time.Second)
}

func waitForLoginEvery(ctx context.Context, cfg loginWaitConfig, view completionView, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	// The sign-out heuristic compares paths. Either completion flag replaces
	// that heuristic, so an unparseable login URL only matters when neither
	// flag is set and the heuristic can still fire.
	loginPath := ""
	if cfg.CompleteSelector == "" && cfg.CompleteCookie == "" {
		parsed, err := url.Parse(cfg.LoginURL)
		if err != nil {
			return fmt.Errorf("parsing login URL: %w", err)
		}
		loginPath = parsed.Path
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		done, err := completionTick(ctx, cfg, loginPath, view)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for login completion: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func completionTick(ctx context.Context, cfg loginWaitConfig, loginPath string, view completionView) (bool, error) {
	pages, err := view.pageSignals(ctx, cfg.CompleteSelector)
	if err != nil {
		return false, err
	}
	var names map[string]struct{}
	if cfg.CompleteCookie != "" {
		names, err = view.cookieNames(ctx)
		if err != nil {
			return false, err
		}
	}
	return loginComplete(cfg, loginPath, pages, names), nil
}

// loginComplete decides whether this observation finishes login.
//
//   - A configured complete-cookie finishes login when that name is in the
//     browser, on any host. It does not consult the JWT carrier cookie;
//     operators pass the same name to both flags when they want that.
//   - A configured complete-selector finishes login when it is visible on
//     any page tab.
//   - When either flag is set, the sign-out heuristic does not run. When
//     both are set, either signal is enough.
//   - Otherwise the sign-out heuristic finishes login when any page tab
//     matches. A password field on one tab does not block a different tab.
func loginComplete(cfg loginWaitConfig, loginPath string, pages []pageSignal, cookieNames map[string]struct{}) bool {
	cookieSet := cfg.CompleteCookie != ""
	selectorSet := cfg.CompleteSelector != ""
	if cookieSet {
		if _, ok := cookieNames[cfg.CompleteCookie]; ok {
			return true
		}
	}
	if selectorSet {
		for _, page := range pages {
			if page.SelectorHit {
				return true
			}
		}
	}
	if cookieSet || selectorSet {
		return false
	}
	for _, page := range pages {
		if heuristicMatches(page, loginPath) {
			return true
		}
	}
	return false
}

// heuristicMatches is the historical single-page check: no password field,
// a sign-out or log-out link or button, and a path different from the login
// URL when that URL has a path.
func heuristicMatches(page pageSignal, loginPath string) bool {
	if page.HasPassword || !page.HasSignout {
		return false
	}
	if loginPath == "" {
		return true
	}
	cur, err := url.Parse(page.Href)
	if err != nil {
		return false
	}
	return cur.Path != loginPath
}

// collectPageSignals probes every page tab. Non-page targets are ignored.
// errTabUnreadable skips one tab; any other error stops the round.
func collectPageSignals(targets []targetRef, probe func(id string) (pageSignal, error)) ([]pageSignal, error) {
	pages := make([]pageSignal, 0, len(targets))
	for _, tgt := range targets {
		if tgt.Type != pageTargetType || tgt.ID == "" {
			continue
		}
		sig, err := probe(tgt.ID)
		if err != nil {
			if errors.Is(err, errTabUnreadable) {
				continue
			}
			return nil, err
		}
		pages = append(pages, sig)
	}
	return pages, nil
}

func completionProbeExpr(selector string) string {
	encoded, err := json.Marshal(selector)
	if err != nil {
		encoded = []byte(`""`)
	}
	return fmt.Sprintf(`({
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
  selectorHit: (function () {
    var sel = %s;
    if (!sel) return false;
    var el;
    try { el = document.querySelector(sel); } catch (e) { return false; }
    if (!el) return false;
    return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  })(),
})`, encoded)
}

func completionDeadline(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("waiting for login completion: %w", err)
	}
	return nil
}

// chromedpCompletionView reads page tabs and cookie names from the capture
// browser attached to the context passed into each method.
type chromedpCompletionView struct {
	mu   sync.Mutex
	tabs map[target.ID]context.Context
}

func newChromedpCompletionView() *chromedpCompletionView {
	return &chromedpCompletionView{}
}

func (v *chromedpCompletionView) pageSignals(ctx context.Context, selector string) ([]pageSignal, error) {
	infos, err := chromedp.Targets(ctx)
	if err != nil {
		if fatal := completionDeadline(err); fatal != nil {
			return nil, fatal
		}
		return nil, nil
	}
	refs := make([]targetRef, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		refs = append(refs, targetRef{ID: string(info.TargetID), Type: info.Type})
	}
	return collectPageSignals(refs, func(id string) (pageSignal, error) {
		return v.probe(ctx, target.ID(id), selector)
	})
}

func (v *chromedpCompletionView) cookieNames(ctx context.Context) (map[string]struct{}, error) {
	cookies, err := snapshotCookies(ctx)
	if err != nil {
		if fatal := completionDeadline(err); fatal != nil {
			return nil, fatal
		}
		return nil, nil
	}
	names := make(map[string]struct{}, len(cookies))
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		names[c.Name] = struct{}{}
	}
	return names, nil
}

func (v *chromedpCompletionView) probe(ctx context.Context, id target.ID, selector string) (pageSignal, error) {
	runCtx := v.tabContext(ctx, id)
	var raw struct {
		Href        string `json:"href"`
		HasPassword bool   `json:"hasPassword"`
		HasSignout  bool   `json:"hasSignout"`
		SelectorHit bool   `json:"selectorHit"`
	}
	err := chromedp.Run(runCtx, chromedp.Evaluate(completionProbeExpr(selector), &raw))
	if err != nil {
		if fatal := completionDeadline(err); fatal != nil {
			return pageSignal{}, fatal
		}
		// Navigation and a closing tab both surface as evaluation errors.
		// Skip the tab; the deadline surfaces on the next select.
		return pageSignal{}, errTabUnreadable
	}
	return pageSignal{
		Href:        raw.Href,
		HasPassword: raw.HasPassword,
		HasSignout:  raw.HasSignout,
		SelectorHit: raw.SelectorHit,
	}, nil
}

func (v *chromedpCompletionView) tabContext(ctx context.Context, id target.ID) context.Context {
	if c := chromedp.FromContext(ctx); c != nil && c.Target != nil && c.Target.TargetID == id {
		return ctx
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tabs == nil {
		v.tabs = make(map[target.ID]context.Context)
	}
	if existing, ok := v.tabs[id]; ok {
		return existing
	}
	// Keep the attached context until the capture context ends. Cancelling it
	// now would close the operator's tab: chromedp's cancel path calls
	// Target.closeTarget.
	tabCtx, _ := chromedp.NewContext(ctx, chromedp.WithTargetID(id))
	v.tabs[id] = tabCtx
	return tabCtx
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
// state blob. includeSubdomains adds host-only cookies on strict
// subdomains; it does not change which domain cookies match.
func filterCookies(all []*network.Cookie, target string, includeSubdomains bool) map[string]string {
	out := make(map[string]string, len(all))
	for _, c := range all {
		if c == nil || !cookieIncluded(c.Domain, target, includeSubdomains) {
			continue
		}
		out[c.Name] = c.Value
	}
	return out
}

func cookieIncluded(cookieDomain, target string, includeSubdomains bool) bool {
	if cookieDomainMatches(cookieDomain, target) {
		return true
	}
	return includeSubdomains && hostOnlySubdomainCookie(cookieDomain, target)
}

// A leading dot is a domain cookie and stays on the RFC 6265 path, including
// a domain cookie scoped only to a subdomain.
func hostOnlySubdomainCookie(cookieDomain, target string) bool {
	cookieHost, ok := canonicalDNSHost(cookieDomain, false)
	if !ok {
		return false
	}
	targetHost, ok := canonicalDNSHost(target, true)
	if !ok {
		return false
	}
	if cookieHost == targetHost {
		return false
	}
	return strings.HasSuffix(cookieHost, "."+targetHost)
}

// canonicalDNSHost returns the ASCII hostname used for the opt-in subdomain
// check. Unicode case folding and IDNA mapping are not applied: a Kelvin
// sign, a unicode dot, or a confusable letter must not become a suffix of
// the capture domain. Chrome stores IDN hosts as punycode, which compares
// literally.
func canonicalDNSHost(raw string, allowLeadingDot bool) (string, bool) {
	host := trimASCIISpace(raw)
	if host == "" || !isASCII(host) {
		return "", false
	}
	host = strings.ToLower(host)
	if allowLeadingDot {
		host = strings.TrimPrefix(host, ".")
	}
	if host == "" || strings.HasPrefix(host, ".") {
		return "", false
	}
	host = strings.TrimRight(host, ".")
	if host == "" || strings.Contains(host, "..") || len(host) > 253 || net.ParseIP(host) != nil {
		return "", false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if !validDNSLabel(label) {
			return "", false
		}
	}
	return host, true
}

func trimASCIISpace(s string) string {
	return strings.Trim(s, " \t\r\n\v\f")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func validDNSLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
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

func removeTempDirEventually(dir string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		_ = os.RemoveAll(dir)

		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
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
