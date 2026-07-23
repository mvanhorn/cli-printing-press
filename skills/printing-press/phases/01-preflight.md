## What Changed In v2

The old skill inflated the path to ship:
- too many mandatory research documents before code existed
- too many separate late-stage validation phases after code existed
- too many chances to discover obvious failures late

This version uses one lean loop:
1. Resolve the spec and write one research brief
2. Generate
3. Build the highest-value gaps
4. Run one shipcheck block
5. Optionally run live API smoke tests

Artifacts are still written, but only the ones that materially help the next step.

## Preflight

**This section MUST run before any user-facing prompt — including the [Orientation & Briefing](../SKILL.md#orientation--briefing) flow in the router.** A missing binary or available upgrade is information the user needs *before* they commit to an API. Do not invoke `AskUserQuestion`, print the orientation prose, or otherwise engage the user until preflight has completed and any signals from `../references/setup-checks.md` have been handled.

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 4.0.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

_press_repo=false
if [ -d "$_scope_dir/cmd/cli-printing-press" ] && [ -f "$_scope_dir/go.mod" ]; then
  _press_repo=true
fi

_resolve_press_bin() {
  if command -v cli-printing-press >/dev/null 2>&1; then
    command -v cli-printing-press
    return 0
  fi
  if command -v printing-press >/dev/null 2>&1 && printing-press version --json >/dev/null 2>&1; then
    command -v printing-press
    return 0
  fi
  return 1
}

# Strict-older semver compare on the first three components. Pre-release
# suffixes collapse to their GA counterpart (acceptable: we ship no pre-release
# tags).
_semver_lt() {
  awk -v a="$1" -v b="$2" 'BEGIN {
    split(a, x, ".")
    split(b, y, ".")
    for (i = 1; i <= 3; i++) {
      if ((x[i] + 0) < (y[i] + 0)) exit 0
      if ((x[i] + 0) > (y[i] + 0)) exit 1
    }
    exit 1
  }'
}

_source_press_version() {
  sed -nE 's/^var[[:space:]]+Version[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' \
    "$_scope_dir/internal/version/version.go" 2>/dev/null | head -n 1
}

_rebuild_local_press_bin_if_stale() {
  if [ "$_press_repo" != "true" ] || [ ! -x "$_scope_dir/cli-printing-press" ]; then
    return 0
  fi

  _local_v="$("$_scope_dir/cli-printing-press" version --json 2>/dev/null | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')"
  _source_v="$(_source_press_version)"
  if [ -z "$_local_v" ] || [ -z "$_source_v" ] || ! _semver_lt "$_local_v" "$_source_v"; then
    return 0
  fi

  echo ""
  echo "[local-binary-stale] local build v$_local_v is older than source v$_source_v"
  if ! command -v go >/dev/null 2>&1; then
    echo "[setup-error] local cli-printing-press binary is stale and Go is not on PATH, so it cannot be rebuilt."
    return 1 2>/dev/null || exit 1
  fi

  if (cd "$_scope_dir" && go build -o ./cli-printing-press ./cmd/cli-printing-press); then
    echo "[local-binary-rebuilt] rebuilt $_scope_dir/cli-printing-press"
    echo ""
  else
    echo "[setup-error] local cli-printing-press binary is stale and rebuild failed."
    return 1 2>/dev/null || exit 1
  fi
}

# Prefer local build when running from inside the printing-press repo.
# Lefthook may keep ./cli-printing-press current, but hooks can be absent or
# disabled. Compare against the checked-out source version before trusting it.
_rebuild_local_press_bin_if_stale || { return 1 2>/dev/null || exit 1; }
if [ "$_press_repo" = "true" ] && [ -x "$_scope_dir/cli-printing-press" ]; then
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/cli-printing-press"
elif ! _resolve_press_bin >/dev/null; then
  # Augment PATH if the binary is in ~/go/bin but not on the user's interactive PATH.
  if [ -x "$HOME/go/bin/cli-printing-press" ]; then
    export PATH="$HOME/go/bin:$PATH"
  elif [ -x "$HOME/go/bin/printing-press" ] && "$HOME/go/bin/printing-press" version --json >/dev/null 2>&1; then
    export PATH="$HOME/go/bin:$PATH"
  else
    # Refuse: the cli-printing-press binary is required and we will not auto-install
    # it. The README's install flow is the source of truth;
    # silent auto-install hides failure modes (network, wrong GOPATH) inside an
    # opaque skill invocation.
    echo ""
    echo "[setup-error] cli-printing-press binary not found."
    echo ""
    if command -v go >/dev/null 2>&1; then
      echo "Install it in your terminal:"
      echo "  go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest"
    else
      echo "Go 1.26.5 or newer is also not installed. Install Go from https://go.dev/dl/, then:"
      echo "  go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest"
    fi
    echo ""
    echo "Verify with: cli-printing-press --version"
    echo "Then re-run /printing-press."
    return 1 2>/dev/null || exit 1
  fi
fi

# Verify the Go toolchain is on PATH. Generation runs Go-based quality gates
# (go mod tidy, go vet, etc.) after writing thousands of lines of scaffolding,
# so a missing `go` only surfaces 5+ minutes in. Fail-fast costs one command -v
# call when Go is present and converts a late, opaque failure into a 30-second
# actionable abort.
if ! command -v go >/dev/null 2>&1; then
  echo ""
  echo "[setup-error] Go toolchain not found."
  echo ""
  echo "The Printing Press generator runs Go-based quality gates after generation."
  echo "Install Go 1.26.5 or newer from https://go.dev/dl/, then verify with:"
  echo "  go version"
  echo "Then re-run /printing-press."
  echo ""
  return 1 2>/dev/null || exit 1
fi

# Verify the installed Go tree can compile and run common standard library
# imports. A truncated Go extraction can leave the binary working enough for
# `go version` while missing packages under $GOROOT/src, which otherwise fails
# deep into generation during later Go quality gates.
_go_smoke_root="${PRINTING_PRESS_GO_SMOKE_DIR:-$HOME/.printing-press-smoke}"
if ! mkdir -p "$_go_smoke_root"; then
  echo ""
  echo "[setup-error] Unable to create Go smoke-test workspace at $_go_smoke_root."
  echo "Set PRINTING_PRESS_GO_SMOKE_DIR to a writable non-temp directory and retry."
  echo ""
  return 1 2>/dev/null || exit 1
fi
_go_smoke_dir="$(mktemp -d "$_go_smoke_root/stdlib.XXXXXX" 2>/dev/null || true)"
if [ -z "$_go_smoke_dir" ]; then
  echo ""
  echo "[setup-error] Unable to create Go smoke-test workspace under $_go_smoke_root."
  echo "Set PRINTING_PRESS_GO_SMOKE_DIR to a writable non-temp directory and retry."
  echo ""
  return 1 2>/dev/null || exit 1
fi
cat > "$_go_smoke_dir/go.mod" <<'__PP_GO_SMOKE_MOD__'
module pp-go-stdlib-smoke

go 1.20
__PP_GO_SMOKE_MOD__
cat > "$_go_smoke_dir/main.go" <<'__PP_GO_SMOKE_MAIN__'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

func main() {
	ctx := context.Background()
	payload, err := json.Marshal(map[string]string{"status": "ok"})
	if err != nil {
		panic(err)
	}
	if !regexp.MustCompile(`ok`).Match(payload) {
		panic("regexp mismatch")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		panic(err)
	}
	_, _ = fmt.Fprint(io.Discard, req.Method)
}
__PP_GO_SMOKE_MAIN__
if ! (cd "$_go_smoke_dir" && GOFLAGS= GOWORK=off go run . >/dev/null 2>"$_go_smoke_dir/error.log"); then
  _go_smoke_output="$(sed -n '1,12p' "$_go_smoke_dir/error.log" 2>/dev/null || true)"
  rm -rf "$_go_smoke_dir"
  echo ""
  echo "[setup-error] Go std library is incomplete (truncated or corrupted install)."
  echo "Reinstall Go from https://go.dev/dl/ and verify with the smoke test before retrying."
  if [ -n "$_go_smoke_output" ]; then
    echo ""
    echo "Go smoke test output:"
    printf '%s\n' "$_go_smoke_output"
  fi
  echo ""
  return 1 2>/dev/null || exit 1
fi
rm -rf "$_go_smoke_dir"

# Resolve and emit the absolute path the agent must use for every later
# `cli-printing-press` invocation. `export PATH` above only affects this one
# Bash tool call; subsequent calls open a fresh shell and resolve bare
# `cli-printing-press` against the user's default PATH. When a global is
# installed at a stale version, that silently shadows the local build the
# preflight just chose. Handing the agent an absolute path eliminates the
# shadow.
if [ "$_press_repo" = "true" ] && [ -x "$_scope_dir/cli-printing-press" ]; then
  PRINTING_PRESS_BIN="$_scope_dir/cli-printing-press"
else
  PRINTING_PRESS_BIN="$(_resolve_press_bin 2>/dev/null || true)"
fi
echo "PRINTING_PRESS_BIN=$PRINTING_PRESS_BIN"
echo "PRESS_REPO_MODE=$_press_repo"

_pp_go_version_norm() {
  printf '%s\n' "$1" | sed -nE 's/.*go([0-9]+)\.([0-9]+)(\.([0-9]+))?.*/\1.\2.\4/p' | awk -F. 'NF >= 2 { printf "%d.%d.%d\n", $1, $2, ($3 == "" ? 0 : $3) }'
}

_pp_check_go_currency() {
  if [ -z "${PRINTING_PRESS_BIN:-}" ] || ! command -v go >/dev/null 2>&1; then
    return 0
  fi

  _pp_go_installed="$(_pp_go_version_norm "$(go env GOVERSION 2>/dev/null)")"
  _pp_go_required="$(_pp_go_version_norm "$(go version "$PRINTING_PRESS_BIN" 2>/dev/null)")"
  if [ -z "$_pp_go_installed" ] || [ -z "$_pp_go_required" ] || ! _semver_lt "$_pp_go_installed" "$_pp_go_required"; then
    return 0
  fi

  echo ""
  if [ "${GOTOOLCHAIN:-auto}" = "local" ]; then
    echo "[setup-error] Go $_pp_go_required or newer is required by this cli-printing-press binary (installed: $_pp_go_installed)."
    echo "GOTOOLCHAIN=local disables automatic toolchain downloads, so later Go quality gates would fail."
    echo "Install Go $_pp_go_required or newer from https://go.dev/dl/, or unset GOTOOLCHAIN."
    echo ""
    return 1
  fi

  echo "[go-toolchain-old] Go $_pp_go_required or newer is required by this cli-printing-press binary (installed: $_pp_go_installed)."
  echo "PRESS_GO_INSTALLED=$_pp_go_installed"
  echo "PRESS_GO_REQUIRED=$_pp_go_required"
  echo "Default GOTOOLCHAIN behavior may download the required toolchain during Go commands."
  echo ""
  return 0
}
_pp_check_go_currency || { return 1 2>/dev/null || exit 1; }

# Shadow detector (advisory). When a local build is in use, surface any
# differing global so the user can see at a glance that the two binaries
# disagree. Detect-only: the absolute path emitted above is the one the
# agent will actually invoke; this warning does not change selection.
if [ "$_press_repo" = "true" ] && [ -x "$_scope_dir/cli-printing-press" ]; then
  _global_bin=""
  for _candidate in "$HOME/go/bin/cli-printing-press" "/usr/local/bin/cli-printing-press" "/opt/homebrew/bin/cli-printing-press" "$HOME/go/bin/printing-press" "/usr/local/bin/printing-press" "/opt/homebrew/bin/printing-press"; do
    if [ -x "$_candidate" ] && [ "$_candidate" != "$_scope_dir/cli-printing-press" ] && "$_candidate" version --json >/dev/null 2>&1; then
      _global_bin="$_candidate"
      break
    fi
  done
  if [ -n "$_global_bin" ]; then
    _local_v="$("$_scope_dir/cli-printing-press" version --json 2>/dev/null | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')"
    _global_v="$("$_global_bin" version --json 2>/dev/null | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')"
    if [ -n "$_local_v" ] && [ -n "$_global_v" ] && [ "$_local_v" != "$_global_v" ]; then
      echo ""
      echo "[binary-shadow] local build v$_local_v differs from global v$_global_v at $_global_bin"
      echo "PRESS_BIN_LOCAL_VERSION=$_local_v"
      echo "PRESS_BIN_GLOBAL_VERSION=$_global_v"
      echo "PRESS_BIN_GLOBAL_PATH=$_global_bin"
      echo ""
    fi
  fi
fi

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="${PRINTING_PRESS_HOME:-$HOME/printing-press}"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_CURRENT="$PRESS_RUNSTATE/current"

_pp_check_disk_space() {
  _pp_disk_warn_kb="${PRINTING_PRESS_DISK_WARN_KB:-3145728}"
  _pp_disk_fail_kb="${PRINTING_PRESS_DISK_FAIL_KB:-524288}"
  case "$_pp_disk_warn_kb$_pp_disk_fail_kb" in
    ""|*[!0-9]*) return 0 ;;
  esac

  _pp_disk_path="$PRESS_HOME"
  while [ ! -e "$_pp_disk_path" ] && [ "$_pp_disk_path" != "/" ]; do
    _pp_disk_path="$(dirname "$_pp_disk_path")"
  done

  _pp_disk_avail_kb="$(df -Pk "$_pp_disk_path" 2>/dev/null | awk 'NR == 2 { print $4; exit }')"
  case "$_pp_disk_avail_kb" in
    ""|*[!0-9]*) return 0 ;;
  esac

  if [ "$_pp_disk_avail_kb" -lt "$_pp_disk_fail_kb" ]; then
    echo ""
    echo "[setup-error] Critically low disk space on the Printing Press workspace volume."
    echo "PRESS_DISK_PATH=$_pp_disk_path"
    echo "PRESS_DISK_AVAIL_KB=$_pp_disk_avail_kb"
    echo "PRESS_DISK_FAIL_KB=$_pp_disk_fail_kb"
    echo "Free disk space or set PRINTING_PRESS_HOME to a volume with more room, then re-run /printing-press."
    echo ""
    return 1
  fi

  if [ "$_pp_disk_avail_kb" -lt "$_pp_disk_warn_kb" ]; then
    echo ""
    echo "[low-disk] Printing Press workspace volume is low on free space."
    echo "PRESS_DISK_PATH=$_pp_disk_path"
    echo "PRESS_DISK_AVAIL_KB=$_pp_disk_avail_kb"
    echo "PRESS_DISK_WARN_KB=$_pp_disk_warn_kb"
    echo "Generation may need several GiB for generated files, Go build cache, and module downloads."
    echo ""
  fi
}
_pp_check_disk_space || { return 1 2>/dev/null || exit 1; }

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY" "$PRESS_MANUSCRIPTS" "$PRESS_CURRENT"

# --- Latest-version advisory (fail-open) ---
# Repo checkouts track origin/main because their skills and local binary come
# from the checkout. Standalone installs track the latest released Go module.
PRESS_VERCHECK_FILE="$PRESS_HOME/.version-check"
PRESS_VERCHECK_TTL=86400
_now_ts=$(date +%s)

_should_check=true
if [ -f "$PRESS_VERCHECK_FILE" ] && [ -z "$PRESS_VERCHECK_FORCE" ]; then
  _last_ts=$(awk -F= '/^last_check=/{print $2}' "$PRESS_VERCHECK_FILE" 2>/dev/null)
  if [ -n "$_last_ts" ] && [ "$((_now_ts - _last_ts))" -lt "$PRESS_VERCHECK_TTL" ]; then
    _should_check=false
  fi
fi

if [ "$_press_repo" = "true" ]; then
  # Repo mode checks origin/main every run because the checkout and local build
  # move quickly; skipped_repo_main suppresses repeated prompts for one SHA.
  if git -C "$_scope_dir" remote get-url origin >/dev/null 2>&1 &&
     git -C "$_scope_dir" fetch --quiet origin main >/dev/null 2>&1; then
    _head_rev=$(git -C "$_scope_dir" rev-parse HEAD 2>/dev/null || true)
    _main_rev=$(git -C "$_scope_dir" rev-parse origin/main 2>/dev/null || true)
    _skipped_repo_main=""
    if [ -f "$PRESS_VERCHECK_FILE" ] && [ -z "$PRESS_VERCHECK_FORCE" ]; then
      _skipped_repo_main=$(awk -F= '/^skipped_repo_main=/{value=$2} END{print value}' "$PRESS_VERCHECK_FILE" 2>/dev/null)
    fi
    if [ -n "$_head_rev" ] && [ -n "$_main_rev" ] &&
       [ "$_head_rev" != "$_main_rev" ] &&
       [ "$_skipped_repo_main" != "$_main_rev" ] &&
       git -C "$_scope_dir" merge-base --is-ancestor "$_head_rev" "$_main_rev" 2>/dev/null; then
      echo ""
      echo "[repo-upgrade-available] origin/main has newer Printing Press changes"
      echo "PRESS_REPO_DIR=$_scope_dir"
      echo "PRESS_REPO_HEAD=$_head_rev"
      echo "PRESS_REPO_MAIN=$_main_rev"
      echo ""
    fi

    printf "last_check=%s\nlatest=%s\nmode=repo\nskipped_repo_main=%s\n" "$_now_ts" "${_main_rev:-unknown}" "$_skipped_repo_main" > "$PRESS_VERCHECK_FILE" 2>/dev/null || true
  fi
elif [ "$_should_check" = "true" ] && command -v go >/dev/null 2>&1; then
  _installed=$("$PRINTING_PRESS_BIN" version --json 2>/dev/null | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')
  _latest=""

  if [ -n "$_installed" ]; then
    _latest=$(go list -m -json github.com/mvanhorn/cli-printing-press/v4@latest 2>/dev/null | awk '
      /"Version":/ {
        version=$2
        gsub(/[",]/, "", version)
        sub(/^v/, "", version)
        print version
        exit
      }
    ')
  fi

  # Currency floor: the lowest release still considered safe to generate with,
  # published out-of-band so maintainers can raise it without a binary or skill
  # release. Fetched here (throttled by the TTL above) and cached for the
  # always-run enforcement gate below.
  _min_supported=""
  _min_reason=""
  if command -v curl >/dev/null 2>&1; then
    _floor_doc=$(curl -fsSL --max-time 5 \
      https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/supported-versions.txt 2>/dev/null || true)
    if [ -n "$_floor_doc" ]; then
      _min_supported=$(printf '%s\n' "$_floor_doc" | awk -F= '/^min_supported=/{print $2; exit}')
      _min_reason=$(printf '%s\n' "$_floor_doc" | sed -nE 's/^reason=//p' | head -n 1)
    fi
  fi

  if [ -n "$_installed" ] && [ -n "$_latest" ] && _semver_lt "$_installed" "$_latest"; then
    # Marker for the skill prose below to detect and offer an interactive upgrade.
    # The skill reads PRESS_UPGRADE_AVAILABLE / PRESS_UPGRADE_INSTALLED from this output.
    echo ""
    echo "[upgrade-available] printing-press v$_latest is available (you have v$_installed)"
    echo "PRESS_UPGRADE_AVAILABLE=$_latest"
    echo "PRESS_UPGRADE_INSTALLED=$_installed"
    echo ""
  fi

  printf "last_check=%s\nlatest=%s\nmode=standalone\nmin_supported=%s\nreason=%s\n" \
    "$_now_ts" "${_latest:-$_installed}" "$_min_supported" "$_min_reason" > "$PRESS_VERCHECK_FILE" 2>/dev/null || true
fi

# --- Currency-floor enforcement (standalone, every run, fail-open) ---
# The floor *fetch* above is throttled to once per TTL, but enforcement must run
# every invocation: a fresh cache must never let a stale binary keep generating
# CLIs with since-fixed bugs. Compare the always-fresh installed version against
# the cached floor; the network is never touched here. Only enforce a floor that
# is itself <= latest, so a typo'd or tampered floor above the newest release
# cannot brick every install.
if [ "$_press_repo" != "true" ] && [ -f "$PRESS_VERCHECK_FILE" ]; then
  _floor_min=$(awk -F= '/^min_supported=/{print $2; exit}' "$PRESS_VERCHECK_FILE" 2>/dev/null)
  _floor_latest=$(awk -F= '/^latest=/{print $2; exit}' "$PRESS_VERCHECK_FILE" 2>/dev/null)
  _floor_reason=$(sed -nE 's/^reason=//p' "$PRESS_VERCHECK_FILE" 2>/dev/null | head -n 1)
  _floor_installed=$("$PRINTING_PRESS_BIN" version --json 2>/dev/null | sed -nE 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')
  if [ -n "$_floor_min" ] && [ -n "$_floor_installed" ] && [ -n "$_floor_latest" ] &&
     _semver_lt "$_floor_installed" "$_floor_min" &&
     ! _semver_lt "$_floor_latest" "$_floor_min"; then
    echo ""
    echo "[upgrade-required] printing-press v$_floor_min is the minimum supported version (you have v$_floor_installed)"
    echo "PRESS_REQUIRED_MIN=$_floor_min"
    echo "PRESS_REQUIRED_INSTALLED=$_floor_installed"
    echo "PRESS_REQUIRED_REASON=$_floor_reason"
    echo ""
  fi
fi

# --- Browser-sniff backend advisory (fail-open, every-run) ---
# browser-use and agent-browser are the preferred Phase 1.7 browser-sniff
# backends. They are not hard requirements — vendor-spec, --spec, and --har
# runs never invoke them — but when discovery does need them, mid-flight
# install prompts are disruptive. Emit a marker every run so setup-checks.md
# can strongly offer install. No decline caching: a run that didn't need them
# yesterday may need them today, and the prompt cost is small.
_browser_use_missing=true
_agent_browser_missing=true
# Use `command -v` only. Do NOT use `uvx browser-use --help` as a fallback
# probe: when uvx exists but browser-use doesn't, that command silently
# downloads and caches the package, which would be an unconsented install.
# Downstream capture commands also invoke `browser-use` directly (not via
# uvx), so a uvx-cache-only state would lie to the detection.
if command -v browser-use >/dev/null 2>&1; then
  _browser_use_missing=false
fi
if command -v agent-browser >/dev/null 2>&1; then
  _agent_browser_missing=false
fi

if [ "$_browser_use_missing" = "true" ] || [ "$_agent_browser_missing" = "true" ]; then
  echo ""
  echo "[browser-tools-missing] one or more browser-sniff backends not installed"
  echo "PRESS_BROWSER_USE_MISSING=$_browser_use_missing"
  echo "PRESS_AGENT_BROWSER_MISSING=$_agent_browser_missing"
  echo ""
fi

# --- Codex mode detection (must run as part of setup, not a separate step) ---
# Codex mode: opt-in only. User must pass "codex" or "--codex" to enable.
if echo "$ARGUMENTS" | grep -qiE '(^| )(--?codex|codex)( |$)'; then
  CODEX_MODE=true
else
  CODEX_MODE=false
fi

# Environment guard: don't delegate if already inside a Codex sandbox
if [ "$CODEX_MODE" = "true" ]; then
  if [ -n "$CODEX_SANDBOX" ] || [ -n "$CODEX_SESSION_ID" ]; then
    CODEX_MODE=false
  fi
fi

# Health check: verify codex binary exists
if [ "$CODEX_MODE" = "true" ]; then
  if command -v codex >/dev/null 2>&1; then
    # Model and reasoning effort inherit from ~/.codex/config.toml. Do not pin -m / -c here.
    CODEX_MODEL=$(grep -E '^model[[:space:]]*=' ~/.codex/config.toml 2>/dev/null | head -1 | sed -E 's/^model[[:space:]]*=[[:space:]]*"?([^"]+)"?.*$/\1/')
    [ -z "$CODEX_MODEL" ] && CODEX_MODEL="codex default"
    echo "Codex mode enabled (model: $CODEX_MODEL). Code-writing tasks will be delegated to Codex."
  else
    echo "Codex CLI not found - running in standard mode."
    CODEX_MODE=false
  fi
fi

# Circuit breaker state
CODEX_CONSECUTIVE_FAILURES=0
```
<!-- PRESS_SETUP_CONTRACT_END -->

**MANDATORY: Read and apply [references/setup-checks.md](../references/setup-checks.md) immediately after the setup contract bash block runs, before any other action.** It handles the contract output signals: `[setup-error]` (refuse to run, surface the install instructions), optional `[local-binary-stale]` / `[local-binary-rebuilt]` repo-mode rebuild markers, `[go-toolchain-old]` / `[low-disk]` advisories, `[repo-upgrade-available]` (interactive `AskUserQuestion` prompt + optional repo pull), `PRESS_REPO_MODE=<true|false>` plus the targeted global open-agent-skills freshness check, the min-binary-version compatibility check (hard stop if binary is too old), `[upgrade-required]` (hard gate below the published currency floor — interactive upgrade-or-abort, no skip), `[upgrade-available]` (interactive `AskUserQuestion` prompt + optional standalone binary upgrade), `[browser-tools-missing]` (interactive `AskUserQuestion` prompt + optional install of browser-use and/or agent-browser), and the `PRINTING_PRESS_BIN=<abs-path>` marker plus optional `[binary-shadow]` warning (capture the path; use it for every subsequent generator invocation). Skipping the reference will cause the skill to proceed with a missing or out-of-date binary, run with stale global skill text when the session is managed by open-agent-skills, hit a mid-flight install prompt if browser-sniff is later needed, or invoke the wrong binary because a stale global or the public-library installer on `PATH` shadowed the local build. Do not skip.

**Absolute-path rule.** The preflight contract always emits `PRINTING_PRESS_BIN=<absolute path>` to stdout. Capture this value and substitute it (the resolved absolute path, not the literal `$PRINTING_PRESS_BIN` token) for every subsequent `cli-printing-press ...` invocation in this skill, references, and any sub-skill you delegate to. The `export PATH=...` line inside the contract only affects the single Bash tool call it runs in; later Bash tool calls open fresh shells and resolve bare `cli-printing-press` against the user's default `PATH`, where a stale globally-installed binary (`$HOME/go/bin/cli-printing-press`, Homebrew copy, etc.) will silently shadow the local build the preflight just chose. Bash code examples below are written `cli-printing-press generate ...` for readability — replace `cli-printing-press` with the captured absolute path each time you actually run one.

Only after preflight completes successfully (no `[setup-error]`; no `[upgrade-required]` left unresolved — the user either upgraded or the run was aborted; no global skill update that requires restart; any `[repo-upgrade-available]`, `[upgrade-available]`, or `[browser-tools-missing]` was offered to the user; `PRINTING_PRESS_BIN` is captured) should you proceed to [Orientation & Briefing](../SKILL.md#orientation--briefing) in the router.


Next: phases/02-run-initialization.md
