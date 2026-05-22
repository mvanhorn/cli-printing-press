---
name: printing-press-amend
description: >
  Amend a published CLI from one of two input sources: (1) dogfood mode mines
  the active Claude Code session transcript for friction (missing flags, hand-
  rolled API payloads, silent-null returns); (2) direct-input mode accepts
  user-supplied asks (rename a command, add commands or feeds, fix a named bug,
  optionally sniff the source site for new endpoints). Confirms scope with the
  user, plans + executes the fix autonomously, scrubs PII, and opens a PR
  against mvanhorn/printing-press-library. Two user-in-loop checkpoints: scope
  after capture, PR draft before open.
  Trigger phrases: "amend the CLI", "submit a patch", "fix what I just
  dogfooded", "open a PR for this CLI", "patch this CLI", "add features to my
  CLI", "rename this command", "add these feeds to <cli>", "sniff for new APIs
  in <cli>", "amend with these ideas", "use printing-press-amend",
  "run printing-press-amend".
version: 0.2.0
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

The artifact this skill produces is semantically a "patch" (in the git/PR sense), tracked by the public library's `// PATCH(...)` source-comment convention and `.printing-press-patches.json` manifest. The slash-skill name is `amend` to disambiguate from the existing `cli-printing-press patch` binary subcommand (which AST-injects pre-defined features — different mechanism, different intent).

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
if [ -x "$_scope_dir/cli-printing-press" ] && [ -d "$_scope_dir/cmd/cli-printing-press" ]; then
  _press_repo=true
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/cli-printing-press"
elif ! command -v cli-printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/cli-printing-press" ]; then
    echo "cli-printing-press found at ~/go/bin/cli-printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "cli-printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

# Resolve and emit the absolute path the agent must use for every later
# `cli-printing-press` invocation. `export PATH` above only affects this one
# Bash tool call; subsequent calls open a fresh shell and resolve bare
# `cli-printing-press` against the user's default PATH, where a stale global
# can silently shadow the local build. The agent captures this marker and
# substitutes the absolute path into every later invocation.
if [ "$_press_repo" = "true" ]; then
  PRINTING_PRESS_BIN="$_scope_dir/cli-printing-press"
else
  PRINTING_PRESS_BIN="$(command -v cli-printing-press 2>/dev/null || true)"
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

After running the setup contract, capture the `PRINTING_PRESS_BIN=<abs-path>` line from stdout. **Every subsequent `cli-printing-press ...` invocation in this skill must use that absolute path** (substitute the value, not the literal `$PRINTING_PRESS_BIN` token) — `export PATH` above only affects the single Bash tool call it runs in, so later calls open a fresh shell where bare `cli-printing-press` resolves against the user's default `PATH` and a stale global can shadow the local build.

After capturing the binary path, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `<PRINTING_PRESS_BIN> version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, stop immediately and tell the user: "cli-printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest` to update."

## Phase 0 — Input Mode Detection

This skill accepts two input sources for the finding list it later patches: a Claude Code session transcript (dogfood mode, current behavior) and user-supplied asks in the slash-command prompt (direct-input mode, added in v0.2). The two modes diverge only in Phase 1; Phase 2 onward is mode-agnostic and consumes a typed finding list with identical shape regardless of source.

Decide the mode before Phase 1 runs.

### Detection rubric

Read the slash-command prompt body and the immediate invocation turn from the conversation context. Classify into one of four branches:

- **`MODE=direct`** — the prompt contains a concrete CLI name AND at least one direct-input signal:
  - Action verbs targeting the CLI: `rename`, `add`, `remove`, `fix`, `sniff`, `discover`
  - Explicit URLs the user wants added (e.g., `https://example.com/feed/x`)
  - An enumerated list of feeds, commands, endpoints, or features
  - Phrasing like "these ideas", "these features", "with the following"
- **`MODE=dogfood`** — the prompt is empty, OR names a CLI without any asks ("amend the superhuman CLI"), OR explicitly references the session ("what I just dogfooded", "this session's friction", "from my session today")
- **`MODE=both`** — the prompt clearly references both: a session AND specific asks ("I dogfooded this session and also want to add feature X", "in addition to the friction I hit, please add command Y")
- **Ambiguous** — only one signal is present (CLI named with no verbs, or verbs with no target CLI, or asks worded so they could be friction reports OR new asks). Ask the user via `AskUserQuestion`:

  > "Two ways to source findings for this amend. Which fits?
  >   1. Mine the current session transcript (dogfood mode)
  >   2. Use the asks I just typed (direct-input mode)
  >   3. Both — combine transcript friction with my asks"

Default when no slash-command prompt is present at all: `MODE=dogfood`. This preserves the canonical UX — `/printing-press-amend` with nothing after still works exactly as it did in v0.1.

### Persist the mode

Write the resolved mode to `$PRESS_RUNSTATE/current/mode.txt` so later phases (and a resumed run) can read it:

```bash
echo "$MODE" > "$PRESS_RUNSTATE/current/mode.txt"
```

### Output

Phase 0 emits one line to Phase 1:

```yaml
mode: <dogfood|direct|both>
```

Phase 1 branches on this value — dogfood findings flow through `### 1a`, direct-input findings flow through `### 1b`, and combined runs execute both sub-sections in sequence. Phase 2 onward ignores the mode entirely — the finding list is the contract.

## Phase 1 — Capture

This phase produces a typed finding list. The list shape is identical across modes: each finding carries `id`, `kind`, `category`, `classification` (bug or feature), `evidence`, `target_cli`, `rationale`, and `provenance` (`transcript` for dogfood, `user-ask` for direct, `sniff` for sniff-derived). Phase 2 consumes the list verbatim.

When `MODE=dogfood`, run only `### 1a`. When `MODE=direct`, run only `### 1b`. When `MODE=both`, run `### 1a` first, then `### 1b`, and merge the two finding lists with non-colliding IDs (1b continues numbering where 1a left off).

### 1a. Dogfood mode (MODE=dogfood)

Read `references/transcript-parsing.md` for the full procedure. Summary of what this sub-section does:

1. **Resolve the active session transcript file** — derive `<project-dir-slug>` from the current working directory, list `~/.claude/projects/<slug>/*.jsonl` by mtime, pick the most-recently-modified. ALWAYS confirm the resolved path with the user via `AskUserQuestion` before reading — wrong-file selection ingests friction from the wrong session.

2. **Walk the transcript and extract friction signals** — non-zero exit codes, error messages, hand-rolled API payloads (e.g. direct `curl` POSTs that should be a CLI command), retry-after-failure patterns, agent commentary like "X doesn't exist" / "X returns 400", missing-flag references, silent-null returns, auth confusion. Each signal carries timestamp + category + verbatim evidence + the `<slug>-pp-cli` it references.

3. **Classify each signal as bug or feature** with a one-line rationale. Bug = CLI behavior is wrong; feature = CLI behavior is missing. The classification is the agent's best read; the user confirms or overrides at the U4 scope checkpoint.

4. **Auto-detect target CLI** — count occurrences of each `<slug>-pp-cli` in the signals, propose the most-touched CLI as the default. Confirm with `AskUserQuestion` (single CLI: simple yes/no; multiple close: pick from list). When the user passed an explicit `<cli-name-or-path>` argument, skip auto-detect.

5. **Resolve target paths** — accept short name, full name, or absolute path (per R4). Look up the public-library category by walking `~/printing-press-library/library/*/` for a matching directory. The category is needed by U7's PR open phase and is captured here so it doesn't have to be re-derived.

Each finding emitted by 1a carries `provenance: transcript`. Output flows into Phase 2 as the structured finding list documented in `references/transcript-parsing.md`.

### 1b. Direct-input mode (MODE=direct)

Read `references/direct-input-parsing.md` for the full procedure (introduced in v0.2). Summary of what this sub-section does:

1. **Read the slash-command prompt body** plus the immediate agent-message turn that fired the skill — these carry the user's verbatim asks (e.g., "rename Digg 1000 to Digg, add these four feeds: ..., sniff for new endpoints"). There is no transcript to confirm; skip the U1 transcript-path modal that 1a runs.

2. **Resolve the target CLI** — same name-resolution rules as 1a step 4-5 (per R4), but the CLI is normally already named in the prompt itself. Extract via regex (`<slug>-pp-cli` or "the <slug> CLI"); if absent, ask the user.

3. **Parse the asks into structured findings** using the rubric in `references/direct-input-parsing.md`. Each ask maps to one finding with a typed `kind` field:
   - `rename` — "rename X to Y" / "call it X instead of Y" → `classification: feature`
   - `add-command` — "add command X" / "add subcommand X" → `classification: feature`
   - `add-feed` — "add feed <url>" / enumerated URLs the user wants added (one finding per URL) → `classification: feature`
   - `add-endpoint` — "add endpoint <url>" / explicit API path → `classification: feature`
   - `fix-bug` — "fix X" / "X is broken" / "X returns null" → `classification: bug`
   - `sniff` — "sniff for new APIs" / "find new endpoints" / "discover more" → routes to the sniff subroutine in `### 1b.i`

4. **Each finding records the user's verbatim phrasing** in `evidence` so the U4 scope confirmation modal shows the user what they actually wrote.

5. **Edge cases** — multi-CLI asks split into two separate runs (out of scope for v0.2; ask the user to pick one). Ambiguous verbs (`update X` without specifics) trigger an `AskUserQuestion` clarification rather than a guess.

Each finding emitted by 1b carries `provenance: user-ask` (or `provenance: sniff` for findings produced by the sniff subroutine). Output flows into Phase 2 as the same structured finding list shape used by 1a.

### 1b.i. Sniff-finding subroutine

Triggered when the parsing rubric tags any 1b ask as `kind: sniff` (phrases like "sniff for new APIs", "find new endpoints", "discover more endpoints in <site>"). Sniff is opt-in per run — never invoked unless the user named it. Skip this subroutine entirely when no sniff finding is present.

**Step 1 — Resolve the target source URL.** Read the target CLI's published manifest at `~/printing-press-library/library/<category>/<slug>/.printing-press.json` and extract `source_url` (or `spec_url` as fallback). Category was resolved in 1b step 2.

If neither field is set, ask the user inline:

> "Sniff needs a target URL — paste the source site you want sniffed, or skip the sniff finding for this run?"

If the user skips, drop the sniff finding from the active list and continue with the other 1b findings. If the user pastes a URL, use it for steps 2-3.

**Step 2 — Run crowd-sniff first (fast, no browser).** Replace `<PRINTING_PRESS_BIN>` with the absolute path captured at setup:

```bash
<PRINTING_PRESS_BIN> crowd-sniff --site "$SOURCE_URL" --json > /tmp/amend-sniff-crowd.json
crowd_exit=$?
```

`crowd-sniff` queries npm SDKs and GitHub code search to discover candidate endpoints — no browser required. Typical runtime is under a minute.

**Step 3 — Optional browser-sniff (only when the user opted in deeper).** When the user's ask explicitly named browser-based discovery ("sniff with browser", "do a deep sniff") AND a captured HAR is already available, run:

```bash
<PRINTING_PRESS_BIN> browser-sniff --har "$HAR_PATH" --json > /tmp/amend-sniff-browser.json
browser_exit=$?
```

This skill does not orchestrate HAR capture itself in v0.2 — capture is user-driven (the user opens the source site in Chrome, exports the HAR, and points the skill at it) or the deep sniff is skipped with a note. v0.3 may extend the skill to drive capture via the claude-in-chrome MCP; out of scope for v0.2.

**Step 4 — Convert discoveries to findings.** For each candidate endpoint in the sniff output, append one finding to the 1b finding list:

- `id: F<n>` (next available number after the parsed asks)
- `kind: add-endpoint`
- `classification: feature`
- `evidence: "discovered via crowd-sniff: <endpoint-path>"` (or `browser-sniff` when applicable)
- `target_cli: <slug>-pp-cli`
- `rationale: <one-line summary from sniff output if available, otherwise "sniff candidate, user to confirm">`
- `provenance: sniff`

Tier these as Tier 3 (polish/architecture) at Phase 3 by default — the user reviews and can promote individual entries to Tier 2 if they're high-priority.

**Step 5 — Degraded paths.**

| Condition | Behavior |
|-----------|----------|
| `.printing-press.json` lacks `source_url` AND user skips when asked | Drop sniff finding; continue with other 1b findings; log "sniff skipped — no source URL". |
| `crowd-sniff` exits non-zero | Log the error; skip sniff findings; continue with other 1b findings. Do NOT abort the amend run. |
| `crowd-sniff` returns zero candidate endpoints | Emit one entry to the deferred-findings list ("sniff ran, no new endpoints discovered") rather than adding nothing — gives the user a record. |
| Browser-sniff requested but no HAR available | Log; fall back to crowd-sniff results only. |

**Step 6 — Surface provenance to the user.** At the Phase 3 scope-confirmation modal, sniff-derived findings are visually grouped under a `(sniff)` provenance tag so the user can decide whether to keep them as a group, e.g.:

```
Tier 3 — Polish / architecture (5)
  F8  add-endpoint /v1/feeds/stars (sniff)
  F9  add-endpoint /v1/feeds/new (sniff)
  F10 add-endpoint /v1/feeds/activity (sniff)
  ...
```



## Phase 2 — Pre-Checkpoint Guards

Two guards run before the user sees the scope menu. Either can suppress findings or abort the run.

### 2a. PR cross-reference (suppress duplicate proposals)

For each finding from Phase 1, search open + recently-merged PRs in `mvanhorn/printing-press-library` for matches. The duplicate-detection criteria (in priority order): (1) the target CLI's directory path overlaps the PR's changed-file list, (2) keywords from the finding's category + rationale match the PR title or body.

```bash
# Replace <PRINTING_PRESS_BIN> use with the absolute path captured at setup.
# This phase uses gh, not the press binary.

# Open PRs touching this CLI
gh pr list --repo mvanhorn/printing-press-library \
  --search "in:title,body <slug>" --state open --limit 20 \
  --json number,title,state,headRefName,files

# Recently merged PRs (last 90 days) touching this CLI.
# Compute "90 days ago" portably — `date -v-90d` is BSD/macOS only, `date -d`
# is GNU/Linux only. Try GNU first, fall back to BSD, then to python3. If
# every form fails, abort with an explicit error rather than letting the
# dedup guard silently drop out with an empty `merged:>` qualifier.
ninety_days_ago=$(date -u -d '90 days ago' +%Y-%m-%d 2>/dev/null \
  || date -u -v-90d +%Y-%m-%d 2>/dev/null \
  || python3 -c 'import datetime; print((datetime.datetime.now(datetime.UTC).date() - datetime.timedelta(days=90)).isoformat())' 2>/dev/null)
if [ -z "$ninety_days_ago" ]; then
  echo "ERROR: cannot compute 90-days-ago date — no GNU date, BSD date, or python3 available."
  exit 1
fi

gh pr list --repo mvanhorn/printing-press-library \
  --search "in:title,body <slug> merged:>$ninety_days_ago" \
  --state merged --limit 20 \
  --json number,title,state,mergedAt,headRefName,files
```

For each finding with a possible-duplicate match, present inline:

> "Finding `F<n>` (`<category>`) may already be addressed by PR #<num> — `<title>` (<state>, <date>). Skip this finding?"

User options: skip (drops to deferred), keep, or "show me PR #<num>" (opens `gh pr view <num> --repo mvanhorn/printing-press-library --web`). The default for clearly-merged matches is "skip"; for open PRs, default is "keep" (the user may want to add to the in-flight PR rather than open a new one).

This guard catches the canonical failure mode from the 2026-05-15 dogfood: proposing auto-refresh for a Printing Press CLI when a similar PR had already shipped on a sibling CLI a few hours earlier. The cost of a false skip is low (the user can re-add via custom selection at U4); the cost of a false-negative duplicate is a rejected PR + reviewer time.

### 2b. Stale-binary check (abort if the dogfooded binary lags published)

Read the public library's `.printing-press.json` for the target CLI to find the published version. Compare to what the local printed CLI binary reports.

```bash
# Read published version (managed clone if available, else gh api)
if [ -f "$HOME/printing-press-library/library/<category>/<slug>/.printing-press.json" ]; then
  published=$(jq -r '.version // empty' "$HOME/printing-press-library/library/<category>/<slug>/.printing-press.json")
else
  published=$(gh api repos/mvanhorn/printing-press-library/contents/library/<category>/<slug>/.printing-press.json \
    --jq '.content' | base64 -d | jq -r '.version // empty')
fi

# Read local binary version (if installed; the user dogfooded with this binary)
local_ver=$(<slug>-pp-cli version --json 2>/dev/null | jq -r '.version // empty' || echo "")
```

If `local_ver` is older than `published` (semver comparison), abort cleanly:

> "The `<slug>-pp-cli` binary you dogfooded is v`<local_ver>`, but the published library version is v`<published>`. The friction you hit may already be fixed in the published version. Run:
>
>     go install github.com/mvanhorn/<slug>-pp-cli@latest
>
> ...then re-run `/printing-press-amend` after re-dogfooding. Aborting this run."

Edge cases: if `.printing-press.json` is missing or has no `version` field, skip the stale check with a note. If the CLI is local-only (not yet published), skip the check.

### Output

Phase 2 emits the (possibly trimmed) finding list to Phase 3:

```yaml
findings_kept:
  - <finding from Phase 1>
findings_suppressed:
  - id: F3
    reason: "Duplicate of PR #571 (merged 2026-05-13)"
target_binary_check: { local: "1.0.0", published: "1.0.0", status: "current" }
```

## Phase 3 — Scope Confirmation Checkpoint (User-in-Loop #1)

This is the first of two user checkpoints. Everything until now has been read-only discovery; this checkpoint commits scope.

### Tier the surviving findings

Group findings into three tiers:

- **Tier 1 — Bugs** — every finding with `classification: bug`. CLI behavior is wrong; fixes restore correctness.
- **Tier 2 — Missing features that solve immediate session pain** — `classification: feature` findings tied to a hand-rolled workaround the user actually built during the session (i.e. the user clearly needed it now, not theoretically).
- **Tier 3 — Polish / architecture** — remaining `classification: feature` findings that are nice-to-have or architectural improvements without an immediate workaround in the session.

Display the tiered list inline before the question:

```
Friction found for <slug>-pp-cli (12 signals, 2 suppressed as duplicates):

Tier 1 — Bugs (4)
  F1  drafts list returns 400 silently
  F4  messages query returns data: null
  F7  refresh-token expiry not surfaced in errors
  F11 ai --query returns code 500

Tier 2 — Missing features that solve session pain (4)
  F2  no `drafts new` command (user hand-rolled writeMessage payload)
  F5  no `--type sent` for threads list (user worked around with messages query)
  F8  no `--remind-in <duration>` flag for send (user manually re-flagged drafts)
  F10 no `bootstrap` to local SQLite (user did 50+ thread API calls)

Tier 3 — Polish / architecture (2)
  F12 `auth status` doesn't link to `auth login` when refresh expired
  F13 doctor doesn't surface stale-binary warning vs. published version
```

### Pick scope via AskUserQuestion

```
Which scope should this patch cover?
  1. Bugs only (Tier 1) — 4 findings
  2. Bugs + immediate features (Tier 1 + Tier 2) — 8 findings
  3. All tiers (Tier 1 + Tier 2 + Tier 3) — 10 findings
  4. Custom selection — pick individual findings
```

The `AskUserQuestion` options must be self-contained (each label must convey what it does without relying on description text — some harnesses hide the description).

For the **custom selection** path: present a multi-select with each finding's id + category + one-line rationale; confirm the user-checked subset before proceeding.

### Persist the excluded findings

For every finding NOT in the confirmed scope, append to a deferred-list markdown file at:

```
$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-deferred.md
```

The `<run-id>` is a fresh timestamped id for this amend run (e.g. `amend-2026-05-15T1432`). Format the deferred file as a YAML preamble + a finding-per-section markdown body so a future `/printing-press-amend` run on the same CLI can re-surface the items.

```yaml
---
date: 2026-05-15
target_cli: superhuman-pp-cli
amend_run_id: amend-2026-05-15T1432
deferred_count: 2
---
```

Then one section per deferred finding with: id, category, classification, rationale, evidence, reason-deferred (e.g. "user picked Tier 1 only"), and `still_relevant: unknown`.

On a subsequent `/printing-press-amend` run, Phase 3 should look in `$PRESS_MANUSCRIPTS/<api-slug>/` for the most-recent `*-deferred.md` and offer the user the option to include any items still relevant in this run's scope. (Implementation note: this re-surfacing logic ships in v0.1; do not silently re-add — always present and confirm.)

### Edge case: nothing to do

If Phase 2 suppressed every finding (everything was a duplicate), Phase 3 reports cleanly and exits without opening the menu:

> "All findings from this session were addressed by existing PRs. No novel patches found."

### Output

Phase 3 emits to Phase 4:

```yaml
scope_tier: bugs+features            # or bugs|all|custom
findings_active: [...]               # the user-confirmed subset
findings_deferred_path: <path>       # where the deferred file landed
```

## Phase 4 — Plan + Execute + Validate (Autonomous)

This phase runs unattended between checkpoints 1 and 2. The user does not see fix-by-fix details; they review the final diff at the Phase 6 PR-draft checkpoint.

### Step 1 — Set up the managed clone

Per the Pre-Implementation Decision in the plan: this skill operates DIRECTLY on the managed clone of `mvanhorn/printing-press-library` rather than on `$PRESS_LIBRARY/<slug>/`. The managed clone is at:

```
$PRESS_HOME/.publish-repo-$PRESS_SCOPE
```

This is the same clone `/printing-press-publish` uses (Step 5 of that skill). Reuse it:

```bash
PUBLISH_REPO_DIR="$PRESS_HOME/.publish-repo-$PRESS_SCOPE"
PUBLISH_CONFIG="$PRESS_HOME/.publish-config-$PRESS_SCOPE.json"

if [ ! -d "$PUBLISH_REPO_DIR/.git" ]; then
  # First-time setup: see references/library-pr-plumbing.md for the full
  # detection (push-vs-fork access via gh api .../permissions.push,
  # SSH-vs-HTTPS protocol detection, scoped-clone cleanup loop).
  echo "Managed clone not present — bootstrapping..."
  # ... (see library-pr-plumbing.md)
else
  # Refresh from upstream. -f on checkout discards any local edits left behind
  # by a prior run that aborted between Phase 4's edits and Phase 7's commit —
  # without -f, those uncommitted changes block the checkout and the subsequent
  # reset --hard never runs.
  cd "$PUBLISH_REPO_DIR"
  git fetch upstream main
  git checkout -f main
  git reset --hard upstream/main
fi
```

The CLI's directory inside the managed clone is `$PUBLISH_REPO_DIR/library/<category>/<slug>/`. The category was resolved in Phase 1 (or look it up with `find "$PUBLISH_REPO_DIR/library" -maxdepth 2 -name "<slug>" -type d`).

```bash
CLI_DIR="$PUBLISH_REPO_DIR/library/<category>/<slug>"
```

All edits in this phase happen INSIDE `$CLI_DIR`. Never touch `$PRESS_LIBRARY/<slug>/` — that's a different working copy and editing it would not flow to the PR.

### Step 2 — Write the per-run plan doc

Before editing code, materialize a plan markdown at:

```
$PRESS_MANUSCRIPTS/<slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>.md
```

Mirror to `/tmp/printing-press/amend/` for quick reference. The plan doc carries:

- Frontmatter: `date`, `target_cli`, `amend_run_id`, `scope_tier`, `findings_count`
- One section per active finding: id, category, classification, rationale, target files (`$CLI_DIR/...` paths), expected behavior change, test scenarios for this finding
- Risks and dependencies between findings (if any)

The plan is decision-shape, not execution-shape — implementer-time sequencing happens during Step 3.

### Step 3 — Execute the plan (with the patch contract)

For each finding in dependency order:

1. Edit the target files under `$CLI_DIR/`. Honor AGENTS.md anti-reimplementation rules (no hand-rolled response builders; novel commands must call the real endpoint or read from the local store via `// pp:client-call` / `// pp:novel-static-reference` opt-outs only when truly justified).

2. Add a `// PATCH(<short reason>)` source comment at every changed site. Format examples:

   ```go
   // PATCH(amend-2026-05-15: surface refresh-token expiry to user) — was silently retrying
   func (c *Client) Refresh(ctx context.Context) error {
       ...
   }
   ```

3. Update `$CLI_DIR/.printing-press-patches.json`. Append an entry under `patches[]`:

   ```json
   {
     "date": "2026-05-15",
     "amend_run_id": "amend-2026-05-15T1432",
     "summary": "fix(superhuman): surface refresh-token expiry; add drafts new + --type sent",
     "files": [
       "internal/auth/refresh.go",
       "internal/cli/drafts.go",
       "internal/cli/threads.go"
     ],
     "findings_addressed": ["F1", "F2", "F5", "F7"],
     "patch_count": <total // PATCH comments added in this run>
   }
   ```

   For a temporary patch with a future supersession path, include the upstream handoff fields in that same patch entry:

   ```json
   {
     "deferred_to_upstream": [
       {
         "feature": "Generator or upstream API capability this printed-CLI patch should eventually supersede",
         "reason": "Why the local patch is intentionally temporary or API-specific."
       }
     ],
     "upstream_issue": "https://github.com/mvanhorn/cli-printing-press/issues/<n>"
   }
   ```

   Both halves of the contract — `// PATCH(...)` source comments AND `.printing-press-patches.json` entries — are MANDATORY. The library's `verify-library-conventions` workflow rejects PRs where one is present without the other. See `~/printing-press-library/AGENTS.md` "How to record a hand-edit" for the authoritative spec.

   Use `deferred_to_upstream` only when the patch intentionally leaves a future supersession path: a public API endpoint is missing today, the command relies on an unofficial host or alternate auth source, a live response shape drifted from generator assumptions, or the fix would become unnecessary once the Printing Press learns the pattern. In those cases, search `mvanhorn/cli-printing-press` issues first; reuse a matching issue or open one before the library PR, then set `upstream_issue` to that URL. Do not leave a machine-level or API-publication dependency only in the PR body.

4. **Machine-vs-printed-CLI judgment** (per AGENTS.md): when a finding's fix would generalize to every printed CLI (e.g. "the generator should emit `--type sent` for any threads list command"), surface as a borderline case:

   > "Finding F5 (`--type sent` missing) looks like a machine-level fix — the generator template `internal/generator/templates/threads.go.tmpl` should emit it for every CLI with this endpoint shape, not just `<slug>-pp-cli`. Defer to a `/printing-press-retro` follow-up, or proceed CLI-specific?"

   When deferred, drop into the deferred-list with classification `machine-level`. When kept because the printed CLI needs a narrow fix now, and the patch still carries a future supersession path, create or reuse the upstream Printing Press issue before opening the library PR, add the issue URL to `.printing-press-patches.json`, and add a `deferred_to_upstream` item naming the machine-level or upstream-API condition that should supersede the local patch.

### Step 4 — Validate

After all edits land, run the consolidated validator (replace `<PRINTING_PRESS_BIN>` with the absolute path captured at setup):

```bash
<PRINTING_PRESS_BIN> publish validate --dir "$CLI_DIR" --json > /tmp/amend-validate.json
exit_code=$?
```

`publish validate` runs manifest, phase5, govulncheck (scoped to this CLI's module), `go vet`, `go build`, `--help`, `--version`. Exit 0 = clean.

### Step 5 — Retry on failure (up to 3 iterations)

If `publish validate` reports failures, parse the error categories from the JSON, attempt targeted fixes, re-run validate. Maximum 3 iterations total. After iteration 3:

```bash
# Save the in-progress plan + diff to a holding location
HELD_PATH="$PRESS_MANUSCRIPTS/<slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-INCOMPLETE.md"
git -C "$PUBLISH_REPO_DIR" diff > "${HELD_PATH%.md}.diff"
cp "$PLAN_PATH" "$HELD_PATH"
```

Surface the final error log to the user, do NOT auto-open the PR, exit. The user can resume by re-invoking the skill (Phase 1 detects the held plan and offers to resume).

### Step 6 — Caveat: validate doesn't enforce the patch contract

`publish validate` does NOT check `.printing-press-patches.json` ↔ `// PATCH(...)` parity. That contract is enforced by the public library's `verify-library-conventions` workflow only after the PR opens. To catch it locally, run a quick parity check before proceeding to Phase 5:

```bash
# Count only NEW // PATCH(...) markers added in this run by diffing against
# upstream. A naive `grep -rc` over $CLI_DIR also counts markers added by
# prior amend runs, which lets a zero-new-markers run pass when prior history
# makes the cumulative count meet or exceed the per-run declared count.
#
# This check runs in Phase 4 Step 6 — BEFORE the Phase 7 commit. The edits
# are still in the working tree, not in any commit, so `git diff` against
# upstream/main (working-tree diff) is the right tool. `format-patch
# upstream/main..HEAD` would scan committed history only, find no commits,
# and emit nothing — silently returning 0 markers for every valid run.
#
# --no-pager + --no-color + --no-ext-diff defeats colorized output and any
# configured `diff.external` tool that would reformat the diff away from
# unified-diff shape and break the grep parse.
new_patch_markers=$(git -C "$PUBLISH_REPO_DIR" --no-pager diff --no-color --no-ext-diff upstream/main -- "$CLI_DIR" \
  | grep -cE '^\+.*// PATCH\(')
patches_entry=$(jq '.patches[-1].patch_count // 0' "$CLI_DIR/.printing-press-patches.json")
if [ "$new_patch_markers" -lt "$patches_entry" ]; then
  echo "ERROR: .printing-press-patches.json claims $patches_entry patch markers added this run, found $new_patch_markers new // PATCH(...) comments in the diff."
  exit 1
fi
```

Mismatched contract → fix locally before continuing. (A follow-up retro item: lift this check into `cli-printing-press publish validate` so future amend runs catch it natively.)

### Output

Phase 4 emits to Phase 5:

```yaml
plan_doc_path: <path>
managed_clone_dir: <path>
cli_dir_in_clone: <path>
findings_addressed: [...]
build_status: PASS|FAIL
test_status: PASS|FAIL
dogfood_status: PASS|FAIL|N/A    # PASS|FAIL when MODE=dogfood (or "both"); always N/A when MODE=direct
validate_iterations: <n>
patch_marker_count: <n>
```

**`dogfood_status` per mode.** When `MODE=dogfood`, the value reflects the result of the dogfood validation step that consumed the transcript-derived findings (PASS if the run produced a clean fix, FAIL if it surfaced a regression). When `MODE=direct`, there is no transcript to dogfood against — set `dogfood_status=N/A`. When `MODE=both`, dogfood validation still runs against the transcript half of the findings; set PASS/FAIL accordingly. This default must be set at the latest by the end of Phase 4 so Phase 7's PR body and Phase 8's RESULT block never emit an empty value.

## Phase 5 — PII Scrub

Read `references/pii-scrubbing.md` for the full procedure. Summary:

The scrub has three layers, each operating on temp staging copies (NOT on the user's session transcript or the in-progress source code):

1. **Credentials** — reuse the regex patterns from `skills/printing-press-retro/references/secret-scrubbing.md` (Stripe, GitHub PATs, bearer tokens, AWS keys, etc.) plus amend-specific additions for `Authorization`/`Cookie`/`X-API-Key` headers in hand-rolled API payloads quoted from the session transcript.
2. **Entities** — companies, people, emails matched against the user-maintained stop-list at `~/.printing-press/amend-config.yaml`. Replace with shape-preserving tokens (`<company-1>`, `<person-1>`, `<email-1>`) that maintain identity across the artifact set so reviewers can still parse intent.
3. **First-mention defense** — walk each artifact for capitalized phrases that look like proper nouns and were NOT in the stop-list. Surface to the user inline before the Phase 6 PR-draft display: "Found `Esper Labs` (3x in plan doc, 1x in PR body) — add to stop-list and scrub, or accept?"

Targets, in priority order: PR title/body draft, per-run plan doc, deferred-findings list, any test fixtures or example outputs newly added to `$CLI_DIR`. For each target, copy to `<path>.pre-pii-scrub` BEFORE scrubbing so the user can audit what was changed.

**Defense-in-depth**: walk every `*.go` file in `$CLI_DIR` for stop-list matches. If any match is found, treat as BLOCKING — pause and require user resolution before Phase 6. The agent should never have introduced PII into Go source; this check exists to catch agent error.

**Stop-list creation**: if `~/.printing-press/amend-config.yaml` doesn't exist, the skill creates a default with a starter list and a comment explaining the format. File-mode validation (warn on world-writable, abort on alien-owned).

The scrub report is written to `$PRESS_MANUSCRIPTS/<slug>/<run-id>/scrub-report.json` (NOT committed; for the user's audit). The user-facing summary at the end of the phase: "X tokens replaced across Y artifacts."

## Phase 6 — PR Draft Review Checkpoint (User-in-Loop #2)

This is the second and final user checkpoint. Everything that follows is unattended (push + PR-open + labels + RESULT block). Show the user EVERYTHING that's about to ship before any `gh` command fires.

### Assemble the draft

Compose the PR title, body, labels, and diff summary in memory. Title format follows the public library convention:

- `fix(<api-slug>): <one-line summary>` when the scope is bugs-only
- `feat(<api-slug>): <one-line summary>` when the scope includes features
- `feat(<api-slug>): <one-line summary>` when mixed (feature wins because it's the bigger contract change)

The `<one-line summary>` is composed from the most important 1-3 findings (e.g. `surface refresh-token expiry; add drafts new + --type sent`).

PR body sections (per origin R27):

1. **Summary** — 1-3 sentences naming the user pain and the shape of the fix
2. **Findings** — table with ID, category, type (bug/feature), rationale
3. **Changes** — output of `git diff --stat upstream/main..HEAD`
4. **Verification** — build/test/dogfood/validate status from Phase 4
5. **Evidence** — full GitHub URLs to the per-run plan doc and `.printing-press-patches.json` at the PR's HEAD SHA (captured AFTER push so links don't 404)
6. **Closes #N** footer when an issue match was found in Step 6 of `library-pr-plumbing.md`

Labels: `comp:<api-slug>` always; `priority:P1` for bugs-only scope, `priority:P2` for bugs+features, `priority:P3` for all-tiers.

### Display before gh fires

Show the user the title, body, label list, and `git diff --stat`. If Phase 5 surfaced unrecognized capitalized phrases that the user accepted as legitimate, RE-DISPLAY those inline now with the sentence each appears in:

> "Reminder: PR body references `<phrase>` (you accepted as legitimate during Phase 5). Confirm before opening."

### AskUserQuestion: open / edit / hold / abort

```
PR draft ready. What now?
  1. Open PR as drafted (recommended)
  2. Edit then open — drop into an interactive review of title/body
  3. Hold — save plan + diff for later resume; nothing pushed
  4. Abort — discard everything, no record kept
```

For **edit then open**: present the title and body as separate editable blocks, accept the user's revisions, re-display the full draft, confirm before proceeding.

For **hold**: save the plan + diff to `$PRESS_MANUSCRIPTS/<slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-HELD.md` and `${path%.md}.diff`. Emit a RESULT block with `status: held` and the resume path. A future `/printing-press-amend` run can detect held files and offer to resume.

For **abort**: emit a brief confirmation. Plan doc from U5 stays (with `status: aborted` written into the frontmatter) so the user has a record of what was found, but nothing else is preserved. Managed clone is reset on next run.

## Phase 7 — PR Open (Autonomous)

If the user picked open or edit-then-open, run `references/library-pr-plumbing.md` Steps 5-7:

1. **Step 5** — `git add "$CLI_DIR"` + commit with conventional message + the findings list
2. **Step 6** — search for an existing issue matching the findings; link or open new; self-assign best-effort
3. **Step 7** — push the branch (push-vs-fork access mode determined in Step 1), `gh pr create` with `--body-file`, capture HEAD_SHA, apply labels

The fork/access detection, branch collision handling, and managed-clone refresh patterns are documented in detail in `references/library-pr-plumbing.md`. Do NOT inline those patterns here — the reference is the authoritative source.

After the PR opens, surface the URL + Greptile note in the user-facing summary:

> "PR open: <url>
>
> Greptile will review within ~2 minutes. Check inline comments:
>
>     gh api repos/mvanhorn/printing-press-library/pulls/<N>/comments
>
> P0/P1 findings are worth addressing before requesting human review."

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
