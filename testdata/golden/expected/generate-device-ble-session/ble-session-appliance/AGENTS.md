# BLE Session Appliance Printed CLI Agent Guide

This directory is a generated `ble-session-appliance-pp-cli` BLE device CLI. It was produced by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press), so treat systemic fixes as upstream Printing Press fixes first. Keep local edits narrow and document why a generated-tree patch belongs here.

## Local Operating Contract

Start by asking the generated CLI for current runtime truth:

```bash
ble-session-appliance-pp-cli doctor --json
ble-session-appliance-pp-cli capabilities --json
```

`doctor` reports whether live BLE is compiled in, the active verify/dogfood state, the device's service UUIDs, and any operating quirks or proven workflows synthesized from the device's protocol sources. `capabilities` lists every telemetry field and command, including commands withheld from the callable surface and why.

This CLI is **replay-backed by default**: `status`, `telemetry`, and command runs return generated/captured values and never touch hardware. Contacting the physical device requires both a `--live` flag and a binary built with the live BLE backend:

```bash
go build -tags ble_live ./...
ble-session-appliance-pp-cli status --live --json
```

The default build links no BLE stack (pure-Go, no CGO). `scan` and any `--live` operation are no-ops without `-tags ble_live`; `doctor` and `capabilities` are safe to run anywhere and never actuate the device.

Inspect any command's help and prefer a dry run before a physical-effect command:

```bash
ble-session-appliance-pp-cli <command> --help
ble-session-appliance-pp-cli <command> --dry-run --json
```

Commands with a `physical-effect` or `configuration-risk` safety class require `--confirm-physical-effect` (or `--dry-run` to preview). Add `--json` for machine-readable output.

## Local Customizations

The default (no-codec) runtime is Tier-1: it writes captured command payloads verbatim and surfaces raw telemetry frames. To drive a device whose protocol needs framing, scaling, checksums, or parameterized values, extend the CLI **without editing generated files**:

- Implement `device.DeviceCodec` in an operator-owned file and register it from an `init` function (`codec = myCodec{}`). It encodes command payloads and decodes telemetry frames.
- Add hand-authored commands via the `novelCommands` hook in package `cli`: a preserved-across-regen file that sets the `novelCommands` var from `init`, adding commands built on `device.Dial` + `device.Link` for stateful, held-connection choreography.

Record each customization as one file per patch under `.printing-press-patches/` at this CLI's root (parallel to `.printing-press.json`) so the change isn't lost on the next regen and is visible to the next reader. One file per patch (`.printing-press-patches/<id>.json`) means two concurrent PRs never conflict on patch metadata.

Minimum shape:

```json
{
  "schema_version": 2,
  "id": "short-identifier",
  "applied_at": "YYYY-MM-DD",
  "base_run_id": "<copy from .printing-press.json>",
  "base_printing_press_version": "<copy from .printing-press.json>",
  "summary": "What changed (one sentence).",
  "reason": "Why this customization was needed (one or two sentences).",
  "files": ["internal/cli/novel_ops.go"],
  "validated_outcome": "Optional: non-obvious test result that confirms the fix."
}
```

Use `deferred_to_upstream` when a local patch is a temporary bridge for behavior the Printing Press should eventually generate correctly (a missing codec idiom, a quirk the generator could auto-handle). Search `mvanhorn/cli-printing-press` issues first; reuse a matching issue or open one, then set `upstream_issue` so the next regen knows what must supersede the patch:

```json
{
  "schema_version": 2,
  "id": "temporary-bridge",
  "summary": "What changed (one sentence).",
  "reason": "Why this customization was needed (one or two sentences).",
  "files": ["internal/cli/novel_ops.go"],
  "validated_outcome": "Optional: non-obvious test result that confirms the fix.",
  "deferred_to_upstream": [
    {
      "feature": "Generator behavior or device-protocol capability that should eventually supersede this patch",
      "reason": "Why the local patch is temporary or device-specific"
    }
  ],
  "upstream_issue": "https://github.com/mvanhorn/cli-printing-press/issues/<n>"
}
```

These entries are an **index of customizations**, not a second copy of the diff. Diffs live in `git`; the directory is what tells the next agent (or regeneration tooling) what was customized and why. Keep `summary` and `reason` short.

For install, longer product guidance, and the command surface, read `README.md` and `SKILL.md`. This file intentionally stays small so repo-local agents get invariant local guidance without duplicating the generated docs.
