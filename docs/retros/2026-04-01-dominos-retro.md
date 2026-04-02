# Printing Press Retro: Domino's Pizza

## Session Stats
- API: Domino's Pizza (food ordering, delivery/carryout)
- Spec source: Sniffed (HAR from authenticated browser session) + community wrapper research (no official OpenAPI spec exists)
- Scorecard: 83 -> 85/100 (Grade A)
- Verify pass rate: N/A (verify cannot parse internal YAML spec format)
- Fix loops: 2
- Manual code edits: 5 (order commands MarkFlagRequired fix, stores example, README rewrite)
- Features built from scratch: 18 (cart, template, address, deals, rewards, menu search, compare-prices, nutrition, menu diff, track watch, analytics insight subcommands)

## Findings

### 1. Dogfood false-positives on intra-file function calls (Tool limitation)

- **What happened:** Dogfood flagged 12 functions as "dead" (defined, never called). All 12 are actually called by other functions within the same file (helpers.go). For example, `classifyAPIError()` calls `apiErr()`, `rateLimitErr()`, and `sanitizeErrorBody()`. Dogfood's static analysis only looks for call sites in OTHER files.
- **Root cause:** `internal/pipeline/dogfood.go` -- the dead function scanner likely uses a per-function grep that matches the definition but doesn't follow internal call chains within the same file.
- **Cross-API check:** This happens on every generated CLI. The helpers.go file always contains helper functions that call each other. The redfin retro also flagged dead helpers.
- **Frequency:** Every API
- **Fallback if machine doesn't fix it:** Claude has to manually verify each "dead" function, find it's a false positive, explain it to the user, and skip it. Claude catches it reliably but the false FAIL verdict confuses users and erodes trust in dogfood.
- **Worth a machine fix?** Yes. A FAIL verdict that's always wrong trains users to ignore dogfood entirely.
- **Inherent or fixable:** Fixable. The dead function scanner can be improved to follow call chains within the same file.
- **Durable fix:** In `internal/pipeline/dogfood.go`, when checking if a function is "dead," also search for call sites from OTHER functions defined in the same file. If function A calls function B, and A is itself called from outside, B is not dead. Alternatively, use `go vet` or `staticcheck`'s unused analysis which handles this correctly.
- **Test:** Generate a CLI with helpers that call each other (the default pattern). Dogfood should NOT flag them as dead. Negative test: a genuinely unused function (not called by anything) should still be flagged.
- **Evidence:** Dogfood reported 12 dead functions; grep confirmed all 12 have call-site references within helpers.go.

### 2. Dogfood false-positives on framework-level flag reads (Tool limitation)

- **What happened:** Dogfood flagged 6 flags as "dead" (declared, never read): agent, noCache, noInput, rateLimit, timeout, yes. All 6 are read -- `agent` sets other flags in root.go, `rateLimit` is passed to `client.New()`, `noCache` is checked in export.go, etc. Dogfood only looks for flag reads inside individual command RunE functions.
- **Root cause:** `internal/pipeline/dogfood.go` -- dead flag detection only scans RunE bodies, not root-level PreRun logic, client construction, or other commands.
- **Cross-API check:** These same 6 flags are emitted for every CLI. They are always read at the framework level, never inside individual RunE functions. This is a universal false positive.
- **Frequency:** Every API
- **Fallback if machine doesn't fix it:** Claude explains to every user that the FAIL verdict is wrong. Reliable but wasteful.
- **Worth a machine fix?** Yes. Same trust erosion issue as finding #1.
- **Inherent or fixable:** Fixable. Expand the scan to include root.go PreRunE, client construction, and other non-RunE code paths.
- **Durable fix:** In the dead flag scanner, also search for flag variable usage in root.go (especially the PreRunE block and client construction) and in files outside the command file. Any reference to the flag variable outside its declaration counts as "read."
- **Test:** Generate any CLI. Flags `agent`, `noCache`, `noInput`, `rateLimit`, `timeout`, `yes` should NOT be flagged as dead. Negative test: add a flag that is truly never referenced anywhere -- it should still be flagged.
- **Evidence:** grep confirmed all 6 flags are read in root.go, export.go, and track_watch.go.

### 3. Verify cannot parse internal YAML spec format (Tool limitation)

- **What happened:** `printing-press verify` failed with "validating parsed spec: at least one resource is required" when given the internal YAML spec, and "invalid character" when given the sniff-generated spec. Verify only accepts OpenAPI 3.0+ JSON/YAML.
- **Root cause:** `internal/cli/verify.go` -- spec loading uses the OpenAPI parser, not the internal spec parser. The internal YAML format (used by `printing-press generate`) has a different structure than OpenAPI.
- **Cross-API check:** This affects any CLI generated from a sniffed spec, crowd-sniffed spec, or internal YAML spec -- which is the majority of non-catalog APIs. Only CLIs generated from OpenAPI specs can run verify.
- **Frequency:** Most APIs (any without an OpenAPI spec)
- **Fallback if machine doesn't fix it:** Verify is skipped entirely. The CLI ships without runtime behavioral testing. This is the most expensive fallback -- verify catches real bugs that scorecard and dogfood miss.
- **Worth a machine fix?** Yes. Critical. Verify is the only tool that catches runtime command failures. Skipping it for non-OpenAPI specs means a large fraction of generated CLIs ship without behavioral testing.
- **Inherent or fixable:** Fixable. Verify should accept the internal YAML spec format (which the generator already parses successfully) in addition to OpenAPI.
- **Durable fix:** In `internal/cli/verify.go`, add a spec format detection step: if the file starts with `name:` and contains `resources:`, parse it with `internal/spec/` instead of `internal/openapi/`. Both parsers produce an API surface that verify can test against. Alternatively, add a `--spec-format` flag to verify: `--spec-format openapi|internal|auto`.
- **Test:** Generate a CLI from an internal YAML spec. Run `printing-press verify --dir <cli> --spec <internal.yaml>`. All commands should be tested. Negative test: an OpenAPI spec should still work as before.
- **Evidence:** `printing-press verify --dir . --spec dominos-combined-spec.yaml` → "validating parsed spec: at least one resource is required"

### 4. Generator emits `MarkFlagRequired("order")` on POST commands with `--stdin` alternative (Bug)

- **What happened:** All three order commands (validate, price, place) had `cmd.MarkFlagRequired("order")`, which prevented `--stdin` from working. Users had to pass both `--order` and `--stdin`, or got "required flag not set" errors.
- **Root cause:** `internal/generator/` -- when generating POST commands with body parameters, the generator marks the first body field as required without considering the `--stdin` escape hatch. The generator emits both `--<field>` and `--stdin` flags but makes the field required unconditionally.
- **Cross-API check:** This affects ANY generated CLI with POST/PUT/PATCH commands that accept JSON bodies. That's most APIs.
- **Frequency:** Most APIs (any with mutation endpoints)
- **Fallback if machine doesn't fix it:** Claude has to manually remove MarkFlagRequired from every POST command. Moderate reliability -- Claude might miss some if there are many POST commands.
- **Worth a machine fix?** Yes. The --stdin pattern is the primary way agents and scripts interact with mutation commands. Breaking it is a functional defect.
- **Inherent or fixable:** Fixable. When a command has both `--<field>` and `--stdin`, don't mark the field as required. Instead, add a validation in RunE: `if !stdinBody && bodyField == "" { return error }`.
- **Durable fix:** In the generator template for POST commands, replace:
  ```go
  _ = cmd.MarkFlagRequired("<field>")
  ```
  with a RunE guard:
  ```go
  if !stdinBody && body<Field> == "" {
      return fmt.Errorf("provide data via --%s or --stdin", "<field>")
  }
  ```
- **Test:** Generate a CLI for any API with POST endpoints. Run `echo '{}' | <cli> <cmd> --stdin --dry-run`. Should succeed without "required flag" errors. Negative test: running without --stdin or --<field> should show the error message.
- **Evidence:** `echo '{"Order":...}' | dominos-pp-cli orders validate_order --stdin` → "required flag(s) \"order\" not set"

### 5. Sniff spec captures infrastructure noise instead of API traffic (Recurring friction)

- **What happened:** The HAR capture contained 654 requests, but `printing-press sniff` generated a spec with Google Maps RPC endpoints, LaunchDarkly event endpoints, Next.js static data routes, and Adobe analytics -- not the actual Domino's API. The real API traffic was a single GraphQL endpoint (`/api/web-bff/graphql`) that sniff missed or couldn't properly decompose.
- **Root cause:** `printing-press sniff` (or the HAR parsing in `internal/pipeline/`) doesn't filter by the target domain aggressively enough. It also doesn't handle single-endpoint GraphQL APIs where all operations go to the same path.
- **Cross-API check:** Any modern SPA sniff will capture analytics, maps, CDN, and third-party traffic alongside API calls. Sites using GraphQL BFFs (increasingly common) will have a single endpoint that sniff can't decompose.
- **Frequency:** Most APIs sniffed from SPAs. GraphQL BFF is a growing pattern.
- **Fallback if machine doesn't fix it:** Claude manually writes a spec from HAR analysis + community research. High effort but Claude caught it this session.
- **Worth a machine fix?** Yes for the domain filtering. The GraphQL decomposition is harder but high value.
- **Inherent or fixable:** Partially fixable.
  - Domain filtering: fixable. Sniff should accept a `--domain` flag or auto-detect the target domain from the API name and filter aggressively.
  - GraphQL decomposition: fixable with effort. When sniff detects all XHR going to a single path with POST method and `operationName` in request bodies, it should extract each operation as a separate spec endpoint.
- **Durable fix:**
  1. Add `--domain` flag to `printing-press sniff`: `--domain order.dominos.com,www.dominos.com`. Only include HAR entries matching these domains.
  2. Add GraphQL detection: if >50% of API requests go to a single path with POST and the request bodies contain `operationName`, classify as GraphQL. Extract each unique `operationName` as a separate endpoint in the spec with its variables as parameters.
  - **Condition:** HAR contains POST requests to a single path with `operationName` in bodies
  - **Guard:** Standard REST APIs with diverse paths skip GraphQL extraction
  - **Frequency estimate:** ~20-30% of modern web apps use GraphQL BFFs
- **Test:** Sniff a HAR from a Next.js GraphQL app. Spec should contain one endpoint per GraphQL operation, not one endpoint for the single `/graphql` path. Negative test: REST API HAR should produce normal path-based endpoints.
- **Evidence:** `printing-press sniff --har sniff-capture.har` produced 18 endpoints across 6 resources, none of which were real API operations. The 24 GraphQL operations were only discovered by manual HAR parsing.

### 6. Generator emits generic analytics command without domain-specific insight (Template gap)

- **What happened:** The generated `analytics` command was a generic "count by type, group by field" utility. It scored 2/10 on insight because it had no domain-specific queries. Had to manually add `analytics summary`, `analytics popular`, and `analytics spending` subcommands.
- **Root cause:** `internal/generator/` -- the analytics template is static. It doesn't use the spec's resource types or the research brief to generate domain-relevant queries.
- **Cross-API check:** Every generated CLI gets the same generic analytics command. The insight score is consistently low unless Claude manually adds domain queries.
- **Frequency:** Every API
- **Fallback if machine doesn't fix it:** Claude builds custom analytics subcommands during Phase 3. Medium reliability -- Claude usually does it but the commands are inconsistent across CLIs.
- **Worth a machine fix?** Yes. The generator has access to the entity types and their fields. It could generate at least `summary` (table counts) and entity-specific queries automatically.
- **Inherent or fixable:** Partially fixable. The generator can emit:
  1. `analytics summary` that counts each entity table (derived from spec resources)
  2. Entity-specific "top N" queries when a field looks like a frequency candidate (e.g., status fields, category fields)
  The truly domain-specific insight ("popular orders," "spending trends") requires understanding the domain, which is Claude's job in Phase 3.
- **Durable fix:** In the analytics template, iterate over the spec's resources and generate:
  - `analytics summary` that queries each resource table for count + last synced
  - For resources with string fields, a `analytics <resource> top --field <field>` subcommand
  This gets insight from 2/10 to ~5/10 automatically. Claude's Phase 3 work raises it higher.
- **Test:** Generate a CLI for any API with 3+ resources. `analytics summary` should show counts for each. `analytics <resource> top --field status` should work. Negative test: none needed -- more analytics is always better.
- **Evidence:** Scorecard insight scored 2/10 both before and after polish. The generated analytics command had no domain awareness.

### 7. Entity-specific store tables must always be built manually (Missing scaffolding)

- **What happened:** The generator emitted a generic `resources` table with FTS5 and entity stubs (stores, menu, orders, tracking, auth). But the actual entity-specific tables (menu_items, toppings, carts, order_templates, deals, loyalty, addresses) with proper columns and typed methods had to be built entirely by hand (375 lines via Codex). This was the single largest manual task.
- **Root cause:** `internal/generator/` -- the store template uses a generic resource pattern. It doesn't derive table schemas from the spec's response types.
- **Cross-API check:** Every CLI needs entity-specific tables. The generator always emits generic tables. This was also flagged in the redfin retro.
- **Frequency:** Every API
- **Fallback if machine doesn't fix it:** Claude/Codex builds 300-500 lines of store code. Medium reliability -- the code works but column choices are inconsistent.
- **Worth a machine fix?** Yes. This is consistently the largest Phase 3 task. The spec contains response schemas (or can be inferred from endpoint descriptions) that would let the generator create typed tables.
- **Inherent or fixable:** Fixable. The spec's `response.item` types and the `body` field definitions contain the column types. The generator already uses these for command parameters -- it should also use them for store table DDL.
- **Durable fix:** In `internal/generator/`, when emitting the store template:
  1. For each resource in the spec, generate a `CREATE TABLE IF NOT EXISTS <resource>` with columns derived from the response fields.
  2. For resources with text fields, generate an FTS5 virtual table.
  3. Generate typed Upsert/Get/List/Search methods instead of generic ones.
  The internal YAML spec's `body` and `response` sections already have field names and types -- pipe them through.
- **Test:** Generate from a spec with 3+ resources, each with 3+ response fields. Each resource should get its own table with typed columns. Generic `resources` table remains as fallback. Negative test: a minimal spec with no response fields should still get the generic table.
- **Evidence:** 375 lines of store.go enhancements added manually via Codex (the single largest Phase 3 task).

### 8. README generated with placeholder examples (Template gap)

- **What happened:** The generated README used `dominos-pp-cli auth list` as the example throughout, including in the Output Formats section. The Quick Start said "Get your API key from your API provider's developer portal" -- generic text not specific to Domino's. README scored 5/10.
- **Root cause:** `internal/generator/readme_augment.go` -- README template uses the first command alphabetically for all examples, and the auth setup section is static.
- **Cross-API check:** Every generated CLI gets the same placeholder examples and generic auth text. README consistently scores 5/10 before manual rewrite.
- **Frequency:** Every API
- **Fallback if machine doesn't fix it:** Claude rewrites the README during polish. High reliability but 10-15 minutes of manual work.
- **Worth a machine fix?** Yes. The generator has access to all command names and can pick representative ones. The auth setup section can use the spec's auth config (env var names, auth type).
- **Inherent or fixable:** Fixable. The generator knows the commands, the auth type, the env var name, and the base URL. It can produce:
  1. Quick Start with the first GET command (not auth list)
  2. Auth setup referencing the actual env var from the spec
  3. Example sections using 3-4 different commands across different resources
- **Durable fix:** In `internal/generator/readme_augment.go`:
  1. Select example commands by resource variety (one from each resource group, prefer GET over POST)
  2. Use the spec's `auth.env_vars[0]` in the credential setup section
  3. Use the spec's `config.path` in the configuration section
  4. Replace "your API provider's developer portal" with context from the spec (or at minimum, the API name)
- **Test:** Generate a CLI from any spec with 3+ resources. README Quick Start should reference 3+ different commands. Auth section should mention the correct env var. Negative test: a spec with no auth should skip the auth setup section.
- **Evidence:** README scored 5/10 before rewrite. After manual rewrite with domain-specific examples, scored 9/10.

## Prioritized Improvements

### Do Now
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 4 | Don't MarkFlagRequired when --stdin exists | Generator templates | Most APIs | Medium | Small | Only for POST commands with --stdin |
| 1 | Fix dogfood intra-file dead function FPs | pipeline/dogfood.go | Every API | Always caught but wastes time | Small | None needed |
| 2 | Fix dogfood framework-level flag read FPs | pipeline/dogfood.go | Every API | Always caught but wastes time | Small | None needed |

### Do Next (needs design/planning)
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|
| 3 | Verify accepts internal YAML spec format | cli/verify.go + spec/ | Most APIs | Verify skipped entirely | Medium | Auto-detect format |
| 7 | Schema-driven entity store tables | Generator templates | Every API | Medium (Codex builds it) | Large | Spec must have response fields |
| 5 | Sniff domain filtering + GraphQL decomposition | pipeline/sniff | Most sniffed APIs | Medium (manual HAR analysis) | Large | GraphQL guard: only when single-path POST pattern |
| 6 | Domain-aware analytics template | Generator templates | Every API | Medium (Claude adds subcommands) | Medium | Requires resource list from spec |
| 8 | Context-aware README examples | generator/readme_augment | Every API | High (Claude rewrites) | Medium | None needed |

### Skip
| # | Fix | Why unlikely to recur |
|---|-----|----------------------|
| (none) | | All findings are cross-API patterns |

## Work Units

### WU-1: Fix dogfood false positives (findings #1, #2)
- **Goal:** Eliminate all false-positive FAIL verdicts from dogfood for standard generated CLIs
- **Target files:** `internal/pipeline/dogfood.go`
- **Acceptance criteria:**
  - Generate any CLI. Dogfood should not flag helpers that call each other within helpers.go.
  - Generate any CLI. Dogfood should not flag agent, noCache, noInput, rateLimit, timeout, yes as dead.
  - A genuinely unused function should still be flagged.
- **Scope boundary:** Does not change verify or scorecard. Does not change the generated code.
- **Complexity:** Small (1 file, straightforward logic change in dead-code scanner)

### WU-2: Fix MarkFlagRequired for --stdin commands (finding #4)
- **Goal:** Generated POST commands accept either --<field> or --stdin without requiring both
- **Target files:** `internal/generator/` (command template for POST endpoints)
- **Acceptance criteria:**
  - Generate a CLI with POST endpoints. `echo '{}' | <cli> <cmd> --stdin --dry-run` succeeds.
  - Running without --stdin or --<field> shows "provide data via --<field> or --stdin" error.
- **Scope boundary:** Only affects POST/PUT/PATCH commands with body parameters.
- **Complexity:** Small (1 template change)

### WU-3: Verify supports internal YAML spec format (finding #3)
- **Goal:** `printing-press verify` works with internal YAML specs, not just OpenAPI
- **Target files:** `internal/cli/verify.go`, potentially `internal/spec/spec.go`
- **Acceptance criteria:**
  - `printing-press verify --dir <cli> --spec <internal.yaml>` runs all commands.
  - `printing-press verify --dir <cli> --spec <openapi.yaml>` still works.
- **Scope boundary:** Does not change the spec format itself. Does not affect generate or dogfood.
- **Dependencies:** None
- **Complexity:** Medium (2-3 files, needs format detection + adapter pattern)

### WU-4: Schema-driven store generation (finding #7)
- **Goal:** Generator emits entity-specific SQLite tables with typed columns derived from spec response schemas
- **Target files:** `internal/generator/` (store template), `internal/spec/spec.go` (response field extraction)
- **Acceptance criteria:**
  - Generate from a spec with 3+ resources. Each resource gets its own table with typed columns.
  - FTS5 virtual table generated for resources with text fields.
  - Typed Upsert/Get/List methods generated (not generic).
  - Minimal spec with no response fields still generates generic fallback.
- **Scope boundary:** Does not change the CLI runtime. Only changes what the generator emits.
- **Dependencies:** None
- **Complexity:** Large (generator template changes + schema inference logic)

### WU-5: Sniff domain filtering and GraphQL decomposition (finding #5)
- **Goal:** `printing-press sniff` produces clean API specs by filtering noise and decomposing GraphQL
- **Target files:** `internal/pipeline/` (sniff logic), potentially `internal/cli/sniff.go`
- **Acceptance criteria:**
  - Sniff a HAR with mixed domains. Only target-domain requests appear in spec.
  - Sniff a HAR from a GraphQL app. Each `operationName` becomes a separate spec endpoint.
  - Sniff a REST API HAR. Normal path-based endpoints generated (GraphQL guard works).
- **Scope boundary:** Does not change the generator or the internal spec format.
- **Dependencies:** None
- **Complexity:** Large (domain filtering is small, GraphQL decomposition is medium-large)

## Anti-patterns

- **Trusting dogfood FAIL verdicts at face value.** In this session, every dogfood FAIL was a false positive. The correct response was to verify each finding, not to try to "fix" them. Future sessions should grep for actual usage before accepting dogfood's dead-code findings.
- **Using `printing-press sniff` output directly for generation when the target site is an SPA.** The sniff tool captures all traffic, not just API traffic. For SPAs, the HAR must be manually filtered before feeding to sniff, or the skill should instruct Claude to write a spec from the GraphQL operations rather than using the raw sniff output.

## What the Machine Got Right

- **Generator quality gates (7/7 pass).** The generated CLI built, ran, and passed all 7 quality gates on the first try. No build failures from generation.
- **Codex delegation.** The codex mode successfully delegated 4 large coding tasks (store enhancement, cart commands, deals/rewards, transcendence features). All 4 built on first try with no circuit breaker triggers.
- **Scorecard accuracy.** The scorecard correctly identified the real quality gaps (insight 2/10, README 5/10) and correctly scored the strengths (agent native 10/10, error handling 10/10). The 83->85 improvement tracked the actual fixes.
- **Live API reachability.** Store finder, menu, order validation, and order pricing all worked against the live Domino's API on the first try. The generated client correctly handled the `/power/` REST endpoints.
- **Auth detection.** The research phase correctly identified that Domino's read-only endpoints don't need auth, which saved time by skipping the API key gate for the main ordering flow.
