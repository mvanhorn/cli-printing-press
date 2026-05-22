---
type: fix
status: active
created: 2026-05-12
plan_id: 2026-05-12-001
title: Make auth-login-chrome actually work (press-auth companion)
target_repo: cli-printing-press
---

# fix: Make auth-login-chrome actually work (press-auth companion)

## Summary

The press emits `auth login --chrome` in every cookie- and composed-auth CLI, but it fails the moment a user runs it against their normal Chrome. The fix introduces **`press-auth`**, a standalone companion binary in `cli-printing-press` that owns cookie capture once and serves cookies to every printed CLI. The generated `auth login --chrome` flow becomes a thin wrapper over `press-auth` with graceful fallback to today's behavior when the companion isn't installed.

This is a **machine fix** (Core Principle in `AGENTS.md`): the template emits new code for every regen, every existing CLI gets the fix on next regen, and every future sniffed-API CLI inherits the working auth-out-of-the-box experience.

## Problem Frame

### What's broken today

`alaska-airlines-pp-cli auth login --chrome` (and every other CLI emitted from `internal/generator/templates/auth_browser.go.tmpl`) tries three paths in order:

1. **Disk read** via `pycookiecheat` / `cookies` / `cookie-scoop` — gets persistent cookies but **misses session cookies**, because Chrome holds session cookies in RAM and never flushes them to the on-disk Cookies DB while running.
2. **`browser-use --connect`** — requires Chrome to be running with `--remote-debugging-port=N`. Normal Chrome launches don't enable this.
3. **`agent-browser --auto-connect`** — same requirement.
4. **Raw CDP on ports 9222/9229** — same requirement.

All four paths fail silently on a vanilla "I'm logged in to Chrome" setup. The error prints "Required cookies not in Chrome's cookie DB (likely session cookies). Attempting to read from live Chrome session... Could not read live session." with no actionable next step.

### What the user actually needs

> "Auth has to work. Give it an auth program where it can automatically pull my cookies or whatever it needs."

Concretely: a one-time login flow that does NOT require the user to learn `--remote-debugging-port` or quit their daily-driver Chrome. After that one-time login, every printed CLI for any sniffed-API service "just works".

### Why this is a press-level fix, not a one-off

`auth_browser.go.tmpl` is emitted for every API where research detects cookie/composed auth — Alaska Airlines today, but also vacations sites, airline-loyalty programs, retail-loyalty programs, undocumented partner APIs. Patching `~/printing-press/library/alaska-airlines/internal/cli/auth.go` solves one CLI; the next sniffed-API regen reintroduces the bug. The fix has to live in the template, and the binary it depends on has to ship as part of `cli-printing-press`.

---

## Requirements

| ID | Requirement | Source |
|----|-------------|--------|
| R1 | A `press-auth` binary in `cli-printing-press` that captures, stores, and serves cookies for a named domain | user request |
| R2 | One-time interactive login flow that opens a controlled Chrome window (separate from the user's daily Chrome), waits for login, captures session, returns | user request |
| R3 | Multi-domain support: one binary maintains state for N APIs (`alaskaair.com`, `vacations.alaskaair.com`, etc.) | press scope (multiple CLIs per machine) |
| R4 | JWT auto-refresh: when stored access tokens are within N seconds of expiry, call the spec-declared refresh endpoint and rotate cookies in place | preserve user session beyond Auth0's 30-min JWT lifetime |
| R5 | The press's `auth_browser.go.tmpl` is updated so generated `auth login --chrome` calls `press-auth` first, with a clean fallback message when `press-auth` isn't installed | machine fix |
| R6 | Backwards compatible: existing CLIs continue to work via the old `pycookiecheat`/browser-use paths when the user has those installed and chooses not to install `press-auth` | don't break working setups |
| R7 | macOS as primary target; document Linux/Windows as v2 (chromedp is cross-platform but keychain and Chrome paths differ) | scope control |
| R8 | All artifacts respect the existing `secret-protection.md` cardinal rules: cookie values never appear in any output, log, manuscript, or git commit | repo policy |

---

## Scope Boundaries

### In scope

- `cmd/press-auth/` new binary in `cli-printing-press`
- Embedded chromedp launcher that spawns its own controlled Chrome instance (separate user-data-dir, never the user's daily profile)
- JSON state file at `~/.press-auth/<domain>.json` with cookie names + values + expiries
- `press-auth login <domain> --login-url <url>` for one-time interactive capture
- `press-auth cookies <domain>` for cookie-header-string output (consumed by generated CLIs)
- `press-auth status <domain>` for health checks
- `press-auth refresh <domain> --refresh-endpoint <path>` for JWT refresh
- `press-auth list` to show all captured domains
- `press-auth forget <domain>` for cleanup
- Template change to `internal/generator/templates/auth_browser.go.tmpl` integrating `press-auth` as the preferred capture path
- New `references/auth-companion.md` documenting the press-auth contract for the skill
- Unit tests for state-file marshaling, JWT decode, expiry math, refresh flow
- Integration test fixture: a fake login server + chromedp flow that exercises the full capture loop
- macOS keychain integration for storing cookie values at rest (don't write plaintext JSON)

### Deferred to Follow-Up Work

- **Linux support**: chromedp works there, but Chrome paths and keychain integration (`libsecret`) need their own handling. File a follow-up plan.
- **Windows support**: same as Linux with DPAPI for at-rest encryption.
- **Background daemon mode**: an always-running `press-auth daemon` that auto-refreshes tokens before they expire. For v1, refresh is lazy (on-read).
- **Web UI for login**: chromedp opens a real browser window. Some power users may want a localhost-served React UI instead. Lazy-add if requested.
- **Multi-profile picker**: if a user has multiple accounts for the same domain, we'll need profile labels. v1 is single-account-per-domain.

### Outside this project's identity

- **Replacing browser-use / agent-browser**: those tools serve a wider purpose (Phase 1.7 discovery). `press-auth` is purpose-built for capture-once-and-serve; it complements rather than replaces.
- **Auto-detecting any cookie-based auth**: `press-auth` requires the printed CLI (or the user) to name the domain and refresh endpoint. We don't try to auto-discover.

---

## Key Technical Decisions

### TD1. Embedded chromedp over user's Chrome relaunch

Use `github.com/chromedp/chromedp` (Go CDP client) to spawn a fresh controlled Chrome instance. Pros:

- Never touches the user's daily Chrome profile (no quit-and-relaunch dance, no `--remote-debugging-port` argument explanation, no risk of clobbering their session)
- Chrome binary path is auto-detected; macOS has it at `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
- chromedp's `Cookies()` / `CookiesAccess` actions return ALL cookies including session ones — no on-disk-DB blind spot
- Already a dependency direction the press is comfortable with (we use Surf for Chrome-fingerprint HTTP; chromedp for capture is an adjacent use)

Alternative rejected: shipping a Python `pycookiecheat` wrapper. Decrypting macOS Chrome cookies via Keychain works for persistent cookies only — same fundamental blind spot we have today.

Alternative rejected: forcing the user to relaunch Chrome with `--remote-debugging-port`. The whole point of this work is removing user-facing setup friction.

### TD2. Per-domain JSON state in `~/.press-auth/<domain>.json`

Rationale:

- Simple, debuggable, no daemon required
- Multi-domain isolation comes for free (one file per domain)
- Cookie names + AES-encrypted values, with the AES key stored in macOS Keychain under `press-auth: <domain>` (one keychain entry per domain — user grants access once per domain via the standard "Always Allow" prompt)
- File mode `0600`, parent dir `0700`
- Format:
  ```json
  {
    "domain": "alaskaair.com",
    "captured_at": "2026-05-12T17:54:00Z",
    "cookies_encrypted": "<base64 AES-GCM ciphertext of JSON{name: value}>",
    "refresh_endpoint": "/account/token",
    "jwt_carrier_cookie": "guestsession",
    "jwt_expiry": "2026-05-12T18:24:00Z"
  }
  ```

Alternative rejected: single `~/.press-auth/state.json` with all domains. Adds locking complexity for marginal benefit.

Alternative rejected: macOS Keychain as the sole store. Keychain entries cap at small payload sizes; alaskaair's `guestsession` cookie is ~3.4KB, well past comfortable keychain limits. AES-encrypted file with keychain-held key is the working pattern.

### TD3. Lazy refresh, not background daemon

When a CLI calls `press-auth cookies <domain>`, the binary:

1. Reads the state file
2. If `jwt_expiry` is more than 60s in the future → return cookies as-is
3. Otherwise → call the `refresh_endpoint` with current cookies, parse the response (Set-Cookie headers + JWT bodies), persist the new state, return the refreshed cookies

No background daemon, no cron, no LaunchAgent. Refresh happens at most once per CLI invocation, only when needed. Deferred to a v2 follow-up if the lazy strategy proves insufficient.

### TD4. Generated CLI integration is additive, not breaking

`auth_browser.go.tmpl` gets a NEW preferred path:

1. **First**: try `exec.LookPath("press-auth")`. If found, shell out: `press-auth cookies <domain>` → use returned cookie header.
2. **Fallback**: existing `detectCookieTool` → `extractCookies` → `extractLiveCookies` chain. Same as today.
3. **If first path fails AND fallback fails**: print a clean diagnostic that names `press-auth` as the recommended install: `go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest`.

Existing CLIs keep working. Newly regenerated CLIs prefer `press-auth` when present. No flag-day migration.

### TD5. Spec carries auth-companion hints

Add an optional `auth_companion` block to internal-spec YAML:

```yaml
auth:
  type: composed
  cookie_domain: alaskaair.com
  cookies: [AS_ACNT, AS_NAME, guestsession, ...]
  refresh_endpoint: /account/token
  jwt_carrier_cookie: guestsession   # NEW: which cookie holds the JWT for expiry tracking
  login_url: https://www.alaskaair.com/account/login   # NEW: where press-auth opens for user login
  login_complete_selector: a[href*=signout]   # NEW: optional DOM selector that signals login done
```

OpenAPI specs get the same via `x-auth-companion`. The press passes these into the generated CLI's auth.go so it can invoke `press-auth login <domain> --login-url <url> --complete-selector <sel>` correctly.

---

## High-Level Technical Design

This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.

### Capture loop (`press-auth login <domain>`)

```text
1. Read spec hints (login URL, complete-selector) from CLI args
2. Generate AES-GCM key, write to macOS Keychain under "press-auth: <domain>"
3. chromedp.NewContext() with fresh user-data-dir under /tmp
4. chromedp.Run( Navigate(login_url) )
5. Wait for either:
   - complete-selector to appear (signals login success), OR
   - 10-minute timeout, OR
   - User Ctrl-C
6. chromedp.Run( Cookies() ) → []Cookie
7. Filter to spec.cookie_domain; serialize {name: value} JSON
8. Encrypt with AES-GCM (key from keychain), base64-encode ciphertext
9. Decode JWT carrier cookie body, extract exp claim → jwt_expiry
10. Write state file
11. Close Chrome
```

### Serve loop (`press-auth cookies <domain>`)

```text
1. Read state file, error clean if not found ("run: press-auth login <domain>")
2. If jwt_expiry > now + 60s → decrypt, format as Cookie header, print, exit 0
3. Otherwise: POST/GET refresh_endpoint with current cookies (via Surf transport)
4. Parse Set-Cookie response, merge into state
5. Re-decode JWT, update jwt_expiry
6. Persist updated state
7. Print refreshed Cookie header
```

### Generated CLI integration (in `auth_browser.go.tmpl`)

```text
func (a *Auth) Header() (string, error) {
  // NEW path
  if path, err := exec.LookPath("press-auth"); err == nil {
    cmd := exec.Command(path, "cookies", a.Domain)
    if out, err := cmd.Output(); err == nil {
      return string(out), nil
    }
    // press-auth installed but failed (no state) → return helpful error naming it
  }
  // EXISTING fallback paths unchanged...
}
```

---

## Output Structure

```text
cli-printing-press/
├── cmd/
│   └── press-auth/
│       └── main.go                          [NEW]
├── internal/
│   ├── pressauth/                           [NEW package]
│   │   ├── state.go                         # JSON state I/O + AES-GCM
│   │   ├── state_test.go
│   │   ├── keychain_darwin.go               # macOS keychain integration
│   │   ├── keychain_other.go                # stub for non-darwin (returns helpful error)
│   │   ├── chrome.go                        # chromedp launcher + capture loop
│   │   ├── chrome_test.go                   # uses local httptest server + headless chromedp
│   │   ├── jwt.go                           # JWT exp claim extraction
│   │   ├── jwt_test.go
│   │   ├── refresh.go                       # token refresh via Surf
│   │   └── refresh_test.go
│   └── generator/
│       └── templates/
│           └── auth_browser.go.tmpl         [MODIFIED — add press-auth path]
├── skills/
│   └── printing-press/
│       └── references/
│           └── auth-companion.md            [NEW — press-auth contract for the skill]
├── docs/
│   └── plans/
│       └── 2026-05-12-001-fix-auth-login-chrome-plan.md  (this file)
└── testdata/
    └── pressauth/
        ├── fake-login-server/               [NEW — httptest-based fake login site]
        └── fixtures/                        [NEW — sample state files, JWTs]
```

---

## Implementation Units

### U1. Scaffold `cmd/press-auth` binary + `internal/pressauth` package

- **Goal**: Empty-but-buildable binary with subcommands stubbed (`login`, `cookies`, `status`, `refresh`, `list`, `forget`). No real behavior yet — just the Cobra surface, help text, and exit codes.
- **Requirements**: R1
- **Dependencies**: none
- **Files**:
  - `cmd/press-auth/main.go`
  - `internal/pressauth/root.go`
  - `internal/pressauth/root_test.go`
  - `go.mod` (add chromedp + keychain deps)
- **Approach**: Cobra root command, subcommand stubs that return `errors.New("not implemented")`. `--json` and `--quiet` flags wired. Help text written so users read it before invoking.
- **Patterns to follow**: Mirror the layout of `cmd/printing-press` (Cobra-driven, `internal/cli/` style command files).
- **Test scenarios**:
  - `press-auth --help` exits 0 and prints all subcommands
  - `press-auth login --help` exits 0 and shows `--login-url`, `--complete-selector`, `--refresh-endpoint`, `--jwt-carrier-cookie` flags
  - `press-auth cookies missing.example.com` exits non-zero with a "not yet captured" message naming `press-auth login`
- **Verification**: `go build ./cmd/press-auth` succeeds. `./press-auth login` (no domain) exits with a clean usage error, not a stack trace.

---

### U2. State file: encrypted JSON + macOS keychain key storage

- **Goal**: `internal/pressauth/state.go` reads, writes, encrypts, decrypts state files. Key lives in macOS keychain under `press-auth: <domain>`.
- **Requirements**: R1, R8
- **Dependencies**: U1
- **Files**:
  - `internal/pressauth/state.go`
  - `internal/pressauth/state_test.go`
  - `internal/pressauth/keychain_darwin.go`
  - `internal/pressauth/keychain_other.go`
- **Approach**: Use `github.com/keybase/go-keychain` for macOS. On non-darwin, `keychain_other.go` returns a clear "press-auth currently requires macOS" error (sets up the Linux/Windows v2 path). State JSON shape per TD2. File mode 0600, parent dir 0700, atomic writes via `os.Rename` from temp file.
- **Patterns to follow**: Existing atomic-write patterns in `internal/store/store.go` for ledger files.
- **Test scenarios**:
  - Write state for `example.com` with 3 cookies, read back identical contents (round trip)
  - Read a state file with wrong key → returns clean "key not found in keychain or wrong key" error, not a panic
  - Write to a domain that already has state → atomic overwrite, no partial-write window observable to a concurrent reader
  - File mode is 0600 after write; parent dir is 0700 after first write
  - JSON shape is stable: `domain`, `captured_at`, `cookies_encrypted`, `refresh_endpoint`, `jwt_carrier_cookie`, `jwt_expiry` (snapshot golden)
  - On non-darwin, keychain operations return the documented "requires macOS" error (compile-time tag check)
- **Verification**: `go test ./internal/pressauth/` passes. State files match the documented JSON shape (golden test). Keychain entries can be inspected via `security find-generic-password -a press-auth -s "press-auth: example.com"`.

---

### U3. chromedp launcher + capture loop (`press-auth login`)

- **Goal**: `press-auth login <domain> --login-url <url> [--complete-selector <sel>]` opens a controlled Chrome window, waits for login, captures cookies, persists state.
- **Requirements**: R1, R2
- **Dependencies**: U2
- **Files**:
  - `internal/pressauth/chrome.go`
  - `internal/pressauth/chrome_test.go`
  - `testdata/pressauth/fake-login-server/main.go`
- **Approach**: `chromedp.NewExecAllocator` with `Headless(false)`, `UserDataDir(/tmp/press-auth-<domain>-<pid>)`, `NoFirstRun(true)`. Navigate to login URL. Poll for either complete-selector visibility OR a generic "logged in" heuristic (URL changed AND no input[type=password] visible AND a signout link is present). Default timeout 10 minutes; user can Ctrl-C. After capture, call `network.GetAllCookies()`, filter to spec.cookie_domain, hand off to state writer.
- **Patterns to follow**: chromedp examples in `internal/generator/templates/` already use `chromedp.Eval` patterns; reuse those conventions.
- **Test scenarios**:
  - Fake-login-server serves a login page → chromedp instance reaches it within 5s in CI
  - `--complete-selector "a#signout"` waits for that selector and returns within 1s of it appearing
  - Default heuristic (no selector) triggers when URL contains `/account/overview` AND no password input is on the page
  - 10-minute timeout fires cleanly (test uses 2s override) and prints "login window timed out" without leaking the partial state to disk
  - Ctrl-C during the wait cleanly closes Chrome and exits with code 130
  - User-data-dir is cleaned up on exit (success and failure paths)
  - Captured cookies filter correctly: cookies for `.example.com` and `www.example.com` are included; cookies for `other.example.com` are excluded (matches the documented domain-suffix rule)
- **Execution note**: This unit has higher integration-test value than unit-test value because the chromedp + httptest interaction is the failure surface. Drive it with the fake-login-server fixture, run headless in CI, headed locally.
- **Verification**: `go test ./internal/pressauth/ -run TestChromeCapture` passes in CI (headless). Manual `press-auth login example.com --login-url http://localhost:8080/login` against the fake server captures cookies and writes a valid state file.

---

### U4. JWT decode + expiry math + lazy refresh (`press-auth cookies`, `press-auth refresh`)

- **Goal**: `press-auth cookies <domain>` returns the current cookie header, auto-refreshing when the JWT is within 60s of expiry.
- **Requirements**: R4
- **Dependencies**: U2
- **Files**:
  - `internal/pressauth/jwt.go`
  - `internal/pressauth/jwt_test.go`
  - `internal/pressauth/refresh.go`
  - `internal/pressauth/refresh_test.go`
- **Approach**: Decode the JWT body (no signature verification — we trust the issuer because we captured the cookie from a real browser session). Extract `exp` claim. If `exp - now > 60s`, return cookies as-is. Otherwise, call the spec's `refresh_endpoint` via Surf transport (Chrome TLS fingerprint, since some refresh endpoints are bot-detected), parse `Set-Cookie` headers, merge into state, recompute `jwt_expiry`, persist.
- **Patterns to follow**: Surf usage in `internal/generator/templates/client.go.tmpl` (the existing Surf-backed HTTP transport).
- **Test scenarios**:
  - JWT with `exp` 30 min in future → `cookies` returns as-is without calling refresh endpoint (mock HTTP records 0 calls)
  - JWT with `exp` 30s in future → triggers refresh; mock endpoint returns new Set-Cookie; state file updated atomically
  - Refresh endpoint returns 401 → state remains unchanged, exit code 4 with "refresh failed; run press-auth login again" message
  - Refresh endpoint returns 200 with no Set-Cookie → exit code 5 with "refresh returned no new cookies"
  - Malformed JWT in `jwt_carrier_cookie` → exit code 3 with "could not decode JWT exp claim", state remains unchanged
  - Missing `refresh_endpoint` in state → exit code 6 with "refresh endpoint not configured; re-run press-auth login --refresh-endpoint <path>"
  - Multiple cookies named differently in Set-Cookie response (e.g., `guestsession` AND a rotated `guestsession_v2`) → all merged correctly, no key collisions silently dropped
- **Verification**: `go test ./internal/pressauth/ -run TestJWT TestRefresh` passes. Manual test: capture state, manually edit `jwt_expiry` to a past timestamp, run `press-auth cookies example.com`, observe a refresh call and updated expiry.

---

### U5. Remaining subcommands (`status`, `list`, `forget`)

- **Goal**: Round out the user-facing CLI surface: `status` reports validity, `list` shows captured domains, `forget` cleans up state + keychain.
- **Requirements**: R1, R3
- **Dependencies**: U2, U4
- **Files**:
  - `internal/pressauth/status.go`
  - `internal/pressauth/list.go`
  - `internal/pressauth/forget.go`
  - `internal/pressauth/status_test.go`
- **Approach**: `status` reads state, decodes JWT, prints `valid until 2026-05-12T18:24:00Z (29m remaining)` or `expired 14m ago — run press-auth refresh`. `list` walks `~/.press-auth/*.json` and prints a table (domain, captured_at, expiry). `forget` removes the state file and the matching keychain entry.
- **Patterns to follow**: Existing table-output helpers in `cliutil` (used by every printed CLI for list commands).
- **Test scenarios**:
  - `status example.com` against a valid state file → prints "valid" with remaining time, exit 0
  - `status example.com` against an expired state → prints "expired" with elapsed time, exit 2
  - `status missing.example.com` → prints "not captured — run press-auth login", exit 3
  - `list` with three state files → prints all three sorted by domain, with human-friendly timestamps
  - `list` with zero state files → prints "no domains captured yet" (helpful empty state, not silent)
  - `forget example.com` → removes the state file AND the keychain entry; subsequent `status` reports "not captured"
  - `forget example.com` when state doesn't exist → exit code 0 (idempotent), prints "nothing to forget"
  - `forget --all --yes` → removes all state files + keychain entries; without `--yes` requires interactive confirmation
- **Verification**: `go test ./internal/pressauth/` passes. Manual: `press-auth list` after capturing 2 domains shows both with correct expiries.

---

### U6. Template change: `auth_browser.go.tmpl` adds press-auth as preferred path

- **Goal**: Generated `auth.go` calls `press-auth cookies <domain>` first and falls back to the existing chain. The fallback message names press-auth as the recommended install path.
- **Requirements**: R5, R6
- **Dependencies**: U4 (functional press-auth binary required for the path to actually work)
- **Files**:
  - `internal/generator/templates/auth_browser.go.tmpl`
  - `testdata/golden/auth_browser/` (golden fixtures need regeneration)
  - `scripts/golden.sh` invocation result
- **Approach**: Insert a new step BEFORE the current Step 1 ("detect cookie extraction tool"). The new step shells out to `press-auth` if found. On success, skip the rest. On failure, fall through unchanged. The final error message (when all paths fail) gains a new "Recommended fix" line: `go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest && press-auth login <domain>`. Update the template documentation comments at the top to reflect the new flow.
- **Patterns to follow**: Existing additive template changes in `auth_browser.go.tmpl` (e.g., the Auth0 path that was added without breaking the bearer path). Use `exec.LookPath` and `exec.Command` consistent with how `extractLiveCookies` shells out today.
- **Test scenarios**:
  - Generate a fresh CLI with composed-auth spec. Inspect generated `internal/cli/auth.go`. Confirm the new press-auth path is present and is checked BEFORE the cookie-tool detection.
  - With `press-auth` not on PATH, the generated `auth login --chrome` reaches the fallback chain exactly as today (no behavior regression).
  - With `press-auth` on PATH but no state for this domain, the generated command receives a clean error from press-auth and surfaces it to the user with the suggested `press-auth login` command in the message.
  - With `press-auth` on PATH and valid state, `<cli> account login-status --json` succeeds where it fails today (true end-to-end check).
  - Golden tests pass after intentional update (`scripts/golden.sh update` once the diff is reviewed).
- **Verification**: Regenerate `alaska-airlines-pp-cli`. After `go install ./cmd/press-auth && press-auth login alaskaair.com --login-url https://www.alaskaair.com/account/login --refresh-endpoint /account/token --jwt-carrier-cookie guestsession`, the user runs `alaska-airlines-pp-cli atmos-rewards balance --member 211405880 --json` and gets a live 200 response.

---

### U7. Spec extension: `auth_companion` block + parser support

- **Goal**: Internal YAML specs and OpenAPI specs can declare the login URL and JWT carrier cookie. Parser passes them into the generator so `auth.go` can pre-fill `press-auth login` arguments.
- **Requirements**: R5
- **Dependencies**: U6
- **Files**:
  - `internal/spec/parser.go` (extend the auth-block schema)
  - `internal/spec/parser_test.go`
  - `internal/openapi/parser.go` (add `x-auth-companion` handling)
  - `internal/openapi/parser_test.go`
  - `docs/SPEC-EXTENSIONS.md` (document the new keys)
  - `internal/generator/templates/auth_browser.go.tmpl` (consume the new fields)
- **Approach**: Add optional fields `login_url`, `login_complete_selector`, `jwt_carrier_cookie` to the auth block. Parser is tolerant: missing fields fall back to a generic `auth login --chrome` flow that prompts the user for the login URL interactively. Generated `auth login --chrome --auto` invokes press-auth non-interactively when the spec carries all hints; without the flag (or without spec hints), generated command prompts.
- **Patterns to follow**: `docs/SPEC-EXTENSIONS.md` already documents `x-auth-vars` and similar extensions. Same style and shape.
- **Test scenarios**:
  - Internal YAML with all new fields → parses cleanly, fields are accessible in the spec model
  - Internal YAML missing `login_url` → parses cleanly, field is empty string, generated CLI prompts
  - OpenAPI spec with `x-auth-companion: { login_url: "...", jwt_carrier_cookie: "..." }` → parses identical to internal YAML
  - Invalid `login_url` (not HTTPS) → validation warning surfaced, generation continues
  - `login_complete_selector` is preserved verbatim through to the generated auth.go (no escaping bugs)
  - `docs/SPEC-EXTENSIONS.md` documentation example round-trips through the parser
- **Verification**: `go test ./internal/spec ./internal/openapi` passes. Regenerate alaska-airlines spec with the new fields and confirm the press-auth invocation in generated `auth.go` includes them.

---

### U8. Skill reference + AGENTS.md + retro file

- **Goal**: The press skill documents press-auth as part of the cookie/composed-auth playbook. AGENTS.md flags the press-auth dependency. The bug history is captured in `docs/solutions/`.
- **Requirements**: R5
- **Dependencies**: U6
- **Files**:
  - `skills/printing-press/references/auth-companion.md` (new — primary reference)
  - `skills/printing-press/SKILL.md` (link to new reference from the auth-section that already exists)
  - `AGENTS.md` (one-line addition under "Quality Gates" noting `press-auth` is the canonical capture path for cookie/composed auth)
  - `docs/solutions/auth-login-chrome-broken-2026-05.md` (new — captures the bug, root cause, and the press-auth fix)
- **Approach**: `auth-companion.md` covers: when to recommend press-auth installation in user output, how to set up press-auth for first-time use, how to debug capture failures, how to scope login URLs and selectors per API. SKILL.md additions are minimal pointers — keep the SKILL file lean.
- **Patterns to follow**: Existing reference files in `skills/printing-press/references/` (e.g., `browser-sniff-capture.md`). Same YAML frontmatter, same prose conventions.
- **Test scenarios**:
  - `auth-companion.md` is referenced from SKILL.md so it's actually loaded when needed (no orphan-reference)
  - The `docs/solutions/` entry has correct frontmatter (`module`, `tags`, `problem_type`) per the convention
  - Markdown linting passes on all three new files
- **Test expectation: none — these are docs/reference files only; the doc-link integrity check above is the only real assertion.**
- **Verification**: A new contributor reading SKILL.md can find `auth-companion.md`, understand when to recommend `press-auth login` to a user, and copy-paste the install command.

---

## System-Wide Impact

| Surface | Impact |
|---------|--------|
| `cmd/press-auth/` | New binary; users install via `go install`. Adds chromedp + keychain deps to `go.mod` |
| `internal/generator/templates/auth_browser.go.tmpl` | Modified — adds a new preferred path. Golden tests need a one-time intentional update |
| Every existing printed CLI with cookie/composed auth | Picks up the fix on next regen. Until then, falls back to today's broken behavior — but with a clearer error message naming press-auth |
| `internal/spec/parser.go`, `internal/openapi/parser.go` | New optional fields. Backwards compatible (omitted fields use defaults) |
| `docs/SPEC-EXTENSIONS.md` | Documents the new auth_companion fields |
| `skills/printing-press/SKILL.md` | One-line addition pointing to the new reference |
| `~/.press-auth/` directory on user machines | New filesystem state. Documented in press-auth `--help` |
| macOS Keychain | New entries under `press-auth: <domain>`. User sees a one-time "Always Allow" prompt per domain |
| CI | New test surface for `internal/pressauth/`. Headless chromedp adds Chrome binary dependency to CI containers — needs ubuntu-with-chromium image or equivalent |

---

## Risk Analysis

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| chromedp + Chrome version skew breaks the launcher | Medium | High | Pin Chrome version detection at startup; fall back to "you need Chrome installed" error with download link. Run capture flow nightly in CI. |
| Keychain prompts surprise the user (security alert on first run) | High | Low | press-auth `--help` and SKILL.md `auth-companion.md` both document the one-time prompt up front. Ship a confirmation message before the keychain write: "macOS will ask permission to store the encryption key. Click 'Always Allow'." |
| User has multiple Chrome profiles and wants to use a specific one's session | Medium | Medium | v1 uses a fresh user-data-dir (no profile reuse), so this is a non-issue for v1. Deferred for follow-up if requested. |
| Refresh endpoint logic varies wildly between APIs | High | Medium | Make the refresh logic spec-driven (the spec declares the endpoint path; press-auth follows Set-Cookie response standards). For APIs that need POST bodies or special headers, defer to v2 with a `--refresh-method`/`--refresh-body` flag. |
| Captured cookie values leak into logs or error messages | Low | High | Cardinal rule in the codebase (`secret-protection.md`). Add a redact pass at the press-auth log layer: any log line containing a known cookie value is replaced with `<REDACTED>`. Unit test: log a cookie value, assert the log line contains `<REDACTED>` not the value. |
| State file corruption between concurrent press-auth invocations | Low | Medium | Atomic writes via temp-file + rename. Reads are best-effort; a corrupt file produces a clean error pointing to `press-auth forget && press-auth login` to recover. |
| AES-GCM nonce reuse if implementation is sloppy | Low | Critical | Use `crypto/rand` for every nonce; assert in tests that two encryptions of the same plaintext produce different ciphertexts. |

---

## Dependencies / Prerequisites

- Go 1.26.3+ (already required by the press)
- `github.com/chromedp/chromedp` (new dependency)
- `github.com/keybase/go-keychain` (new dependency, darwin-only via build tags)
- Google Chrome installed on the user's machine (already implicit for the press's Surf transport workflow)
- macOS as v1 target (Linux/Windows are follow-up plans)

---

## Verification Strategy

1. **Unit tests**: every file in `internal/pressauth/` has a `_test.go`. Coverage target: 80%+ on logic paths; chromedp integration is unit-tested via interface seams and end-to-end-tested separately.
2. **Integration test**: `testdata/pressauth/fake-login-server` serves a deterministic login page. CI runs headless chromedp against it, captures, refreshes (simulated), and verifies state-file integrity.
3. **End-to-end manual smoke test**: documented in `auth-companion.md`. Capture session for a real API (e.g., httpbin.org with cookie-set endpoint), verify `<cli> account login-status` succeeds.
4. **Regen golden**: `scripts/golden.sh verify` after template change. Intentional update accompanied by a one-paragraph diff explanation in the PR.
5. **alaska-airlines regression test**: regenerate `alaska-airlines-pp-cli`, run `press-auth login`, verify `alaska-airlines-pp-cli atmos-rewards balance --member 211405880 --json` returns 200. This is the "the bug that motivated this work is fixed" gate.

---

## Sequencing

Sequential, except where noted:

1. U1 (scaffold)
2. U2 (state file) — depends on U1
3. U3 (chromedp capture) — depends on U2, can develop in parallel with U4
4. U4 (refresh) — depends on U2, can develop in parallel with U3
5. U5 (remaining subcommands) — depends on U2+U4
6. U6 (template change) — depends on U4 (binary must work before template uses it)
7. U7 (spec extension) — depends on U6
8. U8 (docs) — depends on U6, can land in same PR as U7

Phased delivery option: U1–U5 ship as v1 of `press-auth` (the binary works standalone, users can adopt it manually). U6–U8 ship as v2 (press templates integrate it). This lets us validate the binary before committing template golden updates.

---

## Open Questions

| Question | Resolution path |
|----------|-----------------|
| Should `press-auth` ship as a separate Homebrew formula or only as `go install`? | Defer until v2; `go install` is enough for v1's audience. |
| Do we want a `press-auth daemon` mode that warms cookies before they expire? | Deferred. Lazy refresh in v1 should cover the common case. |
| Should the chromedp window be headless by default or visible? | Visible (R2 says "interactive login flow"). Headless makes login impossible — the user has to interact. |
| Cross-domain cookies (e.g., alaskaair.com sets cookies for vacations.alaskaair.com) | v1 only captures cookies whose Domain matches `spec.cookie_domain` (with subdomain match per RFC 6265). Users can run `press-auth login` separately for each subdomain. |
| If the user re-logs in (`press-auth login <domain>` while state exists), we overwrite? | Yes, with a one-line warning. `press-auth login --force` skips the warning. |

---

## Origin

This plan was generated from a direct user request during a session that hit the bug live:

> "Make a plan to fix this. Off has to work. You need to give it an off program where it can automatically pull my cookies or whatever it needs. So you design that experience right now."

The live failure happened on `alaska-airlines-pp-cli auth login --chrome` after publishing v0.1 to `mvanhorn/printing-press-library#515`. The failing trace and root cause analysis are in the conversation transcript.

No upstream brainstorm doc existed; this is a solo plan with no `ce-brainstorm` predecessor.
