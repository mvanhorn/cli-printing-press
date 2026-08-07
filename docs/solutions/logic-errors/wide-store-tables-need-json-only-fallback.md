---
title: "Wide generated store tables need JSON-only fallback"
date: 2026-05-24
category: logic-errors
module: internal/generator/schema_builder
problem_type: logic_error
component: tooling
symptoms:
  - "Generated CLI passes generation and quality gates, then first store open fails with SQLite `too many columns` during migration"
  - "Wide enterprise config resources flatten thousands of response fields into one CREATE TABLE statement"
  - "The failure is invisible until a store-backed command runs against a fresh database"
root_cause: logic_error
resolution_type: code_fix
severity: high
related_components:
  - internal/generator/templates/store.go.tmpl
  - internal/generator/generator.go
tags:
  - schema-builder
  - sqlite-columns
  - wide-schema
  - json-only-fallback
  - generated-store
---

# Wide generated store tables need JSON-only fallback

## Problem

The typed-table emitter used one SQLite column per scalar response field for high-gravity resources. On very wide config singleton schemas, that generated a `CREATE TABLE` wider than SQLite's default column limit, so the printed CLI generated cleanly but failed the first time the local store migrated.

## Symptoms

- A generated CLI can pass build, vet, doctor, and verify gates because no database migration runs during generation.
- First `sync --full --dry-run` or any store-backed command fails while opening the local database.
- Direct inspection of generated `internal/store/store.go` shows a single resource table with thousands of columns.

## What Didn't Work

- **Hand-removing the pathological resource from one printed CLI.** That unblocks the one API but leaves the generator ready to fail on the next wide config singleton.
- **Per-column trimming or allowlists.** This creates API-specific policy in a shared generator path and risks silently dropping useful typed columns from ordinary resources.
- **Only checking `BuildSchema` before dependent columns.** Dependent resources can sit exactly at the safe width before `schemaWithDependentParents` appends `parent_id`; the boundary check must account for generator-added columns too.

## Solution

Keep typed columns for ordinary resources, but mark pathological resources with `JSONOnlyFallback` once their table would exceed the safe width. The fallback keeps the per-resource table and generated `Upsert<Resource>` path, but resets the table to the base `id`, `data`, and `synced_at` columns and drops typed indexes and FTS triggers.

The threshold is intentionally below SQLite's default `SQLITE_MAX_COLUMN=2000` so generated columns and future schema drift have headroom.

```go
if len(table.Columns) > maxStoreDomainTableColumns {
    table.JSONOnlyFallback = true
    table.OriginalColumnCount = len(table.Columns)
    table.Columns = append([]ColumnDef(nil), baseTableColumns...)
    table.Indexes = nil
}
```

The generator also re-checks the width when adding dependent-resource `parent_id`. If that extra column would cross the threshold, it applies the same JSON-only fallback instead of emitting an over-wide dependent table.

## Why This Works

The local store always writes the full response payload to the `data` JSON column. Typed columns are a query and indexing optimization, not the only copy of the data. Falling back to JSON-only preserves sync, local reads, generic SQL access through `data`, and per-resource dispatch while avoiding a migration that SQLite cannot execute.

The fallback is visible: generation prints a `store-fallback` warning with the resource name and original column count, so operators can distinguish intentional degradation from missing schema inference.

## Prevention

- Generator tests for SQLite DDL bugs must prove the risky SQL is actually emitted. Assert the generated `CREATE TABLE` body, not only `BuildSchema` internals.
- Boundary tests should include generator-added columns, especially dependent-resource `parent_id`, because post-schema augmentation can cross a width cap.
- Keep response-schema sourcing separate from fallback policy. Response fields are still the authority for typed columns; JSON-only fallback is a narrow SQLite limit exception.
- Run `scripts/golden.sh verify` for template predicate changes even when existing goldens do not change.

## Related Issues

- [#1753](https://github.com/mvanhorn/cli-printing-press/issues/1753) — tracked the wide config singleton failure.
- `docs/solutions/logic-errors/store-columns-sourced-from-request-params-instead-of-response-2026-05-08.md` — adjacent schema-builder invariant: response schema remains the source for typed columns.
- `docs/solutions/logic-errors/safesqlname-allowlist-misses-strict-reserved-keywords-2026-05-12.md` — adjacent SQLite DDL failure in generated store tables.
