## Run Initialization

After you know `<api>` (from the orientation and briefing flow in [references/run-resolution.md](../references/run-resolution.md); [preflight](01-preflight.md) already ran), initialize the run-scoped artifact paths:

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
PHASE_RECEIPT_LOG="$PIPELINE_DIR/phase-receipts.jsonl"
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
  "phase_receipt_log": "$PHASE_RECEIPT_LOG",
  "spec_path": "<absolute spec path if known>"
}
```

`run_id` is the unique value allocated above from the wall-clock stamp plus a short random suffix. `mkdir "$CANDIDATE_RUN_DIR"` is the collision guard: if another run already owns a candidate directory, allocate another ID instead of reusing the directory. Persisting this value in `state.json` makes the state file the source of truth for generate, dogfood acceptance, promote, `/printing-press-score`, and future state-loading consumers. Without `run_id` in either state or legacy path fallback, `cli-printing-press dogfood --live --write-acceptance` refuses to write the gate marker.

Do not create a `go.work` file in `$CLI_WORK_DIR`. Generated modules must build and test as standalone modules; a mismatched workspace `go` directive can break Go 1.25+ toolchains and lefthook checks. Editor/gopls workspace noise is cosmetic and must not be traded for broken `go build` or `go test`. The generated test gate is `go test -count=1 ./...` so a warm Go test cache cannot satisfy a fresh proof.

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

After writing `$STATE_FILE`, initialize the phase receipt log. Substitute the
captured absolute binary path for `$PRINTING_PRESS_BIN`. Initialization is the
durable handoff from run setup to the first execution phase:

```bash
"$PRINTING_PRESS_BIN" phase-receipt init \
  --file "$PHASE_RECEIPT_LOG" \
  --run-id "$RUN_ID" \
  --phase "02-run-initialization" \
  --evidence "$STATE_FILE"
```

The command must succeed before continuing. Keep the receipt log under
`$PIPELINE_DIR`; Phase 5.6 archives research, proofs, and discovery, not this
disposable sequencing log.

Next: phases/03-resolve-and-reuse.md
