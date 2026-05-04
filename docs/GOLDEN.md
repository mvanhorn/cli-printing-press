# Golden Output Harness

The golden harness is a byte-level behavior check for deterministic, offline `printing-press` commands and generated artifacts. It complements unit tests by catching user-visible output drift and printed CLI artifact drift.

Use golden tests as refactor confidence rails for the Printing Press. When changing internals, templates, pipeline plumbing, or broad architecture, a passing golden suite tells you the externally observable contracts captured by the fixtures did not move. That is the main purpose: preserve stable command output and generated artifact contracts through major machine changes, not exhaustively test every branch.

If a refactor changes machine code but claims behavior is identical, `scripts/golden.sh verify` should pass without fixture updates.

Golden cases must be deterministic, offline, and auth-free. Do not add cases that depend on network access, user credentials or env vars, `~/printing-press`, wall-clock timestamps unless normalized, machine-specific absolute paths unless normalized, or large generated printed CLI trees unless the compared subset is intentional.

Passing `scripts/golden.sh verify` only proves existing fixtures did not drift. It does not prove golden coverage is complete. When adding a new deterministic CLI behavior or artifact contract, explicitly decide whether the golden suite needs a new or expanded case. Add golden coverage when the behavior is user-visible command output or persisted generated artifacts that should remain stable across refactors. Prefer unit tests for narrow helper logic, branchy internals, or cases where a golden snapshot would duplicate a focused package test without proving a CLI-level contract.

## Decision rubric

- **No golden update:** code changed but the captured external behavior is intentionally identical. Run `scripts/golden.sh verify`; it should pass unchanged.
- **Update an existing fixture:** the behavior already covered by a golden case intentionally changed. Run `scripts/golden.sh update`, then inspect and explain the exact expected diff.
- **Add or expand a fixture:** the change creates a new deterministic command output or persisted artifact contract that existing cases do not exercise. Add the smallest fixture that proves that contract.

## Fixture authoring

To add a case, create `testdata/golden/cases/<case-name>/`, add expected outputs under `testdata/golden/expected/<case-name>/`, and list behaviorally important generated files in `artifacts.txt` when the command creates artifacts. Prefer a small, high-signal artifact subset over snapshotting huge trees.

Keep golden artifacts contract-shaped. Snapshot the specific files or output fields that demonstrate the stable behavior. Do not include broad reports, whole generated trees, or incidental diagnostics just because the harness can capture them; unrelated fields make refactors noisy and weaken the signal.

Maintain `testdata/golden/fixtures/golden-api.yaml` as the purpose-built generated-CLI fixture for the Printing Press. When the machine gains deterministic generation capabilities that should survive major refactors, extend this fixture and add the smallest useful artifact comparison that proves the capability. Do not mutate this fixture for one printed CLI's edge case unless it represents a general machine behavior.

## Failure handling

If `verify` fails, inspect `.gotmp/golden/actual/<case-name>/` and the generated `.diff` files. Decide whether the change is a regression or an intentional behavior change. If it is a regression, fix code. If it is intentional, run `scripts/golden.sh update`, review fixture diffs, and mention the golden update in the final summary.

Golden verification does not replace `go test ./...`, `go vet ./...`, `golangci-lint run ./...`, or `go build -o ./printing-press ./cmd/printing-press`. It is an additional check for behavior-sensitive changes and runs in CI as a separate `Golden` workflow, not as part of `go test ./...`.
