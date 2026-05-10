---
title: "Force regen as snapshot+merge with AST-aware classification: preserve hand-edits to templated files without trusting heuristics across specs"
date: 2026-05-10
category: design-patterns
module: cli-printing-press-generator
problem_type: design_pattern
component: tooling
severity: high
applies_when:
  - "A generator's --force flow rewrites templated files in place and risks clobbering hand-edits the user wants preserved"
  - "An AST-aware classifier already exists for the merge path but the destructive regen path bypasses it"
  - "Hand-edits can take the form of literal-value drift or selector-identifier renames inside otherwise-templated decls, not just decl-set additions or call-body changes"
  - "Preservation only makes sense when the spec identity is unchanged — a snapshot from a different spec must not seed a merge"
  - "Mid-merge failure must leave the working tree recoverable, ruling out preserve-files-in-memory + overwrite-then-restore"
tags:
  - force-regen
  - snapshot-merge
  - ast-classifier
  - templated-value-drift
  - selector-identifier-drift
  - cross-spec-guard
  - verdict-switch-hygiene
  - go-format-canonicalization
related_components:
  - generator
  - templates
  - regenmerge
  - tooling
---

# Robust regen pipelines for templated CLIs

## Context

The Printing Press generates printed CLIs from API specs, but printed CLIs are not write-once artifacts — users hand-edit templated files (`internal/client/`, `internal/config/`, parent-command `AddCommand` extensions in `internal/cli/<resource>.go`) and re-run `generate --force` to pick up spec changes. Issue [#907](https://github.com/mvanhorn/cli-printing-press/issues/907) exposed that `--force` was clobbering those hand-edits silently. The original "preserve-restore" approach (PR #897, commit `dceb6e58`) was an under-engineered classifier wrapped in a destructive rename with no recovery path, and the AST classifier itself had blind spots for literal drift and selector renames.

This doc captures the cluster of patterns that emerged from fixing it — they generalize to any pipeline where templates and human edits coexist on the same file tree.

## Guidance

### Snapshot + merge architecture, not preserve-restore

Destructive regen needs an atomic boundary and a recovery path. The new flow in `internal/cli/root.go` (`claimOrForce` → `snapshotForceRegen` → fresh `Generate` → `mergeForceSnapshot`) does three things the old flow didn't:

1. **Snapshot to a sibling tempdir** under the same parent directory, so the rename is an atomic same-filesystem operation.
2. **Refuse symlinks pre-mutation** via `refuseSymlinksUnderForceRegenTree` — multiple `lstat` checks before any rename. Refusing *after* the destructive rename leaves the user with no `<absOut>` directory at all.
3. **Provide a real recovery path** if merge fails mid-way: the snapshot is still on disk, paths are logged, the user can recover.

This carries forward PR #897's "fail before mutating" contract. The general pattern: **identify every reason to bail, check them all before the destructive boundary, and keep the snapshot recoverable until merge succeeds.**

### AST classification needs more than decl-sets and body-drift

The original classifier compared declaration *sets* and detected body drift, which caught added/removed functions and changed function bodies. It missed two real-world hand-edit shapes:

- **Literal drift** — user changes `"Bearer "` to `"Token "` in a header builder.
- **Selector-rename drift** — user changes `cfg.Bearer` to `cfg.Token`.

Both fell through to TEMPLATED-CLEAN and got silently overwritten. The fix in `internal/pipeline/regenmerge/value_drift.go` adds a per-declaration canonical-text compare:

```go
// detectValueDrift compares each shared decl's canonical text.
// canonicalRender strips Doc comments and uses go/format,
// then round-trips through format.Source for further canonicalization.
// stripAddCommandStmts removes parent-command AddCommand statements
// before comparing, since those are user-extensible by design.
```

A subtle implementation gotcha: `go/printer` leaks position info, `go/format` is closer to canonical, and round-tripping through `format.Source` on a synthetic package wrapper canonicalizes further. For AST-text comparisons where you need byte-equal output regardless of source whitespace, use the round-trip — `printer.Fprint` alone will give you false positives on whitespace.

The new TEMPLATED-VALUE-DRIFT verdict slots into `decideBothPresent` after body-drift in `internal/pipeline/regenmerge/classify.go`. The general pattern: **when a verdict's name promises a guarantee ("templated, clean of hand-edits"), the detection must cover every shape of hand-edit, not just the ones that motivated the original design.**

### Cross-spec guard via recorded spec hash

Heuristic preservation is only valid when the snapshot and fresh tree describe the *same* spec. If a user runs `--force` against a different spec, body-drift detection becomes meaningless — every decl looks "drifted." `forceRegenSpecHashMatches` in `root.go` compares the snapshot's recorded `spec_sha256` to the freshly computed hash; on mismatch, we fall back to NOVEL-only preservation (preserve user-authored files but don't try to merge into templated ones).

The general pattern: **structural-similarity heuristics need a guard that confirms the structures are comparable in the first place.**

### Verdict switches with a default-error arm

Both `internal/pipeline/regenmerge/apply.go` and `merge_into_fresh.go` now have switch statements that include:

```go
default:
    return fmt.Errorf("unhandled verdict %q for %s", v.Kind, v.Path)
```

This forces every future verdict addition to explicitly land in both sites. Without it, a new verdict silently no-ops in one branch and you find out by losing user edits in production. The general pattern: **enumerated-state switches in a destructive code path must default to error, never to silent fallthrough.**

### `shouldClassifyFile` is not "all the files we care about"

The classifier only walks Go and module files. Non-Go user-edited files — README, Makefile, `.printing-press.json` — were getting dropped because nothing claimed them. `MergeIntoFreshTree` now has an explicit sweep step that handles non-classified files from the snapshot. The general pattern: **when adding a classifier, enumerate every file shape the snapshot might contain, and either classify it or explicitly sweep it.**

### Preserve syntactically broken hand-edits

A user mid-edit may save a file that doesn't parse. The conservative default in the classifier is to treat parse failures as TEMPLATED-WITH-ADDITIONS rather than discarding the file. The user can fix the broken file after seeing it; they cannot recover work the regen silently dropped. The general pattern: **when uncertain, preserve user bytes and surface the file for review.**

### Multiple review lenses catch different failures

During plan review, the **adversarial lens** caught the selector-identifier drift gap that the **feasibility lens** missed; feasibility caught the README/Makefile non-classified-file gap that adversarial missed. Lens diversity matters specifically when a verdict's name (TEMPLATED-CLEAN) promises broader coverage than its detection delivers. The general pattern: **for guarantees that span multiple file shapes and edit shapes, run at least two review lenses with different failure-mode priors.**

## Why This Matters

Silent data loss is the worst failure mode for a regen tool. Users build trust by running `--force` once, seeing their edits survive, and assuming the contract holds. The first time it doesn't, trust is gone — and they may not notice until much later, when reconstructing the lost edits is expensive. Every pattern above either prevents silent loss (snapshot, symlink refusal, default-error verdicts, broken-file preservation) or strengthens the detection that decides whether loss is happening (value-drift, cross-spec guard, classifier sweeps).

## When to Apply

- Any pipeline that regenerates over a tree the user can edit between runs.
- Any AST-comparison code where "files match" is a binary you act on destructively.
- Any switch on an enumerated kind/verdict where missing cases would silently no-op in a destructive branch.
- Any "atomic replace" operation where partial failure leaves the user worse than the start state.

## Examples

**Before** (preserve-restore, conceptually):

```go
preserveUserFiles(out)   // copies into memory
generateFresh(out)       // overwrites everything
restoreUserFiles(out)    // best-effort, no recovery if step 2 partially fails
```

**After** (snapshot + merge, from `internal/cli/root.go`):

```go
snap, err := snapshotForceRegen(absOut)         // sibling tempdir, atomic rename
if err := refuseSymlinksUnderForceRegenTree(snap); err != nil { ... }
if err := generateFresh(absOut); err != nil { ... } // snap still recoverable
if !forceRegenSpecHashMatches(snap, freshSpec) {
    mergeNovelOnly(snap, absOut)                // cross-spec fallback
} else {
    mergeForceSnapshot(snap, absOut)            // full classifier merge
}
```

**Verdict switch with default-error**, from `apply.go`:

```go
switch v.Kind {
case VerdictNovel:               return applyNovel(...)
case VerdictTemplatedClean:      return nil
case VerdictTemplatedWithAdds:   return applyAdditions(...)
case VerdictTemplatedValueDrift: return applyValueDrift(...)
default:
    return fmt.Errorf("unhandled verdict %q for %s", v.Kind, v.Path)
}
```

## Related

- Plan: [docs/plans/2026-05-10-001-fix-force-regen-preserves-templated-hand-edits-plan.md](../../plans/2026-05-10-001-fix-force-regen-preserves-templated-hand-edits-plan.md) — full design context, alternatives considered, risk register.
- Issue #907 — original report: `--force` preserve-list too narrow; clobbers `internal/client`, `internal/config`, parent-command extensions.
- PR #897 (`dceb6e58`) — preceding fix that introduced the preserve-restore approach this doc supersedes. Established the "fail before mutating" symlink-refusal contract that snapshot+merge inherits.
- [docs/solutions/design-patterns/avoid-classification-when-failure-is-asymmetric-2026-05-06.md](avoid-classification-when-failure-is-asymmetric-2026-05-06.md) — contrast case. That doc rejects classifiers when failure is asymmetric. This doc ships a classifier — but only after extending it to be safe (verdict-switch fail-loud, conservative-on-parse-error, cross-spec guard). Together they bracket "when to classify and when not to."
- [docs/solutions/conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md](../conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md) — sibling preserve-on-regen pattern for attribution metadata.
- [docs/solutions/conventions/soft-validation-in-reusable-library-packages-2026-05-06.md](../conventions/soft-validation-in-reusable-library-packages-2026-05-06.md) — companion fail-soft pattern. The verdict-switch fail-loud rule here is the inverse trade-off for a different decision point: warn-and-fallback when the input is upstream and partially populated; fail-loud when the path is destructive and the verdict universe must stay closed.
- [docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md](../best-practices/validation-must-not-mutate-source-directory-2026-03-29.md) — earlier mutation-safety precedent. The symlink-refusal-before-mutation rule here is a parallel "don't mutate before you know it's safe" rule in the regen layer.
