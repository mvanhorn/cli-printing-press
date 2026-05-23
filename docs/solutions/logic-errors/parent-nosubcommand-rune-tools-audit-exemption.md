---
title: "Tools-audit parent grouper exemptions must recognize generated sentinels"
date: 2026-05-23
category: logic-errors
module: internal/cli
problem_type: logic_error
component: tooling
symptoms:
  - "Generated parent commands with parentNoSubcommandRunE triggered thin-short findings even though they only route users to subcommands"
  - "Polish runs repeatedly accepted or rewrote parent-group Short text instead of fixing the scorer boundary"
  - "Real hand-authored RunE commands still needed thin-short and missing-read-only findings"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - tools-audit
  - scorecard
  - cobra
  - parent-grouper
  - mcp
---

# Tools-audit parent grouper exemptions must recognize generated sentinels

## Problem

`tools-audit` treated any Cobra command literal with `Run` or `RunE` as an actionable shell-out command. Generated parent groupers now use `RunE: parentNoSubcommandRunE(flags)` so invoking the parent without a subcommand returns a structured "subcommand required" envelope. That sentinel made parent containers look like ordinary commands, causing `thin-short` findings on boilerplate Shorts such as `Manage mints`.

## Symptoms

- Generated parent groupers surfaced `thin-short` findings even when their leaf subcommands carried the actionable descriptions.
- Polish runs spent time accepting repeated parent findings or hand-editing generated files that regen would overwrite.
- The old exemption model, "no RunE means parent grouper", no longer matched generated Cobra source.

## What Didn't Work

- Rewriting generated parent Shorts during polish only fixed one printed CLI until the next regen.
- Broadly exempting all `RunE` commands would hide real hand-authored shell-out tools with poor descriptions or missing `mcp:read-only` annotations.
- Relying on runtime command behavior alone was not enough for `tools-audit`, which works from source literals and needs to distinguish the generated sentinel from ordinary closures.

## Solution

Preserve the sentinel identity while extracting Cobra command fields:

```go
case "Run", "RunE":
    f.hasRunE = true
    if key.Name == "RunE" && isParentNoSubcommandRunE(kv.Value) {
        f.hasParentNoSubcommandRunE = true
    }
```

Then exclude only that generated parent sentinel from the Cobra-side audit eligibility check. Keep ordinary `Run` and `RunE` command literals in scope so `thin-short` and `missing-read-only` still fire for real shell-out commands.

The regression coverage should include both layers:

- An extraction test proving `RunE: parentNoSubcommandRunE(flags)` sets the sentinel flag.
- A source-level audit test proving the sentinel parent emits no findings while a parsed real `RunE: func(...)` command with the same thin Short still emits `thin-short` and `missing-read-only`.

## Why This Works

The scorer exemption is tied to the generator-owned parent-grouper sentinel instead of the broad presence or absence of `RunE`. That matches the actual source shape that caused the false positive while preserving the audit for hand-authored commands where the Cobra Short and annotations are still the agent-facing quality surface.

## Prevention

- When generator code changes a parent command from non-runnable to sentinel-runnable, update source-based scorers that previously keyed on `RunE == nil`.
- Pair every audit exemption with a negative test for a nearby real command shape so the exemption cannot silently widen.
- Keep polish playbooks aligned with scorer skip rules; otherwise agents will keep accepting or rewriting findings that the machine now owns.

## Related Issues

- GitHub issue #1565
- `docs/solutions/logic-errors/cobratree-framework-command-depth-parity.md`
- `docs/solutions/logic-errors/reachable-scorer-surfaces-non-rest-clis-2026-05-21.md`
- `docs/solutions/security-issues/mcp-sql-search-readonly-bypass-2026-05-08.md`
