# Durable phase receipts

The receipt ledger is the sequencer for this workflow. Read this file before the
first phase that records a receipt, and again after any interruption.

This is a long-running workflow. Do not rely on conversation memory to decide
which phase comes next. `02-run-initialization` creates a disposable,
append-only receipt log at `$PIPELINE_DIR/phase-receipts.jsonl`. From
`03-resolve-and-reuse` onward, every phase file has an entry command and an
exit command. Receipt IDs are the filename stems (`10-generate`, not "Phase 2").

At each phase boundary:

1. Run the phase file's `enter` command and read its `previous` receipt.
2. On an ordinary handoff, confirm that `previous.next` names this phase. On an
   idempotent re-entry or explicit `--resume`, confirm that `previous.phase`
   names this phase. The binary derives the canonical phase file and next phase
   itself and rejects any other ordering. The only exceptions are the documented rework and hold handoffs,
   recorded with `--next` — an alternate handoff always requires `--note` and
   never combines with `--skip`:
   - discovery rework — `08-ecosystem-absorb-gate` or `09-api-reachability-gate` back to a sniff gate (`08→06`, `08→07`, `09→06`)
   - rework return — `06-browser-sniff-gate` back to the gate that ordered it, and only that gate: the binary reads the ordering handoff out of the ledger (`06→08`, `06→09`)
   - build infeasible — `11-build-the-goat` back to `08-ecosystem-absorb-gate` (`11→08`)
   - shipcheck hold — `12-shipcheck` straight to `20-promote-and-archive` (`12→20`)
   - scope change in review — `17-local-code-review` back to `08-ecosystem-absorb-gate` (`17→08`)
   - promote backtrack — `20-promote-and-archive` back to `18-dogfood-testing` (`20→18`), refused after a `12→20` hold
3. Do the phase work only after the entry receipt succeeds.
4. Run `complete` before following the phase file's `Next:` pointer. Use
   `--skip --note "<allowed reason>"` for an allowed skip.
5. If work cannot continue, run `stop --note "<concise reason>"` instead of
   pretending the phase completed. Add `--failed` for a failed phase. Resume a
   blocked or failed phase by adding `--resume` to its `enter` command.

After context compaction or another interruption, re-run
[preflight](../phases/01-preflight.md) if `state.json` has no
`printing_press_bin` (or that path is gone), then read the current
`state.json`, use its `run_id`, `phase_receipt_log`, and `printing_press_bin`
values to run `phase-receipt status`, and only then choose a phase file by
filename stem. The receipt's `next` field is the restart pointer; do not infer
that pointer from the conversation. A mid-phase interruption has an empty
`next`, because `entered`, `blocked`, and `failed` receipts carry none: when
the latest event is `entered`, the restart pointer is that same phase
(re-entering it is idempotent), and a `blocked` or `failed` receipt is resumed
by re-entering the same phase with `--resume`. If a fresh conversation has lost
the run entirely, find `state.json` at `$PRESS_RUNSTATE/runs/<run-id>/state.json`
— [preflight](../phases/01-preflight.md) defines `$PRESS_RUNSTATE`.

Receipts own sequencing only. Existing artifacts remain the source of truth for
specs, manifests, builds, proofs, acceptance, and promotion. Add `--evidence`
only for paths that already exist; never copy artifact contents into the log.
Keep `--note` to one short decision or result. Never write secrets, credentials,
cookies, raw API responses, or PII to a receipt.

The receipt log is disposable run state. It stays under `$PRESS_RUNSTATE/`, is
never copied into manuscripts, and is never committed. If the hidden
`phase-receipt` helper is unavailable, stop with `[setup-error]`; do not
hand-edit the JSONL or invent a fallback log format.
