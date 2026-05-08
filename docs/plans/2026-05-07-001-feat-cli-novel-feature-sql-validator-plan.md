---
title: "feat(cli): Novel-feature SQL ↔ schema validator in dogfood"
type: feat
status: proposed
date: 2026-05-07
related:
  - cli-printing-press#689
---

# feat(cli): Novel-feature SQL ↔ schema validator in dogfood

## Overview

Novel-feature commands (`portfolio winrate`, `portfolio attribution`, `markets correlate`, etc.) execute SQL against the printed CLI's local SQLite store. Today there is no machine check that the JSON paths those queries reference (`$.settled_at`, `$.cost`, `$.revenue`) actually exist on the synced response shape. The cost is the failure mode reported in #689 Bug 4: a command that builds, registers, passes scorecard, and returns `[]` against real data because every `WHERE`-clause path is null in the row.

Add a `sql_schema_check` to the dogfood report: extract JSON paths from each novel-feature command's SQL, cross-reference them against the response schema for the resource the command targets, and surface mismatches as suspicious findings. Same shape as `reimplementation_check` — structural detection, regex/AST-based, no live API calls — so it runs in `printing-press dogfood` alongside the other gates.

This plan covers the static-only first pass. Runtime validation against actually-synced data is a follow-up that can layer on once the static check is in place.

---

## Problem Frame

Issue #689 ran the printed `kalshi-pp-cli` against a real account and discovered:

- `portfolio winrate` filters on `WHERE type='settlement' OR settled_at IS NOT NULL` but Kalshi's settlement payload uses `settled_time`, not `settled_at`.
- `portfolio attribution` projects `$.cost` but Kalshi splits cost into `$.yes_total_cost_dollars` / `$.no_total_cost_dollars` (also a unit mismatch — dollars-as-strings vs. cents).
- `portfolio exposure` references positions schema fields that don't exist on the synced data (compounded by Bug 3's multi-array envelope drop).

In every case, the SQL was authored against a generic mental model of what a Kalshi-shaped API "ought to" return, not the actual response schema captured in the absorb manifest's example payloads. Three independent commands shipped with three independent JSON-path errors. Scorecard, golden, and dogfood all passed.

The compounding effect is the real failure: Bug 1 (dropped rows) masked Bug 2 (missing endpoints) which masked Bug 4 (wrong field names). A user who fixes Bug 1 still doesn't see real numbers. The validator's job is to break that compounding by failing loudly at generation time when *any* novel-feature SQL references a path the response schema doesn't promise.

---

## Requirements Trace

- R1. The check inspects every novel-feature command listed in `research.json` (or the absorb manifest's transcendence table) that ships with SQL — that is, every handler whose file contains a SQLite/store query.
- R2. The check extracts the JSON paths used in `WHERE`, `ORDER BY`, `SELECT json_extract(...)`, and projection expressions. Paths are the dot-suffixes of `json_extract(data, '$.<path>')` and the equivalent SQLite JSON1 forms.
- R3. The check identifies the resource each query targets — either via a comment annotation (`// pp:novel-sql-target <resource>`) on the handler file, or by inspecting the SQL's `WHERE resource_type = '<x>'` clause as a fallback.
- R4. The check cross-references each extracted path against the response schema's flattened property set for that resource. Paths whose head segment is not present on the item schema are flagged.
- R5. Mismatches are reported as `SQLSchemaFinding{Command, File, Path, Resource, Reason}` and surfaced through `DogfoodReport.SQLSchemaCheck.Suspicious`. The check is `WARN` severity in the verdict matrix until the false-positive rate is known, then promoted to `FAIL`.
- R6. The check skips gracefully when (a) the command has no SQL, (b) no resource target is declared or inferable, (c) the spec lacks a response schema for the targeted resource. `Skipped` and the reason are captured in the report so retro/scorer analytics can distinguish "passed" from "did not run."
- R7. A unit test in `internal/pipeline/sql_schema_check_test.go` exercises: matching path on item schema (pass), mismatched path (flagged), envelope-only path (flagged), missing schema (skipped), missing resource target (skipped), and the Kalshi `$.settled_at` vs. `settled_time` regression specifically.
- R8. A scorecard adjustment surfaces the finding count in the existing novel-features dimension. Skill prose in `printing-press/SKILL.md` directs the novel-feature subagent to consume the absorb manifest's example payloads when authoring SQL — the validator is the safety net, not the primary teacher.

---

## Scope Boundaries

- Static check only. No live SQL execution against synced data, no SQLite engine in dogfood. Path extraction is regex/AST over Go source.
- No SQL parsing beyond JSON-path extraction. We do not validate column existence on synthetic tables (`fills`, `settlements`), only paths threaded through `json_extract` against the `data` blob.
- No proof that the path is *reachable* — a path can exist on the schema but be sparsely populated. That's a runtime concern.
- No fixing the SQL. The validator names the offense; the agent fixes it.
- The validator runs at machine generation time (printing-press's own dogfood/verify), not inside the printed CLI's runtime test suite. The printed CLI's tests are out of scope.

### Deferred to Follow-Up Work

- Runtime validation in live dogfood: when a sync against a real account has produced rows, run each novel-feature SQL and assert >0 rows, then assert each projected column is non-null on >N% of rows. Unblocks #701 (Bug 2: multi-endpoint per resource) by giving us a way to verify "this command's SQL targets resource X" mechanically.
- AST-based path extraction. The first pass is regex over Go source, matching the existing `reimplementation_check` style. AST is a second-pass upgrade if false-positive pressure grows.
- Unit-mismatch detection (cents vs. dollars, ISO timestamp vs. epoch). The Kalshi case had `revenue` in cents and `cost` in dollars; the SQL divided by 100 assuming uniform units. Surfacing this requires schema annotations the absorb step doesn't currently emit.

---

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/dogfood.go` — owns `DogfoodReport`, the verdict matrix, and the Skipped/Issue plumbing the new check needs to slot into.
- `internal/pipeline/reimplementation_check.go` — the closest existing pattern. Walks novel-feature handler files, extracts structural signals (client-call, store-call, annotation), reports findings. Same shape; new file `internal/pipeline/sql_schema_check.go` mirrors its layout.
- `internal/pipeline/dogfood.go::checkReimplementation` call site — where the new check hooks into the report build.
- `internal/spec/spec.go::Endpoint.Response` — the response schema we cross-reference. Already populated by the OpenAPI parser via `mapResponse`.
- `internal/openapi/parser.go::resolveIDFieldFromResponseSchema` and `unwrapItemSchema` — the same item-schema descent logic the validator needs to find the property set to validate against.
- `skills/printing-press/references/novel-features-subagent.md` — owns the prose instructions for authoring novel features. The skill must point the subagent at the absorb manifest's example payloads when authoring SQL; the validator is the enforcement.
- `skills/printing-press/references/absorb-scoring.md::Reimplementation` — frames the policy template the new check inherits: "synthesizes API responses locally" → "Cut or rewrite." SQL referencing nonexistent fields synthesizes a query against a phantom shape; same severity class.

### How Resource Targeting Resolves

Three signal sources, in priority order:

1. **Annotation.** Handler file contains `// pp:novel-sql-target <resource>` on the function or file level. Explicit and unambiguous; preferred when the SQL targets a non-obvious resource (joins across multiple resources, derived tables).
2. **WHERE clause.** SQL contains `WHERE resource_type = '<x>'`. Inferred reliably from the existing pattern in `transcendence.go`-style handlers. Single-resource queries.
3. **Skip.** No annotation, no inferable WHERE, multi-resource join with no annotation. Report as `Skipped` with reason; do not flag false positives.

The annotation lives in the printed CLI's handler files (agent-authored), so emitting it is a skill change — `novel-features-subagent.md` is updated to require the annotation when the absorb manifest names a target resource.

### Why Static Beats Runtime for the First Pass

Runtime validation needs synced data, which needs a real account, which needs credentials, which the dogfood gate doesn't have access to. Static validation runs in CI on every commit, against every spec we have a fixture for, with no external dependencies. It catches the Kalshi `settled_at` vs. `settled_time` class of bug — which is the dominant class — without any live infrastructure.

Runtime validation matters for the residual class: paths that exist on the schema but are populated under conditions the static check can't see (`type = 'settlement'` only when something was settled). That's the live-dogfood follow-up.

---

## Implementation Plan

### Phase 1: Static path extraction and check skeleton

- Add `internal/pipeline/sql_schema_check.go`. Define `SQLSchemaCheckResult` and `SQLSchemaFinding`. Mirror the shape of `ReimplementationCheckResult`.
- Extract JSON paths from each novel-feature handler file. Regexes target `json_extract(<col>, '$.<path>')`, `json(<col>)->>'$.<path>'`, and the SQLite `->` / `->>` operator forms. Keep the regex set narrow for the first pass; expand only on observed misses.
- Identify the resource target via `// pp:novel-sql-target <resource>` annotation; fall back to `resource_type = '<x>'` regex on the SQL string; skip otherwise.
- Hook into `checkReimplementation`'s call site in `internal/pipeline/dogfood.go`.

### Phase 2: Schema cross-reference

- Reuse `unwrapItemSchema` (after Bug 1's named-array envelope fix) to get the item schema for the targeted resource's primary list endpoint.
- Build a flattened property set: for each property on the item schema, recurse into nested objects up to a small depth bound (start with 3) and collect dotted paths. Keep the bound conservative; Kalshi's settlements payload is shallow.
- Compare each extracted SQL path's head segment (and full path when nested) against the property set. Flag mismatches. Tolerate paths whose head matches a property typed as `additionalProperties: true` or `oneOf` (open shape — can't validate).

### Phase 3: Reporting and verdict integration

- Surface findings via `DogfoodReport.SQLSchemaCheck.Suspicious` in the JSON report.
- Add a `WARN` row to the verdict matrix in `dogfood.go` mirroring `reimplementation_check`'s row. Promote to `FAIL` after one or two CLIs land cleanly.
- Surface a one-line summary in the existing dogfood text output.

### Phase 4: Tests

- Unit tests in `internal/pipeline/sql_schema_check_test.go`. Fixture cases:
  - Item schema has `event_ticker`; SQL filters on `$.event_ticker` → pass.
  - Item schema has `settled_time`; SQL filters on `$.settled_at` → flagged with reason "path not in item schema; nearest match: settled_time".
  - SQL filters on `$.cursor` (envelope-level scalar) → flagged with reason "path is on response wrapper, not item schema."
  - No annotation, no inferable WHERE → skipped with reason "resource target not declared."
  - Item schema is `additionalProperties: true` → skipped with reason "item schema is open-ended."
  - Kalshi-shaped fixture: `WHERE type='settlement' OR settled_at IS NOT NULL` → flagged on `$.settled_at`.

### Phase 5: Skill prose

- Update `skills/printing-press/references/novel-features-subagent.md`: require `// pp:novel-sql-target <resource>` on every SQL-bearing novel-feature handler; instruct the subagent to consult the absorb manifest's example payloads when authoring SQL; surface the `sql_schema_check` failure as a Phase 4 blocker just like reimplementation findings.
- Update `skills/printing-press/references/absorb-scoring.md::Reimplementation` row to reference SQL-schema fidelity alongside response-builder synthesis.

---

## Verification

- `go test ./internal/pipeline/...` — unit tests pass.
- `scripts/golden.sh verify` — generator output unchanged (this is a new check, not a generator change).
- Manual: regenerate `kalshi-pp-cli` from the absorbed spec; run dogfood; assert `sql_schema_check.suspicious` lists `portfolio winrate ($.settled_at)`, `portfolio attribution ($.cost)`, and any other paths surfaced by the check. The validator does not fix the SQL — its job is to surface, not heal.
- Manual: regenerate one or two existing public-library CLIs that have novel-feature SQL (steam-run3, recipe-goat, movie-goat). The check should run against them; any findings represent real bugs that were previously invisible. Triage results inform the WARN-vs-FAIL promotion decision.

---

## Risks

- **False positives from open-ended schemas.** APIs that return `additionalProperties: true` or untyped maps will trigger the skip path and reduce signal. Acceptable for v1 — better to skip than flag noise. If the skip rate climbs above ~30% across the public library, revisit the property-set construction.
- **Schema drift from API evolution.** A spec snapshot can lag the live API; the validator catches "SQL drifted from spec" but not "spec drifted from API." Runtime validation in the follow-up plan closes that gap.
- **Annotation discipline.** If the novel-features subagent forgets the `pp:novel-sql-target` annotation, the check skips silently. Mitigation: dogfood surfaces `Skipped` reasons in the report; retro reviewers can scan for "resource target not declared" as a structural smell.
- **Regex extraction has silent-miss bypass classes.** First-pass path extraction matches literal `'$.<path>'` arguments only. SQL using string concatenation (`json_extract(data, '$.' || :col)`), hex literals (`x'...'`), or parameterized paths (`json_extract(data, ?)`) defeats the regex and the validator silently passes. False-confidence risk grows after WARN-to-FAIL promotion. Mitigation: add a "computed-path detector" that flags any non-literal argument to `json_extract` for explicit annotation or acceptance.
- **Promotion gate evidence.** Promoting from WARN to FAIL needs observed false-positive rates across the full public library, not "one or two clean CLIs." Define what "low enough" means (e.g., FP rate &lt;5% across N&gt;=10 CLIs) before the promotion. The composed-header-auth scorecard in `docs/solutions/logic-errors/scorer-dogfood-composed-header-auth-and-example-continuations-2026-05-05.md` is the closest precedent.
