# Printing Press Golden CLI

Purpose-built fixture for golden generation coverage.

## Install

The recommended path installs both the `printing-press-golden-pp-cli` binary and the `pp-printing-press-golden` agent skill in one shot:

```bash
npx -y @mvanhorn/printing-press install printing-press-golden
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press install printing-press-golden --cli-only
```


### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/printing-press-golden-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-golden --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-golden --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-printing-press-golden skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-printing-press-golden. The skill defines how its required CLI can be installed.
```

## Quick Start

### 1. Install

See [Install](#install) above.

### 2. Set Up Credentials

Get your API key from your API provider's developer portal. The key typically looks like a long alphanumeric string.

```bash
export PRINTING_PRESS_GOLDEN_API_KEY="<paste-your-key>"
```

You can also persist this in your config file at `~/.config/printing-press-golden-pp-cli/config.toml`.

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

## Commands

### currencies

Manage currencies

- **`printing-press-golden-pp-cli currencies list`** - List supported currencies

### projects

Manage projects

- **`printing-press-golden-pp-cli projects create`** - Create project
- **`printing-press-golden-pp-cli projects get`** - Get project
- **`printing-press-golden-pp-cli projects list`** - List projects

### public

Manage public

- **`printing-press-golden-pp-cli public get-status`** - Get public service status

### reports

Manage reports



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

## Use with Claude Code

Install the focused skill — it auto-installs the CLI on first invocation:

```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-printing-press-golden -g
```

Then invoke `/pp-printing-press-golden <query>` in Claude Code. The skill is the most efficient path — Claude Code drives the CLI directly without an MCP server in the middle.

<details>
<summary>Use as an MCP server in Claude Code (advanced)</summary>

If you'd rather register this CLI as an MCP server in Claude Code, install the MCP binary first:


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Then register it:

```bash
claude mcp add printing-press-golden printing-press-golden-pp-mcp -e PRINTING_PRESS_GOLDEN_API_KEY=<your-key>
```

</details>

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

## Health Check

```bash
printing-press-golden-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: `~/.config/printing-press-golden-pp-cli/config.toml`

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `PRINTING_PRESS_GOLDEN_API_KEY` | per_call | Yes | Set to your API credential. |

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `printing-press-golden-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $PRINTING_PRESS_GOLDEN_API_KEY`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
