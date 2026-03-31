---
title: "fix: Better auth error handling in generated CLIs"
type: fix
status: completed
date: 2026-03-31
origin: docs/brainstorms/2026-03-31-auth-error-handling-requirements.md
---

# fix: Better auth error handling in generated CLIs

## Overview

Generated CLIs give poor feedback when auth is missing or wrong. HTTP 400 responses for missing keys aren't recognized as auth errors. Error messages don't name the specific env var. Doctor doesn't tell you how to fix auth. This plan adds 400 body pattern matching, env-var-specific error messages, key URL discovery, and improved doctor output.

## Problem Frame

Steam returns HTTP 400 ("Required parameter 'key' is missing") for missing auth, not 401. The generator's `classifyAPIError` only catches 401/403, so the user sees a raw HTML error. Even for 401/403, the hint is generic ("check your credentials") instead of naming the env var. The doctor command says "Auth: not configured" without telling you which env var to set or where to get a key. (see origin: docs/brainstorms/2026-03-31-auth-error-handling-requirements.md)

## Requirements Trace

**Error Classification (400/401/403)**

- R1. Pattern-match 400 bodies for auth keywords, treat as auth error
- R4. Non-auth 400s not misclassified

**Error Message Content**

- R2. Auth errors include specific env var name (`export STEAM_API_KEY=...`)
- R3. Auth errors include setup command
- R13. Error messages include key URL when known
- R14. Key URL omitted cleanly when unknown

**Doctor Output**

- R5. Doctor names the env var when auth not configured
- R6. Doctor confirms auth source when configured

**Auth Behavior**

- R7. Per-call auth checking (no startup gate)
- R8. No fail-fast startup gate

**Pipeline: Spec Schema & Template Access**

- R9. helpers.go.tmpl accesses `{{.Auth.EnvVars}}`
- R10. doctor.go.tmpl accesses `{{.Auth.EnvVars}}`
- R11. Add `key_url` (YAML field) / `KeyURL` (Go struct field) to `spec.AuthConfig`
- R12. Crowd-sniff extracts key URLs from SDK READMEs (best-effort; `KeyURLHint` may be empty for APIs without discoverable key URLs)

## Scope Boundaries

- No OAuth flow improvements
- No per-endpoint auth metadata
- No interactive setup wizard
- Key URL extraction is best-effort from SDK READMEs

## Context & Research

### Relevant Code and Patterns

- **helpers.go.tmpl** receives `helpersTemplateData` which embeds `*spec.APISpec` — so `.Auth.EnvVars`, `.Auth.Type`, `.Auth.In`, `.Name` are all accessible. Already branches on `.Auth.Type` for 401 hints.
- **doctor.go.tmpl** receives `*spec.APISpec` directly — full `.Auth.*` access. Already checks env vars via `{{range .Auth.EnvVars}}` + `os.Getenv`.
- **classifyAPIError** uses `strings.Contains(msg, "HTTP NNN")` for status matching. 400 is not currently handled.
- **spec.AuthConfig** has no `KeyURL` field. Need to add one.
- **crowd-sniff DiscoveredAuth** has no `KeyURLHint` field. Need to add one.
- **npm.go** tarball walker filters to `.js`/`.ts`/`.mjs` only — README.md is skipped. Key URL extraction needs a separate targeted README read, not an extension of `GrepAuth`.

### Institutional Learnings

- `classifyAPIError` string matching depends on client.go formatting errors as `"HTTP NNN"` — confirmed this pattern is consistent across all client templates.

## Key Technical Decisions

- **400 body matching: template-gated, body-keyword-triggered**: The 400 auth handler is only *emitted* in CLIs whose spec declares auth (`.Auth.Type != "none"` at template time). At runtime, `classifyAPIError` only sees the error string — it has no config access. When the error body matches auth keywords, it classifies as auth and shows the hint. This is correct for both missing keys (user forgot to set env var) and wrong keys (API rejects the key with a 400). The hint is useful in both cases. False positives on validation-only 400s are mitigated by the keyword regex targeting auth-specific terms.
- **Template-time embedding**: Env var names and key URLs are embedded as string literals in the generated code at template rendering time, not resolved at runtime. Zero runtime cost, always correct.
- **helpers.go.tmpl already has the right data**: It receives `helpersTemplateData` which embeds `*spec.APISpec`. No generator.go changes needed for data routing — just use `{{.Auth.EnvVars}}` and `{{.Auth.KeyURL}}` in the template.
- **Key URL in spec, not config**: The key URL is a property of the API (where to register), not the user's config. It belongs in `spec.AuthConfig`, not `config.Config`.

## Open Questions

### Resolved During Planning

- **Q: Does helpers.go.tmpl have access to Auth.EnvVars?** Yes — it receives `helpersTemplateData` which embeds `*spec.APISpec`. `.Auth.EnvVars` works directly.
- **Q: What regex for 400 body matching?** Case-insensitive match for auth keywords. The 400 branch is template-gated (only emitted when spec declares auth). At runtime, body keyword match alone triggers the hint — useful for both missing and wrong keys. No runtime credential check needed.

### Deferred to Implementation

- **Q: Exact key URL extraction regex for SDK READMEs** — Need to sample real READMEs to calibrate. Start with patterns like `Get.*(?:API|key).*at\s+(https?://\S+)` and `(?:register|sign.up|developer).*(?:at|:)\s+(https?://\S+)`.

## Implementation Units

- [ ] **Unit 1: Add KeyURL field to spec and crowd-sniff types**

  **Goal:** Add `key_url` to `spec.AuthConfig` and `KeyURLHint` to `DiscoveredAuth`, wire through specgen.

  **Requirements:** R11

  **Dependencies:** None

  **Files:**
  - Modify: `internal/spec/spec.go`
  - Modify: `internal/crowdsniff/types.go`
  - Modify: `internal/crowdsniff/specgen.go`
  - Test: `internal/crowdsniff/specgen_test.go`

  **Approach:**
  - Add `KeyURL string \`yaml:"key_url,omitempty" json:"key_url,omitempty"\`` to `AuthConfig`
  - Add `KeyURLHint string` to `DiscoveredAuth`
  - In `buildAuthConfig()` in specgen.go, populate `spec.Auth.KeyURL` from `DiscoveredAuth.KeyURLHint` when non-empty

  **Patterns to follow:**
  - How `EnvVarHint` flows from `DiscoveredAuth` through `buildAuthConfig` to `spec.AuthConfig.EnvVars`

  **Test scenarios:**
  - Happy path: DiscoveredAuth with KeyURLHint → spec.Auth.KeyURL populated
  - Edge case: Empty KeyURLHint → spec.Auth.KeyURL stays empty
  - Negative test: Existing specs without key_url still parse correctly

  **Verification:**
  - `go build ./...` and `go test ./internal/spec/... ./internal/crowdsniff/...` pass

- [ ] **Unit 2: Extract key URLs from npm SDK READMEs**

  **Goal:** Crowd-sniff detects "get your API key at [url]" patterns in SDK source/READMEs.

  **Requirements:** R12

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `internal/crowdsniff/npm.go`
  - Test: `internal/crowdsniff/npm_test.go`

  **Approach:**
  - **Important:** The npm tarball walker (`npm.go:342-344`) currently filters to `.js`/`.ts`/`.mjs` only — README.md is skipped. Add a targeted README read: after the existing `filepath.Walk`, check for `package/README.md` (npm tarballs always have README at the package root), read it into a separate buffer, and pass it to a new `extractKeyURL(readmeContent, apiName)` function. Do NOT feed markdown into `GrepEndpoints` or `EnrichWithParams`.
  - Add key URL regex patterns targeting auth setup phrases: "Get your API key at [url]", "Register at [url]", "Sign up at [url]", developer portal links
  - Populate `DiscoveredAuth.KeyURLHint` when a URL is found
  - URL must be HTTPS and must either contain the API name OR be on a well-known developer platform domain. Accept URLs from any package that passes the existing crowd-sniff relevance filter (not just official-tier)
  - **ReDoS protection:** Avoid nested `.*` quantifiers in regex patterns. Use possessive quantifiers or bounded repetition. Apply a size limit (100KB) on README content before regex matching

  **Patterns to follow:**
  - `envVarHintPattern` extraction in `GrepAuth` — same approach of regex + relevance filtering

  **Test scenarios:**
  - Happy path: README with "Get your API key at https://steamcommunity.com/dev/apikey" → KeyURLHint extracted
  - Happy path: README with "Register at https://developer.notion.com" → KeyURLHint extracted
  - Edge case: README with no key URL patterns → KeyURLHint empty
  - Edge case: README with irrelevant URLs (npm badge, GitHub link) → not extracted
  - Negative test: Source file (not README) with URL → not extracted (only READMEs)

  **Verification:**
  - `go test ./internal/crowdsniff/...` passes

- [ ] **Unit 3: Improve classifyAPIError with 400 handling and env-var-specific messages**

  **Goal:** Auth errors (400/401/403) show the specific env var name, setup command, and key URL.

  **Requirements:** R1, R2, R3, R4, R9, R13, R14

  **Dependencies:** Unit 1 (KeyURL field must exist in spec)

  **Files:**
  - Modify: `internal/generator/templates/helpers.go.tmpl`
  - Modify: `internal/generator/templates/mcp_tools.go.tmpl` (has a parallel inline error handler that must stay consistent)
  - Test: `internal/generator/generator_test.go`

  **Approach:**
  - Add HTTP 400 handling to `classifyAPIError`: pattern-match the error body for auth keywords using case-insensitive regex. Only emit the 400 auth branch when the spec declares auth (`.Auth.Type != "none"`). At runtime, classify 400+auth-keywords as auth — useful for both missing and wrong keys.
  - **Response body sanitization:** Truncate the raw error body to 200 chars before including in hints. Strip strings matching credential patterns (`sk-`, `sk_live_`, `Bearer `, `key=`) to prevent partial credential leakage from API error responses.
  - Update the parallel error handler in `mcp_tools.go.tmpl` to match — either refactor to call `classifyAPIError` or apply the same 400 handling and improved messages inline.
  - Rewrite all auth error hints (400/401/403) to include:
    - The specific env var: `{{if .Auth.EnvVars}}export {{index .Auth.EnvVars 0}}=<your-key>{{end}}`
    - The key URL when known: `{{if .Auth.KeyURL}}Get a key at: {{.Auth.KeyURL}}{{end}}`
    - The doctor command: `Run '{{.Name}}-pp-cli doctor' to check auth status`
  - Keep the existing auth-type branching (oauth2/bearer/api_key) but enrich each branch with the env var and URL

  **Patterns to follow:**
  - Existing 401 branch in helpers.go.tmpl — extends with env var name and key URL
  - Conditional template emission: `{{if .Auth.KeyURL}}...{{end}}`

  **Test scenarios:**
  - Happy path: Generate CLI with api_key auth + env vars + key URL → error message contains env var name, export command, and key URL
  - Happy path: Generate CLI with api_key auth + env vars, no key URL → error message contains env var but no URL line
  - Happy path: Generate CLI with bearer_token auth → error message mentions token setup
  - Edge case: Generate CLI with no auth → classifyAPIError doesn't mention auth setup at all
  - Edge case: HTTP 400 with body "Required parameter 'key' is missing" → classified as auth error
  - Edge case: HTTP 400 with body "Invalid email format" → NOT classified as auth error (no auth keywords)
  - Negative test: HTTP 400 with auth keywords but auth IS configured → still shows hint (key might be wrong)

  **Verification:**
  - Generated helpers.go contains env var name in auth error messages
  - Generated helpers.go handles HTTP 400 with body matching

- [ ] **Unit 4: Improve doctor output with auth setup instructions**

  **Goal:** Doctor command shows specific env var name and key URL when auth is not configured.

  **Requirements:** R5, R6, R10, R13, R14

  **Dependencies:** Unit 1 (KeyURL field)

  **Files:**
  - Modify: `internal/generator/templates/doctor.go.tmpl`
  - Test: `internal/generator/generator_test.go`

  **Approach:**
  - When auth is not configured: show `Auth: not configured. Set {{index .Auth.EnvVars 0}} in your environment: export {{index .Auth.EnvVars 0}}=<your-key>`
  - When key URL is known: add `Get a key at: {{.Auth.KeyURL}}`
  - When auth IS configured: show `Auth: configured ({{cfg.AuthSource}})`
  - doctor.go.tmpl already receives `*spec.APISpec` directly — `.Auth.EnvVars` and `.Auth.KeyURL` are accessible

  **Patterns to follow:**
  - Existing env var check section in doctor.go.tmpl (search for `range .Auth.EnvVars` or `os.Getenv` to locate)

  **Test scenarios:**
  - Happy path: Generate CLI with auth + env vars + key URL → doctor output includes env var name and key URL
  - Happy path: Generate CLI with auth + env vars, no key URL → doctor shows env var but no URL
  - Edge case: Generate CLI with no auth → doctor auth section shows "not required"

  **Verification:**
  - Generated doctor.go shows specific env var name and key URL in auth status

## System-Wide Impact

- **Generated CLI error messages** change for every API with auth configured — this affects all future generated CLIs
- **Spec format** gains `key_url` field — backward compatible (omitempty), existing specs work unchanged
- **Crowd-sniff output** gains key URL hints — additive, no breaking change
- **Unchanged invariants**: Auth flow itself (how keys are passed to API) is unchanged. Only the error messages and doctor output change.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| 400 body matching false positives | Auth-keyword regex targets specific terms (key, token, unauthorized). Only emitted when spec declares auth. Validation-only 400s won't match. |
| Key URL from malicious/typosquatted npm package | URL must be HTTPS + contain API name or known developer domain. Extracted from packages passing crowd-sniff relevance filter. Provenance logged for skill-time audit. |
| API 400 response leaks partial credentials in error output | Response body truncated to 200 chars. Credential-shaped strings (sk-, Bearer, key=) stripped before display. |
| README regex ReDoS on crafted input | Avoid nested `.*` quantifiers. Cap README processing at 100KB. |
| Template change breaks existing generated CLIs | All changes are in templates — only affects newly generated CLIs. Existing binaries unchanged. |
| MCP error handler diverges from CLI error handler | Unit 3 updates both helpers.go.tmpl and mcp_tools.go.tmpl to keep behavior consistent. |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-03-31-auth-error-handling-requirements.md](docs/brainstorms/2026-03-31-auth-error-handling-requirements.md)
- helpers.go.tmpl: `internal/generator/templates/helpers.go.tmpl` (classifyAPIError at lines 98-138)
- doctor.go.tmpl: `internal/generator/templates/doctor.go.tmpl`
- spec.AuthConfig: `internal/spec/spec.go:27-37`
- crowd-sniff auth: `internal/crowdsniff/npm.go` (GrepAuth), `internal/crowdsniff/types.go` (DiscoveredAuth)
- Steam retro: `docs/retros/2026-03-30-steam-retro.md`
