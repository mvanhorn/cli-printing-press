---
title: "feat(skills): printing-press-amend — dogfood-to-PR skill for printed CLIs"
type: feat
status: active
created: 2026-05-15
origin: docs/brainstorms/2026-05-15-printing-press-amend-requirements.md
---

# feat(skills): printing-press-amend — dogfood-to-PR skill for printed CLIs

## Summary

Add a new `/printing-press-amend` skill to this repo (the machine) that wraps the dogfood-to-PR loop for printed CLIs in the public library. The skill mines the active Claude Code session transcript for friction, scopes the patch with the user, autonomously plans + executes the fix, scrubs PII, and opens a PR against `mvanhorn/printing-press-library` — reusing the family's setup contract, `printing-press publish validate` plumbing, and the public library's mandatory patch-tracking contract (`// PATCH(...)` source comments + `.printing-press-patches.json`). Two user-in-loop checkpoints (scope after capture, PR draft before open); everything else runs unattended.

---

## Problem Frame

When a user dogfoods a printed CLI in a Claude Code session, they hit friction — missing flags, hand-rolled API payloads, silent-null returns, expired-token confusion, missing folder coverage. Today the path from "found it" to "PR open" is manual: synthesize friction into tiers, hand to `/ce-plan`, hand the plan to `/ce-work`, drive the public-library fork-and-PR flow by hand, scrub PII before anything ships. Conventions vary across users, synthesis quality depends on memory, and PR-time gotchas (was this already shipped? does the fix need a `// PATCH(...)` record?) get missed.

The brainstorm document at `docs/brainstorms/2026-05-15-printing-press-amend-requirements.md` defines the WHAT — a self-contained skill that wraps the loop with two checkpoints. This plan defines the HOW: skill placement under `skills/printing-press-amend/`, phase ordering, references file shape, integration with existing publish plumbing, and the public library's patch-record contract.

---

## Requirements (carried from origin)

Origin requirements R1–R33 are carried forward in full (see origin: `docs/brainstorms/2026-05-15-printing-press-amend-requirements.md`). They map to implementation units below as follows:

- **R1–R5 (skill identity, name resolution, auto-detect)** → U1, U2
- **R6–R10 (friction capture, categorization, cross-reference, stale-binary)** → U2, U3
- **R11–R13 (scope confirmation checkpoint)** → U4
- **R14–R17 (plan + execute + validate)** → U5
- **R18–R21 (PII scrub)** → U6
- **R22–R24 (PR draft checkpoint)** → U7
- **R25–R30 (PR open, labels, issue ownership, RESULT block)** → U7, U8
- **R31–R33 (output artifacts)** → U5, U8

The success criteria from origin (Superhuman-shaped session ships PR in under 10 minutes, PR #571-shaped duplicates get caught, zero PII leaks, second user without compound-engineering can run end-to-end) drive the test scenarios per unit.

---

## Pre-Implementation Decisions (resolved 2026-05-15 from doc-review findings)

- **Skill name: `/printing-press-amend`** (not `/printing-press-patch`) — avoids collision with the existing `printing-press patch` binary subcommand for AST-injection. The artifact this skill produces is still semantically a "patch" (in the git/PR sense) but the slash-skill's identifier is `amend` for disambiguation.
- **U1 verifier: build a new `printing-press verify-internal-skill` Go subcommand** (new U9) — `printing-press verify-skill` requires `internal/cli/` source and is wrong-tool for internal skills. The new subcommand lints SKILL.md frontmatter, canonical sections, and allowed-tools shape; reusable across polish/retro/publish/amend.
- **Operate directly on the managed clone of `mvanhorn/printing-press-library`** — U5 edits files inside `$PRESS_HOME/.publish-repo-$PRESS_SCOPE/library/<category>/<slug>/` (the same managed clone `/printing-press-publish` uses). Skips `$PRESS_LIBRARY` entirely. No translation step, no divergence handling, no carry-forward of existing `.printing-press-patches.json`. U7's PR open is just `git push` against the managed clone.

---

## Key Technical Decisions

- **Copy publish plumbing into `references/library-pr-plumbing.md`, defer the publish-side extraction.** `/printing-press-publish` currently has zero extracted helpers — Step 5 (managed clone), Step 7 (collision detection), Step 8 (branch/commit/push/PR) all live inline in its SKILL.md. Refactoring publish to share helpers doubles this PR's scope. Copy the patterns into patch's references file now, document the duplication as a planned follow-up retro item. The two surfaces will drift if not maintained, but the alternative is blocking patch on a refactor.
- **Friction extraction lives in skill prose + agent judgment, not a new binary subcommand.** A `printing-press patch-friction-scan` Go subcommand would be testable but requires generator changes and golden fixtures; the friction-extraction logic is genuinely judgment-shaped (categorizing "the user typed `superhuman-pp-cli drafts new` and it returned 400" as "missing feature: drafts new helper" requires understanding intent, not pattern-matching). Prose + AskUserQuestion confirmation handles edge cases better. Revisit as a binary subcommand in v0.2 if v0.1 friction quality is shaky.
- **PII scrub reuses `skills/printing-press-retro/references/secret-scrubbing.md` patterns instead of duplicating.** Patch's `references/pii-scrubbing.md` will reference retro's module + add the patch-specific stop-list (companies, person names) and shape-preserving tokens. Keeps the scrubbing logic in one place; both skills evolve together.
- **Bake the public library's mandatory contract directly into U7.** Per `~/printing-press-library/AGENTS.md`: every patched site needs a `// PATCH(<short reason>)` source comment AND an entry in the CLI's `.printing-press-patches.json`. These are NOT optional and the library's `verify-library-conventions` workflow will reject PRs without them. U7 enforces both before opening the PR.
- **Use `printing-press publish validate --dir <cli-dir> --json` as the consolidated build/test command.** It already runs manifest, phase5, govulncheck (scoped), go vet, build, --help, --version. U5 calls this instead of stringing together separate `go build`/`go test`/`go vet` invocations.
- **One PR per `/printing-press-amend` run.** Even when bugs and features both surface in a session. Confirmed in brainstorm; bake into U4's scope-tier menu (user picks one tier per run, can re-run the skill for additional scope).
- **`context: fork` skill, `user-invocable: true`.** Self-contained per R3, slash-invocable per R2. Same posture as `/printing-press-polish`.

---

## Scope Boundaries

- Limited to printed CLIs in `mvanhorn/printing-press-library` — no machine-repo patches (those go to `/printing-press-retro`), no third-party CLIs, no other public-library forks
- End-of-session trigger via reading the active transcript file — no live mid-session capture, no background observation daemon
- Does not generate net-new CLIs (`/printing-press` + `/printing-press-publish`)
- Does not improve a pre-publish CLI's quality (`/printing-press-polish`)
- Does not reflect on the machine itself or file machine-improvement issues (`/printing-press-retro`)
- Does not auto-merge — always opens a PR for human review

### Deferred to Follow-Up Work

- Extract `/printing-press-publish` Step 5/7/8 into shared scripts/helpers and migrate both publish + patch to use them (resolves the copy-paste duplication from Key Technical Decisions)
- `printing-press patch-friction-scan` binary subcommand for deterministic, golden-tested friction extraction
- Multi-PR mode (split bug-PR + feature-PR for atomic landability)
- Cross-CLI patches (one session that touched multiple CLIs)
- Live mid-session friction capture (skill listens to session events as they happen)
- Greptile pre-check integration (auto-fix Greptile P0/P1 findings before PR opens)

---

## System-Wide Impact

- **`internal/pipeline/contracts_test.go`** — `TestSkillSetupBlocksMatchWorkspaceContract` test slice gains `printing-press-amend/SKILL.md`. Without this addition, setup contract drift would land silently.
- **`mvanhorn/printing-press-library`** — Every patch PR adds entries to a CLI's `.printing-press-patches.json` and adds `// PATCH(...)` comments to source. The `verify-library-conventions` workflow on that repo gates patches; the skill must produce conformant PRs.
- **`/printing-press-publish`** — No source change in this PR, but the skill becomes a load-bearing reference for `references/library-pr-plumbing.md`. Future publish-side refactors must coordinate with patch.
- **`/printing-press-retro`** — `references/secret-scrubbing.md` becomes shared infrastructure. Changes there now affect patch behavior.
- **README + skill index** — Add `printing-press-amend` to `docs/SKILLS.md` skill catalog and the README skills section.

---

## Implementation Units

### U1. Skill scaffold + setup contract + frontmatter

**Goal:** Create the skill directory, write a working SKILL.md skeleton with the canonical frontmatter and setup contract, register it for contract testing.

**Requirements:** R1, R2, R3, R10 (version check is part of setup)

**Dependencies:** None

**Files:**
- `skills/printing-press-amend/SKILL.md` (new)
- `skills/printing-press-amend/references/.gitkeep` (new — placeholder for U2/U6/U7 references)
- `internal/pipeline/contracts_test.go` (modify — add `printing-press-amend/SKILL.md` to test slice)
- `internal/pipeline/contracts_test.go` (test file already exists; verify the new entry passes)

**Approach:**
- Frontmatter: `name: printing-press-amend`, `description: |` with multi-line trigger phrases ("patch the CLI", "submit a patch", "fix what I just dogfooded", etc.), `allowed-tools: [Bash, Read, Write, Edit, Glob, Grep, AskUserQuestion]`, `version: 0.1.0`, `min-binary-version: "4.0.0"`, `context: fork`, `user-invocable: true`
- Copy the `<!-- PRESS_SETUP_CONTRACT_START -->` ... `<!-- PRESS_SETUP_CONTRACT_END -->` block verbatim from `skills/printing-press-publish/SKILL.md` lines 40–92 (the canonical reference)
- After the contract, add the version-check sequence (parse `<PRINTING_PRESS_BIN> version --json`, compare to `min-binary-version`, abort with the canonical "Run go install … @latest" message if older) — pattern from publish lines 94–96
- SKILL.md body skeleton: introduction (machine-vs-printed-CLI framing per AGENTS.md, "the Printing Press" canonical name), then placeholder phase headers (Phase 1 Friction Capture, Phase 2 Cross-Reference + Stale-Binary, Phase 3 Scope Confirmation, Phase 4 Plan + Execute, Phase 5 PII Scrub, Phase 6 PR Draft Review, Phase 7 PR Open, Phase 8 Output) — each filled in by subsequent units

**Patterns to follow:**
- `skills/printing-press-publish/SKILL.md` lines 1–96 (frontmatter + setup contract + version check)
- `skills/printing-press-polish/SKILL.md` (overall body shape, phase organization)

**Test scenarios:**
- `TestSkillSetupBlocksMatchWorkspaceContract` passes after adding `printing-press-amend/SKILL.md` to the test slice — confirms contract parity with the other 4 skills already in the test
- Negative: introduce a deliberate setup-contract drift in the new SKILL.md (drop `pwd -P`), confirm the test fails with a clear diff
- `printing-press verify-skill --dir skills/printing-press-amend --json` returns no errors against the skeleton (no flag-name drift, no canonical-section drift)
- Frontmatter parses: `python3 -c 'import yaml; yaml.safe_load(open("skills/printing-press-amend/SKILL.md").read().split("---")[1])'` succeeds and has all required fields

**Verification:** `go test ./internal/pipeline/...` passes with new entry; `printing-press verify-skill` clean against the skeleton; `printing-press` binary recognizes the new skill in its catalog if the catalog auto-discovers (otherwise document any catalog-update step).

---

### U2. Friction capture phase (transcript parsing, signal extraction, categorization)

**Goal:** Read the active Claude Code session transcript, extract friction signals tied to a specific printed CLI invocation, categorize each as bug or feature with a one-line rationale.

**Requirements:** R5, R6, R7, R8

**Dependencies:** U1

**Files:**
- `skills/printing-press-amend/SKILL.md` (modify — Phase 1 body)
- `skills/printing-press-amend/references/transcript-parsing.md` (new)

**Approach:**
- Inline in SKILL.md Phase 1: brief instructions to read the active transcript, sample-extract logic at the prose-judgment level (this is judgment work per Key Technical Decisions, not a binary subcommand)
- `references/transcript-parsing.md` contains: canonical paths to look at (`~/.claude/projects/<dir>/<session>.jsonl` and platform variants documented in deferred questions), signal extraction taxonomy (non-zero exit codes, error messages, hand-rolled API payloads, retry-after-failure, "X doesn't exist" agent commentary, missing-flag references, silent-null returns), bug-vs-feature classification rubric with examples drawn from the 2026-05-15 Superhuman session
- Auto-detect target CLI: scan extracted signals for `<cli-name>-pp-cli` invocations, count by frequency, propose the most-touched CLI as default; if `<cli-name-or-path>` arg is provided, use that and skip auto-detect
- Confirm CLI selection with `AskUserQuestion` before continuing

**Patterns to follow:**
- `skills/printing-press-retro/references/` directory structure (retro is the closest sibling for "extract and synthesize")
- `skills/printing-press/references/setup-checks.md` (judgment-shape prose)

**Test scenarios:**
- Happy path: a transcript file with 12 distinct frictions against `superhuman-pp-cli` parses into a finding list with at least 8 categorized items (mirrors the 2026-05-15 dogfood run)
- Auto-detect: a transcript that touched both `superhuman-pp-cli` and `granola-pp-cli` proposes the more-frequent one and asks the user to confirm
- Edge case: empty or unreadable transcript — skill emits a clear "no active session transcript found, pass `<cli-name>` explicitly" message and exits cleanly
- Edge case: transcript with zero CLI invocations — skill reports "no `<cli>-pp-cli` invocations found in the active session" and exits
- Bug vs feature: a session where `superhuman-pp-cli drafts new` returned 400 categorizes as bug; a session where the user hand-rolled a `userdata.writeMessage` payload because `drafts new` doesn't exist categorizes as feature with rationale "no `drafts new` command in CLI"
- Path resolution: `<cli-name>` short form (`superhuman`), `<full-name>` (`superhuman-pp-cli`), and absolute path (`~/printing-press/library/superhuman`) all resolve to the same target directory (per R4)

**Verification:** Running the skill against a captured-fixture transcript file (saved under `testdata/skill-fixtures/patch-superhuman-2026-05-15.jsonl` if needed) produces the expected finding list; the user-confirmation prompt fires before any later phase.

---

### U3. Pre-checkpoint guards (PR cross-reference, stale-binary check)

**Goal:** Before showing the user the scope menu, suppress findings that would re-propose work already shipped in a recent PR, and abort if the running CLI binary is stale relative to the latest published version.

**Requirements:** R9, R10 (R10 covered partially in U1; this unit handles the printed-CLI binary check, distinct from `printing-press` binary itself)

**Dependencies:** U2

**Files:**
- `skills/printing-press-amend/SKILL.md` (modify — Phase 2 body)

**Approach:**
- Inline in SKILL.md Phase 2: for each finding from U2, search `mvanhorn/printing-press-library` open + recently-merged PRs (last 90 days) using `gh pr list --repo mvanhorn/printing-press-library --search "<keyword>" --state all --limit 20 --json number,title,state,mergedAt,headRefName`. Match by file path and keyword extracted from the finding rationale.
- For each match, present to the user inline ("Finding X may be addressed by PR #Y — `<title>`. Skip?"); user confirms skip or keeps.
- Stale-binary: query the printed CLI's published version (read from `~/printing-press-library/library/<category>/<api-slug>/.printing-press.json` if locally cloned, else `gh api repos/mvanhorn/printing-press-library/contents/library/<category>/<api-slug>/.printing-press.json`). Compare to local `<cli>-pp-cli version --json` output. If local is older, instruct user to `go install @latest` and abort the patch run cleanly.

**Patterns to follow:**
- `skills/printing-press-publish/SKILL.md` Step 7 collision detection (lines 488–650) — same `gh pr list` shape
- `skills/printing-press-polish/SKILL.md` divergence-check pattern

**Test scenarios:**
- Happy path: zero open PRs match the findings, all findings pass through to U4
- Match path: a finding "missing `--type sent` flag" matches an existing open PR titled "feat(superhuman): add sent folder support" — finding gets surfaced as a possible-duplicate, user can skip or proceed
- Recently-merged: a finding matches a PR merged 30 days ago — flagged as "already shipped, skip recommended" with a higher confidence than open-PR matches
- Stale binary: local `superhuman-pp-cli` reports v0.3.1, library `.printing-press.json` shows v0.4.0 — skill prints upgrade instructions and aborts before reaching U4
- Network failure: `gh` API call times out — skill prints a warning, asks the user whether to proceed without cross-reference (no silent failure)
- Edge case: printed CLI not yet in the public library (local-only) — stale-binary check skipped with a note, cross-reference returns no matches by definition

**Verification:** Running against the 2026-05-15 fixture would catch a `granola-pp-cli` auto-refresh-style proposal that PR #571 already shipped (the canonical failure-mode-from-the-brainstorm test).

---

### U4. Scope confirmation checkpoint + deferred-findings list

**Goal:** Present the surviving findings (post-U3 filtering) as a tiered list, let the user pick scope via `AskUserQuestion`, persist excluded findings as a deferred list for future patches.

**Requirements:** R11, R12, R13

**Dependencies:** U3

**Files:**
- `skills/printing-press-amend/SKILL.md` (modify — Phase 3 body)

**Approach:**
- Tier the findings: Tier 1 = bugs (CLI returns wrong result, errors, broken behavior), Tier 2 = missing features that solve immediate session pain, Tier 3 = polish/architecture/UX improvements
- Present a 4-option `AskUserQuestion`: (a) bugs only (Tier 1), (b) bugs + immediate features (Tier 1 + Tier 2), (c) all tiers (Tier 1 + 2 + 3), (d) custom selection — drop into a multi-select for individual findings
- Persist the user-excluded findings to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-deferred.md` so a future `/printing-press-amend` run can re-surface them
- Display chosen scope back to user as a confirmed list before proceeding to U5

**Patterns to follow:**
- `skills/printing-press-polish/SKILL.md` post-fix publish-offer prompt (similar 4-option AskUserQuestion shape)
- `skills/printing-press-retro/SKILL.md` triage gate (Phase 2.5)

**Test scenarios:**
- Happy path: 12 findings split 6/4/2 across tiers; user picks "bugs + immediate features", 10 findings move forward, 2 Tier-3 items go to deferred list with full rationale preserved
- Custom selection: user opens the multi-select and unchecks 3 specific findings; only checked findings move forward, unchecked items go to deferred
- Empty after filtering: U3 suppressed every finding (all duplicates); skill reports "no novel patches found this session" and exits cleanly without an empty PR
- Deferred list re-surfacing: a second `/printing-press-amend` run on the same CLI reads the prior deferred list (if present) and offers to include any items still relevant
- Edge case: user picks "abort" via Other — skill exits cleanly, no deferred list written, no PR opened
- AskUserQuestion options must be self-contained per AGENTS.md (no description-only meaning)

**Verification:** Selected scope reaches U5 as a structured list; deferred list file exists at the expected path with each excluded finding's category, rationale, and timestamp.

---

### U5. Plan emission + autonomous execute + validate + retry

**Goal:** Generate a per-run plan document for the confirmed scope, execute the fixes against the local printed CLI checkout, run the consolidated validator, retry on failure up to 3 times, surface persistent failures to the user.

**Requirements:** R14, R15, R16, R17, R31, R32

**Dependencies:** U4

**Files:**
- `skills/printing-press-amend/SKILL.md` (modify — Phase 4 body)

**Approach:**
- Plan emission: write a markdown plan to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>.md` with the confirmed findings as work items, target file paths in `$PRESS_LIBRARY/<api-slug>/`, expected behavior changes, and test scenarios per item. Mirror to `/tmp/printing-press/patch/` for quick reference. Plan format follows the family's per-run-plan shape (not the full ce-plan template — this is a run-log, not a durable design plan)
- Execute: walk the plan items in dependency order; for each, edit the target files, update `.printing-press-patches.json` with the patch entry (per `~/printing-press-library/AGENTS.md`), and add `// PATCH(<short reason>)` source comments at the changed sites
- Validate: after the full set of edits, run `<PRINTING_PRESS_BIN> publish validate --dir "$PRESS_LIBRARY/<api-slug>" --json`. This single call covers manifest, phase5, govulncheck, go vet, build, --help, --version per the publish skill convention.
- Retry: if validate returns errors, parse the error categories, attempt targeted fixes (up to 3 iterations total). If still failing after iteration 3, surface the final error log to the user, save the in-progress plan + diff to a holding location, and pause without proceeding to U6/U7
- Per AGENTS.md machine-vs-printed-CLI rule: changes that should generalize across all CLIs go to a deferred retro item, changes that are CLI-specific stay in the patch. Surface this judgment for borderline cases.

**Patterns to follow:**
- `skills/printing-press-polish/SKILL.md` Phase 2 fix loop (lines 238–247 for the validate invocation, lines 280+ for the retry pattern)
- `~/printing-press-library/AGENTS.md` "How to record a hand-edit" section for the `// PATCH(...)` + `.printing-press-patches.json` contract

**Test scenarios:**
- Happy path: 5 confirmed findings, all 5 fixes pass `printing-press publish validate --json` on first attempt; plan + diff saved; ready for U6
- Retry success: first validate returns 2 verify failures; second iteration fixes them; third iteration not needed
- Retry exhaustion: validate keeps failing after 3 iterations; skill saves plan + diff to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-INCOMPLETE.md`, displays final error log, asks user to inspect (no automatic PR open)
- Patch records: `.printing-press-patches.json` gets new entries with `date`, `summary`, `files`, `findings_addressed` keys; `// PATCH(...)` comments appear at exactly the changed sites (verify by `grep "// PATCH" $PRESS_LIBRARY/<api-slug>/`)
- Machine-vs-CLI judgment: a finding that requires a generator template change is flagged as "machine-level — defer to retro" and excluded from the patch with a note in the deferred list
- Build failure: `go build` fails with a syntax error from the agent's edit; the validate-iteration retry catches and recovers
- Test failure: `go test` fails because the agent didn't update an existing test for the changed behavior; retry iteration adds the missing test or asks user

**Verification:** `printing-press publish validate --dir "$PRESS_LIBRARY/<api-slug>" --json` returns clean status; `.printing-press-patches.json` is well-formed; `grep -c "// PATCH" $PRESS_LIBRARY/<api-slug>` returns ≥ 1; plan doc exists at the expected path with all confirmed findings represented.

---

### U6. PII scrub phase (reuse retro patterns + shape-preserving tokens)

**Goal:** Scrub PII from all artifacts that will leave the local machine (plan doc body, PR title and body, any test fixtures added to the CLI). Replace with shape-preserving tokens; never silently delete.

**Requirements:** R18, R19, R20, R21

**Dependencies:** U5

**Files:**
- `skills/printing-press-amend/SKILL.md` (modify — Phase 5 body)
- `skills/printing-press-amend/references/pii-scrubbing.md` (new)

**Approach:**
- `references/pii-scrubbing.md` references `skills/printing-press-retro/references/secret-scrubbing.md` for the regex baseline (emails, API keys, tokens), then layers patch-specific patterns: company names from a stop-list at `~/.printing-press/amend-config.yaml` (user-maintained), person names matched against the same config, real meeting/email content quoted from the session transcript
- Scrub targets in priority order: (1) the per-run plan doc, (2) the PR title + body draft, (3) any test fixtures or example outputs newly added to the CLI source (defense-in-depth — Go source is presumed PII-free, but check anyway)
- Replacement tokens are shape-preserving: `<email-1>`, `<email-2>`, `<person-1>`, `<company-1>` — same token across appearances of the same source value within one run, distinct tokens for distinct sources. Never silently delete; always replace.
- After scrubbing, emit a summary to the user: "X PII tokens replaced across Y artifacts" — and store the scrub report in the run's plan doc footer
- Pre-PII-scrub backup: before scrubbing, copy each target to `<path>.pre-pii-scrub` (mirrors polish's pre-fix backup convention) so the user can audit what was changed
- Defense-in-depth check on Go source: if any PII pattern matches a `.go` file in `$PRESS_LIBRARY/<api-slug>/`, treat as a bug, surface to user, do not auto-modify Go source — pause for manual review

**Patterns to follow:**
- `skills/printing-press-retro/references/secret-scrubbing.md` (regex baseline + token-replacement pattern)
- `skills/printing-press/references/secret-protection.md` (defense-in-depth posture)
- `skills/printing-press-polish/SKILL.md` `.pre-pii-scrub/` style pre-mutation backup

**Test scenarios:**
- Happy path: plan doc references "Esper Labs" and "Randy Wells" from the session; after scrub, plan reads `<company-1>` and `<person-1>`; pre-PII-scrub backup exists; report says "2 tokens replaced across 1 artifact"
- Multiple instances: "Esper Labs" appears 5 times in one artifact and 3 times in another — all 8 instances become `<company-1>` (same token), one company entry in the report
- Multiple distinct values: "Esper Labs" and "Nothing" both appear — become `<company-1>` and `<company-2>` respectively; report shows 2 distinct values
- Email shape: `mvh@esperlabs.ai` becomes `<email-1>`; `randy@zoox.com` becomes `<email-2>`
- Stop-list miss: a company name not in the config doesn't get scrubbed — surface to user as "did you mean to scrub `<X>`? add to stop-list?" prompt
- Defense-in-depth hit: PII matches Go source — skill pauses, surfaces the file + line, does not auto-modify, requires user resolution before proceeding to U7
- Empty PII case: artifacts contain no PII — skill skips scrub silently, advances to U7
- Stop-list missing: `~/.printing-press/amend-config.yaml` doesn't exist — skill creates a default with a starter list and a comment explaining the format

**Verification:** Diff between pre-scrub and post-scrub artifacts shows only token substitutions; no Go source under `$PRESS_LIBRARY/<api-slug>/` was modified by this phase; scrub report is appended to the plan doc footer.

---

### U7. PR draft checkpoint + library PR open + patch contract enforcement

**Goal:** Show the user a complete PR-draft preview before any `gh` command fires; on confirmation, drive the fork-clone-branch-commit-push-PR flow; enforce the public library's `// PATCH(...)` + `.printing-press-patches.json` contract; emit the structured `---PATCH-RESULT---` block.

**Requirements:** R22, R23, R24, R25, R26, R27, R28, R29, R30

**Dependencies:** U6

**Files:**
- `skills/printing-press-amend/SKILL.md` (modify — Phase 6 + Phase 7 body)
- `skills/printing-press-amend/references/library-pr-plumbing.md` (new — copy from publish Step 5/7/8 patterns)

**Approach:**
- **PR draft preview (Phase 6):** assemble the PR title (`fix(<api-slug>): <summary>` for bugs, `feat(<api-slug>): <summary>` for features; first-tier wins when mixed), body (Summary / Findings table / Changes / Verification / Evidence sections per R27), file diff summary (output of `git diff --stat`), labels (`comp:<api-slug>`, `priority:P<n>`), and the structured `---PATCH-RESULT---` block. Display all of this inline before any `gh` call.
- **Review checkpoint (still Phase 6):** `AskUserQuestion` with 4 options: open PR as drafted / edit then open / hold (save plan + diff for later resume) / abort. Highlight any unscrubbed-PII risk findings (e.g., "the plan doc references `<company-name-not-in-stoplist>` — confirm intentional or scrub before continuing")
- **Open phase (Phase 7):** if user picks open or edit-then-open:
  - Issue ownership: `gh issue list --repo mvanhorn/printing-press-library --search "<api-slug> <keyword>" --state open` — if a matching issue exists, link in the PR body and assign self; if none, open one with the captured findings before the PR
  - Fork/clone: per `references/library-pr-plumbing.md` (copied from publish Step 5) — managed clone at `$PRESS_HOME/.publish-repo-$PRESS_SCOPE`, push-vs-fork detection, SSH-vs-HTTPS detection
  - Branch: `amend/<api-slug>-<short-summary>` (timestamped if branch exists); commit message follows family convention with `fix(<api-slug>):` or `feat(<api-slug>):` prefix
  - Push + PR create: `gh pr create --head <user>:branch --base main --body-file <tmpfile>`. Capture `HEAD_SHA` for durable evidence URLs
  - Apply labels: `gh pr edit <pr-number> --add-label "comp:<api-slug>" --add-label "priority:P<n>"`
- **Output (Phase 8 emission):** `---PATCH-RESULT---` ... `---END-PATCH-RESULT---` block with `pr_url`, `pr_number`, `branch_name`, `api_slug`, `scope_tier`, `files_changed: []`, `build_status`, `test_status`, `dogfood_status`, `pii_scrub_summary`, `findings_addressed: []`, `findings_deferred: []`, `deferred_list_path`, `plan_doc_path`. Format mirrors `---POLISH-RESULT---` shape.
- **Greptile awareness:** mention in SKILL.md that PR will receive a Greptile auto-review. Don't auto-fix, don't poll — surface as a final tip in the user-facing output.

**Patterns to follow:**
- `skills/printing-press-publish/SKILL.md` Step 5 (managed clone, lines 244–397), Step 7 (collision detection, lines 488–650), Step 8 (branch/commit/push/PR, lines 671–925)
- `skills/printing-press-polish/SKILL.md` lines 626–648 (`---POLISH-RESULT---` block shape)
- `~/printing-press-library/AGENTS.md` (PR title/label/issue conventions; `// PATCH(...)` + `.printing-press-patches.json` contract)

**Test scenarios:**
- Happy path: draft preview shows complete PR; user picks "open as drafted"; fork detected (no push access), managed clone created at `$PRESS_HOME/.publish-repo-$PRESS_SCOPE`, branch `amend/superhuman-folder-coverage` pushed, PR opens against `mvanhorn/printing-press-library`, labels applied, `---PATCH-RESULT---` block emitted with the PR URL
- Edit then open: user picks edit option, modifies the PR body inline, confirms, PR opens with the edited body
- Hold: user picks hold; plan + diff saved to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-HELD.md`; no PR opened; `---PATCH-RESULT---` emits with `status: held` and the resume path
- Abort: user picks abort; nothing saved beyond the plan doc from U5 (which exists with `status: aborted`); no PR; clear exit message
- Issue ownership (existing): a matching issue is found; PR body links it and self is assigned in the comment
- Issue ownership (none): no matching issue; skill opens one first, then the PR, linking them
- Branch collision: a branch named `amend/superhuman-folder-coverage` exists from a prior run; skill timestamps the new branch (`amend/superhuman-folder-coverage-2026-05-15-1422`) per publish Step 7
- Push access vs fork: when push access exists, skill pushes directly; when not, fork-detection routes through user's fork
- Label application failure: `gh pr edit` fails (e.g., label doesn't exist on repo) — skill warns but doesn't roll back the PR
- Unscrubbed PII risk surfaced in checkpoint: U6 reported a defense-in-depth hit — checkpoint surfaces this prominently; user must resolve before proceeding
- Patch contract verified: post-PR open, the PR diff includes `.printing-press-patches.json` updates AND `// PATCH(...)` comments at every changed code site (catches the failure mode where one is added and the other isn't)
- Greptile mention: final user output includes "Greptile will auto-review your PR; check `gh api repos/mvanhorn/printing-press-library/pulls/<N>/comments` for inline findings"

**Verification:** PR is open at a real URL; labels are applied; `---PATCH-RESULT---` block parses cleanly; the public library's `verify-library-conventions` workflow (next CI run after the PR opens) passes for the patch.

---

### U8. README + skill index update

**Goal:** Make the new skill discoverable via the repo's documentation surfaces.

**Requirements:** Implicit — skills must be listed in the catalog to be found; this is convention-following work, not new feature behavior.

**Dependencies:** U1–U7 (skill must be fully implemented before being indexed; the catalog one-liner draws from the frontmatter description but the README cross-reference and the SKILLS.md entry should describe the delivered skill, not the skeleton)

**Files:**
- `docs/SKILLS.md` (modify — add `printing-press-amend` row to the skill table)
- `README.md` (modify — add to skill list section if present)

**Approach:**
- Add a one-liner row to the `docs/SKILLS.md` skill catalog: name, slash-command, one-sentence summary, link to SKILL.md
- Mirror in README.md if there's a skills section
- Frontmatter description (from U1) is the source of truth for the summary; copy it down

**Patterns to follow:**
- Existing rows in `docs/SKILLS.md` for printing-press-polish, printing-press-retro, printing-press-publish

**Test scenarios:**
- Test expectation: none -- pure documentation; verified by visual inspection during PR review

**Verification:** `docs/SKILLS.md` lists the new skill; README.md (if applicable) lists it; markdown links resolve.

---

## Open Questions / Deferred to Implementation

- **Active session transcript path** (origin: question after R6) — confirm canonical path during implementation by inspecting `~/.claude/projects/` against the running session. May need platform-conditional logic for non-macOS.
- **Friction signal extraction precision** (origin: question after R7) — start with prose pattern guidance + agent judgment; revisit precision after the first 3 real-session runs.
- **PR cross-reference search syntax** (origin: question after R9) — implement keyword + file-path search initially; tune fuzziness based on false-positive rate.
- **Stale binary version source** (origin: question after R10) — use `.printing-press.json` from the public library as the authoritative version; document fallback for local-only CLIs.
- **PII detection precision** (origin: question after R18) — stop-list + regex baseline for v1 per Key Technical Decisions; named-entity recognition deferred to v0.2.
- **Greptile pre-check integration** — origin doc didn't include this; library AGENTS.md confirms Greptile fires on every PR. v0.1 just mentions it in user output; auto-fix integration deferred to follow-up.
- **`amend-config.yaml` schema** — define during U6 implementation; a starter shape is `{ stoplist: { companies: [...], people: [...] } }` but the exact schema can iterate.

---

## Risks

- **Drift between patch's `references/library-pr-plumbing.md` and publish's inline Step 5/7/8.** Mitigation: deferred-to-follow-up retro item explicitly tracks the extraction. Until then, any change to publish must coordinate with patch.
- **PII stop-list incompleteness.** A user maintaining `~/.printing-press/amend-config.yaml` may miss new entities. Mitigation: defense-in-depth check on Go source + interactive prompt for unrecognized capitalized strings during U6. Real test in production use.
- **Greptile reviews patch PRs too.** Greptile may flag patches as bugs. Mitigation: surface the Greptile follow-up step to the user; don't try to pre-empt. v0.2 could auto-fix P0/P1.
- **Public library AGENTS.md changes.** The `// PATCH(...)` + `.printing-press-patches.json` contract is enforced by `verify-library-conventions`. If that contract evolves, patch's U7 logic must follow. Mitigation: U7 reads patch-record format from a single source (the library AGENTS.md reference); update the reference, not scattered logic.
- **Auto-detect picks the wrong CLI.** Long sessions may touch many CLIs. Mitigation: U2's confirmation step + explicit-arg override. Don't auto-proceed without user confirm.
- **`printing-press publish validate` is the single point of validation.** If it has gaps, patch ships unvalidated changes. Mitigation: rely on the same trust model as `/printing-press-publish`; if validate gains gaps, both skills benefit from fixing it.

---

## Verification Strategy

The skill ships when:

- All 7 implementation units pass their per-unit test scenarios
- `TestSkillSetupBlocksMatchWorkspaceContract` passes with the new entry (U1)
- `printing-press verify-skill --dir skills/printing-press-amend` is clean (U1)
- A real-session smoke test against a captured fixture (the 2026-05-15 Superhuman session is the canonical case) produces a valid PR draft against a test fork of the public library, with PII scrubbed and patch records present
- The PR diff written by the smoke test passes `verify-library-conventions` workflow on the public library
- `docs/SKILLS.md` and README list the new skill (U8)

---

## v0.2 Amendment — direct-input mode (2026-05-16)

This plan documents the v0.1 design (dogfood-only). The 2026-05-16 Digg-CLI amend revealed that dogfood-only is too narrow: when the user already knows the changes they want, the transcript-mining path fails to capture them.

A v0.2 amendment adds a second input mode (`direct-input`) plus a first-class sniff finding type. Both modes converge at Phase 2 onward. The v0.1 dogfood behavior is preserved bit-for-bit; the expansion is additive.

See `docs/plans/2026-05-16-002-feat-printing-press-amend-direct-input-mode-plan.md` for the v0.2 design, new requirements R34-R38, and the implementation units (U1-U6) that land alongside the v0.1 commits on the same PR (#1490).
