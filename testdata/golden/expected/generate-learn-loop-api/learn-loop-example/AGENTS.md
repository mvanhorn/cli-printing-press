# Learn Loop Example Printed CLI Agent Guide

This directory is a generated `learn-loop-example-pp-cli` printed CLI. It was produced by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press), so treat systemic fixes as upstream Printing Press fixes first. Keep local edits narrow and document why a generated-tree patch belongs here.

## Local Operating Contract

Start by asking the generated CLI for current runtime truth:

```bash
learn-loop-example-pp-cli doctor --json
learn-loop-example-pp-cli agent-context --pretty
```

Use runtime discovery instead of relying on a copied command list:

```bash
learn-loop-example-pp-cli which "<capability>" --json
learn-loop-example-pp-cli <command> --help
```

Add `--agent` to command invocations for JSON, compact output, non-interactive defaults, no color, and confirmation-safe scripting:

```bash
learn-loop-example-pp-cli <command> --agent
```

Before running an unfamiliar command that may mutate remote state, inspect its help and prefer a dry run:

```bash
learn-loop-example-pp-cli <command> --help
learn-loop-example-pp-cli <command> --dry-run --agent
```

Use `--yes --no-input` only after the target, arguments, and side effects are clear.

## Self-Learning Loop

This CLI ships with a teach/recall learning loop backed by the local SQLite store. The agent flow is:

1. On a new user question, call `learn-loop-example-pp-cli recall "<question>" --agent` FIRST. If `found=true` and the top result has `entity_match == "exact"` and `confidence >= 2`, skip discovery and go straight to the live fetch for the returned resource IDs.
2. On a cold miss, run normal discovery; on the way out, fire `learn-loop-example-pp-cli teach --query "<question>" --resource <id> --resource-type <type> &` so the next equivalent question short-circuits.
3. Use `learn-loop-example-pp-cli learnings list --agent` to inspect what's been taught, and `learn-loop-example-pp-cli learnings forget "<question>" --resource <id>` to undo a bad teach.
4. `teach-pattern` and `teach-lookup` install manual generalization rules when one teach should cover a whole family (e.g. one country alias unlocks every per-country query). Both are write commands and have no MCP read-only annotation.

`recall` and `learnings list` carry `mcp:read-only=true`; `teach`, `learnings forget`, `teach-pattern`, and `teach-lookup` write the local store and are unannotated (default to "may write").

Disable the loop with `--no-learn` per-invocation or `LEARN_LOOP_EXAMPLE_NO_LEARN=true` for the whole session — useful for deterministic agent flows that don't want a learning row to silently change subsequent query results.

For install, auth, examples, and longer product guidance, read `README.md` and `SKILL.md`. This file intentionally stays small so repo-local agents get invariant local guidance without duplicating the generated docs.

## Local Customizations

If you modify this CLI beyond what the generator produced, record each customization in a `.printing-press-patches.json` at this CLI's root (parallel to `.printing-press.json`) so the change isn't lost on the next regen and is visible to the next reader.

Minimum shape:

```json
{
  "schema_version": 1,
  "applied_at": "YYYY-MM-DD",
  "base_run_id": "<copy from .printing-press.json>",
  "base_printing_press_version": "<copy from .printing-press.json>",
  "patches": [
    {
      "id": "short-identifier",
      "summary": "What changed (one sentence).",
      "reason": "Why this customization was needed (one or two sentences).",
      "files": ["internal/cli/foo.go"],
      "validated_outcome": "Optional: non-obvious test result that confirms the fix."
    }
  ]
}
```

Use `deferred_to_upstream` when a local patch is a temporary bridge for a missing public API endpoint, an unofficial-host workaround, a live response-shape drift, or behavior the Printing Press should eventually generate correctly. Search `mvanhorn/cli-printing-press` issues first; reuse a matching issue or open one, then set `upstream_issue` so the next regen knows what must supersede the patch:

```json
{
  "id": "temporary-bridge",
  "summary": "What changed (one sentence).",
  "reason": "Why this customization was needed (one or two sentences).",
  "files": ["internal/cli/foo.go"],
  "validated_outcome": "Optional: non-obvious test result that confirms the fix.",
  "deferred_to_upstream": [
    {
      "feature": "Generator behavior or upstream API capability that should eventually supersede this patch",
      "reason": "Why the local patch is temporary or API-specific"
    }
  ],
  "upstream_issue": "https://github.com/mvanhorn/cli-printing-press/issues/<n>"
}
```

This file is an **index of customizations**, not a second copy of the diff. Diffs live in `git`; the manifest is what tells the next agent (or regeneration tooling) what was customized and why. Keep `summary` and `reason` short -- if you find yourself writing tables of field renames or code transformations, that detail belongs in the commit message, not here.

Inline `// PATCH:` source comments are optional. If you find them helpful as a navigation aid (`grep -rn 'PATCH' .` surfaces customized sites), feel free to add them -- but they aren't required and aren't enforced by any CI.
