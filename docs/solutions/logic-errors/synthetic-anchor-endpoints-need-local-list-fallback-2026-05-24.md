---
title: "Synthetic anchor endpoints need local list fallback semantics"
date: 2026-05-24
category: logic-errors
module: internal/generator
problem_type: logic_error
component: tooling
related_components:
  - testing_framework
symptoms:
  - "Synthetic-kind generated CLIs tried to dial placeholder .local base URLs for anchor list commands."
  - "After sync populated the local store, endpoint-mirror commands still reported no local data."
  - "Dogfood happy_path and json_fidelity failed for synthetic anchor resources that should have read synced rows."
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - synthetic-specs
  - local-store
  - endpoint-mirror
  - fallback
  - generated-cli
---

# Synthetic anchor endpoints need local list fallback semantics

## Problem

Synthetic specs use anchor resources to define local store shape, but those resources do not have a real upstream REST origin. The generator still emitted normal endpoint-mirror commands, so a command like `<cli> products --json` tried to call `https://<api>.local/products` and failed before returning the synced local data agents expected.

## Symptoms

- Synthetic anchor commands failed with `API unreachable and no local data` even after sync had inserted rows for the resource.
- The JSON envelope never exposed `meta.source: "local"` or a synthetic-specific fallback reason, so agents could not tell the fallback path was intentional.
- Dogfood correctly flagged happy-path and JSON-fidelity failures for synthetic CLIs, forcing per-CLI hand shims.

## What Didn't Work

- Only changing the fallback reason. A synthetic-specific `meta.reason` is useful, but it does not fix the no-data failure if the generated command tells `resolveLocal` that a list endpoint is a single-object lookup.
- Suppressing endpoint command emission for synthetic specs. That avoids the bad network call but removes a useful typed surface from generated CLIs.
- Treating this as a dogfood or scorer problem. The verifier was catching real generated runtime behavior.

## Solution

Keep the endpoint command, but make the generated read path encode both pieces of information the local resolver needs:

1. Synthetic specs, and specs whose placeholder base URL ends in `.local`, emit a generated fallback reason of `synthetic_anchor_fallback`.
2. Non-paginated GET endpoints whose response type is an array are passed to `resolveRead` as list reads only when the endpoint has no path scope and either has the canonical `list` endpoint name or belongs to a synthetic / `.local` spec.

The generator-level helpers make the invariant explicit:

```go
func networkFallbackReason(s *spec.APISpec) string {
    if s == nil {
        return "api_unreachable"
    }
    if s.IsSynthetic() {
        return "synthetic_anchor_fallback"
    }
    u, err := url.Parse(strings.TrimSpace(s.BaseURL))
    if err == nil && strings.HasSuffix(strings.ToLower(u.Hostname()), ".local") {
        return "synthetic_anchor_fallback"
    }
    return "api_unreachable"
}

func localReadIsList(supportsAllPagination bool, apiSpec *spec.APISpec, endpointName string, endpoint spec.Endpoint) bool {
    if supportsAllPagination {
        return true
    }
    if endpointHasPathScope(endpoint) {
        return false
    }
    if strings.EqualFold(endpointName, "list") {
        return true
    }
    return networkFallbackReason(apiSpec) == "synthetic_anchor_fallback" && strings.EqualFold(endpoint.Response.Type, "array")
}
```

The endpoint and promoted-command templates now pass `true` for top-level list-shaped local fallback reads even when the endpoint is not paginated. `data_source.go.tmpl` uses the generated `networkFallbackReason` constant for network-error fallback envelopes.

## Why This Works

`resolveRead` has two jobs: try the live API first, then fall back to the local store for network errors. The second step is shape-sensitive. When `isList` is false, `resolveLocal` treats the URL path tail as an ID and calls `Get(resourceType, id)`. For `/products`, that means looking for an ID literally named `products`, so the fallback reports no data even though `List(resourceType)` would return synced rows.

The bug was not just that synthetic specs used a placeholder host. It was that list-shaped, non-paginated endpoint mirrors were classified as single-object fallbacks. Marking array responses as local list reads lets the existing store path return synced rows while preserving live API behavior for non-synthetic specs.

## Prevention

- When generated fallback code crosses from API transport to local store, carry response shape, scope, and spec kind. Returning every cached row is correct for top-level synthetic anchors, but unsafe for path-scoped child collections or non-synthetic search/filter endpoints.
- Regression tests should execute a generated CLI command after seeding the local store, not only assert template text. This catches the `Get` vs `List` distinction.
- Keep synthetic fallback metadata separate from fallback mechanics. `meta.reason` is an observability contract; `isList` is the data access contract.
- Include a negative fixture for live APIs so fallback labels do not accidentally reclassify ordinary network failures as synthetic behavior.

## Related Issues

- [#1717](https://github.com/mvanhorn/cli-printing-press/issues/1717) - synthetic-spec anchor commands fail because endpoint-mirror tries `.local` base URL.

## Related Docs

- `docs/solutions/logic-errors/store-columns-sourced-from-request-params-instead-of-response-2026-05-08.md` - adjacent generator lesson: local surfaces must derive from response shape, not request-side or path-side hints.
- `docs/solutions/logic-errors/dogfood-soft-failure-error-path-opt-out-2026-05-22.md` - dogfood should reflect generated runtime behavior rather than pushing per-CLI semantic shims.
