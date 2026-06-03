# Skill Authoring

Conventions for the skills shipped from this repo (under `skills/`) and any internal skills under `.claude/skills/`. Loaded on-demand when working on skill content; not needed on every interaction.

## Workflow Parity

When a machine change alters what an agent should do, what a command now guarantees, or where source-of-truth data lives, update the relevant `SKILL.md` in the same change. Don't leave the skill as a stale manual workaround for behavior the machine now owns.

Check `skills/printing-press/SKILL.md` especially when touching generator, dogfood, verify, scorecard, publish, lock/promote, manuscript/runstate, or README/SKILL rendering behavior. If a machine step becomes deterministic, the skill should say the command owns it and reserve agentic review for the remaining semantic judgment. If a command's output, gate, phase order, or failure mode changes, update the phase instructions, reviewer prompt contracts, and fix-order guidance that mention it.

Decide responsibility explicitly:

- **Machine capability:** deterministic transformations, schema sync, provenance fields, generated sections with structured inputs, mechanical validation, artifact copying, score calculations, and anything where the correct output can be derived from repo files or command output without judgment. Implement it in Go/templates/tests; SKILL.md should describe the guarantee, not ask the agent to perform it manually.
- **SKILL.md / agent capability:** judgment calls, product tradeoffs, semantic honesty, whether prose overpromises, whether output is plausible, whether a feature is worth building, or workflows that require user/API context not available to the binary. Keep these as clear agent instructions and reviewer prompt contracts.
- **Both:** the machine should produce or verify the deterministic substrate, then SKILL.md should direct the agent to inspect the remaining semantic layer. Example pattern: dogfood syncs README/SKILL feature blocks from `novel_features_built`; the skill tells the agent to audit surrounding prose, recipes, trigger phrases, and examples for indirect claims.

Public parameter names follow the "Both" pattern. The agent uses evidence to
author semantic `flag_name`, compatibility `aliases`, and wire-only overrides
such as `url_name` / `body_name`; the Printing Press CLI inventories suspicious
wire names with `public-param-audit`, preserves evidence-backed skip decisions
in a ledger, and propagates authored fields through generated CLI, docs, MCP,
and manifest surfaces without inferring names itself.

For any SKILL.md update, search for the old concept across the skill file, not just the paragraph closest to the code change. Agentic review prompts often duplicate workflow assumptions from earlier phase instructions.

## Reference File Pattern

Skills use a `references/` directory for content that is only needed during specific phases or conditions. The SKILL.md stays lean with inline pointers (`Read [references/foo.md](...) when X`), and the agent loads the reference file only when the condition is met.

**Why this matters:** SKILL.md content is loaded into the context window for every tool call in the session. A 2,000-line skill burns tokens on every phase — even phases that don't need most of the content. Extracting conditional sections (e.g., browser capture flows only needed when browser-sniffing, codex templates only needed in codex mode) into reference files reduces baseline context by 30-40%.

**What stays inline:** Cardinal rules, decision matrices, phase structure, user-facing prompts — anything the agent needs at all times or to decide whether to load more.

**What gets extracted:** Implementation details for conditional paths: capture tool CLI commands, delegation templates, scoring frameworks, report templates. These are loaded on-demand when the agent reaches the relevant phase gate.

## Frontmatter: `context: fork` and `user-invocable`

Two skill frontmatter fields shape how a skill participates in larger workflows. Both default to permissive behavior (shared context, user-invocable). Set them explicitly when the skill plays a non-default role.

### `context: fork`

Default: skills run in the caller's context. The skill sees the full parent conversation; the parent sees the skill's tool calls and output interleaved with its own work.

`context: fork` gives the skill its own context window. Two consequences pull in opposite directions:

- **Benefit:** the skill starts with a fresh, dedicated context — its full window is available for its own work (multi-step loops, sub-agent transcripts, large reads) rather than competing with whatever the parent has already accumulated.
- **Cost:** the skill can't see anything from the parent's conversation. Everything it needs must come through `args`, be readable from disk, or be hardcoded.

The decision rule is whether the skill is **self-contained** given its declared inputs. If args plus the filesystem cover everything the skill needs (e.g., `printing-press-polish` takes a CLI dir and reads the rest from the repo and manuscripts; `printing-press-output-review` takes a CLI dir and runs `scorecard --live-check` to gather data), `context: fork` is a clear win. If the skill needs prior tool output, conversation history, or anything else the parent has accumulated, don't fork — the skill won't have access to it and you'll end up plumbing context through args anyway.

### `user-invocable`

Default: `true` — the skill is discoverable as a slash command (`/skill-name`) and routes from trigger phrases in the description. Setting `user-invocable: false` makes it internal-only: only Claude can invoke it (typically via the Skill tool from another skill).

Set `user-invocable: false` when the skill has no standalone meaning for a user. A user typing `/internal-skill` would get half a workflow with no input gate, no follow-up offer, no completion verdict. The actionable wrappers are the parent skills.

In this family, every printing-press skill is user-invocable except `printing-press-output-review`, which runs only as a sub-step inside Phase 4.85 (main skill) and the polish diagnostic loop.

### Internal-only sub-skill pattern

When a workflow step has multiple parents and no standalone user meaning, extract it into a `user-invocable: false` skill that both parents invoke via the Skill tool. Single source of truth for the prompt, gate logic, and any reference docs. The framework dispatches it; nobody has to find and read sibling SKILL.md prose at runtime.

The two fields compose. `context: fork` + `user-invocable: false` is the combo for self-contained internal sub-skills. `context: fork` alone (default user-invocable) is for user-facing skills with their own multi-step workflow that don't need parent context. Default frontmatter is for terse helper skills, or any skill that genuinely needs to see the parent's conversation.

## Frontmatter: `author`, `license`, and `metadata.hermes`

Three top-level frontmatter fields are required for every skill under `skills/` so alternative agent hosts (e.g. Hermes) can discover and install the skill from `mvanhorn/cli-printing-press`. The fields are additive — Claude Code ignores keys it doesn't recognize per its own contract, and Hermes ignores the Claude-Code-specific fields (`context`, `user-invocable`, `min-binary-version`, `allowed-tools`, `deprecated`) the same way.

### Required fields

- **`author: "<display name>"`** — the prose-shaped display name of the person who originally created the skill, double-quoted. Curate this from `git log --format=%an --reverse --follow skills/<name>/SKILL.md | head -1` (the first commit's author) per the `preserve-original-authorship` convention in [`docs/solutions/conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md`](solutions/conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md). **Do NOT** use `git config user.name` at install or sweep time — that flips attribution silently to whoever runs the sweep. The displayed author should match the prose form (`Matt Van Horn`, `Trevin Chow`), not the slug form (`matt-van-horn`, `trevin-chow`).
- **`license: "Apache-2.0"`** — the project's standard SPDX identifier, double-quoted. Mirrors the printed-CLI template at `internal/generator/templates/skill.md.tmpl:5` and the LICENSE in the repo root.
- **`metadata.hermes.tags`** — a YAML list of lowercase tag strings for Hermes search discoverability. The shared base set is `[printing-press, codegen, openapi, go, api]`; add a per-skill function tag (`amend`, `publish`, `import`, `polish`, `retro`, `score`, `reprint`, `review`, `catalog`) for disambiguation. Tag matching is substring-based and case-insensitive, so avoid duplicates across skills and keep the list short. **Exception:** the umbrella `printing-press` skill (the main entry point) is exempt from the function-tag rule — its 5-tag base set is its full tag list, since the umbrella does not need to disambiguate from a sibling. All other skills must add exactly one function tag.

### Style: `description:` scalar

The `description:` field is a string scalar; YAML supports two shapes for prose that fits on one line vs. multiple:

- **Plain scalar** (one line, no quoting needed for safe characters) — use when the description fits in roughly 80 columns and reads naturally as a single sentence. Example: `printing-press/SKILL.md`, `printing-press-catalog/SKILL.md`, `printing-press-publish/SKILL.md`, `printing-press-score/SKILL.md`.
- **Folded scalar (`>`)** — use when the description is multi-line, lists phases, or is intentionally long-form (Phase 0..N walkthrough, multi-step workflow, enumerated triggers). The folded style preserves readability in source while still rendering as a single string. Example: `printing-press-amend/SKILL.md`, `printing-press-import/SKILL.md`, `printing-press-output-review/SKILL.md`, `printing-press-polish/SKILL.md`, `printing-press-reprint/SKILL.md`, `printing-press-retro/SKILL.md`.

Both shapes parse identically; pick the one that reads cleanest in the source file. The shape is **not** part of the lock-in contract (`internal/skills/skills_test.go` asserts only that the field is non-empty), so do not bend a long description into a plain scalar just to match a sibling skill's style.

### Example

```yaml
---
name: printing-press-publish
description: Publish a generated CLI to the printing-press-library repo
author: "Trevin Chow"
license: "Apache-2.0"
version: 0.1.0
min-binary-version: "4.0.0"
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - AskUserQuestion
metadata:
  hermes:
    tags: [printing-press, codegen, openapi, go, api, publish]
---
```

### What is intentionally omitted

- **`version`** — not added where it's currently absent (`printing-press-import`, `printing-press-reprint`, `printing-press-output-review`). Per `internal/generator/skill_test.go:634-642` the Press version would mislead consumers about what changed; the same logic applies to internal skills. A future CI-time stamp (analogous to the library's `manifest.version`) could backfill this; for now, mirror the existing presence/absence per skill.
- **`required_environment_variables`** — not declared. The classifier has asymmetric failure cost (see [`docs/solutions/design-patterns/avoid-classification-when-failure-is-asymmetric-2026-05-06.md`](solutions/design-patterns/avoid-classification-when-failure-is-asymmetric-2026-05-06.md)); internal skills shell out to the Press binary which handles its own auth at run time.
- **`regions` / `api_language`** — not applicable. The Press skills are not region- or language-specific.

### Lock-in

`internal/skills/skills_test.go` parses every `skills/<name>/SKILL.md` frontmatter as YAML and asserts the three required fields are present and well-formed, plus a per-skill curated `internalSkillAuthorByName` map sourced from git first-commit. Any new skill added without populating these fields will fail the test with a clear message naming the missing file.
