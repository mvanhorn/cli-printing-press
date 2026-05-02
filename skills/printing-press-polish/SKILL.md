---
name: printing-press-polish
description: >
  Polish a generated CLI to pass verification and become publish-ready. Runs
  diagnostics (dogfood, verify, scorecard, go vet), automatically fixes all
  issues (verify failures, dead code, descriptions, README, MCP tool quality),
  reports the before/after delta, and offers to publish. Use after any
  /printing-press run, or on any CLI in ~/printing-press/library/. Trigger
  phrases: "polish", "improve the CLI", "fix verify", "make it publish-ready",
  "clean up the CLI", "get this ready to ship".
context: fork
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Edit
  - AskUserQuestion
---

# /printing-press-polish

Polish a generated CLI so it passes verification and is ready to publish.

The retro improves the Printing Press. Polish improves the generated CLI. This skill runs in a forked context (`context: fork`) so its diagnostic and fix loop doesn't pollute the caller — the diagnostic spam, fix iterations, and re-diagnose noise stay scoped to the polish session, and the caller receives a clean summary.

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

### Public-library hint

If the user's request includes phrasing like "polish notion **in the
public library**", "polish **from the public library**", or "polish the
published cal-com" — and the named CLI is **not** in
`$PRESS_LIBRARY/<slug>/` — they're asking to polish a CLI that lives
upstream but not locally. Polish runs against the internal library, so
the right move is to import first.

Suggest: `/printing-press-import <slug>` to bring it in, then re-run
polish. Don't try to polish a CLI that isn't in the internal library.

If the named CLI **is** already in `$PRESS_LIBRARY/<slug>/`, the
"public library" phrasing is informational — just proceed with polish
and let the divergence check (below) handle any drift.

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

### Find spec and research dir

```bash
API_SLUG="${CLI_NAME%-pp-cli}"
SPEC_PATH=""
for f in "$PRESS_HOME/manuscripts/$API_SLUG"/*/research/*.yaml "$PRESS_HOME/manuscripts/$API_SLUG"/*/research/*.json "$PRESS_HOME/manuscripts/$CLI_NAME"/*/research/*.yaml "$PRESS_HOME/manuscripts/$CLI_NAME"/*/research/*.json; do
  if [ -f "$f" ]; then
    SPEC_PATH="$f"
    break
  fi
done

# Build the spec flag once. Empty when no spec was found — diagnostic
# commands accept a missing --spec and degrade gracefully.
SPEC_FLAG=""
if [ -n "$SPEC_PATH" ]; then
  SPEC_FLAG="--spec $SPEC_PATH"
fi

# Locate the research dir (parent of the spec's research/ folder, i.e.
# manuscripts/<api>/<run-id>/). dogfood's --research-dir triggers
# checkNovelFeatures, which writes novel_features_built back into
# research.json AND syncs the verified list into .printing-press.json.
# Without this flag, legacy CLIs whose manifest predates the
# novel_features schema fail publish-validate's transcendence gate.
RESEARCH_DIR=""
for d in "$PRESS_HOME/manuscripts/$API_SLUG"/*/research.json "$PRESS_HOME/manuscripts/$CLI_NAME"/*/research.json; do
  if [ -f "$d" ]; then
    RESEARCH_DIR="$(dirname "$d")"
    break
  fi
done

RESEARCH_FLAG=""
if [ -n "$RESEARCH_DIR" ]; then
  RESEARCH_FLAG="--research-dir $RESEARCH_DIR"
fi
```

### Divergence check

**Stop and run this step before Phase 1. Do not skip it. Do not proceed to diagnostics until you have completed the check and resolved any divergence.**

The internal copy at `$CLI_DIR` can drift from the public library (`mvanhorn/printing-press-library`) copy if anyone edited the public repo directly after this CLI was last published. Polishing a stale internal copy and re-publishing later silently overwrites those public-only fixes — a real failure mode that shipped CLIs hit.

**You must:**

1. **Locate the public library clone.** Honor `$PRINTING_PRESS_LIBRARY_PUBLIC` if set; otherwise scan the user's filesystem however fits this platform. Validate every candidate by checking the git remote points at `mvanhorn/printing-press-library` — other directories may share the name (forks, accidental name collisions). If multiple valid clones exist, prefer the most recently modified; ask the user to disambiguate only if still unclear.
2. **Locate this CLI inside the clone.** `find <clone>/library -type d -name "<api>-pp-cli"` or equivalent.
3. **Run `diff -r <public-cli-dir> $CLI_DIR`** with these exclusions, all of which are expected to diverge after publish:
   - `.printing-press-tools-polish.json` (local ledger, not published)
   - `go.mod` and `go.sum` — publish rewrites the module path from `<api>-pp-cli` to `github.com/mvanhorn/printing-press-library/library/<category>/<api>`
   - All `.go` files where the only difference is the rewritten import path (the publish step propagates the new module path through every internal import). When inspecting `.go` diffs, scan for substantive changes — anything beyond the module-path prefix swap is real divergence.

   Concretely: `diff -r --exclude=go.mod --exclude=go.sum --exclude=.printing-press-tools-polish.json <public-cli-dir> $CLI_DIR`.

   Don't pass `--exclude='<api>-pp-cli'` or `--exclude='<api>-pp-mcp'` — those names match both the root-level binary files **and** the `cmd/<api>-pp-cli/` and `cmd/<api>-pp-mcp/` source directories. Excluding by binary name silently skips the entire `cmd/` subtree, hiding real divergence in `main.go`. The "Only in $CLI_DIR: <api>-pp-cli" line for the built binary is one row of expected output, not noise worth filtering at the cost of completeness.
4. **Surface the result** before continuing.

Outcomes:

- **No clone found** → user doesn't have public locally. State this explicitly ("public library not found locally; proceeding on internal as canonical") and continue.
- **Clone found but doesn't contain this CLI** → never published or under a different name. State this and continue.
- **Found and diff is empty** → in sync. State this and continue.
- **Found and divergent** → **stop**. Do not run Phase 1 diagnostics yet. List the divergent files for the user. Ask via AskUserQuestion: **sync public→internal**, or **proceed without syncing**. If the user picks sync, copy public's version of the divergent files into internal, then continue polish on the synced internal copy.

Before showing the sync prompt, check whether internal has files modified after its `.printing-press.json` timestamp (the user has been polishing locally without publishing). If yes, hedge the prompt explicitly: syncing will overwrite their pending local work. Let them decide whether to keep their local edits or pull public's.

After sync (or explicit skip), the rest of polish operates on `$CLI_DIR` as canonical. The eventual `/printing-press-publish` step pushes internal back to public; no second divergence check is needed there.

**The check has run only when one of the four outcomes above is explicitly stated in your response.** Silent omission counts as not having run it.

## Phase 1: Baseline diagnostics

```bash
cd "$CLI_DIR"

# Build
go build -o "$CLI_NAME" ./cmd/"$CLI_NAME" 2>&1

# Diagnostics. SPEC_FLAG and RESEARCH_FLAG are set in the "Find spec
# and research dir" step above. RESEARCH_FLAG enables dogfood to
# verify novel features and sync them into .printing-press.json
# (required for publish-validate's transcendence gate).
printing-press dogfood --dir "$CLI_DIR" $SPEC_FLAG $RESEARCH_FLAG 2>&1
printing-press verify --dir "$CLI_DIR" $SPEC_FLAG --json 2>&1
printing-press workflow-verify --dir "$CLI_DIR" --json > /tmp/polish-workflow-verify.json 2>&1 || true
printing-press verify-skill --dir "$CLI_DIR" --json > /tmp/polish-verify-skill.json 2>&1 || true
# --live-check samples novel-feature outputs and populates
# live_check.features[].warnings (Wave B entity detection) — required for
# the "Output entity warnings" row below to have data to read.
printing-press scorecard --dir "$CLI_DIR" $SPEC_FLAG --live-check --json > /tmp/polish-scorecard.json 2>&1 || true
printing-press scorecard --dir "$CLI_DIR" $SPEC_FLAG 2>&1
printing-press tools-audit "$CLI_DIR" --json > /tmp/polish-tools-audit-before.json 2>&1 || true
go vet ./... 2>&1
```

verify-skill and workflow-verify run alongside dogfood/verify/scorecard so polish catches the same class of failures the public-library CI catches. Polish hard-gates `ship` on `verify-skill` exit 0 (see ship logic at the end).

Parse findings into categories:

| Category | Source | What to look for |
|----------|--------|------------------|
| Verify failures | verify --json | Commands with score < 3 |
| SKILL static-check failures | verify-skill --json | Any `findings[]` with `severity=error` (flag-names, flag-commands, positional-args). Hard ship-gate: ship cannot fire while these exist. |
| Workflow gaps | workflow-verify --json | Verdict `workflow-fail`. Soft gate: surface in `remaining_issues` and downgrade to `hold` when the workflow is the CLI's primary value. |
| Dead code | dogfood | Dead functions, dead flags |
| Stale files | dogfood | Unregistered commands |
| Description issues | dogfood | Boilerplate root Short |
| README gaps | scorecard | README score < 8 |
| Example gaps | dogfood | Commands missing examples |
| Go vet issues | go vet | Any output |
| Output entity warnings | scorecard JSON | `live_check.features[].warnings` — raw HTML entities in human output |
| Output plausibility | Phase 4.85 | Findings from the agentic output review |
| MCP tool quality | tools-audit | Empty Short, thin Short, missing read-only annotations, thin MCP descriptions |

**Environmental failures vs. CLI defects.** Some Phase 1 outputs surface failures that aren't real CLI bugs and should not block ship:

- `scorecard --live-check` reporting `SQLITE_BUSY`, network timeouts, `401` from a mock or expired token, or HTTP errors that depend on the test workspace's permissions/state — these are test-environment issues, not CLI defects.
- `verify` mock-harness flakes on commands with binary output (e.g., `qr` returning a PNG that the substring matcher can't validate) or commands with optional positional args where dry-run output legitimately doesn't contain the verify probe string.

Classify these as environmental in `skipped_findings` with the specific reason; do not spend Phase 2 cycles trying to "fix" them. The polish skill's ship logic already excludes live-check failures from gating, but the agent should still annotate them so reviewers can see they were considered and dismissed deliberately.

### Phase 4.85 — Agentic output review (Wave B)

After the mechanical diagnostics above complete, invoke the `printing-press-output-review` sub-skill via the Skill tool. The sub-skill carries `context: fork` and owns the dispatch prompt, gate logic, and known blind spots — single source of truth shared with the main printing-press skill.

```
Skill(
  skill: "cli-printing-press:printing-press-output-review",
  args: "$CLI_DIR"
)
```

Parse the returned `---OUTPUT-REVIEW-RESULT---` block. `status: WARN` findings flow into the diagnostic categories above so Phase 2 fixes address both rule-based and plausibility issues. `status: SKIP` is informational — record but don't block.

Wave B gating applies: all findings are warnings, never blockers. Fix if obvious and cheap; document with a short comment if deferred.

Record baseline scores: scorecard total, verify pass rate, dogfood verdict, go vet issue count, output-review finding count.

## Phase 2: Fix

Fix in priority order. After each priority level, update the lock heartbeat:

```bash
printing-press lock update --cli "$CLI_NAME" --phase polish 2>/dev/null
```

### Priority 0: MCP surface migration (legacy CLIs)

If Phase 1's `dogfood` reported `MCP Surface: FAIL` with a parity mismatch, the CLI was generated before the runtime cobratree walker existed and is still on the static `internal/mcp/tools.go` surface. The fix is mechanical:

```bash
printing-press mcp-sync "$CLI_DIR"
```

That migrates the MCP surface to the runtime walker, regenerates `tools-manifest.json` and `internal/mcp/tools.go`, and applies any `mcp-descriptions.json` overrides. Re-run `dogfood` after; the parity gate flips to PASS. This is a known migration path for every CLI generated before the cobratree landed; running it on a CLI already on the runtime walker is a no-op refresh.

Skip this priority on CLIs where dogfood's MCP gate is already passing.

### Priority 1: Verify failures

For each command that fails verify dry-run or exec:

1. Read the command file
2. Find `Args: cobra.ExactArgs(N)` or similar constraint
3. Remove the `Args:` field
4. Add at the top of `RunE`:
   ```go
   if len(args) == 0 {
       return cmd.Help()
   }
   ```
5. For commands needing 2+ args, use `if len(args) < 2`
6. Check for dry-run nil-data crashes and add guards:
   ```go
   if flags.dryRun {
       return nil
   }
   ```

### Priority 2: Dead code

1. For each dead function flagged by dogfood, grep all `.go` files to verify
   it's truly unused (not just its definition matching itself)
2. If truly unused: remove the function
3. If used by another helper: leave it (false positive)
4. After removal, remove unused imports
5. Delete stale files (promoted commands not registered in root.go)

### Priority 3: CLI description and metadata

1. Read root command `Short` in `internal/cli/root.go`
2. If it contains boilerplate ("Reverse-engineered...", raw API title), rewrite:
   Pattern: `"<Product> CLI with <capability-1>, <capability-2>, and <capability-3>"`
3. Check commands for missing `Example` fields. Add realistic examples with
   domain-specific values.

### Priority 4: README

**Cardinal rule: run `<cli> <cmd> --help` for EVERY command you put in the
README.** Never guess flag names, argument formats, or valid values. If you
write `--start-time` but the flag is `--start`, the README is wrong and
users will get errors on their first try.

#### Inject novel features from research

If the README lacks a `## Unique Features` section, check whether the
manuscript archive has novel features to surface:

```bash
PRESS_HOME="$HOME/printing-press"
API_SLUG="${CLI_NAME%-pp-cli}"
RESEARCH_JSON=""
for f in "$PRESS_HOME/manuscripts/$CLI_NAME"/*/research.json \
         "$PRESS_HOME/manuscripts/$API_SLUG"/*/research.json; do
  if [ -f "$f" ]; then RESEARCH_JSON="$f"; break; fi
done
```

If `RESEARCH_JSON` exists, read it and check for a `novel_features` array.
If that array is non-empty and the README has no `## Unique Features`
heading, inject the section **after `## Quick Start`** (or before
`## Usage` if Quick Start doesn't exist).

Format each feature exactly as the generator template does:

```markdown
## Unique Features

These capabilities aren't available in any other tool for this API.

- **`<command>`** — <description>
```

Before injecting, verify each feature's `command` actually exists in the
built CLI by running `./$CLI_NAME <command> --help 2>/dev/null`. Skip any
feature whose command does not exist — it may have been renamed or dropped
during generation.

#### Required sections (must be present and correct)

1. **Title**: "# <Product Name> CLI" — use the product's real name with
   correct casing/punctuation (e.g., "Cal.com" not "Cal Com")
2. **Subtitle**: one sentence describing what the CLI does for the user,
   matching the root `Short` field. NOT a description of the API.
3. **Install**: correct install command. Use the printing-press-library
   repo URL, not a per-CLI repo that doesn't exist.
4. **Authentication**: how to set `<API>_API_KEY` env var, where to get
   a key (link to the provider's settings page), self-hosted URL override
   if supported. Read `config.go` to find all env vars.
5. **Quick Start**: 3-5 commands someone will actually run first. Pick
   commands that are both **most useful** (what you'd run daily) and
   **show the CLI's value** (why install this over curl). Usually:
   `doctor` → `sync` → transcendence command (`today`, `health`) →
   `search`. Avoid raw list commands — they dump data without
   demonstrating why the CLI exists.
6. **Commands**: categorized table. Group by domain function (Scheduling,
   Analytics, Account, Utilities), not by implementation structure.
7. **Output Formats**: show `--json`, `--select`, `--csv`, `--compact`,
   `--dry-run`, `--agent`. Use a real command, not a placeholder.
8. **Agent Usage**: agent-native properties and exit codes.
9. **Cookbook**: 8-15 recipes using **verified flag names** from `--help`.
   Show the CLI's unique capabilities: transcendence commands, filters,
   SQL queries, pipes. Include at least one mutation example.
10. **Health Check**: show actual `doctor` output, not a placeholder.
11. **Configuration**: list ALL env vars from config.go with descriptions.
    Include config file path.
12. **Troubleshooting**: common errors mapped to exit codes with fixes.

#### Optional sections (add at your discretion)

- **Rate Limits**: if the API has documented limits
- **Self-Hosting**: if the CLI supports `--api-url` or `BASE_URL` override
- **Pagination**: if the API has notable pagination behavior
- **Sources & Inspiration**: credits to community projects (generated by
  the machine, preserve if present)

### Priority 4.5: SKILL static-check failures (verify-skill)

Read `/tmp/polish-verify-skill.json` for the full finding list. Each finding has a `check` (`flag-names`, `flag-commands`, or `positional-args`), a `command` (the path the SKILL claimed), and a `detail` describing the mismatch. Common shapes and fixes:

- **`flag-names`** — SKILL references `--foo` but no command in `internal/cli/*.go` declares it. Either the example is wrong (fix the SKILL or remove the recipe) or the flag was deleted (decide if it should come back).
- **`flag-commands`** — `--foo is declared elsewhere but not on <cmd>`. The flag exists somewhere but not on the command the SKILL invoked it on. Two fixes:
  1. If the flag is added via a shared helper like `addXxxFlags(cmd, ...)`, inline the `cmd.Flags().StringVar(...)` declaration directly in the affected command's source file. The verify-skill grep cannot follow function-call indirection.
  2. If the SKILL example is genuinely wrong, fix the example to use a flag the command does declare.
- **`positional-args`** — `got N positional args; Use: "<cmd> <arg>" expects M-M`. The SKILL recipe passed N positional args but the command's `Use:` declares M required. Two fixes:
  1. If the command also accepts the value via a `--flag`, change `Use: "cmd <arg>"` to `Use: "cmd [arg]"` (square brackets = optional). Verify-skill correctly accepts `--flag`-only invocations against an optional positional.
  2. If the SKILL example is missing a required positional, fix the example.

After fixing, re-run `printing-press verify-skill --dir "$CLI_DIR"` and confirm exit 0 before moving on.

### Priority 5: Remaining dogfood issues

- Path validity mismatches
- Auth protocol mismatches
- Example drift (examples referencing wrong commands)
- Data pipeline integrity issues

### Priority 6: MCP tool quality

**Your goal now is to ensure every MCP tool exposed by this CLI carries agent-grade descriptions and correct read/write classifications.** Tool descriptions and classifications are how agents discover and decide whether to call a tool — thin descriptions and missing annotations directly degrade agent UX, and Phase 1's mechanical gates (verify, dogfood) do NOT catch this class of issue.

Stop and:

1. Run `printing-press tools-audit "$CLI_DIR" --json` to surface mechanical findings (empty Short, thin Short, missing `mcp:read-only` on read-shaped command names).
2. You must read `references/tools-polish.md` and follow its instructions to address the findings AND run a judgment pass over every command — regardless of whether the audit flagged it. The audit catches mechanical issues; description quality and borderline classification (read-only vs. local-write) always require agent reasoning. You must not skip this.
3. **Accepting MCP-description findings carries a stricter contract.** `thin-mcp-description` and `empty-mcp-description` accepts require three pre-decision fields (`spec_source_material`, `target_description`, `gap_analysis`) populated per finding. The binary rejects bulk accepts (>5 findings sharing one rationale) and runs that "complete" without lifting MCPDescriptionQuality. Fix via override or generator improvement is the expected path; accept is rare. See `references/tools-polish.md` "Marking a finding accepted" for the full contract.

Proceed to "After all fixes" only when the audit's summary line reads `no pending findings` with no `incomplete:` block — every gate (pre-decision fields, duplicate rationale, scorecard delta) passes.

### After all fixes

```bash
go build -o "$CLI_NAME" ./cmd/"$CLI_NAME"
gofmt -w .
```

## Phase 3: Re-diagnose

Re-run the diagnostic sweep on the fixed CLI:

```bash
printing-press dogfood --dir "$CLI_DIR" $SPEC_FLAG 2>&1
printing-press verify --dir "$CLI_DIR" $SPEC_FLAG --json 2>&1
printing-press workflow-verify --dir "$CLI_DIR" --json 2>&1
printing-press verify-skill --dir "$CLI_DIR" --json 2>&1
printing-press scorecard --dir "$CLI_DIR" $SPEC_FLAG 2>&1
printing-press tools-audit "$CLI_DIR" 2>&1
go vet ./... 2>&1
```

Record the after scores. If verify-skill still has any `severity=error` findings or workflow-verify still reports `workflow-fail`, ship cannot fire (see ship logic below).

## Ship logic

Compute the ship recommendation:

- **`ship`**: verify >= 80%, scorecard >= 75, no critical failures, **AND** verify-skill exits 0 (no SKILL/CLI mismatches), **AND** workflow-verify is not `workflow-fail`, **AND** tools-audit shows zero pending findings (every finding fixed or explicitly accepted with rationale). The SKILL/workflow gates are hard requirements: a CLI that ships with a SKILL that lies about it (verify-skill findings) gives agents broken instructions; a CLI whose primary workflow fails verification has not actually shipped.
- **`ship-with-gaps`**: verify >= 65%, scorecard >= 65, non-critical gaps remain, **AND** the SKILL/workflow gates above hold. Reserved for the rare case where a refactor or external-dependency blocker prevents a clean fix; the gap must be documented in the remaining issues.
- **`hold`**: verify < 65% or scorecard < 65 or critical failures, **OR** verify-skill has unresolved findings, **OR** workflow-verify reports `workflow-fail` and the workflow is the CLI's primary value.

### Push higher without gaming

The ship gates are a floor, not a ceiling. After they pass, look at scorecard dimensions still below max and ask whether each gap is real or structural:

1. **Find the underlying deficit, not the score.** The scorecard is a proxy for quality, not the goal itself. A README scoring 8/10 might be missing a Cookbook section or have outdated commands — that's a real, fixable gap. A `mcp_surface_strategy` scoring 2/10 on a 200-endpoint API might be flagging that the surface is mostly endpoint mirrors — also potentially fixable.
2. **If there's a real, agent-grade improvement available, make it.** Better description, missing flag doc, weak README section, an example that doesn't reflect actual usage. The CLI gets better and the score follows.
3. **If the deficit is structural, document and accept.** Some dimensions assume capabilities the CLI's domain doesn't have (a read-only API scored against write-workflow dimensions, a CLI with no auth scored on auth dimensions, a small API penalized on `surface_strategy` thresholds calibrated for large APIs). Note the reason in `skipped_findings` and move on.
4. **Never add scaffolding to satisfy the scorer.** Fake commands, fake tests, fake flags, or boilerplate prose written purely to nudge a number — those degrade the CLI to satisfy the proxy. The scorer is imperfect by design (the "scoring may be imperfect" caveat in AGENTS.md applies). Trust the underlying judgment, not the number.

Rule of thumb: if your fix would still be valuable if the scorecard didn't exist, do it. If the only motivation is "to push the score," don't.

## Display delta and emit result block

Display the delta to the user, then emit the structured `---POLISH-RESULT---` block. The block lets calling skills (e.g., main printing-press SKILL.md Phase 5.5) parse the recommendation and scores reliably; the human-readable table above is for the user.

```
Polish Results for <CLI_NAME>:

                    Before    After     Delta
  Scorecard:        XX/100    XX/100    +N
  Verify:           XX%       XX%       +N%
  Tools-audit:      XX        XX        -N pending findings

Fixes applied:
  - <one-line description of each fix>

Skipped findings:
  - <finding>: <why you chose not to fix it>

Remaining issues:
  - <one-line description of each issue you tried to fix but couldn't>

---POLISH-RESULT---
scorecard_before: <N>
scorecard_after: <N>
verify_before: <N>
verify_after: <N>
dogfood_before: <PASS|FAIL>
dogfood_after: <PASS|FAIL>
govet_before: <N>
govet_after: <N>
tools_audit_before: <N pending>
tools_audit_after: <N pending>
fixes_applied:
- <one-line description of each fix>
skipped_findings:
- <finding>: <why you chose not to fix it>
remaining_issues:
- <one-line description of each issue you tried to fix but couldn't>
ship_recommendation: <ship|ship-with-gaps|hold>
---END-POLISH-RESULT---
```

The three lists serve different purposes:
- **fixes_applied**: what changed — the caller displays these
- **skipped_findings**: issues you found but deliberately did not fix, with reasoning (e.g., "verify classifies `stale` as read — scorer bug, not a CLI problem", "thin-short on `version` accepted as-is — accurate and brief"). The caller surfaces these so the user can decide whether to address them manually.
- **remaining_issues**: issues you tried to fix but couldn't resolve.

## Publish Offer

If `ship` or `ship-with-gaps`:

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

Re-run Phase 1 → Phase 2 → Phase 3 with the same CLI. Maximum 2 additional polish passes (3 total including the first).

### If "Done for now"

End normally.

## Rules

- Fix everything. Do not ask for approval before fixing — polish is autonomous.
- Report results honestly. Show what improved and what didn't.
- Do not add new features. Polish fixes quality issues, not feature gaps.
- Do not re-run research or generation. Polish works with the CLI as-is.
- Do not modify the printing-press generator. That's `/printing-press-retro`.
- Do not modify any files outside `$CLI_DIR`.
- If polish adds or renames a Cobra command, the MCP surface updates automatically through the generated `internal/mcp/cobratree` runtime mirror. Update `novel_features` only when README/SKILL highlights or registry display should change; use `cmd.Annotations["mcp:hidden"] = "true"` for debug-only commands.
- Maximum 1 fix-and-rediagnose pass per polish invocation. The "Polish again" path runs additional invocations (max 3 total).
- Prefer mechanical fixes over creative decisions. When a creative decision is needed (like the CLI description), use the research brief from manuscripts if available.
