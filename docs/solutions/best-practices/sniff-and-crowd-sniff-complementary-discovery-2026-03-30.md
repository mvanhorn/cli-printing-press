---
title: "Sniff and Crowd-Sniff: Complementary API Discovery for CLI Generation"
date: 2026-03-30
category: best-practices
module: API discovery pipeline
problem_type: best_practice
component: tooling
severity: medium
applies_when:
  - Generating a CLI for an API that lacks a published OpenAPI spec
  - Deciding whether to use sniff, crowd-sniff, or both during Phase 1
  - Evaluating which discovery method produces better CLI coverage
tags:
  - api-discovery
  - sniff
  - crowd-sniff
  - pipeline-strategy
  - spec-generation
---

# Sniff and Crowd-Sniff: Complementary API Discovery for CLI Generation

## Context

Printing-press generates CLIs from API specs. Many public APIs don't publish specs. Two discovery commands fill this gap — `sniff` (Phase 1.7) and `crowd-sniff` (Phase 1.8) — but they discover fundamentally different things and work best together.

Understanding when to use each, and why they're complementary rather than competing, is essential for producing the best CLI coverage.

## Guidance

### What each discovers

**Sniff** browses a live web app headlessly, captures HTTP traffic, and reverse-engineers a spec from observed requests/responses. It sees what the web application does.

**Crowd-sniff** searches npm SDKs and GitHub code to find what developers have already mapped. It sees what developers need.

These are different sets:

| Signal | Sniff | Crowd-sniff |
|--------|-------|-------------|
| Source | One browsing session | Thousands of developers |
| What it finds | What the web app does | What developers need |
| Auth patterns | Cookie/session (often unusable for CLI) | API keys, bearer tokens (CLI-ready) |
| Coverage | Whatever pages you visit in 60-90s | Whatever SDKs implement |
| Popularity signal | No | Yes (frequency across GitHub repos) |
| Parameter types | Inferred from response bodies | Declared in SDK source code |
| Works without browser | No | Yes |
| Response body examples | Yes (from live traffic) | No (only endpoint paths and methods) |

### When to use each

**Use sniff alone** when:
- The API has no SDKs on npm (rare for popular APIs)
- The web app is the primary interface and you want to capture exactly what it does
- You need response body examples for richer spec generation

**Use crowd-sniff alone** when:
- Browser automation isn't available or is unreliable
- The API requires login (sniff skips auth-required sites)
- Speed matters (crowd-sniff runs without a browser, typically 2-4 minutes)
- You want popularity-weighted endpoint coverage

**Use both** when:
- Building a GOAT CLI (this is the default recommendation)
- The API has both a web app and published SDKs
- You want maximum coverage: web-app endpoints AND developer-used endpoints

### How they combine in the pipeline

```
Phase 1.7: Sniff Gate
  → Produces sniff-spec.yaml (endpoints from live traffic)

Phase 1.8: Crowd Sniff Gate
  → Produces crowd-spec.yaml (endpoints from npm + GitHub)

Phase 2: Generate
  → printing-press generate --spec original.yaml --spec sniff-spec.yaml --spec crowd-spec.yaml
```

The `mergeSpecs()` function flattens all endpoints from all specs into one CLI. Collision handling prefixes duplicate resource names with the source spec name. The result: a CLI that covers what the API documents, what the web app does, AND what developers use.

### Why crowd-sniff's popularity signal matters for CLI quality

A spec treats all endpoints equally. Crowd-sniff doesn't — an endpoint found in 200 GitHub repos is more important than one found in 3. This frequency data is carried as `source_count` in the spec's `Meta` field, making it available to the skill during Phase 3 (Build The GOAT) for prioritization decisions:

- High-frequency endpoints deserve better descriptions, examples, and polish
- Low-frequency endpoints might be omitted from the quick-start guide
- Zero-frequency endpoints (only in the spec, never seen in real code) might be candidates for exclusion

### The core insight

For any API popular enough to want a CLI for, someone has already mapped it in code. An npm SDK has every endpoint the vendor tested and ships. GitHub code from hundreds of repos shows real-world usage patterns. Crowd-sniff turns this existing community knowledge into a structured spec — no browsing, no traffic capture, no manual documentation.

Sniff captures the API's behavior. Crowd-sniff captures the community's intent. Together they produce the most complete picture available.

## Why This Matters

Without sniff or crowd-sniff, an API without a published spec is a dead end for CLI generation. With both, printing-press can generate a CLI for virtually any public REST API — the two discovery methods close the gap between "APIs that document themselves" and "APIs that don't."

The complementary nature also improves quality beyond coverage:
- Sniff provides response body examples that crowd-sniff can't (it only finds paths and methods)
- Crowd-sniff provides auth patterns that sniff can't (web apps use cookies; SDKs use API keys)
- Cross-source agreement (an endpoint found by both sniff and crowd-sniff) is a strong confidence signal

## When to Apply

- Every time you run the printing-press skill for an API without a published spec
- When evaluating whether a generated CLI has sufficient endpoint coverage
- When the skill asks whether to run sniff or crowd-sniff — the answer is usually "both"
- When adding new discovery methods to the pipeline (they should be complementary, not replacing)

## Examples

### API with published spec + SDK (e.g., Notion)

Crowd-sniff's `@notionhq/client` SDK produces endpoints that closely match the official spec. Sniff captures the web app's internal API calls (some of which use different paths or additional endpoints not in the public API). Using both reveals the gap between the public API and the internal API.

### API with no spec, no SDK (e.g., obscure SaaS)

Sniff is the primary discovery method — browse the web app and capture what it does. Crowd-sniff may find GitHub code snippets from users calling the API directly, but coverage will be thinner. The skill falls back to `--docs` generation if neither produces results.

### API with SDKs but no web app (e.g., infrastructure APIs)

Crowd-sniff is the primary method — the SDK maps the entire API surface. Sniff has nothing to browse. This is where crowd-sniff's value is most clear: it turns published SDK code into a CLI spec without any human intervention.

## Related

- `docs/solutions/best-practices/multi-source-api-discovery-design-2026-03-30.md` — technical design patterns used in crowd-sniff (testable HTTP clients, errgroup, path normalization, tarball security)
- `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md` — rate limiting for CLIs generated from sniffed specs
- `docs/brainstorms/2026-03-29-crowd-sniff-requirements.md` — origin requirements document
- `docs/plans/2026-03-29-003-feat-crowd-sniff-plan.md` — implementation plan
