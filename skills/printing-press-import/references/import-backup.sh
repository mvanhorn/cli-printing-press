#!/usr/bin/env bash
# import-backup.sh — zip an existing internal library CLI + its manuscripts
# to /tmp/printing-press/ before overwriting.
#
# Usage:
#   import-backup.sh <api-slug>
#
# Backs up:
#   $HOME/printing-press/library/<api-slug>/
#   $HOME/printing-press/manuscripts/<api-slug>/  (if present)
#
# Output: prints the absolute path of the resulting zip on stdout.

set -euo pipefail

[[ $# -eq 1 ]] || { echo "usage: $0 <api-slug>" >&2; exit 2; }

API_SLUG="$1"
BACKUP_DIR="/tmp/printing-press"
TS=$(date -u +%Y%m%dT%H%M%SZ)
ZIP_PATH="$BACKUP_DIR/${API_SLUG}-${TS}.zip"

LIBRARY_DIR="$HOME/printing-press/library/$API_SLUG"
MANUSCRIPTS_DIR="$HOME/printing-press/manuscripts/$API_SLUG"

if [[ ! -d "$LIBRARY_DIR" && ! -d "$MANUSCRIPTS_DIR" ]]; then
  echo "nothing to backup for $API_SLUG" >&2
  exit 0
fi

mkdir -p "$BACKUP_DIR"

# Stage in a temp dir so the zip preserves the relative layout users
# would need to restore by hand: library/<api>/ and manuscripts/<api>/.
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

if [[ -d "$LIBRARY_DIR" ]]; then
  mkdir -p "$STAGE/library"
  cp -R "$LIBRARY_DIR" "$STAGE/library/"
fi
if [[ -d "$MANUSCRIPTS_DIR" ]]; then
  mkdir -p "$STAGE/manuscripts"
  cp -R "$MANUSCRIPTS_DIR" "$STAGE/manuscripts/"
fi

(cd "$STAGE" && zip -qr "$ZIP_PATH" .)
echo "$ZIP_PATH"
