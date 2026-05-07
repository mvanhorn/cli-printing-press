# Printing Press Rich CLI

Purpose-built fixture for rich auth env-var model coverage.

## Install

The recommended path installs both the `printing-press-rich-pp-cli` binary and the `pp-printing-press-rich` agent skill in one shot:

```bash
npx -y @mvanhorn/printing-press install printing-press-rich
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press install printing-press-rich --cli-only
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.23+):

```bash
go install github.com/mvanhorn/printing-press-library/library/other/printing-press-rich/cmd/printing-press-rich-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-rich-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-rich --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-rich --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-printing-press-rich skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-printing-press-rich. The skill defines how its required CLI can be installed.
```

## Quick Start

### 1. Install

See [Install](#install) above.

### 2. Set Up Credentials

Get your API key from your API provider's developer portal. The key typically looks like a long alphanumeric string.

```bash
export RICH_AUTH_API_KEY="<paste-your-key>"
```

You can also persist this in your config file at `~/.config/printing-press-rich-pp-cli/config.toml`.

### 3. Verify Setup

```bash
printing-press-rich-pp-cli doctor
```

This checks your configuration and credentials.

### 4. Try Your First Command

```bash
printing-press-rich-pp-cli items
```

## Usage

Run `printing-press-rich-pp-cli --help` for the full command reference and flag list.

## Commands

### items

Manage items

- **`printing-press-rich-pp-cli items list`** - List items


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
printing-press-rich-pp-cli items

# JSON for scripting and agents
printing-press-rich-pp-cli items --json

# Filter to specific fields
printing-press-rich-pp-cli items --json --select id,name,status

# Dry run — show the request without sending
printing-press-rich-pp-cli items --dry-run

# Agent mode — JSON + compact + no prompts in one flag
printing-press-rich-pp-cli items --agent
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

## Use with Claude Code

Install the focused skill — it auto-installs the CLI on first invocation:

```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-printing-press-rich -g
```

Then invoke `/pp-printing-press-rich <query>` in Claude Code. The skill is the most efficient path — Claude Code drives the CLI directly without an MCP server in the middle.

<details>
<summary>Use as an MCP server in Claude Code (advanced)</summary>

If you'd rather register this CLI as an MCP server in Claude Code, install the MCP binary first:

```bash
go install github.com/mvanhorn/printing-press-library/library/other/printing-press-rich/cmd/printing-press-rich-pp-mcp@latest
```

Then register it:

```bash
claude mcp add printing-press-rich printing-press-rich-pp-mcp -e RICH_AUTH_API_KEY=<your-key>
```

</details>

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-rich-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `RICH_AUTH_API_KEY` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.

```bash
go install github.com/mvanhorn/printing-press-library/library/other/printing-press-rich/cmd/printing-press-rich-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "printing-press-rich": {
      "command": "printing-press-rich-pp-mcp",
      "env": {
        "RICH_AUTH_API_KEY": "<your-key>"
      }
    }
  }
}
```

</details>

## Health Check

```bash
printing-press-rich-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: `~/.config/printing-press-rich-pp-cli/config.toml`

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `RICH_AUTH_API_KEY` | per_call | Yes | Set to your API credential. |
| `RICH_AUTH_CLIENT_ID` | auth_flow_input | No | OAuth application client identifier. |
| `RICH_AUTH_CLIENT_SECRET` | auth_flow_input | No | Set during initial auth setup. |
| `RICH_AUTH_SESSION_COOKIE` | harvested | No | Populated automatically by auth login. |
| `RICH_AUTH_OPTIONAL_TOKEN` | per_call | No | Set to your API credential. |
| `RICH_AUTH_BOT_TOKEN` | per_call | No | Set to your API credential. |
| `RICH_AUTH_USER_TOKEN` | per_call | No | Set to your API credential. |

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `printing-press-rich-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $RICH_AUTH_API_KEY`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
