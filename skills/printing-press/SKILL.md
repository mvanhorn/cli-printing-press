---
name: printing-press
description: Generate a ship-ready CLI for an API with a lean research -> generate -> build -> shipcheck loop.
version: 2.0.0
min-binary-version: "4.0.0"
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - WebFetch
  - WebSearch
  - AskUserQuestion
  - Agent
---

# /printing-press

Generate the best useful CLI for an API without burning an hour on phase theater.

```bash
/printing-press Notion
/printing-press Discord codex
/printing-press --spec ./openapi.yaml
/printing-press --har ./capture.har --name MyAPI
/printing-press https://postman.com/explore
/printing-press https://postman.com
```

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

## Modes

### Default

Normal mode. Claude does research, generation orchestration, implementation, and verification.

### Codex Mode

If the arguments include `codex` or `--codex`, offload pure code-writing tasks to Codex CLI.

Use Codex for:
- writing store/data-layer code
- writing workflow commands
- fixing dead flags / dead code / path issues
- README cookbook edits

Keep on Claude:
- research and product positioning
- choosing which gaps matter
- verification results and ship decisions

If Codex fails 3 times in a row, stop delegating and finish locally.

### Polish Mode (Standalone Skill)

For second-pass improvements to an existing CLI, use the standalone polish skill:

```bash
/printing-press-polish redfin
```

See the `printing-press-polish` skill for details. It runs diagnostics, fixes verify failures, removes dead code, cleans up descriptions and README, and offers to publish.

## Rules

- **Do not ship a CLI that hasn't been behaviorally tested against real targets.** `go build` and `verify` pass-rate are structural signals, not correctness signals. Phase 5's mechanical test matrix runs every subcommand + `--json` + error paths; if that matrix was not executed, the CLI is not shippable. Quick Check is the floor; Full Dogfood is required when the user asks for thoroughness.
- **Bugs found during dogfood are fix-before-ship, not "file for v0.2".** If a 1-3 file edit resolves it, do it now. `ship-with-gaps` is deprecated as a default verdict (see Phase 4). Context is freshest in-session; a v0.2 backlog that may never be revisited ships known-broken CLIs.
- **Features approved in Phase 1.5 are shipping scope.** Do not downgrade a shipping-scope feature to a stub mid-build. If implementation becomes infeasible, return to Phase 1.5 with a revised manifest and get explicit re-approval.
- **Do not quote human-time estimates for sub-tasks** ("~15-30 min", "~1 hour", "quick fix") in `AskUserQuestion` options, phase descriptions, or reference docs. The agent does the work, not the user; agent-fabricated estimates are notoriously bad and train users to distrust the prompt. Describe scope instead (lines of code, files touched, relative size). The carve-outs are wall-clock estimates for genuinely time-bound things: the whole-CLI run (set the user's expectation up front — most CLIs take 30+ minutes), tool installs (`go install` takes ~10 seconds), and printing-press subcommands that do network-bound work (crowd-sniff scans npm + GitHub, ~5-10 minutes). Anything bounded by agent reasoning time is not time-bound — describe scope.
- **Use raw captures for contract research.** When reading official docs, auth/error/rate-limit pages, endpoint references, OpenAPI/Postman links, or source pages whose exact identifiers affect the generated CLI, read [references/fetch-docs.md](references/fetch-docs.md) and use its `fetch-docs.sh` helper. Reserve `WebFetch` for quick TL;DR reads where losing field-level details is acceptable.
- Optimize for time-to-ship, not time-to-document.
- Reuse prior research whenever it is already good enough.
- Do not split one idea across multiple mandatory artifacts.
- Durable files produced by this skill go under `$PRESS_RUNSTATE/` (working state) or `$PRESS_MANUSCRIPTS/` (archived). Short-lived command captures may use `/tmp/printing-press/` and must be removed after use.
- Do not create a separate narrative phase for dogfood, dead-code audit, runtime verification, and final score. Treat them as one shipcheck block.
- Run cheap, high-signal checks early.
- Fix blockers and high-leverage failures first.
- Reuse the same spec path across `generate`, `dogfood`, `verify`, and `scorecard`.
- YAML, JSON, local paths, and URLs are all valid spec inputs for the verification tools.
- Maximum 2 verification fix loops unless the user explicitly asks for more.

## Secret & PII Protection (Cardinal Rules)

**These rules are non-negotiable. They apply at ALL times during a run.**

API key **values**, token **values**, passwords, and session cookies must NEVER
appear in any artifact: source code, manuscripts, proofs, READMEs, HARs, or
anything committed to git. Env var **names** (e.g., `STEAM_API_KEY`) and
placeholders (e.g., `"your-key-here"`) are safe.

During Phase 5.6 (archiving) and before publishing, read and apply
[references/secret-protection.md](references/secret-protection.md) for:
- Exact-value scanning and auto-redaction of artifacts
- HAR auth stripping (headers, query strings, cookies)
- API key handling rules during the run
- Session state cleanup ordering

## Preflight

**This section MUST run before any user-facing prompt — including the Orientation and Briefing flow below.** A missing binary or available upgrade is information the user needs *before* they commit to an API. Do not invoke `AskUserQuestion`, print the orientation prose, or otherwise engage the user until preflight has completed and any signals from `references/setup-checks.md` have been handled.

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

**MANDATORY: Read and apply [references/setup-checks.md](references/setup-checks.md) immediately after the setup contract bash block runs, before any other action.** It handles the contract output signals: `[setup-error]` (refuse to run, surface the install instructions), optional `[local-binary-stale]` / `[local-binary-rebuilt]` repo-mode rebuild markers, `[go-toolchain-old]` / `[low-disk]` advisories, `[repo-upgrade-available]` (interactive `AskUserQuestion` prompt + optional repo pull), `PRESS_REPO_MODE=<true|false>` plus the targeted global open-agent-skills freshness check, the min-binary-version compatibility check (hard stop if binary is too old), `[upgrade-required]` (hard gate below the published currency floor — interactive upgrade-or-abort, no skip), `[upgrade-available]` (interactive `AskUserQuestion` prompt + optional standalone binary upgrade), `[browser-tools-missing]` (interactive `AskUserQuestion` prompt + optional install of browser-use and/or agent-browser), and the `PRINTING_PRESS_BIN=<abs-path>` marker plus optional `[binary-shadow]` warning (capture the path; use it for every subsequent generator invocation). Skipping the reference will cause the skill to proceed with a missing or out-of-date binary, run with stale global skill text when the session is managed by open-agent-skills, hit a mid-flight install prompt if browser-sniff is later needed, or invoke the wrong binary because a stale global or the public-library installer on `PATH` shadowed the local build. Do not skip.

**Absolute-path rule.** The preflight contract always emits `PRINTING_PRESS_BIN=<absolute path>` to stdout. Capture this value and substitute it (the resolved absolute path, not the literal `$PRINTING_PRESS_BIN` token) for every subsequent `cli-printing-press ...` invocation in this skill, references, and any sub-skill you delegate to. The `export PATH=...` line inside the contract only affects the single Bash tool call it runs in; later Bash tool calls open fresh shells and resolve bare `cli-printing-press` against the user's default `PATH`, where a stale globally-installed binary (`$HOME/go/bin/cli-printing-press`, Homebrew copy, etc.) will silently shadow the local build the preflight just chose. Bash code examples below are written `cli-printing-press generate ...` for readability — replace `cli-printing-press` with the captured absolute path each time you actually run one.

Only after preflight completes successfully (no `[setup-error]`; no `[upgrade-required]` left unresolved — the user either upgraded or the run was aborted; no global skill update that requires restart; any `[repo-upgrade-available]`, `[upgrade-available]`, or `[browser-tools-missing]` was offered to the user; `PRINTING_PRESS_BIN` is captured) should you proceed to the Orientation & Briefing section below.

## Orientation & Briefing

After preflight has completed, check whether the user provided arguments. Handle two cases:

### No Arguments: Orientation

If the user typed `/printing-press` with no arguments (no API name, no `--spec`, no `--har`, no URL), print an orientation and ask what they'd like to build:

> The Printing Press generates a fully functional CLI for any API. You give it an API name, a spec file, or a URL. It researches the landscape, catalogs every feature that exists in any competing tool, invents novel features of its own, then generates a Go CLI that matches and beats everything out there — with offline search, agent-native output, and a local SQLite data layer.
>
> By the end, you'll have a working CLI in `$PRESS_LIBRARY/` that you can use for yourself, ship on your own, or apply to add to the printing-press library.
>
> The process takes 30-60 minutes depending on API complexity. Simple APIs with official specs (Stripe, GitHub) are faster. Undocumented APIs that need discovery (ESPN, Domino's) take longer.

Print these example invocations as plain text BEFORE the `AskUserQuestion` call (so they appear as context above the question, not as competing menu options):

```
/printing-press Notion
/printing-press Discord codex
/printing-press --spec ./openapi.yaml
/printing-press --har ./capture.har --name MyAPI
/printing-press https://postman.com
```

Then ask via `AskUserQuestion`:

- **question:** `"What API would you like to build a CLI for?"`
- **header:** `"API target"`
- **multiSelect:** `false`
- **options:**
  1. **label:** `"Type it (recommended)"` — **description:** `"Provide an API name, URL, spec path, or HAR file via the 'Other' option below."`
  2. **label:** `"Browse existing CLIs first"` — **description:** `"Visit the public library to see what's already been printed before deciding what to build."`

**Do not add additional options** — no "Show me popular options", no pre-populated buttons for Notion / Stripe / GitHub / Linear / Discord. The example invocations above already cover the common shapes, and most popular APIs are already in the public library (offering to re-print them is noise). The two options above plus the automatic "Other" field is the entire interface.

If the user picks **"Type it (recommended)"**, they will provide their answer via the auto "Other" field. Set their input as the argument and proceed to the briefing below.

If the user picks **"Browse existing CLIs first"**, print the public library URL prominently and try to open it in the browser, then end the skill so the user can browse before deciding:

```bash
echo ""
echo "Public library: https://github.com/mvanhorn/printing-press-library"
echo "(If you have the Printing Press Library plugin, you can also run /ppl in Claude Code.)"
echo ""
command -v open >/dev/null 2>&1 && open https://github.com/mvanhorn/printing-press-library
```

After printing, end the skill cleanly. Do not proceed to briefing or research — the user is exploring, not building yet. They can re-invoke `/printing-press <api>` once they've decided.

### With Arguments: Briefing

When the user provided an argument (API name, `--spec`, `--har`, or URL), print a brief process overview. This sets expectations and collects any upfront context. (Preflight has already run at this point.)

Print as prose, matching the style of the example below:

> Very well. Setting the type for `<API>`.
>
> **Here is how this will proceed:**
> 1. I shall research `<API>` across the internet: official docs, community wrappers, competing CLIs, MCP servers, and npm/PyPI packages
> 2. I shall catalog every feature that exists in any tool, then devise novel features of my own that no existing tool offers
> 3. I shall present what I found and what I invented — you will have a chance to add your own ideas or adjust the plan before I build
> 4. I shall generate a Go CLI, build every feature from the plan, then verify quality through dogfood, runtime verification, and scoring
>
> **What you will have at the end:** A fully functional CLI at `$PRESS_LIBRARY/<api>` that you can use yourself, ship on your own, or apply to add to the printing-press library.
>
> **Time:** 30-60 minutes depending on API complexity.
>
> **Things that help if you have them:**
> - An API key (for live smoke testing at the end)
> - A logged-in browser session (for discovering authenticated endpoints)
> - A spec file or HAR capture (skips discovery)

If the user provided `--spec`, adapt: "You have provided a spec, so I shall skip discovery and proceed directly to analysis and generation. Should be faster."

If the user provided `--har`, adapt: "You have provided a HAR capture, so I shall generate a spec from your traffic and skip browser browser-sniffing."

Then ask via `AskUserQuestion`:

- **question:** `"Anything you want me to know before I begin? A vision for what this CLI should do, specific features you care about, or auth context I should have?"`
- **header:** `"Briefing"`
- **multiSelect:** `false`
- **options:**
  1. **label:** `"Let's go (recommended)"` — **description:** `"Start research now. I'll ask about API keys, browser auth, or other context when I need them."`
  2. **label:** `"I have context to share"` — **description:** `"Tell me your vision, specific features, or auth context (API key, logged-in browser session) before research starts."`

**Do not add additional options** — auth is already handled by Phase 0.5 (API Key Gate) and Phase 1.6 (Pre-Browser-Sniff Auth Intelligence) downstream. A user who wants to volunteer auth context can do so via option 2's free-text response. The two options above plus the automatic "Other" field is the entire interface.

If the user picks **"Let's go (recommended)"**, proceed to the Multi-Source Priority Gate (or, for single-source runs, directly to Phase 0).

If the user picks **"I have context to share"**, capture their free-text response as `USER_BRIEFING_CONTEXT`. The response may include:

- **Vision / specific features** — captured as-is. This context will be:
  - Added to the Phase 1 Research Brief under a `## User Vision` section
  - Used as a 4th self-brainstorm question in Phase 1.5c.5: "Based on the user's stated vision, what features directly serve their stated goals that the absorbed features don't cover?"
  - Referenced at the Phase Gate 1.5 absorb gate: "You mentioned [summary] at the start. Want to add more, or does the manifest already cover it?"
- **Auth context** — if the user mentions an API key, env var, or logged-in browser session, set the corresponding `AUTH_CONTEXT` fields so the API Key Gate (Phase 0.5) and Pre-Browser-Sniff Auth Intelligence (Phase 1.6) do not re-ask.

### Multi-Source Priority Gate

After the briefing question resolves, inspect the user's original argument AND any `USER_BRIEFING_CONTEXT` they provided. If together they name **two or more distinct services, sites, or APIs** (e.g., "Google Flights and Kayak", "Notion + Linear combo CLI", "flightgoat: Google Flights, Kayak.com/direct, and FlightAware"), this is a combo CLI and priority ordering MUST be confirmed before Phase 1 research.

**Why this gate exists:** Phase 1 research defaults to the first resolvable spec as the primary source. When the user listed services in a specific order, that order is their intent — but the generator's spec-first bias will silently invert it (picking a well-documented paid API over a free reverse-engineered one the user actually wanted as the headline feature). This has caused real user-visible failures where the CLI shipped with the wrong primary and required a paid API key for what the user intended as the free primary command.

**Parse the order from the prose.** Use the user's wording verbatim. Commas, "then", "and", explicit "primary/secondary", or numbered lists all signal ordering. If the user wrote "Google Flights, Kayak, FlightAware" — that is the order. Do not reorder by spec availability, tier, or ease of generation.

**Confirm via `AskUserQuestion`:**

> "You mentioned **<Source A>**, **<Source B>**, and **<Source C>**. I'll treat **<Source A>** as the primary — it gets the headline commands, the top of the README, and the first-run experience. Is that the right order?"

Options:
1. **Yes, that order is correct** — Proceed with `SOURCE_PRIORITY=[A, B, C]` captured to run state.
2. **Different order** — User provides the correct ordering; capture it.
3. **They're peers, no primary** — Rare; capture as equal weighting but warn the user that one will still lead the README.

Write the confirmed ordering to `$API_RUN_DIR/source-priority.json`:

```json
{
  "sources": ["google-flights", "kayak-direct", "flightaware"],
  "confirmed_at": "<ISO timestamp>",
  "raw_user_phrasing": "<verbatim text that established the order>"
}
```

**Phase 1 MUST consult this file.** When selecting a spec source, the primary source wins even if it has no spec and a later source has a clean OpenAPI. When the primary has no official spec, flag that openly in the brief under `## Source Priority` (see template below) and route to the browser-sniff/docs path for the primary — do not promote a secondary source just because its spec is cleaner.

**Economics check.** If the confirmed primary source is free (no API key required) AND the generator's default path would make the primary CLI commands require a paid key (because the auth applies broadly or because a paid secondary source is bleeding into the primary path), surface the tradeoff explicitly before generating:

> "The primary source (**<Source A>**) is free, but the default path would require a **<paid key>** for the headline commands because <reason>. Options: (1) keep primary free, gate only the secondary commands on the paid key; (2) require the paid key for everything; (3) drop the paid source."

Default to option 1 unless the user overrides. Record the decision in `source-priority.json` under `auth_scoping`.

**Single-source runs:** If only one service is named, skip this gate entirely — no ordering to confirm.

---

## Run Initialization

After you know `<api>` (from the Orientation & Briefing flow above; preflight already ran at the top), initialize the run-scoped artifact paths:

```bash
mkdir -p "$PRESS_RUNSTATE/runs"
RUN_ID=""
API_RUN_DIR=""
for attempt in 1 2 3 4 5; do
  RUN_SUFFIX="$(LC_ALL=C tr -dc 'a-f0-9' </dev/urandom 2>/dev/null | head -c 8 || true)"
  if [ -z "$RUN_SUFFIX" ]; then
    RUN_SUFFIX="pid$$-$attempt"
  fi
  CANDIDATE_RUN_ID="$(date +%Y%m%d-%H%M%S)-$RUN_SUFFIX"
  CANDIDATE_RUN_DIR="$PRESS_RUNSTATE/runs/$CANDIDATE_RUN_ID"
  if mkdir "$CANDIDATE_RUN_DIR" 2>/dev/null; then
    RUN_ID="$CANDIDATE_RUN_ID"
    API_RUN_DIR="$CANDIDATE_RUN_DIR"
    break
  fi
done
if [ -z "$RUN_ID" ]; then
  echo "could not allocate a unique run directory under $PRESS_RUNSTATE/runs" >&2
  exit 1
fi
RESEARCH_DIR="$API_RUN_DIR/research"
PROOFS_DIR="$API_RUN_DIR/proofs"
PIPELINE_DIR="$API_RUN_DIR/pipeline"
DISCOVERY_DIR="$API_RUN_DIR/discovery"
CLI_WORK_DIR="$API_RUN_DIR/working/<api>-pp-cli"
STAMP="$(date +%Y-%m-%d-%H%M%S)"

# Session state (live cookies, CSRF tokens captured during authenticated
# browser-sniff) lives OUTSIDE $API_RUN_DIR so the Phase 5.5 archive
# `cp -r "$DISCOVERY_DIR"` cannot pick it up. Containment by location, not by
# manual rm-before-archive.
#
# Base prefix is user-scoped (`printing-press-$(id -u)`) so that on a Linux
# host with a shared /tmp, the umask-077 subshell below does not lock the
# top-level `printing-press` directory to a single user. macOS already gives
# us a per-user $TMPDIR; the $(id -u) suffix keeps semantics identical there.
SESSION_BASE="${TMPDIR:-/tmp}/printing-press-$(id -u)"
SESSION_DIR="$SESSION_BASE/session/$RUN_ID"
SESSION_STATE_FILE="$SESSION_DIR/session-state.json"

mkdir -p "$RESEARCH_DIR" "$PROOFS_DIR" "$PIPELINE_DIR" "$CLI_WORK_DIR"
# Create $SESSION_DIR inside a subshell with a tight umask so it lands at 0700
# at creation, not after a follow-up chmod. The two-step `mkdir; chmod` form
# leaves a TOCTOU window where a concurrent process could open the directory
# (and any session-state.json written into it) while perms are still
# umask-derived (typically 0755 on Linux). The umask propagates to every
# directory `mkdir -p` creates; the user-scoped $SESSION_BASE above is what
# keeps that from blocking other users on the same host.
(umask 077 && mkdir -p "$SESSION_DIR")
STATE_FILE="$API_RUN_DIR/state.json"
```

Maintain a lightweight state file at `$STATE_FILE` so `/printing-press-score` can rediscover the current run. It should always contain:

```json
{
  "api_name": "<api>",
  "run_id": "$RUN_ID",
  "working_dir": "$CLI_WORK_DIR",
  "output_dir": "$CLI_WORK_DIR",
  "spec_path": "<absolute spec path if known>"
}
```

`run_id` is the unique value allocated above from the wall-clock stamp plus a short random suffix. `mkdir "$CANDIDATE_RUN_DIR"` is the collision guard: if another run already owns a candidate directory, allocate another ID instead of reusing the directory. Persisting this value in `state.json` makes the state file the source of truth for generate, dogfood acceptance, promote, `/printing-press-score`, and future state-loading consumers. Without `run_id` in either state or legacy path fallback, `cli-printing-press dogfood --live --write-acceptance` refuses to write the gate marker.

Do not create a `go.work` file in `$CLI_WORK_DIR`. Generated modules must build and test as standalone modules; a mismatched workspace `go` directive can break Go 1.25+ toolchains and lefthook checks. Editor/gopls workspace noise is cosmetic and must not be traded for broken `go build` or `go test`.

There are exactly three durable writable locations. Every generated artifact this
skill preserves goes to one of them:

- **`$PRESS_RUNSTATE/`** — mutable working state for the current run (research, proofs, pipeline artifacts, plans, intermediate docs)
- **`$PRESS_LIBRARY/`** — published CLIs (`<api-slug>/` subdirectories)
- **`$PRESS_MANUSCRIPTS/`** — archived run evidence (research, proofs, discovery)

Short-lived command captures may use `/tmp/printing-press/` with unique `mktemp`
paths and must be deleted after use.

Examples of the current naming/layout:
- `$PRESS_LIBRARY/notion/` — published CLI directory (keyed by API slug)
- `notion-pp-cli` — the binary name inside the directory
- `/printing-press emboss notion` — emboss accepts both slug and CLI name
- `discord-pp-cli/internal/store/store.go` — internal source paths still use CLI name
- `linear-pp-cli stale --days 30 --team ENG` — binary invocations use CLI name
- `github.com/mvanhorn/discord-pp-cli` — Go module paths use CLI name

## Outputs

Every run writes up to 5 concise artifacts under the current managed run and archives them to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/`:

1. `research/<stamp>-feat-<api>-pp-cli-brief.md`
2. `research/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`
3. `proofs/<stamp>-fix-<api>-pp-cli-build-log.md`
4. `proofs/<stamp>-fix-<api>-pp-cli-shipcheck.md`
5. `proofs/<stamp>-fix-<api>-pp-cli-live-smoke.md` (only if live testing runs)

These do not need to be 200+ lines. Keep them dense, evidence-backed, and directly useful.

## Phase 0: Resolve And Reuse

Before new research:

1. Resolve the spec source.

   **Local physical device detection.** If the user's target is a local Bluetooth/BLE-controlled physical device (for example an appliance, toy, light, sensor, exercise machine, lock, or other device controlled from a phone app over Bluetooth), do not route it through browser-sniff as the primary discovery path. Read and apply [references/device-sniff-ble.md](references/device-sniff-ble.md). Use `device-sniff ble` for normalized BLE evidence and `bluetooth-sniff` as the discoverable alias. Community libraries, docs, Android logs, Wireshark/nRF captures, and manual action journals are evidence inputs; they are not a reason to hardcode a vendor-specific generator path.

   **BLE mapping research gate.** A BLE scan/inspect/read/subscribe pass only discovers identity, services, characteristics, and telemetry candidates; it does not by itself discover what write payloads mean. Before generating callable control commands or running any live `write`, establish a command mapping from at least one concrete source: user-provided mapping, official docs, community protocol/library code, Android/iOS/Bluetooth logs, Wireshark/nRF captures, or an operator action journal that correlates a real user action with observed writes. If no mapping source is found, generate only read/status/capability metadata or stop and ask the user for mapping evidence. Do not invent mutating payloads or brute-force probe a physical device.

   **URL Detection** — If the argument contains `://`, it's a URL. Determine whether it's a spec or a website before proceeding.

   **Step 1: Content probe.** Fetch the URL with the raw docs helper from [references/fetch-docs.md](references/fetch-docs.md) and inspect the response status, `Content-Type`, and first few lines of the returned file:
   - Check the `Content-Type` header and the first few lines of the body.
   - If the fetch fails (timeout, 404, DNS error), record the exact status/error, then skip to Step 2 — treat it as a website.

   If the content starts with `openapi:`, `swagger:`, or is valid JSON containing an `"openapi"` or `"swagger"` key → it's a spec. Treat as `--spec` and proceed directly. No disambiguation needed.

   If the content is a HAR file (JSON with `"log"` and `"entries"` keys) → treat as `--har` and proceed directly.

   **Step 2: Disambiguation.** If the content is HTML or the probe failed, ask the user what they want. Extract the site name from the hostname (e.g., `postman.com` → "Postman", `app.linear.app` → "Linear"). Derive `<api>` from the site name using the same `cleanSpecName` normalization the generator uses.

   Use `AskUserQuestion` with:
   - **question:** `"What kind of CLI do you want for <SiteName>?"`
   - **header:** `"CLI target"`
   - **multiSelect:** `false`
   - **options:**
     1. **label:** `"<SiteName>'s official API"` — **description:** `"Build a CLI for <SiteName>'s documented API (e.g. REST endpoints, webhooks, OAuth)"`
     2. **label:** `"The <SiteName> website itself"` — **description:** `"Build from the website itself — I may open or attach to Chrome during generation to capture site traffic, then generate a lightweight CLI from replayable HTTP/HTML surfaces"`

   The user can also pick the automatic "Other" option to describe what they're after in free text.

   **Routing after disambiguation:**
   - "<SiteName>'s official API" → use `<api>` as the argument, proceed with normal discovery (Phase 1 research, then Phase 1.7 browser-sniff gate evaluates independently as usual)
   - "The <SiteName> website itself" → use `<api>` as the argument, set `BROWSER_SNIFF_TARGET_URL=<url>`. Proceed to Phase 1 research. When Phase 1.7 is reached, skip the browser-sniff gate decision and go directly to "If user approves browser-sniff" (the user already approved temporary browser discovery in Phase 0 — do not re-ask). Use `BROWSER_SNIFF_TARGET_URL` as the starting URL for browser capture. The printed CLI must still use a replayable runtime surface; do not ship a resident browser transport.
   - "Other" → read the user's free-form response and adapt

   **End of URL detection.** The remaining spec resolution rules apply when the argument is NOT a URL:

   - If the user passed `--har <path>`, this is a HAR-first run. Before invoking browser-sniff, parse the file as JSON and verify it contains traffic: HAR files must have `.log.entries | length > 0`; enriched capture JSON must have `.entries | length > 0`. If the file is missing, invalid JSON, or has zero entries, do not continue silently. Print `HAR/capture contains no network entries; the export likely recorded no traffic.` and ask via `AskUserQuestion` with exactly these choices: **Capture again (Recommended)** — "Re-export a HAR after recording a real user flow"; **Proceed with docs-only** — "Skip HAR/browser-sniff and continue from docs/spec discovery only"; **HOLD** — "Stop this run until a valid capture is available." Only proceed to browser-sniff when the parsed entry count is non-zero.
   - For a valid `--har <path>`, run `cli-printing-press browser-sniff --har <path> --name <api> --output "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" --analysis-output "$DISCOVERY_DIR/traffic-analysis.json"` to generate a spec and traffic analysis from captured traffic. If `$API_RUN_DIR/source-priority.json` exists with two or more sources, add `--preserve-hosts` so combo-CLI captures retain peer API hosts with per-endpoint `base_url` overrides instead of collapsing them into secondary evidence. Immediately inspect `$DISCOVERY_DIR/traffic-analysis.json`: if it contains the `empty_response_shapes` warning, or if every endpoint cluster has `size_class: "empty"` and `response_shape: {}`, use curl/direct HTTP to call each discovered endpoint and capture response structure before writing or trusting the spec. Do not proceed to `generate` with a sniffed spec that has no type information. Use the generated spec as the primary spec source for the rest of the pipeline only after this quality check passes. Skip the browser-sniff gate in Phase 1.7 (browser-sniff already ran).
   - If the user passed `--spec`, use it directly (existing behavior).
   - Otherwise, proceed with normal discovery (KnownSpecs, apis-guru, web search, and researched docs).

   #### Directory spec-source guard

   If any resolved spec source is a local directory, do not pass the directory
   itself to `cli-printing-press generate` and do not silently pick the first
   file. Enumerate candidate specs first:

   ```bash
   find "$SPEC_SOURCE_DIR" -type f \( -iname '*.json' -o -iname '*.yaml' -o -iname '*.yml' \) | sort
   ```

   Keep only files whose head looks like an OpenAPI or Swagger root document
   (`openapi:`, `swagger:`, or JSON with a top-level `"openapi"` or `"swagger"`
   key). Ignore unrelated JSON/YAML config files.

   When the filtered candidate list is empty, abort with:
   `No OpenAPI/Swagger spec found under <directory>. Pass --spec <file> directly.`
   Do not continue with the raw directory as the spec source.

   When the directory contains exactly one candidate, use that file as the
   spec source and write it to `state.json` as `spec_path`.

   When the directory contains more than one candidate:
   - Print a prominent warning before generation:
     `N OpenAPI/Swagger specs found under <directory>; no single file represents the whole API surface.`
   - List every candidate when `N <= 20`; otherwise list the first 20 sorted
     paths and print `...and N-20 more`.
   - Record the directory and candidates in `$STATE_FILE` before continuing:
     `spec_path` is the directory and `spec_candidates` is the sorted list.
   - Ask the user to choose one spec, several specs, or all specs. If this
     runtime cannot ask a blocking question, stop after printing the warning
     and tell the user to re-run with explicit `--spec <file>` arguments. This
     is the minimum safe floor: never let a directory run finish while hiding
     that additional specs were ignored.
   - After the user confirms the selection, update `$STATE_FILE` with
     `selected_spec_paths` set to the list that will be generated.
   - For multiple selected specs, default to one independent printed CLI per
     spec using a derived `<api>-<spec-slug>` name and a distinct working
     directory under `$API_RUN_DIR/working/`. Do not merge all selected specs
     into one CLI unless the user explicitly asks for a combined surface and
     provides the umbrella name for `--name`.

2. Check for prior research in:
   - `$PRESS_MANUSCRIPTS/<api-slug>/*/research/*`
3. Reuse good prior work instead of redoing it.
4. **Library Check** — Check if a CLI for this API already exists in the library or is actively being built, and present the user with context and options.

   First, check lock status to detect active builds:

   ```bash
   LOCK_STATUS=$("$PRINTING_PRESS_BIN" lock status --cli <api>-pp-cli --json 2>/dev/null)
   LOCK_HELD=$(echo "$LOCK_STATUS" | grep -o '"held"[[:space:]]*:[[:space:]]*[a-z]*' | head -1 | sed 's/.*: *//')
   LOCK_STALE=$(echo "$LOCK_STATUS" | grep -o '"stale"[[:space:]]*:[[:space:]]*[a-z]*' | head -1 | sed 's/.*: *//')
   LOCK_PHASE=$(echo "$LOCK_STATUS" | grep -o '"phase"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"phase"[[:space:]]*:[[:space:]]*"//;s/"//')
   LOCK_AGE=$(echo "$LOCK_STATUS" | grep -o '"age_seconds"[[:space:]]*:[[:space:]]*[0-9]*' | head -1 | sed 's/.*: *//')
   ```

   Then check the library directory:

   ```bash
   CLI_DIR="$PRESS_LIBRARY/<api>"
   HAS_LIBRARY=false
   HAS_GOMOD=false
   CLI_RELEASE_VERSION=""
   PRIOR_STEINBERGER_SCORE=""
   PRIOR_SUB60_REPRINT=false
   if [ -d "$CLI_DIR" ]; then
     HAS_LIBRARY=true
     if [ -f "$CLI_DIR/go.mod" ]; then
       HAS_GOMOD=true
     fi
     # Read manifest if available
     MANIFEST="$CLI_DIR/.printing-press.json"
     if [ -f "$MANIFEST" ]; then
       PRESS_VERSION=$(cat "$MANIFEST" | grep -o '"printing_press_version"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"printing_press_version"[[:space:]]*:[[:space:]]*"//;s/"//')
       GENERATED_AT=$(cat "$MANIFEST" | grep -o '"generated_at"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"generated_at"[[:space:]]*:[[:space:]]*"//;s/"//')
       PRIOR_STEINBERGER_SCORE=$(jq -r '.scorecard.steinberger.percentage // empty' "$MANIFEST" 2>/dev/null || true)
       if [ -n "$PRIOR_STEINBERGER_SCORE" ] && awk "BEGIN { exit !($PRIOR_STEINBERGER_SCORE < 60) }"; then
         PRIOR_SUB60_REPRINT=true
       fi
     fi
     RELEASE_MANIFEST="$CLI_DIR/.printing-press-release.json"
     if [ -f "$RELEASE_MANIFEST" ]; then
       CLI_RELEASE_VERSION=$(jq -r '.version // empty' "$RELEASE_MANIFEST" 2>/dev/null || true)
     fi
     # Get directory modification time as fallback
     CLI_MTIME=$(stat -f "%Sm" -t "%Y-%m-%d" "$CLI_DIR" 2>/dev/null || stat -c "%y" "$CLI_DIR" 2>/dev/null | cut -d' ' -f1)
   fi
   ```

   **Decision matrix:**

   | Library dir? | Lock? | Stale? | Has go.mod? | Action |
   |-------------|-------|--------|-------------|--------|
   | No | No | N/A | N/A | Proceed normally |
   | No | Yes | No | N/A | Warn: "Actively being built (phase: `<phase>`, `<age>` seconds ago). Wait, use a different name, or pick a different API." |
   | No | Yes | Yes | N/A | Offer reclaim: "Interrupted build detected (stale since `<age>`s ago). Reclaim and start fresh?" |
   | Yes | No | N/A | Yes | Existing "Found existing" flow (see below) |
   | Yes | No | N/A | No | Debris: "Found `<api>` directory in library but it appears incomplete (no go.mod). Clean up and start fresh?" If user approves, `rm -rf "$CLI_DIR"` and proceed normally. |
   | Yes | Yes | No | Any | Warn: "Actively being rebuilt (phase: `<phase>`, `<age>` seconds ago). Wait, use a different name, or pick a different API." |
   | Yes | Yes | Yes | Any | Offer reclaim: "Interrupted rebuild detected (stale since `<age>`s ago). Reclaim and start fresh?" |

   **If actively locked (not stale):** Present via `AskUserQuestion` with options to wait, pick a different API, or force-reclaim (`cli-printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE" --force`).

   **If stale lock:** Reclaiming is automatic on `lock acquire` in Phase 2. If user approves, proceed normally — the lock acquire in Phase 2 will auto-reclaim the stale lock.

   **If library exists with go.mod and no lock (completed CLI):** Display context and present options using `AskUserQuestion`:

   > Found existing `<api>` in library (last modified `<date>`).

   If `PRESS_VERSION` is available, append: `Built with printing-press v<version>.`
   If `CLI_RELEASE_VERSION` is available, append: `Published CLI release: <version>.` This is the public-library per-CLI CalVer release, not the generator version; do not use it for the generator staleness comparison below.
   If `PRIOR_SUB60_REPRINT=true`, append: `Prior Steinberger score: <score>%. Reprint will require all approved transcendence rows to ship unless you explicitly accept partial coverage.`

   If prior research was also found (step 2), include the research summary alongside the library info.

   Then ask:
   1. **"Generate a fresh CLI"** — Re-runs the Printing Press into a working directory, overwrites generated code, then rebuilds transcendence features. Prior research is reused if recent.
   2. **"Improve existing CLI"** — Keeps all current code, audits for quality gaps, implements top improvements. The Printing Press is not re-run.
   3. **"Review prior research first"** — Show the full research brief and absorb manifest before deciding.

   If the user picks option 1, proceed to Phase 1 (research) and then Phase 2 (generate) as normal.
   If the user picks option 2, invoke `/printing-press-polish <api>` to improve the existing CLI.
   If the user picks option 3, display the prior research, then re-present options 1 and 2.

   **MANDATORY when re-using prior research after a binary upgrade.** If the user picks "Generate a fresh CLI" (option 1) AND `PRESS_VERSION` from the manifest differs from the current binary's version (parse both via semver and compare; only fire when the leading minor or major segment changed — patch-level deltas don't trigger this), prompt the user once before kicking off Phase 1 research.

   Construct the prompt's "what changed" list from these category buckets — the categories are stable across versions; the specific machine deltas inside each category are not. Read `docs/CHANGELOG.md` (or run `git log --oneline v<PRESS_VERSION>..v<CURRENT> -- internal/`) and tag each notable change to one of these buckets:

   | Category | Affects prior-brief assumption about... |
   |---|---|
   | **Transport / reachability** | Which sources are reachable, what auth/clearance is needed, which clients (stdlib, Surf, browser-clearance) the brief assumed |
   | **Scoring rubrics** | What Phase 1.5/scorecard dimensions the brief targets, whether prior "high-priority" features still rank as such |
   | **Auth modes** | Whether brief's auth choice (api-key, cookie, composed, oauth) is still the right pick, whether new modes unlock new endpoints |
   | **MCP surface** | Whether brief's MCP shape (endpoint-mirror vs intent vs code-orchestration) matches the latest emit defaults |
   | **Discovery** | Whether browser-sniff / crowd-sniff workflows changed, whether prior gate decisions are still valid |

   For the prompt itself, list only the buckets that have at least one notable change between the two versions. If the CHANGELOG / git log is unavailable, list all five buckets generically and let the user decide.

   > "The prior `<api>` was generated with printing-press v`<PRESS_VERSION>`. The current binary is v`<CURRENT>`. Categories where the machine has changed since then: `<applicable buckets>`. Each can invalidate prior research assumptions. Re-validate the prior brief against the current machine before reusing it?"

   Options:
   1. **Yes, re-validate the prior research** — fold the validation into Phase 1 (briefly re-probe reachability for previously-blocked sources, confirm scoring still classifies the prior CLI's pattern correctly, etc.) before reusing the brief.
   2. **No, reuse the prior research as-is** — proceed with the brief verbatim, even if the underlying machine assumptions are stale.

   The prompt forces the user to acknowledge the version delta and explicitly accept (or refuse) re-validation. Skip it entirely on first generation, on same-version regenerations, or when no prior manifest exists.

   If no CLI exists in the local library and no lock is active, run the **Public-library check** below before proceeding to Phase 1.

   #### Public-library check (registry.json + blocked-apis.json)

   The local library check above only sees CLIs this machine has already printed. A user on a fresh checkout — or one who typed a slightly different name than the published slug (`Slack` vs `slack-bot`, `Cal` vs `cal-com`), or who described what they wanted in their own words (`Hacker News reader`, `Notion clone`, `prediction market`) — will miss CLIs that already exist in the public library. Scan `mvanhorn/printing-press-library/registry.json` to catch those cases before Phase 1 research begins (the expensive 30-60-minute portion of the pipeline).

   The public library also carries `blocked-apis.json`, a shared journal of APIs that were attempted and put on hold for reachability or buildability reasons. Scan it in the same Phase 0 window so a user does not repeat an already-known dead-end run before the blocking issue is fixed.

   **Skip this check entirely when:**
   - The local-library check above already prompted (mutual exclusion — do not double-ask).
   - `BROWSER_SNIFF_TARGET_URL` is set (the user is building a from-website CLI; the registry indexes API CLIs and naming collisions are unlikely and intentional).
   - The user passed `--har <path>` with an explicit `--name <api>` for a private capture.

   **Fetch the registry and blocked journal.** Match the pattern `/printing-press-import` and `/printing-press-reprint` already use:

   ```bash
   REGISTRY=$(mktemp)
   if ! gh api -H "Accept: application/vnd.github.v3.raw" \
        repos/mvanhorn/printing-press-library/contents/registry.json \
        > "$REGISTRY" 2>/dev/null; then
     echo "Public-library check failed: registry.json is unreachable. Stop here instead of treating the API as unpublished; retry when GitHub/library access is available."
     rm -f "$REGISTRY"
     exit 1
   fi

   BLOCKED_APIS=$(mktemp)
   if ! gh api -H "Accept: application/vnd.github.v3.raw" \
        repos/mvanhorn/printing-press-library/contents/blocked-apis.json \
        > "$BLOCKED_APIS" 2>/dev/null; then
     echo "Blocked-API journal check skipped: blocked-apis.json unreachable or absent. Proceeding with the registry check."
     rm -f "$BLOCKED_APIS"
     BLOCKED_APIS=""
   fi
   ```

   Do not continue past a registry fetch/parse failure: it can hide an already published nested CLI and route a duplicate greenfield build. Network failure or missing `blocked-apis.json` is still non-blocking. After step 4 finishes, clean up tempfiles only if the fetch succeeded: `[ -n "$REGISTRY" ] && rm -f "$REGISTRY"` and `[ -n "$BLOCKED_APIS" ] && rm -f "$BLOCKED_APIS"`. The blocked-journal failure branch above already removed its file and set its variable to empty, so an unconditional `rm -f "$BLOCKED_APIS"` would run `rm -f ""`.

   **Read the blocked journal before reasoning about registry matches.** If `BLOCKED_APIS` is non-empty, read it directly. Expected shape:

   ```json
   [
     {
       "slug": "1001tracklists",
       "attempted_at": "2026-05-25",
       "verdict": "hold",
       "reason": "Cloudflare Turnstile clearance gate; pure-HTTP cannot mint fsuid",
       "blocking_issue": 2140,
       "permanent": false
     }
   ]
   ```

   Entries are API-slug records, not printed-CLI registry entries. Match the user's requested API against `slug` using the same slug-normalization judgment as the registry check (`<api>`, `<api>-cli`, `<api>-pp-cli`, punctuation and case variants). Do not use vague category or description matching for the blocked journal. A false positive here stops a potentially valid run; only prompt when the entry appears to be the same API under a slug or brand spelling variant.

   If a blocked entry matches, prompt before reading registry matches:

   > "`<entry.slug>` was attempted on `<entry.attempted_at>` and held — `<entry.reason>`<tracking suffix>. The shared blocked-API journal exists so users do not repeat known unreachable or unbuildable runs before the blocker changes. Proceed anyway?"

   Where `<tracking suffix>` is:
   - ` (tracking #<entry.blocking_issue>; marked permanent)` when `blocking_issue` is non-null and `permanent` is `true`.
   - ` (tracking #<entry.blocking_issue>)` when `blocking_issue` is non-null and `permanent` is `false`.
   - ` (marked permanent)` when `permanent` is `true` and `blocking_issue` is `null`.
   - empty when neither applies.

   Options:
   1. **Stop here (recommended)** — end this run. If a tracking issue is present, tell the user to re-attempt only after that issue closes or the journal entry is updated.
   2. **Proceed anyway** — continue to the registry check and then Phase 1. Use this only when the user has new evidence that the blocker no longer applies or wants a deliberate fresh attempt.

   If multiple blocked entries somehow match, pick the most recent `attempted_at` value and mention that additional older journal entries exist. If `blocked-apis.json` is malformed, print "Blocked-API journal check skipped: blocked-apis.json is malformed. Proceeding with the registry check." and continue; do not let a bad journal file block fresh prints.

   **Read the registry and reason about matches** — do not gate on string equality alone. The file is small (~88 KB, ~135 entries today); read it directly and use judgment. Each entry has fields `name` (slug), `category`, `api` (brand display), `description`, `path`, `printer`.

   The user's argument may arrive in many shapes, and only some are catchable by deterministic match:

   - **Slug or near-slug** — `Notion`, `notion-cli`, `notion-pp-cli`
   - **Brand with punctuation** — `Cal.com`, `Customer.io`, `Archive.today`, `Trigger.dev`
   - **Concept or category** — `Hacker News reader`, `Notion clone`, `prediction market`, `prediction-market CLI`
   - **Adjacent product** — `Polymarket` when the registry has `kalshi` (peer prediction market)
   - **Genuinely novel** — no useful overlap

   Classify the best match at three confidence levels and act only on the top two:

   - **High** — same product under a different name (slug variant, brand vs slug form, `-cli`/`-pp-cli` suffix variant, well-known alias). Examples: `Cal.com` ↔ `cal-com`, `Notion` ↔ `notion-cli`, `slack` ↔ `slack-bot`.
   - **Medium** — same category and overlapping function; a reasonable user would want to know before building. Examples: `prediction market` finds `kalshi`, `Hacker News reader` finds `hackernews`, `Polymarket` surfaces `kalshi` as a peer.
   - **Low** — vaguely adjacent (e.g. "payment gateway" finding every payment-related CLI). Skip silently — false-positive prompts get dismissed reflexively at this gate.

   Resist over-matching on `description` keywords. Most descriptions mention several adjacent concepts; matching liberally on description text produces noise. Use the description to *confirm* a name-or-category candidate, not to *discover* candidates from scratch.

   **Combo CLIs.** When `SOURCE_PRIORITY` is set (from the Multi-Source Priority Gate above), skip the single-source High/Medium/No-match branches below. Classify matches per source, then present a single combined prompt rather than asking N times. For combo runs the existing single-source CLIs are usually *informational* — the user came here to build a combo, so the recommended default is to continue with the combo rather than reprint a component standalone.

   **Cap displayed reprint options at 2 across all sources combined** so the prompt fits the 4-option `AskUserQuestion` limit (2 reprints + continue + abort). Pick the 2 best candidates by judgment in this order: (1) High over Medium, (2) primary-source over secondary-source (the first entry in `SOURCE_PRIORITY` wins ties), (3) canonical slug over variant. If additional matches exist beyond the displayed 2, append "(plus N other source matches)" to the prompt body so the user knows the list is truncated. Omit sources with no match rather than listing them as empty rows. If no source has any match at High or Medium, print nothing and proceed to Phase 1.

   > Found matches across the sources you listed:
   >
   > - **`<source1>`**: `<entry1.name>` (`<entry1.api>`) [High] — same product as `<source1>`
   > - **`<source2>`**: `<entry2.name>` (`<entry2.api>`) [Medium] — similar/adjacent
   >
   > This is informational — these components already exist as single-source CLIs. Continue building the combo, switch to reprinting one standalone, or abort?

   Options:
   1. **Continue with the combo as planned (recommended)** — the combo itself is the value-add; proceed to Phase 1 with all sources.
   2. **Reprint `<entry1.name>` standalone instead** — invoke `/printing-press-reprint <entry1.name>` (abandons the combo for now).
   3. **Reprint `<entry2.name>` standalone instead** — same, for the second candidate.
   4. **Abort** — stop here.

   The `[High]` / `[Medium]` tags surface the confidence so the user can distinguish "this is literally the thing you named" from "this is adjacent." Tag in the bullet, not the option label, to keep options scannable.

   **Single-source CLIs.** When `SOURCE_PRIORITY` is not set, use the branches below.

   **High match — prompt strongly.** Under Claude Code, use `AskUserQuestion`; under another harness, use the equivalent native prompt primitive. The option set is the same either way.

   > Found **`<entry.api>`** in the public library (printed by **@`<entry.printer>`**, path `<entry.path>`).
   >
   > `<entry.description>`
   >
   > URL: `https://github.com/mvanhorn/printing-press-library/tree/main/<entry.path>`
   >
   > This CLI already exists. What would you like to do?

   Options:
   1. **Reprint with the current Printing Press (recommended)** — end this run and invoke `/printing-press-reprint <entry.name>`. That skill pulls the existing CLI, carries prior research and post-publish patches into reconciliation, and regenerates under the current binary. Almost always the right choice when a user discovers the CLI exists.
   2. **Continue and build a fresh one anyway** — proceed with the current run from scratch. Rare; appropriate only for a deliberate fork or variant.
   3. **Abort** — stop here.

   **Multiple High matches — present each candidate, do not use the Medium-match phrasing.** Rare — typically only happens when the user's argument is ambiguous between siblings like `slack` and `slack-bot`. Cap displayed candidates at 2 to stay within the 4-option prompt limit alongside continue/abort. If 3+ High candidates somehow qualify, pick the 2 best by judgment (typically the canonical slug match plus the next-most-likely alternative) and note "(plus N other close matches)" in the prompt body so the user knows the list is truncated.

   > Found multiple matches for **`<api>`** in the public library — each appears to be the same product under a different name:
   >
   > - **`<entry1.name>`** (`<entry1.api>`) — `<entry1.description>`
   > - **`<entry2.name>`** (`<entry2.api>`) — `<entry2.description>`
   >
   > Pick one to reprint, or continue/abort.

   Options:
   1. **Reprint `<entry1.name>`** — invoke `/printing-press-reprint <entry1.name>`.
   2. **Reprint `<entry2.name>`** — same, for the second candidate.
   3. **Continue and build a fresh one anyway** — rare; appropriate only for a deliberate fork or variant.
   4. **Abort** — stop here.

   **Medium match — present alternatives.** Cap candidates at 2 to stay within the 4-option prompt limit alongside continue/abort.

   > Found similar entries in the public library that don't exactly match `<api>` but may overlap:
   >
   > - **`<entry1.name>`** (`<entry1.api>`) — `<entry1.description>`
   > - **`<entry2.name>`** (`<entry2.api>`) — `<entry2.description>`
   >
   > Continue with `<api>` as planned, or reprint one of these instead?

   Options:
   1. **Continue with `<api>` as planned** — proceed to Phase 1.
   2. **Reprint `<entry1.name>` instead** — invoke `/printing-press-reprint <entry1.name>`.
   3. **Reprint `<entry2.name>` instead** — same, for the second candidate.
   4. **Abort** — stop here.

   **No High or Medium match:** print nothing, proceed to Phase 1.

5. **API Key Gate** — Check whether this API requires authentication, then handle accordingly.

**First, determine if the API needs auth.** Use these signals:
- The spec has no `security` or `securityDefinitions` section → likely no auth needed
- The API's endpoints are accessible without authentication (e.g., ESPN's undocumented endpoints, weather APIs, public data feeds) — note: "no auth required" does NOT mean the service has an official public API
- No env var matching the API name exists AND no known token pattern applies
- Community docs or npm/PyPI wrappers describe the API as "no auth required"

**If no auth is required**, skip the key gate entirely. Proceed with: "No authentication required for `<API>` — skipping API key gate." Do NOT call it "a public API" unless the service officially publishes one. Many services (ESPN, etc.) have unauthenticated endpoints without having an official API. Live smoke testing in Phase 5 will work without a key.

**If the API DOES require auth**, run the key gate:

Token detection order:
- GitHub: `GITHUB_TOKEN`, `GH_TOKEN`, or `gh auth token`
- Discord: `DISCORD_TOKEN`, `DISCORD_BOT_TOKEN`
- Linear: `LINEAR_API_KEY`
- Notion: `NOTION_TOKEN`
- Stripe: `STRIPE_SECRET_KEY`
- Generic: `API_KEY`, `API_TOKEN`

**If a token IS found**, stop and explain:
> Found `<ENV_VAR>` in your environment. This key will be used **only** for read-only live smoke testing in Phase 5 — listing, fetching, and health checks. It will never be used for write operations (create, update, delete). OK to use it?

- If the user approves → proceed with the key available for Phase 5.
- If the user declines → proceed without the key and display: "Live smoke testing (Phase 5) will be skipped. The CLI will still be generated and verified against mock responses."

**If no token is found**, stop and ask:
> No API key detected for `<API>`. You can provide one now for read-only live smoke testing in Phase 5, or continue without it.
>
> Set it with `export <ENV_VAR>=<your-key>` or paste the key here.

- If the user provides a key → proceed with the key available for Phase 5.
- If the user declines → proceed without the key and display: "Live smoke testing (Phase 5) will be skipped. The CLI will still be generated and verified against mock responses."

Resolve the API key gate (or skip it for public APIs) before moving to Phase 1.

## Phase 1: Research Brief

**When `BROWSER_SNIFF_TARGET_URL` is set:** Skip spec/docs search and SDK wrapper search — none of these exist for an undocumented website feature. Focus research on understanding what the site/feature does, who uses it, what workflows it supports, and what competitors offer similar functionality. The spec will come from browser-sniffing in Phase 1.7.

Before reading documentation, read [references/fetch-docs.md](references/fetch-docs.md). Use `fetch-docs.sh` for the API's primary docs, OpenAPI/Postman links, auth guides, error handling, rate limits, pagination, webhooks, and any per-endpoint reference page. Preserve exact status codes and inspect the returned local file directly so enum values, field constraints, casing, examples, and nav/link variants are not lost through summarization.

There is no built-in catalog lookup in this repo. New CLIs start from researched specs, documentation, browser/crowd/device discovery, or a reprint of an existing local/public-library artifact. If the target already exists in the public library, use the reprint/publish workflow rather than adding source metadata here.

Write one build-driving brief, not a stack of phase essays.

The brief must answer:

1. What is this API actually used for?
2. What are the top 3-5 power-user workflows?
3. What are the top table-stakes competitor features?
4. What data deserves a local store?
5. Why would someone install this CLI instead of the incumbent?
6. What is the product name and thesis?

Research checklist:
- Find the spec or docs source. For docs pages whose details affect generation, fetch the raw page with `fetch-docs.sh`, then read/grep the returned path directly.
- Find the top 1-2 competitors
- **Check GitHub issues on the top wrapper/SDK repo for "403", "blocked", "broken", "deprecated", "rate limit".** If multiple issues report the API is inaccessible or broken, flag this in the research brief as a reachability risk. This is critical for unofficial/reverse-engineered APIs.
- Find official and popular SDK wrappers on npm (`site:npmjs.com`) and PyPI (`site:pypi.org`)
- Find 2-3 concrete user pain points
- Identify the highest-gravity entities
- Pick the top 3-5 commands that matter most

Do not produce separate mandatory documents for:
- workflow ideation
- parity audit
- data-layer prediction
- product thesis

Put them in the one brief.

Write:

`$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-brief.md`

Suggested shape:

```markdown
# <API> CLI Brief

## API Identity
- Domain:
- Users:
- Data profile:

## Reachability Risk
- [None / Low / High] [evidence: e.g., "6 open issues on reteps/redfin about 403 errors since 2025"]
- Tier/permission hints from 4xx body: [omit when absent; otherwise quote the matched bounded line(s) from Phase 1.9]
- Probe-safe endpoint used: [omit when absent; otherwise "<METHOD> <path>" from `x-pp-safe-probe`]

## Top Workflows
1. ...

## Table Stakes
- ...

## Data Layer
- Primary entities:
- Sync cursor:
- FTS/search:

## Codebase Intelligence
- [DeepWiki findings if available, otherwise omit this section]
- Source: DeepWiki analysis of {owner}/{repo}
- Auth: [token type, header, env var pattern]
- Data model: [primary entities and relationships]
- Rate limiting: [limits and behavior]
- Architecture: [key insight about internal design]

## User Vision
- [USER_BRIEFING_CONTEXT if provided, otherwise omit this section]

## Source Priority
- [Only present for combo CLIs. Copy the confirmed ordering from `source-priority.json`.]
- Primary: <Source A> — [spec state: official / community-wrapper / no-spec-browser-sniff-required] — [auth: free / paid]
- Secondary: <Source B> — [...]
- Tertiary: <Source C> — [...]
- **Economics:** [e.g., "Primary is free; paid key for <Source B> is scoped to its own commands only."]
- **Inversion risk:** [e.g., "Primary has no OpenAPI; secondary has 53-endpoint spec. Do NOT let spec completeness invert the ordering."]

## Product Thesis
- Name:
- Why it should exist:

## Build Priorities
1. ...
2. ...
3. ...
```

**MANDATORY: Before proceeding to Phase 1.5 (Absorb Gate), you MUST evaluate Phase 1.6 (Pre-Browser-Sniff Auth Intelligence), Phase 1.7 (Browser-Sniff Gate), and Phase 1.8 (Crowd-Sniff Gate) below.** If no spec source has been resolved yet (no `--spec`, no `--har`, no researched spec URL), the browser-sniff gate decision matrix MUST be evaluated. Do not skip to Phase 1.5.

**Phase 1.5 will refuse to proceed without a `browser-browser-sniff-gate.json` marker file.** Phase 1.7 writes this file with one entry per source (one entry for single-source CLIs, one entry per named source for combo CLIs). Missing marker = HARD STOP back to Phase 1.7. See Phase 1.7 "Enforcement" below for the contract.

## Phase 1.6: Pre-Browser-Sniff Auth Intelligence

After Phase 1 research completes, analyze findings to proactively assess what auth context the user could provide. This step uses research intelligence to ask the right question before browser-sniffing starts, rather than waiting for the user to volunteer "I logged in."

**Skip this step if:** The briefing (Orientation & Briefing section) already captured auth context (`AUTH_CONTEXT` is set from the user selecting "I have an API key or I'm logged in").

**Classify the API's auth profile from research findings:**

| Signal from research | Auth profile | What to ask |
|---------------------|-------------|-------------|
| Community wrappers use API keys (e.g., `STRIPE_SECRET_KEY`), MCP source shows `Authorization: Bearer` headers, spec has `security` section | **API key auth** | "Do you have an API key for `<API>`?" |
| Site has user accounts, research found auth-only features (order history, saved items, rewards, account settings), login pages exist | **Browser session auth** | "This API has authenticated endpoints ([list specific features from research, e.g., order history, saved addresses, rewards]). Are you logged in to `<site>` in your browser? The browser-sniff will find more endpoints if you are." |
| Endpoints accessible without auth, no login-gated features found, community wrappers describe API as "no auth required" | **No auth needed** | Skip this step silently |
| Both API key AND browser session features found | **Dual auth** | Ask about both: API key for smoke testing, browser session for browser-sniff |

**Name the specific features the user would unlock.** Do not say "auth would help." Say "This API has order history, saved addresses, and rewards that require a logged-in session."

**Where signals come from:**
- Phase 1 brief's "Data profile" and "Top Workflows" sections
- Phase 1.5a MCP source code analysis (auth patterns, token formats)
- Community wrapper README "auth" or "authentication" sections
- The API Key Gate's token detection (Phase 0.5) — if it already found a key, don't re-ask

**For API key auth:** Present via `AskUserQuestion`:
> "Do you have an API key for `<API>`? It will be used for read-only live smoke testing in Phase 5."
>
> 1. **Yes** — user provides the key or confirms it's in the environment
> 2. **No, continue without it** — skip live smoke testing

If the user provides a key, set it in `AUTH_CONTEXT` so the API Key Gate (Phase 0.5) does not re-ask.

**For browser session auth:** Present via `AskUserQuestion`:
> "`<API>` has authenticated endpoints ([list features]). Are you logged in to `<site>` in your browser? If so, the generated CLI will support `auth login --chrome` — you'll be able to authenticate just by being logged into the site in Chrome. No API key needed."
>
> 1. **Yes, I'm logged in** — I'll use your session during browser-sniff and enable browser auth in the CLI
> 2. **No, but I can log in** — I'll help you log in before browser-sniffing
> 3. **No, skip authenticated endpoints** — browser-sniff only public endpoints

Set `AUTH_SESSION_AVAILABLE=true` if the user selects option 1 or 2. The Browser-Sniff Gate (Phase 1.7) will use this flag. After traffic capture, Step 2d in [references/browser-sniff-capture.md](references/browser-sniff-capture.md) validates that cookie replay works before enabling browser auth in the generated CLI.

**For dual auth:** Ask about both in sequence — API key first (simple env var check), then browser session.

---

## Phase 1.7: Browser-Sniff Gate

After Phase 1 research, evaluate whether browser-sniffing the live site would improve the spec. This phase MUST produce a decision marker file for every source named in the briefing before Phase 1.5 can proceed.

**Browser discovery is temporary discovery, not a printed-CLI runtime.** Use browser-use, agent-browser, the Claude chrome-MCP (`mcp__claude-in-chrome__*`, when the runtime exposes it), or a manual HAR (optionally augmented with computer-use screenshots for visual guidance, when `mcp__computer-use__*` is exposed) to learn the hidden web contract: URLs, methods, persisted GraphQL hashes, BFF envelopes, response shapes, cookies, CSRF/header construction, HTML/SSR/RSS/JSON-LD surfaces, and whether replay is viable. The final printed CLI must use replayable HTTP, Surf/browser-compatible HTTP, browser-clearance cookie import plus replay, or structured HTML/SSR/RSS extraction. If the only working path requires live page-context execution, HOLD or pivot scope — do not generate a resident browser sidecar transport.

**Automatic offer, explicit consent.** The Printing Press decides when browser discovery should be offered, but opening Chrome, attaching to a browser session, installing browser-use/agent-browser, asking the user to solve a challenge, or driving the user's logged-in Chrome via the chrome-MCP requires explicit user approval through the Phase 0 website choice or the Phase 1.7 `AskUserQuestion` prompt. **Approval at Phase 1.7 covers the full fallback set** including chrome-MCP and computer-use when Step 2c.5's recovery menu later offers them — picking chrome-MCP at the recovery menu is a refinement of the Phase 1.7 consent, not a new consent surface. The disclosure language used at the Phase 1.7 prompt MUST enumerate these possibilities so the user understands what they are approving:

> "Approving browser-sniff means the agent may run browser-use, agent-browser, ask you for a manual DevTools HAR export, or — if the default backends get blocked by an anti-bot gate and your runtime exposes them — drive your already-running Chrome via the chrome-MCP browser extension, or take read-only screenshots of your DevTools window via computer-use to guide you through the HAR export. Capture artifacts are written to `$DISCOVERY_DIR/` and credential headers are stripped at write time. The chrome-MCP option uses your real logged-in Chrome session in a fresh capture tab; the agent never navigates your existing tabs."

If chrome-MCP picks up later in Step 2c.5's recovery menu, do NOT re-fire a per-invocation consent prompt — Phase 1.7's pre-approval covers it. The recovery menu lists chrome-MCP as one of the fallback options the user already pre-approved; the user's selection in the menu is a backend choice, not a new consent step.

### Enforcement: the browser-browser-sniff-gate.json marker file

Phase 1.7 is a hard gate. Phase 1.5 reads a marker file and refuses to proceed without it. The model cannot skip this phase by reasoning around it.

**Marker file location:** `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json`

**Marker file shape:**

```json
{
  "run_id": "20260411-000903",
  "sources": [
    {
      "source_name": "<exact name from briefing, e.g., kayak-direct>",
      "decision": "approved | declined | skip-silent | pre-approved",
      "reason": "<one-line justification>",
      "asked_at": "2026-04-11T00:10:00Z"
    }
  ]
}
```

**Decision values:**

- `approved` — user selected a browser-sniff option via `AskUserQuestion`. Proceed to "If user approves browser-sniff".
- `declined` — user explicitly declined browser-sniff via `AskUserQuestion`. Proceed to "If user declines browser-sniff".
- `skip-silent` — gate was silently skipped per the decision matrix (spec complete, `--har` provided, `--spec` provided, or login required with `AUTH_SESSION_AVAILABLE=false`). The `reason` field names which.
- `pre-approved` — user already chose "The website itself" in Phase 0, where the prompt disclosed temporary Chrome/browser capture during generation, so `BROWSER_SNIFF_TARGET_URL` was set and the question was answered there.

**Every path through Phase 1.7 MUST write a marker entry** — approve, decline, and every silent-skip case. There is no code path that proceeds to Phase 1.5 without writing the marker.

**`asked_at` is mandatory.** It must reflect the actual time `AskUserQuestion` was invoked (or the time the silent-skip decision was made). Fabricated timestamps are a plan violation.

### Banned skip reasons

The following rationales are NOT valid reasons to skip the browser-sniff gate. If any of these apply, you MUST still ask the user via `AskUserQuestion` and record their answer in the marker file:

- **"The target is client-rendered and needs Playwright"** — browser capture tools (browser-use, agent-browser) exist specifically to handle client-rendered sites. A hard-to-browser-sniff target is not the same as an impossible one. Ask.
- **"Direct HTTP/curl got 403, 429, Cloudflare, Vercel, WAF, DataDome, or bot-detection HTML"** — direct HTTP reachability failure is exactly when browser capture is valuable. Do not pivot to RSS, docs-only, official API, or a smaller product shape before attempting the approved browser-sniff. Route to cleared-browser capture instead.
- **"Direct HTTP/curl got HTTP `200` but only a content-less shell, interstitial, or deterministic-size truncation"** — a 200-served shell is a clearance or JavaScript challenge, not a clean response. Do not conclude `IP-blocked`, `rate-limited`, or `wait it out` from this shape. Before declaring the target unreachable, climb the ladder: probe-reachability body-check, curl-impersonate/TLS check, real-browser cookie-warm via the cleared-browser path or chrome-MCP when available, then ask the user. Use chrome-MCP to understand the wall even when it cannot export cookie values.
- **"The 3-minute time budget looks tight"** — the time budget applies AFTER the user approves browser-sniff, not before. You do not pre-judge whether a browser-sniff will fit the budget. Ask. If the budget blows after the user approves, fall back per the Time Budget rules below.
- **"We have a substitute data source from another API"** — substituting one source for another is the user's call, not yours. If the user named a specific site or feature (e.g., Kayak /direct), they chose it deliberately. Ask about that exact source. Offering a different data source is a separate conversation AFTER the gate, not a reason to skip it.
- **"Installing browser-use or agent-browser is friction"** — the browser-sniff capture reference already documents the install path. Tooling friction is not a valid skip reason. Ask.
- **"The documentation looks thorough enough"** — the decision matrix already handles this case explicitly. If research found that competitors or community projects reference more endpoints than the spec covers, that IS a gap and you MUST ask.
- **"The user said 'let's go' earlier and implicitly approved everything"** — "let's go" at the briefing stage is consent to proceed with research, not standing approval for every future decision. Ask each gate individually.
- **"The default browser-use / agent-browser path got hard-blocked by a WAF, so the only remaining option is to pivot scope or fall back to RSS/docs"** — this is exactly when the chrome-MCP and computer-use fallback options enter, when the runtime exposes them. Step 1 of `references/browser-sniff-capture.md` detects which fallback MCPs are available; Step 2c.5 composes the recovery menu including those fallbacks; the gate is "ask before giving up," not "auto-pivot when blocked." Do NOT skip the Step 2c.5 menu. Do NOT pivot scope or substitute an alternate target without first asking the user via that menu.

These banned reasons all fired at once in a past combo-CLI run and caused a user-critical source to be silently swapped out. The marker file exists so this cannot happen again. If you find yourself writing a phrase like "skipping browser-sniff because X" where X is one of the above, stop and call `AskUserQuestion`.

### Combo CLIs: per-source enforcement

When the briefing names multiple sources (e.g., "Google Flights + Kayak + FlightAware"), each named source is evaluated independently. The marker file has one entry per source. All entries must be present before Phase 1.5 can proceed.

**Source identification rule:** source names come from the briefing, verbatim. Use the user's exact wording as the `source_name` (normalized to kebab-case is fine: "Kayak /direct" → `kayak-direct`, "Google Flights" → `google-flights`, "FlightAware" → `flightaware`). Do not merge sources. Do not drop one in favor of another.

**Per-source decision flow:**

For each named source, run the "When to offer browser-sniff" decision matrix independently, using the research findings for THAT source. Each source produces its own `AskUserQuestion` call or its own silent-skip marker entry.

**Combo CLI example** (flightgoat pattern — directional guidance, not prescription):

| Source | Spec state | Expected decision |
|--------|------------|-------------------|
| `flightaware` | Documented OpenAPI spec found (53 endpoints, appears complete) | `skip-silent` with reason `spec-complete` |
| `google-flights` | No official spec, but community wrapper exists (`krisukox/google-flights-api`) | Ask via `AskUserQuestion` → record user's answer |
| `kayak-direct` | No spec, no wrapper, user named this as a key feature | Ask via `AskUserQuestion` → record user's answer |

The marker file for this run would contain three entries. Phase 1.5 would HALT if any were missing.

**When the user cares about only one source:** you still ask for all sources that trigger the gate. The user can decline the others. Asking is cheap. Skipping silently breaks the contract.

### Skip this gate entirely when

These are the only cases where Phase 1.7 is bypassed as a whole (not just skipped for one source). Even in these cases, a marker file with a single `skip-silent` entry is written to satisfy Phase 1.5's check:

- User passed `--spec` and the spec is the canonical source for every named source → marker: `{ "source_name": "<api>", "decision": "skip-silent", "reason": "user-provided-spec" }`
- User passed `--har` → marker: `{ "source_name": "<api>", "decision": "skip-silent", "reason": "user-provided-har" }`
- `BROWSER_SNIFF_TARGET_URL` is set from Phase 0 (user chose "The website itself") → marker: `{ "source_name": "<api>", "decision": "pre-approved", "reason": "phase-0-website-choice" }`, then go directly to "If user approves browser-sniff"

### Direct HTTP challenge rule

If a reachability probe during Phase 1 research returns bot-protection evidence (`403`, `429`, `cf-mitigated: challenge`, `x-vercel-mitigated: challenge`, `x-vercel-challenge-token`, AWS WAF, DataDome, PerimeterX, CAPTCHA, "Just a moment", "access denied"), **run the no-browser reachability probe before announcing any browser escalation**:

```bash
cli-printing-press probe-reachability "<url>" --json
```

This is non-negotiable. **Do not present transport tiers as a peer menu for the user to choose between.** Phrases like "Browser-sniff + clearance cookie", "Browser-sniff with Surf-only", "Try without browser at all", or "Browser-sniff, prefer Surf" route the user through implementation choices (Surf vs cookie vs full browser) they don't have context to make. The classifier is `probe-reachability`; the agent runs it and decides. Intent-level menus are fine — "Browser-sniff or HOLD?", "Browser-sniff or pick a different API?", or the standard yes/no browser-sniff offers below all ask about goals, not transport, and remain available.

Escalate consent in the order the agent actually needs it, not bundled up-front:

1. **Runtime probe (silent)** — `probe-reachability` runs without prompting. The user already opted into "the website itself" or equivalent in Phase 0; running an HTTP request needs no further consent.
2. **Browser-sniff offer (intent prompt)** — Phase 1.7's normal "Browser-Sniff as enrichment" / "Browser-Sniff as primary" prompts ask whether to do browser-sniff at all. These are intent-level. Show them when the discovery matrix says to.
3. **Chrome attach (separate consent if escalation happens)** — when the agent actually needs to open or attach to Chrome (because the discovery flow requires a real browser, or because `mode: browser_clearance_http` means the runtime needs cookie capture), surface that as its own moment so the user knows they may need to solve a challenge or sign in. The user-facing prompts at lines below already disclose Chrome attach as a possibility; that is the right place to confirm. Do not pre-announce Chrome attach when the probe has already settled the runtime as `browser_http` and the spec is complete enough to skip discovery — there is no Chrome attach to announce in that path.

Two concerns are decided here, separately:

- **Runtime** (does the printed CLI need browser-compatible HTTP, a clearance cookie, or live page-context execution?) — settled entirely by `probe-reachability`.
- **Discovery** (does Phase 1.7 need to capture XHR traffic via a real browser to learn endpoints?) — settled by Phase 1.7's normal "When to offer browser-sniff" decision matrix above. Independent of runtime.

The probe runs stdlib HTTP, then Surf with a Chrome TLS fingerprint, and emits one of `standard_http | browser_http | browser_clearance_http | unknown`. Apply `mode` to the **runtime** decision:

- **`mode: standard_http`** — runtime is plain HTTP (the original probe was transient). Continue Phase 1.7 normally; the discovery decision is unchanged.
- **`mode: browser_http`** — **runtime is settled: ship Surf transport** (`UsesBrowserHTTPTransport` will be set in the generator's traffic-analysis hints). The printed CLI will not include `auth login --chrome` for clearance cookies — Surf alone clears the challenge. Continue Phase 1.7's discovery decision normally; the existing "Browser-Sniff as enrichment" / "Browser-Sniff as primary" prompts (above) are framed around endpoint discovery and are correct as-written. Do **not** add clearance-cookie language to those prompts.
- **`mode: browser_clearance_http`** — both probes hit protection signals. The runtime needs more than Surf (clearance cookie or live page-context execution; the probe cannot distinguish), so a real browser capture is required to find out. Proceed through Phase 1.7's normal browser-sniff offer (intent-level yes/no). The consent for Chrome attach happens at the moment the agent actually opens/attaches, where the user-facing prompts in `references/browser-sniff-capture.md` already disclose what's about to happen and may ask the user to solve a challenge. Note in the brief that runtime is provisionally `browser_clearance_http` pending capture results.
- **`mode: unknown`** — probes failed at the transport layer (DNS/timeout/5xx). Fall through to the existing browser-sniff offer; the user decides whether to retry or pivot.

When browser-sniff is approved or pre-approved AND the probe says `browser_clearance_http` or `unknown`:
- Do **not** offer alternate CLI shapes (RSS-first, official API, docs-only, narrower scope, "try anyway") before a real browser capture has been attempted.
- Do **not** write the brief as if browser-sniff is complete after only curl/direct HTTP probes.
- If browser automation tooling is unavailable, offer the user a manual HAR path before offering any scope pivot.

Only after the browser capture attempt fails by the criteria in `references/browser-sniff-capture.md` may you ask whether to pivot to RSS, official API, docs-only, or a smaller CLI scope.

### Time budget

The browser-sniff gate should complete within 3 minutes of the user approving browser-sniff. If browser automation tooling fails to produce results after 3 minutes of attempts, fall back immediately:
- If a spec already exists (enrichment mode): "Browser-Sniff failed after 3 minutes — proceeding with existing spec."
- If no spec exists (primary mode): "Browser-Sniff failed after 3 minutes — falling back to --docs generation."
- If browser-sniff was approved or pre-approved and direct HTTP showed challenge/bot-protection evidence, do **not** auto-fall back to docs/official API, even when `BROWSER_SNIFF_TARGET_URL` is unset. Ask whether the user wants to provide a HAR manually, retry cleared-browser capture, or discuss alternate CLI scope.

Do NOT spend time debugging tool integration issues. Browser-sniff is a temporary discovery aid, not the product runtime. If the first approach fails, fall back to the next option — do not retry the same broken approach.

**The time budget applies AFTER the user approves.** Do not use it as a reason to skip the gate before asking.

### When to offer browser-sniff

| Spec found? | Research shows gaps? | Auth required? | Action |
|-------------|---------------------|----------------|--------|
| Yes | Yes — docs or competitors show significantly more endpoints than the spec | No | **MUST offer browser-sniff as enrichment** |
| Yes | No — spec appears complete | Any | Skip silently (write marker with `decision: skip-silent`) |
| No | Community docs exist (e.g., Public-ESPN-API) | No | **MUST offer browser-sniff OR --docs** — present both options so the user decides |
| No | No docs found either | No | **MUST offer browser-sniff as primary discovery** |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=true` | **Offer authenticated browser-sniff** — the user confirmed a session in Phase 1.6 |
| No | N/A | Yes (login) + `AUTH_SESSION_AVAILABLE=false` | Skip — fall back to `--docs` (write marker with `decision: skip-silent`, `reason: login-required-no-session`) |

**Gap detection heuristic:** If Phase 1 research found documentation, competitor tools, or community projects that reference significantly more endpoints or features than the resolved spec covers, that's a gap signal. Example: "The Zuplo OpenAPI spec has 42 endpoints, but the Public-ESPN-API docs describe 370+."

**When the decision matrix says "Offer browser-sniff", you MUST ask the user via `AskUserQuestion`.** Skipping the question and writing a `skip-silent` marker is a contract violation — `skip-silent` is only valid when the matrix says "Skip silently" or one of the Banned Skip Reasons is the only thing holding you back (in which case, you should be asking anyway).

Every browser-sniff approval prompt must make the consent boundary explicit:
- browser discovery may open or attach to Chrome during generation,
- it may ask the user to log in or solve a challenge,
- it may request permission to install or upgrade browser-use/agent-browser if missing,
- the printed CLI will only ship if discovery finds a replayable surface and will not keep a browser running as normal command transport.

### Browser-Sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more (competitors reference M+ features). Want me to use temporary browser discovery on `<url>` to find replayable endpoints the spec missed? I may open or attach to Chrome during generation, and I will ask before installing or upgrading browser-use/agent-browser."
>
> Options:
> 1. **Yes — browser-sniff and merge** (temporarily open or attach to Chrome during generation, capture traffic, then merge only replayable discovered endpoints with the existing spec. Ask before installing capture tools.)
> 2. **No — use existing spec** (proceed with what we have)

### Browser-Sniff as primary (no spec found)

Present to the user via `AskUserQuestion`. **If `AUTH_SESSION_AVAILABLE=true`**, include an authenticated browser-sniff option:

> "No OpenAPI spec found for `<API>`. Want me to browser-sniff `<likely-url>` to discover the API from live traffic?"
>
> Options:
> 1. **Yes — authenticated browser-sniff** (temporarily open or attach to Chrome during generation, use your browser session to discover public and authenticated traffic, and generate only replayable CLI surfaces. Recommended since you confirmed a session.) *(Only show when `AUTH_SESSION_AVAILABLE=true`)*
> 2. **Yes — browser-sniff the live site** (temporarily browse `<url>` anonymously, capture API/HTML traffic, and generate a spec only from replayable surfaces. Ask before installing capture tools.)
> 3. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 4. **No — I'll provide a spec or HAR** (user will supply input manually)

When `AUTH_SESSION_AVAILABLE=false`, show only options 2-4 (the existing 3-option prompt).

### If user approves browser-sniff

**Before doing anything else, write the marker entry** for this source:

```json
{
  "source_name": "<normalized name from briefing>",
  "decision": "approved",
  "reason": "<which option they picked, e.g., 'authenticated browser-sniff' or 'browser-sniff and merge'>",
  "asked_at": "<current ISO8601 timestamp>"
}
```

Append it to `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json` (create the file if it doesn't exist).

#### Step 0: Identify the User Goal

Before building the capture plan, answer one question: **What does the end user of this CLI actually want to do?**

Read the research brief's Top Workflows. The #1 workflow IS the primary browser-sniff goal. State it in one sentence:
- Domino's: "Order a pizza for delivery"
- Linear: "Create an issue and assign it to a sprint"
- Stripe: "Create a payment intent and confirm it"
- ESPN: "Check today's scores and standings"
- Notion: "Create a page and organize it in a database"

If the API is read-only (news, weather, data feeds), the primary goal is "fetch and filter data" and the flow is search/filter/paginate rather than a multi-step transaction.

The browser-sniff will walk through this goal as an interactive user flow. Secondary workflows become secondary browser-sniff passes if time permits.

State the goal explicitly before proceeding: "Primary browser-sniff goal: [goal]. I will walk through this as a user flow."

Then read and follow [references/browser-sniff-capture.md](references/browser-sniff-capture.md) for the complete
browser-sniff implementation: tool detection, installation, session transfer, browser-use/agent-browser/manual HAR
capture, replayability analysis, and discovery report writing.

### If user declines browser-sniff

**Write the marker entry** for this source before proceeding:

```json
{
  "source_name": "<normalized name from briefing>",
  "decision": "declined",
  "reason": "<which option they picked, e.g., 'use existing spec' or 'use docs instead'>",
  "asked_at": "<current ISO8601 timestamp>"
}
```

Append it to `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json`.

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

### Before leaving Phase 1.7

Every source named in the briefing must have exactly one entry in `browser-browser-sniff-gate.json`. Before proceeding to Phase 1.8, re-read the marker file and verify the count matches the number of named sources from the briefing. If a source is missing, return to the decision matrix for that source. Phase 1.5 will HALT if this check fails.

---

## Phase 1.8: Crowd-Sniff Gate

After Phase 1.7 (Browser-Sniff Gate), evaluate whether mining community signals (npm SDKs and GitHub code search) would improve the spec. Skip this gate entirely if the user already passed `--spec` (spec source is already resolved and appears complete).

**Time budget:** The crowd-sniff gate should complete within 10 minutes. If `cli-printing-press crowd-sniff` fails or times out, fall back immediately:
- If a spec already exists: "Crowd-sniff failed — proceeding with existing spec."
- If no spec exists: "Crowd-sniff failed — falling back to --docs generation."

### When to offer crowd-sniff

| Spec found? | Research shows gaps? | Action |
|-------------|---------------------|--------|
| Yes | Yes — competitors or community projects reference more endpoints | **Offer crowd-sniff as enrichment** |
| Yes | No — spec appears complete | Skip silently |
| No | Community SDKs exist on npm | **Offer crowd-sniff as primary discovery** |
| No | No SDKs or code found | Skip — fall back to `--docs` |

### Crowd-sniff as enrichment (spec exists but has gaps)

Present to the user via `AskUserQuestion`:

> "Found a spec with **N endpoints**, but research shows the live API likely has more. Want me to search npm packages and GitHub code for `<api>` to discover additional endpoints? This typically takes 5-10 minutes."
>
> Options:
> 1. **Yes — crowd-sniff and merge** (search npm SDKs and GitHub code, merge discovered endpoints with the existing spec)
> 2. **No — use existing spec** (proceed with what we have)

### Crowd-sniff as primary (no spec found)

Present to the user via `AskUserQuestion`:

> "No OpenAPI spec found for `<API>`. Want me to search npm packages and GitHub code to discover the API from community usage? This typically takes 5-10 minutes."
>
> Options:
> 1. **Yes — crowd-sniff the community** (search npm SDKs and GitHub code, generate a spec from discovered endpoints)
> 2. **No — use docs instead** (attempt `--docs` generation from documentation pages)
> 3. **No — I'll provide a spec or HAR** (user will supply input manually)

### If user approves crowd-sniff

Read and follow [references/crowd-sniff.md](references/crowd-sniff.md) for the crowd-sniff
command, provenance capture, and discovery report writing.

### If user declines crowd-sniff

Proceed with whatever spec source exists. If no spec was found, fall back to `--docs` or ask the user to provide a spec/HAR manually.

---

## Phase 1.5: Ecosystem Absorb Gate

THIS IS A MANDATORY STOP GATE. Do not generate until this is complete and approved.

### Pre-flight check: browser-sniff-gate marker

Before any absorb work, verify `$PRESS_RUNSTATE/runs/$RUN_ID/browser-browser-sniff-gate.json` exists and contains an entry for every source named in the briefing.

**If the file is missing:** HARD STOP. Print:

> Phase 1.7 Browser-Sniff Gate did not record a decision. Return to Phase 1.7 and evaluate the browser-sniff gate for every source named in the briefing.

Do not proceed to Step 1.5a until the file exists.

**If the file exists but is missing an entry for a named source:** HARD STOP. Print:

> Browser-Sniff Gate missing decision for source `<name>`. Return to Phase 1.7 and evaluate the decision matrix for that source.

Do not proceed until every briefing source has a marker entry.

**Resume leniency:** If the run was started by an older version of the skill that didn't write markers, warn and continue — do not hard-fail on legacy resumes. Distinguish by checking whether `state.json` predates the marker contract (the marker file didn't exist before 2026-04-11). New runs always hard-fail on a missing marker.

**Pre-check (existing):** If no spec or HAR file has been resolved by this point and Phase 1.7 (Browser-Sniff Gate) was not evaluated, STOP. Go back and run the browser-sniff gate decision matrix. The absorb manifest depends on knowing the API surface, which requires a spec.

The GOAT CLI doesn't "find gaps." It absorbs EVERY feature from EVERY tool and then transcends with compound use cases nobody thought of. This phase builds the absorb manifest.

### Step 1.5a: Search for every tool that touches this API

Run these searches in parallel:

1. **WebSearch**: `"<API name>" Claude Code plugin site:github.com`
2. **WebSearch**: `"<API name>" MCP server model context protocol`
3. **WebSearch**: `"<API name>" Claude skill SKILL.md site:github.com`
4. **WebSearch**: `"<API name>" CLI tool site:github.com` (competing CLIs)
5. **WebSearch**: `"<API name>" CLI site:npmjs.com` (npm packages)
6. **Raw fetch**: Check `github.com/anthropics/claude-plugins-official/tree/main/external_plugins` for official plugins with the helper from [references/fetch-docs.md](references/fetch-docs.md), or with `gh api` when it can return the file/listing directly.
7. **WebSearch**: `"<API name>" MCP site:lobehub.com OR site:mcpmarket.com OR site:fastmcp.me`
8. **WebSearch**: `"<API name>" automation script workflow site:github.com`
9. **WebSearch**: `"<API name>" SDK wrapper site:npmjs.com`
10. **WebSearch**: `"<API name>" client library site:pypi.org`

### Step 1.5a.5: Read MCP source code (if found)

If step 1.5a discovered MCP server repos with public source code on GitHub, read the actual source to extract ground-truth API usage — not just README feature descriptions.

**Time budget:** Max 3 minutes total. If extraction is unproductive, fall back to README-only research.

**For the top 1-2 MCP repos found:**

1. **Identify the main source file.** Use `gh api`, raw GitHub URLs, or the helper from [references/fetch-docs.md](references/fetch-docs.md) to inspect the repo tree and source files without a summarization layer. Find the entry point — typically `src/index.ts`, `server.ts`, `server.py`, `main.go`, or a `tools/` directory. MCP servers are usually small (one main file + tool definitions).

2. **Extract three things:**
   - **API endpoint paths**: Look for HTTP client calls (`fetch(`, `axios.`, `requests.`, `http.Get`, `client.`) and extract the URL paths (e.g., `GET /v1/issues`, `POST /graphql`). These are the endpoints the MCP maintainer proved work.
   - **Auth patterns**: Look for how the MCP constructs auth headers — token format (`Bearer`, `Bot`, `Basic`), header name (`Authorization`, `X-API-Key`), environment variable names. This informs our auth setup guidance.
   - **Response field selections**: Look for which fields are extracted from API responses — these are the high-gravity fields that power users actually need.

3. **Feed into absorb manifest.** In step 1.5b, endpoints extracted from source get attributed as `<MCP name> (source)` in the "Best Source" column, distinguishing them from README-derived features. Source-extracted endpoints are high-confidence signals — the maintainer verified they work.

4. **Feed auth patterns into research brief.** If the MCP source reveals token format (e.g., `xoxp-` for Slack, `sk_live_` for Stripe), credential setup steps, or required scopes, note them in the Phase 1 brief's auth section. These hints improve the generated CLI's auth onboarding.

**Skip this step when:**
- No MCP repos were found in 1.5a
- MCP repos are private or archived
- The MCP is a monorepo where the relevant server is hard to locate within 3 minutes

### Step 1.5a.6: DeepWiki Codebase Analysis (if GitHub repos found)

If Phase 1 or Step 1.5a discovered GitHub repos for the API (SDK repos, server repos, MCP server repos), query DeepWiki for a semantic understanding of how the API works - architecture, auth flows, data models, error handling. This complements crowd-sniff (endpoints) and MCP source reading (auth headers) with "how things actually work" context.

**Time budget:** 2 minutes max. If DeepWiki is slow or unavailable, skip silently.

**Run in parallel** with Steps 1.5a through 1.5a.5 when possible. DeepWiki queries do not depend on MCP source reading results.

Read and follow [references/deepwiki-research.md](references/deepwiki-research.md) for the query procedure: wiki structure fetch, targeted section extraction (auth, data model, architecture), and synthesis into the research brief and absorb manifest.

**Skip this step when:**
- No GitHub repos were discovered during Phase 1 or Step 1.5a
- The API is trivially simple (1-2 endpoints, no auth)

### Step 1.5b: Catalog every feature into the absorb manifest

For EACH tool found, list EVERY feature/tool/command it provides. Then define how our CLI matches AND beats it:

```markdown
## Absorb Manifest

### Absorbed (match or beat everything that exists)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-----------|-------------------|-------------|
| 1 | Search issues by text | Linear MCP search_issues | <api>-pp-cli search | Works offline, regex, SQL composable |
| 2 | Create issue | Linear MCP create_issue | <api>-pp-cli issue create --stdin --dry-run | Agent-native, scriptable, idempotent |
| 3 | Sprint board view | jira-cli sprint view | <api>-pp-cli sprint view | Historical velocity, offline |
```

Every row = a feature we MUST build. No exceptions. If someone else has it, we have it AND it works offline, with --json, --dry-run, typed exit codes, and SQLite persistence.

SDK wrapper methods should be treated as features to absorb — each public method/function is a feature the CLI should match.

**Our Implementation must start with a parseable disposition.** Use one of these prefixes so Phase 3 can verify the row mechanically:
- `<api>-pp-cli <clean command path>` for a promoted or hand-built Cobra command path that must resolve via `<binary> <path> --help`.
- `(generated endpoint) <resource> <endpoint>` for generator-emitted typed endpoint commands that retain the upstream resource shape and are covered by the generated endpoint surface.
- `(behavior in <api>-pp-cli <command path>) ...` for features implemented as flags, modes, output shapes, or store behavior inside another command. The named command path still must resolve; the prose after the closing parenthesis explains the behavior to verify later.
- `(stub) ...` only for explicitly approved stubs per the rule below.

Do not leave `Our Implementation` as freeform prose like `FTS5 offline search` or `SQLite-backed sprint query`. If the row maps to a clean user-facing command, put that command path first. If it does not, choose the explicit disposition that explains why Phase 3 should not treat the whole cell as a new command path.

**Stubs must be explicit.** If any row in the manifest will ship as a stub (placeholder implementation that emits "not yet wired" / "wip" messaging), start `Our Implementation` with `(stub)` plus a one-line reason why the full implementation is deferred (e.g., "(stub - requires paid API)", "(stub - requires headless Chrome)"). If the manifest also has a `Status` column, set that value to `(stub)` too, but the `Our Implementation` prefix is the Phase 3 gate's source of truth. Do NOT quietly ship stubs for features the user approved as shipping scope.

The Phase Gate 1.5 prose showcase (below) MUST read out stub items separately so the user explicitly approves the stub list. After approval, Phase 3 builds shipping-scope features fully and stubs with honest messaging; no mid-build downgrade from shipping-scope to stub is permitted. If an agent discovers during Phase 3 that a shipping-scope feature cannot be implemented in-session, they must return to Phase 1.5 with a revised manifest — not unilaterally downgrade to a stub.

### Step 1.5c: Identify transcendence features

Start with the users, not the technology. The best features come from understanding
who uses this service, what their rituals are, and what questions they can't answer
today. "What can SQLite do?" is the wrong question. "What would make a power user
say 'I need this'?" is the right one.

The actual brainstorming runs as a Task subagent in Step 1.5c.5 below — customer
model → 2× candidates → adversarial cut. Step 1.5c is the motivation; do not
generate transcendence features inline here.

The transcendence table in the manifest (Step 1.5d) renders rows in this shape,
which mirrors the subagent's `### Survivors` output. The `Buildability` column
tags each row `spec-emits` or `hand-code` per
[references/novel-features-subagent.md](references/novel-features-subagent.md)
so the Phase Gate 1.5 hand-code count has a source of truth in the manifest.
The optional `Long Description` column carries agent-facing disambiguation
text for Phase 3 Cobra `Long` fields; use `none` when no sibling redirect is
needed:

```markdown
### Transcendence (only possible with our approach)
| # | Feature | Command | Buildability | Why Only We Can Do This | Long Description |
|---|---------|---------|--------------|------------------------|------------------|
| 1 | Bottleneck detection | bottleneck | hand-code | Requires local join across issues + assignees + cycle data | Use this command to find cross-team work blockage. Do NOT use it for personal recency checks; use 'since' instead. |
| 2 | Velocity trends | velocity --weeks 4 | hand-code | Requires historical cycle snapshots in SQLite | none |
| 3 | What did I miss | since 2h | hand-code | Requires time-windowed aggregation no single API call provides | Use this command for recent personal changes. Do NOT use it for backlog bottlenecks; use 'bottleneck' instead. |
```

Minimum 5 transcendence features. These are the commands that differentiate the CLI.

### Step 1.5c.5: Auto-Suggest Novel Features (subagent)

**Always spawn the subagent — first prints and reprints alike.** The subagent
is the only path that produces this step's outputs (customer model, candidate
list, adversarial cut, killed-candidate audit trail). There is no manual
fallback. Specifically, do not:

- hand-curate the transcendence list from a prior manifest, even when the
  prior looks complete. Prior `research.json` is INPUT to Pass 2(d), never
  a substitute for the spawn.
- fall back to inline brainstorming inside the SKILL.
- skip on cost grounds. With a strong prior the subagent confirms or
  reframes; with no prior it generates from scratch. Run it either way.
- treat disclosure as authorization. Announcing a skip in the gate showcase
  does not make the skip legal.

Read [references/novel-features-subagent.md](references/novel-features-subagent.md)
for the prior-research discovery snippet, input bundle, prompt template, and
output contract. Run the discovery snippet as written — do not substitute an
`ls` of the manuscripts directory. The snippet's `none` branch (no prior
research) is a first print, not a skip signal.

The only legitimate non-spawn outcome is the pre-flight HALT (brief lacks
user research) defined in the reference file.

### Step 1.5d: Write the manifest artifact

Write to `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`

The manifest now includes compound use cases (Step 1.5c) and auto-suggested + auto-brainstormed features (Step 1.5c.5) in the transcendence table.

### Step 1.5e: Write research.json for README credits

After writing the absorb manifest, also write `$API_RUN_DIR/research.json` so the generator can credit community projects in the README. This file MUST match the `ResearchResult` JSON schema that `loadResearchSources()` expects.

Populate the `alternatives` array from the absorb manifest's source tools list. Include only tools that:
1. Have a GitHub URL (not npm/PyPI landing pages)
2. Actually contributed features to the absorb manifest
3. Are capped at 8 entries, ordered by number of absorbed features (then by stars)

```bash
cat > "$API_RUN_DIR/research.json" <<REOF
{
  "api_name": "<api>",
  "novelty_score": 0,
  "alternatives": [
    {"name": "<tool1>", "url": "<github-url>", "language": "<Go|JavaScript|Python|etc>", "stars": <N>, "command_count": <N>},
    ...
  ],
  "auth": {
    "canonical_env_var": "<CANONICAL_ENV_VAR, omit when unknown>"
  },
  "novel_features": [
    {
      "name": "<Feature Name>",
      "command": "<cli-subcommand>",
      "description": "<One sentence: what the user gets>",
      "rationale": "<One sentence: why only possible with our approach>",
      "example": "<ready-to-run invocation with realistic args, e.g. 'yahoo-finance-pp-cli portfolio perf --agent'>",
      "why_it_matters": "<One sentence aimed at AI agents: when should they reach for this?>",
      "group": "<Theme name clustering similar features, e.g. 'Local state that compounds'>"
    },
    ...
  ],
  "narrative": {
    "display_name": "<Canonical prose name, exact brand casing/spaces, e.g. Product Hunt, GitHub, YouTube, Cal.com>",
    "headline": "<Bold one-sentence value prop: what makes this CLI worth using>",
    "value_prop": "<2-3 sentence expansion rendered beneath the title>",
    "auth_narrative": "<API-specific auth story; omit for simple API-key auth>",
    "quickstart": [
      {"command": "<cli> <real-command-with-real-args>", "comment": "<why this comes first>"},
      ...
    ],
    "troubleshoots": [
      {"symptom": "<user-visible error or symptom>", "fix": "<actionable one-liner>"},
      ...
    ],
    "when_to_use": "<2-4 sentences describing ideal use cases; rendered in SKILL.md only>",
    "anti_triggers": ["<task boundary this CLI should not handle>", ...],
    "recipes": [
      {"title": "<Recipe name>", "command": "<cli> <invocation>", "explanation": "<one-line paragraph>"},
      ...
    ],
    "trigger_phrases": ["<natural phrase that should invoke this CLI's skill>", ...]
  },
  "gaps": [],
  "patterns": [],
  "recommendation": "proceed",
  "researched_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
REOF
```

For each tool, fill in what you know from the research. Stars and command_count are optional (use 0 if unknown). The `language` field should match the primary implementation language. Skip tools that were found during search but contributed zero features to the manifest.

**Novel features rules** (the `novel_features` array populates the README's "Unique Features" section and SKILL.md's "Unique Capabilities" block; MCP exposure comes from the runtime Cobra-tree mirror, not this list):
1. Include all transcendence features from the manifest that scored >= 5/10. Order by score descending.
2. `description` should be user-benefit language, not implementation detail. Good: "See which team members are overloaded before sprint planning." Bad: "Requires local join across issues + assignees + cycle data."
3. `rationale` should explain why this is only possible with our approach. Good: "Requires correlating bookings, schedules, and staff data that only exists together in the local store." Bad: "Cal.com Insights is paid-tier only."
4. `command` must match the actual CLI subcommand that will be built in Phase 3. For subcommands of a resource (e.g., `issues stale`), use the full command path.
5. `example` is a ready-to-run invocation an agent can copy-paste. Use realistic arguments from the API's domain (e.g. `AAPL`, `customer_42`), not `<placeholder>`. Include the `--agent` flag when the feature benefits from structured output.
6. `why_it_matters` is a single agent-facing sentence answering "when should I pick this over a generic API call?"
7. `group` clusters related features under a theme name. Pick 2–5 themes total (e.g. "Local state that compounds", "Agent-native plumbing", "Reachability mitigation"). Use the same `group` string verbatim across features that belong together — exact matches drive README grouping. Leave `group` empty if the CLI has too few novel features to warrant clustering.
8. If the manifest row has a non-`none` `Long Description`, keep that text with the feature implementation notes and use it as the Cobra `Long` field during Phase 3 hand-code. Do not squeeze redirect prose into `description`; `description` stays one-line user-benefit text.
9. If no transcendence features scored >= 5/10, omit the `novel_features` field entirely.
10. Do not add a feature to `novel_features` merely to expose it through MCP. Any user-facing Cobra command becomes an MCP tool automatically unless it sets `cmd.Annotations["mcp:hidden"] = "true"`.

**Auth research rule**:
1. `auth.canonical_env_var` is the single-token credential env var discovered from vendor docs, MCP/source analysis, or dominant SDK/CLI convention (for example `APIFY_TOKEN`, `GITHUB_TOKEN`, `STRIPE_SECRET_KEY`). Omit it when no canonical name is known, when auth is HTTP Basic or another credential pair, or when the auth flow needs richer metadata. Fresh generation reads this env var first and keeps the parser-derived name as a fallback automatically.

**Narrative rules** (the `narrative` object drives README headline, Quick Start, Auth, Troubleshooting, and the entire SKILL.md):
1. `display_name` is the canonical prose name, discovered during research, with exact brand casing and spacing. This is agentic/research-owned, not slug-inferred by Go code. Good: "Product Hunt", "GitHub", "YouTube", "Cal.com". Bad: "Producthunt", "Github", "Youtube", "Cal Com". Use the slug only for binary names, directories, module paths, config paths, and env-var prefixes.
2. `headline` is the bold one-liner rendered beneath the CLI title. Should name the differentiator, not restate the API. Good: "Every Notion feature, plus sync, search, and a local database no other Notion tool has." Bad: "A CLI for the Notion API."
3. `value_prop` expands the headline to 2–3 sentences. Name specific novel features by command where helpful.
4. `auth_narrative` tells the real auth story for this API (crumb handshake, cookie session, OAuth device flow). Omit for standard API-key auth where the generic branch is fine.
5. `quickstart` is a 3–6 step flow using REAL arguments (symbols, IDs, resource names an agent can actually pass). Each step's `comment` explains *why* it runs. This replaces the generic "resource list" first-command fallback.
   - Step 1 of `quickstart` should usually be verify-safe: it should exit 0 when `validate-narrative --full-examples` appends `--dry-run` in a no-credentials environment.
   - Use `<cli> doctor --dry-run` as step 1 (health check, works without auth). Do not use `<cli> auth set-token <token>` as step 1 because it requires a positional token and is not a verify-safe runnable first step. Auth setup instructions belong in `auth_narrative` prose only, not as an executable quickstart command.
6. `troubleshoots` captures API-specific failure modes (rate-limit mitigation, cookie expiry, paginated quirks). Each `fix` must be actionable — a command or a concrete setting change.
7. `when_to_use` is SKILL-only narrative. 2–4 sentences describing the kinds of agent tasks this CLI is the right choice for. Not rendered in README.
8. `anti_triggers` is SKILL-only narrative. List common task boundaries that should make an agent choose another tool, official SDK, web UI, or human workflow instead of this CLI. Write concrete "do not use this CLI for X" cases, not vague limitations. Omit the field only when no honest boundary is known.
9. `recipes` are 3–5 worked examples rendered in SKILL.md. Each has a title, a real command, and a one-line explanation. Prefer recipes that exercise novel features. **At least one recipe must pair `--agent` with `--select`** — using dotted paths (e.g. `--select events.shortName,events.competitions.competitors.team.displayName`) when the response is deeply nested. APIs like ESPN, HubSpot, and Linear return tens of KB per call; without a `--select` recipe, agents burn context parsing verbose payloads. Pick a command known to return a large or deeply nested response and show the narrowing pattern. **Regex literals must double-escape backslashes** — write `\\b` not `\b` (and `\\t`, `\\f`, etc.) inside any `command`, `fix`, or other JSON string field. JSON parses `\b` as backspace (0x08), `\f` as form feed (0x0C), and so on, which then leak into the rendered SKILL.md as control bytes that render as nothing in most viewers. The generator's render-time scanner rejects these with a clear offset; double-escape from the start to avoid the error.
10. `trigger_phrases` are natural-language phrases a user might say that should invoke this CLI's skill. Include 3–5 domain-specific phrases (e.g. for a finance CLI: "quote AAPL", "check my portfolio", "options for TSLA") and 2 generic phrases ("use <api-name>", "run <api-name>"). Domain verbs vary — don't just template "use X" variants.
11. All `narrative` fields are optional. Omit fields you can't populate honestly rather than emit filler. The generator falls back to generic content gracefully.
12. **Avoid hardcoded counts in narrative copy when the count tracks a runtime list.** A number embedded in `headline` or `value_prop` ("across N trusted sources", "from N retailers", "queries N vendors") propagates into root.go's Short/Long, the README, the SKILL, the MCP tools description, and `which.go` — every output surface that reads the narrative. When the underlying registry grows or shrinks, the count goes stale across all of those surfaces simultaneously, and a single-line edit to add a source requires hunting down ~10 hardcoded copies. Prefer plural-without-count phrasing ("across the major sources", "from a curated set of retailers") or describe the breadth qualitatively ("dozens of vendors") rather than committing to a specific integer. If a count is load-bearing for the value prop, keep the brief's narrative count-free and have the printed-CLI's README/SKILL author write the count once into a single hand-edited paragraph after generation — accepting that it will need a manual update whenever the registry changes.
13. **Use side-effectful examples only when they are the truthful workflow.** `validate-narrative --strict --full-examples` classifies `auth login`, `auth set-token`, `auth logout`, `auth setup`, `--launch`, and mutating `--apply` examples as side-effectful (see `isSideEffectfulNarrativeExample` in `internal/narrativecheck/narrativecheck.go`) and reports each as an `UNSUPPORTED` warning instead of executing it. These warnings do not fail strict aggregation, so it is valid to show an auth or apply command when that is the honest onboarding or bulk-operation shape. Prefer `doctor` or another read-only invocation as `quickstart[0]` when it teaches the same workflow, but do not strip a real auth or apply step just to appease shipcheck. Non-side-effect unsupported examples still fail strict mode when they cannot dry-run, and missing commands, empty command paths, and failed full examples remain failures.

**Pre-render framework-command check.** Before running `generate --research-dir`,
validate the framework command examples already present in `research.json`.
This catches stable template vocabulary mistakes while the fix is still a
single-file `research.json` edit, before README.md, SKILL.md, `.printing-press.json`,
root help, and other generated surfaces consume the bad narrative.

```bash
cli-printing-press validate-narrative --strict --framework-only \
  --research "$API_RUN_DIR/research.json"
```

If this reports `sync --entities`, `search --entities`, `search --types`, an
absolute date for `sync --since`, or another framework-command flag mismatch,
fix `research.json` now and rerun this check before generation. This is a
cheap floor, not a replacement for Phase 4 shipcheck: after the CLI exists,
`shipcheck` still runs `validate-narrative --strict --full-examples` against
the built binary to catch API-specific command paths, generated endpoint flags,
and runtime dry-run failures.

Also write discovery pages if browser-sniff was used. The generator reads these from `$API_RUN_DIR/discovery/browser-sniff-report.md` (which the browser-sniff gate already writes there). No additional action needed for discovery pages -- they are already in the right location.

### Priority inversion check (combo CLIs only)

**Only runs when `source-priority.json` exists from the Multi-Source Priority Gate.**

Before Phase Gate 1.5, tally the commands/features the manifest attributes to each named source. Compare against the confirmed priority ordering:

- If the primary source has **fewer** commands than any secondary source, this is a **priority inversion** — the free/primary-intent source got demoted because the secondary had more spec coverage.
- If the primary source has **zero** commands (all its features were dropped because it lacked a spec), this is a **hard inversion** — the primary was silently replaced.

When an inversion is detected, HALT before Phase Gate 1.5 and print:

> ⚠ **Priority inversion detected.**
>
> The confirmed primary is **<Source A>** but the manifest gives it <N> commands vs **<Source B>** (secondary) with <M> commands. This usually means the primary's discovery path (browser-sniff, community wrapper, HTML parser) didn't land, and the secondary's clean spec took over.
>
> The user said <Source A> is the headline. Shipping this manifest would invert their stated priority.

Then ask via `AskUserQuestion`:

1. **Re-run discovery for <Source A>** — loop back to Phase 1.7 browser-sniff or Phase 1.8 crowd-sniff for the primary source specifically.
2. **Accept the inversion** — the user explicitly confirms they're fine with the secondary leading. Record this in `source-priority.json` as `inversion_accepted: true`.
3. **Drop <Source B>** — remove the secondary from the manifest so it can't overshadow the primary.

Do not proceed to the prose showcase until this is resolved.

### Phase Gate 1.5

**STOP.** Present the absorb manifest to the user in two parts: a prose showcase, then a question.

The prose showcase and the `AskUserQuestion` are two separate turns. Print the showcase as a plain text reply with every novel feature spelled out, then call `AskUserQuestion` with four short options whose descriptions fit on one line each. The question text is one sentence; the user reads the showcase to decide and the options to act. Cramming the feature list into an option description collapses both turns into one and is the failure mode this gate exists to prevent.

**Part 1: Prose showcase (print before the AskUserQuestion)**

The showcase exists so the user can decide approve / trim / add ideas without asking a follow-up. Cover four things:

1. **Scope** — how many features absorbed across which tools, how many novel on top, how that stacks up against the best existing tool.
2. **Per-novel-feature readout** — one line each: feature name, what the user gets, and the specific evidence or persona that makes it worth building.
3. **Hand-code commitment** — of the M novel features, K will require hand-written Go after generate (each ~50-150 LoC plus `root.go` wiring). State the hand-code count and the auto-emitted count, then list the names of the hand-code features. The manifest transcendence table's `Buildability` column (populated from the subagent per [references/novel-features-subagent.md](references/novel-features-subagent.md) "Output contract") is the source of truth: count rows tagged `hand-code`; `spec-emits` rows are excluded from the hand-code total. Approving commits the agent to that scope, so the user must see it explicitly before the AskUserQuestion.
4. **Anything else the user should worry about before approving** — stubs, risky dependencies, expensive endpoints, low-confidence ideas.

Show every novel feature that scored ≥5/10. Group by theme if there are more than ~12; never hide features behind "Plus N more" or "see full manifest." If zero qualified, say so plainly: "No novel features scored high enough to recommend. The absorbed features cover the landscape well."

Format is otherwise yours — markdown headings, prose, a numbered list, whatever reads cleanly. The must-haves are the four things above and the ≥5/10 coverage rule.

**Part 2: AskUserQuestion**

> "Ready to generate with the full [N+M]-feature manifest? Or do you have ideas to add?"

Options (each description must be one short line — the showcase already did the explaining):
1. **Approve — generate now** — Start CLI generation with the full manifest
2. **I have ideas to add** — Tell me features from your experience, then we'll generate
3. **Review full manifest** — Show me every absorbed and novel feature before deciding
4. **Trim scope** — The feature count is too ambitious, let's focus on a subset

If user selects **"I have ideas to add"**, ask 3 structured questions targeting personal knowledge the research couldn't surface:

1. "Beyond the [M] ideas above, what workflows do YOU use `<API>` for that we might have missed?"
2. "What frustrates YOU about this API that the research didn't surface?"
3. "What's YOUR killer feature — something only you'd think of?"

If `USER_BRIEFING_CONTEXT` is non-empty, acknowledge it: "You mentioned [summary of their vision] at the start. Want to add more, or does the manifest already cover it?"

Each answer that produces a concrete feature → score and add to the transcendence table. After the brainstorm, return to this gate with the updated manifest.
WAIT for approval. Do NOT generate until approved.

---

## Phase 1.9: API Reachability Gate

**MANDATORY. Do NOT skip this phase. Do NOT proceed to Phase 2 without running this check.**

Before spending tokens on generation, verify the API actually responds to programmatic requests. One real HTTP call. If it fails, STOP.

**Exception for browser-clearance/browser-sniffed website CLIs:** If Phase 1.7 produced a successful browser capture and `$DISCOVERY_DIR/traffic-analysis.json` reports `reachability.mode` as `browser_clearance_http` or `browser_http`, a plain `curl` 403/429 is expected evidence, not a hard stop. In that case the reachability gate passes only if:
- the browser-sniff capture contains useful non-challenge traffic (real API, SSR data, structured HTML, RSS/feed data, or page-context fetch evidence), and
- Phase 2 will pass `--traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json"` so the generator can emit browser-compatible HTTP transport and, for `browser_clearance_http`, Chrome cookie import.

Do not treat a persistent browser sidecar as a shippable CLI runtime. Browsers are allowed for Printing Press discovery and reusable auth/clearance capture; ordinary printed CLI commands must replay through direct HTTP, Surf/browser-compatible HTTP, or stored reusable auth state. If traffic analysis reports `browser_required`, return to discovery to find a replayable HTTP/HTML/RSS/SSR surface or HOLD the run.

Useful same-site HTML document pages count as a replayable surface when they return real content, not challenge/login pages. Browser-sniff can promote these into `response_format: html` endpoints so generated commands extract page metadata and filtered links through Surf/direct HTTP instead of keeping a browser sidecar alive.

When hand-authoring a `response_format: html` spec with `html_extract.mode: links`,
document and choose `link_prefixes` as path-segment prefixes. A prefix `/items`
matches `/items` and `/items/...`, but not `/items123.html`; use the parent
directory prefix when the leaf segment has embedded IDs or suffixes. See
`skills/printing-press/references/spec-format.md` for the exact contract.

If the browser capture contained only challenge/login/error pages, this exception does not apply.

**Exception for LAN-only / mDNS-discovered APIs:** If the resolved spec's `base_url` is a localhost or loopback placeholder (`http://localhost:<port>`, `http://127.0.0.1:<port>`, or `http://[::1]:<port>`), or Phase 1 research explicitly identifies the API as LAN-only / SSDP / mDNS-discovered with no stable global origin, do not run the generic curl/WebFetch reachability probe. A probe from the generation host would test the agent's loopback or current network, not the user's appliance, speaker, bridge, or local service.

For this case, record a Phase 1.9 PASS carve-out in the research brief:

```markdown
## Reachability Gate
- Decision: PASS (carve-out)
- Reason: lan-only-no-global-url
- Evidence: <base_url or research line showing localhost, loopback, SSDP, mDNS, or LAN-only discovery>
```

Then proceed to Phase 2. Do not write a freeform manual proof for this case, do not call it a missing-API-key skip, and do not use this carve-out for normal public/cloud origins such as `https://api.example.com`; those still run the reachability probe and decision matrix below.

### The Check

Prefer the spec's `auth.verify_path` when it is set; otherwise pick the simplest GET endpoint from the resolved spec (no required params, no auth if possible). If no such endpoint exists, use the spec's base URL. Run one HTTP request and preserve the response body when the server returns a 4xx:

```bash
body_file="$(mktemp "${TMPDIR:-/tmp}/pp-reachability-body.XXXXXX")"
trap 'rm -f "$body_file"' EXIT
status="$(curl -s --max-filesize 65536 -o "$body_file" -w "%{http_code}" -m 10 "<base_url>/<simplest_get_path>" 2>/dev/null || true)"
case "$status" in
  [0-9][0-9][0-9]) ;;
  *) status="000" ;;
esac
printf '%s\n' "$status"
```

Or use `WebFetch` if curl is unavailable. Record the response status and, for any 4xx response body, run the same tier/permission keyword scan against the captured WebFetch body text before deciding. The goal is one real response code plus any 4xx body evidence the API chose to return.

If `status` is any 4xx, inspect the body before deciding. Search it case-insensitively for tier or permission terms:

```bash
grep -Ei 'tier|allowed|permitted|subscription|quota|plan|scope|limit|permission|forbidden|unauthorized|upgrade|trial' "$body_file" | head -20
```

When matched lines are present, add them to the Phase 1 research brief under:

```markdown
## Reachability Risk
- Tier/permission hints from 4xx body: "<matched line, truncated if needed>"
```

Keep the evidence bounded: include only the lines that explain the access model, trim each line to a readable length, and do not paste bearer tokens, API keys, cookies, or unrelated full response dumps. If the GET returns 2xx/3xx, omit this tier-hint subsection.

Do not probe arbitrary mutation endpoints to discover tier limits. A generic "try a PUT/POST/PATCH/DELETE" rule can create accounts, send messages, capture payments, or mutate user data. Mutation probing is allowed only when the resolved spec or OpenAPI operation explicitly marks that endpoint as probe-safe with `x-pp-safe-probe: true`; the endpoint must be idempotent or otherwise harmless for the real account being used. If no endpoint has that explicit marker, stop after the GET body capture above.

If one or more probe-safe endpoints are declared and the user provided credentials, run exactly one declared probe-safe endpoint as a second reachability probe and apply the same 4xx body capture and tier-keyword extraction. When more than one exists, choose the lowest-risk declared endpoint by preferring methods in this order: HEAD/OPTIONS/GET, then PUT/PATCH, then POST, then DELETE only if it is the only declared safe option. Break ties by choosing the endpoint with the fewest required parameters and avoiding paths with account, billing, payment, deletion, or notification terms when any safer declared option exists. Record which endpoint was probe-safe in the brief so later phases know the evidence came from an opt-in safe probe.

### OAuth2 Grant Probe

For OAuth2 Authorization Code + PKCE CLI implementation/review requirements, read [references/oauth2-pkce-cli-checklist.md](references/oauth2-pkce-cli-checklist.md).

If the resolved spec declares `auth.type: oauth2` and has an interactive
authorization URL (`authorizationCode` or `implicit` flow in OpenAPI, or an
equivalent internal YAML auth field), the generic reachability check is not
enough. After the base URL check would otherwise pass, verify the OAuth grant
entry point with the user's real public OAuth input before Phase 2. This probe
is read-only: it stops at the provider's consent, login, or error page and does
not exchange a code, request a token, or ask the user to approve consent.

Do not run this grant probe for OAuth2 `client_credentials` flows that only have
a token URL. Those are server-to-server credentials, not browser grant flows, and
probing the token endpoint would require secret material or a write-like auth
attempt. The base reachability check plus later mock/live auth verification cover
that shape.

**Required inputs:** Use the `client_id` env var or public auth-flow input
already resolved during Phase 0.5 and Pre-Generation Auth Enrichment. If the
spec exposes `x-auth-vars`, prefer the entry with `kind: auth_flow_input`,
`sensitive: false`, and a name or description identifying it as the OAuth
`client_id`. If the real client id is missing, HOLD before generation and tell
the user exactly which env var to set. Do not substitute a fake client id; fake
ids can produce provider-specific errors that look like transport quirks.

Build the authorize URL from the resolved spec, not from a guessed provider
default:

- `client_id`: the real public client id from the env var above.
- `redirect_uri`: the redirect URI declared in the spec or auth metadata.
- `response_type=code` for authorization-code grants, or the spec's documented
  response type for implicit grants.
- For authorization-code grants, include a safe probe PKCE pair using `S256`.
  Use `probe_reachability_check_pkce_probe_literal` as the code verifier and
  compute the URL-safe SHA-256 challenge from it. The verifier is 43 unreserved
  characters, satisfying the RFC 7636 minimum; providers that do not require
  PKCE ignore these params, and providers that enforce PKCE should advance to
  the login or consent page instead of returning a false `invalid_request`.
- `scope`, `audience`, `tenant`, `state`, `prompt`, or other provider-required
  params when the spec or vendor docs require them. Use a benign probe value for
  `state` if required.

Use a redirect-limited GET and inspect the final URL, response body, and
response class:

```bash
PKCE_VERIFIER="probe_reachability_check_pkce_probe_literal"
PKCE_CHALLENGE=$(printf "%s" "$PKCE_VERIFIER" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
AUTH_URL="<authorization_url_with_required_query_params>"
# Add code_challenge_method=S256 and code_challenge=$PKCE_CHALLENGE to AUTH_URL.
PROBE_BODY_AND_META=$(curl -sS -L --max-redirs 10 -m 15 -w "\n%{http_code} %{url_effective}" -o - "$AUTH_URL" 2>/dev/null)
PROBE_META=$(printf "%s\n" "$PROBE_BODY_AND_META" | tail -n 1)
PROBE_BODY=$(printf "%s\n" "$PROBE_BODY_AND_META" | sed '$d')
printf "%s\n" "$PROBE_META"
printf "%s\n" "$PROBE_BODY" | head -c 8000
printf "\n"
```

Interpret the result before Phase 2:

| OAuth probe result | Action |
|--------------------|--------|
| HTTP status is `2xx` or `3xx`, final URL stays on the provider's authorization/login/consent host, does not include `error=`, and the response body does not contain an OAuth error code (`invalid_request`, `invalid_client`, `unauthorized_client`, etc.) | **PASS** - the grant entry point is reachable; proceed to Phase 2 |
| Final URL or response body reports `invalid_request`, `invalid_client`, `redirect_uri_mismatch`, `unauthorized_client`, `unsupported_response_type`, or equivalent | **HARD STOP** - OAuth config is misconfigured; surface the provider error and point the user to the mismatched client id, redirect URI, app type, tenant, or required scope |
| HTTP status is `4xx` or `5xx` without a recognizable OAuth error code | **WARN** - flag provider-specific routing or login-shell behavior for manual review before generation |
| Final URL lands on a generic non-OAuth error page, marketing page, or unrelated login landing page | **WARN** - flag endpoint ambiguity or provider-specific routing for manual review before generation |
| Timeout/DNS/connection refused or HTTP status `000` | **WARN** - same handling as the generic reachability WARN |

On HARD STOP, do not generate. Present a specific, provider-neutral message:

> "WARNING: `<API>`'s OAuth authorize probe failed before generation. The
> provider returned `<error_or_final_url>`. Check that the spec's
> `authorization_url`, `redirect_uri`, `response_type`, client id env var, app
> type, tenant, and required scopes match the registered OAuth application."

This OAuth probe is additive to the base reachability gate. Non-OAuth APIs
(`api_key`, `bearer_token`, `cookie`, `composed`, `session_handshake`, `none`)
skip it entirely.

**If the check returns 403/429 with bot-protection evidence and `probe-reachability` has not already run for this URL during Phase 1.7's Direct HTTP challenge rule, run it now before consulting the decision matrix:**

```bash
cli-printing-press probe-reachability "<base_url>" --json
```

The matrix below references `probe-reachability` `mode` for the bot-detection rows. If the probe already ran in Phase 1.7, reuse that result; do not re-probe.

### Decision Matrix

| Result | Browser capture result | Traffic-analysis reachability | Action |
|--------|------------------------|-------------------------------|--------|
| 2xx/3xx | Any | Any | **PASS** - proceed to Phase 2 |
| 401 (no key provided) | Any | Any | **PASS** - expected when API needs auth and user declined key gate |
| 403/429 with HTML/bot detection | `probe-reachability` returned `browser_http` | runtime is `browser_http` (Surf) | **PASS** - the printed CLI will ship Surf transport which clears the protection. No clearance cookie capture in the printed CLI, regardless of whether browser-sniff also ran for endpoint discovery |
| 403/429 with HTML/bot detection | Successful useful capture | `browser_http` or `browser_clearance_http` | **PASS** - proceed with browser-compatible HTTP / clearance strategy |
| Any | Capture only works through a live page context | `browser_required` | **HOLD** - find a lighter replayable surface before Phase 2 |
| 403/429 with HTML/bot detection | No browser capture attempted but browser-sniff approved/pre-approved AND `probe-reachability` returned `browser_clearance_http` or `unknown` | Any | **RETURN TO PHASE 1.7** - attempt cleared-browser capture before pivoting scope |
| 403/429 with HTML/bot detection | Capture contains only challenge/error pages | Any | **HARD STOP** |
| 403 | No successful useful capture | Research found 403 issues | **HARD STOP** |
| 403 | No successful useful capture | No 403 research issues | **WARN** - ask user |
| Timeout/DNS/connection refused | Any | Any | **WARN** - ask user |

### On HARD STOP

Present via `AskUserQuestion`:

> "WARNING: `<API>` appears to block programmatic access. [what failed: e.g., 'HTTP 403 with HTML error page', 'browser-sniff gate failed with bot detection', 'reteps/redfin has 6+ issues about 403 errors']. Building a CLI against an unreachable API wastes time and tokens."
>
> 1. **Try anyway** - proceed knowing the CLI may not work against the live API
> 2. **Pick a different API** - start over
> 3. **Done** - stop here

### On WARN

Present via `AskUserQuestion`:

> "The API returned [error]. This might be temporary, or it might mean programmatic access is blocked. Want to proceed?"
>
> 1. **Yes - proceed** - generate the CLI anyway
> 2. **No - stop** - pick a different API or provide a spec manually

### On PASS

Proceed silently to Phase 2.

---

## Phase 2: Generate

### Pre-Generation Category Enrichment

Before generating, set the spec's top-level `category` before running
`generate`. The category must come from the Phase 1 research brief's domain
judgment, mapped to the public-library category enum. For internal YAML specs,
add:

```yaml
category: <public-library-category>
```

If the source is an OpenAPI file and the workflow has an editable overlay or
derived internal spec, carry the same top-level category into that generated
spec artifact before the final `generate` invocation. If there is no editable
spec artifact, such as direct `--docs` generation, pass
`--category <public-library-category>` on the final `generate` invocation. Do not add
the category after generation just to satisfy publish; the generated manifest,
README, and SKILL install section must all come from the same category-aware
spec, or `verify-skill canonical-sections` can drift.

### Pre-Generation Cache Enrichment

Before generating, decide whether the spec should opt into generator-owned cache
freshness. The generator already has the freshness helpers and auto-refresh hook,
but it emits them only when the spec declares `cache.enabled: true` and the CLI
has a real sync path. Stateful CLIs otherwise serve local data
exactly as it was last synced, which caps the cache freshness score and can leave
agents reading stale SQLite rows without a warning.

Enable cache freshness only when the resolved spec, profiler output, or absorb
manifest shows at least one covered read path backed by a syncable resource that
`sync` can refresh from the upstream API before serving. Do not enable it from
Phase 1 research notes or scorecard goals alone. Leave it disabled for stateless
read-through wrappers and for local stores that are primarily per-user working
state, such as carts, drafts, or other session-owned data where a pre-read
refresh could replace the user's local state with a different snapshot. Also
leave it disabled for quota-metered, paid, rate-limited, or expensive bulk
refresh APIs unless the refresh path is cheap, bounded, and clearly valuable;
those CLIs should rely on manual `sync` plus the generated `doctor` cache report
instead of surprising users with pre-read upstream calls.

For internal YAML specs, add the cache block before the final `generate`
invocation only when at least one generated syncable resource read command will
be covered automatically, or `cache.commands` will register a real hand-authored
store-reading command:

```yaml
cache:
  enabled: true
  stale_after: 168h        # choose a domain-appropriate default
  refresh_timeout: 30s     # optional; blank uses the generated runtime default
```

Generated resource list/get/search commands are covered automatically from the
syncable resources profile. Use `cache.commands` only for hand-authored novel
commands that read the local store and are not generated resource commands. The
command `name` is the Cobra path without the binary name, and every listed
resource must be declared in `resources:` and classified as syncable.

```yaml
cache:
  enabled: true
  stale_after: 168h        # choose a domain-appropriate default
  commands:
    - name: <novel-read-command>
      resources: [<resource-name>]
```

Pick `stale_after` from the domain's update cadence: shorter for live feeds or
rapidly changing inventory, longer for reference catalogs and archival data. Do
not enable cache just to satisfy the scorecard if there is no upstream refresh
path or no user value in pre-read freshness; the generator intentionally skips
the helpers when they would be dead code.

### Pre-Generation Auth Enrichment

Before generating, check whether the resolved spec has auth. This matters most for
browser-sniffed and crowd-sniffed specs where the mechanical auth detection may have failed
(e.g., session expired during browser-sniff, SDK didn't expose auth patterns).

**Check the spec:**
- For internal YAML specs: look for `auth:` section with `type:` not equal to `"none"`
- For OpenAPI specs: look for `components.securitySchemes` or `security` sections

**If auth is missing** (`type: none` or no auth section) AND Phase 1 research found
auth signals, enrich the spec before generation:

1. Check the research brief for auth mentions (Bearer, API key, token, cookie, OAuth)
2. Check Phase 1.5a MCP source code analysis for auth patterns (header names, token formats)
3. Check Phase 1.6 Pre-Browser-Sniff Auth Intelligence results (if the user confirmed auth)

If any source identified auth, **edit the spec YAML** to add the auth section before
running generate. For internal YAML specs:

```yaml
auth:
  type: bearer_token    # or api_key, depending on what research found
  header: Authorization # or the specific header from MCP source
  in: header
  env_vars:
    - <API_NAME>_TOKEN  # bearer_token → _TOKEN, api_key → _API_KEY
```

When research or source metadata names a real single-token env var, record it
in `research.json` as `auth.canonical_env_var`; fresh generation reads that
name first and keeps the parser-derived env var as a trailing fallback. When
you are editing an internal YAML spec directly, use only the canonical name in
`env_vars`; do not add guessed slug-based aliases.

For OpenAPI specs, choose the security scheme by wire format, not by whether
the token feels like an API key. Use `type: http` with `scheme: bearer` when
the upstream API sends `Authorization: Bearer <token>`, including PAT-shaped
tokens such as Slack `xoxp`, Notion integration tokens, Linear API keys, and
GitHub PATs. Use `type: apiKey` only when the API sends the configured value
as the raw header or query value, such as `X-API-Key: <token>` or
`Authorization: <token>` with no scheme prefix. The generator adds the
`Bearer ` prefix for `http` bearer schemes; `apiKey` sends exactly the
configured value and will not add a prefix.

Quick test: if upstream docs or live traffic show `Authorization: Bearer
<token>`, model it as `http` bearer. If they show `X-API-Key: <token>`,
`?api_key=<token>`, or `Authorization: <token>` with no scheme prefix, model it
as `apiKey`.

For OpenAPI specs, prefer `x-auth-env-vars` on the selected security scheme
when the wrapper slug differs from the underlying API brand.

**If auth IS present** in the spec but Phase 1 evidence shows the slug-derived
env var will differ from the canonical name users have already set for this
API, enrich the spec with the canonical name before generation. The
slug-derivation rule (security-scheme slug uppercased plus `_TOKEN` /
`_API_KEY` / `_OAUTH2` per type) rarely matches the canonical name for
established APIs. Common shapes:

- Stripe (bearer): canonical `STRIPE_SECRET_KEY`, not slug-derived `STRIPE_OAUTH2`
- HubSpot (bearer): canonical `HUBSPOT_PRIVATE_APP_TOKEN`, not slug-derived `HUBSPOT_API_KEY`
- Twilio (HTTP Basic, two-var pair): canonical `TWILIO_ACCOUNT_SID` + `TWILIO_AUTH_TOKEN`, not slug-derived `TWILIO_USERNAME` + `TWILIO_PASSWORD`
- Keap (OAuth2 authorization-code): canonical `KEAP_SERVICE_ACCOUNT_KEY`, not slug-derived `KEAP_OAUTH2`

Walk through:

1. Compute the slug-derived env var the generator will pick (security-scheme
   slug, uppercased, plus the type-suffix above; HTTP Basic produces a
   `_USERNAME` + `_PASSWORD` pair; OAuth2 `client_credentials` produces a
   `_CLIENT_ID` + `_CLIENT_SECRET` pair).
2. Check Phase 1 research, Phase 1.5a MCP source code analysis, and community
   wrapper READMEs for a canonical env var name documented by the vendor or
   in widespread use.
3. If they differ and the canonical name is a single-token credential, record
   it in `research.json` as `auth.canonical_env_var`. The generator will read
   the canonical name first and retain the slug-derived form as a fallback.
   If you are editing the source spec directly instead, add `x-auth-env-vars`
   on the selected security scheme (OpenAPI) or set `auth.env_vars` to the
   canonical name (internal YAML). Use only the canonical name in the spec
   edit; do not retain guessed aliases there. For HTTP Basic, supply the full
   two-entry canonical pair (username position first, password position
   second) via `x-auth-env-vars`. For OAuth2
   `client_credentials`, the parser silently re-applies the
   `CLIENT_ID`/`CLIENT_SECRET` default when `x-auth-env-vars` has fewer
   than two entries (see `applyAuthEnvVarDefaults` in
   `internal/openapi/parser.go`); if the canonical secret is a single
   service-account token for a `client_credentials` flow, use
   `x-auth-vars` instead (next section) so the override is preserved.
4. If research surfaces no canonical name distinct from the slug-derived
   form, do nothing. The slug-derived name is fine, and a spurious
   `x-auth-env-vars` would just shadow it with the same value.

```yaml
# Bearer / API-key single-token case (Stripe, HubSpot, Keap on
# authorization-code grant, most apiKey schemes).
components:
  securitySchemes:
    keapOAuth2:
      type: oauth2
      flows:
        authorizationCode: { ... }
      x-auth-env-vars:
        - KEAP_SERVICE_ACCOUNT_KEY
```

```yaml
# HTTP Basic two-var canonical pair (Twilio).
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
      x-auth-env-vars:
        - TWILIO_ACCOUNT_SID
        - TWILIO_AUTH_TOKEN
```

Skipping this step pushes the agent into hand-patching
`internal/config/config.go` Load and `internal/cli/doctor.go` env-var
checks after a `doctor` FAIL against the operator's real environment.
Enriching the spec avoids that round-trip.

For OpenAPI bearer-token specs that need richer env-var metadata (kind
classification, optional credentials, OR-group relationships), keep the
security scheme as `http` bearer and put `x-auth-vars` on that scheme. Do not
switch to `apiKey` just to attach the richer metadata.

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: xoxp
      x-auth-vars:
        - name: SLACK_BOT_TOKEN
          kind: per_call
          required: false
          sensitive: true
          description: Set this OR `SLACK_USER_TOKEN` for workspace API calls.
        - name: SLACK_USER_TOKEN
          kind: per_call
          required: false
          sensitive: true
          description: Set this OR `SLACK_BOT_TOKEN` for user-scoped API calls.
```

For OpenAPI raw-key schemes that need richer env-var metadata, keep `apiKey`
and place `x-auth-vars` on the raw-key scheme.

```yaml
components:
  securitySchemes:
    rawHeaderKey:
      type: apiKey
      in: header
      name: X-API-Key
      x-auth-vars:
        - name: <API_NAME>_API_KEY
          kind: per_call
          required: true
          sensitive: true
          description: Raw API key header value.
```

See `docs/SPEC-EXTENSIONS.md` for the canonical `x-auth-vars` schema.

`kind` controls who supplies the value:
- `per_call` is the default user-supplied credential used by normal commands.
- `auth_flow_input` is only needed during `auth login`.
- `harvested` is populated by the auth login flow into local config.

`sensitive: true` means credential material that must be redacted in logs and
agent context. Use `sensitive: false` for public configuration, such as an OAuth
`client_id`.

Encode AND/OR relationships with each var's `required` flag plus `description`
text. There is no first-class group syntax. For OR cases, mark each alternative
`required: false` and name the other option in the description. For AND cases,
mark each required member `required: true`.

The parser auto-classifies cookie schemes as `harvested` and OAuth2
`client_credentials` inputs as `auth_flow_input`. Add `x-auth-vars` only when
overriding those defaults or resolving multi-scheme ambiguity.

For OpenAPI specs, add an `info.description` mention if one doesn't exist — the
parser's `inferDescriptionAuth` will detect it automatically.

**Why enrich before generation, not after:** The generator's templates (config, client,
doctor, auth, README) all read `Auth.*` fields from the spec. Patching config.go after
generation only fixes env var support — it misses the doctor auth check, client auth
header, README auth section, and auth command setup. Enriching the spec means every
template produces correct auth from the start.

**When to skip:** If the API genuinely doesn't need auth (ESPN public endpoints, weather
APIs, public data feeds), don't invent auth. The signal must come from research — not
from guessing. No research mention of auth = no enrichment.

#### Public Parameter Name Enrichment

Before generating, inspect endpoint params and body fields whose API names would
make poor CLI or MCP inputs, especially one-letter keys, punctuation-heavy keys,
or names that only make sense inside the upstream protocol. When clear evidence
shows the user-facing meaning, author `flag_name` in the internal spec or overlay
and add `aliases` only for compatibility spellings.

This is an agent judgment step, not a generator inference step. The skill uses
research, docs, SDK/source names, browser-sniff form labels, traffic-analysis
request context, and endpoint workflow evidence to decide the semantic name.
The Printing Press CLI validates and propagates that authored data through Cobra
flags, generated examples, typed MCP schemas, and `tools-manifest.json`; it must
not guess that a cryptic key always means the same thing across APIs.

Run the deterministic inventory before generation when a spec may contain
cryptic wire names:

```bash
cli-printing-press public-param-audit --spec <spec-or-overlay-output> --ledger <runstate>/public-param-audit.json --strict
```

The command does not decide that every finding needs a public flag. It identifies
parameters that need an agent decision. For each pending finding, either:

- Author `flag_name` and compatibility `aliases` in the spec or overlay, then rerun
  the audit so the finding becomes resolved from the spec itself.
- Record `decision: "skip"` in the ledger with `source_evidence` and `skip_reason`
  when source material shows no public rename is warranted.

A ledger entry with `decision: "flag_name"` or `proposed_flag_name` is only a note
to the agent; it is not complete until the spec or overlay actually contains the
public name. Strict mode fails on unreviewed findings, not on evidence-backed
skips. A one-letter wire name alone is enough to enter the inventory, but not
enough to author a rename.

Good evidence:
- Explicit parameter descriptions, vendor docs, or SDK argument names.
- Browser-sniff form/input labels and the interaction that produced the request.
- Traffic-analysis context tying the parameter to a specific endpoint workflow.
- Existing manuscript notes or reviewed examples that use the same task-level term.

Bad evidence:
- A one-letter name alone.
- Generic descriptions such as "query parameter" or "string value".
- Ambiguous sample values with no endpoint context.
- Global assumptions such as "`s` means search" or "`c` means city".

Prefer concise task-level names an agent would naturally use on that command. For
a store-locator endpoint with `s` described as `Street address` and `c` described
as `City, state, zip`, author:

```yaml
params:
  - name: s
    flag_name: address
    aliases: [s]
    description: Street address
  - name: c
    flag_name: city
    aliases: [c]
    description: City, state, zip
```

The upstream wire keys remain `s` and `c`; generated users and agents see
`address` and `city`. If the evidence is unclear after research, leave
`flag_name` unset, record the reviewed source material and evidence gap in the
audit ledger, and preserve the gap in the manuscript rather than inventing a
friendly name.

#### Free/Paid Tier Routing Enrichment

If Phase 1 finds that the headline commands should stay free but secondary
enrichment needs a paid key, declare tier routing in the spec before generation.
Do this only when research identifies a real split; do not invent tiers for a
single-auth API.

Detection signals:
- Research or source-priority notes say the primary source is free but a secondary
  source needs a paid/API key.
- Some endpoints are documented as public while adjacent enrichment endpoints are
  documented as paid, partner, premium, or quota-gated.
- Browser/crowd sniffing found public endpoints and SDK/MCP research found a
  separate credential for expanded coverage.
- Sniffed specs carry per-endpoint `observed_auth` (a list of lowercased request
  header names observed during capture, e.g. `[authorization]` or `[x-api-key]`).
  An empty or missing `observed_auth` on an endpoint is evidence that the request
  went anonymously; a populated list is evidence the endpoint required auth. The
  same per-endpoint signal is mirrored on `TrafficAnalysis.endpoint_clusters[].observed_auth`
  in the traffic-analysis sidecar. Treat both as observation-only — not a security
  scheme declaration — and corroborate with documentation before declaring a tier.

Action:
- Internal YAML: add `tier_routing` plus `tier` on the affected resource or
  endpoint.
- OpenAPI: add `x-tier-routing` at the root or under `info`, and add `x-tier`
  to the path item or operation.
- Use `auth.type: none` for the free tier.
- Use only `api_key` or `bearer_token` for credential tiers in v1.
- If a credential tier uses a different `base_url`, it must be HTTPS and same
  host-family unless `allow_cross_host_auth: true` records explicit review.
- Do not combine `no_auth: true` or OpenAPI `security: []` with a credential tier.

Skip when:
- All useful commands require the same credential.
- The paid source would become the primary headline surface instead of enrichment.
- The auth split requires OAuth, cookie, composed, or session-handshake tier auth;
  handle that as a normal auth-mode decision for now.

Example:

```yaml
tier_routing:
  default_tier: free
  tiers:
    free:
      auth: {type: none}
    paid:
      auth:
        type: api_key
        in: query
        header: api_key
        env_vars: [EXAMPLE_PAID_KEY]
resources:
  search:
    tier: free
    endpoints:
      list:
        method: GET
        path: /search
      enrich:
        method: GET
        path: /paid/search
        tier: paid
```

#### Tagging endpoints `no_auth: true` (composed/cookie auth APIs)

For APIs whose `auth.type` is `cookie`, `composed`, or `session_handshake` — i.e.,
auth that requires interactive setup (browser cookie capture, multi-step token
exchange) — audit each endpoint individually for whether it actually needs
authentication. The default of `no_auth: false` means "auth required"; flip it to
`no_auth: true` for endpoints that work without credentials.

Typical unauthenticated endpoints worth tagging:

- **Auth-flow primitives:** login, registration, password-reset, email-confirm,
  refresh-token, OAuth callback. The user isn't authenticated when calling these —
  they ARE the auth flow.
- **Public discovery:** store/location finder, menu browse, public listings,
  category listing, public search, public product detail.
- **Health/metadata:** health checks, version probes, capability flags, sitemap.

Why this matters: the `no_auth` count drives downstream decisions. Specifically,
a composed-auth API with zero `no_auth: true` tags previously got labeled
`mcp_ready: cli-only` and was suppressed from MCPB manifest emission, which
broke the Claude Desktop install path entirely. The current generator
(post-2.5) ships a manifest regardless, but the readiness label, scorecard
breadth dimension, and SKILL.md prose all read better when the count
reflects reality.

If unsure whether an endpoint requires auth, the safe default is `no_auth: false`
(auth required) — over-tagging can mislead users to expect tools that won't work.

**Example for a composed-auth pizza-ordering API:**

```yaml
resources:
  account:
    endpoints:
      register:
        method: POST
        path: /account/register
        no_auth: true   # registering means you're not yet authenticated
      login:
        method: POST
        path: /account/login
        no_auth: true   # the auth flow itself
      profile:
        method: GET
        path: /account/profile
        # no_auth defaults to false — needs auth to view your own profile
  stores:
    endpoints:
      find:
        method: GET
        path: /stores/near
        no_auth: true   # public store finder
  cart:
    endpoints:
      checkout:
        method: POST
        path: /cart/checkout
        # no_auth defaults to false — placing an order needs auth
```

#### Cookie/composed HTML transport

For specs with `auth.type: cookie` or `auth.type: composed` and any
`response_format: html` endpoint, treat browser fingerprint compatibility as
the safe default. The generator emits Surf-backed Chrome transport for that
shape unless the spec explicitly says `http_transport: standard`.

Before setting an explicit standard opt-out, run
`cli-printing-press probe-reachability` against a representative HTML GET endpoint.
If the probe returns `standard_http`, record `http_transport: standard` in the
spec. If it returns `browser_http`, leave the default or set `http_transport:
browser-chrome`. If it returns `browser_clearance_http`, return to the
browser-clearance flow above so the generated CLI has both browser-compatible
HTTP and reusable browser auth proof.

For cookie/composed-auth CLIs, recommend the `press-auth` companion binary —
it captures cookies once via a controlled Chrome window and serves them to
generated CLIs on demand, avoiding the on-disk session-cookie blind spot
that breaks `auth login --chrome` against a daily Chrome profile. See
[references/auth-companion.md](references/auth-companion.md) for the
recommendation flow, install command, and debug playbook.

### Pre-Generation MCP Enrichment

Before generating, count the spec's MCP tool surface and decide whether to opt
into the spec's `mcp:` enrichment fields. This matters most for medium-to-large
APIs (>30 tools) where the default endpoint-mirror surface scores poorly on the
scorecard's MCP architectural dimensions and burns agent context at runtime.

**Why before generation, not after:** the generator emits the MCP server's
`main.go`, `tools.go`, `intents.go`, `code_orch.go`, `tools-manifest.json`, and
README MCP section from the spec at generate-time. Patching after generation
fragments across 4+ files, won't be byte-identical, and the polish skill cannot
fix it (polish doesn't re-run generation). Enriching the spec means every
template emits the right surface from the start.

**Count the tool surface.** Keep two counts separate:

1. **Typed endpoints** — count `endpoints` across all `resources` (and
   `sub_resources`) in the spec. These become per-endpoint MCP tools at
   generate-time. This is the only count that selects the >50 automatic
   Cloudflare pattern and the only surface `mcp.orchestration: code` collapses.
2. **Cobratree-walked tools** — the runtime walker registers user-facing Cobra
   commands as MCP tools. Estimate as: `extra_commands` count + ~13 framework
   tools that ship by default (sql, search, context, sync, stale, doctor,
   reconcile, etc., minus framework-skipped). When novel features are planned,
   add their estimated command count. These tools still register in code
   orchestration mode so novel commands remain agent-reachable.

The total startup-visible tool count is the visible typed-endpoint tools plus
cobratree-walked tools and explicit framework tools. Use it to estimate runtime
context pressure, but do not treat cobratree tools as reducible by
`mcp.orchestration: code`.

**Decision table:**

| Typed endpoint count | Action |
|----------------------|--------|
| <30 | Skip — default endpoint-mirror surface is fine unless the cobratree count is unusually high. |
| 30–50 | Ask the user. Suggest `mcp.transport: [stdio, http]` for remote reach; suggest `mcp.intents` if there are clear multi-step workflows. If cobratree-walked tools dominate the total, say that code orchestration will not shrink them. |
| >50 | The generator auto-applies the Cloudflare pattern (transport + code orchestration + hidden endpoint tools) unless `mcp.orchestration` / `x-mcp.orchestration` is explicitly set. This collapses typed endpoint mirrors, not the runtime command mirror. |

**Mandatory >50 endpoint-tools confirmation.** If the pre-generation count
predicts more than 50 endpoint tools, expect `generate` to print an informational
line beginning `info: applied Cloudflare MCP pattern`. This is the intended
default and does not require a blocking question. Before verification, polish,
dogfood, or publish, confirm the generated MCP surface is the thin
`<api>_search` + `<api>_execute` pair plus any cobratree-walked command tools.
If the user explicitly wants raw endpoint tools past the threshold, set
`mcp.orchestration: endpoint-mirror` (internal YAML) or
`x-mcp.orchestration: endpoint-mirror` (OpenAPI) before regenerating.

**The Cloudflare pattern** (default for large surfaces without explicit
orchestration) — the generator applies this shape automatically. Add the spec
block only when you need to make the choice explicit or preserve it across
older generator versions:

```yaml
mcp:
  transport: [stdio, http]    # remote-capable; reaches hosted agents
  orchestration: code         # thin <api>_search + <api>_execute pair
  endpoint_tools: hidden      # suppress raw per-endpoint mirrors
  intents:                    # optional; named multi-step intents
    - name: fetch_and_summarize
      description: Fetch an item then summarize it
      params:
        - name: item_id
          type: string
          required: true
          description: item identifier
      steps:
        - endpoint: items.get
          bind:
            id: ${input.item_id}
          capture: item
        - endpoint: items.summarize
          bind:
            body: ${item.body}
          capture: summary
      returns: summary
```

`mcp.transport: [stdio, http]` adds HTTP streamable transport so cloud-hosted
agents (Managed Agents, web clients) can connect. `mcp.orchestration: code`
emits the thin search+execute pair that covers the typed-endpoint surface in
~1K tokens.
`mcp.endpoint_tools: hidden` removes the raw per-endpoint tools that would
otherwise still show up alongside the orchestration pair.

For command-dominant CLIs where cobratree-walked tools greatly outnumber typed
endpoints, do not present code orchestration as the context-reduction remedy for
the command mirror. Keep novel commands reachable by default, and reduce that
surface only with deliberate per-command `cmd.Annotations["mcp:hidden"] =
"true"` opt-outs or framework-command classification in
`internal/mcp/cobratree/classify.go.tmpl`.

For OpenAPI input specs, declare these fields under `x-mcp:` at the document
root (OpenAPI 3.0 `x-*` vendor extensions). The shape is identical to the
internal-YAML `mcp:` block above — same field names, just nested under a
vendor-extension key. See [`docs/SPEC-EXTENSIONS.md`](../../docs/SPEC-EXTENSIONS.md) for the canonical
schema and `info`-level placement option.

**Smaller-surface variants:**

- Just want remote reach? Small APIs (at or under
  `spec.DefaultRemoteTransportEndpointThreshold` typed endpoints) get `[stdio, http]`
  by default — no spec edit needed. Set `mcp.transport: [stdio, http]` explicitly only
  when the API is above the threshold and still wants remote reach.
- Have 3–5 obvious multi-step workflows but <50 endpoints? Add `mcp.intents`
  without code orchestration; leave `endpoint_tools` at default (visible).

**When to skip entirely:** small APIs (<30 tools), one-shot specs that won't
be installed as MCP servers, or APIs where the user explicitly opts out of MCP
enrichment.

**Verifying after generation:** the scorecard's `mcp_remote_transport`,
`mcp_tool_design`, and `mcp_surface_strategy` dimensions reflect the choices
above. A correctly enriched spec for a >50 tool API should score 10/10 on all
three. If polish later reports these dims weak, that's a sign this enrichment
step was skipped — re-run generation with the enriched spec rather than
trying to fix it in polish.

### Lock and Generate

Before running any generate command, acquire the build lock:

```bash
cli-printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE"
```

If acquire fails (another session holds a fresh lock), present the lock status to the user and let them decide: wait, use a different CLI name, force-reclaim, or pick a different API.

The `--category <public-library-category>` flag shown below is for runs whose
category was not already authored into an editable spec.

`--lenient` stubs missing local `#/components/schemas/<Name>` refs as
permissive object schemas with warnings so converted OpenAPI specs can still
generate. Add `--strict-refs` only when a run must fail instead of accepting
those local schema stubs; it does not change the rest of lenient cleanup.

OpenAPI / internal YAML:

```bash
cli-printing-press generate \
  --spec <spec-path-or-url> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --force --lenient --validate
```

Browser-browser-sniff-enriched (original spec + browser-sniff-discovered spec):

```bash
cli-printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --spec-source browser-sniffed \
  --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" \
  --force --lenient --validate
# If proxy pattern was detected during browser-sniff, add:
#   --client-pattern proxy-envelope
```

Sniff-only (no original spec, browser-sniff was the primary source):

```bash
cli-printing-press generate \
  --spec "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --spec-source browser-sniffed \
  --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" \
  --force --lenient --validate
# If proxy pattern was detected during browser-sniff, add:
#   --client-pattern proxy-envelope
```

Crowd-browser-sniff-enriched (original spec + crowd-discovered spec):

```bash
cli-printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --force --lenient --validate
```

Crowd-sniff-only (no original spec, crowd-sniff was the primary source):

```bash
cli-printing-press generate \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --force --lenient --validate
```

Both browser-sniff + crowd-sniff (merged with original):

```bash
cli-printing-press generate \
  --spec <original-spec-path-or-url> \
  --spec "$RESEARCH_DIR/<api>-browser-sniff-spec.yaml" \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --traffic-analysis "$DISCOVERY_DIR/traffic-analysis.json" \
  --force --lenient --validate
```

Docs-only:

```bash
cli-printing-press generate \
  --docs <docs-url> \
  --name <api> \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <public-library-category> \
  --force --validate
```

GraphQL-only APIs:
- Generate scaffolding only in Phase 2
- Build real commands in Phase 3 using a GraphQL client wrapper

After generation:

**Verify the CLI description across every surface.** A single curated one-liner is
rendered into five files: `internal/cli/root.go` (`Short:`), `SKILL.md` frontmatter
(`description:`), `.goreleaser.yaml` (`brews:` description), `internal/cli/agent_context.go`
(`Description:`), and `internal/mcp/tools.go` (the `handleContext` response's `"description"` key). Each resolves
from the authored sources (`narrative.headline` in `research.json`, or `cli_description:`
in the spec) when set. `root.go`'s `Short:` has a safe generic fallback (`"Manage <api>
resources via the <api> API"`); the other four fall through to the spec's raw
`info.description` — which is often the upstream OpenAPI blob leading with a Markdown
heading like `# Introduction` followed by API-shaped paragraphs. Eyeballing only `root.go`
will miss the failure mode because `root.go` is the only surface that's structurally
immune.

Open at least the `SKILL.md` frontmatter `description:` and the `.goreleaser.yaml` `brews:`
block in addition to `root.go`'s `Short:`. If any reads as API documentation rather than
user-facing CLI purpose ("AeroAPI is a simple, query-based API…"), or contains a bare
Markdown heading, the authored sources are missing. Fix at the source: set
`narrative.headline` in `research.json` to a single-sentence differentiator (name what
makes this CLI worth using, don't restate the API), or add a `cli_description:` line to
the spec. Then regenerate. Do not hand-edit the printed files — they revert on the next
regen.

**REQUIRED: Preserve README sections.** The generated README contains 5 standard sections
that the scorecard checks for: Quick Start, Agent Usage, Health Check, Troubleshooting, and
Cookbook. When rewriting the README for this API during Phase 3, **preserve all 5 sections**.
You may add additional sections that help users of this specific API (e.g., "Rate Limits",
"Pagination", "Authentication Setup"), but never remove the standard ones.

**REQUIRED: Verify auth was generated.** Check if the generated `config.go` has auth
env var support (look for `os.Getenv` calls for API key variables). If the
pre-generation auth enrichment ran correctly, this should already be present. If not
(enrichment was missed or the spec was ambiguous), this is the safety net: check the
Phase 1 research brief for auth requirements and manually add env var support to
`config.go` using the pattern: add `APIKey`/`APIKeySource` fields to the Config struct,
and `os.Getenv("<API>_API_KEY")` in the Load function.

**Validate narrative `command` strings before publishing examples.**
The LLM (or human) authoring `research.json` can name commands that don't actually
exist in the generated CLI — `<cli> stats` when the real shape is `<cli> reports stats`,
or a command that was dropped because its endpoint had a complex body. It can also
write a real command path with a bogus flag or positional shape. Without a check, the
broken commands ship to the README's Quick Start (`narrative.quickstart`) and the
SKILL's recipes (`narrative.recipes`); users copy-paste them and hit failures on the
very first invocation.

`cli-printing-press shipcheck` now runs `validate-narrative --strict --full-examples`
automatically after `verify` builds the CLI binary. The standalone command is still
useful immediately after editing `research.json`: it walks every
`narrative.quickstart[].command` and `narrative.recipes[].command`, strips the binary
name and trailing arguments, and runs `<binary> <words> --help` for each. With
`--full-examples`, it also runs the complete example under `PRINTING_PRESS_VERIFY=1`,
appending `--dry-run` when the command advertises it. This catches bad flags and
argument shapes without making live API calls.

```bash
QUICKSTART_BINARY="$CLI_WORK_DIR/<api>-pp-cli"
go build -o "$QUICKSTART_BINARY" "$CLI_WORK_DIR/cmd/<api>-pp-cli"

cli-printing-press validate-narrative --strict --full-examples \
  --research "$API_RUN_DIR/research.json" \
  --binary "$QUICKSTART_BINARY"
```

`--strict` exits non-zero on any missing command, empty subcommand-words entry, or
empty narrative (both sections omitted). With `--full-examples`, it also fails on full
examples that cannot dry-run or whose full invocation fails. Side-effectful auth,
launch, and mutating apply examples are reported as `UNSUPPORTED` warnings and do not
fail strict aggregation. Drop `--strict` to get a warn-only report, omit
`--full-examples` only when you intentionally want the old offline path check, or add
`--json` for machine-readable output.

If any commands are reported missing, fix them in `research.json` before continuing.
Common causes:

- Resource was renamed during generation (typically the spec uses `users` but the LLM
  wrote `user` in research.json).
- The endpoint exists but is hidden (had a complex body and was dropped from the
  promoted-command surface; reach it via the typed `<resource> <endpoint>` form).
- The command name is a placeholder (`<cli> example`) that should have been replaced
  with a real path.
- The path exists but the example uses a flag/argument shape the command does not
  accept; fix the concrete example in `research.json` before it renders into README
  and SKILL prose.

`narrative.quickstart` drives the README Quick Start and `narrative.recipes` drives
the SKILL.md recipes; getting either wrong silently ships copy-paste-broken examples
to users. The `--help`-walk check is the cheapest catch and runs offline against the
just-built binary — no live API access needed.

After the description rewrite, update the lock heartbeat:

```bash
cli-printing-press lock update --cli <api>-pp-cli --phase generate
```

Then:
- note skipped complex body fields
- fix only blocking generation failures here
- do not start broad polish work yet

If generation fails:
- fix the specific blocker
- retry at most 2 times
- prefer generator fixes over manual generated-code surgery when the failure is systemic
- if retries are exhausted, release the lock and stop:
  ```bash
  "$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
  ```

## Phase 3: Build The GOAT

<!-- CODEX_PHASE3_START -->
When `CODEX_MODE` is true, read [references/codex-delegation.md](references/codex-delegation.md)
for the delegation pattern, task type templates, and circuit breaker logic.

When `CODEX_MODE` is false, skip this section.
<!-- CODEX_PHASE3_END -->

Build comprehensively. The absorb manifest from Phase 1.5 IS the feature list.

**First Phase 3 build-log line:** Before writing code, count the shipping-scope transcendence rows in the Phase 1.5 absorb manifest and write this as the first line of `$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md`:

```text
Manifest transcendence rows: <planned> planned, 0 built. Phase 3 will not pass until all <planned> ship.
```

Use only rows that Phase 3 is expected to build: include approved transcendence rows with concrete `Command` values, exclude rows whose implementation starts with `(stub)`, and keep `spec-emits` rows out of the hand-code count while still tracking whether their approved command path exists. Update the build log's built count as rows are completed. If `PRIOR_SUB60_REPRINT=true`, this line is also the strict-gate budget: partial transcendence coverage is a hold by default.

**macOS framework access:** When the plan or manifest specifies macOS framework APIs (ScreenCaptureKit, CoreGraphics, CoreAudio, Vision, Shortcuts, etc.), use the Swift subprocess bridge pattern - Go shells out to `swift -e '<inline script>'`. Swift is always available with Xcode CLT. Do NOT attempt Python+PyObjC - it requires separate installation and is unreliable across Python distributions. Reference `agent-capture-pp-cli/internal/capture/cgwindow.go` as the canonical example of this pattern.

Priority 0 (foundation):
- data layer for ALL primary entities from the manifest
- sync/search/SQL path - this is what makes transcendence possible

After completing Priority 0, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p0
```

Priority 1 (absorb - match everything):
- ALL absorbed features from the Phase 1.5 manifest
- Every feature from every competing tool, matched and beaten with agent-native output
- This is NOT "top 3-5" - it is the FULL manifest

**Lock heartbeat rule for long priority levels:** If Priority 1 has more than 5 features, update the lock heartbeat after every 3-5 features to prevent the 30-minute staleness threshold from triggering mid-build:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p1-progress
```

Priority 2 (transcend - build what nobody else has):
- ALL transcendence features from Phase 1.5
- The NOI commands that only work because everything is in SQLite
- These are the commands that make someone say "I need this"

**Lock heartbeat rule for Priority 2:** Same rule as Priority 1 — if Priority 2 has more than 3 transcendence features, update the heartbeat after every 2-3 features:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p2-progress
```

After completing Priority 2, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p2
```

Priority 3 (polish):
- skipped complex request bodies that block important commands
- naming cleanup for ugly operationId-derived commands
- tests for non-trivial store/workflow logic
- enrich terse flag descriptions: review generated command flags. If any description is under 5 words or is generic spec-derived text (e.g., "access key", "The player"), improve it using the research brief. For example, change "access key" to "Steam API key (get one at steamcommunity.com/dev/apikey)". Focus on auth keys, IDs, and filter parameters.

### Agent Build Checklist (per command)

After building each command in Priority 1 and Priority 2, verify these 13 principles are met. These map 1:1 to what Phase 4.9's agent readiness reviewer will check - apply them now so the review becomes a confirmation, not a catch-all.

1. **Non-interactive**: No TTY prompts, no `bufio.Scanner(os.Stdin)`, works in CI without a terminal
2. **Structured output**: `--json` produces valid JSON, `--select` filters fields correctly. Hand-written novel commands that build a Go-typed slice/struct and emit JSON should use the generated receiver-style helper, `flags.printJSON(cmd, v)`, or call `printJSONFiltered(cmd.OutOrStdout(), v, flags)` directly. Both route through `printOutputWithFlags`, picking up `--select`, `--compact`, `--csv`, and `--quiet` for free. Verify with `<cli> <novel> --json --select <field> | jq 'keys'` returning only the requested fields.
3. **Progressive help**: `--help` shows realistic examples with domain-specific values (not "abc123"). **Use `Example: strings.Trim(\`...\`, "\n")` (preserves leading 2-space indent) NOT `strings.TrimSpace(\`...\`)` (strips it).** TrimSpace makes the first example line unindented; dogfood's example-detection parser is tolerant of this in current versions, but the indented form renders correctly across every Cobra version and is the convention used by every generated command.
4. **Actionable errors**: Error messages name the specific flag/arg that's wrong and the correct usage
5. **Safe retries**: Mutation commands support `--dry-run`, idempotent where possible
6. **Composability**: Exit codes are typed (0/2/3/4/5/7/10 as applicable), output pipes to `jq` cleanly
7. **Bounded responses**: `--compact` returns only high-gravity fields, list commands have `--limit`
8. **Verify-friendly RunE**: Hand-written commands MUST NOT use `Args: cobra.MinimumNArgs(N)` or `MarkFlagRequired(...)`. Cobra evaluates both before RunE runs, so a `--dry-run` guard inside RunE cannot reach if those gates fail. Verify probes commands with `--dry-run` and expects exit 0; commands with hard arg/flag gates fail those probes. Instead: validate inside RunE, fall through to `cmd.Help()` only for unambiguous help-only invocations (no args and no flags), short-circuit on `dryRunOK(flags)` before any IO, and return `usageErr(...)` with exit 2 when required input is missing in real mode.
   - **Use string for "positional OR flag" commands**: when a command accepts a positional `<x>` OR a flag `--y` as alternatives (e.g., `snapshot <co>` or `snapshot --domain example.com`), declare `Use: "<cmd> [x]"` with **square brackets** (optional), not `<x>` (required). Validate "exactly one of x or --y" inside RunE. Required positionals declared with angle brackets break verify-skill recipes that use the flag-only form.
   - **Declare verifier fixture inputs when generic values are not enough**: if the command needs realistic positional values or required flags to pass the verifier's happy path, add `Annotations: map[string]string{"pp:happy-args": "<item>=example-id;--query=example"}` or assign a whole initialized `cmd.Annotations` map after construction. The verifier consumes semicolon-separated tokens in order: `<label>=value` tokens overlay synthesized positional args, and `--flag=value` tokens overlay or add flag/value pairs. Commands without the annotation keep the generic synthesized inputs.
9. **Side-effect commands stay quiet under verify**: Any hand-written command that performs a visible side effect (opens a browser tab, sends a notification, plays audio, dials out to an OS handler) MUST follow both halves of the convention:
   - **Print by default; opt in to the action.** The default behavior prints what would happen (`would launch: <url>`); a flag like `--launch` / `--send` / `--play` is required to actually do it. food52's `open` command is the reference shape — see `internal/cli/open.go` after retro #337.
   - **Short-circuit when `cliutil.IsVerifyEnv()` returns true.** The Printing Press verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess; commands that ignore it can spam the user's environment during a verify pass even with the print-by-default flag pattern. The helper is generated into every CLI's `internal/cliutil/verifyenv.go`. Pattern:
     ```go
     if cliutil.IsVerifyEnv() {
         fmt.Fprintln(cmd.OutOrStdout(), "would launch:", url)
         return nil
     }
     ```
   This is defense-in-depth: the verifier also runs a heuristic side-effect classifier, but it can miss commands whose `--help` text and source don't match the heuristics. The env-var check is the floor.
   - **Long-running commands curtail work under live-dogfood.** Any hand-written command whose happy path is an expensive network operation (full sync loops, content crawlers, bulk archive walks) MUST check `cliutil.IsDogfoodEnv()` and curtail work to fit inside the matrix's flat 30s per-command timeout. `cli-printing-press dogfood --live` sets `PRINTING_PRESS_DOGFOOD=1` in every subprocess. Pattern:
     ```go
     if cliutil.IsDogfoodEnv() {
         return crawl(ctx, opts.WithMaxPages(1))
     }
     ```
     Distinct from `IsVerifyEnv`: dogfood is a real-API matrix, so curtail work (paginate once, smaller `--limit`), never substitute mock data for real calls.
10. **Per-source rate limiting**: any hand-written client in a sibling internal package (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc. — anything not generator-emitted) that makes outbound HTTP calls MUST use `cliutil.AdaptiveLimiter` and surface `*cliutil.RateLimitError` when 429 retries are exhausted. Empty-on-throttle is indistinguishable from "no data exists" and silently corrupts downstream queries. Read [references/per-source-rate-limiting.md](references/per-source-rate-limiting.md) when authoring a sibling client. Enforced at generation time by dogfood's `source_client_check`.
11. **Per-command timeout boundary**: Hand-written novel commands that call a sibling typed HTTP client (`internal/<api>/`, `internal/source/<name>/`, `internal/recipes/`, etc.) MUST wrap `cmd.Context()` with `boundCtx(cmd.Context(), flags)` and `defer cancel()` before the first client call. Generated endpoint commands already pass `flags.timeout` into `client.New`; sibling clients do not. Without this boundary, root `--timeout` is advertised in help but does not bound wide scans, crawlers, or fan-out loops.
12. **Parallel-fetch partial failures**: any command that fans out N API calls and computes an aggregate (averages, rollups, comparisons, cross-source merges, digest summaries) MUST preserve each fetch error through the result channel and exclude error-tagged entries from totals and denominators. Failed fetches may still appear in the response so the caller can see the gap, but they must not become zero-valued phantom rows that dilute averages or counts. Surface the partial failure explicitly with:
   - a stderr warning that names the failed count and the actual aggregation denominator, for example `warning: 2 of 10 fetches failed; averages computed over the remaining 8 items`
   - a `fetch_failures` field in the JSON response envelope listing the failed entries and error messages

Silently averaging phantom zeros is worse than reporting a partial result.
13. **Scan-and-filter caps**: any hand-written transcendence command that scans
    a paginated or otherwise unsorted endpoint, filters locally, and then keeps
    matching rows MUST bound scan effort separately from output size. This is the
    "list, filter locally, fan out to detail" shape: the API cannot filter on the
    dimension the command needs, so the command pages through broad results and
    applies the real predicate in Go. `--limit` is not enough because it bounds
    matches kept, not records scanned.

Required elements for every scan-and-filter command:

1. **`--max-scan-pages int`**, or a unit-specific equivalent such as
   `--max-scan-batches` / `--max-scan-records`, with a conservative default.
   Five pages is a reasonable starting point for typical paginated APIs
   (~250 records at 50/page). Lower it under `cliutil.IsDogfoodEnv()` when the
   happy path would otherwise risk the live-dogfood 30s timeout.
2. **`scanned_<unit>` in the JSON envelope**, for example `scanned_orders` or
   `scanned_issues`, so downstream agents can tell whether an empty result
   examined 20 records or 2,000.
3. **`note` in zero-match JSON output**, explaining that the scan cap was hit
   without finding a match and naming the flag that widens the search.
4. **Clear separation between output and scan caps**: `--limit` controls how
   many matches are returned; `--max-scan-pages` controls how many list pages
   or records the command is allowed to examine.

Use this pattern when the endpoint ordering is unrelated to the local predicate:
search-by-property over relevance-ranked search results, issues by a weakly
server-filtered custom field, pull requests by reviewer from an endpoint with
no reviewer filter, rental orders by date from a broad order list, and similar
cases.

```go
type scanFilterView struct {
	Items         []yourEntryType `json:"items"`
	ScannedItems  int             `json:"scanned_items"`
	MaxScanPages  int             `json:"max_scan_pages"`
	Note          string          `json:"note,omitempty"`
}

func newScanFilterCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var maxScanPages int
	var status string
	cmd := &cobra.Command{
		Use:   "find-by-status",
		Short: "Find matching items by scanning the list endpoint",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				fmt.Fprintf(cmd.OutOrStdout(), "would scan up to %d pages for matching items\n", maxScanPages)
				return nil
			}
			if status == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--status is required"))
			}
			if cliutil.IsDogfoodEnv() && maxScanPages > 1 {
				maxScanPages = 1
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			var matches []yourEntryType
			scanned := 0
			scanCapHit := true
			for page := 1; page <= maxScanPages && len(matches) < limit; page++ {
				data, err := c.Get("/api/v1/items", map[string]string{
					"page":     strconv.Itoa(page),
					"pageSize": "50",
				})
				if err != nil {
					return fmt.Errorf("fetching items page %d: %w", page, err)
				}
				items, err := parseItems(data)
				if err != nil {
					return fmt.Errorf("parsing items page %d: %w", page, err)
				}
				for _, item := range items {
					scanned++
					if item.Status != status {
						continue
					}
					matches = append(matches, item)
					if len(matches) >= limit {
						break
					}
				}
				if len(items) == 0 {
					scanCapHit = false
					break
				}
			}
			view := scanFilterView{
				Items:         matches,
				ScannedItems:  scanned,
				MaxScanPages:  maxScanPages,
			}
			if len(matches) == 0 && scanCapHit {
				view.Note = fmt.Sprintf("scanned %d items across up to %d pages without finding status %q; raise --max-scan-pages to widen the search", scanned, maxScanPages, status)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(view)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum matching items to return")
	cmd.Flags().IntVar(&maxScanPages, "max-scan-pages", 5, "maximum list pages to scan before returning partial or empty results")
	cmd.Flags().StringVar(&status, "status", "", "status to match")
	return cmd
}
```

#### Verify-friendly RunE template

Use this shape for every hand-written transcendence command. The generator emits the `dryRunOK` helper into `internal/cli/helpers.go`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    if len(args) == 0 && cmd.Flags().NFlag() == 0 {
        return cmd.Help()
    }
    if dryRunOK(flags) {
        return nil
    }
    if <required input missing> {
        _ = cmd.Usage()
        return usageErr(fmt.Errorf("<flag-or-arg> is required"))
    }
    // ... real work ...
}
```

Why each branch exists: the `len(args) == 0 && cmd.Flags().NFlag() == 0` branch handles an interactive `<cli> mycommand` help-only invocation without treating help as an error. The `dryRunOK` branch handles verify's `<cli> mycommand <fixture> --dry-run` probes before network or filesystem IO. The required-input branch handles non-help invocations where a mode or output flag is present (`--no-input`, `--agent`, `--json`) but the required ID, query, path, or other command input is still missing. Missing required input must print usage and return `usageErr(...)` so callers get exit code 2 instead of a silent rc=0 skip.

For SQLite-backed novel commands only, add this missing-mirror guard after `dryRunOK(flags)`, after any required-input `usageErr(...)` check, and after `dbPath` is resolved, but before `store.OpenWithContext`, `store.OpenReadOnly`, `sql.Open`, or other SQLite access:

```go
if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
	fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: <cli> sync --resources <resource> --db %s\n", dbPath, dbPath)
	if flags.asJSON || flags.agent {
		fmt.Fprintln(cmd.OutOrStdout(), "[]")
	}
	return nil
}
```

The missing-mirror branch covers a different probe layer from `dryRunOK`: live execution without `--dry-run`, before the user has run `sync`. Return empty JSON (`[]`) for `--json` / `--agent` so agents receive a valid empty result instead of a SQLite open failure; print a human hint to stderr that names the sync command needed to populate the mirror. The unconditional `return nil` is intentional for both machine and human paths: a missing local mirror is an empty local-cache state, not a usage or API failure. Do not add this branch to novel commands that call live API endpoints directly or do not use the local store.

Multi-positional commands (N >= 2 required args) must use a two-check shape so only the bare help probe returns exit 0:

```go
if len(args) == 0 && cmd.Flags().NFlag() == 0 {
	return cmd.Help() // bare invocation help probe
}
if len(args) < N {
	_ = cmd.Usage()
	return usageErr(fmt.Errorf("missing required positional argument"))
}
```

This preserves verify-friendly help behavior for 0 args while making partial positional input (`1..N-1`) fail with exit 2 in dogfood `error_path`. Single-positional commands can keep the single required-input check. If a multi-positional command supports `--dry-run`, place its `dryRunOK(flags)` branch after the `len(args) < N` gate (once all N positionals are present), so the dry-run probe still short-circuits.

Do not collapse the first and third branches into `if len(args) == 0 || <flag empty> { return cmd.Help() }`. `cmd.Help()` returns `nil`, so agents and scripts cannot distinguish "help was requested" from "the command skipped required work."

For commands with no required inputs, omit the `usageErr(...)` branch entirely and keep the help-only plus dry-run branches.

If the command reads a file or directory (`os.ReadFile`, `os.ReadDir`, `os.Stat`, `os.Open`, `os.OpenFile`, `os.Lstat`, `filepath.Walk`, `filepath.WalkDir`, or any other filesystem access), the read MUST come after `dryRunOK()`, not before. Filesystem reads before `dryRunOK()` cause `validate-narrative --full-examples` to fail with a missing-file error rather than a clean dry-run exit 0.

### Phase 3 delegation: require feature-level acceptance

When Phase 3 implementation is delegated to a sub-agent (via `Agent` tool or Codex), the delegation prompt MUST require behavioral acceptance tests per major feature, not just "does the command build and run." Agents consistently over-report success when the contract is only "command executes without error."

Required in every Phase 3 delegation prompt:

1. **Per-feature acceptance assertions** that check output content, not just exit codes. Examples the prompt should make concrete:
   - Search/ranker: "After `<cli> goat 'brownies'`, assert at least 3 of the top 5 results contain 'brown' in their title or URL. If fewer, the extractor is broken."
   - Lookup: "After `<cli> sub buttermilk --json`, assert the parsed JSON is an array of objects with `substitute`, `ratio`, `context` fields."
   - Transform: "After `<cli> recipe get <known-url> --servings 6`, assert the output ingredient quantities differ from the `--servings 4` invocation (scaling actually ran)."
2. **Absence-of-correctness tests** for every feature whose correct answer can be empty or complete:
   - Calendar/window commands: "Given `--days N`, assert exactly N rows are returned, including zero-count days."
   - Drift/diff commands: "Given only one snapshot or no changed values, assert the command returns `[]` rather than fabricating drift."
   - Alert/watch commands: "Given no matching records, assert empty output plus an honest reason, not stale or unrelated data."
3. **Negative tests** per filter/search command: run with a deliberately-mismatching query and assert the result set does NOT contain irrelevant items.
4. **No parent-command delegation without flags.** If a parent command delegates to a leaf command's `RunE`, the parent must declare every flag the delegate accepts. Prefer group parents that show help over aliasing a parent to a child.
5. **Structured pass/fail report** in the agent's response (raw output of each assertion, not a summary).

A Phase 3 delegation that reports PASS without behavioral assertions is treated as untrusted — re-run acceptance tests before accepting the result.

### Search Dedup Rule

When building cross-entity search commands, use per-table FTS search methods individually. Do NOT combine per-table search with the generic `db.Search()` — this causes duplicate results because the same entities exist in both `resources_fts` and per-table FTS indexes.

### Priority 1 Review Gate

After completing ALL Priority 1 (absorbed) features, BEFORE starting Priority 2 (transcendence):

Pick 3 random commands from Priority 1. Run each with:
```bash
<cli> <command> --help          # Does it show realistic examples?
<cli> <command> --dry-run       # Does it show the request without sending?
<cli> <command> --json          # Does it produce valid JSON?
```

If any of the 3 fail, there's a systemic issue. Fix it across all commands before proceeding. This catches problems like "--dry-run not wired" or "--json outputs table instead of JSON" early, when they're cheap to fix.

After passing the Priority 1 Review Gate, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase build-p1
```

Get Priority 0 and 1 working first (the foundation and absorbed features), pass the review gate, then build Priority 2 (transcendence), then verify.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-build-log.md`

Include:
- what was built
- what was intentionally deferred
- skipped body fields that remain
- any generator limitations found

### Phase 3 Completion Gate

**MANDATORY. Do NOT proceed to Phase 4 until this gate passes.**

Before moving to shipcheck, verify the build log against the absorb manifest. Counting alone is not enough: a build that replaces an approved `keywords-data google-ads search-volume --auto-mode` with a self-contained wrapper `keywords volume` keeps the count right while shipping a different command than what Phase 1.5 approved. The gate must verify the **specific approved command path** for each row that declares one.

**Sub-60 reprint strictness:** If this run is reprinting an existing library CLI whose prior `.printing-press.json` had `scorecard.steinberger.percentage < 60` (`PRIOR_SUB60_REPRINT=true` from Phase 0), partial transcendence implementation is a HOLD by default. The Phase 3 Completion Gate may not use `partial-implementation OK` semantics while any shipping-scope transcendence row is missing. To override, write an explicit `partial_transcendence_override` note in the build log that names each missing row, explains why it is intentionally deferred, and states that the user accepted the sub-60 reprint shipping with partial novel coverage. Without that note, any missing approved transcendence row blocks Phase 4.

1. **Per-row Cobra resolution check.** Read approved command paths from `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`:
   - Every transcendence row's `Command` value.
   - Every absorbed row whose `Our Implementation` value starts with `<api>-pp-cli <clean command path>`.
   - Every absorbed row whose `Our Implementation` value starts with `(behavior in <api>-pp-cli <command path>)`. For these rows, first extract the text between the literal prefix `(behavior in ` and the first closing `)`, producing `<api>-pp-cli <command path>`, then apply the same binary-strip and flag-strip rules to that extracted command text.
   - Skip rows that start with `(generated endpoint)` because the generator-emitted typed endpoint surface already covers those commands.
   - Skip rows that start with `(stub)` because the Phase Gate 1.5 stub approval list is their source of truth; stubs are intentionally unresolved implementation placeholders and must not be counted as built commands.
   - Do not infer command paths from freeform prose. Any absorbed row whose `Our Implementation` value does not start with `<api>-pp-cli <clean command path>`, `(behavior in <api>-pp-cli <command path>)`, `(generated endpoint)`, or `(stub)` is an invalid manifest row; return to Phase 1.5 and normalize it before proceeding.

   For each approved path, including command text extracted from `(behavior in <api>-pp-cli <command path>)` rows, strip any leading binary name, then strip flag tokens and quoted args to get the leaf command path (drop everything from the first `-` token onward; `bottleneck` stays `bottleneck`, `velocity --weeks 4` becomes `velocity`, `compare "LeBron" "Curry"` becomes `compare`, `keywords-data google-ads search-volume --auto-mode` becomes `keywords-data google-ads search-volume`). Then run:
   ```bash
   ./<api>-pp-cli <leaf path> --help
   ```
   Assert (a) exit code 0 AND (b) the help output's `Usage:` spec line is `<binary> <leaf path> [flags]` — i.e., the line **immediately before** ` [flags]` is the full leaf path you requested. Cobra falls through to the parent's help when a subcommand is unknown — same exit 0, but the Usage spec line is `<binary> <parent> [command]` instead of `<binary> <parent> <leaf> [flags]`. The grep-able signal is `<leaf> [flags]` for a real command vs `[command]` for a parent fall-through; the leaf appearing only under `Available Commands:` is also a fall-through.
2. **HALT on any miss.** If any approved row fails (a) or (b), STOP and name the manifest section plus row number or source line in the miss message, e.g. `Absorbed row 3: timeline did not resolve as a Cobra command`. Either build the approved command path now, or return to Phase 1.5 with a revised manifest for explicit re-approval per the existing "no mid-build downgrade" rule. Do not invent a wrapper command and silently update the manifest. Do not classify the feature as "documentation-only" because integration touches many files.
3. **Deterministic backstop.** After the per-row walk, run the same machine-checked equivalent so a manifest-vs-`research.json` drift cannot mask a miss:
   ```bash
   cli-printing-press dogfood --dir "$CLI_WORK_DIR" --research-dir "$API_RUN_DIR" --json \
     | jq -e '.novel_features_check | .found == .planned and (.missing // []) == [] and (.skipped // false) == false'
   ```
   The `novel_features_check` block reports planned/found/missing against `research.json`'s `novel_features`; an exit-0 here plus a clean per-row walk means both sources agree the build matches Phase 1.5 approval. **`skipped: true` is a HALT, not a pass at this gate.** Dogfood marks the check skipped only when `--research-dir` is missing or `research.json` has no `novel_features` key — both conditions mean the gate has no source of truth to verify against, which is exactly the silent-bypass path the gate was designed to prevent. If you reach this gate with no `novel_features` in `research.json` but the absorb manifest lists transcendence rows, re-derive `research.json` from the manifest (per Step 1.5e) before re-running. If `dogfood` reports missing features that the manifest still lists, either `research.json` was edited mid-build (re-derive it from the manifest) or the build is genuinely incomplete (return to step 1).
4. **Test presence for pure-logic novel packages.** Every Go package you created under `internal/` for novel-feature logic (parsers, matchers, scalers, scrapers — anything that isn't command wiring) must have a `_test.go` with at least one table-driven happy-path test per exported function. `cli-printing-press dogfood` surfaces violations as structural issues: pure-logic packages with zero tests fail shipcheck; packages with fewer than 3 test functions are flagged as warnings for Phase 4.85's agentic review. Trivial placeholder tests pass the file-presence check but are the wrong shape — write real assertions or the review catches you.

The check is structural — no judgment about whether each command does "enough." Behavioral correctness remains dogfood's and scorecard's job in Phase 4.

The generator handles Priority 0 (data layer) and most of Priority 1 (absorbed API endpoints). Priority 2 (transcendence) is always hand-built — the generator does not produce these. If you skip Priority 2, the CLI ships without the features that differentiate it from every other tool.

**Starter templates for novel commands.** Cobra wiring is mechanical and consistent across novel features; the actual feature work lives in the RunE body. Copy the wrapper below and one of the RunE skeletons that follows, fill in the placeholders from the absorb manifest's transcendence row (`Name`, `Command`, `Description`, `Example`, `WhyItMatters`), and replace the body comments with your implementation. Dogfood, verify, and scorecard still apply to the result — the templates raise the floor without changing what shipcheck checks.

**Helpers already emitted by the generator.** Do not reinvent these helpers in novel command files. They live in `internal/cli/helpers.go` after generation and are available to every hand-written command in package `cli`:

- `printJSONFiltered(w io.Writer, v any, flags *rootFlags) error` - apply `--select`, `--compact`, `--csv`, and `--quiet` while writing JSON from a Go value.
- `printAutoTable(w io.Writer, items []map[string]any) error` - render JSON-like rows as the generated human table format.
- `defaultDBPath(name string) string` - resolve the local SQLite database path for `<name>`.
- `dryRunOK(flags *rootFlags) bool` - detect verify-friendly `--dry-run` short-circuits before network, store, or filesystem work.
- `boundCtx(parent context.Context, flags *rootFlags) (context.Context, context.CancelFunc)` - apply root `--timeout` to hand-written commands that call sibling typed clients instead of the generated `internal/client`.
- `filterFields(data json.RawMessage, fields string) json.RawMessage` - apply `--select` to a JSON blob.
- `compactFields(data json.RawMessage) json.RawMessage` - apply `--compact` to a JSON blob.
- `isTerminal(w io.Writer) bool` - detect terminal output versus pipes.
- `wantsHumanTable(w io.Writer, flags *rootFlags) bool` - detect when output should use the generated human table instead of machine JSON.

```go
// internal/cli/<command>.go — replace <command> with the kebab leaf
// of NovelFeature.Command (e.g., "issues stale" → "issues_stale.go").
package cli

import (
	"github.com/spf13/cobra"
	// add: "encoding/json", "fmt", "os", "<module>/internal/store", etc. as needed
)

func newNovelXxxCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "<leaf-of-Command>",                    // e.g. "stale" for "issues stale"
		Short:   "<NovelFeature.Description, one line>", // truncate to ~70 chars
		Long:    "<optional: manifest Long Description, or Description + WhyItMatters>", // omit if Short is enough
		Example: "  <cli>-pp-cli <Command> --json",       // from NovelFeature.Example
		Annotations: map[string]string{
			// Set "mcp:read-only": "true" only when the command does NOT mutate
			// external state (lookups, comparisons, aggregations, render views).
			// Omit the whole map for commands that mutate (post, delete, write file).
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Pick the matching RunE skeleton below (API-call or store-query),
			// then implement the feature-specific path/query/parsing/formatting.
			return nil
		},
	}
	// cmd.Flags().StringVar(...) — add flags from the planned --flag list, if any
	return cmd
}

// Multi-word Commands like "issues stale": this constructor is registered as
// a child of the matching spec-resource parent (newIssuesCmd) — wire the
// AddCommand call inside root.go via local-variable capture:
//   issuesCmd := newIssuesCmd(flags)
//   issuesCmd.AddCommand(newNovelIssuesStaleCmd(flags))
//   rootCmd.AddCommand(issuesCmd)
// Leaf commands must declare every non-root flag used in their examples.
// Use kebab-case flag names, such as --max-age instead of --maxAge, so the
// generated CLI convention and verify-skill flag scanner stay aligned.
// Do not rely on parent-local flags like --org or --project being accepted by
// child commands unless the parent registered them with PersistentFlags().
// Single-word Commands register directly: rootCmd.AddCommand(newNovelXxxCmd(flags)).
```

**RunE skeleton — API-call shape** (live data via a sibling typed client):

```go
RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		fmt.Fprintln(cmd.OutOrStdout(), "would fetch <resource>")
		return nil
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	c, err := newSiblingClient(flags) // replace with your internal/<api> or internal/source/<name> constructor
	if err != nil {
		return err
	}
	// Pass ctx to every sibling-client request so root --timeout bounds
	// crawls, wide scans, and fan-out loops. Generated endpoint commands that
	// use flags.newClient()/internal/client already consume flags.timeout.
	data, err := c.FetchResource(ctx, <request params>)
	if err != nil {
		return fmt.Errorf("fetching <resource>: %w", err)
	}
	// If the API returns CSV (`response_format: csv` in any spec endpoint),
	// wrap raw client data with cliutil.ParseCSV(data) before embedding it in a JSON envelope.
	// Parse data into your feature's view. Use cliutil.CleanText for any
	// text extracted from HTML or schema.org JSON-LD; re-implementing
	// HTML-entity unescape inline is the &#39; bug class.
	var view yourViewType // = parse(data)
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	// Human/terminal output (table or pretty print).
	return nil
},
```

**RunE skeleton — parallel-fetch aggregation shape** (live fan-out with partial-failure accounting):

Use this shape when a novel command fetches multiple items concurrently and computes a rollup, average, comparison, digest, or cross-source merge. The key invariant is that `err` travels with each result until aggregation, and error-tagged entries are excluded from all totals and denominators.

```go
RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		fmt.Fprintln(cmd.OutOrStdout(), "would fetch <resource> details")
		return nil
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	c, err := newSiblingClient(flags) // replace with your internal/<api> or internal/source/<name> constructor
	if err != nil {
		return err
	}
	type fetchResult struct {
		idx   int
		id    string
		entry yourEntryType
		err   error
	}
	ids := []string{} // derive from args, flags, or an initial list endpoint
	results := make(chan fetchResult, len(ids))
	var wg sync.WaitGroup
	for idx, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.FetchDetail(ctx, id)
			if err != nil {
				results <- fetchResult{idx: idx, id: id, err: err}
				return
			}
			entry, err := parseEntry(data)
			results <- fetchResult{idx: idx, id: id, entry: entry, err: err}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	ordered := make([]yourEntryType, len(ids))
	fetchErrors := make([]error, len(ids))
	for r := range results {
		ordered[r.idx] = r.entry
		if r.err != nil {
			fetchErrors[r.idx] = r.err
		}
	}
	failures := make([]fetchFailure, 0)        // empty marshals as [] not null
	successfulItems := make([]yourEntryType, 0) // empty marshals as [] not null
	var total float64
	var denominator int
	for idx, entry := range ordered {
		if fetchErrors[idx] != nil {
			failures = append(failures, fetchFailure{
				ID:    ids[idx],
				Error: fetchErrors[idx].Error(),
			})
			continue
		}
		successfulItems = append(successfulItems, entry)
		total += entry.Metric
		denominator++
	}
	if len(failures) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d fetches failed; averages computed over the remaining %d items\n", len(failures), len(ids), denominator)
	}
	view := yourAggregateView{
		Items:         successfulItems,
		AverageMetric: safeAverage(total, denominator),
		FetchFailures: failures, // json tag: `json:"fetch_failures,omitempty"`
	}
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	// Human/terminal output, including a visible partial-failure note.
	for _, entry := range view.Items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%.2f\n", entry.Name, entry.Metric)
	}
	if len(failures) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\npartial results: %d of %d fetches failed; average computed over %d items\n", len(failures), len(ids), denominator)
	}
	return nil
},
```

**RunE skeleton — store-query shape** (offline data via the local SQLite):

The generic `resources` table is keyed by `resource_type`. Flat resources synced from `/<resource>` land as `resource_type='<resource>'`. **Hierarchical resources** synced from `/<parents>/{id}/<resource>` land as `resource_type='<parent>_<resource>'` — e.g., `projects_tasks` (Asana), `repos_issues` / `repos_pulls` (GitHub) — *not* the bare `<resource>` name. A novel feature that filters by the bare name returns zero rows against a real DB. Use `IN (...)` to catch both shapes so the same code works whether the API exposes the resource flat or only parent-scoped.

SQLite store queries must use a **drain-first** pattern. SQLite uses a single connection by default, so never issue another query while a `*sql.Rows` is open. If the command needs follow-up lookups, parent-to-child expansion, or ID-to-name/email resolution, scan the first result set into plain structs/slices, check `rows.Err()`, close `rows`, and only then run follow-up `QueryContext` / `QueryRowContext` calls.

```go
// Declare these alongside the cmd literal, before return cmd:
//   var dbPath string
//   cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

RunE: func(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmd.Flags().NFlag() == 0 {
		return cmd.Help()
	}
	if dryRunOK(flags) {
		fmt.Fprintln(cmd.OutOrStdout(), "would query local store")
		return nil
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	if <required input missing> {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<flag-or-arg> is required"))
	}
	if dbPath == "" {
		dbPath = defaultDBPath("<cli>-pp-cli") // replace <cli> with the API slug
	}
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: <cli> sync --resources <resource> --db %s\n", dbPath, dbPath)
		if flags.asJSON || flags.agent {
			fmt.Fprintln(cmd.OutOrStdout(), "[]")
		}
		return nil
	}
	db, err := store.OpenWithContext(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()
	// Filter resources by both the flat and hierarchical naming so the
	// query catches rows synced via /<resource> AND rows synced via
	// /<parents>/{id}/<children>. Drop the parent-scoped entry if the
	// API only exposes the resource flat; add a <resource_singular>
	// entry for APIs that toggle plural/singular casing. SQL must be
	// SELECT-only; the search/sql gates reject mutating statements.
	rows, err := db.DB().QueryContext(ctx, `
		SELECT id, data FROM resources
		WHERE resource_type IN ('<resource>', '<parent>_<resource>')
		  AND ...`)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	// Scan each row. id/data on the resources table are NOT NULL so bare
	// strings are safe; ANY optional field selected via json_extract or
	// pulled from a typed FTS/upsert table can be NULL — use sql.Null*
	// scan targets (or COALESCE in the SQL) for those, see the NULL-safe
	// scans paragraph below.
	rawRows := make([]yourRawRowType, 0) // make([]T, 0) keeps empty JSON as [] not null
	for rows.Next() {
		var row yourRawRowType
		// Scan only columns from this result set here. Do NOT query again
		// inside this loop; drain and close rows first.
		if err := rows.Scan(&row.ID, &row.Data); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan row: %w", err)
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close rows: %w", err)
	}
	results := make([]yourRowType, 0, len(rawRows))
	for _, raw := range rawRows {
		// Follow-up queries are safe here because the parent *sql.Rows is closed.
		// Resolve IDs, expand child rows, or perform local joins, then append.
		_ = raw
	}
	if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !humanFriendly) {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	// Human/terminal output.
	return nil
},
```

For flat-only resources, the typed FTS/upsert tables the generator emits (e.g., `tasks_fts`, `projects`) work too — `SELECT id, data FROM <typed-table>` is the fast path. The `IN (...)` pattern above is the safe default whenever the resource may be hierarchical; `cli-printing-press dogfood --json` shows the actual `resource_type` distribution so you can confirm without running raw SQL.

For features that combine both (cache an API response in the store, or fall through to live when the local store is stale), nest one skeleton inside the other and use the `--data-source auto/local/live` flag pattern from the generated `sync` command.

**Store write transaction guardrail:** `store.Upsert` and `store.UpsertBatch` open their own write transaction. Do not call them from inside an open `db.DB().BeginTx` / `tx.Prepare` / `tx.Exec` write transaction; SQLite WAL permits only one writer and the nested write can busy-wait, fail, or corrupt the outer statement state. If a novel command writes both custom tables and `resources`, either use transaction-scoped helpers that operate on the same `*sql.Tx`, or commit/rollback the custom-table transaction before calling `store.Upsert` / `store.UpsertBatch`. Never discard these errors with `_`; a busy-timeout is data loss.

**Shared helpers available to novel code:** The generator emits `internal/cliutil/` in every CLI. When authoring novel commands, prefer `cliutil.FanoutRun` for any aggregation command (any `--site`/`--source`/`--region` CSV fan-out) and `cliutil.CleanText` for any text extracted from HTML or schema.org JSON-LD. Re-implementing these inline is how recipe-goat's trending silent-drop and `&#39;` entity bugs shipped.

**Hand-coded duration flags MUST use `cliutil.ParseDurationLoose` with a `StringVar` flag (not `DurationVar`).** Go's `time.ParseDuration` rejects the `7d`/`30d`/`1w`/`4w` day/week shorthand that the framework's `sync --since` already accepts, so a `DurationVar` flag fails at runtime on input agents and users reasonably expect. Declare the flag as a `StringVar`, then post-parse with `cliutil.ParseDurationLoose`, which adds `d`/`w` suffix support and otherwise defers to `time.ParseDuration`.

**OData v3 datetime fields MUST be decoded with `cliutil.ParseODataDate`.** OData v3 APIs (Exact Online, Microsoft Dynamics 365 Business Central, Dynamics NAV) return dates as `/Date(1715731200000)/` string literals that no standard parser accepts, so the raw value passes straight through to JSON output and agents cannot parse `created_at`/`due_date`. `cliutil.ParseODataDate(s) (time.Time, bool)` decodes the literal to a UTC `time.Time` and falls back to RFC3339, so callers need not dispatch on format. Re-implementing this inline per command is how the same regex ships inconsistently across OData CLIs.

**Streaming frame normalizers MUST use `cliutil.ExtractNumber` / `cliutil.ExtractInt` rather than raw `float64`/`int64` struct fields.** Real-world WebSocket and streaming JSON feeds (Binance, Coinbase, Kraken, Stripe `*_decimal`, vendor-specific market-data feeds) commonly encode numeric values as JSON-encoded strings (`"price":"1.91"`). `json.Unmarshal` of a JSON string into a `float64` field returns no error and silently leaves the field at 0; combined with NULL-on-zero patterns this discards the entire numeric feed with no error signal anywhere in the pipeline. The helpers accept both shapes (JSON number or JSON-encoded string), report `ok=false` on missing/null/unparseable, and are the canonical extraction path for `map[string]json.RawMessage` decoders. Re-implementing this inline as a `float64` struct field is the silent-aggregation-failure bug class.

**WebSocket-primary APIs SHOULD declare `streaming:` and use the generated live scaffold.** When the API's facts arrive over WebSocket and REST supplies metadata, follow `references/ws-primary-pattern.md`. Do not reimplement dial/subscribe/reconnect, newline-delimited JSON splitting, metadata status polling, or rebase-log writes in novel code unless the API genuinely breaks the generated lifecycle contract.

**NULL-safe SQL scans MUST use `sql.Null*` scan targets (or `COALESCE(<col>, <zero>)` in the query) for any column that can be NULL.** SQLite returns NULL for any absent JSON field selected via `json_extract(data, '$.optional_field')`, for any nullable column in a typed FTS/upsert table the generator emits, and for any field the API omits from a particular response. `database/sql`'s `rows.Scan` into a bare `string`/`int64`/`float64` returns a non-nil error on NULL (`Scan error on column index N: converting NULL to string is unsupported`) — and the surrounding `for rows.Next()` loop typically `continue`s on scan error, silently dropping every row. The result: queries return zero records, no error reaches the caller, the feature looks healthy because the API call succeeded. Use `var v sql.NullString` (or `NullInt64` / `NullFloat64` / `NullTime`) as the scan target and copy `.String` / `.Int64` / `.Float64` / `.Time` into your row struct, accepting the zero value as the missing-field representation. Re-implementing this inline as bare-string scans is the silent-row-drop bug class.

```go
// Wrong — every NULL column kills the row.
var name string
if err := rows.Scan(&id, &name); err != nil { continue }

// Right — NULL becomes the zero value, no row is lost.
var name sql.NullString
if err := rows.Scan(&id, &name); err != nil { continue }
result.Name = name.String
```

Also right: push the default into the query so the scan target stays bare.

```sql
SELECT id, COALESCE(json_extract(data, '$.name'), '') FROM resources WHERE ...
```

**Typed exit-code verification:** If a novel command intentionally returns a non-zero code for a non-error control-flow result, add `cmd.Annotations["pp:typed-exit-codes"] = "0,<code>"` (or the equivalent `Annotations: map[string]string{...}` literal) and document the same command-specific codes in its help. Do not list the global failure palette in command help unless those exits should count as a verify pass for that command; keep general exit-code troubleshooting in README/SKILL prose.

**Dogfood error-path opt-out:** If a real API returns HTTP 200 plus an empty success envelope for unknown IDs, and the command cannot distinguish bad input from a valid empty result without inventing API-specific semantics, annotate the Cobra command with `cmd.Annotations["pp:no-error-path-probe"] = "true"`. Dogfood will still run help, happy-path, and JSON-fidelity checks, but it will skip `error_path` with reason `no-error-path-probe annotation`. Do not add local "empty means not found" heuristics only to satisfy dogfood unless the upstream API contract actually defines that as an error.

<a id="hand-edit-durability"></a>
**Hand-edits must be regen-mergeable.** `cli-printing-press generate --force` snapshots the existing tree, emits a fresh tree, then runs the same AST-aware reconciliation used by `cli-printing-press regen-merge`. Whole hand-authored files and lost `AddCommand` wiring are preserved automatically; straightforward hand-edits to generated Go files (added declarations, literal drift, body drift) are classified and carried forward when the merge can do so safely. For risky edits, use the standalone `regen-merge` command first when you want a previewable report before applying.

For an extension to be durable, put it in its own file beside the emitted one:

- **Custom config fields:** create `internal/config/<api>_config.go` exporting accessors your novel code reads directly. Do not add fields to the emitted `Config` struct.
- **Custom request headers** (vendor fingerprint, `X-CSRF`, app-version, signed timestamps): create `internal/client/<api>_headers.go` exporting a func that builds the header map; novel code passes that map to `client.GetWithHeaders` / `PostWithHeaders` when it calls the API. The generated `client.go` has no global request mutator, so this pattern only covers requests made directly from novel code — it does not intercept calls from generated endpoint commands. Do not edit the templated header block in `client.go`.
- **Custom auth flow** (browser-sniffed sessions, vendor SSO, refresh hooks beyond OAuth2): create `internal/cli/<api>_auth.go` (package `cli`, same as the generated `auth.go`) with the API-specific token capture or refresh, and wire it from a novel command rather than editing the templated `auth.go` constructor functions (`newAuthLoginCmd`, `newAuthSetupCmd`, etc.). If the custom flow implements OAuth2 Authorization Code + PKCE, read [references/oauth2-pkce-cli-checklist.md](references/oauth2-pkce-cli-checklist.md) before writing or reviewing the command.
- **Extended store schema** (typed tables beyond `resources`, vendor JSON columns, full-text indexes): create `internal/store/<api>_migrations.go` running its own `CREATE TABLE ... IF NOT EXISTS` from a lazy init invoked by the novel commands that need it. Do not edit the migration slice in `store.go`.
- **New novel command:** put the command body in its own `internal/cli/<feature>.go` file. Generated TODO scaffolds may refresh on `generate --force`; once you replace the TODO body with a real implementation, regen preserves that hand-authored file instead of re-emitting the scaffold. The `AddCommand` call wiring it into the Cobra tree still goes in `root.go` per the Phase 3 novel-command skeleton above; `cli-printing-press generate --force` re-injects it via the lost-registration merge path. Use standalone `regen-merge` when you want to inspect the merge report before applying. Spec-declared commands are picked up by the generator's typed-tool path and need no hand-wired `AddCommand` at all.

If an extension genuinely cannot live in a separate file (a `case` branch in a templated method switch, an inline modification to a generated handler with no registry hook), file a generator issue requesting the hook rather than depending on repeated conflict-prone merges. The `AddCommand` case above is covered by the merge path.

**MCP exposure:** The generator emits `internal/mcp/cobratree/`, and the MCP binary mirrors the Cobra tree at startup. When you add, rename, or remove a user-facing Cobra command, the MCP surface follows automatically. Two annotations control how each command appears as an MCP tool:

- `cmd.Annotations["mcp:hidden"] = "true"` — exclude the command from the MCP surface entirely. Use only for debug/internal commands that should not become agent tools.
- `cmd.Annotations["mcp:read-only"] = "true"` — declare that this command does not modify external state. The MCP server attaches `readOnlyHint: true` to the resulting tool, so hosts like Claude Desktop don't bucket it under "write/delete tools" and demand permission per call. Apply this to every novel command whose only effect is reading from the API or the local store: lookups, comparisons, aggregations, render-only views, status checks. Skip it for commands that mutate external state (orders, posts, deletes) or write to user-visible files outside the local cache.
- `cmd.Annotations["mcp:write-positionals"] = "0"` — mark zero-based positional indexes that write to user-visible files when populated. Use this with `mcp:read-only` only when the command is otherwise read-only and the positional has a stdout-safe escape such as `-`. The MCP shell-out wrapper rejects non-stdout values for those positionals so a read-only-hinted tool cannot write files through the free-form `args` field. Use comma-separated indexes for multiple write sinks.

Endpoint-mirror tools the generator emits from the spec already get the right annotations automatically (`GET` → read-only, `DELETE` → destructive, etc.) — `mcp:read-only` is only needed on hand-authored Cobra commands the spec doesn't cover.

Do not rationalize skipping transcendence features because "the CLI already works for live API interaction." The absorb manifest was approved by the user. Build what was approved.

## Phase 4: Shipcheck

Run one combined verification block via the `shipcheck` umbrella, which runs all six legs (dogfood, verify, workflow-verify, verify-skill, validate-narrative, scorecard) in canonical order, propagates exit codes, and prints a per-leg verdict summary. The umbrella is the canonical Phase 4 invocation; running the legs individually is supported but not recommended (operators have skipped legs that way and shipped broken CLIs).

Before running shipcheck, update the lock heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase shipcheck
```

```bash
cli-printing-press shipcheck \
  --dir "$CLI_WORK_DIR" \
  --spec <same-spec> \
  --research-dir "$API_RUN_DIR"
```

The umbrella defaults to `verify --fix` (auto-repair common failures), `validate-narrative --strict --full-examples` (README/SKILL narrative command validation), and `scorecard --live-check` (sample novel-feature output against real targets). When Go sources under `cmd/<cli>/` or `internal/` are newer than `build/stage/bin/<cli>`, `scorecard --live-check` rebuilds the staged binary before sampling and reports the refresh action in human and JSON output. Use `--no-fix` for a read-only pass, `--no-live-check` to skip live sampling, or `--json` for a structured envelope (suppresses per-leg output for clean piping). Pass `--api-key` / `--env-var` through to verify when live testing needs a credential, or `--strict` to make verify-skill treat likely-false-positive findings as failures.

During shipcheck, the verify and scorecard legs persist their summaries back into `$CLI_WORK_DIR/.printing-press.json`: `verify.pass_rate`, `verify.verdict`, `scorecard.steinberger.percentage`, `scorecard.steinberger.grade`, and `novel_features_built` from the run's `research.json`. Do not hand-edit those fields; rerun shipcheck (or the standalone leg with `--write-manifest`) when they are stale. Phase 0's sub-60 reprint gate relies on this persisted score on the next run.

If a leg fails, re-run that one leg standalone (e.g., `cli-printing-press verify-skill --dir <CLI_WORK_DIR>`) for focused iteration; once it passes, re-run the full `shipcheck` umbrella to confirm no regression in the others.

Interpretation:
- `dogfood` catches dead flags, dead helpers, invalid paths, example drift, broken data wiring, command tree/config field wiring bugs, stale static MCP surfaces, and novel features that were planned but not built
- `verify` catches runtime breakage and runs the auto-fix loop for common failures
- `workflow-verify` tests the primary workflow end-to-end using the verification manifest (workflow_verify.yaml). Three verdicts: workflow-pass, workflow-fail, unverified-needs-auth
- `verify-skill` checks that every `--flag` and command path in SKILL.md actually exists in the shipped CLI source. Catches bogus examples invented by the absorb LLM (e.g., `search --max-time` when `--max-time` is a `tonight` flag). Exit 1 = findings to fix; exit 0 = SKILL is honest.
- `validate-narrative` checks that every README/SKILL narrative command path, flag, and argument shape in research.json resolves against the built CLI under `PRINTING_PRESS_VERIFY=1`
- `scorecard` is the structural quality snapshot, not the source of truth by itself

Fix order (update heartbeat between each fix category to prevent stale lock during long fix loops):
1. generation blockers or build breaks
2. invalid paths and auth mismatches
3. dead flags / dead functions / ghost tables
4. broken dry-run and runtime command failures
5. missing novel features (see below)
6. scorecard-only polish gaps

When category 4 includes narrative examples, rerun
`cli-printing-press validate-narrative --strict --full-examples` after the fix. The path-only
mode is not enough before publishing because it cannot catch bad flags on an otherwise
valid command.

**Missing novel features fix (step 5):** Dogfood writes `novel_features_built` to research.json — only features whose commands actually exist. The original `novel_features` (aspirational list from absorb) is preserved for the audit trail. Dogfood also syncs the generated `.printing-press.json` `novel_features`, `README.md` `## Unique Features` block, `SKILL.md` `## Unique Capabilities` block, and `internal/cli/root.go` `--help` Highlights block from `novel_features_built`; if none survived, it removes the rendered README/SKILL/root help blocks. Dogfood prints `dogfood: synced ... from novel_features_built` for every rendered artifact it changes. After dogfood:

1. Inspect the dogfood planned-vs-built delta
2. Build missing approved features when they are still in scope
3. Rerun dogfood so research.json, `.printing-press.json`, README.md, SKILL.md, and root `--help` Highlights are all synced from the verified set
4. Audit surrounding README/SKILL/root help prose, recipes, trigger phrases, and examples for indirect references to dropped features
5. Log which features were dropped (planned vs built delta)

After fixing each category, update the heartbeat:
```bash
cli-printing-press lock update --cli <api>-pp-cli --phase shipcheck-fixing
```

<!-- CODEX_PHASE4_START -->
When `CODEX_MODE` is true, read [references/codex-delegation.md](references/codex-delegation.md)
for the Phase 4 fix delegation pattern.

When `CODEX_MODE` is false, fix bugs directly.
<!-- CODEX_PHASE4_END -->

Ship threshold (the umbrella's verdict is the canonical signal — all of these must hold for `shipcheck` to exit 0):
- `shipcheck` exits 0. The umbrella's per-leg summary table shows every leg PASS. A non-zero exit is a fix-before-ship blocker, period — do not ship if the umbrella is red.
- `verify` verdict is `PASS` or high `WARN` with 0 critical failures
- `dogfood` no longer fails because of spec parsing, binary path, or skipped examples
- `dogfood` wiring checks pass (no unregistered commands, no config field mismatches)
- `workflow-verify` verdict is `workflow-pass` or `unverified-needs-auth` (not `workflow-fail`). Exception: if the spec or traffic analysis marks browser-session/browser-clearance auth as required, `unverified-needs-auth` is a `hold` verdict until `auth login --chrome`, `doctor --json`, and a read-only browser-session proof pass against the real site.
- `verify-skill` exits 0 (no mechanical mismatches between SKILL.md and CLI source). Treat non-zero as a fix-before-ship blocker — the SKILL is what agents read; if it lies about the CLI, the lie ships.
- `scorecard` is at least 65 and **no flagship or approved-in-Phase-1.5 feature returns wrong/empty output**

**Behavioral correctness is part of the ship threshold, not just structural quality.** A Grade A scorecard with a broken flagship feature (e.g., `goat "brownies"` returning a chili recipe) does NOT pass the ship threshold. Run a sample invocation of every novel-feature command before declaring shipcheck complete.

**Per-source row for combo CLIs (synthetic spec, multiple data sources).** For every named source in a combo CLI (`internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc.) the dogfood test matrix MUST add one row per source: with the source's limiter exhausted (or the upstream genuinely throttling), assert that the user-facing command surfaces a typed `*cliutil.RateLimitError` referencing the source — not empty JSON / `0 results`. A passing row says: "the CLI distinguishes 'no data' from 'we got rate-limited' for this source." The matrix-builder derives rows from the command tree by default; for combo CLIs, also derive rows from the source list. `source_client_check` catches the static signal that throttling is silently swallowed; only the runtime row proves the user-visible behavior.

Maximum 2 shipcheck loops by default.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-shipcheck.md`

Include:
- command outputs and scores
- top blockers found
- fixes applied
- before/after verify pass rate
- before/after scorecard total
- final ship recommendation: `ship` or `hold`

**Verdict rules:**
- `ship`: all ship-threshold conditions met AND no known functional bugs in shipping-scope features.
- `hold`: one or more conditions missing, OR functional bugs exist that cannot be fixed in-session.

`ship-with-gaps` is deprecated as a default verdict. It is NOT valid for bugs that require only 1-3 file edits; those MUST be fixed before ship. It is only acceptable when (a) a bug genuinely requires a refactor, external dependency change, or API access not available in-session, AND (b) the bug is clearly documented with a `## Known Gaps` block in both the shipcheck report and the generated README. If an agent cannot meet both (a) and (b), the verdict is `hold`, not `ship-with-gaps`.

If the final verdict is `hold`, release the lock without promoting to library:
```bash
"$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR` for potential future retry. Proceed to Phase 5.6 to archive manuscripts (archiving still happens on hold).

## Phase 4.7: Sync Param-Drop Gate

**Runs after shipcheck, before Phase 4.8.** Generated endpoint commands are param-cardinality-checked mechanically by `cobratree` against the spec — hand-authored sync / transcendence code is not. When the printed CLI's `internal/syncer/` calls `client.Get(<path>, params)` (or `Post`/`Put`/`Patch`/`Delete` with body params) against an endpoint the browser-sniff capture also observed, the gate compares the passed-key set against the captured-key set and flags any call where the capture is a strict superset of the code. Same JSON structure on both sides; only cardinality drift catches the "5 params here, 11 on the live site" failure mode.

Skip the gate when there's no `traffic-analysis.json` for this CLI (vendor-spec CLIs without a browser-sniff phase). Otherwise:

```bash
printing-press sync-param-drop \
  --dir "$CLI_WORK_DIR" \
  --traffic-analysis "$API_RUN_DIR/<api>-traffic-analysis.json" \
  --strict
```

The same diff also runs as part of `printing-press dogfood` when you pass `--traffic-analysis`; shipcheck's dogfood leg will surface findings as a WARN-level dogfood issue automatically. Running the standalone subcommand with `--strict` during fix iteration gives a focused exit code without re-running the full dogfood matrix.

### Failure handling

A finding tells the reviewer exactly three things: `<file>:<line>: <METHOD> <path> — dropped params: <key1>, <key2>, ...`. The fix is one of:

1. **Add the missing params to the sync call.** This is the default — the live site captured them, so the printed CLI should too. The dropped keys are almost always required for the response shape the CLI's domain commands expect (Factor75: passing `week, country, locale, subscription, product-sku` returns a generic plan preselect; the live site additionally passes `servings, delivery-option, postcode, preference, customerPlanId, include-future-feedback` to get the user's actual cart). Widening the call is the only fix that resolves the underlying bug.
2. **Annotate the call with an evidence-backed opt-out.** Add `// pp:sync-params-intentional-subset reason=<why>` on the line immediately above the call when the subset is genuinely intentional — for example, a logged-out endpoint that doesn't accept the session-bound keys, or a deliberately broader query the CLI surfaces as a separate command. The `reason=` text is preserved in the audit trail; the gate counts suppressed sites separately so unbounded growth surfaces as its own smell.

The gate does not introspect response content. A passing gate proves request-key parity with the captured site, not response correctness — Phase 4.85's agentic output review remains the layer that catches "wrong response shape, right request shape."

### Scope boundary

- The gate inspects `internal/syncer/`, `internal/sync/`, `internal/transcend/`, and `internal/transcendence/`. Generated endpoint command files under `internal/cli/` are already covered by `cobratree`'s mechanical endpoint-surface check and are intentionally skipped to avoid double-flagging.
- Paths the capture never observed (synthetic / transcendence-only endpoints) are not flagged — the gate's question is "does the live site call this path with more keys," and absence of capture is a no-flag state.
- A call that passes a key the capture never observed (extra-keys-from-code) is not flagged — exotic-mode params the public UI never exercised are out of scope.

## Phase 4.8: Agentic SKILL Review

**Runs after shipcheck, before Phase 5.** `verify-skill` (Phase 4) is a mechanical check — it catches wrong flags on wrong commands, undeclared flags, and positional-arg count mismatches. It cannot catch **semantic** issues that only a reader notices:

- A trigger phrase promises behavior the CLI doesn't have ("plan dinners for the week" when there's no `meal-plan suggest`, only manual `meal-plan set`)
- A novel-feature description says the feature does X; the actual command does Y
- The AuthNarrative mentions `auth login --chrome` when the CLI's auth subcommands are only `set-token`/`logout`/`status`
- Novel features shipped as stubs aren't labeled as such in the SKILL (contradicts Phase 1.5 stub-marking rule)
- Recipes/worked examples produce output that doesn't match their prose claims
- Trigger phrases sound agent-natural or sound like marketing copy

### Dispatch

Use the Agent tool (general-purpose or a dedicated reviewer) with this prompt contract:

> Review the SKILL.md at `$CLI_WORK_DIR/SKILL.md` against the shipped CLI. You have these ground-truth sources:
>
> - `<cli> --help` output — enumerate it recursively if needed.
> - The absorb manifest in `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`.
> - The `research.json` `novel_features` (planned) and `novel_features_built` (verified) fields.
> - The README at `$CLI_WORK_DIR/README.md`.
>
> For each of these semantic checks, report findings under 50 words each:
>
> 1. **Trigger phrases match capabilities.** Does every trigger phrase in the SKILL's description frontmatter correspond to something the CLI can actually do? Flag phrases that imply missing capabilities.
> 2. **Verified-set alignment.** The SKILL's "Unique Capabilities" commands must exactly match `novel_features_built` from research.json. Planned-only features from `novel_features` must not appear there after dogfood sync. Any extra or missing command is a finding.
> 3. **Novel-feature descriptions match commands.** For each feature in the "Unique Capabilities" section, run `<cli> <command> --help` and verify the description matches the actual behavior. Mismatches are findings.
> 4. **Stub/gated disclosure.** If a feature that remains in `novel_features_built` is intentionally stubbed, CF-gated, unavailable without external setup, or returns a known-gap response, the SKILL must label that limitation where an agent decides whether to use the command. Unlabeled limitations are findings.
> 5. **Auth narrative accuracy.** Read the auth section. Does every `auth login/set-token/status` invocation mentioned actually exist on the CLI? Does the narrative match the CLI's auth type (api_key vs cookie vs session_handshake)?
> 6. **Recipe output claims.** For the worked examples, does the prose claim match what the command actually produces? (Not the exact output — the shape and intent.)
> 7. **Marketing-copy smell.** Does the SKILL read like ad copy ("comprehensive", "seamless", "powerful") instead of concrete capability descriptions? Those phrases are findings.
>
> Return a list of findings. For each: check name, severity (error/warning), line number, one-sentence fix. If SKILL passes all seven checks, return "PASS — no findings."

**Description source-of-truth fix path.** When a finding says a novel-feature
description, capability summary, or surrounding narrative content is inaccurate,
fix the source text in `$RESEARCH_DIR/research.json`
(`novel_features[].description`, `novel_features[].narrative`, or the matching
`novel_features_built` entry) and regenerate/sync from there. Do not patch only
README.md or SKILL.md for content-level description fixes: the same description
fans out to README "Unique Features", SKILL "Unique Capabilities",
`internal/cli/root.go` Highlights, `internal/cli/which.go`,
`internal/mcp/tools.go`, and `.printing-press.json`. Flag, command, auth,
example, and structural findings that are not generated from research.json keep
their current local fix path.

### Gate

- If the reviewer returns PASS, proceed to Phase 5.
- If the reviewer returns findings of severity `error`, fix them before Phase 5. Same fix-now contract as other shipcheck findings.
- If the reviewer returns only `warning` findings, surface them to the user and proceed if they approve.

### Why agentic vs template-only

A template-level check would require every possible semantic mismatch to be pattern-matchable against source. Many aren't — "does this trigger phrase correspond to what the CLI does" is an LLM-shaped question. Accept the token cost for the catch.

### Known blind spots

The agent can't verify runtime behavior without running commands; stick to help-text and source-based claims. For runtime-behavior claims (e.g., "returns 5 matching recipes"), Phase 5 dogfood is the right gate.

## Phase 4.9: README/SKILL/AGENTS Correctness Audit

**Runs after Phase 4.8, before Phase 5.** Phase 4.8 reviews whether the SKILL's trigger phrases and major claims match shipped behavior. Phase 4.9 reviews the user-facing artifacts as documents: README.md, SKILL.md, and AGENTS.md must not contain boilerplate that does not apply to this CLI.

Use the Agent tool or review directly with this prompt contract:

> Audit `$CLI_WORK_DIR/README.md`, `$CLI_WORK_DIR/SKILL.md`, and `$CLI_WORK_DIR/AGENTS.md` for factual correctness against the shipped CLI. Ground truth is `<cli> --help` recursively, `$CLI_WORK_DIR/internal/cli/*.go`, `$RESEARCH_DIR/research.json`, and the absorb manifest.
>
> Check:
> - Every command, subcommand, flag, exit code, config path, and example resolves to the printed CLI.
> - README `## Unique Features` and SKILL `## Unique Capabilities` match `novel_features_built`; planned-only features from `novel_features` are not claimed after dogfood sync.
> - Surrounding prose, recipes, trigger phrases, and examples do not indirectly promise planned features that dogfood dropped.
> - No placeholder literals remain in executable examples (`<cli>`, `<command>`, `<resource>`, `<CLI>`).
> - Boilerplate matches the CLI shape: no CRUD/retry/create-stdin/delete/cache/auth/async-job claims unless the CLI actually implements them.
> - Read-only CLIs say they are read-only and do not imply create/update/delete support.
> - No-auth CLIs omit auth troubleshooting and auth exit-code claims unless the binary can raise them.
> - Stubbed, CF-gated, or unavailable commands are disclosed where an agent decides whether to use the CLI.
> - The SKILL has anti-triggers: common requests this CLI should not handle.
> - Brand/display names use the canonical prose name from research, not only the slug.
> - Marketing phrases map to real commands; invented feature names are findings.
>
> Return findings with file, line, severity, and fix. If both files are correct, return `PASS — README/SKILL correctness verified`.

**Description source-of-truth fix path.** When a finding says a novel-feature
description, capability summary, or surrounding narrative content is inaccurate,
fix the source text in `$RESEARCH_DIR/research.json`
(`novel_features[].description`, `novel_features[].narrative`, or the matching
`novel_features_built` entry) and regenerate/sync from there. Do not patch only
README.md, SKILL.md, or AGENTS.md for content-level description fixes: the same
description fans out to README "Unique Features", SKILL "Unique Capabilities",
`internal/cli/root.go` Highlights, `internal/cli/which.go`,
`internal/mcp/tools.go`, and `.printing-press.json`. Flag, command, auth,
example, and structural findings that are not generated from research.json keep
their current local fix path.

**Gate:** Any error finding is fix-before-Phase-5. Warnings may proceed only when they are explicitly explained in the acceptance report.

## Phase 4.85: Agentic Output Review

**Runs after Phase 4.8, before Phase 4.95.** Phase 4.8 reviews SKILL.md prose against the shipped CLI. Phase 4.85 reviews the CLI's **actual command output** for plausibility bugs that rule-based checks can't encode (substring-match relevance failures, format bugs, silent source drops, ranking failures). The dispatch prompt, gate logic, and known blind spots live in the `printing-press-output-review` sub-skill — single source of truth shared with the polish skill (which runs the same review during its diagnostic loop).

Invoke the sub-skill via the Skill tool:

```
Skill(
  skill: "cli-printing-press:printing-press-output-review",
  args: "$CLI_WORK_DIR"
)
```

The sub-skill carries `context: fork` so the reviewer agent's diagnostic chatter stays isolated from this generation flow. It returns a `---OUTPUT-REVIEW-RESULT---` block with `status: PASS|WARN|SKIP` and a list of findings.

**Wave B rollout policy:** all findings surface as **warnings**, not blockers. Shipcheck does not fail on Phase 4.85 findings. Log the findings to `manuscripts/<api>/<run>/proofs/phase-4.85-findings.md` and surface them to the user. The user decides case by case whether to fix before shipping. Wave B calibrates false-positive rates before Wave C flips errors to blocking.

## Phase 4.95: Local Code Review

**Runs after Phase 4.85, before Phase 5.** Reviews the printed CLI source for security and correctness issues *before* any PR exists. This is the cheapest fix window in the pipeline — session context is hot, no PR feedback round-trip, no CI comments to chase. Catching issues here means they never become PR-time review comments, which is the wrong fix window for the same problems.

**Target.** The generated CLI and MCP source under `$CLI_WORK_DIR`. In scope: `internal/cli/`, `internal/mcp/` (excluding `cobratree/`), `internal/store/`, `internal/client/`, and `cmd/`. **Out of scope:** `internal/cliutil/` and `internal/mcp/cobratree/` — these are generator-reserved packages. Any finding there is a machine bug; route to retro, do not patch in place.

**Generated description source-of-truth.** If review flags inaccurate
novel-feature description text in generated code surfaces such as
`internal/cli/root.go` Highlights, `internal/cli/which.go`, or
`internal/mcp/tools.go`, do not patch those files directly. Fix the source text
in `$RESEARCH_DIR/research.json` (`novel_features[].description`,
`novel_features[].narrative`, or the matching `novel_features_built` entry) and
regenerate/sync so README "Unique Features", SKILL "Unique Capabilities",
root help Highlights, which output, MCP tools, and `.printing-press.json` stay
aligned. Non-description code findings keep the normal Phase 4.95 autofix path.

**Native timeout-boundary check.** Before reviewer dispatch, scan every hand-written file under `internal/cli/` that imports a sibling internal package (`internal/<api>/`, `internal/source/<name>/`, `internal/recipes/`, `internal/phgraphql/`, etc.) and makes live requests. Each such command file must call `boundCtx(cmd.Context(), flags)` and pass that context into the sibling client or store query path before the first request. Files that only use `flags.newClient()` / generated `internal/client` are already covered by `client.New(cfg, flags.timeout, ...)` and should not be flagged for missing `boundCtx`.

**Tool selection — pick what's installed, do not name-match.** This phase needs *a* code review, not a specific named command. Survey the review-shaped capabilities the current harness has and pick the best fit. Plausible candidates (names drift across harnesses and plugin sets; treat this as an example list, not a closed set):

- A standalone, working-dir-shaped code review skill that runs against `git diff` and a file list without needing an open PR (e.g., `compound-engineering:ce-code-review`, or similar).
- Codex's built-in code-review mode (`/codex:review`), which reviews the current diff or target directly.
- **Direct reviewer-subagent dispatch via the Agent tool.** Spawn `correctness`, `security`, and `maintainability` reviewers (always-on) plus any conditional reviewers warranted by the diff (`api-contract`, `data-migrations`, `reliability`, `performance`) against the in-scope paths. This is the universal fallback: any harness that runs the press skill has the Agent tool, so this path is always available. When dispatching multiple reviewers, a "round" (per the autofix loop below) means re-running *all* spawned reviewers in parallel and merging their findings into a single set before autofix; convergence is the merged set being empty, not any individual reviewer clearing. Do not re-run only the reviewer whose prior findings were touched — every round must include every reviewer so cascading or newly-introduced issues surface.

**Do not invoke Claude Code's `/review` for this phase.** `/review` is PR-shaped — it fetches an open GitHub PR and comments back via `gh`. There is no PR yet at Phase 4.95; the CLI is in a working dir that has not been promoted or published. Reaching for `/review`, bouncing off its shape, and claiming "harness has no code review" is the failure mode this section is written to prevent.

**Autofix policy.** Session context is hot, no PR feedback round-trip, no publish decision in flight. The default is fix. Surfacing to the user is the exception, not the rule. Severity is informational, not gating: a low-severity nil-deref is a 30-second fix; close it the same as a high-severity one.

Fix without asking when:
- The fix is mechanical (parameterized query, input validation, error wrapping, missing nil check, dead code removal, obvious refactor).
- The fix is small-scope and behavior-preserving from the README's point of view.
- There is no plausible competing implementation a reasonable user would prefer over the chosen one.

Surface to the user only when the fix requires a real tradeoff they have to make. Real tradeoffs look like:
- **Shipping scope shrinks.** Closing the finding cleanly means dropping or significantly degrading a Phase 1.5-approved feature. (Per the Rules section, scope changes route back to Phase 1.5 for re-approval, not a silent shrink here.)
- **Two materially different valid fixes** with different cost, surface, or dependency profiles, and either is defensible.
- **The finding implies a Phase 1 research miss** — wrong primary source, wrong auth model, wrong transport — that the agent cannot resolve from in-session context.
- **The fix re-triggers a long phase** (re-running browser-sniff, regen from spec, etc.).

Treat agent judgment as sufficient here — these categories are distinguishable on inspection. Conservatism is the failure mode, not over-fixing. Drafting an AskUserQuestion because "the user might want to know" is premature; fix the issue and note it in the shipcheck report.

Re-run the review after each autofix round until findings clear. Cap at 3 rounds; if findings persist after round 3, stop and surface — autofix is not converging. Findings in out-of-scope paths (`internal/cliutil/`, `internal/mcp/cobratree/`) file as retro-candidates and do not count toward the convergence check or the 3-round cap; the convergence check applies only to in-scope findings.

**Findings artifact.** Log to `manuscripts/<api>/<run>/proofs/phase-4.95-findings.md`. Skip the per-finding enumeration for fixed-in-place items — the commits and diffs are already the authoritative record. Specifically:
- **Autofix summary (one line).** "N findings autofixed in-place across M rounds; see commits `<hash>`, `<hash>`, …" Do not enumerate the fixed findings.
- **Template-shape retro candidates (full detail).** Each finding's file:line, severity, the template path it appears to come from, and why it was filed instead of fixed. Not fixed in-place, so the log is the only record.
- **Out-of-scope retro candidates (full detail).** Findings in `internal/cliutil/` or `internal/mcp/cobratree/`. Same shape as template-shape entries.
- **Surface-to-user findings (full detail).** Each finding's file:line, severity, the real-tradeoff category it falls into, and the user's decision once they make one. Pending between turns; the log is what carries them.
- **Convergence outcome (one line).** "Findings cleared at round N" or "stopped at round 3 with N findings outstanding — see surface-to-user list."
- **Review path chosen (one line).** Skill name + invocation form, or "direct subagent dispatch" with the persona list. Lets a retro audit tool-selection drift across runs.

The retro skill scans the template-shape and out-of-scope sections for candidates worth filing against the machine.

**Rollout posture.** Unlike Phase 4.85, this phase starts without a warnings-only calibration period. Local code review is a well-understood surface — calibration risk is low. The 3-round autofix cap is the safety net for runaway findings, and the template-shape escape hatch routes systemic issues to retro instead of patching in place.

**Template-shape escape hatch.** Even if a finding lives in an in-scope path, if it appears to come from a generator template (recurs across files in identical shape, sits in a path matched by `internal/generator/templates/`'s emit set, or duplicates a known prior template bug), file as retro-candidate and surface to the user rather than autofixing. Patching the printed CLI hides the machine bug from the next CLI.

**Post-fix simplification (Claude Code only).** After the review + autofix loop converges, the printed CLI has fresh edits from the autofix passes — typically defensive guards, sanitization helpers, and near-duplicate fixes across sibling files. Run `/simplify` scoped to the same in-scope paths to consolidate duplication, remove dead code, and tighten the autofix output before dogfood. `/simplify` is Claude Code-only; skip on Codex and other harnesses (they have no built-in equivalent, and the press skill explicitly avoids custom simplification logic — same rule as the review path above).

**Harness exemption — narrow.** Skipping this phase is legitimate only when the current harness has *neither* a working-dir-shaped review skill *nor* the Agent/subagent capability needed for the direct-dispatch fallback. In practice this is almost never true — any harness that runs the press skill has access to subagents. The following rationales are **not** acceptable for skipping:

- "The first tool name I tried (e.g., `/review`, `code-review:code-review`) didn't fit, so the harness must have no review path." Survey the tool catalog before claiming exemption; if no skill fits, dispatch reviewer subagents directly via the Agent tool.
- "There's no PR yet, so code review can't run here." Pre-PR is the *point* of this phase. CI-time PR review is too late.
- "PR-time CI review will catch it." That defeats the purpose of running review in the cheapest fix window.

If a skip is genuinely warranted, the shipcheck report must state which review-shaped capabilities were searched and why none fit — not just "harness exemption."

## Phase 5: Dogfood Testing

**MANDATORY when an API key is available. Do NOT skip or shortcut this phase.**

Shipcheck verified commands start and return exit codes. Dogfood verifies the CLI
produces correct, useful output for real workflows. These are different checks.

### Step 1: Ask the user for depth

Present via `AskUserQuestion`:

> "Shipcheck passed. How thoroughly should I test against the live API?"
>
> 1. **Full dogfood (recommended)** — Complete mechanical test matrix across every leaf subcommand, including help, happy-path, JSON parse validation, output-mode fidelity, and error paths. Includes write-side lifecycle only with an approved disposable fixture/sandbox plan.
> 2. **Quick check** — A compromise subset when the user explicitly wants speed or full dogfood would consume unapproved real-world cost/side effects.

**Recommendation rule:** Full dogfood is the default recommendation. Do not downgrade because of ordinary time cost; a few extra minutes is cheap compared with the generation run and the cost of shipping a broken CLI. Recommend Quick only when the user asks for speed or when full live testing would create unapproved real-world cost/side effects (paid credits, outbound messages, public posts, real orders, irreversible deletes, invites, bookings, charges). Potential mutation is not itself a reason to downgrade: if the user approves a test account/workspace/calendar/project or the CLI can create and clean up disposable fixtures, Full dogfood remains recommended.

There is no skip option when an API key is available. Phase 5 auto-skips ONLY
when the API requires auth AND no key is available: display "No API key
available — skipping live dogfood testing. The CLI was verified against exit
codes and dry-run only."

For APIs with `auth.type: none` (or no auth section in the spec), Phase 5
is MANDATORY — the API is freely testable without any credentials. Do not
skip testing just because no API key was detected. No-auth APIs are the
easiest to test and the most embarrassing to ship untested.

**LAN-only no-auth carve-out.** Some no-auth APIs are real hardware or
private-network APIs that are testable only from the user's LAN (SSDP, mDNS,
RFC1918/private hostnames, localhost-shaped appliance endpoints). If Phase 5
cannot reach the hardware because the generation host is not on that LAN, do
not fabricate an API-key skip and do not hand-author `phase5-acceptance.json`.
Ask the user whether to hold the CLI or skip live dogfood and promote anyway.
Only when the user explicitly chooses the skip/promote path, write
`phase5-skip.json` with `skip_reason:
"lan-unreachable-from-generation-host"`, `auth_context.type: "none"`, and
`auth_context.local_network_only: true`.

Do NOT proceed without asking. Do NOT substitute an ad-hoc smoke test. If some commands cannot be exercised because fixture values are missing, classify them as `BLOCKED_FIXTURE` and file/fix the machine gap; do not use that as a reason to recommend Quick.

### Step 2: Run the binary-owned test matrix

**Full dogfood is not a judgment call about "enough."** Run the Printing
Press-owned live matrix so command enumeration, exit-code capture, JSON parsing,
and acceptance-marker writing are deterministic:

```bash
cli-printing-press dogfood --live \
  --dir "$CLI_WORK_DIR" \
  --level full \
  --json \
  --write-acceptance "$PROOFS_DIR/phase5-acceptance.json"
```

Use `--level quick` only when the user selected Quick Check in Step 1.

**Cookie / composed / session-handshake auth.** The runner executes the binary
in a sandboxed HOME with no captured browser session, so a CLI whose auth is a
cookie jar will 401 every command unless you hand it the session. To exercise
the matrix for real, point the CLI's config-override env var (the
`<PREFIX>_CONFIG`-style override, e.g. `<API>_CONFIG`) at a `config.toml` that
carries the captured session, and export it before the `dogfood --live` call:

```bash
<API>_CONFIG="$SESSION_DIR/config.toml" cli-printing-press dogfood --live \
  --dir "$CLI_WORK_DIR" --level full --json \
  --write-acceptance "$PROOFS_DIR/phase5-acceptance.json"
```

If no captured session is available to the harness, the runner does not count
the 401s as failures: it records a clean skip verdict
(`skip-cookie-auth-no-session`) and writes `phase5-skip.json` with
`skip_reason: "cookie-auth-no-harness-session"` (see Step 4). The promote gate
accepts that skip marker, so do not hand-author or fabricate a pass marker for a
cookie-auth CLI you could not exercise.

The live dogfood runner enumerates the CLI's `agent-context` command tree,
runs help, happy-path, JSON-fidelity, and error-path checks where applicable,
captures subprocess exit codes directly without shell pipes, and emits a
structured report with pass/fail/skipped counts. Save the JSON report to:

`$PROOFS_DIR/<stamp>-dogfood-results.json`

If the command exits non-zero, inspect the structured failures, fix the CLI, and
rerun live dogfood. The runner writes `phase5-acceptance.json` on every outcome
(`status: "pass"` on success, `status: "fail"` with a `failure_summary` block on
failure), so the Phase 5.6 gate always has a marker to read. Do not hand-edit
`phase5-acceptance.json`; it must come from the runner.

**Quick check (auto-selected test subset):**
1. `doctor` — auth valid, API reachable.
2. 3-5 list commands — return data, not empty.
3. `sync --full` → data appears in local store.
4. `search "<term from synced data>"` — finds results.
5. One list command with `--json`, `--select <fields>`, `--csv` — all produce correct output.
6. One transcendence command — produces output that relates to the query (not just non-empty: verify relevance by checking output content contains query tokens or expected shape).

**Full dogfood adds to the matrix:**
- Every approved feature in the Phase 1.5 manifest gets a sample invocation with domain-realistic args.
- For every command that takes an arg, one error-path test.
- For every command that supports `--json`, one JSON parse validation.
- For write-side commands (when API key + user consent): create test entity with obviously-test data, verify in subsequent list/get, test one mutation, verify change.

### Step 3: Fix issues inline

When a test fails, fix it immediately — do not accumulate failures. Tag each fix:
- **CLI fix** — specific to this printed CLI
- **Printing Press issue** — should be fixed in the Printing Press (note for retro)

### Step 4: Report and gate

Write a structured acceptance report and a machine-readable gate marker. The
JSON marker is **required** — Phase 5.6 and `publish validate` check for it
before promoting or publishing.

```
Acceptance Report: <api>
  Level: Quick Check / Full Dogfood
  Tests: N/M passed
  Failures:
    - [command]: expected [X], got [Y]
  Fixes applied: K
    - [each fix]
  Printing Press issues: J
    - [each issue for retro]
  Gate: PASS / FAIL
```

**Redact PII while authoring the report.** When live API responses include an
organization or workspace name, user email, assignee/collaborator name, or any
other human-identifying string, describe the result generically instead of
quoting the literal value:
- organization or workspace name -> "the test workspace"
- authenticated user email/name -> "the authenticated viewer"
- assignees or collaborators -> "the highest-loaded assignee" / "the project lead"
- team identifiers such as `ENG` or `T2` are OK when they are structural keys

The Phase 5.6 manuscript scan and publish-skill PII scan are defense in depth;
keep PII out of the acceptance report from the moment you write it.

**Acceptance threshold:**
- Quick Check: 5/6 core tests must pass. Auth (`doctor`) or sync failure is automatic FAIL.
- Full Dogfood: every mandatory test in the matrix must pass. A single broken flagship feature is automatic FAIL. Auth/sync failures are automatic FAIL.

**Bugs surfaced in Phase 5 must be fixed now, not deferred.** Do not offer the user a "ship as-is and file for v0.2" option when the fix is a 1-3 file edit. Present a "Fix now" (default), "Fix critical only", "Hold (don't ship)" set. Deferring bugs to a v0.2 backlog is an anti-pattern — context is freshest in-session, and a backlog that may never be revisited ships known-broken CLIs.

**Gate = PASS:** proceed to Phase 5.5 (Polish).

**Gate = FAIL:** fix issues inline (Step 3) and re-run failing tests, up to
2 fix loops. If the gate still fails after 2 loops, put the CLI on hold:
```bash
"$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR`. Proceed to Phase 5.6 to archive
manuscripts (archiving still happens on hold). Tag the failure reason in the
acceptance report so the next run can learn from it.

See [references/dogfood-testing.md](references/dogfood-testing.md) for additional
guidance on common failure patterns and what NOT to test.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-acceptance.md`

For every outcome (PASS or FAIL), the runner writes:

`$PROOFS_DIR/phase5-acceptance.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "pass",
  "level": "quick|full",
  "matrix_size": 42,
  "tests_passed": 42,
  "tests_failed": 0,
  "auth_context": {
    "type": "none|api_key|bearer_token|cookie|composed|session_handshake",
    "api_key_available": true,
    "browser_session_available": false
  }
}
```

On `Gate: FAIL` the same path is written with `status: "fail"` and a
`failure_summary` block grouping failures by category
(`transport_error` / `http_4xx` / `http_5xx` / `exit_nonzero` /
`output_mismatch` / `other`) plus the list of contributing commands. The
Phase 5.6 gate routes this marker to the hold path; do not promote.

For `level: "quick"`, `tests_failed` may be `1` only when the Quick Check
threshold still passed (`matrix_size: 6`, `tests_passed >= 5`) and the miss was
not auth or sync related. For `level: "full"`, `tests_failed` must be `0`.

If Phase 5 is legitimately skipped because the API requires API-key or bearer
auth and no credential was available, write:

`$PROOFS_DIR/phase5-skip.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "skip",
  "level": "none",
  "skip_reason": "auth_required_no_credential",
  "auth_context": {
    "type": "api_key|bearer_token|oauth2",
    "api_key_available": false,
    "browser_session_available": false
  }
}
```

If Phase 5 is legitimately skipped because a no-auth API is LAN-only and the
generation host cannot reach the user's LAN hardware, write:

`$PROOFS_DIR/phase5-skip.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "skip",
  "level": "none",
  "skip_reason": "lan-unreachable-from-generation-host",
  "auth_context": {
    "type": "none",
    "api_key_available": false,
    "browser_session_available": false,
    "local_network_only": true
  }
}
```

If a cookie / composed / session-handshake CLI could not be exercised because
no captured session was available to the harness (the sandboxed HOME has no
cookie jar, so every command 401s), the runner writes this skip marker for you
on a `skip-cookie-auth-no-session` verdict — do not hand-author it:

`$PROOFS_DIR/phase5-skip.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "skip",
  "level": "none",
  "skip_reason": "cookie-auth-no-harness-session",
  "auth_context": {
    "type": "cookie|composed|session_handshake",
    "browser_session_available": false
  }
}
```

Prefer the inject-session path in Step 2 (pass the captured session via the
`<API>_CONFIG` override) so the matrix runs for real; the skip is the floor when
no session is available.

Do **not** write a skip marker for ordinary `auth.type: none` cloud/public APIs.
No-auth APIs are testable and require `phase5-acceptance.json` unless they match
the LAN-only carve-out above. Do **not** use missing API key
(`auth_required_no_credential`) as the skip reason for cookie, composed, or
session-handshake auth; inject the session, or rely on the runner-emitted
`cookie-auth-no-harness-session` skip when none is available.

## Phase 5.5: Polish

**Always runs.** Invoke the `printing-press-polish` skill to run diagnostics, fix quality issues, and return a delta. The polish skill carries `context: fork` in its frontmatter, so its diagnostic-fix-rediagnose loop runs in a forked context — diagnostic spam, fix iterations, and re-audits stay scoped to the polish session and don't pollute this generation flow. The skill is autonomous — no user input needed. The goal is to ship the best CLI possible, not the fastest.

Before invoking polish, collect the Phase 3 transcendence gate state and include
it in the polish input bundle. Include the captured `PRINTING_PRESS_BIN` value
so the forked polish skill uses the same preflight-selected binary instead of
resolving a potentially stale global binary from PATH:

```yaml
printing_press_bin: <captured PRINTING_PRESS_BIN>
phase3_transcendence_rows_planned: <planned>
phase3_transcendence_rows_built: <built>
phase3_transcendence_rows_missing:
  - <manifest row name or command>
prior_sub60_reprint: <true|false>
partial_transcendence_override: <none or build-log note path>
```

Invoke via the Skill tool (**foreground** — must complete before promoting).
Pass `$CLI_WORK_DIR` as the first line of `args`, followed by the Phase 3 bundle:

```
Skill(
  skill: "cli-printing-press:printing-press-polish",
  args: "$CLI_WORK_DIR
printing_press_bin: <captured PRINTING_PRESS_BIN>
phase3_transcendence_rows_planned: <planned>
phase3_transcendence_rows_built: <built>
phase3_transcendence_rows_missing:
  - <manifest row name or command>
prior_sub60_reprint: <true|false>
partial_transcendence_override: <none or build-log note path>"
)
```

Polish must treat `prior_sub60_reprint: true` plus any missing row as `ship_recommendation: hold` unless `partial_transcendence_override` names the accepted exception. This keeps mid-pipeline polish from recommending `ship` for a reprint that regressed from the approved manifest before Phase 6 sees the artifact.

**Pass `$CLI_WORK_DIR` (the absolute working-dir path), not the API slug.** Phase 5.5 fires before Phase 5.6 promotes the working CLI to the library, so `$PRESS_LIBRARY/<slug>/` either doesn't exist yet or contains the *prior* run's CLI. If you paraphrase the args to the slug (e.g., `args: "producthunt"`), polish silently operates on the stale library copy.

**Do not pass `--standalone` in `args`.** Polish's Publish Offer is gated on caller mode (see polish SKILL.md "Publish Offer"): slash-command invocations or Skill-tool invocations carrying `--standalone` run the offer; everything else defers. Phase 5.5 is mid-pipeline — main SKILL owns the publish flow at Phase 6 — so this invocation must remain flag-free. Passing `--standalone` here would re-introduce the failure mode the flag was added to prevent: polish forks the public library, sets global git config, and opens a real PR before the working CLI has been promoted.

The polish skill runs the full diagnostic-fix-rediagnose loop including MCP tool quality polish (via `cli-printing-press tools-audit` plus the playbook at `references/tools-polish.md`) and ends its response with a `---POLISH-RESULT---` block containing scorecard/verify/tools-audit before/after, fixes applied, and a ship recommendation.

Parse the result block. Display the delta to the user:

```
Polish pass:
  Verify:      86% → 93% (+7%)
  Scorecard:   92  → 94  (+2)
  Tools-audit: 76  → 0   pending findings
  Fixed: [summary of fixes_applied from result]
```

**Verdict override:** If the polish skill's `ship_recommendation` is `hold` and the Phase 4 verdict was `ship`, downgrade to `hold`. Release the lock without promoting.

Mid-pipeline polish does **not** run `publish-validate` — that gate is the publish skill's responsibility at Phase 6, where the prerequisites it checks (manifest.printer from `git config github.user`, packaged `tools-manifest.json`, phase5 acceptance proof under `$CLI_DIR/.manuscripts/<run>/proofs/`) are actually satisfied. Polish emits `publish_validate_before: skipped (mid-pipeline)` and `publish_validate_after: skipped (mid-pipeline)` in this invocation; treat those values as informational, never as a hold signal. A first-time user without `git config github.user` set will no longer see their CLI-level run downgraded to `hold` because of a publish prerequisite that the press itself owns satisfying.

Write the polish skill's full response to:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-polish.md`

## Phase 5.6: Promote and Archive

### Acceptance gate check

Before promoting, verify the Phase 5 JSON gate marker:

- If `$PROOFS_DIR/phase5-acceptance.json` exists with `status: "pass"` → proceed to promote.
- If `$PROOFS_DIR/phase5-acceptance.json` exists with `status: "fail"` → CLI is on hold. Do NOT promote. Proceed to Archive Manuscripts.
- If `$PROOFS_DIR/phase5-skip.json` exists and the auth-aware skip is valid → proceed to promote.
- If neither JSON marker exists → Phase 5 was skipped or not recorded. Go back and run it, or write the valid skip marker. Do NOT promote without one.

### Promote to Library

If the shipcheck verdict is `ship` **or** `ship-with-gaps`, promote the verified CLI from the working directory to the library. This must happen BEFORE archiving — the CLI in the library is the primary deliverable, and Phase 6's publish path expects `$PRESS_LIBRARY/<api>/` to hold the current run.

For reprints, creator attribution is guarded in two places. The generate command seeds `$CLI_WORK_DIR` from `$PRESS_LIBRARY/<api>/.printing-press.json` when that same-API manifest has a non-empty permanent `creator`, and records the reprinter as the first `contributors[]` entry when they differ from the creator. `lock promote` repeats the same check before swapping: if the staged working tree still carries the operator as creator, it restores the library creator, prepends the staged creator as contributor, and rewrites generated attribution surfaces (Go headers, README byline, SKILL `author:`, NOTICE/LICENSE copyright lines). A promoted reprint must never silently replace the library creator with the operator's git identity.

**Pick the promote path by whether the library already holds hand-authored content.** `lock promote --dir` performs an **atomic swap** of `$CLI_WORK_DIR` over `$PRESS_LIBRARY/<api>` — every file in the library that is not in the fresh tree is gone after the swap. Whole hand-authored files (a separate `internal/syncer/` package, novel-feature command files under `internal/cli/` without the `// Generated by ...` header, hand-built migration files under `internal/store/`) survive a `cli-printing-press regen-merge` pass but are wiped by a bare swap. This is the same preservation dynamic called out under [**Hand-edits must be regen-mergeable.**](#hand-edit-durability); the orchestration here must honor it.

Detect hand-authored content in the existing library:

```bash
LIB_TARGET="$PRESS_LIBRARY/<api>"
NOVEL_COUNT=0
if [ -d "$LIB_TARGET" ]; then
  MANIFEST="$LIB_TARGET/.printing-press.json"
  HAS_NOVEL_FIELD=false
  if [ -f "$MANIFEST" ]; then
    # `novel_features` is the canonical count when the field is present.
    # Distinguish absent (older manifest predating the field) from explicit zero —
    # `jq '.novel_features // [] | length'` collapses both to 0, which would
    # silently route older CLIs through the destructive Path A swap.
    HAS_NOVEL_FIELD=$(jq 'has("novel_features")' "$MANIFEST" 2>/dev/null || echo false)
  fi
  if [ "$HAS_NOVEL_FIELD" = "true" ]; then
    NOVEL_COUNT=$(jq -r '.novel_features | length' "$MANIFEST" 2>/dev/null || echo 0)
  else
    # File probe fallback fires in three cases: manifest missing entirely
    # (interrupted run, restored-from-backup state, much older CLI), manifest
    # present but predates the `novel_features` field, and manifest present
    # but corrupt-enough that `jq has()` errored. Any *.go file under
    # internal/cli/, internal/syncer/, or internal/store/ whose first 20
    # lines lack the "Generated by CLI Printing Press" header is hand-authored;
    # one such file is enough to route through Path B.
    for dir in "$LIB_TARGET/internal/cli" "$LIB_TARGET/internal/syncer" "$LIB_TARGET/internal/store"; do
      [ -d "$dir" ] || continue
      while IFS= read -r f; do
        if ! head -n 20 "$f" 2>/dev/null | grep -q "Generated by CLI Printing Press"; then
          NOVEL_COUNT=1
          break 2
        fi
      done < <(find "$dir" -type f -name '*.go')
    done
  fi
fi
```

The presence check (`jq 'has("novel_features")'`) and the manifest existence check are independent. A library can exist with a hand-authored layer but no manifest at all (interrupted run, restored-from-backup state, much older CLI), so gating the file-probe fallback behind `[ -f manifest ]` would leave that case routing through the destructive Path A swap.

Before choosing Path B for `NOVEL_COUNT > 0`, distinguish preservation
from from-scratch replacement. A reprint that rebuilt every prior novel into
`$CLI_WORK_DIR` has no unique library content left to preserve; routing that
run through `regen-merge --apply` turns ordinary older-generator drift into a
manual halt and can preserve stale generated framework code.

Run a dry-run report and inspect it against the prior manifest / research
novel list:

```bash
REGEN_DRY_RUN_REPORT="$PROOFS_DIR/regen-merge-dry-run-report.json"
PATH_A_REBUILT_NOVELS=0
if [ -d "$LIB_TARGET" ] && [ "$NOVEL_COUNT" -gt 0 ]; then
  if ! "$PRINTING_PRESS_BIN" regen-merge "$LIB_TARGET" \
      --fresh "$CLI_WORK_DIR" --json > "$REGEN_DRY_RUN_REPORT"; then
    # Real error (input error, missing fresh tree, unreadable library). Release
    # the upstream pipeline lock and surface the dry-run failure.
    "$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
    echo "regen-merge dry-run failed; see $REGEN_DRY_RUN_REPORT" >&2
    exit 1
  fi

  DRY_RUN_BLOCKERS=$(jq '[.files[]? | select(.verdict == "NOVEL"
    or .verdict == "NOVEL-COLLISION")] | length' "$REGEN_DRY_RUN_REPORT")
  MISSING_REFERENTS=$(jq '[.lost_registrations[]?
    | select((.skipped_for_missing_referent // []) | length > 0)] | length' \
    "$REGEN_DRY_RUN_REPORT")
  if [ "$DRY_RUN_BLOCKERS" -eq 0 ] && [ "$MISSING_REFERENTS" -eq 0 ]; then
    PATH_A_REBUILT_NOVELS=1
  fi
fi
```

For a **from-scratch reprint whose fresh tree reimplements all prior novels**,
prefer Path A. The dry-run should show the prior novel surfaces represented in
fresh output, usually as `TEMPLATED-CLEAN` or `NEW-TEMPLATE-EMISSION`, and must
not show any preservation-only novel surface that the fresh tree lacks. Treat
generated-file `TEMPLATED-BODY-DRIFT`, `TEMPLATED-VALUE-DRIFT`, and stale
templated-helper `TEMPLATED-WITH-ADDITIONS` as expected overwrite noise in this
specific branch; Path A intentionally swaps in the fresh tree.

The override is forbidden unless the fresh tree contains the novels:

- If `DRY_RUN_BLOCKERS > 0` because any prior novel file still reports `NOVEL`,
  the fresh tree did not rebuild
  that hand-authored file. Use Path B so the file is preserved.
- If `DRY_RUN_BLOCKERS > 0` because any file reports `NOVEL-COLLISION`, halt
  through Path B's normal review gate. A collision is not version drift.
- If `MISSING_REFERENTS > 0` because
  `lost_registrations[].skipped_for_missing_referent` is non-empty, use
  Path B and investigate; the fresh tree is missing a command constructor that
  published wiring still references.
- If you cannot prove the fresh tree rebuilt every prior novel feature from the
  manifest / research record, use Path B. A false Path A clobbers hand work; a
  false Path B only asks for review.

**Path A — first print, no hand-authored content, or from-scratch reprint with all novels rebuilt (`! -d "$LIB_TARGET"` or `NOVEL_COUNT == 0` or the guarded dry-run override above passed).** Use the destructive swap. Fast path; no library content to preserve:

```bash
# Atomic swap: copies working dir, writes manifest, updates run pointer, releases lock.
"$PRINTING_PRESS_BIN" lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"
```

The `promote` command handles the full sequence: stages the working directory, atomically swaps it into `$PRESS_LIBRARY/<api>` (slug-keyed), writes the `.printing-press.json` manifest, updates the `CurrentRunPointer`, and releases the lock — all in one step. The `--cli` flag accepts the CLI binary name; the Go code translates to the slug-keyed library path internally.

**Path B — reprint over a library with hand-authored content that the fresh tree did not fully rebuild (`-d "$LIB_TARGET"` AND `NOVEL_COUNT > 0` AND the guarded Path A override did not pass).** Use `regen-merge` to fold the fresh tree into the live library before promotion. `regen-merge` classifies every Go file under `internal/` and `cmd/` against the fresh tree, overwrites safely-templated files, re-injects `AddCommand` calls in `root.go` and resource-parents that the fresh tree lacks, and leaves files with hand-edited additions (`TEMPLATED-WITH-ADDITIONS`) untouched for human review. `--apply` writes via stage-and-swap-with-recovery, so partial failure can never lose data.

`regen-merge --apply` exits 0 even when it leaves `TEMPLATED-WITH-ADDITIONS` files (the human-review verdicts are reported, not raised as errors). The halt condition must be checked explicitly against the report — capture `--json` and inspect the verdict counts:

```bash
REGEN_REPORT="$PROOFS_DIR/regen-merge-report.json"
if ! "$PRINTING_PRESS_BIN" regen-merge "$LIB_TARGET" \
    --fresh "$CLI_WORK_DIR" --apply --json > "$REGEN_REPORT"; then
  # Real error (input error, apply failure). Release the lock — it was
  # acquired upstream by the press pipeline; regen-merge does not own it —
  # and surface the failure to the user.
  "$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
  echo "regen-merge --apply failed; see $REGEN_REPORT" >&2
  exit 1
fi

# Halt on review-required verdicts before promoting. regen-merge exits 0
# in these cases; the JSON report is the source of truth.
NEEDS_REVIEW=$(jq '[.files[] | select(.verdict == "TEMPLATED-WITH-ADDITIONS"
  or .verdict == "TEMPLATED-BODY-DRIFT"
  or .verdict == "TEMPLATED-VALUE-DRIFT"
  or .verdict == "NOVEL-COLLISION")] | length' "$REGEN_REPORT")
if [ "$NEEDS_REVIEW" -gt 0 ]; then
  # Release the lock so the next reprint of this CLI is not blocked until
  # timeout. lock promote would have released it; the halt path must too.
  "$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
  echo "regen-merge flagged $NEEDS_REVIEW file(s) for human review. " \
       "Inspect $REGEN_REPORT, resolve inline hand-edits, then re-run." >&2
  exit 1
fi
```

After `regen-merge` succeeds with no review-required verdicts, the live library directory is the new run. Do **not** then call `lock promote --dir "$CLI_WORK_DIR"` — that would atomically swap the working dir over the just-merged library and undo the preservation. Promote in place: point `lock promote --dir` at the library itself so the manifest write, run-pointer update, and lock release still run. Two extra steps are required compared to Path A:

1. Copy the current run's PII-polish ledger into `$LIB_TARGET` before promote. `lock promote --dir` internally runs `validatePIIGateForPromote` against the target directory, which reads `$LIB_TARGET/.printing-press-pii-polish.json`. After `regen-merge --apply` the generator-emitted Go files have fresh line numbers, so the prior reprint's ledger (still sitting in `$LIB_TARGET` from the last atomic swap) has stale `{file, line, kind, span}` identity keys for those files and previously-accepted findings re-surface as pending — the gate fails before the swap and the lock stays held. Bringing the current run's ledger over fixes the identity match for generator-emitted files; hand-authored files with new findings still surface correctly as pending.
2. Guard the promote with an explicit lock-release on failure. Unlike Path A, where a promote-gate failure simply leaves the working dir alone, a Path B failure leaves the lock held on the live library because the gate fires before the swap and before `ReleaseLock`. Mirror the lock-release guards from the regen-merge error branches above.

```bash
if [ -f "$CLI_WORK_DIR/.printing-press-pii-polish.json" ]; then
  cp "$CLI_WORK_DIR/.printing-press-pii-polish.json" "$LIB_TARGET/.printing-press-pii-polish.json"
else
  # Current run produced no PII findings (clean API or polish skipped).
  # Remove the stale prior-reprint ledger so the gate sees a clean state
  # — otherwise the old identity keys would replay against freshly
  # line-shifted generator files and surface false-positive pendings.
  rm -f "$LIB_TARGET/.printing-press-pii-polish.json"
fi
"$PRINTING_PRESS_BIN" lock promote --cli <api>-pp-cli --dir "$LIB_TARGET" || {
  "$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
  echo "lock promote failed for $LIB_TARGET; lock released. " \
       "Inspect the PII gate output above and resolve before re-running." >&2
  exit 1
}
```

`TEMPLATED-WITH-ADDITIONS` and the other review verdicts represent inline hand-edits to generator-emitted files that need human review (see [**Hand-edits must be regen-mergeable.**](#hand-edit-durability) for the separate-file pattern that avoids this in future). The dry-run report (omit `--apply`) is the right tool for inspection once the halt path fires.

`ship-with-gaps` is promoted (on either path) because the verdict means "the CLI is shippable with documented, non-blocking gaps" — the gaps are recorded in the README's `## Known Gaps` block and the user opts in via Phase 6's publish prompt. Treating ship-with-gaps as un-promotable would strand the verified working copy and leave the library on a stale prior run.

If the shipcheck verdict is `hold`, the lock was already released in Phase 4. Do NOT promote on either path. The working copy stays in `$CLI_WORK_DIR` and is not copied to the library.

### Archive Manuscripts

Archive the run's research, proofs, and discovery artifacts to `$PRESS_MANUSCRIPTS/`
**unconditionally** after promotion (or after lock release for `hold` verdicts). This
happens regardless of the shipcheck verdict — even a `hold` run produces research
and proofs that future runs should be able to reuse.

Archiving and publishing are separate concerns. Archiving preserves research for
future `/printing-press` runs on the same API. Publishing ships the CLI to the
library repo. A run that isn't ready to publish still produces valuable research.

```bash
# Archive under API slug (e.g., steam-web), matching the slug-keyed library layout.
API_SLUG="<api>"
mkdir -p "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID"
mkdir -p "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs"
cp -r "$RESEARCH_DIR/." "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research/" 2>/dev/null || true
cp -f "$API_RUN_DIR/research.json" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/research.json" 2>/dev/null || true
cp -r "$PROOFS_DIR/." "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs/" 2>/dev/null || true

# Archive discovery artifacts (browser-sniff captures, URL lists, traffic analysis, browser-sniff report).
# Session state lives outside $DISCOVERY_DIR (see Run Initialization), so the
# archive cannot pick it up. The legacy rm is a no-op safety net for an
# in-flight $DISCOVERY_DIR carried over from a pre-isolation run.
rm -f "$DISCOVERY_DIR/session-state.json" 2>/dev/null || true

# Strip response bodies from HAR before archiving to control size.
if [ -d "$DISCOVERY_DIR" ]; then
  for har in "$DISCOVERY_DIR"/browser-sniff-capture.har "$DISCOVERY_DIR"/browser-sniff-capture.json; do
    if [ -f "$har" ] && command -v jq >/dev/null 2>&1; then
      jq 'del(.log.entries[].response.content.text)' "$har" > "${har}.stripped" 2>/dev/null && mv "${har}.stripped" "$har" || rm -f "${har}.stripped"
    fi
  done
  cp -r "$DISCOVERY_DIR" "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/discovery" 2>/dev/null || true
fi

# Wipe live-auth scratch dir now that the run is archived. The directory lives
# under ${TMPDIR:-/tmp}, so OS-level tmp reaping is the long-tail fallback, but
# we clean explicitly so back-to-back runs do not accumulate session state.
rm -rf "$SESSION_DIR" 2>/dev/null || true
```

**MANDATORY: After archiving, you MUST proceed to Phase 6 below. Do not print a summary and stop. Do not treat archiving as the end of the run. The run ends when the user has been asked about next steps via the ship-path or hold-path menu.**

## Phase 6: Next Steps

**This phase is NOT optional.** Every run MUST reach this point — both `ship` and `hold` verdicts get a menu. Do not skip it.

After archiving, offer the user the next action. The menu shape is determined by the shipcheck verdict and (for ship runs) by polish's self-assessment.

### Gate

Use the most recent shipcheck verdict:
- if Phase 5 reran shipcheck after a live-smoke fix, use that rerun verdict
- otherwise use the Phase 4 verdict
- if Phase 5.5 polish downgraded the verdict (`ship_recommendation: hold`), use the downgraded verdict

Route to the menu shape:
- `ship` or `ship-with-gaps` → **ship-path menu** (below)
- `hold` → **hold-path menu** (below). The CLI did not promote to library; the working copy stays in `$CLI_WORK_DIR`.

### Check for existing PR (ship-path only)

Run a lightweight check for your own open publish PR. The `--author @me` filter avoids matching someone else's PR for the same API slug.

```bash
gh pr list --repo mvanhorn/printing-press-library --head "feat/<api>" --state open --author @me --json number,url --jq '.[0]' 2>/dev/null
```

If this fails (gh not authenticated, network error, etc.), continue without PR context — the publish skill will handle auth in its own Step 1.

### Ship-path menu

Read the polish result block emitted by Phase 5.5. The menu and recommendation are data-driven so the user is never asked to weigh "Polish again" against "Publish" when polish itself just decided another pass would not help.

**Pick the recommended action:**

| Polish state | Recommendation |
|---|---|
| `ship` + `remaining_issues` empty | **Publish** |
| `ship` + `remaining_issues` non-empty + `further_polish_recommended: yes` | **Polish again** |
| `ship` + `remaining_issues` non-empty + `further_polish_recommended: no` | **Publish** if the remaining issues do not touch the CLI's headline commands; surface the trade-off and let the user pick between **Publish** (as-is; README is not auto-updated) and **Done** otherwise |
| `ship-with-gaps` + `further_polish_recommended: yes` | **Polish again** |
| `ship-with-gaps` + `further_polish_recommended: no` | **Publish** (gaps already in README's `## Known Gaps`; polish's ship logic enforces this for `ship-with-gaps`) or **Done** — agent judgment from the actual gap content |

**Suppress "Polish again" entirely when `further_polish_recommended: no`.** Polish has already decided another pass would not help; offering the option anyway is friction. Surface `further_polish_reasoning` as context so the user sees the call.

**Always available:** Publish, Done. Retro is not on this menu — it is offered as a post-publish tail (see "If Publish now" below).

Present via `AskUserQuestion`. The recommended option leads, carries the `(recommended)` label, and a leading `Recommendation:` line states the call explicitly. Three reinforcing channels so the user does not have to infer from ordering.

**Substitute placeholders before showing the prompt.** The example prompts below use `<api>`, `<N>`, `<score>`, `<pass-rate>`, `<PR_URL>`, `<further_polish_reasoning>`, `$CLI_WORK_DIR`, and `$PRESS_LIBRARY/<api>` as fill-ins. Replace each with the concrete value (the API slug, the actual count, the parsed polish-result string, the expanded shell-variable path, etc.) so the user sees real names and paths, not literal placeholder text. The same rule applies to the hold-path menu below.

**If an existing open PR was found:**

The existing-PR branch still honors polish's `further_polish_recommended` signal. When polish thinks another pass would help, offer it as the recommended action ahead of the PR update — pushing a CLI with closable issues into an existing PR is no better than into a new one.

When `further_polish_recommended: yes`:

> "<api> passed shipcheck. There's an open publish PR (#<PR-number>). Polish flagged <type-qualified summary of remaining issues> as closable — '<further_polish_reasoning>'.
>
> Recommendation: Polish again before updating the PR.
>
> 1. **Polish again** (recommended) — close the remaining issues, then update the PR
> 2. **Update PR #<PR-number>** — push this version to the existing PR as-is
> 3. **No — I'm done**"

When `further_polish_recommended: no` (or `remaining_issues` empty):

> "<api> passed shipcheck. There's an open publish PR (#<PR-number>). Want to update it with this version?"
>
> 1. **Yes — update PR #<PR-number>** (recommended) — re-validate, re-package, and push to the existing PR
> 2. **No — I'm done**"

**Polish converged clean** (`remaining_issues` empty, `further_polish_recommended: no`):

> "<api> passed shipcheck (<score>/100, verify <pass-rate>%). Polish ran cleanly — nothing more to fix.
>
> Recommendation: Publish.
>
> 1. **Publish now** (recommended) — validate, package, and open a PR
> 2. **Done for now**

**Polish thinks another pass would help** (`further_polish_recommended: yes`):

When summarizing remaining issues to the user, type-qualify the count instead of just printing `<N> issues`. Pull the categories from the polish result block (`remaining_issues` entries usually carry their type — verify failure, README gap, MCP description, etc.). For example: "2 verify failures and 1 README gap remain" rather than "3 issues remain."

> "<api> passed shipcheck (<score>/100, verify <pass-rate>%). <type-qualified summary of remaining issues>.
>
> Polish notes: '<further_polish_reasoning>'
>
> Recommendation: Polish again before publishing.
>
> 1. **Polish again** (recommended) — close the remaining issues
> 2. **Publish now** — ship as-is
> 3. **Done for now**

**Polish opted out of recommending more polish** (`remaining_issues` non-empty, `further_polish_recommended: no`):

> "<api> passed shipcheck (<score>/100, verify <pass-rate>%). <type-qualified summary of remaining issues> — polish could not auto-resolve these.
>
> Polish notes: '<further_polish_reasoning>'
>
> Recommendation: <Publish | Done — your call from the gap content>.
>
> 1. **Publish now** — ship as-is. The remaining issues are not auto-added to README; if any are user-facing, you'll need to update the README's `## Known Gaps` section in the publish PR before merging
> 2. **Done for now** — leave the CLI in <expanded $PRESS_LIBRARY/<api> path> and address remaining issues manually before any later publish

This prompt fires only for `ship` verdicts (not `ship-with-gaps`), so polish has not auto-written `## Known Gaps`. Polish auto-writes the section only when the verdict is `ship-with-gaps` (see polish SKILL.md "Ship logic"). On a `ship` verdict with non-empty `remaining_issues`, polish has judged those issues acceptable to publish without a verdict bump — but the user may still want to surface user-facing items in README before merging the PR.

If the shipcheck report contains a `## Known Gaps` block, prepend: "Note: shipcheck documented known gaps (see the shipcheck report above)."

### If "Publish now"

Invoke `/printing-press-publish <api>`. The publish skill handles everything from there — fork, branch, manifest checks, `cli-skills/pp-<api-slug>/SKILL.md` regen, push, and PR creation.

**Do not improvise the publish flow.** Even though the publish skill itself runs `gh repo fork`, `git push`, and `gh pr create --repo mvanhorn/printing-press-library …` internally, running those commands by hand from this phase skips the preflight checks (printer sentinel validation, manifest shape, vendor-spec PII scope, govulncheck on the changed module) and the public library's own `AGENTS.md` requirements that the skill mirrors. The CWD here is `cli-printing-press`, so the public library's `AGENTS.md` is not loaded — the skill is the only entry point that brings those rules into context. If the publish skill fails, fix the underlying issue (or report it as a machine bug); do not bypass it. See [`AGENTS.md`](AGENTS.md) "Publishing to the Public Library" for the full rule.

**After publish returns success**, offer retro as a soft tail — **unless a retro proof already exists for this run** (`ls "$PRESS_MANUSCRIPTS/$API_SLUG/$RUN_ID/proofs/"*-retro-*.md` matches, the on-disk artifact `/printing-press-retro` writes when it runs), in which case skip the offer and end normally. Anchor the skip on that file rather than memory — it survives a context-window roll or a mid-session resume, so the decision is the same whether or not the earlier retro is still in context. The publish skill drives its PR to stable green and hands back without offering anything itself; this tail is the only place the ship-path offers retro. It has no business being a peer of publish on the headline menu, but a post-publish optional offer lets users compound learnings without forcing the choice up front. Retro at this point sees the publish step as part of the session it analyzes.

Present via `AskUserQuestion`:

> "PR opened: <PR_URL>. Run a retro? It surfaces systemic gaps from this session (generator misses, scorer bugs, skill-doc drift) as a GitHub issue for the Printing Press maintainers. Every retro filed raises the floor for the next CLI — and your session context is freshest right now."
>
> 1. **No — I'm done** (default)
> 2. **Yes — run retro now**

If the user picks yes, invoke `/printing-press-retro`. The retro skill analyzes the session for generator improvements.

### If "Polish again"

Invoke `/printing-press-polish <api>`. The polish skill runs another diagnostic-fix-rediagnose pass, reports the delta, and offers its own publish at the end with the same data-driven shape used here.

### If "Done for now"

End normally. The CLI is in `$PRESS_LIBRARY/<api>` and the user can run `/printing-press-publish`, `/printing-press-polish`, or `/printing-press-retro` later.

### Hold-path menu

The CLI did not promote to library. The working copy is at `$CLI_WORK_DIR`; manuscripts and proofs are archived. Hold runs are the highest-value retro signal — something blocked the machine from reaching ship, and that signal is most valuable while session context is fresh.

A hold is not permission to change PR shape. Do not substitute a docs-only, plan, proposal, or spec PR for the requested generated CLI. The only public-library PR this menu may lead to is the explicit `blocked-apis.json` journal option; otherwise report the blocker through the menu and stop.

Before rendering the menu, decide whether this hold should offer a blocked-API journal entry. Offer journaling only when the one-line hold reason is a reachability or buildability blocker that would likely repeat for another user before a machine or upstream change, for example browser-clearance barriers, Cloudflare Turnstile, login/session surfaces that a pure-HTTP printed CLI cannot replay, unreachable official specs, or an upstream API that cannot be called from generated code. Do not offer journaling for ordinary fix-loop failures, local setup problems, missing credentials, temporary network outages, test flakes, or quality issues that polish can plausibly fix.

Present via `AskUserQuestion`:

> "<api> couldn't pass shipcheck — <one-line reason from the shipcheck report or polish result>. The working copy is at <expanded $CLI_WORK_DIR path> and was not added to the library. What do you want to do?"
>
> 1. **Run retro** (recommended) — capture what blocked ship so the Printing Press maintainers can fix it for the next CLI you generate
> 2. **Polish to retry** — run another polish pass and try again to reach ship
> 3. **Add to blocked-API journal** — open a public-library PR that appends or updates `blocked-apis.json` so future `/printing-press <api>` runs warn before repeating this held attempt. Include this option only for repeatable reachability/buildability holds as described above.
> 4. **Done for now**

Default the recommendation to **Run retro**. Override to **Polish to retry** when the polish result block specifically says another pass is likely to close the gap (`further_polish_recommended: yes`) — that signal means the CLI is on hold not because the machine is structurally short, but because the last polish pass ran out of time on issues it can plausibly close.

#### If "Run retro"

Invoke `/printing-press-retro`. The retro skill analyzes the session for generator improvements.

#### If "Polish to retry"

**Invoke polish via the Skill tool with `$CLI_WORK_DIR` as the arg:**

```
Skill(
  skill: "cli-printing-press:printing-press-polish",
  args: "$CLI_WORK_DIR
printing_press_bin: <captured PRINTING_PRESS_BIN>"
)
```

Three reasons for this exact form, all mirroring Phase 5.5:

1. **Pass `$CLI_WORK_DIR` (absolute path), not the slug.** Hold runs leave the CLI in the working directory because Phase 5.6 did not promote — `$PRESS_LIBRARY/<slug>/` either does not exist or holds a stale prior run, and a slug-form invocation would polish that stale copy.
2. **Use the Skill tool (forked context), not the `/printing-press-polish` slash command.** This matches Phase 5.5's invocation pattern — same shape, same expectations. Slash-command invocations auto-enable polish's standalone mode (Publish Offer fires); the Skill tool form defers to the parent unless `--standalone` is passed explicitly. Main SKILL owns the menu on this path.
3. **Do not include `--standalone` in `args`.** The flag is what polish gates its Publish Offer on (see polish SKILL.md "Publish Offer"). On the hold path the CLI has not been promoted; firing the offer would open a public PR for an un-promoted, un-shipped working copy.
4. **Pass `printing_press_bin`.** Use the absolute path captured by the parent setup preflight so this forked polish pass cannot downgrade the CLI by resolving an older global binary.

#### If "Add to blocked-API journal"

Invoke `/printing-press-publish --blocked-api-journal <api>`. The publish skill owns public-library writes and will append or update only `blocked-apis.json`, not `registry.json`, README catalog cells, generated skill mirrors, or a printed CLI package.

Pass the journal fields from the current run context:

- `slug`: the canonical API slug (`<api>`), not the CLI binary name.
- `attempted_at`: today's date in `YYYY-MM-DD` form.
- `verdict`: always `hold`.
- `reason`: one concise sentence from the hold reason. Do not include secrets, local paths, cookies, tokens, or user-specific account details.
- `blocking_issue`: the Printing Press issue number that would unblock this API if known, otherwise `null`.
- `permanent`: `true` only when the API is fundamentally incompatible with a replayable printed CLI, such as a resident-browser-only product surface with no API or stable replayable HTTP path. Use `false` for machine gaps that could be fixed.

If the current hold also warrants a retro, tell the user after the journal PR opens that a retro is still useful for the machine-level fix. Do not run retro automatically from this branch; the user chose the journal action.

After polish returns, parse the result block and act on the new `ship_recommendation`:

- **Polish landed on `ship` or `ship-with-gaps`** — the verdict transitioned out of hold. The working copy is still un-promoted; the library is stale. Run promote, then route to the ship-path menu (above):

  ```bash
  "$PRINTING_PRESS_BIN" lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"
  ```

  Then re-enter the ship-path menu using polish's new result block. Skip the Phase 5.6 acceptance-gate JSON check — that gate was already satisfied when this run originally reached Phase 5.6, and polish does not regenerate it.

- **Polish still on `hold`** — re-show this hold-path menu so the user can pick again. Do not loop polish automatically; the user may want retro or to give up after a failed retry.

#### If "Done for now"

End normally. The working copy stays in `$CLI_WORK_DIR` for potential future retry.

## Fast Guidance

### When to use `cli-printing-press print`

Use `cli-printing-press print <api>` only when the user explicitly wants a resumable on-disk pipeline with phase seeds. It is optional.

The fast path for `/printing-press <API>` is:
- brief
- generate
- build
- shipcheck

### When to stop researching

Stop when you can answer:
- what to build first
- what data to persist
- what incumbent features cannot be missing

If the next research step does not change those answers, stop and generate.

### What not to do

Do not:
- write 5 separate mandatory research documents
- defer all workflows to "future work"
- skip verification because the CLI compiles
- treat scorecard alone as ship proof
- discover YAML/URL spec incompatibility late and manually convert specs if the tools can already consume them
- rerun the whole late-phase gauntlet for cosmetic README polish
- skip features because "the MCP already handles that" (absorb everything, beat it with offline + agent-native)
- build only "top 3-5 workflows" when the absorb manifest has 15+ (build them ALL, then transcend)
- generate before the Phase 1.5 Ecosystem Absorb Gate is approved
- call a CLI "GOAT" without matching every feature the best competitor has

### What counts as success

Success is:
- a generated CLI that gets to shipcheck without generator blockers
- verification tools working against the same spec the user generated from
- one or two fix loops, not a maze of re-entry phases
- a CLI that is plausibly shippable today, not a perfect design memo
