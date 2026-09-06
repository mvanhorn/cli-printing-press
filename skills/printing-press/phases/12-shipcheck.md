## 12-shipcheck (Phase 4: Shipcheck)

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "12-shipcheck"
```

Run one combined verification block via the `shipcheck` umbrella, which runs all six legs (dogfood, verify, workflow-verify, verify-skill, validate-narrative, scorecard) in canonical order, propagates exit codes, and prints a per-leg verdict summary. The umbrella is the canonical Phase 4 invocation; running the legs individually is supported but not recommended (operators have skipped legs that way and shipped broken CLIs).

Before running shipcheck, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase shipcheck
```

```bash
cli-printing-press shipcheck \
  --dir "$CLI_WORK_DIR" \
  --spec <same-spec> \
  --research-dir "$API_RUN_DIR"
```

The umbrella defaults to `verify --fix` (auto-repair common failures), `validate-narrative --strict --full-examples` (README/SKILL narrative command validation), and `scorecard --live-check` (sample novel-feature output against real targets). When Go sources under `cmd/<cli>/` or `internal/` are newer than `build/stage/bin/<cli>`, `scorecard --live-check` rebuilds the staged binary before sampling and reports the refresh action in human and JSON output. Use `--no-fix` for a read-only pass, `--no-live-check` to skip live sampling, or `--json` for a structured envelope (suppresses per-leg output for clean piping). Pass `--api-key` / `--env-var` through to verify when live testing needs a credential, or `--strict` to make verify-skill treat likely-false-positive findings as failures.

During shipcheck, the verify and scorecard legs persist their summaries back into `$CLI_WORK_DIR/.printing-press.json`: `verify.pass_rate`, `verify.verdict`, `scorecard.steinberger.percentage`, `scorecard.steinberger.grade`, and `novel_features_built` from the run's `research.json`. Do not hand-edit those fields; rerun shipcheck (or the standalone leg with `--write-manifest`) when they are stale. [Phase 0](03-resolve-and-reuse.md)'s sub-60 reprint gate relies on this persisted score on the next run.

If a leg fails, re-run that one leg standalone (e.g., `cli-printing-press verify-skill --dir <CLI_WORK_DIR>`) for focused iteration; once it passes, re-run the full `shipcheck` umbrella to confirm no regression in the others.

Interpretation:
- `dogfood` catches dead flags, dead helpers, invalid paths, example drift, broken data wiring, command tree/config field wiring bugs, stale static MCP surfaces, and novel features that were planned but not built
- `verify` catches runtime breakage and runs the auto-fix loop for common failures
- `workflow-verify` tests the primary workflow end-to-end using the verification manifest (workflow_verify.yaml). Three verdicts: workflow-pass, workflow-fail, unverified-needs-auth
- `verify-skill` checks that every `--flag` and command path in SKILL.md actually exists in the shipped CLI source. Catches bogus examples invented by the absorb LLM (e.g., `search --max-time` when `--max-time` is a `tonight` flag). Exit 1 = findings to fix; exit 0 = SKILL is honest.
- `validate-narrative` checks that every README/SKILL narrative command path, flag, and argument shape in research.json resolves against the built CLI under `PRINTING_PRESS_VERIFY=1`
- `scorecard` is the structural quality snapshot, not the source of truth by itself

Fix order (update heartbeat between each fix category to prevent stale lock during long fix loops):
1. generation blockers or build breaks
2. invalid paths and auth mismatches
3. dead flags / dead functions / ghost tables
4. broken dry-run and runtime command failures
5. missing novel features (see below)
6. scorecard-only polish gaps

When category 4 includes narrative examples, rerun
`cli-printing-press validate-narrative --strict --full-examples` after the fix. The path-only
mode is not enough before publishing because it cannot catch bad flags on an otherwise
valid command.

**Missing novel features fix (step 5):** Dogfood writes `novel_features_built` to research.json — only features whose commands actually exist. The original `novel_features` (aspirational list from absorb) is preserved for the audit trail. Dogfood also syncs the generated `.printing-press.json` `novel_features`, `README.md` `## Unique Features` block, `SKILL.md` `## Unique Capabilities` block, and `internal/cli/root.go` `--help` Highlights block from `novel_features_built`; if none survived, it removes the rendered README/SKILL/root help blocks. Dogfood prints `dogfood: synced ... from novel_features_built` for every rendered artifact it changes. After dogfood:

1. Inspect the dogfood planned-vs-built delta
2. Build missing approved features when they are still in scope
3. Rerun dogfood so research.json, `.printing-press.json`, README.md, SKILL.md, and root `--help` Highlights are all synced from the verified set
4. Audit surrounding README/SKILL/root help prose, recipes, trigger phrases, and examples for indirect references to dropped features
5. Log which features were dropped (planned vs built delta)

After fixing each category, update the heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase shipcheck-fixing
```

<!-- CODEX_PHASE4_START -->
When `CODEX_MODE` is true, read [references/codex-delegation.md](../references/codex-delegation.md)
for the Phase 4 fix delegation pattern.

When `CODEX_MODE` is false, fix bugs directly.
<!-- CODEX_PHASE4_END -->

Ship threshold (the umbrella's verdict is the canonical signal — all of these must hold for `shipcheck` to exit 0):
- `shipcheck` exits 0. The umbrella's per-leg summary table shows every leg PASS. A non-zero exit is a fix-before-ship blocker, period — do not ship if the umbrella is red.
- `verify` verdict is `PASS` or high `WARN` with 0 critical failures
- `dogfood` no longer fails because of spec parsing, binary path, or skipped examples
- `dogfood` wiring checks pass (no unregistered commands, no config field mismatches)
- `workflow-verify` verdict is `workflow-pass` or `unverified-needs-auth` (not `workflow-fail`). Exception: if the spec or traffic analysis marks browser-session/browser-clearance auth as required, `unverified-needs-auth` is a `hold` verdict until `auth login --chrome`, `doctor --json`, and a read-only browser-session proof pass against the real site.
- `verify-skill` exits 0 (no mechanical mismatches between SKILL.md and CLI source). Treat non-zero as a fix-before-ship blocker — the SKILL is what agents read; if it lies about the CLI, the lie ships.
- `scorecard` is at least 65 and **no flagship or approved-in-Phase-1.5 feature returns wrong/empty output**

**Behavioral correctness is part of the ship threshold, not just structural quality.** A Grade A scorecard with a broken flagship feature (e.g., `goat "brownies"` returning a chili recipe) does NOT pass the ship threshold. Run a sample invocation of every novel-feature command before declaring shipcheck complete.

**Per-source row for combo CLIs (synthetic spec, multiple data sources).** For every named source in a combo CLI (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc.) the dogfood test matrix MUST add one row per source: with the source's limiter exhausted (or the upstream genuinely throttling), assert that the user-facing command surfaces a typed `*cliutil.RateLimitError` referencing the source — not empty JSON / `0 results`. A passing row says: "the CLI distinguishes 'no data' from 'we got rate-limited' for this source." The matrix-builder derives rows from the command tree by default; for combo CLIs, also derive rows from the source list. `source_client_check` catches the static signal that throttling is silently swallowed; only the runtime row proves the user-visible behavior.

Maximum 2 shipcheck loops by default.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-shipcheck.md`

Include:
- command outputs and scores
- top blockers found
- fixes applied
- before/after verify pass rate
- before/after scorecard total
- final ship recommendation: `ship` or `hold`

**Verdict rules:**
- `ship`: all ship-threshold conditions met AND no known functional bugs in shipping-scope features.
- `hold`: one or more conditions missing, OR functional bugs exist that cannot be fixed in-session.

`ship-with-gaps` is deprecated as a default verdict. It is NOT valid for bugs that require only 1-3 file edits; those MUST be fixed before ship. It is only acceptable when (a) a bug genuinely requires a refactor, external dependency change, or API access not available in-session, AND (b) the bug is clearly documented with a `## Known Gaps` block in both the shipcheck report and the generated README. If an agent cannot meet both (a) and (b), the verdict is `hold`, not `ship-with-gaps`.

If the final verdict is `hold`, release the lock without promoting to library:
```bash
"$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR` for potential future retry. A hold does not run the sync-param-drop gate, the review phases, or dogfood for a CLI that is not shipping — it jumps straight to [Phase 5.6](20-promote-and-archive.md) to archive manuscripts (archiving still happens on hold), bypassing the intervening review phases. Record that alternate handoff on the receipt instead of the canonical one:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "12-shipcheck" --next "20-promote-and-archive" --note "hold: <one-line reason>"
```

Then proceed directly to [Phase 5.6](20-promote-and-archive.md) — do **not** follow the canonical `Next:` pointer below. The run arrives at [Phase 5.6](20-promote-and-archive.md) without dogfood markers; that phase documents why a hold archives without them.

**Run exactly one of the two completion blocks: the hold block above, or the ship block below.** On a `ship` verdict, record the canonical handoff and point `--evidence` at the shipcheck proof before following `Next:`:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "12-shipcheck" --evidence "$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-shipcheck.md"
```

Next: phases/13-sync-param-drop-gate.md
