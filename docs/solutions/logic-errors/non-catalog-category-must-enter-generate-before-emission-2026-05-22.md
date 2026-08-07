---
title: Non-catalog category must enter generate before emission
date: 2026-05-22
category: logic-errors
module: cli-printing-press generate and printing-press skill
problem_type: logic_error
component: tooling
symptoms:
  - Non-catalog CLIs could publish with a manifest category added after generation
  - Generated SKILL install sections could drift from manifest-derived canonical expectations
root_cause: missing_workflow_step
resolution_type: code_fix
severity: high
tags: [category, manifest, skill, generator, publish]
---

# Non-catalog category must enter generate before emission

## Problem
Non-catalog runs learn the API domain during research, but that category must reach generation before the generator writes the manifest, README, and SKILL install section. Adding a category later to satisfy publish can make those emitted surfaces disagree and trigger canonical-section failures.

## Symptoms
- Browser-sniffed, HAR, docs-derived, or hand-authored internal-spec CLIs can produce `.printing-press.json` without `category`.
- A post-generation manifest edit can satisfy publish but leave `SKILL.md` with the category-agnostic install fallback.
- `verify-skill canonical-sections` then compares the category-aware manifest expectation to the category-agnostic generated SKILL section and fails.

## What Didn't Work
- Treating this only as a skill prose issue missed docs-only generation. Direct `generate --docs` builds an `APISpec` in memory, so there may be no editable spec artifact where an agent can insert `category:` before emission.
- Letting a user-provided category override catalog metadata recreated the same drift in the opposite direction: generated surfaces could use the flag value while manifest writing still preferred the embedded catalog category.

## Solution
Make category a generation-time input and keep catalog metadata authoritative:

- Add `generate --category <catalog-category>` for non-catalog runs that do not already have `category:` authored into an editable spec.
- Validate the flag against the public catalog category enum at the CLI boundary.
- Apply the category before templates, tools manifests, and `.printing-press.json` generation read the spec.
- Keep embedded catalog categories authoritative by letting catalog enrichment overwrite any supplied category when a catalog entry matches.
- Update the Printing Press skill's Phase 2 command blocks so non-catalog examples carry `--category <catalog-category>`, while catalog-config runs omit it.

## Why This Works
The generated manifest, README, and SKILL are all downstream of the same `APISpec`. Supplying category before `runGenerateProject` means every emitted surface sees one value. Catalog enrichment stays the highest-precedence source for catalog entries, so a non-catalog convenience flag cannot split generated install prose from manifest-derived verification expectations.

## Prevention
- When a value affects generated files and publish metadata, route it into the generator before emission rather than patching one artifact afterward.
- Regression tests should cover both sides of precedence: non-catalog generation accepts a category input, and catalog enrichment still wins over a conflicting supplied value.
- Skill command blocks need the same contract as prose. If the prose says a workflow value is required, the runnable examples should show the flag or explain when to omit it.

## Related Issues
- GitHub issue #1712
- `docs/solutions/conventions/manifest-wins-over-re-derivation-for-identity-fields-in-regen-paths-2026-05-12.md`
- `docs/solutions/best-practices/cross-repo-coordination-with-printing-press-library-2026-05-06.md`
