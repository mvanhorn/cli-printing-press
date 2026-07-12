---
title: "Verify-mode HTTP-verb short-circuit: rolling out to printing-press-library"
date: 2026-05-13
category: best-practices
module: printing-press-library-sweep
problem_type: best_practice
component: tooling
severity: medium
applies_when:
  - "Rolling out docs/plans/2026-05-13-001 (verify-mode short-circuit) to printed CLIs in printing-press-library"
  - "Performing any cross-CLI sweep that depends on regen-merge"
tags:
  - cross-repo
  - regen-merge
  - rollout
  - verify-mode
  - tier-prioritization
related_components:
  - tooling
  - documentation
  - templates
---

# Verify-mode HTTP-verb short-circuit: rolling out to printing-press-library

## Context

`docs/plans/2026-05-13-001-fix-verify-mode-short-circuit-mutating-http-plan.md` introduces a transport-layer gate that short-circuits mutating HTTP verbs (DELETE/POST/PUT/PATCH) under `PRINTING_PRESS_VERIFY=1`. The gate lives in the generator template `internal/generator/templates/client.go.tmpl` plus a new helper in `cliutil_verifyenv.go.tmpl`. Every printed CLI inherits the gate at its next regen, but `printing-press-library` has no library-wide regen command — each CLI must be regenerated and PR'd individually via `printing-press regen-merge`.

This playbook captures the order, mechanics, and known foot-guns so a maintainer can execute the sweep without back-channel questions.

## Prerequisites

1. **A new `printing-press` binary that includes commit `5701f692` (U1+U2+U4), `7b9bbbf1` (U3), and the rest of the verify-mode-plan chain.** Easiest path: pull `cli-printing-press` `fix/verify-mode-http-gate` post-merge, then `go install ./cmd/printing-press@<tag>`.
2. **macOS or Linux.** `printing-press regen-merge --help` currently warns that Windows is not supported (signed-attribute path semantics + the `os.Symlink` calls in the merge layer). A Windows-based maintainer will need WSL, a Linux VM, or to hand the sweep to a Linux-capable colleague.
3. **A local clone of `mvanhorn/printing-press-library` with `main` up to date.** The sweep runs per-CLI from this clone's root.
4. **Verify env CLEAN.** Before running the sweep, confirm `echo "$PRINTING_PRESS_VERIFY $PRINTING_PRESS_VERIFY_LIVE_HTTP"` prints two empty strings. The live verifiers landed in U10 strip both vars from subprocess env, so an inherited value cannot silently noop the destructive paths — but the operator-visible behavior of `printing-press verify` and `printing-press regen-merge` still depends on these being unset.

## Order: tier:official first

The sweep is tier-prioritized for two reasons:

- **Tier:official CLIs see more review attention.** Bugs surface fast on these; you want them in the bag before working through the long tail.
- **Smaller blast radius.** Tier:official is a fixed-name set (~12 CLIs); tier:community is a churning set. Landing tier:official first lets you stabilize the recipe before the volume hits.

Read the tier per CLI from `library/<cat>/<api>/.printing-press.json` or the per-CLI `printing-press.toml`. Sweep tier:official first, tier:community second.

## Per-CLI recipe

For each CLI directory under `printing-press-library/library/<category>/<api>/`:

1. **Materialize a fresh template tree to a temp dir.**

   ```bash
   tmp=$(mktemp -d)
   printing-press print "$api" \
       --spec library/<cat>/<api>/spec.yaml \
       --output "$tmp"
   ```

2. **Run regen-merge with `--apply`.**

   ```bash
   printing-press regen-merge \
       library/<cat>/<api> \
       --fresh "$tmp" \
       --apply
   ```

   Watch the verdict table. Every file should classify as `TEMPLATED-CLEAN`, `TEMPLATED-WITH-ADDITIONS`, or `NEW-TEMPLATE-EMISSION`. The fixture pinned in `internal/pipeline/regenmerge/testdata/verify-short-circuit/` (U7) confirms `internal/client/client.go` resolves to `TEMPLATED-WITH-ADDITIONS` when the operator has hand-edited it; a `TEMPLATED-VALUE-DRIFT` verdict on `client.go` is a regression — stop the sweep and investigate.

3. **Run the CLI's tests.**

   ```bash
   cd library/<cat>/<api>
   go test ./...
   ```

   The U5 emitted-test template adds `internal/client/client_verify_short_circuit_test.go` to every regenerated CLI; this is where verify-mode behavior is pinned at the printed-CLI level. A failure here is a real regression, not a sweep glitch.

4. **Commit + PR.**

   ```bash
   git checkout -b sweep/verify-mode-<api>-2026-05-13
   git add library/<cat>/<api> cli-skills/pp-<api>  # mirror parity, see below
   git commit -m "regen(cli): apply verify-mode short-circuit to <api>"
   gh pr create --base main --title "regen(<api>): apply verify-mode HTTP-verb short-circuit"
   ```

   PR body should reference `docs/plans/2026-05-13-001` so reviewers can find context.

## Mirror parity (don't forget cli-skills/)

The library has a `cli-skills` parity check that runs on every PR — see `docs/solutions/best-practices/cross-repo-coordination-with-printing-press-library-2026-05-06.md`. When the SKILL.md changes between regen passes (uncommon for this plan, but possible if a CLI's verify-related help text moves), run the mirror generator and stage the diff:

```bash
go run ./tools/generate-skills/main.go
git add cli-skills/pp-<api>
```

## Batch vs one-PR-per-CLI

Two viable strategies:

- **One PR per CLI** — clean review, easy revert, but ~20+ PRs. Use when CLIs differ enough that reviewers want each diff in isolation.
- **One PR per tier** — fewer PRs, but the diff is large. Use when the regen-merge diffs look mechanical and reviewers are comfortable scanning bulk.

For this rollout: lean toward **one PR per tier**. The verify-mode gate change emits an identical block into every CLI's `client.go`, so reviewing 12 nearly-identical diffs in 12 PRs is busywork. The exception is any CLI whose regen-merge surfaces a `TEMPLATED-WITH-ADDITIONS` verdict — those deserve their own PR so the preserved-additions delta gets explicit review.

## Known foot-guns

### `PRINTING_PRESS_VERIFY=1` in the operator's shell

If the maintainer running the sweep has `PRINTING_PRESS_VERIFY=1` set in their shell (parent process, CI runner, container image), the U10 env-strip in `live_dogfood` and `workflow_verify` neutralizes the silent-noop risk for live verification — but `printing-press verify` itself and any ad-hoc `go test ./...` runs against the regenerated CLI will see the inherited var. Mutating-verb tests under the inherited env will short-circuit through the synthetic envelope and pass — but they won't be exercising the real wire path. **Unset both env vars before running the sweep.**

### Hand-edited `client.go`

The U7 fixture pins `TEMPLATED-WITH-ADDITIONS` for the canonical hand-edit case (operator added a top-level helper method on `*Client`). Any other shape of hand-edit — particularly INSIDE the existing `do()` method body — will currently classify as `TEMPLATED-CLEAN` because the decl-set comparison doesn't see function-body changes. The merge layer will overwrite the operator's in-function edit silently.

Mitigation: before running `regen-merge --apply`, grep each CLI's `client.go` for non-template additions:

```bash
diff -u \
    <(printing-press print "$api" --spec library/<cat>/<api>/spec.yaml --output - 2>/dev/null | head -n 500) \
    library/<cat>/<api>/internal/client/client.go
```

Any diff that isn't the new short-circuit block deserves a manual review before `--apply`.

### tier:community sprawl

`printing-press-library/library/community/` has CLIs with widely varying maturity. Some have no tests; some have a single happy-path smoke. The emitted U5 test will compile and run on all of them (it uses stdlib + the `client` package only), but a CLI that ships pre-existing test compilation failures will surface those failures on first regen. Triage:

1. If the failure is pre-existing (compile error, missing test file, etc.) — open a separate cleanup PR for that CLI; do not block the verify-mode rollout on it.
2. If the failure is the new U5 test itself — the gate has regressed for that CLI's config; investigate (likely a generator-template variable mismatch).

## Phase R3: smoke re-verification

After the printing-press-library sweep, re-run the matrix `petstore` smoke pipeline (the original B1 surface) against a regenerated CLI:

```bash
PRINTING_PRESS_VERIFY=1 petstore-pp-cli pet delete 42 --json
```

Expected output: outer envelope with `verify_noop: true` and `success: false`, inner `data` carrying `__pp_verify_synthetic__: true` and `reason: "verify_short_circuit"`. No network call attempted. This is the agent-readiness re-test that confirms B1 stays closed end-to-end.

If the re-test passes, the rollout is complete.
