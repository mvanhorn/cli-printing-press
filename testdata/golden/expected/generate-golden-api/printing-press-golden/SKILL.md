---
name: pp-printing-press-golden
description: "Printing Press CLI for Printing Press Golden. Purpose-built fixture for golden generation coverage."
author: "printing-press-golden"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - printing-press-golden-pp-cli
---

# Printing Press Golden — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `printing-press-golden-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install printing-press-golden --cli-only
   ```
2. Verify: `printing-press-golden-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Purpose-built fixture for golden generation coverage.

## Command Reference

**currencies** — Manage currencies

- `printing-press-golden-pp-cli currencies` — List supported currencies

**projects** — Manage projects

- `printing-press-golden-pp-cli projects create` — Create project
- `printing-press-golden-pp-cli projects get` — Get project
- `printing-press-golden-pp-cli projects list` — List projects

**public** — Manage public

- `printing-press-golden-pp-cli public` — Get public service status

**reports** — Manage reports



## Freshness Contract

This printed CLI owns bounded freshness only for registered store-backed read command paths. In `--data-source auto` mode, those paths check `sync_state` and may run a bounded refresh before reading local data. `--data-source local` never refreshes. `--data-source live` reads the API and does not mutate the local store. Set `PRINTING_PRESS_GOLDEN_NO_AUTO_REFRESH=1` to skip the freshness hook without changing source selection.

Covered paths:

- `printing-press-golden-pp-cli currencies`
- `printing-press-golden-pp-cli currencies get`
- `printing-press-golden-pp-cli currencies list`
- `printing-press-golden-pp-cli currencies search`
- `printing-press-golden-pp-cli projects`
- `printing-press-golden-pp-cli projects get`
- `printing-press-golden-pp-cli projects list`
- `printing-press-golden-pp-cli projects search`

When JSON output uses the generated provenance envelope, freshness metadata appears at `meta.freshness`. Treat it as current-cache freshness for the covered command path, not a guarantee of complete historical backfill or API-specific enrichment.

### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
printing-press-golden-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Auth Setup
Run `printing-press-golden-pp-cli auth setup` to print the URL and steps for getting a key (add `--launch` to open the URL). Then set:

```bash
export PRINTING_PRESS_GOLDEN_API_KEY="<your-key>"
```

Or persist it in `~/.config/printing-press-golden-pp-cli/config.toml`.

Run `printing-press-golden-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  printing-press-golden-pp-cli currencies --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Explicit retries** — use `--idempotent` only when an already-existing create should count as success

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
printing-press-golden-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
printing-press-golden-pp-cli feedback --stdin < notes.txt
printing-press-golden-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.local/share/printing-press-golden-pp-cli/feedback.jsonl`. They are never POSTed unless `PRINTING_PRESS_GOLDEN_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `PRINTING_PRESS_GOLDEN_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

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
printing-press-golden-pp-cli profile save briefing --json
printing-press-golden-pp-cli --profile briefing currencies
printing-press-golden-pp-cli profile list --json
printing-press-golden-pp-cli profile show briefing
printing-press-golden-pp-cli profile delete briefing --yes
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

1. **Empty, `help`, or `--help`** → show `printing-press-golden-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add printing-press-golden-pp-mcp -- printing-press-golden-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which printing-press-golden-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   printing-press-golden-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `printing-press-golden-pp-cli <command> --help`.
