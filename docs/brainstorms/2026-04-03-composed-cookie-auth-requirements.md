---
date: 2026-04-03
topic: composed-cookie-auth
---

# Composed Cookie Auth for Sniff-Discovered APIs

## Problem Frame

When the printing press sniffs a website to discover its API, it often finds that authentication uses a custom header composed from browser cookie values — not a standard Bearer token or API key. For example, Pagliacci Pizza uses `Authorization: PagliacciAuth {customerId}|{authToken}` where both values come from cookies set after login.

Today, the sniff discovers this pattern (Step 2a.1.5 item 5 captures the auth header and traces values to cookies), but the pipeline can't act on it. The spec has no way to express "compose this header from these cookie values," and the generator has no template for it. The printed CLI falls back to generic "paste your token" auth, which means the user has to manually extract cookies from DevTools and construct the header string — exactly the friction the printing press exists to eliminate.

The existing `auth_browser.go.tmpl` handles the simpler case where the entire cookie string IS the auth header (`Cookie: name=value; ...`). This feature handles the more common SPA pattern where specific cookies are read and composed into a custom Authorization header.

## Auth Pattern Flow

```
┌─────────────────────────────────────────────────────┐
│                    SNIFF TIME                        │
│                                                      │
│  XHR interceptor captures:                           │
│    Authorization: PagliacciAuth 2432962|FD44DA6A...  │
│                                                      │
│  Cookie scan finds:                                  │
│    customerId=2432962                                │
│    authToken=FD44DA6A-F91C-42E0-AB7D-8A2D35DD655F   │
│                                                      │
│  Sniff writes to spec:                               │
│    auth:                                             │
│      type: composed                                  │
│      format: "PagliacciAuth {customerId}|{authToken}"│
│      cookie_domain: pagliacci.com                    │
│      cookies: [customerId, authToken]                │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│                  GENERATE TIME                       │
│                                                      │
│  Generator sees Auth.Type == "composed"               │
│  Selects auth_composed.go.tmpl                       │
│  Emits CLI with:                                     │
│    - auth login --chrome                             │
│    - auth status                                     │
│    - auth logout                                     │
│    - doctor checks for cookie tool                   │
│    - README documents the auth flow                  │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│                  RUNTIME (user runs CLI)              │
│                                                      │
│  $ pagliacci-pp-cli rewards                          │
│  Error: not authenticated.                           │
│  Log in at https://pagliacci.com/login               │
│  Then run: pagliacci-pp-cli auth login --chrome      │
│                                                      │
│  $ pagliacci-pp-cli auth login --chrome              │
│  Reading cookies from Chrome for pagliacci.com...    │
│    ✓ Found customerId                                │
│    ✓ Found authToken                                 │
│  Session saved.                                      │
│                                                      │
│  $ pagliacci-pp-cli rewards                          │
│  Points: 847 | Next reward: 153 points away          │
└─────────────────────────────────────────────────────┘
```

## Requirements

**Spec Format**

- R1. `AuthConfig` supports `Type: "composed"` — a new auth type for headers composed from named cookie values
- R2. `AuthConfig.Format` carries the header template string with `{cookieName}` placeholders (e.g., `"PagliacciAuth {customerId}|{authToken}"`)
- R3. `AuthConfig.Cookies` (new field) lists the cookie names to extract, in the order they appear in the format string
- R4. `AuthConfig.CookieDomain` carries the domain to read cookies from (already exists)

**Generator**

- R5. Generator selects `auth_composed.go.tmpl` when `Auth.Type == "composed"`
- R6. The template emits `auth login --chrome` that reads the specific named cookies (not the entire cookie jar), composes them into the format string, and saves the resulting header value to config
- R7. The template emits `auth status` showing whether composed auth is configured, the domain, and the config path
- R8. The template emits `auth logout` clearing the saved header
- R9. When auth is not configured and a command fails with 401, the CLI prints the login URL and the `auth login --chrome` command — no interactive prompting, no auto-retry
- R10. The `doctor` template checks for cookie extraction tool availability when `Auth.Type == "composed"`
- R11. The README template documents `auth login --chrome` in Quick Start when `Auth.Type == "composed"`

**Sniff Pipeline**

- R12. The sniff's auth header discovery (Step 2a.1.5 item 5) traces captured Authorization header values back to cookie names by matching literal values against `document.cookie` entries
- R13. The sniff infers the format string by replacing literal cookie values with `{cookieName}` placeholders in the captured header
- R14. The sniff writes `Auth.Type: composed`, `Auth.Format`, `Auth.Cookies`, and `Auth.CookieDomain` into the spec when a composed pattern is detected
- R15. The sniff does NOT attempt to replay credentials or hit login endpoints — the browser is the auth source of truth

**Runtime Behavior**

- R16. `auth login --chrome` uses the same cookie extraction tools as `auth_browser.go.tmpl` (pycookiecheat, cookies CLI, cookie-scoop-cli) — the extraction mechanism is shared, only the post-extraction step differs
- R17. After extracting cookies, the CLI composes the header using the baked-in format string and saves it to config as the auth header value
- R18. The client attaches the composed header to requests using the header name from `Auth.Header` (typically `Authorization`)
- R19. If the required cookies are not found in Chrome (user not logged in), the CLI prints the login URL derived from `Auth.CookieDomain` and exits with a non-zero status

## Success Criteria

- A printed CLI for an API with composed cookie auth (like Pagliacci) has `auth login --chrome` that works end-to-end: reads cookies, composes header, saves to config, subsequent commands authenticate
- The user never sees the auth header format — they run one command and it just works
- The same cookie extraction tools work for both `auth_browser.go.tmpl` (full cookie string) and `auth_composed.go.tmpl` (named cookies → format string)
- Agent-friendly: no interactive prompts, clear error messages with exact commands, `--no-input` respected

## Scope Boundaries

- No credential-based login (no username/password flow in the CLI) — Chrome is the auth source
- No auto-opening the browser — print the URL, let the user navigate
- No auto-retry on 401 — fail with guidance
- macOS Chrome only for v1 (same constraint as existing `auth_browser.go.tmpl`)
- Does not change the existing `cookie` auth type — `composed` is a new type alongside it
- Does not require the sniff to discover the login endpoint — only the auth header pattern and cookie names

## Key Decisions

- **Chrome import only, no credential flow:** The user authenticates through the site's intended login page in their browser. The CLI reads the result. This avoids handling credentials, hitting undocumented login endpoints, and dealing with CSRF/captcha.
- **Fail with guidance, not auto-prompt:** When auth is missing, print the URL and the command. No interactive prompting on 401 — keeps the CLI agent-safe and predictable.
- **New auth type, not extension of existing cookie type:** `composed` is distinct from `cookie` because the post-extraction step is different (compose format string vs. dump cookie string). Keeping them separate avoids breaking the existing cookie auth path.
- **Format string with placeholders:** `"PagliacciAuth {customerId}|{authToken}"` is simple, readable, and covers the patterns we've seen. No need for a more complex templating language.

## Dependencies / Assumptions

- Depends on the sniff auth header discovery (Step 2a.1.5 item 5) already implemented in this branch
- Assumes cookie extraction tools (pycookiecheat etc.) can read individual named cookies — verified: pycookiecheat returns a dict keyed by cookie name
- The `AuthConfig.Format` field already exists in the spec struct but is unused for this purpose

## Outstanding Questions

### Deferred to Planning

- [Affects R3][Technical] What is the best YAML representation for the `Cookies` list — a new field on `AuthConfig` or reuse/extend an existing field?
- [Affects R6][Technical] How much of `auth_browser.go.tmpl`'s cookie extraction code can be shared with `auth_composed.go.tmpl` vs. duplicated?
- [Affects R12][Needs research] Can the sniff reliably match header values to cookie names when values contain special characters or are URL-encoded?

## Next Steps

→ `/ce:plan` for structured implementation planning
