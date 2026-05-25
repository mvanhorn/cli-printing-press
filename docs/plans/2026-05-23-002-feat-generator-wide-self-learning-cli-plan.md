---
status: active
created: 2026-05-23
type: feat
plan_depth: deep
primary_repo: mvanhorn/cli-printing-press
secondary_repo: mvanhorn/printing-press-library
prediction_goat_baseline: ~/printing-press/.publish-repo-prediction-goat-80222e8b/docs/plans/2026-05-23-002-feat-prediction-goat-smart-learning-plan.md
portability_intent: lift the self-learning loop from prediction-goat into every printed CLI by default + retrofit existing library entries
---

# feat: Generator-wide self-learning CLI loop + library sweep

**Primary target repo:** `mvanhorn/cli-printing-press` (this repo).
**Secondary target repo:** `mvanhorn/printing-press-library` (sweep tool + per-CLI retrofit).
**All file paths in this plan are repo-relative.** When a path belongs to the library repo, the prefix `printing-press-library:` is used; everything else is the generator repo.

## Summary

Lift the self-learning CLI loop that just shipped in prediction-goat (a domain-agnostic `internal/learn/` package + per-CLI registration + entity-aware pattern extraction + auto-pre-seeding) into the printing-press generator so every printed CLI gets the loop by default, then sweep the 168 published library CLIs to retrofit the loop in place. After this work: every printed CLI ships with `teach`, `recall`, `learnings` commands, an additive SQLite schema for taught queries and extracted patterns, and a declarative `learn:` spec block that the CLI author fills out with ticker shapes, stopwords, and entity-lookup seeds. The loop is a benign no-op when the spec block is absent.

The loop's value comes from one observation: agents that hit search-shaped CLIs run the same expensive discovery walk every session. Caching the resource IDs they found (typing `kanye` once teaches the next session) and then generalizing those teaches through entity substitution (Portugal + USA + lookups table unlocks every country's odds) compresses the second-time-through cost to near zero. The same pattern applies to any CLI where the agent's path is "free-text query -> identify the right resource -> operate on it" -- which is most of them.

***

## Problem Frame

The prediction-goat CLI shipped two consecutive plans that built and validated a generalizing self-learning loop. The loop works: in dogfooding, after teaching the loop two World Cup queries (Portugal + USA), every other country's odds query (England, France, Brazil, Germany, Argentina, Spain, Mexico) returns the correct Kalshi ticker via pattern substitution against a 923-row entity\_lookups seed -- zero additional teaches, zero discovery walks.

Every other printed CLI in the public library faces the same agent-side pain prediction-goat just solved: the agent does the same expensive discovery walk in session N+1 that it did in session N. There is no machinery for "the CLI leaving notes for itself." The user's intent is explicit: build this into every future CLI made by the generator, and PR it onto every CLI already in the library.

The institutional precedent is to ship generator-side changes for future prints only and defer library retrofit to a separate plan (see Risk Analysis below). This plan respects that precedent by phasing the rollout: generator-side ship first, then pilot into 5 CLIs, then full library sweep -- with explicit gating between phases. The user can stop after any phase if the shape needs more time to bake on real traffic.

***

## Scope

## Per-CLI Applicability Audit

The 168 published CLIs classified by killer-flow shape, justifying the uniform-sweep decision and bounding the dead-code risk that adversarial review (ADV-2) flagged.

* **HIGH-value** = multi-step discovery in killer flow (free-text query -> identify resource -> operate). Learn loop compounds: second-time-through cost drops to near zero.

* **MED-value** = typed-endpoint heavy. Learn helps marginally on the occasional discovery path (find vehicle id, find service slug).

* **LOW-value** = pure-action, no plausible discovery surface. Loop ships as dead code unless author opts out via `.no-learn-sweep`.

### HIGH-value (\~145 CLIs)

The loop pays off across the entire library category by category, with rare exceptions called out below. Full lists per category:

* **ai** (3): openrouter, surgegraph, elevenlabs

* **commerce** (15): amazon-orders, amazon-seller, craigslist, ebay, facebook-marketplace, fedex, gumroad, harris-teeter, instacart, loopnet, shopify, slickdeals, squarespace, tiktok-shop, yahoo-finance

* **developer-tools** (21): all entries (apify, agent-capture, airframe, company-goat, docker-hub, domain-goat, firecrawl, namecheap, nse-india, nvd, openfda, openipa, pdok-location, posthog, postman-explore, pypi, scrape-creators, sec-edgar, supabase, trigger-dev, uspto-tsdr)

* **devices** (4 of 5): dreo, hayward-omnilogic, tesla, whoop

* **education** (1): lawhub

* **food-and-dining** (8 of 11): allrecipes, anylist, coffee-goat, food52, jimmy-johns, recipe-goat, rappi, table-reservation-goat

* **marketing** (15): all entries

* **media-and-entertainment** (25): all entries; this category is search-shaped end to end

* **monitoring** (1): sentry

* **other** (12 of 14): apartments, arxiv, edgar, numista, open-meteo, openalex, pcgs, pvgis, redfin, ufo-goat, usgs-earthquakes, weather-goat

* **payments** (7 of 8): coingecko, kalshi, lunch-money, mercury, monarch-money, pop, stripe

* **productivity** (22): all entries

* **project-management** (3): all entries (clickup, jira, linear)

* **sales-and-crm** (7): all entries

* **social-and-messaging** (3 of 5): multimail, twilio, x-twitter

* **travel** (8): all entries

### MED-value (\~15 CLIs)

Typed-endpoint heavy. Learn helps when the agent searches for an entity (a Cloud Run service, a Tesla vehicle, a pre-defined order), but the killer flow runs on known IDs more often than free-text queries:

* **cloud** (4): cf-domain, cloud-run-admin, digitalocean, render

* **devices**: adminbyrequest

* **food-and-dining**: dominos, ordertogo, pagliacci

* **other**: american-reindustrialization, greatclips

* **payments**: exchangerate-api

* **social-and-messaging**: bird, pushover

### LOW-value (\~5 CLIs)

Pure-action, no plausible discovery surface. The learn package compiles as dead weight unless the maintainer adds `.no-learn-sweep`:

* **commerce**: dominos-pp-cli (stray binary, already excluded by sweep manifest check)

* **media-and-entertainment**: archive-is (single-URL archive action)

### Implications for the sweep

The HIGH-value bucket is roughly 86% of the library (\~145 of 168). The user's "uniform across all CLIs" decision at Phase 0.7 covers this directly -- the dead-code risk that adversarial review flagged applies to \~20 CLIs (MED + LOW), not the 100+ implied by the original framing. Failure Mode C in the Rollback Playbook targets exactly those \~20 if friction surfaces post-Phase-3.

Per-CLI overrides: a CLI author who reads this audit and disagrees with their classification can preempt the sweep by adding `.no-learn-sweep` (LOW->skip) or by leaving `learn.enabled: false` on the spec (MED->skip emission). The audit is a default; the marker is the override.

***

### In scope

* A domain-agnostic `internal/learn/` package emitted into every printed CLI, structurally mirroring the prediction-goat package but with no prediction-market identifiers in core code

* A declarative `learn:` spec block on `APISpec` that carries ticker patterns, stopwords, and entity-lookup seeds, validated at parse time and emitted as registration code in `internal/cli/root.go`

* A `LearnConfig` struct in `internal/spec/spec.go` alongside `CacheConfig` and `ShareConfig`

* Six commands registered conditionally in `internal/cli/root.go`: three core (`teach`, `recall`, `learnings`), two `learnings` subcommands (`learnings list`, `learnings forget`), and two advanced top-level (`teach-pattern`, `teach-lookup`)

* An additive store schema migration (`search_learnings`, `search_patterns`, `entity_lookups`, `teach_log`) with `CREATE TABLE IF NOT EXISTS` only -- existing user data must be untouched

* Updates to `skill.md.tmpl` and `readme.md.tmpl` documenting the loop and the four-branch automatic-learning protocol

* A new generator-reserved internal package (`internal/learn/`) added to `internal/pipeline/internal_packages.go` and to AGENTS.md

* Scorecard awareness in `internal/pipeline/scorecard.go` (modest tier-1 credit for learn presence)

* A new golden test fixture (`generate-learn-loop-api/`) proving the deterministic emission shape

* A new sweep tool in `printing-press-library:tools/sweep-learn-install/` that idempotently retrofits the loop into existing library entries via AST surgery on `internal/cli/root.go` + drop-in of `internal/learn/*.go` + extension of `internal/store/store.go`'s migration slice

* A pilot sweep across 5 high-value CLIs (espn, contact-goat, bugbounty-goat, instacart, podcast-goat) with verification gates before proceeding to the full sweep

* A full sweep across the remaining 163 library CLIs (skipping the dominos stray binary and any CLI lacking `.printing-press.json`)

* Cross-repo documentation in both AGENTS.md files: the cross-repo dependency rule (template change here + sweep change there), the new reserved namespace, the additive-only schema rule, and the opt-out marker

### Deferred for later

* **Typed-endpoint call memoization** (caching arg combinations against natural-language phrasing, e.g. "Trevin's recent issues" -> `linear issues list --user trevin --sort updated`). The user picked discovery-only for v1. This is a different conceptual shape that warrants its own plan after the discovery loop has 4-6 weeks of live traffic.

* **Cross-CLI sync of learnings** (teaching contact-goat about "Trevin" benefits espn). Local-only learnings stay local; cross-CLI sync would change the trust model and is a separate plan.

* **Cross-machine sync of learnings**. Same reasoning.

* **MCP** **`intents:`** **block integration** -- the existing recipe-intents machinery composes endpoint calls; the new learn package's patterns are entity-substitution templates. Wiring patterns into intent-compose is a follow-up.

* **Re-printing each library CLI from scratch** instead of in-place sweep. Per AGENTS.md, fresh reprints don't happen from the library repo; the sweep tool is the established channel.

### Outside this product's identity

* A networked teach-aggregation service (uploads, telemetry). The loop is local-only by design. This protects user privacy, keeps the install zero-config, and matches the existing "no network" stance of `feedback`.

* Any change that makes a recall miss block the underlying command. Best-effort always, never fatal.

* Any change to existing user data in already-running library installs. Additive-only.

### Deferred to Follow-Up Work

* **Auto-prune of stale patterns/preseed rows** -- in the prediction-goat install I left a TODO for "patterns that consistently fail Apply verification should age out." Not blocking v1.

* **Pattern extraction into a shared** **`pkg/learnpatterns`** **library** -- if 30+ CLIs adopt and we see cross-CLI duplication of pattern definitions, factor out. Premature now.

* **A** **`learn:`** **block linter** (catches spec authors who declare a ticker pattern that doesn't match any of their resources). Useful but not blocking.

* **A** **`learnings sync-from-source`** **command** that pre-populates entity\_lookups from a CLI's `sync` data (e.g., espn could auto-seed all NFL/NBA team rosters from its synced data). Cleaner than hand-curated seeds but adds complexity.

* **`select_paths.go`** **codegen primitive** -- the AST walker emits per-command dotted-path cheatsheets for `agent-context`. Lifted from prediction-goat's `tools/select-paths-gen/main.go`. Unrelated to the learn loop; bundle into its own follow-up plan so this plan stays focused.

* **Resource-type auto-detect from** **`spec.resources[].id_pattern`** -- removes the foot-gun where teaching without `--resource-type` ungroups Kalshi vs Polymarket IDs. Real ergonomic win but a separate concern from the loop itself. Add as a follow-up after the loop is shipping in earnest.

* **`docs/PATTERNS.md`** **"self-learning loop pattern" section** -- documentation cleanup, not blocking.

* **`library-sweep-status.md`** **tracker** -- the sweep tool's own summary output is the natural runtime artifact; a tracked status doc is bookkeeping outside this plan's scope.

***

## Requirements

Tracing back to the user's request and the prediction-goat baseline plan's `portability_intent`:

* **R1.** Every printed CLI gets `internal/learn/`, `teach`/`recall`/`learnings` commands, and the learn schema by default.

* **R2.** Two modes:

  * `learn.enabled: false` (or block absent): generator skips emission of the learn package and commands entirely. No binary-size cost, no MCP tool noise.

  * `learn.enabled: true` with empty `entity_lookup_seeds` / `ticker_patterns`: package emits and compiles cleanly. Recall returns `{found: false}`. Teach works but pattern extraction has nothing to generalize from. Soft-warning logged once on startup naming the empty fields.

* **R3.** Per-CLI domain config (ticker patterns, stopwords, entity-lookup seeds) is declarative in the spec YAML, not hand-edited in generated Go.

* **R4.** The CLI's existing user data (data.db, profiles, cache) is untouched by the sweep. Additive-only schema migrations.

* **R5.** Existing library customizations (entries with custom `internal/cli/root.go` shape, with patches.json entries that touch store.go) are detected and either upgraded cleanly or reported for manual review -- never silently broken.

* **R6.** No domain identifiers (`kalshi_`, `polymarket_`, `country_iso2`) appear in `internal/learn/` or any of its templates. Verified by CI grep.

* **R7.** `recall` and `learnings list` are MCP-exposed as `mcp:read-only`. `teach`, `learnings forget`, `teach-pattern`, `teach-lookup` are MCP-exposed without the read-only annotation (they write the local store).

* **R8.** Cross-repo dependency rule extended in both AGENTS.md files: a learn-template change in the generator requires a paired sweep-tool change in the library, or a tracking issue.

* **R9.** Phased rollout is explicit. Phase 1 (generator) does not require Phase 2 (pilot) to ship. Phase 2 does not require Phase 3 (full sweep). The user can stop after any phase.

* **R10.** Phase 2 ships with quantitative stop thresholds (false-positive rate, transferability test). Phase 3 ships only when all pilot CLIs pass thresholds. See Rollback Playbook + Phased Delivery.

***

## Key Technical Decisions

### Naming: `internal/learn/patterns/` not `recipes/`

The cli-printing-press repo already has three meanings of "recipe":

1. `internal/generator/recipe_intents.go` -- MCP intent composition (build-time spec-driven endpoint composition).
2. `internal/pipeline/research.go:Recipe` -- worked SKILL.md examples.
3. `internal/pipeline/learnings.go` -- the printing-press machine's own retro DB (different concept entirely, but the word "learnings" overlaps).

The prediction-goat package used `internal/learn/recipes/` for entity-substitution templates. To avoid a fourth meaning of "recipe" in the same codebase, the generator's emitted package renames to `internal/learn/patterns/`. The functionality is identical; only the package name (and `Recipe` -> `Pattern` struct rename) changes. Document the rename in the learn package's `doc.go` so future readers can follow the lineage back to the prediction-goat plan.

### Spec block follows the `cache:` / `share:` precedent (manifest-wins)

`CacheConfig` (in `internal/spec/spec.go` near line 1184) and `ShareConfig` (near line 1206) are the canonical precedents for a feature-gated spec block. The new `LearnConfig` struct sits alongside them with the same shape (Enabled bool, plus typed fields for the per-CLI domain). Validated by a `validateLearn` helper called from the same path that runs `validateCacheShare`.

Per the institutional learning `manifest-wins-over-re-derivation-for-identity-fields-in-regen-paths-2026-05-12.md`: persist the resolved learn config into `.printing-press.json` so regen reads the manifest first and only falls back to the spec block when the manifest is silent. This prevents spec-block edits from silently flipping behavior on a future reprint.

### Config-per-call layering: refactor the lift source, do NOT keep the singleton

The prediction-goat baseline uses a package-level `DefaultPredictionGoatConfig()` singleton called directly from `recall.go`, `teach.go`, `normalize.go`, and even from inside a store migration (`internal/store/store.go:1390`). The singleton pattern hides config plumbing but creates two problems for the generator lift:

1. The singleton runs before any `root.go` init shim has a chance to register spec-derived patterns/stopwords. Specifically, `store.Open()` is called early in cobra setup and triggers the singleton (via the migration backfill path) before `initLearn()` runs.
2. The source has `store -> learn` import direction (the migration reads `learn.NormalizedQuery` and calls the config helper), the opposite of what the plan needs for "learn rides the store carve-out."

Resolution: the lift takes `*Config` as an explicit parameter through every call site. `learn.Recall(ctx, store, cfg, query)`, `learn.Teach(ctx, store, cfg, query, resources)`, `entities.Extract(query, cfg)`, etc. The store-side migration that currently calls the config getter is rewritten to either (a) defer until cfg is available, or (b) move out of the store package entirely. Net effect:

* `learn` package has no singleton; pure functions threaded with `*Config`

* `store` no longer imports `learn` (cycle eliminated)

* `learn` package helpers either use raw `*sql.DB` (and get a new exemption class in `reimplementation_check.go`) OR call into `store.Get*`/`store.Upsert*` (riding the store carve-out)

* Decision between those two sub-options happens during implementation; both work, choose by friction

This is bigger than a "near-verbatim port" of U3-U5 -- expect to rewrite the call-site shape for every public function in the lift source. The implementer should plan for \~30% more time on U3-U5 than a verbatim port would take.

### Soft validation in the reusable package

Per institutional learning `soft-validation-in-reusable-library-packages-2026-05-06.md`: missing `learn:` config warns and degrades; never fatal. Specifically:

* If spec has no `learn:` block, the package still compiles and recall returns `{found: false, reason: "no resources declared"}`. Teach refuses with a clear error.

* If spec has `learn:` but empty entity-lookup seeds, recall + teach work but pattern extraction returns no candidates (no entities to substitute).

* If a registered ticker regex fails to compile at runtime, log a one-line warning to stderr (suppressed in agent mode) and continue without it -- don't panic.

### Opt-out marker for hand-customized CLIs

The sweep tool checks for `library/<cat>/<slug>/.no-learn-sweep` (a marker file) before installing. CLIs that have hand-rolled their own search-cache (or that genuinely don't benefit, like pure-action CLIs) can opt out by adding the marker. Documented in the sweep tool's README.

### Sweep mechanism: regen-merge primitives + a new tool, not `tools/sweep-canonical/`

Per institutional learning `regen-merge-stage-and-swap-with-recovery`: Go-source surgery goes through the regen-merge transactional model (sibling tempdir -> atomic rename -> .bak-<ts> until commit). The existing `tools/sweep-canonical/` is for SKILL.md / README.md / agentcookie.toml only. A new tool at `printing-press-library:tools/sweep-learn-install/` does the Go-source work, mirroring `sweep-canonical/`'s GOPATH-mode + idempotency-tested shape but using regen-merge primitives under the hood.

### Don't write `.printing-press-patches.json` entries for the learn install

Per institutional learning: the patches manifest is for hand-edits that diverge from generator output, not for generator-owned packages. The learn package is owned by the generator (reserved namespace), so its presence in a library CLI is canonical, not patch-worthy. The sweep tool does NOT write patches.json entries for the install.

### Phased rollout to honor the institutional precedent

Every prior generator-side feature explicitly deferred library retrofit. The user's intent is full uniform rollout but the precedent says staged. This plan compromises: generator-side ship first (Phase 1), pilot sweep into 5 high-traffic CLIs (Phase 2), then full sweep (Phase 3). Each phase has an explicit checkpoint gate where the user can stop -- but the destination is full library-wide rollout, not "ship to one CLI and stop."

### Domain words prohibition enforced by CI grep

Per the prediction-goat plan's R10 generalization rule. Add a CI step (existing `verify-library-conventions.yml` or a new `verify-learn-purity.sh`) that greps `internal/generator/templates/learn*.go.tmpl` for the strings `kalshi`, `polymarket`, `prediction_goat`, `country_iso2`, etc. and fails the build if any appear. The check is cheap and catches accidental contamination.

### Agent-native exposure annotations

Per AGENTS.md "Tool safety annotations" section:

* `recall` -> `mcp:read-only=true` (reads local store, no API calls)

* `learnings list` -> `mcp:read-only=true`

* `learnings forget` -> NO annotation (destructive local-write)

* `teach`, `teach-pattern`, `teach-lookup` -> NO annotation (writes local store; downstream pattern extraction is a read+write)

`recall` carries `readOnlyHint: true` + `openWorldHint: false` (purely local). The local-only stance is the key safety claim.

### Skip-list mirrors auto-refresh

Per the granola auto-refresh plan: framework commands that should never trigger recall include `auth`, `doctor`, `help`, `sync`, `profile`, `feedback`, `which`, `agent-context`, `completion`, `version`. Wired into `PersistentPreRunE` via the same skip-list approach.

***

## High-Level Technical Design

This section illustrates the intended approach for review. The implementing agent should treat it as context, not code to reproduce.

### Spec block shape

```yaml
# In the CLI's spec YAML
name: example
# ... other fields ...
learn:
  enabled: true
  # Optional: regexes that identify CLI-specific tickers/slugs (matched
  # as Entities-of-type-Ticker in the extractor, kept distinct from
  # capitalized country/person names)
  ticker_patterns:
    - "^KX[A-Z0-9]+(-[A-Z0-9]+)*$"   # e.g., Kalshi
    - "^will-[a-z0-9-]+$"             # e.g., Polymarket slugs
  # Optional: domain stopwords on top of the default English filler set
  stopwords:
    - odds
    - wins
    - losing
  # Optional: named entity-lookup seed sets. Each seed has a kind
  # (the bucket name) and a list of canonical -> aliases mappings.
  entity_lookup_seeds:
    country_iso2:
      - canonical: US
        aliases: [USA, United States, America]
      - canonical: PT
        aliases: [Portugal]
    nfl_team:
      - canonical: SF
        aliases: [49ers, San Francisco, Forty Niners]
```

### Per-CLI emission flow

```
spec.yaml (LearnConfig)
        |
        v
generator parses LearnConfig -> APISpec.Learn
        |
        v
renderLearnFiles() emits:
  - internal/learn/{config,extract,normalize,match,recall,teach,preseed,...}.go
  - internal/learn/patterns/{store,extract,apply}.go
  - internal/learn/lookups/{store,seeds}.go
  - internal/cli/teach.go (combined commands)
        |
        v
root.go.tmpl conditionally registers teach/recall/learnings under {{if .Spec.Learn.Enabled}}
        |
        v
root.go.tmpl emits learn config init at startup:
  cfg := learn.NewConfig()
  cfg.RegisterTickerPattern(...)
  cfg.RegisterStopwords(...)
  lookups.SeedFromSpec(...)
  resourceTypeDetector.Register(...) // from spec.resources[].id_pattern
        |
        v
store.go.tmpl extends migrations slice with version-gated learn tables
```

### The recall/teach loop at runtime (unchanged from prediction-goat)

```
agent issues command
        |
        v
PersistentPreRunE checks skip-list
        |
        not skipped
        v
learn.Recall(query) -> hit? -> use cached resource IDs -> live-fetch -> return
        |
        miss
        v
command runs normal discovery walk
        |
        v
on success: learn.Teach(query, resources) writes async to teach.log + DB
        |
        v
pattern extractor runs every N teaches: finds structurally similar groups,
emits pattern -> entity_lookup substitutions, verifies against resources table
```

### Sweep tool flow

```
tools/sweep-learn-install/main.go iterates library/<cat>/<slug>/
        |
        for each CLI:
        v
skip if no .printing-press.json (e.g., dominos-pp-cli stray binary)
        |
        skip if .no-learn-sweep marker present
        |
        v
detect root.go shape (rootFlags-struct vs legacy var rootCmd)
        |
        legacy shape -> log "manual review needed" and skip
        |
        v
write internal/learn/* package files (idempotent: strip-then-re-emit)
        |
        v
AST-inject teach/recall/learnings command registration into root.go
        |
        v
extend internal/store/store.go migrations slice (additive only)
        |
        v
bump StoreSchemaVersion
        |
        v
add learn-purity comment markers so re-runs find the same anchors
        |
        v
go mod tidy (handles the 7 CLIs that don't yet vendor modernc.org/sqlite)
        |
        v
record SKILL.md "Automatic learning" section if not present
        |
        no patches.json entry written -- generator-owned package
        |
        v
rollback all touched files on any per-CLI error, continue to next
```

***

## Output Structure

The generator side adds these new artifacts. Existing files modified noted with \[M], new with \[N].

```
cli-printing-press/
  internal/
    generator/
      generator.go                                 [M] +renderLearnFiles, +prepareOutput dirs
      templates/
        learn/                                     [N]
          config.go.tmpl                           [N]
          extract.go.tmpl                          [N]
          normalize.go.tmpl                        [N]
          match.go.tmpl                            [N]
          recall.go.tmpl                           [N]
          teach.go.tmpl                            [N]
          teach_log.go.tmpl                        [N]
          preseed.go.tmpl                          [N]
          doc.go.tmpl                              [N]
        learn_entities/
          config.go.tmpl                           [N]
          extract.go.tmpl                          [N]
        learn_patterns/                            [N] (renamed from recipes)
          store.go.tmpl                            [N]
          extract.go.tmpl                          [N]
          apply.go.tmpl                            [N]
          doc.go.tmpl                              [N]
        learn_lookups/
          store.go.tmpl                            [N]
          seeds.go.tmpl                            [N]
        teach.go.tmpl                              [N] (cobra commands)
        store.go.tmpl                              [M] +migrations, +StoreSchemaVersion bump
        root.go.tmpl                               [M] +conditional registration, +init wiring
        skill.md.tmpl                              [M] +Automatic learning section
        readme.md.tmpl                             [M] +teach/recall docs
    spec/
      spec.go                                      [M] +LearnConfig struct, +validateLearn
      spec_test.go                                 [M] +TestParseLearnConfig
    pipeline/
      internal_packages.go                         [M] +"learn": true
      scorecard.go                                 [M] +scoreLearn presence
  testdata/
    golden/
      cases/
        generate-learn-loop-api/                   [N] spec + expected/
  AGENTS.md                                        [M] +reserved namespace, +cross-repo dep

printing-press-library/
  tools/
    sweep-learn-install/                           [N]
      main.go                                      [N]
      main_test.go                                 [N]
      root_ast.go                                  [N] (AST surgery on root.go)
      root_ast_test.go                             [N]
  AGENTS.md                                        [M] +sweep-learn-install docs
  library/<cat>/<slug>/                            [M] one PR per pilot CLI; full sweep per category
    internal/learn/                                [N]
    internal/cli/teach.go                          [N]
    internal/cli/root.go                           [M] (AST-injected registrations)
    internal/store/store.go                        [M] (additive migrations)
    SKILL.md                                       [M] (Automatic learning section)
```

***

## Implementation Units

### Phase 1: Generator foundation (cli-printing-press)

### U1. Add `LearnConfig` spec block + validation

**Goal:** Make the spec parser understand a `learn:` block and expose it on `APISpec.Learn`.

**Requirements:** R3, R6.

**Dependencies:** None.

**Files:**

* `internal/spec/spec.go` (modify) -- add `LearnConfig` struct alongside `CacheConfig`/`ShareConfig` near line 1206; add `Learn LearnConfig` field to `APISpec` near line 195; add `validateLearn` function near line 3061; call it from the `validateCacheShare`-equivalent path.

* `internal/spec/spec_test.go` (modify) -- add `TestParseLearnConfig` covering valid block, empty block, invalid ticker regex, duplicate seed kinds.

* `internal/spec/testdata/learn_example.yaml` (new) -- minimal fixture.

**Approach:**

`LearnConfig` carries:

* `Enabled bool`

* `TickerPatterns []string` (each must compile to a valid regex; validateLearn runs `regexp.Compile`)

* `Stopwords []string`

* `EntityLookupSeeds map[string][]LookupSeed` where `LookupSeed{Canonical string, Aliases []string}`

Validation rejects:

* Ticker pattern that fails `regexp.Compile` with a clear "ticker\_patterns\[i] is not a valid Go regexp: ..." error

* Seed kind name containing whitespace or punctuation other than `_`

* Empty `Canonical` in any seed

* Duplicate canonical values within the same seed kind

**Patterns to follow:** `CacheConfig` shape (spec.go:1184), `ShareConfig` shape (spec.go:1206), `validateMCP` shape (spec.go:3061).

**Test scenarios:**

* Happy path: spec with full `learn:` block parses; `APISpec.Learn.Enabled == true`; ticker patterns compiled.

* Empty block: `learn: {}` parses with `Enabled` false; no validation errors; recall path will no-op at runtime.

* Absent block: omitting `learn:` entirely yields zero-value `LearnConfig`; equivalent to disabled.

* Invalid regex: `ticker_patterns: ["[unclosed"]` returns a validation error naming the bad pattern and the regex error.

* Bad seed kind: `entity_lookup_seeds: { "bad name with spaces": [...] }` returns a validation error.

* Duplicate canonical: two seeds in the same kind with same `canonical:` returns a validation error.

* Whitespace stopwords: `stopwords: [" ", "  ", "valid"]` keeps "valid" only (mirrors entities.Config behavior).

**Verification:** `go test ./internal/spec/...` passes including new TestParseLearnConfig.

***

### U2. Reserve `learn` namespace + reimplementation\_check exemption

**Goal:** Tell the dogfood reimplementation\_check that `internal/learn/*.go` is generator-owned, not agent-authored.

**Requirements:** R1, R5.

**Dependencies:** None.

**Files:**

* `internal/pipeline/internal_packages.go` (modify) -- add `"learn": true` to `reservedInternalPackages` map.

* `internal/pipeline/internal_packages_test.go` (modify or create) -- add test confirming "learn" is reserved.

* `internal/pipeline/reimplementation_check_test.go` (modify) -- add a fixture where a novel-feature handler imports `internal/learn` and confirm it's not flagged.

* `AGENTS.md` (modify) -- add `internal/learn/` to the "Generator-reserved namespaces" subsection of the "Agent-Native Surface" section.

**Approach:** One-line map addition + a documentation update. The learn package's runtime helpers delegate to `internal/store` for the actual SQL work, so the existing store carve-out covers the read path automatically -- no new exemption class needed in `exemptionKind`.

**Patterns to follow:** The existing entries in `reservedInternalPackages` (line 8 of `internal/packages.go`).

**Test scenarios:**

* Reserved-list test: confirm `reservedInternalPackages["learn"] == true`.

* Reimplementation-check fixture: a handler that calls `learn.Recall(ctx, query)` and then operates on the result is not flagged as reimplementation (rides the store carve-out because Recall ultimately reads from `internal/store`).

**Verification:** `go test ./internal/pipeline/...` passes; AGENTS.md edit visible in `git diff`.

***

### U3. Port learn package: entities + normalize + match

**Goal:** Emit the entities sub-package, the normalize helper, and the match helper into every printed CLI. These are the cheapest, most-isolated pieces of the prediction-goat learn package -- pure functions with no SQL or HTTP.

**Important refactor scope:** Per Key Technical Decisions ("Config-per-call layering"), this is NOT a verbatim port. The lift source has a package-level `DefaultPredictionGoatConfig()` singleton that this unit must dismantle. Every public function gains an explicit `*Config` parameter. Caller plumbs `*Config` through.

**Requirements:** R1, R2, R6.

**Dependencies:** None.

**Files:**

* `internal/generator/templates/learn_entities/config.go.tmpl` (new) -- port from `~/printing-press/.publish-repo-prediction-goat-80222e8b/library/payments/prediction-goat/internal/learn/entities/config.go`. Replace any prediction-goat-specific defaults with generic defaults (no `odds`/`wins`/`losing` -- those come from spec.Learn.Stopwords).

* `internal/generator/templates/learn_entities/extract.go.tmpl` (new) -- port from prediction-goat `entities/extract.go`. Pure functions; should port near-verbatim.

* `internal/generator/templates/learn/normalize.go.tmpl` (new) -- port from prediction-goat `learn/normalize.go`.

* `internal/generator/templates/learn/match.go.tmpl` (new) -- port from prediction-goat `learn/match.go`.

* `internal/generator/generator.go` (modify) -- add `renderLearnFiles()` method following the `internal/share/` precedent (line 1727). Call it from `renderOptionalSupportFiles()` gated on `g.Spec.Learn.Enabled`. Update `prepareOutput()` (line 1518) to MkdirAll for `internal/learn/entities/` (and the rest of the sub-packages).

* Existing test files in prediction-goat (`entities/extract_test.go`, `learn/normalize_test.go`, `learn/match_test.go`) are emitted alongside as `.tmpl` files.

**Approach:** The prediction-goat package was designed for this lift (per its `portability_intent` frontmatter). Five changes vs. verbatim:

1. Replace `// Copyright 2026 mvanhorn.` with the templated owner block (mirror other emitted files).
2. Add the `// Generated by CLI Printing Press` marker header.
3. Replace any test-fixture references to "Portugal"/"USA"/"KXMENWORLDCUP" with neutral examples like "ENTITY1"/"ENTITY2"/"TICKER-123" (so tests pass for the empty-spec case).
4. Remove the `DefaultPredictionGoatConfig()` singleton. `normalize.Normalize`, `entities.Extract`, and any other call sites become `func(query string, cfg *Config) ...` rather than reaching for the global. Tests updated to construct an explicit cfg.
5. The Match helper similarly takes `*Config` (or just the precomputed entity result) rather than calling the singleton internally.

**Patterns to follow:** `internal/share/share.go.tmpl` for multi-file package emission. `internal/generator/templates/store_upsert_batch_test.go.tmpl` for "ship the test alongside the package" pattern.

**Test scenarios:**

* Emission test: with `learn.enabled: true`, generator emits all four templated files at the expected paths; with `learn.enabled: false`, no learn files emitted.

* Generated package compiles: `go build ./internal/learn/...` in the printed CLI succeeds.

* Round-tripped entities tests pass: the ported `extract_test.go` (now with neutral fixtures) passes in the printed CLI.

**Verification:** Run generator on the new `generate-learn-loop-api/` fixture, confirm the four files emit, run `go build` against the output directory, run `go test ./internal/learn/...` in the output directory.

***

### U4. Port learn package: lookups + patterns

**Goal:** Emit the entity\_lookups sub-package and the patterns sub-package (renamed from recipes) into every printed CLI.

**Requirements:** R1, R2, R6.

**Dependencies:** U3 (the entities helpers are imported by patterns/apply).

**Files:**

* `internal/generator/templates/learn_lookups/store.go.tmpl` (new) -- port from prediction-goat `learn/lookups/store.go`.

* `internal/generator/templates/learn_lookups/seeds.go.tmpl` (new) -- port from prediction-goat `learn/lookups/seeds/` (the in-memory seeding helpers, not the 923-row data). Data comes from `spec.Learn.EntityLookupSeeds` at registration time.

* `internal/generator/templates/learn_patterns/store.go.tmpl` (new) -- port from prediction-goat `learn/recipes/store.go`. RENAME: `Recipe` -> `Pattern`, `recipes` table -> `patterns` table, `learn_recipes` -> `learn_patterns`.

* `internal/generator/templates/learn_patterns/extract.go.tmpl` (new) -- port from prediction-goat `learn/recipes/extract.go`. Same rename.

* `internal/generator/templates/learn_patterns/apply.go.tmpl` (new) -- port from prediction-goat `learn/recipes/apply.go`. Same rename.

* `internal/generator/templates/learn_patterns/doc.go.tmpl` (new) -- port + update doc to reference the `Recipe`->`Pattern` rename with a "lineage" note pointing to the prediction-goat plan.

* Corresponding test files emitted alongside.

* `internal/generator/generator.go` (modify) -- extend `renderLearnFiles()` to emit the lookups + patterns sub-packages.

**Approach:**

The rename is mechanical: `s/Recipe/Pattern/g`, `s/recipes/patterns/g`, `s/learn_recipes/learn_patterns/g`. Done across all .go files and the SQL table-create statements. The doc.go for patterns explicitly notes the rename and points to the prediction-goat plan as the lineage anchor.

`lookups/seeds.go` becomes a helper: it exposes `SeedFromConfig(store *Store, seeds map[string][]ConfigSeed) error` that the printed CLI calls from root.go init with values derived from `spec.Learn.EntityLookupSeeds`. No baked-in seed data in the template itself.

**Patterns to follow:** Same as U3.

**Test scenarios:**

* Emission test: with full `learn:` block, all 6 templated files emit at expected paths.

* Rename test: grep the generated output for "recipe" or "Recipe" -- should find zero (besides the lineage note in patterns/doc.go).

* SQL test: the emitted `patterns/store.go` creates a `learn_patterns` table; verify against the schema test.

* Seed test: `lookups.SeedFromConfig` with a `{country_iso2: [{US, [USA, America]}]}` config populates the entity\_lookups table correctly.

* Apply test (integration): a Pattern matched against a known entity returns a substituted resource ID that exists in the resources table.

**Verification:** Generator emits, files compile, `go test ./internal/learn/...` in the printed CLI passes. Grep verification for "recipe" passes (zero hits in core code).

***

### U5. Port learn package: recall + teach + preseed

**Goal:** Emit the top-level learn package files (recall, teach, teach\_log, preseed) that tie entities/lookups/patterns together.

**Requirements:** R1, R2, R5, R6.

**Dependencies:** U3, U4.

**Files:**

* `internal/generator/templates/learn/recall.go.tmpl` (new) -- port from prediction-goat `learn/recall.go`.

* `internal/generator/templates/learn/teach.go.tmpl` (new) -- port from prediction-goat `learn/teach.go`.

* `internal/generator/templates/learn/teach_log.go.tmpl` (new) -- port from prediction-goat `learn/teach_log.go`.

* `internal/generator/templates/learn/preseed.go.tmpl` (new) -- port from prediction-goat `learn/preseed.go`. Strip the Kalshi/Polymarket-specific preseed scanners. The generator emits a registration shim that printed CLIs can call into from per-source scanners they author.

* `internal/generator/templates/learn/doc.go.tmpl` (new) -- package-level design doc.

* Tests emitted alongside.

**Approach:**

`recall.go` and `teach.go` are the core API. The recall path calls into the store + lookups + patterns helpers, applies entity-aware match, returns `{found, resources, match_score, source}`. The teach path validates resource shape, writes to JSONL teach.log, upserts the search\_learnings row, and triggers async pattern extraction.

`preseed.go` becomes a registration framework: `learn.RegisterPreseedScanner(resourceType string, scanFn func() ([]PreseedEntry, error))`. The generator does not bake in any scanners; per-source preseed scanners stay in the printed CLI's own source code (e.g., a future ESPN CLI would register a scanner that pulls all NFL team rosters into entity\_lookups).

`teach_log.go` is the JSONL writer for `~/.local/share/<cli>/teach.log`. Templated path uses `naming.CLI(g.Spec.Name)`.

**Patterns to follow:** Same as U3/U4.

**Test scenarios:**

* Recall happy path: teach (Q, \[R1, R2]); recall(Q) returns `{found: true, resources: [R1, R2]}`.

* Recall miss: recall on never-taught query returns `{found: false}`.

* Recall confidence floor: teach once -> recall returns confidence=1, threshold=2 filters it out; teach again -> threshold=2 returns it.

* Teach validates shape: invalid resource ID format rejected with structured error.

* Teach log JSONL: each teach appends one line to `~/.local/share/<cli>/teach.log`.

* Preseed registration: a registered scanner runs at first store-open and populates entity\_lookups.

* Entity-aware mismatch: teach with entity "Portugal" + recall with entity "England" returns `entity_match: mismatch` (not a hit).

* Pattern extraction integration: after 3 structurally-similar teaches, extract finds the template and stores it.

**Verification:** Generator emits, `go test ./internal/learn/...` in printed CLI passes.

***

### U6. Wire learn schema into the store template

**Goal:** Bump `StoreSchemaVersion`, add additive `CREATE TABLE IF NOT EXISTS` for learn tables + FTS, ensure existing user data untouched.

**Requirements:** R1, R4.

**Dependencies:** None (but the learn package templates from U3-U5 will reference these tables).

**Files:**

* `internal/generator/templates/store.go.tmpl` (modify) -- bump `StoreSchemaVersion = 2` to `3`; add learn table creates to the `migrations` slice (around lines 255-323) gated by `{{if .Spec.Learn.Enabled}}`; include `search_learnings`, `search_patterns`, `entity_lookups`, `teach_log_metadata` (the JSONL path lives outside SQLite but a metadata table tracks rotation), and the FTS5 virtual table over taught queries.

* `internal/generator/templates/store_schema_version_test.go.tmpl` (modify) -- update expected version + add test that the additive migrations don't drop columns from existing tables.

* `internal/generator/generator.go` (modify) -- `storeData` envelope (around line 2522) extended to pass the learn-table SQL if needed.

**Approach:**

The migrations slice is the version-gated chain. New baseline becomes v3. Migrations look like:

```go
// Version-gate added to migrations slice (illustrative, not literal)
if current < 3 {
    statements = append(statements, learnTablesMigration()) // CREATE TABLE IF NOT EXISTS only
}
```

For library CLIs already at v2, the sweep runs the v2 -> v3 migration on first open, which is a single ALTER TABLE IF NOT EXISTS / CREATE TABLE IF NOT EXISTS pass. No destructive changes.

FTS5 over taught queries: `CREATE VIRTUAL TABLE IF NOT EXISTS search_learnings_fts USING fts5(query_pattern, tokenize='porter unicode61')`. Mirrors the existing `resources_fts` pattern at line 46.

**Patterns to follow:** Existing `migrations := []string{...}` slice in store.go.tmpl; existing `resourcesFTSCreateSQL` for FTS5 shape.

**Test scenarios:**

* Schema version bump: a freshly-opened store reports schema\_version=3.

* Existing data preserved: pre-populate an in-memory store at v2 with resources rows; run migration; resources rows are still present and queryable.

* Idempotent migration: running the migration twice produces zero diff in the schema.

* Learn tables present: after migration, `SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'search_%' OR name LIKE 'entity_%'` returns the expected set.

* FTS5 functional: insert a teach record, query via FTS5 MATCH, get the hit.

* learn.enabled=false case: when spec disables learn, migration slice does not include learn tables; existing tables unchanged.

**Verification:** `go test ./internal/generator/...` passes; golden suite re-runs and shows the new learn tables in the emitted store.go for the learn fixture.

***

### U7. Cobra commands: teach, recall, learnings, learnings forget, teach-pattern, teach-lookup

**Goal:** Emit the user-facing command file and conditionally register the commands in root.go.

**Requirements:** R1, R7.

**Dependencies:** U3, U4, U5, U6.

**Files:**

* `internal/generator/templates/teach.go.tmpl` (new) -- the combined commands file. Lift the teach/recall/learnings/forget command definitions from prediction-goat's `internal/cli/teach.go`. **Carve out** the prediction-goat-specific extension wiring (`applyLearningsForTopic`, `applyLearningsForCompare`, `topicApplier`, `compareApplier`, `matchesTopicResourceType`, `resolveLearnedHit`) -- those stay in the source CLI because they reference cobra commands and result types that only exist there. Expected lifted size: \~470 lines of the source 921.

* `internal/generator/templates/teach_test.go.tmpl` (new) -- ported tests, restricted to the lifted command surface (not the topic/compare rerank tests).

* `internal/generator/templates/root.go.tmpl` (modify) -- add conditional `{{if .Spec.Learn.Enabled}}rootCmd.AddCommand(newTeachCmd(...))` etc.; introduce skip-list machinery in `PersistentPreRunE` (no current generator-side precedent; lift the pattern from granola's per-CLI autorefresh.go but treat as new machinery in the generator).

* `internal/generator/templates/agents.md.tmpl` (modify) -- mention the learn loop in the agents documentation.

**Approach:**

MCP annotations (per AGENTS.md "Tool safety annotations"):

* `recall` and `learnings list` -> `cmd.Annotations["mcp:read-only"] = "true"`

* Others -> no read-only annotation (writes the local store)

Skip-list for `PersistentPreRunE` recall trigger: `auth`, `doctor`, `help`, `sync`, `profile`, `feedback`, `which`, `agent-context`, `completion`, `version`. **Introduces new machinery** in the generator -- the cobratree `frameworkCommands` set serves a different purpose (MCP emission suppression), and the granola autorefresh skip-list is per-CLI patch code that doesn't live in templates. This unit is the first place a generator-level pre-run skip-list ships; budget for that.

The `--resource-type` flag is required and explicit (no auto-detect). Auto-detection from spec.resources\[].id\_pattern moves to Deferred to Follow-Up Work.

**Patterns to follow:** `internal/cli/share_commands.go.tmpl` for conditional-on-spec emission; `granola:internal/cli/autorefresh.go` for skip-list shape.

**Test scenarios:**

* All six commands present: `<cli> --help` lists teach, recall, learnings, learnings forget, teach-pattern, teach-lookup when learn enabled.

* learn.enabled=false: none of the commands appear; root.go conditional skips them.

* recall MCP annotation: cobratree walker classifies recall as read-only.

* teach MCP annotation: cobratree walker classifies teach as a write.

* Skip-list test: `<cli> doctor --query "test"` does NOT trigger recall (verified via teach.log absence of recall\_pre\_run entries).

* Skip-list completeness: framework commands (`auth`, `doctor`, `help`, `sync`, `profile`, `feedback`, `which`, `agent-context`, `completion`, `version`) all skip; novel commands do not.

* Required --resource-type: teach without `--resource-type` errors with a clear message naming the flag.

**Verification:** Generator emits the file; `go test ./internal/cli/...` in printed CLI passes; manual `<cli> teach --help` shows all flags.

***

### U8. Spec-driven config emission + entity-lookup seeding

**Goal:** Emit the init code in root.go (or a dedicated learn\_init.go) that translates `spec.Learn` into runtime registrations.

**Requirements:** R3.

**Dependencies:** U1, U3, U4, U7.

**Files:**

* `internal/generator/templates/learn_init.go.tmpl` (new) -- the per-CLI init shim. Has functions `initLearn(ctx, store) (*learn.Config, error)` that register ticker patterns, stopwords, and entity-lookup seeds from `spec.Learn`.

* `internal/generator/templates/root.go.tmpl` (modify) -- call `initLearn(...)` from the cobra `Execute()` setup path.

* `internal/generator/templates/learn_init_test.go.tmpl` (new) -- unit tests for the emitted init function.

**Approach:**

Per the Config-per-call refactor decided in Key Technical Decisions, the init file constructs a `*learn.Config` once at startup and threads it through every teach/recall/normalize call. Per the package boundary established in U3-U5, there is no singleton. Calling code in root.go cobra setup:

```go
// Illustrative; not literal code in the plan.
learnCfg, err := initLearn(ctx, store, spec.Learn)
if err != nil {
    // soft-validation: log and continue with a default config (default stopwords, no ticker patterns)
}
ctx = learn.WithConfig(ctx, learnCfg)
```

Entity-lookup seed install: at init, `lookups.SeedFromConfig(store, spec.Learn.EntityLookupSeeds)` runs once per process (idempotent on the store side via INSERT OR IGNORE).

**Patterns to follow:** Existing init wiring in root.go.tmpl for cache/share.

**Test scenarios:**

* Empty spec.Learn: `initLearn` returns a working `*learn.Config` (default English stopwords, no ticker patterns); subsequent teach/recall calls work as no-ops.

* Full spec.Learn: ticker patterns registered, stopwords merged on top of defaults, seeds populated.

* Bad runtime regex (shouldn't happen since validateLearn rejects at parse time, but defensive): logged warning, continue without that pattern.

* Init idempotent: calling `initLearn` twice produces the same state (lookups.SeedFromConfig is a `INSERT OR IGNORE`).

**Verification:** Generator emits learn\_init.go; `go test ./internal/cli/...` passes the new init tests.

***

### U9. (removed)

Originally proposed a `select_paths.go` codegen primitive; scope-guardian flagged this as unrelated to the learn loop. Moved to **Deferred to Follow-Up Work** (separate plan). U-ID retained as a gap per the U-ID stability rule.

***

### U10. SKILL.md + README.md template updates + AGENTS.md

**Goal:** Document the learn loop in the templates that every CLI's docs derive from, plus the cross-repo dependency rule.

**Requirements:** R1, R9.

**Dependencies:** U7 (so command names are stable).

**Files:**

* `internal/generator/templates/skill.md.tmpl` (modify) -- add an "Automatic learning" section gated `{{if .Spec.Learn.Enabled}}` mirroring the prediction-goat SKILL.md's four-branch protocol (recall first; if hit, use cached IDs; if miss, do discovery; teach on the way out).

* `internal/generator/templates/readme.md.tmpl` (modify) -- add a brief mention of `teach`/`recall`/`learnings` under the commands list.

* `internal/generator/templates/agent_context.go.tmpl` (modify) -- ensure the agent-context output lists the learn commands and their MCP annotations so introspecting agents discover them.

* `AGENTS.md` (modify) -- expand the "Cross-repo dependency" section to cover learn-template changes pairing with `printing-press-library:tools/sweep-learn-install/`. Mention `internal/learn/` in "Generator-reserved namespaces."

**Approach:**

The SKILL.md addition is the most important doc surface -- it's what agents read at runtime. The four-branch protocol from prediction-goat's SKILL.md:

1. If query looks like a known taught pattern -> use recall.
2. If recall miss but query has an entity that matches a registered seed -> let pattern Apply try substitution.
3. If both miss -> do normal discovery walk, then teach on the way out.
4. If pattern Apply succeeded but verification fails -> demote to discovery walk + re-teach.

The template renders the protocol in CLI-neutral language; the per-CLI `learn:` block fills in domain-specific entity examples via templated values.

**Patterns to follow:** Existing conditional sections in skill.md.tmpl (e.g., the cache/share sections).

**Test scenarios:**

* Test expectation: minimal -- this is template-output; verify via golden fixture diff in U12.

* Manual verification: regenerate one library fixture, eyeball the SKILL.md output, confirm the Automatic learning section appears with correct command names.

**Verification:** Golden suite (U12) covers; manual SKILL.md eyeball confirms readability.

***

### U11. Scorecard awareness + dogfood pass + regen-merge verification + CI purity check

**Goal:** Make the surrounding pipeline aware of the new reserved namespace -- modest scoring credit, no false-positive flags, regen-merge handles new emission correctly, CI catches contamination.

**Requirements:** R1, R6.

**Dependencies:** U2, U3 (need the package emitting).

**Files:**

* `internal/pipeline/scorecard.go` (modify) -- in `scoreVision` (around line 1096), add a small (+0.5) credit for `internal/learn/learn.go` presence. Add comment explaining the credit is for the loop's presence, not its richness.

* `internal/pipeline/scorecard_test.go` (modify) -- add a fixture where learn is present and confirm the +0.5 lands.

* `internal/pipeline/regenmerge/regenmerge_test.go` (modify) -- add a fixture where a published CLI has no `internal/learn/` and regen-merge classifies the new emission as `VerdictNewTemplateEmission` (clean overwrite).

* `internal/pipeline/dogfood.go` (modify or verify) -- confirm `reimplementation_check` does NOT flag learn package files (the namespace addition in U2 should suffice).

* `scripts/verify-learn-purity.sh` (new) -- a grep-based CI check that scopes to **code only** (strips comment lines via `gofmt -d` AST extraction or `grep -v '^\s*//'`) so legitimate lineage notes in `doc.go.tmpl` files don't false-positive. Greps the stripped output for domain identifiers (`kalshi`, `polymarket`, `country_iso2`) and exits non-zero if any appear. **Exception:** allow `prediction-goat` in `doc.go.tmpl` lineage notes via an explicit allowlist line.

* `.github/workflows/verify-learn-purity.yml` (new) -- workflow runner.

**Approach:**

Scorecard credit is intentionally modest. The learn loop adds value over time (as teach traffic accumulates), not at print time. +0.5 prevents the addition from skewing existing scores while still rewarding presence.

Regen-merge: per research, the existing `VerdictNewTemplateEmission` handles new package emission correctly. Add a fixture as confirmation; no code changes expected.

Purity check is a defensive belt: if a future contributor accidentally pastes a prediction-goat-specific identifier into the learn template, CI fails the PR with a clear message pointing to this plan and to the domain-words prohibition.

**Patterns to follow:** `scoreVision` shape (scorecard.go:1096) for the credit; existing purity-grep scripts in `scripts/` for the CI check.

**Test scenarios:**

* Scorecard fixture: a printed CLI with learn enabled gets +0.5 in tier 1 over a comparable CLI without.

* Regen-merge fixture: classify a stage with new `internal/learn/*.go` files as `VerdictNewTemplateEmission`.

* Dogfood fixture: a printed CLI handler calling `learn.Recall(...)` is not flagged by reimplementation\_check.

* Purity check positive: a template with `// TODO: handle kalshi case` fails the purity check.

* Purity check negative: clean templates pass.

**Verification:** `go test ./internal/pipeline/...` passes; `bash scripts/verify-learn-purity.sh` exits 0 against the clean templates.

***

### U12. Golden fixture: generate-learn-loop-api/

**Goal:** Lock the deterministic emission shape via the golden harness so future template changes don't accidentally drift.

**Requirements:** R1.

**Dependencies:** U3, U4, U5, U6, U7, U8, U9, U10.

**Files:**

* `testdata/golden/cases/generate-learn-loop-api/spec.yaml` (new) -- a representative spec with a full `learn:` block (ticker patterns, stopwords, two entity-lookup seed kinds).

* `testdata/golden/expected/generate-learn-loop-api/` (new) -- the expected output tree, generated via `scripts/golden.sh update` after U3-U10 land.

* `scripts/golden.sh` (verify, no changes expected) -- confirm the update path handles the new case.

**Approach:**

Copy `testdata/golden/cases/generate-sync-walker-api/` as the starting point (mature, multi-file). Add the `learn:` block. Run `scripts/golden.sh update`. Inspect diff (should be: new `internal/learn/*` files, modified `internal/store/store.go` with learn tables, modified `internal/cli/root.go` with command registrations + skip-list, new `internal/cli/teach.go`, modified `SKILL.md`/`README.md`).

Commit the expected/ tree. From this point forward, any template change requires either a green golden run or an explicit golden update + diff review.

**Patterns to follow:** Existing golden cases (sync-walker, mcp-api, tier-routing) for fixture shape.

**Test scenarios:**

* Test expectation: golden fixture itself IS the test -- `scripts/golden.sh verify` against the committed expected/ tree must pass.

* Negative: pre-emission state (without U3-U10) cannot pass; once U3-U10 land, generator output matches the golden tree byte-for-byte.

**Verification:** `scripts/golden.sh verify` returns exit 0 with the new case included.

***

### Phase 2: Library sweep (printing-press-library)

### U13. Build `sweep-learn-install` tool

**Goal:** A Go tool at `printing-press-library:tools/sweep-learn-install/` that idempotently retrofits the learn loop into existing library CLIs, mirroring `tools/sweep-canonical/`'s shape but doing Go-source surgery.

**Requirements:** R1, R4, R5.

**Dependencies:** Phase 1 complete and merged (so the generator's emission shape is locked).

**Files:**

* `printing-press-library:tools/sweep-learn-install/main.go` (new) -- the tool entry. GOPATH mode (no go.mod). Iterates `library/<cat>/<slug>/`, applies per-CLI sweep, snapshot-restores on per-CLI failure, continues to next.

* `printing-press-library:tools/sweep-learn-install/main_test.go` (new) -- idempotency tests (every patch function gets a `_Idempotent` test), shape detection tests (rootFlags-struct vs legacy var rootCmd), opt-out marker honored.

* `printing-press-library:tools/sweep-learn-install/root_ast.go` (new) -- the AST surgeon that injects `teach`/`recall`/`learnings` command registrations into `internal/cli/root.go`. Detects rootFlags-struct vs legacy shape; refuses (not silently no-ops) on legacy.

* `printing-press-library:tools/sweep-learn-install/root_ast_test.go` (new) -- AST tests with both shapes as fixtures.

* `printing-press-library:tools/sweep-learn-install/store_migration.go` (new) -- extends each CLI's `internal/store/store.go` migrations slice and bumps `StoreSchemaVersion`. Anchor-based (looks for a stable `// CLI Printing Press: migrations` marker added in Phase 1).

* `printing-press-library:tools/sweep-learn-install/learn_files.go` (new) -- writes the `internal/learn/*.go` files (rendered inline since GOPATH mode means no helper import). Must match generator output byte-for-byte.

* `printing-press-library:tools/sweep-learn-install/skill_patch.go` (new) -- patches the per-CLI `SKILL.md` to add the Automatic learning section (idempotent strip-then-re-emit).

* `printing-press-library:tools/sweep-learn-install/go_mod_tidy.go` (new, or folded into main.go) -- for the \~7 CLIs without `modernc.org/sqlite`, adds the dep pinned to the same version the generator's `go.mod.tmpl` carries, then runs `go mod tidy` in the target CLI directory. Snapshots `go.mod` AND `go.sum` so rollback can restore them on failure. Honors `GOPRIVATE` env if set (per Matt's known friction with sumdb 404s on private modules).

**Approach:**

Mirror `tools/sweep-canonical/` patterns exactly:

* GOPATH mode, runnable as `GO111MODULE=off go run ./tools/sweep-learn-install`

* Per-CLI snapshot-restore on error (in-memory `written []{path, before}`, defer rollback())

* Idempotency contract: second run produces zero textual diff

* Strip-then-re-emit canonical (not detect-and-modify)

* Per-CLI dirty-check via content comparison; skip when unchanged

* Status per CLI: `SWEPT library/<cat>/<api> (<status>)` or `ERROR <dir>: <err>`

* Summary line: `Sweep complete: N patched, M already up-to-date, K errored, S skipped`

Skip rules (silent skip, logged in summary):

* Directory lacks `.printing-press.json` (catches the dominos stray binary)

* `.no-learn-sweep` marker file present

* Legacy `var rootCmd` shape in root.go (agent-capture, instacart-style CLIs -- per AGENTS.md these need manual review)

* Existing hand-modified store.go with non-template structure (anchor not found)

Run sequence per CLI:

1. Read `.printing-press.json`; skip if absent
2. Read `.no-learn-sweep`; skip if present
3. Detect root.go shape; refuse if legacy
4. Snapshot all to-be-touched files to in-memory map (including `go.mod` and `go.sum`)
5. Write `internal/learn/*.go` (strip-and-re-emit)
6. AST-inject root.go registrations + skip-list
7. Extend store.go migrations slice (anchor-based, behind a stable `// CLI Printing Press: migrations` marker added in U6)
8. Patch SKILL.md (idempotent strip-then-re-emit Automatic Learning section)
9. `go mod tidy` if `modernc.org/sqlite` not in go.mod (snapshot go.mod and go.sum first)
10. **Update** **`.printing-press.json`** `printing_press_version` field to the generator version that produced these templates (otherwise future regens will see a version mismatch and re-touch the entire CLI). This is the ONE manifest field the sweep tool writes; no patches.json entry.
11. On any error: rollback all snapshotted files (including go.mod, go.sum, manifest)
12. On success: log status

NO `.printing-press-patches.json` write (per Key Technical Decisions -- generator-owned package).

**Patterns to follow:** `printing-press-library:tools/sweep-canonical/main.go` and `agentcookie.go`.

**Test scenarios:**

* Idempotency (per patch function): run twice, second run produces zero diff.

* Shape detection: rootFlags-struct shape detected and patched; legacy var rootCmd shape detected and refused with clear error.

* Opt-out marker: presence of `.no-learn-sweep` skips with `SKIPPED (opt-out)` status.

* Missing manifest: directory without `.printing-press.json` skips with `SKIPPED (no manifest)` status.

* Hand-edited store.go: missing anchor triggers `SKIPPED (anchor not found)` rather than mangling.

* Snapshot-restore: simulated failure mid-sweep restores all touched files for that CLI; sweep continues to next.

* go.mod with sqlite present: tool skips `go mod tidy`.

* go.mod without sqlite: tool adds `modernc.org/sqlite v1.37.0` and runs `go mod tidy`.

* Byte-for-byte parity: emitted `internal/learn/*.go` matches generator output (regression test against a known fresh-print fixture).

* SKILL.md idempotent: strip-and-re-emit produces zero diff on second run.

**Verification:** `cd printing-press-library/tools/sweep-learn-install && GO111MODULE=off go test .` passes.

***

### U14. Pilot sweep into 5 high-value CLIs

**Goal:** Sweep into 5 high-traffic CLIs as a controlled validation step before full library rollout. Verify CI passes, agent-context lists the new commands, and no regressions surface.

**Requirements:** R1, R5, R10.

**Dependencies:** U13.

**Files:**

* `printing-press-library:library/media-and-entertainment/espn/internal/learn/` (new) + `internal/cli/teach.go` (new) + `internal/cli/root.go` (modify) + `internal/store/store.go` (modify) + `SKILL.md` (modify) + `go.mod` (modify if needed).

* `printing-press-library:library/sales-and-crm/contact-goat/...` (same shape).

* `printing-press-library:library/developer-tools/bugbounty-goat/...`.

* `printing-press-library:library/commerce/instacart/...`.

* `printing-press-library:library/media-and-entertainment/podcast-goat/...`.

**Approach:**

Run `sweep-learn-install` against the 5 CLIs only (initially via a `--only <slug>` flag, or by running the tool then reverting all but the 5). Each CLI gets its own PR or they're bundled into one umbrella PR per category (3 PRs total: media-and-entertainment with espn+podcast-goat, sales-and-crm with contact-goat, developer-tools with bugbounty-goat, commerce with instacart). User decides PR shape.

Pilot CLIs chosen for:

* High discovery-walk cost in real agent traffic (espn: team/game lookup; contact-goat: cross-source person search; bugbounty-goat: program search across H1/HuntR/YesWeHack; instacart: product search; podcast-goat: episode search across sources)

* Active development (not stale forks)

* No hand-customizations in root.go that would trip the AST patcher

Each pilot PR carries:

* Generated learn package files

* AST-injected command registrations

* Schema bump

* SKILL.md Automatic learning section (with empty `learn:` block defaults documented; the CLI author fills in domain-specific entity-lookup seeds in a follow-up commit)

Validation gates before proceeding to Phase 3:

* All 5 PRs CI-green (`verify-library-conventions.yml`, `verify-skills.yml`, govulncheck)

* Manual `<cli> --help` shows teach/recall/learnings

* Manual `<cli> agent-context` lists the new commands

* Manual `<cli> recall "test"` returns `{found: false}` (no panic)

* 1-2 weeks of dogfood traffic confirms no regressions

**Patterns to follow:** Existing per-CLI PRs in `printing-press-library:library/` history.

**Test scenarios:** (executed manually per CLI as part of the validation gate)

* `<cli> --help` lists 6 new commands.

* `<cli> teach --help` shows the resource-type auto-detect flag set.

* `<cli> recall "test"` returns structured JSON (not a panic).

* `<cli> teach --query "test" --resource <known-id>` works end-to-end.

* `go test ./...` in each pilot CLI passes.

* `govulncheck ./...` per-CLI passes.

**Verification:** All 5 CLIs ship to main with green CI, manual smoke covers the happy paths.

***

### U15. Full library sweep + cross-repo docs

**Goal:** Run the sweep across the remaining \~163 library CLIs, ship as per-category umbrella PRs (one PR per category, 17 PRs total since the 5 pilots already shipped), update both AGENTS.md files to lock in the new cross-repo dependency rule and reserved namespace.

**Requirements:** R1, R4, R5, R9, R10.

**Dependencies:** U14 plus the validation gate has passed.

**Files:**

* `printing-press-library:library/<cat>/<slug>/` (modify across \~163 CLIs): same shape as U14 per-CLI changes.

* `printing-press-library:AGENTS.md` (modify) -- document `tools/sweep-learn-install/` alongside `tools/sweep-canonical/`; cover the additive-only schema rule; document the `.no-learn-sweep` opt-out marker.

* `AGENTS.md` (in this repo, modify) -- already touched in U2 and U10; final pass ensures the cross-repo dependency text mentions learn alongside readme/agentcookie.

* `printing-press-library:docs/plans/2026-05-23-XXX-feat-library-learn-sweep-followup-plan.md` (new) -- thin pointer plan in the library repo referencing this plan and the per-CLI PR list. Lets the library-side reviewer find the work.

**Approach:**

Run `sweep-learn-install` against all CLIs. Bundle changes by category for PR ergonomics (each category averages 8-12 CLIs). PR titles: `feat(<category>): install self-learning loop in <N> CLIs`.

Skip-list during the sweep (logged in PR description):

* `commerce/dominos-pp-cli` (stray binary)

* Any CLI carrying `.no-learn-sweep` (zero expected at this point but the mechanism is in place)

* Any CLI with legacy `var rootCmd` shape (agent-capture, instacart -- but instacart is in the pilot, so manually retrofitted there)

Per-category PR description template:

* Sweep summary (N patched, M already-up-to-date from pilot, K skipped with reasons)

* Cross-link to the generator-side plan

* Cross-link to a tracking issue if any CLI was skipped due to legacy shape

* "How to verify" checklist (per-CLI `go test`, `<cli> --help`, `<cli> recall test`)

Documentation updates (AGENTS.md in both repos) lock in the rules so future maintainers don't accidentally untangle them.

Optional: a small `library-sweep-status.md` doc in cli-printing-press tracking which library CLIs have learn installed, which are pending manual review, and which opted out. Useful for the next time a similar sweep happens.

**Patterns to follow:** Previous library-wide PRs (the auto-refresh granola PR shape, sweep-canonical PR shape).

**Test scenarios:** (executed per CLI by the sweep tool; aggregated in PR descriptions)

* Every shipped CLI: `go test ./...` passes.

* Every shipped CLI: govulncheck reachability passes.

* Every shipped CLI: SKILL-vs-source verifier (verify\_skill.py) passes.

* Registry generator (`generate-registry --validate`) green per changed CLI.

* generate-skills.yml regen produces zero diff in `cli-skills/` (since SKILL.md is included in the per-CLI sweep, post-merge regen is a no-op).

**Verification:** All 17 per-category PRs CI-green and merged; final sweep report shows N patched + K skipped with all skips documented.

***

## System-Wide Impact

This plan touches the printing-press machine surface that every printed CLI inherits, plus 168 published library entries. Impacts ripple through:

* **Every printed CLI gets \~8500 LOC of** **`internal/learn/`** plus \~1600 LOC of `internal/cli/teach.go` plus a SQLite schema bump. Build time per CLI increases marginally (\~2-3 seconds for the additional package compilation). Binary size grows \~400KB per CLI (estimated from prediction-goat's `du -sh` deltas).

* **Agent-Native Surface grows by 6 commands per CLI** (teach, recall, learnings, learnings forget, teach-pattern, teach-lookup). The cobratree walker auto-exposes them as MCP tools. Agents that already use the CLI's MCP surface immediately get the new tool inventory.

* **CI pipeline gains a new gate** (`verify-learn-purity.yml`) that runs on every PR touching `internal/generator/templates/learn*`.

* **Two AGENTS.md files updated** to lock in the new reserved namespace, the cross-repo dependency, and the additive-only schema rule.

* **A new generator-reserved namespace** (`internal/learn/`) joins `internal/cliutil/` and `internal/mcp/cobratree/`. Agent-authored novel code in this directory is forbidden going forward.

* **Scorecard outputs shift by +0.5 (tier 1) for every printed CLI with learn enabled** -- the bump is modest but uniformly applied, so relative rankings stay stable.

* **regen-merge** sees a new emission class (`VerdictNewTemplateEmission` for `internal/learn/*`) but no code change is required -- the existing classifier handles it.

* **17 library categories** receive umbrella PRs in Phase 3. The largest category (media-and-entertainment, 25 CLIs) gets the heaviest PR.

Stakeholders:

* **CLI authors (you + collaborators)**: the new `learn:` spec block is opt-in by default in v1 (`enabled: false` means a benign no-op). Authors can fill in the block when they have time; no upfront work required for existing CLIs to keep working.

* **Agent users**: get a faster second-time-through experience on every CLI with a populated `learn:` block. No behavior change on first-session queries.

* **Library reviewers**: face 5 pilot PRs + 17 category PRs = 22 review touches. Bundled by category to amortize review overhead.

* **govulncheck / supply-chain CI**: every CLI gets a small `internal/learn/` addition; reachability scanner sees more code to scan but no new dependencies (modernc.org/sqlite already vendored in 161/168).

***

## Risk Analysis & Mitigation

### Risk 1: The institutional precedent says "don't sweep, ship to generator only"

**Evidence:** Per institutional learnings, every prior generator-side feature (tier-routing, auth-envvar widening, machine-owned freshness, MCP intent composition) explicitly deferred library retrofit. The dominant precedent rule: "This plan changes the machine for future generated CLIs; reprinting or migration is separate work" (tier-routing plan).

**Why this plan diverges:** User intent is explicit -- "build into every future CLI AND PR it onto every one right now." The phased rollout (generator first + pilot + full sweep) is the compromise: it respects the precedent's "don't sweep blindly" caution by gating each phase, while still reaching the user's destination of full uniform rollout.

**Mitigation:** Phase 1 -> Phase 2 -> Phase 3 each has an explicit checkpoint gate. The user can stop at any phase. The plan does not commit to "must run Phase 3" -- it commits to "Phase 3 is the destination if validation gates pass."

### Risk 2: Hand-edited root.go in some library CLIs blocks the AST patcher

**Evidence:** AGENTS.md notes "A few older CLIs (e.g. `agent-capture`, `instacart`) use a package-global `var rootCmd`...Tools that AST-patch root.go should detect this shape and refuse rather than silently no-op."

**Impact:** \~3-5 CLIs likely need manual retrofit instead of automated sweep.

**Mitigation:** U13 explicitly designs for shape detection + refuse-not-no-op. The sweep tool logs skipped CLIs in its summary; reviewers see exactly which ones need manual work. A follow-up issue tracks the manual retrofits.

### Risk 3: Domain identifiers leak into the generator templates

**Evidence:** The prediction-goat package had 923 seeded entity\_lookup rows including ISO countries + NFL/NBA/MLB/MLS rosters and per-source preseed scanners. These are domain-specific and must not appear in the generator templates.

**Impact:** A leak would force every CLI to compile domain-specific data unrelated to its API.

**Mitigation:** U11 adds `scripts/verify-learn-purity.sh` + a CI workflow that greps the templates for known domain identifiers. Fails the build on any hit. U3-U5 acceptance: zero `kalshi`/`polymarket`/`country_iso2` strings in the templates.

### Risk 4: SQLite schema migration breaks existing user installs

**Evidence:** No precedent for cross-CLI schema sweep. Each user's `~/.cache/<slug>-pp-cli/data.db` is irreplaceable user state.

**Impact:** A non-additive change (DROP COLUMN, ALTER TABLE) could destroy user data on first upgrade.

**Mitigation:** U6 + Key Technical Decisions explicitly require `CREATE TABLE IF NOT EXISTS` only. No ALTER, no DROP, no rename. New tables coexist alongside existing tables. Schema version bump is gated so old binaries refuse to open new dbs (safe-fail rather than silent corruption).

### Risk 5: The naming rename ("Recipe" -> "Pattern") confuses future maintainers

**Evidence:** Prediction-goat shipped with "Recipe" in the API. The generator's package uses "Pattern" to avoid overload with `recipe_intents.go` and `research.go:Recipe`.

**Impact:** Someone reading the prediction-goat plan and then the generator code wonders why the names diverged.

**Mitigation:** U4 includes an explicit `doc.go` entry in `internal/learn/patterns/` documenting the rename and pointing to the prediction-goat plan as the lineage anchor. CHANGELOG entry in the generator repo also notes the rename.

### Risk 6: govulncheck per-CLI runs balloon CI time on the full sweep

**Evidence:** Per the library CI gates research, govulncheck runs per-PR on changed CLIs. The full sweep changes every CLI.

**Impact:** Phase 3 PRs could take significantly longer than usual.

**Mitigation:** Per-category PR bundling keeps each PR's changed-CLI count to 5-25, which is manageable. The govulncheck reachability mode is fast per CLI (\~30s). Worst case (largest category, 25 CLIs): \~13 minutes of govulncheck. Acceptable for a one-time sweep.

### Risk 7: Recipe-extraction quality differs by domain

**Evidence:** Prediction-goat's recipes generalize beautifully across countries because countries are a finite, well-known entity class with stable identifiers. ESPN's "teams" or contact-goat's "people" are messier (multi-team trades, name disambiguation).

**Impact:** Pattern extraction may produce false-positive substitutions in domains where entities are less crisply identified, leading to wrong cached results.

**Mitigation:** The Apply step always verifies the substituted resource exists in the resources table before returning a hit. Verification failure demotes to discovery walk (per the four-branch SKILL.md protocol). Worst case: a false-positive recipe wastes one extra round-trip; never returns a wrong answer to the user. Document the verification contract prominently in the SKILL.md update (U10).

### Risk 8: Sweep tool diverges from generator output byte-for-byte

**Evidence:** Per AGENTS.md "the inline format mirrors the helper library's canonical output byte-for-byte so generator-side fresh prints and sweep-side retrofits stay consistent." Sweep tool runs in GOPATH mode (no go.mod) and cannot import the generator's helpers.

**Impact:** If sweep-emitted learn files diverge from fresh-print emission, a subsequent regen-merge sees them as `VerdictTemplatedBodyDrift` and surfaces for manual review on every CLI -- noise.

**Mitigation:** U13 acceptance includes a "byte-for-byte parity" test: a fresh-print fixture's `internal/learn/*.go` files must match the sweep tool's output exactly. CI workflow can re-run the parity check on every change to either repo's templates.

***

## Phased Delivery

### Phase 1: Generator foundation (this repo)

**Units:** U1, U2, U3, U4, U5, U6, U7, U8, U10, U11, U12 (11 units; U9 removed).
**Outcome:** Future printed CLIs ship with the learn loop. No existing library CLI is touched. Validation: golden suite passes, fresh-print fixture has working teach/recall.
**Checkpoint:** The user reviews + merges Phase 1 PRs (likely 3-5 PRs grouped by sub-area). If learn turns out to be the wrong shape, stop here -- no library impact.

### Phase 1.5: Single-CLI publish-flow dry-run (between Phase 1 and Phase 2)

**Not a separate unit** -- a verification step before pilot. Regenerate one existing library CLI from the local cli-printing-press checkout against a spec with `learn.enabled: true`, run the full publish flow (dogfood + verify + workflow-verify + verify-skill + validate-narrative + scorecard) against the regenerated output. Catches publish-flow surprises before the sweep tool exists. If this fails, return to Phase 1 to fix; do not proceed to Phase 2.

### Phase 2: Pilot sweep (library repo)

**Units:** U13, U14.
**Outcome:** 5 high-value library CLIs (espn, contact-goat, bugbounty-goat, instacart, podcast-goat) carry the loop. Sweep tool is battle-tested.

**Quantitative stop thresholds (NOT optional gates):**

1. **False-positive recall rate**: across a sample of 20 agent-issued queries per pilot CLI, manually grade each cached hit as correct/wrong. Stop Phase 3 if any pilot's wrong-rate exceeds 5% (i.e., more than 1 wrong hit out of 20).
2. **Transferability test**: each pilot CLI runs 10 "novel-but-substitutable" queries (queries where pattern extraction should generalize from prior teaches via entity substitution). Stop if substitution succeeds with an actually-wrong-but-resource-exists result on any pilot.
3. **Dogfood traffic minimum**: 1-2 weeks AND at least 10 taught queries per pilot AND at least 5 recall hits per pilot. If the loop sees too little traffic to evaluate, extend Phase 2; do not advance to Phase 3 on a thin sample.
4. **Regression check**: zero existing-command regressions in any pilot's dogfood flow. Any regression rolls back the pilot CLI and pauses Phase 3.

**Checkpoint:** Pass all four thresholds before proceeding. The user can override but the decision must be explicit and recorded in the Phase 3 PR description.

### Phase 3: Full library sweep (library repo)

**Units:** U15.
**Outcome:** Every remaining library CLI (\~163) carries the loop. 17 per-category PRs land. Documentation locks in the doctrine.
**Checkpoint:** Final sweep report; track skip-list (legacy `var rootCmd` shape, opt-outs) for follow-up. Post-Phase-3 monitoring: see Rollback Playbook below.

***

## Rollback Playbook

If a systemic problem surfaces after Phase 2 or Phase 3 ships, here is the procedure per failure-mode class.

### Failure mode A: Pattern extraction false-positive rate too high in production

**Symptoms:** Multiple pilot CLIs (Phase 2) or post-sweep CLIs (Phase 3) show recall returning resource-exists-but-wrong-answer results at a rate that exceeds the 5% threshold.

**Rollback:**

1. **Hot fix path**: ship a generator-side patch that disables pattern extraction (set `learn.PatternExtractor.Enabled = false` by default; recall still works for direct taught queries, just no entity substitution). Re-run sweep with `--patterns-disabled` flag.
2. **Cold fix path**: if the issue is structural (verification is fundamentally insufficient), ship a follow-up plan that adds the Apply-time relevance check (e.g., top-N FTS5 match must include the substituted resource). Until that ships, hot-fix is the bandage.
3. **User data preservation**: existing user `search_learnings` rows stay. Hot-fix only disables the extraction layer, not the data.

### Failure mode B: SQLite schema interaction bug discovered post-sweep

**Symptoms:** A library CLI's existing FTS5 table or migration interacts badly with the additive learn schema; recall returns stale or empty results despite teach having succeeded.

**Rollback:**

1. **Identify the specific CLI(s) affected** (the additive guarantee means only specific CLIs with existing schema quirks should hit this; not all of them).
2. **Per-CLI sweep tool revert**: add a `tools/sweep-learn-revert/` tool that idempotently removes learn package files, AST-removes command registrations, removes learn-table CREATEs from migrations slice, leaves user data intact.
3. **Generator fix**: identify the schema clash and ship a generator-side fix; re-run sweep on affected CLIs only.
4. **Note**: existing user `search_learnings` rows are preserved across revert + re-apply (they're not part of the schema definition the sweep tool touches).

### Failure mode C: Dead-code complaints / binary-size friction at scale

**Symptoms:** Users complain that CLIs they use (especially pure-action like dominos, greatclips, alaska-airlines) carry \~400KB of dead `internal/learn/` code, and binary size matters for `go install` speed or for distribution channels (homebrew bottles, MCPB bundles).

**Rollback:**

1. **Triage retroactively**: classify the 168 CLIs into "discovery-shaped" (keep learn) vs "pure-action" (remove learn). Likely partition: \~140 keep, \~25 pure-action.
2. **Bulk-add** **`.no-learn-sweep`** **marker** to the pure-action CLIs.
3. **Re-run sweep with the revert tool** to strip learn from pure-action CLIs. (Same tool as Failure Mode B.)
4. **Update generator** to treat `.no-learn-sweep` marker as authoritative on regen too (otherwise the next fresh-print of a pure-action CLI would re-install the loop).

### Failure mode D: FTS5 tokenizer drift surfaces in production

**Symptoms:** A CLI's prediction-goat-shipped FTS table uses one tokenizer; the post-sweep generator template assumes another; recall hit-rate is subtly wrong.

**Rollback:**

1. **Detect**: add a startup PRAGMA check that compares the actual tokenizer in `sqlite_master` against the expected one declared by the generator template. Fail-loud on mismatch.
2. **Fix**: either (a) align both to the same tokenizer in the next sweep, OR (b) version the FTS table name (`search_learnings_fts_v1`, `_v2`) so a tokenizer change forces a new table and old data is read-only.
3. **Data**: existing FTS data in the v1 table stays queryable for backward compatibility.

### General principle

Every failure mode preserves user data (taught learnings, custom patterns, entity\_lookups). The rollback tools strip code surfaces; they never destroy user state. This is enforced by the additive-only schema rule from Risk 4 mitigation -- and the rollback tools must explicitly inherit that rule.

***

## Verification

**Phase 1 done when:**

* All 11 generator-side units shipped to main with green CI.

* `scripts/golden.sh verify` passes including the new `generate-learn-loop-api/` case.

* A manual fresh-print on a test spec emits working `teach`/`recall`/`learnings`.

* `scripts/verify-learn-purity.sh` exits 0.

**Phase 1.5 done when:**

* One existing library CLI regenerated from local cli-printing-press checkout, full publish flow (dogfood + verify + workflow-verify + verify-skill + validate-narrative + scorecard) passes.

**Phase 2 done when:**

* 5 pilot CLIs shipped with green CI.

* Each pilot's `<cli> recall "smoke test"` returns valid JSON.

* Each pilot's `<cli> --help` lists the 6 new commands.

* 1-2 weeks of dogfood with at least 10 taught queries per pilot (40 minimum total) and at least 5 recall hits per pilot.

* **False-positive rate < 5%** measured per pilot via the 20-query manual grading sample.

* **Transferability test passes** per pilot: 10 novel-but-substitutable queries return correct OR no-hit, never wrong-but-resource-exists.

* No regressions in existing dogfood flows on any pilot.

**Phase 3 done when:**

* 17 category PRs merged.

* Sweep report shows N patched + K skipped, with K skipped having documented reasons.

* Both AGENTS.md files updated with the new doctrine.

* A follow-up issue tracks manual retrofits for any legacy-shape CLIs.

* Rollback playbook tested against at least one CLI (dry-run on Failure Mode B revert tool to confirm strip + re-apply works).

***

## Execution Retrospective (Phase 1 + Phase 2)

Captured 2026-05-24 after Phase 2 U14 pilot landed at 5/5 CLIs across PR cli-printing-press#2085, library#826, and library#827.

### Phase 1: Generator-side foundation

11 implementation units shipped across 4 dispatch batches via Claude Code worktree-isolated subagents:

* Batch 1 (parallel): U1, U2, U6, U11 — no file overlap
* Batch 2 (one heavy agent): U3+U4+U5 combined — the ~8500 LOC port from prediction-goat
* Batch 3 (serial): U7 (Cobra commands), then U8 (spec-driven config init) due to shared root.go.tmpl
* Batch 4 (parallel): U10 (docs templates), U12 (golden fixture)

Total Phase 1 time: ~3-4 hours of wall clock with parallel dispatch. 1 dedup-fix commit needed during U6 merge (LearnConfig field declared twice on APISpec). 1 lint fix (obsolete `tc := tc` loop-var capture). 5447 tests pass after merge.

### Phase 2: Sweep tool + pilot — 4 iteration cycles

The pilot surfaced bugs the v1 unit tests missed because they didn't exercise the tool against real-world library artifacts:

1. **Bug A** (`store_migration.go`'s `ensureStoreSchemaVersion`): off-by-one splice put `const StoreSchemaVersion = 3` BEFORE the `package store` declaration. Go parser rejected. Fix: use `go/parser` AST walk to find the imports-block end and splice after it, not string-scan from the package keyword.
2. **Bug B** (root.go AST patcher): emitted `&flags` when host signature had `flags *rootFlags`, producing `**rootFlags` type mismatch. Fix: detect scope type and emit `&flags` (when local value) or `flags` (when ptr param).
3. **Bug C** (emitted `teach.go` / `learn_init.go`): referenced `store.OpenWithContext`, `dryRunOK`, `printJSONFiltered`, `parentNoSubcommandRunE` helpers that older library CLIs don't carry. Fix: inline minimal equivalents (`learnPrintJSON` etc.) in the templates, dropped `ctx` param on `store.Open`. Diverges from the generator's `teach.go.tmpl`; documented as intentional divergence in the sweep tool's README + a per-statement comment at the top of the emitted file.
4. **Bug D** (`canonicalLearnMigrationsBlock`): 5 `CREATE TABLE` / `CREATE VIRTUAL TABLE` statements emitted without the closing `)`. Go source compiled, but SQLite rejected migration at `migrate()` time with `incomplete input (1)`. Fix: one line per statement.

Bonus extensions discovered during fixes:

* **`store.Store.DB()` accessor backfill**: the emitted `teach.go` calls `s.DB()` but older library stores didn't expose it. Sweep now adds the accessor if missing.
* **Factory root.go shape** (instacart: `func Root() *cobra.Command` with no `rootFlags` struct): detector existed but refused. Now supported via a tiny `learn_root_shim.go` emit that introduces the `rootFlags` struct + helpers the templates need.
* **`stmts := []string{...}` migrations naming**: instacart and a handful of other CLIs use `stmts` instead of `migrations`. Anchor detector now accepts both.

### The fresh-HOME lesson

v2's "recall FAIL" diagnoses were partially false positives. The swept CLIs hit a stale `~/.local/share/<cli>/data.db` left over from before the CLI's events table was extended with `season_year` column (espn), pre-schema-bump rows (others). The CREATE INDEX migration referencing the new column failed against the old db. Diagnosis: validation tests MUST use `HOME=/tmp/fresh-$(uuidgen)` for every smoke test. Without that, stale user state contaminates every pilot validation across the library.

Same lesson generalizes: any CLI that relies on per-user persistent state can't be validated end-to-end without a fresh HOME. The sweep tool itself is fine.

### Validation gate ordering

The right validation sequence per pilot CLI:

1. `go build ./...` — catches Bug A (Go parse), Bug B (type mismatch), Bug C (undefined helpers)
2. `go test ./...` — catches issues at the test fixtures (lookups/patterns packages); CLI internal/store tests catch Bug D when fresh DB used
3. `go build -o /tmp/<cli> ./cmd/<cli>-pp-cli` — catches Bug C if not caught earlier
4. `HOME=/tmp/fresh-X /tmp/<cli> --help` — catches command registration
5. `HOME=/tmp/fresh-X /tmp/<cli> recall "test" --json` — catches Bug D at runtime + validates JSON envelope
6. `govulncheck ./...` — catches dep vulnerabilities

v1 stopped at step 1 failing. v2 stopped at step 5 (Bug D). v3 passed all but instacart (factory shape). v4 closed the loop.

### Cost of iteration

The 4-iteration cycle isn't waste; each iteration revealed a class of bug the previous one masked. Lessons for U15 (full library sweep):

* Real-world artifacts surface bugs sandbox/fixture tests don't. Plan for at least 1 round of unexpected bugs even on a tested tool.
* Per-CLI validation in the actual library directory layout (not in `/tmp` clones) is where most issues surface. The shape variance across published CLIs is wider than any fixture set captures.
* Worktree-isolated agents are great for parallel work but can't catch class-of-bugs that need real artifacts to surface. The orchestrator's manual sanity check between batches IS valuable.

### Bugs caught by the pilot that would have shipped to U15 unfixed

All 4 bugs (A/B/C/D) plus the factory-shape gap plus the `stmts` naming variant plus the DB() accessor gap plus the stale-HOME diagnostic confusion. If U15 had shipped to 168 CLIs without the pilot, every one would have failed at runtime with Bug D, and ~30% would have failed at build with Bug A or B depending on root.go shape distribution.

### Phase 3 readiness

Tool is now sound enough to run U15. The remaining gates per the plan:

* 1-2 weeks of dogfood traffic against the 5 pilots
* <5% false-positive recall rate (20-query manual grading sample per pilot)
* Transferability test (10 novel-but-substitutable queries per pilot)
* Zero existing-command regressions
* Sweep tool's `-only` flag bug (declared `var onlySlug string` but never wired to a flag) fixed — minor; the `SWEEP_LIBRARY_ROOT` sandbox approach works around it

Measurement window starts the day the pilots merge to main.

***

## Documentation Plan

* **AGENTS.md (cli-printing-press)**: extend "Generator-reserved namespaces" + "Cross-repo dependency" sections to cover learn.

* **AGENTS.md (printing-press-library)**: document `tools/sweep-learn-install/`, the additive-only schema rule, the `.no-learn-sweep` opt-out marker.

* **skill.md.tmpl + readme.md.tmpl**: per-CLI documentation surface.

* **CHANGELOG.md (cli-printing-press)**: entry noting the new spec block, the new reserved namespace, the rename of Recipe to Pattern, and pointing to this plan for full context.

The `docs/PATTERNS.md` "self-learning loop pattern" section is deferred to follow-up work (see Deferred to Follow-Up Work).

***

## Deferred to Implementation

* **Exact AST patch pattern for root.go.** The implementer will see the actual shape variations across library CLIs and may need to handle 2-3 sub-shapes within the rootFlags-struct family (e.g., `var rootCmd = ...` vs `rootCmd := ...` vs a `newRootCmd()` constructor). Plan with a 2-shape baseline; expect to iterate.

* **The exact** **`learn:`** **block schema field names.** Plan uses `ticker_patterns` / `stopwords` / `entity_lookup_seeds`; final names may shift slightly based on what reads well in YAML and matches existing naming conventions in other spec blocks.

* **The per-CLI test entry for the sweep tool.** A general "every CLI go test passes" check is the goal; whether that's a sweep-tool integration test or a CI gate depends on how heavy the sweep-tool fixture would be.

* **CHANGELOG release-please commit format.** The repo uses release-please; whether this is a `feat:` or `feat!:` (breaking) depends on the spec-block design (an opt-in block is `feat:`; a breaking schema change in the existing store is `feat!:` -- expected to be `feat:` per the additive-only constraint).

* **Whether to emit a** **`.printing-press-patches.json`** **"sweep marker" entry.** The Key Technical Decisions say no, per the institutional learning. The implementer will confirm the absence doesn't trip CI gates on the library side.

***

## Related Documents

* **Prediction-goat baseline plan:** `~/printing-press/.publish-repo-prediction-goat-80222e8b/docs/plans/2026-05-23-002-feat-prediction-goat-smart-learning-plan.md`

* **Prediction-goat search-relevance plan:** `~/printing-press/.publish-repo-prediction-goat-80222e8b/docs/plans/2026-05-23-001-feat-prediction-goat-search-relevance-and-freshness-plan.md`

* **Granola auto-refresh (closest precedent for "lift behavior into the CLI lifecycle"):** `printing-press-library:docs/plans/2026-05-14-001-feat-cli-auto-refresh-every-invocation-plan.md`

* **ProductHunt self-warming (the first learn-loop ancestor):** `printing-press-library:docs/plans/2026-04-23-001-feat-producthunt-self-warming-cli-plan.md`

* **Agentcookie secrets bus hookup (recent generator + sweep precedent):** `docs/plans/2026-05-23-001-feat-agentcookie-secrets-bus-hookup-plan.md`

* **Tier-routing rollout (the dominant "machine change without library retrofit" precedent):** `docs/plans/2026-05-04-001-feat-tier-routing-plan.md`

* **Snapshot-merge with AST classifier (regen-safety bible):** `docs/solutions/design-patterns/snapshot-merge-with-ast-classifier-for-force-regen-2026-05-10.md`

* **Manifest-wins over re-derivation:** `docs/solutions/conventions/manifest-wins-over-re-derivation-for-identity-fields-in-regen-paths-2026-05-12.md`

* **Soft validation in reusable library packages:** `docs/solutions/conventions/soft-validation-in-reusable-library-packages-2026-05-06.md`

* **Reachable-scorer surfaces non-REST CLIs (scorecard surprise precedent):** `docs/solutions/logic-errors/reachable-scorer-surfaces-non-rest-clis-2026-05-21.md`
