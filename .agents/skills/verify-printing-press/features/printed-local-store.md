# Printed CLI local store

A printed CLI can mirror API data into a local SQLite store. Users then search it offline, read from it with `--data-source local`, and run analytics on it. The store and config live under per-CLI directories that follow the user's HOME, a `<PREFIX>_HOME` root, per-kind `<PREFIX>_*_DIR` overrides, or `--home`.

## Sub-features

- `store-sync` `sync` fetches syncable resources and reports per-resource totals.
- `store-search` `search <query>` falls back to local data when the API has no search endpoint.
- `store-local-read` `--data-source local` on a read command serves synced rows without calling the API.
- `store-paths` `doctor --json` reports config, data, and state paths and which rung chose them.

## How to get to it (user POV)

- `printing-press-golden-pp-cli sync`, then `search`, `stale`, `orphans`, `load`, or `analytics`.
- Any read command with `--data-source local|live|auto`.
- `doctor --json` to see where things are stored, and `--home <dir>` or `PRINTING_PRESS_GOLDEN_HOME` to move them.

## Driving it with pp-verify

Preconditions:

- Healthy doctor. Store state lives under `$RUN/home`. Pass `-e HOME=<fresh dir>` to start from an empty store.

- **Sync.** `$V cli "$RUN" -- sync --json`. Exit 0. The final JSON has `total_records` ≥ 1 and `warned: 0`. The requests include `GET /v1/projects` and `GET /v1/currencies`.
- **Search.** `$V cli "$RUN" -- search Alpha --json`. Exit 0. `meta.reason` is `no_search_endpoint`, `meta.source` is `local`, and the results include `proj-1`. The drive's `.requests.jsonl` is empty.
- **Local read.** `$V cli "$RUN" -- projects list --data-source local --json`. Exit 0. `meta.freshness.reason` is `data_source_local`. The drive's `.requests.jsonl` is empty.
- **Path relocation.** `$V cli "$RUN" -e PRINTING_PRESS_GOLDEN_HOME="$RUN/relocated" -- doctor --json`. `paths.data.dir` is `$RUN/relocated/data`, with rung `home-env`.

## Gotchas

- Before the first sync, `doctor --json` shows `cache.status: unknown` with a hint to run `sync`. That is the expected empty state.
- `projects list` with auto data-source also writes the store. Use a fresh `-e HOME=` when you need to prove that `sync` alone populated it.
- `scripts/e2e-paths-smoke.sh` covers the full path-relocation and credential-consolidation matrix. It builds from `./cmd/printing-press` (a second entry point next to `./cmd/cli-printing-press`), and nothing in CI calls it.
