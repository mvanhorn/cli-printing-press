---
name: ble-desk-lamp
description: Control BLE Desk Lamp through the generated BLE device CLI.
---

## Prerequisites: Install the CLI

This skill drives the `ble-desk-lamp-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install ble-desk-lamp --cli-only
   ```
2. Verify: `ble-desk-lamp-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Use `ble-desk-lamp-pp-cli capabilities --json` to inspect callable and withheld BLE capabilities, including safety classes and evidence refs. Use `ble-desk-lamp-pp-cli status --json` to inspect replay-backed status output. By default the CLI is replay-backed; build with `-tags ble_live` and pass `--live` to control a real device, `ble-desk-lamp-pp-cli doctor` to check live readiness, and `ble-desk-lamp-pp-cli scan --live` to discover devices. Use `ble-desk-lamp-pp-cli toggle --dry-run --json` to preview the toggle write. Session IPC scaffolding is generated only when the device spec enables device-session support.
