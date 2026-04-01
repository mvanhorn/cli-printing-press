---
title: "feat: Deterministic dead-code removal via printing-press polish"
type: feat
status: active
date: 2026-04-01
origin: docs/retros/2026-04-01-steam-run5-retro.md
---

# Deterministic Dead-Code Removal

## Overview

Add a `printing-press polish --remove-dead-code` command that runs dogfood to identify dead functions and flags, then uses Go's AST parser to surgically remove them from the generated CLI. This replaces unreliable LLM instructions ("don't write dead code") with a deterministic binary pass that catches dead code every time — regardless of what the agent did during the build phase.

## Problem Frame

Across 5 Steam runs, dead code appeared in every single generated CLI. The agent that builds wrapper commands sometimes defines helper functions that end up unused. Dogfood catches them, but removal requires either manual intervention or a polish pass by Claude — which is unreliable. The retro concluded: "move quality enforcement from LLM instructions (unreliable) to deterministic binary passes (reliable)."

(see origin: docs/retros/2026-04-01-steam-run5-retro.md — Key Insight section)

## Requirements Trace

- R1. `printing-press polish --remove-dead-code --dir <cli-dir>` removes all dead functions identified by dogfood
- R2. Removal uses Go AST parsing — not regex or string manipulation — to delete function nodes cleanly
- R3. After removal, `go build ./...` must pass (safety net: if removing a function breaks the build, it wasn't actually dead — dogfood had a false positive)
- R4. The command reports what was removed and verifies the build

## Scope Boundaries

- This plan covers dead FUNCTIONS only (from `helpers.go` and other CLI files). Dead FLAGS are a separate concern with different removal mechanics (editing cobra registration in `root.go`).
- Not changing the dogfood detection logic — it already works. This plan adds removal.
- Not adding this as an automatic post-generation step yet. It's a manual command first. Automation can come later after validation.
- Not removing `usageErr` from the template (that's a separate template fix). This command handles whatever dead code exists in a generated CLI, regardless of source.

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/dogfood.go:420` — `checkDeadFunctions()` identifies dead functions by name. Returns `DeadCodeResult{Items: []string{"funcName1", "funcName2"}}`.
- `internal/cli/dogfood.go` — existing CLI command pattern. `polish` would follow the same structure.
- Go standard library: `go/parser`, `go/ast`, `go/printer`, `go/token` — all needed for AST-based removal.
- Generated CLIs have dead functions in `internal/cli/helpers.go` (from templates) and in command files (from Claude's build phase).

### Key Constraint

Dogfood currently only scans `helpers.go` for dead functions. Claude-introduced dead code (like `formatCompact`) may be in other files under `internal/cli/`. The dead-function scanner may need to be extended to scan ALL `.go` files in `internal/cli/`, not just `helpers.go`. **Defer this decision to implementation** — check whether `checkDeadFunctions` already scans all files or just `helpers.go`.

## Key Technical Decisions

- **Go AST over regex:** Regex can find function definitions but can't cleanly remove them (comments above the function, blank lines after, associated doc comments). Go's AST parser handles all of this correctly — `ast.File.Decls` contains each function as a discrete node that can be removed.

- **Safety net via `go build`:** After removing functions, run `go build ./...` on the CLI directory. If the build fails, a "dead" function was actually called through a path dogfood missed (false positive). Restore the file and report the error. This makes the command safe to run even if dogfood has bugs.

- **Manual command first, not automatic:** The `/printing-press` skill can call this command after the build phase, but it shouldn't be hardwired into the generator or dogfood. The user or skill decides when to run it. This prevents the tool from deleting code the user intended to keep.

## Open Questions

### Resolved During Planning

- **Should it modify files in place or write to a temp dir?** In place — the CLI directory is a working copy, not a precious artifact. The `go build` safety net catches mistakes.

### Deferred to Implementation

- **Does `checkDeadFunctions` scan all CLI files or just helpers.go?** Read the function. If just helpers.go, the removal command should also only target helpers.go initially. Extending to all files is a follow-up.
- **Should dead flags also be removed?** Flags require editing `root.go`'s `PersistentFlags()` registration, which is more complex. Defer to a follow-up.

## Implementation Units

- [ ] **Unit 1: Add `printing-press polish` subcommand with `--remove-dead-code` flag**

**Goal:** A new CLI command that runs dogfood dead-function detection, then AST-removes the identified functions

**Requirements:** R1, R2, R3, R4

**Dependencies:** None

**Files:**
- Create: `internal/cli/polish.go`
- Create: `internal/pipeline/polish.go`
- Test: `internal/pipeline/polish_test.go`
- Modify: `internal/cli/root.go` (register the command)

**Approach:**
- The CLI command (`internal/cli/polish.go`) handles flags (`--dir`, `--remove-dead-code`, `--json`, `--dry-run`) and delegates to `pipeline.RunPolish()`.
- The pipeline function (`internal/pipeline/polish.go`) orchestrates: (1) run `checkDeadFunctions` to get the dead list, (2) for each dead function, parse the file's AST, find the function declaration, remove it from `ast.File.Decls`, (3) write the file back with `go/printer`, (4) run `go build ./...` as safety net, (5) if build fails, restore the original file.
- `--dry-run` reports what would be removed without modifying files.
- Output: list of removed functions, build verification result.

**Patterns to follow:**
- `internal/cli/dogfood.go` for CLI command structure
- `internal/pipeline/dogfood.go` for pipeline function structure
- Go standard library `go/parser` + `go/ast` + `go/printer` for AST manipulation

**Test scenarios:**
- Happy path: helpers.go with 2 dead functions (defined but never called in any other file) → both removed, file rewritten, `go build` passes
- Happy path: helpers.go with 0 dead functions → no changes, clean output
- Edge case: removing a function that has a doc comment above it → comment also removed (AST handles this)
- Error path: dogfood falsely identifies a function as dead (it's actually called through an interface or reflection) → `go build` fails after removal → original file restored, error reported
- Edge case: `--dry-run` → reports what would be removed, no files modified
- Integration: run on the Steam CLI after a fresh generation + build → dead functions removed, scorecard dead_code improves

**Verification:**
- `printing-press polish --remove-dead-code --dir <steam-cli>` removes `usageErr` and `formatCompact`
- `go build ./...` passes after removal
- `printing-press dogfood` reports 0 dead functions after polish

## System-Wide Impact

- **New command surface:** Adds `printing-press polish` as a new subcommand. No changes to existing commands.
- **Dogfood reuse:** Uses `checkDeadFunctions` from the dogfood package — no duplication.
- **Generated CLI files modified:** The command writes to generated CLI files. This is intentional — it's a post-generation polish step.
- **Unchanged:** Generator templates, scorecard, verify, dogfood detection logic.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| AST removal leaves broken formatting | `go/printer` with `format.Source` produces correctly formatted output |
| Dogfood false positive → build breaks after removal | Safety net: run `go build`, restore on failure |
| Function removal breaks an import (removed function was the only user of a package) | `goimports` or `go build` will flag unused imports; the safety net catches this |

## Sources & References

- **Origin:** [docs/retros/2026-04-01-steam-run5-retro.md](docs/retros/2026-04-01-steam-run5-retro.md) — Key Insight section
- Dogfood dead-function detection: `internal/pipeline/dogfood.go:420`
- CLI command pattern: `internal/cli/dogfood.go`
- Go AST docs: `go/ast`, `go/parser`, `go/printer`, `go/token` (standard library)
- PRs #100-104 (mvanhorn/cli-printing-press)
