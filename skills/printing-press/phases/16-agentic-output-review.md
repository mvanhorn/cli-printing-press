## Phase 4.85: Agentic Output Review

**Receipt entry (required):**

```bash
"$PRINTING_PRESS_BIN" phase-receipt enter --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "16-agentic-output-review"
```

**Runs after [Phase 4.8](14-agentic-skill-review.md), before [Phase 4.95](17-local-code-review.md).** [Phase 4.8](14-agentic-skill-review.md) reviews SKILL.md prose against the shipped CLI. Phase 4.85 reviews the CLI's **actual command output** for plausibility bugs that rule-based checks can't encode (substring-match relevance failures, format bugs, silent source drops, ranking failures). The dispatch prompt, gate logic, and known blind spots live in the `printing-press-output-review` sub-skill — single source of truth shared with the polish skill (which runs the same review during its diagnostic loop).

Invoke the sub-skill via the Skill tool:

```
Skill(
  skill: "cli-printing-press:printing-press-output-review",
  args: "$CLI_WORK_DIR"
)
```

The sub-skill carries `context: fork` so the reviewer agent's diagnostic chatter stays isolated from this generation flow. It returns a `---OUTPUT-REVIEW-RESULT---` block with `status: PASS|WARN|SKIP` and a list of findings.

**Wave B rollout policy:** all findings surface as **warnings**, not blockers. Shipcheck does not fail on Phase 4.85 findings. Log the findings to `manuscripts/<api>/<run>/proofs/phase-4.85-findings.md` and surface them to the user. The user decides case by case whether to fix before shipping. Wave B calibrates false-positive rates before Wave C flips errors to blocking.

Before following `Next:`, record the durable handoff and point `--evidence` at
the findings artifact:

```bash
"$PRINTING_PRESS_BIN" phase-receipt complete --file "$PHASE_RECEIPT_LOG" --run-id "$RUN_ID" --phase "16-agentic-output-review" --evidence "<absolute-phase-4.85-findings-path>"
```

Next: phases/17-local-code-review.md
