# Public Param Golden CLI

Public parameter name golden fixture

Created by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

## Install

The recommended path installs both the `public-param-golden-pp-cli` binary and the `pp-public-param-golden` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install public-param-golden
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install public-param-golden --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install public-param-golden --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install public-param-golden --agent claude-code
npx -y @mvanhorn/printing-press-library install public-param-golden --agent claude-code --agent codex
```

### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/public-param-golden-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install public-param-golden --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-public-param-golden --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-public-param-golden --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install public-param-golden --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/public-param-golden-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "public-param-golden": {
      "command": "public-param-golden-pp-mcp"
    }
  }
}
```

</details>

## Quick Start

### 1. Install

See [Install](#install) above.

### 2. Verify Setup

```bash
public-param-golden-pp-cli doctor
```

This checks your configuration.

### 3. Try Your First Command

```bash
public-param-golden-pp-cli stores create --store-code example-value
```

## Usage

Run `public-param-golden-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data such as `data.db` |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `PUBLIC_PARAM_GOLDEN_CONFIG_DIR`, `PUBLIC_PARAM_GOLDEN_DATA_DIR`, `PUBLIC_PARAM_GOLDEN_STATE_DIR`, or `PUBLIC_PARAM_GOLDEN_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `PUBLIC_PARAM_GOLDEN_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export PUBLIC_PARAM_GOLDEN_HOME=/srv/public-param-golden
public-param-golden-pp-cli doctor
```

Under `PUBLIC_PARAM_GOLDEN_HOME=/srv/public-param-golden`, the four dirs resolve to `/srv/public-param-golden/config`, `/srv/public-param-golden/data`, `/srv/public-param-golden/state`, and `/srv/public-param-golden/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "public-param-golden": {
      "command": "public-param-golden-pp-mcp",
      "env": {
        "PUBLIC_PARAM_GOLDEN_HOME": "/srv/public-param-golden"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `PUBLIC_PARAM_GOLDEN_DATA_DIR` overrides an explicit `--home` for that kind. Use `PUBLIC_PARAM_GOLDEN_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `PUBLIC_PARAM_GOLDEN_HOME` does not move files back to platform defaults, and `doctor` cannot find files left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. Run `public-param-golden-pp-cli doctor --fail-on warn` to check path warnings in automation.

## Commands

### stores

Store lookup operations

- **`public-param-golden-pp-cli stores create`** - Create a store record
- **`public-param-golden-pp-cli stores find`** - Find nearby stores by address


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
public-param-golden-pp-cli stores create --store-code example-value

# JSON for scripting and agents
public-param-golden-pp-cli stores create --store-code example-value --json

# Filter to specific fields
public-param-golden-pp-cli stores create --store-code example-value --json --select id,name,status

# Dry run — show the request without sending
public-param-golden-pp-cli stores create --store-code example-value --dry-run

# Agent mode — JSON + compact + no prompts in one flag
public-param-golden-pp-cli stores create --store-code example-value --agent
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
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
public-param-golden-pp-cli doctor
```

Verifies configuration and connectivity to the API.

## Configuration

Run `public-param-golden-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is ``; `--home`, `PUBLIC_PARAM_GOLDEN_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

## Troubleshooting
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
