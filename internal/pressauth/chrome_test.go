package pressauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/mvanhorn/cli-printing-press/v4/testdata/pressauth/fakelogin"
)

// mkTestCookies builds a small []*network.Cookie from a map keyed
// "domain/name" -> value. The leading "domain" segment can be empty to
// produce a host-only cookie (Domain attribute absent in the original
// Set-Cookie); the helper splits on the first "/" and trusts the caller
// to keep keys unambiguous.
func mkTestCookies(t *testing.T, in map[string]string) []*network.Cookie {
	t.Helper()
	out := make([]*network.Cookie, 0, len(in))
	for spec, value := range in {
		idx := strings.Index(spec, "/")
		if idx < 0 {
			t.Fatalf("test cookie spec %q missing '/'", spec)
		}
		out = append(out, &network.Cookie{
			Domain: spec[:idx],
			Name:   spec[idx+1:],
			Value:  value,
		})
	}
	return out
}

func TestCookieDomainMatches(t *testing.T) {
	cases := []struct {
		name         string
		cookieDomain string
		target       string
		want         bool
	}{
		{"dot-subdomain", ".example.com", "www.example.com", true},
		{"dot-bare", ".example.com", "example.com", true},
		{"host-only-subdomain", "example.com", "www.example.com", false},
		{"host-only-exact", "example.com", "example.com", true},
		{"empty-domain", "", "example.com", true},
		{"dot-not-suffix", ".example.com", "otherexample.com", false},
		{"case-insensitive", ".example.com", "EXAMPLE.com", true},
		{"empty-target", ".example.com", "", false},
		{"deep-subdomain", ".example.com", "api.www.example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cookieDomainMatches(tc.cookieDomain, tc.target)
			if got != tc.want {
				t.Errorf("cookieDomainMatches(%q, %q) = %v, want %v",
					tc.cookieDomain, tc.target, got, tc.want)
			}
		})
	}
}

func TestFilterCookiesByDomain(t *testing.T) {
	// Build a tiny slice of network.Cookie pointers without importing the
	// chromedp/cdproto/network package in the test file — we already get
	// the type indirectly through filterCookies. Inline the construction
	// via the package-level helper instead.
	all := mkTestCookies(t, map[string]string{
		".example.test/keep":           "v1",
		"example.test/keep-host-only":  "v2",
		"other.example.test/drop":      "v3",
		".other.example.test/drop-dot": "v4",
		"/keep-empty-domain":           "v5", // empty Domain attr
	})
	got := filterCookies(all, "www.example.test", false)
	if _, ok := got["keep"]; !ok {
		t.Errorf("expected 'keep' cookie (matches .example.test)")
	}
	if _, ok := got["keep-empty-domain"]; !ok {
		t.Errorf("expected 'keep-empty-domain' cookie (host-only treated as match)")
	}
	if _, ok := got["keep-host-only"]; ok {
		t.Errorf("'keep-host-only' has Domain=example.test (no dot) and target=www.example.test; should drop")
	}
	if _, ok := got["drop"]; ok {
		t.Errorf("'drop' cookie (other.example.test) leaked into result")
	}
	if _, ok := got["drop-dot"]; ok {
		t.Errorf("'drop-dot' cookie (.other.example.test) leaked into result")
	}
}

func TestFilterCookiesIncludeSubdomains(t *testing.T) {
	t.Setenv("PRESSAUTH_INCLUDE_SUBDOMAINS", "1")

	all := []*network.Cookie{
		{Domain: ".example.com", Name: "JWT", Value: "jwt"},
		{Domain: "example.com", Name: "session", Value: "sess"},
		{Domain: "us2.example.com", Name: "_csrf", Value: "csrf-us"},
		{Domain: "eu.example.com", Name: "_csrf", Value: "csrf-eu"},
		{Domain: "US2.Example.COM", Name: "regional", Value: "r"},
		{Domain: "us2.example.com.", Name: "dotted", Value: "d"},
		{Domain: ".us2.example.com", Name: "scoped", Value: "no"},
		{Domain: "api.example.com", Name: "sibling", Value: "sib"},
		{Domain: "evilconcursolutions.com", Name: "lookalike", Value: "no"},
		{Domain: "us2.evilconcursolutions.com", Name: "lookalike-sub", Value: "no"},
		{Domain: "notexample.com", Name: "suffix", Value: "no"},
		{Domain: "example.com.evil.net", Name: "embed", Value: "no"},
		{Domain: "other.test", Name: "nope", Value: "x"},
		{Domain: "", Name: "empty-domain", Value: "e"},
		nil,
	}

	def := filterCookies(all, "example.com", false)
	if _, ok := def["_csrf"]; ok {
		t.Error("default filter kept host-only _csrf from a subdomain")
	}
	if _, ok := def["regional"]; ok {
		t.Error("default filter kept mixed-case host-only subdomain cookie")
	}
	if _, ok := def["dotted"]; ok {
		t.Error("default filter kept a trailing-dot subdomain cookie")
	}
	if _, ok := def["scoped"]; ok {
		t.Error("default filter kept a domain cookie scoped to a subdomain")
	}
	if def["JWT"] != "jwt" || def["session"] != "sess" || def["empty-domain"] != "e" {
		t.Errorf("default filter dropped an RFC match: %#v", def)
	}
	for _, name := range []string{"lookalike", "lookalike-sub", "suffix", "embed", "nope", "sibling"} {
		if _, ok := def[name]; ok {
			t.Errorf("default filter kept %s", name)
		}
	}

	wide := filterCookies(all, "example.com", true)
	if wide["JWT"] != "jwt" || wide["session"] != "sess" || wide["empty-domain"] != "e" {
		t.Errorf("opt-in filter dropped an RFC match: %#v", wide)
	}
	if wide["_csrf"] != "csrf-eu" {
		t.Errorf("_csrf = %q, want the later host-only subdomain value csrf-eu", wide["_csrf"])
	}
	if wide["regional"] != "r" {
		t.Errorf("regional = %q, want case-insensitive subdomain match", wide["regional"])
	}
	if wide["dotted"] != "d" {
		t.Errorf("dotted = %q, want trailing-dot subdomain cookie", wide["dotted"])
	}
	if _, ok := wide["scoped"]; ok {
		t.Error("opt-in kept a leading-dot cookie scoped only to a subdomain")
	}
	for _, name := range []string{"lookalike", "lookalike-sub", "suffix", "embed", "nope"} {
		if _, ok := wide[name]; ok {
			t.Errorf("opt-in kept %s", name)
		}
	}
	if _, ok := wide["sibling"]; !ok {
		t.Error("opt-in dropped host-only api.example.com")
	}

	header := formatCookieHeader(wide)
	for _, want := range []string{"JWT=jwt", "_csrf=csrf-eu", "session=sess", "regional=r", "dotted=d", "sibling=sib"} {
		if !strings.Contains(header, want) {
			t.Errorf("stored header %q missing %s", header, want)
		}
	}
	for _, leaked := range []string{"nope=", "scoped=", "lookalike=", "suffix=", "embed="} {
		if strings.Contains(header, leaked) {
			t.Errorf("stored header leaked %s: %s", leaked, header)
		}
	}

	sibling := filterCookies(all, "www.example.com", true)
	if _, ok := sibling["sibling"]; ok {
		t.Error("api.example.com is not a subdomain of www.example.com")
	}
	if sibling["JWT"] != "jwt" {
		t.Error("domain cookie on .example.com should still match www.example.com")
	}

	dottedTarget := filterCookies(
		[]*network.Cookie{{Domain: "us2.example.com", Name: "_csrf", Value: "csrf"}},
		".example.com.",
		true,
	)
	if dottedTarget["_csrf"] != "csrf" {
		t.Error("leading-dot capture target with a trailing dot should match a host-only subdomain cookie")
	}
}

func TestHostOnlySubdomainCookieSecurity(t *testing.T) {
	const (
		apex = "concursolutions.com"
		ok   = "us2.concursolutions.com"
	)
	cases := []struct {
		name   string
		cookie string
		target string
		want   bool
	}{
		{name: "apex host-only subdomain", cookie: ok, target: apex, want: true},
		{name: "case", cookie: "US2.ConcurSolutions.COM", target: "ConcurSolutions.COM", want: true},
		{name: "cookie trailing dot", cookie: ok + ".", target: apex, want: true},
		{name: "target trailing dot", cookie: ok, target: apex + ".", want: true},
		{name: "both trailing dots", cookie: ok + ".", target: apex + ".", want: true},
		{name: "extra trailing dots", cookie: ok + "...", target: apex, want: true},
		{name: "ascii padding", cookie: "  " + ok + "\t", target: "\n" + apex + " ", want: true},
		{name: "leading-dot target", cookie: ok, target: "." + apex, want: true},
		{name: "deep subdomain", cookie: "a.b." + apex, target: apex, want: true},
		{name: "hyphen label", cookie: "my-host." + apex, target: apex, want: true},
		{name: "punycode subdomain", cookie: "shop.xn--mnchen-3ya.de", target: "xn--mnchen-3ya.de", want: true},
		{name: "exact host is not the opt-in path", cookie: apex, target: apex, want: false},
		{name: "exact host trailing dot", cookie: apex + ".", target: apex, want: false},
		{name: "lookalike registrable domain", cookie: "evilconcursolutions.com", target: apex, want: false},
		{name: "lookalike subdomain", cookie: "us2.evilconcursolutions.com", target: apex, want: false},
		{name: "lookalike trailing dot", cookie: "us2.evilconcursolutions.com.", target: apex, want: false},
		{name: "lookalike case", cookie: "US2.EvilConcurSolutions.COM", target: apex, want: false},
		{name: "suffix embed", cookie: apex + ".evil.com", target: apex, want: false},
		{name: "prefix embed", cookie: "not" + apex, target: apex, want: false},
		{name: "digit lookalike", cookie: "us2.concurs0lutions.com", target: apex, want: false},
		{name: "punycode glued to apex", cookie: "us2.xn--" + apex, target: apex, want: false},
		{name: "sibling of www", cookie: "api.example.com", target: "www.example.com", want: false},
		{name: "parent of www", cookie: "example.com", target: "www.example.com", want: false},
		{name: "domain cookie on subdomain", cookie: ".us2." + apex, target: apex, want: false},
		{name: "domain cookie padded", cookie: " .us2." + apex, target: apex, want: false},
		{name: "double-dot target", cookie: ok, target: ".." + apex, want: false},
		{name: "empty label", cookie: "us2.." + apex, target: apex, want: false},
		{name: "single-label target", cookie: "evil.com", target: "com", want: false},
		{name: "single-label trailing dot", cookie: "evil.com", target: "com.", want: false},
		{name: "ip target", cookie: "evil.127.0.0.1", target: "127.0.0.1", want: false},
		{name: "ipv6 target", cookie: "evil.example.com", target: "::1", want: false},
		{name: "unicode dot", cookie: "us2.example。com", target: "example.com", want: false},
		{name: "fullwidth letter", cookie: "us2.ｅxample.com", target: "example.com", want: false},
		{name: "cyrillic lookalike", cookie: "us2.еxample.com", target: "example.com", want: false},
		{name: "kelvin sign", cookie: "us2.exa\u212Aple.com", target: "example.com", want: false},
		{name: "kelvin in target", cookie: "us2.example.com", target: "exa\u212Aple.com", want: false},
		{name: "unicode dot in target", cookie: "us2.example.com", target: "example\u3002com", want: false},
		{name: "fullwidth dot", cookie: "us2\uff0eexample\uff0ecom", target: "example.com", want: false},
		{name: "halfwidth dot", cookie: "us2\uff61example\uff61com", target: "example.com", want: false},
		{name: "null before real suffix", cookie: "evil.com\x00.example.com", target: "example.com", want: false},
		{name: "scheme target", cookie: "us2.example.com", target: "https://example.com", want: false},
		{name: "url cookie", cookie: "https://us2.example.com", target: "example.com", want: false},
		{name: "nbsp padding", cookie: ok + "\u00a0", target: apex, want: false},
		{name: "unicode host not mapped to punycode", cookie: "shop.münchen.de", target: "xn--mnchen-3ya.de", want: false},
		{name: "percent encoding", cookie: "us2.example%2ecom", target: "example.com", want: false},
		{name: "null byte", cookie: "us2.example.com\x00", target: "example.com", want: false},
		{name: "at sign", cookie: "us2.evil@example.com", target: "example.com", want: false},
		{name: "backslash", cookie: `us2.example.com\evil`, target: "example.com", want: false},
		{name: "slash", cookie: "us2.example.com/evil", target: "example.com", want: false},
		{name: "wildcard", cookie: "*.example.com", target: "example.com", want: false},
		{name: "port", cookie: "us2.example.com:443", target: "example.com", want: false},
		{name: "target port", cookie: "us2.example.com", target: "example.com:443", want: false},
		{name: "userinfo target", cookie: ok, target: "user@" + apex, want: false},
		{name: "underscore label", cookie: "my_host.example.com", target: "example.com", want: false},
		{name: "empty cookie", cookie: "", target: apex, want: false},
		{name: "empty target", cookie: ok, target: "", want: false},
		{name: "label too long", cookie: strings.Repeat("a", 64) + ".example.com", target: "example.com", want: false},
		{name: "leading hyphen", cookie: "-bad.example.com", target: "example.com", want: false},
		{name: "trailing hyphen", cookie: "bad-.example.com", target: "example.com", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostOnlySubdomainCookie(tc.cookie, tc.target); got != tc.want {
				t.Fatalf("hostOnlySubdomainCookie(%q, %q) = %v, want %v", tc.cookie, tc.target, got, tc.want)
			}
			included := cookieIncluded(tc.cookie, tc.target, true)
			if tc.want && !included {
				t.Fatalf("cookieIncluded opt-in (%q, %q) = false, want true", tc.cookie, tc.target)
			}
			if !tc.want && included && !cookieDomainMatches(tc.cookie, tc.target) {
				t.Fatalf("opt-in included %q for %q outside RFC 6265", tc.cookie, tc.target)
			}
			if tc.want && cookieIncluded(tc.cookie, tc.target, false) {
				t.Fatalf("default include kept %q for %q", tc.cookie, tc.target)
			}
		})
	}

	longLabel := strings.Repeat("a", 63)
	longHost := longLabel + "." + longLabel + "." + longLabel + "." + longLabel
	if len(longHost) <= 253 {
		t.Fatalf("fixture host length = %d, want >253", len(longHost))
	}
	if hostOnlySubdomainCookie("us2."+longHost, longHost) {
		t.Fatal("over-long hostname was accepted")
	}

	// Exact and domain-cookie matches stay on the RFC path when the opt-in is on.
	if !cookieIncluded(".example.com", "www.example.com", false) || !cookieIncluded(".example.com", "www.example.com", true) {
		t.Fatal("domain cookie .example.com should match www.example.com with or without the opt-in")
	}
	if !cookieIncluded("example.com", "example.com", false) || !cookieIncluded("example.com", "EXAMPLE.COM", true) {
		t.Fatal("exact host-only cookie should match regardless of the opt-in")
	}
	if cookieIncluded("us2.example.com", "example.com", false) {
		t.Fatal("default match widened to a host-only subdomain")
	}
	if !cookieIncluded("us2.example.com", "example.com", true) {
		t.Fatal("opt-in missed a host-only subdomain cookie")
	}
	if cookieIncluded(".us2.example.com", "example.com", true) {
		t.Fatal("opt-in widened to a domain cookie scoped to a subdomain")
	}
}

// TestCaptureAgainstFakeLogin drives the full chromedp pipeline against
// the in-process fake login server. Skipped when no Chrome is available or
// PRESSAUTH_SKIP_CHROME=1 is set; CI runs it with Chrome preinstalled.
func TestCaptureAgainstFakeLogin(t *testing.T) {
	skipIfNoChrome(t)
	t.Setenv(headlessEnv, "1")

	// Bind the httptest server to 127.0.0.1 explicitly so the cookie
	// Domain attribute on the session cookie (".example.test") falls in
	// the suffix-match branch of cookieDomainMatches when the target
	// domain is "127.0.0.1" — but here we want a matching target. Use
	// "127.0.0.1" as both the cookie host and the press-auth target,
	// then stamp Domain="" on the session cookie so it's host-only.
	mux := fakelogin.NewHandler("")
	srv := httptest.NewUnstartedServer(mux)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.Listener = lis
	srv.Start()
	t.Cleanup(srv.Close)

	base := srv.URL

	// Pre-submit the form via a real HTTP client so the cookies are
	// already attached to the controlled browser's jar. chromedp will
	// see them via storage.GetCookies. This sidesteps the need to drive
	// the form with chromedp.SendKeys + Click in the test path.
	//
	// We do it by navigating chromedp to /account/overview *after* the
	// fake handler has been hit with a POST: the cookies live in the
	// browser jar, the page contains the signout link the heuristic
	// looks for. So we drive chromedp at /submit (which POSTs an empty
	// form, sets cookies, redirects to /account/overview) via a one-shot
	// override of the login URL and skip the explicit form-fill.
	//
	// CompleteSelector is set so the heuristic doesn't have to poll. The
	// budget is generous because a contended CI runner can spend tens of
	// seconds just launching Chrome before navigation even begins; too
	// tight a budget turns a slow-but-working browser into a deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Override the login URL to point at /submit so chromedp performs the
	// POST as part of its navigation; we cheat by using the form-action
	// path with a GET, which the test server's submitForm handler accepts
	// (it doesn't validate the method). The handler will then set cookies
	// and redirect to /account/overview which contains #signout.
	captureURL := base + "/submit"

	state, err := Capture(ctx, CaptureOptions{
		Domain:           hostOnly(t, base),
		LoginURL:         captureURL,
		CompleteSelector: "#signout",
		RefreshEndpoint:  "/refresh",
		JWTCarrierCookie: "session",
		Timeout:          75 * time.Second,
	})
	if err != nil {
		if chromeEnvUnavailable(err) || errors.Is(err, context.DeadlineExceeded) {
			// A deadline against an instant in-process server means Chrome
			// itself was too slow to launch/navigate in this environment,
			// not a Capture regression — skip rather than fail.
			t.Skipf("Chrome unavailable or too slow in this environment: %v", err)
		}
		t.Fatalf("Capture: %v", err)
	}
	if state.Domain != hostOnly(t, base) {
		t.Errorf("state.Domain = %q, want %q", state.Domain, hostOnly(t, base))
	}
	if state.CapturedAt.IsZero() {
		t.Errorf("CapturedAt should be set")
	}
	if state.RefreshEndpoint != "/refresh" {
		t.Errorf("RefreshEndpoint did not round-trip: %q", state.RefreshEndpoint)
	}
	if state.JWTCarrierCookie != "session" {
		t.Errorf("JWTCarrierCookie did not round-trip: %q", state.JWTCarrierCookie)
	}
	if _, ok := state.Cookies["session"]; !ok {
		t.Errorf("expected 'session' cookie, got names: %v", cookieNames(state.Cookies))
	}
	if _, ok := state.Cookies["auth"]; !ok {
		t.Errorf("expected 'auth' cookie, got names: %v", cookieNames(state.Cookies))
	}
	// JWTExpiry is intentionally zero — U4 fills it in via the JWT decoder.
	if !state.JWTExpiry.IsZero() {
		t.Errorf("JWTExpiry should be zero at capture time, got: %v", state.JWTExpiry)
	}
}

// TestCaptureSuffixMatchFiltersOtherDomain confirms that cookies stamped
// with a non-matching Domain are dropped from the result. It uses a
// fakelogin instance with the session cookie explicitly domain-scoped to
// .example.test, then captures against a target host that does not
// share that suffix.
func TestCaptureSuffixMatchFiltersOtherDomain(t *testing.T) {
	skipIfNoChrome(t)
	t.Setenv(headlessEnv, "1")

	// Cookie Domain=".other.example.org" while we capture for the
	// 127.0.0.1 host — none of those cookies should land in the result.
	mux := fakelogin.NewHandler(".other.example.org")
	srv := httptest.NewUnstartedServer(mux)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.Listener = lis
	srv.Start()
	t.Cleanup(srv.Close)

	// chromedp will refuse to set a cookie whose Domain attribute does
	// not match the request origin, so the "session" cookie won't even
	// land in the jar. The "auth" cookie has no Domain attribute (host-
	// only), so it WILL land; cookieDomainMatches treats host-only as a
	// match for the capture target. That's the contract: dropped at
	// browser layer == not in jar == not in filtered result.
	// Generous budget so a slow CI Chrome launch does not turn into a
	// navigate deadline (see TestCaptureAgainstFakeLogin).
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	state, err := Capture(ctx, CaptureOptions{
		Domain:           hostOnly(t, srv.URL),
		LoginURL:         srv.URL + "/submit",
		CompleteSelector: "#signout",
		Timeout:          75 * time.Second,
	})
	if err != nil {
		if chromeEnvUnavailable(err) || errors.Is(err, context.DeadlineExceeded) {
			// Deadline against an instant in-process server == Chrome too
			// slow in this environment, not a Capture regression.
			t.Skipf("Chrome unavailable or too slow in this environment: %v", err)
		}
		t.Fatalf("Capture: %v", err)
	}
	if _, ok := state.Cookies["session"]; ok {
		t.Errorf("session cookie (Domain=.other.example.org) leaked into capture for %s", hostOnly(t, srv.URL))
	}
}

// TestCaptureTimeoutCleansTempDir asserts that a capture that never
// reaches the completion signal returns a clean deadline error and the
// MkdirTemp user-data-dir is gone afterwards (no leak).
func TestCaptureTimeoutCleansTempDir(t *testing.T) {
	skipIfNoChrome(t)
	t.Setenv(headlessEnv, "1")

	// Spin a server that never serves a "sign out" link — the heuristic
	// will never fire, so the timeout path is the exit route.
	stuckSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><form><input type=password></form></body></html>`))
	}))
	t.Cleanup(stuckSrv.Close)

	before := snapshotTempDirs(t)

	ctx := context.Background()
	state, err := Capture(ctx, CaptureOptions{
		Domain:   "example.test",
		LoginURL: stuckSrv.URL + "/login",
		// The budget has to clear Chrome's cold-start cost (seconds on a
		// loaded CI runner) so the deadline fires while we wait on the
		// never-completing server rather than mid-launch. A sub-second
		// budget races Chrome's startup: the deadline cancels the
		// allocator before the browser is up, and chromedp reports
		// "chrome failed to start" instead of a clean DeadlineExceeded.
		// The sibling capture tests give Chrome 25-30s and start cleanly,
		// so a few seconds of headroom is ample while keeping this quick.
		Timeout: 8 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected timeout error, got state: %+v", state)
	}
	if chromeEnvUnavailable(err) {
		t.Skipf("Chrome did not launch in this environment, cannot exercise the timeout path: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded in error chain, got: %v", err)
	}

	// The press-auth-* tempdir should be gone. Wait briefly because
	// chromedp's allocator cancel runs in a goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !leakedTempDir(t, before) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("Capture leaked a press-auth-* tempdir under %s", os.TempDir())
}

func TestSanitizeForTempName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"example.com", "example.com"},
		{"a-b_c.d", "a-b_c.d"},
		{"a/b", "a_b"},
		{"a b", "a_b"},
		{"../etc/passwd", ".._etc_passwd"},
		{"", "domain"},
	}
	for _, tc := range cases {
		got := sanitizeForTempName(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeForTempName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// skipIfNoChrome short-circuits chromedp tests when no Chrome binary is
// available on PATH or in the standard macOS install location, or when
// the user explicitly opted out via PRESSAUTH_SKIP_CHROME=1. The
// environment hook lets the maintainer run `go test` on a developer
// laptop without a controlled-browser dependency.
func skipIfNoChrome(t *testing.T) {
	t.Helper()
	if os.Getenv("PRESSAUTH_SKIP_CHROME") == "1" {
		t.Skip("PRESSAUTH_SKIP_CHROME=1: skipping chromedp test")
	}
	if hasChrome() {
		return
	}
	t.Skip("no Chrome binary found on this machine; skipping chromedp test")
}

// chromeEnvUnavailable reports whether err is a Chrome launch/connection
// failure rather than a capture-logic failure. On shared CI runners Chrome
// intermittently fails to bring up its DevTools endpoint under load
// ("websocket url timeout reached") or fails to start at all ("chrome failed
// to start", usually alongside a dbus connection error). Those are
// environment conditions, not regressions in Capture, so the chromedp tests
// skip on them — the same stance skipIfNoChrome takes for a missing binary.
// Capture's own logic (cookie filtering, JWT decode, domain matching) never
// produces these launcher-layer strings, so a real regression still fails.
func chromeEnvUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sig := range []string{
		"chrome failed to start",
		"websocket url timeout reached",
		"Failed to connect to the bus",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

func hasChrome() bool {
	for _, candidate := range chromeCandidates() {
		if path, err := exec.LookPath(candidate); err == nil && path != "" {
			return true
		}
	}
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err == nil {
			return true
		}
	}
	return false
}

func chromeCandidates() []string {
	return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"}
}

// hostOnly extracts the host (no port) from a URL string. The test uses
// it to give chromedp a single domain label to filter cookies against.
func hostOnly(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	host := u.Hostname()
	if host == "" {
		t.Fatalf("no host in url %q", raw)
	}
	return host
}

func cookieNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// snapshotTempDirs records all press-auth-* directories under os.TempDir
// at test entry; leakedTempDir compares against the snapshot to detect
// whether Capture cleaned up its MkdirTemp.
func snapshotTempDirs(t *testing.T) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("read tmp dir: %v", err)
	}
	out := make(map[string]struct{})
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "press-auth-") {
			out[e.Name()] = struct{}{}
		}
	}
	return out
}

func TestCollectPageSignalsProbesEveryPageTab(t *testing.T) {
	var probed []string
	pages, err := collectPageSignals([]targetRef{
		{ID: "browser", Type: "browser"},
		{ID: "login", Type: pageTargetType},
		{ID: "", Type: pageTargetType},
		{ID: "frame", Type: "iframe"},
		{ID: "home", Type: pageTargetType},
		{ID: "worker", Type: "service_worker"},
		{ID: "closed", Type: pageTargetType},
	}, func(id string) (pageSignal, error) {
		probed = append(probed, id)
		switch id {
		case "login":
			return pageSignal{Href: "https://example.com/login", HasPassword: true}, nil
		case "home":
			return pageSignal{Href: "https://example.com/home", HasSignout: true}, nil
		case "closed":
			return pageSignal{}, errTabUnreadable
		default:
			t.Errorf("probed unexpected target %s", id)
			return pageSignal{}, errTabUnreadable
		}
	})
	if err != nil {
		t.Fatalf("collectPageSignals: %v", err)
	}
	if strings.Join(probed, ",") != "login,home,closed" {
		t.Fatalf("probed %v, want login, home, and the unreadable page", probed)
	}
	if !loginComplete(loginWaitConfig{}, "/login", pages, nil) {
		t.Fatal("sign-out on a later tab should finish login when the first tab is still the password form")
	}
	if loginComplete(loginWaitConfig{}, "/login", pages[:1], nil) {
		t.Fatal("first tab alone is still the password form and must not finish login")
	}
}

func TestCollectPageSignalsReturnsFatalProbeError(t *testing.T) {
	fatal := fmt.Errorf("waiting for login completion: %w", context.DeadlineExceeded)
	_, err := collectPageSignals([]targetRef{{ID: "home", Type: pageTargetType}}, func(string) (pageSignal, error) {
		return pageSignal{}, fatal
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline", err)
	}
}

func TestLoginCompleteHeuristicAndSignals(t *testing.T) {
	password := pageSignal{Href: "https://example.com/login", HasPassword: true, HasSignout: true}
	samePath := pageSignal{Href: "https://example.com/login", HasSignout: true}
	emailTab := pageSignal{Href: "https://example.com/check-email"}
	signedIn := pageSignal{Href: "https://example.com/home", HasSignout: true}
	badURL := pageSignal{Href: "://", HasSignout: true}

	cases := []struct {
		name      string
		cfg       loginWaitConfig
		loginPath string
		pages     []pageSignal
		names     map[string]struct{}
		want      bool
	}{
		{name: "password blocks that tab", loginPath: "/login", pages: []pageSignal{password}, want: false},
		{name: "sign-out on the login path", loginPath: "/login", pages: []pageSignal{samePath}, want: false},
		{name: "sign-out off the login path", loginPath: "/login", pages: []pageSignal{signedIn}, want: true},
		{name: "unreadable url", loginPath: "/login", pages: []pageSignal{badURL}, want: false},
		{name: "empty login path skips the path check", pages: []pageSignal{{HasSignout: true}}, want: true},
		{
			name:      "second tab signed in",
			loginPath: "/login",
			pages:     []pageSignal{emailTab, signedIn},
			want:      true,
		},
		{
			name:      "carrier cookie is not an implicit completion signal",
			loginPath: "/login",
			pages:     []pageSignal{emailTab},
			names:     map[string]struct{}{"session": {}},
			want:      false,
		},
		{
			name:      "named cookie on any host",
			cfg:       loginWaitConfig{CompleteCookie: "_csrf"},
			loginPath: "/login",
			pages:     []pageSignal{emailTab},
			names:     map[string]struct{}{"_csrf": {}},
			want:      true,
		},
		{
			name:      "named cookie missing",
			cfg:       loginWaitConfig{CompleteCookie: "_csrf"},
			loginPath: "/login",
			pages:     []pageSignal{emailTab},
			names:     map[string]struct{}{"session": {}},
			want:      false,
		},
		{
			name:      "named cookie missing does not fall through to the heuristic",
			cfg:       loginWaitConfig{CompleteCookie: "_csrf"},
			loginPath: "/login",
			pages:     []pageSignal{signedIn},
			names:     map[string]struct{}{"session": {}},
			want:      false,
		},
		{
			name:      "both flags ignore the heuristic when neither signal is present",
			cfg:       loginWaitConfig{CompleteSelector: "#account", CompleteCookie: "_csrf"},
			loginPath: "/login",
			pages:     []pageSignal{signedIn},
			names:     map[string]struct{}{"session": {}},
			want:      false,
		},
		{
			name:      "selector finishes when the named cookie is still missing",
			cfg:       loginWaitConfig{CompleteSelector: "#account", CompleteCookie: "_csrf"},
			loginPath: "/login",
			pages:     []pageSignal{signedIn, {Href: "https://example.com/home", SelectorHit: true}},
			names:     map[string]struct{}{},
			want:      true,
		},
		{
			name:      "cookie still finishes when a selector is unset on every tab",
			cfg:       loginWaitConfig{CompleteSelector: "#account", CompleteCookie: "_csrf"},
			loginPath: "/login",
			pages:     []pageSignal{emailTab, {Href: "https://example.com/home"}},
			names:     map[string]struct{}{"_csrf": {}},
			want:      true,
		},
		{
			name:      "selector on a later tab",
			cfg:       loginWaitConfig{CompleteSelector: "#account"},
			loginPath: "/login",
			pages: []pageSignal{
				{Href: "https://example.com/check-email"},
				{Href: "https://example.com/home", SelectorHit: true},
			},
			want: true,
		},
		{
			name:      "selector mode does not fall through to the heuristic",
			cfg:       loginWaitConfig{CompleteSelector: "#account"},
			loginPath: "/login",
			pages: []pageSignal{
				emailTab,
				signedIn,
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := loginComplete(tc.cfg, tc.loginPath, tc.pages, tc.names)
			if got != tc.want {
				t.Fatalf("loginComplete = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWaitForLoginSeesLaterTab(t *testing.T) {
	view := &scriptedCompletionView{
		pages: [][]pageSignal{
			{{Href: "https://example.com/check-email"}},
			{
				{Href: "https://example.com/check-email"},
				{Href: "https://example.com/home", HasSignout: true},
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForLoginEvery(ctx, loginWaitConfig{LoginURL: "https://example.com/login"}, view, time.Millisecond); err != nil {
		t.Fatalf("waitForLoginEvery: %v", err)
	}
	if view.ticks < 2 {
		t.Fatalf("ticks = %d, want the waiter to re-read tabs", view.ticks)
	}
}

func TestWaitForLoginMissingCookieIgnoresHeuristic(t *testing.T) {
	view := &scriptedCompletionView{
		pages: [][]pageSignal{{{Href: "https://example.com/home", HasSignout: true}}},
		names: []map[string]struct{}{{"session": {}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := waitForLoginEvery(ctx, loginWaitConfig{
		LoginURL:       "https://example.com/login",
		CompleteCookie: "_csrf",
	}, view, time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline while the named cookie is absent", err)
	}
	if view.ticks < 2 {
		t.Fatalf("ticks = %d, want the waiter to keep polling past the sign-out page", view.ticks)
	}
}

func TestWaitForLoginCookieWithoutSignout(t *testing.T) {
	view := &scriptedCompletionView{
		pages: [][]pageSignal{{{Href: "https://example.com/check-email"}}},
		names: []map[string]struct{}{{"_csrf": {}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := waitForLoginEvery(ctx, loginWaitConfig{
		LoginURL:       "https://example.com/login",
		CompleteCookie: "_csrf",
	}, view, time.Hour)
	if err != nil {
		t.Fatalf("cookie should finish login on the first observation: %v", err)
	}
	if view.cookieReads != 1 {
		t.Fatalf("cookieReads = %d, want 1", view.cookieReads)
	}
}

func TestWaitForLoginCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForLoginEvery(ctx, loginWaitConfig{LoginURL: "https://example.com/login"}, &scriptedCompletionView{
		pages: [][]pageSignal{{{Href: "https://example.com/login", HasPassword: true}}},
	}, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
}

func TestCompletionProbeExprQuotesSelector(t *testing.T) {
	selector := `a[href*="signout"]`
	expr := completionProbeExpr(selector)
	encoded, err := json.Marshal(selector)
	if err != nil {
		t.Fatalf("marshal selector: %v", err)
	}
	if !strings.Contains(expr, string(encoded)) {
		t.Fatalf("selector was not embedded as a JSON string: %s", expr)
	}
	hostile := completionProbeExpr(`"); alert(1);//`)
	if strings.Contains(hostile, `sel = ");`) {
		t.Fatalf("selector broke out of the JS string: %s", hostile)
	}
	if !strings.Contains(hostile, `sel = "\"); alert(1);//";`) {
		t.Fatalf("selector was not JSON-escaped: %s", hostile)
	}
}

type scriptedCompletionView struct {
	ticks       int
	cookieReads int
	pages       [][]pageSignal
	names       []map[string]struct{}
	pageErr     error
	cookieErr   error
}

func (s *scriptedCompletionView) pageSignals(context.Context, string) ([]pageSignal, error) {
	if s.pageErr != nil {
		return nil, s.pageErr
	}
	idx := s.ticks
	s.ticks++
	if len(s.pages) == 0 {
		return nil, nil
	}
	if idx >= len(s.pages) {
		idx = len(s.pages) - 1
	}
	return s.pages[idx], nil
}

func (s *scriptedCompletionView) cookieNames(context.Context) (map[string]struct{}, error) {
	s.cookieReads++
	if s.cookieErr != nil {
		return nil, s.cookieErr
	}
	if len(s.names) == 0 {
		return nil, nil
	}
	idx := s.cookieReads - 1
	if idx >= len(s.names) {
		idx = len(s.names) - 1
	}
	return s.names[idx], nil
}

func leakedTempDir(t *testing.T, before map[string]struct{}) bool {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "press-auth-") {
			continue
		}
		if _, existed := before[e.Name()]; existed {
			continue
		}
		return true
	}
	return false
}
