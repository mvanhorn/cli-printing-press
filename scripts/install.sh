#!/usr/bin/env bash
set -euo pipefail
shopt -s lastpipe 2>/dev/null || true
umask 022

# CLI Printing Press installer
#
# One-line install:
#   curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash
#
# Options:
#   --cli-only          Install or update only the Go generator binary.
#   --skills-only       Install or refresh only the Claude Code skills.
#   -a, --agent NAME    Install skills for an agent supported by `skills`.
#                       May be repeated. Defaults to claude-code.
#   --dry-run           Print commands without executing them.
#   --quiet             Suppress non-error output.
#   --no-gum            Disable gum formatting even when gum is installed.
#   -h, --help          Show help.

readonly PROJECT_NAME="CLI Printing Press"
readonly BINARY_NAME="cli-printing-press"
readonly GO_MIN_VERSION="1.26.3"
readonly GO_INSTALL_TARGET="github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest"
readonly SKILL_SOURCE="mvanhorn/cli-printing-press/skills"
readonly SKILLS_PACKAGE="skills@latest"
readonly DEFAULT_AGENT="claude-code"

INSTALL_CLI=1
INSTALL_SKILLS=1
DRY_RUN=0
QUIET=0
NO_GUM=0
AGENTS=()
PROXY_ARGS=()
TMP_DIR=""
LOCK_DIR=""
CLI_STATUS="skipped"
SKILLS_STATUS="skipped"

cleanup() {
  if [[ -n "${LOCK_DIR:-}" && -d "$LOCK_DIR" ]]; then
    rm -f "$LOCK_DIR/pid" 2>/dev/null || true
    rmdir "$LOCK_DIR" 2>/dev/null || true
  fi
  if [[ -n "${TMP_DIR:-}" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

usage() {
  cat <<'USAGE'
CLI Printing Press installer

Usage:
  bash scripts/install.sh [options]
  curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install.sh | bash -s -- --cli-only

Options:
  --cli-only          Install or update only the Go generator binary.
  --skills-only       Install or refresh only the Claude Code skills.
  -a, --agent NAME    Install skills for an agent supported by `skills`.
                       May be repeated. Defaults to claude-code.
  --dry-run           Print commands without executing them.
  --quiet             Suppress non-error output.
  --no-gum            Disable gum formatting even when gum is installed.
  -h, --help          Show this help.
USAGE
}

for arg in "$@"; do
  case "$arg" in
    --cli-only)
      INSTALL_SKILLS=0
      ;;
    --skills-only)
      INSTALL_CLI=0
      ;;
    --dry-run)
      DRY_RUN=1
      ;;
    --quiet)
      QUIET=1
      ;;
    --no-gum)
      NO_GUM=1
      ;;
    --agent=*)
      AGENTS+=("${arg#--agent=}")
      ;;
    -a=*)
      AGENTS+=("${arg#-a=}")
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      # Handled below so "-a value" and "--agent value" can consume the next arg.
      ;;
  esac
done

# Re-parse options that consume a following value.
if (($# > 0)); then
  while (($# > 0)); do
    case "$1" in
      -a|--agent)
        shift
        if (($# == 0)); then
          echo "error: --agent requires a value" >&2
          exit 1
        fi
        AGENTS+=("$1")
        ;;
      --cli-only|--skills-only|--dry-run|--quiet|--no-gum|-h|--help|--agent=*|-a=*)
        ;;
      *)
        echo "error: unknown option: $1" >&2
        exit 1
        ;;
    esac
    shift || true
  done
fi

if [[ "$INSTALL_CLI" -eq 0 && "$INSTALL_SKILLS" -eq 0 ]]; then
  echo "error: choose only one of --cli-only or --skills-only" >&2
  exit 1
fi

if [[ "${#AGENTS[@]}" -eq 0 ]]; then
  AGENTS=("$DEFAULT_AGENT")
fi

HAS_GUM=0
if command -v gum >/dev/null 2>&1 && [[ -t 1 && "$NO_GUM" -eq 0 ]]; then
  HAS_GUM=1
fi

info() {
  [[ "$QUIET" -eq 1 ]] && return 0
  if [[ "$HAS_GUM" -eq 1 ]]; then
    gum style --foreground 39 "-> $*"
  else
    printf '\033[0;34m->\033[0m %s\n' "$*"
  fi
}

ok() {
  [[ "$QUIET" -eq 1 ]] && return 0
  if [[ "$HAS_GUM" -eq 1 ]]; then
    gum style --foreground 42 "✓ $*"
  else
    printf '\033[0;32m✓\033[0m %s\n' "$*"
  fi
}

warn() {
  [[ "$QUIET" -eq 1 ]] && return 0
  if [[ "$HAS_GUM" -eq 1 ]]; then
    gum style --foreground 214 "! $*"
  else
    printf '\033[1;33m!\033[0m %s\n' "$*"
  fi
}

err() {
  if [[ "$HAS_GUM" -eq 1 ]]; then
    gum style --foreground 196 "✗ $*" >&2
  else
    printf '\033[0;31m✗\033[0m %s\n' "$*" >&2
  fi
}

strip_ansi() {
  sed -E $'s/\x1B\\[[0-9;]*[A-Za-z]//g'
}

draw_box() {
  local color="$1"
  shift
  local lines=("$@")
  local width=0
  local line clean len
  for line in "${lines[@]}"; do
    clean="$(printf '%s' "$line" | strip_ansi)"
    len=${#clean}
    ((len > width)) && width=$len
  done
  ((width < 24)) && width=24
  [[ "$QUIET" -eq 1 ]] && return 0
  if [[ "$HAS_GUM" -eq 1 ]]; then
    printf '%s\n' "${lines[@]}" | gum style --border double --border-foreground "$color" --padding "0 1"
    return 0
  fi
  printf '╔'
  printf '═%.0s' $(seq 1 $((width + 2)))
  printf '╗\n'
  for line in "${lines[@]}"; do
    clean="$(printf '%s' "$line" | strip_ansi)"
    printf '║ %s%*s ║\n' "$line" $((width - ${#clean})) ''
  done
  printf '╚'
  printf '═%.0s' $(seq 1 $((width + 2)))
  printf '╝\n'
}

run_with_spinner() {
  local title="$1"
  shift
  if [[ "$DRY_RUN" -eq 1 ]]; then
    local quoted=()
    local arg
    for arg in "$@"; do
      printf -v arg '%q' "$arg"
      quoted+=("$arg")
    done
    info "[dry-run] ${quoted[*]}"
    return 0
  fi
  if [[ "$HAS_GUM" -eq 1 && "$QUIET" -eq 0 ]]; then
    gum spin --spinner dot --title "$title" -- "$@"
  else
    info "$title"
    "$@"
  fi
}

setup_proxy() {
  PROXY_ARGS=()
  if [[ -n "${HTTPS_PROXY:-}" ]]; then
    PROXY_ARGS=(--proxy "$HTTPS_PROXY")
  elif [[ -n "${HTTP_PROXY:-}" ]]; then
    PROXY_ARGS=(--proxy "$HTTP_PROXY")
  fi
}

detect_platform() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="x86_64" ;;
    arm64|aarch64) arch="aarch64" ;;
  esac
  case "$os" in
    darwin|linux) ;;
    *)
      err "Unsupported OS: $os"
      exit 1
      ;;
  esac
  case "$arch" in
    x86_64|aarch64) ;;
    *)
      err "Unsupported architecture: $arch"
      exit 1
      ;;
  esac
  if [[ "$os" == "linux" ]] && grep -qi microsoft /proc/version 2>/dev/null; then
    warn "WSL detected; continuing with Linux install."
  fi
}

check_disk_space() {
  local path="${TMPDIR:-/tmp}"
  local available
  available="$(df -Pk "$path" | awk 'NR==2 {print $4}')"
  if [[ -n "$available" && "$available" -lt 10240 ]]; then
    err "At least 10MB of free space is required in $path."
    exit 1
  fi
}

check_write_permissions() {
  local probe
  probe="$(mktemp "${TMPDIR:-/tmp}/cli-printing-press-install.XXXXXX")"
  rm -f "$probe"
}

check_network() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    return 0
  fi
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 5 "${PROXY_ARGS[@]}" https://github.com >/dev/null || {
      err "Network check failed: could not reach github.com."
      exit 1
    }
    if [[ "$INSTALL_SKILLS" -eq 1 ]]; then
      curl -fsSL --connect-timeout 5 "${PROXY_ARGS[@]}" https://registry.npmjs.org/skills >/dev/null || {
        err "Network check failed: could not reach registry.npmjs.org."
        exit 1
      }
    fi
  fi
}

acquire_lock() {
  local lock_base="${XDG_STATE_HOME:-$HOME/.local/state}/cli-printing-press"
  mkdir -p "$lock_base"
  LOCK_DIR="$lock_base/install.lock"
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$LOCK_DIR/pid"
    return 0
  fi
  if [[ -f "$LOCK_DIR/pid" ]]; then
    local pid
    pid="$(cat "$LOCK_DIR/pid" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && ! kill -0 "$pid" 2>/dev/null; then
      warn "Removing stale installer lock."
      rm -rf "$LOCK_DIR"
      mkdir "$LOCK_DIR"
      printf '%s\n' "$$" > "$LOCK_DIR/pid"
      return 0
    fi
  fi
  err "Another CLI Printing Press installer appears to be running."
  exit 1
}

parse_go_version() {
  sed -nE 's/^go version go([0-9]+(\.[0-9]+){1,2}).*/\1/p'
}

version_ge() {
  local left="$1"
  local right="$2"
  local IFS=.
  local -a l r
  read -r -a l <<< "$left"
  read -r -a r <<< "$right"
  local i lv rv
  for i in 0 1 2; do
    lv="${l[$i]:-0}"
    rv="${r[$i]:-0}"
    if ((10#$lv > 10#$rv)); then return 0; fi
    if ((10#$lv < 10#$rv)); then return 1; fi
  done
  return 0
}

check_go() {
  if ! command -v go >/dev/null 2>&1; then
    err "Go $GO_MIN_VERSION or newer is required."
    err "Install Go from https://go.dev/dl/, then re-run this installer."
    exit 1
  fi
  local version
  version="$(go version | parse_go_version)"
  if [[ -z "$version" ]]; then
    err "Could not determine the installed Go version. Ensure Go $GO_MIN_VERSION or newer is installed."
    exit 1
  fi
  if ! version_ge "$version" "$GO_MIN_VERSION"; then
    err "Go $GO_MIN_VERSION or newer is required; found Go $version."
    err "Install Go from https://go.dev/dl/, then re-run this installer."
    exit 1
  fi
}

check_node() {
  if ! command -v npm >/dev/null 2>&1 || ! command -v npx >/dev/null 2>&1; then
    err "Node/npm is required for skill installation."
    err "Install Node.js from https://nodejs.org/, then re-run this installer."
    exit 1
  fi
}

installed_cli_version() {
  if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    "$BINARY_NAME" --version 2>/dev/null || true
  fi
}

install_cli() {
  local current
  current="$(installed_cli_version)"
  if [[ -n "$current" ]]; then
    info "Existing $BINARY_NAME detected: $current"
  fi
  run_with_spinner "Installing $BINARY_NAME with go install" go install "$GO_INSTALL_TARGET"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    CLI_STATUS="planned"
  else
    CLI_STATUS="installed"
    ok "Installed $BINARY_NAME"
  fi
}

install_skills() {
  local cmd=(npx -y "$SKILLS_PACKAGE" add "$SKILL_SOURCE" --skill "*" -g -y)
  local agent
  for agent in "${AGENTS[@]}"; do
    cmd+=(-a "$agent")
  done
  run_with_spinner "Installing Printing Press skills" "${cmd[@]}"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    SKILLS_STATUS="planned"
  else
    SKILLS_STATUS="installed"
    ok "Installed Printing Press skills for ${AGENTS[*]}"
  fi
}

preflight_checks() {
  info "Running preflight checks"
  detect_platform
  setup_proxy
  check_disk_space
  check_write_permissions
  check_network
  if [[ "$INSTALL_CLI" -eq 1 ]]; then
    check_go
  fi
  if [[ "$INSTALL_SKILLS" -eq 1 ]]; then
    check_node
  fi
}

print_header() {
  if [[ "$QUIET" -eq 1 ]]; then
    return 0
  fi
  if [[ "$HAS_GUM" -eq 1 ]]; then
    gum style \
      --border normal --border-foreground 39 \
      --padding "0 1" --margin "1 0" \
      "$(gum style --foreground 42 --bold "$PROJECT_NAME installer")" \
      "$(gum style --foreground 245 "Install the generator binary and Claude Code skills")"
  else
    printf '\033[1;32m%s installer\033[0m\n' "$PROJECT_NAME"
    printf '\033[0;90mInstall the generator binary and Claude Code skills\033[0m\n\n'
  fi
}

print_summary() {
  local lines=()
  lines+=("CLI: $CLI_STATUS")
  lines+=("Skills: $SKILLS_STATUS")
  if [[ "$INSTALL_SKILLS" -eq 1 ]]; then
    lines+=("Agents: ${AGENTS[*]}")
    lines+=("Restart Claude Code so refreshed skills are loaded.")
  fi
  draw_box 42 "${lines[@]}"
}

main() {
  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/cli-printing-press-install.XXXXXX")"
  print_header
  acquire_lock
  preflight_checks
  if [[ "$INSTALL_CLI" -eq 1 ]]; then
    install_cli
  fi
  if [[ "$INSTALL_SKILLS" -eq 1 ]]; then
    install_skills
  fi
  print_summary
}

main "$@"
