---
name: pp-learn-loop-example
description: "Printing Press CLI for Learn Loop Example. Golden fixture exercising the spec-declared self-learning loop."
author: "printing-press-golden"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - learn-loop-example-pp-cli
---

# Learn Loop Example — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `learn-loop-example-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install learn-loop-example --cli-only
   ```
2. Verify: `learn-loop-example-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Golden fixture exercising the spec-declared self-learning loop. Demonstrates
the shape every printed CLI gets when its spec declares a `learn:` block:
the generator emits internal/learn/* subpackages, the teach/recall/learnings
commands, the v3 store schema additions, and the self-learning sections in
README.md / SKILL.md / AGENTS.md.

The underlying resource surface mirrors the sync-walker fixture (top-level
games + walker-fanned-out leagues) so the emitted shape covers the typical
multi-file CLI alongside the learn package. Identifiers in the learn block
are intentionally neutral (EXAMPLE-* ticker, ALPHA/BRAVO entities) so the
scripts/verify-learn-purity.sh gate cannot trip on this fixture.


## When Not to Use This CLI

Do not activate this CLI for requests that require creating, updating, deleting, publishing, commenting, upvoting, inviting, ordering, sending messages, booking, purchasing, or changing remote state. This printed CLI exposes read-only commands for inspection, export, sync, and analysis.

## Command Reference

**games** — Top-level games resource. The list endpoint populates the generic resources table; rows carry a `game_key` field that the walker's leagues endpoint extracts for child fan-out.

- `learn-loop-example-pp-cli games` — List games

**leagues** — Leagues, fetched per-game by walking games and extracting each game's game_key into the child path.

- `learn-loop-example-pp-cli leagues <game_key>` — List leagues for a game


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
learn-loop-example-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Auth Setup

Run `learn-loop-example-pp-cli auth setup` for the URL and steps to obtain a token (add `--launch` to open the URL). Then store it:

```bash
learn-loop-example-pp-cli auth set-token YOUR_TOKEN_HERE
```

Or set `LEARN_LOOP_TOKEN` as an environment variable.

Run `learn-loop-example-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  learn-loop-example-pp-cli games --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Read-only** — do not use this CLI for create, update, delete, publish, comment, upvote, invite, order, send, or other mutating requests

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Automatic learning

This CLI ships a self-learning loop: agents `recall` before doing discovery walks, and fire `teach` in the background after answering. Repeat questions skip discovery; structurally similar questions get answered via entity substitution.

### Four-branch protocol

For each user question:

1. **Known taught pattern.** If `recall "<question>"` returns `found=true` with `results[0].entity_match == "exact"` and `results[0].confidence >= 2`, skip discovery and fetch the resources in `results[*].resource_id` directly.
2. **Pattern match via entity substitution.** If recall misses but the query contains an entity that matches a registered seed (taught lookup or pre-seeded list), the loop's pattern engine tries substituting the entity into a stored template. If `Apply` succeeds, use the resolved resource id.
3. **Cold start.** If both miss, run the normal discovery walk (search, list, drill). Once you've assembled the answer, fire `teach &` to record the mapping for next time.
4. **Verification failure after a pattern apply.** If the pattern engine produced a candidate that turns out wrong when you fetch it, demote back to branch 3: re-run discovery and `teach` the correct resource so the next call doesn't repeat the mistake.

Always read `warnings` on the recall envelope before trusting `results`. A non-empty `warnings` array means the validator flagged something (low confidence, parent-vs-child mismatch, store gap) and the row is a hint, not a skip-discovery hit.

### Commands

```bash
learn-loop-example-pp-cli recall "<user's question>" --agent
learn-loop-example-pp-cli teach --query "<user's question>" --resource-type <type> --resource <id> &
learn-loop-example-pp-cli learnings list --agent
learn-loop-example-pp-cli learnings forget "<query>" --resource <id>
learn-loop-example-pp-cli teach-pattern --query-template "<template>" --resource-template "<template>" --resource-type <type>
learn-loop-example-pp-cli teach-lookup --kind <kind> --canonical "<display>" --value <substitution>
```

`teach` is silent on success and safe to background with `&` — append it right before emitting the user-facing response so the write doesn't block. Errors only land in `~/.local/share/learn-loop-example-pp-cli/teach.log`.

### Disabling learning

- `--no-learn` on a single command short-circuits both `recall` and the `teach` write path. Use for deterministic agent flows or tests that must not be affected by accumulated learnings.
- `LEARN_LOOP_EXAMPLE_NO_LEARN=true` in the environment globally disables the pipeline.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
learn-loop-example-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
learn-loop-example-pp-cli feedback --stdin < notes.txt
learn-loop-example-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.local/share/learn-loop-example-pp-cli/feedback.jsonl`. They are never POSTed unless `LEARN_LOOP_EXAMPLE_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `LEARN_LOOP_EXAMPLE_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
learn-loop-example-pp-cli profile save briefing --json
learn-loop-example-pp-cli --profile briefing games
learn-loop-example-pp-cli profile list --json
learn-loop-example-pp-cli profile show briefing
learn-loop-example-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `learn-loop-example-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add learn-loop-example-pp-mcp -- learn-loop-example-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which learn-loop-example-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   learn-loop-example-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `learn-loop-example-pp-cli <command> --help`.
