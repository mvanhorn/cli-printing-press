---
title: "Auth env-var rich model: kind, required, sensitive, with legacy back-derivation"
date: 2026-05-05
category: design-patterns
module: cli-printing-press-generator
problem_type: design_pattern
component: auth
severity: medium
applies_when:
  - "One legacy list is driving multiple auth surfaces with different filtering rules"
  - "Specs need to distinguish user-supplied credentials from auth-flow inputs or harvested browser/session values"
  - "Generated docs, doctor output, MCP context, manifests, or helpers rederive auth env-var semantics independently"
related_components:
  - generator
  - openapi-parser
  - auth-doctor
tags:
  - auth
  - env-vars
  - generator
  - model-design
---

# Auth env-var rich model: kind, required, sensitive, with legacy back-derivation

## Context

The original `Auth.EnvVars []string` model could name credentials, but it could not express kind, required-vs-optional, sensitive-vs-public-config, or OR semantics. Downstream surfaces tried to recover those semantics locally: SKILL frontmatter, doctor checks, MCP agent context, helpers, manifests, and host auth doctor each grew their own classification heuristics. That drift shipped as spurious names, harvested cookies advertised as user-required credentials, OAuth client credentials treated as per-call inputs, and human-prose templates choosing `[0]` as if ordering were semantic.

## Guidance

Use additive struct widening when a legacy scalar/list is already consumed broadly. Add `AuthEnvVar` beside legacy `EnvVars []string`, then normalize lazily so pre-widening specs still flow through the rich path. The rich model owns precedence: if `EnvVarSpecs` exists, back-derive `EnvVars` from it; if only `EnvVars` exists, derive inferred `per_call`, `required`, `sensitive` specs.

Choose one deterministic selector for single-name human prose. `CanonicalEnvVar()` prefers the first required `per_call` entry, then falls back to the first normalized entry, so templates stop encoding accidental `[0]` ordering ambiguity. Use the 3-value kind enum (`per_call`, `auth_flow_input`, `harvested`) plus the orthogonal `Sensitive` flag to drive downstream filtering uniformly.

Keep rare relationship semantics cheap until they earn a real field. AND/OR groups are not first-class because only 4 of 46 published CLIs need them; encode AND as each member `Required: true`, and OR as each alternative `Required: false` with description text naming the other option. Legacy aliases are not first-class either: the library is pre-official-launch, clean breaks are acceptable for renames, and alias support should be promoted only if rename frequency increases.

Normalize before merging generated artifacts. `NormalizeEnvVarSpecs` must run at both per-global and per-tier levels before climanifest merge so tier overrides preserve their semantics. Mixed-version safety needs the same discipline: the `classify.go` double-report bug was preventable only by the dedup guard. Any future field that gets back-derived for legacy compatibility should share model precedence rules rather than letting each surface rederive them from scratch.

## Code Patterns

- `internal/spec/spec.go:467` defines `AuthEnvVar`; `internal/spec/spec.go:484` defines `CanonicalEnvVar()` and is the selector template for human-prose surfaces.
- `internal/openapi/parser.go:583` shows `applyAuthVarsRichOverride`, the conservative parser gate: malformed rich overrides warn and fall back to generated defaults.
- `internal/authdoctor/classify.go:100` shows the mixed-version safety pattern: prefer rich manifest specs, warn on disagreement, and fall back to legacy env vars.
- `internal/pipeline/climanifest.go:336` shows pre-merge normalization and name dedup across global and tier auth blocks.

## Related

- Plan: `docs/plans/2026-05-05-007-feat-auth-envvar-model-widening-plan.md`
- Umbrella issue: [#632](https://github.com/mvanhorn/cli-printing-press/issues/632)
- Related plans: `docs/plans/2026-04-19-004-feat-auth-doctor-plan.md`, `docs/plans/2026-03-31-004-fix-auth-error-handling-plan.md`, `docs/plans/2026-03-31-001-fix-auth-envvar-hint-relevance-plan.md`, `docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md`
