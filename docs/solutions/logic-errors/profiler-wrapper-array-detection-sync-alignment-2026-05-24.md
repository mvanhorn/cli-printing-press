---
title: Profiler wrapper-array detection must match sync extraction
date: 2026-05-24
category: docs/solutions/logic-errors
module: internal/profiler
problem_type: logic_error
component: tooling
symptoms:
  - Generated CLIs can omit syncable resources for APIs that return typed object envelopes such as contacts or opportunities arrays.
  - Runtime sync extraction can support a response envelope shape that the profiler never registers.
  - Type-name fallbacks can accidentally mark singleton object responses as syncable when field metadata proves they are not lists.
root_cause: logic_error
resolution_type: code_fix
severity: medium
related_components:
  - internal/generator/templates/sync.go.tmpl
  - generated-cli-runtime
tags:
  - profiler
  - sync
  - wrapper-envelope
  - generated-cli
  - agent-native
---

# Profiler wrapper-array detection must match sync extraction

## Problem

The profiler decides which resources become syncable, while generated sync code later extracts items from the chosen endpoint responses. If those two heuristics drift, the generated CLI can either omit usable store-backed resources or advertise local-cache resources that extract the wrong data.

## Symptoms

- APIs with envelopes such as `{"contacts": [...], "meta": {...}}` or `{"opportunities": [...]}` do not get those resources into `defaultSyncResources()`.
- Generated sync runtime already knows how to extract a single unambiguous array from an envelope, but the profiler rejects the endpoint before generation.
- Broad type-name fallbacks such as `*Response` or `*Result` can classify a singleton response as list-shaped even when the type fields contain no item array.
- Multi-array envelopes need a shared tie-breaker list; if the runtime knows `features` but the profiler does not, store and agent surfaces drift.

## What Didn't Work

- **Curated wrapper keys only.** Keys like `data`, `results`, and `items` miss common plural resource envelopes such as `contacts`, `opportunities`, `pipelines`, and `tags`.
- **Accepting every single array field blindly.** Singleton objects often include relationship arrays, for example `ProfileResponse{roles: []Role}`. Treating those as top-level collections registers the wrong resource for sync.
- **Trusting collection words without checking the array field.** A `search` endpoint can still return a singleton envelope whose only array is `errors` or `auditLog`. The field name must match the resource context.
- **Counting ancillary arrays as competing item arrays.** Envelopes such as `{"companies": [...], "errors": [...]}` still have one meaningful item array. Error and warning arrays should not make the profiler drop an otherwise clear wrapper.
- **Leaving type-name fallback after a type definition exists.** Once the parser has a real type entry, field metadata should win. A `SettingsResponse` with no arrays should not become syncable just because the type name contains `Response`, even if the parsed field list is empty.
- **Fixing only runtime extraction.** Runtime extraction cannot help if the profiler never registers the resource, and profiler registration is unsafe if runtime extraction cannot choose the same item array.

## Solution

Keep three cases distinct in the profiler:

1. Direct array responses are list-shaped.
2. Known wrapper array keys remain authoritative for ambiguous multi-array envelopes, and the list must stay aligned with generated `extractPageItems`.
3. A typed object with exactly one meaningful array field is list-shaped only when the field name matches the endpoint context: the endpoint name or a static path segment must overlap the array field's resource name.
4. Ancillary arrays such as `errors`, `warnings`, and `validation_errors` do not count as item arrays for the single-array heuristic.

When a type definition exists, do not fall through to the type-name fallback after field scanning fails. The fallback is only for missing type metadata.

The regression coverage should include both sides of the heuristic:

```go
assert.Contains(t, syncNames, "contacts")
assert.Contains(t, syncNames, "opportunities")
assert.Contains(t, syncNames, "companies")
assert.Contains(t, syncNames, "open-opportunities")
assert.Contains(t, syncNames, "places") // known features wrapper
assert.NotContains(t, syncNames, "settings")
assert.NotContains(t, syncNames, "empty-settings")
assert.NotContains(t, syncNames, "audits")
assert.NotContains(t, syncNames, "profile")
```

## Why This Works

The profiler and generated sync runtime now agree on extractable list envelopes without making every object-with-array a collection. Resource-shaped single arrays unlock SaaS envelopes like contacts and opportunities, including envelopes that also contain ancillary error arrays. Known wrapper keys cover ambiguous cases such as GeoJSON-style `features` plus `bbox`. Existing type metadata prevents list-sounding response names from overriding concrete field evidence, including empty parsed field lists.

## Prevention

- When changing generated extraction keys, update profiler wrapper detection in the same change.
- Add positive and negative profiler tests for every new envelope heuristic.
- Include singleton-object negative fixtures with list-sounding type names such as `SettingsResponse` or `ProfileResponse`.
- Include collection-named endpoint negatives where the single array field is not the resource collection.
- Include positive fixtures for compound field names and ancillary arrays, so future tightening does not regress common SaaS response envelopes.
- For generator-owned behavior, test the profiler decision as well as runtime extraction; one green layer does not prove the other is aligned.

## Related Issues

- GitHub issue #1689
- docs/solutions/logic-errors/html-response-format-sync-extraction.md
- docs/solutions/logic-errors/paginated-all-needs-advanceable-signal-2026-05-21.md
