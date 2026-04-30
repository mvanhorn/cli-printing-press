---
title: Framework-collision detection in resource-name extraction
type: fix
status: active
date: 2026-04-30
deepened: 2026-04-30
---

# Framework-collision detection in resource-name extraction

## Summary

Add a generator-time collision check that detects when a spec resource name (e.g., PokéAPI's `version`) would shadow a framework cobra subcommand (e.g., the printed CLI's built-in `version` printer). OpenAPI parser auto-renames colliding resources to `<api-slug>-<resource>` and emits a warning; internal-YAML parser errors out (hand-authored specs should be fixed at source). A unit test scans `root.go.tmpl`'s top-level `rootCmd.AddCommand` registration sites and asserts each registered command name is in the reserved set, so adding a new framework command without updating the set fails CI rather than silently regressing a printed CLI.

---

## Problem Frame

PokéAPI's spec has a path at `/api/v2/version/`. The OpenAPI parser's `resourceAndSubFromSegments` derived the resource name `version` and the generator emitted `Use: "version"` on a cobra subcommand. This shadowed the framework's built-in `<cli> version` printer at generation time without any error or warning. End users running `<cli> version` got the API's version-resource list instead of the version banner. The PokéAPI generation required a hand-edit post-generation (`Use: "version"` → `Use: "game-version"`).

This is exactly the failure mode AGENTS.md flags: "Default to machine changes. If a problem appears in a printed CLI, ask first whether the generator should have gotten this right." A future spec adding an `auth`, `doctor`, `sync`, or any other framework-shaped resource will hit the same silent shadowing today.

A partial protection already exists for one specific shape — `internal/spec/spec.go::ReservedCLIResourceNames` blocks resource names that would collide with single-file framework *templates* (`feedback.go`, `auth.go`, `doctor.go`). But it doesn't catch `version`, because `version` is added inside `root.go.tmpl`, not from a `version.go` template — so the existing reserved-name set has no entry for it.

---

## Requirements

- R1. Detect when a spec resource name would shadow a framework cobra subcommand at generation time, before the printed CLI ships.
- R2. For OpenAPI specs: auto-rename the colliding resource to `<api-slug>-<resource>` and emit a build-time warning naming the resource and chosen rename. Generation succeeds.
- R3. For internal-YAML specs: error out with a clear message naming the collision and the rename hint. Generation fails.
- R4. Cover the full set of top-level framework cobra commands a printed CLI registers — the authoritative source is `root.go.tmpl` (and `plan_root.go.tmpl`)'s `rootCmd.AddCommand(newXxxCmd())` sites. As of this work, the set includes `version`, `auth`, `doctor`, `sync`, `search`, `sql`, `profile`, `feedback`, `which`, `meta`, `analytics`, `api`, `export`, `import`, `workflow`, `agent-context`, `tail`, `jobs`, `share`, `subscribe`, `publish`, `health`, `orphans`, `load`, `stale`, plus cobra runtime built-ins `completion`, `help`. (Illustrative — the drift test in U1 is the source of truth; this enumeration shows expected coverage.)
- R5. Reserved-set drift detection: a unit test fails when a new top-level framework command is registered in `root.go.tmpl` without being listed in the reserved set, and when the reserved set names a command that has no template registration site and is not in the documented carve-out list (cobra runtime built-ins; cobratree-only typed-MCP-tool names).
- R6. Existing specs that don't contain a resource colliding with the reserved set *as of this work's merge* produce byte-identical generated output (no spurious renames, no warnings). Future expansions of the reserved set are explicitly out of this guarantee — see Risks.
- R7. Case-aware normalization: `Version` and `VERSION` reach the collision check after lowercasing/snake-to-kebab so they match `version` in the reserved set. Plural forms (`versions`) are NOT normalized — they emit `Use: "versions"` which doesn't shadow `Use: "version"` at the cobra layer; out of scope. (Adding plural-stripping is a separate decision because it would also affect non-colliding resource names.)

---

## Scope Boundaries

- Do not restructure resource-name extraction in either parser; add a final collision-check + rename step.
- Do not generalize this into a "spec validation framework" — one specific protection against one specific failure mode.
- Do not ship the `--rename old=new` flag from F12. Auto-detect should cover real cases; the manual escape valve stays deferred.
- Do not change the printed CLI's `version`, `auth`, etc. command behavior — only protect them from being shadowed.
- Do not change runtime cobra-command resolution in printed CLIs — this is a generator-time check.
- Do not unify the collision-detection reserved set with the cobratree MCP `frameworkCommands` set (different semantics: blocking vs. narrowing). The drift test enforces a subset relationship instead.

### Deferred to Follow-Up Work

- F12 (`--rename old=new` flag on `generate`): defer until the auto-rename behavior surfaces a real-world case it doesn't cover. The retro itself flagged F12 as "mostly subsumed" by F1's auto-detect.
- Catalog-wide sweep: scan every `catalog/*.yaml` resolved spec for resources that would now trigger the collision check. Surface in a separate sweep PR rather than bundling with the protection itself.

---

## Context & Research

### Relevant Code and Patterns

- `internal/openapi/parser.go::mapResources` (line 976) — main OpenAPI ingestion; iterates spec paths and stores into `out.Resources[primaryName]`. Collision check lands here.
- `internal/openapi/parser.go::resourceAndSubFromSegments` (line 2327) — chokepoint where path segments become the resource name; calls `sanitizeResourceName(strings.ReplaceAll(toSnakeCase(segments[0]), "_", "-"))`. The collision check must run *after* this sanitization so `Versions` reaches the check as `versions`.
- `internal/openapi/parser.go::warnf` (line 3237) — existing OpenAPI parser warning style; non-fatal, writes to stderr. Mirror this for the rename warning.
- `internal/spec/spec.go::ParseBytes` (line 611) — internal-YAML entry; runs `expandOperations()` → `enrichPathParams()` → `validateReservedNames()` → `Validate()`.
- `internal/spec/spec.go::validateReservedNames` (line 689) — direct precedent for the internal-YAML check style. Hard-errors with a rename suggestion. Extend or add a sibling validator using the same pattern.
- `internal/spec/spec.go::ReservedCLIResourceNames` (line 658) — existing snake_case reserved set protecting against template-file collisions. Different semantics from the new set (template-file collision vs. cobra-command shadowing) — the two sets coexist; both checks run.
- `internal/generator/templates/root.go.tmpl` — registers top-level framework cobra commands via `rootCmd.AddCommand(newXxxCmd())` calls. **This is the canonical source of truth for "what is a top-level framework command."** The drift test scans this file (plus `plan_root.go.tmpl`) for AddCommand sites; the constructors named there point to per-command templates whose `Use:` literal is the cobra command name to reserve.
- Per-command framework constructor templates: `auth.go.tmpl`, `auth_simple.go.tmpl`, `auth_browser.go.tmpl`, `doctor.go.tmpl`, `plan_doctor.go.tmpl`, `agent_context.go.tmpl`, `analytics.go.tmpl`, `api_discovery.go.tmpl`, `feedback.go.tmpl`, `profile.go.tmpl`, `which.go.tmpl`, `share_commands.go.tmpl`, `sync.go.tmpl`, `graphql_sync.go.tmpl`, `channel_workflow.go.tmpl`, `jobs.go.tmpl`, `tail.go.tmpl`, `insights/health_score.go.tmpl`, `workflows/pm_orphans.go.tmpl`, `workflows/pm_load.go.tmpl`, `workflows/pm_stale.go.tmpl`. The drift test does NOT walk these blindly for `Use:` literals — many contain *subcommand* `Use:` literals (`Use: "login"` under auth, `Use: "list"` under jobs, etc.) that would falsely block common API resource names. Subcommands are excluded by construction when the test starts from `root.go.tmpl`'s AddCommand sites.
- `internal/generator/templates/cobratree/classify.go.tmpl::frameworkCommands` (line 57) — closely related but different semantics (MCP narrowing). Drift test asserts this is a *subset* of the new collision-detection set. Cobratree includes some entries (`about`, `sql`, `meta`) that have no per-command template (they're typed MCP tools, not generator-emitted cobra commands); those are part of the carve-out, not stale entries.
- `internal/spec/spec_test.go:1872-2006` — table-driven test pattern for `validateReservedNames`. Direct template for new collision-detection tests on the internal-YAML side.

### Institutional Learnings

- `docs/retros/2026-03-30-redfin-retro.md` (F3) — promoted-command-vs-service-group collision, same shape as this work. Fixed with a name-equality guard at emit time. Confirms the pattern: detect collision at the point the cobra `Use:` is finalized, before subcommand registration.
- `docs/plans/2026-03-24-fix-template-sanitization-gauntlet-round2-plan.md` — a `Version` collision at the Go type-name layer was resolved with `usedNames` map + numeric suffix (`Version`, `Version2`, …). Reinforces that "two things sanitize to the same identifier" is recurring; informs the rename collision-resolution decision (deferred to implementation, but numeric-suffix is a known precedent).
- `docs/plans/2026-04-28-001-feat-mcp-cobra-tree-mirror-plan.md` (KTD-3, OQ-3 Locked) — the cobratree `frameworkCommands` set lives as a generator-emitted constant so existing CLIs pick up additions without regen. Useful precedent for "framework-command set as a single named constant," but the cobratree set's *semantics* (narrowing) are not identical to what's needed here (blocking).
- `docs/plans/2026-03-23-feat-cli-printing-press-phase2-openapi-parser-plan.md` — original OpenAPI parser derivation contract; flags that sanitization happens in stages and any post-hoc check must run after all sanitization or risk letting `Versions` slip through.

### External References

None gathered — local patterns and institutional learnings are sufficient. The work is internal generator behavior with no high-risk external surface.

---

## Key Technical Decisions

- **Asymmetric parser behavior** — Internal-YAML hard-errors on collision; OpenAPI auto-renames + warns. Rationale: internal-YAML specs are hand-authored by us and should be fixed at source (matches existing `validateReservedNames` style). OpenAPI specs are third-party and can't be edited; auto-rename + warn is the only ergonomic choice. Confirmed with user during Phase 2.
- **Rename format: `<api-slug>-<resource>`** — Qualifier-prefix puts the API name first. Reads naturally in `--help` output as "the API's X" (`pokeapi-version`) rather than "X but for the API" (`version-pokeapi`). Matches the publish-name-collision plan's `<api-slug>-<qualifier>-pp-cli` shape at a different layer.
- **Source of truth: explicit const + drift-detection test (option c, hybrid)** — Cheap to maintain (const is just a sorted string list), robust against rot (drift test catches a new framework command added without updating the set, and catches reserved-set entries naming a command that no longer exists). Reviewer confirmed the lean toward (c).
- **Const home: `internal/spec/spec.go`** — Both parsers already import the `spec` package; collocates with the existing `ReservedCLIResourceNames`. No new package needed. The two reserved sets coexist with different semantics (template-file collision vs. cobra-command shadowing) and clearly distinct names. Note the **key-format asymmetry**: `ReservedCLIResourceNames` uses snake_case keys (matching template file names: `agent_context.go`); `ReservedCobraUseNames` uses kebab-case keys (matching cobra `Use:` literals: `agent-context`). Each parser normalizes its input to the format of the set it checks before lookup. The asymmetry is intentional — each set's keys match the artifact it protects.
- **Sequence: collision check runs *after* sanitization + dedup** — In OpenAPI parser, after `resourceAndSubFromSegments` produces the kebab-case name. In internal-YAML, after `expandOperations`/`enrichPathParams` (matching where `validateReservedNames` already runs). Sequence matters: running the check before sanitization would let `Versions` slip through and become `version` post-sanitization.
- **Reserved set is broader than cobratree's `frameworkCommands`** — Cobratree's set is a *narrowing policy* (skip-when-MCP-exposing); the new set is a *blocking policy* (reject-as-cobra-name). The blocking set must include everything cobratree skips, plus framework commands cobratree intentionally exposes via MCP (e.g., `sync`, `export`, `import`, `workflow`). The drift test asserts cobratree ⊆ new-set.
- **Warning style: stderr line via existing `warnf`** — No canonical structured-warning convention in the codebase; mirroring the existing OpenAPI parser warning style keeps the change minimal. A future structured-warning pass can convert this and other parser warnings together.

---

## Open Questions

### Resolved During Planning

- **Internal-YAML collision behavior?** Hard-error, matching the existing `validateReservedNames` style. Confirmed with user.
- **Rename format?** `<api-slug>-<resource>`. Qualifier-prefix locked.
- **Source-of-truth shape?** Hybrid: explicit const + drift-detection test. Locked per user lean.
- **F12 (`--rename` flag)?** Deferred per retro; auto-detect should cover real cases. Surface only on real-world recurrence.
- **Reserved set unified with cobratree's `frameworkCommands`?** No — different semantics. Coexist with a subset-relationship drift test.

### Deferred to Implementation

- **`<api-slug>-<resource>` itself collides with another resource in the same spec.** Implementation should pick a strategy (numeric suffix `version-2`, alternate qualifier, etc.) based on whether spec-internal collisions are realistic. Survey `catalog/*.yaml` and `~/printing-press/library/` before deciding.
- **Warning emission shape (plain stderr line vs. structured JSON event)** — Pick at implementation time. Match whichever convention is most consistent with the surrounding warnings in `mapResources`. Defer structured-event conversion to a later pass.
- **Catalog-wide sweep timing** — The new check will surface any latent collisions in existing catalog specs. Run the sweep as part of this work (small-scope) or split into a follow-up PR depending on what surfaces. Implementer decides after running the check against `catalog/`.

---

## Implementation Units

- U1. **Reserved-set constant + drift-detection test**

  **Goal:** Define `ReservedCobraUseNames` in `internal/spec/spec.go` next to `ReservedCLIResourceNames`. Seed it explicitly (the const is the source of truth, maintained by hand). Add a drift-detection test that scans `root.go.tmpl` and `plan_root.go.tmpl` for top-level `rootCmd.AddCommand(newXxxCmd())` registration sites, locates each constructor's `Use:` literal in the corresponding per-command template, and asserts each registered top-level command name is in the reserved set. The test also verifies `cobratree.frameworkCommands ⊆ ReservedCobraUseNames`.

  **Requirements:** R4, R5.

  **Dependencies:** None.

  **Files:**
  - Modify: `internal/spec/spec.go`
  - Test: `internal/spec/reserved_drift_test.go` (new file in `package spec_test` — external test package; the in-package test couldn't import `internal/generator` to walk template files because `generator` already imports `spec` and the cycle would break compilation)

  **Approach:**
  - `ReservedCobraUseNames` is a `map[string]struct{}` (or `map[string]bool`), kebab-case keys, declared as a sorted slice + map-build for diff stability when adding entries.
  - The const is hand-maintained — when adding a new top-level framework command, the implementer adds an entry. The drift test catches misses.
  - Drift test extraction: scan `internal/generator/templates/root.go.tmpl` and `internal/generator/templates/plan_root.go.tmpl` for lines matching `rootCmd.AddCommand(newXxxCmd(...))` (regex tolerant of whitespace and arg lists). For each `newXxxCmd` constructor name, locate the constructor's definition (typically `func newXxxCmd() *cobra.Command` in a sibling per-command template), and extract the static `Use:` literal from the cobra command struct. Subcommand `Use:` literals nested under each constructor are NOT collected — only the top-level command's `Use:`.
  - This mechanism intentionally excludes subcommand Use literals like `Use: "login"` (auth subcommand), `Use: "list"` (jobs/feedback subcommand), `Use: "archive"` (workflow subcommand). Those are children of registered framework commands, not new top-level commands, so they don't need to be in the reserved set — and *should not* be, because they collide with very common resource names.
  - Drift test subset assertion: `cobratree.frameworkCommands ⊆ ReservedCobraUseNames`. Either reads the rendered cobratree set via `internal/cli/tools_audit.go::frameworkCommands` (the in-binary mirror) or parses `internal/generator/templates/cobratree/classify.go.tmpl` directly. The mirror is simpler.
  - Carve-out (entries valid in `ReservedCobraUseNames` despite having no `root.go.tmpl` AddCommand site):
    1. Cobra runtime built-ins: `completion`, `help` — registered automatically by cobra at runtime, no template source.
    2. Cobratree-only typed-MCP-tool names: `about`, `sql`, `meta` (and any cobratree adds in the future). Maintained in cobratree's `frameworkCommands` because they shadow MCP tool names; included in the reserved set transitively via the subset rule.
  - Stale-entry rule: an entry in `ReservedCobraUseNames` that is NOT registered via `root.go.tmpl` AddCommand AND NOT in either carve-out class causes the test to fail. This catches genuinely dead entries while honoring the legitimate non-template populations.

  **Patterns to follow:**
  - `internal/spec/spec.go::ReservedCLIResourceNames` (line 658) — existing reserved-set declaration style. Note: that set uses snake_case keys (template-file collision); the new set uses kebab-case (cobra-Use collision). Documented in Key Technical Decisions.
  - `internal/spec/spec_test.go:1995` "known clobbers are all in the set" — pattern for pinning a hardcoded baseline list to prevent regression.

  **Test scenarios:**
  - Happy path: every command registered via `root.go.tmpl`'s `rootCmd.AddCommand(newXxxCmd())` has its `Use:` literal in `ReservedCobraUseNames`.
  - Drift detection (positive): adding a new `rootCmd.AddCommand(newFooCmd())` line in `root.go.tmpl` without adding `"foo"` to `ReservedCobraUseNames` fails the test with a clear message naming the missing entry.
  - Drift detection (positive): adding a stale entry to `ReservedCobraUseNames` that has no AddCommand site and is not in either carve-out class fails the test.
  - Subset check: `cobratree.frameworkCommands ⊆ ReservedCobraUseNames` — adding an entry to cobratree that's missing from `ReservedCobraUseNames` fails the test.
  - Carve-out: `completion`, `help`, `about`, `sql`, `meta` are present in `ReservedCobraUseNames` and the test does NOT flag them as stale (the carve-out logic skips them in the stale-entry check).
  - Negative: a subcommand `Use: "list"` literal nested under a framework constructor (e.g., inside `jobs.go.tmpl`) does NOT cause `list` to be added to `ReservedCobraUseNames`. (Asserts the test mechanism doesn't sweep up subcommand verbs.)

  **Verification:**
  - `go test ./internal/spec/...` passes.
  - Manually adding `rootCmd.AddCommand(newTestDriftCmd())` to `root.go.tmpl` (without adding the entry to the const) causes the drift test to fail with a clear message.

- U2. **Internal-YAML parser collision check (hard-error)**

  **Goal:** Extend `validateReservedNames` (or add a sibling `validateFrameworkCobraCollisions`) in `internal/spec/spec.go` to also reject resource names that match `ReservedCobraUseNames`. Error message names the colliding resource, the framework command it would shadow, and a rename suggestion.

  **Requirements:** R1, R3, R4, R7.

  **Dependencies:** U1 (`ReservedCobraUseNames` must exist).

  **Files:**
  - Modify: `internal/spec/spec.go`
  - Test: `internal/spec/spec_test.go`

  **Approach:**
  - Either extend `validateReservedNames` to check both sets in one pass, or add a sibling validator called from `ParseBytes` immediately after `validateReservedNames`. Pick whichever produces clearer error messages — the two checks have different remediation hints (template-file collision says "rename or drop the resource"; framework-cobra collision says "rename, e.g., to `<api-slug>-<resource>`").
  - Resource names in internal-YAML are explicit map keys, not derived from paths. The check is a direct string-set membership lookup on each `s.Resources` key, normalized to kebab-case if the key isn't already (current convention is kebab-case but the validator should be defensive).
  - Error format mirrors the existing `validateReservedNames` shape: `fmt.Errorf("resource name %q would shadow framework command %q; rename to e.g. %q", name, name, suggestedRename)`. The suggested rename uses the API slug from `s.Name` (or `s.APIName`, whichever the spec field is).

  **Patterns to follow:**
  - `internal/spec/spec.go::validateReservedNames` (lines 689-697) — existing internal-YAML reserved-name validator. Same shape.
  - `internal/spec/spec_test.go:1872, 1900, 1947, 1969, 1995` — table-driven test pattern.

  **Test scenarios:**
  - Happy path: a spec with no colliding resource passes validation unchanged.
  - Error path: a spec with a top-level resource named `version` returns an error containing "shadow framework command" and a rename suggestion.
  - Error path: a spec with a top-level resource named `auth` errors similarly.
  - Edge case: a spec with `Version` (capitalized) — case normalization happens before the check (kebab-case lower) so this is caught.
  - Edge case: a spec with a sub-resource named `version` (nested under another resource) — sub-resources don't emit top-level cobra commands, so they're exempt. Mirrors existing `validateReservedNames` sub-resource exemption (test at `spec_test.go:1969`).
  - Edge case: a spec with a non-reserved-but-substring name (e.g., `versioning_history`) is allowed, mirroring the existing `customer_feedback` test at `spec_test.go:1947`.
  - Error path: the error message includes the API slug in the rename suggestion (e.g., for `name: pokeapi`, the suggestion is `pokeapi-version`).

  **Verification:**
  - `go test ./internal/spec/...` passes.
  - A hand-constructed YAML spec with a `version` resource fails `printing-press generate` with a clear error.

- U3. **OpenAPI parser collision check (auto-rename + warn)**

  **Goal:** Extend `mapResources` in `internal/openapi/parser.go` so that after `resourceAndSubFromSegments` produces the kebab-case resource name, a collision check runs against `ReservedCobraUseNames`. On collision, rename to `<api-slug>-<resource>` and emit a `warnf` line naming the original name and the rename. Generation continues.

  **Requirements:** R1, R2, R4, R6, R7.

  **Dependencies:** U1.

  **Files:**
  - Modify: `internal/openapi/parser.go`
  - Test: `internal/openapi/parser_test.go`

  **Approach:**
  - Locate the post-sanitization point in `mapResources` (around lines 1042-1161 per research) where `out.Resources[primaryName]` is assigned.
  - Before the assignment, check `primaryName` against `spec.ReservedCobraUseNames`. On collision, derive the renamed name as `<api-slug>-<primaryName>` using whatever API-slug field is available on the parser context (likely `out.Name` or a passed-in arg). Use the renamed name for the map key and downstream use.
  - Call `warnf` with a message like: `resource %q from path %q would shadow framework command; renamed to %q (rename: F1 from PokéAPI retro #421)` — but per AGENTS.md "no dates, incidents, or ticket numbers in code comments," strip the trailing parenthetical at write time. Plain message: `resource %q would shadow framework command %q; renamed to %q`.
  - If `<api-slug>-<resource>` itself collides with another resource in the same spec (rare but possible), defer the resolution strategy to implementation per Open Questions. Numeric suffix (`pokeapi-version-2`) is a known precedent from the Fly.io schema-name dedup work.
  - Sub-resources (resources nested under a parent) don't emit top-level cobra commands and are exempt — match the internal-YAML behavior in U2.

  **Patterns to follow:**
  - `internal/openapi/parser.go::warnf` (line 3237) — existing OpenAPI warning style. Note: `warnf` writes directly to `os.Stderr` with no writer-injection seam; tests asserting on warning content must redirect `os.Stderr` via `os.Pipe` for the duration of the parse call (standard Go test pattern), or U3 may add a package-level `warnWriter io.Writer` defaulting to `os.Stderr` so tests can swap it.
  - `docs/retros/2026-03-30-redfin-retro.md` F3 fix at `internal/generator/generator.go:456` (`buildPromotedCommands`) — name-equality guard at emit time. Same shape, different layer.

  **Test scenarios:**
  - Happy path: an OpenAPI spec with a `/version` path produces a parsed resource named `pokeapi-version` (or whatever the API slug is) with the original-name → renamed-name mapping reflected in the parser output.
  - Happy path: the captured warning output (via `os.Stderr` redirect or `warnWriter` injection — see Patterns to follow) contains both the original and renamed names.
  - Edge case: a spec with a `/Version/` (capitalized) path normalizes through `toSnakeCase` + kebab-case before the check, matches `version` in the reserved set, and renames.
  - Edge case: a spec with a `/versions` (plural) path — verify whether the existing extraction strips plurals before reaching the check. If it doesn't, `versions` doesn't match `version` in the reserved set and isn't renamed. Document the actual behavior in the test (not a bug in this work; out of scope to add plural-stripping).
  - Edge case: a spec without any colliding resource produces byte-identical parser output (no spurious renames, no warnings emitted). Verify by snapshotting the output map keys and the captured warning output (empty when no collision occurs).
  - Edge case: a sub-resource named `version` (e.g., a path like `/games/{id}/version`) is NOT renamed — sub-resources don't emit top-level cobra commands.
  - Error path: if the API slug is empty/missing, fall back to a sentinel rename (e.g., `api-version`) rather than producing `-version` (leading hyphen). Document the behavior.
  - Integration: re-parse PokéAPI's actual spec (or a fixture closely modeled on it) and assert the `version` resource is renamed to `pokeapi-version` (or similar) and a warning is emitted.

  **Verification:**
  - `go test ./internal/openapi/...` passes.
  - Generating against a fixture spec with a `version` resource produces a printed CLI where `<cli> version` still prints the version banner AND the renamed cobra command exists.
  - `scripts/golden.sh verify` passes (no existing fixture currently contains a colliding resource per research; expect zero diff).

- U4. **End-to-end golden case (optional, decide at implementation time)**

  **Goal:** Add a small `testdata/golden/cases/<case-name>/` fixture that drives a tiny OpenAPI spec with a `/version` path through `printing-press generate` and snapshots the renamed cobra `Use:` literal in the generated `internal/cli/<resource>.go` file plus the warning line in the build log. Locks the auto-rename + warning shape against future template churn.

  **Requirements:** R2, R6.

  **Dependencies:** U3.

  **Files:**
  - Create: `testdata/golden/cases/framework-collision-rename/` (case dir)
  - Create: `testdata/golden/cases/framework-collision-rename/spec.yaml` (tiny OpenAPI fixture with a `/version` path and one or two innocuous paths so the run produces a generatable CLI)
  - Create: `testdata/golden/expected/framework-collision-rename/<artifact paths>` (snapshot the renamed `Use:` literal and the warning line; use `artifacts.txt` to scope the comparison narrowly)

  **Approach:**
  - Smallest possible fixture — one colliding resource, one or two non-colliding resources to keep generation viable.
  - Snapshot only the contract-shaped artifacts: the line in `internal/cli/<renamed-resource>.go` that emits the renamed cobra `Use:` literal, and the build log line containing the warning. Don't snapshot the full generated tree; that creates noise unrelated to this protection.
  - Whether to include U4 in the same PR or split into a follow-up depends on golden-suite churn. If the test approach in U2/U3 already adequately exercises the contract, U4 can stay deferred.

  **Patterns to follow:**
  - Existing golden cases in `testdata/golden/cases/` — directory layout and `artifacts.txt` scoping pattern.
  - AGENTS.md "Decision rubric" — adds a fixture when the change creates a new deterministic behavior contract.

  **Test scenarios:**
  - Happy path: `scripts/golden.sh verify` passes against the new fixture, snapshotting the renamed `Use:` and warning line.

  **Verification:**
  - `scripts/golden.sh verify` passes.
  - Manually changing the rename format (e.g., from prefix to suffix) causes the golden test to fail with a clear diff.

---

## System-Wide Impact

- **Interaction graph:** Generator → both parsers → cobra emission. The collision check is a single in-line addition in each parser's resource-name extraction. No runtime path in printed CLIs is affected.
- **Error propagation:** Internal-YAML errors propagate up from `ParseBytes` to `printing-press generate`, which already surfaces parser errors with clear messages. OpenAPI warnings flow through existing `warnf` to stderr.
- **State lifecycle risks:** None. The check runs at parse time on in-memory data; no persistence implications.
- **API surface parity:** The cobratree `frameworkCommands` set (in `internal/generator/templates/cobratree/classify.go.tmpl` and mirrored in `internal/cli/tools_audit.go`) must stay a subset of the new `ReservedCobraUseNames`. The drift test in U1 enforces this. Updating one without the other will fail CI.
- **Integration coverage:** The drift test in U1 scans `root.go.tmpl` AddCommand sites at test time and resolves each constructor's `Use:` literal — a new pattern in this codebase but parallel to the cobratree mirror's runtime tree walk. The test lives in `package spec_test` (external test package) to avoid an import cycle with `internal/generator`.
- **Unchanged invariants:** Printed CLI's `version`, `auth`, `doctor`, etc. commands continue to function identically. Existing `ReservedCLIResourceNames` (template-file collision check) continues to run unchanged. Specs without colliding resources produce byte-identical generator output (R6).

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `ReservedCobraUseNames` initial seed misses a framework command, causing a future spec to silently shadow it. | The drift test in U1 walks framework templates and asserts every static `Use:` literal is in the reserved set. Adding a new framework command without updating the set fails CI. |
| `<api-slug>-<resource>` rename target itself collides with another resource in the spec. | Deferred to implementation; numeric suffix (`pokeapi-version-2`) is a known precedent from Fly.io schema-name dedup. Implementation should survey `catalog/*.yaml` and `~/printing-press/library/` for existing instances before deciding. |
| Internal-YAML error-out blocks a catalog regeneration if a catalog YAML file has a colliding resource. | Run a catalog sweep before merge: parse every `catalog/*.yaml` against the new check and surface any failures. Either fix the catalog specs or split sweep into a follow-up PR (deferred-to-follow-up-work item). |
| OpenAPI auto-rename changes the cobra `Use:` of a resource that downstream tooling (e.g., MCP tool descriptions, dogfood checks) references by literal string. | The cobratree walker registers tools at runtime from the actual cobra tree, so it picks up the renamed name automatically. Dogfood checks that compare against expected names need to read the same renamed name from the parsed spec — verify during U3 implementation. |
| Drift test becomes flaky if it scans template files via filesystem path that differs across CI environments. | Use Go's `embed.FS` or a relative path pinned to the package directory; mirror existing template-loading patterns in `internal/generator/`. The test lives in `package spec_test` (external test package) so it can import `internal/generator` without forming a cycle. |
| Future expansion of `ReservedCobraUseNames` (a new top-level framework command lands later) silently auto-renames a previously-stable resource on next OpenAPI regen, with no record beyond ephemeral stderr — so downstream skills/scripts/manifest consumers pinned to the old resource name break with no breadcrumb. | R6 explicitly limits the byte-identical guarantee to "the reserved set as of this work's merge." Each future expansion should be reviewed for catalog impact before landing. A follow-up structured-warning pass should persist the rename in `tools-manifest.json` so consumers can diff renames across regens. The cobratree precedent (existing CLIs pick up additions without regen) doesn't apply here because cobra command names are part of the printed CLI's contract — every expansion is a potential breaking change for some catalog spec. |

---

## Sources & References

- **Retro issue:** [printing-press-library #421](https://github.com/mvanhorn/cli-printing-press/issues/421) — F1 (the collision), F12 (manual `--rename` deferred).
- **Related code:** `internal/openapi/parser.go::mapResources`, `internal/spec/spec.go::validateReservedNames`, `internal/generator/templates/root.go.tmpl:333`, `internal/generator/templates/cobratree/classify.go.tmpl::frameworkCommands`.
- **Prior art:** `docs/retros/2026-03-30-redfin-retro.md` (F3, emit-time collision guard), `docs/plans/2026-03-24-fix-template-sanitization-gauntlet-round2-plan.md` (`Version` schema-name dedup), `docs/plans/2026-04-28-001-feat-mcp-cobra-tree-mirror-plan.md` (cobratree `frameworkCommands` precedent).
- **Recent merged work:** `9a135062` (WU-2 sync correctness pass) — the immediately preceding generator change; this plan targets `main` post-merge.
