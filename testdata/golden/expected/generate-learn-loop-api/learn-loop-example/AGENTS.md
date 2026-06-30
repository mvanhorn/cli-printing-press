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

## Release Ledger

`CHANGELOG.md` and `.printing-press-release.json` are the public library's per-CLI release ledger. Fresh prints may carry blank skeletons, but the final `YYYY.M.N` CLI release version is assigned only after a publish PR merges in `mvanhorn/printing-press-library`. Do not hand-bump those files or edit `var version = ...` for release bookkeeping; preserve existing ledger files on reprint and let the library workflow stamp the next release.

## Local Customizations

This directory is **generated output** -- a fresh print can overwrite the whole tree, so ad-hoc hand-edits don't survive on their own. If you modify the generated code, record each change under `.printing-press-patches/` (parallel to `.printing-press.json`) so a regen carries the intent forward instead of silently dropping it.

The entry shape, and the altitude to write it at -- a durable reprint-guard, not a changelog -- live in the public library's `AGENTS.md`, which is the single source of truth; this guide intentionally doesn't duplicate them.
