---
title: "feat: Make printing-press-retro a public skill with GitHub issue output"
type: feat
status: complete
date: 2026-04-05
origin: docs/brainstorms/2026-04-04-public-retro-skill-requirements.md
---

# feat: Make printing-press-retro a public skill with GitHub issue output

## Overview

Move the `/printing-press-retro` skill from private (`.claude/skills/`) to public (`skills/`), change its output from local `docs/retros/` files to GitHub issues on `mvanhorn/cli-printing-press` with scrubbed artifact uploads via catbox.moe, and adapt the methodology for external users who don't have the repo checked out.

## Problem Frame

The retro skill captures high-value machine improvement signals but is only available to repo maintainers. External plugin users discover the same issues but have no structured way to contribute findings back. Making it public turns every generation run into a potential machine improvement. (see origin: `docs/brainstorms/2026-04-04-public-retro-skill-requirements.md`)

## Requirements Trace

- R1. Full retro methodology adapted for dual context (in-repo vs external)
- R2. Retro distilled into well-structured GitHub issue body
- R3. Manuscript zip uploaded to catbox.moe, linked from issue
- R4. CLI source zip (excluding binary, vendor/, go.sum) uploaded to catbox.moe
- R5. Layered secrets scrub: exact-value + pattern-based scanning before upload
- R6. GitHub issue creation via `gh issue create --repo mvanhorn/cli-printing-press`
- R7. Context-aware: in-repo gets local save + offer-to-plan; external gets issue-only
- R8. Retro always saved to manuscript proofs
- R9. Refuse if no manuscripts exist
- R10. Clarify which API if ambiguous
- R11. Default to most recent run
- R12. Move to `skills/`, apply reference file pattern
- R13. Remove private skill after verification
- R14. Graceful degradation when `gh` auth fails
- R15. Graceful degradation when catbox upload fails

## Scope Boundaries

- The analytical methodology (classification, scorer audit, cross-API stress tests) is adapted for two contexts, not redesigned
- No Go code changes. No manifest changes (skills auto-discovered)
- No issue labels, milestones, or project board assignment
- One issue per retro run (not per finding)

## Context & Research

### Relevant Code and Patterns

- **Setup contract**: `<!-- PRESS_SETUP_CONTRACT_START -->` block used by 4/5 public skills — sets `PRESS_HOME`, `PRESS_MANUSCRIPTS`, `PRESS_LIBRARY`, detects repo vs standalone
- **Reference file pattern**: Main skill has 11 reference files. Inline pointers like `Read [references/foo.md](references/foo.md) when X`
- **Secret protection**: `skills/printing-press/references/secret-protection.md` — exact-value scanning with `grep -F`, python3 literal replacement, HAR auth stripping
- **Public skill frontmatter**: `name`, `description` (with trigger phrases), `version`, `min-binary-version`, `allowed-tools`
- **Publish skill**: Best analog for `gh` CLI usage — `gh auth status`, `gh pr create` with HEREDOC body, `--repo` targeting, full https:// URLs

### Institutional Learnings

- **Never mutate source directory** (`docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md`): Scrub copies in a temp directory, not originals. Zip the temp copy, upload that.
- **Path traversal protection** (`docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md`): Validate user-provided API names for traversal characters, verify resolved paths stay under `~/printing-press/`.
- **Skill instruction reliability ~70-90%** (`docs/retros/2026-04-01-steam-run5-retro.md`): The secrets scrub must use deterministic bash commands in reference files, not vague prose instructions.
- **Shared setup contract** (`docs/retros/2026-04-03-dominos-retro.md`): Use the standard `PRESS_SETUP_CONTRACT` block to inherit binary detection fixes. **However**: the standard contract exits early if the printing-press binary isn't found. The retro skill doesn't need the binary — it only needs the path variables. A modified contract that skips the binary check is required.
- **Output layout contract** (`docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`): Canonical paths at `~/printing-press/manuscripts/<api-slug>/<run-id>/` and `~/printing-press/library/<api-slug>-pp-cli/`.

## Key Technical Decisions

- **Modified setup contract (no binary check)**: The standard `PRESS_SETUP_CONTRACT` exits early if the printing-press binary isn't found. The retro skill only needs path variables (`PRESS_HOME`, `PRESS_MANUSCRIPTS`, `PRESS_LIBRARY`), not the binary. Use a slimmed-down contract that resolves paths and detects repo context but skips binary detection entirely. This avoids aborting for external users who installed the plugin but not the Go binary.
- **Repo detection via marker file, not origin URL**: Check for `cmd/printing-press/main.go` relative to `_scope_dir`. More reliable than parsing `git remote get-url origin` which breaks on forks and SSH vs HTTPS.
- **Three reference files extracted from SKILL.md**: `references/secret-scrubbing.md` (deterministic scrub commands), `references/issue-template.md` (GitHub issue body format), `references/artifact-packaging.md` (zip + catbox upload). Keeps SKILL.md focused on the analytical methodology.
- **Scrub-on-copy pattern**: Copy artifacts to a temp directory, scrub the copies, zip the scrubbed copies, upload. Never modify `~/printing-press/manuscripts/` or `~/printing-press/library/` in place.
- **Phase 5.5 work units adapt to context**: In-repo runs use Glob/Grep tool invocations (not bash `find`/`grep`) to resolve target file paths. External runs describe target components by name and acceptance criteria without file paths.
- **Phase 6 bifurcates on context**: In-repo: save to `docs/retros/` + manuscript proofs + issue + offer `/ce:plan` (best-effort — guarded by checking skill availability). External: save to manuscript proofs + issue only.
- **`gh issue create` modeled on publish skill's `gh pr create`**: Check `gh auth status`, build body via HEREDOC, use `--repo mvanhorn/cli-printing-press`, return full `https://` URLs.
- **Catbox upload via curl**: `curl -F "reqtype=fileupload" -F "fileToUpload=@file.zip" https://catbox.moe/user/api.php` returns a direct URL. No auth needed.

## Open Questions

### Resolved During Planning

- **Which phases need conditional logic?** Phase 5.5 (work units: file path resolution) and Phase 6 (save/present: output routing). Phases 1-5 work on manuscripts which are always available. Phase 2 "mine the session" is best-effort — if no conversation history exists, note it and proceed with manuscript evidence only.
- **GitHub issue body template**: Full retro goes to manuscript proofs and as a linked artifact. Issue body contains a distilled summary: session stats, priority tables (Do Now / Do Next / Skip), work unit summaries, and artifact download links. Not the full 6-question classification per finding — that's in the attached retro document.
- **Skill frontmatter**: Add `version: 0.1.0`. No `min-binary-version` needed — the retro skill does not invoke the printing-press binary directly (it reads manuscripts and runs `gh`). Update description and trigger phrases for external users.
- **Do we need `min-binary-version`?**: No. The retro skill reads manuscript files and invokes `gh`/`curl`. It does not call the printing-press binary. The setup contract is still useful for `PRESS_HOME`/`PRESS_MANUSCRIPTS` path resolution, but no binary version check is needed.

- **Secret scanner regex patterns (pinned)**: The following patterns are specified for the pattern-based scanner. Implementation may tune thresholds but the pattern list is fixed:
  - `sk_live_[A-Za-z0-9]{20,}` — Stripe live keys
  - `sk_test_[A-Za-z0-9]{20,}` — Stripe test keys
  - `ghp_[A-Za-z0-9]{36,}` — GitHub personal access tokens
  - `gho_[A-Za-z0-9]{36,}` — GitHub OAuth tokens
  - `Bearer [A-Za-z0-9._~+/=-]{20,}` — Bearer tokens in code
  - `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}` — JWT-shaped strings
  - `(SECRET|TOKEN|KEY|PASSWORD)\s*[:=]\s*["'][^"']{16,}["']` — env var assignments with secret-like values
  - Redaction format: `<REDACTED:pattern-name>` (e.g., `<REDACTED:stripe-live-key>`)

### Deferred to Implementation

- **False-positive tuning**: Run the pinned regex patterns against real manuscripts to verify false-positive rate. Adjust match thresholds if needed but do not drop pattern categories.
- **Issue body character limits**: GitHub issue bodies have a practical limit around 65KB. If the distilled retro exceeds this (unlikely for a single API retro), truncate with "Full retro in attached artifact" and retry. This is a distinct error path from R14 (auth failure) — handle it separately.

## Implementation Units

- [ ] **Unit 1: Create reference files**

**Goal:** Write the three reference files that contain conditional implementation details extracted from the main SKILL.md.

**Requirements:** R5, R3, R4, R2, R12

**Dependencies:** None

**Files:**
- Create: `skills/printing-press-retro/references/secret-scrubbing.md`
- Create: `skills/printing-press-retro/references/artifact-packaging.md`
- Create: `skills/printing-press-retro/references/issue-template.md`

**Approach:**

`references/secret-scrubbing.md`:
- Header: "Read this file during Phase 6 before zipping and uploading artifacts."
- Layer 1: Exact-value scanning adapted from `skills/printing-press/references/secret-protection.md`. `grep -F` for session API key value, python3 literal replacement. 16-char minimum guard.
- Layer 2: Pattern-based scanning with deterministic bash commands (not prose instructions). Use the pinned regex patterns from the "Resolved During Planning" section above. Each pattern gets a concrete `grep -E` command block the agent copies and runs.
- Redaction: Replace matched values with `<REDACTED:pattern-name>` to preserve debugging context.
- HAR stripping: Include the `jq` pipeline from `secret-protection.md` for any HAR files in manuscripts.
- **Critical**: All scrub operations work on the temp copy, never originals.

`references/artifact-packaging.md`:
- Header: "Read this file during Phase 6 when packaging artifacts for upload."
- Step 1: Create temp staging directory (`mktemp -d`)
- Step 2: Copy manuscript run directory and CLI source to staging (excluding binary, vendor/, go.sum, .git/)
- Step 3: Read and apply `references/secret-scrubbing.md` on the staging copies
- Step 4: Zip each into named archives: `<api-slug>-manuscripts.zip`, `<api-slug>-cli-source.zip`
- Step 5: Upload each to catbox.moe via curl, capture returned URLs
- Step 6: Clean up temp directory
- Include fallback behavior: if catbox upload fails, preserve local zips and return empty URLs with error message

`references/issue-template.md`:
- Header: "Read this file during Phase 6 when creating the GitHub issue."
- Defines the issue title format: `Retro: <API name> — <N> findings, <M> work units`
- Defines the issue body structure as a markdown template:
  - Session stats table (API, spec source, scorecard, verify rate, fix loops, manual edits)
  - Priority summary tables (Do Now, Do Next, Skip — one-line per finding)
  - Work unit summaries (goal, component, acceptance criteria — no file paths for external users)
  - Artifact links section (catbox URLs for manuscripts and CLI source zips)
  - "Generated by `/printing-press-retro`" footer
- Not the full retro — that's attached as a separate artifact

**Patterns to follow:**
- `skills/printing-press/references/secret-protection.md` — deterministic bash commands, not prose
- `skills/printing-press/references/sniff-capture.md` — header format with load condition

**Test scenarios:**
- Happy path: Reference files load correctly when the skill reaches Phase 6
- Edge case: Manuscript with no HAR files — HAR stripping step skips cleanly
- Edge case: No API key in session (exact-value scan layer skips, pattern scan still runs)
- Error path: catbox curl returns non-200 — packaging preserves local zips, returns empty URLs with error

**Verification:**
- All three files exist under `skills/printing-press-retro/references/`
- Each has a header stating when to load it
- Secret scrubbing uses concrete bash commands, not instructional prose
- No references to absolute paths or user-specific directories

---

- [ ] **Unit 2: Create public SKILL.md**

**Goal:** Write the public retro skill SKILL.md that adapts the 6-phase methodology for dual context (in-repo and external), integrates the reference files, and handles the full lifecycle from guard rails through issue creation.

**Requirements:** R1, R6, R7, R8, R9, R10, R11, R12, R14, R15

**Dependencies:** Unit 1 (reference files must exist for inline pointers)

**Files:**
- Create: `skills/printing-press-retro/SKILL.md`

**Approach:**

Structure of the new SKILL.md (~350-400 lines estimated, down from 606 + new output logic, because conditional content is extracted to references):

**Frontmatter:**
```yaml
name: printing-press-retro
description: >
  Run a retrospective after generating a CLI. Identifies systemic improvements
  to the printing press machine — generator templates, Go binary, skill
  instructions, catalog. Creates a GitHub issue with findings and artifacts
  so anyone can fix the machine. Use after any /printing-press run.
  Trigger phrases: "retro", "retrospective", "what went wrong", "improve
  the press", "post-mortem", "lessons learned", "what can we improve",
  "file a retro", "submit findings".
version: 0.1.0
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Agent
  - AskUserQuestion
```

No `min-binary-version` — skill reads manuscripts and runs `gh`/`curl`, not the binary.

**Modified setup contract (path-only, no binary check):**
- Use a slimmed-down version of the `PRESS_SETUP_CONTRACT` that resolves `_scope_dir`, `PRESS_HOME`, `PRESS_MANUSCRIPTS`, `PRESS_LIBRARY` and creates directories — but **omits the binary detection and early-exit guard**. The standard contract exits with `return 1 || exit 1` if the binary isn't found, which would abort for external users who don't have it installed. The retro skill never invokes the binary.
- After path resolution, detect context: `IN_REPO=false; if [ -f "$_scope_dir/cmd/printing-press/main.go" ]; then IN_REPO=true; fi`
- Mark the contract block with `<!-- RETRO_SETUP_START -->` / `<!-- RETRO_SETUP_END -->` comments to distinguish from the standard binary-requiring contract

**Guard rails (after setup, using resolved paths):**
- Check `$PRESS_MANUSCRIPTS` exists and is non-empty (R9)
- If user provided an API name argument, validate for path traversal (no `/`, `\`, `..`), verify resolved path stays under `$PRESS_MANUSCRIPTS`
- If no API name and multiple APIs exist, list them with most recent run dates, ask user to choose (R10)
- If API has multiple runs, default to most recent, allow user to specify run ID (R11)

**Phases 1-4 (analytical methodology):**
- Carry forward from current private skill with minimal changes
- Phase 1 (Gather evidence): uses `$PRESS_MANUSCRIPTS` and `$PRESS_LIBRARY` paths from setup contract
- Phase 2 (Mine the session): Add note: "If running in a fresh conversation without generation history, note this and proceed with manuscript evidence only. Mark session-dependent findings as 'evidence: manuscripts only'."
- Phase 3 (Classify findings): Unchanged — the six questions framework works in both contexts
- Phase 4 (Prioritize): Unchanged

**Phase 5 (Write the retro):**
- Same template as current skill
- Save to `$PRESS_MANUSCRIPTS/<api>/<run-id>/proofs/<stamp>-retro-<api>-pp-cli.md` (R8, always)

**Phase 5.5 (Work units):**
- Conditional on `IN_REPO`:
  - In-repo: resolve target file paths via `find`/`grep` on `$_scope_dir` (current behavior)
  - External: describe components by name (e.g., "Generator templates in `internal/generator/`"), acceptance criteria, and complexity — skip file path resolution

**Phase 6 (Package, upload, and present):**
- Read and apply `references/artifact-packaging.md` — copy, scrub, zip, upload to catbox
- Read and apply `references/issue-template.md` — build the issue body from retro findings
- Check `gh auth status`: if authenticated, create issue via `gh issue create --repo mvanhorn/cli-printing-press --title "..." --body "$(cat <<'EOF' ... EOF)"`. Print full issue URL.
- If `gh` fails (R14): save retro document and zips locally, print catbox URLs if available, tell user to file manually
- If catbox failed (R15): create issue without artifact links, note artifacts couldn't be uploaded, tell user local zips are at `<path>`
- If `gh issue create` fails due to body size: truncate the issue body to summary tables + artifact links, add "Full retro analysis in attached artifacts", retry
- Conditional on `IN_REPO`: also save to `docs/retros/YYYY-MM-DD-<api>-retro.md` (R7)
- Conditional on `IN_REPO`: offer to plan via `/ce:plan` — best-effort, guarded by checking if the compound-engineering plugin is available. If not available, skip silently (this is existing behavior being carried forward, not a new capability)
- External: show issue URL and done

**Cardinal rules (inline, always loaded):**
- Never upload un-scrubbed artifacts
- Never modify source manuscripts or library directories
- Never skip the secrets scrub, even if the pipeline already ran one
- The retro is about the machine, not the CLI

**Patterns to follow:**
- `skills/printing-press-score/SKILL.md` — frontmatter structure, setup contract placement, CLI resolution logic
- `skills/printing-press-publish/SKILL.md` — `gh auth status` checking, `gh pr create` HEREDOC pattern (adapt for `gh issue create`)
- `.claude/skills/printing-press-retro/SKILL.md` — analytical methodology (Phases 1-5.5), classification framework, scorer audit protocol

**Test scenarios:**
- Happy path: External user runs `/printing-press-retro` with one API in manuscripts → retro analysis runs, artifacts uploaded to catbox, issue created on repo, full URL displayed
- Happy path: In-repo user runs retro → same as above plus local save to `docs/retros/` and offer-to-plan
- Edge case: Multiple APIs in manuscripts, no argument → skill lists APIs with dates, asks user to choose
- Edge case: Fresh conversation (no generation history) → Phase 2 notes limited evidence, proceeds with manuscripts only
- Error path: `gh auth status` fails → retro saved locally, catbox URLs printed if available, manual filing instructions
- Error path: catbox upload fails → issue created without artifact links, local zips preserved
- Error path: Both `gh` and catbox fail → retro saved to manuscripts only, all local paths printed
- Edge case: User provides API name with path traversal (`../../etc`) → rejected with clear error
- Edge case: No manuscripts → refuses to run with clear message (R9)

**Verification:**
- Skill discovered as `cli-printing-press:printing-press-retro` in Claude Code skill list
- Reference files loaded conditionally (only during Phase 6)
- Setup contract resolves `PRESS_HOME`/`PRESS_MANUSCRIPTS` correctly
- Guard rails trigger before any analysis work
- In-repo detection works via marker file check

---

- [ ] **Unit 3: Remove private skill and verify**

**Goal:** Delete the private retro skill and verify the public one is discovered correctly.

**Requirements:** R13

**Dependencies:** Unit 2

**Files:**
- Delete: `.claude/skills/printing-press-retro/SKILL.md`

**Approach:**
- Remove `.claude/skills/printing-press-retro/` directory entirely
- Verify `skills/printing-press-retro/SKILL.md` exists with correct frontmatter
- Start a new Claude Code session from the repo to verify `cli-printing-press:printing-press-retro` appears in the skill list
- Verify the old private skill no longer appears
- Check that `.claude/scripts/install-internal-skills.sh` no longer picks up the retro (it iterates `.claude/skills/` — removing the directory is sufficient)
- **Migration note**: Users who previously ran `install-internal-skills.sh` will have a stale copy at `~/.claude/skills/printing-press-retro/`. Both the stale private skill and the new public skill would be visible. Add a note in the skill description or a one-time migration hint that old copies at `~/.claude/skills/printing-press-retro/` should be removed

**Patterns to follow:**
- Verify via the same discovery mechanism other public skills use — check skill list output

**Test expectation: none** — this is a delete + verify step with no behavioral code

**Verification:**
- `.claude/skills/printing-press-retro/` no longer exists
- `skills/printing-press-retro/SKILL.md` exists with valid frontmatter
- `skills/printing-press-retro/references/` contains three reference files
- New Claude Code session shows `cli-printing-press:printing-press-retro` in available skills

## System-Wide Impact

- **Interaction graph**: The retro skill calls `gh issue create` (new pattern in this plugin), `curl` to catbox.moe (new external dependency), and conditionally invokes `/ce:plan` (existing pattern from compound-engineering plugin)
- **Error propagation**: Failures in catbox/gh degrade gracefully to local-only output. The retro analysis is never lost — worst case it's saved to manuscript proofs only.
- **State lifecycle risks**: Scrub-on-copy pattern prevents accidental mutation of user's manuscripts/library. Temp directory cleanup handles partial failures.
- **API surface parity**: No other skill creates GitHub issues — this establishes a new pattern. Future skills that need issue creation should follow this pattern.
- **Unchanged invariants**: The analytical methodology (Phases 1-5.5) is preserved. The setup contract is the standard shared block. The manuscript layout contract is unchanged.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| catbox.moe goes down or changes API | Graceful degradation (R15): issue created without artifacts, local zips preserved. catbox is simple enough to swap for another anonymous upload service if needed. |
| Secrets leak through pattern-matching gaps | Defense in depth: exact-value scan catches session key, pattern scan catches common formats, HAR stripping catches auth headers. Manual review of first few retro issues recommended. |
| Large manuscripts exceed catbox upload limit | catbox supports 200MB uploads. Largest observed manuscript is 268KB. Even with CLI source (~2MB), well within limits. |
| External users don't have `gh` installed | R14 fallback: save locally, print instructions. `gh` is widely installed by Claude Code users since it's recommended for GitHub workflows. |
| Phase 2 quality degrades without conversation history | Documented limitation. Manuscripts provide strong signal for Phases 1/3/4/5. Phase 2 "mine the session" is explicitly marked as best-effort for fresh conversations. |

## Sources & References

- **Origin document:** [docs/brainstorms/2026-04-04-public-retro-skill-requirements.md](docs/brainstorms/2026-04-04-public-retro-skill-requirements.md)
- Current private skill: `.claude/skills/printing-press-retro/SKILL.md`
- Setup contract pattern: `skills/printing-press-score/SKILL.md` (lines 36-73)
- Secret protection: `skills/printing-press/references/secret-protection.md`
- Publish skill gh pattern: `skills/printing-press-publish/SKILL.md`
- Validation-must-not-mutate: `docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md`
- Path traversal protection: `docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md`
- Related issue: [mvanhorn/cli-printing-press#129](https://github.com/mvanhorn/cli-printing-press/issues/129)
