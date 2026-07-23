## Phase 4.9: README/SKILL/AGENTS Correctness Audit

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "15-readme-skill-agents-correctness-audit"
```

**Runs after [Phase 4.8](14-agentic-skill-review.md), before [Phase 5](18-dogfood-testing.md).** [Phase 4.8](14-agentic-skill-review.md) reviews whether the SKILL's trigger phrases and major claims match shipped behavior. Phase 4.9 reviews the user-facing artifacts as documents: README.md, SKILL.md, and AGENTS.md must not contain boilerplate that does not apply to this CLI.

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

**Description source-of-truth fix path.** When a finding says a novel-feature
description, capability summary, or surrounding narrative content is inaccurate,
fix the source text in `$RESEARCH_DIR/research.json`
(`novel_features[].description`, `novel_features[].narrative`, or the matching
`novel_features_built` entry) and regenerate/sync from there. Do not patch only
README.md, SKILL.md, or AGENTS.md for content-level description fixes: the same
description fans out to README "Unique Features", SKILL "Unique Capabilities",
`internal/cli/root.go` Highlights, `internal/cli/which.go`,
`internal/mcp/tools.go`, and `.printing-press.json`. Flag, command, auth,
example, and structural findings that are not generated from research.json keep
their current local fix path.

**Gate:** Any error finding is fix-before-Phase-5. Warnings may proceed only when they are explicitly explained in the acceptance report.

Before following `Next:`, record the durable handoff with a short result note:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "15-readme-skill-agents-correctness-audit" --note "<pass or fixed finding count>"
```

Next: phases/16-agentic-output-review.md
