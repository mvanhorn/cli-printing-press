---
name: printing-press-retro
description: >
  Run a retrospective after generating a CLI. Identifies systemic improvements
  to the printing press machine — generator templates, Go binary, skill
  instructions, catalog. Creates a GitHub issue with findings and artifacts
  so anyone can fix the machine. Use after any /printing-press run.
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

Analyze a printing press session to find ways to make the *machine* better. Not
fixes to the CLI that was just printed — improvements to the generator, binary,
skill, and catalog so the *next* CLI comes out stronger with less manual effort.

This goes beyond bugs. The most valuable findings are often the work that *succeeded
but shouldn't have been necessary* — features you built by hand that the generator
should have emitted, friction that recurs on every generation, and optimizations you
discovered that should become defaults.

The retro creates a GitHub issue on the printing-press repo with your findings and
artifacts so maintainers (or an AI agent) can fix the machine.

## Cardinal rules

- The retro is about the machine, not the CLI. Do not propose fixes to the generated CLI.
- **Never upload un-scrubbed artifacts.** All artifacts go through the secrets scrub before upload.
- **Never modify source directories.** Manuscripts and library directories are read-only. Scrub operations work on temporary copies.
- **Never skip the secrets scrub,** even if the generation pipeline already ran one. Defense in depth.
- **Never work around a scorer bug in the generator.** If a scoring tool penalizes something incorrectly, the fix goes in the scoring tool.

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

mkdir -p "$PRESS_MANUSCRIPTS" "$PRESS_LIBRARY"

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
CLI_NAME="${API_NAME}-pp-cli"
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

Any time a command failed and was re-run, a build broke, or the generator produced
code that didn't compile. What broke and what fixed it?

### 2b. Manual code edits

Every hand-edit to generated code is a signal — the generator *should have* gotten it
right but didn't. These are the highest-value findings because they point directly at
template gaps.

### 2c. Features built from scratch

Features that had to be written entirely by hand. Ask: is this a feature class the
generator could reasonably emit, or is it genuinely custom?

### 2d. Recurring friction

Work that happens on *every* generation, not just this one. For each: **is this
inherent to the approach, or can the machine eliminate it?**

Propose at least two possible fixes at different levels (generator, binary
post-processing, skill instruction) and assess which is most durable.

### 2e. Discovered optimizations

Improvements noticed during the session — UX ideas, performance improvements, new
command patterns, output format improvements. Could this optimization be detected
automatically and applied by the generator?

### 2f. Scorer accuracy audit

Before proposing machine fixes to improve scores, check whether the scoring itself
is correct. **Changing the generator to satisfy a broken scorer is worse than doing
nothing.**

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

## Phase 3: Classify findings

For each finding from Phase 2, answer The Six Questions. Skip findings that only
affect this specific API and wouldn't recur.

**1. What happened?** One sentence — the symptom, not the fix.

**2. Is the scorer correct?** (mandatory for score-penalty findings)
- **Scorer correct** → fix the generator/template/CLI code
- **Scorer wrong** → fix the scoring tool, not the generator
- **Both** → fix both, label which is primary

**3. What category?**

| Category | Description |
|----------|-------------|
| **Bug** | Generated code is wrong |
| **Scorer bug** | Scoring tool reports a false positive |
| **Template gap** | Generator has no template for a common pattern |
| **Assumption mismatch** | Generator assumes X but API uses Y |
| **Recurring friction** | Happens every generation, might be inherent |
| **Missing scaffolding** | Feature class the generator could emit but doesn't |
| **Default gap** | Generator emits a wrong or placeholder default |
| **Discovered optimization** | Improvement found during use |
| **Skill instruction gap** | Skill told Claude wrong thing or missed a step |

**4. Where in the machine does this originate?**

| Component | Path |
|-----------|------|
| Generator templates | `internal/generator/` |
| Spec parser | `internal/spec/` |
| OpenAPI parser | `internal/openapi/` |
| Catalog | `catalog/` |
| Main skill | `skills/printing-press/SKILL.md` |
| Verify/dogfood/scorecard | CLI commands |

**5. Blast radius and fallback cost — should the machine handle this?**

**Step A: Cross-API stress test.** Test across API shapes (standard REST, proxy-envelope,
RPC-style) and input methods (OpenAPI, crowd-sniffed, HAR-sniffed, no spec).

**Step B: Estimate frequency.** Every API / Most APIs / API subclass (name it) / This API only.

**Step C: Assess fallback cost.** How reliably will Claude catch and fix this across every
future API? A "simple" edit Claude forgets 30% of the time means 30% ship with the defect.

**Step D: Make the tradeoff.** Default is **fix it in the machine**. The burden of proof
is on *not* fixing. Skip only when the behavior is unlikely to recur across 50 different APIs.

When the finding applies to an API subclass, include: Condition (when to activate),
Guard (when to skip), Frequency estimate.

**6. Is this inherent or fixable?** Push hard on whether a smarter generator, a
post-processing step, or better spec analysis could eliminate the friction. If inherent,
propose the cheapest mitigation.

**7. What is the durable fix?** Prefer: generator template fix > binary post-processing > skill instruction.

## Phase 4: Prioritize

Group findings into three buckets using judgment, not a formula:

- **Fix the scorer** — scoring tool is wrong. Highest priority because a wrong scorer
  distorts every future retro.
- **Do** — score is correct, machine fix is warranted. Split into "Do now" (scoped,
  immediate) and "Do next" (needs design/planning).
- **Skip** — unlikely to recur. State why.

No numerical scoring formulas. State the reasoning in words.

## Phase 5: Write the retro

Write the full retro document using this template:

```markdown
# Printing Press Retro: <API name>

## Session Stats
- API: <name>
- Spec source: <catalog/sniffed/docs/HAR>
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
- **Fallback if machine doesn't fix it:** ...
- **Worth a machine fix?** ...
- **Inherent or fixable:** ...
- **Durable fix:** ...
- **Test:** How to verify (positive + negative)
- **Evidence:** Session moment that surfaced this

## Prioritized Improvements

### Fix the Scorer
| # | Scorer | Bug | Impact | Fix target |
|---|--------|-----|--------|------------|

### Do Now
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|

### Do Next
| # | Fix | Component | Frequency | Fallback Reliability | Complexity | Guards |
|---|-----|-----------|-----------|---------------------|------------|--------|

### Skip
| # | Fix | Why unlikely to recur |
|---|-----|----------------------|

## Work Units
(see Phase 5.5)

## Anti-patterns
- ...

## What the Machine Got Right
- ...
```

Save the retro to manuscript proofs (always):

```
$PRESS_MANUSCRIPTS/$API_NAME/$RUN_ID/proofs/$(date +%Y%m%d-%H%M%S)-retro-$CLI_NAME.md
```

## Phase 5.5: Plannable work units

Group related findings into coherent work units a planner could pick up directly.

For each "Do now" or "Do next" group:

```markdown
### WU-1: <Title> (from findings #N, #M, ...)
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

## Phase 6: Package, upload, and present

### Step 1: Package artifacts into staging folder

Read and apply [references/artifact-packaging.md](references/artifact-packaging.md)
**through Step 4 only** (create staging dir, copy, scrub, zip). Do not upload or
clean up yet — the staging folder stays alive until the end of Phase 6.

The staging folder (`$STAGING_DIR`) now contains the scrubbed copies and the zips.
This is both the review target and the upload source.

### Step 2: Confirm before publishing

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
3. **Save locally only** — skip the issue, just save to manuscripts

If the user picks "Let me review the files first," acknowledge and wait. When they
come back, re-ask with Submit / Save locally only.

If the user picks "Save locally only," skip Steps 3 and 4 — save to manuscript
proofs (and `docs/retros/` if in-repo), clean up the staging folder, then jump
to Step 6.

### Step 3: Upload artifacts

Run artifact-packaging.md Step 5 (the catbox upload) using the zips already in
`$STAGING_DIR`. This produces `$MANUSCRIPTS_URL` and `$CLI_SOURCE_URL`.

### Step 4: Create GitHub issue

Read and apply [references/issue-template.md](references/issue-template.md).

Build the issue body from the retro findings (distilled summary — not the full retro
document). Create the issue via `gh issue create --repo mvanhorn/cli-printing-press`.

If `gh` is not authenticated or issue creation fails, follow the graceful degradation
path in the issue-template reference: save locally and print manual filing instructions.

### Step 5: Local save (conditional)

If `IN_REPO=true`, also save the retro to the repo:

```bash
RETRO_DIR="$REPO_ROOT/docs/retros"
mkdir -p "$RETRO_DIR"
RETRO_FILE="$RETRO_DIR/$(date +%Y-%m-%d)-$API_NAME-retro.md"
```

### Step 6: Present results

After the issue is created, show the user:

> **Retro submitted!**
>
> Issue: <full https:// URL>
>
> Found <N> findings across <M> work units.
> *(if artifacts uploaded)* Artifacts: [manuscripts](<URL>) · [CLI source](<URL>)

If the issue wasn't created (user chose local-only, or gh failed), show the local
save paths instead.

### Step 7: Clean up staging folder

Run artifact-packaging.md Step 7 to delete `$STAGING_DIR`.

### Step 8: Offer next steps

Use `AskUserQuestion`:

**If `IN_REPO=true`:**

> 1. **Plan "Do now" work units** — invoke `/compound-engineering:ce-plan`
> 2. **Plan a specific work unit** — pick one WU
> 3. **Done for now**

If the user picks option 1 or 2, try to invoke `compound-engineering:ce-plan`. If
it's not available, fall back to printing the prompt the user would run manually.

**If `IN_REPO=false`:**

> The printing-press maintainers will review your findings.
>
> 1. **Done**

## Rules

- Prefer automatic fixes (generator, binary) over instructional fixes (skill).
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
  Propose making existing phases smarter or the generator emit better defaults.
