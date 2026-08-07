#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/build-ble-probe.sh [replay|live|all] [--target GOOS/GOARCH] [--out DIR]

Builds the standalone BLE probe without printing a full CLI.

Examples:
  scripts/build-ble-probe.sh live
  scripts/build-ble-probe.sh live --target windows/amd64
  scripts/build-ble-probe.sh replay --target darwin/arm64 --out ./dist/ble-probe
USAGE
}

mode="replay"
target=""
out_root="${BLE_PROBE_OUT:-dist/ble-probe}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    replay|live|all)
      mode="$1"
      shift
      ;;
    --target)
      target="${2:-}"
      if [[ -z "$target" ]]; then
        usage
        exit 2
      fi
      shift 2
      ;;
    --out)
      out_root="${2:-}"
      if [[ -z "$out_root" ]]; then
        usage
        exit 2
      fi
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

split_target() {
  local value="$1"
  if [[ "$value" != */* ]]; then
    echo "--target must be GOOS/GOARCH, got: $value" >&2
    exit 2
  fi
  target_goos="${value%%/*}"
  target_goarch="${value##*/}"
  if [[ -z "$target_goos" || -z "$target_goarch" || "$target_goos" == "$target_goarch" ]]; then
    echo "--target must be GOOS/GOARCH, got: $value" >&2
    exit 2
  fi
}

build_one() {
  local flavor="$1"
  local goos="$2"
  local goarch="$3"
  local tags="$4"
  local exe=""
  if [[ "$goos" == "windows" ]]; then
    exe=".exe"
  fi

  local out_dir="$out_root/$flavor/$goos-$goarch"
  local out="$out_dir/ble-probe$exe"
  mkdir -p "$out_dir"

  echo "building $out"
  if [[ -n "$tags" ]]; then
    GOOS="$goos" GOARCH="$goarch" go build -tags "$tags" -trimpath -o "$out" ./cmd/ble-probe
  else
    GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$out" ./cmd/ble-probe
  fi
  echo "built $out"
  echo "doctor: $out doctor"
}

build_replay_target() {
  build_one replay "$1" "$2" ""
}

build_live_target() {
  build_one live "$1" "$2" ble_live
}

if [[ -n "$target" ]]; then
  target_goos=""
  target_goarch=""
  split_target "$target"
  if [[ "$mode" == "replay" || "$mode" == "all" ]]; then
    build_replay_target "$target_goos" "$target_goarch"
  fi
  if [[ "$mode" == "live" || "$mode" == "all" ]]; then
    build_live_target "$target_goos" "$target_goarch"
  fi
  exit 0
fi

if [[ "$mode" == "replay" || "$mode" == "all" ]]; then
  build_replay_target darwin arm64
  build_replay_target linux amd64
  build_replay_target windows amd64
fi
if [[ "$mode" == "live" || "$mode" == "all" ]]; then
  build_live_target "$(go env GOOS)" "$(go env GOARCH)"
  build_live_target linux amd64
  build_live_target windows amd64
fi
