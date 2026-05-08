---
title: "OpenAPI parser silently ignored x-mcp extension"
date: 2026-05-08
category: logic-errors
module: internal/openapi
problem_type: logic_error
component: tooling
symptoms:
  - "x-mcp set in an OpenAPI spec is silently ignored at parse time"
  - "Generator's >50-tool warning recommends a recipe (transport/orchestration/endpoint_tools) that has no effect on OpenAPI specs"
  - "Pre-Generation MCP Enrichment skill section is unimplementable for OpenAPI inputs"
  - "docs/SPEC-EXTENSIONS.md had no x-mcp entry because the parser did not support it"
  - "Large OpenAPI specs (GitHub 819, AWS, Slack 200+) ship endpoint-mirror surfaces with bad MCP scorecard dims"
root_cause: incomplete_setup
resolution_type: code_fix
severity: high
related_components:
  - documentation
tags:
  - openapi-parser
  - mcp-config
  - x-extensions
  - parser-parity
  - spec-extensions
---

# OpenAPI parser silently ignored x-mcp extension

## Problem

The Printing Press skill documents a "Pre-Generation MCP Enrichment" recipe for OpenAPI specs that exceed the 50-tool threshold (the Cloudflare pattern: `transport: [stdio, http]` + `orchestration: code` + `endpoint_tools: hidden`). At runtime the generator emits a warning telling the operator to enrich the spec's `mcp:` block before regenerating. For internal YAML specs this worked; for OpenAPI specs it was a no-op. The parser at `internal/openapi/parser.go` had no code path for the `x-mcp` extension — `grep "x-mcp" internal/openapi/*.go` returned zero matches — so the operator's edits were silently discarded and the generated CLI shipped with the default endpoint-mirror surface and a poor MCP scorecard.

The recipe pointed operators at a switch that wasn't wired up. The warning was correct; the lever it told them to pull didn't exist for the spec format that needs it most — the large public APIs almost all ship OpenAPI (auto memory [claude]: `feedback_pre_generation_mcp_enrichment.md` documented the recipe whose unimplementability for OpenAPI was this bug).

## Symptoms

- Operator follows `skills/printing-press/SKILL.md` Phase 2 enrichment, adds `x-mcp:` to an OpenAPI spec, runs generate.
- Generated CLI still ships endpoint-mirror tools; the >50-tool warning persists; scorecard MCP dims regress.
- No parser error, no warning, no diff — the extension is read by `kin-openapi` into the untyped extension map and then never consulted.
- The polish skill cannot recover the situation post-generation: MCP transport / orchestration / tool-design are spec-level dims, not generator-code fixes (auto memory [claude]: `feedback_polish_mcp_misclassify.md`).

## What Didn't Work

- **Auto-enrich at >50 tools.** Silently flipping `transport`, `orchestration`, and `endpoint_tools` based on a count would defeat the no-reflexive-defaults principle: surfaces compete for the same scarce agent-tool budget, and the right answer per API depends on judgment the generator doesn't have.
- **Drop the recipe for OpenAPI in the skill.** That would remove the recipe exactly where it matters — the large public APIs almost all ship OpenAPI.
- **Convert OpenAPI to internal YAML upstream.** Lossy and recreates the translation burden the OpenAPI parser exists to eliminate.

## Solution

Wire `x-mcp` into the OpenAPI parser using the same shape already in use for `x-tier-routing`:

1. Add `extensionMCP = "x-mcp"` to the named-extension constants block.
2. Look the extension up via `lookupOpenAPIExtension` (root, then `info`, root wins).
3. JSON-marshal the untyped `any` tree returned by `kin-openapi`, then JSON-unmarshal into the typed `MCPConfig` struct. `MCPConfig` and its nested types (`Intent`, `IntentParam`, `IntentStep`) already carry matching `yaml:` and `json:` tags, so the roundtrip is clean.
4. `validateMCP` is already called from `APISpec.Validate()`, so malformed input (e.g., an unknown transport) fails at parse time with a clear error rather than producing a silently broken CLI.

During review, the new `parseMCPExtension` and the existing `parseTierRoutingExtension` collapsed into a generic helper since they differed only in type parameter and key:

```go
func parseTypedExtension[T any](doc *openapi3.T, key string) (T, error) {
    var zero T
    raw, ok := lookupOpenAPIExtension(doc, key)
    if !ok {
        return zero, nil
    }
    data, err := json.Marshal(raw)
    if err != nil {
        return zero, fmt.Errorf("marshaling %s: %w", key, err)
    }
    var cfg T
    if err := json.Unmarshal(data, &cfg); err != nil {
        return zero, fmt.Errorf("parsing %s: %w", key, err)
    }
    return cfg, nil
}
```

Coverage: 7 unit tests (root, info, absence, root-beats-info precedence with mutually-exclusive transports + negative assertion, unknown-transport rejection, addr+threshold roundtrip, Intents nested-struct roundtrip), plus a `generate-mcp-api` golden case locking the end-to-end output (`.printing-press.json`, `internal/mcp/tools.go`, `internal/mcp/code_orch.go`, `cmd/<api>-pp-mcp/main.go`). `docs/SPEC-EXTENSIONS.md` gained an `x-mcp` section per the AGENTS.md pointer-rot rule.

## Why This Works

`MCPConfig`'s pre-existing `json:` tags mean the OpenAPI parser doesn't need a parallel decoder; it borrows the same struct definition the YAML parser already uses. JSON is the lowest-common-denominator bridge from `kin-openapi`'s `map[string]any` extension tree to a typed Go struct, and it composes through arbitrary nesting without per-field plumbing. Validation is centralized in `validateMCP` and runs from `APISpec.Validate()`, so the failure mode for a typo'd `x-mcp` is a parse error, not a confusingly-shipped binary.

## Prevention

- **Parity-check both spec parsers when shipping a documented capability.** `internal/spec/spec.go` and `internal/openapi/parser.go` are siblings; a recipe that targets the `mcp:` block must work in both, and the change that adds the recipe should touch both parsers (or explicitly call out the gap). PR #522 added the skill, the warning, and the YAML side — the OpenAPI side fell off the change.
- **Reach for `parseTypedExtension[T]` for object-valued OpenAPI extensions.** When an extension's value is a YAML/JSON object that maps to a Go struct (with `json:` tags), the JSON-roundtrip generic is the right tool. Don't reimplement deserialization per extension.
- **Use `lookupOpenAPIExtension` + direct type assertion for scalars.** Strings, bools, and string lists (`x-api-name`, `x-website`, etc.) don't need the roundtrip.
- **A new deterministic generator behavior driven by an extension warrants a golden case.** Per AGENTS.md, passing `scripts/golden.sh verify` on existing cases does not prove coverage for new MCP, auth, pagination, manifest, or naming behavior. Add the fixture in the same change.
- **Precedence assertions need mutually-exclusive values plus a negative assertion.** A test that only asserts the dominant value passes even if the parser is silently merging fields instead of replacing.
- **When a skill instruction or runtime warning points operators at a spec lever, verify the lever is wired up end-to-end before shipping the instruction.** An unimplementable instruction is worse than a missing one — the operator wastes time, the warning loses credibility, and the bug surfaces as a "the docs are wrong" report rather than a "the parser has a gap" report.

## Related Issues

- Issue #696 — this bug
- PR #702 — the fix
- PR #522 — introduced the recipe and the YAML-side parser; missed the OpenAPI side
- `skills/printing-press/SKILL.md:1734-1805` — pre-generation enrichment recipe
- `internal/generator/mcp_warning.go` — the >50-tool warning that points at the recipe
- `docs/SPEC-EXTENSIONS.md` — canonical reference for `x-*` extensions, now including `x-mcp`
- `docs/solutions/logic-errors/inline-authorization-param-bearer-inference-2026-05-05.md` — sibling precedent for parser-extension gaps in `internal/openapi/parser.go`
