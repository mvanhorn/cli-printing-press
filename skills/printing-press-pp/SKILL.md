---
name: printing-press-pp
description: "Printing Press 2.0 local app-to-agent compiler and browser fast-path accelerator. Use BEFORE opening a browser tab or computer-use tool for any website, consumer app, or repeat workflow: routes URLs to 150ms local Cartridges and 616+ prebuilt CLIs, compiles new websites via Ardia/HAR in <60s, queries the unified local SQLite Life Lake, audits MCP trust, and auto-heals drifted endpoints."
---

# Printing Press 2.0 (`pp`) - Skip Pixels on Repeat Work

`pp` sits in front of browser automation (`ardia browse`, OpenCode `browse`) and desktop computer use (`cua-driver`). Every time you replace a browser screenshot loop with a compiled local Cartridge, you save 100x-400x latency and 95%+ tokens with zero remote MCP server in the middle.

## Core Routing Loop

1. Check the fast path before opening a browser tab:
   ```bash
   pp route "<url-or-intent>" --json
   ```
   - `matched: true`, `installed: true` -> run `recommended_cmd` immediately (~150ms, ~140 tokens).
   - `matched: true`, `installed: false` -> install with zero Go required:
     ```bash
     pp use <app>
     ```
   - `matched: false` and this workflow will be repeated -> compile once via Ardia (zero focus steal, zero Chrome 144 popups) or HAR:
     ```bash
     pp record --backend ardia <url>
     # or from an exported HAR:
     pp record --from-har ./capture.har
     ```

2. Run local Cartridges cleanly (agent-native flags):
   ```bash
   pp run <app> <command> --compact --json
   pp run <app> <command> --param key=value --limit 10 --json
   pp run <app> <command> --dry-run --json
   ```
   - Mutating commands are blocked unless `--yes` is explicitly passed.
   - Every HTTP cartridge is host-locked to `trust.allowed_hosts` (including redirects).

3. Query the Unified Local Personal Life Lake (`~/.pp/lake.db`):
   ```bash
   pp lake sync espn hackernews chrome-history
   pp lake search "nba injury" --json
   pp lake sql "SELECT source, title, url FROM lake_items ORDER BY synced_at DESC LIMIT 10" --json
   ```

4. Verify Trust Shield and Auto-Heal Drift:
   ```bash
   pp audit --json                          # verifies SHA-256 lockfile + scans MCP configs for rug-pull / prompt-injection risks
   pp heal <app> <command>                  # diagnoses auth vs upstream drift
   pp heal <app> <command> --backend ardia  # 10s surgical re-sniff + local re-pin
   ```

5. Escape hatch for full custom Go CLI:
   Need a standalone Go repository with custom multi-source logic? Run `cli-printing-press generate` (or `/printing-press <app>`).
