# Path Param Posts CLI



Created by [@printing-press-golden](https://github.com/printing-press-golden) (printing-press-golden).

## Install

The recommended path installs both the `path-param-posts-pp-cli` binary and the `pp-path-param-posts` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install path-param-posts
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install path-param-posts --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install path-param-posts --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install path-param-posts --agent claude-code
npx -y @mvanhorn/printing-press-library install path-param-posts --agent claude-code --agent codex
```

### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/path-param-posts-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install path-param-posts --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-path-param-posts --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-path-param-posts --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install path-param-posts --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/path-param-posts-current).
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
    "path-param-posts": {
      "command": "path-param-posts-pp-mcp"
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
path-param-posts-pp-cli doctor
```

This checks your configuration.

### 3. Try Your First Command

```bash
path-param-posts-pp-cli posts list
```

## Usage

Run `path-param-posts-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data such as `data.db` |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `PATH_PARAM_POSTS_CONFIG_DIR`, `PATH_PARAM_POSTS_DATA_DIR`, `PATH_PARAM_POSTS_STATE_DIR`, or `PATH_PARAM_POSTS_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `PATH_PARAM_POSTS_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export PATH_PARAM_POSTS_HOME=/srv/path-param-posts
path-param-posts-pp-cli doctor
```

Under `PATH_PARAM_POSTS_HOME=/srv/path-param-posts`, the four dirs resolve to `/srv/path-param-posts/config`, `/srv/path-param-posts/data`, `/srv/path-param-posts/state`, and `/srv/path-param-posts/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "path-param-posts": {
      "command": "path-param-posts-pp-mcp",
      "env": {
        "PATH_PARAM_POSTS_HOME": "/srv/path-param-posts"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `PATH_PARAM_POSTS_DATA_DIR` overrides an explicit `--home` for that kind. Use `PATH_PARAM_POSTS_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `PATH_PARAM_POSTS_HOME` does not move files back to platform defaults, and `doctor` cannot find files left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. Run `path-param-posts-pp-cli doctor --fail-on warn` to check path warnings in automation.

## Commands

### posts

Manage posts

- **`path-param-posts-pp-cli posts get`** - Get
- **`path-param-posts-pp-cli posts list`** - List


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`path-param-posts-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`path-param-posts-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`path-param-posts-pp-cli learnings list`** - Inspect taught rows
- **`path-param-posts-pp-cli learnings forget <query>`** - Undo a teach
- **`path-param-posts-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`path-param-posts-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`path-param-posts-pp-cli teach-pattern`** - Install a query/resource template up front
- **`path-param-posts-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `PATH_PARAM_POSTS_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `path-param-posts-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
path-param-posts-pp-cli posts list

# JSON for scripting and agents
path-param-posts-pp-cli posts list --json
# Filter to specific fields by name
path-param-posts-pp-cli posts list --json --select <field>[,<field>...]

# Dry run — show the request without sending
path-param-posts-pp-cli posts list --dry-run

# Agent mode — JSON + compact + no prompts in one flag
path-param-posts-pp-cli posts list --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select <field>[,<field>...]` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Read-only by default** - this CLI does not create, update, delete, publish, send, or mutate remote resources
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `5` API error, `7` rate limited, `10` config error.

## Runtime Endpoint

This CLI resolves endpoint placeholders at runtime, so one installed binary can target different tenants or API versions without regeneration.

Endpoint environment variables:
- `PATH_PARAM_POSTS_SHOP` resolves `{shop}`

Base URL: `https://{shop}.example.com`

## Health Check

```bash
path-param-posts-pp-cli doctor
```

Verifies configuration and connectivity to the API.

## Configuration

Run `path-param-posts-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/path-param-posts-pp-cli/config.toml`; `--home`, `PATH_PARAM_POSTS_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

## Troubleshooting
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
