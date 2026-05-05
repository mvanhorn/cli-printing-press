---
title: "Printing-press runstate and publish/archive layout contract"
date: 2026-03-28
last_updated: 2026-05-05
category: best-practices
module: printing-press output layout
problem_type: best_practice
component: tooling
symptoms:
  - "Skills fail when the repo is cloned outside ~/cli-printing-press or run from a worktree"
  - "Mutable run state collides across parallel workspaces or repeated runs"
  - "Runtime tooling breaks when a claimed output directory uses a suffix like -pp-cli-2"
  - "Research and proof artifacts leak into repo-local docs/plans or other source directories"
  - "publish package returns a stale run_id when both API-slug and CLI-name manuscripts dirs exist"
root_cause: config_error
resolution_type: workflow_improvement
severity: high
tags:
  - printing-press
  - worktrees
  - output-paths
  - pp-cli
  - manuscripts
  - runstate
  - workspace-scope
  - lookup-priority
---

# Printing-press runstate and publish/archive layout contract

## Problem

The press used to assume one clone path and one shared mutable output namespace. That broke as soon as the repo lived somewhere other than `~/cli-printing-press`, ran inside a git worktree, or generated the same API more than once.

## Symptoms

- Skills looked for repo-relative or home-relative paths that did not exist in worktrees.
- Active runs shared mutable directories, so parallel worktrees could stomp on each other.
- Manuscript artifacts mixed source code with user output.
- Verification code failed to build claimed directories like `stripe-pp-cli-2` because it guessed `cmd/stripe` instead of `cmd/stripe-pp-cli`.

## What Didn't Work

- Hardcoding `~/cli-printing-press` in skills or docs.
- Treating the generated project directory name as the authoritative command directory name.
- Using published output directories as the source of truth for active mutable runs.
- Writing research, audit, dogfood, or emboss artifacts into repo `docs/plans/`.

## Solution

Use a checkout-scoped runstate derived from the current git root for active work, then publish finished output into global archive locations:

```text
~/printing-press/
  .runstate/<scope>/
    current/
      <api>.json
    runs/
      <run-id>/
        state.json
        manifest.json
        working/
          <api>-pp-cli/
        research/
        proofs/
        pipeline/
        discovery/      (optional — sniff/crowd-sniff captures, reports, URL lists)
  library/
    <api>[-N]/
  manuscripts/<api>/<run-id>/
    research/
    proofs/
    pipeline/
    discovery/          (optional — only for sniff/crowd-sniff derived CLIs)
    manifest.json
```

`<scope>` must be derived from the resolved git root path, not the current shell directory. In Go, the canonical helpers live in `internal/pipeline/paths.go`. In skills, compute the same scope before reading or writing anything:

```bash
REPO_ROOT="$(git rev-parse --show-toplevel)"
PRESS_BASE="$(basename "$REPO_ROOT" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
[ -n "$PRESS_BASE" ] || PRESS_BASE="workspace"
PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$REPO_ROOT" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="$HOME/printing-press"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
```

Keep the naming contract explicit:

- Library directory: `<api>` (keyed by API slug, e.g., `notion`, `cal-com`)
- Binary name: `<api>-pp-cli` (unchanged — the `-pp-cli` suffix is for binary names only)
- Legacy compatibility: accept `<api>-cli` when discovering older projects
- Claimed reruns: allow outer directories like `<api>-2`, but still resolve the actual command entrypoint from `cmd/<api>-pp-cli`

The runtime verifier should discover the command directory independently of the outer project folder:

```go
apiName := naming.TrimCLISuffix(filepath.Base(dir))
cmdDir := filepath.Join(dir, "cmd", naming.CLI(apiName))
```

If that direct lookup fails, scan `cmd/` for a directory satisfying `naming.IsCLIDirName(...)` before falling back to generic single-entry behavior.

Artifact placement rules:

- Active managed runs write only inside `.runstate/<scope>/runs/<run-id>/...`
- Published generated code goes under `library/`
- Archived research documents go under `manuscripts/<api>/<run-id>/research/`
- Archived scorecard, dogfood, emboss, and similar evidence go under `manuscripts/<api>/<run-id>/proofs/`
- Archived phase seeds and pipeline records go under `manuscripts/<api>/<run-id>/pipeline/`
- Archived sniff/crowd-sniff discovery evidence goes under `manuscripts/<api>/<run-id>/discovery/` (optional — absent when no discovery method ran)
- Resume and current-run discovery should read `.runstate` first, not global `library/` or `manuscripts/`

## Why This Works

The checkout scope isolates parallel worktrees without forcing users to hand-configure output paths. Separating active `.runstate` from published `library/` and archived `manuscripts/` keeps mutable work, distributable binaries, and historical evidence from contaminating each other. The `-pp-cli` contract removes ambiguity with upstream or official CLIs, while compatibility helpers still let older `-cli` outputs be rediscovered.

Most importantly, the runtime and skill layers now share the same assumptions:

- same scope derivation
- same CLI suffix
- same current-run lookup model
- same publish/archive layout
- same fallback behavior for older state files

That removes the class of bugs where docs say one thing, skills do another, and Go code expects a third layout.

## Prevention

- Never hardcode `~/cli-printing-press` in skills, docs, or code paths. Always resolve `git rev-parse --show-toplevel` first.
- When adding a new artifact-producing phase, decide first whether it belongs in runstate `research/`, `proofs/`, `pipeline/`, or `discovery/`, and whether it must also be archived at publish time. Do not default to repo `docs/plans/`.
- If a feature writes a file tied to a generated CLI, test both canonical and claimed output dirs, for example `notion` and `notion-2`.
- Keep naming logic centralized in `internal/naming/` and path logic centralized in `internal/pipeline/paths.go`.
- Add tests whenever code infers API names or command directories from filesystem paths.
- Treat published library CLIs as immutable outputs. Workflows like `emboss` should copy them into a new runstate working dir instead of mutating them in place.
- Update README and onboarding docs whenever default output locations change. Path migrations that only touch code are incomplete by definition.
- **Read-side lookup priority must match the write-side convention.** When a binary helper resolves a manuscripts directory (or any other archive path the SKILL writes), check the canonical SKILL key (`<api-slug>`) before any legacy or compatibility key (`<cli-name>`, `<api>-cli`). Label the branches with the convention owner (`SKILL convention` / `legacy binary convention`) — never with temporal adjectives like `new convention` / `old convention`, which invert silently when the convention shifts. Enforcement point in code: `resolveManuscripts` in `internal/cli/publish.go`.
- **Test ordered lookups with a both-keys-present fixture.** Independent per-branch subtests cannot detect priority inversions — both branches resolve fine in isolation, and only the both-keys case exercises which one wins. For any helper that walks an ordered key list, add a fixture that populates every key simultaneously and asserts the canonical key wins. Reprints inevitably create both keys, so an inverted priority will silently publish stale artifacts until a both-keys test catches it. (See `TestManuscriptLookupPriority` in `internal/cli/publish_resolve_test.go` for the pattern.)

Recommended verification for future changes:

- `go test ./internal/pipeline ./internal/cli`
- `go test ./...`
- Manual spot-check: generate once into `<api>-pp-cli`, then again into `<api>-pp-cli-2`, and confirm `verify`, `scorecard`, and `emboss` still resolve the project correctly.

## Related Issues

- `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md` is related only in that both docs codify “shared rules that must stay in sync.” It does not cover path layout or output scoping.
- [#597](https://github.com/mvanhorn/cli-printing-press/issues/597) — Cal.com retro that surfaced F2: `resolveManuscripts` lookup priority lagged the SKILL's API-slug archive convention.
- [#598](https://github.com/mvanhorn/cli-printing-press/issues/598) / [PR #615](https://github.com/mvanhorn/cli-printing-press/pull/615) — fix that swapped the lookup order in `resolveManuscripts` (API-slug first, CLI-name as legacy fallback) and added the both-keys-present regression test that the prevention rule now requires.
