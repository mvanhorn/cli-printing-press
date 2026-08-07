---
title: MCP Body Param Public Name Collisions
date: 2026-05-25
category: docs/solutions/logic-errors
module: internal/generator
problem_type: logic_error
component: tooling
symptoms:
  - "A JSON request-body field disappeared from the generated MCP tool schema when its name collided with a path parameter"
  - "The generated CLI could still send a raw stdin body, but agents could not supply both public inputs through the typed MCP surface"
  - "Direct tools-manifest generation could emit duplicate public parameter names that the generated MCP surface later tried to disambiguate"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [mcp, tools-manifest, openapi, request-body, identifier-collision, wire-name]
---

# MCP Body Param Public Name Collisions

## Problem

OpenAPI permits the same JSON object key to appear in the request body that also appears as a path or query parameter. For example, `POST /tags/{id}/notes` can use the path `id` for the tag and a body `id` field for the note.

The Printing Press previously treated the path/query name as enough reason to drop or hide the body field from the typed MCP-facing shape. That avoided one duplicate public name, but it also removed a real API input from agents.

## Symptoms

- The generated MCP tool exposed `id` for the path parameter but had no separate public input for the body `id`.
- The body wire key needed to remain `id`, so simply renaming the spec field would have produced the wrong JSON payload.
- `tools-manifest.json` could drift from generated MCP output because manifest generation did not reserve the same public names before adding body params.

## What Didn't Work

Dropping body params in the OpenAPI parser is too early. The parser only sees API wire names, while the generator and manifest writer decide which public names become CLI flags, MCP inputs, and manifest parameters.

Mutating `Param.Name` is also wrong. `Name` is the wire key used for JSON payloads, URL query keys, and path replacement. Collision handling must use `IdentName` or manifest `wire_name` so the public input can change without changing the API request.

## Solution

Keep colliding body fields from the OpenAPI parser and disambiguate them later at each public surface:

- In generator flag/body dedupe, reserve endpoint params, including positional path params, before naming top-level object body fields.
- When a top-level body field collides, set `IdentName` to a deterministic suffix such as `id_2`; templates expose `id-2` publicly and still bind `WireName: "id"` for the JSON body.
- In `tools-manifest.json`, reserve endpoint params and mutating-command reserved names before adding body params, then suffix the manifest public name while preserving `wire_name`.

Regression coverage should assert both surfaces:

- Generated `internal/mcp/tools.go` contains both the path input (`id`) and the body input (`id-2`).
- The body binding has `PublicName: "id-2"` and `WireName: "id"`.
- Direct manifest generation, before any generator pre-pass has mutated `IdentName`, emits the same public/body-wire split.

## Why This Works

The collision is a public-interface problem, not a wire-contract problem. Agents need unique input names, but the HTTP request still needs the original API key. `IdentName` and manifest `wire_name` already encode that split, so the fix follows the existing generator collision pattern instead of inventing a special body-only rule.

Doing the manifest dedupe inside the manifest writer also protects standalone manifest generation paths that do not run the full generator pre-pass first.

## Prevention

- Do not remove request-body fields in the parser just because their normalized public name collides with path or query params.
- Treat path params as public MCP inputs even when they are positional Cobra args rather than flags; they still reserve names in agent-facing schemas.
- For any new manifest or MCP surface, test the direct writer path and the generated-code path when collision handling depends on generator pre-passes.
- Preserve `Name` for wire contracts and use `IdentName`, public-name suffixing, or manifest `wire_name` for public disambiguation.

## Related Issues

- GitHub issue: https://github.com/mvanhorn/cli-printing-press/issues/1986
- Related pattern: `docs/solutions/design-patterns/identifier-collision-uniquification-pattern-2026-05-08.md`
- Related MCP surface bug: `docs/solutions/logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md`
