#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SPEC="testdata/golden/fixtures/golden-api.yaml"
APP="printing-press-golden-pp-cli"
MCP_APP="printing-press-golden-pp-mcp"
PREFIX="PRINTING_PRESS_GOLDEN"
AUTH_ENV="PRINTING_PRESS_GOLDEN_API_KEY"

TMP="${TMPDIR:-/tmp}"
TMP="${TMP%/}"
SCRATCH="$(mktemp -d "$TMP/printing-press-paths-smoke.XXXXXX")"
SERVER_PID=""
cleanup() {
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  chmod -R u+w "$SCRATCH" >/dev/null 2>&1 || true
  rm -rf "$SCRATCH"
}
trap cleanup EXIT

log() {
  printf '[paths-smoke] %s\n' "$*" >&2
}

run_clean() {
  local home="$1"
  shift
  env -i PATH="$PATH" HOME="$home" "$@"
}

run_cli() {
  local home="$1"
  local base_url="$2"
  shift 2
  env -i PATH="$PATH" HOME="$home" "${AUTH_ENV}=token" "${PREFIX}_BASE_URL=$base_url" "$@"
}

write_mock_server() {
  local script="$SCRATCH/mock_server.py"
  local port_file="$SCRATCH/mock_port"
  local server_log="$SCRATCH/mock_server.stderr"
  cat >"$script" <<'PY'
import http.server
import json
import pathlib
import socketserver
import sys

port_file = pathlib.Path(sys.argv[1])

class Handler(http.server.BaseHTTPRequestHandler):
    def _send(self, payload):
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/v1/currencies"):
            self._send([{"id": "item-1", "name": "One"}])
            return
        self._send({"ok": True})

    def log_message(self, fmt, *args):
        return

with socketserver.TCPServer(("127.0.0.1", 0), Handler) as httpd:
    port_file.write_text(str(httpd.server_address[1]))
    httpd.serve_forever()
PY
  python3 "$script" "$port_file" >/dev/null 2>"$server_log" &
  SERVER_PID="$!"
  for _ in {1..100}; do
    [[ -s "$port_file" ]] && break
    if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
      echo "mock server exited before publishing its port" >&2
      tail -40 "$server_log" >&2 || true
      exit 1
    fi
    sleep 0.05
  done
  if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    echo "mock server exited during startup" >&2
    tail -40 "$server_log" >&2 || true
    exit 1
  fi
  if [[ ! -s "$port_file" ]]; then
    echo "mock server did not start" >&2
    tail -40 "$server_log" >&2 || true
    exit 1
  fi
  printf 'http://127.0.0.1:%s/v1' "$(cat "$port_file")"
}

json_assert_path() {
  local json_file="$1"
  local kind="$2"
  local want_dir="$3"
  local want_rung="$4"
  local want_source="$5"
  python3 - "$json_file" "$kind" "$want_dir" "$want_rung" "$want_source" <<'PY'
import json
import sys

path, kind, want_dir, want_rung, want_source = sys.argv[1:]
with open(path) as f:
    data = json.load(f)
entry = data["paths"][kind]
assert entry["dir"] == want_dir, (kind, entry["dir"], want_dir)
assert entry["rung"] == want_rung, (kind, entry["rung"], want_rung)
assert entry["source"] == want_source, (kind, entry["source"], want_source)
PY
}

json_assert_agent_path() {
  local json_file="$1"
  local kind_key="$2"
  local want="$3"
  python3 - "$json_file" "$kind_key" "$want" <<'PY'
import json
import sys

path, kind_key, want = sys.argv[1:]
with open(path) as f:
    data = json.load(f)
assert str(data.get("schema_version")) == "4", data.get("schema_version")
got = data["paths"][kind_key]
assert got == want, (kind_key, got, want)
PY
}

json_assert_legacy_doctor() {
  local json_file="$1"
  python3 - "$json_file" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    data = json.load(f)
assert data.get("credentials_location") == "legacy config path", data.get("credentials_location")
locations = data.get("credentials_locations") or []
assert any("config.toml" in item for item in locations), locations
PY
}

json_assert_consolidated_doctor() {
  local json_file="$1"
  python3 - "$json_file" <<'PY'
import json
import sys

with open(sys.argv[1]) as f:
    data = json.load(f)
assert data.get("credentials_location") == "credentials file", data.get("credentials_location")
assert "credentials_location_warning" not in data, data.get("credentials_location_warning")
locations = data.get("credentials_locations") or []
assert locations == ["credentials file"], locations
PY
}

assert_no_config_secrets() {
  local config_path="$1"
  python3 - "$config_path" <<'PY'
import re
import sys

secret_keys = {
    "auth_header",
    "access_token",
    "refresh_token",
    "client_secret",
    "auth_api_key",
    "auth_client_secret",
    "auth_session_cookie",
    "auth_optional_token",
    "auth_bot_token",
    "auth_user_token",
    "press_golden_api_key",
}
key_pattern = re.compile(r"^(" + "|".join(re.escape(k) for k in secret_keys) + r")\s*=")
present = []
with open(sys.argv[1], "r") as f:
    for line in f:
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        m = key_pattern.match(stripped)
        if m:
            present.append(m.group(1))
assert not present, sorted(present)
PY
}

assert_credentials_mode() {
  local path="$1"
  python3 - "$path" <<'PY'
import os
import stat
import sys

mode = stat.S_IMODE(os.stat(sys.argv[1]).st_mode)
assert mode == 0o600, oct(mode)
PY
}

log "building ./printing-press"
go build -o ./printing-press ./cmd/printing-press

BASE_URL="$(write_mock_server)"
CLI_DIR="$SCRATCH/printing-press-golden"
CLI_DIR_2="$SCRATCH/printing-press-golden-2"

log "fresh-printing golden API fixture with validation gates"
./printing-press generate --spec "$SPEC" --output "$CLI_DIR" --force --spec-url "file://$SPEC"

log "building generated CLI and MCP binaries"
(cd "$CLI_DIR" && go build -o "./$APP" "./cmd/$APP" && go build -o "./$MCP_APP" "./cmd/$MCP_APP")
BIN="$CLI_DIR/$APP"

log "smoking help, version, doctor"
SMOKE_HOME="$SCRATCH/smoke-home"
mkdir -p "$SMOKE_HOME"
run_cli "$SMOKE_HOME" "$BASE_URL" "$BIN" --help >/dev/null
run_cli "$SMOKE_HOME" "$BASE_URL" "$BIN" version >/dev/null
run_cli "$SMOKE_HOME" "$BASE_URL" "$BIN" doctor --json >/dev/null

log "running verify with poisoned operator relocation env"
POISON="$SCRATCH/poisoned-data"
if ! env "${PREFIX}_DATA_DIR=$POISON" ./printing-press verify --dir "$CLI_DIR" --spec "$SPEC" --cleanup --json >"$SCRATCH/verify.json" 2>"$SCRATCH/verify.stderr"; then
  cat "$SCRATCH/verify.stderr" >&2
  cat "$SCRATCH/verify.json" >&2
  exit 1
fi
if [[ -e "$POISON" ]] && [[ -n "$(find "$POISON" -mindepth 1 -print -quit 2>/dev/null)" ]]; then
  echo "poisoned ${PREFIX}_DATA_DIR was used by verify subprocesses: $POISON" >&2
  exit 1
fi

log "simulating legacy install and B-to-N credential consolidation"
LEGACY_HOME="$SCRATCH/legacy-home"
LEGACY_CONFIG_DIR="$LEGACY_HOME/.config/$APP"
LEGACY_DATA_DIR="$LEGACY_HOME/.local/share/$APP"
mkdir -p "$LEGACY_CONFIG_DIR" "$LEGACY_DATA_DIR"
cat >"$LEGACY_CONFIG_DIR/config.toml" <<EOF_CONFIG
base_url = "$BASE_URL"
press_golden_api_key = "legacy-token"
EOF_CONFIG
: >"$LEGACY_DATA_DIR/data.db"

run_clean "$LEGACY_HOME" "$BIN" currencies --json >/dev/null
run_clean "$LEGACY_HOME" "$BIN" doctor --json >"$SCRATCH/legacy-doctor.json"
json_assert_legacy_doctor "$SCRATCH/legacy-doctor.json"

run_clean "$LEGACY_HOME" "$BIN" auth set-token migrated-token >/dev/null
CREDENTIALS="$LEGACY_DATA_DIR/credentials.toml"
test -f "$CREDENTIALS"
assert_credentials_mode "$CREDENTIALS"
assert_no_config_secrets "$LEGACY_CONFIG_DIR/config.toml"
run_clean "$LEGACY_HOME" "$BIN" doctor --json >"$SCRATCH/consolidated-doctor.json"
json_assert_consolidated_doctor "$SCRATCH/consolidated-doctor.json"

log "checking relocation matrix and agent-context paths"
DEFAULT_HOME="$SCRATCH/default-home"
mkdir -p "$DEFAULT_HOME"
run_cli "$DEFAULT_HOME" "$BASE_URL" "$BIN" doctor --json >"$SCRATCH/default-doctor.json"
json_assert_path "$SCRATCH/default-doctor.json" "data" "$DEFAULT_HOME/.local/share/$APP" "platform-default" "platform-default"

HOME_ROOT="$SCRATCH/relocated-root"
env -i PATH="$PATH" HOME="$SCRATCH/home-env-home" "${AUTH_ENV}=token" "${PREFIX}_BASE_URL=$BASE_URL" "${PREFIX}_HOME=$HOME_ROOT" "$BIN" doctor --json >"$SCRATCH/home-env-doctor.json"
json_assert_path "$SCRATCH/home-env-doctor.json" "config" "$HOME_ROOT/config" "home-env" "${PREFIX}_HOME"
json_assert_path "$SCRATCH/home-env-doctor.json" "data" "$HOME_ROOT/data" "home-env" "${PREFIX}_HOME"
env -i PATH="$PATH" HOME="$SCRATCH/home-env-home" "${AUTH_ENV}=token" "${PREFIX}_BASE_URL=$BASE_URL" "${PREFIX}_HOME=$HOME_ROOT" "$BIN" agent-context >"$SCRATCH/home-env-agent-context.json"
json_assert_agent_path "$SCRATCH/home-env-agent-context.json" "data_dir" "$HOME_ROOT/data"

DATA_OVERRIDE="$SCRATCH/data-override"
env -i PATH="$PATH" HOME="$SCRATCH/per-kind-home" "${AUTH_ENV}=token" "${PREFIX}_BASE_URL=$BASE_URL" "${PREFIX}_HOME=$HOME_ROOT" "${PREFIX}_DATA_DIR=$DATA_OVERRIDE" "$BIN" doctor --json >"$SCRATCH/per-kind-doctor.json"
json_assert_path "$SCRATCH/per-kind-doctor.json" "data" "$DATA_OVERRIDE" "per-kind-env" "${PREFIX}_DATA_DIR"

FLAG_HOME="$SCRATCH/flag-home"
run_cli "$SCRATCH/flag-base-home" "$BASE_URL" "$BIN" --home "$FLAG_HOME" doctor --json >"$SCRATCH/flag-home-doctor.json"
json_assert_path "$SCRATCH/flag-home-doctor.json" "state" "$FLAG_HOME/state" "--home" "--home"

log "checking generated MCP package under relocated home"
(cd "$CLI_DIR" && env "${PREFIX}_HOME=$HOME_ROOT" go test ./internal/mcp)

log "smoking claimed output dir variant"
./printing-press generate --spec "$SPEC" --output "$CLI_DIR_2" --force --spec-url "file://$SPEC" --validate=false
cp "$CLI_DIR/go.mod" "$CLI_DIR_2/go.mod"
cp "$CLI_DIR/go.sum" "$CLI_DIR_2/go.sum"
(cd "$CLI_DIR_2" && go build -o "./$APP" "./cmd/$APP")
BIN2="$CLI_DIR_2/$APP"
VARIANT_HOME="$SCRATCH/variant-home"
mkdir -p "$VARIANT_HOME"
run_cli "$VARIANT_HOME" "$BASE_URL" "$BIN2" --help >/dev/null
run_cli "$VARIANT_HOME" "$BASE_URL" "$BIN2" version >/dev/null
run_cli "$VARIANT_HOME" "$BASE_URL" "$BIN2" doctor --json >/dev/null

log "PASS paths smoke"
