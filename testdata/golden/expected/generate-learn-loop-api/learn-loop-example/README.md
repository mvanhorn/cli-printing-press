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


Created by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

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

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install learn-loop-example --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-learn-loop-example --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-learn-loop-example --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install learn-loop-example --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

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

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `LEARN_LOOP_EXAMPLE_CONFIG_DIR`, `LEARN_LOOP_EXAMPLE_DATA_DIR`, `LEARN_LOOP_EXAMPLE_STATE_DIR`, or `LEARN_LOOP_EXAMPLE_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `LEARN_LOOP_EXAMPLE_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export LEARN_LOOP_EXAMPLE_HOME=/srv/learn-loop-example
learn-loop-example-pp-cli doctor
```

Under `LEARN_LOOP_EXAMPLE_HOME=/srv/learn-loop-example`, the four dirs resolve to `/srv/learn-loop-example/config`, `/srv/learn-loop-example/data`, `/srv/learn-loop-example/state`, and `/srv/learn-loop-example/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "learn-loop-example": {
      "command": "learn-loop-example-pp-mcp",
      "env": {
        "LEARN_LOOP_EXAMPLE_HOME": "/srv/learn-loop-example"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `LEARN_LOOP_EXAMPLE_DATA_DIR` overrides an explicit `--home` for that kind. Use `LEARN_LOOP_EXAMPLE_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `LEARN_LOOP_EXAMPLE_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `learn-loop-example-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

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

Run `learn-loop-example-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is ``; `--home`, `LEARN_LOOP_EXAMPLE_HOME`, and per-kind env vars can relocate it.

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
