# Printing Press Retro: Pagliacci Pizza

## Session Stats
- API: Pagliacci Pizza (regional Seattle chain, undocumented Angular SPA + Azure REST API)
- Spec source: Browser sniff (browser-use with headed login) + JS bundle extraction
- Scorecard: 84/100 Grade A
- Verify pass rate: 98% (41/42)
- Fix loops: 0 (single generation after final spec)
- Manual code edits: 1 (root description rewrite)
- Features built from scratch: 0
- Spec regenerations: 2 (first without auth, second with full auth after headed login)

## Findings

### 1. Claude skipped browser-use three times despite skill instructions saying it's preferred (skill instruction compliance)

- **What happened:** The skill says "Step 2a: browser-use CLI capture (preferred)" but Claude chose different approaches three times across two APIs:
  1. **Domino's #1:** Used agent-browser auto-connect instead of browser-use. Auto-connect mode can navigate but can't access DOM via eval/snapshot — the sniff captured zero useful data.
  2. **Domino's #2:** Skipped browser-use entirely and went straight to `curl` after extracting endpoints from the JS bundle, bypassing the interactive sniff flow completely.
  3. **Pagliacci #1:** Same pattern — extracted 33 endpoints from the Angular bundle via curl, never opened browser-use.
  The user corrected this three times. A memory entry was created after the third correction.
- **Root cause:** The sniff-capture.md Step 1d (session transfer) had a recommendation "Chrome running → prefer agent-browser auto-connect" that Claude interpreted as the capture backend choice, not just the session transfer method. For the curl shortcut, there was no explicit instruction saying "don't skip browser-use by curling APIs directly." Claude optimized for speed over procedure compliance.
- **Cross-API check:** This is a Claude behavior pattern, not an API-specific issue. It will recur on every sniffed API until the skill instructions are unambiguous.
- **Frequency:** Every sniffed API.
- **Fallback if machine doesn't fix it:** User has to correct Claude. Reliability: never caught on its own — required explicit user correction all three times.
- **Worth a machine fix?** Yes. Already partially fixed (commit b337c12 updated sniff-capture.md). But needs stronger guardrails.
- **Inherent or fixable:** Fixable. The skill instruction was ambiguous. The fix is to make it unambiguous.
- **Durable fix:** Already applied in commit b337c12: "Session transfer vs capture are separate concerns. Use agent-browser for session transfer only. Always use browser-use for the actual capture." Additionally, add to the sniff-capture.md a cardinal rule at the top: "**NEVER skip browser-use for capture. Do NOT substitute curl probing, JS bundle grepping, or agent-browser auto-connect for a proper browser-use interactive sniff.**"
- **Test:** Run /printing-press on a website → verify browser-use is used for capture (not agent-browser or curl).
- **Evidence:** Three corrections across Domino's and Pagliacci sessions.

### 2. Sniff didn't visit authenticated pages — missed the entire account surface (skill instruction gap)

- **What happened:** On the first Pagliacci sniff attempt, Claude used browser-use with --profile "Default" but the session had expired (login page shown). Instead of recognizing this and offering headed login, Claude declared "session expired" and proceeded to build a spec from only the public endpoints. The entire authenticated surface (order history, rewards, saved addresses, stored coupons, customer profile) was missed. The user had to point out "wait why did session expire? you miss the point if we can't do the authenticated calls."
- **Root cause:** Two gaps: (a) The skill doesn't instruct Claude to verify login state after loading a profile and fallback to headed login if the session expired. (b) Claude treated expired session as "skip auth" rather than "try another auth method."
- **Cross-API check:** Session expiry is common — cookies expire, tokens rotate. Any site with authentication will hit this if the user's last login was hours/days ago.
- **Frequency:** Most sniffed APIs with user accounts.
- **Fallback if machine doesn't fix it:** User catches it. Reliability: sometimes — the user caught it here, but Claude didn't self-correct.
- **Worth a machine fix?** Yes.
- **Inherent or fixable:** Fixable. Add a session verification step after profile load.
- **Durable fix:** Add to sniff-capture.md Step 1d, after loading a Chrome profile:
  ```
  After profile loads, verify the session is active:
  1. Check for login/sign-in links vs account/profile links on the page
  2. If login link is visible (session expired), offer headed login:
     "Your session has expired. I'll open a visible browser so you can log in."
  3. Do NOT skip auth discovery because the profile session expired.
  ```
  Condition: AUTH_SESSION_AVAILABLE=true and profile loaded
  Guard: Skip when anonymous sniff
- **Test:** Load an expired Chrome profile → verify the skill offers headed login fallback.
- **Evidence:** "Sign Up / Sign In" visible after --profile load. Claude said "session expired" and moved on.

### 3. Auth header pattern discovery required manual XHR interception (discovered optimization)

- **What happened:** Pagliacci uses a custom auth scheme: `Authorization: PagliacciAuth {customerId}|{authToken}`. This was not discoverable from cookies alone (cookie replay returned 401). Claude had to install an XHR header interceptor, trigger an SPA client-side navigation (to avoid page reload resetting the interceptor), and capture the request headers to discover the pattern. This took 3 attempts.
- **Root cause:** The sniff-capture.md Step 2d (cookie auth validation) only tests cookie replay. Many APIs use custom Authorization headers derived from cookies, not cookie replay itself. The Angular app reads `customerId` and `authToken` cookies and constructs the `PagliacciAuth` header — a pattern invisible to cookie-only validation.
- **Cross-API check:** Custom Authorization headers are common. Many SPAs store tokens in cookies/localStorage and add them as Bearer/custom headers. This isn't just Pagliacci — any site where the frontend constructs auth headers from stored tokens will hit this.
- **Frequency:** API subclass: SPAs with custom auth headers constructed client-side — estimated 30-40% of modern web apps.
- **Fallback if machine doesn't fix it:** Claude must manually intercept headers. Reliability: sometimes — it took 3 attempts in this session (interceptor reset on navigation twice before Claude used SPA-internal navigation).
- **Worth a machine fix?** Yes. The XHR header interception pattern should be part of the standard sniff flow, not a manual debug exercise.
- **Inherent or fixable:** Fixable. Add auth header discovery to the sniff capture procedure.
- **Durable fix:** Add to sniff-capture.md as a new step between Step 2a.1.5 (auth flow) and Step 2d (cookie validation):
  ```
  Step 2a.1.6: Auth header discovery
  After visiting the first authenticated page, install an XHR/fetch header
  interceptor and trigger a client-side navigation (click a link, don't
  use browser-use open which reloads the page):
  
  browser-use eval "window.__authHeaders={};const _s=XMLHttpRequest.prototype.setRequestHeader;
  XMLHttpRequest.prototype.setRequestHeader=function(k,v){
    if(k.toLowerCase()==='authorization')window.__authHeaders[k]=v.substring(0,80);
    _s.apply(this,arguments)};'OK'"
  
  Then click a nav link to trigger API calls. Collect:
  browser-use eval "JSON.stringify(window.__authHeaders)"
  
  If an Authorization header is found:
  - Record the scheme (Bearer, PagliacciAuth, custom)
  - Record the token format
  - Determine where the token comes from (cookie, localStorage, sessionStorage)
  - Use this header format in Step 2d validation instead of cookie replay
  ```
  Condition: AUTH_SESSION_AVAILABLE=true and authenticated pages visited
  Guard: Skip when anonymous sniff or when auth is already known (e.g., Bearer token from spec)
- **Test:** Sniff a site with custom Authorization headers → verify the scheme is captured and used in auth validation.
- **Evidence:** Cookie replay returned 401. XHR interception revealed `PagliacciAuth 2432962|FD44DA6A...` pattern.

### 4. SPA interceptors reset on page navigation — need SPA-aware interception strategy (skill instruction gap)

- **What happened:** Fetch/XHR interceptors installed via `browser-use eval` were lost every time `browser-use open` navigated to a new URL (full page reload in Next.js/Angular SSR). This happened on both Domino's (GraphQL interceptor lost) and Pagliacci (header interceptor lost twice). The workaround was to use client-side navigation (click links) instead of `browser-use open` to keep the interceptor alive.
- **Root cause:** The sniff-capture.md doesn't distinguish between page navigation (resets JS context) and SPA navigation (preserves JS context). It instructs `browser-use open` for each page, which does a full navigation.
- **Cross-API check:** Every SPA (React, Angular, Next.js, Vue) will lose interceptors on full page navigation. Most modern web apps are SPAs.
- **Frequency:** Every sniffed SPA — which is most modern websites.
- **Fallback if machine doesn't fix it:** Claude must manually figure out SPA vs full navigation each time. Reliability: sometimes — Claude figured it out on the third attempt for Pagliacci but never on Domino's.
- **Worth a machine fix?** Yes.
- **Inherent or fixable:** Fixable. The sniff procedure should use click-based navigation for SPAs instead of browser-use open.
- **Durable fix:** Add to sniff-capture.md Step 2a.1, after installing interceptors:
  ```
  SPA Navigation Rule:
  After installing interceptors (fetch/XHR), do NOT use `browser-use open`
  to navigate between pages — it triggers a full page reload which resets
  the JS context and destroys the interceptors.
  
  Instead, use click-based SPA navigation:
    browser-use eval "document.querySelector('a[href*=\"/account\"]').click()"
  or:
    browser-use click <element>
  
  Only use `browser-use open` for the FIRST page load (before interceptors
  are installed) or when you need to re-install interceptors on a new page.
  
  After any `browser-use open`, re-install interceptors before proceeding.
  ```
- **Test:** Install interceptor → navigate via click → verify interceptor still active. Navigate via `browser-use open` → verify interceptor is gone (negative test that explains the rule).
- **Evidence:** Domino's GraphQL interceptor captured 0 ops. Pagliacci header interceptor required 3 attempts.

### 5. JS bundle extraction as supplementary discovery technique (discovered optimization)

- **What happened:** Claude extracted 33 API endpoint paths from Pagliacci's Angular main bundle by grepping for route patterns. This revealed endpoints the sniff would have missed (like /MigrateQuestion, /TransferGift, /AccessDevice) because no user flow visits those pages. The bundle is the complete API surface definition for the frontend.
- **Root cause:** The sniff procedure only discovers endpoints that are actually called during the browsing flow. Endpoints for rarely-used features (account migration, gift transfers, device access) are in the code but never exercised.
- **Cross-API check:** Every SPA bundles its API configuration. Angular, React, Vue apps all embed endpoint paths in the compiled JS.
- **Frequency:** Every sniffed SPA.
- **Worth a machine fix?** Yes — as a supplementary technique alongside browser-use, not a replacement. The sniff discovers response shapes and auth patterns; the bundle extraction discovers the complete endpoint list.
- **Inherent or fixable:** Fixable. Add bundle extraction as a supplementary step.
- **Durable fix:** Add to sniff-capture.md after Step 2a.2 (URL collection):
  ```
  Step 2a.2.3: JS bundle endpoint extraction (supplementary)
  After collecting URLs from browsing, also extract endpoints from the
  main JS bundle as a coverage supplement:
  
  1. Find the main bundle: browser-use eval to get script[src] elements
     matching the site domain and containing 'main' or 'app' in the filename
  2. curl the bundle and search for API path patterns:
     - String literals matching /[A-Z][a-zA-Z]+(/[A-Z][a-zA-Z]+)*
     - apiUrl/baseUrl + concatenated path strings
     - HTTP method calls with path arguments
  3. Merge bundle-discovered endpoints with sniff-discovered endpoints
  4. Mark bundle-only endpoints as "discovered: bundle" vs "discovered: sniff"
  
  This is supplementary — the sniff remains the primary discovery method
  because it provides response shapes, auth patterns, and parameter types.
  Bundle extraction only gives endpoint paths.
  ```
  Condition: SPA detected (Angular/React/Vue indicators in page source)
  Guard: Skip for non-SPA sites (server-rendered HTML without JS bundles)
- **Test:** Sniff a SPA → verify bundle extraction finds additional endpoints not captured during browsing.
- **Evidence:** Bundle extraction found /MigrateQuestion, /TransferGift, /AccessDevice, /PasswordForgot — never visited during sniff.

## Prioritized Improvements

### Fix the Scorer
No scorer bugs identified in this run.

### Do Now
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 1 | Add cardinal rule: never skip browser-use for capture | sniff-capture.md | every sniff | never (corrected 3x) | small | none |
| 2 | Add session expiry detection + headed login fallback | sniff-capture.md | most auth APIs | sometimes | small | AUTH_SESSION_AVAILABLE gate |
| 4 | Add SPA navigation rule for interceptor preservation | sniff-capture.md | every SPA | sometimes | small | none |

### Do Next (needs design/planning)
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 3 | Add auth header discovery (XHR interception) | sniff-capture.md | subclass: custom auth headers (~35%) | sometimes | medium | auth flow active |
| 5 | Add JS bundle endpoint extraction as supplementary | sniff-capture.md | every SPA | never (manual grep) | medium | SPA detection |

### Skip
(none)

## Work Units

### WU-1: Sniff capture robustness (findings #1, #2, #4)
- **Goal:** The sniff capture procedure reliably uses browser-use, detects expired sessions, and preserves interceptors during SPA navigation.
- **Target files:**
  - `skills/printing-press/references/sniff-capture.md`
- **Acceptance criteria:**
  - Cardinal rule at top of file: "NEVER skip browser-use for capture"
  - After profile load: session verification check, headed login fallback on expiry
  - SPA navigation rule: use click-based navigation after installing interceptors, re-install after any browser-use open
  - Negative test: anonymous sniff → session check skipped, headed login not offered
- **Scope boundary:** Does NOT change browser-use or agent-browser tools. Skill instruction changes only.
- **Complexity:** small (1 file, 3 additions to existing steps)

### WU-2: Auth header discovery and bundle extraction (findings #3, #5)
- **Goal:** The sniff automatically discovers custom Authorization headers from XHR interception and supplements endpoint discovery with JS bundle extraction.
- **Target files:**
  - `skills/printing-press/references/sniff-capture.md`
- **Acceptance criteria:**
  - After visiting auth pages: XHR header interceptor captures Authorization scheme
  - Auth scheme propagated to cookie/token validation step
  - After sniff browsing: main JS bundle scanned for additional endpoint paths
  - Bundle-only endpoints marked as "discovered: bundle" in the report
- **Scope boundary:** Does NOT change the generator or spec parser. Skill instruction changes only.
- **Dependencies:** WU-1 (session verification) should complete first so auth pages are actually visited.
- **Complexity:** medium (1 file, 2 new steps with interception logic)

## Anti-patterns

- **Shortcutting the sniff procedure.** Extracting endpoints from JS bundles or curling APIs directly feels faster but skips the interactive discovery that reveals auth patterns, response shapes, and real user flows. The sniff procedure exists for a reason — follow it, then supplement with bundle extraction.
- **Treating expired session as "skip auth."** When a Chrome profile's session has expired, the correct response is to offer an alternative auth method (headed login), not to proceed without auth.
- **Using `browser-use open` for SPA navigation after installing interceptors.** Full page navigation resets the JS context. Use click-based SPA navigation to keep interceptors alive.

## What the Machine Got Right

- **Setup contract local build preference.** The new setup contract correctly detected the repo and used the local binary with all unreleased features. No version mismatch issues.
- **browser-use --profile for Chrome cookie inheritance.** When Chrome was closed, loading the Default profile worked correctly — all cookies were available (the session was expired but the mechanism was sound).
- **browser-use eval for DOM access.** Full DOM inspection, link discovery, cookie reading, and interceptor installation all worked via eval. This is a massive advantage over agent-browser's auto-connect mode.
- **Headed login flow.** When the session was expired, the headed login workaround (open visible browser → user logs in → continue sniff) worked perfectly. The auth cookies were captured and the authenticated surface was fully discovered.
- **Auth header interception via SPA navigation.** Once Claude used click-based navigation instead of `browser-use open`, the XHR header interceptor captured the custom PagliacciAuth scheme correctly.
- **98% verify pass rate on first generation.** The spec was accurate enough that 41/42 commands passed all 3 verify checks without any fix loops.
