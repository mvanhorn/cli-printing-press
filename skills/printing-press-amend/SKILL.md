---
name: printing-press-amend
description: >
  Wrap the dogfood-to-PR loop for a printed CLI in the public library. Mines
  the active Claude Code session transcript for friction (missing flags, hand-
  rolled API payloads, silent-null returns), confirms scope with the user,
  plans + executes the fix autonomously, scrubs PII, and opens a PR against
  mvanhorn/printing-press-library. Two user-in-loop checkpoints: scope after
  capture, PR draft before open.
  Trigger phrases: "amend the CLI", "submit a patch", "fix what I just
  dogfooded", "open a PR for this CLI", "patch this CLI", "use printing-press-amend",
  "run printing-press-amend".
version: 0.1.0
min-binary-version: "4.0.0"
context: fork
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - AskUserQuestion
---

# /printing-press-amend

Turn a dogfood session into a PR for a printed CLI in the public library.

```bash
/printing-press-amend                 # auto-detect target CLI from session
/printing-press-amend superhuman      # explicit short name
/printing-press-amend superhuman-pp-cli
/printing-press-amend ~/printing-press/library/superhuman
```

This skill lives in this repo (the machine) and acts on a printed CLI in the public library. It is sibling to `/printing-press-publish` (adds a new CLI), `/printing-press-polish` (improves a CLI pre-publish), and `/printing-press-retro` (reflects on the machine itself). None of those cover post-publish CLI amendments driven by real-session friction.

The artifact this skill produces is semantically a "patch" (in the git/PR sense), tracked by the public library's `// PATCH(...)` source-comment convention and `.printing-press-patches.json` manifest. The slash-skill name is `amend` to disambiguate from the existing `printing-press patch` binary subcommand (which AST-injects pre-defined features — different mechanism, different intent).

## Setup

Before doing anything else:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 4.0.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

# Prefer local build when running from inside the printing-press repo.
_press_repo=false
if [ -x "$_scope_dir/printing-press" ] && [ -d "$_scope_dir/cmd/printing-press" ]; then
  _press_repo=true
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/printing-press"
elif ! command -v printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/printing-press" ]; then
    echo "printing-press found at ~/go/bin/printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

# Resolve and emit the absolute path the agent must use for every later
# `printing-press` invocation. `export PATH` above only affects this one
# Bash tool call; subsequent calls open a fresh shell and resolve bare
# `printing-press` against the user's default PATH, where a stale global
# can silently shadow the local build. The agent captures this marker and
# substitutes the absolute path into every later invocation.
if [ "$_press_repo" = "true" ]; then
  PRINTING_PRESS_BIN="$_scope_dir/printing-press"
else
  PRINTING_PRESS_BIN="$(command -v printing-press 2>/dev/null || true)"
fi
echo "PRINTING_PRESS_BIN=$PRINTING_PRESS_BIN"

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="$HOME/printing-press"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_CURRENT="$PRESS_RUNSTATE/current"

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY" "$PRESS_MANUSCRIPTS" "$PRESS_CURRENT"
```
<!-- PRESS_SETUP_CONTRACT_END -->

After running the setup contract, capture the `PRINTING_PRESS_BIN=<abs-path>` line from stdout. **Every subsequent `printing-press ...` invocation in this skill must use that absolute path** (substitute the value, not the literal `$PRINTING_PRESS_BIN` token) — `export PATH` above only affects the single Bash tool call it runs in, so later calls open a fresh shell where bare `printing-press` resolves against the user's default `PATH` and a stale global can shadow the local build.

After capturing the binary path, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `<PRINTING_PRESS_BIN> version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, stop immediately and tell the user: "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest` to update."

## Phase 1 — Friction Capture

(To be filled in by U2: read the active Claude Code session transcript at `~/.claude/projects/<dir>/<session>.jsonl`, extract friction signals, categorize each as bug or feature, auto-detect target CLI, confirm with user via `AskUserQuestion`. See `references/transcript-parsing.md`.)

## Phase 2 — Pre-Checkpoint Guards

(To be filled in by U3: cross-reference open + recently-merged PRs in `mvanhorn/printing-press-library` to suppress duplicates, check the printed CLI binary against the library's published `.printing-press.json` version.)

## Phase 3 — Scope Confirmation Checkpoint

(To be filled in by U4: tier the surviving findings, `AskUserQuestion` for scope, persist excluded findings to a deferred-list file at `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-deferred.md`.)

## Phase 4 — Plan + Execute + Validate

(To be filled in by U5: write per-run plan doc, edit files inside `$PRESS_HOME/.publish-repo-$PRESS_SCOPE/library/<category>/<api-slug>/` (the managed clone of the public library — same one `/printing-press-publish` uses), update `.printing-press-patches.json` and add `// PATCH(<short reason>)` source comments at every changed site, run `<PRINTING_PRESS_BIN> publish validate --dir <managed-clone-cli-dir> --json`, retry up to 3 iterations on failure.)

## Phase 5 — PII Scrub

(To be filled in by U6: scrub plan doc, PR title/body, and any test fixtures with shape-preserving tokens. Reuse `/printing-press-retro`'s secret-scrubbing patterns for credentials. User-maintained company/person stop-list at `~/.printing-press/amend-config.yaml`. See `references/pii-scrubbing.md`.)

## Phase 6 — PR Draft Review Checkpoint

(To be filled in by U7: assemble the PR title/body/labels/diff-summary, show inline before any `gh` command fires, `AskUserQuestion` to open / edit / hold / abort.)

## Phase 7 — PR Open

(To be filled in by U7: issue ownership search, fork-clone-branch-commit-push-PR via `references/library-pr-plumbing.md` patterns, apply `comp:<api-slug>` and `priority:P<n>` labels.)

## Phase 8 — Output

Emit the structured `---PATCH-RESULT---` block on completion. Format:

```
---PATCH-RESULT---
pr_url: <url>
pr_number: <n>
branch_name: <name>
api_slug: <slug>
scope_tier: <bugs|bugs+features|all|custom>
files_changed:
- <file>
build_status: <PASS|FAIL>
test_status: <PASS|FAIL>
dogfood_status: <PASS|FAIL|N/A>
pii_scrub_summary: <N tokens replaced across M artifacts>
findings_addressed:
- <one-line-summary>
findings_deferred:
- <one-line-summary>
deferred_list_path: <path>
plan_doc_path: <path>
---END-PATCH-RESULT---
```

## Verification of this skill itself

The static lint pass for this SKILL.md runs via:

```bash
<PRINTING_PRESS_BIN> verify-internal-skill --dir skills/printing-press-amend
```

(See `internal/cli/verify_internal_skill.go` and the matching test file. The setup-contract parity check runs as a Go test in `internal/pipeline/contracts_test.go` — `TestSkillSetupBlocksMatchWorkspaceContract`.)
