---
title: Generator template polish — Usage line, framework Examples, framework JSON envelopes, sync trailing line
type: fix
status: active
date: 2026-05-04
origin: https://github.com/mvanhorn/cli-printing-press/issues/572
---

# Generator template polish — Usage line, framework Examples, framework JSON envelopes, sync trailing line

## Summary

Patch four defects in `internal/generator/templates/` so future printed CLIs ship with correct usage strings, framework-command Examples, framework-command JSON envelopes, and clean `sync --json` output. Reference shapes are already proven in `~/printing-press/library/movie-goat/` from this run.

---

## Problem Frame

The movie-goat run surfaced four template defects (filed as findings F1, F2, F3, F6 in retro #571, absorbed into WU-1 / sub-issue #572). All four have already been patched directly in the printed CLI, which proved the target shapes work; they recur on every future generation because the templates haven't been touched. The durable fix is mechanical: copy the patched shapes back into the templates so the next CLI lands without these defects.

---

## Requirements

- R1. Generated CLIs emit a single binary name in the usage error for any required-positional command (no `<cli> <cli> <subcmd>` doubling).
- R2. Generated framework commands (`doctor`, `profile save/use/list/show/delete`, `feedback list`) emit an `Examples:` block in `--help`.
- R3. Generated framework commands that print human prose by default (`auth status/logout/set-token`, `api`, `import`, `profile delete`, `tail` no-arg, `which`, `multi` no-arg) emit valid JSON when `--json` is set.
- R4. Generated `sync` command produces output parseable as line-delimited JSON when `--json` is set, with `{"event":"sync_summary",...}` as the last non-empty line. No trailing human prose.
- R5. Existing non-JSON behavior outside the four fixes (F1, F2, F3, F6) is unchanged across all affected commands. The fixes themselves (usage-line binary name, Examples blocks, JSON envelopes, sync trailing-line gate) are intentional changes and do not count as regressions under R5.
- R6. The existing `testdata/golden/cases/generate-golden-api/` fixture is extended with new rendered-Go-source artifacts to lock these four contracts as regression guards. No new fixture case is added; byte-comparison of generated `internal/cli/*.go` files is the enforcement mechanism (see U5).

---

## Scope Boundaries

- Generator templates only. No changes to printed CLIs already in `~/printing-press/library/`; backports are explicitly excluded — those are printed-CLI fixes outside this WU's scope.
- Live dogfood matrix accuracy fixes (F4, F5 from the parent retro) are out of scope; they live in WU-2 / sub-issue #573 with its own plan.
- F7 (inline `#` shell-comment in Cobra Examples) is out of scope; skipped at retro triage as an authoring-guidance issue rather than a template change.
- No refactor of the `flags.asJSON` pattern or `printJSONFiltered` helper. The existing helpers and patterns are extended in place; no new helpers are introduced.

### Deferred to Follow-Up Work

- Backporting the new JSON envelope shapes into already-published library CLIs — separate concern, follow-up PRs against `printing-press-library` per CLI when (or if) MCP hosts depend on the new shapes.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/` — all 11 template files in scope:
  - F1: `command_promoted.go.tmpl:95`, `command_endpoint.go.tmpl:128`
  - F2: `doctor.go.tmpl`, `profile.go.tmpl`, `feedback.go.tmpl`
  - F3: `auth.go.tmpl`, `api_discovery.go.tmpl`, `import.go.tmpl`, `profile.go.tmpl` (delete subcommand), `tail.go.tmpl`, `which.go.tmpl`, `command_promoted.go.tmpl`
  - F6: `sync.go.tmpl` (and `graphql_sync.go.tmpl` if it has the same trailing-line issue)
- `printJSONFiltered` helper — already used in `analytics.go.tmpl`, `feedback.go.tmpl`, `profile.go.tmpl`, `which.go.tmpl`, `share_commands.go.tmpl`, etc. The canonical shape is `return printJSONFiltered(cmd.OutOrStdout(), value, flags)` inside an `if flags.asJSON { ... }` branch. Extends without modification.
- Binary-name placeholder convention — templates already use `{{ .Name }}-pp-cli` for in-text references (e.g., `doctor.go.tmpl` has `"{{ .Name }}-pp-cli auth login --chrome"`). New `Example:` strings follow the same pattern.
- Reference shapes proven in this run — every JSON envelope and Examples block from movie-goat's printed CLI:
  - `internal/cli/auth.go` (status / logout / set-token JSON envelopes)
  - `internal/cli/api_discovery.go` (`{interfaces, note}`)
  - `internal/cli/import.go` (`{succeeded, failed, skipped}`)
  - `internal/cli/profile.go` (delete envelope + Examples on save/use/list/show/delete)
  - `internal/cli/tail.go` (no-arg JSON help envelope)
  - `internal/cli/which.go` (matches envelope)
  - `internal/cli/sync.go` (suppress trailing human line under --json)
  - `internal/cli/promoted_multi.go` (no-query JSON envelope)
  - `internal/cli/doctor.go`, `feedback.go` (Examples)

### Institutional Learnings

- `docs/solutions/best-practices/adaptive-rate-limiting-sniffed-apis.md` — adjacent (rate limiting, not template polish); not directly applicable but confirms the pattern of capturing learnings from per-CLI patches that became generator fixes.
- AGENTS.md golden harness section — "Golden cases must be deterministic, offline, and auth-free." Extending the existing `golden-api` fixture is the right shape; new fixtures only when current ones can't exercise the contract.

### External References

- None required. The WU is mechanical template polish with all shapes already proven in a printed CLI.

---

## Key Technical Decisions

- **Use `printJSONFiltered`, not `json.NewEncoder`, for new JSON envelope branches.** AGENTS.md Phase 3 rule #2 mandates `printJSONFiltered` for hand-written novel commands so `--select`/`--compact`/`--csv` work; the same rule applies to generator-emitted framework commands. `which.go.tmpl` currently uses bare `json.NewEncoder` — bring it into line with the rest. Rationale: consistent flag behavior across every command in every printed CLI.
- **Treat new JSON envelope shapes as best-effort-stable contracts, regression-guarded by U5's golden snapshots.** Once shipped, MCP hosts and agents will depend on the keys: `{authenticated, source, config}`, `{cleared, note}`, `{saved, config_path}`, `{interfaces, note}`, `{succeeded, failed, skipped}`, `{deleted}`, `{resources, note}`, `{matches}`, `{error, usage}`, and `sync`'s `sync_summary` event. Future changes additive only (add fields, never rename or remove). Two enforcement layers: (1) document the canonical shapes inline in template comments so the contract is visible at the emission site, (2) U5's byte-comparison of rendered Go source (`expected/<case>/library/.../internal/cli/<file>.go`) will fail CI if any key is renamed or removed, since the literal map keys appear in the snapshot.
- **Commit structure: one PR, two logical commits (default).** Commit 1: F1 + F6 + F2 (mechanical fixes — usage line, sync trailing-line gate, Examples additions). Commit 2: F3 (per-template JSON envelope shapes, including the auth-flavor expansion across `auth_simple.go.tmpl` / `auth_client_credentials.go.tmpl` / `auth.go.tmpl`). The split reduces per-commit diff size without fragmenting the delivery. Exception: if review feedback suggests splitting into two sequential PRs, that's acceptable and commit 2 can be hoisted to a follow-up. Default remains one PR.
- **Extend `testdata/golden/cases/generate-golden-api/` rather than add a new case.** The existing fixture already exercises the full template render path. Adding probe lines to `command.txt` and updating `expected/stdout.txt` is cheaper than scaffolding a new case and gives the same regression coverage. New case only if the contracts genuinely don't fit the existing one (they do).
- **Golden update cadence: run `scripts/golden.sh update` once after all four fixes land.** Inspect the diff manually, confirm only the expected output strings changed, document in the PR description what shifted. Avoids per-commit golden churn.
- **graphql_sync.go.tmpl gets the same F6 fix.** If the trailing human line exists there too (likely — it parallels sync.go.tmpl), apply the same `if !flags.asJSON` gate. If it doesn't, no change.

---

## Open Questions

### Resolved During Planning

- **Should F3's JSON shapes match the printed CLI shapes I already shipped?** Yes — those shapes were vetted by the live dogfood matrix and shipped to the public library in PR #229. Re-using them ensures any agents that already adapted to movie-goat's outputs see the same shape from future CLIs. If a different shape is preferred for a specific command, that's a separate decision but defers to the shipped form by default.
- **Should `which.go.tmpl`'s shift from `json.NewEncoder` to `printJSONFiltered` be in scope?** Yes. It's a one-line swap that aligns with the rest of the codebase and gets `--select`/`--compact`/`--csv` for free. The current `json.NewEncoder` was technically a pre-existing tech-debt issue but addressing it inside this WU is right-sized.
- **Should the doctor template emit a JSON envelope for --json?** It already produces structured output via `report := map[string]any{}` + `json.Marshal`. F3 doesn't include doctor; only Examples (F2) is needed for doctor. Confirmed by re-reading the printed CLI's `doctor.go` — JSON works there already.

### Deferred to Implementation

- _(Resolved during ce-doc-review: `graphql_sync.go.tmpl` has the same trailing-human-line pattern at lines 184-194. U2 now scopes both files explicitly with the `humanFriendly` gate.)_
- Exact wording of the inline template comments documenting the JSON envelope contracts. The comments serve as both maintainer cues and PR review anchor; finalize during U4.
- _(Resolved during ce-doc-review: neither stdout assertions nor live CLI probes — the approach is byte-comparison of rendered Go source files added to `artifacts.txt`. See U5.)_

---

## Implementation Units

- U1. **Fix usage-line double-print (F1)**

**Goal:** Generated CLIs emit a single binary name in the usage error for any required-positional command.

**Requirements:** R1, R5

**Dependencies:** None (U1–U4 are independent; U5 depends on all four).

**Files:**
- Modify: `internal/generator/templates/command_promoted.go.tmpl`
- Modify: `internal/generator/templates/command_endpoint.go.tmpl`
- Test: `internal/generator/template_usage_error_test.go` (create)

**Approach:**
- Replace the format string and arg list at line 95 of `command_promoted.go.tmpl` and line 128 of `command_endpoint.go.tmpl`. Drop `cmd.Root().Name(),` and the leading `%s ` so the format reads `"%s is required\nUsage: %s <%s>"` with arg list `cmd.CommandPath(), "{{.Name}}"`.
- `cmd.CommandPath()` already starts with the root command's name; concatenating with `cmd.Root().Name()` duplicates it.

**Patterns to follow:**
- Other templates that already use `cmd.CommandPath()` correctly without prefixing `cmd.Root().Name()` (search the templates dir if a reference is needed).

**Test scenarios:**
- Happy path: render `command_promoted.go.tmpl` with a representative endpoint having a required positional. Assert the rendered Go source contains `cmd.CommandPath()` exactly once and does not contain `cmd.Root().Name(), cmd.CommandPath()` or `%s %s` together. Covers R1.
- Happy path: render `command_endpoint.go.tmpl` similarly. Same assertion. Covers R1.
- Negative: render an endpoint without required positionals. Assert the rendered Go source contains no usage-error block at all (or the existing block is unchanged structurally — whichever the template's current shape produces).

**Verification:**
- `go test ./internal/generator/...` passes.
- After landing, regenerate the golden-api fixture: `scripts/golden.sh update` produces a diff in usage-error strings only (binary name appears exactly once in each affected expected line).

---

- U2. **Suppress sync trailing human line under `--json` (F6)**

**Goal:** Generated `sync` command produces parseable JSON when `--json` is set.

**Requirements:** R4, R5

**Dependencies:** None (U1–U4 are independent; U5 depends on all four).

**Files:**
- Modify: `internal/generator/templates/sync.go.tmpl` (lines 249-255 — the `Sync complete:` trailing block)
- Modify: `internal/generator/templates/graphql_sync.go.tmpl` (lines 184-194 — confirmed identical pattern)
- Test: `internal/generator/sync_json_output_test.go` (create)

**Approach:**
- Locate the trailing `fmt.Fprintf(os.Stderr, "Sync complete: ...")` block at the end of the sync RunE. Wrap it in `if !humanFriendly { ... }` matching the surrounding template idiom — `humanFriendly` is the package-level bool declared in `helpers.go.tmpl:42` and bound to the `--human-friendly` flag in `root.go.tmpl:153`. Every existing output gate in `sync.go.tmpl` (including the `sync_summary` event gate at lines 245-248) uses `humanFriendly`, not `flags.asJSON`.
- Note: the trailing block writes to `os.Stderr`, not `out`; preserve that.
- The `sync_summary` event already emitted into the JSON stream carries `total_records`, `resources`, `success`, `warned`, `errored`, `duration_ms` — same data as the human line. No information is lost.
- Apply the same `if !humanFriendly { ... }` gate to `graphql_sync.go.tmpl:184-194`. The pattern is confirmed identical.

**Patterns to follow:**
- Existing `humanFriendly` gates throughout `sync.go.tmpl` (30+ sites; the `sync_summary` event gate at lines 245-248 is the closest analogue).
- `analytics.go.tmpl` and `feedback.go.tmpl` use `flags.asJSON` directly because they're in different stream contexts; the sync template's idiom is `humanFriendly`.

**Test scenarios:**
- Happy path: render `sync.go.tmpl` with the canonical golden-api fixture. Assert the rendered Go source contains `if !humanFriendly {` wrapping the trailing `fmt.Fprintf(os.Stderr, ...)` for the human "Sync complete" line. Covers R4.
- Happy path: same assertion for `graphql_sync.go.tmpl` (lines 184-194 equivalent). Covers R4.
- Integration (via golden harness in U5 — see U5's source-grep approach): generated sync's rendered Go source contains the `humanFriendly` gate wrapping the trailing block. Covers R4 end-to-end.
- Negative: rendered sync command run without `--json` (and with default `humanFriendly=true`) still prints "Sync complete: ..." to stderr as today. Covers R5.

**Verification:**
- `go test ./internal/generator/...` passes.
- `scripts/golden.sh verify` shows expected diff only on sync-related output lines.

---

- U3. **Add `Example:` blocks to framework command templates (F2)**

**Goal:** Generated framework commands (`doctor`, `profile save/use/list/show/delete`, `feedback list`) carry `Examples:` blocks in `--help`.

**Requirements:** R2, R5

**Dependencies:** None (U1–U4 are independent; U5 depends on all four).

**Files:**
- Modify: `internal/generator/templates/doctor.go.tmpl`
- Modify: `internal/generator/templates/profile.go.tmpl`
- Modify: `internal/generator/templates/feedback.go.tmpl`
- Test: `internal/generator/framework_examples_test.go` (create)

**Approach:**
- Add an `Example:` field to each cobra command literal in the named templates, using the `{{ .Name }}-pp-cli` placeholder convention already established in those templates.
- Reference shapes (proven in this run's printed CLI):
  - doctor: `{{ .Name }}-pp-cli doctor` / `... doctor --json` / `... doctor --fail-on warn`
  - profile save: `... profile save my-defaults --json --compact` / `... profile save tonight-defaults --region US`
  - profile use: `... profile use my-defaults` / `... profile use tonight-defaults --json`
  - profile list: `... profile list` / `... profile list --json`
  - profile show: `... profile show my-defaults` / `... profile show tonight-defaults --json`
  - profile delete: `... profile delete my-defaults --yes` / `... profile delete old-profile --yes --json`
  - feedback list: `... feedback list` / `... feedback list --limit 5` / `... feedback list --json`
- Keep examples generic enough that they apply to any printed CLI's profile/feedback semantics, not movie-goat-specific. `tonight-defaults` is generic enough as a profile name; if reviewers prefer `my-profile`/`other-profile`, swap during code review.

**Patterns to follow:**
- Existing `Example:` fields in `analytics.go.tmpl`, `auth.go.tmpl` (status), and `which.go.tmpl`.

**Test scenarios:**
- Happy path: render each affected template. Assert the rendered Go source contains `Example:` for each cobra command literal. Covers R2.
- Integration (via golden harness in U5): `<generated-cli> doctor --help`, `<generated-cli> profile save --help`, `<generated-cli> feedback list --help` (and the other profile subcommands) all contain `Examples:` in stdout. Covers R2 end-to-end.
- Negative: existing Examples on commands that already had them (e.g., `auth status`) are unchanged. Covers R5.

**Verification:**
- `go test ./internal/generator/...` passes.
- Live dogfood help-check failures for these 7 commands resolve when running against a freshly-generated CLI.

---

- U4. **Add JSON envelopes to framework command templates (F3)**

**Goal:** Generated framework commands respect `--json` by emitting a documented JSON envelope.

**Requirements:** R3, R5

**Dependencies:** None (U1–U4 are independent; U5 depends on all four).

**Files:**
- Modify: `internal/generator/templates/auth_simple.go.tmpl` (status at line 37, logout at line 100, set-token at line 69) — token-auth flavor; this is what movie-goat used
- Modify: `internal/generator/templates/auth_client_credentials.go.tmpl` (status at line 166, logout at line 225, set-token at line 204) — client-credentials flavor
- Modify: `internal/generator/templates/auth.go.tmpl` (status / logout only — this is the OAuth-flavored path; no set-token here)
- Modify: `internal/generator/templates/api_discovery.go.tmpl`
- Modify: `internal/generator/templates/import.go.tmpl`
- Modify: `internal/generator/templates/profile.go.tmpl` (delete subcommand)
- Modify: `internal/generator/templates/tail.go.tmpl` (no-arg path)
- Modify: `internal/generator/templates/which.go.tmpl` (swap `json.NewEncoder` for `printJSONFiltered`, wrap matches in `{matches: ...}` envelope)
- Modify: `internal/generator/templates/command_promoted.go.tmpl` (no-query path emits JSON envelope under `--json`)
- Test: `internal/generator/framework_json_envelopes_test.go` (create)

The auth flavor is selected by spec's `auth.type` (oauth vs api_key vs client_credentials → different template renders). The same JSON envelope shapes (`{authenticated, source, config}`, `{cleared, note}`, `{saved, config_path}`) land in every flavor so the agent-facing contract is consistent regardless of which is rendered. Total emission sites for auth: status × 3, logout × 3, set-token × 2 (the OAuth flavor doesn't have a set-token subcommand).

**Approach:**
- For each affected template, add an `if flags.asJSON { return printJSONFiltered(cmd.OutOrStdout(), envelope, flags) }` branch BEFORE the existing human prose path. Existing behavior is preserved when `--json` is absent.
- Canonical envelope shapes (matching the proven shapes from this run's printed CLI; document each in an inline template comment at the emission site):
  - `auth status`: `{authenticated: bool, source: cfg.AuthSource, config: cfg.Path}`. Preserve existing exit-non-zero-on-not-authenticated semantics under `--json` — write JSON, then return `authErr`.
  - `auth logout`: `{cleared: true, note: "<env_var> env var is still set"}` when env still set; `{cleared: true}` otherwise. The env var name is template-substituted via existing auth env-vars list.
  - `auth set-token`: `{saved: true, config_path: cfg.Path}`.
  - `api` (root path): `{interfaces: [...], note: "<message>"}` (interfaces empty if none, with a note). **Implementation note:** the current template (lines 54-59) builds interfaces as pre-formatted strings via `fmt.Sprintf("  %-45s %s", ...)`. Restructure to assemble a typed `[]struct{Name, Short string}` once, then use it for both the human format loop AND the JSON marshal — not a one-line branch addition. Same restructuring for the methods sub-path.
  - `api <interface>`: `{interface: name, methods: [...]}`.
  - `import`: `{succeeded: N, failed: M, skipped: K}`.
  - `profile delete`: `{deleted: name}`.
  - `tail` (no resource arg + `--json`): `{resources: [...], note: "tail requires a resource name; pass one of the listed names"}`, return nil (exit 0). Existing happy path with a resource arg continues to stream NDJSON. **Implementation note:** verify whether the resource list is already a typed `[]string` or requires the same restructuring as `api_discovery` — if the human path currently builds resource names via formatted strings, assemble a typed slice once for both paths.
  - `which`: wrap matches as `{matches: [...]}`. Empty match under `--json` returns `{matches: []}` exit 0; non-JSON path keeps existing exit-2 on no-match human behavior.
  - `command_promoted` (no query + `--json`): write `{error: "<thing> is required", usage: "<usage string>"}` to stdout, then return `usageErr(...)` so exit code stays 2 — same as the non-JSON path. The envelope is informational about the error; the exit code carries the actual status. Matches the `auth status` not-authenticated pattern (write JSON, return authErr).
- For `which.go.tmpl`, replace the bare `json.NewEncoder` block with `printJSONFiltered` so `--select`/`--compact`/`--csv` work consistently.
- Inline template comment at each emission site documenting the canonical shape, e.g., `// JSON envelope: {authenticated, source, config} — see WU-1 / issue #572`.

**Patterns to follow:**
- `feedback.go.tmpl`'s existing JSON branch (`if flags.asJSON { return printJSONFiltered(... , map[string]any{...}, flags) }`) — exact shape to clone for the envelope-only commands.
- `analytics.go.tmpl`'s `if flags.asJSON { ... } else { ... }` gate — exact shape for commands that have human prose to preserve under `else`.
- For commands that need to return non-zero after writing JSON (auth status not-authenticated): write JSON first, then return the typed error — same as the existing patched shape in movie-goat's `auth.go`.

**Test scenarios:**
- Happy path: render each affected template. Assert each cobra command literal in scope contains an `if flags.asJSON {` branch and references `printJSONFiltered`. Covers R3.
- Edge case: `auth status` not-authenticated under `--json`: assert the rendered source writes the JSON envelope before returning `authErr`. Covers R3 + the not-authenticated semantic.
- Edge case: `which` with no match under `--json`: assert the rendered source emits `{matches: []}` and returns nil (exit 0); under non-JSON the existing exit-2 path is preserved. Covers R3 + R5.
- Edge case: `which --json --compact` against the generated CLI: assert each match in the `matches` array preserves `entry`/`score` keys (i.e., the envelope wrap routes through `compactObjectFields`'s blocklist, not `compactListFields`'s allowlist). Covers the printJSONFiltered swap correctness.
- Edge case: `which --csv` against the generated CLI: assert the output is CSV-shaped (the bare `json.NewEncoder` baseline ignored `--csv`; the new path honors it).
- Integration (via U5 golden harness): rendered Go source for each affected file in `internal/cli/` contains the expected `if flags.asJSON {` block per the byte-comparison snapshot. Covers R3 end-to-end.
- Negative: each named command without `--json` produces the same human prose as today. Covers R5.

**Verification:**
- `go test ./internal/generator/...` passes.
- Live dogfood `json_fidelity` failures for the 9 framework commands listed in F3 resolve when running against a freshly-generated CLI.

---

- U5. **Extend `testdata/golden/cases/generate-golden-api/` to lock the four contracts (R6)**

**Goal:** Golden harness regression-guards the four fixes so future template churn can't silently regress them.

**Requirements:** R6

**Dependencies:** U1, U2, U3, U4 (all four fixes must be in place before the golden update accurately captures the new expected output).

**Files:**
- Modify: `testdata/golden/cases/generate-golden-api/artifacts.txt` (add the new emission-bearing rendered files for byte-comparison)
- Add: `testdata/golden/expected/generate-golden-api/library/golden-api-pp-cli/internal/cli/{auth_simple,api_discovery,import,profile,tail,which,promoted_multi,sync,doctor,feedback}.go` (snapshots of the rendered Go source, captured by `golden.sh update` once U1-U4 land)

**Approach:**
- The golden harness today runs `printing-press generate/dogfood/scorecard` and compares stdout/stderr/exit + listed artifacts. It does NOT chdir into the generated CLI, build it, or invoke its commands. Pivoting U5 to source-grep / artifact-byte assertions stays inside that envelope: the contracts being locked are template-emission contracts, and rendered Go source is the right artifact to snapshot.
- Add the rendered Go source files listed above to `artifacts.txt`. The byte-comparison mechanism already in place will then catch:
  - F1: missing `cmd.Root().Name(),` regression in `usageErr` format strings (single binary name)
  - F2: presence of `Example:` blocks in framework commands (`doctor.go`, `profile.go`, `feedback.go`)
  - F3: presence of `if flags.asJSON {` branches referencing `printJSONFiltered` in framework commands (`auth_simple.go`, `api_discovery.go`, `import.go`, `profile.go` delete, `tail.go`, `which.go`, `promoted_multi.go`)
  - F6: `if !humanFriendly {` gate wrapping the trailing `Sync complete:` block in `sync.go` (and same for `graphql_sync.go` if a graphql-flavored fixture is added later)
- Run `scripts/golden.sh update` once to capture the new expected snapshots. Inspect the diff manually before committing — confirm only the four contract changes appear and no unrelated incidental shifts (template churn elsewhere) bleed in.
- Document the new artifacts in the PR description so reviewers can map each captured file back to a finding.

**Patterns to follow:**
- AGENTS.md "Golden Output Harness" section — keep cases deterministic, offline, auth-free. The source-grep / byte-comparison approach honors all three (no live CLI execution, no auth, deterministic generator output).
- Existing case structure: `artifacts.txt` lists files for byte-comparison against `expected/<case>/<path>`; the harness diffs them at verify time.
- AGENTS.md guidance: "Snapshot the specific files or output fields that demonstrate the stable behavior. Do not include broad reports, whole generated trees, or incidental diagnostics."

**Test scenarios:**
- Integration: `scripts/golden.sh verify` passes after the four template fixes land and the golden-api expected snapshots are regenerated. Covers R6.
- Integration: with U1's fix in place, intentionally revert one of the two template lines and re-run `scripts/golden.sh verify`. The verifier should fail with a diff showing the doubled `cmd.Root().Name(), cmd.CommandPath()` reverting in the captured `promoted_multi.go`. Confirms the regression guard catches the F1 regression.
- Integration: same revert test for F2 (delete an `Example:` block from a framework template), F3 (remove an `if flags.asJSON` branch), F6 (remove the `if !humanFriendly` gate). All should produce verifiable diffs in `expected/<file>.go`.

**Verification:**
- `scripts/golden.sh verify` passes.
- Diff between old and new `expected/stdout.txt` contains only the new probe outputs and the four expected behavior changes; no unrelated rendered-file shifts.

---

## System-Wide Impact

- **Interaction graph:** The four template fixes affect any newly-generated printed CLI's framework command surface — future generations plus any CLI explicitly regenerated. Already-published CLIs in the `printing-press-library` repo are unaffected until backported via separate follow-up PRs (explicitly deferred — see Scope Boundaries). No runtime effect on existing published CLIs.
- **Error propagation:** F3's `auth status` change preserves the existing typed `authErr` return when not authenticated, ensuring callers who depend on exit codes still see the right exit even when JSON is requested. No new error paths introduced.
- **State lifecycle risks:** None. All four fixes are pure-output changes in commands that don't mutate persisted state.
- **API surface parity:** The new JSON envelope shapes for `auth status`, `auth logout`, `auth set-token`, `api`, `import`, `profile delete`, `tail` (no-arg), `which`, `command_promoted` (no-query), and `sync_summary` become Tier-1 stable contracts. Future changes additive only (add fields, never rename or remove existing keys). MCP hosts and agents may depend on these shapes.
- **Integration coverage:** The golden harness extension in U5 covers cross-template integration — exercises the full generator render pipeline against a representative spec and verifies the output contracts. Unit tests in U1-U4 cover per-template emission, but the full integration is the golden harness.
- **Unchanged invariants:** The existing `printJSONFiltered` helper, `flags.asJSON` field, `cmd.CommandPath()` semantics, all existing template `Example:` blocks on commands that already had them, and all existing non-JSON output paths are explicitly unchanged.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| New JSON envelope shapes lock in contracts that future maintainers may want to change. | Two-layer mitigation: (1) inline template comments document each shape at the emission site; (2) U5's `expected/<file>.go` byte-snapshots include the literal map keys, so any rename/removal fails CI's golden verify. PR description states the contracts explicitly. Future changes follow additive-only rule (add fields, never rename or remove). |
| Golden harness diff in U5 is noisier than expected (incidental output shifts from another change merging concurrently). | Run `scripts/golden.sh verify` once before starting U5 to confirm baseline is clean. If unexpected diffs appear, isolate to this WU's changes before committing the golden update. |
| `graphql_sync.go.tmpl` may have a different trailing-line shape than `sync.go.tmpl`, requiring a non-mechanical fix. | U2's approach allows skip-if-not-applicable. If the GraphQL sync template's trailing path differs significantly, file a follow-up sub-issue under #571 rather than expanding this WU. |
| `which.go.tmpl`'s shift from `json.NewEncoder` to `printJSONFiltered` is NOT a 1:1 output-shape swap. | `printJSONFiltered` routes through `printOutputWithFlags` (`helpers.go.tmpl:592`), which applies `--compact` allowlisting via `compactListFields` (line 662) — for a bare array of `whichMatch{Entry, Score}`, `--compact` would strip every entry. The plan's `{matches: [...]}` wrap routes through `compactObjectFields` (blocklist), which preserves the data. The wrap is what makes the swap safe, not output-format equivalence. Add a U4 test asserting `which --json` produces the wrapped envelope shape, plus separate tests for `--compact` and `--csv` (which the bare `json.NewEncoder` did not honor). |
| `printJSONFiltered`'s `--select`/`--compact`/`--csv` behaviors apply to framework commands that previously didn't support them; users may see surprising filter behavior. | Acceptable — global flags should affect all commands consistently. Document in PR description as a behavior consistency improvement. The U4 test scenarios now exercise `--compact` and `--csv` for `which` so the new behavior is captured rather than discovered. |

---

## Documentation / Operational Notes

- PR description lists each finding (F1, F2, F3, F6) and the exact JSON envelope shapes introduced. This is the canonical reference for downstream MCP host implementations.
- AGENTS.md "Code & Comment Hygiene" rule allows inline template comments documenting WHY (a hidden constraint or stable contract); add comments at each new JSON envelope emission site naming the contract.
- After landing, the next regeneration of any published library CLI (via `/printing-press-reprint`) will pick up these fixes automatically. No coordination needed.

---

## Sources & References

- **Sub-issue:** [#572 — WU-1: Generator template polish](https://github.com/mvanhorn/cli-printing-press/issues/572)
- **Parent retro:** [#571 — Movie Goat retro](https://github.com/mvanhorn/cli-printing-press/issues/571)
- **Reference patches:** `~/printing-press/library/movie-goat/internal/cli/{auth,api_discovery,import,profile,tail,which,sync,promoted_multi,doctor,feedback}.go`
- **Golden harness:** `scripts/golden.sh`, `testdata/golden/cases/generate-golden-api/`
- **Existing JSON envelope pattern:** `internal/generator/templates/feedback.go.tmpl`, `internal/generator/templates/analytics.go.tmpl`
- **AGENTS.md sections:** "Golden Output Harness", "Code & Comment Hygiene", "Phase 3 rule #2 (printJSONFiltered)"
