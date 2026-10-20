---
when-to-read: When implementing or recommending the press-auth companion for a CLI with cookie or composed auth. Also when debugging "auth login --chrome captured no cookies" for an existing CLI.
---

# press-auth Companion: Cookie Capture for Cookie/Composed-Auth CLIs

> **When to read:** Pre-Generation Auth Enrichment (Phase 1.7 / Phase 2) for any spec
> with `auth.type` of `cookie` or `composed`. Also load when the user reports that a
> printed CLI's `auth login --chrome` exits with "Required cookies not in Chrome's
> cookie DB" or any cookie-extraction failure. The Printing Press's
> Cookie/composed HTML transport section in SKILL.md points here for the recommendation
> flow and debug playbook.

## What press-auth is

`press-auth` is a companion binary that ships from `cmd/press-auth/` in
`cli-printing-press`. It captures cookies once via a controlled Chrome window
(separate user-data-dir, never the user's daily profile), stores per-domain
encrypted state, and serves cookie headers to generated CLIs on demand. Lazy JWT
refresh keeps sessions alive past short Auth0/refresh-cookie expiries without a
background daemon. macOS only for v1; Linux and Windows are tracked as follow-up
work.

It exists because the legacy `auth login --chrome` chain
(`pycookiecheat` -> `cookies` -> `browser-use --connect` -> raw CDP) cannot reach
the session cookies that hold modern session/JWT tokens. Chrome keeps session
cookies in RAM, the on-disk Cookies DB never sees them, and the user's daily
Chrome isn't launched with `--remote-debugging-port=N`. press-auth sidesteps both
constraints by spawning its own controlled Chrome.

## When the press skill should recommend installing press-auth

Recommend press-auth in user-facing output when **any** of these are true for
the current run:

- The spec's `auth.type` is `cookie` or `composed` (typical for sniffed APIs:
  loyalty programs, airline portals, vacation sites, retail accounts).
- The user is hitting "Required cookies not in Chrome's cookie DB (likely
  session cookies). Attempting to read from live Chrome session..." from an
  existing CLI's `auth login --chrome`.
- The spec carries the new `login_url` and `jwt_carrier_cookie` hints (the
  spec-extension keys introduced alongside this companion) — those make
  press-auth's setup a single command.
- The user wants a CLI that "just works" across sessions without re-authing.

Do NOT recommend it when:

- `auth.type` is `api_key`, `bearer_token`, or `oauth2_client_credentials`
  with a token URL — those paths are already first-class and don't need
  cookie capture.
- The user is on Linux or Windows. v1 is macOS-only. Surface that limitation
  upfront; don't have them install and then discover the keychain error.

## First-time install for the user

Two-step install, copy-pasted into the user's terminal:

```bash
go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest
press-auth login <domain> \
  --login-url <login-url> \
  --jwt-carrier-cookie <carrier> \
  [--complete-selector <selector>] \
  [--complete-cookie <name>] \
  [--refresh-endpoint <path>] \
  [--include-subdomains]
```

`<domain>` is the cookie domain (e.g., `alaskaair.com`), not a URL.
`<login-url>` is the page where the user signs in. `<carrier>` is the cookie
that holds the JWT for expiry tracking. `--complete-cookie` is a separate
completion signal and replaces the sign-out heuristic; pass the same name
as `<carrier>` only when that cookie should both finish login and drive
refresh. `--complete-selector` also replaces the heuristic. When both are
set, either signal finishes login.

`--include-subdomains` is off by default. Pass it only when `press-auth login --help` lists the flag and a required cookie is host-only on a subdomain of `<domain>` (for example a regional CSRF cookie). The default capture keeps RFC 6265 matching, so a host-only cookie is stored only when its host equals `<domain>`. Do not pass the flag for an ordinary apex capture: it widens the cookies press-auth stores under that domain.

The first `press-auth login` for a given domain triggers a macOS keychain
prompt: "press-auth wants to use your confidential information stored in the
keychain." Tell the user to click **Always Allow** (not just "Allow") — the
prompt repeats on every read otherwise. press-auth itself also prints a one-line
heads-up before the keychain write.

## How press-auth fits into the generated CLI's auth flow

The press's `auth_browser.go.tmpl` (touched by U6 of the bug-fix plan) emits an
`auth.Header()` flow that tries press-auth first:

1. **Step 0 (preferred):** `exec.LookPath("press-auth")`. If found, shell out
   to `press-auth cookies <domain>`. On success, return the cookie header in
   under 100ms. On failure with state present, surface the press-auth error
   verbatim (it names the next command).
2. **Fallback (legacy):** the existing `detectCookieTool` ->
   `extractCookies` -> `extractLiveCookies` chain. Unchanged from pre-fix
   behavior.
3. **Final error (when all paths fail):** message names press-auth as the
   recommended install — `go install ... && press-auth login <domain>`.

The fallback exists so existing CLIs and users without press-auth installed
keep working. There is no flag-day migration; the press doesn't require
press-auth at runtime.

## Debugging capture failures

| Symptom | Cause | Fix |
|---------|-------|-----|
| "No Chrome found" | chromedp can't locate the Chrome binary. | Install Google Chrome at the default location (`/Applications/Google Chrome.app`). chromedp auto-detects from there. |
| A required cookie is missing after an apex capture (`cookie "<name>" not found`) | The cookie is host-only on a subdomain. The default filter keeps a host-only cookie only for an exact host match. | If `press-auth login --help` lists `--include-subdomains`, re-run `press-auth login <domain> --login-url <login-url> --include-subdomains --force`. `--force` is required: that domain already has a capture, and login refuses to open Chrome until the saved state can be replaced. `press-auth cookies <domain>` then prints the stored cookies, including that host-only subdomain cookie. Capturing under the subdomain host instead stores a different domain key, which the printed CLI will not read. |
| Login window opens, user signs in, but capture never completes (10-min timeout) | No completion flag was set and the sign-out heuristic did not match any page tab (path changed, no password field, sign-out link or button). The signed-in page may have no such control, or it may be on a tab other than the first. A configured `--complete-cookie` that is still absent does not fall through to that heuristic. | Re-run with `--complete-cookie <name>` for a cookie that appears only after sign-in, or `--complete-selector` for an element that is visible after login (`a[href*=signout]`, `[data-testid=user-menu]`, `nav .account-name`). Either flag replaces the heuristic, so capture waits for that signal. `--complete-selector` is checked on every page tab. When both flags are set, either signal finishes login. |
| `press-auth cookies <domain>` returns 401 after weeks of working | Refresh window exceeded; the underlying session is dead. | Re-run `press-auth login <domain>`. Lazy refresh only extends sessions while the carrier cookie is still refreshable. |
| Keychain prompt repeats on every `press-auth cookies` call | User clicked "Allow" instead of "Always Allow" on the first prompt. | `press-auth forget <domain>` then `press-auth login <domain>` again. Choose **Always Allow**. |
| `press-auth cookies` exits 5 with "refresh returned no new cookies" | Refresh endpoint responded 200 but didn't rotate the carrier cookie (some APIs only rotate on specific paths). | Verify the spec's `refresh_endpoint` is the correct path. If the API has a separate "extend session" vs "exchange token" endpoint, the spec needs the token-exchange path. |
| `press-auth cookies` exits 3 with "could not decode JWT exp claim" | The `--jwt-carrier-cookie` value doesn't actually carry a JWT (e.g., it's an opaque session ID). | Inspect the cookie value during browser-sniff. If it's `header.payload.signature`-shaped (three base64 segments), it's a JWT. Otherwise pick a different carrier cookie or drop refresh and re-login when sessions die. |
| State file works locally but `auth login --chrome` in a generated CLI still falls through to the legacy chain | The generated CLI was emitted BEFORE the U6 template change landed, or the user has an old binary on PATH. | Regenerate the printed CLI from `cli-printing-press` `main`; `which <api-slug>-pp-cli` to confirm the on-PATH binary is the regenerated one. |

## Scoping login URLs and selectors per API

The three new spec fields (`login_url`, `login_complete_selector`,
`jwt_carrier_cookie`) drive press-auth's one-command setup. Get them right
during Phase 1.7 (browser-sniff) and the generated CLI's `auth login --chrome`
becomes a single command for the end user.

### `login_url`

The page where the user enters credentials. Usually one of:

- `https://<site>/account/login`
- `https://<site>/signin`
- `https://login.<site>/`
- An IdP redirect (`https://<site>/auth/<provider>/start`)

Discovery: open the upstream brand's main site nav and click "Sign in". The
final URL after redirects is what press-auth should navigate to.

Validation: must be HTTPS. press-auth refuses HTTP login URLs (a cookie
captured over plain HTTP isn't worth storing in a keychain-protected file).

### `login_complete_selector` (optional but strongly recommended)

A CSS selector that's only visible AFTER login. press-auth checks it on
every page tab of the capture window, not only the first. When it is set,
it replaces the generic heuristic (any page tab whose path changed, with no
`input[type=password]`, and a sign-out link or button). The heuristic runs
only when neither completion flag is set. It hangs when no tab renders that
control — for example an emailed sign-in link that opens a second tab onto
a home page with no sign-out element.

`--complete-cookie <name>` also replaces the heuristic. Login finishes only
when that cookie is present anywhere in the capture browser, on any host.
A page that matches the heuristic does not finish capture while the cookie
is still absent. When both `--complete-cookie` and the selector are set,
either signal finishes login, and the heuristic still does not run.

Good signals:

- A signout link: `a[href*=signout]`, `a[href*=logout]`
- An account menu trigger: `[data-testid=user-menu]`,
  `[aria-label="Account menu"]`
- A welcome banner: `.welcome-name`, `[data-test=account-greeting]`
- A logged-in-only nav item: `nav a[href="/dashboard"]`

Avoid selectors that match the marketing page or pre-login state — they'll
match too early and capture half-baked cookies.

### `jwt_carrier_cookie`

The cookie whose value holds the JWT used for expiry tracking. press-auth
decodes the `exp` claim from this cookie's body to know when to refresh.

Discovery during Phase 1.7:

1. After login, inspect the browser's request to an authenticated endpoint.
2. Read the `Cookie` header values. The carrier is usually a cookie with a
   value shaped like `eyJhbGciOi...XXXXX.YYYYY` (three dot-separated base64
   segments) OR a URL-encoded JSON wrapper containing that.
3. Common names: `session`, `auth`, `guestsession`, `token`, `id_token`,
   `access_token`. Some sites use brand-specific names like
   `AS_ACNT`/`guestsession` (Alaska), `iC_session` (Iceland Air), etc.

If multiple cookies carry JWTs (e.g., id + access), pick the **access token**
carrier — that's the one the API actually validates and rotates on refresh.

If the spec only declares `cookie_domain` and `cookies` (no
`jwt_carrier_cookie`), press-auth still captures and serves cookies but skips
lazy refresh. Sessions die when the API stops accepting the original
cookie set; the user has to re-login. Acceptable for short-lived workflows;
not great for daily-driver CLIs.

## Honest limits

- **macOS only in v1.** Linux uses `libsecret`/`gnome-keyring`, Windows uses
  DPAPI; both are tracked as follow-up plans. Don't promise cross-platform
  yet.
- **No background daemon.** Refresh happens lazily on `press-auth cookies`
  read. If the user runs a CLI command after a long idle, the first call may
  block on a refresh round-trip (typically <500ms).
- **One account per domain.** press-auth v1 doesn't support multi-profile
  switching (e.g., personal vs work accounts on the same domain). `press-auth
  forget <domain>` and re-login swaps. Multi-profile is on the v2 wish list.
- **chromedp + Chrome version coupling.** If Google ships a Chrome update
  that breaks chromedp's protocol assumptions, press-auth login will fail
  until chromedp is bumped. Run `press-auth login` with `--verbose` to see
  the chromedp protocol error; report upstream.

## Cross-references

- [`references/browser-sniff-capture.md`](browser-sniff-capture.md) — where
  `login_url`, `login_complete_selector`, and `jwt_carrier_cookie` are
  discovered during Phase 1.7 traffic capture.
- [`cmd/press-auth/`](../../../cmd/press-auth/) — the binary's source.
- [`internal/pressauth/`](../../../internal/pressauth/) — the supporting
  package (state file, keychain, chromedp launcher, JWT decode, refresh).
- [`internal/generator/templates/auth_browser.go.tmpl`](../../../internal/generator/templates/auth_browser.go.tmpl) —
  the template that integrates press-auth into generated CLIs.
- [`docs/solutions/logic-errors/auth-login-chrome-broken-2026-05.md`](../../../docs/solutions/logic-errors/auth-login-chrome-broken-2026-05.md) —
  bug history: the original failure, the root cause, the press-auth fix.
- [`docs/SPEC-EXTENSIONS.md`](../../../docs/SPEC-EXTENSIONS.md) — canonical
  reference for the spec keys (`login_url`, `login_complete_selector`,
  `jwt_carrier_cookie`, and the OpenAPI `x-auth-companion` form).
