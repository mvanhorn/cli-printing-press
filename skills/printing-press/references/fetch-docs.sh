#!/usr/bin/env bash
# fetch-docs.sh - fetch fidelity-sensitive docs without a summarization layer.
#
# Usage:
#   fetch-docs.sh [--ttl=<seconds>] [--force] [--md] <url>
#
# Prints the cached/captured file path and response metadata. Exits non-zero on
# network failure or any final HTTP status other than 200.

set -euo pipefail

usage() {
  echo "usage: $0 [--ttl=<seconds>] [--force] [--md] <url>" >&2
  exit 2
}

TTL_SECONDS=86400
FORCE=false
WANT_MD=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ttl=*)
      TTL_SECONDS="${1#--ttl=}"
      [[ "$TTL_SECONDS" =~ ^[0-9]+$ ]] || usage
      shift
      ;;
    --ttl)
      TTL_SECONDS="${2:-}"
      [[ "$TTL_SECONDS" =~ ^[0-9]+$ ]] || usage
      shift 2
      ;;
    --force)
      FORCE=true
      TTL_SECONDS=0
      shift
      ;;
    --md)
      WANT_MD=true
      shift
      ;;
    -h|--help)
      usage
      ;;
    --*)
      usage
      ;;
    *)
      break
      ;;
  esac
done

[[ $# -eq 1 ]] || usage
URL="$1"
case "$URL" in
  http://*|https://*) ;;
  *) echo "fetch-docs: URL must start with http:// or https://" >&2; exit 2 ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "fetch-docs: curl is required" >&2
  exit 127
fi

CACHE_ROOT="${TMPDIR:-/tmp}/printing-press-fetch-docs"
mkdir -p "$CACHE_ROOT"

if command -v shasum >/dev/null 2>&1; then
  URL_HASH="$(printf '%s' "$URL" | shasum -a 256 | awk '{print $1}')"
else
  URL_HASH="$(printf '%s' "$URL" | cksum | awk '{print $1}')"
fi

safe_slug() {
  printf '%s' "$1" |
    sed -E 's%^[a-zA-Z][a-zA-Z0-9+.-]*://%%; s%[/?#&=]+%-%g; s%[^A-Za-z0-9._-]+%-%g; s%^-+%%; s%-+$%%' |
    cut -c1-80
}

extension_for() {
  local url="$1"
  local content_type="$2"
  local path="${url%%\?*}"
  path="${path%%#*}"
  case "$path" in
    *.md|*.markdown) echo "md"; return ;;
    *.mdx) echo "mdx"; return ;;
    *.json) echo "json"; return ;;
    *.yaml|*.yml) echo "yaml"; return ;;
    *.txt) echo "txt"; return ;;
    *.xml) echo "xml"; return ;;
    *.html|*.htm) echo "html"; return ;;
  esac
  case "$content_type" in
    *markdown*) echo "md" ;;
    *json*) echo "json" ;;
    *yaml*|*yml*) echo "yaml" ;;
    *xml*) echo "xml" ;;
    *html*) echo "html" ;;
    *text/plain*) echo "txt" ;;
    *) echo "bin" ;;
  esac
}

SLUG="$(safe_slug "$URL")"
[[ -n "$SLUG" ]] || SLUG="docs"
BASE="$CACHE_ROOT/${SLUG}-${URL_HASH:0:16}"
META_FILE="$BASE.meta"

cache_fresh() {
  local file="$1"
  [[ -f "$file" && -f "$META_FILE" ]] || return 1
  [[ "$FORCE" == "false" ]] || return 1
  [[ "$TTL_SECONDS" -gt 0 ]] || return 1

  local now mtime age
  now="$(date +%s)"
  if mtime="$(stat -f %m "$file" 2>/dev/null)"; then
    :
  else
    mtime="$(stat -c %Y "$file" 2>/dev/null)" || return 1
  fi
  age=$((now - mtime))
  [[ "$age" -lt "$TTL_SECONDS" ]]
}

print_result() {
  local file="$1"
  local status="$2"
  local content_type="$3"
  local effective_url="$4"
  printf 'path=%s\n' "$file"
  printf 'status=%s\n' "$status"
  printf 'content_type=%s\n' "$content_type"
  printf 'effective_url=%s\n' "$effective_url"
}

cached_file=""
if [[ -f "$META_FILE" ]]; then
  cached_file="$(awk -F= '$1 == "path" {print substr($0, index($0, "=") + 1)}' "$META_FILE" 2>/dev/null || true)"
  if [[ -n "$cached_file" ]] && cache_fresh "$cached_file"; then
    status="$(awk -F= '$1 == "status" {print substr($0, index($0, "=") + 1)}' "$META_FILE")"
    content_type="$(awk -F= '$1 == "content_type" {print substr($0, index($0, "=") + 1)}' "$META_FILE")"
    effective_url="$(awk -F= '$1 == "effective_url" {print substr($0, index($0, "=") + 1)}' "$META_FILE")"
    if [[ "$status" != "200" ]]; then
      echo "fetch-docs: cached HTTP $status for $URL (body: $cached_file, final: $effective_url)" >&2
      exit 22
    fi
    print_result "$cached_file" "$status" "$content_type" "$effective_url"
    exit 0
  fi
fi

BODY_TMP="$(mktemp "$CACHE_ROOT/fetch-docs-body.XXXXXX")"
HEADER_TMP="$(mktemp "$CACHE_ROOT/fetch-docs-headers.XXXXXX")"
trap 'rm -f ${BODY_TMP:+"$BODY_TMP"} "$HEADER_TMP"' EXIT

CURL_WRITE=$'status=%{http_code}\ncontent_type=%{content_type}\neffective_url=%{url_effective}\n'
if ! CURL_META="$(curl -sS -L --compressed --connect-timeout 10 --max-time 30 \
    -D "$HEADER_TMP" -o "$BODY_TMP" -w "$CURL_WRITE" "$URL" 2>&1)"; then
  echo "fetch-docs: curl failed for $URL" >&2
  printf '%s\n' "$CURL_META" >&2
  exit 1
fi

status="$(printf '%s\n' "$CURL_META" | awk -F= '$1 == "status" {print substr($0, index($0, "=") + 1)}')"
content_type="$(printf '%s\n' "$CURL_META" | awk -F= '$1 == "content_type" {print substr($0, index($0, "=") + 1)}')"
effective_url="$(printf '%s\n' "$CURL_META" | awk -F= '$1 == "effective_url" {print substr($0, index($0, "=") + 1)}')"
[[ -n "$status" ]] || status="000"

ext="$(extension_for "$effective_url" "$content_type")"
RAW_FILE="$BASE.$ext"
mv "$BODY_TMP" "$RAW_FILE"
BODY_TMP=""

if [[ "$status" != "200" ]]; then
  {
    printf 'path=%s\n' "$RAW_FILE"
    printf 'status=%s\n' "$status"
    printf 'content_type=%s\n' "$content_type"
    printf 'effective_url=%s\n' "$effective_url"
  } > "$META_FILE"
  echo "fetch-docs: HTTP $status for $URL (body: $RAW_FILE, final: $effective_url)" >&2
  exit 22
fi

OUT_FILE="$RAW_FILE"
if [[ "$WANT_MD" == "true" && "$ext" != "md" && "$ext" != "mdx" ]]; then
  MD_FILE="$BASE.md"
  if command -v readability-cli >/dev/null 2>&1 && command -v turndown-cli >/dev/null 2>&1; then
    if readability-cli "$effective_url" < "$RAW_FILE" | turndown-cli > "$MD_FILE"; then
      OUT_FILE="$MD_FILE"
      content_type="text/markdown; converted-from=$content_type"
    else
      echo "fetch-docs: markdown conversion failed; keeping raw capture $RAW_FILE" >&2
      rm -f "$MD_FILE"
    fi
  elif command -v npx >/dev/null 2>&1; then
    if npx --yes readability-cli "$effective_url" < "$RAW_FILE" | npx --yes turndown-cli > "$MD_FILE"; then
      OUT_FILE="$MD_FILE"
      content_type="text/markdown; converted-from=$content_type"
    else
      echo "fetch-docs: npx markdown conversion failed; keeping raw capture $RAW_FILE" >&2
      rm -f "$MD_FILE"
    fi
  else
    echo "fetch-docs: --md requested but readability-cli/turndown-cli or npx are unavailable; keeping raw capture $RAW_FILE" >&2
  fi
fi

{
  printf 'path=%s\n' "$OUT_FILE"
  printf 'status=%s\n' "$status"
  printf 'content_type=%s\n' "$content_type"
  printf 'effective_url=%s\n' "$effective_url"
} > "$META_FILE"

print_result "$OUT_FILE" "$status" "$content_type" "$effective_url"
