# Local Plugin Development

Use this when you are developing the Printing Press plugin from a clone and want Claude Code to load the working tree across restarts and background sessions.

The quick path in the README uses `claude --plugin-dir .`. That is useful for one-off local testing, but it is a process flag. It does not persist to the next `claude` invocation, and background sessions do not inherit it from the parent session.

For persistent local development, register a gitignored local marketplace in `.claude/settings.local.json`.

## Persistent Working-Tree Plugin

Create the local marketplace directory:

```bash
mkdir -p .claude/local/dev-marketplace/.claude-plugin
```

Create `.claude/local/dev-marketplace/.claude-plugin/marketplace.json`:

```json
{
  "name": "cli-printing-press-local",
  "owner": { "name": "local-dev" },
  "plugins": [
    {
      "name": "cli-printing-press",
      "source": "./../../.."
    }
  ]
}
```

The `./../../..` source points from `.claude/local/dev-marketplace/` back to the repository root.

Then create or update `.claude/settings.local.json`:

```json
{
  "extraKnownMarketplaces": {
    "cli-printing-press-local": {
      "source": {
        "source": "directory",
        "path": ".claude/local/dev-marketplace"
      }
    }
  },
  "enabledPlugins": {
    "cli-printing-press@cli-printing-press-local": true
  }
}
```

Restart Claude Code. The plugin's skills, including `/printing-press` and `/printing-press-polish`, load from this working tree without passing `--plugin-dir` each time.

## Why This Is Local-Only

Both `.claude/settings.local.json` and `.claude/local/` are gitignored. Keep this setup out of tracked files because it points Claude Code at your personal checkout and can conflict with another contributor's installed plugin.

Do not change `.claude-plugin/marketplace.json` for local development. That file describes the published plugin source.
