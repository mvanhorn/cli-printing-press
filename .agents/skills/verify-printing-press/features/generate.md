# Generate a printed CLI

A user points `cli-printing-press generate` at an API spec and gets a buildable Go project: a `<api>-pp-cli` command tree, a `<api>-pp-mcp` server, SKILL.md/README/AGENTS docs, manifests, and staged binaries. The 9 quality gates must pass first.

## Sub-features

- `gen-print` turns a spec into a printed tree and staged binaries.
- `gen-gates` runs the 9 quality gates and reports each verdict.
- `gen-dry-run` parses the spec and reports what it would emit, without writing files.
- `gen-force` regenerates into an existing directory and preserves hand-edits.
- `gen-json` emits a machine-readable result.

## How to get to it (user POV)

- `cli-printing-press generate --spec <path-or-url> [--output <dir>]`. Without `--output`, the default is `~/printing-press/library/<name>`; this skill always passes `--output`.
- `cli-printing-press generate --spec <spec> --dry-run`
- `cli-printing-press generate --spec <spec> --output <dir> --force`
- The `/printing-press` skill calls `generate` during its pipeline.

## Driving it with pp-verify

Preconditions:

- A launched `RUN` with a healthy doctor. Launch itself already performed `gen-print` and `gen-gates` for the golden fixture.

- **Print + gates.** Read `$EVIDENCE/launch-generate.log`. It ends with `PASS printing-press-golden-pp-cli version`, then `PASS ... doctor`, then `Generated printing-press-golden at $RUN/cli`. Each of the 9 gates shows one `PASS` line. `ls "$RUN/cli/build/stage/bin"` lists both `-pp-cli` and `-pp-mcp`.
- **Printed tree shape.** `ls "$RUN/cli"` shows `SKILL.md README.md AGENTS.md manifest.json tools-manifest.json spec.yaml go.mod cmd internal build`.
- **JSON result.** Run `$V press "$RUN" generate --spec testdata/golden/fixtures/golden-api.yaml --output "$RUN/gen-json" --json`. The stdout's last line is `{"name":"printing-press-golden","output_dir":"$RUN/gen-json",...,"validated":true}` and the exit is 0.
- **Dry run.** Run `$V press "$RUN" generate --spec testdata/golden/fixtures/golden-api.yaml --output "$RUN/dry" --dry-run`. The exit is 0, and `test ! -e "$RUN/dry"` holds.
- **Force regen keeps a hand-edit.** Append `// hand-edit-marker` to a generated Go file in `$RUN/gen-json/internal/cli/`. Then run `$V press "$RUN" generate --spec testdata/golden/fixtures/golden-api.yaml --output "$RUN/gen-json" --force`. `grep -r hand-edit-marker "$RUN/gen-json/internal/cli"` still finds it.
- **After a generator change.** Doctor must pass on a fresh launch, then drive whichever feature file covers the emitted surface you changed. If the change is variant-specific (auth type, GraphQL, BLE, MCP orchestration), print that variant's case spec from `testdata/golden/cases/<case>/` with `$V press "$RUN" generate --spec ... --output "$RUN/<case>"` and inspect or build it.

## Gotchas

- A print takes about 40s with a warm Go cache. Each extra `generate` in a run costs the same.
- `generate` warns `could not derive run_id from --research-dir`. That is expected outside the `/printing-press` pipeline and not a failure.
- Without `GOPRIVATE` and `PRINTING_PRESS_AGENTCOOKIE_REPLACE`, the `go mod tidy` gate can fail on private modules. `pp-verify press` sets both, so a bare `./cli-printing-press generate` outside the helper is not equivalent.
- `--dry-run` still fetches remote specs. Use local specs here.
- Never generate without `--output`. The default writes into the real library under the real HOME, unless you went through `pp-verify press`, which decoys HOME.
