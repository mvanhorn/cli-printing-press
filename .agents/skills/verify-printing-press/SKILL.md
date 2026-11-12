---
name: verify-printing-press
description: Drive the real Printing Press end to end and capture proof. It builds the cli-printing-press binary, prints the golden fixture CLI into a decoy HOME, and runs the printed CLI, its MCP server, and the press's own verifiers against a request-logging mock. Use it to prove that a generator, template, MCP, or verifier change works in an emitted CLI, rather than only in Go tests or golden text.
---

# Verify the Printing Press

The product surface is a CLI that produces other CLIs. A user runs `cli-printing-press generate` on a spec and gets a printed CLI: `<api>-pp-cli`, plus `<api>-pp-mcp` for agents. Proving a change means printing a CLI and then driving what was printed. The four surfaces, in order of how often a change touches them:

1. `cli-printing-press generate` (spec → printed CLI tree, with 9 quality gates)
2. The printed CLI's commands against an API
3. The printed MCP server's tools and annotations
4. The press's verifiers run on a printed CLI (`verify`, `verify-skill`, `scorecard`, `shipcheck`, `dogfood`)

Everything goes through `scripts/pp-verify` (paths below are relative to this skill directory). It never touches the real `~/printing-press` or the real config of a printed CLI. Each run gets its own decoy HOME, output dir, mock port, and evidence directory, so two runs can go side by side. Every drive in this skill uses the fixture `testdata/golden/fixtures/golden-api.yaml`, which prints `printing-press-golden-pp-cli` with env prefix `PRINTING_PRESS_GOLDEN_`.

This skill complements `scripts/golden.sh verify`, `scripts/verify-generator-output.sh`, and `go test ./...`. Those prove bytes and compilation. This skill proves behavior: what the printed binary actually sends, refuses, and reports.

## Launch

```bash
.agents/skills/verify-printing-press/scripts/pp-verify launch
```

This takes about 45s with a warm Go cache. In order, it:
- builds `./cli-printing-press` from `./cmd/cli-printing-press`
- creates `RUN=$TMPDIR/pp-verify.XXXXXX` with a decoy HOME that holds a sentinel file
- runs `generate` into `$RUN/cli` with all quality gates on
- starts `scripts/mock_api.py` on a free 127.0.0.1 port, logging every request to `$RUN/req.log`

**Ready means:**
- `generate` exited 0
- its log contains `PASS printing-press-golden-pp-cli doctor`
- `$RUN/cli/build/stage/bin/` holds both the CLI and the MCP binary
- the mock published its port

`pp-verify` checks all four and prints two lines. Copy them; every later command needs `RUN`:

```
RUN=/var/folders/.../pp-verify.AbC123
EVIDENCE=<repo>/.gotmp/verify-printing-press/evidence/<timestamp>-<pid>
```

Your real Go build and module caches are reused on purpose. The decoy HOME would otherwise force a cold build of about 5 minutes.

## Doctor

```bash
.agents/skills/verify-printing-press/scripts/pp-verify doctor "$RUN"
```

Read-only. It checks:
- the repo binary and HEAD are unchanged since launch
- both printed binaries are staged
- the mock PID is ours and alive, and answers on its port
- the decoy sentinel is intact
- the printed CLI's own `doctor --json` reports `api: reachable`, `auth_source: env:PRINTING_PRESS_GOLDEN_API_KEY`, and a `config_path` under the decoy HOME

Run it before the first drive, again after any drive that did something surprising, and whenever you edit generator code mid-run. A changed binary or HEAD fails doctor: the printed CLI predates your change, so clean up and relaunch.

## Drive

Every drive command saves its stdout, stderr, exit code, and the exact mock requests it caused. It also prints its own output, so you can read the result inline.

```bash
V=.agents/skills/verify-printing-press/scripts/pp-verify

# Printed CLI: clean env (env -i), decoy HOME, API key + base URL pointed at the mock
$V cli "$RUN" -- projects list --json
$V cli "$RUN" -- projects create --name Delta --visibility private --json
$V cli "$RUN" -e PRINTING_PRESS_VERIFY=1 -- projects create --name Gamma --json   # extra env with -e K=V

# Printed MCP server over stdio
$V mcp "$RUN" list                                       # tool names + annotations
$V mcp "$RUN" call projects_get '{"projectId":"proj-1"}'
$V mcp "$RUN" -e PRINTING_PRESS_GOLDEN_MCP_READ_ONLY=1 list

# The press itself, with the decoy HOME (run from repo root, relative spec paths OK)
$V press "$RUN" verify --dir "$RUN/cli" --spec testdata/golden/fixtures/golden-api.yaml --json
$V press "$RUN" verify-skill --dir "$RUN/cli"

# Everything the mock has received this run
$V requests "$RUN"
```

To find a command or tool handle, read the printed tree's help (`$V cli "$RUN" -- --help`, `$V cli "$RUN" -- <cmd> --help`) or `$V mcp "$RUN" list`. Treat those, not memory, as the stable handles. The mock (`scripts/mock_api.py`) answers the golden fixture's routes with fixed JSON (`proj-1`/`Alpha`, `task-1`, `USD`). Any other path gets `{"ok": true}` with status 200.

To verify a generator change against a different spec, generate it yourself:

```bash
$V press "$RUN" generate --spec <spec> --output "$RUN/other"
```

Then drive it with `press verify --dir "$RUN/other" --spec <spec>`. That uses the press's spec-derived mock. `pp-verify cli` and `pp-verify mcp` are wired only to the golden print.

## Evidence

Evidence goes to `$EVIDENCE`, which is `.gotmp/verify-printing-press/evidence/<run-id>/` in the repo (gitignored). It survives cleanup:

- `launch-generate.log`: full generate output, including the 9 quality gate verdicts
- `NNN-<label>.stdout`, `.stderr`, `.requests.jsonl`: one set per drive
- `transcript.md`: per drive, the command, exit code, and mock request count
- `all-requests.jsonl`: written at cleanup
- `home-escape.txt`: present only if something wrote under the real HOME

**Proof standards:**
- Drive the printed binary the way a user or agent does: CLI args or MCP `tools/call`. Do not call generator Go functions, and do not edit the printed tree to make a drive pass.
- For a write, proof is the request in `NNN-*.requests.jsonl` (method, path, body, `X-API-Key`) plus the CLI's envelope, not only `success: true`.
- For a refusal or no-op (`--dry-run`, `PRINTING_PRESS_VERIFY=1`, MCP read-only mode), proof is **zero** new mock requests for that drive, plus the envelope that says why (`dry_run: true`, `verify_noop: true`, or `tool not found`). Observe the zero; don't trust the flag name.
- For an MCP annotation claim, proof is the `annotations` object from `mcp list`, not the template source.
- For a generator change, show the drive on a print made after the change. Doctor's binary/HEAD check enforces this.
- Mocks are acceptable only because the printed client's base URL is a production config surface (`<PREFIX>_BASE_URL`). Never stub inside the printed code.

## Cleanup

```bash
.agents/skills/verify-printing-press/scripts/pp-verify cleanup "$RUN"
```

Cleanup:
- kills only the mock PID this run started, after confirming its command line references this `$RUN`
- copies the request log into evidence
- checks the real `~/printing-press` and the printed CLI's real config, data, and state dirs for files modified during the run, and writes `home-escape.txt` and a warning if any changed
- moves `$RUN` to the trash

Evidence stays. Run cleanup after a failed launch too: launch prints the cleanup command when it dies. Never `pkill python3` or kill by name.

## Helpers

All three are executable and live in `scripts/`:
- `pp-verify`: the entry point above. Run it with no arguments for usage.
- `mock_api.py PORT_FILE REQUEST_LOG`: request-logging JSON mock. It binds `127.0.0.1:0`, writes its port to PORT_FILE, and appends one JSON line per request.
- `mcp_call.py MCP_BIN list | call TOOL JSON`: minimal MCP stdio client. It runs `initialize`, then `tools/list` (paginated) or `tools/call`. It inherits its environment.

## Feature map

`features/README.md` is the index; read it before driving. Each feature file holds the recipe and the observable end state that proves the feature works. When a change touches a feature, drive every entry point that feature lists, not just one convenient one.
