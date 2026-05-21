# Steinberger Scorecard - Exact Scoring Patterns

This table maps each scorecard dimension to the exact file and string patterns measured by `internal/pipeline/scorecard.go`. Use this to target Phase 4 fixes at patterns that actually score points.

## Scoring Algorithm

Each dimension is 0-10. Total is 0-90. Grade: A >= 80%, B >= 65%, C >= 50%, D >= 35%, F < 35%.

Run the scorecard: `cli-printing-press scorecard --dir ./<api>-cli`

## Dimension Reference

| Dimension | File Checked | Patterns (points each) | Max |
|-----------|-------------|----------------------|-----|
| Output Modes | `internal/cli/root.go` | `json`(+2) `plain`(+2) `select`(+2) `table`(+2) `csv`(+2) | 10 |
| Auth | `internal/config/config.go` + file existence | `os.Getenv` count: 1 call = 5pts, 2+ calls = 8pts. `internal/cli/auth.go` exists = +2pts | 10 |
| Error Handling | `internal/cli/helpers.go` | `hint:` or `Hint:` present = +5. Count of `code:` occurrences (1pt each, max 5) | 10 |
| Terminal UX | `internal/cli/helpers.go` | `colorEnabled`(+5) `NO_COLOR`(+3) `isatty`(+2) | 10 |
| README | `README.md` | Exact section strings: `Quick Start`(+2) `Output Formats`(+2) `Agent Usage`(+2) `Troubleshooting`(+2) `Doctor`(+2) | 10 |
| Doctor | `internal/cli/doctor.go` | Count HTTP patterns x2: `http.Get`, `http.Head`, `http.NewRequest`, `http.Client`, `httpClient.Get`, `httpClient.Head`, `httpClient.Do` | 10 |
| Agent Native | `internal/cli/root.go` + `helpers.go` | `json`(+2) `select`(+2) `dry-run`/`dryRun`/`dry_run`(+2) `non-interactive`/`nonInteractive`(+1) `stdin`(+1) `"yes"`(+1) `409`/`already exists`(+1) `human-friendly`/`humanFriendly`(+1) | 10 |
| Local Cache | `internal/client/client.go` + `internal/cache/cache.go` | `cacheDir`/`readCache`/`writeCache`(+5) `no-cache`/`NoCache`(+2) `sqlite`/`bolt`/`badger` in cache.go or store.go(+3) | 10 |
| Breadth | `internal/cli/*.go` file count | Excludes: helpers.go, root.go, doctor.go, auth.go. Thresholds: <5=0, 5-10=3, 11-20=5, 21-40=7, 41-60=9, 60+=10 | 10 |

## Key Gotchas

- README scorecard checks for **exact string** `"Doctor"` (capital D), not `"Health Check"` or `"doctor"`
- Agent Native checks root.go + helpers.go ONLY - `stdin` in command files doesn't count
- Auth gives +2 just for `auth.go` existing as a file, regardless of content
- Local Cache gives +3 for `sqlite`/`bolt`/`badger` strings in `internal/cache/cache.go` or `internal/store/store.go`
- Breadth counts files, not commands - one file per endpoint beats one file per resource
