---
title: Live dogfood matrix accuracy — camelCase ID examples + kind-aware error_path
type: fix
status: active
date: 2026-05-04
deepened: 2026-05-04
origin: https://github.com/mvanhorn/cli-printing-press/issues/573
---

# Live dogfood matrix accuracy — camelCase ID examples + kind-aware error_path

## Summary

Cut false-fail noise from the live dogfood matrix on every CLI with camelCase ID positionals or search-family commands. Three coordinated fixes: (1) `exampleValue` recognises camelCase ID suffixes (with a string-type fence) so emitted Examples carry a UUID-shape placeholder instead of the literal `example-value`; (2) the matrix walks the sibling list-shape chain to source a real id for each id-shape positional in a `get`-shape command, with a per-companion cache so siblings share lookups, and skip-with-reason when any link in the chain is unreachable; (3) the error_path strategy branches on command shape — search commands accept exit 0 (any result shape) OR non-zero exit, mutating commands keep today's "non-zero exit" expectation. Also restates the quick-level verdict gate so skip-with-reason no longer flips PASS to FAIL.

---

## Problem Frame

The Movie Goat run produced 11 false-fail entries in dogfood: 5 from happy_path/json_fidelity probing get-shape commands with the placeholder string `example-value` (TMDb 404'd), and 6 from error_path probing search commands with `__printing_press_invalid__` (those commands correctly returned exit 0 with empty results — canonical Unix UX). Both classes are scorer accuracy bugs that recur on every CLI matching the same shapes (notion, linear, espn, pagliacci all carry camelCase IDs and search commands). The polish/promote gates trust the dogfood verdict, so the noise weakens an already load-bearing signal.

---

## Requirements

- R1. Specs declaring a camelCase ID positional (`movieId`, `seriesId`, `personId`) emit Examples that use the UUID-shape placeholder, not the literal `example-value`. The recognition rule includes a string-type fence so non-string positionals (booleans like `paid`/`valid`, numerics) flow into their own type branches instead of getting UUIDs.
- R2. Specs that already use snake_case (`movie_id`) or the bare `id` continue to emit the UUID-shape placeholder unchanged.
- R3. Live dogfood for a get-shape command sources a real id for each id-shape positional by walking the sibling list-shape chain — a single-positional get queries one list companion; a nested-resource get (e.g., `projects tasks update <project-id> <task-id>`) walks the chain, threading earlier-resolved ids into later list calls. A per-companion cache shares lookups across sibling get-shape commands. happy_path and json_fidelity then exercise the get path against real data.
- R4. When any link in the chain is unreachable (no sibling at the appropriate depth, companion errors, or no parseable id in the response), happy_path and json_fidelity for that get-shape command skip-with-reason, rather than failing with the placeholder.
- R5. Live dogfood error_path for a search-shaped command (heuristic: `--query` flag is present, or Usage suffix contains a `<query>` placeholder) accepts exit 0 (regardless of result shape) OR non-zero exit as a pass. Only malformed JSON when `--json` was supplied counts as a fail.
- R6. Live dogfood error_path for non-search commands (mutating writes/deletes, plain `get` with bogus id) preserves today's strategy: non-zero exit is required.
- R7. The quick-level verdict gate is restated as `Failed == 0 && (Passed + Skipped) >= 5 && MatrixSize >= 4` so the new skip-with-reason paths cannot flip a PASS run to FAIL purely from increased Skip count.

---

## Scope Boundaries

- Does NOT remove error_path coverage entirely — it adapts strategy by command shape.
- Does NOT fix the `<resource>` placeholder used by `export` (separate concern, lower frequency, called out as out-of-scope in #573).
- Does NOT extend `agent-context`'s emitted schema with a `kind` field. Search detection uses a help-text heuristic local to the live_dogfood subprocess. Schema extension would touch `agent_context.go.tmpl` and every printed CLI's contract for marginal benefit; revisit if a future scorer also wants kind-aware behavior.
- Does NOT migrate live_dogfood to consume `Param.Default` from the spec (adjacent food52-retro work — `liveDogfoodHappyArgs` still parses `--help` examples, not the spec). The substitution introduced in U2 is a runtime override on top of whatever placeholder the example carries.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/generator.go:2944` — `exampleValue(p spec.Param) string`. The first branch checks `strings.HasSuffix(nameLower, "_id") || nameLower == "id"`. CamelCase names lower to `movieid`/`seriesid`/`personid` and miss this branch.
- `internal/generator/generator.go:2982` — `exampleLine` consumes `exampleValue` for every positional. Used for both human Examples and the SKILL template's example block.
- `internal/pipeline/live_dogfood.go:195` — `runLiveDogfoodCommand`. Today's flow: extract Examples section from `--help`, parse the first runnable line via `liveDogfoodHappyArgs`, then run happy/json/error probes uniformly.
- `internal/pipeline/live_dogfood.go:350` — `liveDogfoodHappyArgs(command) ([]string, bool)`. Returns the parsed example args; no positional substitution today.
- `internal/pipeline/live_dogfood.go:260` — error_path branch. Appends `__printing_press_invalid__` and asserts non-zero exit uniformly across kinds.
- `internal/pipeline/runtime_commands.go:189` — `extractPositionalPlaceholders` already strips `[--flag=<val>]` descriptors and yields `<id>`/`<query>` placeholder names from a Usage suffix. Reusable for both U2 and U3.
- `internal/pipeline/dogfood.go:1779` — `extractFlagNames(text)` returns flag names from `--help`. Reusable for the `--query`-flag side of the search heuristic.
- `internal/pipeline/dogfood.go:1747` — `extractExamplesSection(helpOutput)` returns the Examples section. Already used by live_dogfood.

### Institutional Learnings

- **food52 retro #20260427-014521** — proposed canonical mock values for verify positionals; `canonicalargs` registry plus `Param.Default` lookup chain (see `internal/pipeline/runtime_commands.go:resolvePositionalValue`). This plan extends that direction into live dogfood by sourcing real ids from sibling list calls. The two are complementary: verify-mock uses static fixtures; live dogfood uses runtime-discovered fixtures.
- **AGENTS.md anti-pattern** — "Never change the machine for one CLI's edge case." The companion-leaf allowlist must be the cross-API set, not a TMDb-specific list.
- **AGENTS.md golden harness** — "Run `scripts/golden.sh verify` whenever a change may affect CLI command output." U1 changes Example-line bytes for the golden fixture's `projectId`/`taskId` positionals; the fixture update is intentional and documented in U1's verification.

### External References

- TMDb v3 API (`api.themoviedb.org/3`) — primary live test target for U2/U3. Movie list endpoints (`/movie/popular`, `/movie/top_rated`, `/movie/now_playing`) return `{"results":[{"id":N, ...}]}`. Search endpoints (`/search/movie`) return exit 0 with `{"results":[]}` on no-match.

---

## Key Technical Decisions

- **camelCase recognition rule, with type fence.** Extend the ID branch in `exampleValue` to: `nameLower == "id"` (existing) OR `strings.HasSuffix(nameLower, "_id")` (existing) OR `strings.HasSuffix(nameLower, "id") && len(nameLower) > 2 && (p.Type == "string" || p.Type == "")` (new). Rationale: matches `movieid`/`seriesid`/`personid`/`userid`/`groupid`/`pageid`/`issueid`/`eventid`/`teamid` — the cross-API set. The `string`-or-empty type fence prevents boolean positionals like `paid`/`valid` and numeric positionals from getting UUIDs (they fall through to the existing `boolean` / `integer` branches). Pure-string false positives (`acid`, `arid`, `void` as required URL path params) are accepted as a documented risk — they would produce one Example with a UUID and a clear 404 signal at verify time, strictly better than today's literal `example-value`. Add a denylist only when a real-spec FP surfaces.
- **Search detection is help-text heuristic, not schema.** Reuse `extractFlagNames` to detect `--query`, and `extractPositionalPlaceholders` to detect `<query>` in the Usage suffix. Either signal flips the command into search-shaped. Local to the live_dogfood subprocess, no contract change. Defers schema-level kind dispatch until a second scorer needs it.
- **Companion-leaf allowlist — two named sets.** Detection: the appropriate ancestor's parent path plus any leaf below.
  - `crossAPIListVerbs = {list, all, index, query, find, search, discover, browse, recent, feed}` — generic verbs that appear across modern API conventions (Notion's `query`, Stripe's `list`, content-feed APIs' `recent`/`feed`/`browse`, Algolia/Elastic-style `search`).
  - `cinemaListVerbs = {popular, trending, top_rated, latest, now_playing, upcoming, airing_today, on_the_air}` — TMDb / cinema-API specific. Kept because TMDb's printed CLI does not expose a plain `list` leaf.
  - The union is checked at companion-resolve time. Future additions go to the right bucket (generic vs domain-specific) so the cinema bucket doesn't leak coverage promises across non-cinema CLIs.
- **ID extraction try-list.** In order: `.results[0].id`, `.[0].id`, `.items[0].id`, `.data[0].id`, `.list[0].id`, `.data.<any>.nodes[0].id`, `.data.<any>.edges[0].node.id`. First match wins; coerced to string. Rationale: covers TMDb (`results`), top-level array (`[0]`), GitHub REST (`items`), Stripe (`data`), the long-tail (`list`), and GraphQL connection shapes (`.data.<resource>.nodes` for Shopify/Linear/Notion-database, `.data.<resource>.edges[].node` for GitHub GraphQL/Relay-style). The `<any>` wildcard means: walk any single-key field under `.data` and try the child path. When all seven miss, fall through to skip-with-reason — better than substituting a guessed value.
- **Multi-positional resolution via chained companion walk.** For each id-shape positional in the get-shape command, walk up to the appropriate ancestor list-shape sibling, run it with already-resolved parent ids threaded into its argv as positional context, and extract the next id from the response. Bounded by command-tree depth (typically 1-3 levels). Example for `projects tasks update <project-id> <task-id>`: source `<project-id>` from `projects list --json`, source `<task-id>` from `projects tasks list <project-id> --json`, then run `projects tasks update <project-id> <task-id>`. Non-id-shape positionals in the chain (e.g., `<query>` somewhere in the path) abort the chain to skip-with-reason. Rationale: nested resources are common (subresources, comments, items, members); without the chain, a meaningful slice of get-shape commands across the catalog stay at FAIL. The chained walk converts those to live-tested PASS.
- **Per-companion cache.** A run-scoped `map[string]string` keyed by the companion's full argv (path joined with parent ids) caches the extracted id. Sibling get-shape commands sharing a parent reuse one companion subprocess. Combined with the chained walk, this caps total companion subprocess count at O(unique-parent-paths × depth), not O(get-commands × depth).
- **Skip-with-reason on chain failure.** When any link in the chain fails (no sibling at depth, companion errors, no id parseable), happy_path and json_fidelity become `Status=Skip` with a structured reason naming the failed link (`"no list companion at depth N for <name>"`, `"list companion failed at depth N: exit <X>"`, `"no id parseable from companion at depth N"`). Today's behavior is FAIL with the placeholder; skip-with-reason is strictly better signal because the fail was always a scorer artifact, never a real defect.
- **Quick-level verdict gate restated.** Today's gate `MatrixSize == 6 && Passed >= 5` flips to FAIL whenever a Skip drops MatrixSize below 6 — a regression directly caused by this PR's new skip paths. Restate to `Failed == 0 && (Passed + Skipped) >= 5 && MatrixSize >= 4`: any non-failure pattern with at least 4 entries (allowing for one or two skips) and zero Failed counts as PASS. Lower MatrixSize floor (4) accommodates the case where both quick-selected commands skip happy_path + json_fidelity due to no companion. The `Failed == 0` clause keeps real failures fatal.
- **Error_path skip-on-no-positional preserved.** Today's branch already skips error_path when the command has no positional. That's correct and unchanged. The new search dispatch only branches the strategy when a positional IS present.
- **Search-shape error_path: exit 0 = PASS regardless of result shape.** When the command is search-shaped, accept exit 0 with any result shape (empty or non-empty results) OR non-zero exit as PASS. Only fail on exit 0 with malformed JSON when `--json` was supplied. Drops the "exit 0 + non-empty results = FAIL" branch from earlier drafts because real-world content/feed APIs intentionally return recent items as a fallback for unmatched queries (canonical UX). Detecting broken filter logic is a unit-test concern, not a live-dogfood signal — a search-error_path test that fires on production data shape variance would block legitimate ships.

---

## Open Questions

### Resolved During Planning

- **Should `exampleValue` also handle `Id` exactly (PascalCase single token)?** Resolved: `nameLower == "id"` already covers `Id` after lowercasing. No additional clause needed.
- **Should U2 substitution run even when the placeholder isn't ID-shaped?** Resolved: only run substitution when the positional name (extracted from Usage) ends in `id` (the same check U1 wires into the generator). Rationale: substituting a real id into a `<query>` slot would corrupt the test; the heuristic is "only run companion lookup for id-shaped positionals." Non-id-shape positionals encountered mid-chain abort the walk to skip-with-reason.
- **Multi-positional commands.** Resolved: chained companion walk (see Key Technical Decisions) — each id-shape positional is sourced from the appropriate ancestor list-shape sibling, with earlier-resolved ids threaded into later list calls. Skip-with-reason if any link fails.
- **Companion subprocess fan-out.** Resolved: per-companion cache keyed by full argv (path + parent ids) so sibling get-shape commands share one subprocess. Cap is O(unique-parent-paths × depth), not O(get-commands × depth).
- **Companion `--limit` flag detection.** Resolved (in U2 Approach): when the companion supports `--limit` (detected via `extractFlagNames(companion.Help)`), run with `--limit 1` to minimize the API call. If absent, run without and accept the full response.
- **Quick-level verdict gate.** Resolved (R7): restate as `Failed == 0 && (Passed + Skipped) >= 5 && MatrixSize >= 4` so skip-with-reason cannot flip PASS to FAIL purely from increased Skip count.
- **Search error_path strategy on non-empty results.** Resolved: exit 0 = PASS regardless of result shape. Detecting broken filter logic is a unit-test concern, not a live-dogfood signal. Real-world content/feed APIs return fallback items on unmatched queries.
- **Empty-results predicate wording.** Resolved: the predicate looks for "any of {results, items, data, list} key whose value is an empty array" — not strict object equality. TMDb's `{"results":[],"total_results":0,"page":1}` matches because `results` exists with empty array; multi-key responses are not rejected.
- **Does the heuristic-based search detection misclassify any existing commands?** Resolved: scan the catalog for commands carrying both `--query` flag and a mutating verb. None found in current catalog (search/list/find use `--query`; create/delete/update use bodies). Risk is low; reassessable from future retros.

### Deferred to Implementation

- **JSON id type coercion for non-string ids.** TMDb returns `id` as a number; some APIs return strings. The extracted value is converted via `fmt.Sprint` so both shapes serialize correctly into the substituted positional. Confirm at impl time that `json.Decoder.UseNumber()` is wired (not the default float64 decode) so very large ids (e.g., Twitter snowflake > 2^53) survive coercion without scientific-notation rounding.
- **Whether to retain the placeholder result on companion-success but get-failure.** When the chain succeeds and substituted ids are sourced cleanly but the get probe still fails (genuine API/auth issue), happy_path stays FAIL with the standard exit-code reason. No special "chain succeeded but get failed" status — the existing failure signal is correct.
- **Cache-population ordering.** The cache is built lazily as commands run. The matrix iterates alphabetically; sibling get-shape commands sharing a parent will populate the cache on the first call and reuse on subsequent calls. Confirm at impl time that no parallel iteration breaks the cache contract (today's iteration is sequential).

---

## Implementation Units

- U1. **camelCase ID recognition in `exampleValue`**

**Goal:** Recognise camelCase ID suffixes so generated Examples carry the UUID-shape placeholder instead of `example-value`.

**Requirements:** R1, R2

**Dependencies:** None.

**Files:**
- Modify: `internal/generator/generator.go` (`exampleValue` at line 2944)
- Test: `internal/generator/generator_test.go` (add or extend `TestExampleValue`)
- Update (golden): `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/cli/projects_tasks_update-project.go`
- Update (golden): `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/cli/projects_tasks_list-project.go`
- Update (golden, if any other files reference `example-value` for these positionals): inspect via `grep -r "example-value" testdata/golden/expected/`

**Approach:**
- Extend the first `if` in `exampleValue` to a three-clause check with type fence:
  - `nameLower == "id"` (existing)
  - OR `strings.HasSuffix(nameLower, "_id")` (existing)
  - OR `strings.HasSuffix(nameLower, "id") && len(nameLower) > 2 && (p.Type == "string" || p.Type == "")` (new — camelCase + string-type fence)
- The three clauses are flat OR — preserve order so the existing two paths stay first and the new one is the catch-all for camelCase shapes.
- The `(p.Type == "string" || p.Type == "")` fence prevents boolean positionals (`paid`, `valid`) and numeric positionals from getting UUIDs — they fall through to the existing `p.Type == "boolean"` / `p.Type == "integer"` branches later in the function. Spec authors that don't set `p.Type` (legacy internal YAML or browser-sniffed specs) get the UUID by default; this is the safer fallback for unknown-type positionals.
- After the change, run `scripts/golden.sh verify` — expect a deterministic byte-level diff in the two project Example lines (`example-value` → `550e8400-e29b-41d4-a716-446655440000` for `projectId` and `taskId`). Run `scripts/golden.sh update` to refresh, inspect, and explain the diff in the PR body.

**Patterns to follow:**
- Existing `exampleValue` style (flat string-suffix checks, no helper extraction).
- Golden-update workflow per AGENTS.md "Golden Output Harness" section.

**Test scenarios:**
- Happy path — `exampleValue({Name: "movieId", Type: "string"})` returns `550e8400-e29b-41d4-a716-446655440000`.
- Happy path — `exampleValue({Name: "seriesId", Type: "string"})` returns the UUID.
- Happy path — `exampleValue({Name: "personId", Type: "string"})` returns the UUID.
- Edge — `exampleValue({Name: "movieId"})` (no Type) returns the UUID — empty Type passes the fence.
- Edge — `exampleValue({Name: "id", Type: "string"})` returns the UUID (existing equality clause unchanged).
- Edge — `exampleValue({Name: "movie_id", Type: "string"})` returns the UUID (existing snake_case clause unchanged).
- Edge — `exampleValue({Name: "ID", Type: "string"})` returns the UUID (case-insensitive via `nameLower`).
- Edge — `exampleValue({Name: "userIdentifier", Type: "string"})` does NOT return UUID — name does not end in `id`. Falls through to default `"example-value"`.
- Edge — `exampleValue({Name: "", Type: "string"})` does NOT return UUID — empty string fails `len > 2`.
- Edge — `exampleValue({Name: "ix", Type: "string"})` does NOT return UUID — does not end in `id`.
- Type fence — `exampleValue({Name: "paid", Type: "boolean"})` does NOT return UUID; falls through to the boolean branch and returns `"true"`. Same for `valid`/`unpaid`.
- Type fence — `exampleValue({Name: "amountId", Type: "integer"})` does NOT return UUID; falls through to the integer branch and returns `"42"`.
- Type fence — `exampleValue({Name: "creditValid", Type: "boolean"})` does NOT return UUID; ends in `id` but type is boolean — returns `"true"`.
- Accepted FP — `exampleValue({Name: "void", Type: "string"})` DOES return UUID. Documented in Risks; if a real-spec FP surfaces, add a denylist entry.

**Verification:**
- `go test ./internal/generator/...` passes including new cases.
- `scripts/golden.sh verify` produces diffs only in the two `projectId`/`taskId` example lines; running `scripts/golden.sh update` produces a clean updated fixture; manual inspection confirms the only changed bytes are the placeholder values.

---

- U2. **Chained companion walk — multi-positional ID resolution in live dogfood**

**Goal:** For each id-shape positional in a get-shape command, source a real id by walking the sibling list-shape chain — single-positional gets query one list companion; nested-resource gets walk the chain, threading earlier-resolved ids into later list calls. A per-companion cache shares lookups across siblings. Skip-with-reason if any link fails. happy_path and json_fidelity exercise the get path against real data instead of failing on a placeholder.

**Requirements:** R3, R4

**Dependencies:** None — independent of U1 in code (U1 changes the placeholder bytes; U2 substitutes them at runtime regardless of which placeholder shipped).

**Files:**
- Modify: `internal/pipeline/live_dogfood.go` (extend `RunLiveDogfood` to build a sibling index and companion cache; extend `runLiveDogfoodCommand` signature to accept them; add `resolveCommandPositionals` helper, `findListCompanionAtDepth` helper, and `extractFirstIDFromJSON` helper)
- Test: `internal/pipeline/live_dogfood_test.go` (extend the fake-binary fixture with sibling list companions at multiple depths; add tests for single-positional resolve, multi-positional chain resolve, GraphQL-shape resolve, sibling cache hit, resolve-skip at each chain depth)

**Approach:**

*Setup (in `RunLiveDogfood`):* After `discoverLiveDogfoodCommands`, build `siblings map[string][]liveDogfoodCommand` keyed by joined parent path (`strings.Join(path[:len(path)-1], " ")`); root-level commands key on `""`. Build an empty `companionCache := &companionCache{results: map[string]string{}}` shared across the iteration. Thread both into `runLiveDogfoodCommand`.

*Companion-leaf check:* `isCompanionLeaf(name string) bool` returns true if `name` is in the union of `crossAPIListVerbs` and `cinemaListVerbs` (see Key Technical Decisions for the two named sets).

*Chained walk (in `resolveCommandPositionals`):*

```
resolveCommandPositionals(command, happyArgs, siblings, cache, binaryPath, cliDir, timeout):
    placeholders := extractPositionalPlaceholders(usageSuffix(command.Help))
    if len(placeholders) == 0:
        return happyArgs, resolveOK, ""

    resolved := []string{}  // accumulates ids for chain context
    for i, name := range placeholders:
        nameLower := strings.ToLower(name)
        if !strings.HasSuffix(nameLower, "id") || len(nameLower) <= 2:
            // Non-id-shape positional in a chain: abort.
            return nil, resolveSkip, fmt.Sprintf(
                "non-id positional %q at depth %d", name, i)

        // Walk up: positional at depth i sources from a list-shape
        // sibling at the appropriate ancestor level. For a path like
        // [movies, seasons, episodes, get], depth-0 (<series-id>)
        // sources from siblings of [movies], depth-1 (<season-id>)
        // sources from siblings of [movies, <id>, seasons], depth-2
        // (<episode-id>) sources from siblings of [movies, <id>,
        // seasons, <id>, episodes].
        //
        // Practically: the ancestor's parent path is
        //   command.Path[: len(command.Path) - len(placeholders) + i]
        // Excluding the trailing get verb, the ancestor's siblings live
        // at that parent-path key.
        ancestorParentPath := strings.Join(
            command.Path[:len(command.Path) - len(placeholders) + i], " ")
        listCmd := findListCompanionAtDepth(siblings, ancestorParentPath)
        if listCmd == nil:
            return nil, resolveSkip, fmt.Sprintf(
                "no list companion at depth %d for %q", i, name)

        // Build companion args: list path + already-resolved parent ids
        // as positional context + --json, plus --limit 1 if supported.
        listArgs := append([]string{}, listCmd.Path...)
        listArgs = append(listArgs, resolved...)
        listArgs = append(listArgs, "--json")
        if companionSupportsLimit(listCmd, binaryPath, cliDir, timeout):
            listArgs = append(listArgs, "--limit", "1")

        cacheKey := strings.Join(listArgs, "\x00")
        if id, ok := cache.results[cacheKey]; ok:
            resolved = append(resolved, id)
            continue

        run := runLiveDogfoodProcess(binaryPath, cliDir, listArgs, timeout)
        if run.exitCode != 0:
            return nil, resolveSkip, fmt.Sprintf(
                "list companion failed at depth %d: exit %d", i, run.exitCode)

        id, ok := extractFirstIDFromJSON(run.stdout)
        if !ok:
            return nil, resolveSkip, fmt.Sprintf(
                "no id parseable from companion at depth %d", i)

        cache.results[cacheKey] = id
        resolved = append(resolved, id)

    // Substitute resolved ids into happyArgs. Walk happyArgs left-to-right
    // after command.Path; each non-flag arg corresponds to the next
    // positional in `placeholders`. Replace those slots with `resolved`.
    return substitutePositionals(happyArgs, command.Path, resolved), resolveOK, ""
```

*Cache structure:*

```
type companionCache struct {
    // key = listArgs joined with NUL (\x00) so paths and ids can't collide
    // value = extracted id from that companion's response
    results map[string]string
}
```

*ID extraction (`extractFirstIDFromJSON`):* Try paths in order and return the first match coerced via `fmt.Sprint`:
- `.results[0].id`
- `.[0].id` (top-level array)
- `.items[0].id`
- `.data[0].id`
- `.list[0].id`
- `.data.<any>.nodes[0].id` (GraphQL connection — Shopify, Linear, Notion)
- `.data.<any>.edges[0].node.id` (Relay-style — GitHub GraphQL)

The `<any>` walks any single-key field under `.data` and tries the child path. Use `json.Decoder.UseNumber()` so large numeric ids (e.g., Twitter snowflake > 2^53) coerce cleanly via `fmt.Sprint(json.Number(...))` without scientific notation. If all seven paths miss, return `("", false)`.

*Companion `--help` for `--limit` detection:* `companionSupportsLimit` runs `companion.Path + ["--help"]` once per companion (cached on the `liveDogfoodCommand` entry by mutating its `.Help` field, or via a parallel map). The companion's Help is needed to call `extractFlagNames` to check for `--limit`. Lazy-load is acceptable because companion lookup runs only for id-shape commands and the cache prevents repeat probes.

*Wire into `runLiveDogfoodCommand`:* After `liveDogfoodHappyArgs` returns `(happyArgs, true)`, call `resolveCommandPositionals`. On `resolveSkip`, append `skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, reason)` and `skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, reason)`, then continue to the error_path branch (which has its own positional gate and is U3's domain). On `resolveOK` with rewritten args, proceed with the existing happy_path / json_fidelity flow using the new args.

**Patterns to follow:**
- `runLiveDogfoodProcess` for subprocess invocation.
- `extractPositionalPlaceholders` (`runtime_commands.go:196`) for placeholder parsing.
- `extractFlagNames` (`dogfood.go:1779`) for companion `--limit` detection.
- `skippedLiveDogfoodResult` for skip status.

**Test scenarios:**
- Covers R3 (single-positional happy) — fixture exposes `widgets list` returning `{"results":[{"id":"42"}]}` and `widgets get` with `<id>` positional. Matrix probes `widgets get`; chain resolves to `["42"]`; the get probe runs against the fake binary's id=42 path and passes happy_path + json_fidelity.
- Covers R3 (chained multi-positional happy) — fixture exposes `widgets list` returning `{"results":[{"id":"P1"}]}`, `widgets sublist` taking `<widget-id>` positional and returning `{"results":[{"id":"S7"}]}`, and `widgets subwidgets get` taking `<widget-id> <subwidget-id>`. Matrix probes the get command; chain resolves to `["P1", "S7"]` (the sublist call ran with `widgets sublist P1 --json`). get probe runs with `widgets subwidgets get P1 S7` and passes.
- Edge (cinema verb) — companion is `widgets popular` (in `cinemaListVerbs`). Resolution succeeds.
- Edge (top-level array) — companion shape `[{"id":"42"}]`. Resolution succeeds via `.[0].id`.
- Edge (items shape) — companion shape `{"items":[{"id":"42"}]}`. Resolution succeeds via `.items[0].id`.
- Edge (numeric id) — companion shape `{"data":[{"id":42}]}`. Resolution succeeds and serializes to `"42"` via `json.Decoder.UseNumber` + `fmt.Sprint`.
- Edge (large numeric id) — companion returns `{"results":[{"id":1234567890123456789}]}` (snowflake size). Resolution serializes correctly without scientific notation.
- Edge (Shopify GraphQL) — companion shape `{"data":{"products":{"nodes":[{"id":"gid://shopify/Product/42"}]}}}`. Resolution succeeds via `.data.<any>.nodes[0].id` returning `"gid://shopify/Product/42"`.
- Edge (Relay-style GraphQL) — companion shape `{"data":{"viewer":{"repos":{"edges":[{"node":{"id":"R_kgABC123"}}]}}}}`. Resolution succeeds via `.data.<any>.edges[0].node.id` (the wildcard tolerates any single-key under `.data`, here `viewer`).
- Cache hit — fixture has two get-shape commands `widgets get` and `widgets describe` sharing parent `widgets`. The `widgets list` companion is invoked once on the first get; the second get hits the cache. Verify by counting subprocess invocations recorded by the fake binary.
- Covers R4 (no companion) — fixture removes the companion. happy_path = SKIP with reason `"no list companion at depth 0 for id"`. json_fidelity = SKIP with same reason.
- Covers R4 (companion errors) — companion exists but exits non-zero. happy_path = SKIP with reason `"list companion failed at depth 0: exit 2"`.
- Covers R4 (no id in response) — companion returns `{"results":[]}`. happy_path = SKIP with reason `"no id parseable from companion at depth 0"`.
- Covers R4 (chain breaks at depth 1) — first list resolves; second list missing or errors. happy_path = SKIP with reason naming depth 1.
- Covers R4 (non-id-shape mid-chain) — multi-positional command where one positional is `<query>`. happy_path = SKIP with reason `"non-id positional \"query\" at depth N"`. Deliberately fail closed — chain resolution requires every positional in the chain to be id-shape.
- Edge (no positional) — command has no positional. resolveCommandPositionals returns happyArgs unchanged; existing flow exercised.
- Edge (companion supports `--limit`) — resolve runs with `--limit 1`. Verifiable by recording the args passed to the fake binary.
- Edge (root-level get) — top-level get like `accounts get <id>` (no parent path). Sibling map keys on `""`; resolution still works.

**Verification:**
- `go test ./internal/pipeline/...` passes including the new cases.
- Test fixture demonstrates happy_path moving from FAIL→PASS when a companion is wired and from FAIL→SKIP when no companion is reachable.
- Manual probe against a printed Movie Goat CLI (post-U1 regen) shows `movies get`, `tv get`, `people get` happy_path passing live with TMDb when authed, and skip-with-reason when run without a companion fixture.

---

- U3. **Search-aware error_path dispatch in live dogfood**

**Goal:** Recognise search-shaped commands and accept either non-zero exit OR exit 0 with empty results under `--json` as a pass; preserve today's "non-zero exit required" behavior for non-search commands.

**Requirements:** R5, R6

**Dependencies:** None — independent of U1 and U2.

**Files:**
- Modify: `internal/pipeline/live_dogfood.go` (extend the error_path branch in `runLiveDogfoodCommand`; add `commandSupportsSearch` helper; add `errorPathEmptyResults` JSON-shape predicate)
- Test: `internal/pipeline/live_dogfood_test.go` (extend the fixture with a `widgets search` command supporting `--query`; add tests for search-pass-on-empty-results, search-pass-on-non-zero, mutation-still-fails-on-zero-exit)

**Approach:**
- Add `commandSupportsSearch(help string) bool`:
  - Returns true if `extractFlagNames(help)` contains `query`, OR
  - The Usage suffix (via `liveDogfoodUsageSuffix`) yields a placeholder named `query` from `extractPositionalPlaceholders`.
- Extend the error_path branch (`runLiveDogfoodCommand` line 260):
  - If `liveDogfoodCommandTakesArg(command.Help)` is false, current skip path is unchanged.
  - If `commandSupportsSearch(command.Help)` is true (search-shaped strategy):
    - Build args: prefer `--query __printing_press_invalid__ --json` when `--query` flag is supported; otherwise positional `__printing_press_invalid__ --json`. If `--json` is not supported, drop it.
    - Run the probe.
    - **Pass criteria** (broad): exit 0 with valid JSON when `--json` was supplied; OR exit 0 with any output when `--json` was not supplied; OR exit non-zero (consistent with mutation behavior — non-zero is also a valid "no match" signal for some APIs).
    - **Fail criteria** (narrow): exit 0 with `--json` supplied but stdout is not valid JSON. That's the only fail condition.
    - Note: this strategy does NOT require an empty results array. Real-world content/feed APIs return recent items as a fallback for unmatched queries; treating non-empty results as FAIL would block legitimate ships. Detecting broken filter logic is a unit-test concern, not a live-dogfood signal.
  - Else (mutation-shaped strategy, today's behavior preserved): non-zero exit required.
- The mutation-side branch (write/delete with bogus body) is unchanged: `commandSupportsSearch` returns false for those (no `--query`, no `<query>` placeholder), so they fall through to the existing strategy.
- The empty-results predicate from earlier drafts is dropped — search-shape PASS no longer depends on result emptiness, only on exit code and JSON validity.

**Patterns to follow:**
- `extractFlagNames`, `extractPositionalPlaceholders`, `liveDogfoodUsageSuffix` — already used in this file.
- `commandSupportsJSON` for the existing helper shape (one-line predicate over `extractFlagNames`).

**Test scenarios:**
- Covers R5 (happy, --json + empty results) — fixture exposes `widgets search` with `--query` flag and `--json`. Probe runs `widgets search --query __printing_press_invalid__ --json`; fixture returns exit 0 + `{"results":[]}`. error_path = PASS.
- Covers R5 (happy, --json + non-empty results from fallback API) — same fixture but returns exit 0 + `{"results":[{"id":"recent-1"},{"id":"recent-2"}]}` (simulating a content-feed API's recency fallback). error_path = PASS — non-empty under exit 0 is no longer a fail.
- Covers R5 (alt happy, no --json) — search command without `--json` support. Probe runs `widgets search --query __printing_press_invalid__` (no --json); fixture returns exit 0 + `0 results found.` to stdout. error_path = PASS — exit 0 alone is sufficient when --json wasn't supplied.
- Covers R5 (alt happy, positional <query>) — search command via positional `<query>` (no `--query` flag). Probe runs `widgets search __printing_press_invalid__ --json`; fixture returns exit 0 + `{"results":[]}`. error_path = PASS.
- Covers R5 (non-zero exit) — search command returns exit non-zero (e.g., 4xx). error_path = PASS (consistent with non-search behavior).
- Edge (only fail mode) — search command claims `--json` support but emits non-JSON when `--json` was supplied. error_path = FAIL with reason `"invalid JSON"`. This is the sole fail condition for search-shape commands.
- Covers R6 (mutation) — fixture exposes `widgets delete` (no `--query`, no `<query>` placeholder). Probe runs `widgets delete __printing_press_invalid__`; fixture returns exit 2. error_path = PASS (existing behavior preserved).
- Covers R6 (plain get) — fixture exposes `widgets get` (read-shape, no `--query`, no `<query>` — id positional). Probe runs `widgets get __printing_press_invalid__`; fixture returns exit 2. error_path = PASS (existing behavior preserved).
- Edge — `commandSupportsSearch` returns true ONLY when query signal is present. Verify with a fixture that has another flag (e.g., `--queue`) — extractFlagNames must return `queue`, not match `query` partially.

**Verification:**
- `go test ./internal/pipeline/...` passes including new cases.
- Test fixture demonstrates error_path moving from FAIL→PASS for the search command, while remaining PASS for the mutation/get cases.
- Run live dogfood against a printed Movie Goat CLI (post-U1+U2 regen): the 6 false-fail error_path entries from the original retro (`movies search`, `people search`, `tv search`, `multi`, `search`, `auth set-token`) should now PASS.

---

- U4. **Quick-level verdict gate restated**

**Goal:** Update `finalizeLiveDogfoodReport` so the new skip-with-reason paths (introduced by U2) cannot flip a quick-level run from PASS to FAIL purely by reducing MatrixSize.

**Requirements:** R7

**Dependencies:** Conceptually paired with U2 (both touch the `Skip` outcome). Implementation order doesn't matter — U4 can land first as a no-op for today's data, then U2 lights up the new skip paths.

**Files:**
- Modify: `internal/pipeline/live_dogfood.go` (`finalizeLiveDogfoodReport` switch around line 410)
- Test: `internal/pipeline/live_dogfood_test.go` (add tests covering the new gate semantics)

**Approach:**
- Change the verdict switch from:
  ```go
  case report.Level == "quick" && report.MatrixSize == 6 && report.Passed >= 5:
      report.Verdict = "PASS"
  case report.Failed > 0 || report.MatrixSize == 0:
      report.Verdict = "FAIL"
  case report.Level == "quick" && report.MatrixSize != 6:
      report.Verdict = "FAIL"
  ```
  to:
  ```go
  case report.Failed > 0 || report.MatrixSize == 0:
      report.Verdict = "FAIL"
  case report.Level == "quick" && (report.Passed + report.Skipped) >= 5 && report.MatrixSize >= 4:
      report.Verdict = "PASS"
  case report.Level == "quick":
      report.Verdict = "FAIL"
  ```
- Semantics: PASS at quick-level requires zero Failed entries, at least 5 entries that aren't Failed (Passed + Skipped), and a minimum MatrixSize floor of 4 to guard against pathological cases (every test skipping). FAIL still fires for any real failure or empty matrix.
- The switch keeps the `case report.Failed > 0` arm first so any real failure dominates over the quick-PASS arm.

**Patterns to follow:**
- Existing `finalizeLiveDogfoodReport` switch shape.
- Existing `LiveDogfoodStatus` enum (Pass/Fail/Skip already accounted for in the count loop above the switch).

**Test scenarios:**
- Covers R7 (Pass + 1 Skip) — quick-level run with 4 Pass + 1 Skip + 0 Fail = MatrixSize 4 (Skip excluded). Total non-failed = 5. Verdict = PASS (today's gate would FAIL because MatrixSize != 6).
- Covers R7 (Pass + 2 Skips) — 3 Pass + 2 Skip + 0 Fail = MatrixSize 3, non-failed = 5. MatrixSize floor = 4 not met. Verdict = FAIL (the matrix collapsed too far to trust).
- Edge (zero failures, full pass) — 6 Pass + 0 Skip + 0 Fail = MatrixSize 6, non-failed = 6. Verdict = PASS (existing behavior preserved).
- Edge (one failure) — 4 Pass + 1 Skip + 1 Fail = MatrixSize 5, non-failed = 5. Verdict = FAIL (Failed > 0 dominates).
- Edge (all skip) — 0 Pass + 6 Skip + 0 Fail = MatrixSize 0. Verdict = FAIL (matrix size 0 dominates).
- Full-level unchanged — full-level runs continue to use the `Failed > 0 || MatrixSize == 0` arm; the quick-specific arm doesn't affect them.

**Verification:**
- `go test ./internal/pipeline/...` passes including new cases.
- A regen of Movie Goat at quick-level with companion-resolved gets shows quick-PASS even when one or two get-shape commands skip happy_path/json_fidelity due to no companion.

---

## System-Wide Impact

- **Interaction graph.** Changes are contained to two files (`internal/generator/generator.go`, `internal/pipeline/live_dogfood.go`) plus their tests. No call into the printed CLI runtime, no template change, no `agent-context` schema change.
- **Error propagation.** U2 introduces a new "skip-with-reason" status path for happy_path/json_fidelity, and U4 restates the quick-level verdict gate so this skip path no longer flips PASS to FAIL. The `finalizeLiveDogfoodReport` accumulator's count loop (line 397) already routes `Status=Skip` correctly into the `Skipped` counter; the gate change consumes that counter explicitly via the new `(Passed + Skipped) >= 5` arm.
- **State lifecycle risks.** Within request-scoped subprocess execution. The new `companionCache` is run-scoped (built and dropped per `RunLiveDogfood` invocation); no cross-run persistence, no concurrent-mutation risk because the live_dogfood iteration is sequential.
- **API surface parity.** Generator and scorer are both internal to the printing-press binary; no public Go package contract widens. The agent-context schema is unchanged (Decision: defer kind dispatch).
- **Integration coverage.** U2's chained-walk and cache paths cross the subprocess boundary inside the live_dogfood test fixture. Unit-level fake-binary tests are sufficient; the existing `live_dogfood_test.go` shows the pattern for multi-command fixtures. The fake binary's args-recording capability is needed for cache-hit verification — extend the fixture to write per-call argv to a tempfile that the test asserts against.
- **Subprocess-count budget.** Today: 4 subprocesses per command (help, happy, json, error). After U2 with no caching: up to 6 per id-shape get (adds companion --help + companion --json). With the U2 cache: O(unique-parent-paths × depth) total companion calls across the run, not O(get-commands × depth). For chained gets (multi-positional), each chain link adds one cached companion call. Worst-case wall-clock is bounded; expect 30-50% increase for full-mode runs on get-heavy CLIs, mostly amortized by the cache for sibling shares.
- **Unchanged invariants.** The verify pipeline (`internal/pipeline/runtime_commands.go:resolvePositionalValue`) is untouched. The `canonicalargs` registry is untouched. The mock-mode dispatch path is untouched. The MCP runtime walker is untouched. Only the live_dogfood subprocess path and the generator's example-line emitter change.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| U1's type-fence rule still matches pure-string positionals like `void`/`acid`/`arid` ending in `id` (false positive). | Such names as required URL path params are vanishingly rare in real API design. Cost of an FP is one Example line with a UUID — verify will produce a clear 404 signal at the wrong path, strictly better than today's literal `example-value`. Add a denylist entry only when a real-spec FP surfaces in a retro. |
| U2's companion-leaf allowlist misses a CLI's preferred list verb. | Fall-through goes to skip-with-reason, not back to placeholder-fail. Two named sets (`crossAPIListVerbs`, `cinemaListVerbs`) make extension targeted: future generic verbs go in the cross-API set, future cinema-class verbs stay isolated. Add new entries on retro evidence. |
| U2's ID extraction try-list misses an API shape (e.g., `.records[0].id`, deeply nested non-GraphQL shapes). | Falls through to skip-with-reason. Strictly better than today. The seven-path try-list (including two GraphQL shapes) covers TMDb, REST `data`/`items`/`list`, Shopify/Linear/Notion GraphQL, GitHub Relay-style. Add new paths on retro evidence. |
| U2's chained walk fails when a non-id-shape positional appears mid-chain (e.g., `<query>` between two ids). | Chain fails closed to skip-with-reason. Documented test scenario. Acceptable — the chain semantics are "thread ids forward"; a non-id mid-chain breaks that contract. |
| U3 misclassifies a non-search command that happens to ship `--query` (e.g., a list command with `--query` as a filter). | Such commands should also accept exit 0 — that's the correct behavior for a "no matches found" filter. The new strategy is strictly looser than the old one, so misclassification doesn't harm: search-strategy is a superset of acceptable outcomes. |
| Future API ships `--query` on a mutating verb (Elasticsearch `_delete_by_query`, MongoDB `deleteMany --query=...`). The heuristic flags it as search-shape, then probes against the live API with `__printing_press_invalid__`. | Today's risk is theoretical (no such command in current catalog). If tokenization broadens the match, `__printing_press_invalid__` could match real production data and trigger the mutation. Add a verb-name guard if/when such an API enters the catalog: only enter search-strategy when the leaf name itself contains a search-coloured verb (search, find, query, lookup, browse) AND `--query` is present — exclude leaves containing delete/update/create/remove/destroy. Not in scope for this PR. |
| Companion subprocess timing inflates wall-clock for full-mode runs on get-heavy CLIs. | Per-companion cache caps total companion calls at O(unique-parent-paths × depth). Empirical worst-case 30-50% wall-clock increase, mostly amortized for sibling shares. Tighten by lowering `--limit` if companion supports it. If a real run blows the per-process timeout, the existing process timeout fires per-call, not per-CLI; a single slow companion does not stall other commands. |
| Golden update in U1 noisy on git blame. | Two-line bytes update in two files. Clearly explain in PR body that the diff is the intended consequence of the camelCase ID fix. |
| Quick-level verdict regression risk (was: skip-flips-FAIL). | Resolved by U4 — the gate is restated as `Failed == 0 && (Passed + Skipped) >= 5 && MatrixSize >= 4`. Skip outcomes can no longer flip PASS to FAIL. |

---

## Documentation / Operational Notes

- Mention the U1 golden diff in the PR body.
- No SKILL change needed — the new behavior is mechanical and lives entirely in the scorer/generator. SKILL prose still says "live dogfood probes happy_path / json_fidelity / error_path"; the per-shape strategy is an internal scorer detail.
- No change to `docs/PIPELINE.md` (the pipeline phase contracts are unchanged — only one phase's verdict logic gets sharper).
- After landing, regenerate Movie Goat to confirm the 11 false-fail entries clear. That regen is operational follow-up, not part of this plan's scope.

---

## Sources & References

- **Origin issue:** [cli-printing-press#573](https://github.com/mvanhorn/cli-printing-press/issues/573)
- **Parent retro:** [cli-printing-press#571](https://github.com/mvanhorn/cli-printing-press/issues/571)
- **Sibling WU (already shipped):** [cli-printing-press#572](https://github.com/mvanhorn/cli-printing-press/issues/572) — generator template polish, PR #576
- Related code: `internal/generator/generator.go:exampleValue`, `internal/pipeline/live_dogfood.go`, `internal/pipeline/runtime_commands.go`
- Related prior retros: food52 retro #20260427-014521 (canonical mock values for verify positionals; complementary direction)
