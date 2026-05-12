---
title: "safeSQLName hand-rolled reserved-word allowlist missed SQLite strict-reserved keywords"
date: 2026-05-12
category: logic-errors
module: internal/generator/schema_builder
problem_type: logic_error
component: tooling
severity: high
symptoms:
  - "Generated CLI fails at first store open with `running migrations: migration failed: SQL logic error: near \"add\": syntax error (1)`"
  - "Every store-backed command (sync, search, analytics, transcendence) breaks because migrate() runs on every open"
  - "Cross-API blast radius observed across 11 CLIs in the public library carrying unquoted reserved-word table names (cancel, track, errors, status, options, services)"
  - "Bug surfaces only for SQLite strict-reserved keywords (add, to, from); non-strict reserved words shipped fragile-but-functional"
root_cause: logic_error
resolution_type: code_fix
tags:
  - sqlite
  - sql-identifier-emission
  - generator-templates
  - schema-builder
  - reserved-keywords
  - ddl-quoting
related_components:
  - internal/generator/templates/store.go.tmpl
---

## Problem

The generator's `safeSQLName` helper in `internal/generator/schema_builder.go` quoted SQL identifiers only when their lowercase form matched a hand-rolled allowlist of ~50 reserved words. The list omitted SQLite's strict-reserved keywords (`add`, `to`, `from`, and others) — words SQLite refuses to parse unquoted in `CREATE TABLE` context. Any printed CLI whose spec produced a sub-resource leaf snake-casing to one of those words shipped a `CREATE TABLE IF NOT EXISTS add (...)` statement that failed at parse time the first time the binary opened its store.

## Symptoms

- ShipStation printed CLI failed first run with `migration failed: SQL logic error: near "add": syntax error (1)` — `add` came from `/v2/batches/{id}/add`.
- Public-library scan against `~/printing-press/.publish-repo-*/library/*/*/internal/store/store.go` found 11 CLIs already shipping with unquoted reserved-word tables (`cancel`, `track`, `errors`, `status`, `options`, `services`). They happened to work because SQLite is permissive about *non*-strict-reserved keywords in `CREATE TABLE` context — but the same generator path is one keyword away from breaking on every future regen.
- The bug surfaces inside `migrate()`, so every store-backed command in the printed CLI breaks at once.

## What Didn't Work

- **Extending the allowlist with the observed missing keywords.** Adds 13 keywords today (`add`, `cancel`, `track`, etc.); doesn't prevent the next missing keyword from breaking another CLI. SQLite's keyword list grows across versions; a hand-maintained allowlist diverges from the dialect over time. Rejected.
- **Initial TDD integration test using `add` as a top-level resource.** The test added an `add` resource with one list endpoint and no `Response.Item`. Three independent code reviewers caught that the fixture didn't actually exercise the bug: `computeDataGravity` scored at 1 (one endpoint, zero response fields), `hasTypedTable` returned false, and the typed `CREATE TABLE` for `add` was never emitted into the migrations slice. The test passed against the unpatched generator. The lesson: any test that hinges on a typed-table emission must push the resource over the gravity threshold (response type with fields, or a sub-resource that auto-gains a parent_id column — and beware that sub-resources hit a separate FK-NOT-NULL upsert_batch_test bug).

## Solution

Replace the allowlist + bare-identifier check with unconditional double-quoting. SQLite accepts any identifier when quoted, and quoting is a no-op for ordinary names at runtime — `sqlite_master` strips the surrounding quotes when storing the name, so all parameterized lookups by name continue to match.

Before:

```go
var sqlReservedWords = map[string]bool{
    "check": true, "default": true, "from": true, "to": true,
    // ...about 50 entries, none of which include "add"
}

func safeSQLName(name string) string {
    if sqlReservedWords[strings.ToLower(name)] || !isBareSQLiteIdentifier(name) {
        return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
    }
    return name
}
```

After:

```go
// safeSQLName returns an identifier that is safe to use in SQLite DDL.
// Always double-quotes the name, escaping any embedded quote. Quoting is
// harmless for non-keyword identifiers and is the only way to safely emit
// SQLite strict-reserved keywords like "add", "to", and "from" in CREATE
// TABLE / CREATE INDEX context, where they otherwise fail at parse time.
// Maintaining a hand-rolled keyword allowlist diverges from SQLite over
// time; quoting unconditionally is the durable contract.
func safeSQLName(name string) string {
    return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
```

The change also dropped `sqlReservedWords` and `isBareSQLiteIdentifier` entirely. Companion fix in `internal/generator/templates/store.go.tmpl`: the upsert template's `ON CONFLICT(id)` was the only remaining unquoted identifier in the SQL block; routing `id` through `safeName` brings every identifier in the statement under the same quoting policy.

## Why This Works

- SQLite identifies tables by their stored name in `sqlite_master`. Quoted DDL stores the name without surrounding quotes; runtime queries like `tableExists` parameterize the name with bare Go strings, and the lookup still matches.
- `CREATE TABLE IF NOT EXISTS "add"` is unambiguously a table identifier, never a parse-time syntax error.
- Non-keyword identifiers (`leagues`, `id`, `data`) behave identically when quoted — SQLite treats `"leagues"` and `leagues` as the same column at lookup time.
- `IF NOT EXISTS` gates compare the stored unquoted name, so re-running migration against existing user databases is a clean no-op. No data migration needed when an existing CLI is regenerated under the new quoting.

## Prevention

- **For SQL identifier emission, prefer always-quote over hand-rolled allowlists.** The dialect evolves; allowlists don't. Quoting is harmless for non-keyword names and the only safe form for keyword names.
- **Pin the always-quote contract in a unit test.** A table-driven test asserting the function returns `"name"` for every input (plain, keyword, leading-digit, punctuation, embedded-quote) prevents future regressions from someone re-introducing a "skip quoting for plain identifiers" optimization.
- **Integration tests for generator-emission bugs must verify the bug-shaped SQL is actually emitted.** Add cheap `assert.Contains(store, "CREATE TABLE IF NOT EXISTS \"add\"")` pre-checks before running heavy compile/test gates. A spec fixture that doesn't cross the gravity threshold silently produces an inert test that passes against the unpatched generator — and integration tests are expensive to run, so the inert-fixture problem is hard to notice during normal development cycles.
- **Verify regression-catching by stashing the fix and re-running.** A test that fails when the fix is reverted is a real regression guard. A test that still passes when the fix is reverted is decoration.

Cross-reference: `docs/solutions/logic-errors/store-columns-sourced-from-request-params-instead-of-response-2026-05-08.md` is an adjacent learning in the same `internal/generator/schema_builder` module — its lesson that `schema_builder` defects surface only via direct SQLite inspection (never at the CLI surface) applies here too, and motivates the pre-check assertion pattern recommended above.
