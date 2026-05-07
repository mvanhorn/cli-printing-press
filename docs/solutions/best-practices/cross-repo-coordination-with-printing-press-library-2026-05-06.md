---
title: "Cross-repo coordination with printing-press-library: invisible CI couplings when generator templates or schemas change"
date: 2026-05-06
category: best-practices
module: cli-printing-press-generator
problem_type: best_practice
component: tooling
severity: medium
applies_when:
  - "Changing internal/generator/templates/skill.md.tmpl, readme.md.tmpl, or other agent-readable templates"
  - "Adding new install/usage instructions that embed commands from external tools (npm, hermes, claude mcp, go install, brew, etc.)"
  - "Changing the canonical shape of library/<cat>/<api>/SKILL.md or library/<cat>/<api>/README.md content"
  - "Working in printing-press-library and seeing the cli-skills mirror parity check fail or the verify-skill script fail across multiple CLIs at once"
tags:
  - cross-repo
  - generator-templates
  - mirror
  - validator
  - allowlist
  - ci-coupling
related_components:
  - tooling
  - documentation
  - templates
---

# Cross-repo coordination with printing-press-library: invisible CI couplings when generator templates or schemas change

## Context

> **Cross-repo coupling.** The validator and mirror-regeneration workflows referenced here live in [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library). The lesson is filed in `cli-printing-press` (this repo) because it's *our* template / schema changes that break those downstream workflows — the failure is invisible from this side until the next library regen runs.

This repo (`cli-printing-press`) is the upstream generator for printed CLIs. The output flows downstream to `printing-press-library` where the CLIs are published. The library runs CI checks and an auto-regeneration workflow that depend on assumptions about the upstream's output shape. When we change those assumptions without coordinating, downstream CI fails — often across every CLI at once — and the failure messages don't point at the upstream change as the cause.

Two known coupling points have produced this failure mode. Both surfaced during the Hermes / OpenClaw frontmatter alignment work. Future template / schema changes are likely to produce more, and the pattern is what to remember.

## The two known couplings

### 1. cli-skills mirror parity

The library has two trees that must stay byte-identical:

- `library/<cat>/<api>/SKILL.md` — canonical content
- `cli-skills/pp-<api>/SKILL.md` — flat-namespace mirror, used by `npx skills add` and Hermes / OpenClaw install paths

`tools/generate-skills/main.go` (in printing-press-library) is a verbatim mirror tool that copies the source to the mirror. `.github/workflows/generate-skills.yml` runs it on push to main. **It does not run on pull request.**

Separately, a `cli-skills mirror parity` check runs on every PR to verify that the committed `cli-skills/` matches what the generator would produce from the current `library/`. This catches drift.

The asymmetry: PRs verify, but don't regenerate. So a PR that mutates `library/<cat>/<api>/SKILL.md` (intentionally — e.g., the Hermes-alignment sweep) without also regenerating the mirror fails parity. The PR author has to run the mirror generator locally and commit the result.

The Hermes-alignment sweep (`printing-press-library#267`) hit this directly:

```
##[error]cli-skills/ is out of sync with what tools/generate-skills produces from library/<cat>/<slug>/SKILL.md.
##[error]Run `go run ./tools/generate-skills/main.go` locally and commit the result.

Out-of-sync entries:
 M cli-skills/pp-agent-capture/SKILL.md
 M cli-skills/pp-ahrefs/SKILL.md
 ... (47 more)
```

### 2. Flag-validator allowlist

`printing-press-library/.github/scripts/verify-skill/verify_skill.py` runs on every PR that touches `library/**/SKILL.md`. One of its checks: every `--flag` mentioned in a SKILL.md should be declared in that CLI's `internal/cli/*.go` source. This catches typos in real CLI flag mentions.

Our SKILL.md template embeds install commands from other tools:

```bash
npx -y @mvanhorn/printing-press install mercury --cli-only
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-mercury --force
```

`--cli-only` is a flag of the npm installer, not of `mercury-pp-cli`. `--force` is a flag of the hermes CLI. The validator can't distinguish these from real CLI flag mentions, so it fails — across every CLI in the library — when a new external-tool flag appears in the template.

The validator has a `COMMON_FLAGS` allowlist for "flags that are OK to mention without source declaration." Adding a new install instruction that uses a new external flag means adding that flag to the allowlist in the same change.

The Hermes-alignment template change (`cli-printing-press#655`) added `--cli-only` and didn't allowlist it; the next library regen failed all 49 CLIs:

```
=== shopify ===
  ✘ 1 error(s), 0 likely false-positive(s)
    [flag-names] (any): --cli-only is referenced in SKILL.md but not declared in any internal/cli/*.go
```

## Pre-flight checklist before merging a generator template change

When changing templates in this repo:

1. **Did the change add or modify install / usage commands?**
   Scan the new commands for `--flag` tokens. For each flag belonging to a tool other than the printed CLI:
   - Verify it's in `printing-press-library/.github/scripts/verify-skill/verify_skill.py`'s `COMMON_FLAGS` set
   - If not, add it in the same change (or a coordinated companion PR that lands first)
   - Currently allowlisted: `cli-only`, `skill-only`, `registry-url` (npm installer), `force` (hermes)

2. **Did the change alter the shape of `library/<cat>/<api>/SKILL.md` or `library/<cat>/<api>/README.md` content?**
   When library content gets regenerated downstream (via a sweep tool, manual reprint, or normal regen), the regeneration must include `tools/generate-skills/main.go` running once and the resulting `cli-skills/` updates committed in the same PR. The PR-side parity check verifies committed state; the auto-mirror workflow runs only on push to main and won't fix the PR for you.

3. **Did the change alter the schema of `.printing-press.json` or `registry.json`?**
   Downstream tools that read the manifest may need updates. Audit any references in:
   - `printing-press-library/tools/` (mirror tool, registry generator, sweep tools)
   - `printing-press-library/.github/scripts/` (verify-skill validator, others)
   - The `npm/` package's resolver code (it reads the catalog)

4. **Did the change touch a verifier rule (golden, scorecard, dogfood)?**
   These are caught by this repo's CI, not downstream's. Run `scripts/golden.sh verify` locally and update fixtures explicitly if the change is intentional.

## Why these couple

The pattern is "upstream generates, downstream publishes / mirrors / validates." The downstream's CI checks are calibrated to the upstream's current output shape. When the shape evolves, the calibration drifts.

Two architectural choices make the drift invisible until late:

1. **The downstream CI runs on PRs only when the PR touches files in the downstream repo.** Changes here don't trigger checks there. The drift surfaces on the next downstream content change — usually unrelated to the upstream template change.
2. **The downstream's auto-regen workflow runs only on push to main, not on PR.** The verify check on PRs validates committed state, expecting the PR author to have already regenerated. This is the right shape — auto-regen on PR would create churn — but the gap between "verify on PR" and "regen on push" is the place where uncoordinated changes fail.

The fix shape is consistent: when changing something here that the downstream reads, identify which downstream workflows are calibrated to it, and update both sides in the same logical change. The pre-flight checklist above is the operational form of that.

## When to apply

Always, when:

- Changing files in `internal/generator/templates/`
- Changing the schema of `.printing-press.json` (manifest fields read by downstream tools)
- Changing the schema of catalog entries (read by `printing-press-library` for registry building)
- Changing what flags or sections the printed CLI's `--help` produces in ways that propagate into SKILL.md (the validator's `flag-commands` check reads both)

Skip the cross-repo coordination check when:

- Changing internal generator code that doesn't affect output shape (refactor, simplify, performance)
- Changing tests or test fixtures
- Changing docs in this repo only

## Examples

### Anti-pattern — silent CI gap

Template change in `cli-printing-press` merges. Library regen workflow runs on next push to main. CI fails for every CLI. Investigation:

```
##[error]library/marketing/ahrefs/
=== ahrefs ===
  ✘ 1 error(s)
    [flag-names] (any): --cli-only is referenced in SKILL.md but not declared in any internal/cli/*.go
```

Author of the original template change isn't around. Error message points at `internal/cli/*.go` (a CLI source-tree path) rather than at the template that introduced the flag. Triage takes hours.

### Pattern — coordinated change

Template change in `cli-printing-press` includes:

```diff
 # internal/generator/templates/skill.md.tmpl
+1. Install via the Printing Press installer:
+   ```bash
+   npx -y @mvanhorn/printing-press install {{.Name}} --cli-only
+   ```
```

Same logical change includes (in `printing-press-library`, possibly a coordinated companion PR):

```diff
 # .github/scripts/verify-skill/verify_skill.py
 COMMON_FLAGS = {
     "help", "version", "json", ...
+    "cli-only",   # @mvanhorn/printing-press install ... --cli-only
 }
```

Order: validator-allowlist update lands FIRST, template change lands SECOND. If they land out of order, the gap window has every library regen failing.

### Pattern — local mirror regen before PR

```bash
# In printing-press-library, after making any change to library/<cat>/<api>/SKILL.md or README.md:
go run ./tools/generate-skills/main.go
git add library/ cli-skills/
git commit -m "..."
git push
```

One commit, one CI cycle. Without the local regen, you get three commits (source change + mirror catch-up + maybe an additional fixup) and two CI cycles minimum.

## Related

- `printing-press-library/.github/workflows/generate-skills.yml` (cross-repo) — the auto-mirror workflow + its `paths:` triggers
- `printing-press-library/tools/generate-skills/main.go` (cross-repo) — the verbatim mirror generator
- `printing-press-library/.github/scripts/verify-skill/verify_skill.py` (cross-repo) — the validator + `COMMON_FLAGS` allowlist
- `printing-press-library/AGENTS.md` (cross-repo) — has matching pre-flight notes for the validator-allowlist coupling
- `docs/plans/2026-05-06-002-feat-hermes-openclaw-frontmatter-alignment-plan.md` — the plan that surfaced both couplings
