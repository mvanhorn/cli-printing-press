---
title: Polish PII audits must reconcile the same manuscript paths publish scans
date: 2026-05-24
category: logic-errors
module: polish PII audit
problem_type: logic_error
component: tooling
symptoms:
  - "Printing Press polish reports no pending PII findings"
  - "publish package later fails on .manuscripts/<run-id>/research.json or research/*.md"
  - "accepted manuscript findings disappear from the ledger during mid-pipeline promote"
root_cause: missing_workflow_step
resolution_type: code_fix
severity: high
tags: [pii-audit, polish, manuscripts, publish, ledger, runstate]
---

# Polish PII audits must reconcile the same manuscript paths publish scans

## Problem

Printing Press polish audited the generated CLI tree and wrote `.printing-press-pii-polish.json`, but `publish package` later copied manuscript artifacts into `.manuscripts/<run-id>/` before enforcing the PII gate. PII in `research.json` or top-level `research/*.md` could therefore pass polish and fail only at publish time.

## Symptoms

- `cli-printing-press pii-audit <cli-dir>` reports `no pending findings`.
- `publish package` fails after staging `.manuscripts/<run-id>/research.json` or `.manuscripts/<run-id>/research/*.md`.
- A mid-pipeline promote can drop accepted `.manuscripts/<run-id>/...` ledger entries because the promote-time audit runs before staging those files into the working tree.

## What Didn't Work

- Scanning every file under a manuscripts run would overreach. Manuscripts contain proof logs, specs, and other artifacts that publish should not newly classify as polish-blocking narrative PII.
- Changing only the polish skill catches standalone polish, but mid-pipeline promote still rewrites the ledger before the run archive exists under the published path.
- Weakening the publish PII gate hides the real mismatch between polish and publish surfaces.

## Solution

Make the optional manuscript scan explicit and path-stable:

- Add `PIIAuditOptions.ManuscriptsDir`.
- Have `FindPIIWithOptions` scan only `<run>/research.json` and `<run>/research/*.md`.
- Report those findings as `.manuscripts/<run-id>/...` so accepted ledger entries match the publish-staged tree.
- Pass `--manuscripts-dir <run-dir>` from the polish skill whenever it resolves a run directory.
- During `PromoteWorkingCLI`, run the PII gate with the active `PipelineState` run root and stage root `research.json` into `.manuscripts/<run-id>/research.json` before publishing.

The important invariant is that the audit path used for reconciliation must equal the path publish will later scan. For manuscript narratives, that path is the staged `.manuscripts/<run-id>/...` path, not the absolute runstate or archive path.

## Why This Works

`RunPIIAudit` reconciles by finding identity. If an accepted finding exists in the ledger but the current scan omits that file, reconciliation treats it as resolved and rewrites the ledger without the acceptance. Scanning the external run directory with publish-staged relative paths prevents that false resolution before promote, while staging root `research.json` gives the published copy the same file layout publish package already validates.

## Prevention

- When adding audit coverage for files outside the CLI tree, define the publish-visible relative path first and write ledger findings using that path from the start.
- Keep scan scope narrow when an archive contains mixed artifact types. For this PII path, scan `research.json` and top-level narrative markdown, not proofs or specs.
- Regression tests should cover both standalone audit output and the promote path that rewrites the ledger.
- If a skill resolves archived run data, its lookup priority should match the Go publish path: manifest run id first when present, then API slug latest run, then CLI name latest run.

## Related Issues

- GitHub issue #1622
- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`
