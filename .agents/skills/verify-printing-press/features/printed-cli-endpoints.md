# Printed CLI endpoint commands

Each spec operation becomes a command on the printed CLI (`<resource> <action>`). The command sends a real HTTP request with the configured auth and prints the response, as a table or as a JSON envelope. Mutating commands support `--dry-run`, and under `PRINTING_PRESS_VERIFY=1` they become a no-op at the transport layer and never dial.

## Sub-features

- `ep-read` GET commands return API data. With auto data-source, a list command may also refresh the local store.
- `ep-write` POST, PUT, and PATCH commands send the flag-built body and report the API status.
- `ep-dry-run` `--dry-run` prints the request it would send (method, URL, body, masked auth) and sends nothing.
- `ep-verify-gate` with `PRINTING_PRESS_VERIFY=1`, mutating verbs return `verify_noop: true` and send nothing.
- `ep-auth` sends the API key from `<PREFIX>_API_KEY` as the spec's header (`X-API-Key` for golden).
- `ep-required` a missing required flag exits 2 with a usage error.

## How to get to it (user POV)

- `printing-press-golden-pp-cli projects list|get|create`, `currencies`, `public get-status`, `tickets query`, `reports ...`. Take the names from `--help`.
- Any command with `--json`, or with `--agent` (which also implies `--compact --no-input --no-color`).
- Agents reach the same operations through the MCP server's typed tools; see `printed-mcp-server.md`.

## Driving it with pp-verify

Preconditions:

- Healthy doctor. The mock serves `proj-1`/`Alpha` for `GET /v1/projects` and echoes a `proj-new` project for `POST /v1/projects`.

- **Read.** `$V cli "$RUN" -- projects list --json`. Exit 0. `results.data[0].id` is `proj-1`. The drive's `.requests.jsonl` contains `GET /v1/projects?limit=25` with `"x_api_key": "verify-token"`.
- **Single read.** `$V cli "$RUN" -- projects get proj-1 --json` (check `projects get --help` for the arg form). Exit 0. The requests show `GET /v1/projects/proj-1`.
- **Write.** `$V cli "$RUN" -- projects create --name Delta --visibility private --json`. Exit 0, `"status": 201`, `"success": true`. The requests show exactly one `POST /v1/projects` with body `{"name": "Delta", "visibility": "private"}`.
- **Dry run.** `$V cli "$RUN" -- projects create --name Beta --visibility private --dry-run --json`. Exit 0. stdout shows `POST http://127.0.0.1:<port>/v1/projects`, the body, `X-API-Key: ****`, and `"dry_run": true`. The drive's `.requests.jsonl` is empty.
- **Verify gate.** `$V cli "$RUN" -e PRINTING_PRESS_VERIFY=1 -- projects create --name Gamma --json`. Exit 0. `data.reason` is `verify_short_circuit`, `"verify_noop": true`, `"success": false`. The drive's `.requests.jsonl` is empty.
- **Required flag.** `$V cli "$RUN" -- projects create --name Delta --json`. Exit 2, with stderr `required flag "visibility" not set`, and no requests.

## Gotchas

- `projects list` with the default `--data-source auto` makes extra GETs to refresh the store (`sync_*` events appear on stderr). Count requests by path, not by total.
- Under `PRINTING_PRESS_VERIFY=1`, required-flag validation is relaxed, so a create with missing flags still returns the noop envelope. Use the non-verify drive to test required flags.
- `-e PRINTING_PRESS_VERIFY_LIVE_HTTP=1` re-enables real dialing under VERIFY. Set it only when testing that override.
- `pp-verify cli` runs under `env -i`. A behavior that depends on an inherited variable will not show up unless you pass it with `-e`.
