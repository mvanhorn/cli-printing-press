# Library PR Plumbing for `/printing-press-amend`

This reference is loaded by Phase 6 and Phase 7. It carries the fork → managed-clone → branch → commit → push → PR-create patterns adapted from `/printing-press-publish` Steps 5, 7, and 8.

**Drift advisory**: `/printing-press-publish` carries the canonical inline version of these patterns. This file is a copy adapted for amend's use case (existing-CLI patches, not new-CLI publishes). When publish's plumbing changes, this file may drift. A follow-up retro item will extract shared helpers into `scripts/`; until then, audit both surfaces together.

The setup contract environment variables (`PRESS_HOME`, `PRESS_SCOPE`, etc.) are already exported by the time this reference runs — see the SKILL.md's setup contract block.

---

## Step 1 — Resolve managed clone access mode

The managed clone lives at `$PRESS_HOME/.publish-repo-$PRESS_SCOPE`. The auxiliary config at `$PRESS_HOME/.publish-config-$PRESS_SCOPE.json` caches the access mode so detection runs once per scope.

```bash
PUBLISH_REPO_DIR="$PRESS_HOME/.publish-repo-$PRESS_SCOPE"
PUBLISH_CONFIG="$PRESS_HOME/.publish-config-$PRESS_SCOPE.json"

# Read cached config if present
if [ -f "$PUBLISH_CONFIG" ]; then
  managed_by=$(jq -r '.managed_by // empty' "$PUBLISH_CONFIG")
  access=$(jq -r '.access // empty' "$PUBLISH_CONFIG")        # "push" or "fork"
  gh_user=$(jq -r '.gh_user // empty' "$PUBLISH_CONFIG")
  protocol=$(jq -r '.protocol // empty' "$PUBLISH_CONFIG")    # "ssh" or "https"
fi

# Resolve when missing
if [ -z "$access" ]; then
  gh_user=$(gh api user --jq .login)
  push_perm=$(gh api repos/mvanhorn/printing-press-library --jq .permissions.push 2>/dev/null || echo false)
  if [ "$push_perm" = "true" ]; then
    access="push"
  else
    access="fork"
  fi

  # Protocol: prefer SSH if user has it set up
  if ssh -T git@github.com 2>&1 | grep -q "successfully authenticated"; then
    protocol="ssh"
  else
    protocol="https"
  fi

  managed_by="amend"
  jq -n --arg by "$managed_by" --arg a "$access" --arg u "$gh_user" --arg p "$protocol" \
    --arg cp "$PUBLISH_REPO_DIR" --arg sd "$_scope_dir" \
    '{managed_by: $by, access: $a, gh_user: $u, protocol: $p, clone_path: $cp, scope_dir: $sd}' \
    > "$PUBLISH_CONFIG"
fi
```

Reference: publish SKILL.md Step 5 (lines 244-397).

---

## Step 2 — Bootstrap or refresh the managed clone

```bash
if [ ! -d "$PUBLISH_REPO_DIR/.git" ]; then
  # First-time setup
  if [ "$access" = "push" ]; then
    if [ "$protocol" = "ssh" ]; then
      git clone git@github.com:mvanhorn/printing-press-library.git "$PUBLISH_REPO_DIR"
    else
      git clone https://github.com/mvanhorn/printing-press-library.git "$PUBLISH_REPO_DIR"
    fi
    cd "$PUBLISH_REPO_DIR"
    git remote add upstream git@github.com:mvanhorn/printing-press-library.git 2>/dev/null || true
  else
    # Fork-based: ensure user fork exists, clone fork, set upstream
    gh repo fork mvanhorn/printing-press-library --clone=false --remote=false 2>/dev/null || true
    if [ "$protocol" = "ssh" ]; then
      git clone "git@github.com:$gh_user/printing-press-library.git" "$PUBLISH_REPO_DIR"
    else
      git clone "https://github.com/$gh_user/printing-press-library.git" "$PUBLISH_REPO_DIR"
    fi
    cd "$PUBLISH_REPO_DIR"
    git remote add upstream "https://github.com/mvanhorn/printing-press-library.git"
  fi
else
  # Refresh from upstream. -f on checkout discards any local edits left behind
  # by a prior run that aborted between Phase 4's edits and Phase 7's commit —
  # without -f, those uncommitted changes block the checkout and the subsequent
  # reset --hard never runs, leaving the clone permanently stuck on an amend
  # branch with conflicting state.
  cd "$PUBLISH_REPO_DIR"
  git fetch upstream main
  git checkout -f main
  git reset --hard upstream/main
fi
```

The reset-hard + force-checkout on refresh is intentional: the managed clone is treated as a scratch surface, never as long-term local-state storage. Any local edits in it are by definition leftover state from an aborted run and must be discarded before the next run reuses the clone.

---

## Step 3 — Resolve target CLI directory inside the clone

```bash
# Category was resolved in Phase 1; if missing, look it up by walking
if [ -z "$category" ]; then
  category=$(find "$PUBLISH_REPO_DIR/library" -maxdepth 2 -name "$slug" -type d \
    | head -1 | awk -F/ '{print $(NF-1)}')
fi
CLI_DIR="$PUBLISH_REPO_DIR/library/$category/$slug"
[ -d "$CLI_DIR" ] || { echo "ERROR: target CLI dir not found: $CLI_DIR"; exit 1; }
```

This is what U5 edits.

---

## Step 4 — Branch creation with collision detection

```bash
SHORT_SUMMARY=$(echo "$pr_title" | sed -E 's/^(feat|fix)\([^)]+\):\s*//' | tr '[:upper:] ' '[:lower:]-' | sed -E 's/[^a-z0-9-]//g; s/-+/-/g; s/^-//; s/-$//' | cut -c1-40)
BRANCH_NAME="amend/$slug-$SHORT_SUMMARY"

# Check for existing branch (open PR, own merged branch zombie, or fresh)
existing_open=$(gh pr list --repo mvanhorn/printing-press-library \
  --head "$gh_user:$BRANCH_NAME" --state open --limit 1 --json number,title)
existing_local=$(git branch --list "$BRANCH_NAME" | wc -l)

if [ "$(echo "$existing_open" | jq 'length')" -gt 0 ]; then
  # Open PR exists from this branch — surface to user and stop the shell flow.
  # The calling skill must then resolve the conflict via AskUserQuestion
  # (amend the existing PR by pushing to the same branch, or open new with
  # a timestamped branch) before re-entering this snippet with the chosen path.
  echo "ERROR: open PR already exists from $BRANCH_NAME:"
  echo "$existing_open" | jq -r '.[0] | "  PR #\(.number): \(.title)"'
  echo ""
  echo "Resolve before continuing:"
  echo "  1. Amend the existing PR — push to $BRANCH_NAME (skip this snippet's checkout)"
  echo "  2. Open a new PR — re-run with a timestamp suffix on the branch name"
  exit 1
fi

if [ "$existing_local" -gt 0 ]; then
  # Local zombie from prior run — timestamp to avoid clobber
  TIMESTAMP=$(date -u +%Y-%m-%dT%H%M)
  BRANCH_NAME="amend/$slug-$SHORT_SUMMARY-$TIMESTAMP"
fi

git checkout -b "$BRANCH_NAME"
```

When the skill driver sees a non-zero exit from this block, it must invoke `AskUserQuestion` to surface the two-option choice (amend the existing PR vs. open a new timestamped one), then re-enter the snippet with the chosen path. The hard `exit 1` exists so a literal shell-flow execution stops here rather than failing later with a confusing `git checkout -b` error when the local branch already exists.

Reference: publish SKILL.md Step 7 (lines 488-650) for the full collision matrix (open PR + own merged + zombie + branch-timestamping).

---

## Step 5 — Commit

```bash
# Stage every file changed in $CLI_DIR (the validate step ensured no off-target changes)
git add "$CLI_DIR"

# Conventional commit message
git commit -m "$(cat <<EOF
$pr_title

$pr_summary

Findings addressed:
$(echo "$findings_active" | jq -r '.[] | "- \(.id): \(.category) — \(.rationale)"')

amend run: $amend_run_id
EOF
)"
```

The PR title is composed in Phase 6's draft assembly (e.g. `fix(superhuman): surface refresh-token expiry; add drafts new + --type sent`).

---

## Step 6 — Issue ownership

Per `~/printing-press-library/AGENTS.md`, contributors search for an existing issue before opening a PR:

```bash
issue_match=$(gh issue list --repo mvanhorn/printing-press-library \
  --search "$slug $primary_keyword" --state open --limit 5 \
  --json number,title,labels)

if [ "$(echo "$issue_match" | jq 'length')" -gt 0 ]; then
  # Surface candidates; ask user to pick one or open new
  # ...
  ISSUE_NUM=<chosen number>
  PR_BODY_FOOTER="Closes #$ISSUE_NUM"
else
  # Open a new issue first
  ISSUE_NUM=$(gh issue create --repo mvanhorn/printing-press-library \
    --title "$pr_title" \
    --body "$(cat <<EOF
Captured during a /printing-press-amend run on $slug-pp-cli.

Findings:
$(echo "$findings_active" | jq -r '.[] | "- \(.id) (\(.classification)): \(.rationale)"')

PR with the proposed fix follows.
EOF
)" --label "comp:$slug" \
    | grep -oE '[0-9]+$')
  PR_BODY_FOOTER="Closes #$ISSUE_NUM"
fi

# Self-assign the issue (best-effort; permissions may block)
gh issue edit "$ISSUE_NUM" --repo mvanhorn/printing-press-library --add-assignee "$gh_user" 2>/dev/null || true
```

---

## Step 7 — Push and PR-create

```bash
# Push the branch
if [ "$access" = "push" ]; then
  git push origin "$BRANCH_NAME"
  PR_HEAD="$BRANCH_NAME"
else
  git push -u origin "$BRANCH_NAME"
  PR_HEAD="$gh_user:$BRANCH_NAME"
fi

# Capture HEAD_SHA AFTER push so evidence URLs are durable
HEAD_SHA=$(git rev-parse HEAD)

# Compose PR body file
PR_BODY_PATH=$(mktemp -t amend-pr-body)
cat > "$PR_BODY_PATH" <<EOF
## Summary

$pr_summary

## Findings

| ID | Category | Type | Rationale |
|---|---|---|---|
$(echo "$findings_active" | jq -r '.[] | "| \(.id) | \(.category) | \(.classification) | \(.rationale) |"')

## Changes

$(git diff --stat upstream/main..HEAD)

## Verification

- Build: $build_status
- Tests: $test_status
- Dogfood: ${dogfood_status:-N/A}
- \`printing-press publish validate\`: PASS (after $validate_iterations iteration(s))
- Patch contract: $patch_marker_count // PATCH() comments, .printing-press-patches.json updated

## Evidence

- Patch record: https://github.com/$gh_user/printing-press-library/blob/$HEAD_SHA/library/$category/$slug/.printing-press-patches.json
- Per-finding rationale: see the per-finding evidence captured in the patch record above and the Findings table earlier in this PR body
- Local plan doc (not in the PR — PII-scrubbed local artifact): \`\$PRESS_MANUSCRIPTS/$slug/<run-id>/proofs/<timestamp>-amend-$slug.md\` (path provided here for the original printer's reference; the artifact stays local by design)

$PR_BODY_FOOTER
EOF

# Open the PR
PR_URL=$(gh pr create \
  --repo mvanhorn/printing-press-library \
  --head "$PR_HEAD" \
  --base main \
  --title "$pr_title" \
  --body-file "$PR_BODY_PATH")

PR_NUMBER=$(echo "$PR_URL" | grep -oE '[0-9]+$')

# Apply labels
gh pr edit "$PR_NUMBER" --repo mvanhorn/printing-press-library \
  --add-label "comp:$slug" \
  --add-label "priority:P${scope_priority}" 2>/dev/null || true
```

Reference: publish SKILL.md Step 8 (lines 671-925).

---

## Step 8 — Greptile awareness (informational)

Every PR opened against `mvanhorn/printing-press-library` receives a Greptile auto-review. The skill does NOT auto-fix Greptile findings (deferred to v0.2). Tell the user in the final summary:

> "Greptile will review your PR within ~2 minutes. Check inline comments via:
>
>     gh api repos/mvanhorn/printing-press-library/pulls/$PR_NUMBER/comments
>
> ...or in the GitHub UI. P0/P1 findings are worth addressing before requesting human review."

---

## Cleanup

The managed clone stays in place for the next amend run on this scope (refresh-from-upstream on next bootstrap). Nothing to clean up.

If the user explicitly aborts mid-run, leave the clone in whatever state it's in — the next run's reset-hard will restore it.
