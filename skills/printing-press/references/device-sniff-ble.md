# Device Sniff BLE

Use this reference when the requested CLI target is a local physical device controlled over Bluetooth/BLE rather than a public HTTP API or website.

## Routing

- Use `device-sniff ble` as the durable discovery path.
- Use `bluetooth-sniff` as the discoverable alias when the user asks for Bluetooth directly.
- Do not flatten BLE evidence into fake HTTP paths or methods.
- Do not make reusable artifacts vendor-specific. Product names belong only in examples, fixtures, or evidence labels.

Device Sniff is evidence-first. Community libraries, official docs, Android logs, Wireshark/nRF captures, and human action journals can guide discovery, but generated commands should remain tied to observed, replay-validated, or reference-backed BLE evidence.

## Mapping Research Gate

BLE device discovery has a hard mapping gate. A scan or service inspection can tell you that a device exposes characteristics, but it cannot tell you what command payloads mean. Before generating callable control commands or running a live write, establish the action-to-payload mapping from a concrete source.

Accepted mapping sources:

- A user-provided device spec, payload table, or action journal.
- Official docs, protocol notes, SDKs, or vendor examples.
- Community libraries, reverse-engineering notes, GitHub issues, forum posts, or examples that map command names to services, characteristics, and payload bytes.
- Android/iOS Bluetooth logs, btsnoop captures, Wireshark/nRF captures, or other external captures.
- A human action journal correlated with observed writes, where one action maps cleanly to one payload/characteristic candidate.

Required behavior:

- Research mappings before relying on live probing for controls. Search by product name/model, advertised BLE name, service UUIDs, app package name, and known library names.
- Treat scan/inspect/read/subscribe evidence as identity and telemetry discovery unless it is paired with mapping evidence.
- If no mapping source is found, generate a read/status/capabilities-only CLI or stop and ask the user for mapping evidence. Do not create callable write commands from raw GATT shape alone.
- Do not brute-force, fuzz, or actively probe mutating payloads on a physical device.

## Standalone Hardware Probe

Use `cli-printing-press device-sniff ble` when you need real-device evidence without printing a full CLI.

Use the standalone `ble-probe` binary only as a portable diagnostic fallback for machines that do not have the full Printing Press binary.

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
cli-printing-press device-sniff ble doctor
```

For a copied standalone artifact:

```bash
dist/ble-probe/live/$(go env GOOS)-$(go env GOARCH)/ble-probe doctor
```

Run non-actuating evidence capture first:

```bash
cli-printing-press device-sniff ble scan --live --duration-ms 10000 > scan.json
cli-printing-press device-sniff ble inspect --live --address ADDRESS > inspect.json
cli-printing-press device-sniff ble read --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID > read.json
cli-printing-press device-sniff ble subscribe --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID --duration-ms 10000 > notify.json
```

Use `write` only when a payload is known from observed evidence, docs, or a community protocol reference:

```bash
cli-printing-press device-sniff ble write --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID --value-hex PAYLOAD_HEX > write.json
```

Merge capture pieces before analysis:

```bash
cli-printing-press device-sniff ble merge --redact-term PERSONAL_TERM scan.json inspect.json read.json notify.json write.json > evidence.json
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
  --redact-term PERSONAL_TERM \
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
- Physical-effect and configuration-risk writes should require dry-run preview or an explicit confirmation flag before non-verify replay.
- Normal verify/dogfood must not actuate real hardware. Verify-mode no-ops are expected for physical-effect writes.
- MCP read-only annotations must be conservative. False read-only is a bug; missing read-only is just a permission prompt.

## Generated Live Control (Tier-1 vs Tier-2)

The generated CLI is **replay-backed by default**: commands echo the captured payload they would send and `status` reports the telemetry shape, without opening a connection. This default build is pure-Go, CGO-free, and never touches hardware (verify/dogfood/`go test` stay safe). Real control is opt-in on two axes:

- **Build tag** `-tags ble_live` links the real BLE adapter (`go build -tags ble_live ./...`; CGO/CoreBluetooth on macOS, pure-Go D-Bus on Linux, WinRT on Windows).
- **Runtime flag** `--live` (with optional `--address`, `--timeout`) actuates the device. Physical-effect/configuration-risk commands still require `--confirm-physical-effect`; verify mode short-circuits before dialing.

Before shipping, classify the device and act accordingly:

**Tier-1 — fixed-payload commands + readable telemetry.** Works end to end with **zero hand-authoring**. The generated `LiveTransport` scans by service UUID, connects, writes each command's captured payload to its characteristic, and reads readable telemetry characteristics. Nothing to implement — verify `go build -tags ble_live ./...` compiles and (if you have hardware) `--live` actuates.

**Tier-2 — stateful or parameterized protocols.** A device whose commands need computed framing (checksums, sequence counters), value scaling (e.g. km/h → protocol units), notify-based telemetry frames, or a held-connection poll loop **cannot be driven from static captured evidence**. The generic transport will write the raw captured bytes, which is usually wrong for these devices. You MUST, in operator-owned files preserved across regeneration:

1. Write a **codec** (pure Go, no BLE) that builds command frames and parses telemetry frames — the single source of truth for the wire format, grounded in the mapping evidence and cited protocol references. Add a `codec_test.go` that tests it with no hardware.
2. Add **hand-authored commands** via the `novelCommands` hook (set it from an `init` in your own file in `internal/cli`; it is preserved as a NOVEL file across regen). Use the generated, exported `device.Dial(ctx, address, timeout) (device.Link, error)` to open a connection and `device.Link` (`Write`/`Read`/`Subscribe`/`Close`) to drive it with your codec — do not reimplement the BLE backend.
3. Optionally register a `device.DeviceCodec` (`codec = myCodec{}`) so the generated `status` surfaces decoded telemetry values.
4. **Gate every live command** on `cliutil.IsVerifyEnv()` (no-op under verify; `device.Dial` also refuses with `ErrVerifyMode` as a backstop) and `cliutil.IsDogfoodEnv()` (bound long-running work). Keep the print-by-default / `--live`-to-actuate stance and conservative MCP annotations.
5. Verify before ship: `go test ./...` (covers the codec and the generated Tier-1 path), `go build -tags ble_live ./...` compiles, and the live commands actuate on hardware when available.

A printed Tier-2 CLI whose live commands silently no-op or write wrong bytes is a failure, not an acceptable outcome — detect the stateful protocol and implement the codec + novel commands. The reverse-engineered protocol logic is irreducible per-device work; the BLE plumbing, connection management, build tags, flags, and safety gating are already generated.

## Session And UI Considerations

Session scaffolding is optional and device-driven.

- Omit session support for one-shot read/status devices.
- Emit session support when the spec declares low-latency repeated controls, notification streaming, telemetry sampling, reconnect sensitivity, or unreliable one-shot writes.
- Treat the generated session endpoint as local user-scoped control infrastructure for CLI commands and possible future UI apps, not as a public network API.
- For devices that allow only one client, document that the phone app may need to disconnect before laptop control.

## Evidence Order

Prefer this sequence for unknown devices:

1. Identify the exact product, app, advertised BLE name, and service UUIDs.
2. Search for protocol mappings in docs, community code, issues, forums, app logs, or captures.
3. Scan and inspect to confirm identity and available characteristics.
4. Read and subscribe for non-mutating telemetry evidence.
5. Correlate observed writes with an action journal or imported capture.
6. Replay known payloads under operator-visible control.

Do not actively probe mutating payloads without guidance.
