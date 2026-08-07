---
title: "MCP handler pre-marshaled bodyArgs to []byte, causing base64-encoded JSON on every POST/PUT/PATCH"
date: 2026-05-13
category: runtime-errors
module: internal/generator/templates
problem_type: runtime_error
component: tooling
symptoms:
  - "Every mutating MCP tool (POST/PUT/PATCH) sent the request body as a base64-encoded JSON string (e.g. \"eyJ0b2tlbiI6Li4ufQ==\") instead of the JSON object the spec described"
  - "Strict upstream APIs (Pushover, Stripe, Twilio, Jira, Linear) rejected the malformed payload"
  - "CLI-driven mutations worked correctly while MCP-driven mutations of the same endpoint failed — only the MCP path was affected"
  - "Bug reproduced in 38 published CLIs that ship mutating MCP tools, traceable to a single template emission shape"
root_cause: wrong_api
resolution_type: code_fix
severity: high
tags: [mcp, json-marshal, base64-encoding, wire-format, template-emission, encoding-json-gotcha]
---

# MCP handler pre-marshaled bodyArgs to []byte, causing base64-encoded JSON on every POST/PUT/PATCH

## Problem

The generator's `mcp_tools.go.tmpl` emitted `body, _ := json.Marshal(bodyArgs)` and then passed the resulting `[]byte` to `c.PostWithParams(path, params, body)` (and the PUT/PATCH equivalents). The generated `client.do()` already calls `json.Marshal(body)` on whatever it receives. When `body` arrived as a `[]byte`, Go's `encoding/json` applied its special-case behavior for byte slices and base64-encoded the bytes into a quoted JSON string — so every mutating MCP tool sent payloads shaped like `"eyJ0b2tlbiI6Li4ufQ=="` instead of `{"token":"..."}`. Strict APIs rejected the malformed bodies.

## Symptoms

- POST/PUT/PATCH endpoints invoked through the MCP server returned 4xx errors from strict APIs even with valid arguments.
- The same endpoint invoked through the generated CLI worked correctly, because CLI handlers passed `map[string]any` straight to `c.Post(...)`.
- Network captures showed a quoted base64 string where the JSON request object should have been.
- The bug was visible in 38 published CLIs that wrap mutating MCP tools (a `grep -l 'body, _ := json.Marshal(bodyArgs)' library/**/internal/mcp/tools.go` in the public-library mirror returned 38 matches), confirming the defect lived in the shared template, not any individual CLI.

## What Didn't Work

- Inspecting per-CLI generated `tools.go` files for typos — the shape was identical across every affected CLI because it came from a single template.
- Looking for an issue in the OpenAPI parser or the body-binding code — the binding step correctly assembled `bodyArgs` as `map[string]any`. The corruption happened one step later, in the call to the client.
- Adding a `Content-Type: application/json` header to the request — the header was already set; the issue was the body content, not the framing.

## Solution

Drop the intermediate `json.Marshal` in each of the three mutating switch branches and forward `bodyArgs` directly. The downstream `client.do()` marshals whatever it receives.

```go
// internal/generator/templates/mcp_tools.go.tmpl
// Before:
case "POST":
    // ... multipart / form / bodyJSONOverride branches with `break` guards ...
    body, _ := json.Marshal(bodyArgs)
    data, _, err = c.PostWithParams(path, params, body)

// After:
case "POST":
    // ... multipart / form / bodyJSONOverride branches with `break` guards ...
    data, _, err = c.PostWithParams(path, params, bodyArgs)
```

The `bodyJSONOverride` path is unchanged because it already passed a `json.RawMessage`, which round-trips through `json.Marshal` verbatim via its `MarshalJSON` method.

Regression test in `internal/generator/generator_test.go::TestMCPHandlerPassesBodyArgsMap` asserts every branch forwards `bodyArgs` directly and that `json.Marshal(bodyArgs)` does not appear in the generated output. Five golden fixtures under `testdata/golden/expected/.../internal/mcp/tools.go` were regenerated to match the new emission.

## Why This Works

`encoding/json` treats `[]byte` as a special case: it base64-encodes the bytes into a JSON string, because that is the standard way to round-trip arbitrary binary data through JSON. The `client.do()` helper accepts `body any` and marshals it internally — that contract works correctly for `map[string]any`, structs, slices, and `json.RawMessage` (which opts out of marshaling via its custom `MarshalJSON`). Passing an already-marshaled `[]byte` to a body-as-any helper double-encodes by design.

The fix aligns the MCP path with what the CLI path already did: the layer that owns the wire format (the client) is the only layer that marshals, and producers hand it the live Go value, not a serialized intermediate.

## Prevention

- When a helper takes `body any` and marshals internally, never pass it a `[]byte` you produced with `json.Marshal`. Either pass the raw Go value, or convert to `json.RawMessage` if you genuinely need to forward pre-serialized JSON.
- Pin the template's emission shape with a string-contains regression test that asserts both the positive ("body forwarded directly") and negative ("no `json.Marshal(bodyArgs)` pre-marshal") forms. Generator templates fail silently — golden fixtures catch drift, but a focused unit test makes the WHY recoverable.
- When fixing a template-level defect, sweep the published library for the same shape (`grep -l '<defect signature>' library/**/internal/...`) so the reprint/republish backlog can be sized at filing time. The Printing Press intentionally separates machine fixes from per-CLI reprints; counting affected CLIs early prevents surprise after the fix lands.
- Go's `encoding/json` has other type-driven special cases (e.g., `time.Time.MarshalJSON`, `*url.URL.MarshalJSON` returning the string form). When a downstream `Marshal` call sees a different result than expected, check whether the input type has a `MarshalJSON` method or is one of the stdlib special cases (`[]byte`, `Marshaler`, `encoding.TextMarshaler`).

## Related Issues

- [#1352](https://github.com/mvanhorn/cli-printing-press/issues/1352) — Original bug report surfaced by Greptile during review of [printing-press-library#511](https://github.com/mvanhorn/printing-press-library/pull/511), with the 38-CLI grep and a hand-patch on the `feat/pushover` branch ([bdd07db4](https://github.com/mvanhorn/printing-press-library/commit/bdd07db4)).
- [`docs/solutions/logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md`](../logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md) — Different bug in the same MCP handler template (URL path placeholder vs CLI positional query arg). Same file, different root cause.
