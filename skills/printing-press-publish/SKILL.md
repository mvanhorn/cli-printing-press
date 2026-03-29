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

## Setup

Before doing anything else:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 0.5.0
if ! command -v printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/printing-press" ]; then
    echo "printing-press found at ~/go/bin/printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

# Derive scope: prefer git repo root, fall back to CWD
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

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

After running the setup contract, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, warn the user: "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/cmd/printing-press@latest` to update." Continue anyway but surface the warning prominently.

## Configuration

```
PUBLISH_REPO_URL="https://github.com/mvanhorn/printing-press-library"
PUBLISH_REPO_DIR="$PRESS_HOME/.publish-repo"
PUBLISH_CONFIG="$PRESS_HOME/.publish-config.json"
```

## Step 1: Prerequisites

Verify `gh` is authenticated:

```bash
gh auth status
```

If this fails, stop and tell the user: "GitHub CLI is not authenticated. Run `gh auth login` first."

## Step 2: Resolve CLI Name

Run:

```bash
printing-press library list --json
```

Parse the JSON output into a list of CLIs.

**Name resolution order** (matches the score skill for consistency):

1. **Exact match:** If the argument matches a `cli_name` exactly, use it
2. **Suffix match:** If no exact match, try `<argument>-pp-cli`
3. **Glob match:** If no suffix match, search for entries where `cli_name` contains the argument as a substring. Cap at 5 most-recent matches. If multiple matches, present them via AskUserQuestion and let the user pick
4. **No match:** List all available CLIs and ask the user to pick or re-enter
5. **No argument:** If invoked with no name, list all CLIs sorted by modification time and let the user pick

When presenting matches, show the CLI name and modification time in a human-friendly format (e.g., "2 hours ago", "3 days ago").

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
Validating <cli-name>...
  manifest        PASS
  go mod tidy     PASS
  go vet          PASS
  go build        PASS
  --help          PASS
  --version       PASS
  manuscripts     WARN (no manuscripts found)
```

If `"passed": false`, report the failing checks and **stop**. Do not create a partial PR.

Save the `help_output` field from the result — it's used in the PR description.

## Step 5: Package

Create a temporary staging directory and run:

```bash
printing-press publish package \
  --dir <cli-dir> \
  --category <category> \
  --target <staging-dir> \
  --json
```

Parse the JSON result. Note the `staged_dir`, `manuscripts_included`, and `run_id`.

## Step 6: Managed Clone

The publish skill manages its own clone of the library repo at `$PUBLISH_REPO_DIR`.

### First-time setup

If `$PUBLISH_REPO_DIR` does not exist:

1. Detect push access:
   ```bash
   gh api repos/mvanhorn/printing-press-library --jq '.permissions.push'
   ```

2. Detect git protocol:
   ```bash
   ssh -T git@github.com 2>&1 | grep -q "successfully authenticated"
   ```
   If SSH works, use SSH URLs. Otherwise use HTTPS.

3. Clone based on access:
   - **Push access:** Clone directly
   - **No push access:** Fork first with `gh repo fork mvanhorn/printing-press-library --clone=false`, then clone the fork and add `upstream` pointing at `mvanhorn/printing-press-library`

4. Cache the config:
   ```json
   {
     "repo_url": "https://github.com/mvanhorn/printing-press-library",
     "access": "push",
     "protocol": "ssh",
     "clone_path": "~/printing-press/.publish-repo"
   }
   ```
   Write to `$PUBLISH_CONFIG`.

### Subsequent publishes

Read `$PUBLISH_CONFIG`, then freshen. Use `git reset --hard` instead of `git pull` because this is a managed clone, not a user working tree — it should always match the canonical upstream exactly:

```bash
cd "$PUBLISH_REPO_DIR"
git fetch origin
git fetch upstream 2>/dev/null || true
git checkout main
if git rev-parse --verify upstream/main >/dev/null 2>&1; then
  git reset --hard upstream/main
else
  git reset --hard origin/main
fi
```

Also verify the clone is healthy:

```bash
git rev-parse --is-inside-work-tree
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

## Step 7: Check for Existing PR

Before creating a branch, check whether an open PR already exists for this CLI:

```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/<cli-name>" --state open --json number,title,url
```

Parse the result:
- If the list is non-empty, store `EXISTING_PR_NUMBER` and `EXISTING_PR_URL` from the first entry
- If the list is empty or the command fails (network, auth), set `EXISTING_PR_NUMBER=""` — proceed as if no PR exists

If an existing open PR was found, inform the user:
> "Found open PR #N for `<cli-name>`. Will update it with the new version."

This determines the flow in Step 8:
- **Existing open PR:** Overwrite the branch automatically, force-push, update the PR description
- **No open PR:** Standard flow — ask about branch conflicts if any, create a new PR

## Step 8: Branch, Commit, and PR

### Create branch

**If `EXISTING_PR_NUMBER` is set** (updating an existing PR):

Always overwrite the branch — the intent is clearly to update:

```bash
git checkout -B feat/<cli-name>
```

**If `EXISTING_PR_NUMBER` is empty** (no open PR):

Check for an existing branch:

```bash
git branch --list "feat/<cli-name>"
git ls-remote --heads origin "feat/<cli-name>"
```

If a branch exists (from a previously closed or merged PR), ask via AskUserQuestion:
- "Overwrite existing branch"
- "Create timestamped variant (feat/<cli-name>-YYYYMMDD)"

Create the branch:

```bash
# New branch:
git checkout -b feat/<cli-name>

# Overwrite existing:
git checkout -B feat/<cli-name>
```

### Replace CLI package

Remove any existing version of this CLI before copying. This prevents stale files from a previous generation persisting (e.g., deleted commands, renamed files). The glob also handles category changes — if the CLI moved from `productivity` to `developer-tools`, the old directory is cleaned up.

```bash
rm -rf "$PUBLISH_REPO_DIR/library"/*/"<cli-name>"
cp -r <staging-dir>/library/* "$PUBLISH_REPO_DIR/library/"
```

### Update registry.json

Read `$PUBLISH_REPO_DIR/registry.json`, add or update the entry for this CLI:

```json
{
  "cli_name": "<cli-name>",
  "api_name": "<api-name>",
  "category": "<category>",
  "description": "<from manifest or empty>",
  "printing_press_version": "<from manifest>",
  "published_date": "<today YYYY-MM-DD>"
}
```

Write back with `jq` or via the Write tool.

### Commit and push

```bash
cd "$PUBLISH_REPO_DIR"
git add library/ registry.json
git commit -m "feat(<api-name>): add <cli-name>"
```

**If updating an existing PR** (`EXISTING_PR_NUMBER` is set):

```bash
git push --force-with-lease -u origin feat/<cli-name>
```

**If creating a new PR** and you chose "Overwrite existing branch" earlier:

```bash
git push --force-with-lease -u origin feat/<cli-name>
```

**Otherwise** (new branch, no conflicts):

```bash
git push -u origin feat/<cli-name>
```

### Create or update PR

Build the PR description from:
- The manifest (`description`, `api_name`, `category`, `printing_press_version`, `spec_url`)
- The `help_output` captured in Step 4
- The CLI's README (first 2-3 paragraphs, or note that README is missing)
- Links to `.manuscripts/<run-id>/research/` and `.manuscripts/<run-id>/proofs/` within the PR branch
- The validation results from Step 4
- A Gaps section listing any missing manifest fields

**PR description template:**

```markdown
## <cli-name>

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

- [Research Brief](<link to library/<category>/<cli-name>/.manuscripts/<run-id>/research/>)
- [Shipcheck Results](<link to library/<category>/<cli-name>/.manuscripts/<run-id>/proofs/>)

### Validation Results

| Check | Result |
|-------|--------|
| Manifest | PASS/FAIL |
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
gh pr edit "$EXISTING_PR_NUMBER" \
  --repo mvanhorn/printing-press-library \
  --body "<constructed PR body>"
```

Display: "Updated PR #N: <EXISTING_PR_URL>"

**If creating a new PR:**

```bash
gh pr create \
  --repo mvanhorn/printing-press-library \
  --title "feat(<api-name>): add <cli-name>" \
  --body "<constructed PR body>"
```

Display the PR URL prominently.

## Error Handling

- **`gh` not authenticated:** Detect in Step 1, tell user to run `gh auth login`
- **CLI not found:** Show available CLIs in Step 2, let user pick
- **Validation fails:** Show per-check results in Step 4, stop
- **Repo unreachable:** Report clearly in Step 6
- **Existing PR check fails:** Fall back to standard branch-conflict flow (treat as no existing PR)
- **Branch conflict (no existing PR):** Ask user in Step 8 (overwrite or timestamp)
- **Push fails:** Report the error, suggest checking `gh auth status`
- **Staging cleanup:** If any step after packaging (Steps 6-8) fails, remove the staging directory created in Step 5 before stopping. This prevents accumulation of full CLI copies in temp directories across retries
