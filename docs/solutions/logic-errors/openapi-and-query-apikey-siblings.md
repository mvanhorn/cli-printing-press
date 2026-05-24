---
title: OpenAPI AND query apiKey siblings must remain required credentials
date: 2026-05-24
category: logic-errors
module: OpenAPI auth generation
problem_type: logic_error
component: authentication
symptoms:
  - Generated CLIs for Trello-shaped specs sent only the primary query apiKey
  - Required sibling query credentials were parsed out of the auth model
  - Scorecard auth_protocol under-scored the newly supported query-sibling shape
root_cause: logic_error
resolution_type: code_fix
severity: high
tags: [openapi, auth, api-key, query-params, scorecard]
---

# OpenAPI AND query apiKey siblings must remain required credentials

## Problem
OpenAPI security requirements can require multiple schemes at once. When a spec declares two `apiKey` schemes in the same requirement, such as query parameters `key` and `token`, the printed CLI must send both credentials on every request.

## Symptoms
- The parser kept sibling `apiKey` schemes only when they were `in: header`.
- Trello-shaped specs with `APIKey` plus `APIToken` generated a client that configured and sent `key`, but omitted `token`.
- The scorecard did not understand exact sibling query-parameter emission, so `auth_protocol` could remain low even after the generator supported the shape.

## What Didn't Work
- Treating the second scheme as a component-only declaration loses the OpenAPI AND semantics. A sibling scheme in the same security requirement is a required credential, not optional metadata.
- Blindly promoting query credentials into multi-spec merged auth is unsafe. `Auth.AdditionalHeaders` is global in the generated client, and query credentials are usually origin or endpoint scoped.
- Scoring generic query plumbing such as `.Query()` is too weak for composed auth. The verifier needs to see the exact required query parameter name.

## Solution
Allow supported sibling `apiKey` schemes in AND requirements to become additional credentials when their placement is `header` or `query`.

For generation:

- Preserve the scheme placement in `AdditionalAuthHeader.In`.
- Emit header siblings with `req.Header.Set(...)`.
- Emit query siblings by updating `req.URL.Query()` and writing `RawQuery`.
- Load and doctor-check the sibling environment variable the same way as header siblings.

For multi-spec safety:

- Keep query siblings out of merged global auth until auth can be scoped per resource, endpoint, or origin.
- Continue allowing mergeable header siblings only when existing compatibility predicates pass.

For scoring:

- Parse `x-auth-env-vars` into the scorecard security scheme model.
- Credit query `apiKey` schemes only when the generated client writes the exact query parameter name, such as `q.Set("token", ...)`.
- Add positive and negative `auth_protocol` regressions for an AND group of two query apiKeys.

## Why This Works
OpenAPI OR and AND security meanings are encoded by nesting: separate objects are alternatives, while multiple keys in one object are all required. Keeping header and query sibling credentials in the auth model preserves that contract without inventing product-specific rules.

The multi-spec filter prevents a single-spec fix from reintroducing the known global-auth leak class. Query credentials are correct for the owning spec's generated client path, but they are not safe to union into a combined CLI's global request path.

## Prevention
- When broadening parser auth support, update both generation tests and dependent verifiers in the same change.
- Regression tests for composed auth should cover parser output, generated source, generated runtime request behavior, and scorecard scoring.
- Keep multi-spec negative tests for any auth field that is global at runtime.

## Related Issues
- GitHub issue #1663
- `docs/solutions/logic-errors/multi-spec-auth-scope-union-2026-05-22.md`
