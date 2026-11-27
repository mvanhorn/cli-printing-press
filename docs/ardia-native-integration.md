# Native Ardia Browser + Printing Press 2.0 Integration Spec

Audience: Ardia Browser maintainers
Status: Reference spec (working manifest v2 plugin already shipped in `integrations/ardia-plugin/`)

## Why This Matters

Ardia (`ardia browse`) already beats raw Chrome CDP on warm loads (396ms), zero debugging popups, zero focus steal, and prompt-injection nonce fences. Still, when an AI agent checks a site it uses repeatedly (Instacart, Amazon Orders, DoorDash, Linear, Monarch Money, ESPN), spinning up a DOM tab and taking accessibility snapshots costs ~3-4s and ~11,000-40,000 tokens per task.

Printing Press 2.0 (`pp`) already ships 616+ prebuilt local CLIs + declarative Cartridges that run the exact same tasks over the site's JSON/GraphQL APIs in ~150ms and ~150-350 tokens with zero browser tab.

Three small hooks inside Ardia turn `ardia` + `pp` into an invisible zero-pixel fast path for every agent using Ardia.

## Hook 1: Pre-Navigation Fast-Path Check (`pp route`)

When an agent calls `ardia browse open <url>` (when fast-path routing is enabled in Settings or via `--fast-path`):

1. Ardia runs (or queries over a cached local index) in <5ms:
   ```bash
   pp route "<url>" --json
   ```
2. Response contract (already live in `pp` 2.0):
   ```json
   {
     "matched": true,
     "input": "https://www.instacart.com/store/costco",
     "host": "instacart.com",
     "app": "instacart",
     "mode": "prebuilt-binary",
     "installed": false,
     "recommended_cmd": "pp run instacart --help",
     "browser_fallback": "ARDIA_BACKGROUND=1 ardia browse open https://www.instacart.com/store/costco",
     "estimated_savings": {
       "browser_ms": 4500,
       "pp_ms": 180,
       "browser_tokens": 12000,
       "pp_tokens": 180
     }
   }
   ```
3. Behavior in Ardia:
   - If `matched: true`: Ardia can either return the fast-path suggestion right at the top of the CLI output (zero breaking change) OR execute `pp run <app> ...` directly when invoked from Ardia's AI bar / `--fast-path`.
   - If `matched: false`: Ardia proceeds normally with `ardia browse open <url>`.

## Hook 2: Domain-Filtered Cookie IPC (~50ms Instead of ~11s)

Today `pp auth sync <app>` calls:
```bash
ARDIA_BACKGROUND=1 ardia browse cdp Storage.getCookies --target browser --json
```
That works with zero popups and zero Keychain prompts, and `pp` filters strictly to the single target domain using RFC 6265 rules. However, on a real daily-driver Ardia profile (~18,000+ cookies, ~5.8 MB JSON), serializing the entire browser cookie jar takes ~11 seconds.

Proposed Ardia addition:
```bash
ARDIA_BACKGROUND=1 ardia browse cookies get --domain instacart.com --json
```
- Filters in-process inside Ardia before serializing JSON.
- Only returns cookies matching `--domain` (non-target site cookies never leave Ardia over IPC).
- Drops sync latency from ~11s to ~50ms and improves least-privilege isolation.
- Security gate: gated behind the same Ardia control permission (`Settings > Plugins > Command Line`) and optional per-domain allowlist.

## Hook 3: Repeat-Site Auto-Compile Trigger

When Ardia observes an agent session opening the same unmatched domain 3+ times (no `pp` match and no WebMCP `navigator.modelContext` tools):
- Ardia surfaces a one-line suggestion (or background action):
  ```bash
  pp record --backend ardia <url>
  ```
- `pp record --backend ardia` already drives `ARDIA_BACKGROUND=1 ardia browse network har start --content text` / `har stop`, strips ad/consent/telemetry hosts, and compiles the site's JSON APIs into a SHA-256-pinned 15KB Cartridge in <50s with zero focus steal.
- Every subsequent agent visit to that site hits Hook 1 in ~150ms instead of rendering pixels.

## Existing Plugin Surface (Already Live)

- Plugin manifest: `integrations/ardia-plugin/plugin.json` (manifest v2, passes `ardia plugins check printing-press --json` with 0 problems).
- Installer command: `pp ardia setup` (or `pp setup`).
