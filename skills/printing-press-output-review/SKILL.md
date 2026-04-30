---
name: printing-press-output-review
description: >
  Internal sub-skill: agentic review of a printed CLI's sampled command output for
  plausibility issues that rule-based checks can't encode (substring-match
  relevance, format bugs, silent source drops, ranking failures). Invoked via the
  Skill tool by main printing-press SKILL.md (Phase 4.85) and printing-press-polish
  SKILL.md during the diagnostic loop. Not for direct user invocation — its
  actionable wrappers are /printing-press and /printing-press-polish.
user-invocable: false
allowed-tools:
  - Bash
  - Agent
---

# printing-press-output-review (internal)

Review the sampled outputs from a printed CLI for plausibility bugs that dogfood, verify, and the rule-based `scorecard --live-check` rules can't catch. Wave B policy: all findings surface as warnings, never errors.

This skill is **internal-only** (`user-invocable: false`). It's invoked by parents — main printing-press skill at shipcheck Phase 4.85, polish skill during its diagnostic loop. Running it standalone would produce floating findings text with no ship verdict, no fixes applied, no publish offer; the actionable wrappers are `/printing-press` and `/printing-press-polish`. The skill carries `context: fork` so the reviewer agent's diagnostic chatter stays isolated from the calling skill's context.

## Input

The caller passes `$CLI_DIR` as the argument: an absolute path to the printed CLI's working directory.

## What this catches

Bugs that rule-based checks miss, typically surfaced by 5 minutes of hands-on testing but slipping past dogfood, verify, and `scorecard --live-check` rules:

- Substring-match results that coincidentally contain the query but don't match semantically (e.g., a query matches a substring of a larger unrelated term)
- Aggregation commands silently dropping sources when only some of the requested N come back
- Ranking or sort commands returning top-N results that aren't plausibly the best for the query (broken weights, extractor fallbacks)
- URLs in output pointing at category index pages, feed endpoints, or random-selector routes rather than canonical content permalinks
- Format bugs the rule-based layer doesn't catch (mojibake, inconsistent pluralization, truncated/wrapped cell content)

## Procedure

### Step 1: Gather sample data

```bash
printing-press scorecard --dir "$CLI_DIR" --live-check --json > /tmp/output-review-livecheck.json 2>&1 || true
```

If the scorecard call fails or `/tmp/output-review-livecheck.json` is empty, return the SKIP result (Step 3) without dispatching the reviewer.

### Step 2: Dispatch the reviewer agent

Use the Agent tool (general-purpose) with this prompt contract:

> Review the sampled outputs from the shipped CLI at `$CLI_DIR`. You have these ground-truth sources:
>
> - Sampled command output: read `/tmp/output-review-livecheck.json` and inspect the `live_check.features[]` array. Each entry has the command, example invocation, actual stdout (in `output_sample`, bounded to ~4 KiB), the pass/fail reason, and a `warnings` array (populated by rule-based checks like the raw-HTML-entity detector).
> - **Review only `status: pass` entries.** Entries with `status: fail` either crashed, timed out, or had placeholder args (`<id>`, `<url>`) that never produced real output — their sample is empty and there's nothing for you to judge. Phase 5 dogfood handles test-coverage and exit-code concerns.
> - `$CLI_DIR/research.json` `novel_features` (planned behavior per feature) and `novel_features_built` (verified built commands).
> - The CLI binary at `$CLI_DIR/<cli-name>-pp-cli` — you may invoke additional commands to gather more output when a finding needs verification.
>
> For each of these checks, report findings under 50 words each. Only report issues a human user would notice in 5 minutes of hands-on testing — not every edge case a thorough QA pass might find:
>
> 1. **Output *semantically* matches query intent.** For sampled novel features with a query argument, judge relevance beyond what the mechanical query-token check in live-check already enforced. A feature that passed live-check's `outputMentionsQuery` test still contains *some* query token somewhere — but "buttermilk" appearing as a substring of "butter" results, or "brownies" returning a chili recipe because the extractor fell back to adjacent content, both slip past the mechanical check. Only flag when a human user would look at the top results and say "this isn't what I asked for." Skip this check when the example has no query argument.
> 2. **No obvious format bugs.** Does the output contain raw HTML entities, mojibake (question marks or replacement chars in titles), or malformed URLs (pointing at category index pages, feed endpoints, or random-selector routes rather than canonical content permalinks)? Rule-based live-check catches numeric entities; this layer catches the broader class.
> 3. **Aggregation commands show all requested sources.** For commands with a `--source`/`--site`/`--region` CSV flag: if the user requested N sources, does output show N, or does stderr explain the missing ones? Silent drops of failed sources are a top failure mode for fan-out commands.
> 4. **Result ordering/ranking makes sense.** For commands that claim to rank or sort, does the top result look plausibly best given the query? Watch for broken score weights, off-by-one sort bugs, and silent fallback to recency when relevance computation fails.
>
> Return a list of findings. For each: check name, severity (`warning` in Wave B; `error` reserved for Wave C), one-line description, one-sentence fix suggestion. If the CLI passes all four checks, return "PASS — no findings."

### Step 3: Emit the structured result block

End the skill response with a `---OUTPUT-REVIEW-RESULT---` block the parent parses:

**On clean pass:**

```
---OUTPUT-REVIEW-RESULT---
status: PASS
findings: []
---END-OUTPUT-REVIEW-RESULT---
```

**On warnings:**

```
---OUTPUT-REVIEW-RESULT---
status: WARN
findings:
- check: <check-name>
  severity: warning
  description: <one-line>
  suggestion: <one-sentence>
- ...
---END-OUTPUT-REVIEW-RESULT---
```

**On reviewer failure (timeout, agent-budget exhaustion, missing live-check data):**

```
---OUTPUT-REVIEW-RESULT---
status: SKIP
reason: <one-line description>
findings: []
---END-OUTPUT-REVIEW-RESULT---
```

## Wave B policy (current)

- All findings surface as `warning` — never `error`. Shipcheck proceeds regardless.
- The caller logs findings to the run's artifact directory (e.g., `manuscripts/<api>/<run>/proofs/phase-4.85-findings.md`) and surfaces them to the user. Findings are not persisted to `scorecard.json` — that path is reserved for Wave C.
- The user decides case by case whether to fix before shipping.

**Non-interactive contract (CI, cron, batch regeneration):**

- If stdout is not a TTY, callers follow fail-open-with-log: findings recorded, shipcheck proceeds without prompting.
- `status: SKIP` (reviewer crash, timeout, missing data) is informational — shipcheck does not block on it.
- No `--auto-approve-warnings` flag yet. The policy is already "warnings don't block" in Wave B, so the flag has no effect to gate.

Wave C (separate future PR) will flip `error`-severity findings to blocking after calibration data across the library shows false-positive rate below 10%.

## Why agentic vs template-only

Output-plausibility questions are not pattern-matchable against source. Rule-based live-check rules cover what regexes can (numeric HTML entities, query-token absence). Everything else — "are these substitution results plausibly correct for the query?", "does the top search result look related?" — is an LLM-shaped question. The token cost is bounded (once per run, not per command) and the catch rate against the bug classes that motivated this phase justifies the dispatch.

## Known blind spots

- Can't verify numeric accuracy (prices, ratings, rankings vs ground-truth). If the CLI says a recipe has 4.8 stars and it actually has 4.2, this skill won't catch it.
- Can't detect data-freshness issues (recipe published 2019 vs 2024). These need live comparison against authoritative sources.
- Can't judge subjective preferences ("is this the *best* recipe for chocolate chip cookies?").
- Sampled outputs only — covers the commands in `live_check.features[]`. Full command-tree coverage belongs in Phase 5 dogfood.
- Non-English output: the reviewer's query-intent check assumes English-language query/output. For non-English CLIs, calibrate the prompt separately.
