---
title: Generated MCP sql tool opened read-write SQLite under a false readOnlyHint
date: "2026-05-08"
category: docs/solutions/security-issues/
module: internal/generator/templates
problem_type: security_issue
component: tooling
severity: high
symptoms:
  - "MCP sql and search tools declared ReadOnlyHintAnnotation(true) but opened a read-write SQLite handle, so MCP hosts auto-approved invocations that could write"
  - "Keyword-prefix blocklist was defeated by SQL comment prefixes (/* x */ VACUUM INTO, -- x\\nATTACH DATABASE) and by leading semicolons"
  - "VACUUM INTO wrote a full database snapshot to an attacker-chosen path under mode=ro"
  - "ATTACH DATABASE opened a separate writable handle under mode=ro"
root_cause: missing_permission
resolution_type: code_fix
tags:
  - mcp
  - sqlite
  - readonly
  - allowlist
  - sql-injection
  - modernc-sqlite
  - generator-template
related_components:
  - database
  - assistant
---

# Generated MCP sql tool opened read-write SQLite under a false readOnlyHint

## Problem

Generated MCP `sql` and `search` tools in every printed CLI shipping `VisionSet.Store` (49 of 52 in the public library) declared `ReadOnlyHintAnnotation(true)` to signal MCP hosts that auto-approval is safe. The underlying SQLite handle was opened read-write via `store.OpenWithContext`, and the only write-gate was a 6-keyword prefix blocklist applied with `strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), keyword)`. Two independently bypassable attack classes existed:

1. **Blocklist gap.** `VACUUM INTO` and `ATTACH DATABASE` were not in the blocklist. Either command executes against the read-write handle and exfiltrates or creates a writable copy of the local DB.
2. **Comment-prefix bypass.** `TrimSpace` strips outer whitespace but does not understand SQL comment syntax. SQLite ignores leading `--` line comments, `/* */` block comments, and `;` statement separators before parsing the first keyword; the blocklist did not. Any blocked keyword reachable after such a prefix bypassed the gate entirely.

Combined effect: a prompt injection in synced data could exfiltrate the local SQLite store through an MCP tool that hosts auto-approved as read-only, with no permission prompt.

## Symptoms

- `/* x */ VACUUM INTO '/tmp/exfil.db'` submitted to the MCP `sql` tool passes the blocklist and writes a full DB snapshot to disk.
- `-- x\nVACUUM INTO ...` and `/**/VACUUM INTO ...` and `; VACUUM INTO ...` all pass the blocklist identically.
- `/* x */ ATTACH DATABASE 'file:/tmp/x?mode=rwc' AS evil` passes the blocklist and opens a second writable handle at an attacker-controlled path.
- Both vectors succeed on a handle opened with `?mode=ro` when the DSN lacks the `file:` URI prefix — modernc.org/sqlite silently drops `?mode=ro` without `file:`.
- MCP hosts (Claude Code, Cursor) auto-approve the tool call without a permission prompt because `ReadOnlyHintAnnotation(true)` is set.

## What Didn't Work

- **Expanded blocklist without noise stripping.** The community proposal (issue #693) added `ATTACH`, `DETACH`, `PRAGMA`, `REINDEX`, `VACUUM`, and `WITH` to the prefix blocklist. This correctly identified the missing keywords but left the comment-prefix bypass class intact — any keyword in the expanded list is still reachable after a leading comment or semicolon.
- **`WITH` was wrong to add.** Three already-published printed CLIs (pokeapi, producthunt, fedex) accept `WITH`-prefix CTEs as legitimate read-only queries in their hand-rolled novel `sql` commands. Blocking `WITH` on the MCP surface breaks agent-native parity (any action a CLI user can take, an agent can also take). `WITH` was excluded from the final solution; CTE-wrapped writes are caught by the `mode=ro` layer.
- **`mode=ro` without `file:` URI prefix.** Empirical probe against modernc.org/sqlite showed that `dbPath+"?mode=ro&..."` (the exact DSN form the community proposal and the multimail-cli@85af2bf reference fix shipped) silently drops the `?mode=ro` parameter. INSERT succeeded against the supposedly read-only handle. Security in those implementations came entirely from the blocklist, not from `mode=ro`. The `file:` prefix is required to activate the URI parameter.
- **`mode=ro` alone is not sufficient.** Even with the correct `file:` URI form, `mode=ro` does not block `VACUUM INTO` or `ATTACH DATABASE` in modernc.org/sqlite. Both commands succeed and create files on disk under a correctly opened read-only handle. Application-layer rejection is still required for those vectors.

## Solution

Switched from blocklist to allowlist with proper SQL noise stripping. Changes land in two generator templates plus a new test template.

**`internal/generator/templates/mcp_tools.go.tmpl` — two new helpers, `handleSQL` and `handleSearch` updated:**

```go
// validateReadOnlyQuery applies an allowlist (SELECT or WITH) after
// stripping leading whitespace, line comments, block comments, and
// semicolons. SELECT and WITH are the only allowed leading keywords;
// CTE-wrapped writes are caught by OpenReadOnly's mode=ro one layer down.
func validateReadOnlyQuery(query string) error {
    upper := strings.ToUpper(stripLeadingSQLNoise(query))
    if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
        return fmt.Errorf("only SELECT queries are allowed")
    }
    return nil
}

// stripLeadingSQLNoise removes leading whitespace, SQL line comments
// (-- to end of line), block comments (/* ... */), and statement
// separators (;). SQLite skips these before parsing the first keyword.
func stripLeadingSQLNoise(query string) string {
    for {
        query = strings.TrimLeft(query, " \t\r\n;")
        switch {
        case strings.HasPrefix(query, "--"):
            if idx := strings.IndexByte(query, '\n'); idx >= 0 {
                query = query[idx+1:]
                continue
            }
            return ""
        case strings.HasPrefix(query, "/*"):
            if idx := strings.Index(query[2:], "*/"); idx >= 0 {
                query = query[2+idx+2:]
                continue
            }
            return ""
        default:
            return query
        }
    }
}
```

`handleSQL` dispatches through `validateReadOnlyQuery` before opening the store, and switches from `store.OpenWithContext(ctx, dbPath())` to `store.OpenReadOnly(dbPath())`. `handleSearch` switches to `store.OpenReadOnly(dbPath())` (search input is bound, not arbitrary SQL — but the tool surface advertises read-only, so the connection should match).

**`internal/generator/templates/store.go.tmpl` — new `OpenReadOnly` constructor:**

```go
func OpenReadOnly(dbPath string) (*Store, error) {
    db, err := sql.Open("sqlite",
        "file:"+dbPath+"?mode=ro&_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON&_temp_store=MEMORY&_mmap_size=268435456")
    if err != nil {
        return nil, fmt.Errorf("opening database (read-only): %w", err)
    }
    db.SetMaxOpenConns(2)
    return &Store{db: db, path: dbPath}, nil
}
```

The `file:` prefix is load-bearing; without it modernc.org/sqlite silently drops `?mode=ro` and the connection opens read-write.

**`internal/generator/templates/mcp_tools_test.go.tmpl` (new) — ships into every generated CLI's `mcp` package:** exercises 29 attack inputs (direct writes, CTE-wrapped writes, comment-prefix bypasses, statement-separator bypasses) plus positive cases (SELECT, WITH-prefix CTEs, queries with leading comments). Wired in `internal/generator/generator.go` to render alongside `mcp_tools.go.tmpl` whenever `VisionSet.Store` is enabled.

## Why This Works

Two layers, both load-bearing:

| Layer | What it catches | What it misses |
|-------|----------------|----------------|
| Allowlist (`SELECT` or `WITH` only, after noise stripping) | `VACUUM INTO`, `ATTACH DATABASE`, `PRAGMA`, all DDL/DML, comment-prefixed bypasses of any of the above | (nothing the next layer doesn't catch) |
| `mode=ro` (`file:` URI + `mode=ro` DSN) | `INSERT`/`UPDATE`/`DELETE`/`REPLACE` direct writes; CTE-wrapped writes (`WITH x AS (...) INSERT ...`) | `VACUUM INTO`, `ATTACH DATABASE` — handled by the allowlist |

Each layer covers what the other cannot. A comment-prefix attack that strips cleanly through `stripLeadingSQLNoise` to reveal a non-SELECT/WITH keyword is rejected by the allowlist before the store is opened. A CTE-wrapped write that passes the `WITH` allowlist is rejected by `mode=ro` at the driver level.

The `ReadOnlyHintAnnotation(true)` annotation is now accurate: the tool cannot write to any SQLite database reachable from its handle.

This fix follows the same tightening pattern as an earlier `newMCPClient()` change that defaulted `NoCache=true` for the MCP transport (auto memory [claude]). Both bugs were "the CLI behavior was acceptable for humans but leaked privilege or staleness through to the agent surface" — the MCP surface consistently requires stricter defaults than the CLI surface.

## Prevention

**Template-shipped test covers 29 attack inputs.** `mcp_tools_test.go.tmpl` exercises:

- Direct writes: `INSERT`, `UPDATE`, `DELETE`, `REPLACE`, `DROP`, `CREATE`, `ALTER`, `PRAGMA`, `REINDEX`, `DETACH`
- Comment-prefix bypasses: `/* x */ VACUUM INTO ...`, `-- x\nVACUUM INTO ...`, `/**/ATTACH ...`, `; VACUUM INTO ...`, `/* x */ ATTACH DATABASE ...`
- Statement-separator bypasses: `;VACUUM INTO ...`, `; ; VACUUM INTO ...`
- Positive cases: `SELECT ...`, `WITH x AS (SELECT 1) SELECT * FROM x`, queries with leading `-- comment\n` before SELECT

Runs on every CI build in every generated CLI. CTE-wrapped write rejection is pinned by `TestOpenReadOnly_RejectsWrites` in `store_schema_version_test.go.tmpl`.

**Generator-side test pins the emission shape.** `TestGenerateMCPSQLToolUsesReadOnlyStore` asserts:

- `OpenReadOnly` is emitted in `store.go` with the `"file:"+dbPath+"?mode=ro` literal in the DSN
- Both `validateReadOnlyQuery` and `stripLeadingSQLNoise` exist in `tools.go`
- `handleSQL` dispatches through `validateReadOnlyQuery`
- The allowlist contains both `SELECT` and `WITH`

**Three rules for future maintainers working on SQL gates in generated CLIs:**

- **Strip SQL noise before keyword checks.** Any prefix-style gate must strip leading whitespace, `--` line comments, `/* */` block comments, and `;` separators before the keyword comparison. SQLite ignores these before parsing the first keyword; gates that don't match this behavior are bypassable by construction.
- **Use `file:` URI prefix for modernc.org/sqlite read-only handles.** `dbPath+"?mode=ro"` silently drops the parameter. The DSN must start with `file:` for `?mode=ro` to take effect.
- **`mode=ro` does not block `VACUUM INTO` or `ATTACH DATABASE`.** These vectors must be rejected at the application layer (allowlist or explicit blocklist entry) before reaching the driver; they cannot be stopped by the handle's open mode alone.

**Pattern reminder.** Defects in `internal/generator/templates/mcp_tools.go.tmpl` consistently fire on the MCP surface only and are invisible to CLI smoke checks. This is the second class-level defect found in this template (see Related Issues). Treat any change to MCP-surface defaults — connection mode, cache settings, tool annotations, query gates — as a candidate for surface-specific divergence and pin coverage at the generator level, not just at the printed-CLI level.

## Related Issues

- GitHub: [mvanhorn/cli-printing-press#693](https://github.com/mvanhorn/cli-printing-press/issues/693) — the original community report this fix addresses.
- `docs/solutions/logic-errors/mcp-handler-conflates-path-and-query-positional-params-2026-05-05.md` — prior class-level defect in the same `mcp_tools.go.tmpl` file. Same failure shape: MCP surface silently wrong while CLI surface passes smoke checks. Same fix shape: generator-template correction plus published-CLI backfill. The two defects share template surface and failure pattern but are distinct security events.
- `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` — parallel pattern at the HTTP-cache layer. Same theme: MCP-surface defaults must be stricter than CLI-surface defaults, and the generator constructor is the right place to enforce them. The earlier fix tightened freshness; this one tightens write privilege.
- `docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md` — prior allowlist-over-blocklist pattern. Same lesson: when a deny-list grows in response to bypasses, switch to an allow-list grounded in what the surface should accept.
- `docs/solutions/best-practices/cross-repo-coordination-with-printing-press-library-2026-05-06.md` — applies to the cross-repo regen step needed to backfill all 49+ published CLIs with the new `mcp_tools_test.go.tmpl`.
