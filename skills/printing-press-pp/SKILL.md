---
name: printing-press-pp
description: "Printing Press 2.0 zero-pixel browser middleware and local app-to-agent runtime. Use BEFORE opening Ardia or Chrome for any website or repeat workflow: routes URLs across 616+ apps, auto-hydrates verified prebuilt CLIs on first call, detects your logged-in Chrome/Ardia apps with `pp onboard`, syncs domain-scoped Ardia cookies and 1Password `op://` secrets, compiles new sites via Ardia or OpenCode `browse`, batches parallel calls, and wraps untrusted output in random-nonce prompt-injection fences."
---

# Printing Press 2.0 (`pp`) - Zero-Pixel Browser Middleware

`pp` sits in front of Ardia (`ardia browse`), Google Chrome (OpenCode `browse`), and desktop computer use (`cua-driver`). Before wasting browser tabs and 12,000+ tokens on DOM snapshots, check `pp` first.

## Core Middleware Loop

1. Check the fast path before opening a browser tab:
   ```bash
   pp route "<url-or-app>" --json
   ```
   - `matched: true` -> run `pp run <app> <command> --compact` directly. All 616+ library apps auto-hydrate (SHA-256 verified, zero Go) on the first call in ~1s and run in ~150ms thereafter.
   - `matched: false` and this workflow will repeat -> check page-native WebMCP tools (`ardia browse tools` / `browse webmcp`) or compile once with zero popups:
     ```bash
     pp record --backend auto <url>     # Ardia first, then OpenCode browse for Chrome
     pp record --from-har ./capture.har # or exported HAR
     ```

2. Personalize in one command from your local Chrome/Ardia domains:
   ```bash
   pp onboard             # reads plaintext host_key + visit counts only (zero Keychain prompts, zero secret values)
   pp onboard --install   # optionally pre-warm matched apps locally
   pp setup               # detect agents + wire Ardia plugin + MCP snippet
   ```

3. Logged-in sessions (Ardia cookies + 1Password Touch ID):
   ```bash
   pp auth sync <app>     # pulls strictly one domain's cookies from Ardia into encrypted press-auth store
   ```
   - For API-key Cartridges, set `op_ref: op://Vault/Item/field` (or an env var starting with `op://`) to hydrate via 1Password CLI Touch ID with zero plaintext tokens on disk.
   - At login, 2FA, Passkey, or payment gates, `pp` returns a structured `handoff` hint instead of stealing focus.

4. Batch parallel calls, guard output size, and query the local Life Lake:
   ```bash
   pp batch '[{"op":"route","target":"https://www.instacart.com"},{"op":"run","app":"espn","command":"scoreboard","limit":3}]'
   pp lake sync espn hackernews chrome-history
   pp lake search "nba" --json
   pp lake sql "SELECT source, title, url FROM lake_items ORDER BY synced_at DESC LIMIT 10" --json
   ```
   - `pp run` caps serialized output at 12,000 chars by default (`--full` to override); MCP `pp_run`/`pp_lake_search` and `pp batch` wrap untrusted external text in fresh random-nonce content boundaries (`--no-content-boundaries` to disable).

5. Verify Trust Shield and Auto-Heal Drift:
   ```bash
   pp audit --json
   pp heal <app> <command> --backend auto
   ```
