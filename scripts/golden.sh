#!/usr/bin/env bash
set -euo pipefail

mode="${1:-verify}"
if [[ "$mode" != "verify" && "$mode" != "update" ]]; then
  echo "usage: scripts/golden.sh [verify|update]" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

binary="./printing-press"
actual_root=".gotmp/golden/actual"
expected_root="testdata/golden/expected"
actual_abs="$repo_root/$actual_root"

escape_sed() {
  printf "%s" "$1" | sed -e 's/[\/&|]/\\&/g'
}

repo_root_pattern="$(escape_sed "$repo_root")"
home_pattern="$(escape_sed "$HOME")"
actual_root_pattern="$(escape_sed "$actual_root")"
actual_abs_pattern="$(escape_sed "$actual_abs")"

# Keep normalization intentionally narrow. These substitutions remove
# machine-specific paths while preserving behaviorally meaningful output.
normalize() {
  local file="$1"
  sed \
    -e "s|$actual_abs_pattern|<ARTIFACT_DIR>|g" \
    -e "s|$actual_root_pattern|<ARTIFACT_DIR>|g" \
    -e "s|$repo_root_pattern|<REPO>|g" \
    -e "s|$home_pattern|<HOME>|g" \
    "$file"
}

run_case() {
  local case_name="$1"
  local out_dir="$actual_root/$case_name"
  mkdir -p "$out_dir"

  local raw_stdout="$out_dir/stdout.raw"
  local raw_stderr="$out_dir/stderr.raw"
  local exit_file="$out_dir/exit.txt"
  local exit_code=0

  case "$case_name" in
    catalog-list)
      "$binary" catalog list >"$raw_stdout" 2>"$raw_stderr" || exit_code=$?
      ;;
    catalog-show-petstore)
      "$binary" catalog show petstore >"$raw_stdout" 2>"$raw_stderr" || exit_code=$?
      ;;
    browser-sniff-sample)
      "$binary" browser-sniff \
        --har testdata/sniff/sample.har \
        --output "$out_dir/spec.yaml" \
        --analysis-output "$out_dir/traffic-analysis.json" \
        >"$raw_stdout" 2>"$raw_stderr" || exit_code=$?
      ;;
    *)
      echo "unknown golden case: $case_name" >&2
      return 2
      ;;
  esac

  normalize "$raw_stdout" >"$out_dir/stdout.txt"
  normalize "$raw_stderr" >"$out_dir/stderr.txt"
  printf "%s\n" "$exit_code" >"$exit_file"
}

compare_case() {
  local case_name="$1"
  local failed=0
  local actual_dir="$actual_root/$case_name"
  local expected_dir="$expected_root/$case_name"

  for file in stdout.txt stderr.txt exit.txt; do
    if ! diff -u "$expected_dir/$file" "$actual_dir/$file" >"$actual_dir/$file.diff"; then
      echo "FAIL $case_name $file"
      echo "  diff: $actual_dir/$file.diff"
      failed=1
    fi
  done

  return "$failed"
}

update_case() {
  local case_name="$1"
  local expected_dir="$expected_root/$case_name"
  mkdir -p "$expected_dir"
  cp "$actual_root/$case_name/stdout.txt" "$expected_dir/stdout.txt"
  cp "$actual_root/$case_name/stderr.txt" "$expected_dir/stderr.txt"
  cp "$actual_root/$case_name/exit.txt" "$expected_dir/exit.txt"
}

cases=(
  catalog-list
  catalog-show-petstore
  browser-sniff-sample
)

echo "Building $binary"
go build -o "$binary" ./cmd/printing-press

rm -rf "$actual_root"
mkdir -p "$actual_root"

failures=0
for case_name in "${cases[@]}"; do
  run_case "$case_name"

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
  echo "Golden fixtures updated for ${#cases[@]} case(s)."
  exit 0
fi

if [[ "$failures" -gt 0 ]]; then
  echo "Golden verify failed: $failures case(s) changed."
  echo "Actual outputs: $actual_root"
  echo "Run scripts/golden.sh update only for intentional behavior changes."
  exit 1
fi

echo "Golden verify passed: ${#cases[@]} case(s)."
