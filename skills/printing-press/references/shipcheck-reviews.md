# Phase 4.x: Post-Shipcheck Review Cluster

Lazy-loaded bodies for Phases 4.7, 4.8, 4.9, 4.85, and 4.95. These run after the Phase 4 shipcheck umbrella and before Phase 5, in this order. The dispatcher keeps the sequence + each phase's gate outcome; read the relevant section here in full when you reach it.

## Phase 4.7: Sync Param-Drop Gate

**Runs after shipcheck, before Phase 4.8.** Generated endpoint commands are param-cardinality-checked mechanically by `cobratree` against the spec — hand-authored sync / transcendence code is not. When the printed CLI's `internal/syncer/` calls `client.Get(<path>, params)` (or `Post`/`Put`/`Patch`/`Delete` with body params) against an endpoint the browser-sniff capture also observed, the gate compares the passed-key set against the captured-key set and flags any call where the capture is a strict superset of the code. Same JSON structure on both sides; only cardinality drift catches the "5 params here, 11 on the live site" failure mode.

Skip the gate when there's no `traffic-analysis.json` for this CLI (catalog wrapper-only entries, vendor-spec CLIs without a browser-sniff phase). Otherwise:

```bash
printing-press sync-param-drop \
  --dir "$CLI_WORK_DIR" \
  --traffic-analysis "$API_RUN_DIR/<api>-traffic-analysis.json" \
  --strict
```

The same diff also runs as part of `printing-press dogfood` when you pass `--traffic-analysis`; shipcheck's dogfood leg will surface findings as a WARN-level dogfood issue automatically. Running the standalone subcommand with `--strict` during fix iteration gives a focused exit code without re-running the full dogfood matrix.

### Failure handling

A finding tells the reviewer exactly three things: `<file>:<line>: <METHOD> <path> — dropped params: <key1>, <key2>, ...`. The fix is one of:

1. **Add the missing params to the sync call.** This is the default — the live site captured them, so the printed CLI should too. The dropped keys are almost always required for the response shape the CLI's domain commands expect (Factor75: passing `week, country, locale, subscription, product-sku` returns a generic plan preselect; the live site additionally passes `servings, delivery-option, postcode, preference, customerPlanId, include-future-feedback` to get the user's actual cart). Widening the call is the only fix that resolves the underlying bug.
2. **Annotate the call with an evidence-backed opt-out.** Add `// pp:sync-params-intentional-subset reason=<why>` on the line immediately above the call when the subset is genuinely intentional — for example, a logged-out endpoint that doesn't accept the session-bound keys, or a deliberately broader query the CLI surfaces as a separate command. The `reason=` text is preserved in the audit trail; the gate counts suppressed sites separately so unbounded growth surfaces as its own smell.

The gate does not introspect response content. A passing gate proves request-key parity with the captured site, not response correctness — Phase 4.85's agentic output review remains the layer that catches "wrong response shape, right request shape."

### Scope boundary

- The gate inspects `internal/syncer/`, `internal/sync/`, `internal/transcend/`, and `internal/transcendence/`. Generated endpoint command files under `internal/cli/` are already covered by `cobratree`'s mechanical endpoint-surface check and are intentionally skipped to avoid double-flagging.
- Paths the capture never observed (synthetic / transcendence-only endpoints) are not flagged — the gate's question is "does the live site call this path with more keys," and absence of capture is a no-flag state.
- A call that passes a key the capture never observed (extra-keys-from-code) is not flagged — exotic-mode params the public UI never exercised are out of scope.

## Phase 4.8: Agentic SKILL Review

**Runs after shipcheck, before Phase 5.** `verify-skill` (Phase 4) is a mechanical check — it catches wrong flags on wrong commands, undeclared flags, and positional-arg count mismatches. It cannot catch **semantic** issues that only a reader notices:

- A trigger phrase promises behavior the CLI doesn't have ("plan dinners for the week" when there's no `meal-plan suggest`, only manual `meal-plan set`)
- A novel-feature description says the feature does X; the actual command does Y
- The AuthNarrative mentions `auth login --chrome` when the CLI's auth subcommands are only `set-token`/`logout`/`status`
- Novel features shipped as stubs aren't labeled as such in the SKILL (contradicts Phase 1.5 stub-marking rule)
- Recipes/worked examples produce output that doesn't match their prose claims
- Trigger phrases sound agent-natural or sound like marketing copy

### Dispatch

Use the Agent tool (general-purpose or a dedicated reviewer) with this prompt contract:

> Review the SKILL.md at `$CLI_WORK_DIR/SKILL.md` against the shipped CLI. You have these ground-truth sources:
>
> - `<cli> --help` output — enumerate it recursively if needed.
> - The absorb manifest in `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`.
> - The `research.json` `novel_features` (planned) and `novel_features_built` (verified) fields.
> - The README at `$CLI_WORK_DIR/README.md`.
>
> For each of these semantic checks, report findings under 50 words each:
>
> 1. **Trigger phrases match capabilities.** Does every trigger phrase in the SKILL's description frontmatter correspond to something the CLI can actually do? Flag phrases that imply missing capabilities.
> 2. **Verified-set alignment.** The SKILL's "Unique Capabilities" commands must exactly match `novel_features_built` from research.json. Planned-only features from `novel_features` must not appear there after dogfood sync. Any extra or missing command is a finding.
> 3. **Novel-feature descriptions match commands.** For each feature in the "Unique Capabilities" section, run `<cli> <command> --help` and verify the description matches the actual behavior. Mismatches are findings.
> 4. **Stub/gated disclosure.** If a feature that remains in `novel_features_built` is intentionally stubbed, CF-gated, unavailable without external setup, or returns a known-gap response, the SKILL must label that limitation where an agent decides whether to use the command. Unlabeled limitations are findings.
> 5. **Auth narrative accuracy.** Read the auth section. Does every `auth login/set-token/status` invocation mentioned actually exist on the CLI? Does the narrative match the CLI's auth type (api_key vs cookie vs session_handshake)?
> 6. **Recipe output claims.** For the worked examples, does the prose claim match what the command actually produces? (Not the exact output — the shape and intent.)
> 7. **Marketing-copy smell.** Does the SKILL read like ad copy ("comprehensive", "seamless", "powerful") instead of concrete capability descriptions? Those phrases are findings.
>
> Return a list of findings. For each: check name, severity (error/warning), line number, one-sentence fix. If SKILL passes all seven checks, return "PASS — no findings."

### Gate

- If the reviewer returns PASS, proceed to Phase 5.
- If the reviewer returns findings of severity `error`, fix them before Phase 5. Same fix-now contract as other shipcheck findings.
- If the reviewer returns only `warning` findings, surface them to the user and proceed if they approve.

### Why agentic vs template-only

A template-level check would require every possible semantic mismatch to be pattern-matchable against source. Many aren't — "does this trigger phrase correspond to what the CLI does" is an LLM-shaped question. Accept the token cost for the catch.

### Known blind spots

The agent can't verify runtime behavior without running commands; stick to help-text and source-based claims. For runtime-behavior claims (e.g., "returns 5 matching recipes"), Phase 5 dogfood is the right gate.

## Phase 4.9: README/SKILL/AGENTS Correctness Audit

**Runs after Phase 4.8, before Phase 5.** Phase 4.8 reviews whether the SKILL's trigger phrases and major claims match shipped behavior. Phase 4.9 reviews the user-facing artifacts as documents: README.md, SKILL.md, and AGENTS.md must not contain boilerplate that does not apply to this CLI.

Use the Agent tool or review directly with this prompt contract:

> Audit `$CLI_WORK_DIR/README.md`, `$CLI_WORK_DIR/SKILL.md`, and `$CLI_WORK_DIR/AGENTS.md` for factual correctness against the shipped CLI. Ground truth is `<cli> --help` recursively, `$CLI_WORK_DIR/internal/cli/*.go`, `$RESEARCH_DIR/research.json`, and the absorb manifest.
>
> Check:
> - Every command, subcommand, flag, exit code, config path, and example resolves to the printed CLI.
> - README `## Unique Features` and SKILL `## Unique Capabilities` match `novel_features_built`; planned-only features from `novel_features` are not claimed after dogfood sync.
> - Surrounding prose, recipes, trigger phrases, and examples do not indirectly promise planned features that dogfood dropped.
> - No placeholder literals remain in executable examples (`<cli>`, `<command>`, `<resource>`, `<CLI>`).
> - Boilerplate matches the CLI shape: no CRUD/retry/create-stdin/delete/cache/auth/async-job claims unless the CLI actually implements them.
> - Read-only CLIs say they are read-only and do not imply create/update/delete support.
> - No-auth CLIs omit auth troubleshooting and auth exit-code claims unless the binary can raise them.
> - Stubbed, CF-gated, or unavailable commands are disclosed where an agent decides whether to use the CLI.
> - The SKILL has anti-triggers: common requests this CLI should not handle.
> - Brand/display names use the canonical prose name from research, not only the slug.
> - Marketing phrases map to real commands; invented feature names are findings.
>
> Return findings with file, line, severity, and fix. If both files are correct, return `PASS — README/SKILL correctness verified`.

**Gate:** Any error finding is fix-before-Phase-5. Warnings may proceed only when they are explicitly explained in the acceptance report.

## Phase 4.85: Agentic Output Review

**Runs after Phase 4.8, before Phase 4.95.** Phase 4.8 reviews SKILL.md prose against the shipped CLI. Phase 4.85 reviews the CLI's **actual command output** for plausibility bugs that rule-based checks can't encode (substring-match relevance failures, format bugs, silent source drops, ranking failures). The dispatch prompt, gate logic, and known blind spots live in the `printing-press-output-review` sub-skill — single source of truth shared with the polish skill (which runs the same review during its diagnostic loop).

Invoke the sub-skill via the Skill tool:

```
Skill(
  skill: "cli-printing-press:printing-press-output-review",
  args: "$CLI_WORK_DIR"
)
```

The sub-skill carries `context: fork` so the reviewer agent's diagnostic chatter stays isolated from this generation flow. It returns a `---OUTPUT-REVIEW-RESULT---` block with `status: PASS|WARN|SKIP` and a list of findings.

**Wave B rollout policy:** all findings surface as **warnings**, not blockers. Shipcheck does not fail on Phase 4.85 findings. Log the findings to `manuscripts/<api>/<run>/proofs/phase-4.85-findings.md` and surface them to the user. The user decides case by case whether to fix before shipping. Wave B calibrates false-positive rates before Wave C flips errors to blocking.

## Phase 4.95: Local Code Review

**Runs after Phase 4.85, before Phase 5.** Reviews the printed CLI source for security and correctness issues *before* any PR exists. This is the cheapest fix window in the pipeline — session context is hot, no PR feedback round-trip, no CI comments to chase. Catching issues here means they never become PR-time review comments, which is the wrong fix window for the same problems.

**Target.** The generated CLI and MCP source under `$CLI_WORK_DIR`. In scope: `internal/cli/`, `internal/mcp/` (excluding `cobratree/`), `internal/store/`, `internal/client/`, and `cmd/`. **Out of scope:** `internal/cliutil/` and `internal/mcp/cobratree/` — these are generator-reserved packages. Any finding there is a machine bug; route to retro, do not patch in place.

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
- **Shipping scope shrinks.** Closing the finding cleanly means dropping or significantly degrading a Phase 1.5-approved feature. (Per the Rules section, scope changes route back to Phase 1.5 for re-approval, not a silent shrink here.)
- **Two materially different valid fixes** with different cost, surface, or dependency profiles, and either is defensible.
- **The finding implies a Phase 1 research miss** — wrong primary source, wrong auth model, wrong transport — that the agent cannot resolve from in-session context.
- **The fix re-triggers a long phase** (re-running browser-sniff, regen from spec, etc.).

Treat agent judgment as sufficient here — these categories are distinguishable on inspection. Conservatism is the failure mode, not over-fixing. Drafting an AskUserQuestion because "the user might want to know" is premature; fix the issue and note it in the shipcheck report.

Re-run the review after each autofix round until findings clear. Cap at 3 rounds; if findings persist after round 3, stop and surface — autofix is not converging. Findings in out-of-scope paths (`internal/cliutil/`, `internal/mcp/cobratree/`) file as retro-candidates and do not count toward the convergence check or the 3-round cap; the convergence check applies only to in-scope findings.

**Findings artifact.** Log to `manuscripts/<api>/<run>/proofs/phase-4.95-findings.md`. Skip the per-finding enumeration for fixed-in-place items — the commits and diffs are already the authoritative record. Specifically:
- **Autofix summary (one line).** "N findings autofixed in-place across M rounds; see commits `<hash>`, `<hash>`, …" Do not enumerate the fixed findings.
- **Template-shape retro candidates (full detail).** Each finding's file:line, severity, the template path it appears to come from, and why it was filed instead of fixed. Not fixed in-place, so the log is the only record.
- **Out-of-scope retro candidates (full detail).** Findings in `internal/cliutil/` or `internal/mcp/cobratree/`. Same shape as template-shape entries.
- **Surface-to-user findings (full detail).** Each finding's file:line, severity, the real-tradeoff category it falls into, and the user's decision once they make one. Pending between turns; the log is what carries them.
- **Convergence outcome (one line).** "Findings cleared at round N" or "stopped at round 3 with N findings outstanding — see surface-to-user list."
- **Review path chosen (one line).** Skill name + invocation form, or "direct subagent dispatch" with the persona list. Lets a retro audit tool-selection drift across runs.

The retro skill scans the template-shape and out-of-scope sections for candidates worth filing against the machine.

**Rollout posture.** Unlike Phase 4.85, this phase starts without a warnings-only calibration period. Local code review is a well-understood surface — calibration risk is low. The 3-round autofix cap is the safety net for runaway findings, and the template-shape escape hatch routes systemic issues to retro instead of patching in place.

**Template-shape escape hatch.** Even if a finding lives in an in-scope path, if it appears to come from a generator template (recurs across files in identical shape, sits in a path matched by `internal/generator/templates/`'s emit set, or duplicates a known prior template bug), file as retro-candidate and surface to the user rather than autofixing. Patching the printed CLI hides the machine bug from the next CLI.

**Post-fix simplification (Claude Code only).** After the review + autofix loop converges, the printed CLI has fresh edits from the autofix passes — typically defensive guards, sanitization helpers, and near-duplicate fixes across sibling files. Run `/simplify` scoped to the same in-scope paths to consolidate duplication, remove dead code, and tighten the autofix output before dogfood. `/simplify` is Claude Code-only; skip on Codex and other harnesses (they have no built-in equivalent, and the press skill explicitly avoids custom simplification logic — same rule as the review path above).

**Harness exemption — narrow.** Skipping this phase is legitimate only when the current harness has *neither* a working-dir-shaped review skill *nor* the Agent/subagent capability needed for the direct-dispatch fallback. In practice this is almost never true — any harness that runs the press skill has access to subagents. The following rationales are **not** acceptable for skipping:

- "The first tool name I tried (e.g., `/review`, `code-review:code-review`) didn't fit, so the harness must have no review path." Survey the catalog before claiming exemption; if no skill fits, dispatch reviewer subagents directly via the Agent tool.
- "There's no PR yet, so code review can't run here." Pre-PR is the *point* of this phase. CI-time PR review is too late.
- "PR-time CI review will catch it." That defeats the purpose of running review in the cheapest fix window.

If a skip is genuinely warranted, the shipcheck report must state which review-shaped capabilities were searched and why none fit — not just "harness exemption."

