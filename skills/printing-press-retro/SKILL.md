---
name: printing-press-retro
description: >
  Run a retrospective after generating a CLI. Identifies systemic improvements
  to the Printing Press — templates, Go binary, skill instructions, catalog —
  so the next CLI comes out better. Creates a GitHub issue with actionable
  findings when there are Printing Press fixes to make.
  Use after any /printing-press run.
  Trigger phrases: "retro", "retrospective", "what went wrong", "improve
  the press", "post-mortem", "lessons learned", "what can we improve",
  "file a retro", "submit findings".
version: 0.1.0
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Agent
  - AskUserQuestion
---

# /printing-press-retro

Analyze a Printing Press session to find ways to improve the system that produces
CLIs — the Go binary, templates, skills, and catalog. Not fixes to the specific CLI
that was just printed, but improvements so the *next* CLI comes out stronger.

**It is a non-goal for the Printing Press to produce flawless CLIs without manual
tweaks.** That's the nature of the system. We expect agents to reason over the
generated CLI, customize for the specific API, build novel features, and iterate.
Some hand-built work in every run is normal.

The retro's job is to find the subset of manual work where **the machine could
have realistically raised the floor** — given the agent a better starting point,
prevented the issue entirely, or eliminated friction that would recur on the
next CLI. Two clear cases qualify:

1. **The machine could have completely prevented the issue, and the pattern is
   generalizable across many printed CLIs.** File it.
2. **The machine could have raised the floor meaningfully** — better default,
   partial scaffold, helper that absorbs the boilerplate — **across multiple
   CLIs you can name with evidence.** File it.

Otherwise, the manual work is normal iteration and should not generate a finding.
Some items make it back as machine fixes; not all. The retro is the filter that
distinguishes the two.

The retro creates a GitHub issue on the printing-press repo with the findings
that survive triage and the adversarial check, plus artifacts, so maintainers
(or an AI agent) can fix the Printing Press.

## Terminology

- **The Printing Press**: The whole system that produces CLIs. Use this name in all
  user-facing output (issues, retros, prompts). It has four subsystems:
  - **Generator** — templates that emit Go code (`internal/generator/`)
  - **Scorer** — tools that grade the output: verify, dogfood, scorecard
  - **Skills** — SKILL.md instructions that guide Claude during generation
  - **Binary** — the Go CLI itself: commands, flags, parsers (`cmd/printing-press/`)
- **Printed CLI**: A CLI produced by the Printing Press for a specific API (e.g.,
  `notion-pp-cli`). Printed-CLI fixes only help that one CLI.

Use "the Printing Press" when talking about the system. Use the subsystem name when
pointing a developer at what to fix — "fix the scorer" and "fix the generator" are
different PRs.

## Cardinal rules

- **Default is "don't change the machine."** The Printing Press is mature — 30+ CLIs printed, most templates exercised across many shapes. The burden of proof is on the finding, not on the Skip path. Most things you encountered while printing one CLI are that CLI's quirks, iteration noise, or upstream API behavior — not generator gaps. Propose a machine change only when cross-CLI evidence is concrete and the finding survives the Phase 3 adversarial check (Step G).
- **A retro of three sharp findings is more valuable than ten mixed-quality findings.** Each filed finding spends maintainer attention. If you find yourself writing "every finding warrants action" or producing zero drops and zero skips, stop and re-triage — that outcome is the failure mode this skill exists to prevent.
- The retro proposes Printing Press changes that help multiple printed CLIs. Don't propose direct edits to the one CLI that just shipped, and don't propose machine changes whose value is unique to this CLI's quirks — those are printed-CLI fixes wearing a generator costume.
- **Never upload un-scrubbed artifacts.** All artifacts go through the secrets scrub before upload.
- **Never modify source directories.** Manuscripts and library directories are read-only. Scrub operations work on temporary copies.
- **Never skip the secrets scrub,** even if the generation pipeline already ran one. Defense in depth.
- **Never work around a scorer bug in the Printing Press.** If a scoring tool penalizes something incorrectly, the fix goes in the scoring tool.

## Setup

<!-- RETRO_SETUP_START -->
```bash
# Path-only setup — no binary detection required.
# The retro skill reads manuscripts and runs gh/curl. It does not invoke the
# printing-press binary. This avoids aborting for users who installed the
# plugin but not the Go binary.

_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

PRESS_HOME="$HOME/printing-press"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_LIBRARY="$PRESS_HOME/library"
RETRO_SCRATCH_DIR="/tmp/printing-press/retro"

mkdir -p "$PRESS_MANUSCRIPTS" "$PRESS_LIBRARY" "$RETRO_SCRATCH_DIR"

# Detect whether we're inside the printing-press repo
IN_REPO=false
if [ -f "$_scope_dir/cmd/printing-press/main.go" ]; then
  IN_REPO=true
  REPO_ROOT="$_scope_dir"
  echo "Running from printing-press repo: $REPO_ROOT"
fi
```
<!-- RETRO_SETUP_END -->

## Guard rails

### Nothing to retro

```bash
if [ ! -d "$PRESS_MANUSCRIPTS" ] || [ -z "$(ls -A "$PRESS_MANUSCRIPTS" 2>/dev/null)" ]; then
  echo "No manuscripts found. Run /printing-press first to generate a CLI."
  exit 1
fi
```

### Resolve which API

If the user passed an API name as an argument, use that. Validate for path traversal:

```bash
# Reject names with /, \, or ..
if echo "$USER_API_NAME" | grep -qE '[/\\]|\.\.'; then
  echo "Invalid API name: '$USER_API_NAME'. Names cannot contain path separators or '..'."
  exit 1
fi

# Verify resolved path stays under PRESS_MANUSCRIPTS
RESOLVED="$(cd "$PRESS_MANUSCRIPTS/$USER_API_NAME" 2>/dev/null && pwd -P)"
case "$RESOLVED" in
  "$PRESS_MANUSCRIPTS"/*) ;; # OK
  *) echo "Invalid API name: path resolves outside manuscripts directory."; exit 1 ;;
esac
```

If no API name was provided and multiple APIs exist, list them with their most recent
run dates and ask the user to choose:

```bash
echo "Multiple APIs found in manuscripts:"
for api_dir in "$PRESS_MANUSCRIPTS"/*/; do
  api_name=$(basename "$api_dir")
  latest=$(ls -t "$api_dir" 2>/dev/null | head -1)
  echo "  - $api_name (latest run: $latest)"
done
```

Use `AskUserQuestion` to let the user pick.

### Resolve which run

If the API has multiple runs, default to the most recent. If the user specified a
run ID, use that. Otherwise:

```bash
API_DIR="$PRESS_MANUSCRIPTS/$API_NAME"
RUN_ID=$(ls -t "$API_DIR" 2>/dev/null | head -1)
RUN_DIR="$API_DIR/$RUN_ID"

echo "Retro for: $API_NAME (run $RUN_ID)"
echo "Manuscripts: $RUN_DIR"
```

### Resolve CLI directory

```bash
API_SLUG="$API_NAME"
CLI_NAME="${API_SLUG}-pp-cli"
CLI_DIR="$PRESS_LIBRARY/$CLI_NAME"

if [ ! -d "$CLI_DIR" ]; then
  # Try without -pp-cli suffix (legacy naming)
  CLI_DIR="$PRESS_LIBRARY/$API_NAME"
fi

if [ ! -d "$CLI_DIR" ]; then
  echo "WARNING: CLI directory not found at $PRESS_LIBRARY/$CLI_NAME"
  echo "Proceeding with manuscripts only — CLI source will not be included in artifacts."
  CLI_DIR=""
fi
```

## When to run

Best results come from running in the same conversation where the CLI was generated
(post-shipcheck) — the retro can mine the full conversation history for errors,
retries, manual edits, and discoveries.

If running in a fresh conversation, the retro proceeds with manuscript evidence only.
Phase 2 marks session-dependent findings as "evidence: manuscripts only."

## Phase 1: Gather evidence

Read all artifacts from the run:

1. **Research brief** — `$RUN_DIR/research/*brief*`
2. **Absorb manifest** — `$RUN_DIR/research/*absorb*`
3. **Shipcheck proof** — `$RUN_DIR/proofs/*shipcheck*`
4. **Build log** — `$RUN_DIR/proofs/*build-log*` (if exists)
5. **Live smoke log** — `$RUN_DIR/proofs/*live-smoke*` (if exists)
6. **The generated CLI** — `$CLI_DIR/` (if available)

Also gather the scorecard, verify pass rate, and dogfood report (from the shipcheck
proof or by re-running the tools if `IN_REPO` is true and the binary is available).

## Phase 2: Mine the session

Scan the conversation history for six categories of signal and produce a candidate
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

**If running in a fresh conversation without generation history:** Note this and
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

## Phase 2.5: Triage candidates

Before Phase 3 spends deep analysis on each candidate, run a fast triage to drop
candidates that don't justify the deeper look. **Most candidates should die here.**
The retro is a filter, not a funnel — if everything from Phase 2 makes it to Phase
3 unchanged, triage isn't doing its job.

For each candidate, ask in order:

1. **Was this iteration noise?** Normal trial-and-error during generation —
   one-off retry, typo recovery, agent forgetting a flag, transient network blip. Drop.
2. **Is this a printed-CLI fix?** The fix lives in `~/printing-press/library/<api>/`
   and helps only this one CLI. If the proposed change is "edit this command in
   this CLI" or "regenerate after fixing the spec," it's not a retro finding — it's
   a polish pass on that CLI. Drop.
3. **Is this an upstream API quirk?** The vendor returns null instead of 404, or
   ignores a query param the docs claim to honor, or has rate limits the spec
   doesn't declare. The Printing Press doesn't fix vendors. If the only fix is
   "work around this in the generator for every CLI," that's almost always wrong;
   if it's "let one CLI work around it," that's a printed-CLI fix. Drop.
4. **Is the only evidence "I noticed this once"?** A one-time observation that you
   can't connect to a recurring pattern across other CLIs is a candidate for Drop,
   not a P3. P3 means "low priority systemic finding," not "I want to record this
   somewhere."
5. **Does the same finding appear in 2+ prior retros without being implemented?**
   Don't re-raise at the same priority. Either drop it (the cost-benefit math has
   been "no" twice and the retro is becoming a wishlist), or reframe as a smaller
   incremental fix that addresses part of the friction. Search:
   `grep -l "<finding keywords>" ~/printing-press/manuscripts/*/proofs/*-retro-*.md`

Survivors of these five questions go to Phase 3. Dropped candidates are recorded
as one-line entries in the retro's "Dropped at triage" section — they exist for
your own discipline check and for the maintainer to see triage actually ran.

**Anti-pattern to avoid.** A recent Pagliacci retro produced *"Skip: None. Every
finding warrants action."* That sentence is the failure mode this triage exists
to prevent. Two of those findings (snake_case in `Use:`, root.go `Short:` rewrite
that the SKILL already documents as a manual step) were classic per-CLI / instructional
candidates that should have been dropped here. If you find yourself writing
"every finding warrants action," stop and re-run triage.

## Phase 3: Classify findings

For each candidate that survived Phase 2.5 triage, answer these seven questions.
Question 5 has seven sub-steps (A through G); Step G is the adversarial check.
Findings that fail Step G drop out — they don't get a priority, they don't go in
the Do/Skip tables, they go on the dropped-candidates list with the reason.

**1. What happened?** One sentence — the symptom, not the fix.

**2. Is the scorer correct?** (mandatory for score-penalty findings)
- **Scorer correct** → fix the Printing Press (templates, binary, or skill)
- **Scorer wrong** → fix the scoring tool, not the Printing Press
- **Both** → fix both, label which is primary

**3. What category?**

| Category | Description |
|----------|-------------|
| **Bug** | Generated code is wrong |
| **Scorer bug** | Scoring tool reports a false positive |
| **Template gap** | No template for a common pattern |
| **Assumption mismatch** | Printing Press assumes X but API uses Y |
| **Recurring friction** | Happens every generation, might be inherent |
| **Missing scaffolding** | Feature class the Printing Press could emit but doesn't |
| **Default gap** | Printing Press emits a wrong or placeholder default |
| **Discovered optimization** | Improvement found during use |
| **Skill instruction gap** | Skill told Claude wrong thing or missed a step |

**4. Where in the Printing Press does this originate?**

Pick exactly one component. The `slug` column drives the `comp:<slug>` label
applied to the WU sub-issue when filed (Phase 6), which is how agents filter
related WUs across retros (`gh issue list --label comp:<slug>`).

| Component | Slug | Path |
|-----------|------|------|
| Generator templates | `generator` | `internal/generator/` |
| Spec parser | `spec-parser` | `internal/spec/` |
| OpenAPI parser | `openapi-parser` | `internal/openapi/` |
| Catalog | `catalog` | `catalog/` |
| Main skill | `skill` | `skills/printing-press/SKILL.md` |
| Verify/dogfood/scorecard | `scorer` | CLI commands |

If a finding genuinely spans two components, pick the one where the durable
fix lands. Don't multi-label.

**5. Blast radius and fallback cost — should the Printing Press handle this?**

**Step A: Cross-API stress test.** Test across API shapes (standard REST, proxy-envelope,
RPC-style) and input methods (OpenAPI, crowd-sniffed, HAR-sniffed, no spec).

**Step B: Name three concrete APIs from the catalog with direct evidence.** Not "every
API with multi-word resources" or "any browser-sniffed CLI." Name three specific APIs
already in `~/printing-press/library/` (or the embedded `catalog/` directory) where you
can point to evidence the pattern exists: a path in their spec, a known endpoint shape,
a header the vendor documents, an output you can reproduce. "Stripe, Notion, GitHub
probably have this" is hand-waving; "Stripe (Stripe-Version header in spec line N),
GitHub (X-GitHub-Api-Version on the issues endpoints), Linear (api-version on /v2/*)"
is evidence. If you can name only two with evidence — or three with hand-waving — the
finding drops to **P3 max with a `subclass:<name>` annotation**, or moves to Drop.

**Step C: Counter-check question.** Ask explicitly: "If I implemented this fix, would it
actively hurt any API that doesn't have this pattern?" If yes, the fix needs a guard or
condition before being P1/P2 — not a default change. Example: turning on client-side
`?limit=N` truncation by default would hurt APIs that need server-side pagination for
correctness; it stays P2 only because it's gated on profiler-detected absence of a
paginator. Without that guard the same finding is unsafe to land.

**Step D: Recurrence-cost check.** Search prior retros under
`~/printing-press/manuscripts/*/proofs/*-retro-*.md` for the same finding. If the same
finding has been raised in 2+ prior retros without being implemented, the prior cost-
benefit math has been "no" twice. Don't re-raise it at the same priority — either move
to P3 with a "raised N times, still not justified" annotation, or reframe the finding
into a smaller incremental fix that addresses part of the friction. Recurrence at the
same priority is a triage failure, not stronger evidence.

**Capture matched prior retros.** When the search returns hits, record each as a
structured tuple — retro CLI name, retro file path (or GitHub issue number if the
retro file's frontmatter contains one), and a one-word classification:

- `aligned` — the prior retro proposed the same fix direction. Strengthens the case;
  reference it in Step F.
- `contradicts` — the prior retro proposed an *opposing* fix or chose a different
  default. Surface this explicitly: a maintainer reading the new finding must see
  the disagreement. State in one sentence why this retro reaches a different
  conclusion (e.g., "prior retro saw single-paginator APIs; this one saw an
  always-paginated API where the prior default would break").
- `extends` — the prior retro raised an adjacent finding in the same component
  area but a different specific fix. Useful context, doesn't change the case.

These tuples flow forward into the per-finding template ("Related prior retros")
and into the issue body when the finding becomes a sub-issue. GitHub auto-cross-
links any `#N` issue number you write, so contradictions and alignments will show
up in both retro timelines without further action.

**Step E: Assess fallback cost.** How reliably will Claude catch and fix this across every
future API? A "simple" edit Claude forgets 30% of the time means 30% ship with the defect.

**Step F: Make the tradeoff.** Default is **don't change the machine.** The burden of
proof is on the finding to justify a machine change. Continue to Step G only when all
three of these are true:

(a) Step B named three concrete APIs *with evidence* (not speculation).
(b) Step D's recurrence-cost check didn't disqualify the finding.
(c) Step C's counter-check didn't surface a hurts-other-APIs concern that lacks a guard.

If a finding can't clear all three, it doesn't get a priority — it goes to Drop with
the specific reason ("only named 2 APIs with evidence" / "raised 3 times, still not
justified" / "fix would hurt single-paginator APIs without a guard").

When the finding applies to an API subclass, include: Condition (when to activate),
Guard (when to skip), Frequency estimate.

**Step G: Construct the case against filing.** Before recording the finding, write
1-2 sentences arguing the *opposite* — what makes this look like a printed-CLI
fix, an iteration artifact, or a wishlist item. Why might a maintainer close this
as "works as designed" or "too narrow for a machine fix"? What's the strongest
version of "this shouldn't be filed"?

If the case-against is stronger than the case-for, drop the finding. If they're
roughly even, drop the finding (default direction is don't-file). Only when the
case-for is clearly stronger does the finding survive to Phase 4.

This step is not a formality. It is the explicit place where weak findings die.
A finding that survives Step G should be able to state, in one sentence, why
the case-against fails — and that sentence is worth quoting in the retro entry.

**6. Is this inherent or fixable?** Push hard on whether smarter templates, a
post-processing step, or better spec analysis could eliminate the friction. If inherent,
propose the cheapest mitigation.

**7. What is the durable fix?** Prefer: template fix > binary post-processing > skill instruction.

**Strip API-specific details from the proposed fix.** The durable fix must work across
APIs, not just the one that surfaced the finding. If the fix includes hardcoded param
names (e.g., `--sport`, `--league`), date formats (e.g., `YYYYMMDD`), chunking
strategies (e.g., monthly), or domain-specific logic, those are printed-CLI details
leaking into the machine recommendation. The machine fix should be parameterized —
driven by what the profiler detects in the spec, not by what one API happens to need.

Example of the anti-pattern:
- Finding: "ESPN sync needs `--dates` for historical data"
- Bad fix: "Add `--dates` with `YYYYMMDD-YYYYMMDD` format, `--sport`/`--league` flags, and monthly chunking to the sync template"
- Good fix: "When the profiler detects a date-range query param, emit a `--dates` flag that passes the value through to the API"

The bad fix bakes ESPN's date format, scope params, and chunking strategy into the
machine. The good fix lets the profiler drive behavior from the spec.

## Phase 4: Prioritize

Sort survivors of Phase 3 into three buckets:

- **Do** — survived Phase 3 Step G with a clear case-for. Assign a priority (P1,
  P2, P3) based on frequency, fallback reliability, and complexity. Scorer bugs
  are just findings like any other — rank them by impact alongside template gaps
  and parser issues.
- **Skip** — survived Phase 2.5 triage but didn't clear Phase 3 (Step B couldn't
  name 3 APIs with evidence, Step D recurrence-cost disqualified, or Step G's
  case-against was stronger). State the specific step that failed. These are
  listed in the retro so the maintainer can see what was considered and rejected.
- **Drop** — rejected at Phase 2.5 triage as iteration noise, printed-CLI fix,
  upstream API quirk, unproven one-off, or recurring-not-implemented. Listed as
  one-liners only — they don't need full analysis, they need a record so triage
  is auditable.

No numerical scoring formulas. State the priority reasoning in words.

**Sanity check before moving to Phase 5.** Look at the bucket distribution.
Almost every retro should have *some* drops and *some* skips. A retro with
"all Do, no Skip, no Drop" is the failure mode — re-run triage and Step G on
the weakest findings. Likewise, if every Do is P1, you're not prioritizing,
you're inflating; force yourself to identify the weakest "Do" and ask whether
it really beats the Skip bar.

## Phase 5: Write the retro

Write the full retro document using this template:

```markdown
# Printing Press Retro: <API name>

## Session Stats
- API: <name>
- Spec source: <catalog/browser-sniffed/docs/HAR>
- Scorecard: <score>/100 (<grade>)
- Verify pass rate: <X>%
- Fix loops: <N>
- Manual code edits: <N>
- Features built from scratch: <N>

## Findings

### 1. <Title> (<category>)
- **What happened:** ...
- **Scorer correct?** Yes / No / Partially. [details]
- **Root cause:** Component + what's specifically wrong
- **Cross-API check:** Would this recur?
- **Frequency:** every API / most / subclass:<name> / this API only
- **Fallback if the Printing Press doesn't fix it:** ...
- **Worth a Printing Press fix?** ...
- **Inherent or fixable:** ...
- **Durable fix:** ...
- **Test:** How to verify (positive + negative)
- **Evidence:** Session moment that surfaced this
- **Related prior retros:** *(from Phase 3 Step D; "None" if no matches)*
  - `<api-slug>` retro #<issue-num-if-known> — `aligned` / `contradicts` / `extends`. <one-sentence note on what changed or what's shared>
  - ...

## Prioritized Improvements

### P1 — High priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|

### P2 — Medium priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|

### P3 — Low priority
| Finding | Title | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---------|-------|-----------|-----------|---------------------|------------|--------|

*Omit empty priority sections.*

### Skip
| Finding | Title | Why it didn't make it (Step B / Step D / Step G) |
|---------|-------|--------------------------------------------------|

*Findings that survived Phase 2.5 triage but failed Phase 3 — name the specific
step that failed (e.g., "Step B: only 2 APIs with evidence" / "Step G: case-against
stronger; mostly per-CLI"). Empty if every Phase 3 candidate filed.*

### Dropped at triage
| Candidate | One-liner | Drop reason |
|-----------|-----------|-------------|

*Candidates rejected at Phase 2.5. One line each. Reasons: `iteration-noise` /
`printed-CLI` / `API-quirk` / `unproven-one-off` / `raised-N-times`. If this
section is empty, re-check Phase 2.5 — almost every retro has some.*

## Work Units
(see Phase 5.5)

## Anti-patterns
- ...

## What the Printing Press Got Right
- ...
```

Save the retro to manuscript proofs (always) and to the temp retro scratch
directory (always). Do not save retro documents under the source repo's
`docs/retros/` directory; the skill must work the same way for users who do not
have the repo checked out, and retro documents are issue artifacts rather than
durable repo docs.

```bash
RETRO_STAMP="$(date +%Y%m%d-%H%M%S)"
RETRO_PROOF_PATH="$PRESS_MANUSCRIPTS/$API_NAME/$RUN_ID/proofs/$RETRO_STAMP-retro-$CLI_NAME.md"
RETRO_SCRATCH_DIR="/tmp/printing-press/retro"
RETRO_SCRATCH_PATH="$RETRO_SCRATCH_DIR/$RETRO_STAMP-$API_NAME-retro.md"
mkdir -p "$(dirname "$RETRO_PROOF_PATH")" "$RETRO_SCRATCH_DIR"
```

Write the full retro document to `$RETRO_PROOF_PATH`, then copy that file to
`$RETRO_SCRATCH_PATH`. This must complete before Phase 6 Step 1 copies the
manuscripts directory to staging.

## Phase 5.5: Plannable work units

Group related findings into coherent work units a planner could pick up directly.

For each "Do" finding or group of related findings:

```markdown
### WU-1: <Title> (from F1, F3, ...)
- **Priority:** P1 / P2 / P3 *(max priority among absorbed findings — P1 if any
  absorbed finding is P1, else P2 if any is P2, else P3)*
- **Component:** generator / openapi-parser / spec-parser / scorer / skill / catalog
  *(must match one of the six fixed component slugs; drives the `comp:*` label
  applied to the sub-issue when filed)*
- **Goal:** One sentence describing the outcome
- **Target:** <component and area, e.g., "Generator templates in internal/generator/">
- **Acceptance criteria:**
  - positive test: ...
  - negative test: ...
- **Scope boundary:** What this does NOT include
- **Dependencies:** Other work units that must complete first
- **Complexity:** small / medium / large
```

The six fixed component slugs are: `generator` (`internal/generator/`),
`openapi-parser` (`internal/openapi/`), `spec-parser` (`internal/spec/`),
`scorer` (verify / dogfood / scorecard), `skill` (`skills/printing-press/SKILL.md`),
`catalog` (`catalog/`). If a WU genuinely spans two, pick the **primary** one — the
component where the durable fix will land. Pick exactly one; don't multi-label.

**If running from inside the printing-press repo (`IN_REPO=true`):**
Resolve target file paths using Glob and Grep tool invocations on `$REPO_ROOT` to
make work units more precise. E.g., use Glob to find `internal/generator/*.go` files,
Grep to find where sync code is generated.

**If running externally (`IN_REPO=false`):**
Describe target components by name (e.g., "Generator templates in `internal/generator/`")
and acceptance criteria without resolved file paths. The fixer will resolve paths when
they pick up the work.

## Phase 5.6: Issue gate — are there Printing Press improvements?

After prioritization and work units are written, decide whether a GitHub issue is
warranted. The purpose of the issue is to give someone (human or agent) something to
fix in the Printing Press. If every finding is specific to this one printed CLI with
nothing to change in the Printing Press, the issue is noise — there's nothing to act on.

**Skip the GitHub issue if:**
- Every finding landed in "Skip"
- All findings are printed-CLI-specific (manual edits that only apply to this one API
  and wouldn't recur across other CLIs)
- The "Do" table is empty

**Create the GitHub issue if:**
- There is at least one "Do" finding — i.e., something a maintainer or agent could
  act on in the Printing Press (templates, binary, skills, or scoring tools)

Use judgment. A retro that found three things but all three are "this API has a weird
auth scheme no other API uses" is not worth an issue. A retro that found one small
template gap that would help every future CLI *is* worth an issue.

If the issue is skipped, still save the retro locally (manuscript proofs +
`/tmp/printing-press/retro/`), present the findings to the user, then jump
directly to Phase 6 Step 6 (present results — adjusted to show local-only paths)
and Step 8 (offer next steps).

## Phase 6: Package, upload, and present

### Step 1: Package artifacts into staging folder

Read and apply [references/artifact-packaging.md](references/artifact-packaging.md)
**through Step 4 only** (create staging dir, copy, scrub, zip). Do not upload or
clean up yet — the staging folder stays alive until the end of Phase 6.

The staging folder (`$STAGING_DIR`) now contains the scrubbed copies and the zips.
This is both the review target and the upload source.

### Step 2: Confirm before publishing

*This step only runs if the Phase 5.6 issue gate passed (there are Printing Press findings to act on).*

Before uploading anything, show the user a friendly summary and ask for confirmation
via `AskUserQuestion`. Tailor the wording to whether this is single-issue or
parent-with-sub-issues mode (see `references/issue-template.md` Step 2 for the
`WU_COUNT` dispatch).

For **2+ WUs** (parent-with-sub-issues mode):

> **Ready to submit your retro.**
>
> Here's what will happen on [mvanhorn/cli-printing-press](https://github.com/mvanhorn/cli-printing-press):
>
> - **1 parent issue** with retro context (session stats, findings tables, artifact links)
> - **<M> sub-issues** — one per work unit, each with its own priority, component, and finding details, so each fix can be tracked, assigned, and closed independently
> - Labels: `retro`, `retro-parent` (parent only), `priority:P1`/`priority:P2`/`priority:P3` and `comp:*` (sub-issues), so future agents can filter all retro WUs by area or priority
> - Scrubbed artifact zips uploaded to catbox.moe and linked from the parent:
>   - **Manuscripts** (<size>) — research brief, shipcheck proof, build logs
>   - **CLI source** (<size>) — the generated Go code (no binary, no vendor/) *(omit if not available)*
>
> **Top findings:**
> - <1-3 sentence summary of the highest-priority Do Now items>
>
> Everything is staged at `<$STAGING_DIR>` if you'd like to inspect the files first.

For **1 WU** (single-issue mode):

> **Ready to submit your retro.**
>
> Here's what will happen on [mvanhorn/cli-printing-press](https://github.com/mvanhorn/cli-printing-press):
>
> - **1 GitHub issue** with the retro context, work unit, and absorbed findings inline
> - Labels: `retro`, `priority:P<n>`, `comp:<slug>`, so future agents can filter related work across retros
> - Scrubbed artifact zips uploaded to catbox.moe and linked from the issue:
>   - **Manuscripts** (<size>) — research brief, shipcheck proof, build logs
>   - **CLI source** (<size>) — the generated Go code *(omit if not available)*
>
> **Top finding:** <one-sentence summary>
>
> Everything is staged at `<$STAGING_DIR>` if you'd like to inspect the files first.

Options:
1. **Submit** — upload artifacts and create the issue
2. **Let me review the files first** — I'll check the staging folder, then come back
3. **Save locally only** — skip the issue, keep the manuscript proof and temp copy

If the user picks "Let me review the files first," acknowledge and wait. When they
come back, re-ask with Submit / Save locally only.

If the user picks "Save locally only," skip Steps 3 and 4 — the retro is already
saved to manuscript proofs and `/tmp/printing-press/retro/`. Clean up the staging
folder, then jump to Step 6.

### Step 3: Upload artifacts

Run artifact-packaging.md Step 5 (the catbox upload) using the zips already in
`$STAGING_DIR`. This produces `$MANUSCRIPTS_URL` and `$CLI_SOURCE_URL`.

### Step 4: Create GitHub issue(s)

Read and apply [references/issue-template.md](references/issue-template.md). It
covers:

1. **Step 1: Ensure labels exist** — create-only `gh label create` safety net
   for the 9 canonical labels (6 `comp:*` + `priority:P1`/`priority:P2`/`priority:P3`)
   plus the `retro` / `retro-parent` markers. Run once per invocation. Never
   edits existing labels.
2. **Step 2: Dispatch on WU count** — `single` mode (1 WU, inline body) or
   `parent-with-subs` mode (2+ WUs, parent issue + one sub-issue per WU linked
   via the GitHub sub-issues REST API).
3. The full body templates and the `gh api` calls for sub-issue linkage.

Each sub-issue carries its own `priority:P<n>` priority label and `comp:<slug>` component
label. This is what enables `gh issue list --label comp:openapi-parser` to surface
every retro WU in that area across every retro — the cross-retro discovery that
single-issue retros couldn't support.

Each WU sub-issue body also includes the **Related prior retros** block populated
in Phase 3 Step D, so contradictions and alignments with earlier retros are
visible and auto-cross-linked in GitHub timelines.

If `gh` is not authenticated, the sub-issues REST endpoint is unavailable, or any
issue creation fails, follow the graceful degradation path in the issue-template
reference: save locally and print manual filing instructions.

### Step 5: Local scratch copy

Ensure the temp scratch copy exists. This is the human-friendly local path for
reviewing or manually filing the retro when upload or issue creation fails.

```bash
if [ -f "$RETRO_PROOF_PATH" ]; then
  mkdir -p "$RETRO_SCRATCH_DIR"
  cp "$RETRO_PROOF_PATH" "$RETRO_SCRATCH_PATH"
fi
```

### Step 6: Present results

After the issue(s) are created, show the user a summary tailored to `$ISSUE_MODE`.

For **`parent-with-subs`** (`WU_COUNT >= 2`):

> **Retro submitted!**
>
> Parent issue: <full $PARENT_URL>
> Sub-issues (<M> WUs, P1 → P3 order):
>   - [P1] WU-1: <title> — <full WU-1 URL>
>   - [P1] WU-2: <title> — <full WU-2 URL>
>   - [P2] WU-3: <title> — <full WU-3 URL>
>   - ...
>
> Found <N> findings across <M> work units. Sub-issues are tagged with
> `comp:<slug>` and `priority:P<n>` labels — agents can filter related WUs
> across retros with `gh issue list --label comp:<slug>` or
> `gh issue list --label priority:P1`.
> *(if artifacts uploaded)* Artifacts: [manuscripts](<URL>) · [CLI source](<URL>)
> Local copy: <$RETRO_SCRATCH_PATH>

If `$FAILED_WUS` is non-empty (set by `references/issue-template.md` Step P3),
append a warning block before the closing line:

> ⚠️ Some WUs need attention:
>   - WU-2 (...) — issue creation failed
>   - WU-4 (#1234) — sub-issue link failed (cross-link in body remains)
>
> The parent issue's "Work Units" table marks failed creations as
> "**FAILED — file manually**" so the gap is visible. Re-run the skill or file
> the missing sub-issue(s) manually using the retro doc at <$RETRO_SCRATCH_PATH>.

For **`single`** (`WU_COUNT == 1`):

> **Retro submitted!**
>
> Issue: <full $ISSUE_URL>
>
> 1 work unit (P<n>, comp:<slug>) absorbing <N> findings.
> *(if artifacts uploaded)* Artifacts: [manuscripts](<URL>) · [CLI source](<URL>)
> Local copy: <$RETRO_SCRATCH_PATH>

If issue creation wasn't completed (user chose local-only, or gh failed), show
the local save paths and the manual filing instructions printed by the
issue-template fallback path.

### Step 7: Clean up staging folder

Run artifact-packaging.md Step 7 to delete `$STAGING_DIR`.

### Step 8: Offer next steps

Use `AskUserQuestion`:

**If `IN_REPO=true`:**

> 1. **Plan work units** — invoke `/compound-engineering:ce-plan` to plan top-priority WUs
> 2. **Plan a specific work unit** — pick one WU to plan
> 3. **Done for now**

If the user picks option 1 or 2, try to invoke `compound-engineering:ce-plan`. If
it's not available, fall back to printing the prompt the user would run manually.

**If `IN_REPO=false`:**

> The Printing Press maintainers will review your findings.
>
> 1. **Done**

Do not offer to plan work units when `IN_REPO=false` — the user is not in the
Printing Press repo and cannot act on the findings directly.

## Rules

- Prefer automatic fixes (templates, binary) over instructional fixes (skill).
- For recurring friction, always answer "inherent or fixable?" honestly.
- Be honest about what went well. Protecting good patterns matters.
- **Default is don't-file.** Bias toward filing only when Phase 3 Step B gave you
  three concrete cross-API examples *with evidence* (not speculation), and the
  Step G case-against was clearly weaker than the case-for. "20% of catalog"
  without named APIs is optimism. "Every API has multi-word resources" is
  hand-waving. The retro is a filter, not a wishlist; an issue overloaded
  with weak findings wastes maintainer attention.
- **When in doubt, drop.** A finding you're uncertain about almost certainly
  shouldn't be filed. The next CLI's retro will surface it again with stronger
  evidence if it's real; if it doesn't, it wasn't.
- **Look for broader patterns.** When something *does* clear the bar, check
  whether this is the first sighting of a behavior you'd encounter again.
- When a fix applies to an API subclass, include the condition AND the guard.
- **No time estimates.** Use complexity sizing (small/medium/large).
- Be thorough on the findings that survive. Include enough detail that someone
  reading months later can understand the finding, the reasoning, and the proposed
  fix without the original conversation.
- Do not add more phases, documents, or gates to the main printing-press skill.
  Propose making existing phases smarter or the Printing Press emit better defaults.
