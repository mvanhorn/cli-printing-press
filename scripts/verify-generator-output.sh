#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/verify-generator-output.sh [--list] [--keep] [--output-dir DIR] [case ...]

Generates selected golden cases into .gotmp/generator-output-compile, runs
`go mod tidy`, then runs `go build ./...` inside every generated Go module. Use
this after generator or template changes to catch emit/call mismatches that
source-level tests and byte-level goldens can miss.

When no case is supplied, a focused default set covers endpoint templates,
MCP/code-orchestration, rich auth, GraphQL shared endpoints, learn-loop
emission, and generated BLE device variants. Pass extra case names when a fix
touches another generated variant.
USAGE
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

cases_root="testdata/golden/cases"
output_root="${GENERATOR_OUTPUT_VERIFY_DIR:-.gotmp/generator-output-compile}"
keep=false
golden_owner="printing-press-golden"

# Match the generated-CLI compile-test environment used by Go TestMain files.
# The generator consults PRINTING_PRESS_AGENTCOOKIE_REPLACE while emitting
# go.mod, which lets local/CI verification compile without private repo access.
if [[ -z "${GOPRIVATE:-}" ]]; then
  export GOPRIVATE="github.com/mvanhorn/*"
fi
if [[ -z "${PRINTING_PRESS_AGENTCOOKIE_REPLACE:-}" ]]; then
  export PRINTING_PRESS_AGENTCOOKIE_REPLACE="$repo_root/internal/testdata/agentcookie"
fi

default_cases=(
  generate-golden-api
  generate-golden-api-rich-auth
  generate-mcp-api
  generate-graphql-shared-endpoint
  generate-learn-loop-api
  generate-device-ble
  generate-device-ble-control
  generate-device-ble-session
  generate-device-ble-opaque
)

cases=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --list)
      find "$cases_root" -mindepth 1 -maxdepth 1 -type d -name 'generate-*' -print |
        sed "s|^$cases_root/||" |
        sort
      exit 0
      ;;
    --keep)
      keep=true
      shift
      ;;
    --output-dir)
      if [[ $# -lt 2 ]]; then
        echo "--output-dir requires a value" >&2
        exit 2
      fi
      output_root="$2"
      shift 2
      ;;
    --*)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      cases+=("$1")
      shift
      ;;
  esac
done

if [[ "${#cases[@]}" -eq 0 ]]; then
  cases=("${default_cases[@]}")
fi

binary="./cli-printing-press"
echo "Building $binary"
go build -o "$binary" ./cmd/cli-printing-press
trap 'rm -f "$binary"' EXIT

if [[ "$keep" != "true" ]]; then
  rm -rf "$output_root"
fi
mkdir -p "$output_root"

failures=0
for case_name in "${cases[@]}"; do
  case_dir="$cases_root/$case_name"
  command_file="$case_dir/command.txt"
  case_output="$output_root/$case_name"

  if [[ ! -f "$command_file" ]]; then
    echo "FAIL $case_name: missing $command_file" >&2
    failures=$((failures + 1))
    continue
  fi

  echo "Generating $case_name"
  rm -rf "$case_output"
  mkdir -p "$case_output"

  command_text="$(cat "$command_file")"
  if ! BINARY="$binary" CASE_ACTUAL_DIR="$case_output" REPO_ROOT="$repo_root" \
    GIT_CONFIG_COUNT=2 \
    GIT_CONFIG_KEY_0=github.user \
    GIT_CONFIG_VALUE_0="$golden_owner" \
    GIT_CONFIG_KEY_1=user.name \
    GIT_CONFIG_VALUE_1="$golden_owner" \
    bash -c "$command_text"; then
    echo "FAIL $case_name: generation command failed" >&2
    failures=$((failures + 1))
    continue
  fi

  module_count=0
  while IFS= read -r gomod; do
    module_count=$((module_count + 1))
    module_dir="$(dirname "$gomod")"
    echo "Tidying generated module ${module_dir#$repo_root/}"
    if ! (cd "$module_dir" && go mod tidy); then
      echo "FAIL $case_name: go mod tidy failed in $module_dir" >&2
      failures=$((failures + 1))
      continue
    fi

    echo "Building generated module ${module_dir#$repo_root/}"
    if ! (cd "$module_dir" && go build ./...); then
      echo "FAIL $case_name: go build ./... failed in $module_dir" >&2
      failures=$((failures + 1))
    fi
  done < <(find "$case_output" -name go.mod -type f -print | sort)

  if [[ "$module_count" -eq 0 ]]; then
    echo "FAIL $case_name: no generated go.mod found under $case_output" >&2
    failures=$((failures + 1))
  fi
done

if [[ "$failures" -gt 0 ]]; then
  echo "Generated-output verification failed: $failures failure(s)."
  echo "Outputs: $output_root"
  exit 1
fi

echo "Generated-output verification passed for ${#cases[@]} case(s)."
