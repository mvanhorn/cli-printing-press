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

## Execution principles (read before running any step)

The retro doc and the manuscript zip are the durable audit trail; the issue
body is an action surface. Optimize the issue path for speed and signal:

- **Issue body is a slim subset.** Per-finding fields in the issue templates
  below are intentionally fewer than in the retro doc. Don't re-introduce the
  triage-rationale fields (`Scorer correct?`, `Cross-API check`, `Worth a
  fix?`, `Inherent or fixable?`, per-finding `Test`) — they live in the retro
  doc, which the parent issue links via Artifacts. A maintainer reading the
  issue wants the action; the audit trail is one click away.
- **Generate bodies inline, never via the Write tool.** Use shell heredocs into
  variables in a single `Bash` invocation. Writing each body to a file and
  passing `--body-file` adds a tool round-trip per issue and is the single
  largest source of perceived latency the skill historically had.
- **One Bash call where the work allows it.** Phase 6 Step 4 should typically be
  a single Bash invocation that defines all bodies via heredocs, creates the
  parent, creates and links sub-issues in parallel, and edits the parent's
  WU-table backfill — surface the produced URLs at the end.
- **Use `gh api --method POST /repos/.../issues` instead of `gh issue create`
  for sub-issues.** The REST endpoint returns `id`, `number`, and `html_url` in
  one response, so the separate `gh api repos/.../issues/N` lookup for the
  integer DB id is no longer needed (the sub-issues link API requires that DB
  id, not the issue number).
- **Run sub-issue creates in parallel.** Each WU is independent of every other
  WU once the parent number is known. Background subshells writing to indexed
  temp files, then `wait`, then read in order — the pattern is in Step P3.

## Step 1: Ensure labels exist (idempotent, create-only)

Run once per session before creating any issues. The repo is expected to already
have the canonical label set; this step is a safety net for users running the
skill against a fresh fork. **Create-only — never edit existing labels** (the
maintainer may have set custom colors or descriptions; the skill must not
clobber them).

```bash
REPO="mvanhorn/cli-printing-press"

# Fast-path: list existing labels once. If all 11 canonical labels are already
# present, skip the create loop entirely — saves up to 11 gh API calls per
# retro on a repo where prior retros already provisioned the set.
EXISTING_LABELS=$(gh label list --repo "$REPO" --limit 200 --json name --jq '.[].name' 2>/dev/null || echo "")
NEED_CREATE=false
for required in \
  "comp:generator" "comp:openapi-parser" "comp:spec-parser" \
  "comp:scorer" "comp:skill" "comp:catalog" \
  "priority:P1" "priority:P2" "priority:P3" \
  "retro" "retro-parent"; do
  if ! printf '%s\n' "$EXISTING_LABELS" | grep -qFx "$required"; then
    NEED_CREATE=true
    break
  fi
done

if [ "$NEED_CREATE" = true ]; then
  ensure_label() {
    local name="$1" color="$2" desc="$3"
    # Create only if missing; failure (label exists, no permissions, rate
    # limit) is non-fatal — issue creation later will fail loudly if a
    # required label is genuinely absent.
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
fi
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

> Triage rationale (Scorer correct?, Cross-API check, Worth a fix?, Inherent or
> fixable?) and the long-form root-cause prose live in the retro doc linked
> under Artifacts. The bullets below are the action surface.

#### F<n>: <title> (<category>)

- **What happened:** <one-sentence symptom>
- **Frequency:** every / most / subclass:<name>
- **Durable fix:** <prescription, with code snippet if it clarifies the change>
- **Evidence:** <session moment that surfaced this — file, line, command, or proof artifact>
- **Related prior retros:**
  - #<num> (`<api-slug>` retro, `aligned`/`contradicts`/`extends`) — <one-sentence note>
  - *(or "None" if Phase 3 Step D found no matches)*

*(Repeat for each absorbed finding. Per-finding positive/negative tests fold
into the WU's Acceptance criteria above, prefixed `F<n>:` so each finding is
verifiable.)*

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

> Triage rationale (Scorer correct?, Cross-API check, Worth a fix?, Inherent or
> fixable?) and the long-form root-cause prose live in the retro doc linked
> from the parent issue under Artifacts. The bullets below are the action
> surface.

#### F<n>: <title> (<category>)

- **What happened:** <one-sentence symptom>
- **Frequency:** every / most / subclass:<name>
- **Durable fix:** <prescription, with code snippet if it clarifies the change>
- **Evidence:** <session moment — file, line, command, or proof artifact>
- **Related prior retros:**
  - #<num> (`<api-slug>` retro, `aligned`/`contradicts`/`extends`) — <one-sentence note>
  - *(or "None")*

*(Repeat for each absorbed finding. Per-finding positive/negative tests fold
into the WU's Acceptance criteria above, prefixed `F<n>:` so each finding
remains independently verifiable.)*

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

## Summary

<2-3 sentences that tell the maintainer what to do without making them open
sub-issues: the headline pattern across findings (e.g., "framework command
templates ship without `--json` or `Examples:` blocks"), the component(s)
touched, and which WU is the highest-leverage fix. Skip if the WU table below
already conveys this clearly — never restate it twice.>

## What the Printing Press Got Right

- <pattern>
- <pattern>

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

### Step P3: Create each WU sub-issue and link via the sub-issues API (parallel)

Spawn one background subshell per WU. Each subshell:

1. Creates the issue via `gh api --method POST /repos/.../issues` — the REST
   endpoint returns `id`, `number`, and `html_url` in a single response, so
   the legacy "`gh issue create` + `gh api repos/.../issues/N` to fetch the DB
   id" pair collapses into one round trip per WU.
2. Links the new issue under the parent via the sub-issues REST endpoint.
3. Writes a fixed-shape result file to `$WU_TMPDIR/wu-$idx` (one field per
   line — robust against tabs or other whitespace in titles).

After `wait`, the parent shell collects results in original order so the WU
table builds correctly. Per-WU work has no cross-WU dependency once
`$PARENT_NUM` is known, so parallelism is safe and saves roughly `(N-1) ×
round-trip-latency` on N WUs.

The parent body's findings/WU tables reference `WU-N` by ordinal, so each WU
contributes a row to the final WU table whether it succeeded or failed.
Successful ones link to their sub-issue; failed ones surface as `FAILED —
file manually` so the maintainer sees the gap.

```bash
declare -a WU_URLS WU_NUMS WU_TITLES WU_PRIORITIES WU_COMP_SLUGS WU_COMPLEXITIES WU_STATUSES
declare -a FAILED_WUS  # human-readable failures for the final summary
SUB_ISSUE_API_OK=true

WU_TMPDIR=$(mktemp -d)

for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  (
    WU="${SORTED_WORK_UNITS[$wu_idx]}"
    # Each WU contributes: $WU_TITLE, $WU_BODY_TEMPLATE, $WU_PRIORITY_NUM,
    # $WU_PRIORITY_LABEL, $WU_COMP_SLUG, and $WU_COMPLEXITY.

    WU_BODY="${WU_BODY_TEMPLATE/<PARENT_NUMBER>/$PARENT_NUM}"

    STATUS=""
    WU_DB_ID=""
    WU_NUM=""
    WU_URL=""
    FAIL_MSG=""

    # Single-call create: returns id + number + html_url as a tab-separated
    # triple. gh's bundled jq runs the filter against the response.
    if WU_TSV=$(gh api --method POST "/repos/$REPO/issues" \
        -f title="$WU_TITLE" \
        -f body="$WU_BODY" \
        -f "labels[]=retro" \
        -f "labels[]=priority:P${WU_PRIORITY_NUM}" \
        -f "labels[]=comp:${WU_COMP_SLUG}" \
        --jq '"\(.id)\t\(.number)\t\(.html_url)"' 2>&1) \
        && [ -n "$WU_TSV" ] && [[ "$WU_TSV" == *https://* ]]; then
      IFS=$'\t' read -r WU_DB_ID WU_NUM WU_URL <<<"$WU_TSV"

      # Link the new issue under the parent. The REST sub-issues endpoint
      # exists on github.com and most GHES versions; older GHES instances
      # return 404 here.
      if LINK_OUT=$(gh api --method POST \
          -H "Accept: application/vnd.github+json" \
          -H "X-GitHub-Api-Version: 2022-11-28" \
          "/repos/$REPO/issues/$PARENT_NUM/sub_issues" \
          -F "sub_issue_id=$WU_DB_ID" \
          --jq '.id // .number // empty' 2>&1) \
          && [ -n "$LINK_OUT" ]; then
        STATUS="ok"
      else
        STATUS="link-failed"
        FAIL_MSG="WU-$((wu_idx+1)) (#$WU_NUM) — sub-issue link failed (cross-link in body remains)"
      fi
    else
      STATUS="create-failed"
      FAIL_MSG="WU-$((wu_idx+1)) ($WU_TITLE) — issue creation failed: ${WU_TSV:-no-response}"
    fi

    # One field per line — survives tabs/spaces in titles.
    {
      printf '%s\n' "$STATUS"
      printf '%s\n' "$WU_URL"
      printf '%s\n' "$WU_NUM"
      printf '%s\n' "$WU_TITLE"
      printf '%s\n' "$WU_PRIORITY_LABEL"
      printf '%s\n' "$WU_COMP_SLUG"
      printf '%s\n' "$WU_COMPLEXITY"
      printf '%s\n' "$FAIL_MSG"
    } > "$WU_TMPDIR/wu-$wu_idx"
  ) &
done

wait

# Collect in original WU order so the table indexing (WU-1, WU-2, ...) matches
# what the parent body's prose references.
for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  {
    IFS= read -r STATUS
    IFS= read -r URL
    IFS= read -r NUM
    IFS= read -r TITLE
    IFS= read -r PRIORITY
    IFS= read -r COMP
    IFS= read -r COMPLEXITY
    IFS= read -r FAIL_MSG
  } < "$WU_TMPDIR/wu-$wu_idx"

  WU_URLS+=("$URL")
  WU_NUMS+=("$NUM")
  WU_TITLES+=("$TITLE")
  WU_PRIORITIES+=("$PRIORITY")
  WU_COMP_SLUGS+=("$COMP")
  WU_COMPLEXITIES+=("$COMPLEXITY")
  WU_STATUSES+=("$STATUS")

  case "$STATUS" in
    ok)
      echo "WU-$((wu_idx+1)) linked: $URL"
      ;;
    create-failed)
      echo "WARNING: $FAIL_MSG"
      FAILED_WUS+=("$FAIL_MSG")
      ;;
    link-failed)
      echo "WARNING: $FAIL_MSG"
      FAILED_WUS+=("$FAIL_MSG")
      SUB_ISSUE_API_OK=false
      ;;
  esac
done

rm -rf "$WU_TMPDIR"
```

Three distinct failure modes, three distinct outcomes:

| Mode | What happened | What the parent shows | What the user sees in Phase 6 Step 6 |
|------|---------------|----------------------|--------------------------------------|
| `create-failed` | `gh api POST /repos/.../issues` for the WU returned no usable response | WU table row reads `FAILED — file manually` | `FAILED_WUS` summary names this WU |
| `link-failed` | Issue created OK but sub-issues REST POST failed | WU table row links the issue normally; native sub-issue panel won't include it | `FAILED_WUS` notes the issue exists but isn't natively linked |
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
| All retro findings | SKILL.md Phase 4 | Used to populate the parent's Summary section and each WU sub-issue's "Findings absorbed" section |

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
first; keep Session Stats, the Summary section, the WU table, and Artifacts.
