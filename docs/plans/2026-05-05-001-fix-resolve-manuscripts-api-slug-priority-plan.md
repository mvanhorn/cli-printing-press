---
title: "fix(cli): resolveManuscripts prefers API-slug keying over CLI-name"
type: fix
status: active
date: 2026-05-05
origin: https://github.com/mvanhorn/cli-printing-press/issues/598
---

# fix(cli): resolveManuscripts prefers API-slug keying over CLI-name

## Summary

Swap the lookup order in `resolveManuscripts` so the API-slug key (the SKILL's archive convention) is consulted before the CLI-name key (legacy binary convention), and refresh the stale "new convention" / "old convention" comments to match. Update the existing `TestManuscriptLookupPriority` fixture to assert the new priority and add the cal-com regression scenario. Reprints will now publish manuscripts from their actual run, not stale ones keyed under the binary name.

---

## Problem Frame

`resolveManuscripts` in `internal/cli/publish.go` was authored when the printing-press binary archived manuscripts under `manuscripts/<cli-name>/`. The SKILL has since standardized on `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/` as the durable archive convention (SKILL.md lines 440, 489, 2607–2609). The function still tries CLI-name first; when both keys exist, it returns whichever stale run happens to live under the CLI-name directory.

This surfaced during the cal-com reprint retro (#597, F2): `printing-press publish package --json` returned `run_id: 20260405-183800` (a stale April 5 run keyed under `cal-com-pp-cli`) while the actual run that just promoted was `20260504-205634` (keyed under `cal-com`). Every reprint of a CLI that has both keys hits this.

---

## Requirements

- R1. When both `manuscripts/<api-slug>/<run>/` and `manuscripts/<cli-name>/<run>/` exist, `resolveManuscripts(cliName, apiName)` returns the API-slug path and its most-recent run ID. *(Issue F2 positive)*
- R2. When only `manuscripts/<cli-name>/<run>/` exists, `resolveManuscripts` falls back to the CLI-name path. *(Issue F2 legacy fallback)*
- R3. When only `manuscripts/<api-slug>/<run>/` exists, `resolveManuscripts` returns the API-slug path. *(Issue F2 negative — API-slug only)*
- R4. The fuzzy resolve fallback (`resolveManuscriptDir`) still fires when neither keyed directory has runs. *(Preserves existing behavior)*
- R5. `printing-press publish package --dir <library>/<api-slug> --json` returns a `run_id` that matches the staged CLI's `.printing-press.json` `run_id` for the cal-com scenario (and any reprint where both keys exist). *(Issue F2 regression)*
- R6. The comments on the lookup branches (`internal/cli/publish.go:473–476` and `478–481`) accurately describe which key is the SKILL convention and which is the legacy binary convention.

---

## Scope Boundaries

- The inline manuscripts-warn lookup inside `runValidation` (`internal/cli/publish.go:621–628`) is **not** changed in this plan. The issue's scope boundary states "Does NOT touch other manuscripts consumers. Publish-package read path only." That block is a warn-only check that mirrors the same priority order — it stays as-is and may be revisited in a follow-up if the inconsistency causes confusion.
- No changes to the SKILL's archive convention or to `pipeline.PublishedManuscriptsRoot()`.
- No changes to `findMostRecentRun` or `resolveManuscriptDir` themselves.
- No changes to `phase5ProofsDir` (separate consumer of CLI-name and API-name keys for proof location; unaffected by lookup priority for runs).

### Deferred to Follow-Up Work

- Aligning the inline warn-only lookup at `internal/cli/publish.go:621–628` with the same API-slug-first priority. Currently out of scope per issue boundary.

---

## Context & Research

### Relevant Code and Patterns

- `internal/cli/publish.go:464` — `resolveManuscripts(cliName, apiName)`. Single call site at `internal/cli/publish.go:368` inside `newPublishPackageCmd`.
- `internal/cli/publish.go:838` — `findMostRecentRun(msAPIDir)`: returns the lexicographically-greatest entry (timestamp-named runs sort correctly).
- `internal/cli/publish.go` (resolveManuscriptDir, separate file) — fuzzy fallback used when neither keyed directory yields a run.
- `internal/cli/publish_resolve_test.go:129` — `TestManuscriptLookupPriority` simulates the chain step-by-step. The `prefers CLI name over API name` subtest currently encodes the wrong-priority behavior and must be flipped.
- `skills/printing-press/SKILL.md:440, 489, 2607–2609` — canonical statement that archives go to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/`.

### Institutional Learnings

- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md` documents the runstate / publish / archive layout contract that established API-slug as the canonical key. The binary's lookup order has lagged that contract; this plan brings it into compliance.

### External References

- None required. Internal contract change.

---

## Key Technical Decisions

- **Swap order, not "most-recent-mtime across both"**: The issue notes "Optionally pick the most-recent-modified across both when both exist." The simpler swap (API-slug first, CLI-name second) is sufficient — the SKILL writes API-slug exclusively now, so the only way the CLI-name key has a fresher run is if the user is mid-migration on a single repo, which is not a supported scenario. mtime-tiebreaking adds branch complexity for no observed benefit and would obscure the convention shift.
- **Comment refresh**: relabel the API-slug branch as "SKILL convention" and the CLI-name branch as "legacy binary convention" rather than "new" / "old", since "new/old" was the inversion bug's own framing.
- **Test update vs. new test**: flip the existing `prefers CLI name over API name` subtest to `prefers API name over CLI name` and call `resolveManuscripts` directly (instead of stepping through `findMostRecentRun` manually). Calling the function directly makes the priority assertion authoritative rather than indirectly inferred from chain steps.

---

## Open Questions

### Resolved During Planning

- **Should the warn-only lookup at `:621–628` be updated in the same change?** No. Issue scope boundary excludes it; tracked as Deferred to Follow-Up Work.
- **Does the current test suite need a new file?** No. `internal/cli/publish_resolve_test.go` already exists for these helpers; extend it.

### Deferred to Implementation

- Whether the existing `TestManuscriptLookupPriority/prefers_CLI_name_over_API_name` subtest should be deleted-and-replaced or renamed-and-rewritten. Either is fine; pick whichever produces the cleanest diff.

---

## Implementation Units

- U1. **Swap lookup order in `resolveManuscripts` and update tests**

**Goal:** Make `resolveManuscripts` consult the API-slug directory before the CLI-name directory and refresh the lookup-branch comments. Update the priority test to assert the new order and cover the cal-com regression scenario.

**Requirements:** R1, R2, R3, R4, R5, R6

**Dependencies:** None.

**Files:**
- Modify: `internal/cli/publish.go` (`resolveManuscripts` body and surrounding comments at `:464–485`)
- Modify: `internal/cli/publish_resolve_test.go` (`TestManuscriptLookupPriority` subtests; add API-slug-priority assertion using `resolveManuscripts` directly)

**Approach:**
- In `resolveManuscripts`, attempt the API-slug key first (`filepath.Join(msRoot, apiName)`), then the CLI-name key (`filepath.Join(msRoot, cliName)`), then the fuzzy fallback. The fall-through pattern (`if rid, err := findMostRecentRun(...); err == nil && rid != ""` returning early) stays the same — only the order of the two named-key blocks changes.
- Refresh the comments to label branch 1 as the SKILL archive convention (API-slug) and branch 2 as the legacy binary convention (CLI-name). Drop the `new convention` / `old convention` framing.
- In `TestManuscriptLookupPriority`, replace the existing `prefers CLI name over API name` subtest with one that calls `resolveManuscripts("steam-web-pp-cli", "steam-web")` directly against a fixture containing both keys and asserts the API-slug path and run win. Update the fallback subtests' fixture descriptions to match the new priority.
- Add a regression subtest mirroring the cal-com case: fixture with `manuscripts/cal-com/<recent-run>/` and `manuscripts/cal-com-pp-cli/<old-run>/`, call `resolveManuscripts("cal-com-pp-cli", "cal-com")`, assert API-slug path and recent run.

**Patterns to follow:**
- Existing `resolveManuscripts` structure (sequential keyed lookups, fuzzy fallback at the bottom) — preserve the shape; only flip branch order.
- Existing `createRunDir` test helper at `internal/cli/publish_resolve_test.go:13` — reuse it for the new subtests.
- Subtest naming and `assert` style already in `TestManuscriptLookupPriority`.

**Test scenarios:**
- Happy path: fixture has both `manuscripts/<api-slug>/<recent-run>/` and `manuscripts/<cli-name>/<old-run>/`. `resolveManuscripts(cliName, apiName)` returns the API-slug path and the recent run ID. *(Covers R1; the cal-com regression case from F2 positive)*
- Edge case: fixture has only `manuscripts/<cli-name>/<run>/`. `resolveManuscripts(cliName, apiName)` returns the CLI-name path and that run. *(Covers R2)*
- Edge case: fixture has only `manuscripts/<api-slug>/<run>/`. `resolveManuscripts(cliName, apiName)` returns the API-slug path and that run. *(Covers R3)*
- Edge case: fixture has neither named key but has a fuzzy-matchable directory (e.g., `manuscripts/steam/` for `apiName="steam-web"`). `resolveManuscripts` returns the fuzzy match. *(Covers R4)*
- Edge case: empty manuscripts root. `resolveManuscripts` returns empty path and empty run ID without erroring.
- Integration scenario: end-to-end `publish package --json` against a staged CLI whose `.printing-press.json` `run_id` matches an API-slug-keyed manuscripts directory; the JSON output's `run_id` field equals the manifest's `run_id`. Verified manually against a cal-com fixture as the explicit R5 acceptance check (see Verification below). *(Covers R5)*

**Verification:**
- `go test ./internal/cli/...` passes with the new and updated subtests.
- `go vet ./...` and `go build -o ./printing-press ./cmd/printing-press` succeed.
- `golangci-lint run ./...` clean on the touched file.
- `scripts/golden.sh verify` passes (no expected impact on golden fixtures — change is internal to a runtime helper, not a generator output).
- **R5 manual acceptance check (required before commit):** with both `~/printing-press/manuscripts/cal-com/<recent-run>/` and `~/printing-press/manuscripts/cal-com-pp-cli/<old-run>/` present locally, run `printing-press publish package --dir ~/printing-press/library/cal-com --category productivity --target /tmp/pp-r5 --json` and confirm the emitted `run_id` field equals the staged `cal-com/.printing-press.json` `run_id` (which should match `<recent-run>`). If a cal-com library is not available locally, fabricate the equivalent two-key fixture under any other API slug and run the analogous command.

---

## System-Wide Impact

- **Interaction graph:** Single in-binary call site (`newPublishPackageCmd` at `internal/cli/publish.go:368`). No external consumers of `resolveManuscripts`. The inline warn-only lookup at `:621–628` is unaffected (and intentionally not aligned per scope).
- **Error propagation:** No change. Function still returns `("", "")` when no candidate yields a run.
- **State lifecycle risks:** None. Read-only filesystem inspection; no migrations or stashing.
- **API surface parity:** No CLI flag or output schema change. JSON output continues to use the existing `run_id` / `manuscripts_included` fields; only the *value* of `run_id` becomes correct in the both-keys-present case.
- **Integration coverage:** End-to-end `publish package --json` covered by R5. Unit-level priority chain covered by `TestManuscriptLookupPriority`.
- **Unchanged invariants:** The fuzzy fallback (`resolveManuscriptDir`) behavior and the `findMostRecentRun` lex-sort tiebreak are preserved exactly.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| A reprint of a CLI that has *only* the legacy CLI-name key but no API-slug key would behave the same after the swap (CLI-name still wins via the second branch) — but a CLI in mid-migration that has a stale API-slug dir from a prior regen and a fresh CLI-name dir from a manual workflow would now silently pick the stale API-slug run. | Document the SKILL-convention-first semantics in the refreshed comment. The mid-migration case is not a supported scenario; if observed, a separate cleanup path or mtime-tiebreak can be added in follow-up. |
| Test fixture renames could leave a stale subtest name pointing at the wrong assertion. | Re-run the full `TestManuscriptLookupPriority` table after edits; verify subtest names and assertions match. |

---

## Documentation / Operational Notes

- No user-facing docs require updates. SKILL.md already documents the API-slug archive convention as canonical.
- Commit scope: `cli` (per AGENTS.md scope mapping).

---

## Sources & References

- **Origin issue:** [#598 — WU-2: resolveManuscripts prefers API-slug keying over CLI-name](https://github.com/mvanhorn/cli-printing-press/issues/598)
- **Parent retro:** [#597 — Cal.com (reprint) retro](https://github.com/mvanhorn/cli-printing-press/issues/597)
- Related code: `internal/cli/publish.go:464` (`resolveManuscripts`), `internal/cli/publish_resolve_test.go:129` (`TestManuscriptLookupPriority`)
- Related learnings: `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`
- SKILL convention: `skills/printing-press/SKILL.md:440, 489, 2607–2609`
