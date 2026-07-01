---
name: printing-press-publish
description: Publish a generated CLI to the printing-press-library repo
version: 0.1.0
min-binary-version: "4.0.0"
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
/printing-press publish notion --from-polish
/printing-press publish notion --skip-live-test=auth-unavailable
/printing-press publish notion --blocked-api-journal notion
/printing-press publish
```

## PR shape guard

This skill opens only a generated CLI publish PR or, with
`--blocked-api-journal`, a `blocked-apis.json` journal PR. It never opens a
docs-only, plan, proposal, or spec PR as a substitute for a CLI that is not
ready to publish. If generation, validation, or live testing is blocked, report
the exact blocker and stop.

## Direct User Invocation Required

Publishing can fork `mvanhorn/printing-press-library`, push a branch, and open or
update a PR. Before setup or validation, check the invocation context. If this
skill was invoked as a chained continuation from `printing-press-polish`'s
Publish Offer, including an `AskUserQuestion` answer or auto-resolved polish
recommendation, stop immediately and tell the user to send
`/printing-press-publish <cli-name> --from-polish` in a fresh message. A fresh
user-authored request that explicitly asks to publish is sufficient; do not add
another confirmation prompt on top of a direct publish request.

If the fresh user-authored request includes `--from-polish`, record
`POLISH_HANDOFF=true` for the terminal-state step and ignore that marker when
resolving the CLI name. The marker is not a second confirmation and is not
passed to `cli-printing-press`; it only preserves standalone polish's old
post-publish retro offer after the fresh-turn publish completes.

If the request includes `--blocked-api-journal`, enter **Blocked API Journal
Mode** below instead of the normal printed-CLI publish flow. This mode may be
invoked from `/printing-press`'s hold-path menu after the user explicitly chose
"Add to blocked-API journal"; that parent menu choice is sufficient user
authorization for the public-library journal write. Do not require a second
fresh-turn invocation for this journal-only mode.

If the fresh user-authored request includes `--skip-live-test=<reason>`, record
the exact non-empty reason as `SKIP_LIVE_TEST_REASON` and remove the flag before
resolving the CLI name. This is the only supported escape valve for the
publish-time live test gate. Use it only for auth-unavailable, known upstream
outage, LAN-unreachable hardware APIs, or similarly concrete operator-approved
cases; never infer a skip from ordinary latency or from the presence of an
older Phase 5 marker.

The public library treats `library/<category>/<api-slug>/.printing-press.json`
and `manifest.json` as the source of truth for registry-display fields. Do not
edit `registry.json`, README catalog cells, or `cli-skills/pp-<api-slug>/SKILL.md`
in publish PRs; all three are bot-regenerated post-merge by the library's own
workflows. The library's `Fail on changes to generated artifacts` check in
`verify-library-conventions.yml` hard-fails any PR — fork or same-repo — whose
diff against base touches `registry.json` or `cli-skills/pp-*/SKILL.md`, so a
publish that includes either is pre-rejected before review.

The public library also owns per-CLI release accounting. Do not manually bump
`CHANGELOG.md`, `.printing-press-release.json`, or runtime `var version = ...`
for a publish PR. Fresh printed CLIs may include blank release-ledger skeletons;
the library's post-merge workflow assigns the final `YYYY.M.N` release and
stamps the runtime version after merge. When replacing an existing public
library CLI, preserve its existing release-ledger files so changelog history is
not lost in the reprint PR.

`blocked-apis.json` is different: it is a hand-maintained public-library journal,
not a generated registry surface. Journal-only PRs may edit `blocked-apis.json`
and must not stage `library/`, `registry.json`, README catalog cells, or
`cli-skills/`.

## Blocked API Journal Mode

Use this mode only when the invocation includes `--blocked-api-journal`. It
records a held `/printing-press` attempt whose blocker is likely to repeat for
other users until a machine or upstream issue changes.

Required fields from the caller:

- `slug`: canonical API slug, not the CLI binary name.
- `attempted_at`: `YYYY-MM-DD`.
- `verdict`: `hold`.
- `reason`: concise blocker reason, with no secrets, local paths, cookies,
  tokens, or account-specific details.
- `blocking_issue`: Printing Press issue number if known, otherwise `null`.
- `permanent`: boolean.

If the caller did not provide one of these fields, infer only safe values from
the current run context. If `reason` is missing or vague, stop and ask for one
specific blocker sentence; do not write an unhelpful journal entry.

Run the normal Setup, Configuration, scoped clone cleanup, and GitHub auth
checks, then prepare the public-library clone exactly as the normal publish
flow does: fork if needed, ensure `upstream` points to
`mvanhorn/printing-press-library`, fetch `upstream`, and reset the clone to
`upstream/main` before editing.

Then update only `$PUBLISH_REPO_DIR/blocked-apis.json`:

```bash
cd "$PUBLISH_REPO_DIR"
if [ ! -f blocked-apis.json ]; then
  printf '[]\n' > blocked-apis.json
fi
jq --arg slug "<api-slug>" \
   --arg attempted_at "<YYYY-MM-DD>" \
   --arg verdict "hold" \
   --arg reason "<reason>" \
   --argjson blocking_issue '<number-or-null>' \
   --argjson permanent '<true-or-false>' '
  (if type == "array" then . else [] end)
  | map(select(.slug != $slug))
  + [{
      slug: $slug,
      attempted_at: $attempted_at,
      verdict: $verdict,
      reason: $reason,
      blocking_issue: $blocking_issue,
      permanent: $permanent
    }]
  | sort_by(.slug)
' blocked-apis.json > blocked-apis.json.tmp || {
  rm -f blocked-apis.json.tmp
  echo "Error: jq failed to update blocked-apis.json"
  exit 1
}
if ! jq empty blocked-apis.json.tmp; then
  rm -f blocked-apis.json.tmp
  echo "Error: blocked-apis.json update produced invalid JSON"
  exit 1
fi
mv blocked-apis.json.tmp blocked-apis.json
```

Create a journal branch and PR:

```bash
git checkout -B chore/blocked-api-<api-slug>
git add blocked-apis.json
git commit -m "chore(<api-slug>): journal blocked API"
git push --force-with-lease -u origin chore/blocked-api-<api-slug>
```

Open the PR against `mvanhorn/printing-press-library` with a body that includes:

- the held API slug and reason
- whether the block is permanent or tied to `blocking_issue`
- the expected Phase 0 behavior: future `/printing-press <api-slug>` runs warn
  before repeating the attempt

After the PR is open, report the URL and stop. Do not continue into normal
printed-CLI package, live-test, registry, or skill-mirror steps.

## Setup

Before doing anything else:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 4.0.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

# Prefer local build when running from inside the printing-press repo.
_press_repo=false
if [ -x "$_scope_dir/cli-printing-press" ] && [ -d "$_scope_dir/cmd/cli-printing-press" ]; then
  _press_repo=true
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/cli-printing-press"
elif ! command -v cli-printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/cli-printing-press" ]; then
    echo "cli-printing-press found at ~/go/bin/cli-printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "cli-printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

# Resolve and emit the absolute path the agent must use for every later
# `cli-printing-press` invocation. `export PATH` above only affects this one
# Bash tool call; subsequent calls open a fresh shell and resolve bare
# `cli-printing-press` against the user's default PATH, where a stale global
# can silently shadow the local build. The agent captures this marker and
# substitutes the absolute path into every later invocation.
if [ "$_press_repo" = "true" ]; then
  PRINTING_PRESS_BIN="$_scope_dir/cli-printing-press"
else
  PRINTING_PRESS_BIN="$(command -v cli-printing-press 2>/dev/null || true)"
fi
if ! command -v go >/dev/null 2>&1; then
  echo ""
  echo "[setup-error] Go toolchain not found."
  echo ""
  echo "This Printing Press flow runs Go-based build or validation commands."
  echo "Install Go 1.26.4 or newer from https://go.dev/dl/, then verify with:"
  echo "  go version"
  echo "Then re-run this skill."
  echo ""
  return 1 2>/dev/null || exit 1
fi
echo "PRINTING_PRESS_BIN=$PRINTING_PRESS_BIN"

_pp_semver_lt() {
  awk -v a="$1" -v b="$2" 'BEGIN {
    split(a, x, "."); split(b, y, ".")
    for (i = 1; i <= 3; i++) {
      if ((x[i] + 0) < (y[i] + 0)) exit 0
      if ((x[i] + 0) > (y[i] + 0)) exit 1
    }
    exit 1
  }'
}

_pp_go_version_norm() {
  printf '%s\n' "$1" | sed -nE 's/.*go([0-9]+)\.([0-9]+)(\.([0-9]+))?.*/\1.\2.\4/p' | awk -F. 'NF >= 2 { printf "%d.%d.%d\n", $1, $2, ($3 == "" ? 0 : $3) }'
}

_pp_check_go_currency() {
  _pp_go_installed="$(_pp_go_version_norm "$(go env GOVERSION 2>/dev/null)")"
  _pp_go_required="$(_pp_go_version_norm "$(go version "$PRINTING_PRESS_BIN" 2>/dev/null)")"
  if [ -z "$_pp_go_installed" ] || [ -z "$_pp_go_required" ] || ! _pp_semver_lt "$_pp_go_installed" "$_pp_go_required"; then
    return 0
  fi

  echo ""
  if [ "${GOTOOLCHAIN:-auto}" = "local" ]; then
    echo "[setup-error] Go $_pp_go_required or newer is required by this cli-printing-press binary (installed: $_pp_go_installed)."
    echo "GOTOOLCHAIN=local disables automatic toolchain downloads, so later Go quality gates would fail."
    echo "Install Go $_pp_go_required or newer from https://go.dev/dl/, or unset GOTOOLCHAIN."
    echo ""
    return 1
  fi

  echo "[go-toolchain-old] Go $_pp_go_required or newer is required by this cli-printing-press binary (installed: $_pp_go_installed)."
  echo "PRESS_GO_INSTALLED=$_pp_go_installed"
  echo "PRESS_GO_REQUIRED=$_pp_go_required"
  echo "Default GOTOOLCHAIN behavior may download the required toolchain during Go commands."
  echo ""
  return 0
}
_pp_check_go_currency || { return 1 2>/dev/null || exit 1; }

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_CURRENT="$PRESS_RUNSTATE/current"

_pp_check_disk_space() {
  _pp_disk_warn_kb="${PRINTING_PRESS_DISK_WARN_KB:-3145728}"
  _pp_disk_fail_kb="${PRINTING_PRESS_DISK_FAIL_KB:-524288}"
  case "$_pp_disk_warn_kb$_pp_disk_fail_kb" in
    ""|*[!0-9]*) return 0 ;;
  esac

  _pp_disk_path="$PRESS_HOME"
  while [ ! -e "$_pp_disk_path" ] && [ "$_pp_disk_path" != "/" ]; do
    _pp_disk_path="$(dirname "$_pp_disk_path")"
  done

  _pp_disk_avail_kb="$(df -Pk "$_pp_disk_path" 2>/dev/null | awk 'NR == 2 { print $4; exit }')"
  case "$_pp_disk_avail_kb" in
    ""|*[!0-9]*) return 0 ;;
  esac

  if [ "$_pp_disk_avail_kb" -lt "$_pp_disk_fail_kb" ]; then
    echo ""
    echo "[setup-error] Critically low disk space on the Printing Press workspace volume."
    echo "PRESS_DISK_PATH=$_pp_disk_path"
    echo "PRESS_DISK_AVAIL_KB=$_pp_disk_avail_kb"
    echo "PRESS_DISK_FAIL_KB=$_pp_disk_fail_kb"
    echo "Free disk space or set PRINTING_PRESS_HOME to a volume with more room, then re-run this skill."
    echo ""
    return 1
  fi

  if [ "$_pp_disk_avail_kb" -lt "$_pp_disk_warn_kb" ]; then
    echo ""
    echo "[low-disk] Printing Press workspace volume is low on free space."
    echo "PRESS_DISK_PATH=$_pp_disk_path"
    echo "PRESS_DISK_AVAIL_KB=$_pp_disk_avail_kb"
    echo "PRESS_DISK_WARN_KB=$_pp_disk_warn_kb"
    echo "This flow may need several GiB for generated files, Go build cache, module downloads, or repository clones."
    echo ""
  fi
}
_pp_check_disk_space || { return 1 2>/dev/null || exit 1; }

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY" "$PRESS_MANUSCRIPTS" "$PRESS_CURRENT"
```
<!-- PRESS_SETUP_CONTRACT_END -->

After running the setup contract, capture the `PRINTING_PRESS_BIN=<abs-path>` line from stdout. **Every subsequent `cli-printing-press ...` invocation in this skill must use that absolute path** (substitute the value, not the literal `$PRINTING_PRESS_BIN` token) — `export PATH` above only affects the single Bash tool call it runs in, so later calls open a fresh shell where bare `cli-printing-press` resolves against the user's default `PATH` and a stale global can shadow the local build.

If setup emitted `[go-toolchain-old]` or `[low-disk]`, surface the advisory to the user and continue unless setup also emitted `[setup-error]`. `[go-toolchain-old]` means later Go commands may download the required toolchain or fail when downloads are blocked; `[low-disk]` means this run may need several GiB for generated files, Go build cache, module downloads, or repository clones.

After capturing the binary path, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `<PRINTING_PRESS_BIN> version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, stop immediately and tell the user: "cli-printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest` to update."

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
cli-printing-press library list --json
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

2. If the manifest does not provide a category, present the full list via AskUserQuestion:
   - developer-tools, monitoring, cloud, project-management
   - productivity, social-and-messaging, sales-and-crm, marketing
   - payments, auth, commerce, ai, food-and-dining, health, maps, media-and-entertainment, devices, other
   - travel

## Step 4: Validate

Run:

```bash
cli-printing-press publish validate --dir <cli-dir> --json
```

`govulncheck` in this step is intentionally scoped to `<cli-dir>` only. It
uses the default `govulncheck ./...` mode so reachable symbol findings block
publish, while merely-required vulnerable modules without a call path do not
become release blockers. Do not replace this with a full public-library scan or
`govulncheck -show verbose`.

Parse the JSON result. Display each check result to the user:

```
Validating <api-slug>...
  manifest        PASS
  phase5          PASS
  go mod tidy     PASS
  govulncheck     PASS
  go vet          PASS
  go build        PASS
  --help          PASS
  --version       PASS
  manuscripts     WARN (no manuscripts found)
```

If `"passed": false`, report the failing checks and **stop**. Do not create a partial PR.
The `manifest` check is authoritative for the public-library provenance
contract: current `schema_version`, `run_id`, `printing_press_version`,
`printer`, `printer_name`, and MCP metadata files when MCP is advertised. If it
fails, tell the user to re-print or re-package with current Printing Press
metadata before opening the library PR.

Save the `help_output` field from the result — it's used in the PR description.

## Step 4.5: Live End-to-End Gate

Before touching the managed publish clone, rerun the live behavioral gate
against the CLI that is about to be published. Step 4 proves the source builds
and validates structurally; this step proves the current post-edit tree still
works against the real upstream API. Do not rely on an older
`phase5-acceptance.json` from generation or polish because the CLI may have
been hand-edited since that marker was written.

Resolve the Phase 5 proofs directory from the CLI manifest:

```bash
MANIFEST="$CLI_DIR/.printing-press.json"
API_SLUG=$(jq -r '.api_name // empty' "$MANIFEST")
CLI_NAME=$(jq -r '.cli_name // empty' "$MANIFEST")
RUN_ID=$(jq -r '.run_id // empty' "$MANIFEST")
AUTH_TYPE=$(jq -r '.auth_type // "none"' "$MANIFEST")
AUTH_ENV=$(jq -r '.auth_env_vars[0] // empty' "$MANIFEST")

if [ -z "$API_SLUG" ] || [ -z "$RUN_ID" ]; then
  echo "ERROR: manifest is missing api_name or run_id; cannot run publish live gate."
  exit 1
fi

PROOFS_DIR="$CLI_DIR/.manuscripts/$RUN_ID/proofs"
if [ ! -d "$PROOFS_DIR" ] && [ -n "$API_SLUG" ] && [ -d "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs" ]; then
  PROOFS_DIR="$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs"
elif [ ! -d "$PROOFS_DIR" ] && [ -n "$CLI_NAME" ] && [ -d "$PRESS_MANUSCRIPTS/$CLI_NAME/$RUN_ID/proofs" ]; then
  PROOFS_DIR="$PRESS_MANUSCRIPTS/$CLI_NAME/$RUN_ID/proofs"
fi
mkdir -p "$PROOFS_DIR"
```

If `SKIP_LIVE_TEST_REASON` is unset, run full live dogfood and write a fresh
acceptance marker into that proofs directory:

```bash
LIVE_GATE_JSON="$PROOFS_DIR/publish-live-gate.json"
LIVE_GATE_ARGS=(
  dogfood
  --dir "$CLI_DIR"
  --live
  --level full
  --timeout 120s
  --write-acceptance "$PROOFS_DIR/phase5-acceptance.json"
  --json
)
if [ -n "$AUTH_ENV" ]; then
  LIVE_GATE_ARGS+=(--auth-env "$AUTH_ENV")
fi

rm -f "$PROOFS_DIR/phase5-skip.json"
if ! "$PRINTING_PRESS_BIN" "${LIVE_GATE_ARGS[@]}" >"$LIVE_GATE_JSON"; then
  echo "Publish live gate failed. See $LIVE_GATE_JSON and $PROOFS_DIR/phase5-acceptance.json."
  jq -r '.tests[]? | select(.status == "fail") | "- \(.command) [\(.kind)]: \(.reason // "failed")"' "$LIVE_GATE_JSON" 2>/dev/null || true
  exit 1
fi
```

On failure, stop exactly like Step 4's `passed: false`: no managed clone, no
branch, no package, no PR. Report the failed command, exit code when present,
stderr or reason snippet, and the path to the fresh proof files so the operator
can re-run dogfood and fix the CLI.

If `SKIP_LIVE_TEST_REASON` is set from `--skip-live-test=<reason>`, write a
fresh skip marker instead of running dogfood:

```bash
SKIP_REASON_LOWER=$(printf '%s' "$SKIP_LIVE_TEST_REASON" | tr '[:upper:]' '[:lower:]')
case "$AUTH_TYPE" in
  api_key|bearer_token|oauth2)
    ;;
  none)
    case "$SKIP_REASON_LOWER" in
      *upstream*outage*|lan-unreachable-from-generation-host)
        ;;
      *)
        echo "ERROR: --skip-live-test is only valid for auth_type=none during a known upstream outage or LAN-unreachable hardware case."
        exit 1
        ;;
    esac
    ;;
  *)
    echo "ERROR: --skip-live-test is not valid for auth_type=$AUTH_TYPE. Run the live gate instead."
    exit 1
    ;;
esac

API_KEY_AVAILABLE=false
if [ -n "$AUTH_ENV" ] && [ -n "${!AUTH_ENV:-}" ]; then
  API_KEY_AVAILABLE=true
fi

rm -f "$PROOFS_DIR/phase5-acceptance.json"
jq -n \
  --arg api "$API_SLUG" \
  --arg run "$RUN_ID" \
  --arg reason "$SKIP_LIVE_TEST_REASON" \
  --arg auth "$AUTH_TYPE" \
  --argjson api_key_available "$API_KEY_AVAILABLE" \
  --argjson browser_session_available false \
  '{
    schema_version: 1,
    api_name: $api,
    run_id: $run,
    status: "skip",
    level: "none",
    skip_reason: $reason,
    auth_context: {
      type: $auth,
      api_key_available: $api_key_available,
      browser_session_available: $browser_session_available
    }
  }' > "$PROOFS_DIR/phase5-skip.json"
if [ "$SKIP_REASON_LOWER" = "lan-unreachable-from-generation-host" ]; then
  tmp_marker=$(mktemp "${TMPDIR:-/tmp}/phase5-skip.XXXXXX")
  jq '.auth_context.local_network_only = true' "$PROOFS_DIR/phase5-skip.json" > "$tmp_marker" &&
    mv "$tmp_marker" "$PROOFS_DIR/phase5-skip.json"
fi
LIVE_GATE_JSON=""
```

Then rerun Step 4's validation:

```bash
"$PRINTING_PRESS_BIN" publish validate --dir "$CLI_DIR" --json
```

This second validation proves the fresh acceptance or skip marker satisfies the
same Phase 5 contract that package and publish rely on. If it fails, stop
before Step 5.

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
   # Lightweight clone: blobless + shallow + sparse. The publish flow only
   # touches the target CLI's own directory — it no longer regenerates the
   # cli-skills/registry mirror (see Step 6) — so materializing every other
   # CLI's source is wasted bandwidth and disk (a full clone is multiple GB;
   # this is tens of MB). The cone keeps `tools`, `cli-skills`, and the target
   # `library/<category>` so the in-category find/rm/copy operations below work
   # on a real working tree. Cross-category collision checks use `git ls-tree`
   # (which reads the full tree from the blobless clone) instead of `ls`.
   git clone --filter=blob:none --depth 1 --sparse "$REPO_URL" "$PUBLISH_REPO_DIR"
   # Skill-managed clones are owned by this flow; force LF checkout behavior so
   # Windows core.autocrlf defaults do not create CRLF-only mirror diffs.
   git -C "$PUBLISH_REPO_DIR" config core.autocrlf false
   git -C "$PUBLISH_REPO_DIR" sparse-checkout set tools cli-skills library/<category>
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

   # Lightweight clone (blobless + shallow + sparse) — see the push-access
   # branch above for the rationale and cone contents.
   git clone --filter=blob:none --depth 1 --sparse "$FORK_URL" "$PUBLISH_REPO_DIR"
   # Skill-managed clones are owned by this flow; force LF checkout behavior so
   # Windows core.autocrlf defaults do not create CRLF-only mirror diffs.
   git -C "$PUBLISH_REPO_DIR" config core.autocrlf false
   cd "$PUBLISH_REPO_DIR"
   git sparse-checkout set tools cli-skills library/<category>
   git remote add upstream "$UPSTREAM_URL"
   git fetch --filter=blob:none --depth 1 upstream
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
git config core.autocrlf false

if [ "$(jq -r .access $PUBLISH_CONFIG)" = "push" ]; then
  # Push access: origin IS the upstream
  git fetch --filter=blob:none --depth 1 origin
  git checkout main
  git reset --hard origin/main
else
  # Fork: origin is the fork, upstream is canonical
  git fetch --filter=blob:none --depth 1 upstream
  git checkout main
  git reset --hard upstream/main
  # Also sync origin (fork) so git push works cleanly
  git push origin main --force-with-lease 2>/dev/null || true
fi

# Existing managed clones may already be sparse for a different publish
# category. Refresh the cone for the current target category before Step 6 uses
# filesystem-based removal and copy operations.
if git -C "$PUBLISH_REPO_DIR" config --bool core.sparseCheckout | grep -qx true; then
  git -C "$PUBLISH_REPO_DIR" sparse-checkout set tools cli-skills library/<category>
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

### Pre-package publication-state snapshot

Before Step 6 mutates the managed clone, record whether this API slug already
exists in the public library tree. Step 6 removes and replaces
`library/*/<api-slug>`, so any collision or publication-path decision made
after packaging must use this pre-package snapshot, not a fresh `ls`.

```bash
# Read from the git tree, not the working dir: the sparse checkout only
# materializes the target category, but a slug can collide in any category.
PREEXISTING_MERGED_PATHS=$(git -C "$PUBLISH_REPO_DIR" ls-tree -r --name-only HEAD \
  | sed -n 's#^\(library/[^/]*/<api-slug>\)/.*#\1#p' | sort -u || true)
PREEXISTING_MERGED_COLLISION=false
if [ -n "$PREEXISTING_MERGED_PATHS" ]; then
  PREEXISTING_MERGED_COLLISION=true
  # If this is a category-change reprint, materialize the existing category path
  # before Step 6 runs filesystem-based ledger preservation and removal.
  if git -C "$PUBLISH_REPO_DIR" config --bool core.sparseCheckout | grep -qx true; then
    while IFS= read -r EXISTING_MERGED_PATH; do
      [ -n "$EXISTING_MERGED_PATH" ] || continue
      if [ "$EXISTING_MERGED_PATH" != "library/<category>/<api-slug>" ]; then
        git -C "$PUBLISH_REPO_DIR" sparse-checkout add "$EXISTING_MERGED_PATH"
      fi
    done <<EOF
$PREEXISTING_MERGED_PATHS
EOF
  fi
fi
```

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

cli-printing-press publish package \
  --dir <cli-dir> \
  --category <category> \
  --target "$STAGING_DIR" \
  --module-path "$MODULE_PATH" \
  --json
```

Parse the JSON result. Note the `staged_dir`, `module_path`, `manuscripts_included`, and `run_id`. The `module_path` field confirms the Go module path that was set in the packaged CLI's `go.mod` and import paths.

`publish package` performs the mandatory vendor-prefix secret scan over the staged CLI, including copied manuscripts, before returning success. If it reports `vendor-prefix tokens detected`, stop and remove or redact the reported file:line findings before retrying. This is a hard gate and does not depend on `gitleaks`, `trufflehog`, or destination-repo push protection.

Then copy the staged CLI into the publish repo, replacing any existing version
while preserving the public library's release ledger files when this is a
reprint:

```bash
STAGED_CLI_DIR="$STAGING_DIR/library/<category>/<api-slug>"
DEST_CATEGORY_DIR="$PUBLISH_REPO_DIR/library/<category>"
DEST_CLI_DIR="$DEST_CATEGORY_DIR/<api-slug>"

if [ ! -d "$STAGED_CLI_DIR" ]; then
  echo "missing staged CLI directory: $STAGED_CLI_DIR" >&2
  exit 1
fi
mkdir -p "$DEST_CATEGORY_DIR"

# Preserve release-ledger files from the current public-library entry before
# removing it. New CLIs omit .printing-press-release.json until the library's
# post-merge workflow stamps a real release; reprints keep existing changelog
# history and release metadata until that workflow stamps the next release.
RELEASE_LEDGER_TMP="$(mktemp -d)"
PUBLISH_SWAP_DIR="$(mktemp -d "$DEST_CATEGORY_DIR/.<api-slug>.XXXXXX")"
trap 'rm -rf "$RELEASE_LEDGER_TMP" "$PUBLISH_SWAP_DIR"' EXIT
for LEDGER_FILE in CHANGELOG.md .printing-press-release.json; do
  EXISTING_LEDGER="$(find "$PUBLISH_REPO_DIR/library" -mindepth 3 -maxdepth 3 -path "*/<api-slug>/$LEDGER_FILE" -print -quit)"
  if [ -n "$EXISTING_LEDGER" ]; then
    cp "$EXISTING_LEDGER" "$RELEASE_LEDGER_TMP/$LEDGER_FILE"
  fi
done

# Copy staged CLI into a same-category swap dir before deleting the current
# public-library entry. This keeps a failed copy from leaving the publish repo
# with the old CLI removed.
cp -R "$STAGED_CLI_DIR/." "$PUBLISH_SWAP_DIR/"

for LEDGER_FILE in CHANGELOG.md .printing-press-release.json; do
  if [ -f "$RELEASE_LEDGER_TMP/$LEDGER_FILE" ]; then
    cp "$RELEASE_LEDGER_TMP/$LEDGER_FILE" "$PUBLISH_SWAP_DIR/$LEDGER_FILE"
  fi
done

# Remove existing version (handles category changes), then atomically install
# the prepared replacement within the destination category.
rm -rf "$PUBLISH_REPO_DIR/library"/*/"<api-slug>"
mv "$PUBLISH_SWAP_DIR" "$DEST_CLI_DIR"
rm -rf "$RELEASE_LEDGER_TMP"
trap - EXIT

# Remove root-level binaries (should not be committed). publish package
# already strips these before the copy; this rm -f is belt-and-suspenders
# for the agent path. Cover the names local build paths can drop: bare slug,
# CLI binary, live-dogfood probe binary, and MCP peer.
rm -f "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/<api-slug>" \
      "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/<cli-name>" \
      "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/<cli-name>-dogfood" \
      "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/<api-slug>-pp-mcp"

# Defense-in-depth: validate printer attribution before README and registry surfaces.
PRINTER=$(jq -r '.printer // ""' "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/.printing-press.json")
PRINTER_NAME=$(jq -r '.printer_name // ""' "$PUBLISH_REPO_DIR/library/<category>/<api-slug>/.printing-press.json")
if [ -z "$PRINTER" ]; then
  echo "ERROR: manifest .printer is empty. Set 'git config --global github.user <your-handle>' and re-print before publishing."
  exit 1
fi
if [ "$PRINTER" = "USER" ] || [ "$PRINTER" = "user" ]; then
  echo "ERROR: manifest .printer is the literal sentinel \"$PRINTER\" (git config github.user was unset at print time). Set it and re-print before publishing."
  exit 1
fi
if [ -z "$PRINTER_NAME" ]; then
  echo "ERROR: manifest .printer_name is empty. Set 'git config --global user.name <your display name>' and re-print before publishing."
  exit 1
fi

# Do NOT regenerate or commit `cli-skills/pp-<api-slug>/SKILL.md` or
# `registry.json` here. Both are regenerated post-merge by the library's
# `generate-skills.yml` and `generate-registry.yml` workflows via
# `[skip ci]` bot commits. The library's `Fail on changes to generated
# artifacts` check in `verify-library-conventions.yml` hard-fails any PR
# whose diff against base touches these files, regardless of fork vs
# same-repo origin. The library no longer has an in-PR auto-fix path;
# do not re-introduce a mirror or registry regen here. Also do NOT hand-update
# CHANGELOG.md, .printing-press-release.json, or runtime version strings for
# release accounting; the library release-ledger workflow owns those post-merge.

# Verify this changed/new CLI builds and has no reachable Go vulnerabilities from the publish repo
cd "$PUBLISH_REPO_DIR/library/<category>/<api-slug>" \
  && go build ./... \
  && go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
```

Keep vulnerability verification scoped to `library/<category>/<api-slug>` in
publish PRs. The public library is a historical collection and cannot be kept
fully current on every unrelated PR; whole-library govulncheck sweeps belong in
a scheduled/reporting workflow, while blocking CI should scan only added or
changed CLI modules.

After the publish repo copy and build verification are complete, remove the staging
directory:

```bash
rm -rf "$STAGING_PARENT"
```

Note: `staged_dir` is keyed by the API slug (e.g., `espn`), matching the publish repo's directory layout. The copy step is a same-name copy, not a rename.

## Step 6.5: Record Customizations

Before collision detection or branch creation, inspect the packaged CLI's
customizations index:

The index ships in one of two shapes: the per-patch directory
`.printing-press-patches/` (current) or the legacy single-array
`.printing-press-patches.json` (older prints, not yet normalized). Validate
whichever is present:

```bash
PATCHES_DIR="$PUBLISH_REPO_DIR/library/<category>/<api-slug>/.printing-press-patches"
PATCHES_INDEX="$PUBLISH_REPO_DIR/library/<category>/<api-slug>/.printing-press-patches.json"
if [ -d "$PATCHES_DIR" ]; then
  # Per-patch directory: every <id>.json must be a JSON object carrying the same
  # provenance the legacy single-array file kept at its top level (now per file),
  # so validation is at parity with the legacy branch below. _meta.json
  # (CLI-global lists) and .gitkeep are exempt.
  for f in "$PATCHES_DIR"/*.json; do
    [ -e "$f" ] || continue
    [ "$(basename "$f")" = "_meta.json" ] && continue
    if ! jq -e '
      (type == "object") and
      (.schema_version | type == "number") and
      (.id | type == "string" and length > 0) and
      (.applied_at | type == "string" and length > 0) and
      (.base_run_id | type == "string" and length > 0) and
      (.base_printing_press_version | type == "string" and length > 0)
    ' "$f" >/dev/null; then
      echo "ERROR: packaged CLI has a malformed patch file $f. Reprint with a current cli-printing-press binary before publishing."
      exit 1
    fi
  done
elif [ -f "$PATCHES_INDEX" ]; then
  if ! jq -e '
    (.schema_version | type == "number") and
    (.applied_at | type == "string" and length > 0) and
    (.base_run_id | type == "string" and length > 0) and
    (.base_printing_press_version | type == "string" and length > 0) and
    (.patches | type == "array")
  ' "$PATCHES_INDEX" >/dev/null; then
    echo "ERROR: packaged CLI has malformed .printing-press-patches.json. Reprint with a current cli-printing-press binary before publishing."
    exit 1
  fi
else
  echo "ERROR: packaged CLI is missing its patches index (.printing-press-patches/ or .printing-press-patches.json). Reprint with a current cli-printing-press binary before publishing."
  exit 1
fi
```

Fresh prints from current `cli-printing-press generate` include the
`.printing-press-patches/` directory with just a `.gitkeep`; leave it unchanged
when no hand customization was made after generation. If neither shape is
present, the CLI was generated by an older binary; reprint with a current
`cli-printing-press` build rather than synthesizing the
deterministic provenance fields by hand.

## Step 6.6: Record contributor attribution

When the human running this publish is **not** the CLI's original creator,
record them as a contributor so they are credited in the README byline, NOTICE,
and the public registry. The command is idempotent — it skips the creator and
anyone already listed — so it is safe to run on every publish:

```bash
"$PRINTING_PRESS_BIN" contributors add \
  --dir "$PUBLISH_REPO_DIR/library/<category>/<api-slug>" \
  || echo "note: this binary predates 'contributors add'; skipping contributor recording"
```

The step is best-effort: `contributors add` is an additive command, so a binary
that predates it simply skips recording rather than blocking the publish (the
`min-binary-version` floor only tracks the major). Pass `--front` when this
publish is a reprint (a from-scratch regeneration) so the reprinter is listed
first among contributors. Never edit `contributors[]` or the `creator` block by
hand — the creator is permanent, and the command owns the list (matching the
manifest-as-authority rule).

If you changed generated CLI files during the print or publish session, record
one concise entry per customization before opening the library PR — one
`.printing-press-patches/<id>.json` file per patch (the directory supersedes the
legacy `patches[]` array, so concurrent PRs never conflict). These entries are
the durable hand-edit contract that tells future agents and regen tooling what
must be preserved beyond generator output.

Use this shape (one file, `.printing-press-patches/<id>.json`):

```json
{
  "schema_version": 2,
  "id": "<api-slug>-<short-feature-name>",
  "applied_at": "<YYYY-MM-DD>",
  "base_run_id": "<copy from .printing-press.json>",
  "base_printing_press_version": "<copy from .printing-press.json>",
  "summary": "What changed (one sentence).",
  "reason": "Why the generated output needed this customization.",
  "files": ["internal/cli/example.go"],
  "validated_outcome": "Optional: focused check that proved the customization.",
  "upstream_issue": "Optional: https://github.com/mvanhorn/cli-printing-press/issues/<n>"
}
```

Rules:

- Filename is the patch `id`. Use kebab-case ids prefixed with the API slug for
  grep-ability across the public library.
- Keep `summary` and `reason` short. Each entry is an index, not a duplicate
  of the git diff.
- Include non-Go support files in `files` when they are part of the same
  code-level customization. README/SKILL.md-only polish does not need a patch
  manifest entry.
- Inline `// PATCH(...)` source comments are optional navigation aids. The public
  library verifier requires a patches index (the directory or the legacy file)
  and well-formed entries; it does not require a marker/comment pairing.
- If an entry exists only to work around an old verifier or pipeline bug that no
  longer applies, delete the stale workaround file instead of carrying it
  forward.

For the authoritative public-library authoring contract, read the
`mvanhorn/printing-press-library` `AGENTS.md` section
"`.printing-press-patches/` records library-side customizations".

## Step 7: Collision Detection & Resolution

After the managed clone is freshened, check for name collisions before creating a branch or PR. This replaces the previous "Check for Existing PR" step.

### Detection

Run these checks in sequence:

**1. Check merged CLIs in managed clone:**

```bash
MERGED_COLLISION="$PREEXISTING_MERGED_COLLISION"
MERGED_PATHS="$PREEXISTING_MERGED_PATHS"
```

Use the pre-package snapshot from Step 5. Do not re-run `ls
"$PUBLISH_REPO_DIR/library"/*/"<api-slug>"` here: Step 6 has already copied the
new package into that path, so a fresh `ls` would make every new print look like
a merged collision. If `MERGED_COLLISION=true`, note the category path from
`MERGED_PATHS`.

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

This is the existing update flow with a divergence guard. Set
`EXISTING_PR_NUMBER` from the detection step and proceed to Step 8, which
fetches the current PR branch head, checks for branch-only fixes that the new
package would revert, and only then handles force-push and PR description
update.

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
# Check merged (read the git tree, not the sparse working dir)
git -C "$PUBLISH_REPO_DIR" ls-tree -r --name-only HEAD \
  | sed -n 's#^\(library/[^/]*/<suggestion>\)/.*#\1#p' | sort -u
# Check open PRs
gh pr list --repo mvanhorn/printing-press-library --head "feat/<suggestion>" --state open --json number
```

If a suggestion collides, skip it or increment the numeric suffix.

**4. Rename the CLI in the publish repo:**

Since Step 6 copied the staged CLI into `$PUBLISH_REPO_DIR`, the rename operates on that directory. Note: `--old-name`/`--new-name` still use CLI-name format (e.g., `dub-pp-cli`) because `RenameCLI` does content replacement — bare slugs would cause collateral damage. The `--dir` path uses the slug-keyed directory.

```bash
cli-printing-press publish rename \
  --dir "$PUBLISH_REPO_DIR/library/<category>/<api-slug>" \
  --old-name <old-cli-name> \
  --new-name "$NEW_CLI_NAME" \
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

Fetch and inspect the current PR branch before replacing it. The latest
`origin/main` plus the newly packaged `library/<category>/<api-slug>/` tree is
the proposed update. The remote PR branch may also contain accepted review
fixes from the drive-to-green loop. Those branch-only edits must not be erased
silently.

```bash
UPDATE_BRANCH="feat/<api-slug>"
UPDATE_BASE_REF="refs/printing-press-update-base/<api-slug>"

git fetch origin "+main:refs/remotes/origin/main" "+$UPDATE_BRANCH:$UPDATE_BASE_REF"

# Show the scoped change from the current PR head to the new packaged working
# tree. This is informational for clean updates and mandatory context for holds.
git diff --stat "$UPDATE_BASE_REF" -- "library/<category>/<api-slug>/"

# Branch-only paths are files that exist on the current PR branch but are absent
# from the new packaged working tree. These are always a hold because a
# force-push would delete them.
WORKTREE_PATHS=$(find "library/<category>/<api-slug>" -type f -print 2>/dev/null | sort)
BRANCH_ONLY_PATHS=$(comm -23 \
  <(git ls-tree -r --name-only "$UPDATE_BASE_REF" -- "library/<category>/<api-slug>/" | sort) \
  <([ -n "$WORKTREE_PATHS" ] && printf '%s\n' "$WORKTREE_PATHS" || true))

# Modified paths need human review only when a branch patch relative to
# origin/main is not present in the new packaged working tree. A strict superset
# passes: if the branch patch can be reverse-applied from the working tree, the
# fix is still there even if the file also has fresh generated changes.
BRANCH_ONLY_EDITS=$(git diff --name-only origin/main "$UPDATE_BASE_REF" -- "library/<category>/<api-slug>/" | while read -r path; do
  [ -n "$path" ] || continue
  if git diff origin/main "$UPDATE_BASE_REF" -- "$path" | git apply --check --reverse >/dev/null 2>&1; then
    continue
  else
    printf '%s\n' "$path"
  fi
done | sort -u)

if [ -n "$BRANCH_ONLY_PATHS$BRANCH_ONLY_EDITS" ]; then
  echo "HOLD: updating PR #$EXISTING_PR_NUMBER would overwrite branch-only changes."
  if [ -n "$BRANCH_ONLY_PATHS" ]; then
    echo "Files present on the PR branch but missing from the new package:"
    printf '%s\n' "$BRANCH_ONLY_PATHS" | sed 's/^/- /'
  fi
  if [ -n "$BRANCH_ONLY_EDITS" ]; then
    echo "Files edited on the PR branch and changed again by the new package:"
    printf '%s\n' "$BRANCH_ONLY_EDITS" | sed 's/^/- /'
  fi
  echo "Do not push yet. Reconcile by restoring the branch-only fixes onto the new tree, or ask the user for explicit overwrite confirmation after showing the paths above."
  exit 1
fi
```

If the guard exits, offer the user two choices via `AskUserQuestion`:

- **Reconcile first** — restore the named branch-only files or edits onto the
  new packaged tree, keep or add matching `.printing-press-patches/<id>.json`
  records for code-level fixes, rerun Step 6 verification, then rerun this
  divergence guard.
- **Overwrite intentionally** — only after the user confirms the listed paths
  are obsolete, continue and include a PR-body note naming the overwritten
  branch-only paths.

If the guard finds no branch-only paths or edits, overwrite the local branch:

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
git add library/
# The library .gitignore has a `*-pp-mcp` rule intended for the built MCP
# binary, but the pattern also matches the generated MCP *source* directory
# `cmd/<api-slug>-pp-mcp/`. A plain `git add library/` silently skips it, and
# the PR then fails the "Validate MCPB manifest contract" CI check with
# "cmd/<api-slug>-pp-mcp directory is missing". Force-add the source directory
# so all MCP entry-point files ship. The built binary stays ignored because it
# lives at the packaged CLI root, not under cmd/.
git add -f "library/<category>/<api-slug>/cmd/<api-slug>-pp-mcp/" 2>/dev/null || true
git commit -m "feat(<api-slug>): add <api-slug>"
```

Push to origin (which is the fork for non-push users, or the upstream for push users):

**If updating an existing PR** (`EXISTING_PR_NUMBER` is set):

```bash
# Only run this after the update-path divergence guard above has passed, or
# after the user explicitly confirmed an intentional overwrite of the named
# branch-only paths.
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

### Capture the pushed commit SHA

After pushing, capture the head commit SHA. This is used to build durable manuscript links in the PR body (see "Build the PR description" below).

```bash
HEAD_SHA=$(git rev-parse HEAD)
```

The SHA stays resolvable on `mvanhorn/printing-press-library` for the life of the PR (GitHub mirrors fork-PR head commits to `refs/pull/<N>/head` on the upstream), and remains valid after the PR is merged and the branch is deleted. Each invocation of this skill captures a fresh `HEAD_SHA` after its push and rewrites the body, so links stay current across updates the skill performs. If the branch is force-pushed outside this skill, re-run `/printing-press-publish` to refresh the body — the prior links will still resolve, but they'll point at the manuscript contents from before the out-of-band push.

### Create or update PR

Read `access` and `gh_user` from `$PUBLISH_CONFIG`. These determine how `gh pr create` is called.

**For fork-based PRs** (`access` is `fork`): use `--head <gh_user>:feat/<api-slug>` so GitHub creates a cross-repo PR from the fork to the upstream. Without `--head`, `gh pr create` would try to find the branch on the upstream repo (where the user can't push) and fail.

**For push-access PRs** (`access` is `push`): use `--head feat/<api-slug>` so GitHub creates the PR from the branch this flow just pushed, even when the managed clone or shell session has other branches checked out.

Build the PR description from:
- The manifest (`description`, `api_name`, `category`, `printing_press_version`, `spec_url`)
- The manifest's `novel_features` array from the packaged CLI after Step 6
- The `help_output` captured in Step 4
- The CLI's README (first 2-3 paragraphs, or note that README is missing)
- Links to every file under `.manuscripts/<run-id>/research/` and `.manuscripts/<run-id>/proofs/`. Each link must be a full `https://github.com/mvanhorn/printing-press-library/blob/<HEAD_SHA>/library/<category>/<api-slug>/.manuscripts/<run-id>/<subdir>/<filename>` URL — never a relative path (GitHub resolves those against `…/pull/`, producing broken `…/pull/library/…` URLs) and never a directory (the blob view requires a file). Enumerate the actual files; do not invent or skip them.
- The validation results from Step 4
- The publish live gate result from Step 4.5, including any explicit
  `--skip-live-test` reason
- A Gaps section listing any missing manifest fields

Read `novel_features` from
`$PUBLISH_REPO_DIR/library/<category>/<api-slug>/.printing-press.json` after
packaging. Preserve the manifest order. Do not derive
this section from README prose, SKILL prose, root help, or memory of the run:
those surfaces may be summarized or hand-edited, while the packaged manifest is
the publish-time source of truth. For each entry, include the command, name, and
description. If the array is empty, write `No novel commands recorded in
.printing-press.json.` and include the missing field in **Gaps**; do not omit the
section.

Also include a publication-path line so new prints, reprints, PR updates, and
collision renames are distinguishable:
- `New print` — no merged CLI and no existing PR matched this slug.
- `Update existing PR #<N>` — this publish refreshes an open PR.
- `Reprint/replace` — a merged library CLI existed before this publish and the
  selected path replaces it. This must be based on
  `PREEXISTING_MERGED_COLLISION=true`, not on the post-package tree.
- `Alongside print` — this publish renamed the API slug to avoid a collision;
  include the original slug.
If `/printing-press-reprint` handed off a degraded reprint with no prior
public-library source, use `New print` and add the degraded-reprint note only if
that context is available from the handoff.

**MANDATORY: Before constructing the PR body, scrub all workspace PII.** The library
repo is public. Scan any live test results, acceptance data, or manuscript excerpts
for organization names, team member names, and email addresses. Replace with generic
descriptions ("the workspace", "5 team members", "12 users"). Team keys (e.g., "ESP")
are OK but org names (e.g., "Acme Corp") are not. See `references/secret-protection.md`
in the printing-press skill for the full policy.

Write the constructed PR body to a temporary Markdown file and pass it with
`--body-file`. Do this for both PR creation and PR updates. Do not inline the
body in a shell argument; large fenced help output, Markdown tables, and
backticks are too easy to mangle.

**PR description template:**

```markdown
## <api-slug>

<If this is a Replace path, add: "⚠️ **Replaces existing `<api-slug>`** — <reason from user>">

<description from manifest, or "No description available">

**API:** <api_name> | **Category:** <category> | **Press version:** <printing_press_version>
**Spec:** <spec_url or "Not specified">

### Publication Path

<New print | Update existing PR #N | Reprint/replace | Alongside print from <original-api-slug>>

### CLI Shape

\`\`\`bash
$ <cli-name> --help
<help_output from validation>
\`\`\`

### Novel Commands

| Command | Name | Description |
|---------|------|-------------|
| `<command>` | <name> | <description> |

### What This CLI Does

<First 2-3 paragraphs from README.md in the CLI directory, or "README not found">

### Manuscripts

<!-- One bullet per file, NOT one per directory. Repeat the research/ row for every file in research/, and the proofs/ row for every file in proofs/. Use a human label that matches the file (e.g. `Research Brief`, `Absorb Manifest`, `Novel Features Brainstorm`, `Phase 5 Acceptance`). Substitute `<HEAD_SHA>` with the value captured after push. Do NOT use relative paths. -->

- [<label>](https://github.com/mvanhorn/printing-press-library/blob/<HEAD_SHA>/library/<category>/<api-slug>/.manuscripts/<run-id>/research/<filename>)
- [<label>](https://github.com/mvanhorn/printing-press-library/blob/<HEAD_SHA>/library/<category>/<api-slug>/.manuscripts/<run-id>/research/<filename>)
- … (one bullet for each remaining file in `.manuscripts/<run-id>/research/`)
- [<label>](https://github.com/mvanhorn/printing-press-library/blob/<HEAD_SHA>/library/<category>/<api-slug>/.manuscripts/<run-id>/proofs/<filename>)
- [<label>](https://github.com/mvanhorn/printing-press-library/blob/<HEAD_SHA>/library/<category>/<api-slug>/.manuscripts/<run-id>/proofs/<filename>)
- … (one bullet for each remaining file in `.manuscripts/<run-id>/proofs/`)

### Validation Results

| Check | Result |
|-------|--------|
| Manifest | PASS/FAIL |
| Phase 5 | PASS/FAIL |
| go mod tidy | PASS/FAIL |
| govulncheck (this CLI only, reachable findings) | PASS/FAIL |
| go vet | PASS/FAIL |
| go build | PASS/FAIL |
| --help | PASS/FAIL |
| --version | PASS/FAIL |
| Manuscripts | PRESENT/MISSING |

### Publish Live Gate

<If Step 4.5 ran dogfood: "Full live dogfood reran at publish time and passed. Proof: `<proof path or manuscript link>`">
<If Step 4.5 was skipped: "Skipped with explicit reason: `<SKIP_LIVE_TEST_REASON>`">

### Gaps

<List any missing manifest fields, or omit this section if everything is present>
```

**If updating an existing PR** (`EXISTING_PR_NUMBER` is set):

```bash
cd "$PUBLISH_REPO_DIR"
PR_BODY_FILE="$(mktemp)"
# Write the constructed PR body Markdown to "$PR_BODY_FILE".
gh pr edit "$EXISTING_PR_NUMBER" \
  --repo mvanhorn/printing-press-library \
  --body-file "$PR_BODY_FILE"
rm -f "$PR_BODY_FILE"
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

PR_BODY_FILE="$(mktemp)"
# Write the constructed PR body Markdown to "$PR_BODY_FILE".

gh pr create \
  --repo mvanhorn/printing-press-library \
  --head "$PR_HEAD_REF" \
  --base main \
  --title "feat(<api-slug>): add <api-slug>" \
  --body-file "$PR_BODY_FILE"

rm -f "$PR_BODY_FILE"
```

Display the full PR URL (e.g., `https://github.com/mvanhorn/printing-press-library/pull/10`), not the shorthand `org/repo#N` format. The full URL is clickable in all terminals and contexts.

## After the PR opens

Once the PR is open, it enters the public library repo's review contract. That contract is owned by [`mvanhorn/printing-press-library` AGENTS.md → "Automated code review with Greptile"](https://github.com/mvanhorn/printing-press-library/blob/main/AGENTS.md#automated-code-review-with-greptile); read it for the canonical version. An agent invoking this skill from `cli-printing-press` will not have loaded the library's AGENTS.md, so the obligations are summarized here.

Greptile reviews **incrementally**: every commit you push re-triggers a fresh review, which can surface new findings the previous round didn't. This is a loop, not a single pass — drive the PR to a *stable* green and don't declare done after round one.

### Drive the PR to stable green

Iterate until **all** of these hold, confirmed by the review that your most recent fix commit triggered:

- **Greptile score ≥ 4.** The 0-5 score is a confidence signal, not a hard gate; 4/5 and 5/5 are both acceptable end states, and the score lands there naturally once threads are addressed.
- **No unresolved review threads.** For each P0/P1/P2 thread, either push a fix or reply with a concrete reason it shouldn't fire — not "won't fix", but *why* the code is right as written or *why* deferral is justified.
- **All CI checks pass.** `verify-library-conventions`, `Govulncheck`, and any other workflow on the PR.

Read findings from two surfaces — they don't overlap:

- `gh pr view <PR> --repo <owner>/<repo> --comments` returns the top-level issue conversation (Greptile's summary comment, score, CI bots).
- `gh api repos/<owner>/<repo>/pulls/<PR>/comments` returns the inline diff-anchored review comments — Greptile posts each P0/P1/P2 finding here, **and these are NOT included in `--comments`**. Skipping this call is how an agent silently declares "all findings resolved" while every inline thread is still open.

**Monitoring is the harness's job, not a busy-loop you hand-roll.** Use whatever PR-activity monitoring your environment provides — react to review/CI events as they arrive, or re-check on an interval if it doesn't push events. After each fix push, wait for the re-triggered review to land before judging done; a new round can reopen the gate.

**Don't hand-edit `registry.json` or `cli-skills/pp-<api-slug>/SKILL.md` to satisfy a finding** — both are bot-regenerated post-merge by `[skip ci]` commits, and the library's `Fail on changes to generated artifacts` check pre-rejects any PR that touches them.

### Terminal state — then hand back

Once the PR is stably green, the skill's job is done. **Do not merge it and do not poll waiting for it to merge** — merges into the public library are the maintainer's manual review, not this skill's and (for a fork contributor) not the user's either.

Read `access` from `$PUBLISH_CONFIG` (`jq -r .access "$PUBLISH_CONFIG"`) to determine what to do next:

- **If `access` is `push`** (maintainer/admin with push access): apply the `awaiting-maintainer` label to signal the PR is ready for manual review:
  ```bash
  gh pr edit <PR> --repo mvanhorn/printing-press-library --add-label awaiting-maintainer
  ```
- **If `access` is `fork`** (community contributor): you cannot merge or label the upstream PR. There is nothing more to do once it's green.

Then **report the terminal state and return control to the caller.** Do not offer a retro or any follow-up menu from this skill by default — that decision belongs to whoever invoked publish. The `printing-press` pipeline offers retro as its own post-publish tail; a direct human invocation without `--from-polish` just ends here.

If `POLISH_HANDOFF=true`, offer retro as a soft tail after the PR is green. This preserves the standalone polish -> publish workflow without allowing polish's same-turn `AskUserQuestion` answer to create or update a public-library PR.

Present via `AskUserQuestion`:

> "PR opened: <PR_URL>. Run a retro? It surfaces systemic gaps from this session (generator misses, scorer bugs, skill-doc drift) as a GitHub issue for the Printing Press maintainers. Every retro filed raises the floor for the next CLI, and your session context is freshest right now."
>
> 1. **No, I'm done** (default)
> 2. **Yes, run retro now**

If the user picks yes, invoke `/printing-press-retro`.

## Secret & PII Protection

Before creating the PR, verify that no secrets leaked into the packaged CLI.

**This matters because the library repo is public.** A leaked API key in a PR is
a security incident — anyone can see it, even if the PR is later closed.

### What the Printing Press checks (deterministic)

The generation skill (`/printing-press`) runs an exact-value scan during Phase 5.5
if the user provided an API key. By the time publish runs, the Printing Press's own
mistakes should already be caught. But the user may have edited files between
generation and publish.

### What publish checks

1. **Mandatory binary scan:** `cli-printing-press publish package` scans the staged CLI and manuscripts for live-looking vendor-prefix tokens (`sk-or-v1-*`, `sk_live_*`, `ghp_*`, `ghs_*`, `xoxb-*`, `AKIA*`, and similar). If it fails with `vendor-prefix tokens detected`, treat the package as unpublishable. Do not copy, commit, push, or open a PR until the reported file:line findings are removed or redacted.

2. **If the user's exact API key value is known**, scan the packaged tree before creating the PR. This catches edits or manuscripts added after Phase 5.5:
   ```bash
   if [ -n "$API_KEY_VALUE" ] && [ ${#API_KEY_VALUE} -ge 16 ]; then
     if grep -rF "$API_KEY_VALUE" "$PUBLISH_REPO_DIR/library/<category>/<api-slug>" 2>/dev/null; then
       echo "BLOCKING: API key value found in staged publish tree."
       exit 1
     fi
   fi
   ```

3. **If `gitleaks` or `trufflehog` is installed**, run it as an enrichment pass on the staged directory:
   ```bash
   if command -v gitleaks >/dev/null 2>&1; then
     gitleaks detect --source "<staging-dir>/library" --no-git --verbose 2>&1
   elif command -v trufflehog >/dev/null 2>&1; then
     trufflehog filesystem "<staging-dir>/library" 2>&1
   fi
   ```
   These tools use vendor-specific patterns (Steam keys, Stripe keys, GitHub
   tokens) with low false-positive rates. Their findings add detector breadth
   beyond the mandatory floor. Review any finding before proceeding.

4. **Always do the lightweight structural check:**
   - Verify no `.env` files, `session-state.json`, or `config.toml` with
     real credentials exist in the staged directory
   - Check README examples use `"your-key-here"` placeholders, not real values
   - Check manuscripts (if included) don't contain auth headers or cookie values

5. **Never include** in the staged directory:
   - `.env` files
   - `session-state.json`
   - Config files with real credentials
   - HAR captures with un-stripped auth headers

If the mandatory binary scan or exact-value scan finds issues, stop. For
external-tool or lightweight structural findings, warn the user and ask whether
to proceed. The user makes the final call on those non-mandatory findings.

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
