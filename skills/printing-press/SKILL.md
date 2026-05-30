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

# Prefer local build when running from inside the printing-press repo.
# The lefthook build hook keeps ./cli-printing-press current after every commit/pull,
# so it's always newer than the go-install version.
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
      echo "Go 1.26.3 or newer is also not installed. Install Go from https://go.dev/dl/, then:"
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
  echo "Install Go 1.26.3 or newer from https://go.dev/dl/, then verify with:"
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

  if [ -n "$_installed" ] && [ -n "$_latest" ] &&
     awk -v installed="$_installed" -v latest="$_latest" 'BEGIN {
       split(installed, a, ".")
       split(latest, b, ".")
       # Integer truncation means pre-release suffixes (e.g. "4.0.0-rc.1") are
       # treated as equal to their GA counterpart. Acceptable while we do not
       # ship pre-release tags; revisit if that changes.
       for (i = 1; i <= 3; i++) {
         if ((a[i] + 0) < (b[i] + 0)) exit 0
         if ((a[i] + 0) > (b[i] + 0)) exit 1
       }
       exit 1
     }'; then
    # Marker for the skill prose below to detect and offer an interactive upgrade.
    # The skill reads PRESS_UPGRADE_AVAILABLE / PRESS_UPGRADE_INSTALLED from this output.
    echo ""
    echo "[upgrade-available] printing-press v$_latest is available (you have v$_installed)"
    echo "PRESS_UPGRADE_AVAILABLE=$_latest"
    echo "PRESS_UPGRADE_INSTALLED=$_installed"
    echo ""
  fi

  printf "last_check=%s\nlatest=%s\nmode=standalone\n" "$_now_ts" "${_latest:-$_installed}" > "$PRESS_VERCHECK_FILE" 2>/dev/null || true
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

**MANDATORY: Read and apply [references/setup-checks.md](references/setup-checks.md) immediately after the setup contract bash block runs, before any other action.** It handles the contract output signals: `[setup-error]` (refuse to run, surface the install instructions), `[repo-upgrade-available]` (interactive `AskUserQuestion` prompt + optional repo pull), `PRESS_REPO_MODE=<true|false>` plus the targeted global open-agent-skills freshness check, the min-binary-version compatibility check (hard stop if binary is too old), `[upgrade-available]` (interactive `AskUserQuestion` prompt + optional standalone binary upgrade), `[browser-tools-missing]` (interactive `AskUserQuestion` prompt + optional install of browser-use and/or agent-browser), and the `PRINTING_PRESS_BIN=<abs-path>` marker plus optional `[binary-shadow]` warning (capture the path; use it for every subsequent generator invocation). Skipping the reference will cause the skill to proceed with a missing or out-of-date binary, run with stale global skill text when the session is managed by open-agent-skills, hit a mid-flight install prompt if browser-sniff is later needed, or invoke the wrong binary because a stale global or the public catalog installer on `PATH` shadowed the local build. Do not skip.

**Absolute-path rule.** The preflight contract always emits `PRINTING_PRESS_BIN=<absolute path>` to stdout. Capture this value and substitute it (the resolved absolute path, not the literal `$PRINTING_PRESS_BIN` token) for every subsequent `cli-printing-press ...` invocation in this skill, references, and any sub-skill you delegate to. The `export PATH=...` line inside the contract only affects the single Bash tool call it runs in; later Bash tool calls open fresh shells and resolve bare `cli-printing-press` against the user's default `PATH`, where a stale globally-installed binary (`$HOME/go/bin/cli-printing-press`, Homebrew copy, etc.) will silently shadow the local build the preflight just chose. Bash code examples below are written `cli-printing-press generate ...` for readability — replace `cli-printing-press` with the captured absolute path each time you actually run one.

Only after preflight completes successfully (no `[setup-error]`; no global skill update that requires restart; any `[repo-upgrade-available]`, `[upgrade-available]`, or `[browser-tools-missing]` was offered to the user; `PRINTING_PRESS_BIN` is captured) should you proceed to the Orientation & Briefing section below.

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
RUN_ID="$(date +%Y%m%d-%H%M%S)"
API_RUN_DIR="$PRESS_RUNSTATE/runs/$RUN_ID"
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

`run_id` is the same `YYYYMMDD-HHMMSS` value computed earlier as `RUN_ID="$(date +%Y%m%d-%H%M%S)"`. The generator's manifest writer derives the same value from the `--research-dir` basename when generate is invoked through the canonical `$API_RUN_DIR` (whose basename equals `$RUN_ID`); persisting it in `state.json` here keeps `/printing-press-score` and any future state-loading consumer in sync. Without `run_id` in either path, `cli-printing-press dogfood --live --write-acceptance` refuses to write the gate marker.

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


→ **Read and apply [references/phase-0-resolve-and-reuse.md](references/phase-0-resolve-and-reuse.md) in full before any research.** It is procedural but contains blocking decisions — do not skip it.

Phase 0 runs first (after Run Initialization, before Phase 1). It must resolve and honor every one of these outcomes before Phase 1 begins:

1. **Resolve the spec source** — URL content-probe + disambiguation (`AskUserQuestion` for API-vs-website; may set `BROWSER_SNIFF_TARGET_URL`), `--har` / `--spec` handling, and the directory spec-source guard (never pass a directory to `generate`; enumerate, filter, and on >1 candidate ask the user — never finish a directory run while hiding ignored specs).
2. **Reuse prior research** under `$PRESS_MANUSCRIPTS/<api-slug>/*/research/*` instead of redoing it.
3. **Library + lock decision matrix** — may **wait / abort / reclaim** a stale lock, or hand off to `/printing-press-polish` (improve existing). **Blocking.**
4. **Version-delta re-validation prompt** — mandatory when reusing prior research across a minor/major binary upgrade.
5. **Public-library check (registry.json)** — High/Medium match may **end this run** and hand off to `/printing-press-reprint`. **Blocking.** Combo-CLI runs present one combined prompt.
6. **API Key Gate** — detect auth need; if auth required, get explicit consent for read-only Phase 5 use, or proceed key-less with smoke testing skipped. Resolve (or skip for no-auth APIs) before Phase 1. **Blocking.**

## Phase 1: Research Brief

**When `BROWSER_SNIFF_TARGET_URL` is set:** Skip the catalog check, spec/docs search, and SDK wrapper search — none of these exist for an undocumented website feature. Focus research on understanding what the site/feature does, who uses it, what workflows it supports, and what competitors offer similar functionality. The spec will come from browser-sniffing in Phase 1.7.

Before reading documentation, read [references/fetch-docs.md](references/fetch-docs.md). Use `fetch-docs.sh` for the API's primary docs, OpenAPI/Postman links, auth guides, error handling, rate limits, pagination, webhooks, and any per-endpoint reference page. Preserve exact status codes and inspect the returned local file directly so enum values, field constraints, casing, examples, and nav/link variants are not lost through summarization.

→ **Read and apply [references/phase-1-research-and-brief.md](references/phase-1-research-and-brief.md)** for the full procedure: the built-in catalog check (`cli-printing-press catalog show <api> --json`) and its three branches (spec-based → offer skip-discovery; **wrapper-only** → the generator cannot build directly from a wrapper, so record the user's chosen path in `state.json` `implementation`; no-hit → proceed normally); the research checklist (including the GitHub-issues `403`/`blocked`/`deprecated` reachability-risk scan); and the brief markdown template.

Write **one** build-driving brief to `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-brief.md` that answers: (1) what the API is actually used for; (2) top 3–5 power-user workflows; (3) table-stakes competitor features; (4) what data deserves a local store; (5) why install this over the incumbent; (6) product name + thesis. Do not split these into separate documents.


**MANDATORY: Before proceeding to Phase 1.5 (Absorb Gate), you MUST evaluate Phase 1.6 (Pre-Browser-Sniff Auth Intelligence), Phase 1.7 (Browser-Sniff Gate), and Phase 1.8 (Crowd-Sniff Gate) below.** If no spec source has been resolved yet (no `--spec`, no `--har`, no catalog spec URL), the browser-sniff gate decision matrix MUST be evaluated. Do not skip to Phase 1.5.

**Phase 1.5 will refuse to proceed without a `browser-browser-sniff-gate.json` marker file.** Phase 1.7 writes this file with one entry per source (one entry for single-source CLIs, one entry per named source for combo CLIs). Missing marker = HARD STOP back to Phase 1.7. See Phase 1.7 "Enforcement" below for the contract.

## Phase 1.6: Pre-Browser-Sniff Auth Intelligence

After Phase 1 research completes, analyze findings to proactively assess what auth context the user could provide. This step uses research intelligence to ask the right question before browser-sniffing starts, rather than waiting for the user to volunteer "I logged in."

**Skip this step if:** The briefing (Orientation & Briefing section) already captured auth context (`AUTH_CONTEXT` is set from the user selecting "I have an API key or I'm logged in").

→ **Read and apply [references/phase-1.6-auth-classification.md](references/phase-1.6-auth-classification.md)** to classify the auth profile (API-key / browser-session / no-auth / dual) from research signals and ask the right question via `AskUserQuestion`. Name the specific authenticated features the user would unlock — never a generic "auth would help."

**Flags this step sets (read by later phases):**
- `AUTH_CONTEXT` — set when the user provides/confirms an API key, so the Phase 0.5 API Key Gate does not re-ask.
- `AUTH_SESSION_AVAILABLE=true` — set when the user is (or can get) logged in; Phase 1.7 uses it to offer authenticated browser-sniff and `auth login --chrome`.
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

### Direct HTTP challenge rule & time budget

→ **Read and apply [references/phase-1.7-reachability-modes.md](references/phase-1.7-reachability-modes.md)** when a Phase 1 reachability probe returns bot-protection evidence (`403`, `429`, `cf-mitigated`, `x-vercel-mitigated`, WAF/DataDome/PerimeterX, CAPTCHA, "Just a moment").

Non-negotiable contracts it carries (kept visible here):
- **Run `cli-printing-press probe-reachability "<url>" --json` before announcing any browser escalation.** The classifier — not the user — decides transport. **Do not present transport tiers (Surf vs cookie vs full browser) as a peer menu.** Intent-level menus ("Browser-sniff or HOLD?") are fine.
- Apply the probe's `mode` (`standard_http | browser_http | browser_clearance_http | unknown`) to the **runtime** decision; the discovery decision is independent.
- When browser-sniff is approved AND mode is `browser_clearance_http`/`unknown`: do **not** offer alternate CLI shapes (RSS, official API, docs-only, narrower scope) before a real browser capture is attempted; offer a manual HAR before any scope pivot.
- The **3-minute time budget applies AFTER the user approves** — never a reason to skip the gate.


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

### Steps 1.5a–1.5c: Search the ecosystem, then build the absorb manifest

→ **Read and apply [references/phase-1.5-search-and-manifest.md](references/phase-1.5-search-and-manifest.md)** for the full procedure: 1.5a parallel ecosystem searches (plugins, MCP servers, competing CLIs, SDKs); 1.5a.5 read MCP source for ground-truth endpoints/auth; 1.5a.6 DeepWiki codebase analysis; 1.5b catalog **every** feature into the Absorb Manifest; 1.5c identify ≥5 transcendence features.

Two contracts the rest of the pipeline reads, so they stay visible here:
- **Absorb Manifest `Our Implementation` cells start with a parseable disposition** — `<api>-pp-cli <command path>` | `(generated endpoint) <resource> <endpoint>` | `(behavior in <api>-pp-cli <path>) …` | `(stub) …`. Phase 3 verifies rows mechanically against these prefixes.
- **Stubs must be explicit.** Any row shipping as a placeholder starts `Our Implementation` with `(stub)` + a one-line reason; the Phase Gate 1.5 showcase reads stubs out separately for explicit approval. No mid-build downgrade from shipping-scope to stub — a Phase 3 agent that can't implement a feature returns here with a revised manifest.


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

→ **Read and apply [references/phase-1.5-research-json.md](references/phase-1.5-research-json.md)** to write `$API_RUN_DIR/research.json` (the generator reads it to credit community projects and to drive the README/SKILL narrative). The file MUST match the `ResearchResult` schema `loadResearchSources()` expects. Cap `alternatives` at 8 GitHub-URL tools that actually contributed features. The reference covers the full template plus the novel-features, auth, and narrative rules, and the pre-render `validate-narrative --strict --framework-only` floor.


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

→ **Read and apply [references/phase-1.9-oauth-probe.md](references/phase-1.9-oauth-probe.md)** when the resolved spec declares `auth.type: oauth2` with an interactive authorization URL (`authorizationCode`/`implicit`). It is a **read-only** probe of the OAuth grant entry point using the user's real public `client_id` (HOLD before generation if the client id is missing — never substitute a fake one). Skip entirely for `client_credentials`-only flows and for all non-OAuth auth types. **HARD STOP** (do not generate) on a provider OAuth error (`invalid_request`, `invalid_client`, `redirect_uri_mismatch`, etc.); WARN on ambiguous 4xx/5xx/timeout.

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

### Pre-Generation Enrichments

→ **Read and apply [references/phase-2-enrichments.md](references/phase-2-enrichments.md)** for the full procedure of each step below. Run them (where applicable) on the resolved spec *before* the Lock-and-Generate invocation — they shape the spec/flags that drive `generate`, and patching generated files after the fact causes `verify-skill canonical-sections` drift.

1. **Category Enrichment** — set the spec's top-level `category` (from the Phase 1 brief's domain judgment, mapped to `docs/CATALOG.md`) for non-catalog runs: edit the internal/derived spec or pass `--category` on direct `--docs` runs. **Catalog-mode runs skip this** (keep the built-in entry's category).
2. **Cache Enrichment** — decide whether to declare `cache.enabled: true`. Enable only when a covered read path is backed by a syncable resource `sync` can refresh. Leave disabled for stateless wrappers, session-owned local state (carts/drafts), and quota-metered/expensive refresh APIs. **Catalog-mode runs skip this.**
3. **Auth Enrichment** — verify the resolved spec's auth, especially for browser-/crowd-sniffed specs where mechanical detection may have failed. Pick the auth mode (api-key, bearer, basic, cookie, composed, session-handshake, oauth) and emit the correct spec shape. If the spec's auth is wrong, **re-run generation with the corrected spec — do not fix in polish.**
4. **MCP Enrichment** — choose the MCP surface shape (endpoint-mirror vs intent vs code-orchestration) for the resolved spec.

### Lock and Generate

Before running any generate command, acquire the build lock:

```bash
cli-printing-press lock acquire --cli <api>-pp-cli --scope "$PRESS_SCOPE"
```

If acquire fails (another session holds a fresh lock), present the lock status to the user and let them decide: wait, use a different CLI name, force-reclaim, or pick a different API.

The `--category <catalog-category>` flag shown below is for non-catalog runs
whose category was not already authored into an editable spec. Omit it for
catalog-config runs; the built-in catalog category is authoritative there.

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
  --category <catalog-category> \
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
  --category <catalog-category> \
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
  --category <catalog-category> \
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
  --category <catalog-category> \
  --force --lenient --validate
```

Crowd-sniff-only (no original spec, crowd-sniff was the primary source):

```bash
cli-printing-press generate \
  --spec "$RESEARCH_DIR/<api>-crowd-spec.yaml" \
  --output "$CLI_WORK_DIR" \
  --research-dir "$API_RUN_DIR" \
  --category <catalog-category> \
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
  --category <catalog-category> \
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
  --category <catalog-category> \
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
  cli-printing-press lock release --cli <api>-pp-cli
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

→ **Read and apply [references/phase-3-build-checklist.md](references/phase-3-build-checklist.md)** for the full text of each principle, the scan-and-filter pattern, and the Verify-friendly RunE template. After building each Priority 1 and Priority 2 command, confirm these 12 principles (they map 1:1 to Phase 4.9's reviewer checks — apply now so review is a confirmation):

1. **Non-interactive** — no TTY prompts; works headless.
2. **Structured output** — `--json`/`--select` via the generated print helpers.
3. **Progressive help** — realistic domain examples; `Example: strings.Trim(...,"\n")`.
4. **Actionable errors** — name the wrong flag/arg + correct usage.
5. **Safe retries** — mutations support `--dry-run`, idempotent where possible.
6. **Composability** — typed exit codes, clean `jq` piping.
7. **Bounded responses** — `--compact`, list `--limit`.
8. **Verify-friendly RunE** — no `MinimumNArgs`/`MarkFlagRequired`; validate inside RunE, short-circuit on `dryRunOK`, `usageErr` (exit 2) on missing input.
9. **Side-effect commands stay quiet under verify** — print-by-default + opt-in action flag; short-circuit on `cliutil.IsVerifyEnv()`; curtail expensive work under `cliutil.IsDogfoodEnv()`.
10. **Per-source rate limiting** — sibling clients use `cliutil.AdaptiveLimiter` + surface `*cliutil.RateLimitError`.
11. **Parallel-fetch partial failures** — preserve per-fetch errors, exclude from denominators, surface `fetch_failures`.
12. **Scan-and-filter caps** — `--max-scan-pages` (bounds records scanned) separate from `--limit` (bounds matches kept); emit `scanned_<unit>` + zero-match `note`.

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

**Building Priority 2 (transcendence) commands by hand.**

→ **Read and apply [references/phase-3-build-guidance.md](references/phase-3-build-guidance.md)** for the starter templates: the Cobra command wrapper, the three RunE skeletons (API-call, parallel-fetch aggregation, store-query), and full guidance. Two correctness contracts that bite silently, kept visible here:

- **Reuse generator-emitted helpers** in `internal/cli/helpers.go` (`printJSONFiltered`, `printAutoTable`, `defaultDBPath`, `dryRunOK`, and the `cliutil.*` helpers) — do not reinvent them in novel command files.
- **NULL-safe SQL scans are mandatory.** Scan any nullable column (including every `json_extract(data, '$.field')`) into `sql.Null*` targets or `COALESCE(...)` in the query. A bare `string`/`int64` scan errors on NULL, the `for rows.Next()` loop `continue`s on the error, and the feature silently drops every row while looking healthy. This is the silent-row-drop bug class.
- **Hand-edits must stay regen-mergeable.** `generate --force` snapshots, regenerates, then runs the `regen-merge` AST reconciliation; use standalone `regen-merge` first for a previewable report on risky edits.

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
cli-printing-press lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR` for potential future retry. Proceed to Phase 5.6 to archive manuscripts (archiving still happens on hold).

## Phases 4.7–4.95: Post-Shipcheck Review Cluster

→ **Read and apply [references/shipcheck-reviews.md](references/shipcheck-reviews.md)** for the full procedure of each sub-phase below. They run in this order, after the Phase 4 shipcheck umbrella and before Phase 5. Each has a fix-before-Phase-5 gate — none is optional.

- **Phase 4.7 — Sync Param-Drop Gate.** Skip when there's no `traffic-analysis.json`. Otherwise compares captured-vs-coded request param cardinality on hand-authored sync code. Fix = widen the call or annotate an evidence-backed `// pp:sync-params-intentional-subset` opt-out.
- **Phase 4.8 — Agentic SKILL Review.** Agent-tool semantic review of SKILL.md vs shipped CLI (trigger phrases, verified-set alignment, novel-feature/auth narrative accuracy, marketing-copy smell). **Gate:** `error` findings fix-before-Phase-5; `warning` findings surface + proceed on approval.
- **Phase 4.9 — README/SKILL/AGENTS Correctness Audit.** Document-level boilerplate/factual audit against the shipped CLI. **Gate:** any `error` is fix-before-Phase-5.
- **Phase 4.85 — Agentic Output Review.** Invoke the `printing-press-output-review` sub-skill on `$CLI_WORK_DIR`. Wave B: all findings are **warnings** (non-blocking); log + surface to user.
- **Phase 4.95 — Local Code Review.** Pick an installed working-dir-shaped review path (skill, Codex review, or direct Agent-tool reviewer dispatch — never Claude Code `/review`, which is PR-shaped). Autofix mechanical findings (cap 3 rounds); surface only real tradeoffs; route template-shape/out-of-scope findings to retro. Harness-exemption is narrow — almost never legitimate.

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
cli-printing-press lock release --cli <api>-pp-cli
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

Do **not** write a skip marker for ordinary `auth.type: none` cloud/public APIs.
No-auth APIs are testable and require `phase5-acceptance.json` unless they match
the LAN-only carve-out above. Do **not** use missing API key as the skip reason
for cookie, composed, or session-handshake auth; those require browser session
proof or a hold decision.

## Phase 5.5: Polish

**Always runs.** Invoke the `printing-press-polish` skill to run diagnostics, fix quality issues, and return a delta. The polish skill carries `context: fork` in its frontmatter, so its diagnostic-fix-rediagnose loop runs in a forked context — diagnostic spam, fix iterations, and re-audits stay scoped to the polish session and don't pollute this generation flow. The skill is autonomous — no user input needed. The goal is to ship the best CLI possible, not the fastest.

Before invoking polish, collect the Phase 3 transcendence gate state and include
it in the polish input bundle:

```yaml
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

**Pick the promote path by whether the library already holds hand-authored content.** `lock promote --dir` performs an **atomic swap** of `$CLI_WORK_DIR` over `$PRESS_LIBRARY/<api>` — every file in the library that is not in the fresh tree is gone after the swap. Whole hand-authored files (a separate `internal/syncer/` package, novel-feature command files under `internal/cli/` without the `// Generated by ...` header, hand-built migration files under `internal/store/`) survive a `cli-printing-press regen-merge` pass but are wiped by a bare swap. This is the same preservation dynamic called out under [**Hand-edits must be regen-mergeable.**](#hand-edit-durability); the orchestration here must honor it.

Detect hand-authored content in the existing library:

→ **Read and apply [references/phase-5.6-promote-detection.md](references/phase-5.6-promote-detection.md)** for the bash that sets `NOVEL_COUNT` (and `LIB_TARGET="$PRESS_LIBRARY/<api>"`). It reads the manifest `novel_features` length when the field is present — using `jq 'has("novel_features")'` to distinguish an absent field from an explicit zero — and otherwise falls back to a file-probe that sets `NOVEL_COUNT=1` if any `.go` file under `internal/cli`, `internal/syncer`, or `internal/store` lacks the `Generated by CLI Printing Press` header.

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
  if ! cli-printing-press regen-merge "$LIB_TARGET" \
      --fresh "$CLI_WORK_DIR" --json > "$REGEN_DRY_RUN_REPORT"; then
    # Real error (input error, missing fresh tree, unreadable library). Release
    # the upstream pipeline lock and surface the dry-run failure.
    cli-printing-press lock release --cli <api>-pp-cli
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
cli-printing-press lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"
```

The `promote` command handles the full sequence: stages the working directory, atomically swaps it into `$PRESS_LIBRARY/<api>` (slug-keyed), writes the `.printing-press.json` manifest, updates the `CurrentRunPointer`, and releases the lock — all in one step. The `--cli` flag accepts the CLI binary name; the Go code translates to the slug-keyed library path internally.

**Path B — reprint over a library with hand-authored content that the fresh tree did not fully rebuild (`-d "$LIB_TARGET"` AND `NOVEL_COUNT > 0` AND the guarded Path A override did not pass).** Use `regen-merge` to fold the fresh tree into the live library before promotion. `regen-merge` classifies every Go file under `internal/` and `cmd/` against the fresh tree, overwrites safely-templated files, re-injects `AddCommand` calls in `root.go` and resource-parents that the fresh tree lacks, and leaves files with hand-edited additions (`TEMPLATED-WITH-ADDITIONS`) untouched for human review. `--apply` writes via stage-and-swap-with-recovery, so partial failure can never lose data.

`regen-merge --apply` exits 0 even when it leaves `TEMPLATED-WITH-ADDITIONS` files (the human-review verdicts are reported, not raised as errors). The halt condition must be checked explicitly against the report — capture `--json` and inspect the verdict counts:

```bash
REGEN_REPORT="$PROOFS_DIR/regen-merge-report.json"
if ! cli-printing-press regen-merge "$LIB_TARGET" \
    --fresh "$CLI_WORK_DIR" --apply --json > "$REGEN_REPORT"; then
  # Real error (input error, apply failure). Release the lock — it was
  # acquired upstream by the press pipeline; regen-merge does not own it —
  # and surface the failure to the user.
  cli-printing-press lock release --cli <api>-pp-cli
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
  cli-printing-press lock release --cli <api>-pp-cli
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
cli-printing-press lock promote --cli <api>-pp-cli --dir "$LIB_TARGET" || {
  cli-printing-press lock release --cli <api>-pp-cli
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

→ **Read and apply [references/phase-5.6-archive.md](references/phase-5.6-archive.md)** for the archive commands. Ordering contract (honor even before loading the file): archive the run's `research/`, `research.json`, `proofs/`, and `discovery/` to `$PRESS_MANUSCRIPTS/<api>/$RUN_ID/` **unconditionally** after promote (or after lock release on `hold`). **Strip response bodies from the HAR** (`del(.log.entries[].response.content.text)`) before copying to control size. Wipe `$SESSION_DIR` **last**, after the archive is written.

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

### Ship-path / Hold-path menus

→ **Read and apply [references/phase-6-menus.md](references/phase-6-menus.md)** for the full ship-path and hold-path menu shapes, recommendation tables, and `AskUserQuestion` prompt templates. Substitute all placeholders (`<api>`, `<score>`, `$CLI_WORK_DIR`, etc.) with concrete values before showing any prompt.

Load-bearing invariants (honor these even before loading the menu file):

- **Do not improvise the publish flow.** "Publish now" → invoke `/printing-press-publish <api>` and let it run. The CWD here is `cli-printing-press`, so the public library's `AGENTS.md` is not loaded; the publish skill is the only entry point that brings its preflight checks (printer sentinel, manifest shape, vendor-spec PII scope, govulncheck) into context. If publish fails, fix the cause — do not hand-run `gh`/`git` to bypass it. See [`AGENTS.md`](AGENTS.md) "Publishing to the Public Library".
- **Polish-to-retry invocation form** (hold path) — invoke via the **Skill tool** with `$CLI_WORK_DIR` (absolute path, NOT the slug), and **never** pass `--standalone`. Slug-form would polish a stale library copy; `--standalone` would fire polish's Publish Offer and open a PR for an un-promoted working copy.
- **Promote before re-entering the ship menu** on a hold→ship transition: if polish moves the verdict out of `hold`, run `cli-printing-press lock promote --cli <api>-pp-cli --dir "$CLI_WORK_DIR"` before routing to the ship-path menu, and skip the Phase 5.6 acceptance-gate JSON re-check (already satisfied this run).
- **Post-publish retro** is a soft tail only, suppressed if a retro proof already exists for this run.

## Fast Guidance

→ **Read [references/fast-guidance.md](references/fast-guidance.md)** for the fast-path order, when to stop researching, the "what not to do" list, and what counts as success.

- **Fast path** for `/printing-press <API>`: brief → generate → build → shipcheck. (`cli-printing-press print` is optional — only for a resumable on-disk pipeline.)
- **Stop researching** once you can answer: what to build first, what data to persist, what incumbent features cannot be missing. If the next step doesn't change those, generate.
- **Do NOT**: write separate mandatory research docs; defer workflows to "future work"; skip verification because it compiles; treat scorecard as ship proof; generate before the Phase 1.5 absorb gate is approved; build only "top 3-5" when the manifest has 15+ (build them ALL, then transcend).
- **Success** = a CLI that reaches shipcheck without generator blockers, in one or two fix loops, plausibly shippable today.
