---
title: "fix: Enforce sniff gate so model always asks before skipping"
type: fix
status: completed
date: 2026-04-11
---

# fix: Enforce sniff gate so model always asks before skipping

## Overview

The printing-press skill's Phase 1.7 Sniff Gate already says the model MUST ask the user via AskUserQuestion before skipping sniff. The model ignored this in a recent flightgoat run by reasoning around the instruction ("client-rendered needs Playwright, time budget is tight, we have a substitute source"). This plan adds a hard-gate marker file that Phase 1.5 (Absorb Gate) must verify before proceeding, plus per-source enforcement for combo CLIs.

## Problem Frame

Phase 1.7 is a soft rule in prose. The model can rationalize around it, especially when:

1. The target site is client-rendered (needs a real browser)
2. The 3-minute time budget looks tight before any attempt
3. Another data source could substitute for the sniff target
4. The CLI has multiple named sources (combo CLI) and the model decides "one of them is enough"

During the flightgoat run, all four failure modes fired at once. The user explicitly named Kayak's `/direct` matrix as "their magical thing" - the most valuable piece of the whole CLI - and the model unilaterally swapped it out for a FlightAware substitute without asking. This is exactly what the existing MUST-ask language was supposed to prevent.

The current gate has no forcing function. A marker file that Phase 1.5 refuses to proceed without makes the question un-skippable.

## Requirements Trace

- R1. The model cannot proceed to Phase 1.5 (Absorb Gate) without a sniff-gate decision recorded for every user-named source that triggers the gate
- R2. The decision is recorded in a machine-readable marker file under the run directory
- R3. Each source gets its own AskUserQuestion when it triggers the gate (one gate per source for combo CLIs)
- R4. The rationale "too hard to sniff / time budget tight / substitute source exists" is explicitly banned as a skip justification
- R5. The model cannot write the research brief until every applicable source has a marker entry
- R6. Skip conditions that DO silently bypass the gate (spec complete, user passed `--har` or `--spec`, `SNIFF_TARGET_URL` set) also write a marker entry so the same check works uniformly
- R7. Backward compatible with existing runs - new field, not a schema change

## Scope Boundaries

- Fixes the printing-press SKILL.md prose and adds a marker file contract - does NOT change the `printing-press` Go binary
- Does NOT change Phase 1.6 (Pre-Sniff Auth Intelligence) or Phase 1.8 (Crowd Sniff Gate)
- Does NOT change what sniffing does once approved - only the decision point
- Does NOT retroactively fix the flightgoat CLI that already shipped

## Context & Research

### Relevant Code and Patterns

- `skills/printing-press/SKILL.md` lines 603-677 - current Phase 1.7 Sniff Gate prose
- `skills/printing-press/SKILL.md` line 557 - existing "MANDATORY" enforcement for Phase 1.5 that this plan will extend
- `skills/printing-press/SKILL.md` Phase 0 "SNIFF_TARGET_URL" flag pattern - shows how run-state variables flow through phases
- `skills/printing-press/references/sniff-capture.md` - detailed capture instructions, referenced but not modified by this plan
- `$PRESS_RUNSTATE/runs/$RUN_ID/` - existing run directory structure where the marker file will live

### Institutional Learnings

- The model already ignores soft "MUST" language when it has plausible-sounding technical reasoning. The fix has to be structural, not another strongly-worded sentence.
- Marker files work in this codebase - the run directory already holds `state.json`, `research.json`, and lock files. Adding `sniff-gate.json` fits the pattern.
- Per-source questions for combo CLIs matter because the user's value is often concentrated in one specific source (Kayak /direct was flightgoat's heart)

## Key Technical Decisions

- **Hard gate via marker file**: Write `$PRESS_RUNSTATE/runs/$RUN_ID/sniff-gate.json` with one entry per source. Phase 1.5 reads the file and refuses to proceed if any expected source is missing an entry. Chosen over prose-only because the flightgoat failure shows prose alone is insufficient.
- **Per-source gate for combo CLIs**: When the user names multiple sources, emit one AskUserQuestion per source with a gap. Chosen over a single multiSelect question because each source has different answers/rationales and a single question is easy to skip uniformly.
- **Banned-reasons list**: Add an explicit block of reasoning the model may NOT use to skip sniff: client-rendering difficulty, time budget pre-judgment, substitute-source availability, tooling install friction. Strictly additive guardrail that complements the marker file.
- **Silent-skip paths also write a marker**: When the gate skips silently (spec complete, `--har`, `--spec`, `SNIFF_TARGET_URL`), write a marker with `decision: "skip-silent", reason: "<why>"`. This way Phase 1.5's check is a single uniform read rather than conditional logic.
- **Marker schema is additive**: If a run doesn't have `sniff-gate.json`, Phase 1.5 treats it as a soft warning on resumes only (doesn't HARD-FAIL on runs that started before this change). New runs always write it.

## Open Questions

### Resolved During Planning

- Q: Should the marker track just decisions or also outcomes? A: Decisions only. Outcomes live in existing proof files.
- Q: Does crowd sniff (Phase 1.8) also need a marker? A: No, out of scope. Phase 1.8 has different failure modes and is less user-facing.
- Q: What if the user declines all sniff options for all sources? A: Valid outcome. Marker records "declined" for each, Phase 1.5 proceeds.

### Deferred to Implementation

- Q: Exact JSON schema for `sniff-gate.json` - will be finalized while writing the prose, keeping it minimal (source_name, decision, reason, timestamp)
- Q: How strictly Phase 1.5 should fail on a missing marker on resumes of old runs - the implementer can tune between warn-and-continue and hard-fail once the prose is written

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

Gate state machine per source:

```
research complete for source
        |
        v
gap detected for source?
        |
    +---+---+
    |       |
    no      yes
    |       |
    v       v
marker    MUST ask user via AskUserQuestion
write:    (per-source, named options)
skip-     |
silent    +---+---+---+
          |   |   |
        approve decline alt-source
          |   |   |
          v   v   v
        marker write: decision + reason
          |
          v
all sources have markers?
          |
        +-+-+
        |   |
        no  yes
        |   |
        v   v
      HALT  proceed to Phase 1.5
```

Marker file shape (directional):

```json
{
  "run_id": "20260411-000903",
  "sources": [
    {
      "source_name": "kayak-direct",
      "decision": "approved|declined|skip-silent",
      "reason": "user chose to sniff / user declined / spec already complete",
      "asked_at": "2026-04-11T00:10:00Z"
    }
  ]
}
```

## Implementation Units

- [ ] **Unit 1: Rewrite Phase 1.7 sniff gate prose with hard enforcement**

**Goal:** Replace the existing Phase 1.7 prose with an enforced version that requires a marker file entry for every source before Phase 1.5 can proceed

**Requirements:** R1, R2, R4, R5

**Dependencies:** None

**Files:**
- Modify: `skills/printing-press/SKILL.md`

**Approach:**
- Keep the existing "When to offer sniff" decision matrix (it's correct), keep the "If user approves" and "If user declines" branches
- Add a new subsection at the top of Phase 1.7 titled "Enforcement" that describes the marker file contract
- Add a banned-reasons list as a subsection after "When to offer sniff"
- Every path through the phase (approve, decline, skip-silent) must end with a "write marker entry" instruction
- Update the "MANDATORY" paragraph before Phase 1.6 (currently at line 557 of SKILL.md) to reference the marker file

**Patterns to follow:**
- Existing `SNIFF_TARGET_URL` pattern for run-state variables
- Existing "MANDATORY" enforcement prose at the top of Phase 1.6

**Test scenarios:**
- Happy path: single-source run, user approves sniff -> marker has one entry with `decision: approved`
- Happy path: single-source run, user declines -> marker has one entry with `decision: declined`
- Happy path: `--spec` passed, spec appears complete -> marker has one entry with `decision: skip-silent, reason: spec-complete`
- Edge case: `--har` passed -> marker has one entry with `decision: skip-silent, reason: har-provided`
- Edge case: `SNIFF_TARGET_URL` set in Phase 0 -> marker has one entry with `decision: pre-approved-in-phase-0`
- Error path: model tries to proceed to Phase 1.5 without a marker -> HALT with message pointing back to Phase 1.7
- Error path: model attempts a banned rationale ("too hard", "budget tight", "substitute exists") -> explicit prose instructs to ask anyway

**Verification:**
- Re-read the full Phase 1.7 section and confirm every branch writes a marker
- Confirm the banned-reasons list is present and names the flightgoat failure modes explicitly
- Confirm Phase 1.5's MANDATORY header references the marker contract

- [ ] **Unit 2: Add per-source enforcement for combo CLIs**

**Goal:** When the initial briefing names multiple sources (e.g., "Google Flights + Kayak + FlightAware"), Phase 1.7 must evaluate each source independently and emit one AskUserQuestion per source with a detected gap

**Requirements:** R1, R3

**Dependencies:** Unit 1

**Files:**
- Modify: `skills/printing-press/SKILL.md`

**Approach:**
- Add a subsection in Phase 1.7 titled "Combo CLIs (multiple sources)" after "When to offer sniff"
- Define the identification rule: if Phase 0's briefing captured >1 named source, each is evaluated separately
- Source names come from the briefing, not from research inference - the user's exact words are the source identifiers
- Each source gets its own row in the decision matrix using the same criteria
- The marker file has one entry per source, all required before Phase 1.5
- Explicit example using the flightgoat pattern: FlightAware has a spec (skip-silent), Google Flights has fli (offer sniff as enrichment), Kayak /direct has no spec (offer sniff as primary)

**Patterns to follow:**
- The existing single-source decision matrix structure at line 614
- The briefing phase at line 142 where user arguments are parsed

**Test scenarios:**
- Happy path: single-source CLI with a spec -> one marker entry, behavior unchanged from before
- Happy path: combo CLI with 3 sources, all different spec states -> 3 marker entries, 2-3 AskUserQuestion calls (one per gap)
- Edge case: combo CLI where user only cares about one source -> still asks for all three, user can decline the others
- Edge case: user names two sources but second is an alias for the first -> implementer note: source dedup happens in the briefing, not here

**Verification:**
- A combo CLI run with 3 sources writes a marker with 3 entries
- Phase 1.5 refuses to proceed if any of the 3 is missing

- [ ] **Unit 3: Explicit banned-reasons list with flightgoat callout**

**Goal:** Enumerate the rationales the model may NOT use to skip the sniff gate, named directly so future models can't reason around them with slightly different words

**Requirements:** R4

**Dependencies:** Unit 1

**Files:**
- Modify: `skills/printing-press/SKILL.md`

**Approach:**
- Add a subsection titled "Banned skip reasons" inside Phase 1.7, after the "When to offer sniff" matrix
- List each banned reason with a brief explanation of why it's invalid:
  - "Client-rendered site needs Playwright" - browser capture tools (browser-use, agent-browser) handle client rendering, that's what they're for
  - "3-minute time budget looks tight" - the budget applies AFTER the user approves, not before. Pre-judging the budget is not the model's call
  - "We have a substitute data source" - substitution is the user's call, not the model's. Ask.
  - "Tooling installation is friction" - the skill already has an install path for capture tools
  - "Docs look thorough enough" - the existing prose already covers this; the model must still ask when gaps are detected
- Include a one-paragraph postmortem note referencing the flightgoat failure by scenario (not by user-identifying data): "A combo-CLI run once skipped the gate for a client-rendered site because all four banned reasons fired at once. The marker file exists so this can't happen again."

**Test scenarios:**
- Test expectation: none -- this is prose documentation, not behavior. Verification is by re-reading the rendered skill.

**Verification:**
- All four banned reasons are listed with explanations
- The postmortem paragraph is present
- The marker file contract is cross-referenced from this subsection

- [ ] **Unit 4: Update Phase 1.5 header to enforce the marker file**

**Goal:** Phase 1.5 (Absorb Gate) must read the marker file and HALT if any expected source is missing an entry, making the gate un-skippable from downstream

**Requirements:** R1, R5

**Dependencies:** Unit 1

**Files:**
- Modify: `skills/printing-press/SKILL.md`

**Approach:**
- Update the existing MANDATORY paragraph before Phase 1.6 (line 557) to also reference the marker file
- Add a new instruction at the very top of Phase 1.5 titled "Pre-flight check: sniff-gate marker"
- Instruction: read `$PRESS_RUNSTATE/runs/$RUN_ID/sniff-gate.json`. If missing, HALT with "Phase 1.7 Sniff Gate did not record a decision. Return to Phase 1.7." If the file exists but is missing an entry for a source named in the briefing, HALT with "Sniff Gate missing decision for source `<name>`."
- Do not proceed to Phase 1.5's Step 1.5a until the check passes

**Patterns to follow:**
- The existing "HARD STOP" pattern in Phase 1.9 (API Reachability Gate) for how to frame blocking checks

**Test scenarios:**
- Happy path: marker file exists with all expected entries -> Phase 1.5 proceeds
- Error path: marker file missing -> HALT with clear instruction to return to Phase 1.7
- Error path: marker file exists but missing an entry for a user-named source -> HALT with specific source name in the error
- Edge case: marker file exists with an unknown source (one the user named but briefing did not record) -> warn, do not halt (allow user-driven additions)

**Verification:**
- A dry-run through Phase 1.5 with no marker file produces the HALT message
- A dry-run with a correctly populated marker proceeds

- [ ] **Unit 5: Update sniff-capture.md reference to mention the marker contract**

**Goal:** The sniff-capture reference file should briefly note that Phase 1.7's gate writes a marker file, so implementers reading the reference understand the control flow

**Requirements:** R2

**Dependencies:** Unit 1

**Files:**
- Modify: `skills/printing-press/references/sniff-capture.md`

**Approach:**
- Add a short note at the top of sniff-capture.md: "This file documents what happens AFTER Phase 1.7 decides to sniff. The decision itself (approved, declined, skip-silent) is recorded in `$PRESS_RUNSTATE/runs/$RUN_ID/sniff-gate.json` by Phase 1.7 before this reference is used."
- No behavioral changes, just a pointer

**Test scenarios:**
- Test expectation: none -- pure documentation pointer, no behavior change.

**Verification:**
- The note is present at the top of sniff-capture.md

## System-Wide Impact

- **Interaction graph:** Phase 1.7 -> writes marker -> Phase 1.5 reads marker. Phase 1.6 (Pre-Sniff Auth) is unchanged. Phase 1.8 (Crowd Sniff) is unchanged but its behavior stays independent.
- **Error propagation:** A missing marker file in Phase 1.5 produces an explicit HALT. The model cannot silently skip forward.
- **State lifecycle risks:** Resumes of runs started before this change won't have a marker. The Phase 1.5 check should warn-and-continue for legacy resumes, HARD-FAIL for new runs. Distinguish via presence of `sniff-gate.json` adjacent files in the run dir.
- **API surface parity:** No generator or CLI template changes. Only the skill's prose and the marker file contract change. Generated CLIs are not affected.
- **Integration coverage:** Because this is skill-level prose, the main integration test is a re-run of a combo CLI scenario. Pick a test API with multiple sources, confirm the gate fires per-source, confirm the marker file is written, confirm Phase 1.5 enforces it.
- **Unchanged invariants:** The sniff capture flow itself (sniff-capture.md), the generator, the CLI templates, the library repo, and all already-shipped CLIs are untouched.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Model rewrites the marker file to fake approvals | The banned-reasons subsection explicitly calls this out as cheating. Marker includes `asked_at` timestamp that Phase 1.5 can sanity-check against the run's start time. |
| Legacy runs without markers fail on resume | The check distinguishes new-run vs resume. Resumes warn-and-continue; new runs hard-fail. |
| Per-source matrix creates too many questions for large combo CLIs (say 5+ sources) | In practice combo CLIs are 2-4 sources. Above that, the briefing itself should push back. Out of scope for this plan. |
| The skill is too long already - adding more prose makes it harder to follow | Offset by the banned-reasons list being short and the marker contract being mostly a 5-line JSON example |

## Documentation / Operational Notes

- After shipping, re-run a combo CLI scenario end-to-end to verify the gate fires correctly
- Update the printing-press retro template to include a "sniff gate compliance" check
- No changes needed to published CLIs or the library repo

## Sources & References

- Origin: user session feedback after flightgoat run on 2026-04-11 - model skipped Kayak /direct sniff despite user naming it as the key feature
- Related code: `skills/printing-press/SKILL.md` Phase 1.7 (lines 603-677), Phase 1.5 MANDATORY header (line 557)
- Related references: `skills/printing-press/references/sniff-capture.md`
