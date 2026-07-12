---
title: "MCP template's literal `eq .Type \"integer\"` check missed OpenAPI-parsed `\"int\"` shape — every numeric body/query param bound as JSON string"
date: 2026-05-14
category: logic-errors
module: internal/generator/templates
problem_type: logic_error
component: tooling
symptoms:
  - "Pushover's update_highest_message.json endpoint rejected every direct CLI invocation and every MCP tool call with HTTP 400 (`\"message\":\"789\"` instead of `\"message\":789`)."
  - "Surfaced by Greptile on printing-press-library#511; local fixes shipped on commits 19bc3147 (inline novel-cmd) and 49156f77 (standalone CLI + MCP), filed upstream as #1362."
  - "Visible in golden output: `WithString(\"limit\", ...)`, `WithString(\"year\", ...)`, `WithString(\"overwrite\", ...)`, `WithString(\"notify\", ...)` against an OpenAPI golden spec that declared those params as `type: integer` / `type: boolean`."
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - generator-template
  - mcp
  - openapi-parser
  - type-normalization
  - primitive-kind
  - withnumber
  - withboolean
  - integer-body-fields
  - golden-harness
---

# MCP template's literal `eq .Type "integer"` check missed OpenAPI-parsed `"int"` shape

## Problem

`mcp_tools.go.tmpl` and `mcp_intents.go.tmpl` decided which `mcplib.With*` binder to emit using literal string comparison on `spec.Param.Type`:

```gotemplate
{{- if eq .Type "integer"}}
            mcplib.WithNumber(...)
{{- else if eq .Type "boolean"}}
            mcplib.WithBoolean(...)
{{- else}}
            mcplib.WithString(...)
{{- end}}
```

Two parser code paths populate `Param.Type`:

- The **internal-spec** YAML parser preserves whatever the author wrote, so `type: integer` flows through verbatim.
- The **OpenAPI** parser at `internal/openapi/parser.go:3616` (`mapSchemaType`) normalizes to canonical Go-shaped tokens: `openapi3.TypeInteger -> "int"`, `openapi3.TypeNumber -> "float"`, `openapi3.TypeBoolean -> "bool"`.

The `if eq .Type "integer"` branch only matched the internal-spec literal. Every OpenAPI-parsed numeric body/query/path param fell through to `WithString`, so the JSON-RPC client sent `{"message":"789"}` and any type-strict endpoint (Pushover, several Stripe write paths, Linear filters, anything declaring `type: integer` body fields) returned HTTP 400.

The CLI surface was *not* affected: `bodyVarDecls` / `bodyFlagRegs` / `bodyMapForEndpoint` route through `cobraFlagFunc` and `goType`, which both call `primitiveKind` — that helper already collapses `"integer"`/`"int"` to the same kind. The MCP template was the only emitter doing literal-match without normalization.

## Symptoms

- Pushover `update_highest_message.json` rejected `"message":"789"` (the StringVar flag value flowed straight into the body map and into the MCP wire payload).
- Five sibling Pushover fields exhibited the same shape: `glances_update` `count` and `percent`; `messages_send` `priority`/`retry`/`expire`/`timestamp`; the `html`/`monospace`/`encrypted` integer-as-bool flags.
- Visible in the golden harness once the fix landed: `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/mcp/tools.go` flipped 6 lines from `WithString` to `WithNumber` (`limit`, `year`) and `WithBoolean` (`overwrite`, `notify`, `completed`).
- Sampling against the published library by grep: every published CLI generated from an OpenAPI vendor spec emitted `WithString` for `type: integer` query/path params, so the bug was uniform across the catalog, not specific to a single API.

## What Didn't Work

- Looking only at the CLI emission. A first reading of the issue assumed the CLI `StringVar` shape needed a typed `Int64Var` patch; closer inspection of `cobraFlagFunc` / `primitiveKind` showed the CLI side was already correct for both spec shapes. The Pushover CLI's `var bodyMessage string` was a spec-author bug (the internal YAML declared `type: string` for `message`), not a generator bug. The generator bug was confined to the MCP template.
- Special-casing the Pushover-named field. The fix needed to land in the generator (one place), not in the printed CLI (one site per affected field per CLI). The `printing-press-library#511` patches `19bc3147` and `49156f77` are local printed-CLI workarounds that the upstream fix renders unnecessary on regen.

## Solution

Add a single `mcpBindingFunc(t string) string` helper that funnels the type through the existing `primitiveKind` normalizer and returns the `mcplib.With*` name. Replace the three duplicated inline switches in the MCP templates with one call each.

**Helper** (`internal/generator/generator.go`, alongside `cobraFlagFunc`):

```go
func mcpBindingFunc(t string) string {
    switch primitiveKind(t) {
    case "int", "float":
        return "WithNumber"
    case "bool":
        return "WithBoolean"
    default:
        return "WithString"
    }
}
```

Registered in the template FuncMap immediately after `cobraFlagFunc` / `cobraFlagFuncForParam` (same "kind-mapping helper" cluster).

**Template change** (`mcp_tools.go.tmpl`, `mcp_intents.go.tmpl`) — five call sites total: top-level params, top-level body, sub-resource params, sub-resource body in `mcp_tools.go.tmpl`, and the intent params loop in `mcp_intents.go.tmpl`:

```gotemplate
{{- range $endpoint.Body}}
            mcplib.{{mcpBindingFunc .Type}}({{printf "%q" (mcpInputName .)}}{{if .Required}}, mcplib.Required(){{end}}, mcplib.Description({{printf "%q" (mcpParamDesc .)}})),
{{- end}}
```

**Tests**:

- `TestMCPBindingFunc` (`cursor_param_test.go`, table-driven): asserts every cross-product of `{integer, int, number, float, boolean, bool, string, "", object, array, INTEGER}` maps to the expected `With*` name.
- `TestMCPBindingNumericTypesAcrossSpecShapes` (`generator_test.go`, integration): generates a two-endpoint spec — one with OpenAPI-shape types (`int`, `bool`) and one with internal-spec literals (`integer`, `boolean`) — and asserts both shapes emit `WithNumber` / `WithBoolean` on body and query surfaces.

**Golden update**: one expected file (`generate-golden-api/.../mcp/tools.go`), 6 lines. The fixture was already an OpenAPI spec with `type: integer` and `type: boolean` params, so the golden diff *is* the bug shape rendered visible.

## Why this matters / how to spot the pattern

This is the same shape as any "literal-match in a template against a typed-but-non-canonical input field" bug. The signal is: a Go helper (here `primitiveKind`) that accepts multiple aliases for the same kind, used everywhere *except* the templates, which do their own literal compares instead.

Two checks to apply on adjacent code:

1. Grep template tree for `eq .Type "<literal>"` clauses. Each one is a candidate for the same bug if the source field can carry parser-canonical synonyms (`int`/`integer`, `float`/`number`, `bool`/`boolean`, `array`/`list`, etc.).
2. When a normalizer helper exists in Go (`primitiveKind`, `oneOf`, `canonicalize…`), templates calling raw `.Field` instead of `(normalize .Field)` are smells. Push the normalization through a one-line FuncMap helper rather than duplicating the switch in template-language.

The CLI surface stayed correct here because `cobraFlagFunc` and `goType` both call `primitiveKind` internally. The MCP template was the outlier — that's worth a project-wide grep next time someone changes a `Param.Type` consumer.
