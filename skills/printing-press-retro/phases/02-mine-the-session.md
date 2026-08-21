## Phase 2: Mine the session

Scan the conversation history for seven categories of signal and produce a candidate
list. The candidate list is *not* the finding list — Phase 2.5 triage will cull it
and Phase 3 will further drop weak survivors. Most candidates will not survive.

While collecting, distinguish:

- **Iteration noise** — one-off retries, typos, normal trial-and-error during a
  long generation. Skip these even at the candidate stage; they don't survive triage.
- **Per-CLI quirks** — behavior tied to this API's shape (auth oddity, undocumented
  endpoint, vendor-specific envelope) that wouldn't recur on another spec. Add to
  the candidate list with a "looks per-CLI" tag — most will be dropped at triage.
- **Systemic friction** — patterns that would plausibly recur on the next CLI
  (template gap, default that needs to change, skill instruction that misled you).
  These are what the retro exists to surface.

**Context budget.** Retro typically starts inside a print session that is already
deep, and mining is the phase that reads the most. The live conversation is not the
only source: the session transcript exists on disk (Claude Code writes
`~/.claude/projects/<project-slug>/*.jsonl`; other harnesses keep equivalents), so an
orchestrated run may delegate this phase to a subagent that reads the transcript file
plus the Phase 1 evidence and returns only the candidate list. Later phases consume
the candidate list, never the transcript.

**If running in a fresh conversation without generation history:** Prefer mining the
print session's on-disk transcript (above) when it is still available — that path
loses no fidelity. Otherwise note this and
proceed with manuscript evidence only. Focus on what the manuscripts reveal — scorecard
gaps, verify failures, dogfood issues, and obvious template patterns in the CLI source.
Mark session-dependent findings as "evidence: manuscripts only."

### 2a. Errors and retries

Any time a command failed and was re-run, a build broke, or the Printing Press produced
code that didn't compile. What broke and what fixed it?

### 2b. Manual code edits

Manual edits during iteration are normal — agents reason over the generated CLI
and tweak. A single edit to handle this CLI's quirk is the workflow.

For each manual edit, ask: **could the machine have raised the floor here?**

- *Could the machine have completely prevented this edit?* Default was wrong for
  most APIs, template emitted broken code, parser missed a common pattern. If
  yes AND the same edit would be needed on multiple CLIs you can name with
  evidence → candidate.
- *Could the machine have given a better starting point that made the edit
  smaller, simpler, or skippable in common cases?* Even if you'd still tweak,
  raising the floor compounds across future CLIs. If yes AND generalizable →
  candidate.
- *Was this just per-API customization the agent was expected to do?* Drop.
- *Was this iteration noise (typo, retry, transient confusion)?* Drop.

The triage question is whether the machine raising the floor would compound
across future CLIs — not whether this one CLI would have shipped a few lines
lighter.

### 2c. Features built from scratch

Hand-built features (transcendence commands, novel commands, helper packages for
secondary APIs) are part of the workflow — agents build the domain-specific
value layer on top of the API surface the machine emits. Building features by
hand is not by itself a finding.

For each hand-built feature, ask: **could the machine have raised the floor for
this kind of feature?**

- *Could the machine have emitted a working default version, even if you'd still
  customize it?* (E.g., every list+detail API benefits from a `summary`
  aggregation that the machine could scaffold from the spec.) Candidate, if
  generalizable across multiple named APIs.
- *Could the machine have emitted scaffolding, types, or helpers that would have
  cut the build effort meaningfully?* (E.g., a typed secondary-client template
  for combo CLIs, a fanout-aggregation helper.) Candidate, if generalizable.
- *Is this genuinely custom domain logic the machine couldn't realistically
  generate from a spec?* (E.g., booking a slot is custom orchestration; the
  machine can emit the underlying endpoints but not the choreography.) Drop —
  the SKILL is the right place to share the recipe, not the generator.

The "raises the floor" test separates "machine fix" from "SKILL recipe": if the
machine's contribution would still leave significant per-CLI work, the recipe
belongs in the SKILL so the next agent knows the pattern; if the machine could
absorb the boilerplate cleanly, it's a generator template.

### 2d. Recurring friction

Work that happens on *every* generation, not just this one. For each: **is this
inherent to the approach, or can the Printing Press eliminate it?**

Propose at least two possible fixes at different levels (generator templates, binary
post-processing, skill instruction) and assess which is most durable.

### 2e. Discovered optimizations

Improvements noticed during the session — UX ideas, performance improvements, new
command patterns, output format improvements. Could this optimization be detected
automatically and applied by the Printing Press?

### 2f. Scorer accuracy audit

Before proposing Printing Press fixes to improve scores, check whether the scoring
itself is correct. **Changing the Printing Press to satisfy a broken scorer is worse
than doing nothing.**

For each score penalty from dogfood, verify, and scorecard:

1. **Trace the scorer's logic.** Read the scoring tool's source code to understand
   exactly what it checks. Don't guess.
2. **Test the scorer's assumption against reality.** Does the CLI actually have the
   problem the scorer claims?
3. **Classify the penalty:**
   - **Scorer is correct** — the CLI genuinely has this problem.
   - **Scorer is wrong** — the CLI is fine; the scoring tool has a bug.
   - **Scorer is partially right** — both could be better.

Common scorer bugs: name derivation mismatches, grep-based detection missing patterns,
file exclusions too broad, section-counting heuristics.

The scorer audit is not optional. Every finding from a score penalty must have a
"Scorer correct?" assessment before proposing a fix direction.

### 2g. Combo CLI priority audit

**Only runs when the briefing named 2+ sources.** Check `$RUN_DIR/source-priority.json`
(from the Multi-Source Priority Gate in the main skill). If it doesn't exist but the
briefing or user command clearly listed multiple services, that's itself a finding:
the priority gate didn't fire when it should have.

For runs with a `source-priority.json`, cross-reference it against the absorb manifest
and the shipped CLI:

1. **Command count per source.** Count commands attributed to each named source in the
   manifest. The primary should have **at least as many** as any secondary. If it has
   fewer, that's a **priority inversion** and becomes a finding — even if the user
   approved the manifest, it means the skill's discovery path for the primary failed
   silently.
2. **Auth scoping.** If the primary was declared free in the priority gate but the
   shipped CLI requires a paid key for the primary's headline commands, that's a
   finding — the economics check either didn't run or didn't route the paid key
   correctly to secondary-only scope.
3. **README leadership.** The primary should lead the README and `--help`. If a
   secondary is the first thing the user sees, flag it.

Each of these is a **skill instruction gap** category finding. The durable fix lives
in `skills/printing-press/SKILL.md` (the Multi-Source Priority Gate, the Priority
inversion check before Phase Gate 1.5, and the brief's `## Source Priority` section)
or in the generator if README ordering is template-driven.

Next: phases/03-triage-candidates.md
