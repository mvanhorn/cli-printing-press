# Device Sniff BLE

Use this reference when the requested CLI target is a local physical device controlled over Bluetooth/BLE rather than a public HTTP API or website.

## Routing

- Use `device-sniff ble` as the durable discovery path.
- Use `bluetooth-sniff` as the discoverable alias when the user asks for Bluetooth directly.
- Do not flatten BLE evidence into fake HTTP paths or methods.
- Do not make reusable artifacts vendor-specific. Product names belong only in examples, fixtures, or evidence labels.

Device Sniff is evidence-first. Community libraries, official docs, Android logs, Wireshark/nRF captures, and human action journals can guide discovery, but generated commands should remain tied to observed, replay-validated, or reference-backed BLE evidence.

## Standalone Hardware Probe

Use `ble-probe` when you need real-device evidence without printing a full CLI.

Build a live probe:

```bash
scripts/build-ble-probe.sh live
```

Build a copyable Windows probe from macOS:

```bash
scripts/build-ble-probe.sh live --target windows/amd64
```

Check the artifact before hardware work:

```bash
dist/ble-probe/live/$(go env GOOS)-$(go env GOARCH)/ble-probe doctor
```

Run non-actuating evidence capture first:

```bash
ble-probe scan --live --duration-ms 10000 > scan.json
ble-probe inspect --live --address ADDRESS > inspect.json
ble-probe read --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID > read.json
ble-probe subscribe --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID --duration-ms 10000 > notify.json
```

Use `write` only when a payload is known from observed evidence, docs, or a community protocol reference:

```bash
ble-probe write --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID --value-hex PAYLOAD_HEX > write.json
```

See `docs/BLE-PROBE.md` for macOS and Windows copy/run details.

## Device Sniff Command

Given normalized evidence:

```bash
cli-printing-press device-sniff ble \
  --input evidence.json \
  --output "$RESEARCH_DIR/<device>-device.yaml" \
  --analysis-output "$DISCOVERY_DIR/ble-analysis.json" \
  --evidence-output "$DISCOVERY_DIR/ble-evidence-redacted.json" \
  --json
```

The alias must produce the same backend result:

```bash
cli-printing-press bluetooth-sniff \
  --input evidence.json \
  --output "$RESEARCH_DIR/<device>-device.yaml"
```

Generate from the device spec:

```bash
cli-printing-press generate --spec "$RESEARCH_DIR/<device>-device.yaml" --validate
```

## Safety Stance

Safety labels are classification and provenance, not moral policing.

- Label accurately: `read-only`, `low-risk-write`, `physical-effect`, `configuration-risk`, or `unknown`.
- If unsure, use `unknown`.
- Observed or replay-validated commands can be generated even when labeled `physical-effect`.
- Unknown or insufficiently validated commands should stay metadata-only until stronger evidence exists.
- Normal verify/dogfood must not actuate real hardware. Verify-mode no-ops are expected for physical-effect writes.
- MCP read-only annotations must be conservative. False read-only is a bug; missing read-only is just a permission prompt.

## Session And UI Considerations

Session scaffolding is optional and device-driven.

- Omit session support for one-shot read/status devices.
- Emit session support when the spec declares low-latency repeated controls, notification streaming, telemetry sampling, reconnect sensitivity, or unreliable one-shot writes.
- Treat the generated session endpoint as local user-scoped control infrastructure for CLI commands and possible future UI apps, not as a public network API.
- For devices that allow only one client, document that the phone app may need to disconnect before laptop control.

## Evidence Order

Prefer this sequence for unknown devices:

1. Scan and inspect.
2. Read and subscribe.
3. Import docs, community protocol references, or external captures.
4. Correlate writes with an action journal.
5. Replay known payloads under operator-visible control.

Do not actively probe mutating payloads without guidance.
