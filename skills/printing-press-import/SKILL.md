---
name: printing-press-import
description: >
  Bring a published CLI from the public library into the internal library
  so it's identical to a freshly-generated copy — module path reverted,
  manuscripts placed alongside, ready for /printing-press-polish or
  /printing-press-emboss. Use when the public library has a CLI you
  don't have locally, or to recover from a broken/lost internal copy.
  Trigger phrases: "import the CLI", "bring it into my library",
  "fetch from public library", "I don't have it locally yet".
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - AskUserQuestion
---

# /printing-press-import

Bring a published CLI from the public library
([`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library))
into the internal library at `$PRESS_LIBRARY/` so it matches
the form the generator would produce. Manuscripts ride along.

```bash
/printing-press-import notion
/printing-press-import cal.com
/printing-press-import allrecipes --from-clone ~/Code/printing-press-library
```

The internal library is the working copy; the public library is the
durable artifact. After import, the CLI is ready for polish, emboss, or
re-publish — the publish step will re-apply the module path rewrites.

## When to run

- The public library has a CLI you don't have locally
- The internal copy is broken, lost, or out of sync
- You want a clean baseline before running polish on a published CLI

If the user is asking to polish a CLI and mentions "in/from the public
library" or "from the repo", suggest running this skill first.

## Setup

```bash
PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
SCRIPTS_DIR="$(dirname "${BASH_SOURCE[0]:-$0}")/references"

# --- Go toolchain present (Phase 4 compiles the imported CLI with Go) ---
if ! command -v go >/dev/null 2>&1; then
  echo ""
  echo "[setup-error] Go toolchain not found."
  echo "Import compiles the imported CLI (Phase 4) with Go. Install Go from https://go.dev/dl/, verify with 'go version', then re-run."
  echo ""
  return 1 2>/dev/null || exit 1
fi

# --- Disk preflight (fail-open unless critically low) ---
# Import clones the library CLI tree (multi-GB with --clone), rewrites it, and
# compiles it. A nearly-full volume fails deep into the run with an opaque "no
# space left on device". Warn early; hard-stop only when space is critically
# low. Thresholds env-tunable for CI.
_pp_disk_warn_kb="${PRINTING_PRESS_DISK_WARN_KB:-3145728}"   # 3 GiB
_pp_disk_fail_kb="${PRINTING_PRESS_DISK_FAIL_KB:-524288}"    # 512 MiB
_pp_disk_target="$PRESS_HOME"
while [ -n "$_pp_disk_target" ] && [ "$_pp_disk_target" != "/" ] && [ ! -d "$_pp_disk_target" ]; do
  _pp_disk_target="$(dirname "$_pp_disk_target")"
done
[ -d "$_pp_disk_target" ] || _pp_disk_target="$HOME"
_pp_disk_avail_kb="$(df -Pk "$_pp_disk_target" 2>/dev/null | awk 'NR==2{print $4}')"
if printf '%s' "$_pp_disk_avail_kb" | grep -qE '^[0-9]+$'; then
  if [ "$_pp_disk_avail_kb" -lt "$_pp_disk_fail_kb" ]; then
    echo ""
    echo "[setup-error] Only $((_pp_disk_avail_kb / 1024)) MiB free on the volume holding $_pp_disk_target; import clone and compilation need more space."
    echo "Free up space (at least $((_pp_disk_fail_kb / 1024)) MiB, ideally a few GiB) and re-run."
    echo ""
    return 1 2>/dev/null || exit 1
  elif [ "$_pp_disk_avail_kb" -lt "$_pp_disk_warn_kb" ]; then
    echo ""
    echo "[low-disk] $((_pp_disk_avail_kb / 1024)) MiB free on the volume holding $_pp_disk_target"
    echo "PRESS_DISK_AVAIL_KB=$_pp_disk_avail_kb"
    echo "PRESS_DISK_WARN_KB=$_pp_disk_warn_kb"
    echo ""
  fi
fi
```

If the setup block printed a `[setup-error]` line (missing Go toolchain or
critically low disk), it already exited non-zero — surface the message verbatim
and stop; do not proceed to resolve, clone, or build. If it printed a
`[low-disk]` advisory, surface the free-space note to the user once and continue.

The four reference scripts live alongside this SKILL.md under
`references/`:

- `import-fetch.sh <library-path> <staging> [--clone <path>]`
- `import-backup.sh <api-slug>` (prints zip path on stdout)
- `import-rewrite.sh <staging> <api-slug>`
- `import-place.sh <staging> <api-slug>`

## Phase 1 — Resolve the CLI

The argument can be anything natural: an API slug (`notion`), a brand
name (`cal.com`), an old CLI name (`notion-pp-cli`), or close enough
(`Allrecipes`). Resolve via the public library's `registry.json` —
which carries `name`, `category`, `api`, `description`, and `path` for
every entry, in one fetch.

```bash
REGISTRY=$(mktemp)
gh api -H "Accept: application/vnd.github.v3.raw" \
  repos/mvanhorn/printing-press-library/contents/registry.json \
  > "$REGISTRY"
```

Match in this order:

1. **Exact `name` match** — `jq --arg q "$ARG" '.entries[] | select(.name == $q)' "$REGISTRY"`
2. **Normalized exact** — strip `-pp-cli` suffix, lowercase, dot→hyphen, then exact match
3. **Substring on `name` or `description`** — case-insensitive contains

```bash
# Exact:
jq --arg q "$ARG" '.entries[] | select(.name == $q)' "$REGISTRY"

# Normalized exact (after $ARG2 = lowercase, dot→hyphen, suffix-stripped):
jq --arg q "$ARG2" '.entries[] | select(.name == $q)' "$REGISTRY"

# Fuzzy (substring on name or description):
jq --arg q "$ARG2" '.entries[]
  | select((.name | ascii_downcase | contains($q | ascii_downcase))
        or (.description | ascii_downcase | contains($q | ascii_downcase)))
' "$REGISTRY"
```

If you get one match: use it. If multiple: present at most 4 to the user
via `AskUserQuestion` showing `name` + `description` per candidate. If
zero: tell the user the public library doesn't have that CLI.

The matched entry gives you everything you need:
- `LIB_PATH` from `.path` (e.g., `library/productivity/cal-com`)
- `API_SLUG` from `.name`
- `CATEGORY` from `.category`

**Don't slurp whole files** when reasoning over candidates. The fields
above are enough; if you genuinely need more, the per-CLI manifest is
just `<LIB_PATH>/manifest.json` and the description there can be pulled
the same way (`gh api -H "Accept: ... raw" .../manifest.json | jq -r '.description'`).

## Phase 2 — Decide on overwrite

Check whether the internal library already has this CLI:

```bash
LIB_TARGET="$PRESS_LIBRARY/$API_SLUG"
MAN_TARGET="$PRESS_MANUSCRIPTS/$API_SLUG"
```

**If neither exists:** straightforward import — proceed to Phase 3.

**If either exists:** read provenance from both sides to decide whether
to overwrite. Don't read whole `.printing-press.json` files — pull just
the fields that matter:

```bash
# Internal provenance (if present):
jq '{run_id, generated_at, printing_press_version, spec_checksum}' \
  "$LIB_TARGET/.printing-press.json" 2>/dev/null

# Public provenance (one-shot via raw):
gh api -H "Accept: application/vnd.github.v3.raw" \
  repos/mvanhorn/printing-press-library/contents/$LIB_PATH/.printing-press.json \
  | jq '{run_id, generated_at, printing_press_version, spec_checksum}'
```

Reason over the diff:

- **Same `run_id`** — public is the same generation as internal. Likely
  no-op; ask before clobbering. If the user wants to import anyway
  (e.g., to recover from a broken internal copy), proceed.
- **Public newer `generated_at`** — public has changes the internal
  doesn't. Importing is the safe move; ask the user to confirm.
- **Internal newer `generated_at`** — internal has work the public
  doesn't (in-progress polish, manual fixes). Importing would clobber
  that. Stop and surface this to the user — they likely want to publish
  the internal changes first.
- **Either side missing `.printing-press.json`** — older or hand-imported.
  Ask the user.

When the user confirms overwrite, the backup step in Phase 3 captures
the current internal state.

## Phase 3 — Import

```bash
STAGING=$(mktemp -d)

# Fetch (remote unless --from-clone was passed)
if [[ -n "${CLONE_PATH:-}" ]]; then
  bash "$SCRIPTS_DIR/import-fetch.sh" "$LIB_PATH" "$STAGING" --clone "$CLONE_PATH"
else
  bash "$SCRIPTS_DIR/import-fetch.sh" "$LIB_PATH" "$STAGING"
fi

# Backup if anything is being clobbered. Prints zip path on stdout.
if [[ -d "$LIB_TARGET" || -d "$MAN_TARGET" ]]; then
  BACKUP_ZIP=$(bash "$SCRIPTS_DIR/import-backup.sh" "$API_SLUG")
  echo "Backed up to: $BACKUP_ZIP"
fi

# Reverse the publish-step module path rewrites.
bash "$SCRIPTS_DIR/import-rewrite.sh" "$STAGING" "$API_SLUG"

# Atomically move staging into place.
bash "$SCRIPTS_DIR/import-place.sh" "$STAGING" "$API_SLUG"
```

## Phase 4 — Verify internal consistency

After the move, confirm the imported CLI builds and is structurally
intact. Treat any failure as a real problem — don't paper over it.

```bash
cd "$LIB_TARGET"

# Module path is local form
grep -q "^module ${API_SLUG}-pp-cli\$" go.mod \
  || { echo "FAIL: go.mod still on public module path"; exit 1; }

# No public module path leaked into source
if grep -rq "github.com/mvanhorn/printing-press-library/library" \
   --include='*.go' --include='*.yaml' --include='*.yml' .; then
  echo "FAIL: source still references public module path"
  exit 1
fi

# Build
go build ./... \
  || { echo "FAIL: go build"; exit 1; }

# Doctor (self-check)
make doctor 2>/dev/null \
  || ./bin/${API_SLUG}-pp-cli doctor 2>/dev/null \
  || true   # best-effort; not all CLIs have doctor wired the same way
```

Report the import outcome:

- Source path (from registry: `<category>/<api-slug>`)
- Run ID (from `.printing-press.json`)
- Manuscripts run-ids placed (count + names)
- Backup zip path (if any)
- Build status

## Polish-side hint

If the user's request to import was triggered by a polish ask (e.g.,
they said "polish notion in the public library"), suggest:

```
Imported $API_SLUG. To polish: /printing-press-polish $API_SLUG
```

The polish skill operates on the internal library, so import-then-polish
is the right flow when starting from a published CLI.
