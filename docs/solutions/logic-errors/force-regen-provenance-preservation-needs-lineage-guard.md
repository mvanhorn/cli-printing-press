---
title: "Force regen provenance preservation needs a same-lineage guard"
date: 2026-05-25
category: logic-errors
module: cli-printing-press-generator
problem_type: logic_error
component: tooling
symptoms:
  - "`generate --force` preserved hand-edits but dropped `.manuscripts/` provenance records from promoted CLI directories"
  - "Fresh `.printing-press.json` writes removed promoted manifest metadata such as `category`, `novel_features`, run IDs, and printer attribution"
  - "Naive preservation could resurrect metadata from a different API when force-regenerating into the same output path"
root_cause: logic_error
resolution_type: code_fix
severity: high
tags:
  - force-regen
  - manifest-preservation
  - same-lineage
  - manuscripts
  - provenance
related_components:
  - regenmerge
  - manifest
  - publish
---

# Force regen provenance preservation needs a same-lineage guard

## Problem

`generate --force` used a snapshot plus fresh-generation merge to preserve hand-authored CLI code, but it did not treat provenance as first-class state. Promoted CLIs could lose `.manuscripts/<run-id>/` records and manifest metadata needed by publish validation, creating a loop where the recommended canonical-drift fix made the next publish step fail.

## Symptoms

- `.manuscripts/<run-id>/research.json`, phase 5 acceptance, proofs, and briefing context disappeared after force regen.
- `.printing-press.json` was rewritten from fresh generation data and dropped promoted metadata such as `category` and `novel_features`.
- Preserving those fields without a lineage guard introduced the opposite failure: stale run IDs, attribution, category, or feature metadata from a previous API could be copied into a different fresh generation.

## What Didn't Work

- Preserving only source-like non-classified files was insufficient because `.manuscripts/` is hidden and the regen sweep skipped hidden directories.
- Rewriting the manifest from a fresh `CLIManifest` struct fixed generated fields but erased operator-added or publish-synced metadata.
- Blindly merging every old manifest key into the fresh manifest was unsafe because `--force` can be used against a different API or spec. Provenance preservation is only valid when the old and fresh manifests describe the same lineage.

## Solution

Treat provenance preservation as a same-lineage merge:

- Let `regenmerge.MergeIntoFreshTree` walk `.manuscripts/` despite the hidden-directory skip, and copy files with a streaming atomic helper so large research artifacts do not get loaded entirely into memory.
- In `WriteManifestForGenerate`, read the existing manifest after the force snapshot has been merged back.
- Preserve known durable fields only when the existing manifest and fresh manifest have the same `api_name` and, when available, matching `spec_checksum`, `spec_url`, or `spec_path`.
- Overlay fresh manifest fields onto the existing raw manifest instead of deleting every known JSON key. This keeps same-lineage known fields that fresh generation omits because of `omitempty`.
- Use explicit-clear signals for fields where empty has meaning, such as `novel_features` when dogfood produced an explicit empty built list.

The practical boundary is:

```go
preserveExisting := hasExisting && sameGenerateManifestLineage(existing, fresh)

if preserveExisting && p.RunID == "" && existing.RunID != "" {
    fresh.RunID = existing.RunID
}
if preserveExisting && p.NovelFeatures == nil {
    fresh.NovelFeatures = existing.NovelFeatures
}
if preserveExisting && p.NovelFeatures != nil && len(p.NovelFeatures) == 0 {
    clearFields["novel_features"] = struct{}{}
}
```

For cross-lineage force regen, discard the old raw manifest extras and write a clean fresh manifest. That avoids stale metadata resurrection while still allowing same-CLI regens to keep publish-critical provenance.

## Why This Works

The force-regen snapshot is a recovery source, not an authority by itself. `.manuscripts/` is safe to copy back because it is provenance under the same output tree, but manifest values need a stricter rule: preserve when they describe the same generated CLI lineage, replace or clear when fresh generation has an explicit answer, and drop when the old manifest belongs to a different API/spec.

This also keeps side artifacts coherent. The manifest writer updates the in-memory `CLIManifest` for preserved run ID, owner, printer, category, description, and novel features before writing sibling artifacts such as MCPB metadata, so the JSON on disk and generated sidecars do not disagree.

## Prevention

- Regression tests for force-regen preservation should include both files and metadata: `.manuscripts/` content/mode/mtime, `.printing-press.json` category, novel features, run ID, owner, printer, and unknown top-level keys.
- Add reciprocal tests for replacement and clearing, not just preservation. Fresh category or non-empty novel features must replace stale manifest values; explicit empty built features must clear stale features.
- Include a cross-API or cross-spec test proving stale manifest extras are not carried forward.
- For any future preserved manifest field, decide whether empty means "unavailable, preserve existing" or "fresh result is explicitly empty, clear existing" and encode that distinction in tests.

## Related Issues

- Issue #1976: force regen wiped `.manuscripts/` and manifest extras in promoted CLIs.
- [Snapshot merge with AST classifier for force regen](../design-patterns/snapshot-merge-with-ast-classifier-for-force-regen-2026-05-10.md)
- [Existing manifest wins over re-derivation for identity fields in regen paths](../conventions/manifest-wins-over-re-derivation-for-identity-fields-in-regen-paths-2026-05-12.md)
