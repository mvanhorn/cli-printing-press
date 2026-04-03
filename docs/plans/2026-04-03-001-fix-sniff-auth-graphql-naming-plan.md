---
title: "fix: Authenticated sniff flow, GraphQL BFF discovery, and apostrophe naming"
type: fix
status: active
date: 2026-04-03
origin: docs/retros/2026-04-03-dominos-retro.md
---

# fix: Authenticated sniff flow, GraphQL BFF discovery, and apostrophe naming

## Overview

Three gaps discovered during the Domino's Pizza generation run. The sniff procedure doesn't explore authenticated endpoints when the user confirms login, doesn't inspect GraphQL POST bodies when it discovers a BFF pattern, and `cleanSpecName` mangles brand names with apostrophes.

These are ordered by dependency: WU-3 (naming) is independent, WU-1 (auth sniff) is independent, WU-2 (GraphQL) is independent but benefits from WU-1 since GraphQL BFFs often have both public and authenticated operations.

## Problem Frame

The chrome-auth-sniff branch (`9c9bfa1`) built the downstream infrastructure: `Auth.Type=cookie` → `auth_browser.go.tmpl` → `auth login --chrome` in printed CLIs. But the upstream discovery path — the sniff procedure that identifies cookie-authenticated endpoints and feeds them into the spec — has three gaps that prevent the downstream infrastructure from activating. See `docs/retros/2026-04-03-dominos-retro.md` findings #1-3 and #6.

## Requirements Trace

- R1. When `AUTH_SESSION_AVAILABLE=true`, the sniff user-flow plan MUST include authenticated page visits (account, order history, rewards, profile) after the primary flow
- R2. Endpoints discovered on authenticated pages must be classified as auth-required in the discovery report
- R3. When auth-only endpoints are found, Step 2d (cookie auth validation) must run and propagate `Auth.Type=cookie` + `CookieDomain` into the spec
- R4. When the sniff detects multiple POST requests to the same URL path, it must inspect request bodies for GraphQL `operationName` fields
- R5. GraphQL operations must be recorded in the discovery report with operation name, type (query/mutation), and domain grouping
- R6. `cleanSpecName("Domino's Pizza API")` must produce `"dominos-pizza"`, not `"domino-s-pizza"`
- R7. All existing `cleanSpecName` behavior for names without apostrophes must be unchanged

## Scope Boundaries

- Does NOT change generator templates (`auth_browser.go.tmpl` already exists per `docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md`)
- Does NOT change `specgen.go` (already handles cookie auth from HAR captures)
- Does NOT add GraphQL schema introspection — only discovers operations from captured traffic
- Does NOT implement command alias generation (retro finding #4 — deferred, needs larger design)
- WU-1 changes skill instructions only; WU-2 changes skill instructions only; WU-3 changes Go code only

## Context & Research

### Relevant Code and Patterns

- `internal/openapi/parser.go:1898-1967` — `cleanSpecName()` function. Character filtering at line 1909 keeps only `unicode.IsLetter()` and `unicode.IsDigit()`, treating apostrophes as non-alphanumeric → replaced with space → `"Domino's"` → `"domino s"` → `"domino-s"`
- `skills/printing-press/references/sniff-capture.md` — Step numbering: 1→1d (detection/auth), 2a→2d (capture/validation), 3→5 (analysis/reporting). New auth flow goes in Step 2a.1; GraphQL detection goes after Step 2a.2
- `skills/printing-press/SKILL.md` Phase 1.6 (line ~517) sets `AUTH_SESSION_AVAILABLE=true`; Phase 1.7 (line ~563) passes it to sniff-capture.md
- `internal/websniff/specgen.go:318-334` — Already handles `captureType == "cookie"` and sets `CookieDomain` from `capture.BoundDomain`. The downstream pipeline works; the upstream doesn't feed it.

### Institutional Learnings

- Retro finding #5 (local build preference) was already fixed in commit `bb33d2d`
- The browser auth plan (`2026-04-02-001`) covers R1-R12 of the downstream pipeline. This plan covers the upstream discovery that feeds it.

### Related Plans

- `docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md` — Complementary. That plan implements the generator templates and CLI runtime. This plan implements the sniff discovery that feeds the generator.

## Key Technical Decisions

- **Auth flow in sniff-capture.md, not SKILL.md:** The authenticated page visits belong in the capture procedure (sniff-capture.md Step 2a.1), not in the main skill's Phase 1.6/1.7. Phase 1.6 already sets the flag correctly; the gap is in the capture reference file that doesn't use it.
- **GraphQL detection by URL convergence, not content-type:** The signal is multiple POST requests to the same path (all going to `/api/web-bff/graphql`). Content-type alone isn't sufficient since both REST and GraphQL use `application/json`.
- **Apostrophe stripping before character filter, not after:** Strip apostrophes early (before the unicode filter loop) so `"Domino's"` → `"Dominos"` → normal flow. This is simpler and more correct than post-processing.

## Open Questions

### Resolved During Planning

- **Q: Should GraphQL operations become REST-like spec paths?** Yes — the spec still uses OpenAPI format, so each GraphQL operation becomes a path like `/graphql#GetStoreMenu`. The generator already handles spec paths; no new generator changes needed.
- **Q: What authenticated pages to visit?** Common patterns: `/account`, `/profile`, `/orders`, `/order-history`, `/rewards`, `/settings`, `/addresses`, `/payment-methods`. The skill should try common patterns and also derive page names from the research brief's top workflows.

### Deferred to Implementation

- **Q: How to handle GraphQL mutations discovered during auth page visits?** The sniff should record them but the spec builder may need to decide whether mutations become CLI commands. Defer to implementation.
- **Q: Rate limiting during auth page visits?** The existing sniff pacing rules apply. No new logic needed.

## Implementation Units

- [ ] **Unit 1: Strip apostrophes in cleanSpecName (WU-3)**

**Goal:** Brand names with apostrophes produce clean CLI names.

**Requirements:** R6, R7

**Dependencies:** None

**Files:**
- Modify: `internal/openapi/parser.go` (the `cleanSpecName` function, lines 1898-1967)
- Test: `internal/openapi/parser_test.go`

**Approach:**
Add `strings.ReplaceAll(title, "'", "")` immediately after the lowercase/trim step (line 1899) and before the unicode character filter loop (line 1906). This strips possessive apostrophes so `"domino's"` → `"dominos"` before the filter runs.

Also strip the Unicode right single quotation mark (`\u2019`, `'`) which appears in some formatted API titles.

**Patterns to follow:**
- Line 1903 already does `strings.ReplaceAll(title, "open api", " ")` — same pattern of pre-processing before the main filter loop

**Test scenarios:**
- Happy path: `cleanSpecName("Domino's Pizza API")` → `"dominos-pizza"`
- Happy path: `cleanSpecName("McDonald's API")` → `"mcdonalds"`
- Happy path: `cleanSpecName("Lowe's Home Improvement")` → `"lowes-home-improvement"`
- Edge case: Unicode right quote `cleanSpecName("Domino\u2019s Pizza")` → `"dominos-pizza"`
- Edge case: Multiple apostrophes `cleanSpecName("Rock'n'Roll API")` → `"rocknroll"`
- Regression: `cleanSpecName("Stripe API")` → `"stripe"` (unchanged)
- Regression: `cleanSpecName("Steam Web API")` → `"steam-web"` (unchanged)
- Regression: `cleanSpecName("cal.com API")` → existing behavior unchanged

**Verification:**
- `go test ./internal/openapi/ -run TestCleanSpecName` passes with new cases
- Existing tests still pass

- [ ] **Unit 2: Add authenticated flow to sniff user-flow plan (WU-1)**

**Goal:** When `AUTH_SESSION_AVAILABLE=true`, the sniff automatically visits authenticated pages after the primary flow, discovers auth-only endpoints, and classifies them.

**Requirements:** R1, R2, R3

**Dependencies:** None (but conceptually feeds into the browser-auth-cookie-runtime plan's pipeline)

**Files:**
- Modify: `skills/printing-press/references/sniff-capture.md` (Step 2a.1 — user flow plan section)

**Approach:**
Add a new sub-section to Step 2a.1 after the existing flow plan guidance. The section activates only when `AUTH_SESSION_AVAILABLE=true`. It instructs Claude to:

1. After completing the primary flow, record which endpoints were discovered (the "public set")
2. Navigate to common account/profile URLs derived from the site (e.g., `/account`, `/my-account`, `/profile`, `/orders`, `/order-history`, `/rewards`, `/settings`)
3. Also derive auth page URLs from the research brief's top workflows — if the brief mentions "order history" or "rewards", visit those specific pages
4. Capture endpoints from auth pages (the "auth set")
5. Classify endpoints as auth-required if they appear ONLY in the auth set (not in the public set)
6. If auth-only endpoints are found, ensure Step 2d (cookie auth validation) runs

Also add guidance to the discovery report template (Step 5) to include an "Auth-Only Endpoints" section when auth-only endpoints were discovered.

**Patterns to follow:**
- Step 2a.1 already has domain-specific examples (Domino's, Linear, ESPN) — add a note about secondary auth flows
- Step 2c (thin-results safety check) already compares discovered vs expected endpoints — similar comparison pattern

**Test scenarios:**
- Happy path: Sniff with `AUTH_SESSION_AVAILABLE=true` → auth pages appear in the user flow plan, auth-only endpoints listed in discovery report
- Happy path: Auth-only endpoints found → Step 2d cookie validation triggered → spec gets `Auth.Type=cookie`
- Edge case: Auth pages return 302 redirect to login → report as "auth page requires active session, session may have expired"
- Edge case: No auth-only endpoints found (all endpoints accessible publicly) → report "no auth-only endpoints discovered" and skip cookie auth classification
- Regression: Sniff without `AUTH_SESSION_AVAILABLE` (anonymous) → no auth page visits, existing behavior unchanged

**Verification:**
- Review sniff-capture.md for the new section
- The section is gated on `AUTH_SESSION_AVAILABLE=true`
- The section includes specific page URL patterns and research-brief-derived URLs

- [ ] **Unit 3: Add GraphQL BFF detection to sniff capture (WU-2)**

**Goal:** When the sniff detects multiple POST requests to the same URL path, it extracts GraphQL operation names from request bodies and records them in the discovery report.

**Requirements:** R4, R5

**Dependencies:** None

**Files:**
- Modify: `skills/printing-press/references/sniff-capture.md` (new Step 2a.2.5 after URL collection)

**Approach:**
Add a new step between Step 2a.2 (Collect API URLs) and Step 2a.3 (Deduplicate and normalize). The step:

1. After collecting URLs, check if >50% of captured XHR/fetch URLs resolve to the same POST endpoint
2. If yes, classify as GraphQL BFF
3. For each POST request to the BFF endpoint:
   - For agent-browser: use `network requests --type xhr --json` to list requests, then `network request <id> --json` to get the full request including body
   - For browser-use: inject a fetch interceptor before browsing that logs POST bodies to a known variable, then read them via `eval`
4. Parse `operationName` and `query` fields from each request body
5. Record operations in a structured format: `{operationName, type (query/mutation), variables}` 
6. Group operations by domain prefix (e.g., `GetStore*`, `GetMenu*`, `AddToOrder*`)
7. Use operation names instead of URL paths for the discovery report's endpoint table

Also update Step 5 (discovery report template) to include a "GraphQL Operations" section when BFF pattern is detected.

**Patterns to follow:**
- The existing "Proxy Pattern Detection" section (lines 20-38 of sniff-capture.md) already detects same-URL patterns — the GraphQL detection is a specialization of this
- Step 2a.2 already uses Performance API for URL collection — GraphQL detection runs after URL collection

**Test scenarios:**
- Happy path: Sniff a site with GraphQL BFF → operations extracted, discovery report lists operation names instead of repeated `/graphql` path
- Happy path: Mixed REST + GraphQL → REST endpoints listed by path, GraphQL operations listed by name
- Edge case: POST bodies are encrypted or non-JSON → report "GraphQL BFF detected but operation names could not be extracted"
- Edge case: Only 1-2 POST requests to `/graphql` (below 50% threshold) → don't classify as BFF, treat as regular endpoint
- Regression: Standard REST API with distinct URL paths → GraphQL detection does not activate

**Verification:**
- Review sniff-capture.md for the new Step 2a.2.5
- The step is gated on URL convergence (>50% same POST endpoint)
- Operation names are recorded in the discovery report

## System-Wide Impact

- **Interaction graph:** Unit 1 affects `cleanSpecName` which is called by the OpenAPI parser during `printing-press generate`. Unit 2 and 3 affect the sniff skill instructions which are read by Claude during `/printing-press` runs.
- **Error propagation:** Unit 1 has no new error paths. Units 2-3 add new skill instruction branches that are gated and have explicit fallback paths (skip if condition not met).
- **State lifecycle risks:** None — Unit 1 is a pure function change; Units 2-3 are skill instruction changes with no persistent state.
- **API surface parity:** None — no exported APIs change.
- **Unchanged invariants:** The generator templates (`auth_browser.go.tmpl`, `auth_simple.go.tmpl`), specgen.go, and the main SKILL.md Phase 1.6/1.7 flow are not modified. The downstream pipeline from spec → generated CLI is unchanged.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Auth page URLs are site-specific (not all sites use `/account`) | Use common patterns + derive from research brief. If no auth pages found, report and continue |
| GraphQL POST body interception may fail on some sites (CORS, CSP) | Gated on successful extraction. Falls back to "BFF detected but operations not extracted" |
| Apostrophe stripping may affect edge cases (e.g., `O'Brien's API`) | Test with possessive and Irish-name patterns. The stripping only removes the character, doesn't change surrounding text |

## Sources & References

- **Origin document:** [docs/retros/2026-04-03-dominos-retro.md](docs/retros/2026-04-03-dominos-retro.md)
- Related plan: [docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md](docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md) — downstream pipeline
- `internal/openapi/parser.go:1898-1967` — `cleanSpecName` function
- `skills/printing-press/references/sniff-capture.md` — sniff capture procedure
- `internal/websniff/specgen.go:318-334` — cookie auth handling in specgen
