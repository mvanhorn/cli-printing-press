---
name: ble-session-appliance
description: Control BLE Session Appliance through the generated BLE device CLI.
---

## Prerequisites: Install the CLI

This skill drives the `ble-session-appliance-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install ble-session-appliance --cli-only
   ```
2. Verify: `ble-session-appliance-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Use `ble-session-appliance-pp-cli capabilities --json` to inspect callable and withheld BLE capabilities, including safety classes and evidence refs. Use `ble-session-appliance-pp-cli status --json` to inspect replay-backed status output. By default the CLI is replay-backed; build with `-tags ble_live` and pass `--live` to control a real device, `ble-session-appliance-pp-cli doctor` to check live readiness, and `ble-session-appliance-pp-cli scan --live` to discover devices. Use `ble-session-appliance-pp-cli start --dry-run --json` to preview the start write. To run it outside verify mode, pass `--confirm-physical-effect` after checking the dry-run output. Use `ble-session-appliance-pp-cli session start --json` and `ble-session-appliance-pp-cli session status --json` to inspect the local replay session runtime, including lock, capability-token, and endpoint metadata. Use `ble-session-appliance-pp-cli telemetry capture --json` and `ble-session-appliance-pp-cli telemetry latest --json` for the local telemetry store scaffold. Use `ble-session-appliance-pp-cli telemetry sessions --json` to inspect stored BLE session summaries.
