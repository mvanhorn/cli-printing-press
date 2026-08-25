# Studio command JSON surfaces

Machine-readable contracts for **Press Studio** (and other subprocess consumers) that spawn `cli-printing-press` with `--json` and parse **stdout**. Product orchestration rules live in the MLX-Studio contract ([`PRESS_STUDIO_CONTRACT.md`](https://github.com/paride-cmd/MLX-Studio/blob/main/docs/PRESS_STUDIO_CONTRACT.md) §4 C3, §5 C2). This file documents **engine** argv, exit codes, and JSON shapes only.

**Integration rules (engine side):**

1. Prefer `--json` when the flag exists; parse a single JSON value from **stdout**.
2. Treat **stderr** as human progress / warnings (never as the structured payload).
3. Typed exit codes: `internal/cli/exitcodes.go` — `0` success, `1` input, `2` spec, `3` generation, `4` unknown, `5` publish. Some commands document additional exit semantics below.
4. Do **not** expect an HTTP server from this binary in v1.

Artifact roots: [`ARTIFACTS.md`](ARTIFACTS.md). BLE probe ops: [`BLE-PROBE.md`](BLE-PROBE.md).

---

## §4.2 checklist (verified)

| Contract action | Command | `--json` | Status |
|-----------------|---------|----------|--------|
| List projects | `library list --json` | yes | **Verified** — clean array on stdout |
| Full quality sweep | `shipcheck --dir <lib>/<slug> --json` | yes | **Verified** — end-of-run envelope; legs discarded in JSON mode |
| Structural check | `dogfood --dir <path> --json` | yes | **Verified** — parse `verdict` (structural FAIL still exits `0`) |
| Runtime check | `verify --dir <path> --json` | yes | **Verified** — fix-loop progress goes to stderr (stdout stays one JSON value) |
| Score | `scorecard --dir <path> --json` | yes | **Verified** — grade is informational (exit not grade-gated) |
| Generate | `generate --spec … --output … --json` | yes | **Verified** — multi-shape; see below |
| BLE analyze | `device-sniff ble --input … --output … --json` | yes | **Verified** — summary + on-disk artifacts |
| Publish preflight | `publish validate --dir … --json` | yes | **Verified** — fail → exit `5`, `Silent` |
| Auth overview | `auth doctor --json` | yes | **Verified** — diagnostic; findings do not fail exit |
| Version gate | `version --json` | yes | **Verified** |

---

## Global exit codes

```go
// internal/cli/exitcodes.go
ExitSuccess = 0
ExitInputError = 1
ExitSpecError = 2
ExitGenerationError = 3
ExitUnknownError = 4
ExitPublishError = 5
```

`ExitError.Silent` skips printing the error text when stdout already carries structured failure details (`cmd/cli-printing-press/main.go`).

---

## `library list`

**Argv**

```bash
cli-printing-press library list --json
```

**Exit:** `0` on success (including empty library); scan failure → `1` (`ExitInputError`).

**Stdout:** JSON array of `LibraryEntry` (`internal/cli/library.go`):

| Field | JSON key |
|-------|----------|
| CLIName | `cli_name` |
| Dir | `dir` |
| APIName | `api_name` |
| Category | `category` |
| Regions | `regions` |
| APILanguage | `api_language` |
| Description | `description` |
| Modified | `modified` (RFC3339 via `time.Time`) |

Scans `pipeline.PublishedLibraryRoot()` (`$PRINTING_PRESS_HOME/library` or `~/printing-press/library`).

---

## `shipcheck`

**Argv**

```bash
cli-printing-press shipcheck \
  --dir ~/printing-press/library/<slug> \
  [--spec <openapi-or-device.yaml>] \
  [--research-dir <run-dir>] \
  --json
```

**Exit:** Umbrella code = max effective leg exit (`shipcheckUmbrellaCode`). `HOLD` (unverified scorecard) can be non-zero with `Silent: true`.

**Stdout:** `shipcheckJSONEnvelope` (`internal/cli/shipcheck.go`):

```json
{
  "passed": true,
  "exit_code": 0,
  "verdict": "PASS",
  "reason": "",
  "started_at": "2026-08-25T12:00:00Z",
  "elapsed_ms": 120000,
  "legs": [
    {
      "name": "dogfood",
      "exit_code": 0,
      "passed": true,
      "verdict": "PASS",
      "detail": "",
      "started_at": "2026-08-25T12:00:00Z",
      "elapsed_ms": 30000,
      "command": "cli-printing-press dogfood --dir ..."
    }
  ]
}
```

`verdict`: `PASS` | `FAIL` | `HOLD`. Sensitive flag values in `legs[].command` are redacted.

**JSON mode behavior:** per-leg stdout/stderr are discarded so the envelope is the only stdout payload. For per-leg JSON detail, run that leg standalone with `--json`.

**Legs (order):** verify → validate-narrative → dogfood → workflow-verify → apify-audit → verify-skill → scorecard.

**Artifacts:** legs may update `.printing-press.json` via `--write-manifest` when that flag is passed through.

---

## `dogfood`

**Argv (structural — Studio §4.2)**

```bash
cli-printing-press dogfood --dir <cli-dir> [--spec <path>] [--research-dir <dir>] --json
```

**Argv (live — not required for Studio v1 list)**

```bash
cli-printing-press dogfood --dir <cli-dir> --live [--level quick|full] [--write-acceptance <path>] --json
```

**Exit**

| Mode | FAIL verdict |
|------|----------------|
| Structural | Still **exit `0`** after a successful run — Studio **must** read `verdict` / `issues` |
| Live | `FAIL` → exit `3` (`ExitGenerationError`) |
| Runner error | exit `3` |

**Stdout (structural):** `pipeline.DogfoodReport` (`internal/pipeline/dogfood.go`) — key fields: `dir`, `verdict`, nested `*_check` objects, `issues[]`.

**Stdout (live):** `pipeline.LiveDogfoodReport` (`internal/pipeline/live_dogfood.go`) — `verdict`, `pass_rate`, `tests[]`, `matrix_size`, …

**Artifacts:** optional `--write-acceptance` → Phase 5 acceptance JSON.

---

## `verify`

**Argv (Studio)**

```bash
cli-printing-press verify --dir <cli-dir> [--spec <path>|--no-spec] [--threshold 80] [--write-manifest <path>] --json
```

Do **not** pass `--fix` for Studio v1 unless you need the fix loop. Fix-loop progress prints on **stderr** so `--json` stdout remains a single JSON value.

**Exit (JSON mode):** `verdict == FAIL` → exit `3`, `Silent: true`. `WARN` → exit `0`. Human mode uses process exit `1` on FAIL (inconsistent with JSON — prefer JSON for Studio).

**Stdout:**

```json
{
  "verify": { /* pipeline.VerifyReport */ },
  "fix_loop": { /* optional pipeline.FixLoopReport */ }
}
```

`VerifyReport` (`internal/pipeline/runtime.go`): `mode`, `total`, `passed`, `failed`, `pass_rate`, `verdict` (`PASS`|`WARN`|`FAIL`), `results[]`, `binary`, …

**Artifacts:** `--write-manifest` updates `.printing-press.json`; `--cleanup` removes transient build artifacts.

---

## `scorecard`

**Argv**

```bash
cli-printing-press scorecard --dir <cli-dir> [--spec <path>] [--research-dir <dir>] [--live-check] [--write-manifest <path>] --json
```

**Exit:** Run errors → `1`/`3`. Low `overall_grade` does **not** force a non-zero exit.

**Stdout:**

```json
{
  "scorecard": { /* pipeline.Scorecard */ },
  "live_check": { /* optional */ }
}
```

`Scorecard` (`internal/pipeline/scorecard.go`): `api_name`, `steinberger`, `competitor_scores`, `overall_grade`, `gap_report`, … Treat new Steinberger dimensions as additive.

---

## `generate`

**Argv (Studio §4.2)**

```bash
cli-printing-press generate --spec <path-or-url> --output <dir> [--force] --json
```

Also: `--docs <url>`, `--plan <file>`, or device YAML via `--spec`.

**Exit:** typed `1` / `2` / `3` via `ExitError`.

**Stdout:** one JSON object; **shape depends on mode** (ad-hoc maps in `internal/cli/root.go`):

| Mode | Keys |
|------|------|
| OpenAPI / docs | `name`, `output_dir`, `spec_files`, `validated`, `polished` |
| Plan | `name`, `output_dir`, `plan_file`, `commands` |
| Device spec | OpenAPI keys + `protocol` |

Progress and warnings print on **stderr** even with `--json`. Cancel long runs with `SIGTERM`.

**Artifacts:** CLI tree under `--output` or `~/printing-press/library/<slug>/`; `.printing-press.json`; archived `openapi.*` / `device-spec.yaml`.

---

## `device-sniff ble` (analyze)

**Argv**

```bash
cli-printing-press device-sniff ble \
  --input evidence.json \
  --output device.yaml \
  [--analysis-output analysis.json] \
  [--evidence-output redacted-evidence.json] \
  [--redact-term '<term>'] \
  --json
```

**Exit:** failures typically surface as untyped errors → exit `4` unless wrapped.

**Stdout:** `deviceSniffSummary` (`internal/cli/device_sniff.go`) — matches contract §5.5:

```json
{
  "device_spec_path": "/path/device.yaml",
  "analysis_path": "/path/analysis.json",
  "evidence_path": "/path/evidence.json",
  "commands": 2,
  "telemetry": 1,
  "requires_operator_selection": false,
  "warnings": []
}
```

**On-disk:** DeviceSpec YAML, analysis report JSON, redacted evidence JSON (defaults under `~/.cache/printing-press/device-sniff/` when `--output` omitted).

### Probe subcommands (always JSON stdout; no `--json` flag)

```bash
cli-printing-press device-sniff ble doctor
cli-printing-press device-sniff ble scan --input fixtures.json   # or --live --duration-ms 10000
cli-printing-press device-sniff ble inspect --live --address '<addr>'
cli-printing-press device-sniff ble read --live --address '<addr>' --service '<uuid>' --characteristic '<uuid>'
cli-printing-press device-sniff ble subscribe --live --address '<addr>' --service '<uuid>' --characteristic '<uuid>' --duration-ms 10000
cli-printing-press device-sniff ble write --live --address '<addr>' --service '<uuid>' --characteristic '<uuid>' --value-hex '<hex>'
cli-printing-press device-sniff ble merge [--redact-term '<term>'] scan.json inspect.json > evidence.json
```

| Command | Stdout type |
|---------|-------------|
| `doctor` | `bleprobe.DoctorReport` — `binary`, `goos`, `goarch`, `live`, `replay_supported`, `smoke_commands`, `hardware_commands` |
| `scan` / `inspect` / `read` / `subscribe` / `write` / `merge` | `ble.EvidenceInput` |

`--live` is refused under `PRINTING_PRESS_VERIFY=1`. See [`BLE-PROBE.md`](BLE-PROBE.md).

### C2 evidence schema

Go type: `internal/devicesniff/ble/types.go` → `EvidenceInput`.

Required for v1 clients: `name`, `identity` (`advertised_names`, `address_policy`, `match_strength`). Optional: `display_name`, `redaction_terms`, `events[]`, `actions[]`, `community_references[]`.

### Golden BLE fixtures (compatibility)

Press Probe / Studio tests must round-trip against:

| Fixture | Path |
|---------|------|
| Guided sample | [`testdata/golden/expected/device-sniff-ble-sample/evidence.json`](../testdata/golden/expected/device-sniff-ble-sample/evidence.json) |
| Ambiguous multi-device | [`testdata/golden/expected/device-sniff-ble-ambiguous/evidence.json`](../testdata/golden/expected/device-sniff-ble-ambiguous/evidence.json) |

Companion artifacts in the same directories: `analysis.json`, `device.yaml`, `stdout.txt`. Cases: `testdata/golden/cases/device-sniff-ble-{sample,ambiguous}/`.

**Sample analysis** includes `command_candidates`, `telemetry_candidates`, `command_discovery.status: guided_candidates`. **Ambiguous** sets `requires_operator_selection` / `command_discovery.status: ambiguous_guidance` without command/telemetry candidates.

---

## `publish validate`

**Argv**

```bash
cli-printing-press publish validate --dir ~/printing-press/library/<slug> --json
```

**Exit**

| Condition | Code |
|-----------|------|
| Missing `--dir` | `1` (`ExitInputError`) |
| Validation failed (`passed: false`) | **`5`** (`ExitPublishError`), `Silent: true` in JSON mode |
| All checks passed | `0` |

Human and JSON modes both use exit `5` on failure (contract §4.6).

**Stdout:** `ValidateResult` (`internal/cli/publish.go`):

```json
{
  "passed": false,
  "cli_name": "notion-pp-cli",
  "api_name": "notion",
  "help_output": "...",
  "checks": [
    { "name": "go.mod", "passed": true },
    { "name": "module path", "passed": false, "error": "..." }
  ]
}
```

Human check lines go to **stderr**; JSON mode keeps stdout clean.

---

## `version`

**Argv**

```bash
cli-printing-press version --json
```

**Exit:** `0`.

**Stdout:**

```json
{ "version": "<semver>", "go": "<runtime.Version()>" }
```

Compare against `supported-versions.txt` on `main` for the currency floor (contract §10).

---

## `auth doctor`

**Argv**

```bash
cli-printing-press auth doctor --json
```

**Exit:** Always `0` for completed scans (including `not_set` / `suspicious`). Scan failure → `5`.

**Stdout:** `{ "summary": Summary, "findings": []Finding }` via `authdoctor.RenderJSON` (`internal/authdoctor/`).

`Finding`: `api`, `type`, `env_var`, `status`, `fingerprint`, `reason`. Status enum: `ok`, `suspicious`, `not_set`, `info`, `no_auth`, `unknown`.

---

## Remaining gaps (engine)

Documented; not blocking Studio parse of §4.2 surfaces:

| Gap | Guidance |
|-----|----------|
| Structural `dogfood` FAIL → exit `0` | Gate on `verdict` / `issues`, not exit code |
| `generate --json` multi-shape | Branch on presence of `plan_file` / `protocol` / `spec_files` |
| `scorecard` grade never fails exit | Use `overall_grade` / `gap_report` in UI |
| `auth doctor` never gates | Dashboard-only |
| Probe cmds lack `--json` flag | Always emit JSON — treat as implicit JSON mode |
| Shipcheck umbrella omits per-leg payloads | Re-run legs with `--json` for detail |

No HTTP serve surface in v1 (by contract).
