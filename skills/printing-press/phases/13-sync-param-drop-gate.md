## Phase 4.7: Sync Param-Drop Gate

**Runs after shipcheck, before [Phase 4.8](14-agentic-skill-review.md).** Generated endpoint commands are param-cardinality-checked mechanically by `cobratree` against the spec — hand-authored sync / transcendence code is not. When the printed CLI's `internal/syncer/` calls `client.Get(<path>, params)` (or `Post`/`Put`/`Patch`/`Delete` with body params) against an endpoint the browser-sniff capture also observed, the gate compares the passed-key set against the captured-key set and flags any call where the capture is a strict superset of the code. Same JSON structure on both sides; only cardinality drift catches the "5 params here, 11 on the live site" failure mode.

Skip the gate when there's no `traffic-analysis.json` for this CLI (vendor-spec CLIs without a browser-sniff phase). Otherwise:

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

The gate does not introspect response content. A passing gate proves request-key parity with the captured site, not response correctness — [Phase 4.85](16-agentic-output-review.md)'s agentic output review remains the layer that catches "wrong response shape, right request shape."

### Scope boundary

- The gate inspects `internal/syncer/`, `internal/sync/`, `internal/transcend/`, and `internal/transcendence/`. Generated endpoint command files under `internal/cli/` are already covered by `cobratree`'s mechanical endpoint-surface check and are intentionally skipped to avoid double-flagging.
- Paths the capture never observed (synthetic / transcendence-only endpoints) are not flagged — the gate's question is "does the live site call this path with more keys," and absence of capture is a no-flag state.
- A call that passes a key the capture never observed (extra-keys-from-code) is not flagged — exotic-mode params the public UI never exercised are out of scope.


Next: phases/14-agentic-skill-review.md
