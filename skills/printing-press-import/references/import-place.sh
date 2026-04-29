#!/usr/bin/env bash
# import-place.sh — atomically move a staged CLI + its manuscripts into
# the internal library at $HOME/printing-press/.
#
# Layout:
#   <staging>/                      (CLI files at root)
#   <staging>/.manuscripts/<run>/   (one or more run-id dirs)
#
# Lands as:
#   $HOME/printing-press/library/<api-slug>/             (CLI files)
#   $HOME/printing-press/manuscripts/<api-slug>/<run>/   (each run dir)
#
# Any pre-existing target dirs are removed first; back them up with
# import-backup.sh before invoking this.
#
# Usage:
#   import-place.sh <staging-dir> <api-slug>

set -euo pipefail

[[ $# -eq 2 ]] || { echo "usage: $0 <staging-dir> <api-slug>" >&2; exit 2; }

STAGING="$1"
API_SLUG="$2"

[[ -d "$STAGING" ]] || { echo "staging dir not found: $STAGING" >&2; exit 1; }

LIB_TARGET="$HOME/printing-press/library/$API_SLUG"
MAN_TARGET_ROOT="$HOME/printing-press/manuscripts/$API_SLUG"

# Move manuscripts out of the staging dir before placing the CLI. This
# keeps the CLI subtree clean (no .manuscripts/ inside the library dir).
MAN_STAGE="$STAGING/.manuscripts"

mkdir -p "$(dirname "$LIB_TARGET")"
rm -rf "$LIB_TARGET"

if [[ -d "$MAN_STAGE" ]]; then
  mkdir -p "$MAN_TARGET_ROOT"
  for run_dir in "$MAN_STAGE"/*/; do
    [[ -d "$run_dir" ]] || continue
    run_name=$(basename "$run_dir")
    run_target="$MAN_TARGET_ROOT/$run_name"
    rm -rf "$run_target"
    mv "$run_dir" "$run_target"
  done
  rmdir "$MAN_STAGE" 2>/dev/null || true
fi

# Now the staging dir contains only CLI files; move it into place.
mv "$STAGING" "$LIB_TARGET"

echo "placed: $LIB_TARGET"
[[ -d "$MAN_TARGET_ROOT" ]] && echo "manuscripts: $MAN_TARGET_ROOT"
