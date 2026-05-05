---
name: pp-printing-press-golden
description: "Printing Press CLI for Printing Press Golden. Purpose-built fixture for golden generation coverage."
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - printing-press-golden-pp-cli
    install:
      - kind: go
        bins: [printing-press-golden-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/other/printing-press-golden/cmd/printing-press-golden-pp-cli
---

# Printing Press Golden — Printing Press CLI

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



### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
printing-press-golden-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Auth Setup

Set your API key via environment variable:

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

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal — piped/agent consumers get pure JSON on stdout.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
printing-press-golden-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
printing-press-golden-pp-cli feedback --stdin < notes.txt
printing-press-golden-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.printing-press-golden-pp-cli/feedback.jsonl`. They are never POSTed unless `PRINTING_PRESS_GOLDEN_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `PRINTING_PRESS_GOLDEN_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

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
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → CLI installation
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## CLI Installation

1. Check Go is installed: `go version` (requires Go 1.23+)
2. Install:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/other/printing-press-golden/cmd/printing-press-golden-pp-cli@latest
   ```
3. Verify: `printing-press-golden-pp-cli --version`
4. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/other/printing-press-golden/cmd/printing-press-golden-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add printing-press-golden-pp-mcp -- printing-press-golden-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which printing-press-golden-pp-cli`
   If not found, offer to install (see CLI Installation above).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   printing-press-golden-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `printing-press-golden-pp-cli <command> --help`.
