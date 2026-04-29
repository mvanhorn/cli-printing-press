#!/usr/bin/env bash
# import-fetch.sh — fetch one CLI subtree + its .manuscripts from the
# public library into a staging dir.
#
# Usage:
#   import-fetch.sh <library-path> <staging-dir> [--clone <local-clone-path>]
#
# Where <library-path> is the `path` field from registry.json (e.g.
# `library/productivity/cal-com`). When --clone is supplied, the script
# copies from the local clone instead of hitting GitHub.
#
# Exits 0 on success; non-zero on any failure.

set -euo pipefail

usage() {
  echo "usage: $0 <library-path> <staging-dir> [--clone <path>]" >&2
  exit 2
}

[[ $# -lt 2 ]] && usage

LIB_PATH="$1"
STAGING="$2"
shift 2

CLONE_PATH=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --clone)
      CLONE_PATH="${2:-}"
      [[ -z "$CLONE_PATH" ]] && usage
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

mkdir -p "$STAGING"

if [[ -n "$CLONE_PATH" ]]; then
  SRC="$CLONE_PATH/$LIB_PATH"
  [[ -d "$SRC" ]] || { echo "source not found in clone: $SRC" >&2; exit 1; }
  cp -R "$SRC/." "$STAGING/"
  exit 0
fi

# Remote fetch: shallow clone the whole repo to a temp dir, then copy the
# subtree out. GitHub's contents API can't return whole subtrees in one
# call and rate-limits per-file fetching for large CLIs. A shallow clone
# is ~2-5 MB and one round-trip.
TMP_CLONE=$(mktemp -d)
trap 'rm -rf "$TMP_CLONE"' EXIT

git clone --depth 1 --quiet \
  https://github.com/mvanhorn/printing-press-library.git \
  "$TMP_CLONE"

SRC="$TMP_CLONE/$LIB_PATH"
[[ -d "$SRC" ]] || { echo "source not found in clone: $SRC" >&2; exit 1; }
cp -R "$SRC/." "$STAGING/"
