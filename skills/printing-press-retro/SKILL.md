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
that was just printed, but improvements so the *next* CLI comes out stronger with
less manual effort.

This goes beyond bugs. The most valuable findings are often the work that *succeeded
but shouldn't have been necessary* — features you built by hand that the Printing
Press should have emitted, friction that recurs on every generation, and optimizations
you discovered that should become defaults.

The retro creates a GitHub issue on the printing-press repo with your findings and
artifacts so maintainers (or an AI agent) can fix the Printing Press.

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

- The retro is about the Printing Press, not the printed CLI. Do not propose fixes to one specific generated CLI.
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

Scan the conversation history for six categories of signal. Every finding becomes a
row in Phase 3 — don't filter yet, just collect.

**If running in a fresh conversation without generation history:** Note this and
proceed with manuscript evidence only. Focus on what the manuscripts reveal — scorecard
gaps, verify failures, dogfood issues, and obvious template patterns in the CLI source.
Mark session-dependent findings as "evidence: manuscripts only."

### 2a. Errors and retries

Any time a command failed and was re-run, a build broke, or the Printing Press produced
code that didn't compile. What broke and what fixed it?

### 2b. Manual code edits

Every hand-edit to generated code is a signal — the Printing Press *should have* gotten
it right but didn't. These are the highest-value findings because they point directly
at template gaps.

### 2c. Features built from scratch

Features that had to be written entirely by hand. Ask: is this a feature class the
Printing Press could reasonably emit, or is it genuinely custom?

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

## Phase 3: Classify findings

For each finding from Phase 2, answer these seven questions. Skip findings that only
affect this specific API and wouldn't recur.

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

| Component | Path |
|-----------|------|
| Generator templates | `internal/generator/` |
| Spec parser | `internal/spec/` |
| OpenAPI parser | `internal/openapi/` |
| Catalog | `catalog/` |
| Main skill | `skills/printing-press/SKILL.md` |
| Verify/dogfood/scorecard | CLI commands |

**5. Blast radius and fallback cost — should the Printing Press handle this?**

**Step A: Cross-API stress test.** Test across API shapes (standard REST, proxy-envelope,
RPC-style) and input methods (OpenAPI, crowd-sniffed, HAR-sniffed, no spec).

**Step B: Estimate frequency.** Every API / Most APIs / API subclass (name it) / This API only.

**Step C: Assess fallback cost.** How reliably will Claude catch and fix this across every
future API? A "simple" edit Claude forgets 30% of the time means 30% ship with the defect.

**Step D: Make the tradeoff.** Default is **fix it in the Printing Press**. The burden of
proof is on *not* fixing. Skip only when the behavior is unlikely to recur across 50
different APIs.

When the finding applies to an API subclass, include: Condition (when to activate),
Guard (when to skip), Frequency estimate.

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

Sort findings into two buckets:

- **Do** — a Printing Press fix is warranted. Assign a priority (P1, P2, P3) based on
  frequency, fallback reliability, and complexity. Scorer bugs are just findings like
  any other — rank them by impact alongside template gaps and parser issues.
- **Skip** — unlikely to recur. State why.

No numerical scoring formulas. State the priority reasoning in words.

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
| Finding | Title | Why unlikely to recur |
|---------|-------|----------------------|

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
- **Goal:** One sentence describing the outcome
- **Target:** <component and area, e.g., "Generator templates in internal/generator/">
- **Acceptance criteria:**
  - positive test: ...
  - negative test: ...
- **Scope boundary:** What this does NOT include
- **Dependencies:** Other work units that must complete first
- **Complexity:** small / medium / large
```

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
via `AskUserQuestion`.

> **Ready to submit your retro.**
>
> Here's what will happen:
>
> - A GitHub issue will be created on [mvanhorn/cli-printing-press](https://github.com/mvanhorn/cli-printing-press) with your **<N> findings** and **<M> work units**
> - Scrubbed artifact zips will be uploaded to catbox.moe and linked from the issue:
>   - **Manuscripts** (<size>) — research brief, shipcheck proof, build logs
>   - **CLI source** (<size>) — the generated Go code (no binary, no vendor/) *(omit if not available)*
>
> **Top findings:**
> - <1-3 sentence summary of the highest-priority Do Now items>
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

### Step 4: Create GitHub issue

Read and apply [references/issue-template.md](references/issue-template.md).

Build the issue body from the retro findings (distilled summary — not the full retro
document). Create the issue via `gh issue create --repo mvanhorn/cli-printing-press`.

If `gh` is not authenticated or issue creation fails, follow the graceful degradation
path in the issue-template reference: save locally and print manual filing instructions.

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

After the issue is created, show the user:

> **Retro submitted!**
>
> Issue: <full https:// URL>
>
> Found <N> findings across <M> work units.
> *(if artifacts uploaded)* Artifacts: [manuscripts](<URL>) · [CLI source](<URL>)
> Local copy: <$RETRO_SCRATCH_PATH>

If the issue wasn't created (user chose local-only, or gh failed), show the local
save paths instead.

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
- **Bias toward fixing.** When in doubt, fix it — scope narrowly with conditional
  logic if needed.
- **Look for broader patterns.** Before skipping, consider whether this is the first
  sighting of a behavior you'd encounter again.
- When a fix applies to an API subclass, include the condition AND the guard.
- **No time estimates.** Use complexity sizing (small/medium/large).
- Be thorough. Include enough detail that someone reading months later can understand
  the finding, the reasoning, and the proposed fix without the original conversation.
- Do not add more phases, documents, or gates to the main printing-press skill.
  Propose making existing phases smarter or the Printing Press emit better defaults.
