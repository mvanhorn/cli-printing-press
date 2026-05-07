#!/usr/bin/env bash
set -euo pipefail

mode="${1:-verify}"
if [[ "$mode" != "verify" && "$mode" != "update" && "$mode" != "list" && "$mode" != "--list" ]]; then
  echo "usage: scripts/golden.sh [verify|update|list]" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

binary="./printing-press"
cases_root="testdata/golden/cases"
expected_root="testdata/golden/expected"
actual_root=".gotmp/golden/actual"
actual_abs="$repo_root/$actual_root"
golden_owner="printing-press-golden"

escape_sed() {
  printf "%s" "$1" | sed -e 's/[\/&|]/\\&/g'
}

repo_root_pattern="$(escape_sed "$repo_root")"
home_pattern="$(escape_sed "$HOME")"
actual_root_pattern="$(escape_sed "$actual_root")"
actual_abs_pattern="$(escape_sed "$actual_abs")"

# Keep normalization intentionally narrow. These substitutions remove
# machine-specific paths while preserving behaviorally meaningful output.
normalize_text() {
  sed \
    -e "s|$actual_abs_pattern|<ARTIFACT_DIR>|g" \
    -e "s|$actual_root_pattern|<ARTIFACT_DIR>|g" \
    -e "s|$repo_root_pattern|<REPO>|g" \
    -e "s|$home_pattern|<HOME>|g"
}

normalize_json() {
  local file="$1"

  case "$file" in
    */.printing-press.json)
      jq -S '
        .generated_at = "<GENERATED_AT>"
        | .printing_press_version = "<PRINTING_PRESS_VERSION>"
      ' "$file" | normalize_text
      ;;
    */manifest.json)
      # MCPB manifest version stamps the printing-press release; normalize
      # so the golden doesn't drift on every release bump.
      jq -S '.version = "<PRINTING_PRESS_VERSION>"' "$file" | normalize_text
      ;;
    *)
      jq -S . "$file" | normalize_text
      ;;
  esac
}

case_names() {
  find "$cases_root" -mindepth 1 -maxdepth 1 -type d -print |
    sed "s|^$cases_root/||" |
    sort
}

artifact_list() {
  local case_name="$1"
  local manifest="$cases_root/$case_name/artifacts.txt"

  if [[ ! -f "$manifest" ]]; then
    return 0
  fi

  sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$manifest"
}

require_update_allowed() {
  if [[ "${GOLDEN_ALLOW_DIRTY:-}" == "1" ]]; then
    return 0
  fi

  if [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
    echo "Refusing to update golden fixtures with a dirty worktree." >&2
    echo "Review or commit changes first, or set GOLDEN_ALLOW_DIRTY=1 intentionally." >&2
    exit 1
  fi
}

list_cases() {
  local case_name
  local artifacts

  for case_name in $(case_names); do
    echo "$case_name"
    sed 's/^/  /' "$cases_root/$case_name/command.txt"

    artifacts="$(artifact_list "$case_name")"
    if [[ -n "$artifacts" ]]; then
      echo "  artifacts:"
      printf "%s\n" "$artifacts" | sed 's/^/    /'
    fi
  done
}

run_case() {
  local case_name="$1"
  local case_dir="$cases_root/$case_name"
  local out_dir="$actual_root/$case_name"
  local raw_stdout="$out_dir/stdout.raw"
  local raw_stderr="$out_dir/stderr.raw"
  local exit_file="$out_dir/exit.txt"
  local exit_code=0
  local command_text

  if [[ ! -f "$case_dir/command.txt" ]]; then
    echo "missing command.txt for golden case: $case_name" >&2
    return 2
  fi

  mkdir -p "$out_dir"
  command_text="$(cat "$case_dir/command.txt")"

  # Both git config keys use the same slug-shaped fixture value so goldens
  # are deterministic. In production, `user.name` resolves to a prose
  # display name (e.g. "Trevin Chow"); golden output's slug-shaped
  # `author:` field is a fixture artifact, not the production shape.
  BINARY="$binary" CASE_ACTUAL_DIR="$out_dir" REPO_ROOT="$repo_root" \
    GIT_CONFIG_COUNT=2 \
    GIT_CONFIG_KEY_0=github.user \
    GIT_CONFIG_VALUE_0="$golden_owner" \
    GIT_CONFIG_KEY_1=user.name \
    GIT_CONFIG_VALUE_1="$golden_owner" \
    bash -c "$command_text" >"$raw_stdout" 2>"$raw_stderr" || exit_code=$?

  normalize_text <"$raw_stdout" >"$out_dir/stdout.txt"
  normalize_text <"$raw_stderr" >"$out_dir/stderr.txt"
  printf "%s\n" "$exit_code" >"$exit_file"
}

normalize_artifacts() {
  local case_name="$1"
  local actual_dir="$actual_root/$case_name"
  local normalized_dir="$actual_dir/_normalized"
  local artifact
  local source
  local target

  rm -rf "$normalized_dir"

  while IFS= read -r artifact; do
    source="$actual_dir/$artifact"
    target="$normalized_dir/$artifact"

    if [[ ! -f "$source" ]]; then
      echo "missing artifact for $case_name: $artifact" >&2
      return 1
    fi

    mkdir -p "$(dirname "$target")"
    case "$artifact" in
      *.json)
        normalize_json "$source" >"$target"
        ;;
      *)
        normalize_text <"$source" >"$target"
        ;;
    esac
  done < <(artifact_list "$case_name")
}

compare_file() {
  local case_name="$1"
  local label="$2"
  local expected="$3"
  local actual="$4"
  local diff_path="$5"

  mkdir -p "$(dirname "$diff_path")"
  if ! diff -u "$expected" "$actual" >"$diff_path"; then
    echo "FAIL $case_name $label"
    echo "  diff: $diff_path"
    return 1
  fi

  rm -f "$diff_path"
  return 0
}

compare_case() {
  local case_name="$1"
  local failed=0
  local actual_dir="$actual_root/$case_name"
  local expected_dir="$expected_root/$case_name"
  local artifact

  for file in stdout.txt stderr.txt exit.txt; do
    compare_file "$case_name" "$file" "$expected_dir/$file" "$actual_dir/$file" "$actual_dir/$file.diff" || failed=1
  done

  while IFS= read -r artifact; do
    compare_file \
      "$case_name" \
      "$artifact" \
      "$expected_dir/$artifact" \
      "$actual_dir/_normalized/$artifact" \
      "$actual_dir/$artifact.diff" || failed=1
  done < <(artifact_list "$case_name")

  return "$failed"
}

update_case() {
  local case_name="$1"
  local actual_dir="$actual_root/$case_name"
  local expected_dir="$expected_root/$case_name"
  local artifact

  mkdir -p "$expected_dir"
  cp "$actual_dir/stdout.txt" "$expected_dir/stdout.txt"
  cp "$actual_dir/stderr.txt" "$expected_dir/stderr.txt"
  cp "$actual_dir/exit.txt" "$expected_dir/exit.txt"

  while IFS= read -r artifact; do
    mkdir -p "$(dirname "$expected_dir/$artifact")"
    cp "$actual_dir/_normalized/$artifact" "$expected_dir/$artifact"
  done < <(artifact_list "$case_name")
}

if [[ "$mode" == "list" || "$mode" == "--list" ]]; then
  list_cases
  exit 0
fi

if [[ "$mode" == "update" ]]; then
  require_update_allowed
fi

if find "$cases_root" -name artifacts.txt -print -quit | grep -q . && ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for golden artifact normalization" >&2
  exit 1
fi

echo "Building $binary"
go build -o "$binary" ./cmd/printing-press

rm -rf "$actual_root"
mkdir -p "$actual_root"

failures=0
case_count=0
for case_name in $(case_names); do
  case_count=$((case_count + 1))
  run_case "$case_name"

  if ! normalize_artifacts "$case_name"; then
    failures=$((failures + 1))
    continue
  fi

  if [[ "$mode" == "update" ]]; then
    update_case "$case_name"
    echo "UPDATED $case_name"
    continue
  fi

  if compare_case "$case_name"; then
    echo "PASS $case_name"
  else
    failures=$((failures + 1))
  fi
done

if [[ "$mode" == "update" ]]; then
  if [[ "$failures" -gt 0 ]]; then
    echo "Golden update failed: $failures case(s) could not be normalized."
    echo "Actual outputs: $actual_root"
    exit 1
  fi

  echo "Golden fixtures updated for $case_count case(s)."
  exit 0
fi

if [[ "$failures" -gt 0 ]]; then
  echo "Golden verify failed: $failures case(s) changed."
  echo "Actual outputs: $actual_root"
  echo "Run scripts/golden.sh update only for intentional behavior changes."
  exit 1
fi

echo "Golden verify passed: $case_count case(s)."
