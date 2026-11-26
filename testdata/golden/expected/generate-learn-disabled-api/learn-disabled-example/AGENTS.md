# Learn Disabled Example Printed CLI Agent Guide

This directory is a generated `learn-disabled-example-pp-cli` printed CLI. It was produced by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press), so treat systemic fixes as upstream Printing Press fixes first. Keep local edits narrow and document why a generated-tree patch belongs here.

## Local Operating Contract

This file is for agents changing this CLI's source. Build and test here, then run that binary:

```bash
go build -o ./learn-disabled-example-pp-cli ./cmd/learn-disabled-example-pp-cli
go test ./...
```

Leave any installed `learn-disabled-example-pp-cli` on `PATH` unused. It uses the operator's config, data, state, and cache, and commands can write the local store or send live API requests.

Relocate all four directories to an empty absolute directory before every run. `LEARN_DISABLED_EXAMPLE_HOME` does that for the process; `--home` does it for one invocation. Both win over `HOME`, `USERPROFILE`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_CACHE_HOME`. `LEARN_DISABLED_EXAMPLE_CONFIG_DIR`, `LEARN_DISABLED_EXAMPLE_DATA_DIR`, `LEARN_DISABLED_EXAMPLE_STATE_DIR`, and `LEARN_DISABLED_EXAMPLE_CACHE_DIR` win over both, so clear them when they were inherited. Those variables must be absolute; a relative value is skipped. `--home` must be absolute. On Unix, `mktemp -d` creates the directory; on Windows, pass an empty absolute path.

Set `LEARN_DISABLED_EXAMPLE_CONFIG` to a file inside that directory, even when the file does not exist yet. Without it, a missing config under the relocated directory loads `~/.config/learn-disabled-example-pp-cli/config.toml` from the real home (`os.UserHomeDir`): `Load` applies that file, `doctor` opens it, and a later save can rewrite it. An inherited `LEARN_DISABLED_EXAMPLE_CONFIG` is a config path, so replace it. `--config` is that same explicit file for one invocation. `--home` does not stop those reads.

`os.UserHomeDir` ignores `LEARN_DISABLED_EXAMPLE_HOME`. On Unix it reads `HOME`. On Windows it reads `USERPROFILE` and ignores `HOME` when `USERPROFILE` is set. `devcli` sets both, plus `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_CACHE_HOME`, on the CLI process only. An inherited `XDG_*_HOME` value is used when `LEARN_DISABLED_EXAMPLE_HOME` is unset or relative. Do not export HOME or USERPROFILE for the shell; `go` and `git` keep the real profile.

```bash
iso="$(mktemp -d)"
export LEARN_DISABLED_EXAMPLE_HOME="$iso"
export LEARN_DISABLED_EXAMPLE_CONFIG="$iso/config.toml"
export LEARN_DISABLED_EXAMPLE_CONFIG_DIR=
export LEARN_DISABLED_EXAMPLE_DATA_DIR=
export LEARN_DISABLED_EXAMPLE_STATE_DIR=
export LEARN_DISABLED_EXAMPLE_CACHE_DIR=
devcli() {
  env HOME="$iso" USERPROFILE="$iso" \
    XDG_CONFIG_HOME="$iso/xdg/config" \
    XDG_DATA_HOME="$iso/xdg/data" \
    XDG_STATE_HOME="$iso/xdg/state" \
    XDG_CACHE_HOME="$iso/xdg/cache" \
    ./learn-disabled-example-pp-cli "$@"
}
devcli doctor --json
devcli agent-context --pretty
```

Keep `LEARN_DISABLED_EXAMPLE_HOME`, `LEARN_DISABLED_EXAMPLE_CONFIG`, and the cleared directory variables exported, and run later commands through `devcli`, including `which` and `--help`. Send live API requests only when the task requires them.

Use runtime discovery on that binary instead of a copied command list:

```bash
devcli which "<capability>" --json
devcli <command> --help
```

Add `--agent` to command invocations for JSON, compact output, non-interactive defaults, and no color:

```bash
devcli <command> --agent
```

Before running an unfamiliar command that may mutate remote state, inspect its help and prefer a dry run:

```bash
devcli <command> --help
devcli <command> --dry-run --agent
```

A completed `--dry-run` reports `"success": true` and `"dry_run": true` and omits HTTP `status`. Hand-written commands should call `writeDryRun` for that shape. A dry run that fails validation exits non-zero with `"success": false`.

When a command requires confirmation, pass `--yes` explicitly only after the target, arguments, and side effects are clear. `--agent` does not imply `--yes`.

## Novel Command Data Sources

Every hand-written novel command must declare its strategy in a Go line comment:

```go
// pp:data-source auto
```

Use exactly one of `auto`, `local`, `live`, or `computed`. Keep `auto` when the command reads a synced collection and can fall back locally to the same question: top-level resources, and parent-scoped sub-collections whose rows were stored with `parent_id`. Pass `resourceType` as the stored name or the path segment; camelCase, snake_case, and kebab-case names for the same resource resolve to the stored table, and a path like `/parents/{id}/children` returns only that parent's rows. Use `live` when the read is not a synced collection. A local read that cannot apply a row-selecting parameter (`filter`, `orderBy`/`sort`/`order`, search, cursor, a page without a usable limit, or an equality key that is not a stored field) fails with a clear error naming those parameters and suggesting `--data-source live`; it does not return a different row set, and `auto` does the same when live is unreachable. Projection parameters such as `fields` stay warnings. Use `local` for local-only reads and `computed` for pure computation from embedded rules. Change a generated scaffold's `auto` default deliberately when its implementation has a narrower source, and keep `cmd.Annotations["pp:data-source"]` on the same command in sync; `--agent` envelopes read that annotation for `meta.source`. Root rejects an incompatible `--data-source` for a `live` or `local` annotation before any network or store access; `auto`, `computed`, and unannotated commands are unchanged. A command that is live or local only in some modes stays unannotated and validates in its own RunE. TODO stubs still fail dogfood even when annotated.

## Platform Credential References

Normal API authentication is separate from optional platform-source credential
resolution. If this CLI uses indirect references for a tenant-gated platform
source, add the downstream registration in a preserved hand-authored file
under `internal/cli/` and provide both `CredentialResolverFactory` and
`ValidateSourceProfile` on `platformSourceRegistration` for any selected source
that has references. A source with no references may omit both hooks and receives
an empty credential map. Keep reference values opaque to shared profile code,
validate only the selected source in the downstream hook, and never persist
resolved credential bytes. Do not edit generator-owned `internal/platform`
packages; a reprint refreshes those files while retaining the downstream
registration file.

For install, auth, examples, and longer product guidance, read `README.md` and `SKILL.md`. This file intentionally stays small so repo-local agents get invariant local guidance without duplicating the generated docs.

## Release Ledger

`CHANGELOG.md` and `.printing-press-release.json` are the public library's per-CLI release ledger. Fresh prints carry an unstamped runtime version such as `0.0.0-dev`; the final `YYYY.M.N` CLI release version is assigned only after a publish PR merges in `mvanhorn/printing-press-library`. Do not hand-bump those files or edit `var version = ...` for release bookkeeping; preserve existing ledger files on reprint and let the library workflow stamp the next release.

## Local Customizations

This directory is **generated output** -- a fresh print can overwrite the whole tree, so ad-hoc hand-edits don't survive on their own. If you modify the generated code, record each change under `.printing-press-patches/` (parallel to `.printing-press.json`). Regen and publish-validate read those records and fail closed when a recorded file or call site is gone, so a dropped customization cannot ship as if it were still applied.

The entry shape, and the altitude to write it at -- a durable reprint-guard, not a changelog -- live in the public library's `AGENTS.md`, which is the single source of truth; this guide intentionally doesn't duplicate them.
