---
name: ble-session-appliance
description: Control BLE Session Appliance through the generated BLE device CLI.
---

Use `ble-session-appliance-pp-cli capabilities --json` to inspect callable and withheld BLE capabilities, including safety classes and evidence refs. Use `ble-session-appliance-pp-cli status --json` to inspect replay-backed status output. Use `ble-session-appliance-pp-cli start --dry-run --json` to preview the start write. To replay it outside verify mode, pass `--confirm-physical-effect` after checking the dry-run output. Use `ble-session-appliance-pp-cli session start --json` and `ble-session-appliance-pp-cli session status --json` to inspect the local replay session runtime, including lock, capability-token, and endpoint metadata. Use `ble-session-appliance-pp-cli telemetry capture --json` and `ble-session-appliance-pp-cli telemetry latest --json` for the local telemetry store scaffold. Use `ble-session-appliance-pp-cli telemetry sessions --json` to inspect stored BLE session summaries.
