---
name: printing-press-retro
description: >
  Run a retrospective after generating a CLI with the printing press. Identifies
  systemic improvements to the machine — generator templates, Go binary, skill
  instructions, catalog — not patches to the specific CLI. Covers bugs, but also
  recurring friction (like dead code), features that had to be built manually,
  and optimizations discovered during the session. Use after any /printing-press
  run. Trigger phrases: "retro", "retrospective", "what went wrong", "improve
  the press", "post-mortem", "lessons learned", "what can we improve".
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

## When to run

Run in the same conversation where the CLI was generated (post-shipcheck). The retro
needs the full conversation history — every error, retry, manual edit, and discovery.

If running in a fresh conversation, point it at the manuscripts directory, but know
you'll miss the in-conversation debugging context.

## Setup

```bash
PRESS_HOME="$HOME/printing-press"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"

# Find the most recent run across all APIs
LATEST_RUN=$(find "$PRESS_MANUSCRIPTS" -name "*shipcheck*" -type f -exec stat -f "%m %N" {} \; | sort -rn | head -1 | awk '{print $2}')

if [ -z "$LATEST_RUN" ]; then
  echo "No shipcheck proofs found. Run /printing-press first."
  exit 1
fi

API_NAME=$(echo "$LATEST_RUN" | sed "s|$PRESS_MANUSCRIPTS/||" | cut -d'/' -f1)
RUN_ID=$(echo "$LATEST_RUN" | sed "s|$PRESS_MANUSCRIPTS/||" | cut -d'/' -f2)
RUN_DIR="$PRESS_MANUSCRIPTS/$API_NAME/$RUN_ID"

echo "Retro for: $API_NAME (run $RUN_ID)"
echo "Manuscripts: $RUN_DIR"
```

If the user passed an API name as an argument, use that instead of auto-detecting.

## Phase 1: Gather evidence

Read all artifacts from the run:

1. **Research brief** — `$RUN_DIR/research/*brief*`
2. **Absorb manifest** — `$RUN_DIR/research/*absorb*`
3. **Shipcheck proof** — `$RUN_DIR/proofs/*shipcheck*`
4. **Build log** — `$RUN_DIR/proofs/*build-log*` (if exists)
5. **Live smoke log** — `$RUN_DIR/proofs/*live-smoke*` (if exists)
6. **The generated CLI** — `$PRESS_HOME/library/<api>-pp-cli/`

Also gather the scorecard, verify pass rate, and dogfood report (from the shipcheck
proof or by re-running the tools).

## Phase 2: Mine the session

Scan the full conversation history for five categories of signal. Every finding
becomes a row in Phase 3 — don't filter yet, just collect.

### 2a. Errors and retries

Any time a command failed and was re-run, a build broke, or the generator produced
code that didn't compile. What broke, what fixed it, and how long did it take?

### 2b. Manual code edits

Every hand-edit to generated code is a signal. Each one means the generator *should
have* gotten it right but didn't. These are the highest-value findings because they
point directly at template gaps.

Examples from real sessions:
- Rewriting the root command `Short:` description from API-speak to user-speak
- Adding top-level commands to wrap deeply-nested generated commands
- Fixing `serviceForPath` routing for proxy-envelope APIs
- Rewriting the sync command for offset-based pagination
- Adding entity-specific store tables the generator didn't create

### 2c. Features built from scratch

Features in the absorb manifest or transcendence list that had to be written entirely
by hand during Phase 3. The generator produced no scaffolding for them. Ask: is this
a feature class the generator could reasonably emit, or is it genuinely custom?

For example: if every CLI needs a `trending` command that queries local SQLite, maybe
the generator should emit a trending template when it detects time-series metrics in
the spec.

### 2d. Recurring friction

Work that happens on *every* generation, not just this one. The key question for each:
**is this inherent to the approach, or can the machine eliminate it?**

Examples:
- **Dead code** — The generator emits generic helpers (CSV, delete-classify, etc.) and
  some are never called. Is the fix to stop emitting them (risk: some CLIs need them)?
  Or to add a post-generation dead-code sweep? Or to make the generator smarter about
  which helpers each API actually needs?
- **Default resource mismatch** — `defaultSyncResources()` always returns a placeholder.
  Could the generator derive the right resources from the spec's entity types?
- **DB path inconsistency** — Different generated commands use different default paths.
  Could the generator emit a single `defaultDBPath()` and reference it everywhere?

For each piece of friction, propose at least two possible fixes at different levels
(generator, binary post-processing, skill instruction) and assess which is most durable.

### 2e. Discovered optimizations

Improvements noticed during the session that weren't fixing a problem — they were
making something better. These might be UX ideas, performance improvements, new
command patterns, or output format improvements that emerged from actually using the
CLI.

Ask: could this optimization be detected automatically and applied by the generator?

## Phase 3: Classify findings

For each finding from Phase 2, answer these questions. Skip findings that only
affect this specific API and wouldn't recur.

### The Six Questions

**1. What happened?**
One sentence. Describe the symptom or the work that was done, not the fix.

**2. What category is this?**

| Category | Description | Example |
|----------|-------------|---------|
| **Bug** | Generated code is wrong | serviceForPath returns wrong service |
| **Template gap** | Generator has no template for a common pattern | No top-level command aliases |
| **Assumption mismatch** | Generator assumes X but API uses Y | Cursor pagination vs offset |
| **Recurring friction** | Happens every generation, might be inherent | Dead code cleanup |
| **Missing scaffolding** | Feature class the generator could emit but doesn't | Entity-specific store tables |
| **Default gap** | Generator emits a wrong or placeholder default | Sync resources list, DB path |
| **Discovered optimization** | Improvement found during use | Compact number formatting |
| **Skill instruction gap** | Skill told Claude wrong thing or missed a step | Phase ordering issue |
| **Tool limitation** | Verify/dogfood/scorecard missed or mis-reported | False positive dead code |

**3. Where in the machine does this originate?**

| Component | Path | Controls |
|-----------|------|----------|
| Generator templates | `internal/generator/` | Go code emitted for commands, store, client |
| Spec parser | `internal/spec/` | Internal YAML spec parsing |
| OpenAPI parser | `internal/openapi/` | OpenAPI 3.0+ parsing |
| Catalog | `catalog/` | API entries and metadata |
| Main skill | `skills/printing-press/SKILL.md` | Orchestration instructions |
| Verify/dogfood/scorecard | CLI commands | Quality checking tools |

**4. Blast radius and fallback cost — should the machine handle this?**

This is the most important question and the easiest to get wrong. The retro runs
right after a session with one API, and pattern-matching from a single example is
unreliable. A finding that felt universal during the Postman Explore session might
be specific to proxy-envelope APIs, or to sniffed specs, or to APIs with entity-type
enum params.

**Step A: Cross-API stress test.** Mentally test each finding against at least three
different API shapes:

- A standard REST API with clean OpenAPI spec (e.g., Stripe, GitHub)
- A minimal/undocumented API discovered via sniff or HAR
- An API with a different auth model or response format

For each, ask: "Would this exact problem occur? Would the proposed fix help, be
irrelevant, or actively hurt?"

**Step B: Estimate frequency.** Based on the stress test, assign a blast radius:

- **Every API** — occurs regardless of API shape. Be skeptical of this label — it
  must fail the stress test for all three shapes.
- **Most APIs** — affects common patterns. Name the triggering condition.
- **API subclass** — affects a specific pattern. Name the subclass precisely:
  proxy-envelope, GraphQL, sniffed-only, offset-paginated, etc.
- **This API only** — isolated quirk.

**Step C: Assess fallback cost.** This is what happens if the machine does NOT have
the fix. For each finding, the fallback is one of:

| Fallback | Cost | Example |
|----------|------|---------|
| **Claude rewrites from scratch** | High — 10+ min, error-prone, may forget | Rewriting the entire sync command for offset pagination |
| **Claude makes targeted edits** | Medium — 2-5 min, usually succeeds | Fixing serviceForPath routing, changing a default path |
| **Claude deletes/tweaks one thing** | Low — <1 min, mechanical | Removing 3 dead functions, rewriting a Short description |
| **CLI ships broken** | Critical — user hits the bug at runtime | Infinite sync loop, wrong API responses, empty search |

**Step D: Make the tradeoff.** The decision to add conditional logic to the machine
is NOT just "is it general enough?" — it's a cost-benefit:

```
machine fix justified when:
  (frequency × fallback cost) > (implementation effort + regression risk)
```

This means:
- A finding affecting only 20% of APIs (API subclass) can still justify a machine fix
  if the fallback is "Claude completely rewrites the sync command" (high cost) or
  "CLI ships with an infinite loop" (critical).
- A finding affecting 100% of APIs might NOT justify a machine fix if the fallback is
  "Claude changes one line" (low cost) and the fix is complex to implement.
- Narrow-scope fixes should include conditional logic (activate when X, skip otherwise)
  so they don't regress the simple case. But the mere fact that a condition is required
  is not a reason to skip the fix — it's a reason to scope it carefully.

**The counterpoint to being conservative:** if the machine doesn't cover a wide enough
breadth of cases, the printed CLIs suffer. Best case is Claude dynamically fixes the
problem during generation — which is inefficient, error-prone, and might not happen.
Worst case is the CLI ships with the defect. Every finding left out of the machine is
a bet that Claude will catch it every time, and Claude won't.

When the finding applies to an API subclass, the recommendation must include:
- **Condition:** When to activate (e.g., "spec has `x-proxy-routes`")
- **Guard:** When to skip (e.g., "standard REST APIs without proxy pattern")
- **Frequency estimate:** How common is this subclass? If it's >20% of APIs the
  printing press targets, the conditional logic is likely worth the complexity.

**5. Is this inherent or fixable?**
This question matters most for recurring friction. Some friction is structural — code
generation will always produce some unused code because templates are generic. But
"inherent" shouldn't be the default answer. Push hard on whether a smarter generator,
a post-processing step, or better spec analysis could eliminate the friction.

If inherent: propose the cheapest mitigation (e.g., "dogfood auto-deletes dead helpers
as a post-generation step").

If fixable: propose the fix at the right level.

**6. What is the durable fix?**
A concrete change to the machine. Prefer this hierarchy:

1. **Generator template fix** — Code is emitted correctly from the start. Zero manual work.
2. **Binary post-processing** — A printing-press command that auto-fixes after generation
   (like a `printing-press polish` that removes dead code and aligns paths).
3. **Skill instruction** — Tell Claude to do it during generation. Last resort because
   Claude might forget or get it wrong. Every instruction is a tax on every future run.

Describe what test would verify the fix: "Generate a CLI for an API with offset
pagination and verify sync terminates after fetching all pages."

## Phase 4: Prioritize

Score each finding using the tradeoff from Question 4:

```
priority = (frequency × fallback cost) / (implementation effort + regression risk)
```

Where:
- **Frequency**: every=4, most=3, subclass=2, this-API=1
- **Fallback cost**: critical=4, high(rewrite)=3, medium(targeted edit)=2, low(one-liner)=1
- **Implementation effort**: 1(hours) to 4(weeks)
- **Regression risk**: 0(conditional/guarded) to 3(blanket change touching all APIs)

Present as a ranked table. Group into tiers:
- **Tier 1: Do now** — high frequency×fallback, low effort, guarded implementation
- **Tier 2: Plan** — high frequency×fallback but needs design work or careful guards
- **Tier 3: Backlog** — low fallback cost (Claude handles it fine dynamically) or
  inherent friction with cheap mitigations
- **Skip** — this-API-only findings or cases where the dynamic fix is genuinely easier
  than the machine fix

## Phase 5: Write the retro

```markdown
# Printing Press Retro: <API name>

## Session Stats
- API: <name>
- Spec source: <catalog/sniffed/docs/HAR>
- Scorecard: <before> -> <after> (if applicable)
- Verify pass rate: <X>%
- Fix loops: <N>
- Manual code edits: <N>
- Features built from scratch: <N>
- Time to ship: ~<X>m

## Findings

### 1. <Title> (<category>)
- **What happened:** ...
- **Root cause:** Component + what's specifically wrong
- **Cross-API check:** Would this occur for [standard REST]? [sniffed API]? [different auth]?
- **Frequency:** every API / most / subclass:<name> / this API only
- **Fallback if machine doesn't fix it:** What Claude has to do dynamically (rewrite,
  edit, one-liner) or what ships broken
- **Tradeoff:** Is the machine fix worth it given frequency × fallback cost vs
  implementation effort + regression risk?
- **Inherent or fixable:** ...
- **Durable fix:** Concrete machine change. If subclass-scoped, include:
  - Condition: when to activate
  - Guard: when to skip
  - Frequency estimate: how common is this subclass?
- **Test:** How to verify, including a negative test for APIs that should NOT be affected
- **Evidence:** Session moment that surfaced this

### 2. ...

## Prioritized Improvements

### Tier 1: Do Now
| # | Fix | Component | Frequency | Fallback Cost | Effort | Guards |
|---|-----|-----------|-----------|--------------|--------|--------|

### Tier 2: Plan
| # | Fix | Component | Frequency | Fallback Cost | Effort | Guards |
|---|-----|-----------|-----------|--------------|--------|--------|

### Tier 3: Backlog
| # | Fix | Component | Frequency | Fallback Cost | Effort | Guards |
|---|-----|-----------|-----------|--------------|--------|--------|

## Work Units

### WU-1: <Title> (findings #N, #M)
- **Goal:** ...
- **Target files:** actual paths from Glob/Grep
- **Acceptance criteria:**
  - positive test: ...
  - negative test: ...
- **Scope boundary:** ...
- **Effort:** ...

### WU-2: ...

## Anti-patterns

Patterns that looked right but led to problems. These should become warnings in
AGENTS.md or the relevant skill:
- ...

## What the Machine Got Right

Patterns to preserve and extend — things that worked well and should not be
accidentally degraded by future changes:
- ...
```

## Phase 5.5: Plannable work units

The retro's findings are analytical. To bridge to implementation planning (e.g.,
via `/compound-engineering:ce-plan`), group related findings into coherent work units that a planner
could pick up directly.

For each tier 1 or tier 2 group, produce a work unit block:

```markdown
## Work Units

### WU-1: <Title> (from findings #N, #M, ...)
- **Goal:** One sentence describing the outcome
- **Target files:** Specific file paths in the printing-press repo to modify
  (use Glob/Grep to resolve component names to actual files)
- **Acceptance criteria:** 2-3 concrete, testable scenarios:
  - "Generate from postman-explore spec → sync terminates without manual fix"
  - "Generate from Stripe spec → sync still uses cursor-based pagination (negative test)"
- **Scope boundary:** What this does NOT include
- **Dependencies:** Other work units that must complete first (if any)
- **Estimated effort:** hours/days
```

To resolve target files, actually look at the printing-press repo:

```bash
# Find generator template files
find <repo>/internal/generator -name "*.go" -o -name "*.tmpl" | head -20

# Find where sync code is generated
grep -rl "syncResource\|defaultSyncResources\|determinePaginationDefaults" <repo>/internal/
```

Group related findings into work units when they touch the same files or when
one fix enables another. For example:
- Response envelope unwrapping + pagination detection + sync resource derivation
  → "WU: Data layer generation pipeline"
- Entity-specific store tables + FTS index generation + typed columns
  → "WU: Schema-driven store generation"

A good work unit is something one person could implement in 1-3 days with a clear
definition of done. If a work unit is bigger than that, split it.

## Phase 6: Save and present

### Save locations

Save the retro to two places:

1. **Manuscripts** (ephemeral, tied to the run):
   ```
   $PRESS_MANUSCRIPTS/<api>/<run-id>/proofs/<stamp>-retro-<api>-pp-cli.md
   ```

2. **Repo** (durable, checkable-into-git, readable by future sessions):
   ```bash
   REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
   RETRO_DIR="$REPO_ROOT/docs/retros"
   mkdir -p "$RETRO_DIR"

   # Filename: YYYY-MM-DD-<api>-retro.md
   RETRO_FILE="$RETRO_DIR/$(date +%Y-%m-%d)-<api>-retro.md"
   ```

   The repo copy is the canonical one. It accumulates over time so future retros
   can reference past findings ("this was also flagged in the notion retro on 2026-03-15").

### Present summary

Show the user: top 3 findings, the tier 1 table, and the work units.

### Offer to plan

Check whether `/compound-engineering:ce-plan` is available (the compound-engineering plugin is in
`.claude/settings.json` as a dependency, so it should be — but might not be for
standalone installs). Use `AskUserQuestion` to offer next steps:

> "Retro saved to `docs/retros/<date>-<api>-retro.md`. Found <N> findings across
> <M> work units. Want to plan implementation?"
>
> 1. **Plan Tier 1 work units** — invoke `/compound-engineering:ce-plan` with the retro's Tier 1 work
>    units as input
> 2. **Plan a specific work unit** — pick one WU to plan
> 3. **Done for now** — retro is saved, plan later

If the user picks option 1 or 2, invoke the `compound-engineering:ce:plan` skill
(if that name doesn't resolve, try `compound-engineering:ce-plan` as a fallback)
with a prompt like:

```
Create a plan to improve the printing-press CLI generation system in this repo.
We just generated a CLI for <API> and encountered systemic problems and
opportunities documented in the retro. The retro includes prioritized work units
with target files, acceptance criteria, and scope boundaries:
docs/retros/<date>-<api>-retro.md
[If option 2: Focus on work unit WU-<N>: <title>.]
```

If neither skill name resolves, fall back to:
- Tell the user the retro is saved and they can invoke it manually
- Print the prompt they'd use:
  `/compound-engineering:ce-plan Create a plan to improve the printing-press system given the retro at docs/retros/<file>`

## Rules

- The retro is about the machine, not the CLI. Do not propose fixes to the generated
  CLI.
- Do not add more phases, documents, or gates to the main skill. It's already long.
  Propose making existing phases smarter or the generator emit better defaults.
- Prefer automatic fixes (generator, binary) over instructional fixes (skill).
- For recurring friction, always answer "inherent or fixable?" honestly. Don't
  dismiss friction as inherent without considering alternatives.
- Be honest about what went well. Protecting good patterns is as important as
  fixing bad ones.
- **Resist over-generalization.** You just spent an entire session with one API. Your
  intuition about "every API needs this" is based on a sample size of one. Stress-test
  every finding against other API shapes before claiming broad blast radius. A fix
  that helps proxy-envelope APIs but adds unnecessary complexity to a clean REST API
  is a regression, not an improvement. When in doubt, scope the fix narrowly with
  conditional logic and let future retros widen it if the pattern recurs.
- When a fix only applies to a subclass of APIs, the recommendation must include the
  condition (when to activate) AND the guard (when to skip). A generator change
  without a guard is a blanket change, and blanket changes break simple cases.
- Be thorough. The retro document is a reference for future planning — include
  enough detail that someone reading it months later can understand the finding,
  the tradeoff reasoning, and the proposed fix without needing the original
  conversation.
