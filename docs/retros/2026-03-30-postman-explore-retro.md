# Printing Press Retro: Postman Explore

## Session Stats
- API: Postman Explore (public API network directory)
- Spec source: Catalog (sniffed via crowd-sniff, proxy-envelope client pattern)
- Scorecard: 76 -> 85/100 Grade A
- Verify pass rate: 62% (9/24 failures are positional-arg commands the verifier can't test)
- Manual code edits: 7 (serviceForPath routing, sync rewrite, DB path, FTS table wiring, envelope unwrap, command restructuring, description rewrite)
- Features built from scratch: 17 (7 top-level commands + 10 transcendence features)
- Dead code removed: 3 functions (classifyDeleteError, firstNonEmpty, printOutputFiltered); 7 dogfood false positives
- Time to ship: ~45m

## Findings

### 1. Sync Hardcodes Cursor Pagination (assumption mismatch)
- **What happened:** Generated sync assumes cursor-based pagination (`after` param) but Postman uses offset-based (`offset` integer param). Teams endpoint has no pagination at all -- sync looped infinitely.
- **Root cause:** `internal/generator/templates/sync.go.tmpl` -- `determinePaginationDefaults()` hardcodes `cursorParam: "after"`. The spec already declares `Pagination.Type` (cursor/offset/page_token) per endpoint but the sync template ignores it.
- **Cross-API check:** Standard REST (Stripe): uses cursor -- current template works. Sniffed API (Postman): uses offset -- breaks. Different auth (GitHub): uses page/per_page -- also breaks. This hits any API not using cursor pagination.
- **Frequency:** Most APIs. Offset pagination is at least as common as cursor pagination. The spec struct already has `Pagination.Type` field -- the data is there, just unused.
- **Fallback if machine doesn't fix it:** Claude rewrites the entire sync command from scratch (high cost, ~15 min, error-prone -- the teams infinite loop proves it).
- **Tradeoff:** freq(3) x fallback(3) / effort(2) + risk(1) = 3.0. The spec already carries pagination metadata. The sync template just needs to branch on it. Low regression risk because cursor stays the default.
- **Inherent or fixable:** Fixable. The spec's `Pagination` struct has `Type`, `CursorParam`, `LimitParam`. The sync template should read these instead of hardcoding.
- **Durable fix:** In `sync.go.tmpl`, change `determinePaginationDefaults()` to read from the spec's pagination config. When `Type == "offset"`, use offset+limit loop. When no pagination exists on the endpoint, fetch once (no loop). When `Type == "cursor"`, use current logic.
- **Test:** Generate from postman-explore spec -> sync uses offset and terminates. Generate from Stripe-like spec -> sync still uses cursor. Generate for single-page endpoint -> no loop.
- **Evidence:** Complete sync.go rewrite required; teams endpoint looped infinitely before fix.

### 2. Proxy serviceForPath Ignores x-proxy-routes (bug)
- **What happened:** `serviceForPath()` was hardcoded to return the API slug ("postman-explore") for all paths. The proxy needs different service names per path prefix (publishing, search, notebook).
- **Root cause:** `internal/generator/templates/client.go.tmpl` -- the template already has conditional logic for `.ProxyRoutes` but the OpenAPI parser writes them to `spec.ProxyRoutes` while the generator's template context may not propagate them. The sniffed spec had routes in `x-proxy-routes` extension and the parser reads them (line 153-164 of `internal/openapi/parser.go`), but the generated output had a stub.
- **Cross-API check:** Standard REST: not affected (no proxy pattern). Sniffed proxy API: breaks every time. Different auth: irrelevant. Affects only proxy-envelope APIs.
- **Frequency:** API subclass: proxy-envelope. Currently only postman-explore in catalog, but the pattern exists in other sniffed APIs (e.g., any SPA with a backend proxy).
- **Fallback if machine doesn't fix it:** Claude makes a targeted edit to client.go (~5 min, medium cost). But if missed, CLI ships broken -- every API call goes to wrong service (critical).
- **Tradeoff:** freq(2) x fallback(4) / effort(1) + risk(0) = 8.0. The template already has the conditional -- this is a wiring fix.
- **Inherent or fixable:** Fixable. The template has `{{- if .ProxyRoutes}}` logic. Need to verify the spec's `proxy_routes` are being passed through to the template context.
- **Durable fix:** Verify the generator passes `spec.ProxyRoutes` to the client template context. Add a test: generate from a spec with `proxy_routes` and assert `serviceForPath` contains the route map.
  - **Condition:** `spec.ClientPattern == "proxy-envelope"` AND `spec.ProxyRoutes` is non-empty
  - **Guard:** Standard REST APIs skip this entirely (template already guards with `{{if eq .ClientPattern "proxy-envelope"}}`)
  - **Frequency estimate:** ~10% of catalog APIs, but 100% of proxy-envelope APIs
- **Test:** Generate from postman-explore spec -> `serviceForPath("/search-all")` returns "search". Generate from standard REST spec -> no serviceForPath function emitted.
- **Evidence:** First fix applied in session; manual edit to client.go.

### 3. Response Envelope Unwrapping Not Used in Sync (assumption mismatch)
- **What happened:** Categories and teams sync failed because responses are wrapped in `{"data": [...]}`. The generated sync tried direct array unmarshal.
- **Root cause:** `internal/generator/templates/sync.go.tmpl` -- `syncResource()` uses `extractPageItems()` which tries `"data"` as a wrapper key, but the custom per-resource sync functions in the final CLI (`syncCategories`, `syncTeams`) were written from scratch because the generic `syncResource` couldn't handle the entity-specific store methods.
- **Cross-API check:** Standard REST (Stripe): wraps in `{"data": [...]}` -- same problem. GitHub: returns direct arrays -- works. Most modern APIs use envelopes.
- **Frequency:** Most APIs. The `extractPageItems` helper already handles common wrapper keys. The issue is that the sync template's generic `syncResource` uses it, but the per-endpoint sync code doesn't.
- **Fallback if machine doesn't fix it:** Claude writes an `unwrapDataArray()` helper (~5 min). Medium cost but easy to forget.
- **Tradeoff:** freq(3) x fallback(2) / effort(1) + risk(0) = 6.0. The spec's `ResponsePath` field already exists and could be used.
- **Inherent or fixable:** Fixable. The spec's `Endpoint.ResponsePath` field (e.g., "data") tells the generator exactly where the array lives. The sync template should use it.
- **Durable fix:** When generating per-resource sync functions, use `Endpoint.ResponsePath` to emit unwrap code. If not set, fall through to the generic `extractPageItems` heuristic.
- **Test:** Generate from a spec with `response_path: "data"` -> sync unwraps correctly. Generate from a spec without it -> uses heuristic.
- **Evidence:** `unwrapDataArray()` written by hand; categories and teams failed on first sync attempt.

### 4. Top-Level Commands Not Generated (template gap)
- **What happened:** Generator produced nested commands (`api list-network-entities`, `search-all search_all`). 7 top-level user-facing commands (search, browse, categories, teams, stats, show, open) had to be written from scratch.
- **Root cause:** `internal/generator/templates/command_endpoint.go.tmpl` -- derives command names from OpenAPI operationIds and path segments. Produces API-internal structure, not user-facing CLI structure.
- **Cross-API check:** Standard REST (Stripe): `api list-charges` vs `charges list` -- same gap. Sniffed API: worse because operationIds are often auto-generated. Every API needs user-friendly top-level commands.
- **Frequency:** Every API. But the fix is complex -- mapping API paths to user-friendly command names requires heuristics that could produce bad names for some APIs.
- **Fallback if machine doesn't fix it:** Claude writes 5-10 command files from scratch (high cost, ~20 min). The absorb manifest defines the right names, but Claude has to implement each one.
- **Tradeoff:** freq(4) x fallback(3) / effort(3) + risk(2) = 2.4. High value but high implementation complexity. The heuristic for naming commands from path segments is brittle.
- **Inherent or fixable:** Partially fixable. The generator could emit top-level aliases for resources with a single entity type. For multi-entity endpoints (like networkentity with entityType enum), the mapping is API-specific. A reasonable middle ground: emit the `api` group as-is AND a set of resource-noun commands that delegate to the `api` commands.
- **Durable fix:** Add a "command promotion" pass: for each spec resource, if it has a list endpoint, emit a top-level command named after the resource (pluralized). For resources with entity-type enum params, emit one command per enum value. Keep `api *` as escape hatch.
- **Test:** Generate from postman-explore spec -> verify `browse`, `categories`, `teams` commands exist alongside `api list-*`.
- **Evidence:** 7 command files written from scratch.

### 5. Entity-Specific Store Methods Not Generated (missing scaffolding)
- **What happened:** Needed 24 store methods (UpsertCollectionBatch, UpsertCategoryBatch, UpsertTeamBatch, SearchCollections, etc.) and 3 entity-specific FTS tables. Generator emits generic `resources` table + `UpsertBatch`.
- **Root cause:** `internal/generator/schema_builder.go` already computes entity-specific tables with typed columns and FTS via `BuildSchema()`. `internal/generator/templates/store.go.tmpl` emits these tables in migrations. But the template only generates `Upsert<Entity>` for single-object inserts -- no batch upsert or entity-specific search methods.
- **Cross-API check:** Every API that uses sync + offline search needs batch upsert and entity-specific FTS queries. The schema builder already detects high-gravity entities.
- **Frequency:** Every API with store/sync. The schema is generated correctly; only the methods are missing.
- **Fallback if machine doesn't fix it:** Claude writes batch upsert and search methods per entity (high cost, ~20 min for 3+ entities). Error-prone -- the FTS table was wired wrong on first attempt (queried `resources_fts` instead of `collections_fts`).
- **Tradeoff:** freq(4) x fallback(3) / effort(2) + risk(1) = 4.0. The schema is already computed. Emitting methods for it is incremental.
- **Inherent or fixable:** Fixable. The `BuildSchema()` output already knows which tables have FTS. The store template should emit `UpsertBatch<Entity>`, `Search<Entity>`, and `Get<Entity>ByID` for each high-gravity table.
- **Durable fix:** Extend `store.go.tmpl` to iterate over `Tables` and emit: (1) `Upsert<Entity>Batch` for tables with >3 columns, (2) `Search<Entity>` for tables with FTS5, (3) `Get<Entity>ByID` for all entity tables. The template data already has `Tables` with `FTS5`, `FTS5Fields`, and `Columns`.
- **Test:** Generate a CLI -> verify `store.go` contains `UpsertCollectionBatch` and `SearchCollections` methods. Verify FTS queries use entity-specific FTS tables.
- **Evidence:** 24 store methods written by hand; offline search initially used wrong FTS table.

### 6. DB Path Inconsistency (default gap)
- **What happened:** `defaultDBPath()` in channel_workflow.go returned `~/.config/<cli>/store.db` but sync used `~/.local/share/<cli>/data.db`. Trending/leaderboard silently used empty database.
- **Root cause:** Two templates (`sync.go.tmpl` and `channel_workflow.go.tmpl`) independently define DB path defaults with different values.
- **Cross-API check:** Every API. Every CLI with store+workflow has this mismatch.
- **Frequency:** Every API. Both templates are always emitted when store vision is enabled.
- **Fallback if machine doesn't fix it:** Claude changes one line in one file (low cost, <1 min).
- **Tradeoff:** freq(4) x fallback(1) / effort(1) + risk(0) = 4.0. Trivial fix, no regression risk.
- **Inherent or fixable:** Fixable. Emit one `defaultDBPath()` in `helpers.go.tmpl` and reference it from both templates.
- **Durable fix:** Add `defaultDBPath()` to `helpers.go.tmpl`. Remove inline path construction from `sync.go.tmpl` and `channel_workflow.go.tmpl`. Route `data.db` through the data-kind resolver so the platform default remains `~/.local/share/<cli>/data.db` but relocation stays centralized.
- **Test:** Generate a CLI -> grep for `UserHomeDir` in all files -> only one definition of DB path.
- **Evidence:** Manual fix to channel_workflow.go; trending returned "No data" until path aligned.

### 7. Dead Code from Blanket Helper Emission (recurring friction)
- **What happened:** Dogfood flagged 10 dead functions; 3 truly dead (classifyDeleteError, firstNonEmpty, printOutputFiltered), 7 false positives from incomplete reference tracing.
- **Root cause:** `helpers.go.tmpl` emits all utility functions unconditionally. `classifyDeleteError` emitted even though spec has no DELETE endpoints. Dogfood's reference tracing misses cross-function calls.
- **Cross-API check:** Every API. Template emits full helper set regardless of which are needed.
- **Frequency:** Every API. Typically 3-5 truly dead functions per generation.
- **Fallback if machine doesn't fix it:** Claude deletes 3-5 functions (low cost, <2 min). Mechanical.
- **Tradeoff:** freq(4) x fallback(1) / effort(2) + risk(1) = 1.3. Low priority -- the fallback is cheap.
- **Inherent or fixable:** Partially inherent. Template-based generation will always over-emit. Two complementary fixes: (1) conditional emission for obvious cases (no DELETE -> no classifyDeleteError), (2) post-generation `polish` command that removes unreferenced functions.
- **Durable fix:** Short-term: add conditionals in `helpers.go.tmpl` for method-specific helpers. Long-term: `printing-press polish --dead-code` post-generation step. Fix dogfood false positive rate separately.
- **Test:** Generate from a spec with no DELETE endpoints -> `classifyDeleteError` not present. Run polish -> no dead functions remain.
- **Evidence:** 3 manual deletions.

### 8. Verify Can't Test Positional-Arg Commands (tool limitation)
- **What happened:** 9/24 commands reported as FAIL because verify runs `<cmd> --dry-run` without positional args. All 9 pass --help and work with correct args.
- **Root cause:** The verify tool doesn't parse cobra `Use` fields for `<arg>` patterns.
- **Cross-API check:** Every API with positional-arg commands (most CLIs).
- **Frequency:** Every API. Typically 30-40% of commands need positional args.
- **Fallback if machine doesn't fix it:** Verify pass rate is misleadingly low but not actionable -- no fix needed, just noise. Low cost.
- **Tradeoff:** freq(4) x fallback(1) / effort(2) + risk(0) = 2.0. Improves signal quality but no runtime impact.
- **Inherent or fixable:** Fixable. Parse `Use` field for `<...>` patterns. Extract placeholder values from `Example` field.
- **Durable fix:** In the verify tool, parse each command's `Use` field. Supply placeholder values from the first `Example` line or use "test"/"1" defaults.
- **Test:** Run verify on postman-explore -> all 24 commands pass.
- **Evidence:** 62% pass rate with 9 false failures.

## Prioritized Improvements

### Tier 1: Do Now
| # | Fix | Component | Frequency | Fallback Cost | Effort | Guards |
|---|-----|-----------|-----------|--------------|--------|--------|
| 6 | Emit single `defaultDBPath()` in helpers.go.tmpl | `internal/generator/templates/helpers.go.tmpl` | every | low (1-liner) | 1 hr | none needed |
| 2 | Read `x-proxy-routes` into `serviceForPath` | `internal/generator/templates/client.go.tmpl` | subclass:proxy-envelope | critical (broken API) | 2 hrs | `{{if .ProxyRoutes}}` already exists |
| 1 | Branch sync on `Pagination.Type` (offset/cursor/none) | `internal/generator/templates/sync.go.tmpl` | most | high (full rewrite) | 4 hrs | cursor stays default |

### Tier 2: Plan
| # | Fix | Component | Frequency | Fallback Cost | Effort | Guards |
|---|-----|-----------|-----------|--------------|--------|--------|
| 5 | Generate batch upsert + FTS search methods for entity tables | `internal/generator/templates/store.go.tmpl` | every | high (24+ methods) | 2 days | only for tables with gravity >= 6 |
| 3 | Use `ResponsePath` for envelope unwrapping in sync | `internal/generator/templates/sync.go.tmpl` | most | medium (helper) | 4 hrs | fallback to heuristic |
| 4 | Generate top-level command aliases from resources | `internal/generator/templates/` (new) | every | high (5-10 files) | 3 days | keep `api` group as escape hatch |

### Tier 3: Backlog
| # | Fix | Component | Frequency | Fallback Cost | Effort | Guards |
|---|-----|-----------|-----------|--------------|--------|--------|
| 7 | Conditional helper emission + polish command | `internal/generator/templates/helpers.go.tmpl` | every | low (delete 3 funcs) | 1 day | method-specific conditionals |
| 8 | Verify: parse Use field for positional args | verify tool | every | low (noise only) | 4 hrs | none |

## Work Units

### WU-1: Pagination-Aware Sync Generation (findings #1, #3)
- **Goal:** Generated sync command correctly handles offset, cursor, and single-page pagination without manual rewrite.
- **Target files:**
  - `internal/generator/templates/sync.go.tmpl` -- branch `determinePaginationDefaults()` on spec pagination type
  - `internal/generator/generator.go` -- pass per-resource pagination metadata to sync template
  - `internal/profiler/profiler.go` -- ensure pagination type is detected for each syncable resource
- **Acceptance criteria:**
  - Generate from postman-explore spec -> sync uses offset+limit for collections, no loop for teams, no loop for categories
  - Generate from a cursor-paginated API (e.g., Notion) -> sync uses cursor-based pagination (negative test)
  - Generate for an endpoint with no pagination -> sync fetches once (no infinite loop)
- **Scope boundary:** Does NOT include response envelope unwrapping (WU-2) or entity-specific store methods (WU-3). Pagination metadata flows from spec to sync template only.
- **Estimated effort:** 1 day

### WU-2: Response Envelope + Proxy Route Wiring (findings #2, #3)
- **Goal:** Generated client correctly routes proxy-envelope requests AND sync correctly unwraps response envelopes using spec metadata.
- **Target files:**
  - `internal/generator/templates/client.go.tmpl` -- verify ProxyRoutes data flows through
  - `internal/generator/templates/sync.go.tmpl` -- use `Endpoint.ResponsePath` for unwrapping
  - `internal/generator/generator.go` -- pass endpoint-level ResponsePath to sync template context
- **Acceptance criteria:**
  - Generate from postman-explore spec with proxy-envelope -> `serviceForPath("/search-all")` returns "search"
  - Generate from postman-explore spec -> sync unwraps `{"data": [...]}` without manual fix
  - Generate from standard REST spec -> no serviceForPath emitted, no unwrap code (negative test)
- **Scope boundary:** Does NOT include entity-specific sync functions -- this fixes the generic sync path only.
- **Estimated effort:** 1 day

### WU-3: Entity-Specific Store Method Generation (finding #5)
- **Goal:** For high-gravity entities, generate batch upsert and FTS search methods so Claude doesn't write 24 methods from scratch.
- **Target files:**
  - `internal/generator/templates/store.go.tmpl` -- add batch upsert, search, get-by-id method generation
  - `internal/generator/schema_builder.go` -- already computes the needed metadata; may need minor extensions
- **Acceptance criteria:**
  - Generate a CLI -> `store.go` contains `UpsertCollectionBatch` for high-gravity tables
  - Generate a CLI -> `store.go` contains `SearchCollections` using `collections_fts` (not `resources_fts`)
  - Generate from a minimal spec with low-gravity entities -> only generic `Upsert` methods (negative test)
- **Scope boundary:** Does NOT include command generation or sync integration -- just the store layer.
- **Dependencies:** None (store methods are independent of sync/pagination changes)
- **Estimated effort:** 2 days

### WU-4: DB Path Consolidation (finding #6)
- **Goal:** Single `defaultDBPath()` definition used by all store-consuming templates.
- **Target files:**
  - `internal/generator/templates/helpers.go.tmpl` -- add `defaultDBPath()`
  - `internal/generator/templates/sync.go.tmpl` -- remove inline path, call `defaultDBPath()`
  - `internal/generator/templates/channel_workflow.go.tmpl` -- remove inline path, call `defaultDBPath()`
- **Acceptance criteria:**
  - Generate a CLI -> `grep -r 'UserHomeDir' internal/cli/` finds exactly one definition
  - All store-consuming commands use the same path
- **Scope boundary:** Path consolidation only. Does not change the XDG-standard path choice.
- **Estimated effort:** 2 hours

## Anti-patterns

- **"The generic sync just works"** -- The generated sync hardcodes cursor pagination, doesn't unwrap response envelopes, and uses a single resource path. It needed a complete rewrite for Postman and will need significant edits for any non-cursor API. The spec already carries the pagination metadata -- the template just ignores it.
- **"Schema builder output = store completeness"** -- The schema builder correctly generates entity-specific tables with typed columns and FTS, but the store template only emits single-object upsert methods. Having the right schema without the right methods is half the job.
- **"Dead code is inherent to templates"** -- Some is, but emitting `classifyDeleteError` for an API with zero DELETE endpoints is not inherent -- it's a missing conditional.

## What the Machine Got Right

- **Proxy-envelope client pattern** -- Once serviceForPath was fixed, the entire proxy client (envelope wrapping, rate limiting, retry, dry-run, caching) worked flawlessly. The template is well-designed; the wiring just needed to propagate.
- **Adaptive rate limiter** -- Handled 429s during teams sync burst with zero manual intervention. The ramp-up/back-off logic is production-quality.
- **Schema builder gravity scoring** -- `BuildSchema()` correctly identified collections, categories, and teams as high-gravity entities and emitted typed columns with FTS. The data model was right; only the store methods were missing.
- **Output format infrastructure** -- `--json`, `--compact`, `--csv`, `--select`, `--agent` all worked from generation. The smart default (terminal=table, pipe=JSON) is the correct design.
- **Quality gate pipeline** -- 7/7 gates passed on first generation. The dogfood + verify + scorecard trio, despite individual limitations, caught every real issue.
- **Catalog metadata propagation** -- `client_pattern`, `spec_source`, `auth_required` from the catalog entry correctly influenced generation defaults (rate limiting, proxy client). This metadata channel works well.
