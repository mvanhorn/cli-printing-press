# Patterns

Cross-cutting design patterns the printing press uses, with the rules for applying them. Loaded on-demand when designing or extending a workflow that needs one.

## Deterministic Inventory + Agent-Marked Ledger

When a workflow has a checklist where detection is mechanical but each item needs per-item judgment, split the work between a binary-emitted inventory and an agent-maintained ledger. The binary owns "what's there"; the agent owns "what to do about each item." A persistent file holds both, so the work survives context flushes and the audit trail surfaces the agent's reasoning.

The canonical example is `printing-press tools-audit` + `skills/printing-press-polish/references/tools-polish.md`. The binary parses every Cobra command and the runtime tools manifest, emits findings (empty Short, thin Short, missing read-only annotation, thin/empty MCP description). The agent walks each finding, fixes most, and marks the rest `accepted` with a one-sentence rationale plus pre-decision fields where the gate requires them. The ledger persists at `<cli-dir>/.printing-press-tools-polish.json` for 24 hours. `printing-press public-param-audit` uses the same shape for cryptic wire parameters: the binary finds decision-required params, while the agent either authors `flag_name` in the spec or records an evidence-backed skip.

Reach for this pattern when the work has the **detect mechanically + decide per-item + persist rationale** shape. The trigger isn't a numeric item count — a 15-item list with three accept decisions across two sessions benefits, while a 200-item batch update where every item has the same fix does not. Skip it when one pass is enough, when every item has the same fix, when detection itself requires judgment, or when a `TodoWrite` task list with rationale in the description carries the whole workflow.

### Structure

1. **Binary writes the inventory.** A subcommand emits a structured snapshot file (`.<topic>-ledger.json` or similar) on every run. Each entry has stable identity fields (file, line, kind, key) and may carry agent-written `status` and `note` fields (`omitempty` so the bare audit output stays clean).
2. **Agent annotates the ledger.** When the agent decides to keep an item as-is, it edits the ledger to set `status: "accepted"` and writes a `note`. Code fixes are *not* marked manually — the next run re-detects and the finding disappears automatically.
3. **Re-runs preserve agent state.** The binary reads the previous ledger before writing the new one. Findings whose identity key matches inherit `status` and `note`. Findings present last run but absent now read as "resolved" in the delta line. New findings start fresh as pending.
4. **Staleness, not history.** Ledgers age out (e.g., 24h) and are deleted. They're working state, not artifacts to preserve in version control. Add the ledger filename to the relevant repo's `.gitignore` if the cli-dir lives inside one.
5. **Verification asks for zero pending plus zero gate failures, not zero findings.** "Done" means every finding is either fixed (auto-removed) or explicitly accepted with a note that satisfies the enforcement primitives below. Reviewers can see accepts in the ledger and judge whether each rationale holds.

### Enforcement primitives (when bulk-accept is the failure mode)

The five-point structure above gives you a ledger. It does not, on its own, force the agent to deliberate per item — `/simplify-and-refactor-code-isomorphically` (Isomorphism Card filled before each edit) and `/library-updater` (per-package log + crash-recovery checkpoint) layer additional checks on top of the same shape. Apply the primitives below when the workload is large enough that bulk-accept is a realistic failure mode (>50 findings of similar kind, >1 review round expected, or anywhere the agent might reach for "this is systemic" as a punt).

1. **Pre-decision fields per item, filled before the verdict.** For finding kinds where bulk-accept is the failure mode, add required fields the agent must populate before `status: "accepted"` counts. The fields force per-item reasoning — naming the specific source material, the target output, and the gap between them is much harder to fake than a one-line `note`. The binary refuses runs where any accepted entry of that kind has empty pre-decision fields.

   Concrete example from tools-audit: `thin-mcp-description` accepts require `spec_source_material` (what the OpenAPI spec actually provides for this endpoint), `target_description` (what a 10/10 description would say), and `gap_analysis` (why the generator can't produce target from source today). The third field is load-bearing — it forces the agent to decide between "file as a generator improvement" and "write an override," instead of a generic "specs are thin" punt.

2. **Reject identical rationales above a threshold.** Cluster accepted entries by normalized `note` text (lowercase, collapsed whitespace). If any cluster exceeds N entries (5 in tools-audit), the run is incomplete. Differentiated rationales survive; bulk paste-the-same-thing-everywhere does not.

   The threshold is a hedge, not an absolute: 3-4 accepts sharing a rationale is normal noise; 50 sharing one is a punt. Set the threshold low enough to catch the punt without false-positiving on natural overlap.

3. **Numeric end-state gate tied to a scorer dimension.** Capture the relevant score *before* work begins (sticky in the ledger across runs). On each subsequent run, recompute the current score. If the agent accepted findings that *should* have driven the dimension up but didn't, the run is incomplete. Either the accepts were unwarranted (overrides would have lifted the score) or the dimension is mis-scored (rare; surface to retro).

   Concrete example from tools-audit: `scorecard_before.mcp_description_quality` is captured on the first run. If subsequent runs accept any `thin-mcp-description` findings without lifting `MCPDescriptionQuality`, the run is incomplete. The agent owes either an override or a generator-improvement filing — accept-and-walk-away is no longer a complete state.

4. **Resume protocol with explicit progress field.** The 24h staleness rule covers across-runs cleanup but not within-run context flushes. Add `progress.last_processed_finding_id` to the ledger header; the agent updates it after each decision. The binary's render surfaces the next-pending finding as a `next:` line so a re-invocation after compaction picks up where the agent left off rather than re-scanning from the head.

   The progress field is a soft hint, not a gate — when absent, the binary derives the next-pending finding from `status` + pre-decision-fields state. Setting the field is the explicit checkpoint the agent updates as they walk.

### Comparison to alternatives

Pure `TodoWrite` state is invisible to the binary and dies with the session; pure binary recompute can't track accept decisions and re-flags them every run; multi-file artifacts (cards/, ledger.md, rejections.md per the `simplify-and-refactor` skill) are heavier than warranted when each item is small and self-contained. The single-JSON ledger plus the four enforcement primitives is the minimum that survives both context flushes and bulk-accept patterns.
