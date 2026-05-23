---
title: "Scorecard scorers must follow reachable command surfaces"
date: 2026-05-21
last_updated: 2026-05-23
category: logic-errors
module: internal/pipeline
problem_type: logic_error
component: tooling
symptoms:
  - "Non-REST CLIs score too low when real client, error, output, or sync logic lives in sibling internal packages"
  - "Dead generic REST scaffolding can look like the only scored surface"
  - "Re-adding dead scaffold would improve scores without improving the printed CLI"
  - "Capability-equivalent resource-grouped or generic-store commands score lower than literal-pattern command files"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags:
  - scorecard
  - scorer
  - non-rest
  - reachability
  - dead-code
  - json-rpc
---

# Scorecard scorers must follow reachable command surfaces

## Problem

Several scorecard dimensions assumed generated REST scaffolding was the canonical implementation surface. For JSON-RPC or other non-REST CLIs, the real command behavior can live in a sibling package such as `internal/<api>/`, while the generic `internal/client` scaffold is absent or intentionally removed.

The same failure mode applies when a REST CLI exposes equivalent capability through a different reachable shape: resource-grouped subcommands instead of top-level literal filenames, generic `resources` SQL queries instead of typed store search methods, or quota-aware cache designs that intentionally omit auto-refresh.

## Symptoms

- Error handling, output modes, and sync correctness were under-scored even when a registered command delegated to richer sibling-package code.
- Renamed sync command files, such as `sync_<api>.go`, missed sync credit when the runtime Cobra mirror made them reachable.
- Nested Cobra subcommands were invisible when only the parent command's first `Use:` literal was scanned.
- Broad scans risked rewarding unregistered command files or dead files inside imported sibling packages.
- Generic resources SQL searches could be under-scored when the command used the local store directly instead of a typed `SearchX` wrapper.

## What Didn't Work

Reading fixed filenames such as `internal/cli/helpers.go`, `internal/client/client.go`, or `internal/cli/sync.go` worked for standard REST prints but contradicted the sanctioned non-REST path. Broadening to all CLI files was also insufficient: it could follow imports from orphan command files and count scoring strings from unreachable sibling-package files.

## Solution

Build scorer evidence from the registered command surface:

- Seed CLI reachability from `root.go`, framework helper files, and constructors reachable through Cobra `AddCommand` calls.
- Follow child command constructors so subcommands split across files remain visible.
- Follow internal package imports from reachable files, but include only sibling-package files that define called symbols plus same-package callees.
- Reuse the local-data signal used by the reimplementation check, including raw `database/sql` access paired with `sql.Open` or `sql.OpenDB`.
- Run sync correctness and pagination AST checks against the same reachable files used by output and error scoring.
- When adding a new capability-equivalent heuristic, evaluate it against registered command content or reachable files. For paired signals such as SQL text plus `Query` execution, require the evidence to live in the same command file unless cross-file matching is explicitly designed and covered by tests.

## Why This Works

The scorer now evaluates behavior a user or agent can actually reach. It gives non-REST CLIs credit for their real implementation while preserving the anti-gaming boundary: orphan commands, dead generic scaffolds, and unused sibling-package files do not inflate scores.

## Prevention

When adding scorecard heuristics, prefer registered-command reachability over hardcoded filenames. If a dimension needs paired signals, keep those pairs within the same source unit unless cross-file matching is deliberately justified. Add both positive reachable-package tests and negative dead-code tests so future broadening does not reintroduce scorer inflation. For equivalent-capability scoring, pair each positive fixture with a negative fixture for copied SQL, split-file evidence, or unregistered command files so the scorer rewards real capability rather than plausible-looking source text.

## Related

- `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching-2026-03-27.md`
- `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md`
