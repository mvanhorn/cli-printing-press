---
title: "test: Live-dogfood resolve-success and search-aware error_path coverage"
type: test
status: active
date: 2026-05-04
deepened: 2026-05-04
---

# test: Live-dogfood resolve-success and search-aware error_path coverage

## Summary

Extend `internal/pipeline/live_dogfood_test.go` so the chained companion walk (parent plan U2) and search-aware error_path dispatch (parent plan U3) are exercised end-to-end against the fake binary instead of silently exercising the skip path. Add a parallel rich-fixture builder for multi-resource hierarchies, search variants, and mutation-shape commands; surgically update the existing fixture so `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` asserts real PASS rather than skip-with-overall-PASS. No production-code changes — PR #577's behavior is correct, only its integration coverage is incomplete.

---

## Problem Frame

PR #577 shipped the WU-2 fix-up (chained companion walk, per-companion cache, search-aware error_path, quick-verdict gate) and the unit tests pass. The integration-level tests in `live_dogfood_test.go` silently exercise `resolveSkip` with reason `"no id parseable from companion at depth 0"` because the existing fake-binary fixture returns plain text (`widget 1`) for `widgets list --json`. Result: `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` passes because the full-level verdict tolerates skip, but the chained walk's success path is never exercised at integration level. Surfaced by `/ce-code-review` (testing reviewer T-01..T-04, cross-corroborated by maintainability T-02 and correctness testing-gaps).

---

## Requirements

- R1. The existing acceptance-marker test exercises resolve-success — `widgets get` happy_path returns `LiveDogfoodStatusPass`, not `LiveDogfoodStatusSkip` (origin: issue #579 AC #3; verifies parent plan R3's implementation at integration level).
- R2. A rich-fixture builder exposes a multi-resource hierarchy, search variants (both `--query` flag and positional `<query>` shape), a mutation command, and an opt-in argv-logging side channel (origin: issue #579 AC #1).
- R3. Integration tests cover the five U2 resolve-success scenarios from issue #579: single-positional happy, chained multi-positional happy, cache hit, `companionSupportsLimit` exercise, negative-cache sentinel hit (origin: issue #579 AC #2 first list; verifies parent plan R3, R4 implementations at integration level).
- R4. Integration tests cover the seven U3 search/error_path scenarios from issue #579: `--json` + empty results, `--json` + non-empty fallback results, no `--json` support, positional `<query>`, non-zero exit, search + invalid JSON (only fail mode currently exercised), mutation with no `--query`/`<query>` (origin: issue #579 AC #2 second list; verifies parent plan R5, R6 implementations at integration level).
- R5. The argv-logging mechanism is shell-friendly, opt-in (no behavior change when env var unset), and isolated per test via `t.TempDir()`. Sequential matrix-walk ordering is the load-bearing serialization guarantee.
- R6. Existing tests (`TestRunLiveDogfoodDetectsJSONParseFailure`, `TestRunLiveDogfoodErrorPathAcceptsExpectedNonZeroExit`, `TestRunLiveDogfoodAcceptanceRequiresManifestIdentity`, `TestRunLiveDogfoodExplicitBinaryNameMustExist`) continue to pass against the unchanged simple fixture.

---

## Scope Boundaries

- No production-code changes in `internal/pipeline/live_dogfood.go`, `internal/pipeline/dogfood.go`, or `internal/generator/`. PR #577's behavior is correct; only its integration-test coverage is incomplete.
- Depends on parent plan `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md` (PR #577) being merged. This plan adds integration-test coverage for already-landed production code; without #577, the assertions exercise behavior that doesn't exist yet.
- No replacement of the existing `writeLiveDogfoodFixture` — the simple fixture's purpose (intentional regression coverage for malformed-JSON detection via `brokenJSONFixed: false`) is preserved.
- No new test file. Extend `internal/pipeline/live_dogfood_test.go` rather than splitting integration tests into a separate file.
- No Windows portability work. Existing tests skip on Windows (`runtime.GOOS == "windows"`); new tests follow the same convention.
- No golden-fixture changes. Generator output is not affected by this work.

### Deferred to Follow-Up Work

- Cross-platform (Windows) live-dogfood test coverage: would require replacing shell-script fake binaries with Go-built ones across the entire test file. Out of scope here; tracked separately if/when it becomes a real requirement.

---

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/live_dogfood_test.go:577` — `writeLiveDogfoodFixture` is the existing fake-binary builder. Conditional shell-script generation keyed off `brokenJSONFixed`. Pattern to follow for `writeLiveDogfoodRichFixture`.
- `internal/pipeline/live_dogfood_test.go:694` — `writeTestManifestForLiveDogfood` writes a minimal `CLIManifest` to the fixture dir. Reusable as-is.
- `internal/pipeline/live_dogfood.go:252` — `buildSiblingMap` keys on `strings.Join(c.Path[:len(c.Path)-1], " ")`. For a root-level command at `Path = ["get"]`, the key is `""`. Confirms the root-level scenario shape.
- `internal/pipeline/live_dogfood.go:266` — `findListCompanion` selects the first sibling in `companionLeaf` allowlist. Cross-API verbs (`list`, `all`, `index`, `query`, `find`, `search`, `discover`, `browse`, `recent`, `feed`) and cinema verbs (`popular`, `trending`, etc.).
- `internal/pipeline/live_dogfood.go:320` — chained walk computation: `siblingKey = strings.Join(command.Path[:pathLen-nPlaceholders+i], " ")`. Implementer should re-read this for chain-test argv shape.
- `internal/pipeline/dogfood.go:1779` — `extractFlagNames` and `extractFlagsSection` (PR #577 PERF-001). Used by `commandSupportsSearch` and `companionSupportsLimit`.
- `cliutil.IsVerifyEnv` — established pattern for shell-side env-var sentinels (`PRINTING_PRESS_VERIFY=1`). Follow naming: `PRINTING_PRESS_TEST_ARGV_LOG`.

### Institutional Learnings

- AGENTS.md anti-pattern: "no API names in reusable artifacts." Rich fixture uses generic `widgets`/`projects`/`tasks`/`accounts` names, not API-specific brands.
- AGENTS.md "Code & Comment Hygiene": no dates, incidents, or ticket numbers in code comments. Fixture comments document shape, not the issue that drove them.
- Issue #579 was filed because `/ce-code-review` couldn't mechanically write the new shell scripts and orchestrate multi-resource fixtures — keep test scenarios specific enough that an implementer doesn't have to invent coverage.

### External References

None. This is purely test-infrastructure work using established Go testing patterns and POSIX shell.

---

## Key Technical Decisions

- **Hybrid fixture strategy**: surgically update `writeLiveDogfoodFixture` (scrub `--limit 2` from the `widgets list --help` Examples block, delete the now-unreachable `widgets list --limit 2 --json` shell branch, add a clean `widgets list --json` branch returning `{"results":[{"id":"123"}]}`, and update the existing acceptance test's assertion) AND add a parallel `writeLiveDogfoodRichFixture` for multi-resource scenarios. Rationale: the simple fixture's purpose is intentional regression coverage for malformed-JSON detection (`brokenJSONFixed: false`); replacing it would couple unrelated concerns and break the unit-shaped surface the PR #577 retro itself called out as missing. The Examples-block scrub is required because `companionSupportsLimit` calls `extractFlagNames` on raw help text (not flags-section-scoped), so any `--limit` token anywhere in the help — including Examples — makes the resolver append `--limit 1` to the companion call and miss a bare `widgets list --json` branch. Drift risk between the two shell scripts is small (~30 lines of overlap) and surfaces as test failures the next time someone touches either.
- **argv recording via env-var-pointed tempfile**: `PRINTING_PRESS_TEST_ARGV_LOG=$(mktemp)` set per-test; rich fixture's shell script appends `printf '%s\n' "$*" >> "$LOG"` on every invocation guarded by `[ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ]`. Rationale: the live-dogfood walker iterates the matrix sequentially, so writes are serialized by construction — no concurrency-atomicity guarantees are needed. A counter directory (mkdir-based) gives count but not content; a Go fake binary would require build ceremony for no portability gain over the shell-skip-on-Windows convention. Defensive env-var guard means tests that don't care don't have to set it. **Important — `--help` invocations also log**: `companionSupportsLimit` invokes `<companion> --help` as a separate subprocess before the actual companion call, and its argv is captured the same way. Tests asserting on companion-call counts must filter to `--json`-bearing argv lines (the actual companion call), not just the companion path.
- **Per-command behavior-mode env vars** for U3 fixture variants: instead of one `PRINTING_PRESS_TEST_SEARCH_MODE` that affects every search-shape branch in a matrix walk, use distinct keys per command — e.g., `PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE=empty|fallback|invalid` only affects `widgets search`, `PRINTING_PRESS_TEST_WIDGETS_SEARCH_POSITIONAL_MODE` only affects `widgets search-positional`. Defaults to `empty` when unset. Rationale: the matrix walker probes ALL commands per run, so a single shared env var would cross-pollute branches inside one test, hiding real failures or producing spurious ones.
- **Three implementation units** (not 2 or 4): U1 isolates the AC #3 fix as a small standalone commit (one help-text scrub + one shell-branch swap + one assertion update); U2 bundles the rich-fixture builder with its first five consumers (resolve-success scenarios) so the fixture is reviewable end-to-end; U3 layers the seven search/error_path scenarios on top. **U3 must execute strictly serial after U2** — both extend the same `writeLiveDogfoodRichFixture` shell-script literal, so parallel ce-work dispatch would produce a literal-merge conflict in the worktree-isolation flow. Rationale: a fixture-only commit (4-unit shape) lacks a consumer and is unreviewable in isolation; a single-test-commit shape (2-unit) creates a ~400-line append that crosses the adversarial-reviewer threshold and bundles two distinct test concerns.
- **Test function naming convention**: U2's resolve-success tests use the `TestRunLiveDogfoodResolveSuccess*` prefix (e.g., `TestRunLiveDogfoodResolveSuccessSinglePositional`, `TestRunLiveDogfoodResolveSuccessChainedMultiPositional`, `TestRunLiveDogfoodResolveSuccessCacheHit`, `TestRunLiveDogfoodResolveSuccessCompanionLimit`, `TestRunLiveDogfoodResolveSuccessNegativeCacheSentinel`). U3's tests use `TestRunLiveDogfoodSearchErrorPath*` (e.g., `TestRunLiveDogfoodSearchErrorPathEmptyResults`, `TestRunLiveDogfoodSearchErrorPathFallbackResults`, etc.). Each scenario is a distinct top-level test function — no table-driven `t.Run` nesting — to keep AC-to-test traceability obvious in test output and `-run` flag use. Rationale: 13 new tests added to a file with 6 existing `TestRunLiveDogfood*` tests; without a convention, names drift and AC traceability is lost.
- **"Only fail mode" is a tripwire, not a hard claim**: U3's "search + invalid JSON" scenario is the only fail mode currently produced by the search-shape error_path code in `live_dogfood.go:693-711`. Future production-code changes that add new fail branches (timeout, empty stdout under `--json`, schema-mismatch) require a corresponding new U3-shape integration test in the same change. Documented in U3's verification block as an invariant note so the assertion doesn't quietly become a coverage gap.
- **Generic resource names** (`widgets`, `projects`, `tasks`, `accounts`): no API-specific brands. AGENTS.md "no API names in reusable artifacts" applies even to test fixtures because test code compounds across CLIs.

---

## Open Questions

### Resolved During Planning

- Fixture extension strategy: hybrid (parallel builder + minimal update). See Key Technical Decisions.
- argv recording mechanism: env-var-pointed tempfile, with explicit caveat that `--help` probes also log (assertions filter on `--json` substring). See Key Technical Decisions.
- Unit shape: 3 units; U3 strictly serial after U2 due to shared fixture literal. See Key Technical Decisions.
- Root-level get scenario: dropped from R3. Production code's `resolveCommandPositionals` short-circuits at `pathLen < nPlaceholders+1`; for a top-level `get <id>` command at `Path=["get"]` with one placeholder, `1 < 2` fires and the resolver skips before consulting the sibling map. The empty-key sibling case is unreachable at integration level; the existing unit test `TestResolveCommandPositionalsSkipPaths` already covers the rejected shape. R3 reduced from 6 to 5 scenarios.
- U3 fixture mode dispatch: per-command env var keys (`PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE`, `PRINTING_PRESS_TEST_WIDGETS_SEARCH_POSITIONAL_MODE`), not a single shared key. See Key Technical Decisions.
- Test function naming convention: distinct top-level test functions with `TestRunLiveDogfoodResolveSuccess*` and `TestRunLiveDogfoodSearchErrorPath*` prefixes. See Key Technical Decisions.
- Companion-leaf invariant: U2 and U3 tests must assert the chosen companion explicitly via argv-log substring (`widgets list --json` not just any companion path). Documented in U2's verification block.

### Deferred to Implementation

- Exact JSON shape returned by `projects list --json` and `projects tasks list <pid> --json`: pick the canonical-path shape (`.results[0].id`) for clarity unless a chain-walk test specifically exercises a different path. Implementer chooses during U2.
- Whether to gate `widgets describe` (cache-hit sibling) under a fixture opt or always include it: include always — cheap to add a no-op branch, and tests that don't probe it ignore it.
- Whether to record argv as `\t`-joined or newline-joined per invocation: pick newline-per-invocation for trivial parsing; argv tokens within a line joined with single spaces (matching `$*` shell behavior). Implementer can adjust if a test needs token-level precision.
- Argv ordering for `--limit 1` (resolver may emit `--json --limit 1` or `--limit 1 --json`): implementer should verify against `live_dogfood.go` resolver logic when wiring U2's `companionSupportsLimit` test fixture branches.

---

## Implementation Units

- U1. **Surgical update to existing fixture + acceptance-test assertion fix**

**Goal:** Make `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` exercise the resolve-success path end-to-end, asserting `widgets get` happy_path returns `LiveDogfoodStatusPass`. AC #3 from issue #579.

**Requirements:** R1, R6

**Dependencies:** None.

**Files:**
- Modify: `internal/pipeline/live_dogfood_test.go` (scrub `--limit 2` from `widgets list --help` Examples block; delete the unreachable `widgets list --limit 2 --json` shell branch; add a `widgets list --json` branch returning canonical-path JSON; add one assertion in `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass`)

**Approach:**
- In `writeLiveDogfoodFixture`'s `widgets list --help` block, remove the `Examples:` line containing `widgets list --limit 2`. This makes `extractFlagNames(help)` return no `limit` flag, so `companionSupportsLimit` returns false, so the resolver calls `widgets list --json` (no `--limit`).
- Delete the existing shell branch matching `--limit 2 --json` (lines 657-660 in current source). It returned `{"widgets":[{"id":"1"}]}`, which is unreachable after the help scrub and was using a non-canonical JSON path (`.widgets[]` is not in the resolver's 7-path try-list anyway, so it would never have been parseable).
- Add a new shell branch matching `widgets list --json` returning `{"results":[{"id":"123"}]}` — canonical path #1 (`.results[0].id`) so the resolver parses it on the first attempt.
- In `TestRunLiveDogfoodWritesAcceptanceMarkerOnPass`, add an assertion that locates the `widgets get` happy_path test result via `Command == "widgets get" && Kind == LiveDogfoodTestHappy` and asserts `Status == LiveDogfoodStatusPass`. Existing overall-verdict and marker-file assertions stay.
- Verify the existing `widgets get 123 --json` branch (already returns `{"id":"123"}`) and the no-json branch (returns `widget 123`) still work — they should.

**Patterns to follow:**
- Existing shell branch structure in `writeLiveDogfoodFixture`.
- Existing `report.Tests[i].Command == "widgets get" && report.Tests[i].Kind == LiveDogfoodTestHappy` lookup pattern from `TestRunLiveDogfoodErrorPathAcceptsExpectedNonZeroExit:90-99`.

**Test scenarios:**
- Happy path — `widgets get` happy_path resolves via companion `widgets list --json` returning `{"results":[{"id":"123"}]}`, gets substituted to `widgets get 123`, runs against fixture's `widgets get 123` branch, returns `Status == LiveDogfoodStatusPass`. Existing overall verdict assertion still passes.
- Edge case — `TestRunLiveDogfoodDetectsJSONParseFailure` (which uses `brokenJSONFixed: false`) continues to pass because the help scrub and new shell branch only affect `widgets list`, not `widgets broken`.
- Regression — all other existing tests against `writeLiveDogfoodFixture` (`TestRunLiveDogfoodErrorPathAcceptsExpectedNonZeroExit`, `TestRunLiveDogfoodAcceptanceRequiresManifestIdentity`, `TestRunLiveDogfoodExplicitBinaryNameMustExist`) still pass.

**Verification:**
- `go test ./internal/pipeline/... -run TestRunLiveDogfoodWritesAcceptanceMarkerOnPass` passes with the new assertion.
- `go test ./internal/pipeline/... -run TestRunLiveDogfoodDetectsJSONParseFailure` still passes (no regression on broken-JSON coverage).
- `go test ./internal/pipeline/...` passes overall.

---

- U2. **Rich-fixture builder + U2 resolve-success integration tests**

**Goal:** Build `writeLiveDogfoodRichFixture` exposing a multi-resource hierarchy with argv logging, and add the five U2 resolve-success integration tests from issue #579 AC #2 first list (root-level scenario dropped — see Open Questions).

**Requirements:** R2, R3, R5

**Dependencies:** None (independent of U1; both modify the same test file but target separate fixture builders and separate test functions, so merge-conflict surface is small).

**Files:**
- Modify: `internal/pipeline/live_dogfood_test.go` (add `writeLiveDogfoodRichFixture`; add five resolve-success test functions)

**Approach:**

Rich fixture exposes:
- `widgets list` (no `--limit` declared in help — see U1 rationale on raw-help flag detection), `widgets get <id>`, `widgets describe <id>` (cache-hit sibling).
- `widgets list-with-limit` (separate companion that DOES declare `--limit` in its help — for the `companionSupportsLimit` test exclusively, with its own `widgets get-with-limit <id>` sibling so its companion choice is unambiguous and isolated).
- `projects list`, `projects tasks list <project-id>`, `projects tasks update <project-id> <task-id>` (multi-resource chain).
- `failing-resource list` (returns exit non-zero for negative-cache sentinel test) and `failing-resource get <id>`, `failing-resource describe <id>` (two siblings sharing a failing companion).

**Companion-leaf invariant**: every get-shape command in the rich fixture has exactly ONE allowlisted sibling, and that sibling's leaf name is intentionally chosen to win alphabetically against any other commands the fixture might add later. `widgets`-family siblings: only `widgets list` is allowlisted (`describe`, `get` are not in `crossAPIListVerbs`/`cinemaListVerbs`). `projects tasks`-family: only `projects tasks list`. `failing-resource`-family: only `failing-resource list`. Tests explicitly assert the chosen companion via argv-log substring (e.g., `widgets list --json` must appear, not just any `widgets *` companion call) so future fixture additions can't silently change which companion wins.

argv logging:
- Defensive shell guard: `[ -n "${PRINTING_PRESS_TEST_ARGV_LOG:-}" ] && printf '%s\n' "$*" >> "$PRINTING_PRESS_TEST_ARGV_LOG"`.
- Tests opt in by setting `t.Setenv("PRINTING_PRESS_TEST_ARGV_LOG", filepath.Join(t.TempDir(), "argv.log"))` before invoking `RunLiveDogfood`.
- **`--help` invocations log too**: `companionSupportsLimit` calls `<companion> --help` as a separate subprocess before the actual companion call. Tests asserting on companion-call counts must filter argv lines to those containing `--json` (the actual companion call), not just the companion path.

Each command's `--help` block declares its flags accurately so `commandSupportsJSON`, `commandSupportsSearch`, `companionSupportsLimit`, and `extractPositionalPlaceholders` see the right surface. Per the U1 finding, `companionSupportsLimit` operates on raw help text — keep `--limit` out of help blocks for companions where the test wants the resolver to NOT append `--limit 1`.

**Patterns to follow:**
- `writeLiveDogfoodFixture` shell-script-template structure.
- `t.Setenv` for env-var isolation per test (avoids cross-test pollution).
- `t.TempDir()` for argv-log file isolation.

**Test scenarios** (5 — root-level scenario dropped per Open Questions, each becomes a distinct top-level test function with `TestRunLiveDogfoodResolveSuccess*` prefix):

- Happy path (`TestRunLiveDogfoodResolveSuccessSinglePositional`) — fixture exposes `widgets get <id>` with companion `widgets list --json` returning `{"results":[{"id":"42"}]}`. Probe substitutes id, runs `widgets get 42`, returns `LiveDogfoodStatusPass`. argv-log assertion: contains `widgets list --json` (companion-pin) and `widgets get 42` (probe).
- Happy path (`TestRunLiveDogfoodResolveSuccessChainedMultiPositional`) — fixture exposes `projects tasks update <project-id> <task-id>`. Probe walks `projects list --json` → `{"results":[{"id":"P1"}]}`, then `projects tasks list P1 --json` → `{"results":[{"id":"T7"}]}`, then runs `projects tasks update P1 T7`. argv log records both companion calls in chained order plus the final probe call.
- Happy path (`TestRunLiveDogfoodResolveSuccessCacheHit`) — fixture exposes `widgets get <id>` and `widgets describe <id>` sharing companion `widgets list`. Run live-dogfood once; assert `count(argv lines containing "widgets list --json") == 1` (filtered to exclude the `--help` probe), plus both `widgets get 42` and `widgets describe 42` probes appear in log.
- Happy path (`TestRunLiveDogfoodResolveSuccessCompanionLimit`) — fixture's `widgets list-with-limit --help` declares a `--limit` flag in its Flags section. Resolver appends `--limit 1` to the companion call. Probe `widgets get-with-limit <id>` resolves via this companion. Assert argv log contains a line matching both `widgets list-with-limit` AND `--limit 1` AND `--json` (substring, order-agnostic since resolver may emit `--json --limit 1` or `--limit 1 --json`).
- Edge case (`TestRunLiveDogfoodResolveSuccessNegativeCacheSentinel`) — fixture exposes `failing-resource get <id>` and `failing-resource describe <id>` sharing failing companion `failing-resource list` (exit 2 on `--json` call; exit 0 on `--help`). First probe runs `companionSupportsLimit` (`failing-resource list --help`, succeeds + cached) then the actual companion (`failing-resource list --json`, fails) — sentinel cached. Second probe hits the sentinel without re-invoking the actual companion. Assert `count(argv lines containing "failing-resource list" AND "--json") == 1` (the `--help` from probe 1 is filtered out and `companionSupportsLimit`'s help cache prevents a second `--help` call). Both probes return `LiveDogfoodStatusSkip` with reasons naming the failed companion (probe-1 reason includes `exit N`; probe-2 reason includes `previously failed`).

**Verification:**
- `go test ./internal/pipeline/...` passes including all five new test functions.
- Each test's argv-log assertion explicitly pins the chosen companion (e.g., `widgets list --json` not just `widgets list`) so future fixture additions can't silently flip companion choice.
- argv log assertions work reliably across consecutive test runs (no leakage between tests via `t.TempDir()` isolation).
- Each test exits cleanly without leaving zombie subprocesses or temp files (Go test framework handles cleanup).

---

- U3. **U3 search-aware error_path integration tests**

**Goal:** Add the seven search-shape and mutation-shape error_path tests from issue #579 AC #2 second list, layered on top of U2's rich fixture.

**Requirements:** R4

**Dependencies:** U2 (consumes `writeLiveDogfoodRichFixture` and its argv-logging convention). **Strictly serial after U2** — U3 extends the same `writeLiveDogfoodRichFixture` shell-script literal that U2 introduces. Parallel ce-work dispatch under worktree isolation would produce a literal-merge conflict in the multi-line raw string. Run sequentially.

**Files:**
- Modify: `internal/pipeline/live_dogfood_test.go` (extend `writeLiveDogfoodRichFixture` with `widgets search`, `widgets search-no-json`, `widgets search-positional`, `widgets delete` branches; add seven test functions)

**Approach:**

Rich-fixture additions for U3 (each command honors its OWN env-var key — see Key Technical Decisions on per-command mode dispatch):

- `widgets search` with `--query` flag and `--json` flag (declared in Flags section of help). Behavior dispatch via `PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE`:
  - unset or `empty` → exit 0 + `{"results":[]}`
  - `fallback` → exit 0 + `{"results":[{"id":"recent-1"},{"id":"recent-2"}]}`
  - `nonzero` → exit 4 + empty stdout
  - `invalid` → exit 0 + `{not-json` (the only fail mode currently)
- `widgets search-no-json` with `--query` flag but NO `--json` flag in help. Returns exit 0 + plain text `0 results found.` Used for the "no `--json` support" scenario.
- `widgets search-positional` with positional `<query>` (no `--query` flag in help) and `--json` flag. Branch: `<q> --json` returns exit 0 + `{"results":[]}`. Drives the positional-query-arg-construction assertion.
- `widgets delete <id>` (mutation-shape: no `--query` flag, no `<query>` positional, accepts id positional). Branch: `widgets delete __printing_press_invalid__` returns exit 2. Verifies mutation-shape commands fall through to the non-zero-required strategy.

The fixture surface grows but each branch is a small shell conditional. Structure shell branches by command then by env-var mode then by argv shape.

**Patterns to follow:**
- U2's argv-logging convention (and its `--help` filtering caveat).
- U2's companion-leaf invariant — none of U3's added commands should leak into the U2 widgets-family companion choice (`widgets search` IS in `crossAPIListVerbs`, but `widgets list` sorts alphabetically before `widgets search`, so `findListCompanion` still picks `widgets list` for `widgets get`/`widgets describe`. Tests should pin this via argv-log assertion).
- `writeLiveDogfoodFixture`'s shell-branch-per-arg-shape structure.
- `t.Setenv` for per-test env-var control. Each test sets exactly the env vars it needs; defaults handle the rest.

**Test scenarios** (7 — each becomes a distinct top-level test function with `TestRunLiveDogfoodSearchErrorPath*` prefix):

- Happy path (`TestRunLiveDogfoodSearchErrorPathEmptyResults`) — env vars unset (default empty). `widgets search --query __printing_press_invalid__ --json` returns exit 0 + `{"results":[]}`. error_path = `LiveDogfoodStatusPass`.
- Happy path (`TestRunLiveDogfoodSearchErrorPathFallbackResults`) — `t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "fallback")`. Same probe shape; fixture returns exit 0 + `{"results":[{"id":"recent-1"}]}`. error_path = `LiveDogfoodStatusPass` (recency-fallback APIs return content under unmatched queries).
- Happy path (`TestRunLiveDogfoodSearchErrorPathNoJSONSupport`) — probe runs `widgets search-no-json --query __printing_press_invalid__` (no `--json`); fixture returns exit 0 + plain text. error_path = `LiveDogfoodStatusPass` (exit 0 is sufficient when `--json` wasn't supplied).
- Happy path (`TestRunLiveDogfoodSearchErrorPathPositionalQuery`) — probe runs `widgets search-positional __printing_press_invalid__ --json`; fixture returns exit 0 + `{"results":[]}`. error_path = `LiveDogfoodStatusPass`. argv-log assertion: probe used positional argument, NOT `--query` flag.
- Happy path (`TestRunLiveDogfoodSearchErrorPathNonZeroExit`) — `t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "nonzero")`. Fixture returns exit 4. error_path = `LiveDogfoodStatusPass` (non-zero exit is also a valid "no match" signal for some APIs).
- Error path (`TestRunLiveDogfoodSearchErrorPathInvalidJSON`) — `t.Setenv("PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE", "invalid")`. Fixture returns exit 0 + `{not-json` under `--json`. error_path = `LiveDogfoodStatusFail` with reason matching `"invalid JSON"`. **Tripwire**: this is the only fail mode currently produced by `live_dogfood.go:693-711`'s search-shape switch. If a future production change adds a new fail branch (timeout, empty stdout under `--json`, schema mismatch), add a corresponding new `TestRunLiveDogfoodSearchErrorPath*` test in the same change.
- Mutation-shape (`TestRunLiveDogfoodSearchErrorPathMutationFallthrough`) — probe runs `widgets delete __printing_press_invalid__`; fixture returns exit 2. error_path = `LiveDogfoodStatusPass`. Verifies `commandSupportsSearch` correctly rejects mutation-shape commands (no `--query` flag, no `<query>` positional) and falls through to the existing non-zero-required strategy.

**Verification:**
- `go test ./internal/pipeline/...` passes including all seven new test functions.
- All seven scenarios use the same `writeLiveDogfoodRichFixture` from U2 — no fixture proliferation.
- `commandSupportsSearch` predicate correctness verified at integration level (search-shape commands take search strategy; mutation-shape commands take mutation strategy).

---

## System-Wide Impact

- **Interaction graph:** No production code changes. New tests interact with `RunLiveDogfood` and `writeTestManifestForLiveDogfood` (existing pattern). Argv-log env var (`PRINTING_PRESS_TEST_ARGV_LOG`) is a test-only side channel — never set in production code paths.
- **Error propagation:** Test failures surface via `assert`/`require` in standard Go testing pattern. argv-log read errors should `require.NoError` to fail tests fast; missing log files when expected indicate test setup bugs, not production issues.
- **State lifecycle risks:** None. Each test owns its `t.TempDir()` and `t.Setenv` scope. Go test framework cleans up subprocess and tempfile state.
- **API surface parity:** No public-API changes. `writeLiveDogfoodRichFixture` is package-internal (lowercase initial). Argv-log env-var name (`PRINTING_PRESS_TEST_ARGV_LOG`) follows the established `PRINTING_PRESS_*` naming convention but is documented only in test code, not in user-facing docs.
- **Integration coverage:** This plan IS the integration coverage. Unit tests in PR #577 already exist for the helpers (`extractFirstIDFromJSON`, `commandSupportsSearch`, `buildSiblingMap`, `findListCompanion`, `substitutePositionals`); this plan adds the missing end-to-end integration layer.
- **Unchanged invariants:** `writeLiveDogfoodFixture` interface (`(t *testing.T, brokenJSONFixed bool)`) is unchanged. Existing tests using it (`TestRunLiveDogfoodDetectsJSONParseFailure`, `TestRunLiveDogfoodErrorPathAcceptsExpectedNonZeroExit`, `TestRunLiveDogfoodAcceptanceRequiresManifestIdentity`, `TestRunLiveDogfoodExplicitBinaryNameMustExist`) continue to pass. Production code in `live_dogfood.go` and `dogfood.go` is unchanged.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Argv-log writes interleave under parallel subprocess invocation | Live-dogfood walker iterates the matrix sequentially, so writes are serialized by construction. Forbid `t.Parallel()` in this test file (no current test uses it; future contributors must not add it without redesigning the argv-log mechanism). |
| Drift between simple fixture and rich fixture as features land in one but not the other | Drift surfaces as test failures the next time someone touches either. Acceptable cost; both fixtures live in the same test file ~100 lines apart, making review at touch-time straightforward. |
| Rich-fixture shell script becomes hard to read as branches multiply | Structure shell branches by command then by env-var mode then by argv shape (matches existing `writeLiveDogfoodFixture` pattern). If branch count grows past ~50, consider splitting into per-command helper builders. Not a concern at this plan's scope (~25 branches across 8 commands). |
| Per-command env-var dispatch (`PRINTING_PRESS_TEST_WIDGETS_SEARCH_MODE` etc.) creates a proliferation of env vars | Each env var has a defensive default (`empty` when unset), is documented in the rich-fixture comment, and is scoped to exactly one command's behavior. Tests `t.Setenv` only the variables they need. The cost is one extra env var per command-with-multiple-modes, which is a small price for not having shared-mode pollution across the matrix walk. |
| Companion-leaf alphabetical-sort dependence (e.g., `widgets list` wins over `widgets search`) silently breaks if a future fixture rename or addition flips the order | Tests pin the chosen companion explicitly via argv-log substring assertion. Document the constraint in the rich-fixture comment so a reviewer adding new commands sees the invariant. |
| Companion `--help` invocations from `companionSupportsLimit` show up in argv log alongside actual companion calls | Tests filter argv lines on `--json` substring (the actual companion call), not the bare companion path. Documented as a Key Technical Decision and reinforced in U2's verification block. |
| `companionSupportsLimit` operates on raw help text (not flags-section-scoped, unlike `commandSupportsSearch`), making fixture help blocks load-bearing for any `--limit` token anywhere in the help | Documented as Key Technical Decision (U1's help-text scrub motivation). For companions where the test wants `--limit 1` appended, declare `--limit` in the Flags section. For companions where the test wants the bare call, keep `--limit` out of the help entirely (including Examples). The asymmetry between `companionSupportsLimit` and `commandSupportsSearch` is a latent inconsistency in PR #577's production code; this plan codifies it into fixture conventions rather than fixing it. |

---

## Documentation / Operational Notes

- No user-facing doc updates. The argv-log env var is test-internal and not documented in user-facing material.
- No skill or generator-template changes. AGENTS.md skill-authoring rule ("update SKILL.md when machine change alters what an agent should do") doesn't apply — no machine change here.

---

## Sources & References

- **Origin issue:** [issue #579](https://github.com/mvanhorn/cli-printing-press/issues/579) — Test coverage gap surfaced by `/ce-code-review` on PR #577.
- **Parent sub-issue:** [issue #573](https://github.com/mvanhorn/cli-printing-press/issues/573) — WU-2 from retro #571.
- **Parent retro:** [issue #571](https://github.com/mvanhorn/cli-printing-press/issues/571) — Movie Goat retro that surfaced the live-dogfood matrix accuracy gaps.
- **Implementing PR:** [PR #577](https://github.com/mvanhorn/cli-printing-press/pull/577) — WU-2 fix-up (camelCase ID, chained companion walk, search-aware error_path, quick-verdict gate). The production code being tested.
- **Parent plan:** `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md` — R3, R4, R5, R6 trace through to this plan's R3, R4.
- **Run artifact:** `/tmp/compound-engineering/ce-code-review/20260504-141500-907bca25/` — `testing.json` carries per-finding evidence from the original code review pass.
- Related code: `internal/pipeline/live_dogfood.go`, `internal/pipeline/live_dogfood_test.go`, `internal/pipeline/dogfood.go`.
