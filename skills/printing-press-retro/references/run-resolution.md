# Run resolution

Resolve the API, run, and CLI directory before entering any phase. The rest of
the skill reads `$API_NAME`, `$RUN_ID`, `$RUN_DIR`, `$CLI_DIR`, `$IN_REPO`, and
`$RETRO_SCRATCH_DIR`.

## Setup

<!-- RETRO_SETUP_START -->
```bash
# Path-only setup — no binary detection required.
# The retro skill reads manuscripts and runs gh/curl. It does not invoke the
# cli-printing-press binary. This avoids aborting for users who installed the
# plugin but not the Go binary.

_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_LIBRARY="$PRESS_HOME/library"
RETRO_SCRATCH_DIR="/tmp/printing-press/retro"

mkdir -p "$PRESS_MANUSCRIPTS" "$PRESS_LIBRARY" "$RETRO_SCRATCH_DIR"

# Detect whether we're inside the printing-press repo
IN_REPO=false
if [ -f "$_scope_dir/cmd/cli-printing-press/main.go" ]; then
  IN_REPO=true
  REPO_ROOT="$_scope_dir"
  echo "Running from printing-press repo: $REPO_ROOT"
fi
```
<!-- RETRO_SETUP_END -->

## Nothing to retro

```bash
if [ ! -d "$PRESS_MANUSCRIPTS" ] || [ -z "$(ls -A "$PRESS_MANUSCRIPTS" 2>/dev/null)" ]; then
  echo "No manuscripts found. Run /printing-press first to generate a CLI."
  exit 1
fi
```

## Resolve which API

If the user passed an API name as an argument, use that. Validate for path traversal:

```bash
# Reject names with /, \, or ..
if echo "$USER_API_NAME" | grep -qE '[/\\]|\.\.'; then
  echo "Invalid API name: '$USER_API_NAME'. Names cannot contain path separators or '..'."
  exit 1
fi

# Verify resolved path stays under PRESS_MANUSCRIPTS
RESOLVED="$(cd "$PRESS_MANUSCRIPTS/$USER_API_NAME" 2>/dev/null && pwd -P)"
case "$RESOLVED" in
  "$PRESS_MANUSCRIPTS"/*) ;; # OK
  *) echo "Invalid API name: path resolves outside manuscripts directory."; exit 1 ;;
esac
```

If no API name was provided and multiple APIs exist, list them with their most recent
run dates and ask the user to choose:

```bash
echo "Multiple APIs found in manuscripts:"
for api_dir in "$PRESS_MANUSCRIPTS"/*/; do
  api_name=$(basename "$api_dir")
  latest=$(ls -t "$api_dir" 2>/dev/null | head -1)
  echo "  - $api_name (latest run: $latest)"
done
```

Use `AskUserQuestion` to let the user pick.

## Resolve which run

If the API has multiple runs, default to the most recent. If the user specified a
run ID, use that. Otherwise:

```bash
API_DIR="$PRESS_MANUSCRIPTS/$API_NAME"
RUN_ID=$(ls -t "$API_DIR" 2>/dev/null | head -1)
RUN_DIR="$API_DIR/$RUN_ID"

echo "Retro for: $API_NAME (run $RUN_ID)"
echo "Manuscripts: $RUN_DIR"
```

## Resolve CLI directory

```bash
API_SLUG="$API_NAME"
CLI_NAME="${API_SLUG}-pp-cli"
CLI_DIR="$PRESS_LIBRARY/$CLI_NAME"

if [ ! -d "$CLI_DIR" ]; then
  # Try without -pp-cli suffix (legacy naming)
  CLI_DIR="$PRESS_LIBRARY/$API_NAME"
fi

if [ ! -d "$CLI_DIR" ]; then
  echo "WARNING: CLI directory not found at $PRESS_LIBRARY/$CLI_NAME"
  echo "Proceeding with manuscripts only — CLI source will not be included in artifacts."
  CLI_DIR=""
fi
```
