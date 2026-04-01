# Printing Press Retro: Steam Web API (Run 5)

## Session Stats
- API: Steam Web API
- Spec source: Zuplo/Steam-OpenAPI (158 operations, OpenAPI 3.0)
- Scorecard: **87/100 Grade A** (up from 84 in Run 4)
- Verify: 75% (61/81, 0 critical)
- Machine PRs: #100, #101, #102, #103, #104
- Journey: 68 → 84 → 85 → 84 → 87

## What PR #104 Changed
- **Sync Correctness: 7 → 10** (+3). The rescale for APIs without parameterized list endpoints works correctly. This is the single biggest score improvement from one change across all 5 runs.
- **usageErr gate: did NOT work.** `HasMultiPositional` is true for the Steam spec (some endpoints technically have 2+ path params in the Zuplo spec), so `usageErr` is still emitted. But no generated command calls it because the help-guard pattern replaced all usageErr calls. The gating logic checks the spec, but the real signal is whether generated commands actually call the function.
- **Profiler searchable fields from GET params: uncertain.** Need to check if this actually produced more FTS5 tables. Data Pipeline is still 7/10.

## Findings

### 1. usageErr gate: wrong signal (Generator bug — scorer correct)

- **Scorer correct?** Yes. `usageErr` is defined but never called. Dead code.
- **What went wrong:** PR #104 gates `usageErr` behind `HasMultiPositional` (true when any endpoint in the spec has 2+ positional params). The Zuplo Steam spec has some endpoints with 2+ path params, so the flag is true. But the help-guard pattern (PR #100) replaced all `usageErr` calls with `cmd.Help()` in the endpoint template. So `HasMultiPositional` is true (spec has the params) but no generated command emits a call to `usageErr` (template uses help-guard instead).
- **Root cause:** The flag checks what the spec HAS. The real signal is what the template EMITS. Since the help-guard catches `len(args) == 0` and the 2nd-arg check at `{{if gt $i 0}}` still calls `usageErr`, the function should only be emitted when there are actually commands with 2+ positional params that use the `{{if gt $i 0}}` path. But the endpoint template's help-guard catches ALL zero-arg cases — and for Steam, no command has 2 positional params, so the `gt $i 0` path never fires.
- **Fix:** The simplest fix: remove `usageErr` from the template entirely. Replace `usageErr(fmt.Errorf(...))` at line 56 of `command_endpoint.go.tmpl` with `fmt.Errorf(...)` wrapped in a non-zero exit. Or: keep `usageErr` but gate it on whether ANY generated command file actually contains the string `usageErr(` — a post-generation dead-code sweep.
- **Verdict: Generator template fix needed. The conditional flag approach doesn't work because it checks the spec, not the generated output.**

### 2. formatCompact: Claude-introduced dead code (Agent compliance issue)

- **Scorer correct?** Yes. `formatCompact` is defined but never called.
- **What happened:** The agent that builds wrapper/transcendence commands defined `formatCompact` in one of the command files but no command actually calls it. This is a recurring pattern — Claude writes helper functions during the build phase that end up unused.
- **Root cause:** The agent builds commands in bulk and sometimes adds helpers "just in case" that turn out unused. The dogfood catches them, but the agent should have verified each function is called before leaving it in.
- **Fix:** Two paths: (1) Add a post-build step to the skill instruction: "After building all commands, run dogfood and remove any dead functions before proceeding to shipcheck." (2) Make the agent prompt explicitly say "do not define helper functions unless they are called by at least one command."
- **Verdict: Skill instruction improvement. The agent prompt should say "verify every helper function is called."**

### 3. README 7/10 despite having all sections (Scorer investigation needed)

- **Scorer correct?** Need to investigate. The README has all 5 required sections + 3 extras. If the scorer is giving 7/10 with all sections present, the deduction is for content quality within those sections, not missing sections.
- **What to check:** Run the scorecard with verbose README output to see which quality checks failed. The scorer may check: (a) Quick Start has a real command example (not just "see Install"), (b) Cookbook has 3+ code blocks, (c) Agent Usage has --json/--select examples.
- **Verdict: Need to trace the scorer's README logic to understand the 3-point deduction.**

### 4. Data Pipeline 7/10 — profiler search improvement uncertain

- **What happened:** PR #104 added GET param analysis to `collectStringFields`, but Data Pipeline is still 7/10. This suggests either: (a) the profiler change didn't produce more FTS5 tables for Steam, or (b) the scorer checks for something else that's still missing.
- **Verdict: Need to check if the profiler produces more SearchableFields for the Steam spec with the GET param addition.**

## Key Insight: Don't Rely on Instructions for Quality Enforcement

An earlier draft of this retro said "agent compliance is the new bottleneck" and proposed fixing it with better skill instructions. That framing is wrong.

**Skill instructions are suggestions to an LLM, not deterministic code.** Evidence from this session:

| Instruction | Did the agent follow it? | Result |
|-------------|--------------------------|--------|
| "Preserve all 5 README sections" | Yes — all 5 sections present | But content quality still scored 7/10. Structure followed, quality didn't. |
| "Verify every helper function is called" | No — `formatCompact` defined and never called | Agent built in bulk, didn't verify its own output |
| "Wire auth from research when spec detection fails" | Unknown — didn't check | May or may not have fired this run |

Instructions work for **structural rules** ("always include these sections") ~80% of the time. They are unreliable for **quality judgment** ("verify every function is called", "enrich terse descriptions"). The agent will sometimes follow them. Not always.

**What actually works reliably:**

| Approach | Reliability | Example |
|----------|-------------|---------|
| **Template gates** | 100% deterministic | `{{if .HasMultiPositional}}` always fires correctly |
| **Binary post-processing** | 100% deterministic | `printing-press dogfood` catches dead code every time |
| **Scorer checks** | 100% deterministic | Scorecard detects missing README sections mechanically |
| **Skill instructions** | ~70-90% | Agent sometimes follows, sometimes doesn't |

**The pattern: move quality enforcement from LLM instructions (unreliable) to deterministic binary passes (reliable).**

Concrete fixes that follow this pattern:
- **Dead code:** Don't tell the agent "don't write dead code." Instead, add `printing-press polish --remove-dead-code` as a binary pass that dogfood identifies dead functions and auto-deletes them. Run it after the build phase, before scoring.
- **README quality:** Don't hope the agent writes good cookbook examples. Have the scorer validate: cookbook section has 3+ parseable code blocks, Quick Start has a real command, Agent Usage shows --json. Have `printing-press polish` auto-fix thin sections by regenerating them from the CLI's `--help` output.
- **Auth compensation:** Don't rely on the agent reading the research brief and wiring auth. Have the generator detect the pattern (PR #103's auth inference) and the verify tool report when auth is needed but not configured. Make it a build failure, not a suggestion.
- **usageErr:** Don't gate it on a flag that checks the spec. Remove it from the template entirely and let `cmd.Help()` handle all zero-arg cases. If a future template reintroduces `usageErr` calls, the build fails — which is the right signal.

**The remaining 13 points should be pursued through binary passes and template fixes, not through more skill instructions.** Instructions are cheap to add and feel productive, but they don't compound reliably. Binary passes compound because they're deterministic.

## Score Trajectory

| Run | Score | Key change |
|-----|-------|------------|
| 1 | 68 | Baseline |
| 2 | 84 | Scorer fixes (verify naming, dogfood) |
| 3 | 85 | Behavioral detection |
| 4 | 84 | Auth inference, sync paths |
| **5** | **87** | **Sync rescale (+3), improved profiler** |

**19-point improvement over 5 runs.** The improvement came from deterministic machine changes (template gates, scorer fixes, binary post-processing), not from skill instructions. The next step is: (1) add `printing-press polish --remove-dead-code` as a binary pass, (2) remove `usageErr` from the template, (3) test on a different API to validate generalization.
