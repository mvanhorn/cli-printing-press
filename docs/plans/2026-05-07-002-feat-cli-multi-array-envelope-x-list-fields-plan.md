---
title: "feat(cli): Multi-array envelope support via x-list-fields"
type: feat
status: proposed
date: 2026-05-07
related:
  - cli-printing-press#689
---

# feat(cli): Multi-array envelope support via x-list-fields

## Overview

Sync's array extractor in `internal/generator/templates/sync.go.tmpl` bails when a response envelope contains more than one top-level array field, leaving the resource un-syncable. Kalshi's `/portfolio/positions` returns `{event_positions: [...], market_positions: [...], cursor: "..."}`; the extractor sees `arrayCount == 2` and falls through. The `exposure` novel-feature command, which depends on positions, cannot be unblocked even after #689 Bugs 1 and 2 are fixed.

Add a path-level OpenAPI extension `x-list-fields` that explicitly declares the names of array fields to drain from a multi-array envelope. The Printing Press's pre-generation enrichment step auto-emits the extension when it detects multi-array shapes in the response schema or absorb-manifest example payloads — so spec authors never see the extension and operators never hand-write it. The generator threads the extension through the parser onto `Endpoint`, the sync template iterates all listed fields, and each row is tagged with a `kind` discriminator derived from the source array's name so downstream queries can distinguish event-level from market-level rows.

This plan covers the extension contract, parser surface, template change, auto-emission hook, docs, and tests.

---

## Problem Frame

The current sync extractor's logic (lines 605-620 of `sync.go.tmpl`):

```go
arrayCount := 0
for key, raw := range envelope {
    if json.Unmarshal(raw, &candidate); ... {
        arrayKey = key
        arrayItems = candidate
        arrayCount++
    }
}
if arrayCount == 1 {
    return arrayItems, ...
}
return nil, "", false
```

This is the right *conservative* default — picking one array out of two would silently drop data. But it leaves multi-array envelopes unreachable. The Kalshi run in #689 worked around this by hand-adding `portfolio-settlements` as a separate sync resource; that doesn't generalize and doesn't solve `positions` (which is one endpoint serving two related arrays the API author chose to bundle).

Three signals point at "explicit declaration" as the right contract, not "smarter heuristic":

1. **Two arrays sharing an envelope are usually deliberate.** When an API author bundles `event_positions` and `market_positions` together, they're saying these are facets of one logical resource (positions). A heuristic that picks the longer one or picks alphabetically would be wrong half the time.
2. **The `kind` discriminator must be deterministic and queryable.** Novel-feature SQL like `WHERE kind = 'market_position'` needs a stable name. The array key is the natural choice; that requires preserving it.
3. **Spec authors don't author extensions.** `x-resource-id` is mostly emitted by our absorb step or is in vendor specs we control. Same model applies here: the Printing Press sets `x-list-fields`, not humans.

The trio of changes — extension contract + parser surface + template emission — is small. The auto-emission step is the one with design weight, because it sits in the absorb pipeline where multiple agents author overlay deltas.

---

## Requirements Trace

- R1. The OpenAPI extension `x-list-fields` is recognized at path-item level, parallel to `x-resource-id` and `x-critical`.
- R2. The extension's value is an ordered list of strings, each naming a top-level property of the response schema's success envelope. Validation rejects non-array values, non-string elements, empty lists, and names that don't appear as object properties on the envelope schema.
- R3. The parser threads the list onto `spec.Endpoint.ListFields []string`. Empty list means "fall through to the existing single-array detection."
- R4. The sync template (`sync.go.tmpl`) drains each declared field's array, concatenates the rows, and tags each row with a synthetic `kind` field derived from the source field name.
- R5. The store schema includes `kind` as a column on the `resources` table (or as a JSON-extracted virtual column) so downstream SQL can `WHERE kind = '<field>'`.
- R6. The Printing Press's pre-generation enrichment step (the same hook that emits MCP enrichment) auto-detects multi-array envelopes and writes `x-list-fields` into the spec. Detection looks at the response schema and, when an absorb manifest with example payloads is available, uses those to confirm the arrays are populated in real responses.
- R7. `docs/SPEC-EXTENSIONS.md` documents the extension alongside `x-resource-id`, with an example.
- R8. Goldens cover one fixture spec with a multi-array envelope. The generator emits sync code that drains both arrays and tags rows; the parser's existing tests are extended with the new field.
- R9. The `IDField` resolution path (Bug 1's `unwrapItemSchema`) does not regress: a multi-array envelope with `x-list-fields` declared resolves IDField from the *first* listed field's item schema. Without the extension, the fallback path (single-array detection) still applies as before.

---

## Scope Boundaries

- One spec extension, one parser field, one template change, one auto-emission rule. No new core data model.
- Auto-emission is a heuristic over the response schema and (when available) the absorb manifest's example payloads. Hand-authored `x-list-fields` is supported and wins over auto-emission.
- The store schema gains one column (`kind`). Existing single-array resources leave it empty; multi-array resources populate it. No migration needed for first-time syncs.
- Novel-feature SQL is responsible for using the `kind` discriminator when it cares about array-of-origin. The validator from #689 Bug 4 will catch SQL that ignores `kind` when it should — out of scope here.
- No support for nested or N>2 multi-array shapes in v1 beyond what falls naturally out of the iterator. The two-array Kalshi case is the canonical fixture; a 3+ array envelope works the same way.

### Deferred to Follow-Up Work

- Per-array `kind` overrides (e.g. `x-list-fields: [{field: event_positions, kind: event}, ...]`). v1 derives `kind` from the field name directly; explicit overrides are a follow-up if API shapes prove awkward.
- Cross-array deduplication. If two arrays share IDs (rare; would indicate a mis-modeled API), v1 lands both rows. A retro can decide whether to add dedup later.
- Auto-emission for nested envelopes (`{data: {arrays: {...}}}`). Out of scope; the auto-emitter looks one level deep only.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/sync.go.tmpl::extractPageItems` (the array extractor that currently bails on `arrayCount != 1`). The Phase 3 template change replaces that block with a per-resource `listFields` lookup that, when populated, drains the named fields; otherwise falls through to today's single-array path.
- `internal/openapi/parser.go::readPathItemResourceID` — mirror pattern for `readPathItemListFields`: read `x-list-fields`, validate, return `[]string`.
- The `pathResourceIDOverride` read site in `internal/openapi/parser.go::mapResources` — add a parallel `pathListFields := readPathItemListFields(...)` and assign it to each endpoint built under that path.
- `internal/spec/spec.go::Endpoint` — add `ListFields []string` with YAML/JSON tags.
- `docs/SPEC-EXTENSIONS.md::x-resource-id` — insert `x-list-fields` immediately above or below it; same shape.
- `internal/generator/templates/store.go.tmpl` — owns the store schema. Add `kind TEXT` to the resources table DDL and to the upsert path.
- `internal/pipeline/` — owns the pre-generation enrichment hooks the auto-emitter sits in. The closest existing pattern is the auth-enrichment Phase 2 step referenced in `skills/printing-press/SKILL.md:1937` ("pre-generation auth enrichment ran correctly"). The list-fields auto-emitter lives in the same orchestration layer.

### How Auto-Emission Decides

Two-stage detection, run after the spec is loaded but before the parser builds endpoints:

1. **Response schema has 2+ top-level array properties on the success envelope.** This is the strict trigger. Single-array envelopes already work via the `data:` fast path and the new single-named-array path landed in Bug 1.
2. **Absorb manifest example payloads (when present) confirm both arrays are populated in at least one captured response.** This is the safety net. If the schema declares two arrays but real responses only ever populate one, prefer the single-array fallback rather than emitting an extension that splits the data unnecessarily.

When both conditions hold, write `x-list-fields: [<field1>, <field2>, ...]` to the path item. Order is the order of property declaration in the schema (kin-openapi preserves this through the `Required` slice when fields are listed there; otherwise alphabetical for determinism). Hand-authored `x-list-fields` short-circuits both detection stages.

### Why an Extension Beats a Heuristic

Two failure modes for a "drain-all-arrays" template heuristic:

1. **Metadata arrays.** `{users: [...], errors: [], warnings: []}` — `errors` and `warnings` are status, not data. A drain-all heuristic ships warning rows into the resources table.
2. **Mixed-shape arrays.** `{data: [...], links: [{rel, href}, ...]}` — `links` is HATEOAS metadata with a different schema. Concatenation would produce schema-violating rows.

An extension says "these specific fields are list payloads," which is the information the parser needs but cannot infer from shape alone. The auto-emitter applies a cautious heuristic *once* (at enrichment time, with the manifest in hand for confirmation); the runtime template has no heuristic at all.

### Where the `kind` Tag Comes From

Field name → snake_case → trim `_positions` / `_list` / `_items` / etc. suffix when present (heuristic; gives `event` from `event_positions`); fall back to the raw field name when the trim would empty the string. Alternative: skip the trim and use the field name as-is (`event_positions` literally) — simpler, less pretty. The trim heuristic is a v1 nicety that can be cut if it surfaces edge cases.

---

## Implementation Plan

### Phase 1: Extension contract and parser surface

- Add `readPathItemListFields(pathItem, path) []string` in `internal/openapi/parser.go`. Validate: must be a YAML sequence of non-empty strings; reject other shapes with `warnf` and return nil.
- Read the extension at the same place `readPathItemResourceID` is called inside `mapResources`. Pass through to each endpoint built under the path.
- Add `ListFields []string` to `internal/spec/spec.go::Endpoint` with `yaml:"list_fields,omitempty"` and `json:"list_fields,omitempty"`.
- Cross-validate against the response schema: every name in `x-list-fields` must appear as a property on the envelope schema. Names that don't appear emit a warning and are dropped from the resolved list.
- Unit tests in `internal/openapi/parser_test.go`: valid two-name list passes through; non-array value rejected; empty strings rejected; names absent from schema dropped with warning; missing extension leaves field empty.

### Phase 2: Sync template

- Replace the existing `arrayCount`-based fallback block in `sync.go.tmpl::extractPageItems` with: if `Endpoint.ListFields` is non-empty, iterate the named fields in order, accumulate rows, and tag each row with `kind` derived from the field name. Fall through to the existing single-array path when `ListFields` is empty.
- Update `extractID` and the row write path to include `kind` in the row metadata.
- Update `internal/generator/templates/store.go.tmpl` to declare `kind TEXT` in the resources table DDL and accept it in the upsert. Existing rows without `kind` carry an empty string — backwards-compatible since old queries don't filter on it.
- Goldens: add a fixture spec under `testdata/golden/fixtures/multi-array-envelope.yaml` whose `/positions` endpoint returns `{event_positions: [...], market_positions: [...], cursor}` with `x-list-fields: [event_positions, market_positions]`. Golden case under `testdata/golden/cases/generate-multi-array-envelope/`.

### Phase 3: Auto-emission hook

- Add `internal/pipeline/list_fields_enrichment.go`. Runs after spec load, before parser. Walks the OpenAPI doc; for each path with a multi-array envelope on its success response schema, emits `x-list-fields` if not already present.
- When an absorb manifest with example payloads is reachable, gate auto-emission on "at least one captured response populates each named array." When no manifest is available (test fixtures, lib-imported specs without research), apply schema-only emission with a warning logged.
- Unit tests in `internal/pipeline/list_fields_enrichment_test.go`: schema with 2+ arrays + populated examples → emits extension; schema with 2+ arrays + only one populated in examples → does not emit; schema with 1 array → no-op; hand-authored extension → respected without modification.

### Phase 4: Docs and skill

- Add `x-list-fields` section to `docs/SPEC-EXTENSIONS.md` immediately after `x-resource-id`. Include a Kalshi-shaped example.
- Update `skills/printing-press/SKILL.md` to mention the extension's existence and the auto-emission step in the same paragraph as auth/MCP enrichment. The skill doesn't need to teach the extension — the agent never writes it directly — but reviewers should know it exists when reading absorbed specs.

### Phase 5: Manual verification on Kalshi

- Re-absorb Kalshi (or apply the new auto-emitter to the existing absorbed spec); confirm `x-list-fields: [event_positions, market_positions]` is on `/portfolio/positions`.
- Regenerate `kalshi-pp-cli`; run sync against a real account; confirm both arrays land as rows in the `resources` table with distinct `kind` values.
- Run `portfolio exposure` against the synced data; confirm it returns non-empty rows. (The novel-feature SQL itself may also need to filter on `kind` — that's a printed-CLI concern, but documenting the manual repro is worth it for the retro.)

---

## Verification

- `go test ./...` — all unit tests pass.
- `scripts/golden.sh verify` — new multi-array fixture produces stable output; existing fixtures unchanged because `Endpoint.ListFields` is empty by default.
- `scripts/golden.sh verify` after adding the `kind` column — store DDL diff is the only generator-output change; existing store fixtures show an additional column with no behavioral effect on single-array sync paths.
- Manual re-sync of `kalshi-pp-cli` populates positions; `exposure` returns non-empty rows; the dogfood SQL-schema validator (#689 Bug 4 plan) accepts the new schema once present.

---

## Risks

- **Auto-emitter false positives.** A schema declares two arrays but the API rarely populates the second; the emitter still writes the extension. Outcome: a `kind` column with one rare value. Low harm. Mitigation: the manifest-gated detection path catches the common cases.
- **`kind` column on existing single-array CLIs.** Adding a column to the store DDL is technically a schema change for already-deployed CLIs. SQLite tolerates additive columns, and existing rows will have `kind = ''`. No migration script needed; existing user data survives.
- **Order sensitivity.** The order in `x-list-fields` determines which array's item schema feeds `IDField` resolution. If an author hand-writes the list with the "wrong" array first, the resolved IDField may differ. Acceptable: the auto-emitter is the dominant author and uses schema-property order; manual authors can be guided by docs.
- **Field-name collisions across arrays.** `event_positions[].id` and `market_positions[].id` both produce rows with `id` populated, but the values are distinct namespaces. Sync's existing PK uniqueness assumes one array per resource; the `kind` column is part of the deduplication key for multi-array rows. The store DDL change in Phase 2 must include `kind` in the unique constraint.
