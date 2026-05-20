---
date: 2026-05-15
topic: printing-press-amend-skill
---

# printing-press-amend: Dogfood-to-PR Loop for Printed CLIs

## Problem Frame

When a user dogfoods a printed CLI in a Claude Code session, they hit friction — missing flags, hand-rolled API payloads, silent-null returns, expired-token confusion, missing folder coverage. Today the path from "found it" to "PR open" is manual and stitched together: synthesize the friction into tiers, hand it to `/ce-plan`, hand the plan to `/ce-work`, drive the public-library fork-and-PR flow by hand, scrub PII before anything leaves local. Each step is real work, the conventions vary across users, and the synthesis quality depends on the user remembering everything they hit.

That manual path should be a single skill. The user fires `/printing-press-amend` after a dogfood session, the skill mines the active Claude Code transcript for friction, scopes the patch with the user, plans + executes the fix autonomously, and opens a PII-scrubbed PR against `mvanhorn/printing-press-library` using the same plumbing `/printing-press-publish` already established.

The skill lives in this repo (the machine) and acts on a printed CLI in the public library. It is sibling to `/printing-press-publish` (which adds a new CLI), `/printing-press-polish` (which improves a CLI pre-publish), and `/printing-press-retro` (which reflects on the machine itself). None of them cover post-publish CLI amendments driven by real-session friction.

## Requirements

**Skill Identity**
- R1. Standalone Claude Code skill at `skills/printing-press-amend/SKILL.md`
- R2. Invocable as `/printing-press-amend` with no required arguments; optional `<cli-name-or-path>` to scope explicitly
- R3. Self-contained — no dependency on the compound-engineering plugin or any other agent toolkit. Bundles its own planning + execution flow shaped like the rest of the family.
- R4. Name resolution: when a CLI is named explicitly, accept short name (`superhuman`), full name (`superhuman-pp-cli`), or path (`~/printing-press/library/superhuman-pp-cli`) — same UX as `/printing-press-polish` R3
- R5. Auto-detection: when no CLI is named, scan the active Claude Code session transcript for `<cli-name>-pp-cli` invocations and use the most-touched CLI as the default; confirm with the user before proceeding

**Friction Capture Phase**
- R6. Read the active Claude Code session transcript file (`~/.claude/projects/<dir>/<session>.jsonl` or equivalent) for the current session
- R7. Extract friction signals: non-zero exit codes, error messages, hand-rolled API payloads (e.g., direct `userdata.writeMessage` POSTs), retry-after-failure patterns, "X doesn't exist" / "X returns 400" type comments, missing-flag references, silent-null returns
- R8. Categorize each finding as **bug** (CLI behavior is wrong) or **feature** (CLI behavior is missing) with a one-line rationale
- R9. Cross-reference open + recently-merged PRs in `mvanhorn/printing-press-library` to suppress findings already shipped or in flight; report any matches and skip them
- R10. Stale-binary check: confirm the running CLI binary matches the latest published module version; if stale, instruct the user to update and abort the patch run (mirrors the `go install @latest` rule)

**Scope Confirmation Checkpoint (User-in-Loop #1)**
- R11. Present the captured findings as a tiered list: bugs (Tier 1), missing features that solve immediate pain (Tier 2), polish/architecture improvements (Tier 3). Use `AskUserQuestion`.
- R12. The user picks scope (one of: bugs only, bugs + Tier 2, all tiers, custom selection). Proceed only after confirmation.
- R13. Findings the user excludes go to a deferred list saved alongside the plan, not into the active patch.

**Plan + Execute Phase (Autonomous)**
- R14. Generate a plan document with implementation units for the confirmed scope, naming files in the target printed CLI, test scenarios per unit, and dependencies. Plan lives at `~/printing-press/manuscripts/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>.md` per family convention, plus mirrored to `/tmp/printing-press/patch/` for local copy
- R15. Execute the plan against the local printed CLI checkout (`~/printing-press/library/<api-slug>/`), respecting the family's machine-vs-printed-CLI rules: changes that should generalize go to a follow-up retro, changes that are CLI-specific stay in the patch
- R16. Build and test the modified CLI: `go build`, `go test ./...`, `go vet ./...`, plus `printing-press dogfood` against the modified binary if applicable
- R17. If any build/test fails, fix and retry up to 3 times; on persistent failure, surface the failure to the user with logs and pause (do not auto-open the PR)

**PII Scrub Phase**
- R18. Scrub PII from all artifacts that will leave the local machine: the plan doc body, the PR title and body, any test fixtures or example outputs added to the CLI
- R19. PII categories include: real email addresses, person names, company names matching a stop-list (configurable via `~/.printing-press/amend-config.yaml`), real meeting/email content quoted from the session
- R20. Replace scrubbed values with shape-preserving tokens (`<email-1>`, `<person-1>`, `<company-1>`) so reviewers can see the original intent. Do not silently delete content.
- R21. The skill MUST NOT touch the printed CLI's source code with PII (Go source is presumed PII-free; this is a defense-in-depth check)

**PR Draft Review Checkpoint (User-in-Loop #2)**
- R22. Show the user the PR title, body, file diff summary, and the structured `---PATCH-RESULT---` block before any `gh` command fires
- R23. The user picks one of: open PR as drafted, edit then open, hold (save plan + diff for later), or abort. Use `AskUserQuestion`.
- R24. Highlight any unscrubbed-PII risk findings explicitly in the review (e.g., "the plan doc references 'Esper Labs' — confirm this is intentional or scrub")

**PR Open Phase**
- R25. Reuse `/printing-press-publish`'s fork-clone-branch-push-PR plumbing where possible (call into the same shared scripts/helpers rather than reimplementing)
- R26. PR title format: `fix(<api-slug>): <one-line summary>` for bugs, `feat(<api-slug>): <one-line summary>` for features, matching the public library's commit/PR conventions
- R27. PR body sections: **Summary** (1-3 sentences), **Findings** (table of bug/feature with brief rationale), **Changes** (file-level diff summary), **Verification** (build/test/dogfood results), **Evidence** (full-URL links to before/after artifacts captured during the run)
- R28. Apply labels `comp:<api-slug>` and `priority:P<n>` per family convention; tier 1 bugs get `priority:P1`, tier 2 features get `priority:P2`, tier 3 improvements get `priority:P3`
- R29. Issue ownership: search for an existing open issue matching the findings; if one exists, link it and assign self in the PR; if none exists, open one with the captured findings before the PR (per the public library's contributor convention)
- R30. Emit structured `---PATCH-RESULT---` block on completion with PR URL, branch name, scope tier, files changed, build/test status, and PII scrub summary

**Output Artifact**
- R31. Local plan + run log saved to `~/printing-press/manuscripts/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>.md`
- R32. Mirrored copy to `/tmp/printing-press/patch/` for quick reference
- R33. Deferred-findings list (R13) saved alongside the plan as `<timestamp>-amend-<cli-name>-deferred.md` so future patches can pick them up

## Success Criteria

- A user finishing a session like the Superhuman dogfood from 2026-05-15 (12+ findings: missing folder coverage, hand-rolled drafts, silent AI-proxy nulls, expired-token confusion) fires `/printing-press-amend`, picks scope, reviews the PR draft, and lands an open PR against `mvanhorn/printing-press-library` in under 10 minutes — without manually invoking `/ce-plan`, `/ce-work`, `gh`, or any scrub tool
- The skill detects and skips a finding that would re-propose work already shipped in a recent PR (the "PR #571 already shipped" failure mode from 2026-05-15)
- Zero PII appears in the PR body, plan doc, or any committed test fixture across a representative sample of patch runs
- A second user without compound-engineering installed can run `/printing-press-amend` end-to-end without errors

## Scope Boundaries

- Limited to printed CLIs in `mvanhorn/printing-press-library` — does not patch the cli-printing-press machine repo, does not patch arbitrary third-party CLIs, does not target other public-library forks
- Triggers at the end of a Claude Code session (or any time the active session transcript is readable) — does not run live mid-session, does not observe in the background
- Does not generate net-new CLIs (that's `/printing-press` + `/printing-press-publish`)
- Does not improve a pre-publish CLI's quality (that's `/printing-press-polish`)
- Does not reflect on the machine itself or file machine-improvement issues (that's `/printing-press-retro`)
- Does not auto-merge the PR; always opens for human review

## Key Decisions

- **Wraps the full loop, not just PR packaging**: One command from session-end to PR-open. Two user checkpoints (scope after capture, PR draft before open) keep the user in the loop without making them drive each phase.
- **Self-contained, no compound-engineering dependency**: Bundles its own planning + execution rather than calling `/ce-plan` and `/ce-work`. PP-library users without that plugin still get the full workflow.
- **Auto-detect target CLI from the session transcript by default**: User can override with an explicit argument, but the killer experience is `/printing-press-amend` with no args at end of session.
- **Reuse `/printing-press-publish`'s fork/PR plumbing**: Don't reimplement the gh/git dance; share helpers so conventions stay aligned across the family.
- **PII scrub is default-on with a stop-list, not opt-in**: Real customer/person/company content from the session must never leak into a public PR. Replace with shape-preserving tokens so reviewers can see intent.
- **Stale-binary + duplicate-PR check before scope confirmation**: Borrowed from `/printing-press-polish`'s divergence pattern. Catches the "you proposed what PR #571 already shipped" failure before wasting the user's review time.
- **One PR per `/printing-press-amend` run**: Even when bugs and features both surface in a session. Keeps PR review cheap; the user can run the skill twice if they want atomic landings.

## Outstanding Questions

### Deferred to Planning
- [Affects R6][Technical] What's the canonical path to read the active Claude Code session transcript? Is there a stable API or do we walk `~/.claude/projects/`?
- [Affects R7][Needs research] What signal extraction technique is most robust for "user manually worked around a missing flag" — pattern-match on bash command sequences, look at agent text annotations, both?
- [Affects R9][Technical] PR cross-reference: search `mvanhorn/printing-press-library` open+recent PRs by what — keyword from finding, file path, scope tag? How fuzzy?
- [Affects R10][Technical] How does the skill know what "the latest published module version" of a printed CLI is? Is there a version registry, or does it query GitHub releases?
- [Affects R18-R21][Technical] PII detection mechanism: regex patterns, embed-based similarity, named-entity recognition, or a stop-list-only baseline for v1?
- [Affects R25][Technical] Which helpers in `/printing-press-publish` are extractable into shared scripts vs need to stay in that skill? May require refactor in publish.
- [Affects R29][Process] Does `mvanhorn/printing-press-library`'s contributor convention require an issue before the PR? Confirm against that repo's AGENTS.md.

## v0.2 Amendment — direct-input mode (2026-05-16)

The original brainstorm above frames the skill as dogfood-only — Phase 1 always mines the active session transcript for friction. The 2026-05-16 Digg-CLI amend surfaced a gap: when the user already knows what they want changed (rename a command, add named feeds, sniff for new endpoints), the dogfood-only framing fails. The skill auto-selects an unrelated recent transcript, produces a noisy or empty finding list, and halts at the transcript-confirmation modal.

The skill is extended to a second input mode (`direct-input`) plus a first-class sniff finding type. Both modes converge at Phase 2 onward — the typed finding list is mode-agnostic. The original dogfood-mode behavior is preserved bit-for-bit.

New requirements R34-R38 and the design are captured in `docs/plans/2026-05-16-002-feat-printing-press-amend-direct-input-mode-plan.md`. The expansion lands in the same PR (#1490) — the v0.1 skill had not merged yet.

## Next Steps

`/ce-plan` (which is what just got invoked) — implementation planning for this skill.
