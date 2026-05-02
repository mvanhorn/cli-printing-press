# Skill Authoring

Conventions for the skills shipped from this repo (under `skills/`) and any internal skills under `.claude/skills/`. Loaded on-demand when working on skill content; not needed on every interaction.

## Workflow Parity

When a machine change alters what an agent should do, what a command now guarantees, or where source-of-truth data lives, update the relevant `SKILL.md` in the same change. Don't leave the skill as a stale manual workaround for behavior the machine now owns.

Check `skills/printing-press/SKILL.md` especially when touching generator, dogfood, verify, scorecard, publish, lock/promote, manuscript/runstate, or README/SKILL rendering behavior. If a machine step becomes deterministic, the skill should say the command owns it and reserve agentic review for the remaining semantic judgment. If a command's output, gate, phase order, or failure mode changes, update the phase instructions, reviewer prompt contracts, and fix-order guidance that mention it.

Decide responsibility explicitly:

- **Machine capability:** deterministic transformations, schema sync, provenance fields, generated sections with structured inputs, mechanical validation, artifact copying, score calculations, and anything where the correct output can be derived from repo files or command output without judgment. Implement it in Go/templates/tests; SKILL.md should describe the guarantee, not ask the agent to perform it manually.
- **SKILL.md / agent capability:** judgment calls, product tradeoffs, semantic honesty, whether prose overpromises, whether output is plausible, whether a feature is worth building, or workflows that require user/API context not available to the binary. Keep these as clear agent instructions and reviewer prompt contracts.
- **Both:** the machine should produce or verify the deterministic substrate, then SKILL.md should direct the agent to inspect the remaining semantic layer. Example pattern: dogfood syncs README/SKILL feature blocks from `novel_features_built`; the skill tells the agent to audit surrounding prose, recipes, trigger phrases, and examples for indirect claims.

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
