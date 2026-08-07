---
title: "Lenient OpenAPI parsing must stub missing local schema refs visibly"
date: 2026-05-22
category: logic-errors
module: internal/openapi
problem_type: logic_error
component: tooling
symptoms:
  - "generate --lenient still exits when a converted OpenAPI spec references a missing local schema"
  - "agents patch generated specs by hand before every regen to replace dangling refs with permissive objects"
  - "existing lenient cleanup can strip a broken path instead of preserving the endpoint with degraded schema fidelity"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - openapi-parser
  - lenient-mode
  - schema-refs
  - generated-cli
---

# Lenient OpenAPI parsing must stub missing local schema refs visibly

## Problem

Converted OpenAPI specs can contain local `$ref` values pointing at `#/components/schemas/<Name>` entries the converter failed to emit. Before this fix, `generate --lenient` still aborted on those dangling local schema refs, even though the operator had explicitly chosen tolerant parsing.

## Symptoms

- `generate --lenient` logs a cleanup attempt, then exits with `map key "<Name>" not found`.
- A manual pre-generation patcher that injects `{type: object, additionalProperties: true}` for each missing schema lets the same spec generate successfully.
- The older lenient cleanup path removes references or entire paths, which can turn a recoverable missing schema into lost endpoint coverage.

## What Didn't Work

- Treating the issue as a printed-CLI patch would only help one generated CLI. The next regeneration from the same upstream spec would fail again.
- Letting the old lenient cleanup remove paths preserves parser success in some cases, but it silently drops endpoint surface area.
- Stubbing every `$ref`-looking string in the raw document is too broad. Example payloads and vendor extensions can legitimately contain literal `$ref` data that is not an OpenAPI Reference Object.

## Solution

Handle exact missing local schema refs before the OpenAPI loader runs in lenient mode:

```go
schemas[name] = map[string]any{
    "type":                 "object",
    "description":          fmt.Sprintf("Stub for missing local schema ref %s.", name),
    "additionalProperties": true,
}
warnf("stubbing missing local schema ref %q as permissive object", name)
```

The recovery is deliberately narrow:

- Only exact `#/components/schemas/<Name>` refs are stubbable.
- Nested pointers like `#/components/schemas/<Name>/properties/id` stay strict because a top-level object stub does not satisfy the full pointer path.
- Example/default payloads and extension values are skipped so literal `$ref` strings do not create fake missing schemas.
- `--strict-refs` keeps lenient cleanup available while failing on missing local schema refs.

The same parser option is wired through both `generate` and `public-param-audit`, so command surfaces that expose `--lenient` have a matching strict-ref opt-out.

## Why This Works

The permissive object stub preserves endpoint coverage while making the degradation visible through warnings. It avoids inventing typed fields from request parameters or examples, so downstream generated types degrade to an object-shaped placeholder instead of a plausible but false schema.

Failing early for `--lenient --strict-refs` prevents the older cleanup loop from deleting only the broken path and reporting success when another healthy path remains. That keeps the strict-ref contract about missing local schema refs precise without changing unrelated lenient cleanup behavior.

## Prevention

- Add parser-level coverage for both the tolerant path and the strict opt-out.
- Add command-level coverage when a parser option is surfaced as a CLI flag.
- Include adversarial fixtures for literal `$ref` strings in example/default data and for nested local schema pointers.
- Update the Printing Press skill whenever a machine behavior changes what agents should assume during generation.

## Related Issues

- Issue #1534
- `docs/solutions/logic-errors/openapi-parser-missing-x-mcp-extension-support-2026-05-08.md`
- `docs/solutions/conventions/soft-validation-in-reusable-library-packages-2026-05-06.md`
- `docs/solutions/logic-errors/store-columns-sourced-from-request-params-instead-of-response-2026-05-08.md`
