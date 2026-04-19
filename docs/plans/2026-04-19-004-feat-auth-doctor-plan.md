---
title: "feat(cli): auth doctor - env-var visibility across installed printed CLIs"
type: feat
status: active
date: 2026-04-19
---

# feat(cli): auth doctor - env-var visibility across installed printed CLIs

## Overview

Add `printing-press auth doctor`. One command that scans every installed printed CLI's `tools-manifest.json`, checks whether the declared env vars are set, prints a traffic-light table with fingerprints, and calls out obvious problems (truncated values, missing required tokens). Zero new state on disk, zero regeneration required. Piggybacks on the manifests megamcp already reads.

This supersedes the more ambitious unified auth plan (`docs/plans/2026-04-19-003-feat-unified-auth-manager-plan.md`) which added a credential store, OAuth flow, and template changes. The store was marginal value for most users. The diagnostic is the piece that clearly beats the status quo today.

## Problem Frame

A typical failure today: user runs `hubspot-pp-cli contacts list`, gets a 401, does not know whether `HUBSPOT_ACCESS_TOKEN` is unset, stale, truncated, or the token is simply invalid at the vendor. They have to inspect their shell, test against the vendor, and guess. An agent hits the same wall and cannot self-correct.

A unified diagnostic changes that: one command answers "what is the auth state across every PP CLI I have installed?" The answer is offline, fast, and pipes cleanly to an agent.

No store is needed. No CLI regeneration is needed. Every printed CLI already ships a `tools-manifest.json` that declares its auth type and env var names, and megamcp already knows how to read those manifests.

## Requirements Trace

- R1. `printing-press auth doctor` scans installed printed CLIs and prints per-API auth status.
- R2. Status must distinguish: env var set and well-formed, env var set but suspicious (empty, too short for the declared type), env var unset, API has no auth requirement.
- R3. Fingerprints show the first 4 characters of each set value so users can tell "this is my new token" vs "this is my old token" without the full secret leaking.
- R4. `--json` output for agents (auto-triggered when stdout is piped).
- R5. Works against whatever printed CLIs the user has installed under `~/printing-press/library/<api>/`. Does not require a separate registry or store.

## Scope Boundaries

In scope:
- `printing-press auth doctor` subcommand.
- Scan of `~/printing-press/library/<api>/tools-manifest.json` files.
- Optional `--catalog` flag to additionally include APIs from the embedded `catalog.FS` that are not yet installed, so users see "you could install X but have not."
- Human-friendly table in TTY, auto-JSON when piped.

### Deferred to Separate Tasks

- Credential store (`~/.pp/credentials.json`). See the superseded plan (`docs/plans/2026-04-19-003`). Revisit only if users actually report pain managing many tokens.
- `auth link`, `auth list`, `auth remove`, `auth fix-perms`. Deferred with the store.
- OAuth loopback flows. Separate plan when a library CLI needs OAuth.
- Live API health probes (actually calling the vendor with the token). Could be a `--network` flag later.
- Shell-file origin attribution (parsing `.zshrc` to find where a var was set). Brittle, skip.

## Context & Research

### Relevant code and patterns

- `internal/megamcp/manifest.go` - `ToolsManifest` struct with `Auth.Type`, `Auth.EnvVars`, `Auth.Format`. Doctor reads the same shape.
- `internal/megamcp/auth.go` - `hasAuthConfigured` and `ApplyAuthFormat` show how megamcp already reasons about env-var presence. Reuse the same signals.
- `internal/megamcp/metatools.go` - `library_info` handler already enumerates installed manifests. Doctor is the CLI-side analogue with environment status layered on.
- `internal/catalog/` + `catalog.FS` - embedded catalog of 18 baseline APIs. Source for the `--catalog` mode showing "not installed" entries.
- `internal/cli/` - existing Cobra subcommand wiring. Auth group lives beside `generate`, `verify`, `scorecard`.
- Local library layout: `~/printing-press/library/<api>/tools-manifest.json` per `AGENTS.md` glossary.

### Institutional learnings

- AGENTS.md machine-vs-printed rule: this is a machine change (new subcommand on the `printing-press` binary), no printed CLI changes.
- PP exit-code contract: doctor uses 0 for "ran successfully, findings inside"; 5 only if the scan itself fails (e.g., library dir unreadable).

### External references

- None needed. Pure local scan.

## Key Technical Decisions

KTD-1. Doctor reads `~/printing-press/library/<api>/tools-manifest.json` as the source of truth for which APIs to check and what env vars each one declares. Rationale: same file megamcp reads. Staying on one manifest keeps both surfaces consistent as the library evolves.

KTD-2. Default mode shows installed CLIs only. `--catalog` adds the embedded baseline catalog (18 APIs in `catalog.FS`) so the user sees install suggestions alongside runtime status. Rationale: most users want "what is wrong with what I have"; the discovery use case is secondary and flag-gated.

KTD-3. Fingerprints show the first 4 characters of each set value. Never show the full value, never log it. Rationale: enough signal to distinguish tokens ("pat-abc..." vs "xoxb-..."), no leakage risk.

KTD-4. Suspicious-value detection is heuristic, not schema-validated. Minimum lengths per auth type (api_key >= 8, bearer_token >= 20) flag obviously truncated values. Rationale: false positives are harmless (user confirms the value is correct); false negatives are acceptable because this is a nudge, not a gate.

KTD-5. Output is a table in TTY and auto-JSON when stdout is piped. Rationale: matches PP's existing agent-native behaviour across every other subcommand.

## Open Questions

### Resolved During Planning

- Q: Where does doctor find the list of APIs to check? A: Installed manifests at `~/printing-press/library/<api>/tools-manifest.json`. KTD-1.
- Q: Include not-yet-installed catalog APIs? A: Only under `--catalog`. KTD-2.
- Q: Show full token values? A: No, first 4 chars only. KTD-3.
- Q: Probe the live API? A: No. Offline-only v1. A `--network` flag is future work.

### Deferred to Implementation

- Exact column widths and header style for the TTY table. Decide at first render against a real library.
- Whether to include `auth_type: composed` (cookie-based) APIs in the default scan. Probably yes, but the field to check differs from `env_vars`. Confirm when the implementer sees the actual manifest shape.

## High-Level Technical Design

> This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.

### Output shape (directional)

```
$ printing-press auth doctor

API          Type      Env Var                    Status      Value
-----------  --------  -------------------------  ----------  -----------
dub          api_key   DUB_TOKEN                  ok          dub_...
espn         api_key   ESPN_KEY                   not set     -
hubspot      api_key   HUBSPOT_ACCESS_TOKEN       ok          pat-...
kalshi       api_key   KALSHI_API_KEY             suspicious  abc (too short)
linear       api_key   LINEAR_API_KEY             ok          lin_...
cal-com      api_key   CAL_COM_TOKEN              not set     -
hackernews   none      -                          n/a         -

Summary: 3 ok, 1 suspicious, 2 not set, 1 no auth required
```

`--json` shape (directional):

```json
{
  "summary": {"ok": 3, "suspicious": 1, "not_set": 2, "no_auth": 1},
  "apis": [
    {"api": "dub", "type": "api_key", "env_var": "DUB_TOKEN", "status": "ok", "fingerprint": "dub_"},
    {"api": "kalshi", "type": "api_key", "env_var": "KALSHI_API_KEY", "status": "suspicious", "fingerprint": "abc", "reason": "value shorter than 8 chars"}
  ]
}
```

## Implementation Units

- [ ] Unit 1: Library scan + status classifier

  Goal: Ship the core scan function that reads installed manifests and classifies each API's env-var status.

  Requirements: R1, R2, R3, R5

  Dependencies: None.

  Files:
  - Create: `internal/authdoctor/scan.go` (scan `~/printing-press/library/<api>/tools-manifest.json`, load into the existing `megamcp.ToolsManifest` shape).
  - Create: `internal/authdoctor/classify.go` (given a manifest + current env, return a typed `Finding`).
  - Create: `internal/authdoctor/fingerprint.go` (first-4-chars rendering with non-printable guard).
  - Test: `internal/authdoctor/scan_test.go`, `classify_test.go`, `fingerprint_test.go`.

  Approach:
  - Reuse `internal/megamcp/manifest.go` types for manifest parsing. No duplicate parser.
  - Classifier states: `ok`, `suspicious`, `not_set`, `no_auth`, `unknown` (manifest declares an auth type the classifier does not know).
  - Suspicious thresholds: api_key minimum 8 chars, bearer_token minimum 20 chars. These live in a small table inside `classify.go` and are easy to tune.
  - `composed` auth type is treated as `ok` if any of its declared env vars is set and `not_set` if none are set. Do not over-engineer composed-cookie inspection in v1.
  - Scan gracefully handles a missing `~/printing-press/library/` directory by returning an empty result with no error. That is the "fresh install" case.

  Patterns to follow: `internal/megamcp/metatools.go` for manifest iteration. Keep the scan read-only and idempotent.

  Test scenarios:
  - Happy path: seeded library with three manifests (api_key, bearer_token, no auth) classifies correctly when env is set appropriately.
  - Happy path: env var set to a long well-formed value produces `ok` with the first-4 fingerprint.
  - Edge case: env var set to a short value produces `suspicious` with a reason string.
  - Edge case: env var set but empty string produces `suspicious` with "empty value".
  - Edge case: manifest declares `auth.type: none` produces `no_auth`.
  - Edge case: manifest declares a type the classifier does not know produces `unknown` without failing the whole scan.
  - Edge case: missing `~/printing-press/library/` returns an empty result and no error.
  - Edge case: manifest file with invalid JSON is reported as one `Finding` with status `unknown` and a "manifest parse error" reason; scan continues for other APIs.
  - Edge case: `composed` auth with two env vars - one set, one unset - classifies as `ok` (at least one present).
  - Edge case: fingerprint of a value containing control characters or non-printables renders as `?` for those positions rather than leaking raw bytes.

  Verification: scanning a seeded fixture library covering all classifier states produces the expected set of `Finding` objects.

- [ ] Unit 2: `auth doctor` command + rendering + docs

  Goal: Ship the user-facing Cobra command, table and JSON renderers, and update README.

  Requirements: R1, R4

  Dependencies: Unit 1.

  Files:
  - Create: `internal/cli/auth_doctor_cmd.go` (Cobra wiring for `printing-press auth doctor`).
  - Create: `internal/authdoctor/render.go` (TTY table + JSON renderers).
  - Modify: `cmd/printing-press/main.go` to register the `auth` subcommand group with `doctor` under it.
  - Modify: `README.md` to add a short "Diagnosing auth problems" note linking to `auth doctor`.
  - Modify: `AGENTS.md` glossary to add `internal/authdoctor/`.
  - Test: `internal/cli/auth_doctor_cmd_test.go`, `internal/authdoctor/render_test.go`.

  Approach:
  - Register an `auth` parent command with `doctor` as the only current child. Future auth subcommands (if they ever land) attach to the same parent.
  - Renderer chooses table vs JSON based on `isatty(stdout)` with `--json` as an explicit override.
  - `--catalog` flag adds entries from `catalog.FS` whose slugs are not already in the installed manifest set. Each catalog-only entry is rendered with status `not installed`.
  - Non-zero exit only when the scan itself fails (e.g., library dir exists but is unreadable). Findings with `not_set` or `suspicious` do not change the exit code; the command is diagnostic, not gating.
  - README section is ~10 lines. ONBOARDING.md does not need changes for this.

  Patterns to follow: existing Cobra command style in `internal/cli/`. Auto-JSON-when-piped pattern from printed CLIs.

  Test scenarios:
  - Happy path: TTY output for the seeded fixture library renders a stable column layout.
  - Happy path: piped stdout auto-switches to JSON without a flag.
  - Happy path: `--json` forces JSON even in TTY.
  - Happy path: `--catalog` merges installed + catalog entries; catalog-only entries show `not installed`.
  - Edge case: empty library renders "No printed CLIs installed." summary and exits 0.
  - Edge case: output rendering of a value containing a terminal-escape sequence strips or escapes the sequence (defensive; aligns with KTD-3 safety framing).
  - Error path: `~/printing-press/library/` exists but is unreadable (perm denied) returns exit 5 with an actionable message; scan does not partially render.

  Verification: on a real machine with 2+ installed printed CLIs, `printing-press auth doctor` prints a correct table. `printing-press auth doctor --json | jq` round-trips cleanly.

## System-Wide Impact

- Interaction graph: `internal/authdoctor` is a new leaf package. It reads the manifest type from `internal/megamcp/manifest.go`. Megamcp is not modified.
- Error propagation: exit 0 for "scan ran, here are findings". Exit 5 only for scan failures. Exit 2 for usage errors (unknown flag).
- State lifecycle risks: none. Doctor is read-only.
- API surface parity: no MCP-side meta-tool in this plan. If someone wants `auth_doctor` exposed via megamcp later, it is a trivial wrapper.
- Unchanged invariants: no printed CLI changes. No new files on disk. No changes to existing env-var behaviour. Users who never run `auth doctor` see zero difference.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Fingerprint leaks enough characters to identify a token via side-channel | First 4 chars only; value-type prefixes (`pat-`, `xoxb-`, `Bearer `) are the same across all tokens of that type, so 4 chars do not narrow to a user-specific secret |
| Doctor produces false-positive "suspicious" findings for intentionally short tokens | Thresholds are heuristic and tunable in one file; false positives are nudge-level, not blocking |
| Library layout changes (`~/printing-press/library/<api>/`) in a future refactor | Scan goes through a small helper that resolves the library root; one function to update |
| Manifest schema drift adds fields doctor does not understand | Doctor only reads `Auth.Type` and `Auth.EnvVars`; extra fields are ignored |

## Documentation / Operational Notes

- Changelog scope `cli`.
- No migration. No feature flag. Single binary release.
- README gets a short "Diagnosing auth" section. AGENTS.md glossary adds the new package.

## Sources & References

- Related code: `internal/megamcp/manifest.go`, `internal/megamcp/auth.go`, `internal/megamcp/metatools.go`, `internal/catalog/`, `internal/cli/`.
- Superseded plan: `docs/plans/2026-04-19-003-feat-unified-auth-manager-plan.md` (credential store + OAuth; deferred).
- Repo conventions: `AGENTS.md` (machine-vs-printed rule, glossary, commit style).
