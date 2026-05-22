---
title: "Generated auth login --chrome could not capture session cookies from a daily-driver Chrome"
date: 2026-05-12
category: logic-errors
module: generator/templates/auth_browser
problem_type: bug
component: tooling
status: fixed
fix_landed: 2026-05-12
tags:
  - auth
  - chrome
  - cookies
  - capture
  - press-auth
  - macos
  - cookie-auth
  - composed-auth
  - generator-template
---

# Generated `auth login --chrome` could not capture session cookies from a daily-driver Chrome

## Symptom

Every printed CLI emitted from `internal/generator/templates/auth_browser.go.tmpl`
for `auth.type: cookie` or `auth.type: composed` failed at `auth login --chrome`
the moment a user ran it against their normal Chrome profile.

The visible failure was deterministic:

```text
Required cookies not in Chrome's cookie DB (likely session cookies).
Attempting to read from live Chrome session...
Could not read live session.
```

Exit was non-zero, no actionable next step printed. The user was logged in
to Chrome, the site visibly worked in their browser, and the CLI refused to
proceed. Every cookie/composed-auth CLI in the public library was affected on
first run; the bug was discovered against `alaska-airlines-pp-cli` and confirmed
to be template-shaped, not API-shaped.

## Root cause

The legacy `auth.go` emitted four extraction paths in sequence, each with a
distinct failure mode against an unmodified daily Chrome:

1. **Disk read via `pycookiecheat` / `cookies` / `cookie-scoop`.** These tools
   decrypt the on-disk Chrome Cookies SQLite DB. They return persistent
   cookies but miss the session cookies that hold modern session/JWT tokens.
   **Chrome holds session cookies in RAM and never flushes them to the
   on-disk DB while running.** Quitting Chrome doesn't help either — session
   cookies are dropped on close by definition.
2. **`browser-use --connect`.** Requires Chrome to be running with
   `--remote-debugging-port=N`. A normal Chrome launch (dock, Spotlight,
   `open -a "Google Chrome"`) doesn't enable the debug port.
3. **`agent-browser --auto-connect`.** Same `--remote-debugging-port`
   requirement.
4. **Raw CDP probe on ports 9222 / 9229.** Same requirement.

All four paths failed silently against the only configuration most users have:
a regular Chrome window where they're signed in. The on-disk fallback couldn't
see the session cookie; the live-attach fallback couldn't reach Chrome's CDP
because the debug port wasn't open.

This was a template-shape bug, not an API-shape bug. Every cookie/composed-auth
spec went through `auth_browser.go.tmpl` and inherited the broken extraction
chain. Patching one printed CLI fixed one CLI; the next sniffed-API regen
reintroduced the bug.

## What didn't work

- **Asking the user to quit Chrome and relaunch with
  `--remote-debugging-port=9222`.** Tried as a documentation-only fix. It
  worked for power users and failed for everyone else (Chrome warnings about
  unsupported launch arguments, profile-data lock conflicts, no help if Chrome
  was already running with multiple profiles open).
- **Forcing the on-disk path with a longer wait.** Session cookies never reach
  disk regardless of how long you wait.
- **Shipping pycookiecheat as a hard dependency.** Doesn't fix the underlying
  blind spot; Chrome's session cookies still aren't on disk.

## Fix

A `press-auth` companion binary (this work, plan
`docs/plans/2026-05-12-001-fix-auth-login-chrome-plan.md`).

`press-auth` lives in `cmd/press-auth/` with the supporting package in
`internal/pressauth/`. It:

1. Spawns its own controlled Chrome via `github.com/chromedp/chromedp` with a
   fresh user-data-dir under `/tmp` — never touches the user's daily Chrome
   profile, never requires `--remote-debugging-port` on the user's Chrome.
2. Captures cookies at the end of an interactive login (user clicks "Sign In"
   in the controlled window, press-auth detects a completion selector or
   generic logged-in heuristic, calls `chromedp`'s
   `network.GetAllCookies()` which returns session cookies and persistent
   cookies alike — no on-disk blind spot).
3. Persists per-domain state at `~/.press-auth/<domain>.json`, AES-GCM encrypted
   with a key stored in the macOS Keychain under `press-auth: <domain>`.
4. Serves cookies on demand via `press-auth cookies <domain>`. Lazy JWT
   refresh: when the carrier cookie's `exp` claim is within 60s of now,
   press-auth calls the spec-declared refresh endpoint and rotates the cookie
   set in place before returning.

The template change to `internal/generator/templates/auth_browser.go.tmpl`
adds a "Step 0" preferred capture path: `exec.LookPath("press-auth")`. If
found, shell out to `press-auth cookies <domain>` and use the returned cookie
header. The legacy four-step extraction chain is preserved as fallback so
existing setups and machines without press-auth installed continue to work.
The final error message (when all paths fail) names press-auth as the
recommended install with a copy-pasteable command.

Two spec extensions support the integration: `login_url` and
`jwt_carrier_cookie` (and an optional `login_complete_selector`) on the
internal-YAML auth block, and the OpenAPI `x-auth-companion` equivalent.
Generated `auth login --chrome` can call `press-auth login <domain>` with all
flags pre-filled when the spec carries the hints.

## Why this works

- press-auth's Chrome instance is controlled, so it doesn't have to ask the
  user to enable `--remote-debugging-port` on their daily Chrome. The CDP
  connection is internal to press-auth.
- chromedp's `network.GetAllCookies()` returns the in-memory cookie jar of the
  controlled Chrome, including session cookies, because press-auth is the one
  controlling that Chrome.
- Storage is per-domain and per-keychain-entry, so multi-CLI users (many
  printed CLIs on the same machine) don't have cross-domain coupling.
- The template fallback is unchanged, so users without press-auth installed
  experience exactly the legacy behavior. No flag-day migration. No new
  hard dependency on press-auth.

## Forward

Tracked as deferred follow-up in the plan; not blocking the v1 fix:

- **Linux support.** chromedp is cross-platform; the gap is keychain
  integration (`libsecret` / `gnome-keyring`) and the Chrome binary path.
- **Windows support.** Same as Linux with DPAPI for at-rest encryption.
- **Background daemon mode.** v1 refreshes lazily on every `press-auth
  cookies` read. A `press-auth daemon` that warms cookies before expiry would
  remove the occasional 500ms hitch on first call after a long idle.
- **Web UI for login.** Some power users may prefer a localhost-served React
  UI over a chromedp window. v1 ships the chromedp window only.
- **Multi-profile per domain.** v1 is one captured profile per domain. If a
  user has multiple accounts on the same domain (personal + work),
  `press-auth forget` + re-login is the swap.

## References

- Plan: [`docs/plans/2026-05-12-001-fix-auth-login-chrome-plan.md`](../../plans/2026-05-12-001-fix-auth-login-chrome-plan.md)
- Skill reference: [`skills/printing-press/references/auth-companion.md`](../../../skills/printing-press/references/auth-companion.md)
- Template (U6): `internal/generator/templates/auth_browser.go.tmpl`
- Binary: `cmd/press-auth/`
- Supporting package: `internal/pressauth/`
- Spec extensions: `docs/SPEC-EXTENSIONS.md` (`login_url`,
  `login_complete_selector`, `jwt_carrier_cookie`, `x-auth-companion`)
