# Codex Delegation

> **When to read:** This file is referenced by Phase 3 and Phase 4 of the printing-press skill.
> Read it when `CODEX_MODE` is true to delegate code-writing and bug-fix tasks to Codex CLI.

**IMPORTANT:** Delegate via `echo $PROMPT | codex exec` in Bash. Do NOT use the Skill tool with `codex:codex-cli-runtime` - that skill is only for the rescue subagent, not general delegation.

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

   d. **Delegate** — Remove any stale completion marker from a prior task, then pipe to Codex:
   ```bash
   # Model and reasoning effort inherit from ~/.codex/config.toml. Do not pin -m / -c here.
   cd "$PRESS_LIBRARY/<api>-pp-cli" && rm -f _codex-result.json && echo "$CODEX_PROMPT" | codex exec \
     --yolo \
     -
   ```

   e. **Validate** — Check the completion marker first (self-describing on failure), then the build:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli"
   if [ ! -f _codex-result.json ] || [ "$(jq -r '.status // empty' _codex-result.json 2>/dev/null)" != "complete" ]; then
     echo "codex output marker missing — partial work may be present in $PWD; review before continuing" >&2
     false
   else
     go build ./... && go vet ./...
   fi
   ```
   Also verify `git diff --stat` shows a non-empty diff.

   A missing or non-complete marker means Codex exited before finishing the prompt (sandbox abort, OOM, SIGINT, internal toolchain crash); the `if` branch prints the diagnostic itself so the agent does not have to second-guess which arm failed. Fall through to step (g) as a failure. Do NOT proceed to the next priority task with whatever Codex managed to write before bailing.

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

All CONVENTIONS lists apply the empty-collection rule to every list-shaped
output: initialize empty result slices so JSON output is `[]`, not `null`.

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

COMPLETION MARKER: After VERIFY succeeds (build green, vet clean), and only then, write a JSON object to _codex-result.json in the cwd:
  {"status":"complete","files_written":["internal/store/store.go"],"timestamp":"<ISO 8601 UTC, e.g. 2026-05-16T12:00:00Z>"}
If you bail, hit a fatal error, or VERIFY fails, do NOT write this file. The parent skill uses its presence and "status":"complete" as the only signal that the prompt ran end-to-end; missing marker is treated as a partial-completion failure regardless of process exit code.
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
- EMPTY-COLLECTION CONVENTION:
  When the command's primary output is a list/array/cluster, declare it
  as `results := make([]T, 0)` (not `var results []T`). Nil-slice marshals
  to JSON `null`, which breaks `jq '.[]'` agent pipelines; an initialized
  empty slice marshals to `[]` and lets downstream consumers iterate
  uniformly across empty and non-empty results.
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

COMPLETION MARKER: After VERIFY succeeds (build green, vet clean), and only then, write a JSON object to _codex-result.json in the cwd:
  {"status":"complete","files_written":["internal/cli/<command>.go","internal/cli/root.go"],"timestamp":"<ISO 8601 UTC>"}
If you bail, hit a fatal error, or VERIFY fails, do NOT write this file. The parent skill uses its presence and "status":"complete" as the only signal that the prompt ran end-to-end; missing marker is treated as a partial-completion failure regardless of process exit code.
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
- EMPTY-COLLECTION CONVENTION:
  When the command's primary output is a list/array/cluster, declare it
  as `results := make([]T, 0)` (not `var results []T`). Nil-slice marshals
  to JSON `null`, which breaks `jq '.[]'` agent pipelines; an initialized
  empty slice marshals to `[]` and lets downstream consumers iterate
  uniformly across empty and non-empty results.
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

COMPLETION MARKER: After VERIFY succeeds (build green, vet clean), and only then, write a JSON object to _codex-result.json in the cwd:
  {"status":"complete","files_written":["internal/cli/<command>.go","internal/cli/root.go"],"timestamp":"<ISO 8601 UTC>"}
If you bail, hit a fatal error, or VERIFY fails, do NOT write this file. The parent skill uses its presence and "status":"complete" as the only signal that the prompt ran end-to-end; missing marker is treated as a partial-completion failure regardless of process exit code.
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

   COMPLETION MARKER: After VERIFY succeeds (build green, vet clean), and only then, write a JSON object to _codex-result.json in the cwd:
     {"status":"complete","files_written":["<exact file path>"],"timestamp":"<ISO 8601 UTC>"}
   If you bail, hit a fatal error, or VERIFY fails, do NOT write this file. The parent skill uses its presence and "status":"complete" as the only signal that the prompt ran end-to-end; missing marker is treated as a partial-completion failure regardless of process exit code.
   ```

   ```bash
   # Model and reasoning effort inherit from ~/.codex/config.toml. Do not pin -m / -c here.
   cd "$PRESS_LIBRARY/<api>-pp-cli" && rm -f _codex-result.json && echo "$CODEX_PROMPT" | codex exec \
     --yolo \
     -
   ```

5. **Validate** — same as Phase 3: check the completion marker first (self-describing on failure), then build and vet:
   ```bash
   cd "$PRESS_LIBRARY/<api>-pp-cli"
   if [ ! -f _codex-result.json ] || [ "$(jq -r '.status // empty' _codex-result.json 2>/dev/null)" != "complete" ]; then
     echo "codex output marker missing — partial work may be present in $PWD; review before continuing" >&2
     false
   else
     go build ./... && go vet ./...
   fi
   ```
   Also verify `git diff --stat` shows a non-empty diff. The `if` branch prints the diagnostic itself before exiting non-zero, so the agent does not have to second-guess which arm failed; fall through to step 7.

6. **On success** — `git stash drop`, reset `CODEX_CONSECUTIVE_FAILURES=0`.

7. **On failure** — `git checkout -- . && git stash pop`, increment `CODEX_CONSECUTIVE_FAILURES`, Claude fixes this bug directly.

8. **Circuit breaker** — shares the same counter from Phase 3. If already disabled, all fixes use Claude.
