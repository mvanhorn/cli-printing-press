# Device Sniff BLE

Use this reference when the requested CLI target is a local physical device controlled over Bluetooth/BLE rather than a public HTTP API or website.

## Routing

- Use `device-sniff ble` as the durable discovery path.
- Use `bluetooth-sniff` as the discoverable alias when the user asks for Bluetooth directly.
- Do not flatten BLE evidence into fake HTTP paths or methods.
- Do not make reusable artifacts vendor-specific. Product names belong only in examples, fixtures, or evidence labels.

Device Sniff is evidence-first. Community libraries, official docs, Android logs, Wireshark/nRF captures, and human action journals can guide discovery, but generated commands should remain tied to observed, replay-validated, or reference-backed BLE evidence.

## Protocol Contract Synthesis Gate

BLE device discovery has a hard gate. A scan or service inspection tells you a device exposes characteristics, but not what its command payloads mean, nor how to *operate* the device. Before generating callable control commands or running a live write, synthesize the device's protocol contract from concrete sources — in one deliberate research pass, not command-by-command on hardware. The gate has three parts; satisfy all three.

**1. The action map — what the bytes mean.** Establish action -> service/characteristic/payload from a concrete source.

Accepted sources:

- A user-provided device spec, payload table, or action journal.
- Official docs, protocol notes, SDKs, or vendor examples.
- Community libraries, reverse-engineering notes, GitHub issues, forum posts, or examples that map command names to services, characteristics, and payload bytes.
- Android/iOS Bluetooth logs, btsnoop captures, Wireshark/nRF captures, or other external captures.
- A human action journal correlated with observed writes, where one action maps cleanly to one payload/characteristic candidate.

**2. The operational contract — how to talk to the device.** The same sources that give you payloads almost always document the *operating* contract too, and it is exactly the part that gets silently rediscovered on hardware if you skip it. Extract it into the spec's `transport:` block and `quirks:` list (see "Protocol Contract" below): write semantics, command spacing, connect ceremony and ordering, settle delays, poll cadence, teardown behavior, single-client constraints, and qualitative quirks (init tricks, stale-session gotchas, firmware-variant opcode shifts, notify-enable dances).

**3. The workflow spine — how the device is *operated* end to end.** Commands and the operating contract are the parts; the **workflows** are how a working client assembles them into a sequence that actually achieves each user goal (start a walk, stop the belt, pair-then-arm). The reference encodes these sequences directly — the order of mode → settle → start, the wait-for-running before setting speed, the stop ceremony. Understanding individual commands is not enough; the *spine* is where a from-scratch implementation goes wrong. Extract each primary goal's proven sequence into the spec's `workflows:` list (`{name, goal, steps, evidence_refs}`) — ordered and cited — so the QA fidelity pass and the dogfood checklist have a contract to check the implementation against. This is the part that, when left unwritten, gets rediscovered by guessing on hardware. Capture it once.

Required behavior:

- **Run an explicit external web-research pass; do not stop at the seeds.** A user-provided repo or doc is seed #1, not the finish line — go wider before you build. Web-search and GitHub-code-search by product name/model, advertised BLE name, service UUIDs, app package name, and known library names; pull Home Assistant / ESPHome integrations, vendor docs and protocol notes, issues, and forum threads. Find the most complete reference, and cross-check the contract across **at least two independent sources** when they exist — a wrapper that merely imports another library is not an independent source.
- **Record what you consulted (a research ledger).** In the discovery output, list the sources you actually examined — URL, what each contributed, how independent it is — so breadth is an artifact, not an assumption. `evidence_refs` that all trace back to the single repo the user handed you is a red flag: state plainly whether ≥2 independent sources corroborate the contract, or why only one credibly exists.
- **Cite reference repos; do not vendor them into the manuscript.** Cloning a reference library to study its protocol is expected, but it is research *input*, not authored manuscript *output*. Download it to scratch *outside* the manuscript tree (e.g. a temp dir) and record only the URL + commit (in `evidence_refs` and the ledger) — never copy a third-party repo into `manuscripts/<slug>/research/sources/`. Publishing copies of someone else's code is a licensing problem and a secret/PII vector. Publish excludes any `sources/` from shipped manuscripts as a machine backstop, but keep it out in the first place.
- **Synthesize once, then build.** Capture the action map into `capabilities`, the operational contract into `transport:` + `quirks:`, and the proven sequences into `workflows:`, each with `evidence_refs`, before writing the codec or touching hardware.
- **Don't relearn cited facts.** If a source states a timing, ordering, or write-semantics fact (a command-spacing minimum, a subscribe-before-handshake order, an acknowledged-write requirement, an init quirk), record it in the contract and implement it from the citation. Hardware trial-and-error is for genuinely undocumented gaps only — never to re-derive a fact a reference already states.
- Treat scan/inspect/read/subscribe evidence as identity and telemetry discovery unless it is paired with mapping evidence.
- If no mapping source is found, generate a read/status/capabilities-only CLI or stop and ask the user for mapping evidence. Do not create callable write commands from raw GATT shape alone.
- Do not brute-force, fuzz, or actively probe mutating payloads on a physical device.

## Protocol Contract

The device spec carries three views of "how this device works", alongside the action map in `capabilities`:

**`transport:` — the quantitative contract.** Fields the generator consumes or the codec needs:

- `write_mode: acknowledged | without-response` — acknowledged (default) confirms each write before the next, so a control command is not dropped by an immediate disconnect. The generator emits the matching write path.
- `command_spacing_ms: <int>` — minimum time between writes. The firmware drops a command sent sooner than this after the previous one, so a burst loses all but the first. When set, the generator emits a paced writer that sleeps the deficit before every write — you do not hand-author pacing.
- `connect_ceremony: [{name, characteristic_uuid, value_hex, wait_ms}]` — the post-subscribe handshake some devices need before they accept control or stream telemetry. Codec/choreography input today; codegen is on the roadmap.
- `settle_delays: [{name, ms}]` — required pauses between state changes (e.g. a mode switch before a start).
- `poll_cadence_ms: <int>` — keep-alive / telemetry poll cadence.
- `teardown: keep-running | stop-on-disconnect` — does dropping the connection stop in-flight actuation?
- `single_client: <bool>` — only one BLE client at a time (the vendor app may need to disconnect first).

**`quirks:` — the qualitative contract.** Behavioral facts that do not reduce to a field but must be factored in: an init trick, a stale-session gotcha, a firmware-variant opcode shift, a notify-enable dance. Each is `{category, summary, handling, evidence_refs}`. They cannot drive codegen, so the generated CLI surfaces them in `doctor` (text + JSON); they are required reading for the codec author and a line on the dogfood checklist.

**`workflows:` — the proven spine.** The ordered, end-to-end sequences that operate the device for each user goal — start a walk, stop the belt, pair-then-arm — composing the `transport:` facts and the `capabilities` action map into the steps a working reference is known to use. Each is `{name, goal, steps, evidence_refs, notes}` with ordered, human-readable `steps`. It does not drive codegen; the generated CLI surfaces it in `doctor` (text + JSON), and it is the contract the implemented control flow (codec + held-connection choreography) is checked against in the QA workflow-fidelity pass (see below) and a line on the dogfood checklist. Capturing the spine is what keeps a from-scratch implementation from rediscovering a documented sequence by guessing.

Capture every transport field, quirk, and workflow you have evidence for, and cite each. The contract and the spine are the synthesis output — and they are your dogfood checklist: each entry is something to **confirm** on hardware, not rediscover. A divergence between the recorded contract/spine and observed behavior is a spec correction, not a fresh discovery.

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

1. Write a **codec** (pure Go, no BLE) implementing `device.DeviceCodec` — `EncodeCommand(command, args)` builds a command's payload (using its positional CLI args for parameterized commands) and `DecodeTelemetry(field, raw)` turns a telemetry frame into a typed value. This is the single source of truth for the wire format, grounded in the mapping evidence and cited protocol references. Add a `codec_test.go` that tests it with no hardware. Register it from an `init` (`codec = myCodec{}`).
2. **Declare parameterized commands in the device spec** (`commands[].parameters: [{name, type}]`). The generator then emits the command with its `<arg>` usage, exact-arg validation, safety gating, dry-run, and verify no-op, and routes the args to your `EncodeCommand`. You do not hand-author the cobra command — only the encode logic. (A parameterized command with no registered codec is a hard error, not a silent static write.)
3. For **stateful choreography only** (hold a connection and poll, multi-step sequences), add a hand-authored command via the `novelCommands` hook (set it from an `init` in your own file in `internal/cli`; preserved as a NOVEL file across regen). Use the exported `device.Dial(ctx, address, timeout) (device.Link, error)` + `device.Link` (`Write`/`Read`/`Subscribe`/`Close`) with your codec — do not reimplement the BLE backend. Drive the choreography from the `transport:` contract: the connect ceremony, ordering (subscribe before handshake), settle delays, and poll cadence are recorded there, not re-derived. **Command spacing and write mode are already handled** — when `transport.command_spacing_ms` is set the generated `device.Link.Write` paces every write, and `transport.write_mode` selects the write path. Do not re-implement a paced-writer wrapper.
4. **Gate every hand-authored live command** on `cliutil.IsVerifyEnv()` (no-op under verify; `device.Dial` also refuses with `ErrVerifyMode` as a backstop) and `cliutil.IsDogfoodEnv()` (bound long-running work). Generated commands already carry this gating. Keep the print-by-default / `--live`-to-actuate stance and conservative MCP annotations.
5. Verify before ship: `go test ./...` (covers the codec and the generated Tier-1 path), `go build -tags ble_live ./...` compiles, and the live commands actuate on hardware when available.

A printed Tier-2 CLI whose live commands silently no-op or write wrong bytes is a failure, not an acceptable outcome — detect the stateful protocol and implement the codec. The reverse-engineered protocol logic is irreducible per-device work; the command surface, BLE plumbing, connection management, build tags, flags, and safety gating are generated.

## QA: Workflow Fidelity

Before ship — once the codec and any held-connection choreography exist, and again during dogfood — diff the implemented control flow against each `workflows:` spine. `<cli> doctor --json` surfaces the recorded workflows as the deterministic inventory; for each one, walk the implementation (the codec's `EncodeCommand`, the novel choreography command, the ordering, settle delays, and waits) and confirm every step is present, in order, with nothing invented. Resolve any missing, reordered, or fabricated step before ship.

Assuming the cited source is credible (which it is when corroborated by the research pass above), a divergence from the proven spine is the prime suspect — caught here rather than through haphazard hardware trial-and-error. This is judgment, not a mechanical gate, and it is not definitive: the source can be wrong or the device variant can differ, which is exactly what hardware dogfood is for. But "does this follow the proven sequence?" beats guessing. A divergence the agent can justify (a documented better path, a known variant difference) is a spec note, not a silent deviation.

## Session And UI Considerations

Session scaffolding is optional and device-driven.

- Omit session support for one-shot read/status devices.
- Emit session support when the spec declares low-latency repeated controls, notification streaming, telemetry sampling, reconnect sensitivity, or unreliable one-shot writes.
- Treat the generated session endpoint as local user-scoped control infrastructure for CLI commands and possible future UI apps, not as a public network API.
- For devices that allow only one client, document that the phone app may need to disconnect before laptop control.

## Evidence Order

Prefer this sequence for unknown devices:

1. Identify the exact product, app, advertised BLE name, and service UUIDs.
2. Run an external web-research pass for protocol sources (docs, community code, HA / ESPHome, issues, forums, app logs, captures), expanding from any user-provided seeds rather than stopping at them; record the sources consulted and whether ≥2 are independent.
3. Synthesize the action map (`capabilities`), the operational contract (`transport:` + `quirks:`), and the proven workflows (`workflows:`) from the best sources, cross-checked and cited.
4. Scan and inspect to confirm identity and available characteristics.
5. Read and subscribe for non-mutating telemetry evidence.
6. Correlate observed writes with an action journal or imported capture.
7. Replay known payloads under operator-visible control.
8. Dogfood to **verify** the contract and spine on hardware — confirm each `transport:` field and `quirk:` holds, and that the implemented control flow follows each `workflow:` (the QA workflow-fidelity pass); a divergence is a spec correction, not a fresh discovery.

Do not actively probe mutating payloads without guidance.
