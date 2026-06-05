---
name: ble-opaque-binary
description: Control BLE Opaque Binary Device through the generated BLE device CLI.
---

## Prerequisites: Install the CLI

This skill drives the `ble-opaque-binary-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer into a user bin directory:
   ```bash
   npx -y @mvanhorn/printing-press-library install ble-opaque-binary --cli-only --bin-dir ~/.local/bin
   ```
2. Verify: `ble-opaque-binary-pp-cli --version`
3. Ensure `~/.local/bin` is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Use `ble-opaque-binary-pp-cli capabilities --json` to inspect callable and withheld BLE capabilities, including safety classes and evidence refs. Use `ble-opaque-binary-pp-cli status --json` to inspect replay-backed status output. By default the CLI is replay-backed; build with `-tags ble_live` and pass `--live` to control a real device, `ble-opaque-binary-pp-cli doctor` to check live readiness, and `ble-opaque-binary-pp-cli scan --live` to discover devices. Session IPC scaffolding is generated only when the device spec enables device-session support.
