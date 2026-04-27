---
title: Printing Press P1 machine fixes from Dub retro
type: fix
status: active
date: 2026-04-27
deepened: 2026-04-27
origin: /tmp/printing-press/retro/20260427-011631-dub-retro.md
---

# Printing Press P1 machine fixes from Dub retro

## Overview

Three high-priority Printing Press fixes surfaced by retro on the Dub generation run (issue [#336](https://github.com/mvanhorn/cli-printing-press/issues/336)). Each ships as its own PR. None of them are Dub-specific — they affect every future generation:

- **U1: Make SKILL.md emit promoted-leaf paths.** When the generator promotes a single-op resource to a flat leaf (`qr`), `skill.md.tmpl` should emit the leaf form (`<cli> qr`) instead of the spec-operation-id form (`<cli> qr get-qrcode`) which is not a registered cobra path. The library repo's CI `unknown-command` check rejected this on the dub PR.
- **U2: Add the `unknown-command` check + sync test for verify-skill.** The bundled and canonical scripts are byte-identical at HEAD and neither implements `unknown-command`. Port the check from the library repo's CI script (`mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py`), keep both copies in sync via a Go hash test, and wire a lefthook hook to copy canonical → bundled on commit.
- **U3: Fix `lock promote`'s lost RunID so research-enrichment works.** `lock promote` already calls the enriching path (`pipeline.PromoteWorkingCLI` → `writeCLIManifestForPublish`). The actual bug: when the runstate lookup falls back to `pipeline.NewMinimalState`, `RunID` stays empty, `state.PipelineDir()` resolves to a bogus path, and `LoadResearch` silently ENOENTs. Real fix: have `NewMinimalState` (or the lock-promote path) recover the RunID from the working directory's adjacent runstate, OR pass an explicit research path through to enrichment.

**U1 must land before U2** to avoid a regeneration regression window (see Risks). U3 is independent of both. Recommended sequence: U1 → U2 → U3 (U1 is the user-visible fix the dub PR needs; U2 then makes Phase 4 catch future SKILL phantoms locally; U3 fixes the silent `novel_features` dropout).

> **What changed in this revision** (deepened 2026-04-27 after ce-doc-review). The first revision of this plan inherited the retro's diagnoses literally, two of which were wrong against current source: U3 claimed `lock promote` "bypasses enrichment" (it doesn't — `PromoteWorkingCLI` calls the enriching path, but `RunID` is empty in the fallback state); U2 claimed bundled vs canonical scripts had ~200 lines drift (they're byte-identical at HEAD, both 3-check, both missing `unknown-command`); U1 proposed a hidden cobra child to make `qr get-qrcode` resolve, but `command_promoted.go.tmpl` uses `cmd.Flags()` (local) so the hidden child wouldn't inherit flags. Reviewers (ce-feasibility-reviewer + ce-adversarial-document-reviewer) caught all three. The current plan replaces those approaches with corrected diagnoses traced to specific source lines.

---

## Problem Frame

This run printed `dub-pp-cli` end-to-end and exposed three machine-level defects that surface as **silent local-vs-CI divergence and broken publish flow**. Every defect is a recurring class of bug — not specific to Dub, and not blocked by Dub's spec shape — so the cost of leaving them is paid on every future generation.

Verified diagnoses against `internal/` source at HEAD (corrected from the retro's first-pass framing):

- **U1 (the dub PR's actual CI failure):** `internal/generator/templates/skill.md.tmpl` emits the spec operation-id form (`qr get-qrcode`) for every endpoint, regardless of whether `internal/generator/generator.go:buildPromotedCommandPlan` collapsed that resource into a leaf-promoted command. The maps `promotedResourceNames` and `promotedEndpointNames` already exist in the generator but are not propagated into the template's data context. Result: SKILL.md references a path that no `*cobra.Command.Use` matches, and the library repo's CI `unknown-command` check (which exists in its workflow but is missing from this repo's verifier — see U2) rejects the PR.

- **U2 (the silent local-vs-CI gap):**
  - `internal/cli/verify_skill_bundled.py` and `scripts/verify-skill/verify_skill.py` are byte-identical at HEAD (`md5 = 201bdc939a8a2e0c942f63c9f3afbae9` for both, 785 lines each).
  - **Neither file implements an `unknown-command` check.** Both run only `flag-names`, `flag-commands`, `positional-args`. Reference: `verify_skill.py:700` — `checks = only or {"flag-names", "flag-commands", "positional-args"}`.
  - The library repo's CI workflow (`mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py`) runs a different fork of the script that DOES implement `unknown-command`. That's the script that caught the dub PR.
  - The retro's "200 lines diverged" was likely a snapshot at a different point in time; it's not the current state. Whatever drift existed has been resolved by hand. What remains is that the canonical check the library repo runs has not been ported into either copy in this repo.

- **U3 (the missing novel_features in the manifest):**
  - `internal/cli/lock.go:241` calls `pipeline.PromoteWorkingCLI(cliName, dir, state)`.
  - `internal/pipeline/lock.go:225` (inside `PromoteWorkingCLI`) calls `writeCLIManifestForPublish(state, stagingDir)` — **the same enriching path the publish-pipeline uses.**
  - `internal/pipeline/publish.go:265-274` runs `LoadResearch(state.PipelineDir())` and copies `novel_features_built` into `m.NovelFeatures`.
  - `state.PipelineDir()` (in `internal/pipeline/state.go`) returns `RunPipelineDir(s.RunID)`.
  - When `lock.go:238` cannot find a runstate via `pipeline.FindStateByWorkingDir(dir)`, it falls back to `pipeline.NewMinimalState(cliName, dir)`. `NewMinimalState` (state.go:308) sets `APIName` and `WorkingDir` but **does not set `RunID`**. So `state.PipelineDir()` returns `RunPipelineDir("")` — a bogus path with an empty path segment. `LoadResearch` reads ENOENT and silently returns no novel features.
  - Result: every plan-driven or older-runstate `lock promote` produces a manifest missing `novel_features`, and `publish validate`'s transcendence check fails on the next step.
  - Note: catalog enrichment (CatalogEntry, Category, Description) at `publish.go:258-263` keys off `state.APIName`, which IS set by `NewMinimalState`, so catalog enrichment works in the same fallback path — the bug is specific to research lookup.

---

## Requirements Trace

- R1. **Promoted-resource SKILL emission matches cobra reality.** When the generator promotes a single-op resource to a leaf, `skill.md.tmpl`'s Command Reference emits the leaf form (`<cli> <resource>`), not the operation-id form (`<cli> <resource> <endpoint>`). (Origin F1)
- R2. **Multi-op resources unchanged.** Resources with two or more endpoints continue to emit the parent-and-children form (`<cli> links create`, `<cli> links delete`, etc.). The promoted-leaf treatment in R1 applies only to single-op resources that the generator already promotes. (Origin F1 negative)
- R3. **Verify-skill `unknown-command` check.** The canonical script `scripts/verify-skill/verify_skill.py` implements an `unknown-command` check that flags any command path in SKILL.md that does not resolve to a registered cobra `Use:` declaration in `internal/cli/*.go`. The bundled copy `internal/cli/verify_skill_bundled.py` matches the canonical byte-for-byte. (Origin F2 — corrected: the check is missing from both, not just the bundled copy.)
- R4. **Drift prevention.** A Go test (`TestVerifySkillScriptInSync`) hashes both files and fails when they diverge. A lefthook hook copies canonical → bundled on commit so the canonical is the source of truth. (Origin F2)
- R5. **`lock promote` enriches `novel_features` reliably.** When a working directory has accessible research data (whether via a recoverable runstate or via a research.json adjacent to the working dir), `lock promote` writes a manifest whose `novel_features` matches `research.json`'s `novel_features_built`. Absence of all research data does not error. (Origin F3 — corrected: the bug is in research-path resolution, not in helper extraction.)
- R6. **Local Phase 4 catches what CI catches.** Implicit cross-cutting requirement — the bundled `verify-skill` is in sync with the canonical (R3 + R4), so Phase 4 shipcheck catches `unknown-command` before PRs ship. The dub-style "local PASS, CI FAIL" pattern stops happening for promoted-leaf paths. (Origin F1 + F2 — joint requirement.)

---

## Scope Boundaries

- Existing multi-op resource generation is unchanged.
- The visible canonical command path users learn from `--help` and the README's Quick Start does **not** change. Users still see `<cli> qr` and the SKILL still highlights the leaf form.
- No changes to the auth env-var alias work (retro F5), the manuscripts resolver order (retro F4), the scorer's `registeredCommandFiles` walker (retro F6), the Insight scorer's store-backed regex (retro F7), or the recursion depth in README/SKILL templates (retro F8) — those are tracked separately as P2/P3 in issue #336.
- No changes to the cli-name-keyed → slug-keyed manuscripts migration (retro F4).
- No changes to legacy manifest-writing functions except where `RunID` resolution needs to land (U3).
- No cobra mechanism changes (no hidden subcommand, no aliases). The promoted-leaf fix is a SKILL template change only.
- The library repo's CI workflow (`mvanhorn/printing-press-library/.github/workflows/verify-skills.yml`) is not touched. It already runs the upstream-fork script that has `unknown-command`. After this plan ships, the upstream library repo's script and the canonical script in this repo will converge in behavior — but bringing them under a single source of truth is out of scope here.

### Deferred to Follow-Up Work

- **P2/P3 retro findings (F4–F8):** Each gets its own work unit and PR after this batch lands. See issue [#336](https://github.com/mvanhorn/cli-printing-press/issues/336).
- **Library repo CI signal:** No changes to `mvanhorn/printing-press-library/.github/workflows/verify-skills.yml` or its embedded `verify_skill.py`. Library repo continues running its own canonical script; this plan ensures the printing-press repo's bundled and canonical scripts implement the same checks.
- **Catalog metadata enrichment in `lock promote`:** Already works via `state.APIName` (which `NewMinimalState` sets correctly), so no change needed here. Mentioning it explicitly so a future reader doesn't assume U3's fix changes catalog handling.

---

## Context & Research

### Relevant Code and Patterns

- **Promotion logic (read-only for U1):**
  - `internal/generator/first_command_example.go:isPromotableSingleEndpoint` — gate predicate
  - `internal/generator/generator.go:buildPromotedCommandPlan` — emission planning, returns `(promotedCommands, promotedResourceNames, promotedEndpointNames)`. The two maps are already computed; just not propagated to SKILL.
  - `internal/generator/templates/command_promoted.go.tmpl` — the rendered `new<X>PromotedCmd` constructor (read-only for U1; do not modify cobra wiring)
- **Template rendering (U1's target):**
  - `internal/generator/generator.go:readmeData()` builds `readmeTemplateData` which feeds both `readme.md.tmpl` and `skill.md.tmpl` (per generator.go:959-960). Add the promoted maps here.
  - `internal/generator/generator.go:type readmeTemplateData` — extend with `PromotedResourceNames` and `PromotedEndpointNames`.
  - `internal/generator/templates/skill.md.tmpl` — Command Reference section emits `<cli> <resource> <endpoint>`; gate on the new fields to emit `<cli> <resource>` instead for promoted resources.
- **Verify-skill embedding (U2's target):**
  - `internal/cli/verify_skill.go` — `//go:embed verify_skill_bundled.py` then `var verifySkillScript string` then `os/exec` of `python3 -c verifySkillScript`
  - `internal/cli/verify_skill_bundled.py` — the bundled copy
  - `scripts/verify-skill/verify_skill.py` — the canonical copy (byte-identical to bundled at HEAD)
- **Manifest writers (U3's target):**
  - `internal/pipeline/state.go:NewMinimalState` (around line 308) — currently sets `APIName` and `WorkingDir` but not `RunID`. The fix lives here or in a wrapper invoked from `lock.go`.
  - `internal/pipeline/state.go:PipelineDir()` (method on `*PipelineState`) — `return RunPipelineDir(s.RunID)`.
  - `internal/cli/lock.go:newLockPromoteCmd` (around line 208-260) — already calls `PromoteWorkingCLI`, which already calls `writeCLIManifestForPublish`, which already runs the enrichment block. No change needed here unless the fix lives in lock.go (option B below).
  - `internal/pipeline/publish.go:265-274` — the enrichment block. Read-only reference for U3; no extraction needed.
- **Test patterns to mirror:**
  - `internal/generator/first_command_example_test.go:TestFirstCommandExampleHonorsPromotion` — promotion-aware behavior test pattern
  - `internal/generator/promoted_presence_check_test.go:TestPromotedPresenceCheckUsesPromotedType` — minimal-spec fixture pattern for promotion tests
  - `internal/generator/skill_test.go` — skill rendering test pattern (template fields, expected output substring assertions)
  - `internal/cli/verify_skill_test.go` — full verifier integration tests (`buildPrintingPressBinary`, generated SKILL.md fixture, exec the binary, parse JSON)
  - `internal/pipeline/climanifest_test.go:TestWriteCLIManifest` — manifest-writing fixture pattern
  - `internal/cli/lock_test.go` — lock-command integration test pattern
- **Pre-existing precedent for the upstream `unknown-command` check:**
  - `mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py` — the script the library repo's CI runs. Has the `unknown-command` check. **Read this and port the check to `scripts/verify-skill/verify_skill.py` in U2.** The check parses every \`<cli> ...\` invocation in SKILL.md (in bash recipes and inline backticks under "## Command Reference"), enumerates every cobra `Use:` declaration in `internal/cli/*.go`, and reports paths that don't resolve.

### Institutional Learnings

None directly applicable. The retro itself is the institutional record (`/tmp/printing-press/retro/20260427-011631-dub-retro.md`), and the source-truth corrections in this revision are documented inline.

### External References

- Library repo's canonical verifier — runs on every PR's `Verify SKILL.md` workflow. The exact failure that motivated U1: PR [mvanhorn/printing-press-library#135](https://github.com/mvanhorn/printing-press-library/pull/135), first run, `[unknown-command] dub-pp-cli qr get-qrcode: command path not found`.
- Cobra parent-with-Run + child semantics — researched in the original revision but no longer load-bearing. The chosen approach (template-only fix) avoids cobra changes entirely.

---

## Key Technical Decisions

- **Fix the SKILL template, not the cobra wiring (U1).** The original revision proposed a hidden cobra child on the promoted command. Reviewer evidence shows that approach breaks against `command_promoted.go.tmpl`'s use of local (`cmd.Flags()`) rather than persistent flags — the hidden child would have its own empty flag set and reject `--foo` even though the parent declares it. It also breaks against `Use: "{{.PromotedName}}{{positionalArgs .Endpoint}}"` declaring positional args on the parent only. The simpler fix is to make SKILL emission honor the promoted shape: the leaf form is the canonical post-promotion form, and SKILL should reflect that. Roughly 10 template lines and one struct-field addition.
- **Port `unknown-command` from the library repo, don't fork (U2).** The library repo already has a working implementation of the check. Read it, port it byte-equivalent into both copies in this repo, then enforce drift prevention via the sync test + lefthook hook.
- **Source-of-truth: canonical → bundled (U2).** Edits land in `scripts/verify-skill/verify_skill.py`. A pre-commit lefthook hook copies it into `internal/cli/verify_skill_bundled.py` so the embedded version is always in sync. The Go sync test is the safety net for any commit that bypasses lefthook.
- **Fix the actual bug in NewMinimalState's RunID resolution (U3).** Don't extract a no-op helper. Pick the option that best preserves correctness: either (A) make `NewMinimalState` look up an adjacent runstate (scan for a `pipeline/state.json` next to the working dir, or under a known relative location) and populate RunID when found; or (B) keep `NewMinimalState` as-is and have `internal/cli/lock.go:newLockPromoteCmd` resolve research.json explicitly (e.g., look for `<workingDir>/.printing-press-internal/research.json` or via the runstate paths the press writes during generation) and pass an enriched state.
- **Don't extract `EnrichManifestFromResearch` as a helper.** The original revision called for this; it's a no-op refactor (`writeCLIManifestForPublish` already runs the block). Leave the existing inline block at `publish.go:265-274` untouched. U3's surface is `state.go` (NewMinimalState) and/or `cli/lock.go`, not `publish.go`.
- **Add debug-level logging on LoadResearch path.** When `LoadResearch` returns ENOENT, write a single debug line ("research.json not found at \<path\>; skipping novel-features enrichment") so future regressions like F3 leave a breadcrumb. This is an FYI-grade improvement that costs nothing and prevents the next silent dropout from being silent.
- **No SKILL/README content change beyond U1's promoted-resource emission.** Multi-op resources keep today's exact wording. The change is conditional on the new template fields and does not touch unrelated content.

---

## Open Questions

### Resolved During Planning

- **Q: Where does leaf promotion happen?** A: `internal/generator/generator.go:buildPromotedCommandPlan` decides which resources promote and returns `promotedResourceNames` + `promotedEndpointNames`. `internal/generator/templates/command_promoted.go.tmpl` renders the constructor. The retro mentioned `internal/generator/wiring.go` — there is no such file.
- **Q: What function in `pipeline/publish.go` does the enrichment?** A: `writeCLIManifestForPublish` (not `BuildAndWriteCLIManifest` as the retro labeled it). The enrichment block runs at lines 265-274.
- **Q: Does `lock promote` already share any code with the publish-pipeline path?** A: **YES.** `internal/cli/lock.go:241` calls `pipeline.PromoteWorkingCLI(cliName, dir, state)`. `internal/pipeline/lock.go:225` (inside `PromoteWorkingCLI`) calls `writeCLIManifestForPublish`. The bug is upstream of `writeCLIManifestForPublish`, in the state's RunID being empty when `NewMinimalState` is the fallback.
- **Q: Are the bundled and canonical verify-skill scripts actually drifted?** A: **No, they are byte-identical at HEAD** (`md5 = 201bdc939a8a2e0c942f63c9f3afbae9` for both). The retro's "200 lines drift" was true at some past commit but has been manually reconciled. The remaining work is to add the missing `unknown-command` check (which both copies lack) and prevent future drift.
- **Q: Does the cobra hidden-child approach in U1 work?** A: **No.** `command_promoted.go.tmpl` uses `cmd.Flags()` (not `PersistentFlags()`), so flag declarations don't inherit to children. The parent's positional-arg declaration in `Use: "{{.PromotedName}}{{positionalArgs .Endpoint}}"` also doesn't inherit. The plan now uses the SKILL-template-only approach instead.
- **Q: Does CatalogEntry/Category/Description enrichment in `lock promote` need fixing too?** A: **No, it already works.** Catalog enrichment at `publish.go:258-263` keys off `state.APIName` (not `state.PipelineDir()`), and `NewMinimalState` does set `APIName`. The bug is specific to research lookup.
- **Q: Pick U3 Option A or Option B?** A: **Option A.** `grep -rn 'NewMinimalState' internal/ cmd/` returns exactly three references at HEAD: the definition (state.go:308), one test (lock_test.go:410), and one production caller (cli/lock.go:238). Only one production caller. By the plan's own rubric, A and B converge; A is preferred because it makes `NewMinimalState` correct by construction.
- **Q: Where should U3 Option A actually scan?** A: **Use `findRunstateStatePath(apiName)`** (state.go:201), which already iterates the scoped runstate registry and returns the most-recent matching entry. The "scan dir + parents up 3 levels" framing in earlier revisions was wrong: runstate doesn't live adjacent to working dirs — it's centralized at `~/printing-press/.runstate/<scope>/runs/<runID>/state.json`. `findRunstateStatePath` is the right primitive; reuse it from `NewMinimalState` after the `FindStateByWorkingDir` exact-match path has failed. Recovery only works while the runstate is still on disk; for GCed runs, `RunID` stays empty and enrichment fails gracefully (matching today's behavior with a clearer log).
- **Q: Lefthook hook stage for U2's sync?** A: **Pre-commit with `stage_fixed: true`**, mirroring the existing `fmt` block in `lefthook.yml`. Pre-push is unnecessary; the Go sync test is the safety net for `--no-verify`. No Makefile required for this work — the lefthook step plus the Go sync test cover both correctness paths.
- **Q: Where does the press write research.json during generation?** A: **`<RunRoot>/pipeline/research.json`** where `RunRoot` is `~/printing-press/.runstate/<scope>/runs/<runID>/`. `RunPipelineDir(runID)` (paths.go:80-94) constructs this path; `LoadResearch` reads from it. U3 Option A's recovery path goes through `findRunstateStatePath` to recover RunID, then the existing `LoadResearch(state.PipelineDir())` call resolves correctly.

### Deferred to Implementation

- **U1: Where in `Generate()` should `buildPromotedCommandPlan` run so `readmeData()` can read its outputs?** Today `renderSingleFiles()` (which calls `g.readmeData()` for SKILL emission) runs at `generator.go:1072` BEFORE `buildPromotedCommandPlan` runs at line 1079. The implementer must either: (a) lift `buildPromotedCommandPlan` to the top of `Generate()` and store its outputs on `g`, (b) memoize via `sync.Once`, or (c) call it twice (the function is pure over `g.Spec`, so safe but feels duplicative). Option (a) is the cleanest and is the recommended approach unless the implementer discovers a test-fixture dependency on the current ordering.
- **U2: How thorough does the upstream port need to be?** The upstream library script (`mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py`, 777 lines, md5 `1e5ab2af4b9ba2d5d28d1bf9f8069c55`) and the in-repo canonical (785 lines, md5 `201bdc939a8a2e0c942f63c9f3afbae9`) have **diverged ~1,400 lines**. They are forks that evolved independently — not a clean "missing one check" delta. The upstream's `check_unknown_commands` calls `find_command_source(cli_dir, cmd_path)`; the in-repo has a `find_command_source` too but with different return semantics (the in-repo wraps a `resolve_command_path` ambiguity-handler the upstream lacks). The implementer should: (a) diff the two scripts, (b) identify the helper closure the upstream `check_unknown_commands` depends on, (c) for each helper, decide whether to re-use the in-repo version (verifying semantic equivalence on test fixtures) or port the upstream version. Approach 1 ("port verbatim") is misleading — pick re-derivation using in-repo helpers as the primary path and audit the resulting behavior against a few library CLIs before merging.
- **U3: Should the LoadResearch debug log live in `publish.go` or be guarded to fire only on `lock promote`?** The plan's diagnosis says "U3's surface is `state.go` (NewMinimalState) and/or `cli/lock.go`, not `publish.go`," but the proposed log lives in publish.go's enrichment block — a shared codepath. If existing publish flows hit research-less states for unrelated reasons, the log will fire there too. The implementer can either (a) accept the broader log surface (it's debug-level, low volume), (b) thread an explicit "log on miss" hint through `state` so only `lock promote` triggers it, or (c) drop the log change from this plan entirely and file as a follow-up. Decide based on whether `LoadResearch` ENOENT is rare in publish flows today.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

### U1 — SKILL template emits promoted-leaf paths

```text
Today (single-op resource "qr" with endpoint "get-qrcode" — promoted by buildPromotedCommandPlan):

  cobra path:   <cli> qr                        ← real, registered
  SKILL.md says: <cli> qr get-qrcode            ← phantom — fails unknown-command

Proposed:

  cobra path:   <cli> qr                        ← unchanged
  SKILL.md says: <cli> qr                       ← matches reality

Implementation surface:
  generator.go:type readmeTemplateData          ← add 2 maps
  generator.go:readmeData()                     ← populate maps from buildPromotedCommandPlan
  skill.md.tmpl  Command Reference loop         ← gate emission on map lookup
```

Multi-op resources are entirely unchanged — the gate fires only when `PromotedResourceNames[<resource>]` is true.

### U3 — Recover RunID in NewMinimalState via the scoped runstate registry

```text
Today:

  newLockPromoteCmd
   └── FindStateByWorkingDir(dir) → ERR             ← exact-match by WorkingDir failed
        └── NewMinimalState(cliName, dir)            ← APIName set, RunID=""
             └── PromoteWorkingCLI(state)
                  └── writeCLIManifestForPublish
                       ├── catalog enrichment        ← works (uses APIName)
                       └── novel_features enrichment ← FAILS (PipelineDir = RunPipelineDir("") = bogus)

Proposed (Option A — chosen because NewMinimalState has only one production caller):

  NewMinimalState(cliName, dir)
   ├── set APIName, WorkingDir                 ← existing
   ├── findRunstateStatePath(APIName)          ← NEW: scoped runstate registry lookup
   │   └── ~/printing-press/.runstate/<scope>/runs/*/state.json (most recent matching APIName)
   ├── if found AND state's APIName matches:
   │   └── populate state.RunID                ← recovery succeeds
   └── if not found OR validation fails:
       └── leave state.RunID = ""              ← today's behavior preserved (debug log emitted in publish.go)

Result:
  state.PipelineDir() now returns RunPipelineDir(<recovered RunID>)
   └── ~/printing-press/.runstate/<scope>/runs/<runID>/pipeline/
        └── research.json found → enrichment populates novel_features
```

`findRunstateStatePath` is the existing primitive (state.go:201) — no new infrastructure required. The recovery only works while the runstate is still on disk; for GCed runs, RunID stays empty and enrichment falls through to its no-op path with a debug log on miss. Catalog enrichment is unaffected (already works via APIName).

---

## Implementation Units

- U1. **Make SKILL.md emit promoted-leaf paths**

**Goal:** When the generator promotes a single-op resource to a leaf (e.g., `qr` for `qr/get-qrcode`), `skill.md.tmpl` emits `<cli> qr` in its Command Reference section, not `<cli> qr get-qrcode`. Multi-op resources are unchanged. The shipped SKILL.md passes the upstream `unknown-command` check.

**Requirements:** R1, R2, R6

**Dependencies:** None. (The original revision said this depends on U2, but U1 can be tested directly against the upstream library-repo script today.)

**Files:**
- Modify: `internal/generator/generator.go` — extend `readmeTemplateData` with `PromotedResourceNames map[string]bool` and `PromotedEndpointNames map[string]string`; populate them in `readmeData()` from `buildPromotedCommandPlan`'s outputs (which are computed earlier in the generation pipeline; surface them on the `Generator` struct or pass via context)
- Modify: `internal/generator/templates/skill.md.tmpl` — in the Command Reference loop where endpoints are listed, gate on `PromotedResourceNames[<resource>]` and emit `<cli> <resource>` instead of `<cli> <resource> <endpoint>` for promoted resources
- Add: tests in `internal/generator/skill_test.go` (or a new sibling)

**Approach:**
- `buildPromotedCommandPlan` (generator.go:1094-1112) already computes the maps and returns them (`promotedResourceNames`, `promotedEndpointNames`). Today they're called mid-`Generate()` at line 1079 — AFTER `renderSingleFiles()` (which renders SKILL.md via `readmeData()`) at line 1072. To make the maps available to `readmeData()`, **lift `buildPromotedCommandPlan` to the top of `Generate()`** (before `renderSingleFiles`), store its outputs on `g` (e.g., `g.PromotedResourceNames`, `g.PromotedEndpointNames`, `g.PromotedCommands`), and have downstream phases consume the cached values. The function is pure over `g.Spec`, so the move is mechanically safe — verify by running existing tests after the move.
- Then add `PromotedResourceNames` and `PromotedEndpointNames` fields to `readmeTemplateData` and populate them from `g.*` in `readmeData()`.
- In `skill.md.tmpl`'s Command Reference section (lines 98-104), the per-resource block is currently of the form `- \`{{$cli}} {{$resource}} {{$endpoint}}\` — {{$desc}}`. Wrap the inner per-endpoint emission in a conditional: when `index $.PromotedResourceNames $name` is true, emit `- \`{{$cli}} {{$name}}{{positionalArgs $endpoint}}\` — {{$desc}}` (the leaf form, with positional args preserved from the existing template helper); otherwise emit today's per-endpoint form.
- Multi-op resources never have entries in `PromotedResourceNames` (the map is populated only by `buildPromotedCommandPlan` for single-op promotion candidates per `isPromotableSingleEndpoint`), so the negative branch automatically preserves today's emission.
- Out of scope for this work unit: `$resource.SubResources` iteration. The current template ignores SubResources; that gap is pre-existing and tracked separately.

**Patterns to follow:**
- `internal/generator/first_command_example_test.go:TestFirstCommandExampleHonorsPromotion` — the "honor promotion in emission" test pattern. The same fixture-spec idea applies here: a minimal spec with one single-op resource, render SKILL, assert the leaf form is emitted.
- `internal/generator/skill_test.go` — existing skill rendering tests; mirror their fixture-and-substring-assertion shape.

**Test scenarios:**
- Happy path (single-op): `TestSkillEmitsPromotedLeafPath`. Fixture spec with one resource `qr` containing one endpoint `get-qrcode`. Render SKILL. Assert it contains `\`<cli>-pp-cli qr\`` AND does NOT contain `\`<cli>-pp-cli qr get-qrcode\`` in its Command Reference section.
- Negative (multi-op unchanged): `TestSkillRetainsOpIDFormForMultiOp`. Fixture spec with one resource `links` containing `create` + `delete`. Render SKILL. Assert it contains `\`<cli>-pp-cli links create\`` and `\`<cli>-pp-cli links delete\`` (today's behavior preserved).
- Edge (mixed-shape spec): `TestSkillMixedPromotedAndMulti`. Fixture spec with one single-op resource `qr/get-qrcode` and one multi-op resource `links/create + links/delete`. Assert both shapes coexist correctly.
- Edge (positional-args resource): `TestSkillPromotedLeafWithPositionalArgs`. Fixture spec where the promoted endpoint has a positional arg (e.g., `qr` with required positional `<url>`). Assert SKILL emits `\`<cli>-pp-cli qr <url>\`` (leaf form with positional preserved by the existing `positionalArgs` template helper), not `\`<cli>-pp-cli qr get-qrcode <url>\``.
- *Note:* `TestSkillPassesUnknownCommandCheck` (the CI-parity integration test that runs the full `verify-skill --only unknown-command` against a generated single-op fixture) lives in U2's PR, not U1's. The check itself doesn't exist in this repo until U2 lands. Until then, U1's manual smoke verification (regenerate dub, grep for phantom paths) is the equivalent signal.

**Verification:**
- `go test ./internal/generator/...` passes.
- Smoke: regenerate a known single-op CLI (e.g., dub) from its spec; grep its `SKILL.md` for `qr get-qrcode` and `tokens create-referrals-embed` — both should be absent. The leaf forms (`qr`, `tokens`) should be present.
- Regenerated dub's SKILL.md passes `python3 scripts/verify-skill/verify_skill.py --dir <gen-dir> --only unknown-command` (post-U2).
- Golden harness (`scripts/golden.sh verify`) passes — the golden fixture for `golden-api` may need an update if it has a single-op resource. Run `scripts/golden.sh update` after verification and review the diff: expected delta is the SKILL Command Reference line for any promoted resource flipping from operation-id form to leaf form.

---

- U2. **Add `unknown-command` check + drift prevention for verify-skill**

**Goal:** The canonical script `scripts/verify-skill/verify_skill.py` implements the `unknown-command` check (matching the library repo's CI). The bundled copy `internal/cli/verify_skill_bundled.py` is byte-identical to the canonical, enforced by a Go test. A lefthook hook keeps them in sync on commit.

**Requirements:** R3, R4, R6

**Dependencies:** None.

**Files:**
- Modify: `scripts/verify-skill/verify_skill.py` — port the `unknown-command` check from `mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py`. Update `checks = only or {...}` (currently line 700) to include `"unknown-command"`. Add the check function and route it in the dispatcher.
- Replace: `internal/cli/verify_skill_bundled.py` (becomes a generated copy of the canonical, regenerated by the lefthook hook)
- Add: `internal/cli/verify_skill_sync_test.go` — hashes both files, asserts equal
- Modify: `lefthook.yml` — add a pre-commit hook that runs `cp scripts/verify-skill/verify_skill.py internal/cli/verify_skill_bundled.py` and stages the bundled copy
- Optionally add: `internal/cli/verify_skill_test.go` extension for `unknown-command` integration tests (or add a new test file)

**Approach:**
- **Step 1: Diff-and-decide before porting.** Read `mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py` (locally cached at `~/printing-press/.publish-repo/.github/scripts/verify-skill/verify_skill.py`, 777 lines, md5 `1e5ab2af4b9ba2d5d28d1bf9f8069c55`). Compare to the in-repo `scripts/verify-skill/verify_skill.py` (785 lines, md5 `201bdc939a8a2e0c942f63c9f3afbae9`). The two have **diverged ~1,400 lines** — they are forks that evolved independently; **a verbatim lift will not work**. Identify the upstream's `check_unknown_commands` implementation (around lines 596-655 in the upstream) and its helper closure (`find_command_source`, `resolve_command_path` if present). For each helper, check whether the in-repo version already exists with semantically-equivalent behavior. **Prefer re-deriving `check_unknown_commands` using the in-repo's existing helpers** (which have richer ambiguity-handling) over a blind lift.
- **Step 2: Port the check into in-repo canonical.** Add a `check_unknown_commands` function to `scripts/verify-skill/verify_skill.py` that uses the in-repo's existing `find_command_source` / `resolve_command_path` primitives. Mirror the upstream's docstring and finding format. Update the `choices` and `checks` set at line 700-758 to include `"unknown-command"`. The check parses every \`<cli> ...\` invocation in SKILL.md (in bash recipes and inline backticks under "## Command Reference"), enumerates every cobra `Use:` declaration in `internal/cli/*.go`, and reports paths that don't resolve. Built-in cobra commands (`help`, `completion`, `version`) are whitelisted.
- **Step 3:** Copy `scripts/verify-skill/verify_skill.py` → `internal/cli/verify_skill_bundled.py` (or run via the lefthook hook from step 5 to do the copy automatically).
- **Step 4:** Add `internal/cli/verify_skill_sync_test.go`:
  ```text
  func TestVerifySkillScriptInSync(t *testing.T) {
      // Read both files, compute SHA-256, assert equal.
      // On mismatch, fail with a message naming
      // scripts/verify-skill/verify_skill.py as the source of truth and pointing
      // at the lefthook hook to regenerate.
  }
  ```
  (signature is directional — implementer picks exact shape)
- **Step 5:** Add a pre-commit hook in `lefthook.yml` mirroring the existing `fmt` block: `glob: "scripts/verify-skill/verify_skill.py"`, `run: cp scripts/verify-skill/verify_skill.py internal/cli/verify_skill_bundled.py`, `stage_fixed: true`. The `stage_fixed: true` makes lefthook re-add the regenerated bundled file to the commit. Pre-push is not needed; the Go sync test is the `--no-verify` safety net.
- **Step 6: Validate against existing library CLIs.** Before merging, run `scripts/verify-skill/verify_skill.py --only unknown-command` against every CLI in `~/printing-press/library/`. Any false positive on a CLI that the upstream library-repo CI passes is a porting bug and must be fixed before landing.

**Patterns to follow:**
- `internal/cli/verify_skill_test.go:TestVerifySkill_*` — full verifier integration tests already exist; mirror their `buildPrintingPressBinary(t)` setup for the new `unknown-command` integration test.
- The library repo's existing `unknown-command` implementation is the spec — port it verbatim, don't reinvent.
- `lefthook.yml` existing pre-commit blocks (see go-fmt and friends) — mirror the shape.

**Test scenarios:**
- Happy path (sync test): `TestVerifySkillScriptInSync`. Reads both `internal/cli/verify_skill_bundled.py` and `scripts/verify-skill/verify_skill.py`, computes SHA-256 of each, asserts equal. Failure message names the canonical file and references the lefthook hook.
- Happy path (unknown-command check): `TestVerifySkill_DetectsPhantomPath`. Build a fixture CLI source where SKILL.md references `\`<cli> qr get-qrcode\`` but the cobra source has only `Use: "qr"`. Run `printing-press verify-skill --dir <fixture>`. Assert non-zero exit and `unknown-command` finding in output. Mirrors `TestVerifySkill_DetectsWrongFlagOnCommand`'s shape.
- Happy path (unknown-command no false positive): `TestVerifySkill_PassesWhenAllPathsResolve`. Build a fixture CLI source where every SKILL path resolves. Run the verifier. Assert exit 0.
- Edge (recipe vs Command Reference scope): the upstream check parses both bash recipes and inline backticks under "## Command Reference". Test that a phantom path in a recipe block IS flagged; test that a phantom path inside a different markdown section (e.g., "## Auth") is NOT flagged (matches upstream scope).
- Edge (cobra default subcommands): `help`, `completion`, `version` are auto-registered by cobra. Test that paths like `\`<cli> help\`` are NOT flagged as unknown.
- Integration (CI-parity, moved from U1): `TestSkillPassesUnknownCommandCheck`. Generate a CLI from a U1-style single-op fixture spec (or reuse U1's fixture if it lands first). Run `python3 scripts/verify-skill/verify_skill.py --dir <gen-dir> --only unknown-command` (now valid because U2 added the check). Assert no findings on a clean fixture; assert findings on a deliberately-broken SKILL.
- Negative (lefthook bypass): documented in the failure message, not in a Go test. If a commit lands with `--no-verify` and the bundled copy isn't refreshed, the next CI run fails the sync test.
- Negative (correctness vs identity): the sync test ensures canonical and bundled are byte-identical, but does not validate the check's correctness. The integration tests above (running `verify-skill` against fixture CLIs) are the correctness layer.

**Verification:**
- `go test ./internal/cli/... -run VerifySkill` passes.
- `printing-press verify-skill --dir <a CLI with SKILL.md referencing a phantom command> --json` exits non-zero with `unknown-command` in output.
- `diff scripts/verify-skill/verify_skill.py internal/cli/verify_skill_bundled.py` is empty.
- After running `lefthook run pre-commit`, both files have identical hashes.
- Golden harness passes.

---

- U3. **Fix `lock promote`'s lost RunID so research-enrichment works**

**Goal:** `printing-press lock promote --cli <X> --dir <work>` produces a CLI manifest whose `novel_features` is populated from `research.json`'s `novel_features_built`, even when `FindStateByWorkingDir` falls back to `NewMinimalState`. Absence of all research data does not error.

**Requirements:** R5

**Dependencies:** None.

**Files:**
- Modify: `internal/pipeline/state.go:NewMinimalState` (Option A) — recover `RunID` by scanning for adjacent `pipeline/state.json` or research.json relative to the working dir; populate `RunID` when found. **OR**
- Modify: `internal/cli/lock.go:newLockPromoteCmd` (Option B) — resolve research.json directly (read from a known relative location under the working dir or via the runstate paths the press writes during generation) and ensure the state passed to `PromoteWorkingCLI` has `RunID` set.
- Modify: `internal/pipeline/publish.go` (small change) — when `LoadResearch` returns ENOENT, write a single debug-level log line ("research.json not found at \<path\>; skipping novel_features enrichment") so future regressions leave a breadcrumb. (Address Adversarial F6.)
- Add: tests in `internal/pipeline/state_test.go` (Option A) or `internal/cli/lock_test.go` (Option B), plus an integration test exercising `lock promote` end-to-end with a research.json fixture

**Approach:**
- **Use Option A — recovery in `NewMinimalState`.** Verified at HEAD: `NewMinimalState` has exactly one production caller (`internal/cli/lock.go:238`) — `grep -rn 'NewMinimalState' internal/ cmd/` returns the definition, one test, and one production reference. Options A and B converge in scope; A is preferred because it makes `NewMinimalState` correct by construction.
- **Use `findRunstateStatePath` for recovery, not an adjacent-directory scan.** The runstate registry is centralized at `~/printing-press/.runstate/<scope>/runs/<runID>/state.json` — it does NOT live adjacent to working dirs. `findRunstateStatePath(apiName)` (state.go:201) already iterates the scoped runstate registry by APIName and returns the most-recent matching entry. Reuse it from `NewMinimalState` after the canonical `FindStateByWorkingDir(workingDir)` exact-match has failed:
  - In `NewMinimalState`, after setting `APIName` and `WorkingDir`, call `findRunstateStatePath(state.APIName)` (or its public equivalent if it's currently package-private — promote to exported if needed).
  - If a state file is found, read it, extract its `RunID`, and validate that the discovered state's `APIName` matches (post-trim) before adopting. The validation defends against runstate collisions in the same scope across different APIs.
  - If found and validated, populate `state.RunID`. If not found or validation fails, leave `RunID` empty (today's behavior preserved) — the enrichment block falls through to its no-op path.
  - Recovery only works while the runstate is still on disk. For GCed runs, `RunID` stays empty and `novel_features` enrichment is genuinely unrecoverable from this entrypoint — the debug log makes that visible.
- **Validate APIName comparison uses trimmed form on both sides.** `NewMinimalState(cliName, ...)` derives `APIName = naming.TrimCLISuffix(cliName)`. The runstate's stored `APIName` was set during `generate` via `cleanSpecName()`. Both pipelines should produce equivalent slugs for normal API names. Edge cases (names with periods, e.g. cal.com) may diverge — verify with a test fixture.
- **Logging in publish.go:** In `writeCLIManifestForPublish`'s enrichment block at line 265-274, capture the path that `LoadResearch` was called with. On error (ENOENT or otherwise), emit a single `fmt.Fprintf(os.Stderr, "debug: ...")` line. The log fires for every caller of `writeCLIManifestForPublish`, not just `lock promote` — that's intentional: any flow that reaches this enrichment block with no research.json benefits from the breadcrumb. If existing publish flows hit this fallback frequently (verify by checking existing `publish` integration tests), guard the log behind an explicit "log on miss" hint threaded through `*PipelineState` — but the default is unconditional debug-level emit.

**Patterns to follow:**
- `internal/pipeline/state_test.go` — existing tests for state functions (look for `NewState`, `NewStateWithRun`, etc.); mirror their fixture-and-assertion shape.
- `internal/pipeline/climanifest_test.go:TestWriteCLIManifest` — manifest fixture pattern using `t.TempDir()` and reading back the JSON.
- `internal/cli/lock_test.go` — full lock-command integration test pattern.

**Test scenarios:**
- Happy path (Option A): `TestNewMinimalState_RecoversRunIDFromAdjacentState`. Fixture: a working dir with a sibling `pipeline/state.json` containing `{"run_id": "20260427-001"}`. Call `NewMinimalState(cliName, workingDir)`. Assert returned state has `RunID == "20260427-001"`.
- Edge (Option A): `TestNewMinimalState_NoAdjacentStateLeavesRunIDEmpty`. Fixture: a working dir with no adjacent runstate. Call `NewMinimalState`. Assert `RunID == ""` (back-compat, no error, today's behavior preserved).
- Happy path (full integration, regardless of A/B): `TestLockPromote_PopulatesNovelFeatures`. Fixture: a generated CLI working dir with an adjacent runstate AND a `pipeline/research.json` containing `novel_features_built: [...3 entries...]`. Run `lock promote`. Read back `<library>/<api>/.printing-press.json`. Assert `novel_features` contains 3 entries with matching `name`/`command`/`description`. Mirrors existing lock_test.go style.
- Negative (no research): `TestLockPromote_NoResearchJsonStillSucceeds`. Fixture: a working dir with no research.json (e.g., a hand-built CLI). Run `lock promote`. Assert success, exit 0, manifest has no `novel_features` field (omitempty).
- Edge (research path missing — should NOT error): `TestLockPromote_HandlesMissingPipelineDir`. Fixture: a working dir whose adjacent runstate has a `RunID` that resolves to a non-existent pipeline dir (e.g., the runstate was deleted but the working dir survived). Assert `lock promote` succeeds without error.
- Logging: `TestLoadResearch_LogsPathOnEnoent`. Capture stderr while exercising the enrichment path with no research.json. Assert a debug line mentioning the attempted path appears. (Optional — only if the implementer adds a log.)
- Regression (publish-pipeline path still works): `TestWriteCLIManifestForPublish_StillEnriches`. Existing fixture/test path that exercises the publish-pipeline directly with a fully-populated state. Assert `novel_features` is populated as before. (If no such test exists today, add a smoke version.)

**Verification:**
- `go test ./internal/pipeline/... ./internal/cli/...` passes.
- Manual smoke: generate dub fixture → `lock promote` → `cat .printing-press.json | jq .novel_features` returns the expected entries (matching research.json).
- `printing-press publish validate --dir <library/dub>` reports `transcendence: PASS`.
- Golden harness passes.

---

## System-Wide Impact

- **Interaction graph:**
  - U1 affects every generated CLI with a single-op resource (most APIs have at least one). The change is template-only; no cobra wiring touched. Multi-op resources are gated out and unchanged. **U1 also lifts `buildPromotedCommandPlan` to run earlier in `Generate()` so its outputs are visible to `readmeData()`.** This re-ordering touches the generation pipeline's call sequence; existing tests should still pass because the function is pure over `g.Spec`, but a regression in any test that depends on the current call ordering would surface immediately.
  - U2 affects every Phase 4 shipcheck — the bundled `verify-skill` becomes stricter. Existing CLIs in the library will start failing this check if their SKILL.md has phantom command paths. **This couples to U1.** When U1 lands first (recommended), regenerated CLIs auto-resolve. If U2 ships before U1, every regeneration of older library CLIs fails Phase 4 on phantom paths until U1 also lands. See Risks table for the mitigation.
  - U3 affects every `lock promote` invocation that hits the `NewMinimalState` fallback — i.e., plan-driven CLIs and any other path where `FindStateByWorkingDir` doesn't resolve. Existing flows that don't have research.json (hand-built CLIs) continue to work — the recovery-or-leave-empty pattern keeps `RunID` empty when nothing is found.
- **Error propagation:**
  - U2: If the upstream `unknown-command` check has any false-positive cases, those propagate into Phase 4. Mirror the upstream check verbatim to avoid divergence; the upstream is already tuned against multiple library CLIs.
  - U3: A corrupt or unparseable adjacent state file should not break `NewMinimalState`. Treat unrecoverable runstate as "no recovery" and leave `RunID` empty — same fallback as today.
- **State lifecycle risks:**
  - U2's bundled-script regeneration runs every commit via lefthook. The Go sync test catches drift if lefthook is bypassed. Both layers are needed: lefthook prevents drift on the happy path, sync test catches `--no-verify` cases.
  - U3's research recovery reads adjacent state but does not write it. No new state lifecycle risks.
- **API surface parity:**
  - U2: The binary's `verify-skill` exit codes and JSON shape do not change; only the check-set expands. Callers reading `--json` will see new finding entries with `check: unknown-command`. Document in release notes.
- **Integration coverage:**
  - U1's `TestSkillPassesUnknownCommandCheck` (post-U2) exercises cross-tool cohesion: the same check runs locally and in CI.
  - U3's `TestLockPromote_PopulatesNovelFeatures` exercises the full `lock promote` → `PromoteWorkingCLI` → `writeCLIManifestForPublish` chain.
- **Unchanged invariants:**
  - The visible canonical command path users see in `--help` and the README's Quick Start. `<cli> qr --url X` still works exactly as today.
  - Multi-op resource generation. `<cli> links create`/`delete`/etc. unchanged.
  - The legacy publish-pipeline manifest output bytes. After U3, byte-identical to today for runs where the runstate was already resolvable; ENRICHED for runs that previously lost `novel_features`.
  - Cobra command surface. No new commands, no aliases, no hidden subcommands. SKILL emission only.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| U1's template change has subtle interactions with the existing Command Reference loop | Mirror the existing emission style; add the conditional with the smallest possible scope (one-line gate per emission). The mixed-shape test (`TestSkillMixedPromotedAndMulti`) catches surface bugs in either branch. |
| U1's `buildPromotedCommandPlan` reordering breaks a test that depends on the current `Generate()` call sequence | Run `go test ./internal/generator/...` after the move and before the template change. If a test fails, investigate; the function is pure over `g.Spec`, so any failure points at a test fixture coupling that needs cleanup, not a real regression in generation. |
| U2's port of `unknown-command` re-uses in-repo helpers that diverge from upstream's behavior | The two scripts have diverged ~1,400 lines and call into different helper graphs — a verbatim lift will not work. Diff first, identify the helper closure, prefer re-derivation using in-repo helpers. Validate by running the new check against every CLI in `~/printing-press/library/` and comparing findings against what the upstream library-repo CI reports for the same set. |
| U2 → U1 sequencing creates a regression window for library CLIs in `lock promote` regeneration | **Recommended sequencing: U1 lands BEFORE U2.** When U2's `unknown-command` check goes live in Phase 4 shipcheck, U1's SKILL emission already produces clean leaf paths, so existing CLIs auto-pass on regeneration. If circumstances force U2 first, gate the `unknown-command` check behind a flag that defaults off until U1 ships. |
| Sync test breaks every PR that touches the canonical script until bundled is regenerated | Lefthook hook auto-regenerates on commit; sync test is the safety net for `--no-verify`. Test failure message names the lefthook command to run manually. |
| U3's runstate recovery via `findRunstateStatePath` adopts a stale runstate that doesn't match the current working dir | Validate the discovered state's `APIName` matches `naming.TrimCLISuffix(cliName)` before adopting. For APIs whose names diverge across `cleanSpecName` and `TrimCLISuffix` (e.g., "cal.com"), add a fixture test to confirm both paths produce equivalent slugs. If the recovered state's `WorkingDir` is also reachable from the state file, optionally cross-check it against the current working dir for stronger validation. |
| U3 Option A makes `NewMinimalState` slower for callers that don't care about runstate | Only one production caller exists (`lock.go:238`), and it explicitly wants this recovery. The runstate registry scan is O(number-of-runs-for-this-API), bounded in practice. The performance cost is negligible vs the correctness gain. |
| U3's chosen option (A vs B) doesn't fully resolve research.json for plan-driven CLIs | The integration test `TestLockPromote_PopulatesNovelFeatures` runs the full chain. If it fails for plan-driven fixtures, the chosen option is incomplete; iterate to find the actual research-path location for that flow. |
| Golden fixture diffs from U1's template change | Golden harness `scripts/golden.sh verify` will surface any expected diff. If the golden fixture exercises a single-op resource, the implementer runs `scripts/golden.sh update` and reviews the diff. Expected delta is the SKILL Command Reference line for any promoted resource flipping from operation-id form to leaf form. Document the rationale in the PR description so the diff is reviewable. |
| Library repo's CI script and this repo's canonical drift in opposite directions in the future | Out of scope for this plan, but worth flagging: a follow-up could submodule the library repo's script or vice versa. For now, the assumption is that this repo's canonical is authoritative and the library repo's script will be updated to match (separate PR). |

---

## Documentation / Operational Notes

- Release notes: "Phase 4 shipcheck now runs an `unknown-command` check matching the library repo's CI, and SKILL.md emission for promoted single-op resources now uses the leaf form (`<cli> qr` instead of `<cli> qr get-qrcode`). Existing CLIs may need a regeneration to pick up the matching SKILL fix."
- No changes to `AGENTS.md` or other developer docs beyond the release notes.
- One follow-up reminder: U2's sync test forces the canonical and bundled scripts to track each other. If a future check is added to the canonical, both copies update automatically via the lefthook hook. The sync test is the safety net.
- U3's debug log is intentionally low-volume — it should fire only when enrichment fails to find research.json. If it starts firing on every `lock promote`, that's a signal that the recovery in U3 is incomplete and warrants follow-up.

---

## Sources & References

- **Origin document:** [Dub retro](/tmp/printing-press/retro/20260427-011631-dub-retro.md) — findings F1, F2, F3 (with corrected diagnoses verified against current source)
- **Issue:** [#336 Retro: Dub — 8 findings, 8 work units](https://github.com/mvanhorn/cli-printing-press/issues/336)
- **Concrete CI failure that motivated U1:** [mvanhorn/printing-press-library#135](https://github.com/mvanhorn/printing-press-library/pull/135) first run, `[unknown-command] dub-pp-cli qr get-qrcode`
- **The upstream `unknown-command` implementation:** `mvanhorn/printing-press-library/.github/scripts/verify-skill/verify_skill.py` (locally cached at `~/printing-press/.publish-repo/.github/scripts/verify-skill/verify_skill.py`)
- **Related code (verified locations at HEAD):**
  - `internal/generator/templates/skill.md.tmpl` — Command Reference loop (U1's target)
  - `internal/generator/generator.go:type readmeTemplateData` and `readmeData()` — template data wiring (U1's target)
  - `internal/generator/generator.go:buildPromotedCommandPlan` — produces `promotedResourceNames` and `promotedEndpointNames` (U1's data source)
  - `internal/generator/templates/command_promoted.go.tmpl` — read-only for U1; the existing `cmd.Flags()` (local) usage is what makes the original revision's hidden-child mechanism unworkable
  - `internal/cli/verify_skill.go` — `//go:embed verify_skill_bundled.py` (U2's host)
  - `internal/cli/verify_skill_bundled.py` and `scripts/verify-skill/verify_skill.py` — byte-identical at HEAD (U2's targets)
  - `internal/pipeline/state.go:NewMinimalState` — does not set `RunID` (U3's Option A target)
  - `internal/cli/lock.go:newLockPromoteCmd` — calls `PromoteWorkingCLI`; the fallback to `NewMinimalState` is at ~line 238 (U3's Option B target)
  - `internal/pipeline/lock.go:PromoteWorkingCLI` — calls `writeCLIManifestForPublish` (read-only for U3; not the bug site)
  - `internal/pipeline/publish.go:writeCLIManifestForPublish` (lines 176-278) — already runs the enrichment block; not the bug site (read-only for U3)
  - `internal/pipeline/publish.go:265-274` — the LoadResearch enrichment block; receives the `state.PipelineDir()` that returns a bogus path when RunID is empty (the symptom site, not the fix site)
- **Recent precedent:** PR [#335 fix(cli): printing-press P1 machine fixes (issue #333)](https://github.com/mvanhorn/cli-printing-press/pull/335) — the same shape of multi-finding P1 retro fix bundle. Different findings (doctor transport, no-auth gating, html-extract noscript) but same review pattern: one PR per work unit, references the issue.
- **Document review (deepening pass):** ce-coherence-reviewer, ce-feasibility-reviewer, ce-adversarial-document-reviewer (run 2026-04-27). Major findings led to this revision; reviewers' raw output preserved in the conversation transcript and the retro at `/tmp/printing-press/retro/20260427-011631-dub-retro.md`.
