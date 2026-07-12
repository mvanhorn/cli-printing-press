# Printing Press Oauth2 CLI

Purpose-built fixture for the OAuth2 device-code auth shape.

Created by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

## Install

The recommended path installs both the `printing-press-oauth2-pp-cli` binary and the `pp-printing-press-oauth2` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install printing-press-oauth2
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --agent claude-code
npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --agent claude-code --agent codex
```

### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-oauth2-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-oauth2 --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-oauth2 --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install printing-press-oauth2 --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

The bundle reuses your local OAuth tokens — authenticate first if you haven't:

```bash
export DEVICE_CODE_CLIENT_ID=<client-id>
printing-press-oauth2-pp-cli auth login --device-code
```

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-oauth2-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `DEVICE_CODE_CLIENT_ID` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "printing-press-oauth2": {
      "command": "printing-press-oauth2-pp-mcp",
      "env": {
        "DEVICE_CODE_CLIENT_ID": "<your-key>"
      }
    }
  }
}
```

</details>

## Quick Start

### 1. Install

See [Install](#install) above.

### 2. Authenticate

Authorize with the OAuth2 device-code flow:

```bash
export DEVICE_CODE_CLIENT_ID=<client-id>
printing-press-oauth2-pp-cli auth login --device-code
```

Open the verification URL, enter the printed user code, and return to the CLI. Your tokens are stored locally and refreshed automatically.

### 3. Verify Setup

```bash
printing-press-oauth2-pp-cli doctor
```

This checks your configuration and credentials.

### 4. Try Your First Command

```bash
printing-press-oauth2-pp-cli items
```

## Usage

Run `printing-press-oauth2-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `PRINTING_PRESS_OAUTH2_CONFIG_DIR`, `PRINTING_PRESS_OAUTH2_DATA_DIR`, `PRINTING_PRESS_OAUTH2_STATE_DIR`, or `PRINTING_PRESS_OAUTH2_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `PRINTING_PRESS_OAUTH2_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export PRINTING_PRESS_OAUTH2_HOME=/srv/printing-press-oauth2
printing-press-oauth2-pp-cli doctor
```

Under `PRINTING_PRESS_OAUTH2_HOME=/srv/printing-press-oauth2`, the four dirs resolve to `/srv/printing-press-oauth2/config`, `/srv/printing-press-oauth2/data`, `/srv/printing-press-oauth2/state`, and `/srv/printing-press-oauth2/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

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

Precedence matters in fleets: an ambient per-kind variable such as `PRINTING_PRESS_OAUTH2_DATA_DIR` overrides an explicit `--home` for that kind. Use `PRINTING_PRESS_OAUTH2_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `PRINTING_PRESS_OAUTH2_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `printing-press-oauth2-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

## Commands

### items

Manage items

- **`printing-press-oauth2-pp-cli items`** - List items


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`printing-press-oauth2-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`printing-press-oauth2-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`printing-press-oauth2-pp-cli learnings list`** - Inspect taught rows
- **`printing-press-oauth2-pp-cli learnings forget <query>`** - Undo a teach
- **`printing-press-oauth2-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`printing-press-oauth2-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`printing-press-oauth2-pp-cli teach-pattern`** - Install a query/resource template up front
- **`printing-press-oauth2-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `PRINTING_PRESS_OAUTH2_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `printing-press-oauth2-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
printing-press-oauth2-pp-cli items

# JSON for scripting and agents
printing-press-oauth2-pp-cli items --json

# Filter to specific fields
printing-press-oauth2-pp-cli items --json --select id,name,status

# Dry run — show the request without sending
printing-press-oauth2-pp-cli items --dry-run

# Agent mode — JSON + compact + no prompts in one flag
printing-press-oauth2-pp-cli items --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Read-only by default** - this CLI does not create, update, delete, publish, send, or mutate remote resources
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
printing-press-oauth2-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Run `printing-press-oauth2-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/printing-press-oauth2-pp-cli/config.toml`; `--home`, `PRINTING_PRESS_OAUTH2_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `DEVICE_CODE_CLIENT_ID` | auth_flow_input | Yes |  |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `printing-press-oauth2-pp-cli doctor` reports `agentcookie: detected` and `auth-status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `printing-press-oauth2-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $DEVICE_CODE_CLIENT_ID`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
