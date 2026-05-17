---
title: "feat(skills): printing-press-amend — add direct-input mode + sniff finding type"
type: feat
status: active
created: 2026-05-16
origin: docs/plans/2026-05-15-feat-printing-press-amend-skill-plan.md
related_pr: https://github.com/mvanhorn/cli-printing-press/pull/1490
related_brainstorm: docs/brainstorms/2026-05-15-printing-press-amend-requirements.md
---

# feat(skills): printing-press-amend — add direct-input mode + sniff finding type

## Summary

Extend the unmerged `/printing-press-amend` skill (PR #1490, branch `feat/printing-press-amend`) so it accepts two input sources instead of one:

1. **Dogfood mode** (already built) — mine the active Claude Code session transcript for friction.
2. **Direct-input mode** (this plan) — accept user-supplied asks in the slash-command prompt: rename a command, add commands or feeds, fix a named bug, optionally sniff the source site for new endpoints.

Both modes converge at Phase 2 onward. Findings have identical shape; Phase 3 tiering, Phase 4 plan+execute, Phase 5 scrub, Phase 6 PR draft, and Phase 7 PR open are mode-agnostic. The expansion is additive — dogfood mode behavior does not change.

The trigger is the 2026-05-16 Digg-CLI amend, where the user supplied a concrete delta (rename, add four feed URLs, sniff for more) and the skill instead halted hunting for a transcript. Direct-input mode makes that ask first-class.

This plan ships in the same PR (#1490) — the skill has not landed yet and the broader scope replaces the current dogfood-only framing.

---

## Problem Frame

The current `SKILL.md` Phase 1 (`Friction Capture`) opens with: *"Resolve the active session transcript file ... walk the transcript and extract friction signals."* There is no input path that does not require a transcript.

When the user already knows what they want changed — verbatim asks like "rename Digg 1000 to Digg, add these four feed URLs, sniff for new endpoints" — the skill has nowhere to bind those asks. It auto-selects the most-recent transcript (likely unrelated to the target CLI), produces a noisy or empty finding list, and halts at the U1 transcript-confirmation checkpoint. This is exactly what happened during the 2026-05-16 Digg dogfood — the skill scanned five unrelated transcripts and reported back that none was a clean amend-from-dogfood signal.

The fix is one new phase ahead of the current Phase 1 plus one alternative capture branch. The rest of the skill (Phase 2 guards, Phase 3 tiered scope, Phase 4 plan+execute, Phase 5 scrub, Phase 6 PR draft, Phase 7 PR open) already operates on a typed finding list — it does not care whether the findings came from a transcript or a user prompt.

---

## Requirements (carried from origin + new)

Carried verbatim from `docs/brainstorms/2026-05-15-printing-press-amend-requirements.md`:

- R1-R5 (skill identity, naming, name resolution, auto-detect) — unchanged.
- R6-R10 (friction capture, classification, cross-reference, stale-binary check) — apply in dogfood mode; cross-reference + stale-binary check apply in both modes (R9, R10 promoted to mode-agnostic).
- R11-R13 (scope confirmation, deferred list) — unchanged, mode-agnostic.
- R14-R17 (plan + execute) — unchanged, mode-agnostic.
- R18-R21 (PII scrub) — unchanged, mode-agnostic.
- R22-R24 (PR draft) — unchanged.
- R25-R30 (PR open) — unchanged.
- R31-R33 (output artifacts) — unchanged.

New requirements introduced by this plan:

- R34. The skill accepts two input modes: **dogfood** (current) and **direct-input** (new). Mode is detected from the slash-command invocation context; the user is asked only when detection is ambiguous.
- R35. Direct-input mode parses user-supplied asks from the slash-command prompt into structured findings. Each ask maps to one finding with `kind: {rename, add-command, add-endpoint, add-feed, fix-bug, sniff}`, `evidence: <verbatim user phrasing>`, and an inferred classification (`bug` or `feature`).
- R36. Sniff is a first-class finding type. When the user explicitly asks to sniff (phrases like "sniff for new APIs", "find new endpoints"), the skill runs `printing-press browser-sniff` and/or `crowd-sniff` against the target CLI's source site (resolved from `.printing-press.json`) and merges each discovered candidate endpoint as an additional finding. Sniff is opt-in; the skill does not run it by default.
- R37. Mode does not leak past Phase 1. Phases 2-7 are mode-agnostic; their interfaces consume the typed finding list only.
- R38. The original dogfood-mode UX is preserved bit-for-bit. A user who invokes `/printing-press-amend` after a session with no direct asks in the prompt sees identical behavior to today.

---

## Key Technical Decisions

- **Mode detection is heuristic + askable, not flag-based.** No new CLI flag on the slash command. The agent reads the slash-command prompt and conversation context, decides dogfood vs direct-input, and asks only when ambiguous. Matches the family's UX: zero structured arguments on slash invocations, agent reads intent.
- **Direct-input asks are parsed in prose, not in a structured DSL.** The user types `/printing-press-amend rename X to Y, add feeds A B C, sniff for more` and the agent maps phrases to finding kinds via a small parsing rubric documented in `references/direct-input-parsing.md`. No JSON, no flag soup. Misclassification falls back to the U4 scope confirmation modal, which already exists.
- **Sniff is opt-in per run.** Running `browser-sniff` requires a Chrome session and noticeable wall time; running `crowd-sniff` hits external APIs and writes files under `~/printing-press/manuscripts/`. Surprising it onto users on every direct-input run is bad UX. Trigger only when the user's prompt explicitly names sniffing.
- **Both modes converge at Phase 2.** Phase 2's PR cross-reference check and stale-binary check apply equally — a duplicate PR is a duplicate regardless of how the finding was sourced; a stale local binary still means the user is about to amend the wrong code. The published-version check stays mode-agnostic.
- **Append-don't-rewrite for upstream docs.** The original brainstorm (`2026-05-15-printing-press-amend-requirements.md`) and original plan (`2026-05-15-feat-printing-press-amend-skill-plan.md`) stay as the dogfood-mode design record. This plan attaches as a follow-up artifact; the originals get a one-paragraph addendum pointing here, not a rewrite.
- **All work lands in PR #1490, not a new PR.** The skill is not merged yet; broadening its scope inside the unmerged PR keeps history coherent and avoids a second review cycle. The PR title and body update to reflect both modes.

---

## Scope Boundaries

**In scope:**
- Frontmatter expansion (description + trigger phrases) to advertise direct-input mode.
- New `Phase 0 — Input Mode Detection` section in `SKILL.md`.
- Restructured `Phase 1` to branch on mode: dogfood path (existing) vs direct-input path (new).
- New `references/direct-input-parsing.md` documenting the ask-to-finding parsing rubric.
- Sniff finding type integrated into the direct-input branch.
- Addendum on the original brainstorm and original plan pointing to this plan.
- PR #1490 title + body refresh.

**Outside this plan:**

### Deferred to Follow-Up Work
- A structured-input mode using YAML or JSON for non-Claude-Code harnesses. The direct-input parser works from prose; if a future user wants machine-driven amend (CI, cron), that's a separate plan.
- Sniff result caching beyond what `browser-sniff` / `crowd-sniff` already do. Cross-run sniff dedup is a separate sniff-layer improvement.
- Bumping `printing-press patch` (the AST-injecting Go subcommand, unrelated to this skill) to consume the new finding kinds. Different mechanism, different surface.

**True non-goals:**
- Auto-merging the resulting PR — always opens for human review (carried from origin Scope Boundaries).
- Running mid-session — still triggers at the end of a session or on explicit invocation (carried from origin).
- Generating net-new CLIs — that's `/printing-press`, not `/printing-press-amend` (carried from origin).

---

## System-Wide Impact

- **`SKILL.md`** is the primary surface. ~80 lines of additions, no deletions, no behavioral changes to existing phases.
- **`references/transcript-parsing.md`** gets a one-line scope note ("applies in dogfood mode only") but its body is untouched.
- **New file: `references/direct-input-parsing.md`** mirrors the structure of `transcript-parsing.md` for the new mode.
- **`printing-press browser-sniff` / `crowd-sniff`** are invoked from the skill in sniff-finding-type runs. No changes to those subcommands; they are called as-is. If a target CLI's `.printing-press.json` lacks `source_url`/`spec_url`, sniff degrades gracefully (the skill asks the user for a URL or skips the sniff finding).
- **`internal/cli/verify_internal_skill.go`** — verified to enforce only frontmatter shape + body-has-heading. Adding Phase 0 does not break the linter. No code change needed.
- **`internal/pipeline/contracts_test.go` (`TestSkillSetupBlocksMatchWorkspaceContract`)** — setup block in `SKILL.md` is untouched, so this test continues to pass. Verified by re-running `go test ./internal/pipeline/...` after each unit.
- **PR #1490** — title and body update to advertise both modes. Existing commits stay; new commits added on the same branch.

---

## Implementation Units

### U1. Frontmatter expansion + trigger phrases

**Goal:** Broaden `SKILL.md` frontmatter so the skill advertises direct-input mode and the trigger-phrase catalog reflects the new entry points.

**Requirements:** R34, R38.

**Dependencies:** none.

**Files:**
- `skills/printing-press-amend/SKILL.md` (frontmatter block, lines 1-25)

**Approach:**
- Description rewrites from "Mines the active Claude Code session transcript for friction..." to: "Amend a published CLI from either a dogfood session (mines the active Claude Code transcript for friction) OR direct user-supplied asks (rename a command, add commands or feeds, fix a named bug, optionally sniff the source site for new endpoints). Confirms scope with the user, plans + executes the fix autonomously, scrubs PII, and opens a PR against `mvanhorn/printing-press-library`. Two user-in-loop checkpoints: scope after capture, PR draft before open."
- Trigger phrases — keep existing ("amend the CLI", "submit a patch", "fix what I just dogfooded", "open a PR for this CLI", "patch this CLI"). Add: "add features to my CLI", "rename this command", "add these feeds to <cli>", "sniff for new APIs in <cli>", "amend with these ideas".
- `min-binary-version` unchanged at 4.0.0. `allowed-tools` unchanged.

**Patterns to follow:**
- `skills/printing-press-polish/SKILL.md` frontmatter for trigger-phrase shape.

**Test scenarios:**
- `printing-press verify-internal-skill --dir skills/printing-press-amend` exits 0.
- YAML frontmatter still parses (any YAML linter or `python -c "import yaml; yaml.safe_load(open(...))"`).
- `grep "^name: printing-press-amend"` in `SKILL.md` still matches.
- `Test expectation: none -- frontmatter-only edit; behavior tests live in U2-U4.`

**Verification:** `printing-press verify-internal-skill --dir skills/printing-press-amend` returns clean. New trigger phrases appear in `description`.

---

### U2. Phase 0 — Input Mode Detection

**Goal:** Insert a new `Phase 0 — Input Mode Detection` section between `## Setup` and the existing `## Phase 1 — Friction Capture`. Decide dogfood vs direct-input, persist the mode for later phases, ask only when ambiguous.

**Requirements:** R34, R37.

**Dependencies:** U1.

**Files:**
- `skills/printing-press-amend/SKILL.md` (insert new section after the `## Setup` block, before `## Phase 1 — Friction Capture`)

**Approach:**
- New section header: `## Phase 0 — Input Mode Detection`.
- Detection rubric (documented in the section, not in code):
  - **Direct-input mode** when the slash-command prompt contains a concrete CLI name AND at least one of: verb signals (`rename`, `add`, `remove`, `fix`, `sniff`), explicit URLs, an enumerated list of feeds/commands/endpoints, or "these ideas / these features".
  - **Dogfood mode** when the prompt is empty, or names a CLI without asks ("amend the superhuman CLI"), or explicitly references the session ("what I just dogfooded", "this session's friction").
  - **Ambiguous** when only one signal is present (CLI name with no verbs, or verbs with no target CLI). Ask the user via `AskUserQuestion`:
    > "Two ways to source findings for this amend. Which fits?
    >   1. Mine the current session transcript (dogfood mode)
    >   2. Use the asks I just typed (direct-input mode)
    >   3. Both — combine transcript friction with my asks"
  - Default when no slash-command prompt is present: dogfood mode (preserves the canonical UX).
- Output: emit a `MODE=<dogfood|direct|both>` marker for later phases. Persist in the run-state directory (`$PRESS_RUNSTATE/current/mode.txt`) for resumability.
- Document the "both" combined-mode escape hatch: when the user has friction in the session AND a few specific asks, run dogfood Phase 1 first, then append direct-input findings to the same finding list before Phase 2.

**Patterns to follow:**
- Detection-then-ask pattern matches `/printing-press-publish`'s Phase 1 push-vs-fork resolution (heuristic + askable, never silent).

**Test scenarios:**
- Direct-input detected: prompt = "rename X to Y in foo-pp-cli, add feeds A B C" → mode=direct, no transcript walk.
- Dogfood detected: prompt = empty → mode=dogfood, transcript walked.
- Ambiguous: prompt = "amend superhuman" → AskUserQuestion fires with 3-option modal.
- Both: prompt = "I dogfooded this session and also want to add feature X" → mode=both, transcript + asks merged.
- `printing-press verify-internal-skill --dir skills/printing-press-amend` exits 0 after the insertion.

**Verification:** `verify-internal-skill` clean; `Phase 0 — Input Mode Detection` heading present immediately after `## Setup`; detection rubric documents all four branches (direct, dogfood, ambiguous, both).

---

### U3. Direct-input capture branch (Phase 1-DI)

**Goal:** Restructure Phase 1 as mode-conditional. Existing transcript-mining body stays; add a peer branch for direct-input. Both branches emit the same typed finding list.

**Requirements:** R35, R37, R38.

**Dependencies:** U2.

**Files:**
- `skills/printing-press-amend/SKILL.md` (the `## Phase 1 — Friction Capture` section: split into two named sub-sections)
- `skills/printing-press-amend/references/transcript-parsing.md` (top-of-file scope note: "applies when MODE=dogfood")

**Approach:**
- Rename `## Phase 1 — Friction Capture` to `## Phase 1 — Capture`.
- Add two sub-sections:
  - `### 1a. Dogfood mode (MODE=dogfood)` — current Phase 1 body lifts here verbatim.
  - `### 1b. Direct-input mode (MODE=direct)` — new body, ~30 lines, points to `references/direct-input-parsing.md` for the parsing rubric.
- Direct-input flow:
  1. Read the slash-command prompt body and any agent-message context from the immediate invocation turn.
  2. Apply the parsing rubric (rename → kind: rename; "add command X" → kind: add-command; URL list → kind: add-feed or add-endpoint; "sniff for…" → kind: sniff; "fix X" → kind: fix-bug).
  3. For each ask, create a finding with `id: F<n>`, `kind`, `evidence` (verbatim user phrasing), `target_cli` (resolved from the CLI name in the prompt or Phase 0's name resolution), `classification` (kind:rename + add-* + sniff → feature; kind:fix-bug → bug).
  4. Skip the U1 transcript-path confirmation modal entirely in this mode — there is no transcript to confirm.
  5. Target-CLI resolution still runs (per origin R4): short name, full name, or path.
- "Both" mode (MODE=both): run 1a first, then 1b; append direct-input findings to the dogfood-derived list with non-colliding IDs.

**Patterns to follow:**
- Finding shape matches Phase 1 dogfood output (see `references/transcript-parsing.md` for the schema). Reuse identical field names so Phase 2-7 do not need to branch.

**Test scenarios:**
- Single rename ask: prompt = "rename Digg 1000 to Digg in digg-pp-cli" → 1 finding, kind=rename, classification=feature.
- Multi-feed add: prompt = "add https://digg.com/ai/github/stars, /new, /activity, /recent feeds to digg-pp-cli" → 4 findings, kind=add-feed, target_cli=digg-pp-cli.
- Mixed asks + sniff: prompt = "rename A to B, add command X, sniff for new endpoints" → 3 findings (rename, add-command, sniff).
- Bug ask: prompt = "fix the silent-null in `superhuman drafts list` in superhuman-pp-cli" → 1 finding, kind=fix-bug, classification=bug.
- Both mode: prompt = "amend foo from this session + add command bar" → dogfood findings F1-Fn + direct findings F(n+1), F(n+2)…
- Empty direct-input prompt: should not enter Phase 1b at all (Phase 0 routes to dogfood).

**Verification:** `verify-internal-skill` clean. `Phase 1 — Capture` has exactly two named sub-sections. Existing transcript-mining text is intact in 1a (diff `references/transcript-parsing.md` shows only the scope note added).

---

### U4. Sniff finding type — inline browser-sniff / crowd-sniff integration

**Goal:** When the user's asks include "sniff for new APIs" (or equivalent), run `printing-press browser-sniff` and/or `crowd-sniff` against the target CLI's source site and merge each discovered candidate endpoint as an additional finding.

**Requirements:** R36.

**Dependencies:** U3.

**Files:**
- `skills/printing-press-amend/SKILL.md` (extend `### 1b. Direct-input mode` with a "Sniff-finding subroutine" sub-section)
- `skills/printing-press-amend/references/direct-input-parsing.md` (sniff rubric details)

**Approach:**
- Triggered when the parsing rubric tags any ask as `kind: sniff`.
- Resolve the source site:
  - Read `~/printing-press-library/library/<category>/<slug>/.printing-press.json` for `source_url` or `spec_url`. (Phase 1 already resolves category; reuse.)
  - If missing, ask the user inline ("Sniff needs a target URL — paste one or skip the sniff finding?").
- Run `<PRINTING_PRESS_BIN> crowd-sniff --site <url> --json > /tmp/amend-sniff-crowd.json` first (no browser, fast).
- If the user invocation environment has Chrome MCP available AND the user opted in to deeper sniff, run `<PRINTING_PRESS_BIN> browser-sniff` (the existing browser-sniff CLI consumes a captured HAR; the skill does not orchestrate the capture itself in v0.2 — capture is user-driven or skipped).
- Convert each discovered candidate endpoint to one finding: `kind: add-endpoint`, `classification: feature`, `evidence: "discovered via crowd-sniff: <endpoint>"`, `provenance: sniff`.
- Tier these as Tier 3 (polish/architecture) by default; user can promote to Tier 2 at the Phase 3 scope-confirmation checkpoint.
- Degraded paths:
  - No `source_url` in `.printing-press.json` → ask user OR skip sniff finding with a logged note.
  - `crowd-sniff` exits non-zero → log, skip, do not abort the whole amend run.
  - Zero endpoints discovered → emit one "sniff ran, no new endpoints" entry to the deferred list, not the active findings.

**Patterns to follow:**
- `/printing-press` (the generate skill) calls `browser-sniff` and `crowd-sniff` in Phase 1.7 — mirror the invocation pattern (absolute-path binary, `--json`, graceful degradation on non-zero exit).

**Test scenarios:**
- Sniff ask + crowd-sniff finds 5 endpoints → 5 Tier-3 findings appended, all with `provenance: sniff`.
- Sniff ask + crowd-sniff finds 0 → no findings added, one entry in deferred list.
- Sniff ask + `.printing-press.json` lacks `source_url` → AskUserQuestion fires for URL OR skip path; sniff finding becomes deferred if user skips.
- Sniff ask + `crowd-sniff` non-zero exit → log + skip, amend run continues with other findings.
- No sniff ask → `crowd-sniff` is NOT invoked (verified by absence of `/tmp/amend-sniff-crowd.json`).

**Verification:** Sniff runs only when explicitly requested. Discovered endpoints appear as separate findings with `provenance: sniff`. No invocation of sniff binaries in dogfood-only runs.

---

### U5. New reference doc: `references/direct-input-parsing.md`

**Goal:** Document the prose-to-finding parsing rubric in detail, mirroring the structure of the existing `references/transcript-parsing.md` so the SKILL.md body stays compact.

**Requirements:** R35, R36.

**Dependencies:** U3, U4.

**Files:**
- `skills/printing-press-amend/references/direct-input-parsing.md` (new file)

**Approach:**
- Section structure mirrors `transcript-parsing.md`:
  1. **Input** — the slash-command prompt body, plus the agent-message turn that fired the skill (which often carries elaborations).
  2. **Parsing rubric — verbs to finding kinds:**
     - "rename X to Y" / "call it X instead of Y" → `kind: rename`, `classification: feature`.
     - "add command X" / "add subcommand X" → `kind: add-command`, `classification: feature`.
     - "add feed <url>" / "add these feeds: <url>, <url>" → one `kind: add-feed` finding per URL.
     - "add endpoint <url>" / explicit API path → `kind: add-endpoint`.
     - "fix X" / "X is broken" / "X returns null" → `kind: fix-bug`, `classification: bug`.
     - "sniff for new APIs" / "find new endpoints" / "discover more" → `kind: sniff`.
  3. **Finding shape** — same `id`, `kind`, `classification`, `evidence`, `target_cli`, `rationale` schema as dogfood findings. New optional `provenance` field (`user-ask` for direct, `transcript` for dogfood, `sniff` for sniff-derived).
  4. **Target-CLI resolution** — same rules as origin R4 (short name, full name, path). When the user names the CLI inside the prompt, extract via regex (`<slug>-pp-cli` or "the <slug> CLI"); if absent, fall back to Phase 0 auto-detection or ask.
  5. **Edge cases** — multi-CLI asks ("amend foo-pp-cli and bar-pp-cli") → split into two runs (out of scope for v0.2; ask user to pick one). Ambiguous verbs ("update X") → ask. URLs without context → ask whether they're feeds, endpoints, or sniff targets.

**Patterns to follow:**
- `references/transcript-parsing.md` for section ordering and tone.

**Test scenarios:**
- Doc renders cleanly in GitHub (no broken markdown).
- All five finding kinds (`rename`, `add-command`, `add-feed`, `add-endpoint`, `fix-bug`, `sniff`) have at least one example phrasing.
- Multi-CLI edge case is explicitly listed.
- `Test expectation: none -- reference doc only; behavior tests live in U3 and U4.`

**Verification:** File exists, ~80-120 lines, structure mirrors `transcript-parsing.md`. Referenced by `SKILL.md` Phase 1b.

---

### U6. Upstream doc addendum + PR #1490 refresh

**Goal:** Add a short v0.2 addendum to the original brainstorm and original plan pointing at this plan, and refresh PR #1490's title and body to advertise both modes.

**Requirements:** none (housekeeping for traceability).

**Dependencies:** U1-U5 (changes need to exist before the PR description references them).

**Files:**
- `docs/brainstorms/2026-05-15-printing-press-amend-requirements.md` (append a section)
- `docs/plans/2026-05-15-feat-printing-press-amend-skill-plan.md` (append a section)
- PR #1490 title + body (via `gh pr edit 1490`)

**Approach:**
- Brainstorm addendum (append at end, before `## Next Steps`):
  ```
  ## v0.2 Amendment — direct-input mode (2026-05-16)

  The original brainstorm above frames the skill as dogfood-only. The Digg-CLI
  amend on 2026-05-16 surfaced a gap: when the user already knows what they
  want changed, the dogfood-only framing fails. The skill is extended to a
  second input mode (direct-input) plus a sniff finding type. See
  `docs/plans/2026-05-16-001-feat-printing-press-amend-direct-input-mode-plan.md`
  for the design.
  ```
- Plan addendum: similar two-paragraph note pointing here.
- PR #1490 refresh:
  - Title: "feat(skills): add /printing-press-amend skill — dogfood + direct-input modes"
  - Body: append a "## v0.2 — direct-input mode" section describing the new mode in 3-5 bullets and linking the new plan + brainstorm addendum.

**Patterns to follow:**
- Origin-preserving addendum pattern (don't rewrite prior plans; append a v0.2 section) — matches `docs/brainstorms/2026-03-29-publish-skill-requirements.md` and its follow-up addenda.

**Test scenarios:**
- `gh pr view 1490 --json title,body` shows the new title and the new section.
- Addenda are appended, not interleaved (original sections unchanged).
- Links between docs resolve (relative-path links work in GitHub preview).
- `Test expectation: none -- doc updates; no behavior tests.`

**Verification:** PR #1490 title and body reflect both modes; brainstorm and plan addenda point at this plan; no original section content was deleted or rewritten.

---

## Open Questions / Deferred to Implementation

- **Q1.** Should "both" mode (dogfood + direct-input combined) ship in v0.2 or be deferred to v0.3? Plan currently ships it but only as a thin path (dogfood first, then append direct findings). If implementation reveals merge conflicts in the finding list (e.g., a transcript finding and a direct-input finding describe the same bug), defer to v0.3 and surface as a known limitation in U2.
- **Q2.** Should `browser-sniff` be auto-invoked when Chrome MCP is available, or always require explicit opt-in? Plan currently requires explicit opt-in. If user dogfood reveals this is friction, relax in v0.3.
- **Q3.** Where exactly does the slash-command prompt text live in the Claude Code session model? The agent currently reads it from the conversation context; if a stable transcript-path API exists, U2's detection could read it directly. Defer to implementation — the conversation-context read should work.
- **Q4.** Does `gh pr edit 1490 --title --body-file` preserve existing PR comments and review state? Verify before firing the edit during U6.

---

## Risks

- **R1. Verifier surprise.** If `internal/cli/verify_internal_skill.go` or a CI lint enforces canonical phase names elsewhere (e.g., requires `Phase 1 — Friction Capture` verbatim), the Phase 0 insertion and Phase 1 rename break. Verified manually that the current verifier only checks frontmatter + body-has-heading, but a downstream gate could exist. Mitigation: run `printing-press verify-internal-skill --dir skills/printing-press-amend` after U2 and after U3 before proceeding.
- **R2. Direct-input parsing brittleness.** Ambiguous user prompts could mis-classify a feature ask as a bug or vice versa. Mitigation: the U4 (Phase 3) scope-confirmation modal already shows tiered findings with rationale; the user catches and corrects there. Worst case is one extra confirmation cycle, not a wrong PR.
- **R3. Sniff failure modes.** `crowd-sniff` depends on external APIs (GitHub search, npm). Rate-limit failures or 5xx responses could halt an amend run. Mitigation: U4's degraded-path branches treat non-zero sniff exit as "skip the sniff finding, continue with other findings" — never abort the whole run.
- **R4. PR description drift.** Editing PR #1490 mid-flight could collide with reviewer comments referencing the old title. Mitigation: do U6's PR edit last, after U1-U5 commits are pushed. Existing comments on PR #1490 are non-blocking review notes, not approval gates.
- **R5. Dogfood-mode regression.** Any structural change to `Phase 1` risks subtly breaking dogfood-mode behavior. Mitigation: U3 lifts the existing Phase 1 body verbatim into `### 1a` — diff-only-additions; the dogfood text is preserved character-for-character.

---

## Verification Strategy

End-to-end verification is the 2026-05-16 Digg amend itself:

1. After U1-U5 land on `feat/printing-press-amend`, fire `/printing-press-amend` with the user's original Digg ask ("rename Digg 1000 to Digg, add the four github feeds, sniff for new").
2. Confirm Phase 0 detects direct-input mode (no transcript modal fires).
3. Confirm Phase 1b emits 5+ findings (1 rename, 4 add-feed, 1 sniff that expands further).
4. Confirm Phase 2 cross-references PRs without halting on duplicates.
5. Confirm Phase 3 tiering presents the findings to the user.
6. Confirm Phase 4-7 plan + execute + scrub + PR open without code changes to those phases.

Per-unit verification:
- After U1: `printing-press verify-internal-skill --dir skills/printing-press-amend` → exit 0.
- After U2: same verifier → exit 0. `grep "## Phase 0" skills/printing-press-amend/SKILL.md` → match.
- After U3: same verifier → exit 0. `grep "### 1a\\|### 1b" skills/printing-press-amend/SKILL.md` → two matches.
- After U4: same verifier → exit 0. Inspect SKILL.md for "Sniff-finding subroutine" sub-section.
- After U5: `cat skills/printing-press-amend/references/direct-input-parsing.md | wc -l` → 80-120 lines.
- After U6: `gh pr view 1490 --json title` → contains "dogfood + direct-input".
- Repo-wide: `go test ./internal/pipeline/...` → `TestSkillSetupBlocksMatchWorkspaceContract` passes (setup block untouched).
