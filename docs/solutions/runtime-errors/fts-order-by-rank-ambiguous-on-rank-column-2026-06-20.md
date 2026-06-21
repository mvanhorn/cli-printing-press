---
title: "Generated FTS ORDER BY rank is ambiguous when a resource has a typed rank column"
date: 2026-06-20
category: runtime-errors
module: internal/generator/templates
problem_type: runtime_error
component: store
symptoms:
  - "`search` on a printed CLI whose resource carries a typed `rank` column failed with `SQL logic error: ambiguous column name: rank (1)`"
  - "Only resources with a `rank` response field (e.g. rankings/standings resources) were affected; rank-less resources searched fine"
  - "The generic `Search` and the per-resource `Search<Resource>` both failed at query time the first time FTS MATCH produced a row"
root_cause: code_generation_bug
resolution_type: code_fix
severity: high
tags:
  - sqlite
  - fts5
  - store
  - search
  - codegen
  - ambiguous-column
related_components:
  - store
  - insights
issue: 2973
---

# Generated FTS `ORDER BY rank` is ambiguous on rank-fielded resources

## Problem

The store template (`internal/generator/templates/store.go.tmpl`) and the insights similar template (`internal/generator/templates/insights/similar.go.tmpl`) emitted FTS5 search queries with an unqualified `ORDER BY rank`:

```sql
-- per-resource Search<Resource>
SELECT t.data FROM events t
 JOIN events_fts ON events_fts.rowid = t.rowid
 WHERE events_fts MATCH ?
 ORDER BY rank LIMIT ?

-- generic Search
SELECT r.data FROM resources r
 JOIN resources_fts f ON r.id = f.id AND r.resource_type = f.resource_type
 WHERE resources_fts MATCH ?
 ORDER BY rank LIMIT ?
```

`rank` is an FTS5 special column exposed by every `USING fts5(...)` table. It is also an extremely common name for a real response field (rankings, standings, priorities). When schema extraction gives the data table a typed `rank` column (`events.rank`, `resources.rank`), the unqualified `ORDER BY rank` resolves against **both** the FTS table's special column and the data table's typed column, and SQLite rejects the query at prepare time:

```
SQL logic error: ambiguous column name: rank (1)
```

So `search` broke for every rank-fielded resource (pp-espn etc.) with zero proximate cause — the store opened fine, sync populated the index fine, and the failure surfaced only the first time FTS MATCH returned a row.

## The non-obvious part: qualify with the alias, not the table name

The instinct is to qualify with the FTS table name: `ORDER BY resources_fts.rank`. That **fails** for the generic query, because `resources_fts` is aliased as `f` in the JOIN:

```
no such column: resources_fts.rank
```

SQLite resolves `resources_fts MATCH ?` (the FTS5 MATCH operator) to the aliased table by base name — a quirk that makes the base name *look* usable — but for a normal column reference like `.rank`, once a table is aliased the alias is the only valid qualifier. The per-resource query has no alias on its FTS table, so the base name works there; the generic and insights queries are aliased, so the alias is required. The fix qualifies every `rank` reference to whatever name actually scopes it in that query:

| Query | FTS table binding | Correct qualifier |
|---|---|---|
| per-resource `Search<Resource>` | `JOIN {{name}}_fts` (no alias) | `{{name}}_fts.rank` |
| generic `Search` | `JOIN resources_fts f` | `f.rank` |
| insights `similar` | `FROM resources_fts fts` | `fts.rank` (both the SELECT and ORDER BY) |

The insights query also selected `rank` (not just ordered by it), and that bare `rank` in the SELECT list was equally ambiguous — both had to be qualified.

## Solution

Qualify every FTS5 `rank` reference to its scoping name. Emitted result:

```sql
-- per-resource
ORDER BY events_fts.rank LIMIT ?
-- generic (aliased)
ORDER BY f.rank
-- insights similar (aliased)
SELECT r.id, r.resource_type, r.data, fts.rank ... ORDER BY fts.rank
```

No behavior change for rank-less resources: the qualifier just names the only `rank` in scope, which is what SQLite resolved to anyway.

## Prevention

`TestGenerateStoreFTSOrderByRankQualified` (`internal/generator/fts_order_by_rank_test.go`) builds a spec with a `rank`-fielded, FTS5-enabled resource, generates the CLI, and asserts:

1. The emitted store.go qualifies the per-resource rank to the FTS table and the generic rank to the `f` alias.
2. No unqualified `ORDER BY rank` remains anywhere in the store.
3. The generated `TestSearch<Resource>QuotesFTSQuerySyntax` actually executes against a SQLite DB whose table carries a typed `rank` column — the exact shape that failed before the fix.

Before the fix, (3) fails with the live error `SearchItems("10.0.0.1"): SQL logic error: ambiguous column name: rank`, so the test cannot silently regress. When adding a new FTS query to a template, qualify `rank` to whatever name binds the FTS table in that query (alias if aliased, base name if not) and extend this test.

## Related Issues

- mvanhorn/cli-printing-press#2973 (bug report — "always-correct, zero-downside")
