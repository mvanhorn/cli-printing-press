---
name: printing-press-reprint
description: >
  Regenerate an existing printed CLI from scratch under the current Printing
  Press, with prior research and prior novel features carried into the
  novel-features subagent's reprint reconciliation rather than dropped on
  the floor. Pulls the CLI from the public library if it isn't local,
  recommends reuse-vs-redo of prior research based on age, then hands off
  to /printing-press with the right context. Use when a machine upgrade
  would benefit a published CLI more than manual polish.
  Trigger phrases: "reprint <api>", "regenerate <api>", "redo the <api> CLI",
  "rebuild <api> from scratch", "this CLI would benefit from a reprint".
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
  - Skill
---

# /printing-press-reprint

Regenerate an existing printed CLI under the current machine. The user gives
a CLI name and (optionally) reasons for the reprint. This skill ensures the
prior CLI is locally present, recommends whether to reuse or redo prior
research, and hands off to `/printing-press` with the context the
novel-features subagent needs to reconcile prior features against the
current machine — keep, reframe, or drop with reasons, never silent.

```bash
/printing-press-reprint notion
/printing-press-reprint cal.com  the new MCP intent surface landed and the prior CLI ships endpoint-mirror only
/printing-press-reprint allrecipes
```

## When to run

- A significant Printing Press upgrade (new MCP surface, new auth modes, new
  transport, scoring rubric changes) would lift this CLI more than manual
  polish.
- The published CLI ships with a known systemic gap a reprint would fix.
- The user wants prior novel features re-evaluated against the current
  machine and current personas, not carried forward verbatim.

For one-off code-quality fixes, prefer `/printing-press-polish` — it doesn't
redo research or rebuild the manuscript.

## Setup

```bash
PRESS_HOME="$HOME/printing-press"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
```

## Phase A — Resolve and reconcile presence

Resolve the user's argument the same way `/printing-press-import` does:
fetch the public library `registry.json` once, then exact → normalized →
fuzzy match. The argument can be an API slug (`notion`), a brand name
(`cal.com`), an old `<api>-pp-cli` form, or close enough.

Then check what exists locally and reconcile against the public library by
reading both provenance manifests' `run_id` and `generated_at`:

| Local | Public registry | Action |
|-------|-----------------|--------|
| absent | absent | STOP — nothing to reprint; suggest `/printing-press <api>` for a fresh print |
| absent | present | invoke `/printing-press-import <api>`, then continue |
| present | absent | continue — never-published local CLI; skip import |
| present, same `run_id` | present | continue without import |
| present, public newer `generated_at` | present | offer import via `AskUserQuestion`; user decides |
| present, local newer `generated_at` | present | STOP — local has unpublished work; tell user to publish or discard first |

When invoking `/printing-press-import`, let it own backup, overwrite,
build-verify, and module-path-rewrite. Wait for it to return clean before
continuing.

## Phase B — Verify prior research is reconcilable

Confirm the two paths the novel-features subagent checks for prior research
are populated:

```bash
LIB_TARGET="$PRESS_LIBRARY/$API_SLUG"
LIB_RESEARCH="$LIB_TARGET/research.json"
MAN_RESEARCH=$(ls -1t "$PRESS_MANUSCRIPTS/$API_SLUG"/*/research.json 2>/dev/null | head -1)
```

If neither exists, the published CLI predates `research.json` provenance.
The subagent will treat the run as a first print and Pass 2(d) reprint
reconciliation will not fire — there is nothing for it to read. Surface
this and ask:

> Published `<api>` was built before `research.json` provenance landed.
> Without it, the novel-features subagent will treat this as a first
> print — there is nothing to reconcile against. Continue as a degraded
> reprint (essentially a fresh print with a kept binary name)?

If the user declines, exit. If they continue, record the absence so the
hand-off prompt notes that this is a degraded reprint.

## Phase C — Recency recommendation

Pull `researched_at` from the most-recent prior `research.json` and
`printing_press_version` + `generated_at` from `.printing-press.json`:

```bash
RESEARCHED_AT=$(jq -r '.researched_at // empty' "$MAN_RESEARCH" 2>/dev/null)
PRESS_VERSION=$(jq -r '.printing_press_version // empty' "$LIB_TARGET/.printing-press.json" 2>/dev/null)
GENERATED_AT=$(jq -r '.generated_at // empty' "$LIB_TARGET/.printing-press.json" 2>/dev/null)
```

Compute the calendar age of the research with `python3` so it stays portable
across macOS/Linux and tolerates the microsecond precision that
`generated_at` carries (BSD `date -f` rejects fractional seconds; `python3`
is on every supported platform):

```bash
AGE_DAYS=$(python3 -c "
from datetime import datetime, timezone
ts = '$RESEARCHED_AT'.replace('Z', '+00:00')
print(int((datetime.now(timezone.utc) - datetime.fromisoformat(ts)).total_seconds() // 86400))
" 2>/dev/null)
```

Surface both signals to the user — research age and prior machine version.
Age thresholds are rules of thumb, not gates:

- under 30 days → reuse looks safe
- 30–120 days → reuse plausible; the user should mention any known API
  churn in their reprint reason so the subagent's Pass 2 picks it up
- over 120 days → redo recommended

Don't predict API churn from age alone — describe the signals and let the
user override. The Phase 0 binary-version-bump revalidation in
`/printing-press` handles the machine-delta side independently; don't
duplicate it here.

Ask via `AskUserQuestion`:

1. **Reuse prior research** — keep the prior brief; the subagent re-scores
   prior novel features against current personas
2. **Redo research** — re-run Phase 1 from scratch; the subagent still
   ingests prior novel features as Pass 2(d) input
3. **Show me first** — display the prior brief's headline + novel-features
   list, then re-ask between options 1 and 2

## Phase D — Hand off to `/printing-press`

Invoke `/printing-press <api>` and bundle three things into the prompt:

1. **A header line** stating the user already chose to regenerate, so
   Phase 0's library-check should select "Generate a fresh CLI" and not
   re-prompt fresh-vs-improve.
2. **Research mode** from Phase C (`reuse` or `redo`). Phase 0's existing
   reuse logic consumes this.
3. **The user's freeform reprint reason**, verbatim, in a `User context`
   block. This propagates into the brief as `## User Vision` and becomes
   Pass 2(e) input to the novel-features subagent — the right hook for
   "I want better MCP support" → bias the brainstorm accordingly.

Do **not** pass a separate "this is a reprint" marker. The novel-features
subagent runs unconditionally on every print and discovers prior research
via its own discovery snippet (see
`skills/printing-press/references/novel-features-subagent.md`). The paths
import populated in Phase A are exactly the paths it checks; Pass 2(d)
fires whenever prior `research.json` exists.

## After hand-off

The printing-press flow drives the rest. Don't summarize its work — let
the user see the live phases.

If `/printing-press` halts with the subagent's pre-flight HALT (brief
lacks concrete `Users` / `Top Workflows` content), the reused prior brief
predates the subagent's required schema. Recommend re-running with **Redo
research** selected at Phase C.
