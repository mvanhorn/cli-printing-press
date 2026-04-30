---
title: "feat: Three retro work units from postman-explore — POST-as-query detection, novel-command --select, scorecard YAML"
type: feat
status: active
date: 2026-04-30
origin: https://github.com/mvanhorn/cli-printing-press/issues/423
---

# feat: Three retro work units from postman-explore — POST-as-query detection, novel-command --select, scorecard YAML

## Summary

Implement three of the eight work units filed in retro issue #423 from the postman-explore generation session — the small/medium complexity wins with broad cross-CLI impact. Land them in dependency-light order (scorecard YAML → novel-command `--select` → POST-as-query detection) so each PR is reviewable on its own and the higher-blast-radius change comes last when the easier wins have already validated the testing posture.

---

## Problem Frame

Three independent Printing Press defects surfaced during the postman-explore v3 regeneration. Each one was confirmed via cross-API audit to affect more than just postman-explore:

- **POST-as-query is misclassified as a write across the generator.** Every API with a `POST /search`-style endpoint, every GraphQL CLI, and every proxy-envelope CLI gets the wrong README boilerplate, and any promoted POST endpoint additionally fails at runtime because the promoted-command template hardcodes `c.Get(...)`.
- **Hand-written novel commands silently drop `--select` and `--compact`.** Confirmed broken in recipe-goat (14 files), dub (16), espn (1), yahoo-finance (2), and postman-explore (was 8 before the session-time fix). Agents keep falling into this trap because the SKILL never tells them how to plumb the filters through.
- **`scorecard --spec foo.yaml` rejects OpenAPI YAML.** Forces every user with a YAML spec to convert to JSON before scoring. Most OpenAPI specs in the wild are YAML.

The retro classifies F1/F2 (POST-as-query) and F3 (novel-command `--select`) as P1 because the audit named three or more concrete APIs that would benefit. F4 (scorecard YAML) is P2.

---

## Requirements

- **R1.** `printing-press scorecard --spec foo.yaml` works for OpenAPI 3.x YAML files without conversion. (Origin: retro F4 / WU-3.)
- **R2.** Hand-written novel commands honor `--select` and `--compact` for JSON output via a generator-emitted helper. The Phase 3 SKILL build instructions require novel commands to use the helper. (Origin: retro F3 / WU-2.)
- **R3.** `methodIsWrite` (or its replacement) returns `false` for POST endpoints that are semantically queries — search, GraphQL, RPC-style — based on operation-id prefix, request-body shape, or annotations. (Origin: retro F1 / WU-1.)
- **R4.** Promoted endpoints emit the correct HTTP verb in the generated command. POST-only endpoints that get promoted run successfully against the live endpoint. (Origin: retro F2 / WU-1.)

**Origin actors:** the agent running `/printing-press` for any future API; the agent running `/printing-press-polish` against any existing CLI; users who pass an OpenAPI YAML spec to `scorecard` directly.

**Origin acceptance examples** (carried verbatim from retro):
- AE1 (covers R1): `scorecard --dir <cli> --spec foo.yaml` against an OpenAPI 3.x YAML spec returns scores byte-equivalent to running it against the same spec converted to JSON. Existing JSON specs and internal-YAML specs continue to work byte-identically.
- AE2 (covers R2): a fresh CLI with a hand-written novel command running `<cli> <novel> --json --select foo,bar` returns only `foo` and `bar` fields. Endpoint-mirror commands continue to apply filters byte-identically. The same call via MCP `tools/call` honors the `select` argument.
- AE3 (covers R3): a spec with `POST /search-all` (operation id `searchAll`) flips `HasWriteCommands` to false; the generated README emits "Read-only by default". A spec with `POST /graphql` (operation id `query*`) similarly classifies as read. A spec with `POST /users` (operation id `createUser`) keeps `HasWriteCommands` true.
- AE4 (covers R4): a spec with `POST /search-all` promoted emits `c.Post(path, body)` and runs successfully against the live endpoint. A spec with `GET /resources` promoted continues emitting `c.Get(path, params)` byte-identically.

---

## Scope Boundaries

- Does not refactor the existing 33+ library CLIs to use the new `printJSONFiltered` helper. The SKILL update + generator emit fix the next regeneration; existing CLIs catch up via `/printing-press-polish` and individual runs (out of scope here).
- Does not change MCP `destructiveHint` annotations on POST endpoints. The retro flags this as a separate concern; revisit if the README/HasWriteCommands fix surfaces cross-impact.
- Does not address the four other retro work units (WU-4 narrative-command verification, WU-5 browser-sniff direct-probe fallback, WU-6 proxy-envelope subclass fixes, WU-7 store-migration concurrency, WU-8 traffic-analysis version doc). Those land separately.
- Does not change verify or dogfood spec loading. WU-3 fixes only `loadOpenAPISpec` in `internal/pipeline/scorecard.go`.

### Deferred to Follow-Up Work

- Backfilling existing library CLIs with `printJSONFiltered` calls in their hand-written novel commands: tracked by the polish skill on next invocation per CLI.
- A separate sweep that flips `MCP destructiveHint` for POST-as-query endpoints once the new write-detection signals are in place.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/generator.go` — current `methodIsWrite` (line search: `func methodIsWrite(method string) bool`) and the two callers `hasWriteCommands` / `resourceHasWriteCommand` that propagate it. Endpoint struct in `internal/spec/spec.go` carries `Method`, `Path`, `Description`, `Body []Param`, `Meta map[string]string`. The map key in `Resource.Endpoints` is the operation id (used elsewhere as the canonical name).
- `internal/generator/templates/command_promoted.go.tmpl` — currently emits `data, err := c.Get(path, params)` unconditionally for the no-store branch. The `HasStore` branch already calls `resolveRead`/`resolvePaginatedRead`. Verb branching needs to slot in alongside both.
- `internal/generator/templates/helpers.go.tmpl` — the existing emission point for `filterFields` (line 451), `compactFields` (line 601), `wrapWithProvenance` (line 1242), and `printOutputWithFlags`. Add `printJSONFiltered` here so it's emitted into every CLI's `internal/cli/helpers.go` alongside the helpers it composes.
- `internal/pipeline/scorecard.go:loadOpenAPISpec` — current shape: read file → `isInternalYAMLSpec` check → fall through to `json.Unmarshal`. The pipeline package already imports YAML elsewhere (`fullrun.go`, `dogfood.go`, `workflow_manifest.go`).
- `skills/printing-press/SKILL.md` — Phase 3 "Agent Build Checklist" section is where the new principle for `--select` plumbing belongs. Existing principles 1-10 cover non-interactive, structured output, progressive help, etc. Add a new principle (or extend principle 2 "Structured output") that requires `printJSONFiltered`.

### Institutional Learnings

- AGENTS.md "Code & Comment Hygiene" guidance: prefer mechanical fixes over instructional fixes. Both WU-1 (generator) and WU-2 (generator + skill) fit this — the skill instruction alone wouldn't reach existing CLIs, but the generator emit + skill instruction together do for regeneration.
- AGENTS.md "Updating dependent verifiers in the same change": F1's fix touches `HasWriteCommands` which influences README emission. The README template branching (`{{- if .HasWriteCommands}}{{else}}Read-only by default{{end}}`) is already in place, so no template change required — only the detector logic. Confirmed during retro audit.

### External References

None needed — all three changes are internal to the printing-press repo with no external API contracts to preserve.

---

## Key Technical Decisions

- **U1 (scorecard YAML) lands first.** Smallest surface, isolated to one function in the pipeline package, no cross-template implications. Establishes the test pattern for the next two units. Lowest risk for the build/test infrastructure.
- **U2 (novel-command `--select`) lands second.** Self-contained: one new helper in `helpers.go.tmpl` plus one new principle in SKILL.md. No dependency on U1, but lands second because U3 may want to reference the build-checklist style established by U2's SKILL update.
- **U3 (POST-as-query detection) lands last.** Highest blast radius — touches `methodIsWrite` (called from `hasWriteCommands`), the `command_promoted.go.tmpl` template (used by every CLI with a promoted endpoint), and indirectly the README/SKILL emission. Lands last so the simpler fixes have validated the test approach and any cross-template regression caught by golden tests is investigated in isolation.
- **Don't change `methodIsWrite`'s signature.** Replace `methodIsWrite(method string) bool` with a new `endpointIsWriteCommand(endpoint spec.Endpoint, name string) bool` that takes the operation id (the map key). Keep `methodIsWrite` around as a thin wrapper for any non-endpoint callers (e.g., raw method-string contexts) so we don't ripple through unrelated code.
- **Use the operation id as the primary semantic signal.** It's the most reliable cross-API signal — every spec parser and every endpoint mirror uses it. Body shape (request body present but only filter-shaped) is a weaker secondary signal. Annotations (`mcp:read-only` is the existing flag; check whether endpoints can carry it) are a tertiary signal.
- **YAML loader: use `yaml.Unmarshal` from the same package the rest of the pipeline uses.** Don't add a new dependency. The `gopkg.in/yaml.v3` (or equivalent) module is already in `go.mod` per `fullrun.go`/`dogfood.go`.

---

## Open Questions

### Resolved During Planning

- **Should `printJSONFiltered` go in `helpers.go.tmpl` or a new `cliutil_*.go.tmpl`?** Resolved: `helpers.go.tmpl`. Same package as the helpers it composes (`filterFields`, `compactFields`); no need for a new emitted file when an existing one already owns the JSON-output helper surface. Existing `cliutil/` templates (`cliutil_fanout.go.tmpl`, `cliutil_text.go.tmpl`, etc.) hold cross-cutting utilities for novel-feature client code; the JSON output helpers are CLI-formatting concerns and belong with the rest of `helpers.go`.
- **Should `methodIsWrite` change its signature or be replaced?** Resolved: introduce `endpointIsWriteCommand(endpoint, name)` as a new function; have `resourceHasWriteCommand` call the new function. Keep `methodIsWrite` exported as a thin shim so any cross-package callers continue to work. The semantic upgrade lives in the new function; the old one becomes a fallback for callers that only have a verb string.
- **Should the YAML loader consolidate JSON parsing through `yaml.Unmarshal`?** Resolved: keep the YAML and JSON branches separate. YAML is a JSON superset and `yaml.Unmarshal` would accept JSON input, but separate branches preserve clear, format-specific error messages (a JSON syntax error reports as "parsing spec JSON" with the JSON parser's error; a YAML syntax error reports as "parsing OpenAPI YAML spec" with the YAML parser's error). The shrinkage from consolidation isn't worth the diagnostic ambiguity.

### Deferred to Implementation

- **Exact list of operation-id prefixes that signal "read".** Plan baseline: `get*, list*, search*, find*, query*, count*, describe*, fetch*`. The implementer should verify this against the catalog's existing specs (grep `operationId` patterns) and add anything that's clearly read-shaped before locking the list. The list is encoded as a slice of prefixes for testability.
- **Whether to short-circuit `endpointIsWriteCommand` on `mcp:read-only` annotation.** Plan baseline: yes, check `endpoint.Meta["mcp:read-only"] == "true"` first. Confirm during implementation whether `Endpoint.Meta` is the right map (vs. a sibling annotation field) by reading current usages.
- **Body-shape signal for "POST-as-query".** Plan baseline: when `endpoint.Body` contains only filter-style params (e.g., `query`, `filter`, `limit`, `offset`, `from`, `size`, `cursor`, `page`), classify as read. Implementer can refine the keyword list once they see how `Body []Param` is populated for the postman-explore search-all and any GraphQL CLI in the catalog.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

```
                  Spec parser
                       │
                       ▼
              Endpoint{ Method, Body, Meta, ... }
                       │
                       │  (called per endpoint, with operation id)
                       ▼
   ┌────────────────────────────────────────────────────────────┐
   │  endpointIsWriteCommand(endpoint, name)                    │
   │  ────────────────────────────────────────────────────────  │
   │  1. mcp:read-only annotation set? → read (false)           │
   │  2. Method ∈ {GET, HEAD, OPTIONS}? → read (false)          │
   │  3. operationId prefix in {get,list,search,find,query,...} │
   │     → read (false)                                         │
   │  4. Body shape: only filter-style params? → read (false)   │
   │  5. otherwise → write (true)                               │
   └────────────────────────────────────────────────────────────┘
                       │
                       ▼
              hasWriteCommands → README template
                       │
                       ▼
        command_promoted.go.tmpl: branch on .Endpoint.Method
        ┌─────────────────────────┬────────────────────────────┐
        │ Method == "GET"         │ Method != "GET"            │
        │  data, err := c.Get(    │  data, _, err := c.Post(   │
        │    path, params)        │    path, body)             │
        └─────────────────────────┴────────────────────────────┘
```

---

## Implementation Units

- U1. **Scorecard accepts OpenAPI YAML specs**

**Goal:** `printing-press scorecard --spec foo.yaml` succeeds for OpenAPI 3.x YAML files without manual conversion. Existing JSON specs and internal-YAML specs continue to work byte-identically.

**Requirements:** R1 (AE1).

**Dependencies:** None.

**Files:**
- Modify: `internal/pipeline/scorecard.go` (the `loadOpenAPISpec` function only)
- Test: `internal/pipeline/scorecard_tier2_test.go` (extend the existing test surface)

**Approach:**
- Add a `yaml.Unmarshal` fallback to `loadOpenAPISpec`: after the internal-YAML check fails and before the JSON fallback, attempt YAML decoding. The YAML branch produces the same `map[string]any` shape that the JSON branch consumes, so downstream code (paths/security extraction) doesn't change.
- Match the existing import style for YAML in `internal/pipeline/` (whichever yaml package is already used in `fullrun.go`/`dogfood.go`).
- Preserve the existing internal-YAML detection (`isInternalYAMLSpec`) — internal YAML and OpenAPI YAML are distinguishable by top-level keys. Internal YAML has `kind:` / `resources:` / `auth:` top-levels; OpenAPI has `openapi:` / `paths:` / `components:`.
- Decision: keep YAML and JSON branches separate (don't consolidate via `yaml.Unmarshal` for both) to preserve clear error messages when a JSON spec has a syntax error vs. a YAML spec has one.

**Patterns to follow:**
- `internal/pipeline/fullrun.go` and `internal/pipeline/dogfood.go` for the existing YAML import in the package.
- The `isInternalYAMLSpec` detection pattern already in `loadOpenAPISpec` for short-circuiting on top-level keys.

**Test scenarios:**
- Covers AE1. Happy path: feeding an OpenAPI 3.x YAML spec to `loadOpenAPISpec` returns a populated `*openAPISpecInfo` with the same `Paths` and `SecuritySchemes` as feeding the same spec converted to JSON.
- Happy path: an internal-YAML spec (with `kind: yaml-internal` or whatever the current sentinel is) continues to short-circuit through the internal branch — verified by reading the round-tripped APISpec.
- Happy path: an OpenAPI 3.x JSON spec continues to load via the existing JSON branch — verified by an unchanged paths/security output.
- Error path: a YAML file with malformed top-level (e.g., a tab-indented `paths:`) returns a clear "parsing OpenAPI YAML spec" error message that includes the file path.
- Error path: a JSON file with a syntax error returns the existing "parsing spec JSON" message — no regression in diagnostic clarity.
- Edge case: an empty file (zero bytes) returns a non-nil error referencing the empty input. Confirm behavior matches whatever the existing JSON-fallback does with empty input.

**Verification:**
- `printing-press scorecard --dir <fixture-cli> --spec testdata/openapi-yaml-spec.yaml` runs to completion and produces the same total score as running it with the JSON form of the same spec.
- All existing scorecard tests pass without changes.

---

- U2. **Hand-written novel commands honor `--select` and `--compact`**

**Goal:** Generator emits a `printJSONFiltered` helper into every CLI's `internal/cli/helpers.go`, and the SKILL Phase 3 build checklist requires novel commands to use it. After this lands, the next regeneration of any CLI with novel features produces commands that respect `--select` and `--compact` on JSON output.

**Requirements:** R2 (AE2).

**Dependencies:** U1 (none functionally — but landing U1 first establishes the test pattern).

**Files:**
- Modify: `internal/generator/templates/helpers.go.tmpl` (add `printJSONFiltered` near the existing `filterFields`/`compactFields` functions — line vicinity 451-700)
- Modify: `skills/printing-press/SKILL.md` (Phase 3 "Agent Build Checklist" section — add a new principle or extend principle 2 "Structured output")
- Test: `internal/generator/helpers_template_test.go` (add or extend a golden-style test that builds a CLI from a fixture spec and asserts the emitted helpers.go contains `printJSONFiltered` with the right signature)

**Approach:**
- Add `printJSONFiltered(cmd interface{ OutOrStdout() io.Writer }, v any, flags *rootFlags) error` to `helpers.go.tmpl`. Marshal `v`, apply `filterFields(raw, flags.selectFields)` if `flags.selectFields != ""`, else apply `compactFields(raw)` if `flags.compact`, then encode with two-space indent.
- The `cmd` parameter takes the minimal interface required (just `OutOrStdout()`) so the helper stays testable without importing cobra in test files.
- Update `SKILL.md` Phase 3 "Agent Build Checklist" with a new principle (call it principle 11 or a sub-bullet under principle 2): "Novel commands MUST use `printJSONFiltered(cmd, v, flags)` for JSON output. Direct `flags.printJSON(cmd, v)` calls drop `--select` and `--compact`. Verify with `<cli> <novel-cmd> --json --select <field> | jq 'keys'` returning only the requested fields."
- Don't refactor the existing 33+ library CLIs in this PR. Their next regeneration picks up the helper; their next polish run picks up the SKILL guidance for novel-feature additions.

**Patterns to follow:**
- The existing `filterFields` / `compactFields` / `wrapWithProvenance` style in `helpers.go.tmpl` — same indentation, same comment density, same parameter ordering convention.
- Existing build-checklist principles 1-10 in `SKILL.md` for the SKILL.md addition's prose style.

**Test scenarios:**
- Covers AE2. Happy path: build a CLI from a small fixture spec that has a novel-feature command emitted via the new helper. Run `<cli> <novel> --json --select foo,bar` against the binary; assert the output JSON has exactly the keys `foo` and `bar` and nothing else.
- Happy path: same fixture with `--json --compact` returns the trimmed field set per `compactFields`'s known blocklist.
- Happy path: same fixture with `--json` (no select, no compact) returns the full field set.
- Edge case: `--json --select foo,nonexistent_field` returns a JSON object containing `foo` and omits `nonexistent_field` (matches existing `filterFields` semantics).
- Edge case: empty result set with `--json --select foo` returns `[]` (matches the empty-array path the helper takes).
- Integration: emit the helper into a CLI fixture and grep the generated `internal/cli/helpers.go` for the function signature to assert the template produced it. This proves the generator emit, not just the SKILL doc.
- Negative: existing endpoint-mirror commands' JSON output is unchanged (they use `printOutputWithFlags`, not `printJSONFiltered`). Confirmed by running an existing fixture's endpoint mirror with `--json --select` before and after the change and asserting byte-equivalence.

**Verification:**
- `go test ./internal/generator/...` passes including any new test that builds a fixture CLI and asserts `printJSONFiltered` is emitted.
- A regenerated postman-explore CLI (run as a manual smoke test, not committed) shows `canonical stripe --json --select name,publisherHandle,forkCount` returning only those three fields.
- `skills/printing-press/SKILL.md` diff renders cleanly and the new principle is in the Agent Build Checklist with the same numbering style as 1-10.

---

- U3. **Generator-aware POST-as-query detection**

**Goal:** Stop the generator from treating POST endpoints as writes when they're queries (search, GraphQL, RPC). Fix the promoted-command template to emit the correct HTTP verb. Both changes are needed together to fully resolve postman-explore-style breakage; landing only one is a half-fix.

**Requirements:** R3 (AE3), R4 (AE4).

**Dependencies:** U2 (no functional dep; ordering is for review/test posture only).

**Files:**
- Modify: `internal/generator/generator.go` (introduce `endpointIsWriteCommand(endpoint spec.Endpoint, name string) bool` and route `resourceHasWriteCommand` through it; keep `methodIsWrite(method string) bool` as a thin shim for verb-only callers)
- Modify: `internal/generator/templates/command_promoted.go.tmpl` (add a verb branch: `{{- if eq .Endpoint.Method "GET" }}c.Get(...){{else}}c.Post(...){{end}}`; gracefully handle the `Pagination` / `HasStore` matrix that already exists)
- Test: `internal/generator/generator_test.go` (existing — add cases for `endpointIsWriteCommand` against contrived Endpoint values)
- Test: `internal/generator/generator_promoted_test.go` (or wherever promoted-command emission is tested — add a fixture with a POST-promoted endpoint and assert the emitted code calls `c.Post`)

**Approach:**
- New function `endpointIsWriteCommand(endpoint spec.Endpoint, name string) bool` checks signals in this order:
  1. If `endpoint.Meta["mcp:read-only"] == "true"`, return false. (Verify `Meta` is the right field by reading current usages.)
  2. If `methodIsWrite(endpoint.Method)` is false (i.e., GET/HEAD/OPTIONS), return false.
  3. If `name` (the operation id, which is the map key) starts with any of `get`, `list`, `search`, `find`, `query`, `count`, `describe`, `fetch`, return false. (Match case-insensitively to match Postman's `searchAll` and any `Search*` etc.)
  4. If `endpoint.Body` is non-empty but every param has a name in `{query, queryText, filter, limit, offset, from, size, cursor, page, pageSize, sort, sortBy}`, return false. (Filter-shaped body = query semantics.)
  5. Otherwise, return true (genuine write).
- Refactor `resourceHasWriteCommand` to iterate `for name, endpoint := range resource.Endpoints` and call `endpointIsWriteCommand(endpoint, name)`. The function in `generator.go` is the only caller of the existing `methodIsWrite` for write detection; keep `methodIsWrite` exported for any out-of-package callers.
- Update `command_promoted.go.tmpl` to add a verb branch in the no-store fallback (line ~bottom of the no-pagination, no-store path):
  ```
  {{- if eq .Endpoint.Method "GET" }}
              data, err := c.Get(path, params)
  {{- else }}
              data, _, err := c.{{pascal (lower .Endpoint.Method)}}(path, body)
  {{- end }}
  ```
  The `body` value comes from the same Body marshalling that the underlying typed POST command uses (read the equivalent typed-command template for the pattern).
- The `HasStore` and `Pagination` branches in the same template should also gain verb awareness if they hit POST endpoints. Audit during implementation whether `resolveRead` / `resolvePaginatedRead` already handle non-GET, or if they need a sibling. This is the most uncertain part of the implementation; budget time for it.

**Execution note:** Add the test cases first. The semantic-signal logic in `endpointIsWriteCommand` is the most novel part; building tests against `Endpoint` fixtures up front locks the intended behavior before refactoring the dispatcher.

**Patterns to follow:**
- `internal/generator/generator.go` existing `hasWriteCommands` / `resourceHasWriteCommand` for the iteration shape.
- The existing `{{- if .HasStore}}{{else}}{{end}}` branching style in `command_promoted.go.tmpl` for the verb branch.
- `internal/generator/generator_test.go` for the test fixture setup pattern (build a `spec.Endpoint` literal, assert on classification).
- The typed POST endpoint template (likely `command.go.tmpl` or similar) for the `c.Post(path, body)` call shape and how `body` is constructed from `endpoint.Body` params.

**Test scenarios:**
- Covers AE3. Happy path: `POST /search-all` with operation id `searchAll`, body `[{queryText, size, from}]` classifies as read. `HasWriteCommands` for a resource containing only this endpoint returns false. README emits "Read-only by default" for a fixture spec with only this endpoint.
- Covers AE3. Happy path: `POST /graphql` with operation id `query` or `queryRoot` and body `[{query, variables}]` classifies as read.
- Covers AE3. Happy path: `POST /users` with operation id `createUser` and body `[{name, email, role}]` (no filter-shaped names) classifies as write.
- Edge case: GET endpoint with operation id `deleteUser` (nonsensical but possible from poor specs) classifies as read because verb is GET. Document the behavior — verb wins over name when verb is read-shaped.
- Edge case: POST endpoint with no body and no operation id signal classifies as write (fail-closed).
- Edge case: endpoint with `Meta["mcp:read-only"] = "true"` classifies as read regardless of verb or name.
- Covers AE4. Integration: a fixture spec with `POST /search-all` promoted and emitted by the generator produces a Cobra command whose RunE calls `c.Post("/search-all", body)` — verified by greping the generated command file. The emitted code compiles (`go build`).
- Covers AE4. Integration: a fixture spec with `GET /resources` promoted continues to emit `c.Get("/resources", params)` byte-identically. Confirms no regression in the GET path.
- Negative: existing fixtures used by golden-style tests in `internal/generator/` that include POST endpoints (CRUD APIs) continue to classify them as writes — confirmed by re-running the existing test suite without changes to its expectations.

**Verification:**
- `go test ./internal/generator/...` passes including new endpoint-classification tests.
- Manually regenerate postman-explore (in a scratch dir) and confirm: README shows "Read-only by default", and the promoted `search-all` command (now unhidden, since it works) calls `c.Post` and returns real data against the live endpoint.
- Existing CRUD-shape fixtures (the golden-test specs) still classify their POST/PUT/DELETE endpoints as writes; their generated READMEs still emit Retryable bullets.

---

## System-Wide Impact

- **Interaction graph (U3 only):** `endpointIsWriteCommand` feeds `hasWriteCommands` which feeds `HasWriteCommands` in template data which gates the README's Retryable section AND drives any other consumer of `HasWriteCommands` (audit during implementation — at minimum the README template, possibly SKILL prose). The promoted-command template change has no upstream effects but is gated on `.Endpoint.Method` which is already part of the template data.
- **Error propagation:** No changes to error handling. `loadOpenAPISpec` (U1) returns the same error type from the YAML branch as the JSON branch.
- **State lifecycle risks:** None — all three changes are pure code generation / classification. No runtime state involved.
- **API surface parity:** U2's `printJSONFiltered` is a new exported helper in every regenerated CLI's `internal/cli/helpers.go`. It composes existing exports; no API surface is removed.
- **Integration coverage:** U3's verb-branch fix needs an end-to-end smoke against a real POST-promoted endpoint (postman-explore is the natural fixture; postman is no-auth so the smoke is cheap). U2's helper needs a regenerate-and-run smoke against a fixture novel command. U1's YAML support is unit-testable in isolation.
- **Unchanged invariants:** Existing internal-YAML specs continue to load via the same code path. Existing OpenAPI JSON specs continue to load via the same code path. The `methodIsWrite` function continues to exist for any cross-package callers. The `flags.printJSON` helper is not removed; existing endpoint-mirror commands continue to use `printOutputWithFlags`.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| U3's verb-branch fix in `command_promoted.go.tmpl` interacts with the existing `HasStore`/`Pagination` matrix in non-obvious ways. The store branches use `resolveRead` / `resolvePaginatedRead` which may not have POST equivalents. | Audit the template and the resolver helpers during U3 implementation. Budget time to either add POST equivalents or scope the fix to the no-store branch and explicitly defer the HasStore+POST combination as a follow-up. |
| The operation-id prefix list in U3 misses prefixes used by some specs in the catalog, causing legitimate write commands to be misclassified as reads. | Test against the catalog's existing specs (grep for `operationId:` in `catalog/specs/*.yaml`) before locking the prefix list. Lean toward fail-closed: when in doubt, classify as write. |
| Golden tests in `internal/generator/` lock in specific generated output for fixture specs. U3's emit changes (verb branch in promoted template) may break golden tests that include promoted POST endpoints. | Run `scripts/golden.sh verify` before and after each unit. If a fixture's emitted code legitimately changes for the better, run `scripts/golden.sh update` and review the diff per AGENTS.md guidance. If a fixture breaks because of an unintended side effect, fix the code. |
| YAML import collision: pipeline package may import a different YAML package than the spec parser. | Check `go.mod` and `go list -m all` before the U1 implementation; use whichever YAML package the pipeline already imports rather than introducing a new dependency. |

---

## Documentation / Operational Notes

- **CHANGELOG / commit messages:** Each unit lands as a separate commit (or PR) with the conventional-commit prefix `fix(cli):` for U1 and U3, `feat(cli,skills):` for U2. The retro issue #423 is referenced in the body. Per `AGENTS.md`, a `fix:` triggers a patch bump and a `feat:` triggers a minor bump in the next release.
- **Polish skill follow-up:** After U2 lands, the next `/printing-press-polish` invocation per CLI in the library should detect existing `flags.printJSON` calls in hand-written novel commands and offer to swap them. That's tracked separately and not part of this plan; the deferred-work section already names it.
- **Docs/PIPELINE.md and docs/SKILLS.md:** Neither needs edits for this plan. The phase contract isn't changing; the SKILL update is a single principle addition.

---

## Sources & References

- **Origin retro issue:** https://github.com/mvanhorn/cli-printing-press/issues/423
- **Origin retro doc (full findings):** https://files.catbox.moe/84v2w8.md (also at `manuscripts/postman-explore/20260429-230407/proofs/20260430-004209-retro-postman-explore-pp-cli.md`)
- **Postman-explore PR:** https://github.com/mvanhorn/printing-press-library/pull/159 (where the `--select` fix shipped as a printed-CLI workaround; this plan generalizes the fix to the generator)
- **Related code:** `internal/generator/generator.go:methodIsWrite`, `internal/generator/templates/command_promoted.go.tmpl`, `internal/generator/templates/helpers.go.tmpl`, `internal/pipeline/scorecard.go:loadOpenAPISpec`, `skills/printing-press/SKILL.md` Phase 3 Agent Build Checklist.
