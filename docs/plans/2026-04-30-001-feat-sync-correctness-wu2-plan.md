---
title: WU-2 — Sync correctness pass (pagination, ID extraction, exit policy, write serialization)
type: feat
status: active
date: 2026-04-30
deepened: 2026-04-30
origin: https://github.com/mvanhorn/cli-printing-press/issues/421
---

# WU-2 — Sync correctness pass (pagination, ID extraction, exit policy, write serialization)

## Summary

Land four template-level sync correctness fixes from PokéAPI retro WU-2 — `--max-pages` default raised with a cap-hit warning, profiler-driven primary-key extraction with a fallback chain, exit-code policy that distinguishes partial from total failure via a new `x-critical` OpenAPI extension, and `sync.Mutex`-serialized writes in the store template to eliminate `SQLITE_BUSY` at default concurrency. All work happens in `internal/generator/templates/*.tmpl`, `internal/profiler/`, and `internal/openapi/parser.go`; printed CLIs inherit the fixes at next regen.

---

## Problem Frame

A PokéAPI generation surfaced four sync-correctness bugs that compound across most synced APIs: silent truncation at 100 items per resource, `SQLITE_BUSY` at any concurrency above 1, silent zero-row stores when list items lack a `name` field, and non-zero exit codes when any resource fails (even non-essential ones). Two of the four findings are recurrences from prior retros — F2 (movie-goat 2026-04-11 WU-5) and F4a (kalshi 2026-04-10 WU-3) — meaning the partial fixes shipped previously left enough rough edges that the same shape keeps biting. F3 has a prior plan (2026-04-12-002) that recommended `MaxOpenConns(2) + WAL` with `store.writeMu` as a belt-and-suspenders option but the mutex was never adopted. F5 is new territory.

The work is template-side: sync code lives in `internal/generator/templates/sync.go.tmpl` and `store.go.tmpl`, not in the printing-press binary itself. Each printed CLI compiles the templates into its own `internal/cli/sync.go` and `internal/store/store.go` at generation time, so fixes propagate via re-generation, not via binary upgrade.

---

## Requirements

- R1. Sync's pagination terminates naturally on API-reported end-of-data; default page-cap is high enough that real datasets don't silently truncate; cap-hits emit a structured `sync_warning`.
- R2. Sync upserts cannot trigger `SQLITE_BUSY` at default concurrency. Concurrent fetch is preserved; writes serialize through a single guard at the store layer.
- R3. ID extraction in sync follows a documented fallback chain (`x-resource-id` extension → `id` → `name` → first required scalar in response schema). Spec authors can override per resource. Sync emits a structured warning when fetched > 0 but stored = 0 instead of silently dropping rows.
- R4. Sync's exit code reflects intent: a single non-essential resource failure should not fail the whole run. Spec authors flag essential resources via `x-critical: true`. Existing strict semantics remain available via an explicit `--strict` flag for backward compatibility.
- R5. Each sub-fix lands as its own commit/PR with a `feat(cli):` or `fix(cli):` prefix. Golden harness fixtures (`testdata/golden/fixtures/golden-api.yaml`) are extended to lock in the new contracts so future template churn cannot silently regress them.

---

## Scope Boundaries

- **F4b is held out.** The "pokemon-species/form/evolution-chain reported 100 fetched but stored 0 rows despite having `name` fields" finding from the retro is NOT in scope — it was never reproduced under controlled conditions and warrants a focused repro before claiming a fix shape.
- **No sync UX redesign.** Flag names, JSON event shapes, command structure stay as-is. The new `--strict` flag is the only flag-surface addition.
- **Other WU-2-adjacent retro findings are not in scope.** F1 (resource-name collision detection), F6 (scorecard YAML), F7 (search `--json` empty stdout), F9 (live-check tokenizer), F11 (novel-command stub generation) — all separate work units.
- **No auto-rate-limiting in sync.** The WAL contention fix is the boundary; rate-limit semantics are a separate concern.
- **No migration of existing public-library CLIs.** Already-shipped CLIs pick up the new sync semantics on next regen. Backporting via `mcp-sync`-style migration command is out of scope.
- **No retroactive `x-critical` declarations.** Spec catalog edits are out of scope; existing specs default to "no resource is critical" and rely on the `--strict` escape hatch when callers want fail-fast semantics.

### Deferred to Follow-Up Work

- **Promote sync mutex pattern to `docs/PATTERNS.md`** (the parallel-fetch + serialized-write pattern is currently undocumented; capture once this work lands so the next sync change does not re-litigate the architecture). The original draft also mentioned `/ce-compound` as a capture target — that's the compound-engineering knowledge-base skill, used to document solved-once patterns. Optional; the pattern doc is sufficient on its own.
- **Profiler-driven response-schema scalar detection for `x-resource-id` fallback** — the "first required scalar in response schema" tier of F4a's fallback chain is the most spec-shape-dependent. If response-schema detection turns out to require deeper schema walking than expected, this tier may land in a follow-up plan; the first three tiers (`x-resource-id` → `id` → `name`) cover the common case.
- **Catalog-spec retrofit policy for new `x-` extensions.** This plan introduces `x-resource-id` and `x-critical` as the canonical declarations of two pieces of information the machine previously inferred. The trajectory question — does the printing-press become "OpenAPI-spec generator" or "annotated-spec generator"? — is deliberately deferred. Two paths to evaluate in a follow-up: (a) push annotations upstream into vendor specs (slow, requires vendor cooperation); (b) maintain a sidecar overlay file per catalog API that the parser merges with the upstream spec at generation time (fast, machine-owned). Today's behavior preserves the inference fallbacks so unannotated specs still work — but consistency across the public library will drift until the policy is set.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/sync.go.tmpl` (805 lines) — entire generated sync command.
  - `newSyncCmd` lines 30-262 — cobra wiring, flag declarations (including `--max-pages` at line 255).
  - Worker pool at lines 145-176 — buffered work channel + N goroutines (default 4 per `--concurrency`).
  - `syncResource` lines 264-429 — per-resource page loop. Inline `db.UpsertBatch` call at lines 357-378 (where F3's serialization needs to land).
  - Loop break condition lines 414-416 — `!hasMore || len(items) < pageSize.limit || nextCursor == ""`. Already follows `next` URLs to natural end-of-data; F2 just needs the cap raised + sync_warning emission.
  - Exit-code logic lines 240-246 — `errCount > 0` and `successCount == 0` checks (where F5 lands).
  - `extractID` lines 795-805 — current fallback list, parallels the duplicated loop in store.go.tmpl.
  - `sync_anomaly` event line 374 — already covers fetched>0/stored=0; F4a reuses.
  - `sync_warning` event lines 324, 725 — already structured with `status/reason/message`.

- `internal/generator/templates/store.go.tmpl` (790 lines) — generated store/upsert layer.
  - `Open` lines 42-64 — `_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000` and `db.SetMaxOpenConns(2)`.
  - `UpsertBatch(resourceType, items)` lines 526-583 — single-transaction loop. F3 mutex wraps the body.
  - PK-extraction loop lines 540-554 — the kalshi-accreted fallback list `{id, ID, ticker, event_ticker, series_ticker, key, code, uid, uuid, slug, name}`. F4a replaces with templated `IDField` plus generic fallback.

- `internal/generator/templates/helpers.go.tmpl` lines 138-234 — `accessWarning` struct, `isSyncAccessWarning` helper. Add critical-resource classification helper here if it doesn't fit in sync.go.tmpl.

- `internal/profiler/profiler.go`
  - `SyncableResource` struct lines 57-61 — currently `{Name, Path}`. Extend with `IDField string` and `Critical bool`.
  - Detection in `Profile()` lines 109+. New ID-field/critical reads happen during this pass.

- `internal/openapi/parser.go`
  - Existing `x-*` extension reads at lines 170, 190, 204, 218, 356, 359, 364, 369 — pattern is `doc.Info.Extensions["x-foo"]` or `scheme.Extensions["x-auth-type"]`. Mirror this for `x-resource-id` and `x-critical` at the path-item / operation level.
  - `selectResponseSchema` line 1861, response-schema array walk lines 1799-1828 — where the "first required scalar" fallback tier reads.

- `internal/generator/templates/store_upsert_batch_test.go.tmpl` — existing template-emitted tests for upsert behavior. F3 and F4a test additions follow this shape.

- `internal/generator/templates/cliutil_fanout.go.tmpl` — bounded-concurrency + per-source error pattern. Closest analog for channel-sizing conventions if the F3 implementation needs it (mutex avoids the channel question entirely).

- `testdata/golden/fixtures/golden-api.yaml` — purpose-built generated-CLI fixture. Extended in this plan to declare `x-resource-id` and `x-critical` so the generation contract is locked in.

### Institutional Learnings

- **`docs/retros/2026-04-11-movie-goat-retro.md`** (F5, WU-5) — added `--max-pages` flag with default 10, 0=unlimited, ceiling-hit log line. PokéAPI hit the next layer because default 10 is too low and the cap-hit message is human-readable stderr only. This plan raises the default and converts the message to a structured `sync_warning`.
- **`docs/retros/2026-04-10-kalshi-retro.md`** (F3, WU-3) — recommended profiler-driven primary key detection (`x-identifier` + first-path-parameter inference) with the fallback list as a safety net. Kalshi accreted `ticker, event_ticker, series_ticker` into the fallback list as a quick fix; the better profiler-driven fix was deferred. This plan adopts that deferred approach.
- **`docs/plans/2026-04-12-002-fix-cross-cli-retro-remaining-findings-plan.md`** (Unit 1, R3) — weighed `MaxOpenConns(1)` (forced serialization, deadlocks) vs `MaxOpenConns(2)` (current). Recommended `MaxOpenConns(2) + WAL` with optional `store.writeMu sync.Mutex` belt-and-suspenders. PokéAPI hitting `SQLITE_BUSY` is the trigger for adopting the optional mutex.
- **`docs/retros/2026-04-11-movie-goat-retro.md`** ("Async goroutine write-through timing" anti-pattern) — documents that synchronous write-through is the correct default; an async goroutine variant exited the process before writes flushed. Reinforces choosing mutex over single-writer-goroutine for F3.
- **No prior precedent for sync exit-code policy** — F5 is new ground. Plan captures the policy with rationale so the next sync change doesn't re-litigate it.

### External References

- modernc.org/sqlite WAL behavior — pure-Go SQLite. WAL allows multiple readers + 1 writer; the 5-second `_busy_timeout` retries on lock contention, but high-concurrency goroutines burning through the timeout window is what produces the visible `SQLITE_BUSY`. A Go-side mutex eliminates contention before SQLite's lock layer sees it.

---

## Key Technical Decisions

- **F3: `sync.Mutex` over single-writer-goroutine.** The retro proposed a single-writer goroutine; the existing 2026-04-12 plan and the movie-goat anti-pattern both lean toward simpler synchronous serialization. A mutex on the store's write methods (`UpsertBatch` plus other write paths) achieves identical serialization with less code, no goroutine lifecycle to manage, and no risk of the async-write-through bug movie-goat documented. The performance question (does serializing writes hurt throughput?) doesn't materialize because writes are not the bottleneck — fetch latency is, and concurrent fetch is preserved.

- **F4a: profiler-driven `IDField`, fallback chain in templates.** The profiler resolves the ID field per-resource at generation time using the chain `x-resource-id → id → name → first required scalar in response schema` and emits the resolved field name into both `sync.go.tmpl`'s `extractID` and `store.go.tmpl`'s `UpsertBatch` PK loop. Templates retain a generic fallback list as a runtime safety net when the templated field is absent in some payload, but the primary path is profiler-driven, eliminating the kalshi-style "accrete API-specific names into the fallback list" pattern.

- **F5: `x-critical` OpenAPI path-item extension, default false.** Spec authors flag resources whose failure should fail the run. Default `false` is safe for existing specs (current strict behavior changes to "exit 0 with sync_warning on partial failure") — to preserve callers who depend on the old strict behavior, a new `--strict` flag reverts to "any failure = non-zero exit." This is the minimum surface change consistent with the scope boundary.

- **Naming: `x-resource-id` and `x-critical` (no `pp` prefix).** Existing extensions in this repo use bare `x-<noun>` (e.g., `x-api-name`, `x-auth-type`). Match the convention; a `pp-` prefix is unnecessary because OpenAPI extensions are namespaced by the `x-` prefix already and these are read only by the printing-press parser.

- **Sequencing: U1 (parser/profiler) → U5 (F3 mutex) → U2 (F2) → U4 (F5) → U3 (F4a).** U1 is prerequisite for U3 and U4. U5 lands second (revised from "last") because U3 also modifies `UpsertBatch` (PK-extraction loop at lines 540-554) and U5 wraps the same function body — landing them in opposite order produces a merge conflict the original sequencing claim downplayed. With U5 second, the mutex is the stable base and U3 modifies the PK loop inside an already-locked function. U2 lands third because it's a small mechanical fix and de-risks the golden fixture extension. U4 (F5) lands fourth (depends only on U1). U3 lands last because it has the most surface area in store.go.tmpl + sync.go.tmpl and benefits from a clean base.

- **`sync_anomaly` event reuse.** F4a does NOT introduce a new event name. The existing `sync_anomaly` event at sync.go.tmpl line 374 already covers fetched>0/stored=0; F4a extends it to fire whenever the fallback chain fails to extract an ID, with `reason` field distinguishing cases (`primary_key_unresolved`, `all_items_failed_id_extraction`).

- **Backward-compatibility shape.** Existing CLIs in `~/printing-press/library/` are NOT migrated by this plan; they pick up the fix on next regen via `/printing-press` or `/printing-press emboss`. Specs without `x-resource-id` keep working through the fallback chain. Specs without `x-critical` get the new "partial failure = exit 0" behavior — to make this contract change discoverable, the new default emits a one-shot `sync_warning` with `reason: "exit_policy_default_changed"` whenever it suppresses what would have been a non-zero exit. Users who depended on the old strict semantics opt into `--strict`. The kalshi-specific fallback names (`ticker`, `event_ticker`, `series_ticker`) are dropped without a deprecation runway because the user owns that CLI and will regenerate it with `x-resource-id` annotations as part of landing this WU; no other public-library CLIs depend on those names.

---

## Open Questions

### Resolved During Planning

- **Should `--max-pages 0` mean unlimited or be removed?** Confirmed via Phase 0.7 dialogue: keep the natural-pagination behavior, raise the default cap from 10 to 1000, surface a structured `sync_warning` when the cap is hit. The magic-zero semantic stays as a power-user opt-in but is not the primary path.
- **Mutex vs single-writer goroutine for F3?** Mutex. Cited rationale in Key Technical Decisions.
- **Extension naming?** `x-resource-id`, `x-critical`. Cited rationale in Key Technical Decisions.
- **F5 critical-resource definition?** Spec annotation `x-critical: true`, default false (existing specs are non-critical).
- **F4a fallback chain?** `x-resource-id → id → name → first required scalar`. Confirmed via Phase 0.7 dialogue.

### Deferred to Implementation

- **Default `--max-pages` value.** Plan proposes 1000 as the new default. **Implementation step:** quick-survey the largest paginated resource across `~/printing-press/library/` and pick a default that covers the 95th percentile. If 1000 turns out too low for any catalog API, raise to 2000-5000. The structured `sync_warning` (`reason: "max_pages_cap_hit"`) exists precisely so the right value can be found empirically without silent truncation.
- **Channel sizing in F3 if mutex turns out insufficient.** If single-mutex serialization measurably slows large syncs (unlikely; writes are not the bottleneck), or if memory pressure from in-flight fetcher results becomes visible, fall back to per-resource transaction batching with a bounded channel. Decision deferred until benchmarks against a representative spec.
- **First-required-scalar implementation depth in F4a.** If walking response schemas to find the first scalar required field requires more parser plumbing than expected, ship the first three tiers (`x-resource-id → id → name`) and defer the response-schema fallback to a follow-up. The first three tiers cover the documented PokéAPI/kalshi/standard-OpenAPI cases.
- **F4b root cause investigation.** U3's `stored_count_zero_after_extraction` probe surfaces F4b's symptom but does not fix the underlying cause. Once shipped, the next time the symptom appears in the wild, the warning + reason field will give a concrete repro target. The actual fix (FTS5 trigger? transaction rollback? character encoding?) is deferred to a follow-up WU triggered by real-world recurrence.
- **Golden fixture diff scope.** Each unit will produce some `testdata/golden/expected/generate-golden-api/` diff. Whether the diffs are minor (one new emitted line) or larger (regenerated test files) depends on the unit; reviewer will confirm intent at PR time.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

The four sub-fixes converge through three concrete additions and one architectural refactor:

```
                Generation Time                       Runtime (in printed CLI)
                ───────────────                       ────────────────────────

   spec.yaml   ──►  parser.go          SyncableResource{        sync.go.tmpl
   ┌─────────┐      reads:             Name, Path,              ┌─────────────────────────┐
   │ x-resource-id ─────►              IDField,        ────►    │ extractID uses templated│
   │ x-critical    ─────►              Critical} ──────►        │ IDField; fallback chain │
   └─────────┘                                                  │ runs as safety net      │
                                                                │                         │
                                                                │ exit-code logic reads   │
                                                                │ Critical flag           │
                                                                │                         │
                                                                │ --max-pages default 1000│
                                                                │ cap-hit emits           │
                                                                │ sync_warning            │
                                                                │                         │
                                                                │ N fetcher goroutines ───┼──┐
                                                                └─────────────────────────┘  │
                                                                                             │ writeMu
                                                                                             ▼
                                                                store.go.tmpl
                                                                ┌─────────────────────────┐
                                                                │ UpsertBatch:            │
                                                                │   writeMu.Lock()        │
                                                                │   tx, _ := db.Begin()   │
                                                                │   loop items            │
                                                                │   commit                │
                                                                │   writeMu.Unlock()      │
                                                                └─────────────────────────┘
```

The mutex sits at the store layer (not the sync orchestrator) so all write paths — `UpsertBatch`, future `Delete*`, future bulk-write methods — inherit serialization without each caller having to know about it.

---

## Implementation Units

- U1. **Profiler + parser additions: read `x-resource-id` and `x-critical`; populate `SyncableResource`**

  **Goal:** Extend the profiler's `SyncableResource` struct with `IDField string` and `Critical bool`. Extend the OpenAPI parser to read path-item-level `x-resource-id` and `x-critical` extensions and route them into the profiler. Implement the ID-field fallback chain at profile time (`x-resource-id → id → name → first required scalar in response schema`) so templates receive a single resolved field name.

  **Requirements:** R3, R4 (prerequisite).

  **Dependencies:** None.

  **Files:**
  - Modify: `internal/profiler/profiler.go` — extend `SyncableResource` (lines 57-61), populate new fields during `Profile()` walk (lines 109+).
  - Modify: `internal/openapi/parser.go` — add path-item / operation extension reads near line 1799 (response-schema walk) and lines 170-218 (extension-read patterns).
  - Modify: `internal/spec/spec.go` if `IDField` / `Critical` need to flow through the internal spec representation (likely yes, since profiler reads from internal spec).
  - Test: `internal/profiler/profiler_test.go` — extend table-driven tests to cover the four-tier fallback.
  - Test: `internal/openapi/parser_test.go` — add table-driven tests for `x-resource-id` and `x-critical` extension reads.

  **Approach:**
  - Mirror the existing `Extensions["x-foo"]` cast pattern from parser.go lines 170/190/204/218 for the new path-item extensions. Operation-level extensions sit on the `*openapi3.Operation` (kin-openapi); path-item-level on `*openapi3.PathItem`. Choose one consistently — recommended path-item, because critical-ness applies to the resource not the verb.
  - Fallback chain in `Profile()`:
    1. If `x-resource-id` is set on the resource's path-item, use that string.
    2. Else if response schema declares an `id` field (required or optional), use `"id"`.
    3. Else if response schema declares a `name` field, use `"name"`.
    4. Else walk `selectResponseSchema` (parser.go line 1861) for the first scalar field present in the response schema's `required:` array (OpenAPI's required-property list, NOT a heuristic like "non-nullable" or "present in N% of payloads"). Scalar = OpenAPI types `string`, `integer`, `number`, `boolean` — exclude objects, arrays, references. Walk in the order fields appear in the schema's `properties` block. If no required field is scalar, fall through to tier 5.
    5. Else leave `IDField` empty; templates fall back to runtime list scanning (preserves current behavior for unannotated specs).
  - For `Critical bool`: simple read, default `false` when extension absent.

  **Patterns to follow:**
  - Existing extension-read pattern: `if v, ok := scheme.Extensions["x-auth-type"]; ok { ... }` from parser.go ~line 364.
  - Existing profiler test patterns: table-driven with stdlib `testing`; see profiler_test.go conventions.

  **Test scenarios:**
  - Happy path: spec with `x-resource-id: ticker` on a path-item → profiler emits `IDField: "ticker"`.
  - Happy path: spec with `x-critical: true` on a path-item → profiler emits `Critical: true`.
  - Fallback tier 2: spec without `x-resource-id` but with `id` in response schema → profiler emits `IDField: "id"`.
  - Fallback tier 3: spec without `x-resource-id` or `id` but with `name` in response schema → profiler emits `IDField: "name"`.
  - Fallback tier 4: spec without `x-resource-id`/`id`/`name` but with a required scalar field → profiler emits that field name.
  - Fallback bottoms out: spec with no detectable PK → `IDField: ""` (templates handle this).
  - Edge case: malformed extension value (`x-resource-id: 123` integer instead of string) → profiler logs warning, treats as unset.
  - Edge case: `x-critical: "true"` string instead of bool → profiler accepts truthy strings (`"true"`, `"1"`) AND bools, rejects others as `Critical: false` with warning.
  - Negative: existing fixture (spec without any new extensions) → profiler output unchanged from baseline.

  **Verification:**
  - `go test ./internal/profiler/... ./internal/openapi/... ./internal/spec/...` passes.
  - Adding `x-resource-id: ticker` to `testdata/golden/fixtures/golden-api.yaml` and running `scripts/golden.sh verify` either passes (no template change yet) or shows a controlled diff in the profiler-output snapshot if one exists.
  - The new `IDField` and `Critical` fields are visible in profiler output when run against a synthetic spec with both extensions set.

---

- U2. **F2: raise default `--max-pages` to 1000; emit structured `sync_warning` when cap is hit**

  **Goal:** Change the generated sync command's `--max-pages` default from 10 to 1000 (covers nearly all real datasets), and convert the existing human-readable cap-hit stderr message into a structured `sync_warning` event with `reason: "max_pages_cap_hit"`. Verify that the natural-pagination loop terminates correctly when the API runs out of `next` URLs (i.e., the loop already does the right thing; this unit doesn't rewrite the loop, only its cap and reporting).

  **Requirements:** R1, R5.

  **Dependencies:** None.

  **Files:**
  - Modify: `internal/generator/templates/sync.go.tmpl`:
    - Change default at line 255 from `10` to `1000`.
    - Add `sync_warning` emission inside the flat-resource cap-hit branch (lines 408-410).
    - Add `sync_warning` emission inside the dependent-resource cap-hit branch (line 762 currently has the cap check but no warning at all). Same `reason: "max_pages_cap_hit"` shape.
    - Add a sticky-cursor detector inside the page loop (~line 414 area): if `nextCursor != "" && nextCursor == lastNextCursor` (same value across consecutive pages), break out of the loop and emit `sync_warning` with `reason: "stuck_pagination"`, `message: "API returned the same next cursor across two pages; aborting to prevent budget waste."`. Apply to both `syncResource` and `syncDependentResource`.
  - Modify: `testdata/golden/fixtures/golden-api.yaml` — add a paginated resource with > 100 items so the golden fixture exercises a multi-page sync. May require fixture-helper updates to generate enough mock items.
  - Test: `internal/generator/templates/store_upsert_batch_test.go.tmpl` is unrelated; tests for sync's pagination should be added as a new template `sync_max_pages_test.go.tmpl` if a behavioral test is feasible at template-emit time, or as a generator-level test that runs the generated CLI against a multi-page mock.

  **Approach:**
  - Change line 255: `cmd.Flags().IntVar(&maxPages, "max-pages", 1000, "Maximum pages to fetch per resource (0 = unlimited; default raised from 10 to 1000 in WU-2)")`.
  - Cap-hit emission at lines 408-410 AND line 762 (where the cap-hit currently logs nothing on the dependent-resource path): emit a `sync_warning` event matching the existing `sync_anomaly` / `sync_warning` style in this template (literal `"%s"` interpolation with embedded double-quotes, **not** `%q` Go-escaping — the rest of sync.go.tmpl uses `"%s"` and mixing styles produces inconsistent JSON shapes for resource names containing quotes/backslashes). Use `reason: "max_pages_cap_hit"` and a `message` that names the cap value and suggests `--max-pages 0` for an unlimited re-run. Reference shape: `sync.go.tmpl` line 374's `sync_anomaly` emission.
  - **Sticky-cursor detection:** the existing loop break condition (`!hasMore || len(items) < pageSize.limit || nextCursor == ""`) terminates correctly for well-behaved APIs but not for APIs that echo a non-empty `next` URL when exhausted. Track `lastNextCursor` across iterations; break + emit `sync_warning` if the cursor doesn't advance. Defends against budget-burn on a 1000-cap default.
  - Do NOT otherwise modify the natural-pagination loop body — `len(items) < pageSize.limit || nextCursor == ""` continues to handle well-behaved APIs.

  **Patterns to follow:**
  - Existing `sync_warning` JSON shape from sync.go.tmpl lines 324 / 725. Match field names (`status`, `reason`, `message`).
  - Existing flag-default pattern in cobra command setup.

  **Test scenarios:**
  - Happy path: spec with a 250-item paginated resource, default flags → all 250 items synced, no `sync_warning` emitted.
  - Cap-hit case (flat): spec with a 250-item resource, `--max-pages 2` (forces cap below natural termination) → fewer items synced, `sync_warning` event emitted with `reason: "max_pages_cap_hit"` and the cap value in `message`.
  - Cap-hit case (dependent): spec with a parent resource and a paginated dependent resource, `--max-pages 2` → dependent-resource cap-hit also emits `sync_warning` with `reason: "max_pages_cap_hit"` (verifies the line-762 emission).
  - Unlimited case: spec with a large resource, `--max-pages 0` → fetches until natural termination, no `sync_warning`.
  - Sticky-cursor case: mock API returns `next: "https://api/page2"` for page 1 AND page 2 (cursor never advances) → loop breaks after page 2, emits `sync_warning` with `reason: "stuck_pagination"`. No 1000-page budget burn.
  - Edge case: spec with a resource that returns an empty first page → loop terminates immediately, 0 items synced, no warning.
  - Edge case: API returns malformed pagination (`next` URL points back to itself / sticky) → sticky-cursor detector catches this; loop breaks within 2 iterations, not at the cap. Cap remains a safety net for non-sticky pathological loops.

  **Verification:**
  - Running `scripts/golden.sh verify` shows expected diffs only in cap-related assertions / fixture output; intentional diffs explained in PR.
  - Generated CLI from updated golden fixture runs through a 250-item mock and stores 250 rows.
  - `printing-press generate` emits the new flag default in the printed CLI's `sync.go --help`.

---

- U3. **F4a: ID extraction fallback chain in templates; reuse `sync_anomaly` event as structured warning**

  **Goal:** Replace the hardcoded ID-field lookup loops in `sync.go.tmpl` (`extractID`) and `store.go.tmpl` (`UpsertBatch` PK loop lines 540-554) with a templated `IDField` value emitted by the profiler (from U1). Keep a runtime fallback list as a safety net for payloads where the templated field is unexpectedly absent. Promote the existing `sync_anomaly` event to also fire when the fallback chain fails for a single item — not just when 100% fail — so users see the warning the first time silent drops occur, not only on total failure.

  **Requirements:** R3, R5.

  **Dependencies:** U1 (profiler must emit `IDField`).

  **Files:**
  - Modify: `internal/generator/templates/sync.go.tmpl` — `extractID` lines 795-805. Templated lookup of `IDField` first, runtime fallback list second.
  - Modify: `internal/generator/templates/store.go.tmpl` — `UpsertBatch` PK loop lines 540-554. Templated `IDField` first, runtime fallback list second. Remove kalshi-accreted API-specific names (`ticker`, `event_ticker`, `series_ticker`) from the generic fallback — they were quick fixes that no longer needed because U1's profiler-driven path handles them.
  - Modify: `internal/generator/templates/sync.go.tmpl` `sync_anomaly` event around line 374 — extend to fire per-item when `IDField` resolution fails AND fall back to current "all items failed" summary case. Differentiate via `reason` field: `"primary_key_unresolved"` (per-item) vs `"all_items_failed_id_extraction"` (rolled-up summary).
  - Modify: `testdata/golden/fixtures/golden-api.yaml` — add a resource with `x-resource-id: <field>` and a resource with no extractable PK (forces the safety-net path).
  - Test: `internal/generator/templates/store_upsert_batch_test.go.tmpl` — extend table-driven cases for `x-resource-id`-templated ID, plus the runtime fallback chain.

  **Approach:**
  - Template parameter: `{{.IDField}}` from `SyncableResource` (set by U1). When non-empty, emit `id := item[{{.IDField | quote}}]` as the first lookup. When empty, emit only the runtime fallback list.
  - Runtime fallback list (reduced to truly-generic names): `{"id", "ID", "name", "uuid", "slug", "key", "code", "uid"}`. **Drop `ticker`, `event_ticker`, `series_ticker`** — these were API-specific names accreted by the kalshi retro's quick fix; the user owns the kalshi CLI and will regenerate it after this change with `x-resource-id` annotations on its spec. No deprecation runway needed because no other public-library CLIs have specs depending on these names. `code` and `uid` stay because they are generic enough to plausibly appear on other APIs without API-specific intent.
  - **`sync_anomaly` extension (per-item):** emit per-item when ID extraction fails for that item (rate-limited to prevent log spam — emit at most once per resource, with a count). The summary-level event continues to fire when the per-resource counter hits 100%. Resource-level concurrency is `1` by construction (one goroutine per resource in the worker pool — `sync.go.tmpl` work channel sized by `len(resources)`), so the per-resource counter does not race.
  - **F4b symptom probe (added):** when a resource finishes its sync with `consumed > 0 && stored == 0` AND PK extraction succeeded (`extractFailures < consumed`), emit a `sync_anomaly` with `reason: "stored_count_zero_after_extraction"`. This catches the F4b symptom (rows extracted but not landed for some other reason — FTS5 trigger error, transaction rollback, character-encoding) without trying to fix the underlying cause. Preserves visibility for the next reproduction attempt.

  **Patterns to follow:**
  - Existing kalshi precedent for accreting fallback names — but this plan reverses that pattern; profiler-driven ID is primary, fallback is generic-only.
  - Existing `sync_anomaly` event shape from sync.go.tmpl line 374.

  **Test scenarios:**
  - Happy path: spec with `x-resource-id: ticker` → templated extractID uses `ticker`; items with `ticker` field land correctly.
  - Happy path: spec without `x-resource-id`, items have `id` field → fallback tier 2 picks up; items land.
  - Happy path: spec without `x-resource-id`/`id`, items have `name` field → fallback tier 3 picks up.
  - Edge case: spec has `x-resource-id: ticker` but some items in the response are missing `ticker` → those items hit the runtime fallback list; if no fallback matches, per-item `sync_anomaly` fires with `reason: "primary_key_unresolved"`.
  - Edge case: 100% of items in a resource fail PK extraction → roll-up `sync_anomaly` fires with `reason: "all_items_failed_id_extraction"` (existing behavior preserved).
  - Negative: kalshi-style spec that previously relied on `ticker` in the fallback list but has no `x-resource-id` → re-run shows per-item warnings (so kalshi maintainers add the extension on their next regen). This is intentional: the reduction of the fallback list is a forcing function for explicit annotations.
  - Integration: golden fixture's existing resources all annotate `x-resource-id` cleanly so the golden harness exercises the templated path, not the safety net.

  **Verification:**
  - `go test ./internal/profiler/... ./internal/openapi/... ./internal/spec/... ./internal/generator/...` passes.
  - `scripts/golden.sh verify` shows controlled diffs in template emission and expected fixture output.
  - Re-generating the kalshi CLI (or any prior CLI with non-`name`/`id` PK) against a spec WITHOUT `x-resource-id` produces a runnable CLI, with sync emitting clear per-item warnings the first time PK extraction fails — instead of silent drops.

---

- U4. **F5: exit-code policy via `x-critical`; add `--strict` opt-out for backward compatibility**

  **Goal:** Replace the current "any error → non-zero exit" logic in the generated sync command with a policy that distinguishes critical from non-critical resources. Specs with no `x-critical` annotations preserve existing strict behavior via a new `--strict` flag; absent the flag, partial failures emit `sync_warning` events and the run exits 0 if any data was synced.

  **Requirements:** R4, R5.

  **Dependencies:** U1 (profiler must emit `Critical`).

  **Files:**
  - Modify: `internal/generator/templates/sync.go.tmpl` — flag declaration block in `newSyncCmd` (around line 230-260). Add `--strict` bool flag. Modify exit-logic branch at lines 240-246 to read templated `Critical` per-resource and `--strict` flag value.
  - Modify: `testdata/golden/fixtures/golden-api.yaml` — annotate one resource as `x-critical: true`; add a fixture case where a critical resource fails (mock 404) and a non-critical resource fails (mock 404).
  - Test: new test fixture for sync exit-code behavior. May fit in existing template test infrastructure.

  **Approach:**
  - New flag at sync command setup: `cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero on any per-resource failure (legacy behavior; default policy treats non-critical resource failures as warnings).")`.
  - **Critical-flag runtime mechanism (specified):** the generator emits a `criticalResources := map[string]bool{ {{range .SyncableResources}}{{if .Critical}}{{.Name | quote}}: true,{{end}}{{end}} }` literal at the top of `newSyncCmd` (template-time emission from `SyncableResource.Critical` set by U1). At worker-result aggregation, look up `criticalResources[result.Resource]` to classify each error. No struct-shape change to the `syncResult` channel, no per-resource const bloat, no extension to `defaultSyncResources()` signature.
  - Exit-logic refactor: replace `errCount > 0` with:
    ```
    if strict && errCount > 0:                  exit non-zero (legacy)
    elif criticalErrCount > 0:                  exit non-zero (any critical failed)
    elif successCount == 0:                     exit non-zero (nothing synced)
    else:                                       exit 0 (any data synced + no critical failed)
    ```
  - **In-band default-flip signal:** when `errCount > 0 && !strict && criticalErrCount == 0 && successCount > 0` (i.e., the new default suppressed what would have been a non-zero exit under the old contract), emit one final `sync_warning` to stderr with `reason: "exit_policy_default_changed"` and `message: "<N> resource(s) failed but exit code is 0 because the new default treats non-critical failures as warnings. Pass --strict to restore the old behavior, or annotate critical resources with x-critical: true. See CHANGELOG."`. Fires once per sync run (not per failure). Gives CI scripts that depend on `$? != 0` a discoverable in-band signal of the contract change.
  - Sync's `--json` output already includes `sync_warning` events for non-critical failures; the new `exit_policy_default_changed` reason joins that family.

  **Patterns to follow:**
  - Existing flag-declaration pattern in `newSyncCmd`.
  - Existing exit-code branch logic at sync.go.tmpl lines 240-246.

  **Test scenarios:**
  - Happy path: 5 resources sync successfully, no `x-critical` flags → exit 0, no warnings.
  - Partial failure non-strict: 4 resources succeed, 1 non-critical fails (404) → exit 0, `sync_warning` emitted, `sync_summary` shows `errored: 1`.
  - Partial failure strict: same as above with `--strict` → exit non-zero, same warnings.
  - Critical failure: 1 resource flagged `x-critical: true` fails → exit non-zero regardless of `--strict`.
  - Total failure: all 5 resources fail → exit non-zero, regardless of `--strict`.
  - Edge case: spec has zero resources marked critical, run with `--strict` → strict mode behaves as today (any failure = non-zero exit).
  - Edge case: spec marks ALL resources critical → critical-failure exit logic indistinguishable from `--strict` behavior. Documented as intentional.
  - Backward-compat: existing CI scripts that depend on "any sync failure = non-zero exit" run with `--strict` and behave as before.

  **Verification:**
  - `go test ./internal/generator/...` passes including new exit-policy cases.
  - `scripts/golden.sh verify` shows expected diffs in flag enumeration and exit-logic emitted code.
  - End-to-end: regenerate golden-api fixture, run sync against a mock with mixed critical/non-critical failures, observe exit codes match the policy table.

---

- U5. **F3: `sync.Mutex` on store write paths; eliminate `SQLITE_BUSY` at default concurrency**

  **Goal:** Add a `sync.Mutex` field to the generated store struct that wraps **every** write method called concurrently from sync — not just `UpsertBatch`. Concurrent fetcher goroutines in `sync.go.tmpl`'s worker pool continue to fetch in parallel; writes serialize through the mutex at the store layer. Eliminates `SQLITE_BUSY` events without requiring goroutine-pipeline complexity at the sync orchestrator.

  **Requirements:** R2, R5.

  **Dependencies:** None at the spec/profiler layer (this is pure store-template work). Lands SECOND per sequencing decision (revised) so U3 has a stable mutex base when it modifies the same `UpsertBatch` body.

  **Files:**
  - Modify: `internal/generator/templates/store.go.tmpl`:
    - Add `writeMu sync.Mutex` field to `Store` struct.
    - Wrap **every** of the following write-path functions with `s.writeMu.Lock(); defer s.writeMu.Unlock()` at the function entry:
      - `UpsertBatch` (lines 526-583)
      - `SaveSyncState` (~line 618) — called every page from concurrent worker goroutines via `sync.go.tmpl` lines 103, 122, 398, 422, 779
      - `SaveSyncCursor` (~line 641)
      - `ClearSyncCursors` (~line 698)
      - `Upsert` (single-object path)
      - Every typed `Upsert<Pascal>` (per-resource generated upsert; templated)
      - Any `Delete*` method that calls `s.db.Exec`
      - `migrate()` — called only once from `Open()` so contention is impossible, but lock for consistency so future callers don't accidentally race
    - `db.SetMaxOpenConns(2)` at line 44 stays as-is. Mutex gives stronger guarantee than relying on connection-count + WAL alone. Reads (`Get`, `List`, `Query`, `QueryRow`, `GetSyncCursor`, `ListIDs`) do NOT take the lock — they run concurrently against the WAL.
  - Test: `internal/generator/templates/store_upsert_batch_test.go.tmpl` — extend with a high-concurrency case (16 goroutines, mix of UpsertBatch + SaveSyncState + SaveSyncCursor calls) asserting zero `SQLITE_BUSY` errors.

  **Approach:**
  - Field addition at `Store` struct definition:
    ```go
    type Store struct {
        db      *sql.DB
        writeMu sync.Mutex
    }
    ```
  - Wrap pattern for every write method (uniform):
    ```go
    func (s *Store) <Method>(...) (...) {
        s.writeMu.Lock()
        defer s.writeMu.Unlock()
        // existing body unchanged
    }
    ```
  - Audit completeness check: grep `internal/generator/templates/store.go.tmpl` for `s.db.Exec` and `s.db.Begin` — every match must be inside a function whose body is mutex-wrapped. Missing one means SQLITE_BUSY recurs.
  - **Read-then-write sequences** (e.g., `GetSyncCursor` followed by `SaveSyncState`): rely on the resource-level concurrency invariant — the worker pool dispatches one resource per goroutine via `work := make(chan string, len(resources))` (sync.go.tmpl line 151). State this invariant in a code comment so future refactors don't silently violate it.

  **Patterns to follow:**
  - Existing `sync.Mutex` patterns elsewhere in the printing-press codebase if any (search; otherwise this is a precedent).
  - The existing `Store` struct shape (line ~30 of store.go.tmpl).

  **Test scenarios:**
  - Happy path: 16-goroutine concurrent `UpsertBatch` call with disjoint resource types → no `SQLITE_BUSY` errors, all rows persisted, total wall-clock time roughly equal to N × per-batch time (serialized).
  - Happy path: 1-goroutine sequential UpsertBatch call → identical behavior to pre-mutex baseline (mutex is uncontended).
  - Mixed reads + writes: 10 reader goroutines + 4 writer goroutines → readers proceed in parallel, writers serialize, no SQLITE_BUSY.
  - Edge case: a writer goroutine panics inside the locked section → `defer s.writeMu.Unlock()` releases the lock, subsequent writers proceed.
  - Edge case: deadlock check — no code path takes the mutex twice from the same goroutine. Audit explicit.

  **Execution note:** Run the high-concurrency test scenario before claiming the unit is done. The bug's signature is timing-dependent; missing the regression test would let the bug recur silently on the next concurrency change.

  **Verification:**
  - `go test -race ./internal/generator/...` passes including the new 16-goroutine case.
  - `scripts/golden.sh verify` shows the expected store.go.tmpl emission diffs.
  - End-to-end: regenerate a CLI from the golden fixture, run `<cli> sync --concurrency 16` against a multi-resource mock, observe zero `SQLITE_BUSY` events in `--json` output.

---

## System-Wide Impact

- **Interaction graph:** sync (`internal/cli/sync.go` in printed CLIs) calls store (`internal/store/store.go`). Other commands that write (e.g., printed CLI's `import`, `workflow`) hit the same store path — they inherit U5's mutex protection automatically.
- **Error propagation:** non-critical resource failures now propagate as `sync_warning` events with exit 0; critical failures or `--strict` runs preserve non-zero exit. JSON consumers that watch the `sync_summary` event count `errored` field already see the data; only the exit-code policy changes.
- **State lifecycle risks:** the mutex (U5) prevents partial-write races at the row level, but does not change transaction shape. `UpsertBatch` is still single-transaction-per-call; rollback semantics on error preserved.
- **API surface parity:** the `--strict` flag is new; `--max-pages` default change is the only existing-flag behavior change. Generated CLI `--help` output reflects both. SKILL.md may need a one-line note about exit-code policy if any skill prose references "sync exits non-zero on failure" — audit during implementation.
- **Integration coverage:** golden harness (`scripts/golden.sh`) is the cross-cutting safety net. Each unit produces template-emission diffs that the harness locks in. Miss-running golden after a template change is the most likely silent regression vector.
- **Unchanged invariants:**
  - Sync's `--json` output schema is unchanged except for new `sync_warning` cases (cap-hit, primary-key-unresolved). All existing event names and field shapes preserved.
  - Store reads (`Get`, `List`, `Query`) are not serialized; concurrent reads remain a hot-path optimization.
  - The cobra command tree of the printed CLI is unchanged at the `sync` and `store` boundaries — no command renames, no flag deprecations.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Backward compat: existing CIs depend on "any sync failure = non-zero exit." | `--strict` flag preserves the old behavior. **In-band signal:** the new default emits a `sync_warning` with `reason: "exit_policy_default_changed"` whenever it suppresses what would have been a non-zero exit, so CIs that watch sync output (vs. only exit code) discover the contract change. CHANGELOG entry + PR description + SKILL.md note. |
| Mutex serialization measurably slows large syncs. | Unlikely (writes aren't the bottleneck) but verifiable. **Tradeoffs explicitly accepted:** mutex loses (a) backpressure (slow disk doesn't push back on fetchers; large in-memory result accumulation possible if many fetchers complete concurrently and stall on the lock), (b) opportunistic write batching (each `UpsertBatch` call is its own transaction). If benchmarks show memory pressure or throughput degradation in real-world syncs, revisit single-writer goroutine pattern — deferred decision in Open Questions. |
| Removing kalshi-accreted fallback names breaks regen for kalshi-equivalent specs that lack `x-resource-id`. | **No deprecation needed in this case.** The user owns the kalshi CLI and will regenerate it with an `x-resource-id` annotation as part of landing this WU. No other public-library CLIs have specs that depend on the kalshi-specific fallback names. The runtime per-item `sync_anomaly` warning (`reason: "primary_key_unresolved"`) still fires loudly if any other unannotated spec tries to rely on those names. |
| `x-critical` semantics confuse users (default false changes existing exit behavior). | `--strict` flag is the escape hatch. **In-band default-flip signal** (see backward-compat row above) makes the change discoverable. Default `false` documented in CHANGELOG, `--help` text, and SKILL.md. |
| Golden harness churn — every unit produces diffs in `testdata/golden/expected/`. | Run `scripts/golden.sh verify` after each unit; explain diffs in PR description per AGENTS.md golden-test convention. |
| Single-writer mutex over-serializes when multiple workers process the same table at once. | Writes were already racing on the same table-level lock at the SQLite layer. Mutex moves the race to the Go layer where it's free of `SQLITE_BUSY`. Net latency unchanged for write-bound paths. |
| Mutex scope incomplete — `SaveSyncState`/`SaveSyncCursor`/`ClearSyncCursors` calls bypass the lock and SQLITE_BUSY recurs. | U5 enumerates every write method to wrap (Files section). Audit step requires grepping `s.db.Exec` and `s.db.Begin` for completeness. |
| Sticky-cursor APIs burn the full 1000-page budget every sync. | U2 adds a sticky-cursor detector — break + emit `sync_warning` with `reason: "stuck_pagination"` when `nextCursor` doesn't advance across pages. |
| F4b symptom (rows extracted but not stored) recurs silently. | U3 adds a `consumed > 0 && stored == 0 && extractFailures < consumed` probe that emits `sync_anomaly` with `reason: "stored_count_zero_after_extraction"`. Does not fix F4b's root cause (which remains under controlled-repro investigation) but ensures the symptom is visible the moment it recurs. |
| Identity drift: introducing `x-resource-id` and `x-critical` shifts positioning from "zero-config OpenAPI generation" toward "annotated-spec generation." | Acknowledged. Both extensions have functional fallback chains so unannotated specs still work. The catalog-spec retrofit question is deferred to a follow-up WU (see Scope Boundaries). |
| `--max-pages 1000` default not data-backed; could be too low or too high for catalog APIs. | Open Question deferred to implementation: pick value based on a quick survey of the largest paginated resource across `~/printing-press/library/`. The cap-hit `sync_warning` exists precisely so empirical tuning is feasible. |
| `x-resource-id` extension naming conflicts with future OpenAPI spec evolution. | Bare `x-` prefix is OpenAPI-spec-compliant; the extension is local to the printing-press parser. If conflict arises, rename in a future migration with a deprecation period. |

---

## Documentation / Operational Notes

- **CHANGELOG:** each unit adds a CHANGELOG entry under the `feat(cli):` or `fix(cli):` scope. The most user-visible changes are U2 (default `--max-pages` 10 → 1000) and U4 (exit-code policy + new `--strict` flag).
- **AGENTS.md:** the "Anti-reimplementation" section already covers commands that read from the local store; no change needed. The "Side-effect command convention" doesn't apply here.
- **SKILL.md:** if any skill prose claims "sync exits non-zero on any failure," update to "sync exits non-zero on critical-resource failure or with `--strict`." Audit during implementation.
- **Public library:** existing CLIs in `mvanhorn/printing-press-library` keep working with old behavior until next regen. Maintainers who want the new behavior re-run `/printing-press` or `/printing-press emboss`.

---

## Sources & References

- **GitHub issue:** [mvanhorn/cli-printing-press#421](https://github.com/mvanhorn/cli-printing-press/issues/421) — the retro that produced this WU
- **Retro doc:** `manuscripts/pokeapi/20260429-230641/proofs/20260430-000000-retro-pokeapi-pp-cli.md` (under `~/printing-press/manuscripts/`)
- **Prior retro for F2:** `docs/retros/2026-04-11-movie-goat-retro.md` (WU-5 added `--max-pages` flag)
- **Prior retro for F4a:** `docs/retros/2026-04-10-kalshi-retro.md` (WU-3 recommended profiler-driven primary key extraction)
- **Prior plan for F3:** `docs/plans/2026-04-12-002-fix-cross-cli-retro-remaining-findings-plan.md` (Unit 1, R3 — `MaxOpenConns(2) + WAL` with optional `writeMu`)
- **Anti-pattern reference:** `docs/retros/2026-04-11-movie-goat-retro.md` ("Async goroutine write-through timing") — synchronous write-through preferred
- **Public-library PR that surfaced the run:** [mvanhorn/printing-press-library#158](https://github.com/mvanhorn/printing-press-library/pull/158)
