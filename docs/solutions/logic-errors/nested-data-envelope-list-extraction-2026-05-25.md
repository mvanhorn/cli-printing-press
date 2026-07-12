---
title: Nested data envelopes need ordered list extraction
date: 2026-05-25
category: docs/solutions/logic-errors
module: internal/generator
problem_type: logic_error
component: tooling
symptoms:
  - Generated sync can persist zero rows from JSend-style list responses whose array sits under a nested data object.
  - Live read write-through caching can miss rows from the same nested envelope shape.
  - Broad single-array fallback can accidentally treat metadata arrays or child arrays as resource items.
root_cause: logic_error
resolution_type: code_fix
severity: medium
related_components:
  - internal/generator/templates/sync.go.tmpl
  - internal/generator/templates/data_source.go.tmpl
  - generated-cli-runtime
tags:
  - generated-cli
  - sync
  - write-through-cache
  - jsend
  - envelope-unwrapping
---

# Nested data envelopes need ordered list extraction

## Problem

Some APIs return list responses as a two-level envelope, for example `{"success":true,"data":{"users":[...]}}`. Generated CLIs previously looked for a direct array or a one-level wrapper array, so sync and live read write-through paths could silently persist zero rows even though the API returned real data.

## Symptoms

- `sync` receives a successful response body with items under `data.<resource>` but stores no rows.
- Live read write-through caching sees the same response shape and leaves the local table empty.
- A top-level metadata array such as `errors` or `warnings` can look like the only array sibling unless extraction checks nested `data` before broad fallback.
- A detail object with an ID and one child array can be mistaken for a list envelope if arbitrary single-array fallback runs before detail-object guards.

## What Didn't Work

- **Only adding another wrapper key.** `data` already existed as a wrapper key; the missing behavior was descending into object-valued `data` and then rerunning the item-array extraction rules.
- **Running broad single-array fallback first.** That accepts any lone object array and can select `errors`, `warnings`, or a child relationship array before the real nested resource list is inspected.
- **Treating all `data:null` envelopes as empty pages.** Existing generated tests require bare `{"data":null}` to remain a non-empty singleton-field response. The empty-page special case needs a failure signal such as `success:false` or `status:"fail"`.

## Solution

Keep list extraction ordered by confidence:

1. Try explicit top-level list wrapper keys first.
2. For known data envelope keys such as `data` and `result`, unwrap one object level and rerun explicit-key plus single-array extraction inside that object.
3. Only after nested envelopes have been checked, run the broad single-array fallback on the top-level object.
4. Ignore known metadata arrays such as `errors` and `warnings` during broad sync extraction.
5. In write-through caching, allow arbitrary single-array fallback only when every non-array sibling is known list metadata. Detail objects with real scalar fields must continue through the single-object upsert path.
6. Merge pagination from inner and outer envelopes: if the inner object has `has_more` but no cursor, an outer `pagination.next_cursor` still needs to advance the sync loop.

## Why This Works

The order prevents lower-confidence heuristics from preempting higher-confidence envelope structure. Nested `data` is a known API wrapper, so it deserves inspection before arbitrary sibling arrays. The write-through path keeps its existing detail-object safety rule by requiring non-array siblings to be metadata before treating a single array as a collection.

The regression tests generate real CLI code and exercise the unexported generated helpers in package scope. They cover nested list extraction, inner and outer pagination placement, failed JSend `data:null` envelopes, metadata arrays, empty nested lists, and detail objects with child arrays.

## Prevention

- When adding response-envelope support, test both sync extraction and write-through cache behavior.
- Preserve extraction order in tests: explicit keys before nested data envelopes, nested data envelopes before broad single-array fallback.
- Include negative fixtures for metadata arrays and detail objects with child arrays.
- Include pagination fixtures where cursor metadata lives at a different envelope level from the item array.
- Run `scripts/golden.sh verify` after template changes and update generated fixtures only for intentional emitted-code drift.

## Related Issues

- GitHub issue #2013
- docs/solutions/logic-errors/profiler-wrapper-array-detection-sync-alignment-2026-05-24.md
- docs/solutions/logic-errors/paginated-all-needs-advanceable-signal-2026-05-21.md
