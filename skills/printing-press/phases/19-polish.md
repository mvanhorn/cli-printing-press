## Phase 5.5: Polish

**Always runs.** Invoke the `printing-press-polish` skill to run diagnostics, fix quality issues, and return a delta. The polish skill carries `context: fork` in its frontmatter, so its diagnostic-fix-rediagnose loop runs in a forked context — diagnostic spam, fix iterations, and re-audits stay scoped to the polish session and don't pollute this generation flow. The skill is autonomous — no user input needed. The goal is to ship the best CLI possible, not the fastest.

Before invoking polish, collect the [Phase 3](11-build-the-goat.md) transcendence gate state and include
it in the polish input bundle. Include the captured `PRINTING_PRESS_BIN` value
so the forked polish skill uses the same preflight-selected binary instead of
resolving a potentially stale global binary from PATH:

```yaml
printing_press_bin: <captured PRINTING_PRESS_BIN>
phase3_transcendence_rows_planned: <planned>
phase3_transcendence_rows_built: <built>
phase3_transcendence_rows_missing:
  - <manifest row name or command>
prior_sub60_reprint: <true|false>
partial_transcendence_override: <none or build-log note path>
```

Invoke via the Skill tool (**foreground** — must complete before promoting).
Pass `$CLI_WORK_DIR` as the first line of `args`, followed by the [Phase 3](11-build-the-goat.md) bundle:

```
Skill(
  skill: "cli-printing-press:printing-press-polish",
  args: "$CLI_WORK_DIR
printing_press_bin: <captured PRINTING_PRESS_BIN>
phase3_transcendence_rows_planned: <planned>
phase3_transcendence_rows_built: <built>
phase3_transcendence_rows_missing:
  - <manifest row name or command>
prior_sub60_reprint: <true|false>
partial_transcendence_override: <none or build-log note path>"
)
```

Polish must treat `prior_sub60_reprint: true` plus any missing row as `ship_recommendation: hold` unless `partial_transcendence_override` names the accepted exception. This keeps mid-pipeline polish from recommending `ship` for a reprint that regressed from the approved manifest before [Phase 6](21-next-steps.md) sees the artifact.

**Pass `$CLI_WORK_DIR` (the absolute working-dir path), not the API slug.** Phase 5.5 fires before [Phase 5.6](20-promote-and-archive.md) promotes the working CLI to the library, so `$PRESS_LIBRARY/<slug>/` either doesn't exist yet or contains the *prior* run's CLI. If you paraphrase the args to the slug (e.g., `args: "producthunt"`), polish silently operates on the stale library copy.

**Do not pass `--standalone` in `args`.** Polish's Publish Offer is gated on caller mode (see polish SKILL.md "Publish Offer"): slash-command invocations or Skill-tool invocations carrying `--standalone` run the offer; everything else defers. Phase 5.5 is mid-pipeline — main SKILL owns the publish flow at [Phase 6](21-next-steps.md) — so this invocation must remain flag-free. Passing `--standalone` here would re-introduce the failure mode the flag was added to prevent: polish forks the public library, sets global git config, and opens a real PR before the working CLI has been promoted.

The polish skill runs the full diagnostic-fix-rediagnose loop including MCP tool quality polish (via `cli-printing-press tools-audit` plus the playbook at `../../printing-press-polish/references/tools-polish.md`) and ends its response with a `---POLISH-RESULT---` block containing scorecard/verify/tools-audit before/after, fixes applied, and a ship recommendation.

Parse the result block. Display the delta to the user:

```
Polish pass:
  Verify:      86% → 93% (+7%)
  Scorecard:   92  → 94  (+2)
  Tools-audit: 76  → 0   pending findings
  Fixed: [summary of fixes_applied from result]
```

**Verdict override:** If the polish skill's `ship_recommendation` is `hold` and the [Phase 4](12-shipcheck.md) verdict was `ship`, downgrade to `hold`. Release the lock without promoting.

Mid-pipeline polish does **not** run `publish-validate` — that gate is the publish skill's responsibility at [Phase 6](21-next-steps.md), where the prerequisites it checks (manifest.printer from `git config github.user`, packaged `tools-manifest.json`, phase5 acceptance proof under `$CLI_DIR/.manuscripts/<run>/proofs/`) are actually satisfied. Polish emits `publish_validate_before: skipped (mid-pipeline)` and `publish_validate_after: skipped (mid-pipeline)` in this invocation; treat those values as informational, never as a hold signal. A first-time user without `git config github.user` set will no longer see their CLI-level run downgraded to `hold` because of a publish prerequisite that the press itself owns satisfying.

Write the polish skill's full response to:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-polish.md`


Next: phases/20-promote-and-archive.md
