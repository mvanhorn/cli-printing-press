---
title: "feat: Composed cookie auth for sniff-discovered APIs"
type: feat
status: active
date: 2026-04-03
origin: docs/brainstorms/2026-04-03-composed-cookie-auth-requirements.md
---

# feat: Composed cookie auth for sniff-discovered APIs

## Overview

When the sniff discovers an API that uses a custom Authorization header composed from browser cookie values (e.g., `PagliacciAuth {customerId}|{authToken}`), the printed CLI should support `auth login --chrome` that reads the specific named cookies, composes the header, and saves it — so the user never sees the format.

## Problem Frame

The sniff pipeline now discovers custom auth headers (Step 2a.1.5 item 5) and traces values back to cookies, but the spec format can't represent this, the generator has no template for it, and the printed CLI falls back to generic "paste your token" auth. This feature threads the composed pattern through the entire pipeline: spec → generator → printed CLI runtime. (see origin: `docs/brainstorms/2026-04-03-composed-cookie-auth-requirements.md`)

## Requirements Trace

- R1. `AuthConfig` supports `Type: "composed"`
- R2. `AuthConfig.Format` carries the header template with `{cookieName}` placeholders
- R3. `AuthConfig.Cookies` (new field) lists cookie names to extract
- R4. `AuthConfig.CookieDomain` carries the domain (already exists)
- R5. Generator selects composed auth template when `Auth.Type == "composed"`
- R6. `auth login --chrome` reads specific named cookies, composes the header, saves to config
- R7-R8. `auth status` and `auth logout` work for composed auth
- R9. 401 fails with login URL + `auth login --chrome` command (no prompting)
- R10. `doctor` checks for cookie extraction tool
- R11. README documents `auth login --chrome`
- R12-R14. Sniff traces header values → cookie names → format string → spec
- R15. No credential flow — browser is auth source of truth
- R16. Same cookie extraction tools as existing `auth_browser.go.tmpl`
- R17-R18. Composed header saved to config, attached via `Auth.Header`
- R19. Missing cookies → print login URL, exit non-zero

## Scope Boundaries

- No credential-based login — Chrome import only
- No auto-retry on 401 — fail with guidance
- macOS Chrome only for v1
- Does not change existing `cookie` auth type behavior
- Sniff pipeline changes are skill instruction changes only (no Go binary changes for sniff)

## Context & Research

### Relevant Code and Patterns

- `internal/spec/spec.go:27-39` — `AuthConfig` struct. Has `Format` (unused for cookie auth), `CookieDomain`, but no `Cookies` field.
- `internal/generator/generator.go:340-352` — Auth template selection: OAuth2 → `auth.go.tmpl`, cookie → `auth_browser.go.tmpl`, else → `auth_simple.go.tmpl`.
- `internal/generator/templates/auth_browser.go.tmpl` — 559 lines. Cookie extraction (detectCookieTool, extractViaPycookiecheat, discoverChromeProfiles, resolveChromeProfile) is ~400 lines of reusable infrastructure. The pycookiecheat path returns `map[string]string` keyed by cookie name — perfect for picking named cookies.
- `internal/generator/templates/config.go.tmpl:129-139` — AuthHeader() for cookie type returns `c.AccessToken` directly. Does not use `Format` field.
- `internal/generator/templates/doctor.go.tmpl:42-69` — Cookie tool availability check.
- `internal/generator/templates/readme.md.tmpl:69-87` — Cookie auth Quick Start section.
- `internal/generator/templates/client.go.tmpl:336-346` — Auth header attachment. Uses `Auth.Header` for header name (already supports custom names like `Authorization`).
- `internal/websniff/specgen.go:318-335` — Sets `Auth.Type: "cookie"` from captures.

### Key Insight: Extend auth_browser.go.tmpl, Don't Duplicate

The existing `auth_browser.go.tmpl` has ~400 lines of cookie extraction infrastructure (tool detection, Chrome profile discovery, cookie extraction via pycookiecheat/cookies/cookie-scoop). The composed auth path differs only in the *post-extraction* step (~20 lines): instead of joining all cookies into a string, it picks specific named cookies and fills a format template.

Duplicating the entire 559-line template for ~20 lines of different logic is wrong. Instead, extend the existing template with Go template conditionals.

## Key Technical Decisions

- **Extend `auth_browser.go.tmpl` rather than create a new template:** The cookie extraction infrastructure is identical. Add conditional logic within the template for the composed path. The generator still selects the same `auth_browser.go.tmpl` for both `cookie` and `composed` types. Rationale: ~400 lines of shared code, only ~20 lines differ.
- **`Cookies []string` as a new field on AuthConfig:** No existing field serves this purpose. A simple string slice with YAML tag `cookies` is the right representation.
- **Config stores the composed header value, not individual cookies:** After composition, the result (`"PagliacciAuth 2432962|FD44DA6A..."`) is stored as `AccessToken` in config — same as the cookie type. The composition happens at `auth login --chrome` time, not at request time. Rationale: simpler client code, no need to re-compose on every request.
- **Format string replacement is literal, not regex:** `strings.ReplaceAll(format, "{cookieName}", cookieValue)` for each named cookie. No special character escaping needed because values are literal strings from the browser's cookie store.

## Open Questions

### Resolved During Planning

- **Cookies field representation:** New `Cookies []string` field on `AuthConfig`. YAML: `cookies: [customerId, authToken]`. Simple, matches how it appears in the requirements doc.
- **Code sharing between cookie and composed:** Same template (`auth_browser.go.tmpl`) with conditional branches. Generator selects `auth_browser.go.tmpl` for both `Auth.Type == "cookie"` and `Auth.Type == "composed"`.
- **Special characters in cookie values:** pycookiecheat returns decoded values. Format string replacement uses `strings.ReplaceAll` (literal match). No regex, no escaping issues.

### Deferred to Implementation

- Exact placement of the conditional branches within auth_browser.go.tmpl — depends on the template's current structure which may change on the branch.

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```
auth_browser.go.tmpl decision flow:

  auth login --chrome
    │
    ├── detectCookieTool()          ← shared
    ├── resolveChromeProfile()      ← shared
    ├── extractCookies()            ← shared (returns map[string]string)
    │
    ├── IF Auth.Type == "cookie":
    │     Join all cookies → "k1=v1; k2=v2; ..."
    │     Save as AccessToken
    │
    └── IF Auth.Type == "composed":
          For each cookie in Auth.Cookies:
            Look up value in extracted map
            If missing → error "cookie {name} not found"
          Replace {name} placeholders in Auth.Format
          Save composed header as AccessToken

  config.AuthHeader():
    cookie:    return AccessToken (raw cookie string)
    composed:  return AccessToken (pre-composed header value)
    
  client.go.tmpl:
    cookie:    req.Header.Set("Cookie", authHeader)
    composed:  req.Header.Set("Authorization", authHeader)
    ↑ Already handled by Auth.Header field
```

## Implementation Units

- [ ] **Unit 1: Add Cookies field to AuthConfig**

**Goal:** The spec struct can represent which named cookies to extract for composed auth.

**Requirements:** R1, R3

**Dependencies:** None

**Files:**
- Modify: `internal/spec/spec.go`
- Test: `internal/spec/spec_test.go`

**Approach:**
- Add `Cookies []string` field to `AuthConfig` with YAML/JSON tags `cookies`
- Update the `Type` comment to include `composed` in the enum list
- Add a test case that parses a spec YAML with `type: composed`, `format`, `cookies`, and `cookie_domain` and verifies all fields are populated

**Patterns to follow:**
- Existing field style in `AuthConfig` (e.g., `Scopes []string`)

**Test scenarios:**
- Happy path: Parse YAML with `auth.type: composed, auth.cookies: [customerId, authToken]` → AuthConfig.Cookies == ["customerId", "authToken"]
- Happy path: Parse YAML with `auth.type: cookie` (no cookies field) → AuthConfig.Cookies is nil (regression)
- Edge case: Parse YAML with empty cookies list → AuthConfig.Cookies == []

**Verification:**
- `go test ./internal/spec/` passes with new test cases
- Existing spec tests unchanged

- [ ] **Unit 2: Extend generator to select auth_browser.go.tmpl for composed type**

**Goal:** The generator routes `Auth.Type == "composed"` to the same template as `cookie`, so the template can handle both.

**Requirements:** R5

**Dependencies:** Unit 1

**Files:**
- Modify: `internal/generator/generator.go` (auth template selection ~line 347)
- Test: `internal/generator/generator_test.go`

**Approach:**
- Change the condition at line 347 from `g.Spec.Auth.Type == "cookie"` to `g.Spec.Auth.Type == "cookie" || g.Spec.Auth.Type == "composed"`
- Add a generator test that creates a spec with `Auth.Type: "composed"` and verifies the generated CLI compiles and has `auth login --chrome`

**Patterns to follow:**
- Existing generator tests that verify template selection (search for `auth_browser` or `CookieDomain` in generator_test.go)

**Test scenarios:**
- Happy path: Generate with `Auth.Type: "composed"` → auth.go file exists, contains "chrome" flag
- Regression: Generate with `Auth.Type: "cookie"` → same behavior as before
- Regression: Generate with `Auth.Type: "api_key"` → uses auth_simple.go.tmpl

**Verification:**
- `go test ./internal/generator/` passes
- Generated CLI for composed auth compiles with `go build ./...`

- [ ] **Unit 3: Extend auth_browser.go.tmpl for composed cookie logic**

**Goal:** The template handles both `cookie` (full cookie string) and `composed` (named cookies → format string) auth paths.

**Requirements:** R6, R7, R8, R9, R16, R17, R19

**Dependencies:** Units 1, 2

**Files:**
- Modify: `internal/generator/templates/auth_browser.go.tmpl`

**Approach:**
- In `newAuthLoginCmd`, after `extractCookies()` returns the cookie string, add a conditional:
  - If `Auth.Type == "composed"`: parse the cookie string back into a map (split on `; `, split each on `=`), look up each cookie in `Auth.Cookies`, replace `{name}` placeholders in `Auth.Format`, save the composed value
  - If `Auth.Type == "cookie"`: existing behavior (save the raw string)
- In the "cookies not found" error path: for composed, report which specific cookie was missing and print the login URL from CookieDomain
- In `newAuthStatusCmd`: add composed-specific status display showing the format and domain
- The `newAuthLogoutCmd` needs no change (already clears AccessToken)

**Key detail:** `extractViaPycookiecheat` already returns `name=value; name2=value2` format. Parsing this back to a map is straightforward. The `cookies` CLI and `cookie-scoop` return the same format.

**Patterns to follow:**
- Existing Go template conditionals in the file: `{{- if eq .Auth.Type "cookie"}}` patterns

**Test scenarios:**
- Happy path: Generate CLI with composed auth → `auth login --chrome` extracts named cookies, composes "PagliacciAuth {id}|{token}", saves to config
- Happy path: `auth status` for composed auth shows "composed" source and domain
- Error path: Named cookie not found in extracted set → error names the missing cookie and prints login URL
- Edge case: Cookie value contains `|` or `=` characters → format string composed correctly (literal replacement)
- Regression: Generate CLI with cookie auth → `auth login --chrome` still saves full cookie string

**Verification:**
- Generated CLI compiles
- `auth login --chrome --help` shows the chrome flag
- Template renders without Go template syntax errors for both `cookie` and `composed` types

- [ ] **Unit 4: Extend config.go.tmpl and client.go.tmpl for composed auth**

**Goal:** Config returns the composed header value; client attaches it with the correct header name.

**Requirements:** R17, R18

**Dependencies:** Unit 3

**Files:**
- Modify: `internal/generator/templates/config.go.tmpl`
- Modify: `internal/generator/templates/client.go.tmpl` (likely no change needed — verify)

**Approach:**
- In `config.go.tmpl AuthHeader()`: add a `composed` branch that returns `c.AccessToken` (same as cookie — the composition already happened at login time). Set `AuthSource = "chrome-composed"` to distinguish from "browser" in status output.
- In `client.go.tmpl`: verify the existing logic already works. `Auth.Header` for composed will be `"Authorization"` (not `"Cookie"`), so `req.Header.Set("Authorization", authHeader)` is correct. No changes expected.

**Patterns to follow:**
- Existing cookie branch in config.go.tmpl (lines 129-139)

**Test scenarios:**
- Happy path: Config with composed AccessToken → AuthHeader() returns the composed string
- Happy path: Client sets `Authorization: PagliacciAuth ...` header (not `Cookie:`)
- Regression: Config with cookie AccessToken → AuthHeader() returns raw cookie string

**Verification:**
- Generated CLI's config and client code compiles
- `Auth.Header` correctly controls the HTTP header name for composed type

- [ ] **Unit 5: Extend doctor.go.tmpl and readme.md.tmpl for composed auth**

**Goal:** Doctor checks for cookie tools; README documents the auth flow.

**Requirements:** R10, R11

**Dependencies:** Unit 3

**Files:**
- Modify: `internal/generator/templates/doctor.go.tmpl`
- Modify: `internal/generator/templates/readme.md.tmpl`

**Approach:**
- In `doctor.go.tmpl`: extend the existing cookie-type condition to also trigger for composed. `{{- else if or (eq .Auth.Type "cookie") (eq .Auth.Type "composed")}}` — the cookie tool check is identical.
- In `readme.md.tmpl`: add a composed section that explains "This CLI uses your browser session" and shows `auth login --chrome`. Nearly identical to the cookie section but can mention the specific domain.

**Patterns to follow:**
- Existing cookie sections in both templates

**Test scenarios:**
- Happy path: Doctor for composed CLI → checks for pycookiecheat, reports availability
- Happy path: README for composed CLI → Quick Start includes `auth login --chrome`
- Regression: Doctor for cookie CLI → unchanged behavior

**Verification:**
- Generated CLI's doctor command runs without error
- Generated README contains auth documentation

- [ ] **Unit 6: Update sniff skill instructions for composed auth detection**

**Goal:** The sniff pipeline writes `Auth.Type: composed` with format string, cookie names, and domain into the spec when it detects a composed cookie pattern.

**Requirements:** R12, R13, R14, R15

**Dependencies:** Units 1-5 (spec format must exist for the sniff to target it)

**Files:**
- Modify: `skills/printing-press/references/sniff-capture.md` (Step 2a.1.5 item 5)

**Approach:**
- In the auth header discovery step (Step 2a.1.5 item 5), after capturing the Authorization header and cookie values, add instructions for Claude to:
  1. Match each value in the header against cookie names from `document.cookie`
  2. Construct the format string by replacing literal values with `{cookieName}` placeholders
  3. Write the composed auth config into the spec YAML that Claude builds:
     ```yaml
     auth:
       type: composed
       header: Authorization
       format: "PagliacciAuth {customerId}|{authToken}"
       cookie_domain: pagliacci.com
       cookies: [customerId, authToken]
     ```
- Add a worked example using the Pagliacci pattern for clarity

**Patterns to follow:**
- Existing sniff-capture.md instruction style (numbered steps with bash examples)

**Test scenarios:**
- Happy path: Sniff discovers `Authorization: PagliacciAuth 2432962|FD44DA6A...` + cookies `customerId=2432962, authToken=FD44DA6A...` → spec has `type: composed, format: "PagliacciAuth {customerId}|{authToken}", cookies: [customerId, authToken]`
- Edge case: Sniff discovers `Authorization: Bearer {token}` where token is in cookie `auth_token` → spec has `type: composed, format: "Bearer {auth_token}", cookies: [auth_token]`
- Regression: Sniff discovers plain cookie auth (Cookie header with full string) → spec has `type: cookie` (not composed)

**Verification:**
- The sniff-capture.md instructions are clear enough that Claude produces the correct spec YAML for a composed auth pattern

## System-Wide Impact

- **Interaction graph:** The change touches the spec struct → generator → 4 templates → sniff skill. Each layer passes data to the next. No callbacks or middleware affected.
- **Error propagation:** Auth errors (missing cookies, missing tool) propagate as non-zero exit codes with human-readable messages. No new error types needed — extends existing patterns.
- **API surface parity:** The `auth` subcommands (`login --chrome`, `status`, `logout`) behave identically to the existing cookie type — same flags, same output patterns, just different internal composition.
- **Unchanged invariants:** Existing `cookie` auth type behavior is unchanged. The `api_key`, `bearer_token`, and `oauth2` paths are untouched. The `Auth.Format` field, though now used for composed auth, was previously unused for cookie auth — no existing behavior changes.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Template conditional complexity — auth_browser.go.tmpl grows with two code paths | The divergence is small (~20 lines). If it grows beyond that, extract shared code into template helper functions |
| Cookie extraction tools may not be installed | Same mitigation as existing cookie auth — doctor warns, auth login errors with install instructions |
| Sniff may not reliably trace header values to cookies (URL encoding, partial matches) | The sniff instructions include fallback: if cookie matching fails, report the auth scheme without composed config. The CLI falls back to generic token auth |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-04-03-composed-cookie-auth-requirements.md](docs/brainstorms/2026-04-03-composed-cookie-auth-requirements.md)
- Related plan: [docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md](docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md) — the original cookie auth plan that built auth_browser.go.tmpl
- Existing template: `internal/generator/templates/auth_browser.go.tmpl`
- Spec struct: `internal/spec/spec.go:27-39`
- Generator selection: `internal/generator/generator.go:340-352`
