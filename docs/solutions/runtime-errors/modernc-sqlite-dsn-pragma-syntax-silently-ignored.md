---
title: "modernc.org/sqlite ignores mattn-style DSN pragmas (WAL, busy_timeout) silently"
date: 2026-05-26
category: runtime-errors
module: cli-printing-press-generator
problem_type: runtime_error
component: store
symptoms:
  - "A read concurrent with a write failed immediately with `database is locked (5) (SQLITE_BUSY)` — e.g. an MCP `sql`/`search`/analytics query while `sync` was writing"
  - "`sqlite3 <data.db> 'PRAGMA journal_mode'` returned `delete`, not `wal`, on every printed CLI's store"
  - "`PRAGMA busy_timeout` was `0` and `PRAGMA foreign_keys` was `0` despite the DSN appearing to set them"
root_cause: configuration_error
resolution_type: code_fix
severity: high
tags:
  - sqlite
  - modernc
  - dsn
  - wal
  - busy-timeout
  - silent-failure
  - store
related_components:
  - store
  - migrate
  - mcp
issue: 2394
pr: 2399
---

# modernc.org/sqlite ignores mattn-style DSN pragmas silently

## Problem

The store template (`internal/generator/templates/store.go.tmpl`, emitted as `internal/store/store.go` in every printed CLI) opened SQLite with mattn/go-sqlite3-style DSN parameters (`?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON&_synchronous=NORMAL&_temp_store=MEMORY&_mmap_size=…`). The driver is `modernc.org/sqlite`, which recognizes only the `_pragma=<name>(<value>)` form. The mattn-style keys were parsed as unknown query parameters and dropped, so none of the intended pragmas applied — every store ran in default rollback-journal (`delete`) mode with `busy_timeout=0`.

## Symptoms

- A read issued while a write was in flight failed immediately with `database is locked (5) (SQLITE_BUSY)` instead of waiting on the lock.
- Reading the pragmas back showed defaults: `journal_mode=delete`, `busy_timeout=0`, `foreign_keys=0` — `synchronous`, `temp_store`, and `mmap_size` were likewise no-ops.
- The template's own comments asserted WAL was in effect ("WAL readers don't normally block on writers"), and one comment actively claimed the underscore-prefixed form "works either way" — both false.

## What Didn't Work

- **Trusting the in-code comment.** A comment in `OpenReadOnly` claimed `_journal_mode`/`_busy_timeout` "work either way; they're parsed out of the DSN by the driver before sqlite3_open_v2." That belief is what let the bug ship. The fix only became obvious after reading the pragmas back empirically.
- **Reordering pragmas to put `busy_timeout` first** (so the WAL conversion would honor the timeout). As the *sole* fix it measured no improvement on the concurrent-open race — the connect-time conversion BUSY is not covered by the statement-level busy handler, which is why this fix shipped behind the `retryOnBusy`-wrapped `Conn()` acquisition instead. Revisiting it later (see #2926) showed that ordering `busy_timeout` before `journal_mode` still reduces the residual race window once the retry wrapper is in place, so the DSN now lists `busy_timeout` first as defense-in-depth alongside the retry. Both layers are needed; neither is sufficient alone.

## Solution

Switch both opens to the `_pragma=` form (verify with the pinned driver version, here `modernc.org/sqlite v1.37.0`):

```go
// read-only
sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)")

// read-write (adds synchronous)
sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)")
```

Empirical proof with the pinned driver — the mattn-style DSN is byte-for-byte equivalent to passing no parameters:

| DSN | journal_mode | busy_timeout | foreign_keys |
|---|---|---|---|
| `?_journal_mode=WAL&_busy_timeout=5000&…` (old) | `delete` | `0` | `0` |
| no params | `delete` | `0` | `0` |
| `?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&…` | `wal` | `5000` | `1` |

A `mode=ro` open of a WAL file is fine; `journal_mode(WAL)` on a read-only handle is a harmless no-op. The read-write open performs the one-time `delete`→`wal` conversion, and published CLIs convert automatically on their next read-write open.

### Second bug, exposed by the first fix

Enabling WAL for real surfaced a latent race. The `journal_mode(WAL)` conversion runs at connection-establishment time (when `database/sql` opens a physical connection, modernc executes the DSN `_pragma` directives), which in the migration path happens inside `s.db.Conn(ctx)` — **outside** the existing `retryOnBusy` coverage that wrapped only the user_version read, `BEGIN IMMEDIATE`, and `COMMIT`. Concurrent first-opens of a fresh DB tripped `SQLITE_BUSY` ~40% of the time. Fix: wrap the `Conn()` acquisition in the same `retryOnBusy` helper against the shared migration deadline.

```go
deadline := time.Now().Add(migrationLockTimeout)
var conn *sql.Conn
if err := retryOnBusy(ctx, deadline, "acquiring migration connection", func() error {
    c, err := s.db.Conn(ctx)
    if err != nil {
        return err
    }
    conn = c
    return nil
}); err != nil {
    return err
}
defer conn.Close()
```

## Why This Works

`modernc.org/sqlite` is a pure-Go reimplementation, not a binding to the C `sqlite3` library, and it does not parse `mattn/go-sqlite3`'s `_<pragma>=<value>` DSN keys. Its own convention is `_pragma=<name>(<value>)`, applied left-to-right at physical-connection establishment. Unrecognized keys are silently ignored rather than rejected, so a syntactically wrong DSN produces a working connection with the wrong settings — no error anywhere.

## Prevention

- **Two SQLite drivers, two DSN dialects.** When the driver is `modernc.org/sqlite`, pragmas are `_pragma=name(value)`. When it is `mattn/go-sqlite3` (CGO), they are `_journal_mode=…` etc. The two are not interchangeable and the wrong one fails silently. Printed CLIs use modernc (pure Go, no CGO) — see `store.go.tmpl`'s import.
- **A config string that is silently a no-op can mask a latent bug.** The store had concurrency-hardening machinery (`retryOnBusy`, `BEGIN IMMEDIATE`) that only had to cover statement-level contention because WAL was never actually on. Turning WAL on exposed a connect-time conversion race the no-op had hidden. When you fix a setting that was previously inert, re-test the paths that setting touches — the "fix" can reveal bugs the broken state was suppressing.
- **Verify pragmas by reading them back, never by trusting the DSN or a comment.** `TestOpenAppliesPragmas` (in `store_schema_version_test.go.tmpl`) opens the store, then asserts `PRAGMA journal_mode == wal` and `PRAGMA busy_timeout == 5000` on both the read-write and read-only handles, so the DSN cannot silently regress:

```go
func requirePragma(t *testing.T, db *sql.DB, name, want string) {
    t.Helper()
    var got string
    if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
        t.Fatalf("read pragma %s: %v", name, err)
    }
    if got != want {
        t.Fatalf("PRAGMA %s = %q, want %q", name, got, want)
    }
}
```

  (Reading the value as text covers both string pragmas like `journal_mode` and integer pragmas like `busy_timeout`.)

## Related Issues

- mvanhorn/cli-printing-press#2394 (bug report, with the empirical pragma-readback table)
- mvanhorn/cli-printing-press#2399 (fix)
