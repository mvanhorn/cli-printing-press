# Transcript Parsing — Friction Capture for `/printing-press-amend`

**Scope:** This reference applies when `MODE=dogfood` (Phase 0 detected a session-friction invocation) or when running the dogfood half of `MODE=both`. For direct-input mode (`MODE=direct`), see `direct-input-parsing.md` instead — that mode parses the user's prompt and never touches the transcript.

This reference is loaded by Phase 1's `### 1a. Dogfood mode` sub-section of `printing-press-amend`. It defines how the agent reads a Claude Code session transcript and extracts friction signals tied to a specific printed CLI invocation.

## Where the active session transcript lives

Claude Code stores per-session transcripts as JSONL files under `~/.claude/projects/<project-dir-slug>/<session-uuid>.jsonl`. The slug is derived from the working directory path (slashes replaced with `-`).

Resolution order (use the first that resolves to a readable file):

1. **Skill argument or environment** — if the user passed an explicit transcript path, use it.
2. **Active session via working dir** — derive `<project-dir-slug>` from `pwd -P` (replace `/` with `-`, strip leading `-`), then list `~/.claude/projects/<slug>/*.jsonl` and pick the most-recently-modified. This is the heuristic; it can be wrong when multiple Claude Code panes are running in the same dir, so confirm with the user.
3. **Fallback** — list `~/.claude/projects/` directories sorted by mtime, list each one's `*.jsonl` files, pick the most-recently-modified across all. This catches the case where the user invokes `/printing-press-amend` from a different working directory than the session was started in.

After picking a candidate file, ALWAYS show the user the resolved path with `AskUserQuestion`:

> "Detected active session at `<path>` (modified <relative-time>). Mine this session for friction, or pick a different transcript?"

Options:
- Use this transcript (recommended)
- Pick a different file (drops into a list of recent JSONL files under `~/.claude/projects/`)
- Cancel

The confirmation is non-optional — wrong-file selection ingests friction from the wrong session and ships PRs for bugs the user never hit.

## Signal extraction taxonomy

The transcript is line-delimited JSON; each line is a turn in the conversation. Walk the file and extract these signal categories. Each signal carries: `timestamp` (from the turn), `category` (one of below), `evidence` (the verbatim quote that triggered it), and `target_cli` (when the signal references a specific `<slug>-pp-cli` invocation).

| Category | Signal | Bug or feature? |
|---|---|---|
| **Non-zero exit code** | A `tool_result` block whose stderr indicates a non-zero exit on a `<cli>-pp-cli` invocation | Bug |
| **Error message** | Lines like `Error: ...`, `failed:`, `panic:`, `HTTP 4xx`, `HTTP 5xx` returned from the CLI | Bug (usually) |
| **Hand-rolled API payload** | Bash commands that POST/PUT directly to a URL (e.g. `curl -X POST https://api.example.com/...` or scripted JSON construction) instead of calling the CLI | Feature (the CLI doesn't expose what the user needed) |
| **Retry-after-failure** | The same command run ≥ 2 times in a row with similar args, separated by manual edits or tool tweaks | Bug or feature (look at what changed) |
| **Hand-rolled workaround comment** | Agent prose saying "X doesn't exist", "X returns 400", "I had to manually...", "going around the CLI", "no built-in for ..." | Feature when "doesn't exist", bug when "returns wrong" |
| **Missing-flag reference** | Agent text mentioning a flag it tried that the CLI didn't accept, e.g. "tried --type sent but it's rejected" | Feature (missing flag) |
| **Silent-null returns** | A CLI returns `data: null` or empty JSON when the user clearly expected content; agent commentary acknowledges the unexpected emptiness | Bug |
| **Auth confusion** | Agent text mentioning expired tokens, refresh failures, "need to re-auth", confusing `auth status` output | Bug (poor error surfacing) |

### Bug vs feature classification rubric

For each signal, choose `bug` or `feature` with a one-line rationale:

- **Bug** = the CLI behavior is *wrong* given what the CLI claims to do (broken endpoint, wrong return shape, error masked, contradicting `--help`).
- **Feature** = the CLI behavior is *missing* — the user wanted to do something the CLI doesn't expose.

When the same signal could be either (e.g. silent-null), prefer `feature` if the workaround was "construct the API call yourself" and `bug` if the workaround was "retry with different args".

The classification is the agent's best read; the user confirms or overrides at the U4 scope checkpoint.

## Auto-detect target CLI

After extracting signals, count occurrences of each `<slug>-pp-cli` mentioned. The most-touched CLI is the proposed default target. When ties exist or the top two are close (within 1 mention of each other), present a small `AskUserQuestion` with the candidates:

> "Which CLI is this patch for?"
>
> 1. `<slug-A>-pp-cli` (8 friction signals)
> 2. `<slug-B>-pp-cli` (7 friction signals)
> 3. Other (paste a CLI name)

When only one CLI was touched, default to it but still confirm:

> "Detected target: `<slug>-pp-cli` (12 friction signals). Proceed?"

When the user explicitly passed a target as the skill argument, skip auto-detect entirely and use what they passed.

### Path resolution for the chosen target

Accept any of:
- short name: `superhuman` -> slug `superhuman`
- full name: `superhuman-pp-cli` -> strip `-pp-cli`, slug `superhuman`
- absolute path: `$PRESS_LIBRARY/superhuman` -> basename slug `superhuman`

After normalizing the slug, resolve publish status by slug lookup in the public library before consulting local working-copy state:

1. If a local public-library clone exists, search `~/printing-press-library/library/*/<slug>` for a matching directory.
2. If that clone is absent or stale enough to miss the slug, query GitHub for the same public-library path. First enumerate top-level categories with `gh api repos/mvanhorn/printing-press-library/contents/library --jq '.[] | select(.type == "dir") | .name'`, then iterate those category names with `gh api repos/mvanhorn/printing-press-library/contents/library/<category>/<slug>` until the slug is found or every category has been checked.
3. If the slug is found, set `published_status: published`, set `target_category` from the matched parent directory, and route all edits through the managed clone opened later in the amend flow.
4. Only set `published_status: local-only` when the slug is absent from the public library.

Do not infer publish status from the local CLI working copy's git remotes. Printed-library CLIs can intentionally have no `origin`, and a remote-less local checkout may still correspond to a published CLI. Likewise, do not treat a missing `$PRESS_LIBRARY/<slug>` working copy as unpublished; the local-library path is only informational once the public-library slug lookup succeeds. The skill operates on the managed clone (per the Pre-Implementation Decisions in the plan), so the actual edits land in the managed clone created in U7.

## Edge cases

- **Empty / unreadable transcript** — emit "no active session transcript found at `<path>`; pass an explicit `<cli-name-or-path>` argument and re-run, or use `--transcript <path>` to point at a saved session" and exit cleanly.
- **Transcript with zero `<slug>-pp-cli` invocations** — emit "no `<slug>-pp-cli` invocations found in this session; if you dogfooded a CLI in a different session, point me at that transcript" and exit.
- **Transcript references a CLI that's not in the public library by slug** — emit a warning and ask the user to confirm; the CLI may be local-only (pre-publish) or under a different slug. This warning must be based on the public-library slug lookup, not on local git remotes or `$PRESS_LIBRARY/<slug>` presence.
- **Signal extraction returns < 2 candidates** — proceed but note in the user-facing summary that the signal yield was low; the user may want to resume after more dogfooding.

## Output shape

Phase 1 emits a structured finding list to the next phase:

```yaml
target_cli: superhuman-pp-cli
target_dir: $PRESS_LIBRARY/superhuman   # may be informational; managed-clone path resolved later
target_category: productivity                      # resolved from the public library, used by U7
published_status: published
findings:
  - id: F1
    category: missing-folder-coverage
    classification: feature
    rationale: "User tried --type sent but only inbox/draft/etc. allowed"
    evidence: "threads list --type sent → Error: invalid value for --type"
  - id: F2
    category: hand-rolled-payload
    classification: feature
    rationale: "drafts new doesn't exist; user POST'd userdata.writeMessage directly"
    evidence: "curl ... -d '{\"messageId\": ..., \"writes\": [...]}'"
  - ...
```

This list flows into U3 (cross-reference + stale-binary) and then U4 (scope confirmation).
