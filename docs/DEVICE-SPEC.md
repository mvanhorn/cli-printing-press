# Device Spec

Device specs are the Printing Press artifact for local physical devices whose control surface is not HTTP-shaped. BLE is the first supported protocol.

## Purpose

A device spec preserves device-native evidence so generation can emit a CLI without pretending GATT services and characteristic writes are REST endpoints. It records:

- identity hints such as advertised names, service UUIDs, manufacturer data, and address policy
- BLE services, characteristics, and properties
- commands with payload encoding, evidence refs, validation status, and safety class
- telemetry fields with source characteristics and optional store hints
- session requirements for one-shot, optional, or required maintained connections

## Discovery Flow

`device-sniff ble` consumes normalized BLE evidence and writes:

- a device spec YAML file
- a BLE analysis report JSON file
- a redacted evidence JSON file

`bluetooth-sniff` is an alias for the same BLE backend.

Generate from the spec with:

```bash
cli-printing-press generate --spec <device-spec.yaml> --validate
```

## Evidence And Uncertainty

Commands must remain traceable to observed, replay-validated, or reference-backed evidence. Unknown binary payload bytes, counters, checksums, and ambiguous action correlations should be preserved rather than over-decoded.

If multiple devices match the same name/service/RSSI hints, discovery should require explicit operator selection before replaying controls.

## Safety

Safety labels classify command behavior:

- `read-only`
- `low-risk-write`
- `physical-effect`
- `configuration-risk`
- `unknown`

These labels inform generated metadata, MCP annotations, CLI confirmation flags, and verification behavior. `physical-effect` and `configuration-risk` commands require `--confirm-physical-effect` for non-dry-run replay outside verify mode. If the evidence is uncertain, use `unknown`; the generator withholds insufficiently validated commands from the callable surface while keeping them visible in capability metadata.

Normal verification must prove wiring through replay or no-op behavior and must not actuate real hardware.

## Real Hardware Probe

Use `cli-printing-press device-sniff ble scan|inspect|read|subscribe|merge` to gather real-device evidence without printing a full CLI. Use the standalone `ble-probe` binary only when you need a copyable diagnostic artifact for another machine. See `docs/BLE-PROBE.md` for macOS, Linux, and Windows build/run commands.
