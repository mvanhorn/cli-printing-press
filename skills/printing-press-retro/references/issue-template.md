# GitHub Issue Template

Read this file during Phase 6 when filing GitHub issues from retro findings.

Each work unit becomes one flat top-level issue. There is no parent, no
sub-issue hierarchy. The "Findings absorbed" section inside each issue
groups the 1+ findings that share a single fix.

This shape was chosen deliberately:

- A retro of N work units produces N issues, each independently trackable —
  open, close, assign, comment.
- The default GitHub issue list is not duplicated; there is no parent to
  mirror children, no progress bar to drift, no parent-not-auto-closing
  bookkeeping.
- Cross-retro discovery still works through labels: `source:retro` marks
  provenance, `comp:<slug>` surfaces every retro WU touching one component,
  `priority:P1` surfaces high-priority work, and `bug`/`enhancement` carry the
  real issue type.
- Inter-issue links inside the same retro are not auto-generated. They appear
  only when an issue genuinely relates to another (a contradicting prior
  retro, or a `related-area` open issue surfaced by the dedup scan).

## Formatting rules

Use the `F` prefix for findings (`F1`, `F3`) and `WU-` for work units inside
issue bodies. **Real GitHub issue references in the `Related issues` block
are intentional `#N`** — that's where we *want* GitHub to auto-link the
timelines together, because those references reach across retros and across
filed work.

## Execution principles (read before running any step)

The retro doc and the manuscript zip are the durable audit trail; each
issue is an action surface. Optimize the issue path for speed and signal:

- **Issue body is self-contained for debugging.** An agent picking up an
  issue should be able to start fixing it without opening the manuscripts
  attachment. Concrete file paths, line numbers, command output, and spec
  excerpts go in the body. The retro doc URL stays as a supplement, not a
  required read.
- **Don't manufacture confidence.** If the retro analyst can't confidently
  isolate one root cause or one fix, the body should say so — list candidate
  causes/fixes and how to disambiguate. Wrong-but-plausible prescriptions
  are worse than honest uncertainty.
- **Title is succinct and problem-stated.** Describes what's wrong, not how
  to fix it. No priority prefix (`[P1]`), no WU ordinal (`WU-1`) — both
  belong on labels and in the user-facing summary, not in the title that
  someone scans across retros.
- **Build bodies inline; never via the Write tool.** Use shell heredocs
  into variables (or `printf` into bash-resident temp files inside a single
  `Bash` invocation). Per-WU `Write` tool round-trips are the single largest
  source of perceived latency the skill historically had. *Bash-resident
  temp files passed to `gh ... --body-file` are fine* — they're written and
  consumed inside one Bash call with no tool round-trip and unblock the
  Phase 6 Step 3 scrub step (`scrub_body` needs a file input/output, and
  `gh --body-file` is the natural consumer of the scrubbed output).
- **Run issue creates and comments in parallel.** Each WU's filing is
  independent of every other WU. Background subshells writing to indexed
  temp files, then `wait`, then read in order — the pattern is in Step 3.
  - **Anti-pattern:** Do not use `URL=$(gh issue create ... &)` or
    `URL=$(gh issue comment ... &)`. The `&` runs inside the command
    substitution subshell, the subshell exits before `gh` finishes, `URL`
    is empty, and the still-running `gh` process can create or comment
    successfully in the background. Retrying then produces duplicate work.
    Use Step 3's `(gh ... > "$ISSUE_TMPDIR/issue-$wu_idx") &` capture-file
    shape instead.

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
  "comp:scorer" "comp:skill" \
  "priority:P1" "priority:P2" "priority:P3" \
  "bug" "enhancement" "source:retro"; do
  if ! printf '%s\n' "$EXISTING_LABELS" | grep -qFx "$required"; then
    NEED_CREATE=true
    break
  fi
done

if [ "$NEED_CREATE" = true ]; then
  ensure_label() {
    local name="$1" color="$2" desc="$3"
    gh label create "$name" --repo "$REPO" --color "$color" --description "$desc" 2>/dev/null || true
  }

  # Component labels (5) — drive cross-retro discovery (`gh issue list --label comp:<slug>`)
  ensure_label "comp:generator"      "5319e7" "Generator templates (internal/generator/)"
  ensure_label "comp:openapi-parser" "5319e7" "OpenAPI parser (internal/openapi/)"
  ensure_label "comp:spec-parser"    "5319e7" "Internal spec parser (internal/spec/)"
  ensure_label "comp:scorer"         "5319e7" "verify / dogfood / scorecard"
  ensure_label "comp:skill"          "5319e7" "skills/printing-press/SKILL.md and related skill instructions"

  # Priority labels (3) — drive priority-based filtering. The label is the
  # primary carrier; titles do not duplicate the priority prefix.
  ensure_label "priority:P1" "b60205" "High priority: safety, correctness, release, or broad user impact"
  ensure_label "priority:P2" "d93f0b" "Medium priority: meaningful recurring defect or capability gap"
  ensure_label "priority:P3" "fbca04" "Low priority: useful systemic improvement"

  # Real issue types. These are actionable taxonomy, not routing or provenance.
  ensure_label "bug"         "d73a4a" "Something isn't working"
  ensure_label "enhancement" "a2eeef" "New feature or request"

  # Canonical provenance marker. Do not create the legacy `retro` label.
  ensure_label "source:retro" "c59c0f" "Issue produced by /printing-press-retro; systemic Printing Press finding"
fi

REFRESHED_LABELS=$(gh label list --repo "$REPO" --limit 200 --json name --jq '.[].name' 2>/dev/null || true)
if [ -n "$REFRESHED_LABELS" ]; then
  EXISTING_LABELS="$REFRESHED_LABELS"
fi
```

The `retro-parent` label is intentionally omitted — there are no parent
issues. If the legacy `retro` label exists from prior retros, leave it for
discovery during the cutover; the skill never creates new issues with it.

### Resolve the provenance marker for writes

`source:retro` is the canonical provenance label. Before creating an issue,
choose a marker from labels that are actually available, preferring
`source:retro` and falling back to legacy `retro` only when the canonical label
cannot be created or is not available. Never pass a guessed label to `gh issue
create`:

```bash
has_label() {
  printf '%s\n' "$EXISTING_LABELS" | grep -qFx "$1"
}

RETRO_PROVENANCE_LABEL=""
if has_label "source:retro"; then
  RETRO_PROVENANCE_LABEL="source:retro"
elif has_label "retro"; then
  RETRO_PROVENANCE_LABEL="retro"
else
  gh label create "source:retro" --repo "$REPO" --color "c59c0f" \
    --description "Issue produced by /printing-press-retro; systemic Printing Press finding" 2>/dev/null || true
  EXISTING_LABELS=$(gh label list --repo "$REPO" --limit 200 --json name --jq '.[].name' 2>/dev/null || echo "")
  if has_label "source:retro"; then
    RETRO_PROVENANCE_LABEL="source:retro"
  elif has_label "retro"; then
    RETRO_PROVENANCE_LABEL="retro"
  else
    echo "ERROR: neither source:retro nor legacy retro is available; cannot file a provenance-marked issue." >&2
    exit 1
  fi
fi
```

The initial label pass provisions `source:retro` on a fresh fork. The
post-create re-list makes the fallback safe when label creation races or is
denied. Existing issues are not relabeled solely to rename provenance.

## Step 2: Sort work units

Sort WUs by priority: P1 first, then P2, then P3. Within a priority bucket,
keep the order they appeared in Phase 5.5 (typically by ascending WU number,
but the skill may have intentionally ordered them by dependency — preserve
that).

```bash
# SORTED_WORK_UNITS is populated by sorting the complete WU records from
# $WORK_UNITS P1 → P3. Each record retains its own `Stable ID: WU-N` field;
# never sort a work-unit array and an ID array independently.
# Dependency edges use the stable ID extracted from each sorted record, never
# a sorted array position.
```

## Step 2.5: Dedup against open issues

Before filing, check whether any WUs match an issue that's already open.
If they do, comment on the existing issue with new evidence rather than
file a duplicate.

This is two label-filtered `gh` calls followed by per-WU agent reasoning over
the de-duplicated result set. **It does not need to be bulletproof** — false negatives (filing new when
one exists) are recoverable; false positives (commenting on the wrong
issue) are uglier. **Bias toward `file-new` when uncertain.**

### Fetch open retro issues

```bash
EXISTING_OPEN_RETROS_SOURCE=$(gh issue list \
  --repo "$REPO" \
  --label source:retro \
  --state open \
  --limit 200 \
  --json number,title,url,labels 2>/dev/null \
  || echo "[]")

EXISTING_OPEN_RETROS_LEGACY=$(gh issue list \
  --repo "$REPO" \
  --label retro \
  --state open \
  --limit 200 \
  --json number,title,url,labels 2>/dev/null \
  || echo "[]")

EXISTING_OPEN_RETROS=$(jq -s 'add | unique_by(.number)' \
  <(printf '%s\n' "$EXISTING_OPEN_RETROS_SOURCE") \
  <(printf '%s\n' "$EXISTING_OPEN_RETROS_LEGACY"))
```

The two results are merged by issue number so an issue carrying both labels is
considered once. No per-WU component filtering — the agent reasons over titles
across the whole open-retro set so a related-area issue under a different
component still surfaces. The `labels` field lets the executor preserve the
one-type invariant when commenting on an existing match.

### Classify each WU against the candidate set

For each WU in `$SORTED_WORK_UNITS`, the skill executor (the agent running
this skill) reads the WU's title and summary, then scans
`$EXISTING_OPEN_RETROS` for matches. Per-candidate verdict:

| Verdict | When | Effect |
|---|---|---|
| `same` | Title and summary describe the same root problem already filed (high confidence) | Marks WU for `comment` instead of create |
| `related-area` | Different problem but in adjacent territory worth cross-referencing (cursor handling vs. cursor format on same template; auth refresh on different envelope) | Will be cited in the new issue's `Related issues` block via `#N` |
| `unrelated` | No meaningful overlap | Ignored |

Per WU, fold the per-candidate verdicts into a single `$WU_DEDUP[i]` slot:

```
$WU_DEDUP[i]:
  "comment:NN"        — at least one candidate was `same`. Comment on NN.
                        If multiple `same`, pick the most-recent and treat
                        the rest as related (folded into $WU_RELATED).
  "" (empty)          — no `same`. The agent will create a new issue.
```

And capture related issues separately:

```
$WU_RELATED[i]:
  comma-separated list of issue numbers classified as `related-area`,
  combined with prior-retro issue numbers from Phase 3 Step D.
  Empty when neither scan surfaced anything.
```

The decisions feed forward into the Phase 6 Step 2 confirm summary so the
user sees what will happen and can override before anything is filed.

## Step 3: Build bodies and execute (in parallel)

For each WU, build either an issue body or a comment body, then run `gh`
in a background subshell. `wait`, then collect.

**Layer 0 body scrub is mandatory.** Before posting any body to GitHub, the
parallel loop runs `scrub_body` on the body file. Source the function from
[`secret-scrubbing.md`](secret-scrubbing.md) "Layer 0" at the top of the
bash block that contains the loop — paste the `scrub_body() { ... }`
definition verbatim, or `source` an equivalent shell fragment. Without this,
the loop will fail with `scrub_body: command not found` per-WU.

### Issue title (for new issues)

Succinct, problem-stated. Examples:

- ✅ `Spec-declared version headers dropped during generation`
- ❌ `Emit Stripe-Version header from spec` *(prescribes the fix)*
- ✅ `Sync template misroutes cursor pagination`
- ❌ `[P1] WU-1: Switch sync template to cursor-aware pagination` *(priority + WU number + prescription)*

### Issue body (for new issues)

The body must be self-contained — an agent should be able to act on it
without opening the manuscripts attachment. Use the shape below.

```markdown
> Filed by `/printing-press-retro` from a run of **<api-display-name>**
> (scorecard <score>/100, <X>% verify pass).

## Summary

<2-4 sentences: what the problem is and why it matters. Stay
problem-shaped, not solution-shaped. Someone reading this paragraph alone
should understand what they're being asked to address.>

## Where to look

- **Component:** <comp-slug>
- **Issue type:** `bug` or `enhancement` from the deterministic finding-category mapping
- **Likely area:** <path or files in the printing-press repo, e.g. `internal/generator/templates/`>
- **Triggered when:** <spec shape, API behavior, or runtime context that surfaces this>

## What we observed

<Concrete evidence usable to reproduce or locate the issue without opening
attachments. This is the meat of the body — be specific.>

- File paths and line numbers (in the printed CLI or the printing-press repo)
- Command + output snippet showing the failure
- Spec snippet showing the trigger condition
- Error messages, stack traces, or scorer output verbatim where relevant

> ⚠️ **Redact secrets and PII before pasting.** Issue bodies are public. When
> the evidence comes from scanner output, dogfood payloads, Greptile review
> comments, or live API responses, replace credentials with
> `<REDACTED:<vendor>-<kind>:<first4>...<last4>:<len>ch>` and PII with
> `<REDACTED:<kind>>` per [`secret-scrubbing.md`](secret-scrubbing.md)
> "Layer 0". The Phase 6 Step 3 loop runs `scrub_body` on this file and will
> hard-fail the WU's filing if it finds an unredacted vendor-prefix token —
> that's the floor, not a substitute for redacting at write time.
> **This applies most strictly to findings about secret/PII leaks**: quoting
> the actual leaked value to prove the leak exists re-leaks it.

## Suspected root cause

<Hypothesis with explicit confidence level. Don't manufacture certainty.>

- **If certain:** "The template at `internal/generator/templates/sync.go.tmpl`
  hardcodes `pageToken` as the cursor param name."
- **If uncertain:** "Likely the openapi-parser is dropping the version
  header on parse, but the generator template could also be omitting the
  emit. Either could be the root cause; verify by checking parser output
  before pinning a fix."

## Suggested direction

<Proposed fix direction with explicit confidence level. Offer alternatives
when reasonable doubts exist; always include a verification step.>

- **If certain:** "Add a profiler check in `internal/openapi/parser.go`
  that detects spec-declared version headers and writes them to the
  generator config. Emit them in the client template alongside auth headers."
- **If uncertain:** "Direction A: surface the field through the parser;
  Direction B: post-process in the generator. A is more durable but
  requires changes in two packages. Reproduce the failure with the
  manuscripts evidence first, then choose."

## Acceptance criteria

- positive: <test that proves the fix works on the API class this targets>
- negative: <test that proves the fix doesn't regress unaffected APIs>

## Frequency

every / most APIs / subclass:<name> / this API only

## Complexity

small / medium / large

## Scope boundary

<What this issue does NOT include. Helps prevent fix creep. Skip if not applicable.>

## Dependencies

<"None." unless this work unit has a real prerequisite. For an explicit
prerequisite, name the blocking issue or work unit here. After issue numbers are
known, the filing pass applies GitHub's native relationship with
`gh issue edit <dependent> --repo "$REPO" --add-blocked-by <prerequisite>`.
Related-area references do not belong here and must remain prose in
`Related issues`.

## Findings absorbed

*Omit if 1:1 (the body above is the finding).*

#### F<n>: <title>

- <one-sentence symptom>
- Evidence: <where it showed up>

#### F<n>: ...

## Related issues

<Combined output from Phase 3 Step D (prior-retro doc archaeology) and
Step 2.5 (open-issue dedup `related-area` classification). Auto-cross-links
via `#N`. Sibling WUs in this same retro do NOT appear here unless one is
genuinely a prerequisite; a real prerequisite is also represented by a native
`blocked-by`/`blocking` relationship, not only by this prose link.>

- #<num> — prior retro (`aligned`/`contradicts`/`extends`): <one-sentence note>
- #<num> — open issue (`related-area`): <one-sentence note on the adjacency>
- *(or "None" when neither scan surfaced anything)*

## Artifacts

| Artifact | Link |
|----------|------|
| Retro document | <$RETRO_DOC_URL or "Upload failed — see local copy"> |
| Manuscripts (research + proofs) | <$MANUSCRIPTS_URL or "Upload failed — see local copy"> |
| Generated CLI source code | <$CLI_SOURCE_URL or "Upload failed — see local copy"> |

---

*Generated by `/printing-press-retro` · [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)*
```

What's *not* in the body, by design:

- **Session Stats table** — collapsed into the one-line provenance header.
  The retro doc has the full stats.
- **What the Printing Press Got Right** — retro-wide; lives in the retro doc.
- **Skipped table** — retro-wide triage record; lives in the retro doc.
- **Auto-cross-references to sibling WUs in this same retro** — these are
  noise unless one WU is genuinely a prerequisite for another (and that
  goes in `Dependencies:` and becomes a native `blocked-by` relationship after
  issue numbers are known).

### Comment body (for `comment:#N` decisions)

```markdown
**Recurrence in <api-display-name> retro** *(<retro-date>)*

Same problem surfaced again in this run. New observations:

- <concrete observation 1: file:line, command output, etc.>
- <concrete observation 2>

Updated frequency estimate: <every / most / subclass:<name>>

[Full retro doc](<$RETRO_DOC_URL>) · [Manuscripts](<$MANUSCRIPTS_URL>)

---

*Comment added by `/printing-press-retro` from the <api-display-name> retro.*
```

The comment is intentionally short — it's recurrence evidence, not a
re-statement of the original issue. The retro doc carries the full audit
trail for anyone who wants more context.

### Parallel execution

```bash
declare -a OUTCOME_KIND OUTCOME_URL OUTCOME_TITLE OUTCOME_PRIORITY OUTCOME_COMP OUTCOME_COMPLEXITY
declare -a WU_DEPENDENCY_EDGES
declare -a FAILED_ISSUES
declare -A OUTCOME_ISSUE_NUM_BY_WU_ID SORTED_WU_ID_SEEN

extract_wu_id() {
  printf '%s\n' "$1" \
    | sed -nE 's/^- \*\*Stable ID:\*\* (WU-[1-9][0-9]*)([[:space:]].*)?$/\1/p' \
    | head -1
}

ISSUE_TMPDIR=$(mktemp -d)
ISSUE_RUN_START_ISO=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  WU_ID="$(extract_wu_id "${SORTED_WORK_UNITS[$wu_idx]}")"
  if [[ ! "$WU_ID" =~ ^WU-[1-9][0-9]*$ ]]; then
    echo "ERROR: sorted WU $wu_idx has missing or duplicate stable ID: ${WU_ID:-<empty>}" >&2
    exit 1
  fi
  if [[ -n "${SORTED_WU_ID_SEEN[$WU_ID]+x}" ]]; then
    echo "ERROR: sorted WU $wu_idx has duplicate stable ID: $WU_ID" >&2
    exit 1
  fi
  SORTED_WU_ID_SEEN["$WU_ID"]=1
done

for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  (
    WU="${SORTED_WORK_UNITS[$wu_idx]}"
    WU_ID="$(extract_wu_id "$WU")"
    DEDUP="${WU_DEDUP[$wu_idx]}"  # "comment:NN" or empty

    # Each WU contributes: $WU_ID, $WU_TITLE, $WU_BODY, $WU_COMMENT_BODY,
    # $WU_PRIORITY_NUM, $WU_PRIORITY_LABEL, $WU_TYPE_LABEL, $WU_COMP_SLUG,
    # $WU_COMPLEXITY.

    KIND=""
    URL=""
    FAIL_MSG=""

    # Layer 0 body scrub — runs once per WU on whichever body is about to be
    # posted. scrub_body is defined in references/secret-scrubbing.md and must
    # be sourced or pasted in the bash block enclosing this loop.
    #
    # Hard-fail behavior: if the body contains an unredacted vendor-prefix
    # token (lin_api_, sk_live_, ghp_, etc.) we refuse to post that WU and
    # surface it in $FAILED_ISSUES so the agent can hand-redact and retry.
    # PII patterns auto-redact in place. The original $WU_BODY /
    # $WU_COMMENT_BODY shell var is untouched; only the file passed to gh
    # gets the scrubbed text.
    BODY_TMP="$ISSUE_TMPDIR/body-$wu_idx.md"
    BODY_TMP_SCRUBBED="$ISSUE_TMPDIR/body-$wu_idx.scrubbed.md"
    if [[ "$DEDUP" == comment:* ]]; then
      printf '%s' "$WU_COMMENT_BODY" > "$BODY_TMP"
    else
      printf '%s' "$WU_BODY" > "$BODY_TMP"
    fi
    if ! scrub_body "$BODY_TMP" "$BODY_TMP_SCRUBBED" 2>"$ISSUE_TMPDIR/scrub-$wu_idx.err"; then
      KIND="scrub-failed"
      SCRUB_REASON=$(tr '\n' ' ' < "$ISSUE_TMPDIR/scrub-$wu_idx.err" | head -c 400)
      FAIL_MSG="$WU_TITLE — body scrub hard-failed (vendor-prefix secret in body); not posted. Reason: $SCRUB_REASON. Body left at $BODY_TMP for hand-redaction."
      URL=""
    elif [[ "$DEDUP" == comment:* ]]; then
      ISSUE_NUM="${DEDUP#comment:}"
      # A recurrence may target a legacy issue that predates real type labels.
      # Normalize the two actionable type labels before adding evidence so the
      # existing issue still satisfies the one-type taxonomy invariant.
      if ! gh issue edit "$ISSUE_NUM" \
            --repo "$REPO" \
            --remove-label bug \
            --remove-label enhancement \
            --add-label "$WU_TYPE_LABEL" >/dev/null 2>&1; then
        KIND="type-normalization-failed"
        FAIL_MSG="$WU_TITLE — could not normalize #$ISSUE_NUM to exactly one type label ($WU_TYPE_LABEL); comment not posted."
        URL=""
      elif URL=$(gh issue comment "$ISSUE_NUM" \
            --repo "$REPO" \
            --body-file "$BODY_TMP_SCRUBBED" 2>&1) \
            && [[ "$URL" == https://* ]]; then
        KIND="commented"
      else
        KIND="comment-failed"
        FAIL_MSG="$WU_TITLE — comment on #$ISSUE_NUM failed: ${URL:-no-response}"
        URL=""
      fi
    else
      if URL=$(gh issue create \
            --repo "$REPO" \
            --title "$WU_TITLE" \
            --body-file "$BODY_TMP_SCRUBBED" \
            --label "$RETRO_PROVENANCE_LABEL" \
            --label "$WU_TYPE_LABEL" \
            --label "priority:P${WU_PRIORITY_NUM}" \
            --label "comp:${WU_COMP_SLUG}" 2>&1) \
            && [[ "$URL" == https://* ]]; then
        KIND="created"
      else
        KIND="create-failed"
        FAIL_MSG="$WU_TITLE — issue creation failed: ${URL:-no-response}"
        URL=""
      fi
    fi

    {
      printf '%s\n' "$WU_ID"
      printf '%s\n' "$KIND"
      printf '%s\n' "$URL"
      printf '%s\n' "$WU_TITLE"
      printf '%s\n' "$WU_PRIORITY_LABEL"
      printf '%s\n' "$WU_COMP_SLUG"
      printf '%s\n' "$WU_COMPLEXITY"
      printf '%s\n' "$FAIL_MSG"
    } > "$ISSUE_TMPDIR/issue-$wu_idx"
  ) &
done

wait

# Defensive duplicate detector. If an agent accidentally used the malformed
# `URL=$(gh issue create ... &)` shortcut outside this reference, those
# backgrounded issue creates may succeed even though the captured URL is empty.
# Extra issues created by the current user since the run began, beyond the issue
# numbers captured in per-WU temp files, signal that parallel filing leaked
# creates. Use the live REST list endpoint instead of search, whose index can lag
# immediately after creation.
EXPECTED_ISSUE_NUMBERS=$(
  for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
    if [ -f "$ISSUE_TMPDIR/issue-$wu_idx" ]; then
      {
        IFS= read -r WU_ID_TMP
        IFS= read -r KIND_TMP
        IFS= read -r URL_TMP
      } < "$ISSUE_TMPDIR/issue-$wu_idx"
      if [[ "$KIND_TMP" == created && "$URL_TMP" =~ /issues/([0-9]+)$ ]]; then
        printf '%s\n' "${BASH_REMATCH[1]}"
      fi
    fi
  done | sort -u
)
CURRENT_GH_USER=$(gh api user --jq .login 2>/dev/null || true)
RECENT_CREATED_LINES=""
if [ -n "$CURRENT_GH_USER" ]; then
  RECENT_CREATED_LINES=$(gh api --method GET "repos/$REPO/issues" \
    -f state=open \
    -f since="$ISSUE_RUN_START_ISO" \
    -f sort=created \
    -f direction=desc \
    -f per_page=100 \
    --jq ".[] | select((.pull_request | not) and .user.login == \"$CURRENT_GH_USER\" and .created_at >= \"$ISSUE_RUN_START_ISO\") | \"#\(.number) \(.title)\"" 2>/dev/null || true)
fi
UNEXPECTED_CREATED_LINES=$(printf '%s\n' "$RECENT_CREATED_LINES" | sed '/^$/d')
if [ -n "$EXPECTED_ISSUE_NUMBERS" ]; then
  while IFS= read -r issue_num; do
    [ -z "$issue_num" ] && continue
    UNEXPECTED_CREATED_LINES=$(printf '%s\n' "$UNEXPECTED_CREATED_LINES" | grep -v "^#$issue_num " || true)
  done <<EOF
$EXPECTED_ISSUE_NUMBERS
EOF
fi
EXPECTED_CREATES=0
for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  if [[ "${WU_DEDUP[$wu_idx]}" != comment:* ]]; then
    KIND_TMP=$(head -1 "$ISSUE_TMPDIR/issue-$wu_idx" 2>/dev/null)
    [[ "$KIND_TMP" == scrub-failed ]] || EXPECTED_CREATES=$((EXPECTED_CREATES + 1))
  fi
done
UNEXPECTED_CREATED_COUNT=$(printf '%s\n' "$UNEXPECTED_CREATED_LINES" | sed '/^$/d' | wc -l | tr -d ' ')
if [ "$UNEXPECTED_CREATED_COUNT" -gt 0 ]; then
  printf 'WARNING: %s unexpected issue(s) were created by the current user since %s, outside this retro'\''s %s expected new issue(s). Check for duplicate issues before presenting results.\n' \
    "$UNEXPECTED_CREATED_COUNT" "$ISSUE_RUN_START_ISO" "$EXPECTED_CREATES" >&2
  printf '%s\n' "$UNEXPECTED_CREATED_LINES" | sed 's/^/  /' >&2
fi

for wu_idx in "${!SORTED_WORK_UNITS[@]}"; do
  {
    IFS= read -r WU_ID
    IFS= read -r KIND
    IFS= read -r URL
    IFS= read -r TITLE
    IFS= read -r PRIORITY
    IFS= read -r COMP
    IFS= read -r COMPLEXITY
    IFS= read -r FAIL_MSG
  } < "$ISSUE_TMPDIR/issue-$wu_idx"

  EXPECTED_WU_ID="$(extract_wu_id "${SORTED_WORK_UNITS[$wu_idx]}")"
  if [[ "$WU_ID" != "$EXPECTED_WU_ID" ]]; then
    FAILED_ISSUES+=("sorted WU $wu_idx returned stable ID ${WU_ID:-<empty>}, expected ${EXPECTED_WU_ID:-<empty>}")
    continue
  fi

  OUTCOME_KIND+=("$KIND")
  OUTCOME_URL+=("$URL")
  OUTCOME_TITLE+=("$TITLE")
  OUTCOME_PRIORITY+=("$PRIORITY")
  OUTCOME_COMP+=("$COMP")
  OUTCOME_COMPLEXITY+=("$COMPLEXITY")
  if [[ "$URL" =~ /issues/([0-9]+) ]]; then
    OUTCOME_ISSUE_NUM_BY_WU_ID["$WU_ID"]="${BASH_REMATCH[1]}"
  elif [[ "${WU_DEDUP[$wu_idx]}" == comment:* ]]; then
    OUTCOME_ISSUE_NUM_BY_WU_ID["$WU_ID"]="${WU_DEDUP[$wu_idx]#comment:}"
  else
    OUTCOME_ISSUE_NUM_BY_WU_ID["$WU_ID"]=""
  fi

  case "$KIND" in
    created|commented)
      echo "${KIND^}: $URL"
      ;;
    create-failed|comment-failed|type-normalization-failed|scrub-failed)
      echo "WARNING: $FAIL_MSG"
      FAILED_ISSUES+=("$FAIL_MSG")
      ;;
  esac
done

# Apply only explicitly declared prerequisite edges, after all creates and
# dedup decisions have produced issue numbers. Each edge is
# `WU-2|wu:WU-1` or `WU-2|issue:123`. IDs are stable WU-N values, so this
# remains correct when a P1 WU-2 sorts before a P2 WU-1. Resolve both endpoints
# through the associative outcome map; never use sorted array positions.
# Related-area references never enter this array; they remain prose links.
for edge in "${WU_DEPENDENCY_EDGES[@]}"; do
  IFS='|' read -r dependent_id prerequisite_ref <<< "$edge"
  if [[ ! "$dependent_id" =~ ^WU-[1-9][0-9]*$ ]]; then
    FAILED_ISSUES+=("dependency edge $edge — unknown or malformed dependent stable WU ID")
    continue
  fi
  if [[ -z "${SORTED_WU_ID_SEEN[$dependent_id]+x}" ]]; then
    FAILED_ISSUES+=("dependency edge $edge — unknown dependent stable WU ID")
    continue
  fi
  dependent_issue="${OUTCOME_ISSUE_NUM_BY_WU_ID[$dependent_id]-}"
  case "$prerequisite_ref" in
    wu:*)
      prerequisite_id="${prerequisite_ref#wu:}"
      if [[ ! "$prerequisite_id" =~ ^WU-[1-9][0-9]*$ ]]; then
        FAILED_ISSUES+=("dependency edge $edge — unknown or malformed prerequisite stable WU ID")
        continue
      fi
      if [[ -z "${SORTED_WU_ID_SEEN[$prerequisite_id]+x}" ]]; then
        FAILED_ISSUES+=("dependency edge $edge — unknown prerequisite stable WU ID")
        continue
      fi
      prerequisite_issue="${OUTCOME_ISSUE_NUM_BY_WU_ID[$prerequisite_id]-}"
      ;;
    issue:*)
      prerequisite_issue="${prerequisite_ref#issue:}"
      if [[ ! "$prerequisite_issue" =~ ^[1-9][0-9]*$ ]]; then
        FAILED_ISSUES+=("dependency edge $edge — malformed existing issue number")
        continue
      fi
      ;;
    *)
      prerequisite_issue=""
      ;;
  esac
  if [[ -z "$dependent_issue" || -z "$prerequisite_issue" ]]; then
    FAILED_ISSUES+=("dependency edge $edge — issue number unavailable; native relationship not applied")
  elif ! gh issue edit "$dependent_issue" --repo "$REPO" \
      --add-blocked-by "$prerequisite_issue" >/dev/null 2>&1; then
    FAILED_ISSUES+=("dependency edge #$dependent_issue blocked by #$prerequisite_issue — native relationship failed")
  fi
done

# Cleanup is conditional on scrub-failed WUs. Those WUs' body files are the
# canonical hand-redaction source the failure message points at — wiping
# $ISSUE_TMPDIR would destroy the recovery path. Keep the dir alive when any
# WU scrub-failed; the agent (or user) can read the body file, hand-redact,
# and re-run the affected WUs without re-deriving the body. The dir is in
# $(mktemp -d) so it self-cleans on OS reboot / `tmpwatch` regardless.
SCRUB_FAILED_COUNT=0
for KIND in "${OUTCOME_KIND[@]}"; do
  [ "$KIND" = "scrub-failed" ] && SCRUB_FAILED_COUNT=$((SCRUB_FAILED_COUNT + 1))
done
if [ "$SCRUB_FAILED_COUNT" -eq 0 ]; then
  rm -rf "$ISSUE_TMPDIR"
else
  echo "NOTE: $SCRUB_FAILED_COUNT WU body file(s) preserved at $ISSUE_TMPDIR for hand-redaction. Delete manually after retrying the affected WU(s)." >&2
fi
```

Failure modes:

| Mode | What happened | What the user sees in Phase 6 Step 6 |
|---|---|---|
| `created` | New issue filed with labels | Listed in success summary |
| `commented` | Comment added to existing issue | Listed as "commented on #N" |
| `create-failed` | `gh issue create` returned no usable URL | `$FAILED_ISSUES` summary; manual filing instructions |
| `comment-failed` | `gh issue comment` failed | `$FAILED_ISSUES` summary; manual comment instructions |
| `type-normalization-failed` | Existing matched issue could not be normalized to exactly one real type label | `$FAILED_ISSUES` summary; manual label repair instructions |
| `scrub-failed` | Body contained an unredacted vendor-prefix secret; `scrub_body` refused to write the scrubbed copy. Body file left at `$BODY_TMP` for hand-redaction | `$FAILED_ISSUES` summary; agent must hand-redact per `secret-scrubbing.md` Layer 0 and retry the WU |

## Variables expected

| Variable | Set by | Contains |
|---|---|---|
| `$REPO` | This file Step 1 | Owner/repo string for `gh` |
| `$RETRO_DOC_URL` | artifact-packaging.md | catbox URL for retro .md, or empty |
| `$MANUSCRIPTS_URL` | artifact-packaging.md | catbox URL or empty |
| `$CLI_SOURCE_URL` | artifact-packaging.md | catbox URL or empty |
| `$RETRO_PROOF_PATH` | SKILL.md Phase 5 | Path to saved retro in manuscript proofs |
| `$RETRO_SCRATCH_PATH` | SKILL.md Phase 5 | Path to temp retro copy under `/tmp/printing-press/retro/` |
| `$RETRO_PROVENANCE_LABEL` | This file Step 1 | Available write marker: canonical `source:retro`, or legacy `retro` only during cutover fallback |
| `$WORK_UNITS` | SKILL.md Phase 5.5 | Array of WU records |
| `$SORTED_WORK_UNITS` | This file Step 2 | Complete `$WORK_UNITS` records sorted P1 → P3; each record retains its own stable WU-N identifier |
| `$EXISTING_OPEN_RETROS` | This file Step 2.5 | De-duplicated JSON of open issues carrying `source:retro` or legacy `retro` |
| `$WU_DEDUP` | This file Step 2.5 | Per-WU dedup decision: `comment:NN` or empty |
| `$WU_RELATED` | This file Step 2.5 + Phase 3 Step D | Per-WU comma-separated related-issue numbers (annotated for the body) |
| `$WU_DEPENDENCY_EDGES` | SKILL.md Phase 5.5 + this file Step 3 | Explicit stable-ID prerequisite edges such as `WU-2|wu:WU-1` applied with native `--add-blocked-by`; related-area references are excluded |
| `$OUTCOME_ISSUE_NUM_BY_WU_ID` | This file Step 3 | Issue numbers keyed by stable WU-N, independent of sorted array positions |
| `$WU_TYPE_LABEL` | Each sorted WU record | Exactly one mapped real issue type: `bug` or `enhancement` |
| All retro findings | SKILL.md Phase 4 | Used to populate each issue's "Findings absorbed" section |

## Variables produced

| Variable | Contains |
|---|---|
| `$OUTCOME_KIND` | Array, one per WU: `created` / `commented` / `create-failed` / `comment-failed` / `scrub-failed` |
| `$OUTCOME_URL` | Array of issue/comment URLs (empty for failures) |
| `$FAILED_ISSUES` | Array of human-readable failure descriptions; empty if every WU succeeded |

## Handling `gh` failure (graceful degradation)

Check `gh` auth at the start of Phase 6 Step 4:

```bash
if ! gh auth status 2>/dev/null; then
  echo "GitHub CLI is not authenticated. Cannot create issues or comments."
  GH_AVAILABLE=false
else
  GH_AVAILABLE=true
fi
```

If `GH_AVAILABLE=false`, or if every per-WU action failed, fall back to
printing manual filing instructions:

```bash
if [ "$GH_AVAILABLE" = false ] || [ "${#FAILED_ISSUES[@]}" -eq "${#SORTED_WORK_UNITS[@]}" ]; then
  echo ""
  echo "Could not create issues or comments automatically."
  echo ""
  echo "To file the retro manually:"
  echo "  1. Go to: https://github.com/mvanhorn/cli-printing-press/issues"
  echo "  2. Use the body templates from the retro document at:"
  echo "       $RETRO_PROOF_PATH"
  if [ -n "$RETRO_SCRATCH_PATH" ] && [ -f "$RETRO_SCRATCH_PATH" ]; then
    echo "       $RETRO_SCRATCH_PATH"
  fi
  echo "  3. File one issue per work unit. Apply labels: $RETRO_PROVENANCE_LABEL, bug or enhancement, priority:P<n>, comp:<slug>. Use native --add-blocked-by only for explicit prerequisites; keep related-area references in prose."
  if [ -n "$MANUSCRIPTS_URL" ]; then
    echo "  4. Manuscripts: $MANUSCRIPTS_URL"
  fi
  if [ -n "$CLI_SOURCE_URL" ]; then
    echo "  5. CLI source: $CLI_SOURCE_URL"
  fi
fi
```

Per-WU partial failures (some succeed, some don't) are surfaced through
`$FAILED_ISSUES` in Phase 6 Step 6 — the user sees both the successful
URLs and the failed ones with manual filing instructions for just the
missing ones.

## Handling body size

GitHub issue bodies have a practical limit (~65KB). The flat-issue shape
keeps each issue narrow, but a single WU with many absorbed findings +
long related-issue chains could still approach it. If `gh issue create`
rejects a body for size:

1. Truncate "What we observed" and "Findings absorbed" to one bullet
   per item, with a pointer to the retro doc.
2. Add: "Full finding analysis available in the retro document linked
   under Artifacts."
3. Retry.
