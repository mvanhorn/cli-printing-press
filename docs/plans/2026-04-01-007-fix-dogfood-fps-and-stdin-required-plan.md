---
title: "fix: Eliminate dogfood false positives and MarkFlagRequired/stdin conflict"
type: fix
status: active
date: 2026-04-01
origin: docs/retros/2026-04-01-dominos-retro.md
---

# fix: Eliminate dogfood false positives and MarkFlagRequired/stdin conflict

## Overview

Two systemic issues affect every generated CLI: (1) dogfood reports false-positive dead functions and dead flags, producing a misleading FAIL verdict, and (2) the generator emits `MarkFlagRequired` on body fields for POST commands that also have `--stdin`, breaking the primary agent/script input path.

## Problem Frame

Every CLI that ships through the printing press gets a dogfood FAIL that is wrong. Users and Claude both spend time investigating false positives. Separately, every generated CLI with POST endpoints breaks `echo '{}' | cli cmd --stdin` because the body field is marked required even when stdin provides the data. Both issues were flagged in the Domino's retro (findings #1, #2, #4) and the dead function issue was also present in the Redfin retro. (see origin: docs/retros/2026-04-01-dominos-retro.md)

## Requirements Trace

- R1. Dogfood must not flag functions as dead when they are called transitively by other helpers that ARE called from command files
- R2. Dogfood must not flag framework-level flags (agent, noCache, noInput, rateLimit, timeout, yes) as dead when they are read in root.go PreRunE, client construction, or other non-RunE code paths
- R3. Dogfood must still correctly flag genuinely unused functions and flags
- R4. Generated POST/PUT/PATCH commands must accept `--stdin` without requiring the body field flag
- R5. Generated POST/PUT/PATCH commands must error clearly when neither `--stdin` nor the body field is provided

## Scope Boundaries

- Does not change verify, scorecard, or the generated CLI runtime
- Does not add new dogfood checks (only fixes existing ones)
- Does not change how the generator handles GET commands or path parameters

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/dogfood.go` lines 365-475: `checkDeadFlags` and `checkDeadFunctions`
- `internal/pipeline/dogfood_test.go`: Table-driven tests creating temp dirs with mock CLI files
- `internal/generator/templates/command_endpoint.go.tmpl` lines 235-254: Flag registration and MarkFlagRequired
- `internal/generator/generator_test.go`: `TestGeneratedOutput_MutatingCommandsHaveEnvelope` and similar POST-related tests

### Root Cause: Dead Functions

The `checkDeadFunctions` scanner (line 420) extracts function names from `helpers.go`, strips definition lines, and searches all `internal/cli/*.go` files for call sites. The stripped helpers.go IS included in the search corpus. However, the scanner does not do **transitive reachability**. If `apiErr` is only called by `classifyAPIError` (within helpers.go), and `classifyAPIError` IS called from command files, `apiErr` is still flagged as dead because the scanner sees no direct external call to `apiErr`. The scanner needs a second pass: after identifying "live" functions (called from outside helpers.go), mark any function called by a live function as also live.

### Root Cause: Dead Flags

The `checkDeadFlags` scanner (line 365) extracts field names from `&flags.<field>` patterns in root.go, removes declaration lines, then searches for `flags.<field>` or `.<field>` access patterns. Investigation confirmed all 6 flags ARE found by this scan in the dominos CLI. The false positives may be version-dependent or the scanning may miss patterns when flags are accessed through a renamed receiver. Need to verify the exact failure mode with a targeted test.

### Root Cause: MarkFlagRequired

The template at lines 243-247 unconditionally emits `cmd.MarkFlagRequired("<flagName>")` for required body fields. Lines 252-254 also emit the `--stdin` flag for POST/PUT/PATCH. The `--stdin` handler (lines 84-94) reads from stdin and bypasses body flags entirely, but cobra's required-flag validation runs before RunE, so the command fails before stdin is ever read.

## Key Technical Decisions

- **Transitive reachability via iterative marking, not full call-graph**: Building a proper call graph is overkill. Instead, after the initial scan identifies "live" functions, do a second pass: scan the (stripped) bodies of live functions for calls to other helpers. Mark those as live. Repeat until no new functions are marked. This is a simple fixed-point iteration that handles chains of any depth.
- **Skip MarkFlagRequired for body fields on mutation commands, add RunE guard instead**: Rather than trying to make cobra understand "required unless --stdin," remove the required constraint and add an explicit check at the top of RunE. This is the same pattern used across the CLI ecosystem for "either --file or --stdin" commands.

## Open Questions

### Resolved During Planning

- **Q: Does the dead flag scanner actually produce false positives on the dominos CLI?** Investigation during this session confirmed all 6 flags have matching access patterns. The false positives may be specific to how the dominos CLI was modified during Phase 3 (Codex added files that use different patterns). Need to write a test that reproduces the exact false positive to confirm the root cause before fixing.

### Deferred to Implementation

- **Q: How many iterations does the transitive reachability loop need in practice?** The helpers.go call chain is typically 2-3 deep. The loop should converge in 2-3 iterations. Verify empirically during implementation.

## Implementation Units

- [ ] **Unit 1: Add transitive reachability to checkDeadFunctions**

**Goal:** Functions called by other helper functions that ARE externally used should not be flagged as dead.

**Requirements:** R1, R3

**Dependencies:** None

**Files:**
- Modify: `internal/pipeline/dogfood.go` (`checkDeadFunctions` at line 420)
- Test: `internal/pipeline/dogfood_test.go`

**Approach:**
After the initial scan that identifies which functions have external call sites ("live set"), add an iterative expansion step:
1. Build a map of intra-helpers calls: for each function in helpers.go, which other helper functions does it call?
2. Seed the "live set" with functions that have external callers
3. Iterate: for each live function, add any helpers it calls to the live set
4. Repeat until the live set stops growing
5. Only report functions NOT in the live set as dead

The intra-helpers call map can reuse the same regex (`\b<funcname>\s*\(`) against each function's body (extracted between its `func` line and the next `func` line).

**Patterns to follow:**
- Existing `checkDeadFunctions` already uses regex-based scanning and `sortedKeys` iteration
- Test file uses `writeTestFile` helper and temp dirs

**Test scenarios:**
- Happy path: helpers.go has funcA (called from command files), funcB (called only by funcA), funcC (genuinely unused). Dogfood reports only funcC as dead.
- Edge case: chain of 3 (funcA calls funcB calls funcC, only funcA called externally). All three should be live.
- Edge case: mutual recursion (funcA calls funcB, funcB calls funcA, funcA called externally). Both should be live.
- Error path: helpers.go is empty or missing. Should return empty result (existing behavior preserved).
- Negative test: funcD is defined but never called by any function (neither from command files nor from other helpers). funcD should be flagged as dead.

**Verification:**
- Run `go test ./internal/pipeline/...` and all tests pass
- Run `printing-press dogfood --dir ~/printing-press/library/dominos-pp-cli` and no helper functions are flagged as dead (since all 12 are transitively reachable)

- [ ] **Unit 2: Investigate and fix dead flag false positives**

**Goal:** Framework-level flags should not be reported as dead.

**Requirements:** R2, R3

**Dependencies:** None (parallel with Unit 1)

**Files:**
- Modify: `internal/pipeline/dogfood.go` (`checkDeadFlags` at line 365)
- Test: `internal/pipeline/dogfood_test.go`

**Approach:**
First, write a test that reproduces the false positive by creating a mock root.go matching the generated pattern (flags declared with `&flags.agent`, read via `if flags.agent {` in PreRunE). If the test passes (no false positive), the issue may be environmental. If it fails, inspect the regex patterns for edge cases:
- Does `.noCache` matching fail when the receiver is `f` instead of `flags`? (The scanner checks for `.<field>` which should match `f.noCache`)
- Does line-stripping accidentally remove usage lines that also contain `&flags.`?

If the false positives cannot be reproduced in a clean test, document that they are environment-specific and add the known framework flags to an allowlist instead.

**Patterns to follow:**
- Existing `checkDeadFlags` test in dogfood_test.go creates a mock root.go with `&flags.jsonOutput` declarations and `flags.jsonOutput = true` usage

**Test scenarios:**
- Happy path: root.go declares `&flags.agent` and reads it via `if flags.agent {`. Should not be flagged.
- Happy path: root.go declares `&flags.rateLimit` and passes it via `client.New(cfg, f.timeout, f.rateLimit)`. Should not be flagged (`.rateLimit` access pattern).
- Edge case: flag declared with `&flags.noCache`, read in a DIFFERENT file (export.go) via `flags.noCache`. Should not be flagged.
- Negative test: `&flags.deadOnly` declared but never referenced anywhere. Should be flagged.

**Verification:**
- Run `go test ./internal/pipeline/...` and all tests pass
- Run `printing-press dogfood --dir ~/printing-press/library/dominos-pp-cli` and no framework flags are flagged

- [ ] **Unit 3: Replace MarkFlagRequired with RunE guard for stdin commands**

**Goal:** Generated POST/PUT/PATCH commands accept `--stdin` without requiring body field flags.

**Requirements:** R4, R5

**Dependencies:** None (parallel with Units 1-2)

**Files:**
- Modify: `internal/generator/templates/command_endpoint.go.tmpl` (lines 243-254)
- Test: `internal/generator/generator_test.go`

**Approach:**
In the template, conditionally skip `MarkFlagRequired` for body fields when the command method is POST, PUT, or PATCH (since those methods always get the `--stdin` flag). Instead, emit a guard at the top of RunE:

Template change at lines 243-247: wrap the `MarkFlagRequired` call in a condition that checks whether `--stdin` will be emitted (i.e., the method is POST/PUT/PATCH). For body fields on those methods, skip MarkFlagRequired.

Template addition near the top of RunE (after the `if flags.dryRun` block at approximately line 37): emit a guard that checks whether at least one body field or stdin was provided. The guard should list the required body field names in the error message.

**Patterns to follow:**
- Template already uses `{{- if or (eq .Endpoint.Method "POST") ...}}` conditionals
- Existing tests in generator_test.go verify generated output with `strings.Contains` assertions

**Test scenarios:**
- Happy path: Generate a CLI from a spec with a POST endpoint having a required body field. The generated command file should NOT contain `MarkFlagRequired` for the body field. It should contain a RunE guard checking `!stdinBody && body<Field> == ""`.
- Happy path: The generated command should contain the `--stdin` flag registration.
- Edge case: POST endpoint with NO required body fields. No guard needed, no MarkFlagRequired emitted.
- Edge case: GET endpoint with required params. `MarkFlagRequired` should still be emitted (GET commands don't get --stdin).
- Integration: Generate a full CLI, build it, and verify `echo '{}' | <cli> <cmd> --stdin --dry-run` exits 0.
- Integration: Generate a full CLI, build it, and verify `<cli> <cmd>` (no --stdin, no body flag) exits non-zero with a helpful error message.

**Verification:**
- Run `go test ./internal/generator/...` and all tests pass
- Generate a test CLI from a spec with POST endpoints and verify no `MarkFlagRequired` appears for body fields in mutation commands

## System-Wide Impact

- **Interaction graph:** Dogfood results feed into the shipcheck proof, which gates publishing. Fixing false positives means more CLIs will pass dogfood, changing the ship recommendation from `ship-with-gaps` to `ship` in some cases.
- **Error propagation:** The MarkFlagRequired fix changes the error source from cobra's flag validation (pre-RunE) to the RunE guard (during execution). Error messages will be more helpful ("provide data via --order or --stdin" vs "required flag not set").
- **Unchanged invariants:** Scorecard, verify, and the generated CLI's runtime behavior for valid inputs remain identical. Only invalid-input error handling changes for mutation commands.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Transitive reachability loop doesn't terminate | Helpers.go is finite and small (~500 lines, ~20 functions). Add a safety cap of 50 iterations. |
| Dead flag fix changes behavior for genuinely dead flags | Every test includes a negative case with a truly dead flag that must still be caught. |
| Template change breaks non-mutation commands | Guard condition is scoped to POST/PUT/PATCH only. GET/DELETE commands are unaffected. Template tests verify both paths. |

## Sources & References

- **Origin document:** [docs/retros/2026-04-01-dominos-retro.md](docs/retros/2026-04-01-dominos-retro.md) findings #1, #2, #4
- Related code: `internal/pipeline/dogfood.go` lines 365-475, `internal/generator/templates/command_endpoint.go.tmpl` lines 235-254
- Prior art: Redfin retro also flagged dead helper false positives
