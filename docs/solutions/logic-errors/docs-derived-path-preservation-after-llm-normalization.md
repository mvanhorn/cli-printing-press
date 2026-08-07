---
title: "Docs-derived paths must preserve documented request paths after LLM normalization"
date: 2026-05-25
category: logic-errors
module: internal/docspec
problem_type: logic_error
component: tooling
symptoms:
  - "Generated docs-source commands hit request paths that differed from the vendor documentation"
  - "Plural path segments could be singularized, such as get_pending_orders becoming get_pending_order"
  - "Slash-separated documented segments could be collapsed into underscores, such as funding_rate/batch becoming funding_rate_batch"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - docspec
  - docs-derived-specs
  - path-preservation
  - llm-normalization
  - generator
---

# Docs-derived paths must preserve documented request paths after LLM normalization

## Problem

`generate --docs` can route documentation through an LLM doc-to-spec pass before the generator emits a printed CLI. Issue #1987 showed that this pass could normalize path segments while deriving YAML: Bitunix Futures docs listed `/api/v1/futures/tpsl/get_pending_orders` and `/api/v1/futures/market/funding_rate/batch`, but generated commands used `/get_pending_order` and `/funding_rate_batch`.

That is a request-path contract bug. The endpoint name, resource name, and command path can be derived for ergonomics, but the HTTP request path must come from the documentation verbatim when the docs contain an explicit method/path pair.

## Symptoms

- Generated commands received real API error envelopes because they called non-documented routes.
- The failures looked like path-segment normalization rather than random hallucination: plural-to-singular and slash-to-underscore drift.
- Local manual correction of the request paths made the live endpoints succeed.

## What Didn't Work

- Relying on prompt wording alone is not enough. It reduces the odds of normalization, but it does not make the LLM output authoritative.
- Repairing `Endpoint.Path` after parsing without re-syncing path params is incomplete. `spec.ParseBytes` has already enriched `{placeholder}` params, so a later path rewrite can leave stale positional/path params behind.

## Solution

Treat the regex-extracted docs method/path pairs as the authority for request paths, then use the LLM spec as the authority for the richer surrounding metadata. After `GenerateFromDocsLLM` parses the YAML, compare each parsed endpoint path against the documented path set:

- Leave exact method/path matches unchanged.
- Build a conservative canonical key that tolerates the observed LLM drift: separator differences, simple trailing plural `s`, and placeholder-name normalization.
- Rewrite only when the current method plus canonical path maps to exactly one documented path.
- Prune stale positional/path params on rewritten endpoints, then rerun `EnrichPathParams` and `Validate`.

The regression test should exercise the full `GenerateFromDocsLLM` path with a fake LLM binary on `PATH`, not just the helper, so the wiring is protected.

## Why This Works

The documented request path is the source of truth for the wire contract. Names derived for CLI ergonomics may safely normalize, but the path sent to the API must not. Matching through a uniqueness gate avoids guessing when two documented paths collapse to the same canonical form, while re-enrichment keeps the repaired path and generated command schema aligned.

## Prevention

- When a generator step combines an authoritative source with an LLM or heuristic derivation, name which field remains authoritative before emitting the generated artifact.
- Add negative tests for lookalike paths and HTTP-method partitioning so fuzzy repair cannot rewrite a different endpoint.
- If a fix mutates endpoint paths after parsing, re-run path-param enrichment or prove the placeholder set is unchanged.

## Related Issues

- Issue #1987
- `docs/solutions/logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md`
- `docs/solutions/logic-errors/store-columns-sourced-from-request-params-instead-of-response-2026-05-08.md`
- `docs/solutions/logic-errors/non-catalog-category-must-enter-generate-before-emission-2026-05-22.md`
