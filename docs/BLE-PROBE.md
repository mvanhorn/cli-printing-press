# BLE Probe Hardware Testing

`cli-printing-press device-sniff ble` includes hardware smoke commands for the BLE device-sniff path. It lets you scan, inspect, read, subscribe, and capture write evidence without generating a full printed CLI.

`ble-probe` is the same probe surface packaged as a standalone binary. Use it when you need a copyable diagnostic artifact for Windows or Linux machines that do not have the full Printing Press checkout or binary.

## Build

Build a live BLE probe for the current machine:

```bash
scripts/build-ble-probe.sh live
```

Build a copyable Windows artifact from macOS:

```bash
scripts/build-ble-probe.sh live --target windows/amd64
```

The artifacts land under `dist/ble-probe/live/<goos>-<goarch>/ble-probe` with `.exe` on Windows.

Replay-only artifacts do not need Bluetooth permissions and are useful for CI or evidence fixtures:

```bash
scripts/build-ble-probe.sh replay --target darwin/arm64
```

## Check The Binary

Run the doctor command before hardware work:

```bash
cli-printing-press device-sniff ble doctor
```

For the standalone binary:

```bash
dist/ble-probe/live/darwin-arm64/ble-probe doctor
```

On Windows PowerShell:

```powershell
.\ble-probe.exe doctor
```

The `live.compiled` field must be `true` for real device scans. If it is `false`, the binary is replay-only and should be rebuilt with the `live` mode.

## macOS Smoke Test

Give the terminal or host app Bluetooth permission in System Settings if macOS prompts for it.

```bash
cli-printing-press device-sniff ble scan --live --duration-ms 10000 > scan.json
cli-printing-press device-sniff ble inspect --live --address '<address-from-scan>' > inspect.json
```

Read and subscribe commands are non-actuating ways to gather characteristic evidence:

```bash
cli-printing-press device-sniff ble read --live --address '<address>' --service '<uuid>' --characteristic '<uuid>' > read.json
cli-printing-press device-sniff ble subscribe --live --address '<address>' --service '<uuid>' --characteristic '<uuid>' --duration-ms 10000 > notify.json
```

Merge the capture pieces before analysis:

```bash
cli-printing-press device-sniff ble merge --redact-term '<personal-name-or-room-name>' scan.json inspect.json read.json notify.json > evidence.json
cli-printing-press device-sniff ble --input evidence.json --output device.yaml --redact-term '<personal-name-or-room-name>' --json
```

Use `--redact-term` for owner names, room names, or other personal strings that appear in advertised device names. Redaction terms are used for the archived evidence artifact and are not written back into that redacted artifact.

## Windows Smoke Test

Copy `dist/ble-probe/live/windows-amd64/ble-probe.exe` to the Windows machine, then run:

```powershell
.\ble-probe.exe doctor
.\ble-probe.exe scan --live --duration-ms 10000 > scan.json
.\ble-probe.exe inspect --live --address '<address-from-scan>' > inspect.json
```

Use a current Windows 11 machine with a working Bluetooth adapter. If Windows blocks access, run from a normal PowerShell session after pairing permissions or Bluetooth adapter setup has been handled by the OS.

## Capturing Control Evidence

`write` is explicit because it can change device state:

```bash
cli-printing-press device-sniff ble write --live --address '<address>' --service '<uuid>' --characteristic '<uuid>' --value-hex '<payload>' > write.json
```

For unknown devices, prefer this order:

1. `scan`
2. `inspect`
3. `read`
4. `subscribe`
5. `write` only when a payload is known from docs, a community library, or prior observed evidence

Live `inspect` records the discovered service and characteristic UUIDs but not their read/write/notify property flags — the underlying `tinygo.org/x/bluetooth` backend does not expose a characteristic property bitmask after discovery. Writability is therefore inferred from action markers and community references (the same signals the replay path uses), not auto-detected from a live GATT scan. Annotate control characteristics through those evidence fields rather than relying on `inspect` alone.

The JSON files are normalized BLE evidence inputs. Use `ble-probe merge` to combine multiple capture files into one analyzer input before running `device-sniff ble`.
