---
name: printing-press-polish
description: >
  Polish a generated CLI to pass verification and become publish-ready. Runs
  diagnostics (dogfood, verify, scorecard, go vet), automatically fixes all
  issues (verify failures, dead code, descriptions, README), reports the
  before/after delta, and offers to publish. Use after any /printing-press run,
  or on any CLI in ~/printing-press/library/. Trigger phrases: "polish",
  "improve the CLI", "fix verify", "make it publish-ready", "clean up the CLI",
  "get this ready to ship".
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Edit
  - Agent
  - AskUserQuestion
---

# /printing-press-polish

Polish a generated CLI so it passes verification and is ready to publish.

The retro improves the Printing Press. Polish improves the generated CLI. The actual
fix protocol lives in the `polish-worker` agent — this skill resolves the CLI,
checks locks, dispatches the agent, and offers to publish.

```bash
/printing-press-polish redfin
/printing-press-polish redfin-pp-cli
/printing-press-polish ~/printing-press/library/redfin
```

## When to run

After any `/printing-press` generation, especially when:
- The shipcheck verdict is `ship-with-gaps`
- The verify pass rate is below 80%
- The scorecard is below 85
- You want the CLI publish-ready in one pass

Can also be run standalone on any CLI in `~/printing-press/library/`.

## Setup

```bash
PRESS_HOME="$HOME/printing-press"
PRESS_LIBRARY="$PRESS_HOME/library"
```

### Resolve CLI

The argument can be:
- A short name: `redfin` (looks up `$PRESS_LIBRARY/redfin`)
- A full name: `redfin-pp-cli` (strips suffix, looks up `$PRESS_LIBRARY/redfin`)
- A path: `~/printing-press/library/redfin` (used directly)

Resolution order:
1. If the argument is an absolute or `~`-prefixed path and exists, use it
2. Try `$PRESS_LIBRARY/<arg>` (exact match — works for slug like `redfin`)
3. If arg has `-pp-cli` suffix, strip it and try `$PRESS_LIBRARY/<slug>` (e.g., `redfin-pp-cli` → `redfin`)
4. Fuzzy search: `ls $PRESS_LIBRARY/ | grep -i <arg>` for close matches

If no match or multiple matches, present via `AskUserQuestion`. Show at most 4
matches sorted by modification time (most recent first) with human-friendly
relative timestamps (e.g., "generated 2 hours ago").

```bash
CLI_DIR="<resolved path>"
CLI_NAME="$(basename "$CLI_DIR")"

# Check if there's an active build lock — polish edits would be overwritten
# when the running build promotes to library.
_lock_json=$(printing-press lock status --cli "$CLI_NAME" --json 2>/dev/null)
if echo "$_lock_json" | grep -q '"held".*true'; then
  if echo "$_lock_json" | grep -q '"stale".*true'; then
    echo "Warning: stale lock exists for $CLI_NAME (build may have crashed)."
    echo "Proceeding with polish. Run 'printing-press lock release --cli $CLI_NAME' to clear."
  else
    echo "An active build is in progress for $CLI_NAME."
    echo "Polish edits would be overwritten when the build promotes."
    echo "Wait for the build to finish, then run polish."
    exit 1
  fi
fi

# Verify it's a valid Go CLI
if [ ! -f "$CLI_DIR/go.mod" ]; then
  echo "Not a valid CLI directory: $CLI_DIR"
  exit 1
fi

echo "Polishing: $CLI_NAME"
echo "Location: $CLI_DIR"
```

### Find spec

```bash
API_SLUG="${CLI_NAME%-pp-cli}"
SPEC_PATH=""
for f in "$PRESS_HOME/manuscripts/$API_SLUG"/*/research/*.yaml "$PRESS_HOME/manuscripts/$API_SLUG"/*/research/*.json "$PRESS_HOME/manuscripts/$CLI_NAME"/*/research/*.yaml "$PRESS_HOME/manuscripts/$CLI_NAME"/*/research/*.json; do
  if [ -f "$f" ]; then
    SPEC_PATH="$f"
    break
  fi
done
```

## Polish: Dispatch the Agent

Dispatch the `polish-worker` agent to run the full diagnostic-fix-rediagnose
loop. The agent is autonomous and returns a structured result.

```
Agent(
  subagent_type: "cli-printing-press:polish-worker",
  description: "Polish CLI quality",
  prompt: "Polish this CLI.\nCLI_DIR: $CLI_DIR\nCLI_NAME: $CLI_NAME\nSPEC_PATH: $SPEC_PATH"
)
```

The agent returns a `---POLISH-RESULT---` block. Parse it and display the delta:

```
Polish Results for <CLI_NAME>:

                    Before    After     Delta
  Scorecard:        XX/100    XX/100    +N
  Verify:           XX%       XX%       +N%

Fixes applied:
  - [from fixes_applied in result]

Remaining issues:
  - [from remaining_issues in result]
```

## Publish Offer

If `scorecard_after` >= 65 and `verify_after` >= 80:

Present via `AskUserQuestion`:

> "<CLI_NAME> polished: scorecard XX/100, verify XX%. Ready to publish?"
>
> 1. **Publish now** — validate, package, and open a PR to printing-press-library
> 2. **Polish again** — run another fix pass on remaining issues
> 3. **Done for now** — CLI is at ~/printing-press/library/<cli-name>

If remaining issues exist, prepend: "Note: some issues remain (see above)."

### If "Publish now"

Check for existing PR:
```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/$CLI_NAME" --state open --author @me --json number,url --jq '.[0]' 2>/dev/null
```

Then invoke `/printing-press-publish <cli-name>`.

### If "Polish again"

Re-dispatch the `polish-worker` agent with the same arguments. Maximum 2
additional polish passes (3 total including the first).

### If "Done for now"

End normally.

## Rules

- Fix everything. Do not ask for approval before fixing — the agent handles it.
- Report results honestly. Show what improved and what didn't.
- Do not add new features. Polish fixes quality issues, not feature gaps.
- Do not re-run research or generation. Polish works with the CLI as-is.
- Do not modify the printing-press generator. That's `/printing-press-retro`.
- Maximum 3 total polish passes (initial + 2 retries).
