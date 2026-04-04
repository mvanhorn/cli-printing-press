---
title: "feat: Hide raw resource commands when promoted commands exist, add api discovery"
type: feat
status: active
date: 2026-04-04
---

# Hide Raw Resource Commands + Add API Discovery

## Overview

When the generator produces promoted commands (friendly top-level aliases like `resolve`, `profile`, `games`), the raw resource parent commands (`isteam-user`, `iplayer-service`) should be hidden from `--help` but remain functional. An `api` discovery command should be auto-generated to let agents and power users browse hidden interfaces.

This is conditional: CLIs without promoted commands show resource parents as usual (they ARE the user interface). The `api` command is only generated when there are hidden commands to discover.

## Problem Frame

The generator creates promoted commands (friendly aliases) alongside the original resource parent commands. Both appear in `--help`, creating a noisy, confusing listing where `isteam-user get-player-summaries` sits next to `profile`. Users don't know which to use. Agents parsing `--help` get duplicate functionality.

The Steam CLI proved the pattern: hide raw commands with `Hidden: true`, add an `api` command for discovery. This plan makes it a machine change so every future CLI benefits.

## Requirements Trace

- R1. Resource parent commands set `Hidden: true` when the CLI has promoted commands
- R2. Resource parent commands remain visible when no promoted commands exist
- R3. An `api` discovery command is generated when promoted commands exist, listing all hidden interfaces and their methods
- R4. The `api` command is NOT generated when there are no promoted commands (nothing to discover)
- R5. Hidden commands remain fully functional — only hidden from `--help` output

## Scope Boundaries

- **Not changing promoted command generation logic.** `buildPromotedCommands()` is unchanged.
- **Not changing which commands get promoted.** The selection criteria stay the same.
- **Not adding manual promoted command configuration.** The Phase 3 agent-built commands (like Steam's `resolve`, `profile`) are printed-CLI work, not machine work. This plan handles the machine's auto-generated promoted commands only.

## Key Technical Decisions

- **Use `len(.PromotedCommands) > 0` as the gate**: The root template data already has `.PromotedCommands`. If the slice is non-empty, hide resource parents and add `api`. If empty, no change from today. No new flags or configuration needed.

- **`Hidden` field on command_parent.go.tmpl**: Add a `Hidden bool` to the parent command template data struct. Set it to `true` when promoted commands exist. The template emits `Hidden: true,` in the cobra.Command struct.

- **`api_discovery.go.tmpl` as a new template**: A standalone template (not vision-gated) rendered only when promoted commands exist. It creates the `api` command that lists hidden interface commands and drills into their methods.

## Implementation Units

- [ ] **Unit 1: Add `Hidden` field to resource parent template**

**Goal:** Resource parent commands set `Hidden: true` when promoted commands exist.

**Requirements:** R1, R2, R5

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/command_parent.go.tmpl`
- Modify: `internal/generator/generator.go` (add `Hidden bool` to parent template data struct)
- Test: `internal/generator/generator_test.go`

**Approach:**
- Add `Hidden bool` to the parent command template data struct in `generator.go` (around line 310)
- Set `Hidden: len(promotedCommands) > 0` — but `promotedCommands` is computed after resource rendering. Solution: compute promoted commands earlier (before the resource rendering loop), or pass the count through a different path. Since we already moved profiling early (for HasStore), we can compute `promotedCommands` before the resource loop too.
- In `command_parent.go.tmpl`, add `{{if .Hidden}}Hidden: true,{{end}}` after the `Short:` field

**Patterns to follow:**
- Existing `HasStore bool` field on endpoint template data (same pattern — generator sets, template uses)
- Existing `Hidden: true` pattern in cobra (standard cobra field)

**Test scenarios:**
- Happy path: spec with promoted commands (e.g., clerk) → resource parent commands have `Hidden: true` in generated code
- Happy path: spec without promoted commands (e.g., petstore with few resources) → resource parent commands do NOT have `Hidden: true`
- Integration: generate a CLI with promoted commands → `--help` shows promoted commands, hides resource parents
- Edge case: resource parent with no promotable endpoint → still hidden if OTHER resources have promoted commands (the gate is global, not per-resource)

**Verification:**
- Generated CLI with promoted commands shows clean `--help` without raw interface names
- Generated CLI without promoted commands shows resource parents as before

---

- [ ] **Unit 2: Create `api_discovery.go.tmpl`**

**Goal:** Auto-generate an `api` command that lists hidden interfaces and their methods.

**Requirements:** R3, R4

**Dependencies:** Unit 1

**Files:**
- Create: `internal/generator/templates/api_discovery.go.tmpl`
- Modify: `internal/generator/generator.go` (render the template conditionally when promoted commands exist)
- Test: `internal/generator/generator_test.go`

**Approach:**
- Create `api_discovery.go.tmpl` in the `cli` package. It generates a command that:
  - With no args: lists all `Hidden` commands on the root, with name and Short description
  - With one arg (interface name): lists that interface's subcommands (methods)
  - Includes realistic examples in the help text
- In `generator.go`, render `api_discovery.go.tmpl` only when `len(promotedCommands) > 0`. Place the render after promoted commands are computed but before `root.go.tmpl` is rendered (so the template can reference the api command constructor).
- Register `newAPICmd` in `root.go.tmpl` conditionally: `{{if .PromotedCommands}}rootCmd.AddCommand(newAPICmd(&flags)){{end}}`

**Patterns to follow:**
- The `cmd_api.go` file we manually built for Steam — use it as the reference implementation
- Existing conditional command registration in `root.go.tmpl` (e.g., `{{if .VisionSet.Search}}`)

**Test scenarios:**
- Happy path: generate a CLI with promoted commands → `api` command exists, lists hidden interfaces
- Happy path: generate a CLI without promoted commands → no `api` command, no `api_discovery.go` file
- Happy path: `api <interface>` shows methods for that interface
- Edge case: `api nonexistent` returns a clear error
- Integration: generated CLI compiles with `api` command, `--help` shows it alongside promoted commands

**Verification:**
- Generated CLI with promoted commands has `api` in `--help`
- `api` lists all hidden interfaces
- `api <interface>` shows methods
- Generated CLI without promoted commands does NOT have `api`

---

- [ ] **Unit 3: Update root.go.tmpl registration and file count test**

**Goal:** Register `api` command conditionally and update expected file counts.

**Requirements:** R3, R4

**Dependencies:** Unit 1, Unit 2

**Files:**
- Modify: `internal/generator/templates/root.go.tmpl`
- Modify: `internal/generator/generator_test.go` (update expected file counts for specs that get promoted commands)

**Approach:**
- Add `{{if .PromotedCommands}}rootCmd.AddCommand(newAPICmd(&flags)){{end}}` before the promoted commands registration block
- Update expected file counts in `TestGenerateProjectsCompile` — specs with promoted commands get +1 file (api_discovery.go)

**Test scenarios:**
- Happy path: all existing test specs compile with updated file counts
- Happy path: petstore (no promoted commands) → no api_discovery.go, file count unchanged
- Happy path: clerk/loops (with promoted commands) → api_discovery.go generated, file count +1

**Verification:**
- `go test ./internal/generator/...` passes
- `go test ./...` passes (1055+ tests)

## System-Wide Impact

- **`--help` output changes for CLIs with promoted commands**: Resource parent commands disappear from `--help`. This is the intended behavior — the promoted commands are the user-facing interface. Hidden commands remain functional.
- **No change for CLIs without promoted commands**: Petstore-class CLIs with few resources and no promotable endpoints show resource parents exactly as before.
- **Scorecard unaffected**: The scorecard checks command count, help output, etc. — hidden commands still count as commands. The `api` command adds one more visible command.
- **Verify unaffected**: Verify discovers commands from both `--help` and root.go parsing. Hidden commands are still testable.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Promoted commands may not cover all important operations | The `api` command provides full access to hidden interfaces. Power users and agents can discover and use any endpoint. |
| Breaking change for existing CLIs that regenerate | `--help` output changes. This is intentional and desirable — the old help was noisy. No functional behavior changes. |
| `buildPromotedCommands()` may produce poor aliases for some APIs | Not in scope — the selection criteria are unchanged. The skill's Phase 3 is where the agent makes product decisions about command naming. |

## Sources & References

- Reference implementation: `steam-web-pp-cli/internal/cli/cmd_api.go` (manually built)
- Related code: `internal/generator/generator.go` (buildPromotedCommands, root template data)
- Related code: `internal/generator/templates/command_parent.go.tmpl` (resource parent template)
- Related code: `internal/generator/templates/root.go.tmpl` (command registration)
