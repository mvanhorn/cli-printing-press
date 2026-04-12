---
title: "fix: address 6 remaining cross-CLI retro findings (entity store, parent-child sync, deadlock, selection, swagger scorer, GraphQL templates)"
type: fix
status: active
date: 2026-04-12
origin: ~/printing-press/manuscripts/cross-cli-retro-20260412.md
---

# Fix Cross-CLI Retro Remaining Findings

**Target repo:** mvanhorn/cli-printing-press. All file paths are relative to that repo root.

## Overview

The cross-CLI retro (Slack, Trigger.dev, Linear) surfaced 12 findings. 6 were already addressed by per-CLI retro plans (usageErr, FTS5 DELETE, extractPageItems wrapper keys, config.go var, scorer internal YAML compat, GraphQL struct dedup). This plan covers the 6 that remain open, organized into three phases by complexity and dependency.

## Problem Frame

The Printing Press produces structurally correct CLIs (200+ commands across 3 test CLIs, zero structural fixes) but the data pipeline layer requires significant manual rework on every generation. The six remaining gaps cause: empty entity tables that transcendence commands query in vain, no template for the most common API data relationship (parent-child), a latent deadlock in every CLI's store, suboptimal resource selection for large APIs, missing Swagger 2.0 auth scoring, and no GraphQL-specific wire protocol templates.

## Requirements Trace

- R1. Low-gravity resources get typed columns and entity-specific Upsert so transcendence commands find data
- R2. APIs with parent-child endpoints (60-70% of APIs) get generated dependent-sync functions
- R3. Store's Query() method cannot deadlock when callers iterate rows and call other store methods
- R4. Resource selection for APIs with >100 endpoints prioritizes user-facing over admin endpoints
- R5. Scorecard auth_protocol scores Swagger 2.0 securityDefinitions correctly
- R6. GraphQL-sourced CLIs get a functional sync pipeline without hand-written client code

## Scope Boundaries

- Does NOT change any existing generated CLI in the library. These are Printing Press machine fixes.
- Does NOT add GraphQL subscriptions or real-time features (R6 covers queries and mutations only).
- Does NOT change the 500-resource cap number, only the selection strategy (R4).
- Does NOT change parent-child sync to handle arbitrary nesting depth, only one level of parent-child (R2).

## Context & Research

### Relevant Code and Patterns

- Entity table generation: `internal/generator/schema_builder.go` lines 52-60 (gravity >= 4 gate), lines 125-186 (`computeDataGravity`)
- Upsert dispatch: `internal/generator/templates/store.go.tmpl` lines 285-344, gated on `len(.Columns) > 3`
- Sync Upsert dispatch: `internal/generator/templates/sync.go.tmpl` lines 455-463, same `len(.Columns) > 3` gate
- MaxOpenConns: `store.go.tmpl` line 44 (`db.SetMaxOpenConns(1)`)
- Exposed Query method: `store.go.tmpl` line 503 (returns `*sql.Rows` to callers)
- Resource selection: `internal/openapi/parser.go` lines 911-915 (`sort.Strings(pathKeys)`), cap at line 964
- Parent-child exclusion: `internal/profiler/profiler.go` lines 246-251 (excludes paths with `{`)
- Swagger 2.0 scorer gap: `internal/pipeline/scorecard.go` lines 1113-1125 (only checks `components.securitySchemes`)
- GraphQL parser: `internal/graphql/parser.go` (produces spec.APISpec with all paths set to `/graphql`)
- FTS5Triggers: `schema_builder.go` line 88 (opt-in when all FTS fields are extracted columns)
- Existing GraphQL plan (parser-level, not templates): `docs/plans/2026-03-26-feat-graphql-api-support-plan.md`
- Related retro issues: #182 on cli-printing-press

### Institutional Learnings

- Kalshi retro: extractPageItems universal fallback shipped; expanding primary key detection shipped
- Trigger.dev retro: scorer internal YAML fix shipped, config.go fix shipped
- Linear retro: FTS5 fix shipped, GraphQL struct dedup shipped, but entire client/sync layer was hand-written
- Slack retro: per-channel message sync was 100+ lines manual, confirmed 60-70% of APIs need this pattern

## Key Technical Decisions

- **Lower gravity threshold from 4 to 2 (R1):** Gravity 2 means a resource has at least 2 endpoints OR 1 endpoint + some field richness. This is conservative enough to avoid emitting typed columns for truly trivial resources while capturing the mid-tier resources that Trigger.dev and Slack missed.
- **FTS5Triggers should default to true (R1):** The opt-in gate (`allFieldsAreColumns`) should become the default. When FTS fields are NOT extracted columns, fall back to manual sync with the already-fixed DELETE syntax. This eliminates the "FTS5 triggers are better but only for some resources" inconsistency.
- **Parent-child detection via path-parameter cross-referencing (R2):** When a path like `/channels/{channelId}/messages` exists, the profiler should detect that `channelId` references the `channels` resource and create a `DependentSync` entry. The sync template then emits a function that iterates parent records before fetching children.
- **MaxOpenConns(2) with WAL mode (R3):** WAL mode already allows concurrent reads. Changing from 1 to 2 connections means a caller can hold one query open while executing a second. This is the smallest change that eliminates the deadlock without complex transaction management. Write serialization is maintained by SQLite's WAL locking.
- **Resource selection scoring by path depth + tag deprioritization (R4):** Shorter paths (fewer `/` segments) score higher. Tags containing "admin", "internal", "system", "management" get a penalty. This is simple, effective, and doesn't require API-specific knowledge.
- **Swagger 2.0 securityDefinitions fallback (R5):** When `components.securitySchemes` is empty, check `securityDefinitions` (the Swagger 2.0 equivalent). Map `type: "apiKey"` and `type: "oauth2"` to the same scoring buckets as their OAS3 counterparts.
- **GraphQL client template with POST + query body (R6):** The SDL parser already produces a valid APISpec. The new template emits a GraphQL client that sends `POST /graphql` with `{"query": "...", "variables": {...}}` instead of REST GETs. Pagination uses Connection's `pageInfo.hasNextPage` + `endCursor` which the parser already detects.

## Open Questions

### Resolved During Planning

- **Should we lower gravity to 1?** No. Gravity 1 means a resource with a single GET endpoint and no field metadata. These are genuinely too thin for typed columns. Gravity 2 is the right floor.
- **Can we fix the deadlock without changing MaxOpenConns?** The alternative is refactoring Query() to return collected slices instead of cursors. This is a larger change that breaks the template's current API. MaxOpenConns(2) is safer and smaller.
- **Should resource selection use the profiler or the parser?** The parser, because selection happens before profiling. The profiler runs on the already-selected resources. Add a `pathPriorityScore()` function in parser.go.

### Deferred to Implementation

- Exact gravity scoring adjustments if threshold 2 proves too aggressive (may need to be 3)
- Whether GraphQL mutation sync is useful or only queries should sync
- Exact tag-deprioritization word list for resource selection scoring

## Phased Delivery

### Phase 1: Quick wins (small complexity, no dependencies)

Units 1-3 are independent. Can be done in parallel or any order.

### Phase 2: Store and sync improvements (medium complexity)

Units 4-5 depend on each other (parent-child sync needs entity-aware store).

### Phase 3: GraphQL templates (large complexity)

Unit 6 depends on Units 4 (entity store) being complete for the sync pipeline to work end-to-end.

## Implementation Units

### Phase 1: Quick Wins

- [ ] **Unit 1: MaxOpenConns safety (F7)**

**Goal:** Eliminate the latent deadlock when callers iterate Query() results and call other store methods.

**Requirements:** R3

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/store.go.tmpl`
- Test: `internal/generator/generator_test.go` (verify generated store compiles and MaxOpenConns is set correctly)

**Approach:**
- Change `db.SetMaxOpenConns(1)` to `db.SetMaxOpenConns(2)` at line 44
- Add a comment explaining why: "WAL mode + 2 connections allows one read cursor open while a second query executes (e.g., analytics commands calling helpers during row iteration). Writes are serialized by SQLite's WAL lock."
- Alternatively, if the risk of 2 concurrent writes is concerning, add `store.writeMu sync.Mutex` and wrap all write operations (Upsert, Delete, Exec) in `writeMu.Lock()/Unlock()`

**Patterns to follow:**
- Existing `db.SetMaxOpenConns(1)` at store.go.tmpl line 44
- WAL mode pragma at store.go.tmpl line 46

**Test scenarios:**
- Happy path: generated store compiles with MaxOpenConns(2). Existing tests still pass.
- Integration: a generated CLI's analytics command that calls a helper function while iterating query results does not deadlock (test by running a transcendence command against a populated SQLite database in the captured_test).
- Edge case: concurrent goroutines writing via Upsert do not produce "database is locked" errors (SQLite WAL handles this natively with 2 connections).

**Verification:**
- `go build ./...` passes on a generated CLI
- No "database is locked" or deadlock in existing test fixtures
- grep confirms MaxOpenConns(2) in generated output

---

- [ ] **Unit 2: Smarter resource selection (F8)**

**Goal:** Replace alphabetical path selection with scored selection so admin/internal endpoints don't crowd out user-facing endpoints on large APIs.

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/openapi/parser.go`
- Test: `internal/openapi/parser_test.go`

**Approach:**
- Add a `pathPriorityScore(path string, tags []string) int` function
- Scoring: base score = 100. Subtract 10 per path segment (depth penalty). Subtract 30 for tags containing "admin", "internal", "system", "management" (case-insensitive). Add 10 for tags containing "users", "projects", "items" or other high-value signals.
- Before the cap check at line 964, sort `pathKeys` by score descending (stable sort, alphabetical as tiebreaker) instead of alphabetically
- Keep the 500-resource cap unchanged

**Patterns to follow:**
- Existing `sort.Strings(pathKeys)` at line 915 (replace with `sort.SliceStable`)
- Tag extraction from the OpenAPI spec's path items

**Test scenarios:**
- Happy path: a spec with 600 paths including 100 admin.* paths selects the 500 non-admin paths first. Admin paths are the ones dropped by the cap.
- Edge case: a spec with < 500 paths includes all paths regardless of score (no change in behavior)
- Edge case: two paths with the same score are sorted alphabetically (stable sort preserves deterministic output)
- Error path: a path with no tags gets the base score minus depth penalty (still valid)

**Verification:**
- Test fixture with > 500 paths confirms admin endpoints are deprioritized
- Existing parser tests still pass (stable sort means small specs are unaffected)

---

- [ ] **Unit 3: Swagger 2.0 auth scoring (F10)**

**Goal:** Scorecard's auth_protocol dimension correctly evaluates Swagger 2.0 specs.

**Requirements:** R5

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/scorecard.go`
- Test: `internal/pipeline/scorecard_test.go`

**Approach:**
- In `loadOpenAPISpec` around line 1113, after checking `components.securitySchemes` (OAS3), add a fallback that checks `securityDefinitions` (Swagger 2.0)
- Map Swagger 2.0 security types to the same scoring buckets: `type: "apiKey"` maps to API key scheme, `type: "oauth2"` maps to OAuth2 scheme, `in: "header"` with `name: "Authorization"` maps to Bearer
- Set `info.SecuritySchemes` and `info.SecurityRequirements` from the Swagger 2.0 data so downstream scoring logic works unchanged

**Patterns to follow:**
- Existing OAS3 parsing at scorecard.go lines 1113-1125

**Test scenarios:**
- Happy path: a Swagger 2.0 spec with `securityDefinitions: { api_key: { type: apiKey, in: header, name: Authorization } }` scores 10/10 on auth_protocol
- Happy path: a Swagger 2.0 spec with `securityDefinitions: { oauth2: { type: oauth2, flow: accessCode } }` scores correctly for OAuth2
- Edge case: a spec with both OAS3 `components.securitySchemes` AND Swagger 2.0 `securityDefinitions` uses the OAS3 version (don't double-count)
- Edge case: a spec with no auth definitions at either level scores 0/10 (unchanged behavior)

**Verification:**
- Slack's Swagger 2.0 spec scores 10/10 on auth_protocol instead of 3/10
- Existing OAS3 test fixtures are unaffected

### Phase 2: Store and Sync Improvements

- [ ] **Unit 4: Lower gravity threshold + FTS5Triggers default (F1)**

**Goal:** More resources get typed columns and entity-specific Upsert. FTS5 triggers become the default mode.

**Requirements:** R1

**Dependencies:** None (but benefits from Unit 1's MaxOpenConns fix being in place)

**Files:**
- Modify: `internal/generator/schema_builder.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- Change gravity threshold from `>= 4` to `>= 2` at line 52
- In `computeDataGravity`, add `+2` for any resource that appears in `SyncableResources` (has a paginated list endpoint). This ensures every syncable resource gets typed columns.
- Change FTS5Triggers default: at line 88, set `table.FTS5Triggers = true` unconditionally. The manual-sync fallback path (already fixed for modernc.org/sqlite in commit 92074e6) handles the case where FTS fields aren't extracted columns.
- This means: after this change, every syncable resource gets a typed table with real columns, entity-specific Upsert, and content-linked FTS5 triggers.

**Patterns to follow:**
- Existing `computeDataGravity()` scoring at schema_builder.go lines 125-186
- Existing `FTS5Triggers` conditional at schema_builder.go line 88

**Test scenarios:**
- Happy path: a resource with 2 endpoints and text fields (gravity 3 under old rules, now >= 2) gets typed columns in the generated store
- Happy path: `SELECT count(*) FROM <entity>` returns records after sync (not all going to generic resources table)
- Edge case: a resource with gravity 1 (single GET, no fields) still uses the generic table (threshold excludes truly thin resources)
- Integration: generate a CLI from a test fixture, run sync, verify entity tables are populated. Verify FTS5 search returns results.
- Edge case: FTS5Triggers = true even when FTS fields are NOT extracted columns. The manual sync fallback (DELETE WHERE fts MATCH 'rowid:' || ?) still works correctly.

**Verification:**
- `go build ./...` passes on generated CLIs
- Test fixtures confirm entity tables get > 3 columns for syncable resources
- Existing captured_test fixtures still pass

---

- [ ] **Unit 5: Parent-child sync pattern (F4)**

**Goal:** APIs with parent-child endpoints (e.g., `/channels/{channelId}/messages`) get generated dependent-sync functions.

**Requirements:** R2

**Dependencies:** Unit 4 (entity-aware store must create typed tables for child entities)

**Files:**
- Modify: `internal/profiler/profiler.go`
- Modify: `internal/generator/schema_builder.go`
- Create: `internal/generator/templates/sync_dependent.go.tmpl` (or extend `sync.go.tmpl`)
- Test: `internal/profiler/profiler_test.go`
- Test: `internal/generator/generator_test.go`

**Approach:**
- **Profiler change:** Add a `DependentResource` struct to the profile: `{ Name, ParentResource, ParentIDParam, ChildPath, ChildPagination }`. In `classifySyncable()`, when a path contains `{<param>}` AND the param name matches a known resource name (e.g., `channelId` matches `channels`), create a DependentResource entry instead of excluding the path.
- **Schema change:** schema_builder creates a typed table for dependent resources with a `parent_id` column in addition to the standard columns.
- **Template change:** New template (or conditional block in sync.go.tmpl) emits a `syncDependentResource()` function. Shape: query parent table for all IDs, then for each parent ID, paginate the child endpoint, upserting with parent_id set. Rate-limit awareness: respect the global rate limiter between parent iterations.
- **One level only:** Does not handle grandchild relationships (messages -> reactions). That's a separate plan.

**Patterns to follow:**
- Existing `syncResource()` in sync.go.tmpl for the per-page-fetch-and-upsert loop
- Existing `SyncableResource` struct in profiler.go for the flat sync pattern
- Existing parent FK column pattern in schema_builder.go (if any)

**Test scenarios:**
- Happy path: a spec with `/channels` and `/channels/{channelId}/messages` generates a `syncDependentResource("messages", "channels", "channelId")` function. After syncing channels, messages are synced per channel.
- Happy path: `SELECT count(*) FROM messages WHERE parent_id IS NOT NULL` returns records after dependent sync.
- Edge case: a spec with only `/channels/{channelId}/messages` but no `/channels` list endpoint skips dependent sync (can't iterate parents without a parent list endpoint).
- Edge case: a parent with 10,000 records and 100 messages each handles pagination on the child endpoint correctly.
- Error path: if parent sync fails, dependent sync is skipped with a warning (not a hard failure).
- Integration: generate a CLI from a test fixture with parent-child endpoints, run sync, verify child records have correct parent_id values.

**Verification:**
- Test fixture with parent-child paths generates dependent sync code
- `go build ./...` passes
- Profiler test confirms DependentResource is populated correctly
- Generated CLI's sync command populates child tables with parent_id set

### Phase 3: GraphQL Templates

- [ ] **Unit 6: GraphQL sync/client templates (F11)**

**Goal:** CLIs generated from GraphQL SDL specs get a functional sync pipeline without hand-written client code.

**Requirements:** R6

**Dependencies:** Unit 4 (entity-aware store for typed tables), and architecturally benefits from Unit 5 (parent-child sync, since GraphQL APIs commonly have nested Connection types)

**Files:**
- Create: `internal/generator/templates/graphql_client.go.tmpl`
- Create: `internal/generator/templates/graphql_queries.go.tmpl`
- Create: `internal/generator/templates/graphql_sync.go.tmpl`
- Modify: `internal/generator/generator.go` (template selection based on spec source)
- Modify: `internal/graphql/parser.go` (enrich APISpec with query operation strings)
- Test: `internal/generator/generator_test.go`
- Test fixture: `testdata/graphql/` (SDL fixtures for testing)

**Approach:**
- **graphql_client.go.tmpl:** Emits a `GraphQLClient` struct with `Query(ctx, operationName, query, variables) (json.RawMessage, error)` and `Mutate(ctx, ...)`. All requests are `POST /graphql` with `Content-Type: application/json` body `{"query": "...", "variables": {...}}`. Auth injection via the existing `AuthHeader()` pattern.
- **graphql_queries.go.tmpl:** For each resource in the APISpec, emits a `const <Resource>Query = "query { <resource>(first: $first, after: $after) { nodes { ... } pageInfo { hasNextPage endCursor } } }"` string. Field selection is derived from the SDL parser's field list for each type.
- **graphql_sync.go.tmpl:** Like sync.go.tmpl but calls `client.Query(ctx, "<Resource>", <Resource>Query, map[string]any{"first": pageSize, "after": cursor})` instead of `client.Get(path, params)`. Pagination loop checks `pageInfo.hasNextPage` and updates `cursor` from `pageInfo.endCursor`.
- **Generator routing:** In generator.go, when `spec.Source == "graphql-sdl"`, use the GraphQL template set for client, queries, and sync instead of the REST templates. All other templates (store, root, commands, doctor, export, etc.) are shared.
- **Parser enrichment:** The GraphQL parser already produces field lists per type. Add the query string construction as a field on each Resource so the template can emit it directly.

**Patterns to follow:**
- Existing `client.go.tmpl` for REST client structure
- Existing `sync.go.tmpl` for the paginate-and-upsert loop
- Linear's hand-written `graphql.go` and `queries.go` (in `~/printing-press/library/linear/internal/`) as the reference implementation

**Test scenarios:**
- Happy path: `go build ./...` passes for a CLI generated from a GraphQL SDL test fixture without any hand-written client code
- Happy path: sync paginates through a Connection type using `first`/`after` variables and stores records in typed entity tables
- Edge case: a GraphQL type with no Connection pagination (single-object query) generates a non-paginated fetch
- Edge case: a mutation endpoint generates a command that sends `POST /graphql` with the mutation query (not a REST PUT/POST)
- Error path: GraphQL error response `{"errors": [...]}` is parsed and returned as a Go error, not silently swallowed
- Integration: generate a CLI from the Linear SDL (testdata fixture), run `go build`, verify sync.go uses GraphQL queries not REST GETs

**Verification:**
- New test fixture in `testdata/graphql/` with a small SDL schema
- `TestGenerateProjectsCompile` extended to include a GraphQL test case
- Generated CLI's sync command produces `POST /graphql` requests (not GET)
- `go build ./...` and `go vet ./...` pass

## System-Wide Impact

- **Interaction graph:** Units 1, 4, 5 change the generated store layer. Every future CLI's store will be different. Existing CLIs in the library are unaffected (they were already compiled).
- **Error propagation:** Parent-child sync (Unit 5) introduces a new failure mode: parent sync succeeds but child sync fails per-parent. The template should log the failed parent ID and continue to the next parent, not abort.
- **State lifecycle risks:** Lowering the gravity threshold (Unit 4) means more tables with typed columns. If a resource's response shape doesn't match the column expectations, the typed Upsert will fail. The generic-table fallback should catch this.
- **API surface parity:** Resource selection (Unit 2) changes which commands are generated for large APIs. This is intentional but means regenerating an existing CLI could produce different commands.
- **Integration coverage:** Unit 6 (GraphQL) is the highest-risk unit. The captured_test.go.tmpl pattern needs to work with GraphQL fixtures, not just REST fixtures.
- **Unchanged invariants:** The 500-resource cap number stays the same. Existing template shapes for REST CLIs are unchanged. The existing internal YAML spec format is unchanged. The profiler's API is additive only (new fields, no removed fields).

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Lowering gravity to 2 generates typed columns for resources with unreliable field metadata | Keep the generic-table fallback. If typed Upsert fails, fall back to generic silently. Monitor via the existing dogfood pipeline_check. |
| MaxOpenConns(2) introduces rare concurrent-write edge cases | SQLite WAL mode handles concurrent writes natively. Add a brief stress test to captured_test. |
| Parent-child sync multiplies API calls (N parents x M pages per parent) | Respect the CLI's existing --rate-limit flag. Add a stderr progress line: "syncing <child> for <parent> (N/M parents)" |
| GraphQL template produces wrong query syntax for edge-case schemas | Use Linear's hand-written client as the reference implementation. Add the Linear SDL as a test fixture. |
| Resource selection scoring is subjective | Keep it simple (depth + tag penalty). Don't try to be perfect. The fallback is that users can still write a custom internal YAML spec. |

## Sources & References

- **Origin document:** `~/printing-press/manuscripts/cross-cli-retro-20260412.md`
- Related issue: mvanhorn/cli-printing-press#182
- Related completed plans: `2026-04-08-001-fix-graphql-dedup-fts5-plan.md`, `2026-04-10-002-fix-kalshi-retro-findings-plan.md`
- Linear reference implementation: `~/printing-press/library/linear/internal/` (hand-written GraphQL client and sync)
- Slack reference implementation: `~/printing-press/library/slack/internal/` (hand-written per-channel message sync)
