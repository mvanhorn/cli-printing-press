---
title: "Paginated --all needs an advanceable response signal"
date: 2026-05-21
category: logic-errors
module: internal/openapi
problem_type: logic_error
component: tooling
symptoms:
  - "Generated CLI --all fetches page 1, emits a normal complete event, and exits 0"
  - "Numeric response cursors such as meta.nextPage do not advance pagination"
  - "has_more-only pagination can repeat the same request without a cursor update"
root_cause: logic_error
resolution_type: code_fix
severity: high
related_components:
  - internal/generator
  - generated-cli-runtime
tags:
  - pagination
  - openapi-parser
  - generated-cli
  - truncation-warning
  - agent-native
---

# Paginated --all needs an advanceable response signal

## Problem

Generated read commands can expose `--all` even when the Printing Press has not wired a response signal that lets the runtime advance beyond page 1. In issue #1688, a generated command fetched 100 records from a 3,204-record collection, emitted `{"event":"complete","total":100,"pages":1}`, and exited 0. That is dangerous for agent workflows because downstream analysis treats the truncated data set as complete.

## Symptoms

- `--all --json` prints one `page_fetch` event, then a normal `complete` event.
- The reported `total` is the number of fetched records, not the upstream total available.
- Response metadata includes a next-page signal such as `meta.nextPage`, but generated command code passes an empty `nextCursorPath`.
- When a response signal is numeric, string-only cursor extraction silently fails and the loop stops.
- When a response has `has_more: true` but no extracted cursor, the loop can re-fetch the same page because the request params never change.

## What Didn't Work

- **Relying on a pagination block with only a limit param.** That makes `--all` available but does not prove the runtime can advance.
- **String-only cursor extraction.** Some APIs return page numbers or numeric IDs as cursors. Treating only JSON strings as cursors recreates page-1 truncation.
- **Continuing on `has_more` alone.** A has-more flag says more data exists; it does not say how to request it. Continuing without mutating the cursor param repeats the same request.
- **Only testing `paginatedGet` with manually supplied paths.** That verifies runtime behavior but misses the generator-facing failure where the OpenAPI parser never populated `NextCursorPath`.

## Solution

Keep parser detection and runtime pagination aligned around advanceable signals:

1. Detect nested response metadata such as `meta.nextPage`, `pagination.next_page`, and `response_metadata.next_cursor` when the request declares a real cursor or page parameter.
2. Do not wire a response-side next cursor when `CursorParam` is empty. A cursor value with no query key cannot advance the request safely.
3. Convert numeric cursor values to strings in `paginatedGet` so page-number cursors like `2` can flow back through `map[string]string` request params.
4. When `--all` has no declared `nextCursorPath` or `hasMoreField`, emit an explicit structured truncation event instead of a normal-looking page-1 completion.
5. When `hasMoreField` is true but no cursor was extracted, emit a structured truncation event and stop instead of looping.

The important invariant is: `--all` may only continue when the next request will be observably different from the previous request. If the runtime cannot prove that, it should return the pages it has and emit an agent-readable truncation warning.

## Why This Works

The fix separates "more pages exist" from "the runtime can advance." `NextCursorPath` plus a non-empty `CursorParam` is advanceable. A numeric cursor is advanceable after string conversion. A bare has-more flag is not advanceable, so it becomes an explicit truncation signal rather than a loop trigger.

The parser-side regression test starts from an OpenAPI response schema with `meta.nextPage` and verifies the generated command passes `"meta.nextPage"` into `resolvePaginatedRead`. The runtime regression tests cover numeric cursor advance, non-`--all` numeric truncation warnings, missing-signal `--all` warnings, and has-more-without-cursor warnings.

## Prevention

- Add parser-level and generated-runtime tests for pagination changes. A direct helper test is not enough for generator-owned behavior.
- Treat any pagination signal as incomplete unless the request side has a known parameter to carry it.
- Include negative tests for loop progress: if a branch can `continue`, assert the request params changed first.
- Run `scripts/golden.sh verify` after template changes, and update fixtures only when emitted generated CLI behavior intentionally changes.

## Related Issues

- GitHub issue #1688
- docs/solutions/logic-errors/openapi-parser-missing-x-mcp-extension-support-2026-05-08.md
