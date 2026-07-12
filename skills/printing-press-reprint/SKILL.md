---
name: printing-press-reprint
description: >
  Regenerate an existing printed CLI from scratch under the current Printing
  Press, with prior research, prior novel features, and prior patches
  (post-publish hand-fixes) carried into the writing pipeline as
  reconciliation context rather than dropped on the floor. Pulls the CLI
  from the public library if it isn't local, recommends reuse-vs-redo of
  prior research based on age, then hands off to /printing-press with the
  right context. Use when a machine upgrade would benefit a published CLI
  more than manual polish.
  Trigger phrases: "reprint <api>", "regenerate <api>", "redo the <api> CLI",
  "rebuild <api> from scratch", "this CLI would benefit from a reprint".
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
  - Skill
---

# /printing-press-reprint

Regenerate an existing printed CLI under the current machine. The user gives
a CLI name and (optionally) reasons for the reprint. This skill ensures the
prior CLI is locally present, recommends whether to reuse or redo prior
research, and hands off to `/printing-press` with the context the
novel-features subagent needs to reconcile prior features against the
current machine — keep, reframe, or drop with reasons, never silent.

```bash
/printing-press-reprint notion
/printing-press-reprint cal.com  the new MCP intent surface landed and the prior CLI ships endpoint-mirror only
/printing-press-reprint allrecipes
```

## When to run

- A significant Printing Press upgrade (new MCP surface, new auth modes, new
  transport, scoring rubric changes) would lift this CLI more than manual
  polish.
- The published CLI ships with a known systemic gap a reprint would fix.
- The user wants prior novel features re-evaluated against the current
  machine and current personas, not carried forward verbatim.

For one-off code-quality fixes, prefer `/printing-press-polish` — it doesn't
redo research or rebuild the manuscript.

## Setup

```bash
PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"

# Mid-pipeline callers may pass printing_press_bin: <abs-path> in the args
# bundle. Prefer it so reprint keeps using the parent skill's preflight-selected
# binary instead of re-resolving through PATH.
PRINTING_PRESS_BIN="${PRINTING_PRESS_BIN:-}"
if [ -z "$PRINTING_PRESS_BIN" ] && [ -n "${ARGUMENTS:-}" ]; then
  PRINTING_PRESS_BIN="$(printf '%s\n' "$ARGUMENTS" | sed -nE 's/^[[:space:]]*printing_press_bin:[[:space:]]*(.+)$/\1/p' | head -1)"
fi
if [ -z "$PRINTING_PRESS_BIN" ]; then
  PRINTING_PRESS_BIN="$(command -v cli-printing-press 2>/dev/null || true)"
fi

if [ -z "$PRINTING_PRESS_BIN" ]; then
  echo "cli-printing-press binary not found."
  echo "Install with:  go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest"
  return 1 2>/dev/null || exit 1
fi
echo "PRINTING_PRESS_BIN=$PRINTING_PRESS_BIN"
```

## Phase A — Resolve and reconcile presence

Resolve the user's argument the same way `/printing-press-import` does:
fetch the public library `registry.json` once, then exact → normalized →
fuzzy match. The argument can be an API slug (`notion`), a brand name
(`cal.com`), an old `<api>-pp-cli` form, or close enough.

Capture from the matched registry entry: `API_SLUG` (from `.name`) and
`LIB_PATH` (from `.path`, e.g., `library/productivity/cal-com`). Phase B
uses `$LIB_PATH` for the public patches fetch. For the "present | absent"
never-published row, `$LIB_PATH` stays empty — Phase B's fetch
short-circuits on that.

Then check what exists locally and reconcile against the public library by
reading both provenance manifests' `run_id` and `generated_at`:

| Local | Public registry | Action |
|-------|-----------------|--------|
| absent | absent | STOP — nothing to reprint; suggest `/printing-press <api>` for a fresh print |
| absent | present | invoke `/printing-press-import <api>`, then continue |
| present | absent | continue — never-published local CLI; skip import |
| present, same `run_id` | present | continue without import |
| present, public newer `generated_at` | present | offer import via `AskUserQuestion`; user decides |
| present, local newer `generated_at` | present | STOP — local has unpublished work; tell user to publish or discard first |

When invoking `/printing-press-import`, let it own backup, overwrite,
build-verify, and module-path-rewrite. Wait for it to return clean before
continuing.

## Phase B — Verify reconcilable prior context

Locate the two artifacts the writing pipeline should be aware of: research
(drives novel-features Pass 2(d)) and patches (post-publish hand-fixes
recorded by `/printing-press-amend`, e.g. live-discovered API quirks the
spec didn't reveal).

```bash
LIB_TARGET="$PRESS_LIBRARY/$API_SLUG"
LIB_RESEARCH="$LIB_TARGET/research.json"
MAN_RESEARCH=$(ls -1t "$PRESS_MANUSCRIPTS/$API_SLUG"/*/research.json 2>/dev/null | head -1)
```

### Research absent

If neither research path exists, the published CLI predates `research.json`
provenance. The subagent will treat the run as a first print and Pass 2(d)
reprint reconciliation will not fire — there is nothing for it to read.
Surface this and ask:

> Published `<api>` was built before `research.json` provenance landed.
> Without it, the novel-features subagent will treat this as a first
> print — there is nothing to reconcile against. Continue as a degraded
> reprint (essentially a fresh print with a kept binary name)?

If the user declines, exit. If they continue, record the absence so the
hand-off prompt notes that this is a degraded reprint.

### Patches discovery

Refresh the local patches index from public when reachable, then read
locally so downstream references are durable. Amends may have landed
against the public copy without triggering a regen, so the local copy
can lag even when `run_id` matches; this step closes that gap.

The index ships in one of two shapes: the per-patch directory
`.printing-press-patches/` (current) or the legacy single-array
`.printing-press-patches.json` (older CLIs not yet normalized). Always check
both shapes when reachable. Prefer the directory when it contains patch files,
but never let an absent legacy single-file index set `PATCH_COUNT=0` until the
directory fallback has been read.

```bash
PATCHES_DIR="$LIB_TARGET/.printing-press-patches"
PATCHES_LEGACY="$LIB_TARGET/.printing-press-patches.json"
if [[ -n "$LIB_PATH" ]]; then
  # Fetch the legacy shape if present, but do not treat a 404 as proof that no
  # patches exist. Current library entries may carry only the per-patch
  # directory below.
  tmp=$(mktemp)
  if gh api -H "Accept: application/vnd.github.v3.raw" \
       "repos/mvanhorn/printing-press-library/contents/$LIB_PATH/.printing-press-patches.json" \
       > "$tmp" 2>/dev/null; then
    mv "$tmp" "$PATCHES_LEGACY"
  else
    rm -f "$tmp"
  fi

  # Fetch the current directory shape independently. This is the required
  # fallback when `.printing-press-patches.json` is absent.
  listing=$(gh api "repos/mvanhorn/printing-press-library/contents/$LIB_PATH/.printing-press-patches" 2>/dev/null || true)
  if jq -e 'type == "array"' <<<"$listing" >/dev/null 2>&1; then
    mkdir -p "$PATCHES_DIR"
    jq -r '.[] | select(.name | endswith(".json")) | "\(.name)\t\(.download_url)"' <<<"$listing" \
    | while IFS=$'\t' read -r name url; do
        tmp=$(mktemp)
        if curl -fsSL "$url" -o "$tmp" 2>/dev/null; then
          mv "$tmp" "$PATCHES_DIR/$name"   # atomic: a dropped transfer never leaves corrupt JSON
        else
          rm -f "$tmp"
        fi
      done
  fi
fi

# Count from the first non-empty shape locally; PATCHES_SOURCE is what Phase D reads.
if [[ -d "$PATCHES_DIR" ]]; then
  DIR_PATCH_COUNT=$(find "$PATCHES_DIR" -maxdepth 1 -name '*.json' ! -name '_meta.json' | wc -l | tr -d ' ')
else
  DIR_PATCH_COUNT=0
fi
if [[ "$DIR_PATCH_COUNT" != "0" ]]; then
  PATCH_COUNT="$DIR_PATCH_COUNT"
  PATCHES_SOURCE="$PATCHES_DIR"
elif [[ -f "$PATCHES_LEGACY" ]]; then
  PATCH_COUNT=$(jq '(.patches // []) | length' "$PATCHES_LEGACY" 2>/dev/null || echo 0)
  PATCHES_SOURCE="$PATCHES_LEGACY"
else
  PATCH_COUNT=0
  PATCHES_SOURCE="$PATCHES_DIR"
fi
```

If `$PATCH_COUNT == 0` or no index is present (older CLI predating the
patches contract), skip the rest of this subsection — no patches block in
the hand-off.

If `$PATCH_COUNT > 0`, surface a one-liner to the user before continuing:

> Public `<api>` has `$PATCH_COUNT` recorded patch(es) against the prior
> printed CLI. Will carry into the brief as a watch-list (informational,
> not a re-apply mandate) so the fresh code doesn't silently regress
> live-validated fixes.

Hold `$PATCHES_SOURCE` and `$PATCH_COUNT` for Phase D.

## Phase C — Recency recommendation

Pull `researched_at` from the most-recent prior `research.json` and
`printing_press_version` + `generated_at` from `.printing-press.json`:

```bash
RESEARCHED_AT=$(jq -r '.researched_at // empty' "$MAN_RESEARCH" 2>/dev/null)
PRESS_VERSION=$(jq -r '.printing_press_version // empty' "$LIB_TARGET/.printing-press.json" 2>/dev/null)
GENERATED_AT=$(jq -r '.generated_at // empty' "$LIB_TARGET/.printing-press.json" 2>/dev/null)
```

Compute the calendar age of the research with `python3` so it stays portable
across macOS/Linux and tolerates the microsecond precision that
`generated_at` carries (BSD `date -f` rejects fractional seconds; `python3`
is on every supported platform):

```bash
AGE_DAYS=$(python3 -c "
from datetime import datetime, timezone
ts = '$RESEARCHED_AT'.replace('Z', '+00:00')
print(int((datetime.now(timezone.utc) - datetime.fromisoformat(ts)).total_seconds() // 86400))
" 2>/dev/null)
```

Surface both signals to the user — research age and prior machine version.
Age thresholds are rules of thumb, not gates:

- under 30 days → reuse looks safe
- 30–120 days → reuse plausible; the user should mention any known API
  churn in their reprint reason so the subagent's Pass 2 picks it up
- over 120 days → redo recommended

Don't predict API churn from age alone — describe the signals and let the
user override. The Phase 0 binary-version-bump revalidation in
`/printing-press` handles the machine-delta side independently; don't
duplicate it here.

Ask via `AskUserQuestion`:

1. **Reuse prior research** — keep the prior brief; the subagent re-scores
   prior novel features against current personas
2. **Redo research** — re-run Phase 1 from scratch; the subagent still
   ingests prior novel features as Pass 2(d) input
3. **Show me first** — display the prior brief's headline + novel-features
   list, then re-ask between options 1 and 2

## Phase D — Hand off to `/printing-press`

Before invoking `/printing-press`, use the prior CLI's scorecard and manifest
to decide whether the reprint should offer spec enrichment that the first print
could not have used. Reprints have better evidence than fresh prints: they know
which structural dimensions were weak, which Printing Press version produced
the prior CLI, and what the user named as the reason for regenerating.

Find the most recent scorecard JSON from the prior manuscript run. If no
scorecard artifact exists, run a fresh structural scorecard against the local
library copy:

```bash
SCORECARD_SOURCE=$(ls -1t "$PRESS_MANUSCRIPTS/$API_SLUG"/*/proofs/scorecard.json 2>/dev/null | head -1)
SCORECARD_JSON=""
if [[ -n "$SCORECARD_SOURCE" ]]; then
  SCORECARD_JSON=$(cat "$SCORECARD_SOURCE" 2>/dev/null || true)
elif [[ -d "$LIB_TARGET" ]]; then
  SCORECARD_SOURCE=$(mktemp)
  if "$PRINTING_PRESS_BIN" scorecard --dir "$LIB_TARGET" --json > "$SCORECARD_SOURCE" 2>/dev/null; then
    SCORECARD_JSON=$(cat "$SCORECARD_SOURCE" 2>/dev/null || true)
  fi
  rm -f "$SCORECARD_SOURCE"
  SCORECARD_SOURCE=""
fi
```

If `SCORECARD_JSON` is empty, continue without enrichment prompting and say the
reprint is proceeding without prior score evidence. Do not invent a prompt from
the reprint reason alone.

When `SCORECARD_JSON` is available, inspect only dimensions that map to a
pre-generation spec edit and skip dimensions that already score 10/10:

- `mcp_remote_transport`, `mcp_token_efficiency`, `mcp_tool_design`, and
  `mcp_surface_strategy` below 10 can be lifted by the `/printing-press`
  Phase 2 section **Pre-Generation MCP Enrichment**. Examples:
  - remote transport is below 10: offer `mcp.transport: [stdio, http]` or the
    OpenAPI `x-mcp.transport` equivalent before regeneration.
  - token efficiency, tool design, or surface strategy is below 10: offer the
    Phase 2 MCP surface decision, including intents for clear multi-step
    workflows or the Cloudflare pattern for large surfaces.
- `auth_protocol` below 10, or prior manifest evidence that the CLI used a
  slug-derived env var where the ecosystem has a canonical env var, can be
  lifted by **Pre-Generation Auth Enrichment**. Offer to carry canonical
  `auth.env_vars` or OpenAPI `x-auth-env-vars` guidance into the spec.
- `data_pipeline_integrity` below 10 is only an enrichment opportunity when the
  prior CLI or research shows sync-eligible resources. In that case, point the
  handoff at the relevant Phase 2 sync/cache enrichment decision rather than
  treating the score alone as proof that a local store should exist.

Use `AskUserQuestion` for each concrete opportunity before the handoff. Phrase
the question around the scorecard evidence and the named canonical section, not
around a freeform rewrite. Example:

> Prior scorecard shows `mcp_remote_transport: 5/10`. Offer MCP transport
> enrichment before regenerating, using `/printing-press` Phase 2
> **Pre-Generation MCP Enrichment** as the source of truth?

Options:

1. **Apply enrichment to the handoff** - include the selected spec edit in the
   `/printing-press` prompt so Phase 2 can update the spec before generation.
2. **Skip for this reprint** - leave the spec unchanged for this dimension.
3. **Show score evidence first** - print the relevant scorecard lines and then
   re-ask between options 1 and 2.

Do not auto-apply enrichment. If the prior scorecard already has
`mcp_remote_transport: 10/10`, do not ask the redundant MCP transport question.
If the user accepts any opportunity, add a `## Reprint Spec Enrichment`
section to the `/printing-press` handoff. Keep it brief: name the weak
dimension, the accepted enrichment, and the canonical `/printing-press` Phase 2
section to execute. Do not duplicate the canonical enrichment text here.

Invoke `/printing-press <api>` and bundle these into the prompt:

1. **A header line** stating the user already chose to regenerate, so
   Phase 0's library-check should select "Generate a fresh CLI" and not
   re-prompt fresh-vs-improve.
2. **Research mode** from Phase C (`reuse` or `redo`). Phase 0's existing
   reuse logic consumes this.
3. **The user's freeform reprint reason**, verbatim, in a `User context`
   block. This propagates into the brief as `## User Vision` and becomes
   Pass 2(e) input to the novel-features subagent — the right hook for
   "I want better MCP support" → bias the brainstorm accordingly.
4. **Reprint spec enrichment** — only when the scorecard-driven prompt above
   found an accepted opportunity. Include under a
   `## Reprint Spec Enrichment` heading.
5. **Prior patches** — only when Phase B found `$PATCH_COUNT > 0`.
   Include under a `## Prior Patches` heading.

   Lead the section with this framing sentence, verbatim:

   > The following were hand-fixed against the prior printed CLI. They
   > are informational — the machine may have absorbed some of these
   > upstream since the patch was applied. Stay aware so the fresh code
   > doesn't silently regress the underlying API truth or architectural
   > pattern, but do not treat as a re-apply checklist.

   Then summarize the patches from `$PATCHES_SOURCE`. Lead each entry
   with the *substance* the patch encodes (the API truth, the
   architectural pattern, the cross-file convention) drawn from each
   patch's `reason` and `validated_outcome` — not just `summary`. Patch
   metadata (`id`, `files`) is fine as parenthetical context but never
   the headline.

   Scale the section to patch count:

   - **1–3 patches**: one short paragraph per patch — substance first,
     then how it manifested. Model the shape on:
     > Linear's personal API keys go in `Authorization: lin_api_…` raw,
     > with no `Bearer` prefix; OAuth tokens use `Authorization: Bearer
     > <token>`. The prior CLI shipped with `auth set-token` only
     > (Bearer-defaulted) and was patched post-publish to add
     > `auth set-api-key` plus the raw-header path in `config.go`
     > (`linear-auth-api-key-vs-oauth-token`).
   - **4–9 patches**: one tight sentence per patch, same substance-first
     shape.
   - **10+ patches**: thematic summary by category (auth, pagination,
     query encoding, MCP additions, helpers, classifiers, error
     envelopes). Two or three sentences per theme citing 2–3 patch `id`s
     as evidence. Do not enumerate all entries inline.

   Close the section with a pointer so a downstream agent can drill into
   any specific patch when working in the relevant area:

   > Full patch detail: the per-patch files under `$PATCHES_SOURCE`
   > (each `<id>.json` is one self-contained patch; the legacy
   > single-array `.printing-press-patches.json` carries the same fields
   > under `patches[]`).

Do **not** pass a separate "this is a reprint" marker. The novel-features
subagent runs unconditionally on every print and discovers prior research
via its own discovery snippet (see
`skills/printing-press/references/novel-features-subagent.md`). The paths
import populated in Phase A are exactly the paths it checks; Pass 2(d)
fires whenever prior `research.json` exists.

The library-preservation contract is owned by `/printing-press` Phase 5.6
("Promote to Library"), not by this skill. When the existing library has
`novel_features > 0` in its manifest (or hand-authored files under
`internal/cli/`, `internal/syncer/`, or `internal/store/`), Phase 5.6 first
dry-runs `"$PRINTING_PRESS_BIN" regen-merge "$LIB_TARGET" --fresh "$CLI_WORK_DIR"
--json` to decide whether the fresh tree rebuilt the prior novels. If the
fresh tree contains all prior novel work, Phase 5.6 uses the swap path and
treats generated-file version drift as expected overwrite. Otherwise it routes
promotion through `regen-merge --apply` so still-unique hand-authored novels
survive the reprint and genuine `NOVEL-COLLISION` / missing-referent cases halt
for review. This honors the prefer-`regen-merge` guidance under the
**Hand-edits must be regen-mergeable.** section of
`skills/printing-press/SKILL.md` (anchor `hand-edit-durability`). If a future
edit to that phase changes the routing rule, update this paragraph in the same
PR -- the reprint skill is the dominant entry point that fires it.

Before the hand-off, compare regenerated manifest files against the tracked
published tree. If `$LIB_TARGET/manifest.json` or
`$LIB_TARGET/tools-manifest.json` exists, diff each file against the freshly
generated counterpart under `$CLI_WORK_DIR` and surface non-empty diffs in the
handoff prompt. Treat a diff as a reconciliation checkpoint, not as automatic
overwrite approval: the operator must decide whether to preserve the tracked
hand-edit, fold it into the spec/research inputs, or intentionally accept the
regenerated value. Do not continue silently when tracked manifest fields,
tool metadata, or descriptions would be dropped.

Attribution also stays owned by `/printing-press`: the hand-off runs generation
for the same API slug, and the generate/promote path must preserve the existing
library manifest's permanent `creator` while adding the reprinter to
`contributors[]` when they differ. Do not repair this by hand-editing
`creator`, `contributors[]`, README bylines, SKILL `author:`, or NOTICE; rerun
with a current Printing Press binary so the manifest-first guard rewrites the
working tree and promoted tree consistently.

## After hand-off

The printing-press flow drives the rest. Don't summarize its work — let
the user see the live phases.

If `/printing-press` halts with the subagent's pre-flight HALT (brief
lacks concrete `Users` / `Top Workflows` content), the reused prior brief
predates the subagent's required schema. Recommend re-running with **Redo
research** selected at Phase C.
