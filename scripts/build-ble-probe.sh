#!/usr/bin/env bash
set -euo pipefail

mode="${1:-replay}"
out_root="${BLE_PROBE_OUT:-dist/ble-probe}"

case "$mode" in
  replay|live|all) ;;
  *)
    echo "usage: scripts/build-ble-probe.sh [replay|live|all]" >&2
    exit 2
    ;;
esac

build_one() {
  local flavor="$1"
  local goos="$2"
  local goarch="$3"
  local tags="$4"
  local exe=""
  if [[ "$goos" == "windows" ]]; then
    exe=".exe"
  fi

  local out_dir="$out_root/$flavor"
  local out="$out_dir/ble-probe-$goos-$goarch$exe"
  mkdir -p "$out_dir"

  echo "building $out"
  if [[ -n "$tags" ]]; then
    GOOS="$goos" GOARCH="$goarch" go build -tags "$tags" -trimpath -o "$out" ./cmd/ble-probe
  else
    GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$out" ./cmd/ble-probe
  fi
}

build_replay() {
  build_one replay darwin arm64 ""
  build_one replay linux amd64 ""
  build_one replay windows amd64 ""
}

build_live() {
  # TinyGo Bluetooth's CoreBluetooth backend does not currently cross-compile
  # from Apple Silicon to darwin/amd64. Build the native macOS artifact only.
  build_one live "$(go env GOOS)" "$(go env GOARCH)" ble_live

  # These targets compile from macOS today and are the first copy-to-machine
  # smoke artifacts for mainstream Linux and modern Windows PCs.
  build_one live linux amd64 ble_live
  build_one live windows amd64 ble_live
}

if [[ "$mode" == "replay" || "$mode" == "all" ]]; then
  build_replay
fi
if [[ "$mode" == "live" || "$mode" == "all" ]]; then
  build_live
fi
