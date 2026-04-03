---
title: "feat: Browser cookie auth — sniff-to-CLI runtime pipeline"
type: feat
status: draft
date: 2026-04-02
---

# feat: Browser cookie auth — sniff-to-CLI runtime pipeline

## Overview

When the sniff discovers a cookie-authenticated API (no developer API keys, just browser sessions), the printed CLI should support `auth login --chrome` — reading Chrome cookies at login time so the CLI works whenever the user is logged in via their browser. This threads through the entire pipeline: sniff detection, spec generation, template rendering, and the final CLI's auth/doctor/README.

## Problem Frame

Today, when the sniff detects `cookie` auth, it writes `Format: "informational only; no template support"` into the spec. The generated CLI has no way to use cookie auth. The user gets a CLI that can't authenticate to the API it was built for — unless they manually extract cookies from DevTools and paste them into env vars. That's the exact friction this feature eliminates.

The insight: if the user is logged into the site in Chrome, the CLI should just work. Cookies are read once during an explicit `auth login --chrome` action, saved to the CLI's config, and used for subsequent requests. The cookies stay in Chrome; the CLI reads them at login time, not silently on every request.

## Requirements Trace

- R1. Sniff detects cookie auth and records `CookieDomain` (from `AuthCapture.BoundDomain`)
- R2. Sniff validates that Chrome cookies exist for the domain and that they produce authenticated responses — before promising the feature in the generated CLI
- R3. If validation fails, the sniff tells the user: "Authenticated endpoints discovered but browser auth won't work for this API (CSRF/cookie policy). These endpoints will be included but marked auth-required."
- R4. If validation passes, `Auth.Type = "cookie"` flows into the spec with `CookieDomain` populated
- R5. Generator selects `auth_browser.go.tmpl` when `Auth.Type == "cookie"`
- R6. Generated CLI has `auth login --chrome` that reads Chrome cookies for the domain and saves to config
- R7. Generated CLI has `auth status` that shows browser session state and expiry
- R8. Generated CLI's `doctor` checks for cookie tool availability and session freshness
- R9. Generated CLI's README documents `auth login --chrome` in the Quick Start
- R10. Cookie extraction is done by shelling out to an external tool — no custom crypto in printed CLIs
- R11. `doctor` recommends installing a cookie tool if none found, with install instructions
- R12. The client uses saved cookies from config — no special cookie injection path at request time

## Scope Boundaries

- macOS Chrome only for v1. Firefox/Safari/Edge/Linux/Windows are future work.
- No custom cookie decryption code. Shell out to an existing tool.
- No silent/ambient cookie reading — only during explicit `auth login --chrome`.
- No CSRF auto-detection in v1. If CSRF is required, validation (R2) will fail and the feature won't be offered.
- The cookie tool is not bundled — it's a recommended install, like browser-use/agent-browser for sniff.

## Context & Research

### Cookie extraction tools (shell-out candidates)

The printed CLI shells out to one of these at `auth login --chrome` time:

| Tool | Install | Output | Notes |
|------|---------|--------|-------|
| `pycookiecheat` | `pip install pycookiecheat` | JSON (domain-filtered) | 809 stars, maintained since 2015, handles Chrome v24 format. Requires Python. Most battle-tested. |
| `barnardb/cookies` | `brew install barnardb/cookies/cookies` | Cookie header string | 74 stars, Go/Homebrew, built on kooky. Easiest macOS install. |
| `cookie-scoop-cli` | `cargo install cookie-scoop-cli` | JSON or header | Rust, static binary. Inspired by steipete/sweet-cookie. Newest. |

The printed CLI checks for any of these (same pattern as browser-use vs agent-browser detection in sniff). `doctor` reports which is available and how to install if none found.

### Underlying algorithm (stable, well-understood)

All three tools implement the same ~100 lines of logic:
1. Copy Chrome's `~/Library/Application Support/Google/Chrome/Default/Cookies` SQLite file
2. Query for cookies matching the target domain
3. Get Keychain password via `/usr/bin/security find-generic-password -s "Chrome Safe Storage" -w`
4. PBKDF2 + AES-128-CBC decrypt

This scheme has been stable for ~8 years on macOS. The Keychain prompt is the only user-visible side effect (one-time if "Always Allow" is clicked).

### Relevant code and patterns

| Purpose | File |
|---------|------|
| Current cookie auth detection (returns "informational only") | `internal/websniff/specgen.go:318-332` |
| AuthConfig struct (needs `CookieDomain` field) | `internal/spec/spec.go:27-38` |
| AuthCapture with BoundDomain | `internal/websniff/types.go:52-58` |
| Auth template selection logic | `internal/generator/generator.go:341-350` |
| Simple auth template (model for browser auth template) | `internal/generator/templates/auth_simple.go.tmpl` |
| Doctor template (needs browser session check) | `internal/generator/templates/doctor.go.tmpl` |
| README template (needs cookie auth Quick Start section) | `internal/generator/templates/README.md.tmpl` |
| Client template (no changes — uses config like all auth types) | `internal/generator/templates/client.go.tmpl` |
| Sniff skill Phase 1.6 (pre-sniff auth intelligence) | `skills/printing-press/SKILL.md:510-551` |
| Sniff capture reference (session transfer) | `skills/printing-press/references/sniff-capture.md` |

## Key Technical Decisions

### D1: Cookie tool detection order

Check in this order: `pycookiecheat` (most reliable) > `cookies` (barnardb, easiest install) > `cookie-scoop-cli` (newest).

### D2: Cookies saved to config, not re-read per request

`auth login --chrome` reads cookies once and saves them to the CLI's config file (same `SaveTokens` mechanism used by OAuth2 and API key auth). All subsequent requests use config. This means:
- No repeated Chrome reads or Keychain prompts
- Standard config auth path in client.go — no special cookie injection
- User runs `auth login --chrome` again when session expires

### D3: Sniff-time validation before promising the feature

Before the sniff generates a spec with `Auth.Type = "cookie"`, it must validate that the cookies actually produce authenticated responses. The sniff already has the cookies (it used them to capture traffic). Validation:
1. Pick one endpoint that returned 200 during sniff
2. Replay the request with cookies → expect 200
3. Replay without cookies → expect 401/403 or different response
4. If both succeed identically, cookies aren't actually required — mark as `Auth.Type = "none"`
5. If cookied request fails, something is wrong (CSRF, cookie policy) — warn user, include endpoints but don't offer browser auth in the CLI

### D4: Config storage format for cookies

Cookies are stored as a single `Cookie` header value in the config (e.g., `token_v2=abc123; notion_browser_id=xyz`). This maps directly to `req.Header.Set("Cookie", value)` in the client. No changes to `config.go.tmpl` beyond treating it like any other auth header value.

## Implementation Plan

### Task 1: Add `CookieDomain` to `AuthConfig`

**File:** `internal/spec/spec.go`

Add `CookieDomain string` field to `AuthConfig`:
```go
CookieDomain string `yaml:"cookie_domain,omitempty" json:"cookie_domain,omitempty"`
```

**Test:** Update any existing AuthConfig tests to include the new field.

### Task 2: Wire cookie auth detection in specgen

**File:** `internal/websniff/specgen.go`

Change `detectCapturedAuth` (lines 318-332) to populate real auth config instead of "informational only":

```go
case "cookie":
    return spec.AuthConfig{
        Type:         "cookie",
        Header:       "Cookie",
        In:           "cookie",
        CookieDomain: capture.BoundDomain,
        EnvVars:      envVarsOrNil(envPrefix, "COOKIES"),
    }
```

Both the `len(capture.Headers) > 0` and `len(capture.Cookies) > 0` branches.

**Test:** Update `specgen_test.go` — cookie auth capture should produce a usable AuthConfig, not "informational only".

### Task 3: Create `auth_browser.go.tmpl`

**File:** `internal/generator/templates/auth_browser.go.tmpl`

New template for cookie-auth CLIs. Provides three subcommands:
- `auth login --chrome` — detects cookie tool, shells out, saves cookies to config
- `auth status` — shows session state, domain, cookie count, approximate expiry
- `auth logout` — clears saved cookies from config

Cookie tool detection logic (same pattern as sniff tool detection):
```go
func detectCookieTool() (string, error) {
    for _, tool := range []struct{ name, check string }{
        {"pycookiecheat", "python3 -m pycookiecheat --help"},
        {"cookies", "cookies --help"},
        {"cookie-scoop-cli", "cookie-scoop --help"},
    } {
        if exec.Command("sh", "-c", tool.check).Run() == nil {
            return tool.name, nil
        }
    }
    return "", fmt.Errorf("no cookie tool found")
}
```

Extraction dispatch (each tool has different invocation/output format):
- `pycookiecheat`: `python3 -c "from pycookiecheat import chrome_cookies; ..."` → JSON
- `cookies`: `cookies https://<domain>` → Cookie header string
- `cookie-scoop-cli`: `cookie-scoop --domain <domain> --header` → Cookie header string

Save the resulting cookie header string to config via `cfg.SaveTokens()`.

### Task 4: Generator template selection for cookie auth

**File:** `internal/generator/generator.go`

Update auth template selection (line 341-350) to add a third branch:

```go
authTmpl := "auth_simple.go.tmpl"
if g.Spec.Auth.AuthorizationURL != "" {
    authTmpl = "auth.go.tmpl"
} else if g.Spec.Auth.Type == "cookie" {
    authTmpl = "auth_browser.go.tmpl"
}
```

### Task 5: Update `doctor.go.tmpl` for browser auth

**File:** `internal/generator/templates/doctor.go.tmpl`

Add cookie-auth-specific checks inside the `{{- else}}` block (after line 39):

```go
{{- if eq .Auth.Type "cookie"}}
// Check cookie tool availability
cookieTool, toolErr := detectCookieTool()
if toolErr != nil {
    report["cookie_tool"] = "not found — install: pip install pycookiecheat"
} else {
    report["cookie_tool"] = cookieTool
}
// Check if browser session is fresh
if header != "" {
    report["auth"] = "configured (browser session)"
} else {
    report["auth"] = "not configured — run: auth login --chrome"
}
{{- else}}
// ... existing non-cookie auth checks
{{- end}}
```

### Task 6: Update `README.md.tmpl` for cookie auth

**File:** `internal/generator/templates/README.md.tmpl`

Add a `{{- else if eq .Auth.Type "cookie"}}` branch after line 68:

```markdown
### 2. Authenticate

This CLI uses your browser session for authentication. Log in to {{.Auth.CookieDomain}} in Chrome, then:

    {{.Name}}-pp-cli auth login --chrome

Requires a cookie extraction tool. Install one:

    pip install pycookiecheat          # Python (recommended)
    brew install barnardb/cookies/cookies  # Homebrew

When your session expires, run `auth login --chrome` again.
```

### Task 7: Sniff-time cookie validation

**File:** `skills/printing-press/references/sniff-capture.md`

Add a new step after traffic capture and before spec generation: **Step 2d: Cookie Auth Validation**.

When the sniff detects cookie auth in captured traffic:

1. Select one endpoint that returned 200 during capture
2. Replay with captured cookies → should get 200
3. Replay without cookies → should get 401/403 or clearly different response
4. **Pass**: Both conditions met → cookie auth is viable, proceed with `Auth.Type = "cookie"` and `CookieDomain` in spec
5. **Fail**: Cookied request fails (CSRF/SameSite issues) → warn user:
   > "Authenticated endpoints were discovered but browser cookie auth won't work for this API (likely requires CSRF tokens or has strict cookie policies). These endpoints will be included in the spec but the generated CLI won't offer `auth login --chrome`. You'll need to manually provide auth tokens."

   Set `Auth.Type = "none"` with a note in the sniff report.

### Task 8: Update Phase 1.6 skill text

**File:** `skills/printing-press/SKILL.md`

In the browser session auth section (line 541-548), add context about the `--chrome` login that will be available:

> "If you're logged in, the generated CLI will include `auth login --chrome` — you'll be able to authenticate the CLI just by being logged into the site in Chrome. No API key needed."

This sets the user's expectation during the sniff decision.

## Dependency Graph

```
Task 1 (AuthConfig field)
  └→ Task 2 (specgen wiring)
       └→ Task 7 (sniff validation — skill change, no Go code dependency)
  └→ Task 3 (auth_browser template)
       └→ Task 4 (generator selection)
  └→ Task 5 (doctor template)
  └→ Task 6 (README template)
Task 8 (skill text — independent)
```

Tasks 3, 5, 6, 8 can be done in parallel after Task 1.

## UX Flow (end to end)

### During sniff (printing press run)

```
Phase 1.6: "This API has order history and saved addresses that require a logged-in session.
            Are you logged in to dominos.com in Chrome?"
            → User: "Yes"

Phase 1.7: Authenticated sniff runs, captures cookie-auth traffic

Step 2d:   Cookie validation passes — browser auth will work

Phase 2:   generate receives spec with Auth.Type=cookie, CookieDomain=.dominos.com
```

### In the generated CLI

```bash
$ dominos-pp-cli doctor
✓ config          ok
✓ cookie_tool     pycookiecheat
⚠ auth            not configured — run: dominos-pp-cli auth login --chrome
✓ connectivity    dominos.com reachable

$ dominos-pp-cli auth login --chrome
🔑 Reading Chrome cookies for .dominos.com...
   [macOS Keychain prompt — user clicks Allow]
✓ Found 8 cookies for .dominos.com
✓ Session saved to ~/.config/dominos-pp-cli/config.toml

$ dominos-pp-cli orders list
# works

# 3 hours later, session expired:
$ dominos-pp-cli orders list
✗ 401 Unauthorized

$ dominos-pp-cli auth login --chrome
✓ Session refreshed
```

## What's NOT in scope

- Custom cookie decryption code in printed CLIs
- Cross-platform (Linux/Windows) or multi-browser (Firefox/Safari)
- CSRF token auto-detection or auto-fetching
- Silent/ambient cookie reading (every request hitting Chrome)
- Bundling a cookie tool inside the printed CLI binary
