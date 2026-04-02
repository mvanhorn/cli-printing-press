# Codex Delegation

> **When to read:** This file is referenced by Phase 3 and Phase 4 of the printing-press skill.
> Read it when `CODEX_MODE` is true to delegate code-writing and bug-fix tasks to Codex CLI.

## Phase 3: Codex Delegation

When `CODEX_MODE` is true, delegate code-writing tasks to Codex CLI. Claude still decides WHAT to build and in what order. Codex does the hands — writing Go functions.

**Delegation loop for each priority task:**

1. **Decompose** the current priority level into discrete tasks (one per command/feature from the absorb manifest).

2. **For each task**, follow this delegation cycle:

   a. **Read context** — Read the relevant source files from the generated CLI to extract actual code for the prompt. Use `head -50`, `grep -A 20`, or `cat` to get real code, not descriptions.

   b. **Snapshot** — Create a clean restore point before Codex writes anything:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && git add -A && git stash push -m "pre-codex-task"
   ```

   c. **Assemble prompt** — Build a CODEX_PROMPT using the appropriate task type template (see below).

   d. **Delegate** — Pipe to Codex:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && echo "$CODEX_PROMPT" | codex exec \
     --yolo \
     -c 'model_reasoning_effort="medium"' \
     -m "gpt-5.4" \
     -
   ```

   e. **Validate** — Check the result:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && go build ./... && go vet ./...
   ```
   Also verify `git diff --stat` shows a non-empty diff.

   f. **On success** — Discard the restore point and reset the failure counter:
   ```bash
   git stash drop 2>/dev/null
   ```
   Set `CODEX_CONSECUTIVE_FAILURES=0`.

   g. **On failure** (build fails, vet fails, empty diff, or Codex error) — Revert and fall back:
   ```bash
   git checkout -- . && git stash pop 2>/dev/null
   ```
   Increment `CODEX_CONSECUTIVE_FAILURES`. Claude implements this task directly (standard non-codex path).

   h. **Circuit breaker** — If `CODEX_CONSECUTIVE_FAILURES` reaches 3:
   ```bash
   echo "Codex disabled after 3 consecutive failures — completing in standard mode."
   CODEX_MODE=false
   ```
   All remaining tasks in Phase 3 (and Phase 4) use Claude directly.

3. **After each priority level**, run the same quality checks as non-codex mode (e.g., Priority 1 Review Gate).

**Task type prompt templates:**

All templates follow this structure. Paste ACTUAL CODE in the CURRENT CODE section — never descriptions of code.

**Store table task:**
```
TASK: Add <entity> table with Upsert and Search methods to the SQLite store.

FILES TO MODIFY:
- internal/store/store.go

CURRENT CODE (existing table pattern):
$(grep -A 30 "CREATE TABLE IF NOT EXISTS" internal/store/store.go | head -40)

EXPECTED CHANGE:
Create a new table for <entity> with columns: <fields from spec>.
Add Upsert<Entity>(ctx, item) and Search<Entity>(ctx, query) methods following the existing pattern.
Add FTS5 virtual table if entity has searchable text fields.

CONVENTIONS:
- Package: store
- Use the same CreateTable/Upsert/Search pattern as existing tables
- Error handling: return fmt.Errorf("upsert <entity>: %w", err)
- All table names are snake_case

CONSTRAINTS:
- Do NOT run git commit, git push, or git add
- Do NOT modify files outside internal/store/store.go
- Keep changes under 200 lines
- Run: go build ./... && go vet ./...

VERIFY: After making changes, run:
  cd . && go build ./... && go vet ./...
```

**Workflow command task:**
```
TASK: Create the <command> subcommand for <api>-pp-cli.

FILES TO MODIFY:
- internal/cli/<command>.go (create new)

CURRENT CODE (cobra command pattern from an existing command):
$(cat internal/cli/<existing-command>.go | head -60)

CURRENT CODE (root command registration):
$(grep -A 5 "AddCommand" internal/cli/root.go)

EXPECTED CHANGE:
Create a <command> command that:
<plain English description of what the command does, from the absorb manifest>

Must support: --json, --select, --compact, --limit, --dry-run (for mutations).
Must have realistic --help examples with domain-specific values.

CONVENTIONS:
- Package: cli
- Use cobra.Command pattern matching existing commands
- Error handling: return fmt.Errorf with context
- Progress output: fmt.Fprintf(os.Stderr, ...)
- Register with rootCmd.AddCommand in root.go

CONSTRAINTS:
- Do NOT run git commit, git push, or git add
- Do NOT modify files outside internal/cli/<command>.go and internal/cli/root.go
- Keep changes under 200 lines per file
- Run: go build ./... && go vet ./...

VERIFY: After making changes, run:
  cd . && go build ./... && go vet ./...
```

**Transcendence command task:**
```
TASK: Create the <command> transcendence command — a compound query across local SQLite data.

FILES TO MODIFY:
- internal/cli/<command>.go (create new)

CURRENT CODE (available store methods):
$(grep -E "^func \(db \*DB\)" internal/store/store.go | head -20)

CURRENT CODE (cobra pattern):
$(cat internal/cli/<existing-command>.go | head -40)

EXPECTED CHANGE:
Create a <command> command that:
<plain English description — what entities it joins, what insight it produces>

This command ONLY works because all data is in local SQLite.
Must support: --json, --select, --compact, --limit.

CONVENTIONS:
- Package: cli
- Query across tables using db methods, not raw SQL in CLI layer
- Format output as a table by default, JSON with --json

CONSTRAINTS:
- Do NOT run git commit, git push, or git add
- Do NOT modify files outside internal/cli/<command>.go and internal/cli/root.go
- Keep changes under 200 lines per file
- Run: go build ./... && go vet ./...

VERIFY: After making changes, run:
  cd . && go build ./... && go vet ./...
```

## Phase 4: Codex Delegation (Fixes)

When `CODEX_MODE` is true, delegate each bug fix to Codex. The shipcheck tools themselves (dogfood, verify, scorecard) always run on Claude — they are Go binary executions. Only the CODE FIXES are delegated.

**For each bug identified from dogfood/verify/scorecard output:**

1. **Read the finding** — identify the exact file, the issue, and what needs to change.

2. **Read the code** — extract the actual broken code for context:
   ```bash
   grep -n -A 10 "<broken pattern or function name>" "$PRESS_LIBRARY/<api>-pp-cli/<file>"
   ```

3. **Snapshot** before Codex writes:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && git add -A && git stash push -m "pre-codex-fix"
   ```

4. **Assemble and delegate** using the fix prompt template:
   ```
   TASK: Fix <finding summary from dogfood/verify>.

   FILES TO MODIFY:
   - <exact file path>

   CURRENT CODE (the broken section):
   <actual code from the file — use grep -A or head/tail, not descriptions>

   BUG:
   <the dogfood/verify finding, verbatim>

   EXPECTED FIX:
   <plain English description of the correct behavior>

   CONSTRAINTS:
   - Do NOT run git commit, git push, or git add
   - Do NOT modify files outside the listed path
   - Keep changes under 50 lines
   - Run: go build ./... && go vet ./...

   VERIFY: After making changes, run:
     cd . && go build ./... && go vet ./...
   ```

   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli" && echo "$CODEX_PROMPT" | codex exec \
     --yolo \
     -c 'model_reasoning_effort="medium"' \
     -m "gpt-5.4" \
     -
   ```

5. **Validate** — same as Phase 3: `go build`, `go vet`, non-empty diff.

6. **On success** — `git stash drop`, reset `CODEX_CONSECUTIVE_FAILURES=0`.

7. **On failure** — `git checkout -- . && git stash pop`, increment `CODEX_CONSECUTIVE_FAILURES`, Claude fixes this bug directly.

8. **Circuit breaker** — shares the same counter from Phase 3. If already disabled, all fixes use Claude.
