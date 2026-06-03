---
name: ble-temperature-sensor
description: Control BLE Temperature Sensor through the generated BLE device CLI.
---

## Prerequisites: Install the CLI

This skill drives the `ble-temperature-sensor-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press-library install ble-temperature-sensor --cli-only
   ```
2. Verify: `ble-temperature-sensor-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.

Use `ble-temperature-sensor-pp-cli capabilities --json` to inspect callable and withheld BLE capabilities, including safety classes and evidence refs. Use `ble-temperature-sensor-pp-cli status --json` to inspect replay-backed status output. Live BLE control and optional session IPC are generated only when device-session support is enabled by the device spec.
