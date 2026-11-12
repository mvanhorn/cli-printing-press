# Printing Press verification map

This directory is the maintained source for verifying what a user of the Printing Press sees: the press generating a CLI, and what that printed CLI and its MCP server then do. Read this index before driving, then use the matching feature file as the recipe.

## Baseline preconditions

- `pp-verify launch` succeeded and you hold its `RUN` and `EVIDENCE`.
- `pp-verify doctor "$RUN"` prints `doctor: healthy`.
- The print is `printing-press-golden-pp-cli` from `testdata/golden/fixtures/golden-api.yaml`, made by the binary built at launch.
- Never drive a `RUN` you did not launch, and never point drives at the real `~/printing-press/library`.

## Driving conventions

- `V=.agents/skills/verify-printing-press/scripts/pp-verify`, run from the repo root.
- Printed-CLI actions: `$V cli "$RUN" -- <args>`. MCP actions: `$V mcp "$RUN" list|call ...`. Press actions: `$V press "$RUN" <args>`.
- Add `--json` to printed-CLI commands when the proof is in the envelope.
- Take command names, flags, and tool names from `--help` and `mcp list` on this print. They are derived from the spec and change when naming rules change.
- Recipes that change mock-side state need no restore, because the mock is stateless. Recipes that write the printed CLI's local store (sync, teach) write only under the decoy HOME.

## Proof and skip reporting

- CLI proof is the drive's `.stdout`, `.stderr`, exit code, and `.requests.jsonl`, all saved by `pp-verify`.
- A write's proof includes the request body the mock received. A no-op's proof includes zero requests for that drive.
- An MCP proof includes the `tools/list` annotations or the `tools/call` result content.
- Name the feature file and sub-feature ID with each claim.
- Report an unreachable path with the command tried and the unmet precondition (for example, `sql` is not a CLI command on this print). Do not report it as verified through a different entry point.

## Feature entry contract

Each feature file has an H1 title, one paragraph describing the user-visible behavior, then exactly these H2s in order: `Sub-features`, `How to get to it (user POV)`, `Driving it with pp-verify` (starting with `Preconditions:`), `Gotchas`.

## Features

- [Generate a printed CLI](./generate.md): spec to printed tree, quality gates, staged binaries, `--dry-run`, regeneration with `--force`.
- [Printed CLI endpoint commands](./printed-cli-endpoints.md): reads, writes, `--dry-run`, the `PRINTING_PRESS_VERIFY=1` transport gate, auth, output envelopes.
- [Printed MCP server](./printed-mcp-server.md): tool catalog, safety annotations, `tools/call`, the `<PREFIX>_MCP_READ_ONLY=1` switch.
- [Printed CLI local store](./printed-local-store.md): `sync`, `search`, `--data-source local`, path relocation.
- [Press verifiers on a print](./press-verifiers.md): `verify`, `verify-skill`, `scorecard`, `shipcheck`.
