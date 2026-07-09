# Printing Press Golden CLI

Purpose-built fixture for golden generation coverage.

Created by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

## Install

The recommended path installs both the `printing-press-golden-pp-cli` binary and the `pp-printing-press-golden` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install printing-press-golden
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-golden --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-golden --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-golden --agent claude-code
npx -y @mvanhorn/printing-press-library install printing-press-golden --agent claude-code --agent codex
```

### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-golden-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install printing-press-golden --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-golden --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-golden --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-golden --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-golden-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `PRINTING_PRESS_GOLDEN_API_KEY` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "printing-press-golden": {
      "command": "printing-press-golden-pp-mcp",
      "env": {
        "PRINTING_PRESS_GOLDEN_API_KEY": "<your-key>"
      }
    }
  }
}
```

</details>

## Quick Start

### 1. Install

See [Install](#install) above.

### 2. Set Up Credentials

Get your API key from your API provider's developer portal. The key typically looks like a long alphanumeric string.

```bash
export PRINTING_PRESS_GOLDEN_API_KEY="<paste-your-key>"
```

To persist credentials, use `printing-press-golden-pp-cli auth set-token <token>`. Stored secrets live in `credentials.toml` under the data directory, not in `config.toml`.

### 3. Verify Setup

```bash
printing-press-golden-pp-cli doctor
```

This checks your configuration and credentials.

### 4. Try Your First Command

```bash
printing-press-golden-pp-cli currencies
```

## Usage

Run `printing-press-golden-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `PRINTING_PRESS_GOLDEN_CONFIG_DIR`, `PRINTING_PRESS_GOLDEN_DATA_DIR`, `PRINTING_PRESS_GOLDEN_STATE_DIR`, or `PRINTING_PRESS_GOLDEN_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `PRINTING_PRESS_GOLDEN_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export PRINTING_PRESS_GOLDEN_HOME=/srv/printing-press-golden
printing-press-golden-pp-cli doctor
```

Under `PRINTING_PRESS_GOLDEN_HOME=/srv/printing-press-golden`, the four dirs resolve to `/srv/printing-press-golden/config`, `/srv/printing-press-golden/data`, `/srv/printing-press-golden/state`, and `/srv/printing-press-golden/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "printing-press-golden": {
      "command": "printing-press-golden-pp-mcp",
      "env": {
        "PRINTING_PRESS_GOLDEN_HOME": "/srv/printing-press-golden"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `PRINTING_PRESS_GOLDEN_DATA_DIR` overrides an explicit `--home` for that kind. Use `PRINTING_PRESS_GOLDEN_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `PRINTING_PRESS_GOLDEN_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `printing-press-golden-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

## Commands

### currencies

Manage currencies

- **`printing-press-golden-pp-cli currencies`** - List supported currencies

### projects

Manage projects

- **`printing-press-golden-pp-cli projects create`** - Create project
- **`printing-press-golden-pp-cli projects get`** - Get project
- **`printing-press-golden-pp-cli projects list`** - List projects

### public

Manage public

- **`printing-press-golden-pp-cli public`** - Get public service status

### reports

Manage reports



### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`printing-press-golden-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`printing-press-golden-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`printing-press-golden-pp-cli learnings list`** - Inspect taught rows
- **`printing-press-golden-pp-cli learnings forget <query>`** - Undo a teach
- **`printing-press-golden-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`printing-press-golden-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`printing-press-golden-pp-cli teach-pattern`** - Install a query/resource template up front
- **`printing-press-golden-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `PRINTING_PRESS_GOLDEN_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `printing-press-golden-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
printing-press-golden-pp-cli currencies

# JSON for scripting and agents
printing-press-golden-pp-cli currencies --json

# Filter to specific fields
printing-press-golden-pp-cli currencies --json --select id,name,status

# Dry run — show the request without sending
printing-press-golden-pp-cli currencies --dry-run

# Agent mode — JSON + compact + no prompts in one flag
printing-press-golden-pp-cli currencies --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Explicit retries** - add `--idempotent` to create retries when a no-op success is acceptable
- **Confirmable** - `--yes` for explicit confirmation of destructive actions
- **Piped input** - write commands can accept structured input when their help lists `--stdin`
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Freshness

This CLI owns bounded freshness for registered store-backed read command paths. In `--data-source auto` mode, covered commands check the local SQLite store before serving results; stale or missing resources trigger a bounded refresh, and refresh failures fall back to the existing local data with a warning. `--data-source local` never refreshes, and `--data-source live` reads the API without mutating the local store.

Set `PRINTING_PRESS_GOLDEN_NO_AUTO_REFRESH=1` to disable the pre-read freshness hook while preserving the selected data source.

Covered command paths:
- `printing-press-golden-pp-cli currencies`
- `printing-press-golden-pp-cli currencies get`
- `printing-press-golden-pp-cli currencies list`
- `printing-press-golden-pp-cli currencies search`
- `printing-press-golden-pp-cli projects`
- `printing-press-golden-pp-cli projects get`
- `printing-press-golden-pp-cli projects list`
- `printing-press-golden-pp-cli projects search`

JSON outputs that use the generated provenance envelope include freshness metadata at `meta.freshness`. This metadata describes the freshness decision for the covered command path; it does not claim full historical backfill or API-specific enrichment.

## Health Check

```bash
printing-press-golden-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Run `printing-press-golden-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/printing-press-golden-pp-cli/config.toml`; `--home`, `PRINTING_PRESS_GOLDEN_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `PRINTING_PRESS_GOLDEN_API_KEY` | per_call | Yes | Set to your API credential. |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `printing-press-golden-pp-cli doctor` reports `agentcookie: detected` and `auth-status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `printing-press-golden-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $PRINTING_PRESS_GOLDEN_API_KEY`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
