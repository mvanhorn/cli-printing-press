# Device Spec

Device specs are the Printing Press artifact for local physical devices whose control surface is not HTTP-shaped. BLE is the first supported protocol.

## Purpose

A device spec preserves device-native evidence so generation can emit a CLI without pretending GATT services and characteristic writes are REST endpoints. It records:

- identity hints such as advertised names, service UUIDs, manufacturer data, and address policy
- BLE services, characteristics, and properties
- commands with payload encoding, evidence refs, validation status, and safety class
- telemetry fields with source characteristics and optional store hints
- session requirements for one-shot, optional, or required maintained connections
- the operational protocol contract (`transport:`), qualitative behavioral quirks (`quirks:`), and the proven operating sequences (`workflows:`)

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

## Protocol Contract

The action map in `capabilities` says what the bytes mean; the **protocol contract** says how to *talk to* the device. It is synthesized from reference implementations, docs, and captures during the research gate — in one pass, cited — and **verified** on hardware during dogfood, not rediscovered command-by-command. It has two halves.

`transport:` is the quantitative contract:

- `write_mode` — `acknowledged` (default; the device confirms each write before the next, so a control command is not dropped by an immediate disconnect) or `without-response`. The generator emits the matching write path.
- `command_spacing_ms` — minimum time between writes. When set, the generator emits a **paced writer**: every write sleeps the deficit, so a back-to-back burst is never dropped. No hand-authored pacing.
- `connect_ceremony` — ordered post-subscribe handshake steps (`{name, characteristic_uuid, value_hex, wait_ms}`).
- `settle_delays` — required pauses between state changes (`{name, ms}`).
- `poll_cadence_ms` — keep-alive / telemetry poll cadence.
- `teardown` — `keep-running` or `stop-on-disconnect`: does dropping the connection stop in-flight actuation?
- `single_client` — only one BLE client at a time.

`quirks:` is the qualitative contract — behavioral facts that do not reduce to a field (an init trick, a stale-session gotcha, a firmware-variant opcode shift, a notify-enable dance). Each is `{category, summary, handling, evidence_refs}`. They cannot drive codegen, so the generated CLI surfaces them in `doctor` (text + JSON), and they are required reading for the codec author and a line on the dogfood checklist.

`workflows:` is the proven **spine** — the ordered, end-to-end sequences that actually operate the device for each user goal (start a walk, stop the belt, pair-then-arm), composing the `transport:` facts and the `capabilities` action map into the steps the reference implementation is known to use. Each is `{name, goal, steps, evidence_refs, notes}` with ordered, human-readable `steps`. Like quirks it does not drive codegen; the generated CLI surfaces it in `doctor` (text + JSON), and it is the contract the implemented control flow (codec + held-connection choreography) is checked against in the QA **workflow-fidelity** pass — a missing, reordered, or invented step is a divergence to resolve before ship. Capturing the spine here is what stops an agent from rediscovering a documented sequence by guessing on hardware.

The contract and the workflow spine are the synthesis output and the dogfood checklist: each `transport:` field, `quirk:`, and `workflow:` is something to **confirm** on hardware, and the implementation is diffed against each workflow before ship. Authoring detail, the research-breadth rule, and the don't-relearn-cited-facts rule live in the `device-sniff-ble` skill reference.

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
