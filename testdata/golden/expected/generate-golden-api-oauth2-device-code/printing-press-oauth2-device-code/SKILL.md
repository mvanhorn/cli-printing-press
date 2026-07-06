---
name: pp-printing-press-oauth2
description: "Printing Press CLI for Printing Press Oauth2. Purpose-built fixture for the OAuth2 device-code auth shape."
author: "printing-press-golden"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - printing-press-oauth2-pp-cli
---

# Printing Press Oauth2 — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `printing-press-oauth2-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --cli-only
   ```
2. Verify: `printing-press-oauth2-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Purpose-built fixture for the OAuth2 device-code auth shape.

## When Not to Use This CLI

Do not activate this CLI for requests that require creating, updating, deleting, publishing, commenting, upvoting, inviting, ordering, sending messages, booking, purchasing, or changing remote state. This printed CLI exposes read-only commands for inspection, export, sync, and analysis.

## Command Reference

**items** — Manage items

- `printing-press-oauth2-pp-cli items` — List items


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
printing-press-oauth2-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Auth Setup

Run `printing-press-oauth2-pp-cli auth setup` for setup details if needed. Then authenticate:

```bash
export DEVICE_CODE_CLIENT_ID=<client-id>
printing-press-oauth2-pp-cli auth login --device-code
```

Open the verification URL, enter the printed user code, and return to the CLI. Tokens are stored locally and refreshed automatically.

Run `printing-press-oauth2-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  printing-press-oauth2-pp-cli items --agent --select id,name,status
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

## Paths and state

Agents should treat the CLI's path resolver as part of the runtime contract:

- Use `--home <dir>` for one invocation, or set `PRINTING_PRESS_OAUTH2_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `PRINTING_PRESS_OAUTH2_CONFIG_DIR`, `PRINTING_PRESS_OAUTH2_DATA_DIR`, `PRINTING_PRESS_OAUTH2_STATE_DIR`, `PRINTING_PRESS_OAUTH2_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `PRINTING_PRESS_OAUTH2_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars. `state` contains persisted queries, jobs, and `teach.log`. `cache` contains regenerable HTTP/cache files.
- Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.
- Run `printing-press-oauth2-pp-cli doctor --fail-on warn` to surface path and credential-location warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

  ```json
  {
    "mcpServers": {
      "printing-press-oauth2": {
        "command": "printing-press-oauth2-pp-mcp",
        "env": {
          "PRINTING_PRESS_OAUTH2_HOME": "/srv/printing-press-oauth2"
        }
      }
    }
  }
  ```

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `PRINTING_PRESS_OAUTH2_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `PRINTING_PRESS_OAUTH2_HOME`, or `doctor` will not find credentials left under the former root.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
printing-press-oauth2-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
printing-press-oauth2-pp-cli feedback --stdin < notes.txt
printing-press-oauth2-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `PRINTING_PRESS_OAUTH2_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `PRINTING_PRESS_OAUTH2_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

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

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled or recurring agent reuses the same saved flags while providing different input each run.

```
printing-press-oauth2-pp-cli profile save briefing --json
printing-press-oauth2-pp-cli --profile briefing items
printing-press-oauth2-pp-cli profile list --json
printing-press-oauth2-pp-cli profile show briefing
printing-press-oauth2-pp-cli profile delete briefing --yes
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

1. **Empty, `help`, or `--help`** → show `printing-press-oauth2-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add printing-press-oauth2-pp-mcp -- printing-press-oauth2-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which printing-press-oauth2-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   printing-press-oauth2-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `printing-press-oauth2-pp-cli <command> --help`.
