---
name: pp-public-param-golden
description: "Printing Press CLI for Public Param Golden. Public parameter name golden fixture"
author: "printing-press-golden"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - public-param-golden-pp-cli
    install:
      - kind: go
        bins: [public-param-golden-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/other/public-param-golden/cmd/public-param-golden-pp-cli
---

# Public Param Golden — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `public-param-golden-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press install public-param-golden --cli-only
   ```
2. Verify: `public-param-golden-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.23+):

```bash
go install github.com/mvanhorn/printing-press-library/library/other/public-param-golden/cmd/public-param-golden-pp-cli@latest
```

If `--version` reports "command not found" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.

Public parameter name golden fixture

## Command Reference

**stores** — Store lookup operations

- `public-param-golden-pp-cli stores create` — Create a store record
- `public-param-golden-pp-cli stores find` — Find nearby stores by address


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
public-param-golden-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Auth Setup

No authentication required.

Run `public-param-golden-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  public-param-golden-pp-cli stores create --store-code example-value --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Non-interactive** — never prompts, every input is a flag
- **Explicit retries** — use `--idempotent` only when an already-existing create should count as success

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
public-param-golden-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
public-param-golden-pp-cli feedback --stdin < notes.txt
public-param-golden-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.public-param-golden-pp-cli/feedback.jsonl`. They are never POSTed unless `PUBLIC_PARAM_GOLDEN_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `PUBLIC_PARAM_GOLDEN_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

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
public-param-golden-pp-cli profile save briefing --json
public-param-golden-pp-cli --profile briefing stores create --store-code example-value
public-param-golden-pp-cli profile list --json
public-param-golden-pp-cli profile show briefing
public-param-golden-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `public-param-golden-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/other/public-param-golden/cmd/public-param-golden-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add public-param-golden-pp-mcp -- public-param-golden-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which public-param-golden-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   public-param-golden-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `public-param-golden-pp-cli <command> --help`.
