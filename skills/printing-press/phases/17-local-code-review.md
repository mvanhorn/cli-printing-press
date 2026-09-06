## 17-local-code-review (Phase 4.95: Local Code Review)

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "17-local-code-review"
```

**Runs after [Phase 4.85](16-agentic-output-review.md), before [Phase 5](18-dogfood-testing.md).** Reviews the printed CLI source for security and correctness issues *before* any PR exists. This is the cheapest fix window in the pipeline — session context is hot, no PR feedback round-trip, no CI comments to chase. Catching issues here means they never become PR-time review comments, which is the wrong fix window for the same problems.

**Target.** The generated CLI and MCP source under `$CLI_WORK_DIR`. In scope: `internal/cli/`, `internal/mcp/` (excluding `cobratree/`), `internal/store/`, `internal/client/`, and `cmd/`. **Out of scope:** `internal/cliutil/` and `internal/mcp/cobratree/` — these are generator-reserved packages. Any finding there is a machine bug; route to retro, do not patch in place.

**Generated description source-of-truth.** If review flags inaccurate
novel-feature description text in generated code surfaces such as
`internal/cli/root.go` Highlights, `internal/cli/which.go`, or
`internal/mcp/tools.go`, do not patch those files directly. Fix the source text
in `$RESEARCH_DIR/research.json` (`novel_features[].description`,
`novel_features[].narrative`, or the matching `novel_features_built` entry) and
regenerate/sync so README "Unique Features", SKILL "Unique Capabilities",
root help Highlights, which output, and `.printing-press.json` stay
aligned. `dogfood --research-dir` leaves a differing
`command_mirror_capabilities` block in `internal/mcp/tools.go` unmodified and
prints the path; pass `--overwrite-command-mirror` only when the operator
intends to replace that block from `research.json`. Non-description code
findings keep the normal Phase 4.95 autofix path.

**Native timeout-boundary check.** Before reviewer dispatch, scan every hand-written file under `internal/cli/` that imports a sibling internal package (`internal/<api>/`, `internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc.) and makes live requests. Each such command file must call `boundCtx(cmd.Context(), flags)` and pass that context into the sibling client or store query path before the first request. Files that only use `flags.newClient()` / generated `internal/client` are already covered by `client.New(cfg, flags.timeout, ...)` and should not be flagged for missing `boundCtx`. Binary/stream calls on that generated client skip the default whole-call Timeout unless `--timeout` was set explicitly (including a run profile that supplies `timeout`); do not add a `boundCtx` deadline around those downloads.

**Tool selection — pick what's installed, do not name-match.** This phase needs *a* code review, not a specific named command. Survey the review-shaped capabilities the current harness has and pick the best fit. Plausible candidates (names drift across harnesses and plugin sets; treat this as an example list, not a closed set):

- A standalone, working-dir-shaped code review skill that runs against `git diff` and a file list without needing an open PR (e.g., `compound-engineering:ce-code-review`, or similar).
- Codex's built-in code-review mode (`/codex:review`), which reviews the current diff or target directly.
- **Direct reviewer-subagent dispatch via the Agent tool.** Spawn `correctness`, `security`, and `maintainability` reviewers (always-on) plus any conditional reviewers warranted by the diff (`api-contract`, `data-migrations`, `reliability`, `performance`) against the in-scope paths. This is the universal fallback: any harness that runs the press skill has the Agent tool, so this path is always available. When dispatching multiple reviewers, a "round" (per the autofix loop below) means re-running *all* spawned reviewers in parallel and merging their findings into a single set before autofix; convergence is the merged set being empty, not any individual reviewer clearing. Do not re-run only the reviewer whose prior findings were touched — every round must include every reviewer so cascading or newly-introduced issues surface.

**Do not invoke Claude Code's `/review` for this phase.** `/review` is PR-shaped — it fetches an open GitHub PR and comments back via `gh`. There is no PR yet at Phase 4.95; the CLI is in a working dir that has not been promoted or published. Reaching for `/review`, bouncing off its shape, and claiming "harness has no code review" is the failure mode this section is written to prevent.

**Autofix policy.** Session context is hot, no PR feedback round-trip, no publish decision in flight. The default is fix. Surfacing to the user is the exception, not the rule. Severity is informational, not gating: a low-severity nil-deref is a 30-second fix; close it the same as a high-severity one.

Fix without asking when:
- The fix is mechanical (parameterized query, input validation, error wrapping, missing nil check, dead code removal, obvious refactor).
- The fix is small-scope and behavior-preserving from the README's point of view.
- There is no plausible competing implementation a reasonable user would prefer over the chosen one.

Surface to the user only when the fix requires a real tradeoff they have to make. Real tradeoffs look like:
- **Shipping scope shrinks.** Closing the finding cleanly means dropping or significantly degrading a [Phase 1.5](08-ecosystem-absorb-gate.md)-approved feature. (Scope changes route back to [Phase 1.5](08-ecosystem-absorb-gate.md) for re-approval, not a silent shrink here.)
- **Two materially different valid fixes** with different cost, surface, or dependency profiles, and either is defensible.
- **The finding implies a [Phase 1](04-research-brief.md) research miss** — wrong primary source, wrong auth model, wrong transport — that the agent cannot resolve from in-session context.
- **The fix re-triggers a long phase** (re-running browser-sniff, regen from spec, etc.).

Treat agent judgment as sufficient here — these categories are distinguishable on inspection. Conservatism is the failure mode, not over-fixing. Drafting an AskUserQuestion because "the user might want to know" is premature; fix the issue and note it in the shipcheck report.

When the shipping-scope-shrinks tradeoff sends the run back to [Phase 1.5](08-ecosystem-absorb-gate.md) for re-approval, record that scope-change handoff so the receipt gate accepts the re-entry rather than treating it as an out-of-order jump, then re-enter the absorb gate instead of following the canonical `Next:` below:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "17-local-code-review" --next "08-ecosystem-absorb-gate" --note "scope change: <feature>"
```

Re-run the review after each autofix round until findings clear. Cap at 3 rounds; if findings persist after round 3, stop and surface — autofix is not converging. Findings in out-of-scope paths (`internal/cliutil/`, `internal/mcp/cobratree/`) file as retro-candidates and do not count toward the convergence check or the 3-round cap; the convergence check applies only to in-scope findings.

**Findings artifact.** Log to `manuscripts/<api>/<run>/proofs/phase-4.95-findings.md`. Skip the per-finding enumeration for fixed-in-place items — the commits and diffs are already the authoritative record. Specifically:
- **Autofix summary (one line).** "N findings autofixed in-place across M rounds; see commits `<hash>`, `<hash>`, …" Do not enumerate the fixed findings.
- **Template-shape retro candidates (full detail).** Each finding's file:line, severity, the template path it appears to come from, and why it was filed instead of fixed. Not fixed in-place, so the log is the only record.
- **Out-of-scope retro candidates (full detail).** Findings in `internal/cliutil/` or `internal/mcp/cobratree/`. Same shape as template-shape entries.
- **Surface-to-user findings (full detail).** Each finding's file:line, severity, the real-tradeoff category it falls into, and the user's decision once they make one. Pending between turns; the log is what carries them.
- **Convergence outcome (one line).** "Findings cleared at round N" or "stopped at round 3 with N findings outstanding — see surface-to-user list."
- **Review path chosen (one line).** Skill name + invocation form, or "direct subagent dispatch" with the persona list. Lets a retro audit tool-selection drift across runs.

The retro skill scans the template-shape and out-of-scope sections for candidates worth filing against the machine.

**Rollout posture.** Unlike [Phase 4.85](16-agentic-output-review.md), this phase starts without a warnings-only calibration period. Local code review is a well-understood surface — calibration risk is low. The 3-round autofix cap is the safety net for runaway findings, and the template-shape escape hatch routes systemic issues to retro instead of patching in place.

**Template-shape escape hatch.** Even if a finding lives in an in-scope path, if it appears to come from a generator template (recurs across files in identical shape, sits in a path matched by `internal/generator/templates/`'s emit set, or duplicates a known prior template bug), file as retro-candidate and surface to the user rather than autofixing. Patching the printed CLI hides the machine bug from the next CLI.

**Post-fix simplification (Claude Code only).** After the review + autofix loop converges, the printed CLI has fresh edits from the autofix passes — typically defensive guards, sanitization helpers, and near-duplicate fixes across sibling files. Run `/simplify` scoped to the same in-scope paths to consolidate duplication, remove dead code, and tighten the autofix output before dogfood. `/simplify` is Claude Code-only; skip on Codex and other harnesses (they have no built-in equivalent, and the press skill explicitly avoids custom simplification logic — same rule as the review path above).

**Harness exemption — narrow.** Skipping this phase is legitimate only when the current harness has *neither* a working-dir-shaped review skill *nor* the Agent/subagent capability needed for the direct-dispatch fallback. In practice this is almost never true — any harness that runs the press skill has access to subagents. The following rationales are **not** acceptable for skipping:

- "The first tool name I tried (e.g., `/review`, `code-review:code-review`) didn't fit, so the harness must have no review path." Survey the tool catalog before claiming exemption; if no skill fits, dispatch reviewer subagents directly via the Agent tool.
- "There's no PR yet, so code review can't run here." Pre-PR is the *point* of this phase. CI-time PR review is too late.
- "PR-time CI review will catch it." That defeats the purpose of running review in the cheapest fix window.

If a skip is genuinely warranted, the shipcheck report must state which review-shaped capabilities were searched and why none fit — not just "harness exemption."

Before following `Next:`, record the durable handoff with a short result note.
If the review was legitimately skipped, add `--skip --note "<allowed reason>"`:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "17-local-code-review"
```

Next: phases/18-dogfood-testing.md
