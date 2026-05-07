---
name: printing-press-publish
description: Publish a generated CLI to the printing-press-library repo
version: 0.1.0
min-binary-version: "0.5.0"
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - AskUserQuestion
---

# /printing-press publish

Publish a generated CLI from your local library to the [printing-press-library](https://github.com/mvanhorn/printing-press-library) repo as a pull request.

```bash
/printing-press publish notion-pp-cli
/printing-press publish notion
/printing-press publish
```

The public library treats `library/<category>/<api-slug>/.printing-press.json`
and `manifest.json` as the source of truth for registry-display fields. Do not
edit `registry.json` or README catalog cells in publish PRs; the library's
post-merge workflow regenerates them. Do regenerate and commit the
`cli-skills/pp-<api-slug>/SKILL.md` mirror because PR CI verifies it against
`library/<category>/<api-slug>/SKILL.md`.

## Setup

Before doing anything else:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 0.5.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

# Prefer local build when running from inside the printing-press repo.
if [ -x "$_scope_dir/printing-press" ] && [ -d "$_scope_dir/cmd/printing-press" ]; then
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/printing-press"
elif ! command -v printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/printing-press" ]; then
    echo "printing-press found at ~/go/bin/printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="$HOME/printing-press"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_CURRENT="$PRESS_RUNSTATE/current"

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY" "$PRESS_MANUSCRIPTS" "$PRESS_CURRENT"
```
<!-- PRESS_SETUP_CONTRACT_END -->

After running the setup contract, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, warn the user: "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v3/cmd/printing-press@latest` to update." Continue anyway but surface the warning prominently.

## Configuration

```
PUBLISH_REPO_URL="https://github.com/mvanhorn/printing-press-library"
PUBLISH_REPO_DIR="$PRESS_HOME/.publish-repo-$PRESS_SCOPE"
PUBLISH_CONFIG="$PRESS_HOME/.publish-config-$PRESS_SCOPE.json"
```

### Publish config

`$PUBLISH_CONFIG` stores persistent publish settings as JSON. On first publish, create it with defaults. The user can edit it to change the library repo or module path base.

```json
{
  "managed_by": "printing-press-publish",
  "repo_url": "https://github.com/mvanhorn/printing-press-library",
  "access": "push",
  "protocol": "ssh",
  "clone_path": "<home>/printing-press/.publish-repo-<scope>",
  "scope_dir": "/absolute/path/to/source/worktree",
  "module_path_base": "github.com/mvanhorn/printing-press-library/library"
}
```

The `module_path_base` field sets the Go module path prefix for published CLIs. During packaging, the full module path is constructed as `<module_path_base>/<category>/<api-slug>`. If the user wants CLIs published to a different repo or path, they edit this field.
Store expanded absolute paths for `clone_path` and `scope_dir` so cleanup can
check them without relying on shell-specific `~` expansion. The `managed_by`
field is required before cleanup may delete anything.

### Scoped clone cleanup

Before creating or reusing `$PUBLISH_REPO_DIR`, prune scoped publish clones whose
source worktree no longer exists. This keeps concurrent worktrees isolated
without accumulating one library clone forever per short-lived worktree.

```bash
find "$PRESS_HOME" -maxdepth 1 -name '.publish-config-*.json' -type f | while read -r cfg; do
  [ "$cfg" = "$PUBLISH_CONFIG" ] && continue
  managed_by=$(jq -r '.managed_by // empty' "$cfg" 2>/dev/null || true)
  scope_dir=$(jq -r '.scope_dir // empty' "$cfg" 2>/dev/null || true)
  clone_path=$(jq -r '.clone_path // empty' "$cfg" 2>/dev/null || true)
  [ "$managed_by" = "printing-press-publish" ] || continue
  [ -z "$scope_dir" ] && continue
  [ -e "$scope_dir" ] && continue
  [ -d "$clone_path/.git" ] || continue
  case "$clone_path" in "$PRESS_HOME"/.publish-repo-*) ;; *) continue ;; esac
  origin=$(git -C "$clone_path" remote get-url origin 2>/dev/null || true)
  case "$origin" in *mvanhorn/printing-press-library*|*/*/printing-press-library*) ;; *) continue ;; esac
  [ -z "$(git -C "$clone_path" status --porcelain)" ] || continue
  [ "$(git -C "$clone_path" rev-parse --abbrev-ref HEAD 2>/dev/null || true)" = "main" ] || continue
  rm -rf "$clone_path" "$cfg"
done
```

## Step 1: Prerequisites

Verify `gh` is authenticated:

```bash
gh auth status
```

If this fails, stop and tell the user: "GitHub CLI is not authenticated. Run `gh auth login` first."

## Step 2: Resolve API Slug

Run:

```bash
printing-press library list --json
```

Parse the JSON output into a list of CLIs. The library is now keyed by API slug (the directory name), not CLI name.

**Name resolution order** (matches the score skill for consistency):

1. **Exact match:** If the argument matches a directory name (API slug) exactly, use it
2. **CLI name match:** If no exact match, try matching against `cli_name` fields, then derive the API slug from the manifest's `api_name` field
3. **Suffix match:** If no match yet, try `<argument>-pp-cli` against `cli_name` fields
4. **Glob match:** If no suffix match, search for entries where `cli_name` or `api_name` contains the argument as a substring. Cap at 5 most-recent matches. If multiple matches, present them via AskUserQuestion and let the user pick
5. **No match:** List all available CLIs and ask the user to pick or re-enter
6. **No argument:** If invoked with no name, list all CLIs sorted by modification time and let the user pick

Once resolved, read the manifest's `api_name` field to get the API slug. Use this slug for all downstream operations (branch names, registry entries, collision detection, path construction). The `cli_name` from the manifest is only used for binary-level operations.

When presenting matches, show the API slug and modification time in a human-friendly format (e.g., "2 hours ago", "3 days ago").

## Step 3: Determine Category

Read `.printing-press.json` from the resolved CLI directory.

**Category resolution order:**

1. If the manifest has a `category` field, present it for confirmation:
   > "Publishing as **<category>**. OK?"
   Give the user the option to change it

2. If no `category` but `catalog_entry` is present, look it up:
   ```bash
   printing-press catalog show <catalog_entry> --json
   ```
   Extract the category from the result. Present for confirmation

3. If neither provides a category, present the full list via AskUserQuestion:
   - developer-tools, monitoring, cloud, project-management
   - productivity, social-and-messaging, sales-and-crm, marketing
   - payments, auth, commerce, ai, media-and-entertainment, devices, other

## Step 4: Validate

Run:

```bash
printing-press publish validate --dir <cli-dir> --json
```

Parse the JSON result. Display each check result to the user:

```
Validating <api-slug>...
  manifest        PASS
  phase5          PASS
  go mod tidy     PASS
  go vet          PASS
  go build        PASS
  --help          PASS
  --version       PASS
  manuscripts     WARN (no manuscripts found)
```

If `"passed": false`, report the failing checks and **stop**. Do not create a partial PR.

Save the `help_output` field from the result — it's used in the PR description.

## Step 5: Managed Clone

The publish skill manages its own clone of the library repo at `$PUBLISH_REPO_DIR`.

### First-time setup

If `$PUBLISH_REPO_DIR` does not exist:

1. **Detect push access:**
   ```bash
   GH_USER=$(gh api user --jq '.login')
   HAS_PUSH=$(gh api repos/mvanhorn/printing-press-library --jq '.permissions.push' 2>/dev/null || echo "false")
   ```

2. **Detect git protocol:**
   ```bash
   USE_SSH=false
   if ssh -T git@github.com 2>&1 | grep -q "successfully authenticated"; then
     USE_SSH=true
   fi
   ```

3. **Clone based on access:**

   **Push access** (`HAS_PUSH` is `true`):
   ```bash
   # Clone directly — origin IS the upstream
   if [ "$USE_SSH" = "true" ]; then
     REPO_URL="git@github.com:mvanhorn/printing-press-library.git"
   else
     REPO_URL="https://github.com/mvanhorn/printing-press-library.git"
   fi
   git clone --depth 50 "$REPO_URL" "$PUBLISH_REPO_DIR"
   ```

   **No push access** (`HAS_PUSH` is `false`):
   ```bash
   # Fork first — fail explicitly if forking is blocked
   if ! gh repo fork mvanhorn/printing-press-library --clone=false 2>&1; then
     echo "ERROR: Could not fork mvanhorn/printing-press-library."
     echo "The repo may restrict forking, or you may already have a fork with a different name."
     echo "Fork manually at https://github.com/mvanhorn/printing-press-library/fork"
     exit 1
   fi
   FORK="$GH_USER/printing-press-library"

   # Build URLs based on protocol preference
   if [ "$USE_SSH" = "true" ]; then
     FORK_URL="git@github.com:$FORK.git"
     UPSTREAM_URL="git@github.com:mvanhorn/printing-press-library.git"
   else
     FORK_URL="https://github.com/$FORK.git"
     UPSTREAM_URL="https://github.com/mvanhorn/printing-press-library.git"
   fi

   git clone --depth 50 "$FORK_URL" "$PUBLISH_REPO_DIR"
   cd "$PUBLISH_REPO_DIR"
   git remote add upstream "$UPSTREAM_URL"
   git fetch upstream
   ```

4. **Cache the config:**
   ```json
   {
     "managed_by": "printing-press-publish",
     "repo_url": "https://github.com/mvanhorn/printing-press-library",
     "access": "push or fork",
     "gh_user": "<gh username>",
     "protocol": "ssh or https",
     "clone_path": "<expanded $PUBLISH_REPO_DIR>",
     "scope_dir": "<absolute source worktree path>",
     "module_path_base": "github.com/mvanhorn/printing-press-library/library"
   }
   ```
   Write to `$PUBLISH_CONFIG`. The `access` field determines the flow for all subsequent steps. The `gh_user` field is used for cross-repo PR heads. The `module_path_base` always references the upstream repo (PRs land there).

### Subsequent publishes

Read `$PUBLISH_CONFIG`, then re-check access in case it changed (user was granted push access, or access was revoked):

```bash
CURRENT_ACCESS=$(gh api repos/mvanhorn/printing-press-library --jq '.permissions.push' 2>/dev/null || echo "false")
CACHED_ACCESS=$(jq -r .access "$PUBLISH_CONFIG")

if [ "$CURRENT_ACCESS" = "true" ] && [ "$CACHED_ACCESS" = "fork" ]; then
  echo "Access upgraded to push. Reconfiguring clone..."
  rm -rf "$PUBLISH_REPO_DIR"
  # Re-run first-time setup with push access
fi
if [ "$CURRENT_ACCESS" = "false" ] && [ "$CACHED_ACCESS" = "push" ]; then
  echo "Push access revoked. Reconfiguring clone with fork..."
  rm -rf "$PUBLISH_REPO_DIR"
  # Re-run first-time setup with fork access
fi
```

If the clone was removed due to an access change, re-run first-time setup above. Otherwise, freshen the clone to match the canonical upstream:

```bash
cd "$PUBLISH_REPO_DIR"

if [ "$(jq -r .access $PUBLISH_CONFIG)" = "push" ]; then
  # Push access: origin IS the upstream
  git fetch origin
  git checkout main
  git reset --hard origin/main
else
  # Fork: origin is the fork, upstream is canonical
  git fetch upstream
  git checkout main
  git reset --hard upstream/main
  # Also sync origin (fork) so git push works cleanly
  git push origin main --force-with-lease 2>/dev/null || true
fi
```

Verify the clone is healthy:

```bash
git rev-parse --is-inside-work-tree
test "$(git rev-parse --abbrev-ref HEAD)" = "main"
```

If this fails, the clone is corrupt. Remove `$PUBLISH_REPO_DIR` and re-run first-time setup.

### Interrupted state recovery

Before creating a new branch, check for uncommitted changes:

```bash
cd "$PUBLISH_REPO_DIR"
git status --porcelain
```

If there are uncommitted changes, ask the user via AskUserQuestion:
- "Reset and start fresh"
- "Continue with existing changes"

If reset, run `git checkout -- . && git clean -fd`.

## Step 6: Package

Read `$PUBLISH_CONFIG` to get `module_path_base`. Construct the full module path using the API slug (not the CLI name):

```
MODULE_PATH="<module_path_base>/<category>/<api-slug>"
```

For example: `github.com/mvanhorn/printing-press-library/library/productivity/notion`

Run `publish package` with `--target` to stage the CLI into a unique temporary
directory, then copy it into the publish repo:

```bash
PUBLISH_STAGING_ROOT="/tmp/printing-press/publish"
mkdir -p "$PUBLISH_STAGING_ROOT"
STAGING_PARENT="$(mktemp -d "$PUBLISH_STAGING_ROOT/<api-slug>-XXXXXX")"
STAGING_DIR="$STAGING_PARENT/package"

printing-press publish package \
  --dir <cli-dir> \
  --category <category> \
  --target "$STAGING_DIR" \
  --module-path "$MODULE_PATH" \
  --json
```

Parse the JSON result. Note the `staged_dir`, `module_path`, `manuscripts_included`, and `run_id`. The `module_path` field confirms the Go module path that was set in the packaged CLI's `go.mod` and import paths.

Then copy the staged CLI into the publish repo, replacing any existing version:

```bash
# Remove existing version (handles category changes)
rm -rf "$PUBLISH_REPO_DIR/library"/*/"<api-slug>"

# Copy staged CLI into publish repo (slug-keyed directory)
cp -r "$STAGING_DIR/library/<category>/<cli-name>" "$PUBLISH_REPO_DIR/library/<category>/<api-slug>"

# Remove binaries (should not be committed)
rm -f "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/<api-slug>" "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/<cli-name>"

# Regenerate the flat cli-skills mirror so library PR CI passes mirror parity.
if [ -f "$PUBLISH_REPO_DIR/tools/generate-skills/main.go" ]; then
  (cd "$PUBLISH_REPO_DIR" && go run ./tools/generate-skills/main.go)
fi

# Verify it builds from the publish repo
cd "$PUBLISH_REPO_DIR/library/<category>/<api-slug>" && go build ./...
```

After the publish repo copy and build verification are complete, remove the staging
directory:

```bash
rm -rf "$STAGING_PARENT"
```

Note: `staged_dir` uses the CLI name (e.g., `espn-pp-cli`) but the publish repo uses the API slug (e.g., `espn`). The copy step handles this rename.

## Step 7: Collision Detection & Resolution

After the managed clone is freshened, check for name collisions before creating a branch or PR. This replaces the previous "Check for Existing PR" step.

### Detection

Run these checks in sequence:

**1. Check merged CLIs in managed clone:**

```bash
ls "$PUBLISH_REPO_DIR/library"/*/"<api-slug>" 2>/dev/null
```

If found, record `MERGED_COLLISION=true` and note the category path.

**2. Check all open PRs (any author):**

```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/<api-slug>" --state open --json number,title,url,author
```

If the list is non-empty, record `PR_COLLISION=true`. For each PR, note the PR number, URL, and author login.

**3. Identify own PRs:**

Filter the PR list from step 2 by `--author @me`:

For fork-based PRs, the head includes the username prefix:

```bash
ACCESS=$(jq -r .access "$PUBLISH_CONFIG")
GH_USER=$(jq -r .gh_user "$PUBLISH_CONFIG")

if [ "$ACCESS" = "fork" ]; then
  HEAD_REF="$GH_USER:feat/<api-slug>"
else
  HEAD_REF="feat/<api-slug>"
fi

gh pr list --repo mvanhorn/printing-press-library --head "$HEAD_REF" --state open --author @me --json number,title,url
```

If found, record `OWN_PR=true`, store `EXISTING_PR_NUMBER` and `EXISTING_PR_URL`.

**If no open PR was found**, also check for a previously merged PR on the same branch — by ANY author, not just yours:

```bash
MERGED_PR=$(gh pr list --repo mvanhorn/printing-press-library --head "$HEAD_REF" --state merged --json number --jq '.[0].number' 2>/dev/null)
```

If `MERGED_PR` is non-empty, the branch name was already used and merged. Set `BRANCH_MERGED=true` so Step 8 creates a new branch name (e.g., `feat/<api-slug>-YYYYMMDD`) instead of reusing the merged branch. Do NOT force-push onto a merged branch — `gh pr edit` would silently update a closed PR nobody is watching.

The author-agnostic lookup also catches **squash-zombie branches**: GitHub squash-merge leaves the source branch behind on the remote, with pre-squash commit refs that look "ahead of main" but are content-equivalent to the squash commit. Without this check, the skill misclassifies the zombie as fresh-publish, then `git push -u` fails because the remote branch already exists. Timestamping sidesteps the issue entirely.

### No collision

If no merged CLI exists and no open PRs match (other than your own), set `EXISTING_PR_NUMBER` from the own-PR check (or empty if none) and proceed to Step 8 normally.

If an existing open PR of yours was found, inform the user:
> "Found your open PR #N for `<api-slug>`. Will update it with the new version."

### Collision detected — display info

Show the user what was found:

```
⚠️  Name collision detected for <api-slug>

  Merged: <category>/<api-slug> exists in the library
  Open PR: #<number> by <author> — <url>
```

Show all applicable lines. If `OWN_PR=true`, tag the PR as "(yours)".

### Resolution paths

Present three options via AskUserQuestion:

**If `OWN_PR=true` (your own open PR exists):**
- **Update** — Update your existing PR with the new version (default, preserves current behavior)
- **Alongside** — Rename yours with a qualifier and publish next to the existing one
- **Bail** — Cancel the publish

**If PR collision exists but is another user's, or merged collision only:**
- **Replace** — Intentionally overwrite the existing CLI
- **Alongside** — Rename yours with a qualifier and publish next to the existing one
- **Bail** — Cancel the publish and view the existing CLI/PR

#### Update path (own PR)

This is the existing update flow. Set `EXISTING_PR_NUMBER` from the detection step and proceed to Step 8, which handles force-push and PR description update.

#### Replace path

**For merged CLIs or your own PR:** Standard confirmation:
> "This will replace the existing `<api-slug>`. Continue?"

**For another user's PR:** Stronger confirmation naming the other author:
> "⚠️  This will replace `<author>`'s `<api-slug>` (PR #N). Are you sure?"

If confirmed:
- The PR description must include: `⚠️ **Replaces existing \`<api-slug>\`** — <reason provided by user or "newer version">`
- Set `EXISTING_PR_NUMBER=""` (create a new PR, don't update theirs)
- Proceed to Step 8 normally

#### Alongside path (rename)

**1. Extract the original API slug** from the manifest's `api_name` field:

```bash
# Read from .printing-press.json in the publish repo's staged CLI
ORIGINAL_API_SLUG=$(cat "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/.printing-press.json" | jq -r '.api_name')
```

**2. Generate rename suggestions** using slug format. Derive the new CLI name from the chosen slug:

- Numeric: `<api-slug>-2` (if that collides, try `-3`, `-4`, etc.)
- Non-numeric: `<api-slug>-alt`
- Custom: prompt the user for a qualifier word

After the user chooses a slug, compute:

```bash
NEW_API_SLUG="<chosen-slug>"
NEW_CLI_NAME="${NEW_API_SLUG}-pp-cli"
```

Present the format to the user:
> "Rename format: `<api-slug>-<qualifier>`. Pick a qualifier:"
>
> 1. `2` → `<api-slug>-2`
> 2. `alt` → `<api-slug>-alt`
> 3. Enter custom qualifier

**3. Verify each suggestion is non-colliding** before presenting:

```bash
# Check merged
ls "$PUBLISH_REPO_DIR/library"/*/"<suggestion>" 2>/dev/null
# Check open PRs
gh pr list --repo mvanhorn/printing-press-library --head "feat/<suggestion>" --state open --json number
```

If a suggestion collides, skip it or increment the numeric suffix.

**4. Rename the CLI in the publish repo:**

Since Step 6 copied the staged CLI into `$PUBLISH_REPO_DIR`, the rename operates on that directory. Note: `--old-name`/`--new-name` still use CLI-name format (e.g., `dub-pp-cli`) because `RenameCLI` does content replacement — bare slugs would cause collateral damage. The `--dir` path uses the slug-keyed directory.

```bash
printing-press publish rename \
  --dir "$PUBLISH_REPO_DIR/library/<category>/<api-slug>" \
  --old-name <old-cli-name> \
  --new-name "$NEW_CLI_NAME" \
  --api-name "$ORIGINAL_API_SLUG" \
  --json
```

Parse the JSON result. Verify `"success": true`. Note that `new_dir` should now be `$PUBLISH_REPO_DIR/library/<category>/$NEW_API_SLUG`.

**5. Update all downstream references for Step 8:**

- Branch name: `feat/$NEW_API_SLUG` (not the old slug)
- PR title: `feat($NEW_API_SLUG): add $NEW_API_SLUG`
- Commit message: `feat($NEW_API_SLUG): add $NEW_API_SLUG`
- Registry.json entry: `name` → `$NEW_API_SLUG`
- Set `EXISTING_PR_NUMBER=""` (always a new PR for a renamed CLI)

Proceed to Step 8 with the new name.

#### Bail path

Show links to what exists:
- If merged: "Existing CLI at `library/<category>/<api-slug>/`"
- If open PR: "Open PR: <url>"

Exit the publish flow. If Step 6 already wrote files into `$PUBLISH_REPO_DIR`, clean up with `git checkout -- . && git clean -fd` in the managed clone.

## Step 8: Branch, Commit, and PR

### Create branch

**If `EXISTING_PR_NUMBER` is set** (updating an existing PR):

Always overwrite the branch — the intent is clearly to update:

```bash
git checkout -B feat/<api-slug>
```

**If `EXISTING_PR_NUMBER` is empty and `BRANCH_MERGED` is true** (previous PR was merged):

Auto-create a timestamped branch — do not reuse the merged branch name:

```bash
git checkout -b feat/<api-slug>-$(date +%Y%m%d)
```

**If `EXISTING_PR_NUMBER` is empty and `BRANCH_MERGED` is not set** (no open or merged PR):

Check for stale branches and competing PRs:

```bash
# Check local and remote branches
LOCAL_BRANCH=$(git branch --list "feat/<api-slug>" | head -1)
REMOTE_BRANCH=$(git ls-remote --heads origin "feat/<api-slug>" 2>/dev/null | head -1)

# If a remote branch exists, check who owns it
if [ -n "$REMOTE_BRANCH" ]; then
  # Check for ANY open PR on this branch (not just ours)
  OTHER_PR=$(gh pr list --repo mvanhorn/printing-press-library --head "feat/<api-slug>" --state open --json number,author --jq '.[0]' 2>/dev/null)
fi
```

**If another user's open PR exists on this branch** (`OTHER_PR` is non-empty and author is not `@me`):
> "Someone else has an open PR for `<api-slug>` (PR #N by @author). Creating a timestamped branch to avoid conflicts."

Auto-create a timestamped branch: `feat/<api-slug>-YYYYMMDD`. Do NOT offer to overwrite — that would stomp their work.

**If the branch exists but no competing PR** (stale branch from a previously closed/merged PR):

Ask via AskUserQuestion:
> "Found a stale branch `feat/<api-slug>` (likely from a previous publish). Overwrite it?"

- "Overwrite existing branch" — reuse the branch name
- "Create timestamped variant (feat/<api-slug>-YYYYMMDD)"

**If no branch exists:** Create normally.

```bash
# New branch:
git checkout -b feat/<api-slug>

# Overwrite existing:
git checkout -B feat/<api-slug>
```

### Commit and push

```bash
cd "$PUBLISH_REPO_DIR"
git add library/ cli-skills/
git commit -m "feat(<api-slug>): add <api-slug>"
```

Push to origin (which is the fork for non-push users, or the upstream for push users):

**If updating an existing PR** (`EXISTING_PR_NUMBER` is set):

```bash
git push --force-with-lease -u origin feat/<api-slug>
```

**If creating a new PR** and you chose "Overwrite existing branch" earlier:

```bash
git push --force-with-lease -u origin feat/<api-slug>
```

**Otherwise** (new branch, no conflicts):

```bash
git push -u origin feat/<api-slug>
```

### Create or update PR

Read `access` and `gh_user` from `$PUBLISH_CONFIG`. These determine how `gh pr create` is called.

**For fork-based PRs** (`access` is `fork`): use `--head <gh_user>:feat/<api-slug>` so GitHub creates a cross-repo PR from the fork to the upstream. Without `--head`, `gh pr create` would try to find the branch on the upstream repo (where the user can't push) and fail.

**For push-access PRs** (`access` is `push`): use `--head feat/<api-slug>` so GitHub creates the PR from the branch this flow just pushed, even when the managed clone or shell session has other branches checked out.

Build the PR description from:
- The manifest (`description`, `api_name`, `category`, `printing_press_version`, `spec_url`)
- The `help_output` captured in Step 4
- The CLI's README (first 2-3 paragraphs, or note that README is missing)
- Links to `.manuscripts/<run-id>/research/` and `.manuscripts/<run-id>/proofs/` within the PR branch
- The validation results from Step 4
- A Gaps section listing any missing manifest fields

**MANDATORY: Before constructing the PR body, scrub all workspace PII.** The library
repo is public. Scan any live test results, acceptance data, or manuscript excerpts
for organization names, team member names, and email addresses. Replace with generic
descriptions ("the workspace", "5 team members", "12 users"). Team keys (e.g., "ESP")
are OK but org names (e.g., "Acme Corp") are not. See `references/secret-protection.md`
in the printing-press skill for the full policy.

**PR description template:**

```markdown
## <api-slug>

<If this is a Replace path, add: "⚠️ **Replaces existing `<api-slug>`** — <reason from user>">

<description from manifest, or "No description available">

**API:** <api_name> | **Category:** <category> | **Press version:** <printing_press_version>
**Spec:** <spec_url or "Not specified">

### CLI Shape

\`\`\`bash
$ <cli-name> --help
<help_output from validation>
\`\`\`

### What This CLI Does

<First 2-3 paragraphs from README.md in the CLI directory, or "README not found">

### Manuscripts

- [Research Brief](<link to library/<category>/<api-slug>/.manuscripts/<run-id>/research/>)
- [Shipcheck Results](<link to library/<category>/<api-slug>/.manuscripts/<run-id>/proofs/>)

### Validation Results

| Check | Result |
|-------|--------|
| Manifest | PASS/FAIL |
| Phase 5 | PASS/FAIL |
| go mod tidy | PASS/FAIL |
| go vet | PASS/FAIL |
| go build | PASS/FAIL |
| --help | PASS/FAIL |
| --version | PASS/FAIL |
| Manuscripts | PRESENT/MISSING |

### Gaps

<List any missing manifest fields, or omit this section if everything is present>
```

**If updating an existing PR** (`EXISTING_PR_NUMBER` is set):

```bash
cd "$PUBLISH_REPO_DIR"
gh pr edit "$EXISTING_PR_NUMBER" \
  --repo mvanhorn/printing-press-library \
  --body "<constructed PR body>"
```

Display the full PR URL: "Updated PR: <EXISTING_PR_URL>" (use the full `https://` URL, not shorthand).

**If creating a new PR:**

```bash
cd "$PUBLISH_REPO_DIR"

# Read access mode from config
ACCESS=$(jq -r .access "$PUBLISH_CONFIG")
GH_USER=$(jq -r .gh_user "$PUBLISH_CONFIG")

if [ "$ACCESS" = "fork" ]; then
  PR_HEAD_REF="$GH_USER:feat/<api-slug>"
else
  PR_HEAD_REF="feat/<api-slug>"
fi

gh pr create \
  --repo mvanhorn/printing-press-library \
  --head "$PR_HEAD_REF" \
  --base main \
  --title "feat(<api-slug>): add <api-slug>" \
  --body "<constructed PR body>"
```

Display the full PR URL (e.g., `https://github.com/mvanhorn/printing-press-library/pull/10`), not the shorthand `org/repo#N` format. The full URL is clickable in all terminals and contexts.

## Secret & PII Protection

Before creating the PR, verify that no secrets leaked into the packaged CLI.

**This matters because the library repo is public.** A leaked API key in a PR is
a security incident — anyone can see it, even if the PR is later closed.

### What the Printing Press checks (deterministic)

The generation skill (`/printing-press`) runs an exact-value scan during Phase 5.5
if the user provided an API key. By the time publish runs, the Printing Press's own
mistakes should already be caught. But the user may have edited files between
generation and publish.

### What publish checks (best-effort, warn-only)

1. **If `gitleaks` or `trufflehog` is installed**, run it on the staged directory:
   ```bash
   if command -v gitleaks >/dev/null 2>&1; then
     gitleaks detect --source "<staging-dir>/library" --no-git --verbose 2>&1
   elif command -v trufflehog >/dev/null 2>&1; then
     trufflehog filesystem "<staging-dir>/library" 2>&1
   fi
   ```
   These tools use vendor-specific patterns (Steam keys, Stripe keys, GitHub
   tokens) with low false-positive rates. Their findings are warnings — the
   user reviews and decides.

2. **If no scanning tool is installed**, do a lightweight check:
   - Verify no `.env` files, `session-state.json`, or `config.toml` with
     real credentials exist in the staged directory
   - Check README examples use `"your-key-here"` placeholders, not real values
   - Check manuscripts (if included) don't contain auth headers or cookie values

3. **Never include** in the staged directory:
   - `.env` files
   - `session-state.json`
   - Config files with real credentials
   - HAR captures with un-stripped auth headers

If any issues are found, warn the user and ask whether to proceed. The user
makes the final call — they may have intentionally included something the scan
flagged (e.g., a test fixture with a fake key). Don't block silently.

### PII pattern scanning (mandatory)

Beyond the secret scans above, run the **PII pattern scanning** step from
[../printing-press/references/secret-protection.md](../printing-press/references/secret-protection.md#pii-pattern-scanning)
(section "PII pattern scanning"). This catches PII captured during live dogfood
that the prose guidance missed — emails, real attendee names, account
identifiers — before they ship to the public library repo.

The scan has two tiers:
- **Tier 1 (auto-redact silently):** vendor-prefix-anchored bearer tokens
  (`Bearer cal_live_*`, `Bearer sk_live_*`, `Bearer ghp_*`, `xoxp-*`, etc.).
  Near-zero false-positive rate.
- **Tier 2 (warn, batched user prompt):** generic emails, generic bearer tokens,
  capitalized first+last name patterns. Allowlist suppresses spec-derived API
  vocabulary ("Event Types", "Booking Links") automatically.

A pre-scrub copy of the staging directory is preserved at
`<staging>.pre-pii-scrub/` so the user can recover from a wrong redaction.

Two prior PII leaks shipped to the public library before this scan existed.
The scan is the mechanical defense layer the prose guidance alone could not
provide.

## Error Handling

- **`gh` not authenticated:** Detect in Step 1, tell user to run `gh auth login`
- **CLI not found:** Show available CLIs in Step 2, let user pick
- **Validation fails:** Show per-check results in Step 4, stop
- **Repo unreachable:** Report clearly in Step 5
- **Fork creation fails:** `gh repo fork` may fail if the user already has a fork with a different name, or if the org restricts forking. Report the error and suggest the user fork manually via the GitHub web UI.
- **Collision check fails:** If `gh pr list` or `ls` commands fail (network, auth), warn but don't block — proceed as if no collision exists
- **Rename fails:** Show the error from `publish rename --json`. Offer to retry with a different qualifier or bail. If the publish repo is in a partial state, reset with `git checkout -- . && git clean -fd` before retrying
- **Branch conflict (no existing PR):** Ask user in Step 8 (overwrite or timestamp)
- **Push fails:** For fork users, ensure they're pushing to their fork (origin), not upstream. Report the error, suggest checking `gh auth status` and `git remote -v`
- **Cross-repo PR creation fails:** If `gh pr create --head user:branch` fails with "head not found", the branch wasn't pushed to the fork. Verify with `git ls-remote origin feat/<api-slug>`
