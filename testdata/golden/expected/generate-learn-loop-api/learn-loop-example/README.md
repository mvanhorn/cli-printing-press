# Learn Loop Example CLI

Golden fixture exercising the spec-declared self-learning loop. Demonstrates
the shape every printed CLI gets when its spec declares a `learn:` block:
the generator emits internal/learn/* subpackages, the teach/recall/learnings
commands, the v3 store schema additions, and the self-learning sections in
README.md / SKILL.md / AGENTS.md.

The underlying resource surface mirrors the sync-walker fixture (top-level
games + walker-fanned-out leagues) so the emitted shape covers the typical
multi-file CLI alongside the learn package. Identifiers in the learn block
are intentionally neutral (EXAMPLE-* ticker, ALPHA/BRAVO entities) so the
scripts/verify-learn-purity.sh gate cannot trip on this fixture.


Printed by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

## Install

The recommended path installs both the `learn-loop-example-pp-cli` binary and the `pp-learn-loop-example` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install learn-loop-example
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install learn-loop-example --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install learn-loop-example --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install learn-loop-example --agent claude-code
npx -y @mvanhorn/printing-press-library install learn-loop-example --agent claude-code --agent codex
```

### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/learn-loop-example-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-learn-loop-example --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-learn-loop-example --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-learn-loop-example skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-learn-loop-example. The skill defines how its required CLI can be installed.
```

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/learn-loop-example-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `LEARN_LOOP_TOKEN` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "learn-loop-example": {
      "command": "learn-loop-example-pp-mcp",
      "env": {
        "LEARN_LOOP_TOKEN": "<your-key>"
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

Get your access token from your API provider's developer portal, then store it:

```bash
learn-loop-example-pp-cli auth set-token YOUR_TOKEN_HERE
```

Or set it via environment variable:

```bash
export LEARN_LOOP_TOKEN="your-token-here"
```

### 3. Verify Setup

```bash
learn-loop-example-pp-cli doctor
```

This checks your configuration and credentials.

### 4. Try Your First Command

```bash
learn-loop-example-pp-cli games
```

## Usage

Run `learn-loop-example-pp-cli --help` for the full command reference and flag list.

## Commands

### games

Top-level games resource. The list endpoint populates the generic resources table; rows carry a `game_key` field that the walker's leagues endpoint extracts for child fan-out.

- **`learn-loop-example-pp-cli games`** - List games

### leagues

Leagues, fetched per-game by walking games and extracting each game's game_key into the child path.

- **`learn-loop-example-pp-cli leagues <game_key>`** - List leagues for a game


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the four-branch protocol.

- **`learn-loop-example-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`learn-loop-example-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`learn-loop-example-pp-cli learnings list`** - Inspect taught rows
- **`learn-loop-example-pp-cli learnings forget <query>`** - Undo a teach
- **`learn-loop-example-pp-cli teach-pattern`** - Install a query/resource template up front
- **`learn-loop-example-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `LEARN_LOOP_EXAMPLE_NO_LEARN=true` to disable the loop for deterministic flows.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
learn-loop-example-pp-cli games

# JSON for scripting and agents
learn-loop-example-pp-cli games --json

# Filter to specific fields
learn-loop-example-pp-cli games --json --select id,name,status

# Dry run — show the request without sending
learn-loop-example-pp-cli games --dry-run

# Agent mode — JSON + compact + no prompts in one flag
learn-loop-example-pp-cli games --agent
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
learn-loop-example-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: ``

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `LEARN_LOOP_TOKEN` | per_call | Yes | Set to your API credential. |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `learn-loop-example-pp-cli doctor` reports `agentcookie: detected` and `auth-status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `learn-loop-example-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $LEARN_LOOP_TOKEN`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
