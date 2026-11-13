# Press verifiers on a print

The press ships verifiers that grade a printed CLI before it is published:
- `verify`: runs every command against a spec-derived mock or the real API.
- `verify-skill`: checks that SKILL.md and README match the shipped Cobra source.
- `scorecard`: quality score.
- `shipcheck`: runs all six Phase 4 legs as one sweep.

Users and the `/printing-press` skill rely on their verdicts and exit codes to decide whether a CLI ships.

## Sub-features

- `ver-verify` `verify --dir --spec` builds the print, runs it against a mock, and returns a PASS/WARN/FAIL verdict with per-command results.
- `ver-skill` `verify-skill --dir` reports flag-names, flag-commands, positional-args, shell-var-quotes, unknown-command, and canonical-sections.
- `ver-skill-catch` `verify-skill` fails when SKILL.md references a flag the CLI does not declare.
- `ver-scorecard` `scorecard --dir --spec` produces a score.
- `ver-shipcheck` `shipcheck --dir --spec` runs every leg and returns a per-leg verdict.

## How to get to it (user POV)

- `cli-printing-press verify|verify-skill|scorecard|shipcheck --dir <printed-cli> [--spec <spec>] [--json]`.
- Phase 4 of the `/printing-press` skill runs `shipcheck`.

## Driving it with pp-verify

Preconditions:

- Healthy doctor, and `$RUN/cli` unmodified since launch.

- **Verify.** `$V press "$RUN" verify --dir "$RUN/cli" --spec testdata/golden/fixtures/golden-api.yaml --json`. Exit 0. `verify.verdict` is `PASS`, `verify.mode` is `mock`, and `verify.failed` is 0. The golden print gives `total` 42. This takes about 6s.
- **Verify-skill clean.** `$V press "$RUN" verify-skill --dir "$RUN/cli"`. Exit 0, with stdout `✓ All checks passed (flag-names, flag-commands, positional-args, shell-var-quotes, unknown-command)` and `✓ canonical-sections passed`.
- **Verify-skill catches a lie.** Append the line `printing-press-golden-pp-cli projects list --no-such-flag` inside a bash block in `$RUN/cli/SKILL.md`, then rerun. The exit is non-zero and a `flag-names` finding names `--no-such-flag`. Revert the edit afterwards. To keep the print pristine, use a copy: `cp -R "$RUN/cli" "$RUN/cli-lie"`.
- **Scorecard.** `$V press "$RUN" scorecard --dir "$RUN/cli" --spec testdata/golden/fixtures/golden-api.yaml --json`. Exit 0, with a JSON score summary.
- **Shipcheck.** `$V press "$RUN" shipcheck --dir "$RUN/cli" --spec testdata/golden/fixtures/golden-api.yaml --no-live-check --json`. A per-leg envelope for dogfood, verify, workflow-verify, verify-skill, validate-narrative, and scorecard. Without `--research-dir`, the legs that need research skip their novel-feature checks.

## Gotchas

- `verify` reads its JSON under a top-level `verify` key, not at the root.
- `verify --fix` and the shipcheck default can edit the print. Use `--no-fix` on shipcheck when the print must stay unmodified for later drives.
- `verify` against the real API needs `--api-key` or `--env-var`. This skill never passes them, so verify stays in mock mode.
- Shipcheck's novel-feature legs need a `/printing-press` run's `--research-dir`. On a bare print, a skip there is expected, not a pass.
