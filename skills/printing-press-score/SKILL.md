---
name: printing-press-score
description: Score a generated CLI against the Steinberger bar, compare two CLIs side-by-side
version: 0.1.0
min-binary-version: "0.3.0"
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - AskUserQuestion
---

# /printing-press-score

Score generated CLIs against the Steinberger bar. Supports rescoring, scoring by name/path, and comparing two CLIs.

## Quick Start

```
/printing-press-score                              # rescore current CLI
/printing-press-score notion-pp-cli-4              # score by name
/printing-press-score ~/my-cli                     # score by path
/printing-press-score notion-pp-cli-4 vs notion-pp-cli-2  # compare two
```

## Prerequisites

- Go 1.26.3 or newer installed
- `printing-press` binary on PATH (install with `go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest`)

## Step 0: Setup

Before any other commands, run the setup contract to verify the printing-press binary is on PATH and initialize scope variables:

<!-- PRESS_SETUP_CONTRACT_START -->
```bash
# min-binary-version: 0.3.0

# Derive scope first — needed for local build detection
_scope_dir="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
_scope_dir="$(cd "$_scope_dir" && pwd -P)"

# Prefer local build when running from inside the printing-press repo.
if [ -x "$_scope_dir/printing-press" ] && [ -d "$_scope_dir/cmd/printing-press" ]; then
  export PATH="$_scope_dir:$PATH"
  echo "Using local build: $_scope_dir/printing-press"
elif ! command -v printing-press >/dev/null 2>&1; then
  if [ -x "$HOME/go/bin/printing-press" ]; then
    echo "printing-press found at ~/go/bin/printing-press but not on PATH."
    echo "Add GOPATH/bin to your PATH:  export PATH=\"\$HOME/go/bin:\$PATH\""
  else
    echo "printing-press binary not found."
    echo "Install with:  go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest"
  fi
  return 1 2>/dev/null || exit 1
fi

PRESS_BASE="$(basename "$_scope_dir" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9_-]/-/g; s/^-+//; s/-+$//')"
if [ -z "$PRESS_BASE" ]; then
  PRESS_BASE="workspace"
fi

PRESS_SCOPE="$PRESS_BASE-$(printf '%s' "$_scope_dir" | shasum -a 256 | cut -c1-8)"
PRESS_HOME="$HOME/printing-press"
PRESS_RUNSTATE="$PRESS_HOME/.runstate/$PRESS_SCOPE"
PRESS_LIBRARY="$PRESS_HOME/library"
PRESS_MANUSCRIPTS="$PRESS_HOME/manuscripts"
PRESS_CURRENT="$PRESS_RUNSTATE/current"

mkdir -p "$PRESS_RUNSTATE" "$PRESS_LIBRARY" "$PRESS_MANUSCRIPTS" "$PRESS_CURRENT"
```
<!-- PRESS_SETUP_CONTRACT_END -->

After running the setup contract, check binary version compatibility. Read the `min-binary-version` field from this skill's YAML frontmatter. Run `printing-press version --json` and parse the version from the output. Compare it to `min-binary-version` using semver rules. If the installed binary is older than the minimum, warn the user: "printing-press binary vX.Y.Z is older than the minimum required vA.B.C. Run `go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest` to update." Continue anyway but surface the warning prominently.

Current-run state is resolved from `$PRESS_RUNSTATE`. Published CLIs are resolved from `$PRESS_LIBRARY`. Archived manuscripts are resolved from `$PRESS_MANUSCRIPTS`.

## Step 1: Parse Arguments

Read the user's input after `/printing-press-score`. The input is **free-form** — interpret intent, don't enforce syntax.

**Noise words to strip:** `compare`, `vs`, `versus`, `and`, `against`, `with`, `to`

After stripping noise words, count the remaining tokens:

- **0 tokens** → Rescore Current mode
- **1 token** → Score Single mode
- **2 tokens** → Compare mode

## Step 2: Resolve CLI Directories

For each CLI identifier, resolve it to a directory path:

### If the token contains `/` or `.`
Treat it as a path (absolute or relative). Verify the directory exists.

### If the token is a plain name
Try these locations in order:
1. `$PRESS_LIBRARY/<name>/` — exact match
2. `$PRESS_LIBRARY/<name>-pp-cli/` — with -pp-cli suffix
3. If neither exists, Glob `$PRESS_LIBRARY/<name>-pp-cli*`
4. If exactly one glob match exists and is a directory, use it
5. If multiple glob matches exist, present a numbered menu using AskUserQuestion

If neither exists, scan current-run and archived state:
6. Use Glob to find `$PRESS_RUNSTATE/runs/*/state.json` files
7. Read each, look for an `output_dir` or `working_dir` value whose basename contains the name
8. If found and the directory exists, use it

If nothing resolves, report the error: "Could not find CLI '<name>'. Provide a path or check the name."

### Rescore Current (0 tokens)
1. Use Glob to find all `$PRESS_CURRENT/*.json` files
2. Read each to get `api_name`, `state_path`, and `working_dir`
3. Filter to those whose `working_dir` actually exists on disk
4. If none are found, Glob `$PRESS_LIBRARY/*-pp-cli*` and use those directories instead
5. If exactly one → use it automatically
6. If multiple → present a numbered menu using AskUserQuestion:
   ```
   Multiple CLIs found. Which one to score?
   1. stripe-pp-cli ($PRESS_LIBRARY/stripe-pp-cli)
   2. notion-pp-cli ($PRESS_LIBRARY/notion-pp-cli)
   3. linear-pp-cli ($PRESS_LIBRARY/linear-pp-cli)
   ```
7. If none found → report: "No generated CLIs found. Provide a name or path."

## Step 3: Find Spec for Tier 2 Scoring

For each resolved CLI directory, find the OpenAPI spec:

1. Check `<cli-dir>/spec.json` — the pipeline converts YAML specs to JSON during generation
2. If not found, scan `$PRESS_RUNSTATE/runs/*/state.json` files for one matching this CLI's directory. Read its `spec_path` field. If that file exists on disk, use it.
3. If no spec found, **proceed without `--spec`**. Note to the user: "No spec found — spec-derived dimensions will be marked N/A and omitted from the denominator. Provide a spec path for full scoring."

## Step 4: Run Scorecard

### Single Score Mode

Run the scorecard command:

```bash
printing-press scorecard --dir <resolved-path> --json
```

If a spec was found, add `--spec <spec-path>`.

Parse the JSON output. The structure is:

```json
{
  "api_name": "...",
  "steinberger": {
    "output_modes": 8,
    "auth": 7,
    "error_handling": 6,
    "terminal_ux": 9,
    "readme": 5,
    "doctor": 10,
    "agent_native": 7,
    "local_cache": 4,
    "breadth": 7,
    "vision": 6,
    "workflows": 3,
    "insight": 5,
    "path_validity": 0,
    "auth_protocol": 0,
    "data_pipeline_integrity": 7,
    "sync_correctness": 6,
    "type_fidelity": 4,
    "dead_code": 3,
    "total": 72,
    "percentage": 72
  },
  "overall_grade": "B",
  "gap_report": ["..."],
  "unscored_dimensions": ["path_validity", "auth_protocol"]
}
```

If `unscored_dimensions` is present, those dimensions should be rendered as `N/A`, not `0/x`, and should be described as omitted from the denominator rather than as fixable CLI defects. For backward compatibility, JSON still encodes the numeric fields as `0`; consumers must use `unscored_dimensions` to distinguish `N/A` from a real zero.

### Compare Mode

Run **both** scorecard commands in **parallel** using two simultaneous Bash tool calls:

```bash
# Call 1:
printing-press scorecard --dir <path1> --spec <spec1> --json

# Call 2:
printing-press scorecard --dir <path2> --spec <spec2> --json
```

Parse both JSON outputs.

## Step 5: Render Output

### Single Score Table

Render a rich markdown table. Note: Tier 1 dimensions are all /10. Tier 2 dimensions are /10 except TypeFidelity and DeadCode which are /5.

```
Scorecard: <api_name>

Infrastructure (Tier 1)
| Dimension      | Score |
|----------------|-------|
| Output Modes   | 8/10  |
| Auth           | 7/10  |
| Error Handling | 6/10  |
| Terminal UX    | 9/10  |
| README         | 5/10  |
| Doctor         | 10/10 |
| Agent Native   | 7/10  |
| Local Cache    | 4/10  |
| Breadth        | 7/10  |
| Vision         | 6/10  |
| Workflows      | 3/10  |
| Insight        | 5/10  |

Domain Correctness (Tier 2)
| Dimension               | Score |
|--------------------------|-------|
| Path Validity            | 9/10  |
| Auth Protocol            | 8/10  |
| Data Pipeline Integrity  | 7/10  |
| Sync Correctness         | 6/10  |
| Type Fidelity            | 4/5   |
| Dead Code                | 3/5   |

**Total: 72/100 — Grade B**
```

If `gap_report` is non-empty, list the gaps:

```
Gaps:
- <gap 1>
- <gap 2>
```

If `unscored_dimensions` is non-empty, add a note after the table:

```
Note: path_validity, auth_protocol were unscored and omitted from the denominator. Provide a spec path for full scoring.
```

### Compare Table

Render a side-by-side table with a delta column. Show the first CLI name and second CLI name as column headers. Calculate delta as (CLI 1 score - CLI 2 score). Show `+N` for positive, `-N` for negative, `—` for zero.

```
Scorecard Comparison: <name1> vs <name2>

Infrastructure (Tier 1)
| Dimension      | <name1> | <name2> | Delta |
|----------------|---------|---------|-------|
| Output Modes   | 8/10    | 5/10    | +3    |
| Auth           | 7/10    | 7/10    | —     |
| ...            |         |         |       |

Domain Correctness (Tier 2)
| Dimension               | <name1> | <name2> | Delta |
|--------------------------|---------|---------|-------|
| Path Validity            | 9/10    | 6/10    | +3    |
| ...                      |         |         |       |

| **Total**  | **72/100 (B)** | **56/100 (C)** | **+16** |
```

## Error Handling

- If the printing-press binary is not on PATH → show install instructions: `go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest`
- If the scorecard command fails → report the error with the full stderr output
- If a CLI directory doesn't exist → report which name couldn't be resolved
- If JSON parsing fails → show the raw output and report the parsing error
