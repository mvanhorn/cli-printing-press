---
title: Multi-spec auth metadata must merge only across compatible auth boundaries
date: 2026-05-22
category: logic-errors
module: multi-spec OpenAPI generation
problem_type: logic_error
component: authentication
symptoms:
  - Generated combo CLIs requested only the primary spec's OAuth scopes
  - Secondary API endpoints failed live calls with insufficient-permission responses
root_cause: scope_issue
resolution_type: code_fix
severity: high
tags: [multi-spec, oauth, auth-merge, generator]
---

# Multi-spec auth metadata must merge only across compatible auth boundaries

## Problem
Multi-spec generated CLIs used a single merged `AuthConfig`, but the merge path carried the primary spec's auth metadata instead of collecting compatible auth requirements from every input spec. OAuth-only combo CLIs could authenticate successfully and still fail secondary API calls because the consent flow never requested the secondary spec's scopes.

## Symptoms
- Generated `internal/cli/auth.go` contained only the primary spec's OAuth scopes.
- Live calls to secondary API endpoints returned insufficient-permission errors.
- Static checks stayed green because the generated CLI compiled and the missing scope surfaced only after a real secondary endpoint call.

## What Didn't Work
- Treating this as a printed-CLI issue would require hand-editing generated `auth.go`, but the next regeneration would drop the secondary scopes again.
- Blindly unioning all auth metadata is unsafe. A single generated client sends `Auth.AdditionalHeaders` globally, so secondary API-key headers can leak to unrelated origins if they are promoted without an origin guard. OAuth scopes also cannot be mixed across different authorization or token URLs.

## Solution
Fix the generator-side multi-spec merge so it preserves the selected auth block but accumulates compatible auth requirements:

- Union OAuth scopes only when the contributing spec has the same auth type, effective OAuth grant, authorization URL, token URL, and refresh-token mechanism.
- Promote secondary header API keys only when they are explicit `in: header` credentials, have one canonical request credential, and share the selected auth spec's origin.
- Keep query keys, ambiguous OR-style credential sets, and cross-origin secondary headers out of the global `Auth.AdditionalHeaders` list.

The regression coverage should include both the happy path and the safety boundaries:

- shared-provider OAuth specs produce an `auth.go` scope literal containing scopes from both specs
- single-spec generation does not pick up unrelated secondary scopes
- a later OAuth spec still becomes the merged auth source when the primary spec has no login URL
- mismatched OAuth providers and cross-origin API-key specs do not pollute the selected auth block

## Why This Works
The generated CLI has one login flow and one global auth header path. Scope union is correct only when every contributing scope belongs to the same OAuth authority; otherwise the selected provider receives scopes it cannot grant. Additional API-key headers are also global today, so promoting them is safe only when the secondary spec shares the same origin as the selected auth source and the credential-to-header mapping is unambiguous.

## Prevention
- When broadening a multi-spec merge, audit whether the merged field is global at runtime or scoped per resource/endpoint.
- Add negative tests for incompatible auth boundaries whenever a merge starts collecting metadata from secondary specs.
- Prefer compatibility predicates over product-name special cases; combo CLIs should work across API families without hardcoded vendor assumptions.

## Related Issues
- GitHub issue #1526
- `docs/solutions/design-patterns/auth-envvar-rich-model-2026-05-05.md`
