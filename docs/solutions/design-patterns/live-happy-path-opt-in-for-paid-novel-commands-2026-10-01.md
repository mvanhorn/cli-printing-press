---
title: Let operators approve one real run of a paid novel command so coverage is not hollow forever
date: 2026-10-01
category: design-patterns
module: live_dogfood
problem_type: design_pattern
component: testing_framework
severity: medium
applies_when:
  - "A novel feature is a paid generation, upload, or local file write"
  - "Live dogfood forces --dry-run on it, so the phase5 gate reports hollow coverage"
related_components:
  - pipeline
  - publish
tags:
  - dry-run
  - live-dogfood
  - phase5
---

## Problem

`finalizeLiveDogfoodCoverage` counts a novel feature as executed only when a
non-`--dry-run` happy path passes. Live dogfood dry-runs every mutating command
that advertises `--dry-run`, and `--allow-destructive` does not change that.
So a CLI whose headline features are paid generations (WaveSpeed `run`, `pack`,
`batch`, `compose`, ...) or file writers (`init`, `download`) can never pass the
publish gate, even when the operator is willing to pay for one small real run.
See issues #4539 and #4263.

## Pattern

Two keys, both required, before a mutating command runs live:

1. The command opts in with `pp:live-happy-path: "true"` (the CLI author
   decides it is safe to run once against a real account, and supplies cheap
   `pp:happy-args` / `pp:happy-stdin`).
2. The operator opts in per run with `dogfood --live --allow-destructive`.

When both hold, the happy path drops any `--dry-run` from the example args,
adds `--json` if supported, and runs exactly once from a throwaway working
directory (fixture paths under the CLI dir are made absolute first). JSON
fidelity is judged from that same run, never a second invocation, so a paid
command bills once. Commands without the annotation keep the dry-run default
even under `--allow-destructive`. A read-only command that carries the
annotation already runs for real; the annotation only moves that run into the
scratch directory, so a downloader cannot drop files into the CLI tree.

## Why not just honor --allow-destructive

`--allow-destructive` already means "run destructive-at-auth endpoints and
mutating examples without a preview flag". Widening it to every dry-run-capable
mutator would turn one flag into "spend money on every POST in the tree".
The per-command annotation keeps the blast radius to commands the author
picked and gave cheap inputs.
