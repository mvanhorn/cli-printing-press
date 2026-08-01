# Dogfood Testing Reference

> **Supplementary reference for Phase 5.** The test protocol (steps 1-4, test
> lists, reporting format) is inline in the main skill. This file contains
> additional guidance: common failure patterns and what NOT to test.

## Common Failure Patterns

| Symptom | Likely cause | Fix location |
|---------|-------------|--------------|
| All list commands return empty | Response envelope not unwrapped | Client or output helpers |
| `--select` strips everything | filterFields can't parse envelope | Add extractResponseData call |
| `--csv` shows JSON | CSV check after JSON pipe check | Promoted template output path |
| `--json` or `--agent` prints empty-result prose | Human empty-result branch runs before the machine-mode check | Select `wantsHumanTable` first and route machine output through `printJSONFiltered` |
| `--csv` prints `[]` for no rows | Empty array falls through the single-object JSON fallback | Keep empty arrays as an empty CSV stream in `printCSV` |
| `search` returns no results | FTS table not wired into search cmd | search.go switch statement |
| `sync` gets 404 on some endpoints | API version header mismatch | Client header per-path |
| Mutation command requires ugly name | operationId not cleaned up | Command Use: field |
| `<cmd> --help` shows wrong example | Example field has placeholder values | Command Example: field |
| `me` shows "0 results" | Provenance counter assumes array | Count single objects as 1 |
| Cancel/confirm path not discoverable | Subcommand buried under operationId group | Check `bookings --help` for Available Commands |

## What NOT to Test

- Internal implementation details (store schema, migration order)
- Performance benchmarks
- Concurrent access
- Edge cases that require specific account setup (team features, org hierarchy)
- Endpoints the user doesn't have access to (org-level when user is individual)

## Full Dogfood Confirmation

For list-shaped or novel commands with a reproducible zero-result case, run the
case with `--json`, `--agent`, and `--csv`. JSON and agent stdout must parse
without a leading human message. CSV may be empty when there is no schema or
rows, but must never contain the JSON literal `[]`; non-empty rows must remain
CSV, and human mode may retain the explanatory empty-result prose.

When the user selects "Full dogfood", confirm before creating test data:

> "I'll create test data on your account (test bookings, test records, etc.)
> and clean up by cancelling/deleting them. OK to proceed?"
