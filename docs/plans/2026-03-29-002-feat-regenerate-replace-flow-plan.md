---
title: "feat: Show existing CLI context and clarify regeneration menu in skill"
type: feat
status: completed
date: 2026-03-29
---

# feat: Show existing CLI context and clarify regeneration menu in skill

## Overview

When a prior CLI exists, the skill's menu ("Regenerate fresh / Emboss existing") provides no context about what exists and no clarity about what each option does. The user doesn't know their CLI's age, version, or score — and doesn't understand that "Regenerate fresh" overwrites template files in the same directory (via `--force`) while "Emboss" leaves generated code untouched.

## Problem Frame

A user runs `/printing-press Cal.com` for the second time. They already have `cal-com-pp-cli` in their library. The skill detects prior research and shows:

```
Reusing prior run from yesterday:
- Research brief: 285-endpoint OpenAPI 3.0 spec...
- Prior CLI scored 87/100...
```

Then presents options (Regenerate fresh / Emboss existing / Review research). The user doesn't know:
1. What "Regenerate fresh" actually does to the existing CLI (it overwrites generated files in-place via `--force`, preserving hand-written files that don't collide)
2. How old the existing CLI is or what generator version made it
3. That "Emboss" never re-runs the generator — it only fixes and improves existing code

## Requirements Trace

- R1. The skill shows existing CLI context (name, age, generator version) before the regenerate/emboss menu
- R2. Menu options clearly describe what happens to the existing CLI

## Scope Boundaries

- SKILL.md-only change — no Go code modifications
- Uses existing `printing-press library list --json` for manifest data
- No version comparison or automated upgrade detection
- No changes to the emboss flow
- No changes to `PublishWorkingCLI` or `ClaimOutputDir` (the `-2` suffix issue in the autonomous path is a separate concern)

## Context & Research

### Relevant Code and Patterns

- `skills/printing-press/SKILL.md` lines 197-208 — Phase 0: Resolve And Reuse, where prior run detection happens
- `skills/printing-press/SKILL.md` lines 707-741 — Phase 2: Generate, shows that `--force` writes directly to `$PRESS_LIBRARY/<api>-pp-cli`
- `internal/cli/library.go` — `library list --json` already reads `.printing-press.json` manifests and returns `cli_name`, `api_name`, `category`, `description`, `modified` for each CLI
- `skills/printing-press/SKILL.md` lines 209-243 — API Key Gate, shows the pattern for AskUserQuestion usage in Phase 0

### Key Behavioral Facts

- The skill generates directly to `$PRESS_LIBRARY/<api>-pp-cli` with `--force`, which overwrites template outputs in the existing directory. Hand-written files that don't collide with template outputs survive. The skill then rebuilds transcendence features after generation.
- `library list --json` returns an array of objects with fields: `cli_name`, `dir`, `api_name`, `category`, `catalog_entry`, `description`, `modified`. The `modified` timestamp comes from directory mtime. For CLIs with manifests, it also includes manifest data.
- Pre-manifest CLIs (generated before the manifest feature) will show up in `library list` with `api_name` derived from the directory name and `modified` from mtime.

## Key Technical Decisions

- **Use `library list --json` instead of adding a new command**: The skill can filter the JSON output for the target API. This avoids Go code changes for what is fundamentally a UX improvement. Performance is not a concern — the library typically has 1-10 entries.

- **No replace-or-keep question after regeneration**: The skill already replaces in-place via `--force`. Adding a post-regeneration question would imply there's a choice, but the behavior is already defined by the generate command. Instead, the menu description should be explicit upfront about what happens.

## Implementation Units

- [x] **Unit 1: Update skill Phase 0 with context display and clearer menu**

  **Goal:** Show existing CLI info before the menu and make option descriptions explain what actually happens.

  **Requirements:** R1, R2

  **Dependencies:** None

  **Files:**
  - Modify: `skills/printing-press/SKILL.md` — Phase 0: Resolve And Reuse section

  **Approach:**

  In Phase 0, after resolving the API name and before presenting the regenerate/emboss menu:

  1. Check if the API has an existing CLI in the library:
     ```bash
     printing-press library list --json
     ```
     Filter the JSON output for an entry matching the target API name.

  2. If found, display context before the menu:
     ```
     Found existing <cli-name> in library (last modified <relative-time>).
     ```
     If the manifest includes `printing_press_version`, add: `Built with printing-press v<version>.`

  3. Present the regenerate/emboss menu with descriptive options:
     - "Generate a fresh CLI — re-runs the generator with `--force` into the same directory, overwrites generated code, then rebuilds transcendence features on top. Prior research is reused if recent. ~15-20 min."
     - "Improve existing CLI — keeps all current code, audits for gaps, implements top improvements. Generator is not re-run. ~10 min."
     - "Review prior research first"

  4. If `library list --json` fails (binary too old, no library directory), fall back to the current menu without context — no error, just less info.

  **Patterns to follow:**
  - API Key Gate in Phase 0 (lines 209-243) for AskUserQuestion flow pattern
  - Existing prior-run detection logic in Phase 0 (lines 197-208)

  **Test scenarios:**
  - Happy path: prior CLI exists with manifest — context displayed with version and age, menu presented with descriptive options
  - Edge case: prior CLI exists without manifest (pre-manifest CLI) — context shows name and mtime-based age, no version shown
  - Edge case: no prior CLI in library — skip context display, show menu without the "Found existing..." line (or skip to normal first-run flow)
  - Edge case: `library list --json` command fails (old binary without the command) — fall back to current menu behavior
  - Happy path: user picks "Generate a fresh CLI" — routes to existing Phase 2 generation flow
  - Happy path: user picks "Improve existing CLI" — routes to emboss flow

  **Verification:**
  - Skill displays existing CLI context before the menu when a library CLI exists
  - Menu option descriptions explain the behavioral difference (generator re-runs vs. not)
  - Both paths (regenerate and emboss) work correctly after the menu
  - Graceful fallback when no manifest or old binary

## System-Wide Impact

- **Skill-only change**: No binary changes, no API surface changes, no library directory behavior changes.
- **Backward compatible**: Falls back gracefully if `library list --json` is unavailable.
- **No behavioral change to generation or emboss**: The skill just communicates better about what already happens.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `library list --json` output format changes | The skill should tolerate missing fields — use whatever is available. |
| Pre-manifest CLIs show limited context | Fall back to directory name and mtime. |
| Old binary without `library list` | Skill detects failure and falls back to current menu. |
| Menu wording is too long for terminal display | Iterate on wording during implementation — keep it concise. |
