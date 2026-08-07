---
title: "Self-teaching loop generator port: U14 dogfood findings"
date: 2026-05-27
category: port-retrospective
module: internal/generator/templates/learn**
problem_type: port_retrospective
component: generator
severity: informational
applies_when:
  - "Looking up what the U1-U14 learn-loop port actually delivered"
  - "Triaging a regression in the cross-alias / playbook init / teach --playbook-file paths"
  - "Planning per-CLI backports of the loop to already-published library entries"
related_components:
  - learn_loop
  - playbook_init
  - cross_alias
  - greptile_findings
tags:
  - learn-loop
  - playbooks
  - cross-alias
  - greptile
  - dogfood
  - port
---

## What landed (U1-U13)

The full self-teaching loop is now in the generator templates. Future fresh
prints of any CLI whose spec declares `learn:` inherit the loop without
per-CLI hand-port work.

- **U1.** Schema bump v6 -> v7 with `learning_playbooks` table.
- **U2.** `internal/learn/playbooks.go` + test. `Playbook` / `PlaybookStep`
  types + `ResolveSlots` (entity-only candidate pool).
- **U3.** `internal/learn/promote.go` + test. `PromoteEntities` +
  `CanonicalResolver` (cache only on full success).
- **U4.** `internal/store/playbooks.go` + test. `UpsertPlaybook` (partial
  update CASE), atomic `AppendPlaybookNotes`, `GetPlaybookByFamily`,
  `ListPlaybooks` (sentinel-filtered).
- **U5.** `internal/cli/recall.go` cross-alias machinery + Greptile guards
  (case-1 + case-2 `entitySlicesIntersect`, case-insensitive helper).
- **U6.** Recall envelope surfaces `Playbook` + `Notes` (nil-safe).
- **U7.** `teach-playbook` + `playbook list` + `playbook amend` commands.
- **U8.** Integrated `teach --playbook-file` / `--playbook-notes-file` /
  `--playbook-notes` flags (one-call teach + playbook write).
- **U9.** `internal/cli/playbooks/embed.go` + `MANIFEST.md` scaffold.
  `//go:embed *.md` placeholder; extends to `*.json *.md` when authoring.
- **U10.** `internal/cli/playbook_init.go` embed-FS auto-install +
  4 Greptile round-4 hygiene fixes (sentinel-only-on-full-success,
  `[amend ` marker -> `PreserveExistingNotes`, ctx propagation,
  per-playbook example fan-out).
- **U11.** `root.go.tmpl` wires `runPlaybookInitOnce` + registers
  `teach-playbook` / `playbook amend` / `playbook list`.
- **U12.** `learn_patterns/extract.go` multi-entity generalization +
  `anyMulti` guard.
- **U13.** `skill.md.tmpl` Step 5/6 decision tree + PII discipline.

## Multi-entity recipes generalization (U12 note)

`learn_patterns/extract.go.tmpl` is now domain-neutral: it handles the
single-entity-per-query family AND the multi-entity case via the
`anyMulti` guard, replacing the prediction-goat-specific
hand-port. Verified by `scripts/verify-learn-purity.sh` (passes
on 41 learn templates).

## Greptile-found correctness fixes carried forward

All 8 from ESPN PR #851 rounds 2-4:

1. case-1 `entitySlicesIntersect` guard (cross-alias false-positive).
2. case-2 `entitySlicesIntersect` guard (same-entity admit).
3. Case-insensitive comparison in the entity helper.
4. Atomic `AppendPlaybookNotes` under writeMu (8-goroutine concurrent
   append test passes).
5. `ListPlaybooks` filters `__`-prefixed sentinel rows; `GetPlaybookByFamily`
   still resolves them.
6. `ResolveSlots` candidate pool restricted to `normalized.Entities` only
   (non-entity tokens that coincidentally resolve through `entity_lookups`
   cannot steal slots).
7. Partial-update CASE in `UpsertPlaybook` (empty `NotesText` leaves
   stored notes intact).
8. `[amend ` marker in stored notes triggers `PreserveExistingNotes: true`
   in the install path; sentinel writes ONLY on full success
   (next install retries on partial failure).

`rows.Err()` checks on canonical-resolve + pragma-columns paths
(implicit in the round-2 cache-poison fix) are also in.

## Verification matrix (U14)

| Gate | Result |
| ---- | ------ |
| `scripts/golden.sh verify` | 25/25 PASS |
| `scripts/verify-generator-output.sh` (5 cases) | 5/5 PASS (tidy + build) |
| `scripts/verify-learn-purity.sh` | 41/41 templates PASS |
| `go test ./...` | 6006 passed in 40 packages |
| Synthesized `learn-loop-example` `go test -race ./...` | 512 passed in 16 packages |
| Synthesized binary `--version` | PASS (`learn-loop-example-pp-cli 1.0.0`) |
| Synthesized `teach --help` integrated flags | PASS (`--playbook-file`, `--playbook-notes-file`, `--playbook-notes` all present) |
| Synthesized `teach-playbook --help` | PASS |
| Synthesized `playbook --help` (amend + list) | PASS |
| Synthesized `playbook list` on fresh DB | PASS (`[]`, sentinel filter working) |
| Synthesized `recall` envelope on fresh DB | PASS (`found: false`, `results: []`, playbook nil, notes empty) |
| Synthesized `internal/cli/playbooks/embed.go` directive | PASS (`//go:embed *.md` with MANIFEST.md only; comment guides authors to `*.json *.md` when content lands) |

Synthesized playbook-init test coverage exercises the JSON-content path
via injected `fstest.MapFS` (`TestPlaybookInit_SeedsAllPlaybooks`,
`TestPlaybookInit_Idempotent`, `TestPlaybookInit_ConcurrentSafe`,
`TestPlaybookInit_ReseedReplacesNotesWithoutAmend`,
`TestPlaybookInit_ReseedPreservesNotesWithAmend`,
`TestPlaybookInit_AmendMarkerSpecificity`,
`TestPlaybookInit_SkipsPlaybookWithoutExamples`,
`TestPlaybookInit_FailureLeavesSentinelStale`,
`TestPlaybookInit_HonorsContextCancel`). No new fixture needed; existing
`learn-loop-example` spec + `fstest.MapFS` injection in the synthesized
tests cover both the empty-embed and JSON-content code paths.

## Deviations from the plan worth noting

None of substance. Each unit landed on its planned shape; the test-first
discipline called for in U2-U5 / U10 was applied unit-by-unit. No
template-text drift, no compile failures discovered late, no
post-merge Greptile-equivalent regressions surfaced by the dogfood
verification.

## Recommended next steps

- Per-CLI backports to already-published library entries are downstream
  follow-on work (the library-side sweep tool — see AGENTS.md "Cross-repo
  dependency: published-library sweep tool" — needs a parallel update so
  existing published CLIs can be retrofitted to the new
  `internal/learn` + `internal/cli/playbooks` shape without manual
  per-CLI work).
- Fresh prints from this generator now produce CLIs born at 4-5/5
  Greptile equivalent. No per-CLI hand-port required for the loop.
- The branch is intentionally not pushed; merge cadence is the user's
  call.
