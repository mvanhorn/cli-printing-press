# Golden test data

Each subdirectory is one golden test case. `golden.json` is the expected stdout
for the invocation defined in `golden_test.go`.

To add a new case:
1. Add an entry to the `cases` slice in `internal/cli/golden_test.go`.
2. Run `make golden-update` (or `go test ./internal/cli/... -run TestGolden -update-golden`).
3. Inspect the new `golden.json` for correctness.
4. Commit.

To intentionally update a golden after a planned schema change:
1. Run `make golden-update`.
2. Review the diff carefully.
3. Bump the semver appropriately (MAJOR if schema broke, MINOR if field added).
