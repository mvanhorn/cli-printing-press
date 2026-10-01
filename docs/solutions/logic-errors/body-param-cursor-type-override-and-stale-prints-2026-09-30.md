---
title: JSON Body Pagination Fields Sent as Strings in Pre-4.30 Prints
date: 2026-09-30
category: docs/solutions/logic-errors
module: internal/generator
problem_type: logic_error
component: tooling
symptoms:
  - "A POST search command fails with HTTP 400 such as: Field \"page\" must be a number."
  - "The generated flag for a body field named page, offset, cursor, or min_time is declared StringVar with a quoted default such as \"1\""
  - "A sibling command whose body uses the same field name emits IntVar, so only some endpoints of one CLI fail"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags: [openapi, request-body, pagination, flag-types, json, published-library, stale-print]
---

# JSON Body Pagination Fields Sent as Strings in Pre-4.30 Prints

## Problem

`isCursorParam` retypes numeric parameters named `page`, `offset`, `cursor`,
`min_time`, `max_time` (and a few more) to `string`. For URL query parameters
that is correct: Go's float formatting turns large integers into scientific
notation, and the query string is untyped anyway.

Before v4.30.0 the same override was applied to JSON request-body fields.
The body then carried `"page": "1"`, a JSON string, where the spec declared
`type: integer`. Strict APIs reject it with HTTP 400. Lenient APIs accept it,
which is why the defect survived dogfood and verify.

## Symptoms

- Only endpoints whose body field name matches the cursor list fail. In the
  same CLI, a body field named `page` could be typed `int` on one command and
  `string` on another, depending on which template path emitted it.
- The MCP tool for the same endpoint advertises `WithNumber("page")` and
  works, so the CLI and MCP surfaces disagree.

## Solution

Fixed in the generator by #3867 (v4.30.0): body params use
`goTypeForBodyParam` / `cobraFlagFuncForBodyParam` / `defaultValForBodyParam`,
which keep the declared scalar type. The identifier, cursor, and limit
overrides are URL-only. Required booleans still use a string backing value so
omitted and explicit `false` stay distinguishable.

Rule: an override that exists to make a value survive URL encoding must never
change the JSON type of a body field. The request body is typed; the query
string is not.

## Stale printed CLIs

A generator fix does not reach already-published CLIs. On 2026-09-30 the
published library still contained this shape in CLIs printed before v4.30:

```bash
grep -rlE 'StringVar\(&body(Page|Offset)\b' --include='*.go' library/
```

matched 23 files across `wavespeed`, `cloudflare`, `listingview`, `immich`,
`spotify`, `supermemory-admin`, and `withings`. Each one is a candidate
HTTP 400 depending on how strict the upstream API is.

The published library's rule applies: patch the published CLI first, then
confirm the generator fix covers the spec shape. For the sweep, check the
spec's declared type before changing a flag. Some APIs document `page` as a
string token, and those must stay strings.

## Prevention

- `TestGeneratedPaidSubmitUploadRetryAndRecovery` compiles a print whose
  POST body declares integer `page`, `page_size`, `offset`, `cursor`,
  number `min_time`, and booleans, runs the binary against httptest, and
  asserts the body carries JSON numbers and booleans (`"page":1`, not
  `"page":"1"`). Required booleans still use a string flag and are parsed
  before marshalling. Cursor-named URL query params stay string-typed by
  design; non-cursor numeric and boolean query params keep their types.
- When the reprint/currency tooling compares a published CLI's
  `printing_press_version` against the latest release, list known fixed
  defect classes (like this one) so the operator knows a reprint or a
  targeted library patch is due.
