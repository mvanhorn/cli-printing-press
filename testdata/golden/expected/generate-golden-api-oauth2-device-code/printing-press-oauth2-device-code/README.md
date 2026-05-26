# Printing Press Oauth2 CLI

Purpose-built fixture for the OAuth2 device-code auth shape.

Printed by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

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

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-oauth2 --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-printing-press-oauth2 --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-printing-press-oauth2 skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-printing-press-oauth2. The skill defines how its required CLI can be installed.
```

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

## Commands

### items

Manage items

- **`printing-press-oauth2-pp-cli items`** - List items


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

Config file: `~/.config/printing-press-oauth2-pp-cli/config.toml`

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
