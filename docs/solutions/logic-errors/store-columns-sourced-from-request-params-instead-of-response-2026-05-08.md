---
title: "Store table columns sourced from request query params instead of response schema"
date: 2026-05-08
category: logic-errors
module: internal/generator/schema_builder
problem_type: logic_error
component: tooling
symptoms:
  - "Generated SQLite tables get columns named after a list endpoint's query parameters (filter, sort, per_page, page) instead of fields from the response payload"
  - "Sync reports success and writes the JSON payload to the data column, but every typed column stays NULL forever because nothing populates it"
  - "SQL queries against typed columns (WHERE state='open', ORDER BY created_at) return NULL or zero rows regardless of sync volume"
  - "Affects every paginated REST API; visible only via direct SQLite inspection or the sql tool, never at the CLI surface"
root_cause: logic_error
resolution_type: code_fix
severity: high
related_components:
  - internal/openapi
  - internal/spec
tags:
  - schema-builder
  - response-schema
  - sqlite-columns
  - openapi-parser
  - generator-pipeline
  - typefield-format
---

# Store table columns sourced from request query params instead of response schema

## Problem

`collectResponseFields` in `internal/generator/schema_builder.go` was misnamed — it iterated `endpoint.Params` (query/path parameters) and `endpoint.Body` (request body) instead of `endpoint.Response`. Every paginated REST resource ended up with a SQLite table whose typed columns mirrored its GET filter knobs rather than its response entity shape, which sync could never populate because filter inputs don't appear in response payloads.

## Symptoms

- For GitHub's `GET /issues`, the generated `issues` table emitted columns `filter, state, labels, sort, since, per_page, page` — exactly the GET query params, none of the actual response fields (`id, number, title, body, created_at, updated_at`).
- After `sync`, the `data` JSON column held the real response, but typed columns were NULL on every row.
- `SELECT state FROM issues WHERE state='open'` returned nothing because the column was uniformly NULL; the same query expressed as `SELECT json_extract(data, '$.state') FROM issues WHERE json_extract(data, '$.state') = 'open'` returned the right rows.
- `sync_complete` events reported the right counts; the failure was schema-shape, not transport.
- Same pattern reproduced across every printed CLI generated from any paginated REST API spec.

## What Didn't Work

**Fix only `schema_builder.go`, leave the parser alone.** This worked for specs whose response types were defined in `Components/Schemas` and already registered in `s.Types`. It silently failed for any spec that declared an inline list response (no `$ref`) — `s.Types[endpoint.Response.Item]` had no entry, so the resource fell back to `id/data/synced_at` with no typed columns at all. Required the parser to call `registerInlineSchemaType` so inline schemas fill the same `s.Types` map.

**Inline registration logic written directly inside `mapResponse`.** The field-walking code was ~35 lines nearly identical to what `mapTypes` already did for Components.Schemas entries. Code review flagged the duplication. Extracted `buildTypeFields(schemaRef)` as a shared helper called from both paths.

**Add an exists-check inside `mapTypes` to prevent collisions.** The goal was to let Components-defined types win over inline registrations. Adding the guard inside `mapTypes` required `mapTypes` to know about inline names, coupling the two code paths. The cleaner fix was to swap call order — `mapTypes` runs before `mapResources` (which calls `mapResponse`/`registerInlineSchemaType`), so Components types are already in `out.Types` when inline registration runs. The existing exists-check in `registerInlineSchemaType` then naturally yields without any new coupling.

**Use a flat `endpointName + "Item"` for the inline-item synthetic name.** Two resources both with a default-named GET (`list`) both computed `ListItem`. The second resource's inline registration silently inherited the first resource's field shape. Adversarial code review caught this as a P1 cross-resource collision: it re-introduced the wrong-columns class for cross-resource cases. Fixed by namespacing: `targetResourceName + "_" + endpointName` as the fallback so names are distinct per resource.

## Solution

Three files changed in coordination:

**`internal/spec/spec.go` — `TypeField` gains `Format`.**

```go
type TypeField struct {
    Name string   `yaml:"name" json:"name"`
    Type string   `yaml:"type" json:"type"`
    Enum []string `yaml:"enum,omitempty" json:"enum,omitempty"`
    Format string `yaml:"format,omitempty" json:"format,omitempty"`  // new
    Selection string `yaml:"selection,omitempty" json:"selection,omitempty"`
}
```

This carries the OpenAPI `format` hint (`date-time`, `date`) from response schema through to `sqliteType`, enabling `DATETIME` instead of `TEXT` for temporal fields end-to-end.

**`internal/openapi/parser.go` — parser registers inline response schemas.**

- Extracted `buildTypeFields(schemaRef)`: walks object properties (flattening JSON:API shapes), filters underscore-prefixed names and Go-name collisions, emits `[]spec.TypeField` with `Format` populated. Called from both `mapTypes` and the inline registration path.
- `registerInlineSchemaType(out, itemRef, fallbackName)` — when `itemRef` has no `$ref` and the slot is empty, registers the type into `out.Types` under the synthetic name `mapResponse` will later set on `endpoint.Response.Item`. Handles list-item schemas and single-object detail responses uniformly.
- `mapResponse` signature gained `out *spec.APISpec`. All three response shape branches (`{data: array}` envelope, bare array, single object) now register inline schemas. Before this change the single-object branch registered nothing — detail-only resources lost typed columns entirely.
- Inline-item synthetic names namespaced by resource: `targetResourceName + "_" + endpointName` as fallback, so two resources with default-named GETs cannot collide on a shared `ListItem` Types entry.
- Swapped the call order in `Parse`: `mapTypes` now runs before `mapResources`, so Components-defined types populate `out.Types` first and inline registrations naturally yield to them.

**`internal/generator/schema_builder.go` — `collectResponseFields` rewritten.**

Before:

```go
for _, ep := range r.Endpoints {
    if ep.Method != "GET" { continue }
    for _, p := range ep.Params { ... }   // request-side filter knobs
    for _, p := range ep.Body { ... }     // request body fields
}
// Plus a second pass over POST/PUT bodies "as they often mirror response shape"
```

After:

```go
typeName := ep.Response.Item              // what the API returns
typeDef, ok := s.Types[typeName]          // registered by parser
if !ok { continue }
for _, f := range typeDef.Fields { ... }
```

GET endpoints walked first. Non-GET (POST/PUT) walked only when no GET endpoint contributed any fields, so write-only resources (event-emit, webhook ingestion) keep typed columns from their canonical create-response shape without letting wrapper-shaped responses pollute typed columns when a GET exists.

Format-aware dedup: when the same field appears across endpoints with differing format hints, an entry with empty `Format` is upgraded when a later endpoint declares one — preventing list-vs-detail format drift from downgrading `DATETIME` to `TEXT` based on alphabetic endpoint-key order.

Additional housekeeping: `baseTableColumns` promoted to package-level slice (shared by `BuildSchema` and `buildSubResourceTable`); `textFieldKeywords` promoted to package scope; `seenIndexes` map gates `_id` index emission to prevent duplicate `IndexDef.Name`; `isScalarTypeField`/`hasTypeField` helpers extracted; fields resolved once per resource and threaded into `computeDataGravity`/`collectTextFieldNamesFromFields` instead of being re-walked five times.

## Why This Works

The original `collectResponseFields` was misnamed — it was iterating the request side of the endpoint while its declared purpose was to derive the SQLite schema from what the API *returns*. The OpenAPI parser correctly populated both `endpoint.Response.Item` (the type name) and `s.Types[<TypeName>]` (the field list), but `schema_builder.go` never read either. Since sync writes the response payload to `data` and then extracts into typed columns via `lookupFieldValue(obj, columnName)`, a column whose name is `filter` or `sort` or `per_page` can never be populated: the API does not echo filter inputs back as top-level response fields.

The fix closes the loop end-to-end: the parser registers every response item type — whether from `Components/Schemas` (`$ref`) or inline — into `s.Types` under the same name `endpoint.Response.Item` carries. The schema builder reads that entry. The column list now matches what sync actually stores.

## Prevention

**Invariant to internalize.** Response schema is the authority for SQLite column shape. Request `Params` are input filters sent *to* the API; they describe what the caller can say, not what the API returns. Any generator surface that describes the entity shape (column lists, FTS5 fields, typed indexes) must source from `endpoint.Response.Item` → `s.Types[name]`, never from `endpoint.Params` or `endpoint.Body`.

**Write-time discipline in fixture tests.** Five existing generator tests had declared request `Params` and relied on the buggy column-from-params behavior. They passed precisely because the bug was in production. When writing generator tests that assert column derivation, declare `Response.Item` and the corresponding `Types` entry — not `Params`. A test that only populates `Params` and asserts column names is testing the wrong invariant and will silently validate a future regression.

Correct fixture shape:

```go
resource.Endpoints["list"] = spec.Endpoint{
    Method: "GET",
    Response: spec.ResponseDef{Type: "array", Item: "Issue"},
}
s.Types["Issue"] = spec.TypeDef{Fields: []spec.TypeField{
    {Name: "id", Type: "integer"},
    {Name: "title", Type: "string"},
    {Name: "created_at", Type: "string", Format: "date-time"},
}}
```

**Pin the response-vs-request distinction with a regression test on every schema-builder change.** The fix added `TestBuildSchema_ColumnsFromResponseSchema` (request params don't leak), `TestBuildSchema_ParamResponseNameOverlap` (when names collide, the response field's *type* drives the column — fixture uses param=string, response=integer; asserts INTEGER), and `TestBuildSchema_NoResponseTypeFallback` (table-driven over both unregistered-type-name and empty-Item cases; both yield only base columns). Future changes to `collectResponseFields` should keep these green.

**Golden coverage gap to close.** The golden suite has no fixture for `internal/store/store.go` (the generated SQLite DDL). A regression that reverts column sourcing would pass `scripts/golden.sh verify` silently. Adding a store.go golden — with a spec that has both request params and a distinct response shape — would catch this class of bug mechanically.

**Bug shape is wider than this instance.** The same wrong-source pattern produced a related defect documented in `docs/solutions/logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md` (MCP handler treating URL path placeholders as positional query args). When adding new generator surfaces that read from endpoints, explicitly identify which field of `spec.Endpoint` is the authoritative source and add a comment naming the invariant, as the rewritten `collectResponseFields` now does.

## Related Issues

- [#698](https://github.com/mvanhorn/cli-printing-press/issues/698) — the tracked issue for this fix.
- [#689](https://github.com/mvanhorn/cli-printing-press/issues/689) — Kalshi sync correctness retro. Bug 4 in #689 ("novel-feature SQL hardcodes field names that don't match real API shape") shares the same philosophy: artifacts emitted without grounding in the response schema. Different surface (SQL authoring vs. column derivation), same root principle.
- `docs/solutions/logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md` — same bug class. Generator consumed the wrong-source data; bug survived because the wrong source partially overlapped the right source in common cases.
- `docs/solutions/logic-errors/inline-authorization-param-bearer-inference-2026-05-05.md` — same file (`internal/openapi/parser.go`), same extension pattern (a `register*` / `infer*` helper that walks an inline OpenAPI structure to fill a derivation gap).
