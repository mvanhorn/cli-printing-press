---
title: "feat: Detect and emit required API headers from OpenAPI specs"
type: feat
status: completed
date: 2026-04-04
origin: docs/retros/2026-04-04-cal-com-retro.md (finding #5)
---

# feat: Detect and emit required API headers from OpenAPI specs

## Overview

Many APIs require per-request headers beyond Authorization — version headers (`cal-api-version`, `Stripe-Version`, `anthropic-version`), custom client identifiers, or API-specific metadata headers. The generator currently only emits `Authorization` and `User-Agent`. When a required header is missing, API calls silently fail (404, stale behavior). This feature detects required headers from the OpenAPI spec and emits them in the generated client.

## Problem Frame

During the Cal.com CLI generation, all API calls returned 404 because the `cal-api-version: 2024-08-13` header was missing. The generator had no mechanism to detect or emit it. Manual intervention was required to add the header to the printed CLI's client.go — work that should have been automatic.

This affects ~10-20% of APIs that use explicit version headers or other required per-request headers. (see origin: `docs/retros/2026-04-04-cal-com-retro.md`, finding #5)

## Requirements Trace

- R1. Required headers declared in the OpenAPI spec (with `in: header` and `required: true`) that appear on >80% of all operations (denominator: total operations in the spec) are emitted as default headers in the generated client
- R2. The default value for each header is extracted from the parameter's `schema.default` or first `schema.enum` value. If neither exists, the header is still detected but emitted with an empty value — doctor warns about unconfigured required headers
- R3. Auth-related headers (Authorization) and dynamic headers (Content-Type, User-Agent) are excluded from detection
- R4. APIs without required headers (Stripe with no version header in spec, GitHub, Petstore) generate identical client code to today — no regression
- R5. The detected headers are available to all templates that make HTTP requests (client, doctor, auth browser)

## Scope Boundaries

- **In scope**: Global required headers (>80% of operations) with known default values
- **Not in scope**: Per-operation headers that vary across endpoints (these remain as command flags)
- **Not in scope**: Cal.com's per-endpoint version *routing* (different version values per path prefix, e.g., bookings use `2024-08-13`, event-types use `2024-06-14`). That routing logic is API-specific. What IS in scope: detecting `cal-api-version` as a globally required header from the spec — the header name and a single default value. Cal.com's spec declares the header on most operations; the >80% threshold detects it. The per-endpoint value differences are a Cal.com-specific concern addressed separately in the printed CLI.
- **Not in scope**: Auth inference from description text (retro finding #7 — separate feature)
- **Not in scope for detection**: Internal YAML specs. Auto-detection applies only to OpenAPI specs. Internal YAML spec users can declare `RequiredHeaders` manually via the new struct field.

## Context & Research

### Relevant Code and Patterns

- `internal/openapi/parser.go:744` — `filterGlobalParams`: frequency-based threshold (>80%) for identifying global query params. Same architectural pattern needed here.
- `internal/openapi/parser.go:366` — `inferQueryParamAuth`: scans operations for auth-like params when security section is empty. Same iteration pattern.
- `internal/openapi/parser.go:966` — `mapParameters`: **the drop point** — currently filters out `in: header` params (`parameter.In != "path" && != "query"` → continue). This is where header params get silently discarded.
- `internal/openapi/parser.go:1077` — `mergeParameters`: correctly collects ALL params including headers. The data is available before the filter.
- `internal/spec/spec.go:11-26` — `APISpec` struct: no `RequiredHeaders` field exists. `Auth.Header` only handles the auth header.
- `internal/generator/templates/client.go.tmpl:343-346` — header insertion point (between Authorization and User-Agent)

### Institutional Learnings

- Cal.com retro finding #5: complete design spec with threshold, template, guard, and test plan
- Steam retro finding #5: frequency-threshold pattern validated for auth param detection (>30%)
- Both retros confirm the >80% threshold is the right level for "this is global, not per-operation"

### Negative Test Fixtures

- `testdata/openapi/stytch.yaml`: has `in: header` params (`X-Stytch-Member-Session`) that are `required: false` and per-operation — must NOT be detected
- `testdata/openapi/petstore.yaml`: has `api_key` as `in: header` on a single endpoint, `required: false` — must NOT be detected

## Key Technical Decisions

- **80% threshold** (matching `filterGlobalParams`): A header must appear as `required: true` on >80% of operations to be considered global. This avoids false positives on per-operation headers while catching version headers that are ubiquitous. Rationale: `filterGlobalParams` uses this threshold successfully for query params; version headers have the same "almost every endpoint needs this" pattern.

- **Default value from schema, not hardcoded**: The parser extracts the default from `schema.Default` or the first entry in `schema.Enum`. If neither exists, the header name is stored but the value is empty — the template emits it with an empty string and the user must configure it. Rationale: we can't invent version numbers, but we can surface the header so the user knows it's needed.

- **Struct on APISpec, not a separate config**: `RequiredHeaders` goes directly on `APISpec` so all templates can access it without plumbing changes. Rationale: `client.go.tmpl` already receives `*spec.APISpec` directly (line 299 of generator.go).

- **Exclude list**: Skip `Authorization`, `Content-Type`, `Accept`, `User-Agent` — these are handled by other mechanisms. Rationale: Authorization is set by the auth system, Content-Type is set per-request based on body presence, User-Agent is generated.

## Open Questions

### Resolved During Planning

- **Q: What about APIs with per-endpoint versioning (Cal.com)?** Resolution: Out of scope. Per-endpoint routing (different version per path prefix) is API-specific. This feature handles the common case: one version header across all endpoints. Cal.com's per-endpoint routing was a manual fix in the printed CLI, and correctly so.

- **Q: Should the header value be configurable via env var?** Resolution: No for v1. The value comes from the spec's default/enum. If an API changes its version, the user regenerates. Adding env var configurability is premature — we haven't seen demand for it.

### Deferred to Implementation

- **Q: What happens when a header has no default value and no enum?** Store the header name with empty value. The client template emits `req.Header.Set("X-Api-Version", "")`. The doctor template checks for empty-value required headers and emits a warning: `WARN Required header X-Api-Version has no configured value`. This converts a silent 404 into a visible diagnostic.

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification.*

```
OpenAPI spec
    │
    ▼
mergeParameters()  ─── collects ALL params (path, query, header)
    │
    ├── mapParameters()  ─── filters to path+query only (existing, unchanged)
    │
    └── detectRequiredHeaders()  ─── NEW: filters to header only
            │
            ├── Filter: in=header, required=true
            ├── Exclude: Authorization, Content-Type, Accept, User-Agent
            ├── Count frequency across all operations
            ├── Threshold: >80% of operations
            └── Extract default from schema.Default or schema.Enum[0]
                    │
                    ▼
            APISpec.RequiredHeaders []RequiredHeader{Name, Value}
                    │
                    ▼
            client.go.tmpl: {{range .RequiredHeaders}}
                req.Header.Set("{{.Name}}", "{{.Value}}")
            {{end}}
```

## Implementation Units

- [ ] **Unit 1: Add RequiredHeader struct and field to APISpec**

**Goal:** Define the data structure for required headers in the spec

**Requirements:** R1, R5

**Dependencies:** None

**Files:**
- Modify: `internal/spec/spec.go`
- Test: `internal/spec/spec_test.go`

**Approach:**
- Add `RequiredHeader` struct with `Name string` and `Value string` fields
- Add `RequiredHeaders []RequiredHeader` field to `APISpec` with yaml/json tags + omitempty
- Follow existing struct tag conventions (yaml + json)

**Patterns to follow:**
- `AuthConfig` struct in the same file for naming/tag conventions

**Test scenarios:**
- Happy path: `APISpec` with `RequiredHeaders` round-trips through YAML marshal/unmarshal
- Happy path: `APISpec` with empty `RequiredHeaders` omits the field in YAML output (omitempty)

**Verification:**
- `go test ./internal/spec/...` passes
- `RequiredHeaders` field accessible from templates via `{{.RequiredHeaders}}`

---

- [ ] **Unit 2: Implement detectRequiredHeaders in the OpenAPI parser**

**Goal:** Scan OpenAPI parameters for required headers appearing on >80% of operations

**Requirements:** R1, R2, R3

**Dependencies:** Unit 1

**Files:**
- Modify: `internal/openapi/parser.go`
- Test: `internal/openapi/parser_test.go`

**Approach:**
- New function `detectRequiredHeaders(doc *openapi3.T, auth spec.AuthConfig) []spec.RequiredHeader`
- Iterate all path items and operations via `doc.Paths` (same iteration pattern as `inferQueryParamAuth`, not `filterGlobalParams` which walks post-mapped resources)
- For each operation, call `mergeParameters` to get all params including headers
- Filter to `parameter.In == "header"` and `parameter.Required == true`
- Exclude known headers using case-insensitive comparison (`strings.EqualFold`): Authorization, Content-Type, Accept, User-Agent, and the auth header from `auth.Header`
- Count frequency per header name across total operations
- Apply 80% threshold
- For qualifying headers, extract value from `parameter.Schema.Value.Default` or first `parameter.Schema.Value.Enum` entry
- Call from `parse()` after `mapAuth` and `mapResources`, store result in `result.RequiredHeaders`

**Patterns to follow:**
- `inferQueryParamAuth` (line 366) for the iteration pattern (walks `doc.Paths` directly, same as needed here)
- `filterGlobalParams` (line 744) for the threshold math only (80% frequency calculation)
- `mergeParameters` (line 1077) for collecting all params

**Test scenarios:**
- Happy path: Spec with `cal-api-version` header (required: true) on 100% of operations → detected with correct name and default value
- Happy path: Spec with version header on 85% of operations → detected (above 80%)
- Edge case: Header on 75% of operations → NOT detected (below 80%)
- Edge case: Header with `required: false` → NOT detected regardless of frequency
- Edge case: `Authorization` header with `required: true` on all operations → excluded (auth-handled)
- Edge case: Header with no default value and no enum → detected with empty Value
- Edge case: Spec with zero operations → empty result, no panic
- Integration: Existing `testdata/openapi/petstore.yaml` → no required headers detected (api_key is optional, single endpoint)
- Integration: Existing `testdata/openapi/stytch.yaml` → no required headers detected (session headers are optional)

**Verification:**
- `go test ./internal/openapi/...` passes
- New test fixture exercises the threshold boundary

---

- [ ] **Unit 3: Emit required headers in client and doctor templates**

**Goal:** Generated clients set the detected headers on every request

**Requirements:** R4, R5

**Dependencies:** Unit 2

**Files:**
- Modify: `internal/generator/templates/client.go.tmpl`
- Modify: `internal/generator/templates/doctor.go.tmpl`
- Modify: `internal/generator/templates/auth_browser.go.tmpl`
- Test: `internal/generator/generator_test.go`

**Approach:**
- In `client.go.tmpl`, add `{{range .RequiredHeaders}}` block between the auth header (line ~344) and User-Agent (line ~346)
- Same block in `doctor.go.tmpl` auth health check request (line ~153)
- Same block in `auth_browser.go.tmpl` auth validation request (line ~704)
- For APIs without required headers, the range produces no output — no regression

**Patterns to follow:**
- Existing `{{if .Auth.Header}}` conditional pattern in client.go.tmpl
- The `{{range .Tables}}` pattern in store.go.tmpl for iterating slices

**Test scenarios:**
- Happy path: Generate from spec with required headers → client.go contains `req.Header.Set("cal-api-version", "2024-08-13")`
- Happy path: Generate from spec without required headers → client.go has no extra header lines (identical to current output)
- Edge case: Required header with empty Value → client.go contains `req.Header.Set("X-Api-Version", "")` AND doctor.go emits a WARN for the unconfigured header
- Integration: Full generation from petstore.yaml → binary builds, --help works, no extra headers in client

**Verification:**
- `go test ./internal/generator/...` passes
- Generated client for a versioned-API spec sets the correct header
- Generated client for petstore has no extra headers

---

- [ ] **Unit 4: Add test fixture for versioned API**

**Goal:** A reusable OpenAPI test fixture that exercises required header detection

**Requirements:** R1, R2

**Dependencies:** Unit 2

**Files:**
- Create: `testdata/openapi/versioned-api.yaml`
- Modify: `internal/openapi/parser_test.go`

**Approach:**
- Minimal OpenAPI spec with 5 endpoints, all requiring `X-Api-Version: 2024-01-01` header
- One endpoint also has a per-operation optional header (should not be detected as global)
- Used by Unit 2's parser tests as the primary positive fixture

**Test scenarios:**
- Test expectation: none -- this is a test fixture, not a feature-bearing unit

**Verification:**
- The fixture is valid OpenAPI 3.0 (parseable by kin-openapi)
- Parser tests using this fixture pass

## System-Wide Impact

- **Interaction graph:** The new `RequiredHeaders` field flows from OpenAPI parser → `APISpec` → client/doctor/auth templates. No callbacks, middleware, or observers involved.
- **Unchanged invariants:** Auth detection (`mapAuth`, `inferQueryParamAuth`) is not modified. The `mapParameters` function that maps path/query params to CLI flags is not modified. Existing generated CLIs without required headers produce identical output.
- **API surface parity:** Three templates set HTTP headers (client, doctor, auth_browser). All three must emit required headers for consistency.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| False positive: Content-Type detected as global header | Explicit exclude list: Authorization, Content-Type, Accept, User-Agent |
| Threshold too aggressive (80%) misses headers on 70% of ops | Conservative choice matches existing `filterGlobalParams`. Can lower later if needed. |
| Default value missing from spec | Store with empty Value; template still emits header. Doctor could warn (future enhancement). |
| Breaking existing CLIs | Range over empty slice produces no output — zero-diff for APIs without required headers |

## Sources & References

- **Origin document:** [docs/retros/2026-04-04-cal-com-retro.md](docs/retros/2026-04-04-cal-com-retro.md) — finding #5
- Related code: `internal/openapi/parser.go` — `filterGlobalParams` (line 744), `inferQueryParamAuth` (line 366), `mapParameters` (line 959)
- Related code: `internal/spec/spec.go` — `APISpec` struct (line 11)
- Related code: `internal/generator/templates/client.go.tmpl` — header emission (line 343-346)
