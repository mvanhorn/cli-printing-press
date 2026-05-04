# GitHub Issue Template

Read this file during Phase 6 when creating GitHub issue(s) from retro findings.

The shape depends on the work-unit count:

- **0 WUs** — Phase 5.6 already gated this out; no issue gets filed.
- **1 WU** — single issue; the WU is inlined in the issue body, no parent/child
  hierarchy. Labels are still applied so cross-retro filtering still works.
- **2+ WUs** — **parent issue** (retro context, summary tables, artifact links) +
  **one sub-issue per WU** (full WU details + absorbed findings + prior-retro
  links). Sub-issues are linked to the parent via the GitHub sub-issues REST
  API. This is the structural fix for "monster issues that are impossible to
  track" — each WU gets its own open/closed/assignee state.

## Formatting rules

**Never use `#N` notation** for finding or work unit numbers in *summary* text where
GitHub auto-linking would be confusing. Use the `F` prefix for findings (`F1`, `F3`)
and `WU-` for work units. **Real GitHub issue references in the `Related prior retros`
block and the parent's "Sub-issues" backfill table are intentional `#N`** — that's
where we *want* GitHub to auto-link the timelines together.

## Step 1: Ensure labels exist (idempotent, create-only)

Run once per session before creating any issues. The repo is expected to already
have the canonical label set; this step is a safety net for users running the
skill against a fresh fork. **Create-only — never edit existing labels** (the
maintainer may have set custom colors or descriptions; the skill must not
clobber them).

```bash
REPO="mvanhorn/cli-printing-press"

ensure_label() {
  local name="$1" color="$2" desc="$3"
  # Create only if missing; failure (label exists, no permissions, rate limit)
  # is non-fatal — issue creation later will fail loudly if a required label is
  # genuinely absent.
  gh label create "$name" --repo "$REPO" --color "$color" --description "$desc" 2>/dev/null || true
}

# Component labels (6) — drive cross-retro discovery (`gh issue list --label comp:<slug>`)
ensure_label "comp:generator"      "5319e7" "Generator templates (internal/generator/)"
ensure_label "comp:openapi-parser" "5319e7" "OpenAPI parser (internal/openapi/)"
ensure_label "comp:spec-parser"    "5319e7" "Internal spec parser (internal/spec/)"
ensure_label "comp:scorer"         "5319e7" "verify / dogfood / scorecard"
ensure_label "comp:skill"          "5319e7" "skills/printing-press/SKILL.md and related skill instructions"
ensure_label "comp:catalog"        "5319e7" "catalog/ entries"

# Priority labels (3) — duplicate the title prefix to enable label-based filtering.
ensure_label "priority:P1" "b60205" "Retro priority P1 (high)"
ensure_label "priority:P2" "d93f0b" "Retro priority P2 (medium)"
ensure_label "priority:P3" "fbca04" "Retro priority P3 (low)"

# Marker labels
ensure_label "retro"        "0e8a16" "Issue produced by /printing-press-retro"
ensure_label "retro-parent" "0e8a16" "Parent retro issue with sub-issue WUs"
```

### Priority labels

Apply `priority:P<n>` matching the WU's internal priority. The label and the
`[P<n>]` title prefix carry the same value — the label enables
`gh issue list --label priority:P1` filtering across retros, the title prefix
gives at-a-glance scanning in issue lists.

## Step 2: Resolve the work-unit count and dispatch

```bash
WU_COUNT="${#WORK_UNITS[@]}"  # populated from Phase 5.5

if [ "$WU_COUNT" -eq 0 ]; then
  echo "Phase 5.6 should have gated this out. Aborting."
  exit 1
elif [ "$WU_COUNT" -eq 1 ]; then
  ISSUE_MODE="single"
else
  ISSUE_MODE="parent-with-subs"
fi
```

## Single-issue mode (`WU_COUNT == 1`)

### Title

```
Retro: <api-display-name> — 1 work unit (P<n>)
```

Example: `Retro: Cal.com — 1 work unit (P1)`

### Body

```markdown
## Session Stats

| Metric | Value |
|--------|-------|
| API | <api-display-name> |
| Spec source | <catalog / browser-sniffed / docs / HAR> |
| Scorecard | <score>/100 (<grade>) |
| Verify pass rate | <X>% |
| Fix loops | <N> |
| Manual code edits | <N> |
| Features built from scratch | <N> |
| Triage | <K> candidates → 1 filed / <S> skipped / <X> dropped |

## What the Printing Press Got Right

- <pattern>
- <pattern>

## Work Unit

### WU-1: <title>

- **Priority:** P<n>
- **Component:** <comp-slug>
- **Complexity:** small / medium / large
- **Goal:** <one sentence>
- **Target:** <component and area>
- **Acceptance criteria:**
  - positive: ...
  - negative: ...
- **Scope boundary:** ...
- **Dependencies:** <other WUs or "None">

### Findings absorbed

#### F<n>: <title> (P<n>, <category>)

- **What happened:** ...
- **Scorer correct?** ...
- **Root cause:** ...
- **Cross-API check:** ...
- **Frequency:** every / most / subclass:<name> / this-API
- **Fallback:** ...
- **Worth a fix?** ...
- **Inherent or fixable:** ...
- **Durable fix:** ...
- **Test:** positive + negative
- **Evidence:** ...
- **Related prior retros:**
  - #<num> (`<api-slug>` retro, `aligned`/`contradicts`/`extends`) — <one-sentence note>
  - *(or "None" if Phase 3 Step D found no matches)*

*(Repeat for each absorbed finding.)*

## Skipped

| Finding | Title | Why it didn't make it (Step B / Step D / Step G) |
|---------|-------|--------------------------------------------------|
| F<n>    | ...   | ...                                              |

*(Omit if no findings were skipped.)*

## Artifacts

| Artifact | Link |
|----------|------|
| Retro document | <$RETRO_DOC_URL or "Upload failed — see below"> |
| Manuscripts (research + proofs) | <$MANUSCRIPTS_URL or "Upload failed — see below"> |
| Generated CLI source code | <$CLI_SOURCE_URL or "Upload failed — see below"> |

---

*Generated by `/printing-press-retro` · [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)*
```

### Create

```bash
ISSUE_URL=$(gh issue create \
  --repo "$REPO" \
  --title "$ISSUE_TITLE" \
  --body "$BODY" \
  --label retro \
  --label "priority:P${PRIORITY_NUM}" \
  --label "comp:${COMPONENT_SLUG}")
```

## Parent-with-sub-issues mode (`WU_COUNT >= 2`)

### Step P1: Build the WU bodies first (in priority order)

Sort WUs by priority: P1 first, then P2, then P3. Within a priority bucket, keep
the order they appeared in Phase 5.5 (typically by ascending WU number, but the
skill may have intentionally ordered them by dependency — preserve that).

For each WU, build the sub-issue body:

```markdown
**Parent retro:** #<PARENT_NUMBER>  *(backfilled after parent is created — see Step P3)*

## Work Unit

- **Priority:** P<n>
- **Component:** <comp-slug>
- **Complexity:** small / medium / large
- **Goal:** <one sentence>
- **Target:** <component and area>
- **Acceptance criteria:**
  - positive: ...
  - negative: ...
- **Scope boundary:** ...
- **Dependencies:** <other WU sub-issue numbers, backfilled if known, otherwise "None">

## Findings absorbed

#### F<n>: <title> (P<n>, <category>)

- **What happened:** ...
- **Scorer correct?** ...
- **Root cause:** ...
- **Cross-API check:** ...
- **Frequency:** every / most / subclass:<name> / this-API
- **Fallback:** ...
- **Worth a fix?** ...
- **Inherent or fixable:** ...
- **Durable fix:** ...
- **Test:** positive + negative
- **Evidence:** ...
- **Related prior retros:**
  - #<num> (`<api-slug>` retro, `aligned`/`contradicts`/`extends`) — <one-sentence note>
  - *(or "None")*

*(Repeat for each absorbed finding.)*

---

*Sub-issue of the [<api-display-name> retro](#<PARENT_NUMBER>) · Generated by `/printing-press-retro`*
```

Sub-issue title:

```
[P<n>] WU-<m>: <title>
```

Examples: `[P1] WU-1: Emit Stripe-Version header from spec`, `[P2] WU-2: Default
auth scaffold for cookie+CSRF APIs`.

### Step P2: Create the parent issue

Title:

```
Retro: <api-display-name> — <N> findings, <M> work units
```

Body — note the explicit placeholder `<!-- WU_TABLE -->`. This is replaced in
Step P5 once sub-issue URLs are known.

```markdown
## Session Stats

| Metric | Value |
|--------|-------|
| API | <api-display-name> |
| Spec source | <catalog / browser-sniffed / docs / HAR> |
| Scorecard | <score>/100 (<grade>) |
| Verify pass rate | <X>% |
| Fix loops | <N> |
| Manual code edits | <N> |
| Features built from scratch | <N> |
| Triage | <K> candidates → <D> filed / <S> skipped / <X> dropped |

## What the Printing Press Got Right

- <pattern>
- <pattern>

## Findings

### P1 — High priority

| Finding | Title | Component | Frequency | WU |
|---------|-------|-----------|-----------|-----|
| F1 | ... | comp:generator | every | WU-1 |

### P2 — Medium priority

| Finding | Title | Component | Frequency | WU |
|---------|-------|-----------|-----------|-----|
| F2 | ... | comp:openapi-parser | most | WU-2 |

### P3 — Low priority

| Finding | Title | Component | Frequency | WU |
|---------|-------|-----------|-----------|-----|
| F3 | ... | comp:skill | subclass:browser-sniffed | WU-2 |

*Omit empty priority sections.*

## Skipped

| Finding | Title | Why it didn't make it |
|---------|-------|------------------------|
| F<n> | ... | Step B / Step D / Step G: ... |

*Omit if empty. Drops (rejected at Phase 2.5 triage) live in the full retro
document linked under Artifacts — they're triage hygiene, not maintainer signal.*

## Work Units

Each WU is filed as a sub-issue for independent tracking. GitHub renders the
sub-issue list above the comments; the table below mirrors it for at-a-glance
reading and search.

<!-- WU_TABLE -->

## Artifacts

| Artifact | Link |
|----------|------|
| Retro document | <$RETRO_DOC_URL or "Upload failed — see below"> |
| Manuscripts (research + proofs) | <$MANUSCRIPTS_URL or "Upload failed — see below"> |
| Generated CLI source code | <$CLI_SOURCE_URL or "Upload failed — see below"> |

---

*Generated by `/printing-press-retro` · [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)*
```

Create the parent. The parent gets `retro` + `retro-parent` labels and **no
priority/component labels** — those are per-WU concerns.

```bash
PARENT_URL=$(gh issue create \
  --repo "$REPO" \
  --title "$PARENT_TITLE" \
  --body "$PARENT_BODY" \
  --label retro \
  --label retro-parent)

if ! echo "$PARENT_URL" | grep -q '^https://'; then
  echo "WARNING: parent issue creation failed. Falling back to manual filing instructions."
  GH_AVAILABLE=false
else
  GH_AVAILABLE=true
  PARENT_NUM=$(echo "$PARENT_URL" | grep -oE '[0-9]+$')
  echo "Parent issue: $PARENT_URL"
fi
```

If `GH_AVAILABLE=false`, skip Steps P3-P5 and continue with the graceful
degradation path documented at the bottom of this file. Do not create WU issues
without a parent issue number.

### Step P3: Create each WU sub-issue and link via the sub-issues API

Loop in priority order. For each WU, replace the parent placeholder in the WU
body, create the issue, fetch its database ID, then POST to the sub-issues
endpoint.

Track each WU's outcome explicitly. The parent body's findings tables reference
`WU-N` by ordinal, so a silent `continue` past a failed creation would leave the
parent advertising sub-issues that don't exist. Instead, every WU contributes a
row to the final WU table — successful ones link to their sub-issue, failed
ones surface as `FAILED — file manually` so the maintainer sees the gap.

```bash
declare -a WU_URLS WU_NUMS WU_TITLES WU_PRIORITIES WU_COMP_SLUGS WU_COMPLEXITIES WU_STATUSES
declare -a FAILED_WUS  # human-readable failures for the final summary
SUB_ISSUE_API_OK=true

for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  WU="${SORTED_WORK_UNITS[$wu_idx]}"
  # Each WU contributes: $WU_TITLE, $WU_BODY_TEMPLATE, $WU_PRIORITY_NUM,
  # $WU_PRIORITY_LABEL, $WU_COMP_SLUG, and $WU_COMPLEXITY.

  # Backfill the parent reference in the body
  WU_BODY="${WU_BODY_TEMPLATE/<PARENT_NUMBER>/$PARENT_NUM}"

  WU_URL=$(gh issue create \
    --repo "$REPO" \
    --title "$WU_TITLE" \
    --body "$WU_BODY" \
    --label retro \
    --label "priority:P${WU_PRIORITY_NUM}" \
    --label "comp:${WU_COMP_SLUG}")

  if ! echo "$WU_URL" | grep -q '^https://'; then
    echo "WARNING: WU sub-issue creation failed for: $WU_TITLE"
    echo "         Error: $WU_URL"
    # Record the failure so the parent's WU table and the final summary
    # surface it; do NOT silently continue.
    WU_URLS+=("")
    WU_NUMS+=("")
    WU_TITLES+=("$WU_TITLE")
    WU_PRIORITIES+=("$WU_PRIORITY_LABEL")
    WU_COMP_SLUGS+=("$WU_COMP_SLUG")
    WU_COMPLEXITIES+=("$WU_COMPLEXITY")
    WU_STATUSES+=("create-failed")
    FAILED_WUS+=("WU-$((wu_idx+1)) ($WU_TITLE) — issue creation failed")
    continue
  fi

  WU_NUM=$(echo "$WU_URL" | grep -oE '[0-9]+$')
  WU_STATUS="ok"

  # Fetch the integer database ID — required by the sub-issues REST API.
  # gh issue view --json id returns the GraphQL node ID (string), which the
  # REST endpoint rejects. The REST endpoint returns the integer id we want.
  WU_DB_ID=$(gh api "repos/$REPO/issues/$WU_NUM" --jq '.id' 2>/dev/null)

  if [ -z "$WU_DB_ID" ] || [ "$WU_DB_ID" = "null" ]; then
    echo "WARNING: could not fetch DB id for issue #$WU_NUM; skipping sub-issue link."
    SUB_ISSUE_API_OK=false
    WU_STATUS="link-failed"
    FAILED_WUS+=("WU-$((wu_idx+1)) (#$WU_NUM) — sub-issue link skipped (DB id unavailable)")
  else
    # Link as sub-issue. The REST endpoint exists on github.com and most GHES
    # versions; older GHES instances return 404 here.
    LINK_RESPONSE=$(gh api \
      --method POST \
      -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      "/repos/$REPO/issues/$PARENT_NUM/sub_issues" \
      -F "sub_issue_id=$WU_DB_ID" 2>&1)

    if echo "$LINK_RESPONSE" | grep -qE '"number"|"id"'; then
      echo "Linked WU sub-issue: $WU_URL"
    else
      echo "WARNING: sub-issue link failed for #$WU_NUM; body cross-link will be the only relationship."
      echo "         Response: $LINK_RESPONSE"
      SUB_ISSUE_API_OK=false
      WU_STATUS="link-failed"
      FAILED_WUS+=("WU-$((wu_idx+1)) (#$WU_NUM) — sub-issue link failed (cross-link in body remains)")
    fi
  fi

  WU_URLS+=("$WU_URL")
  WU_NUMS+=("$WU_NUM")
  WU_TITLES+=("$WU_TITLE")
  WU_PRIORITIES+=("$WU_PRIORITY_LABEL")
  WU_COMP_SLUGS+=("$WU_COMP_SLUG")
  WU_COMPLEXITIES+=("$WU_COMPLEXITY")
  WU_STATUSES+=("$WU_STATUS")
done
```

Three distinct failure modes, three distinct outcomes:

| Mode | What happened | What the parent shows | What the user sees in Phase 6 Step 6 |
|------|---------------|----------------------|--------------------------------------|
| `create-failed` | `gh issue create` for the WU returned a non-URL | WU table row reads `FAILED — file manually` | `FAILED_WUS` summary names this WU |
| `link-failed` | Issue created OK but sub-issues REST POST failed (or DB id fetch failed) | WU table row links the issue normally; native sub-issue panel won't include it | `FAILED_WUS` notes the issue exists but isn't natively linked |
| `ok` | Issue created and linked | WU table row links the issue; native sub-issue panel includes it | nothing |

### Step P4: Build the sub-issue table

The table iterates over **every** WU, including failed ones. A failed WU's row
reads `FAILED — file manually` in the Sub-issue column so the parent doesn't
silently advertise sub-issues that don't exist.

```bash
WU_TABLE=$'| WU | Title | Priority | Component | Complexity | Sub-issue |\n'
WU_TABLE+=$'|----|-------|----------|-----------|------------|-----------|\n'

for i in "${!WU_TITLES[@]}"; do
  if [ "${WU_STATUSES[$i]}" = "create-failed" ]; then
    SUB_CELL="**FAILED — file manually**"
  else
    SUB_CELL="#${WU_NUMS[$i]}"
  fi
  WU_TABLE+="| WU-$((i+1)) | ${WU_TITLES[$i]} | ${WU_PRIORITIES[$i]} | comp:${WU_COMP_SLUGS[$i]} | ${WU_COMPLEXITIES[$i]} | ${SUB_CELL} |"$'\n'
done
```

### Step P5: Edit the parent body to backfill the WU table

```bash
PARENT_BODY_FINAL="${PARENT_BODY//<!-- WU_TABLE -->/$WU_TABLE}"

gh issue edit "$PARENT_NUM" --repo "$REPO" --body "$PARENT_BODY_FINAL"
```

If the edit fails (rate limit, permissions), the parent stays with its placeholder
visible — readable enough that the user understands what's missing, and GitHub's
native sub-issue panel still shows the linked WUs.

### Step P6: Cross-reference behavior

If `SUB_ISSUE_API_OK=false` (sub-issues endpoint unavailable, e.g., older GHES,
permissions, feature not enabled), the WUs still cross-link to the parent via
the `**Parent retro:** #<PARENT_NUMBER>` line in their body. The parent's WU
table also still shows `#<num>` references, which GitHub auto-links. This means
even with the sub-issues API completely broken, the relationship between parent
and WUs is preserved as ordinary issue cross-references — the only thing lost
is GitHub's native sub-issue rendering and progress bar.

## Variables expected

| Variable | Set by | Contains |
|----------|--------|----------|
| `$REPO` | This file Step 1 | Owner/repo string for `gh` |
| `$RETRO_DOC_URL` | artifact-packaging.md | catbox URL for retro .md, or empty |
| `$MANUSCRIPTS_URL` | artifact-packaging.md | catbox URL or empty |
| `$CLI_SOURCE_URL` | artifact-packaging.md | catbox URL or empty |
| `$RETRO_PROOF_PATH` | SKILL.md Phase 5 | Path to saved retro in manuscript proofs |
| `$RETRO_SCRATCH_PATH` | SKILL.md Phase 5 | Path to temp retro copy under `/tmp/printing-press/retro/` |
| `$WORK_UNITS` | SKILL.md Phase 5.5 | Array of WU records (title, priority, comp slug, complexity, body) |
| `$SORTED_WORK_UNITS` | This file Step P1 | `$WORK_UNITS` sorted P1 → P3 |
| All retro findings | SKILL.md Phase 4 | Used to populate parent findings tables and WU "Findings absorbed" sections |

## Variables produced

| Variable | Contains |
|----------|----------|
| `$PARENT_URL` | Parent issue URL (only in `parent-with-subs` mode) |
| `$PARENT_NUM` | Parent issue number (only in `parent-with-subs` mode) |
| `$WU_URLS` | Array of WU sub-issue URLs (empty string for failed creations; only in `parent-with-subs` mode) |
| `$WU_STATUSES` | Array, one per WU: `ok` / `create-failed` / `link-failed` (only in `parent-with-subs` mode) |
| `$FAILED_WUS` | Array of human-readable failure descriptions; empty if every WU succeeded |
| `$SUB_ISSUE_API_OK` | `false` if any sub-issue link failed; `true` otherwise |
| `$ISSUE_URL` | Single issue URL (only in `single` mode) |
| `$ISSUE_MODE` | `single` or `parent-with-subs` — used by Phase 6 Step 6 to format presentation |

## Handling `gh` failure (graceful degradation)

Check `gh` auth at the start of Phase 6 Step 4:

```bash
if ! gh auth status 2>/dev/null; then
  echo "GitHub CLI is not authenticated. Cannot create issue(s)."
  GH_AVAILABLE=false
else
  GH_AVAILABLE=true
fi
```

If `GH_AVAILABLE=false`, or if any of the issue-creation commands fail with a
network/permissions error that the per-step fallbacks didn't already absorb, fall
back to printing manual filing instructions:

```bash
if [ "$GH_AVAILABLE" = false ]; then
  echo ""
  echo "Could not create GitHub issue(s) automatically."
  echo ""
  echo "To file the retro manually:"
  echo "  1. Go to: https://github.com/mvanhorn/cli-printing-press/issues/new"
  echo "  2. Use the title and body from the retro document at:"
  echo "       $RETRO_PROOF_PATH"
  if [ -n "$RETRO_SCRATCH_PATH" ] && [ -f "$RETRO_SCRATCH_PATH" ]; then
    echo "       $RETRO_SCRATCH_PATH"
  fi
  echo "  3. For the 2+ WU case, file one issue per WU and link them as sub-issues"
  echo "     via the issue page's 'Sub-issues' panel after creation."
  if [ -n "$MANUSCRIPTS_URL" ]; then
    echo "  4. Manuscripts: $MANUSCRIPTS_URL"
  fi
  if [ -n "$CLI_SOURCE_URL" ]; then
    echo "  5. CLI source: $CLI_SOURCE_URL"
  fi
fi
```

## Handling body size

GitHub issue bodies have a practical limit (~65KB). The breakout into sub-issues
makes hitting this limit much less likely than the old monster-issue mode, but a
single WU with many absorbed findings + long prior-retro chains could still
approach it. If `gh issue create` rejects a body for size:

1. Truncate the absorbed findings within the WU body to: title + one-sentence
   summary + `Evidence:` link.
2. Add: "Full finding analysis available in the manuscripts artifact linked
   from the parent issue."
3. Retry.

For the parent: drop "What the Printing Press Got Right" and the Skipped table
first; keep Session Stats, Findings tables, the WU table, and Artifacts.
