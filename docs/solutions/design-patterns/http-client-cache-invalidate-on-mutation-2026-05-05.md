---
title: HTTP client response cache must invalidate on successful mutations
date: 2026-05-05
category: design-patterns
module: cli-printing-press-generator
problem_type: design_pattern
component: tooling
severity: medium
applies_when:
  - "Emitting an HTTP-client template with any response cache (memory, disk, sqlite)"
  - "Reusing one client across read and write methods"
  - "Generating downstream consumers (MCP servers, batch tools, replay harnesses) that share the cache directory"
  - "The client has a writeCache() but no invalidateCache() — that asymmetry is the diagnostic signal"
related_components:
  - service_object
tags:
  - http-client
  - cache-invalidation
  - generator-templates
  - printed-cli
  - mcp
  - read-after-write
  - mutation-safety
---

# HTTP client response cache must invalidate on successful mutations

## Context

Every CLI emitted by `cli-printing-press` ships a stock HTTP client at `internal/client/client.go`. The client has a 5-minute on-disk response cache keyed by `sha256(path, params)[0:8]`, written from successful GETs via `writeCache()`. The cache was originally write-only — every Get/Post/Delete/Patch/Put funneled through `do()`, but only the GET path interacted with the cache file. There was no invalidation path.

This is a two-consumer problem. The same client services both:

1. **Human CLI use** — interactive `cli list`, `cli get <id>`, etc. Cache is valuable here: repeated reads of the same data return fast.
2. **Agent/MCP use** — `cli-pp-mcp` server reads the same cache. Agents drive the API in tight mutate-then-read loops; stale reads break their reasoning.

The two consumer paths each surfaced the same root cause separately, in different sessions:

- **2026-05-02, Dub.co MCP dogfood** (session history): Create → Get-info (warmed cache) → Delete → Get-info-after-delete returned the stale cached record instead of a 404. The fix landed as PR #521: `newMCPClient()` sets `c.NoCache = true` so MCP bypasses the cache entirely. Library backfill of 38 already-published CLIs went out as PR #213. The MEMORY.md note "MCP server should default to NoCache=true" came from that session.
- **2026-05-04, cal-com follow-up smoke test** (this session): `link create --slug pp-test-45` followed by `link list` (no `--no-cache`) returned the pre-mutation list. Same root cause, different consumer — the human-CLI path that PR #521 didn't cover.

The MCP-side `NoCache=true` workaround (auto memory [claude]) (session history) handles the agent path by disabling the cache. It does not solve the bug for human users, who still see stale list-after-mutation. The fix below covers both paths without sacrificing cache benefits on read-only flows.

## Guidance

**If your generated client ships a response cache, invalidate the entire cache directory at the success branch of any non-GET request.** The two-pronged approach:

1. **Constructor default for MCP-shaped consumers** (already in template via PR #521 (session history)): `c.NoCache = true` for clients driven by agents.
2. **Cache invalidation on mutation for ALL clients**: in `do()`, after a successful response, if the request was non-GET, drop the entire cache.

```go
// In do(), success branch:
if resp.StatusCode < 400 {
    c.limiter.OnSuccess()
    if method != http.MethodGet {
        c.invalidateCache()
    }
    return json.RawMessage(respBody), resp.StatusCode, nil
}

// invalidateCache removes all cached responses. Wholesale nuke is the right
// tradeoff: cache TTL is 5 minutes, losing it after a mutation costs at most
// one extra GET on the next read.
func (c *Client) invalidateCache() {
    if c.cacheDir == "" {
        return
    }
    _ = os.RemoveAll(c.cacheDir)
}
```

Constraints on the call site:

- Only fire on `resp.StatusCode < 400` — 4xx/5xx didn't mutate state.
- Only fire when `method != http.MethodGet`.
- Skip on dry-run paths and inside the retry loop (only the final success).
- Best-effort: ignore the `RemoveAll` error so a cache-clear failure does not fail the surrounding successful mutation.

### Discarded alternative — selective invalidation by resource family

It is tempting to invalidate only entries whose path prefix matches the mutation (e.g., POST `/v2/me/ooo` invalidates only `/v2/me/ooo*`). Reject this: the cache key is `sha256(path+params)[0:8]`, which is opaque on disk — recovering the path requires restructuring the cache file format to store path metadata alongside the body. That is roughly a 10x larger change for marginal benefit when the TTL is already 5 minutes; the worst case of wholesale invalidation is one extra GET on the next read.

### Discarded alternative — shorter TTL or NoCache by default

Shorter TTL (e.g., 30 seconds) only narrows the bug window; it does not fix it. NoCache-by-default for the human CLI defeats the cache's purpose for the common read-after-read pattern (`bookings list` then `bookings get <uid>`). The MCP path is different — agents almost always need fresh data — which is why PR #521's MCP-only `NoCache=true` is the right default for that consumer specifically (session history).

## Why This Matters

**User-visible failure mode**: a successful write *appears not to have been written.* `link create` reports success and returns a 2xx body, but `link list` immediately after omits the new entry. Users re-run the create, get a duplicate-slug error, and conclude the CLI is broken.

**Agent failure mode is worse**: an agent driving the API cannot distinguish "the POST silently failed" from "the POST succeeded but the cache is lying." It will retry, escalate, or report incorrect state to the human. PR #521 narrowed this to the MCP transport via `NoCache=true`; this pattern eliminates the bug entirely so the cache stays valuable for read-only flows on every transport.

**Compounding cost without this rule**: every CLI generated by the printing-press template inherits the asymmetry. As of 2026-05-02 the public library had 38 published CLIs missing the original MCP-side fix; the human-CLI invalidation gap was previously uncovered. The generator-template equivalent of this fix avoids both backfills going forward.

## When to Apply

- Every code path that emits an HTTP-client template with any form of response caching (memory, disk, sqlite).
- Any client reused across both read and write methods.
- Any client whose code base has a `writeCache()` (or equivalent) but no `invalidateCache()` — the asymmetry is the diagnostic signal.
- When generating downstream consumers (MCP servers, batch tools, replay harnesses) that share the cache directory.
- Re-audit the client whenever you add a new HTTP method helper, a new cache backend, or a new transport that wraps the same client.

## Examples

**Before — `do()` success branch (post-PR #521, pre-this-fix):**

```go
if resp.StatusCode < 400 {
    c.limiter.OnSuccess()
    return json.RawMessage(respBody), resp.StatusCode, nil
}
```

**After:**

```go
if resp.StatusCode < 400 {
    c.limiter.OnSuccess()
    if method != http.MethodGet {
        c.invalidateCache()
    }
    return json.RawMessage(respBody), resp.StatusCode, nil
}
```

**Round-trip verification (cal-com CLI, live API, 2026-05-04):**

| Sequence | Pre-fix | Post-fix |
|---|---|---|
| `link create` → `link list` (no `--no-cache`) | pre-mutation list (new entry missing) | new entry present |
| `ooo set` → `ooo list` | count 0 (stale) | count 1, fresh entry |
| `ooo delete` → `ooo list` | count 1 (stale) | count 0 |

Both legs (MCP-side `NoCache=true` from PR #521 (session history) and CLI-side `invalidateCache()` from this fix) are required: the MCP default ensures agent paths always see fresh data; the on-mutation invalidation ensures human CLI paths do too without burning the cache benefit on read-only sequences.

## Related

- **PR #521** (`mvanhorn/cli-printing-press`) — generator template change adding `NoCache bool` field and `!c.NoCache` cache guard; merged before 2026-05-04. The MCP-side leg of this two-part pattern. (session history)
- **PR #213** (`mvanhorn/printing-press-library`) — bulk backfill of `NoCache=true` across 38 published CLIs that pre-dated PR #521. (session history)
- **Issue #515** — Dub retro that originally surfaced the MCP-side stale-read on Create → warm-cache → Delete → stale 200. (session history)
- **PR #237** (`mvanhorn/printing-press-library`) commit `d26a2862` — the cal-com CLI-side `invalidateCache()` fix this doc captures. Printed-CLI-level patch; does NOT yet propagate to other CLIs.
- **Issue #603** (closed 2026-05-05) — emits `invalidateCache()` and the `do()` success-branch hook in every printed CLI's `client.go` template so future CLIs inherit it without per-CLI patching.
- **MEMORY.md note** "MCP server should default to NoCache=true" (auto memory [claude]) — captures the MCP-side workaround. With this fix, the note's "agents driving the API through MCP get stale snapshots after DELETE/PATCH" caveat is now resolved at the client level; `NoCache=true` remains the right MCP default for freshness reasons unrelated to mutations.
