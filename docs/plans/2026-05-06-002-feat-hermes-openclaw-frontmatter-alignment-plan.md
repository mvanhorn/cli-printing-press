---
title: feat: Hermes + OpenClaw frontmatter alignment
type: feat
status: active
date: 2026-05-06
supersedes: docs/plans/2026-05-06-001-feat-hermes-agent-compatibility-plan.md
---

# feat: Hermes + OpenClaw frontmatter alignment

## Summary

Make printed CLIs installable via Hermes by adding Hermes-recognized frontmatter fields and READMEs install sections. Strip the OpenClaw `requires.env` and `envVars` blocks (false-positive risk on harvested credentials, no real consumers yet) so both formats stay honest. Add a `Prerequisites: Install the CLI` section to the SKILL.md body so any agent host — not just OpenClaw — knows the underlying binary must exist before commands run. Sweep the existing public library to apply the same shape to already-published CLIs via line-targeted edits.

---

## Problem Frame

The Press's current `skill.md.tmpl` emits OpenClaw-shaped `metadata.openclaw.requires.env` and `envVars` blocks meant to declare which env vars the user must set. For specs without rich `EnvVarSpecs`, those blocks emit harvested credentials (cookies, OAuth-flow tokens) as if the user must enter them — confusing for agents and impossible for users to satisfy. We've avoided the burn so far only because OpenClaw isn't launched.

A separate experience report: a Hermes session loaded a printed-CLI skill and didn't install the underlying CLI binary because the install signal lives only in OpenClaw-shaped metadata that Hermes doesn't read. The skill body has no plain-prose "install the CLI before invoking commands" instruction.

`printing-press-library#266` (merged) made the public library a verbatim mirror of `library/<cat>/<api>/SKILL.md`, so the Press is now the single source of truth for SKILL.md content. The fix surface is here.

---

## Requirements

- R1. Strip `metadata.openclaw.requires.env`, `metadata.openclaw.envVars`, and `metadata.openclaw.primaryEnv` from `skill.md.tmpl`. Keep `requires.bins` and `install`. Both formats avoid declaring user-must-set env vars in v1.
- R2. Add Hermes-recognized top-level fields to `skill.md.tmpl` frontmatter: `version`, `author`, `license`. (Note: `metadata.hermes.tags` is not added — most printed-CLI specs lack a populated catalog category and the field would emit `[other]` for the majority. Defer until catalog enrichment work makes it useful.)
- R3. Move the existing `## CLI Installation` section in `skill.md.tmpl` (currently around line 379) to immediately after the H1 title and rewrite it with direct imperative wording so any host's LLM-driven agent reads it as instruction: "**You must verify the CLI is installed before invoking any command.**" Failure mode explicit ("do not proceed with skill commands until verification succeeds"). Title becomes `## Prerequisites: Install the CLI` to signal precondition rather than reference. No duplication — the existing section is moved, not copied.
- R4. Add `## Install via Hermes` and `## Install via OpenClaw` sections to `readme.md.tmpl`. Hermes section emits both CLI form and chat form in code fences. OpenClaw section emits the operator-copyable instruction in a code fence.
- R5. ~~`version` sources via two-tier resolution from the Press's `printing_press_version`.~~ **Superseded by `fix/skill-md-omit-version`**: the SKILL.md `version:` field is intentionally omitted. The Press version describes the generator, not the CLI; emitting it as `version:` actively misleads consumers about what changed. Hermes lists `version` as optional; OpenClaw isn't launched. CI-time stamping from goreleaser tags in `printing-press-library` is a possible future addition.
- R6. `author` reads from `git config user.name` at template-render time via a new `resolveOwnerNameForRender()` helper (NOT a reuse of `resolveOwnerForNew`, which sanitizes to slug form, prefers `github.user`, and falls back to `"USER"` — wrong shape for a display name). YAML-escaped via the existing `yamlDoubleQuoted` helper. No manifest decouple, no flag, no userconfig package, no authors lookup file.
- R7. `printing-press-library`'s `.github/workflows/generate-skills.yml` triggers on `library/**/SKILL.md` and `library/**/README.md` changes so future hand-edits auto-mirror to `cli-skills/`.
- R8. A one-time sweep tool in `printing-press-library/tools/` line-targets the same shape onto every existing `library/<cat>/<api>/SKILL.md` and `library/<cat>/<api>/README.md`. Idempotent, snapshot-restore safe, follows the `tools/migrate-skill-metadata/main.go` precedent. Sweep covers all 49 CLIs (including the 5 from issue #654 — Phase 3 reprints supersede the sweep's edits for those 5). Sweep derives `<category>` for any URL-templated string from the directory path (`filepath.Base(filepath.Dir(skillMdPath))`), NOT from `.printing-press.json`'s `category` field (which is `omitempty` and present in only ~22% of legacy manifests).
- R9. The 5 synthesis-only CLIs (Linear, HubSpot, Trigger.dev, Slack, Steam Web) are reprinted to the canonical shape, tracked in [#654](https://github.com/mvanhorn/cli-printing-press/issues/654). Independent of this plan; runs in parallel.

---

## Scope Boundaries

- `required_environment_variables` (Hermes) and `requires.env` / `envVars` (OpenClaw) — both removed in v1. Re-add only when there's signal from real users.
- `metadata.hermes.config` — not introduced. The non-secret config field doesn't earn its keep until there's a real consumer.
- `metadata.hermes.required_credential_files`, `platforms`, `related_skills`, `requires_toolsets`, `fallback_for_*` — out of scope.
- Owner identity dual-key model (`owner_name` / `owner_slug` decouple, manifest storage, `--owner-name` flag, golden-harness differentiation, backwards-compat alias). Read git config at render time, that's it.
- Press repo MIT → Apache 2.0 license migration. Separate plan with proper contributor-consent process for Matt Van Horn, Trevin Chow, Cathryn Lavery, Dinakar Sarbada.
- Internal Press skills (`skills/printing-press*/SKILL.md`) Hermes parity. No Hermes consumer for development tooling.
- LLM-generated `required_for` text per CLI.
- `auth.key_url` backfill across legacy specs.
- Catalog enrichment for specs lacking `Category`.

### Deferred to Follow-Up Work

- Re-add env-var hoisting (Hermes `required_environment_variables` and/or OpenClaw `requires.env` + `envVars`) once there's user signal on what the install-time prompt UX should look like.
- Press → Apache 2.0 migration as its own plan.
- `auth.key_url` backfill (improves any future Hermes `help` field quality).
- Catalog enrichment for specs without `Category` (improves any future Hermes `tags` quality).
- Pin `go install` invocations across all install instructions (Prerequisites section, README install sections, MCP register commands) — supply-chain hardening that should be addressed library-wide, not in one section. Per-CLI release-tag plumbing doesn't currently exist; pinning to per-CLI goreleaser tags requires a lookup mechanism that's its own scope.

---

## Context & Research

### Repos involved

This plan touches two repos. Each implementation unit is labeled with `**Repo:** <repo>` so the file paths and dependencies are unambiguous.

| Repo | GitHub | Local path | What lives here |
|---|---|---|---|
| `cli-printing-press` | [`mvanhorn/cli-printing-press`](https://github.com/mvanhorn/cli-printing-press) | `~/Code/cli-printing-press` | The Press generator, templates, this plan, the Press's own tooling. **Default repo for this plan.** |
| `printing-press-library` | [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) | `~/Code/printing-press-library` | Public library of generated CLIs (49 entries), `cli-skills/` mirror, `tools/generate-skills/main.go`, the `generate-skills.yml` workflow. |

All file paths in the plan are repo-relative to the unit's labeled repo.

### Relevant Code

- **Skill template**: `internal/generator/templates/skill.md.tmpl` — currently emits the OpenClaw block including `requires.env` and `envVars`. R1, R2, R3 all touch this file.
- **README template**: `internal/generator/templates/readme.md.tmpl` — `Use with Claude Code` section near line 297 is the existing install-section pattern. R4 slots two new sections alongside.
- **Generator helpers**: `internal/generator/generator.go:240` is where helpers register (`currentYear`, `modulePath`). R5, R6 add `licenseSPDX`, `pressVersion`, plus the `OwnerName` field exposure.
- **Existing escape helper**: `yamlDoubleQuoted` (already registered) — apply to `OwnerName` at template emission.
- **Sweep precedent**: `tools/migrate-skill-metadata/main.go` (formerly in this repo; retired in U7 of this same plan). Line-targeted, idempotent transform pattern. R8's sweep tool followed the same shape; git history is the canonical reference for the precedent.
- **Public library workflow**: `printing-press-library/.github/workflows/generate-skills.yml` — currently triggers on `registry.json`, `library/**/.printing-press.json`, `tools/generate-skills/**`. R7 adds two more paths.
- **Manifest source**: `.printing-press.json` carries `cli_name`, `category`, `printing_press_version`. Sweep + template both read from here. All 49 library CLIs have this file.

### Public library state (as of 2026-05-06)

- 49 CLIs total, all with `library/<cat>/<api>/SKILL.md` and `.printing-press.json`
- 35 with `spec.yaml`; 14 without (synthesis-only or hand-curated origins)
- 5 of the 14 are issue #654's reprint candidates
- PR #266 merged: public library is now a verbatim mirror, no synthesis path

---

## Key Technical Decisions

- **Coexistence**: Hermes top-level fields and OpenClaw nested block live in the same frontmatter. Hermes ignores unknown keys per its docs; one SKILL.md serves both hosts.
- **No env-var declarations in either format**: false-positive on harvested vars is asymmetrically worse than no declaration. The existing `readme.md.tmpl` already branches its install instructions by `auth.Type` (api_key emits "set the env var" with the canonical name; cookie/composed/oauth2 emit "run `auth login` (variant)"; bearer_token similar). That branching IS the conservative classification we want — applied at the install-instruction level instead of the metadata level. The plan must not break this existing logic, and does not add a new generic "Authentication" section that would risk re-introducing the harvested-vs-user-set classification problem at a different surface.
- **Prerequisites section is plain markdown, not host-specific metadata**: any LLM-driven agent reads it regardless of frontmatter format support. Direct imperative ("you must verify") and explicit failure mode ("do not proceed") signal it as instruction.
- **`version` field omitted from SKILL.md frontmatter** (superseded decision; original plan tracked the Press version, removed in `fix/skill-md-omit-version` follow-up). The Press version describes the generator, not the CLI being described, and the printed CLI's release version is independent and not known at template-render time. Hermes lists `version` as optional. Possible future enhancement: library CI stamps `version:` from per-CLI goreleaser tags.
- **`author` reads git config at render time via a new helper**: not a reuse of `resolveOwnerForNew`, which sanitizes to slug form. New helper reads raw `git config user.name`, no sanitization. Empty value falls back to the slug-shaped `Owner` with a loud stderr warning rather than hard-failing — the generator package is reused by tests, mcp-sync, and regen-merge where setting `OwnerName` is awkward, so a hard error was over-strict. The sweep tool's per-CLI authorship mapping overrides this code path entirely; the soft-fallback only fires for fresh prints by users who haven't set `git config user.name`.
- **Library publish path hardcoded `mvanhorn/printing-press-library`** in templates. Single constant; no abstraction layer.
- **Sweep is line-targeted, not template re-rendering**: follows `tools/migrate-skill-metadata/main.go` precedent. Frontmatter region replaced surgically; Prerequisites section inserted via anchor; README sections inserted via anchor. Body content untouched.
- **Sweep covers all 49 CLIs** including the 5 from #654: Phase 3 reprints supersede the sweep's edits for those 5 with full regenerated content. Phases 3 and 4 are independent and can run in parallel.
- **`go install ...@latest` retained for v1**: matches the pattern in every existing library README. Pinning would require per-CLI release-tag plumbing that doesn't exist (the Press version isn't the printed CLI's release version — those are independent). The supply-chain risk applies equally to all install instructions; addressing it for the Prerequisites section without addressing it across the README would be inconsistent. Tracked in Risks; revisit when a broader install-pinning effort is justified.
- **`metadata.hermes.tags` not emitted in v1**: catalog `category` is `omitempty` in `.printing-press.json` and present in only ~22% of legacy manifests. Even with directory-path derivation, only ~14 of 49 CLIs have a non-`other` catalog category. Shipping `tags: [other]` for the majority is worse than no tags. Defer until catalog enrichment makes the field meaningful.

---

## Implementation Units

### U1. Update `skill.md.tmpl` frontmatter

**Repo:** `cli-printing-press`

**Goal:** Strip OpenClaw `requires.env`, `envVars`, and `primaryEnv`; add Hermes top-level fields (`version`, `author`, `license`).

**Requirements:** R1, R2, R5, R6

**Dependencies:** U4 (helpers must exist first)

**Files:**
- Modify: `internal/generator/templates/skill.md.tmpl`
- Test: `internal/generator/skill_test.go`
- Update: `testdata/golden/cases/generate-golden-*` (4 fixtures)

**Approach:**
- Remove the `requires.env` line, the entire `envVars:` block, and any `primaryEnv:` line (legacy synthesized CLIs may have it; the live template doesn't emit it but the canonical post-strip shape needs to be unambiguous about absence). Lines 9-44 of the current template are the relevant region.
- Add top-level `author: "{{yamlDoubleQuoted .OwnerName}}"`, `license: "Apache-2.0"` after the existing `description:` field. License is a literal string, no helper needed. (Original plan also added `version: "{{pressVersion}}"`; that decision was superseded — see Key Technical Decisions and `fix/skill-md-omit-version` follow-up.)
- Keep `metadata.openclaw.requires.bins` and `metadata.openclaw.install`.

**Test scenarios:**
- Happy path: rendered frontmatter has `version`, `author`, `license` populated correctly for an api_key CLI (Shopify-shape); no `requires.env`, no `envVars`, no `primaryEnv` anywhere.
- Happy path: rendered frontmatter for a no-auth CLI (Wikipedia-shape) emits Hermes fields, no env-var blocks of any kind.
- Edge case: `OwnerName` containing YAML special characters (colon, quote) survives `yamlDoubleQuoted` round-trip.
- Edge case: `OwnerName` empty (no git config) falls back to the slug-shaped `Owner` and emits a loud stderr warning. Generation does not fail.
- Edge case: regenerating a CLI that previously had `primaryEnv` in its template output produces output without `primaryEnv`.
- Integration: rendered frontmatter parses as valid YAML via `gopkg.in/yaml.v3`.

**Verification:**
- `internal/generator/skill_test.go` passes.
- `scripts/golden.sh verify` passes after intentional fixture updates.

---

### U2. Move and rewrite `## CLI Installation` as `## Prerequisites: Install the CLI`

**Repo:** `cli-printing-press`

**Goal:** The existing `## CLI Installation` section in `skill.md.tmpl` (around line 379) is too far down the body — agents read top-down and decide what command to run before reaching it. Move it to immediately after the H1 and rewrite with imperative wording so any agent host's LLM reads it as a precondition, not a reference.

**Requirements:** R3

**Dependencies:** None (independent of U1)

**Files:**
- Modify: `internal/generator/templates/skill.md.tmpl`
- Test: `internal/generator/skill_test.go`

**Approach:**
- Locate the existing `## CLI Installation` section (currently around line 379) and remove it from its current position.
- Insert it immediately after the H1 (`# {{.ProseName}} — Printing Press CLI`), before the value-prop paragraph.
- Rename heading to `## Prerequisites: Install the CLI` so it signals a precondition.
- Rewrite the prose with direct imperative language. Use bold for the must-verify clause so the LLM-driven agent doesn't skim past. No duplication — there is one section, in one place, with stronger wording:

```markdown
## Prerequisites: Install the CLI

This skill drives the `{{.Name}}-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

​```bash
go install github.com/mvanhorn/printing-press-library/library/{{if .Category}}{{.Category}}{{else}}other{{end}}/{{.Name}}/cmd/{{.Name}}-pp-cli@latest
​```

Verify: `{{.Name}}-pp-cli --version`

If `--version` reports "command not found" after install, ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`. Do not proceed with skill commands until verification succeeds.
```

**Test scenarios:**
- Happy path: rendered SKILL.md contains the section after the H1 and before the value-prop paragraph.
- Happy path: `go install` URL substitutes `{{.Name}}` and `{{.Category}}` correctly.
- Edge case: empty `Category` falls back to `other` in the install URL.

**Verification:**
- Tests pass; visual inspection of one rendered fixture confirms placement.

---

### U3. Add `Install via Hermes` and `Install via OpenClaw` sections to `readme.md.tmpl`

**Repo:** `cli-printing-press`

**Goal:** Two new install sections in the README so users (not agents) have copy-pasteable install instructions for the two new agent hosts.

**Requirements:** R4

**Dependencies:** None

**Files:**
- Modify: `internal/generator/templates/readme.md.tmpl`
- Test: `internal/generator/readme_test.go`

**Approach:**
- Insert after the existing `Use with Claude Desktop` section. Place a stable anchor comment (e.g., `<!-- pp-hermes-install-anchor -->`) immediately before the new sections so the sweep tool (U7) can locate the insertion point idempotently in legacy READMEs that were generated before this change.
- Both Hermes commands go in `bash` code fences. OpenClaw goes in a plain code fence (no language tag — it's prose for the agent to read, not a shell command).

```markdown
<!-- pp-hermes-install-anchor -->
## Install via Hermes

From the Hermes CLI:

​```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-{{.Name}} --force
​```

Inside a Hermes chat session:

​```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-{{.Name}} --force
​```

## Install via OpenClaw

Tell your OpenClaw agent (copy this):

​```
Install the pp-{{.Name}} skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-{{.Name}}. The skill defines how its required CLI can be installed.
​```
```

**Test scenarios:**
- Happy path: rendered README contains both new section headings and the anchor comment.
- Happy path: install URLs always say `mvanhorn/...` regardless of CLI's owner — catches accidentally templating the publisher.
- Integration: golden fixtures update for all four `generate-golden-*` cases.

**Verification:**
- Tests pass; `scripts/golden.sh verify` passes after fixture updates.

---

### U4. Plumbing: `OwnerName` field

**Repo:** `cli-printing-press`

**Goal:** Register the `OwnerName` field that U1 consumes. License is a literal `"Apache-2.0"` in the template, no helper needed (zero variability). (Original plan also added a `pressVersion` template helper; the `version:` field it served was removed in `fix/skill-md-omit-version` follow-up, taking the helper with it.)

**Requirements:** R5, R6

**Dependencies:** None — lands first or alongside U1.

**Files:**
- Modify: `internal/generator/generator.go` (register `pressVersion` helper)
- Modify: `internal/spec/spec.go` (add `OwnerName` field on `APISpec`)
- Modify: `internal/generator/plan_generate.go` (add new `resolveOwnerNameForRender()` — does NOT reuse `resolveOwnerForNew`)
- Test: `internal/generator/generator_test.go`, `internal/spec/spec_test.go`

**Approach:**
- `pressVersion` helper: two-tier resolution.
  1. If `<outputDir>/.printing-press.json` exists, read `printing_press_version` from it (regen / sweep path: preserves the version that produced the current SKILL.md).
  2. Otherwise (fresh-print path: manifest hasn't been written yet — that happens during publish, after templates render), fall back to `version.Version` (the Press binary's own version constant).
  - Mirrors the existing `resolveOwnerForExisting` / `resolveOwnerForNew` split shape. Reading the manifest must happen via a local helper to avoid the existing `pipeline → generator` import direction.
- `OwnerName`: new helper `resolveOwnerNameForRender()` in `plan_generate.go`.
  - Reads raw `git config user.name`.
  - Does NOT fall back to `github.user`, does NOT sanitize via `sanitizeOwner` (which would mangle "Trevin Chow" → "trevin-chow"), does NOT default to `"USER"`.
  - If empty: `Generate()` falls back to the slug-shaped `Owner` and emits a stderr warning. Soft-fallback (not a hard failure) so the generator package stays reusable by tests, mcp-sync, and regen-merge without forcing them to plumb `OwnerName`.
  - Exposed as `.OwnerName` field on the spec (set during resolution, before template render).
- `OwnerSlug` plumbing stays unchanged — the existing `Owner` field continues to drive copyright headers and module paths via the existing `resolveOwnerForNew`.

**Test scenarios:**
- Happy path (fresh print): `pressVersion` falls back to `version.Version` when `.printing-press.json` is absent.
- Happy path (regen): `pressVersion` reads `printing_press_version` from existing `.printing-press.json`.
- Happy path: `OwnerName` populated from `git config user.name` flows to `.OwnerName` in the template context.
- Edge case: `OwnerName` empty → generation returns clear error mentioning git config.
- Edge case: `OwnerName` containing spaces ("Trevin Chow") survives unchanged into the template (no sanitization).
- Edge case: `OwnerName` and `OwnerSlug` co-exist with diverging values ("Trevin Chow" vs "trevin-chow"); each lands in its correct surface (display name in author / copyright header, slug in module path).

**Verification:**
- Tests pass.

---

### U5. Workflow trigger fix in `printing-press-library`

**Repo:** `printing-press-library` (cross-repo from this plan's home)

**Goal:** Add `library/**/SKILL.md` and `library/**/README.md` to `.github/workflows/generate-skills.yml` triggers so future hand-edits to library content auto-mirror to `cli-skills/`.

**Requirements:** R7

**Dependencies:** None — independent, lands in the public library repo.

**Files:**
- Modify: `.github/workflows/generate-skills.yml`

**Approach:**
- Add the two new paths under `on.push.paths`. One PR, two-line change.

**Test scenarios:**
- Test expectation: none — workflow change is verified by inspection plus a one-off test commit after merge.

**Verification:**
- After merge, a no-op commit touching one library SKILL.md triggers the workflow and produces a no-op `cli-skills/` diff.

---

### U6. One-time sweep tool in `printing-press-library/tools/`

**Repo:** `printing-press-library` (cross-repo from this plan's home)

**Goal:** Walk all 49 library entries, line-target-edit `SKILL.md` (frontmatter + Prerequisites section) and `README.md` (install sections) in place. Idempotent, snapshot-restore safe.

**Requirements:** R8

**Dependencies:** U1, U2, U3 (so the canonical shape the sweep applies is defined — these live in `cli-printing-press` and must merge before this unit runs). U5 helpful but not blocking — the workflow is the auto-mirror path; if not landed yet, the sweep PR can manually run `tools/generate-skills/main.go` once at the end.

**Files:**
- Create: `tools/sweep-frontmatter/main.go`
- Create: `tools/sweep-frontmatter/main_test.go`
- The sweep PR also commits the patched `library/**/SKILL.md` and `library/**/README.md` files.

**Approach:**
- Walk `library/*/*/`. For each:
  1. Read `.printing-press.json` for `cli_name` and `printing_press_version`. Derive `<category>` from the directory path (`filepath.Base(filepath.Dir(skillMdPath))`), NOT from the manifest's `category` field — only ~22% of legacy manifests have it populated, but every CLI lives at `library/<cat>/<api>/` so the path is authoritative.
  2. Read existing `SKILL.md`. Strip OpenClaw `requires.env`, `envVars` block, and any `primaryEnv` line — handle all four legacy shapes: `env: []` (empty inline), `env: ["FOO"]` (inline with value), `env:` followed by block-style `- FOO` continuation lines, and absent. Replace the frontmatter region (between the leading `---` and closing `---`) with the canonical-shape frontmatter built from the manifest data plus operator's `git config user.name` for `author:`. Move the existing `## CLI Installation` section (if present) to immediately after the H1 and rename to `## Prerequisites: Install the CLI` with imperative wording. **Idempotency check for SKILL.md**: presence of the literal heading `## Prerequisites: Install the CLI` near the top of the body — if found, skip the move/rewrite step (the sweep already ran for this file).
  3. Read existing `README.md`. Insert `## Install via Hermes` and `## Install via OpenClaw` sections at the anchor `<!-- pp-hermes-install-anchor -->`. For legacy READMEs without the anchor, fall back: insert after the last `## Use with Claude Desktop`, or `## Use with Claude Code`, or `## Install`, in that order. If none match, append at end of file. **Idempotency check for README.md**: presence of the anchor comment OR the literal heading `## Install via Hermes` — either signals the sweep already ran.
  4. Snapshot all touched files first; restore on any per-CLI error.
- Sweep is line-targeted (matching the `tools/migrate-skill-metadata/main.go` precedent), not yaml-round-trip, so existing comments and formatting in non-frontmatter content are byte-preserved.

**Test scenarios:**
- Happy path: sweep produces canonical-shape SKILL.md and README for an api_key CLI fixture (e.g., Shopify-shape).
- Happy path: sweep is idempotent — running twice produces zero textual diff on the second run, verified via `cmp -s`.
- Edge case: legacy README without the anchor falls back to the last `## Use with Claude Desktop`; absence of all named fallbacks appends at EOF.
- Edge case: legacy SKILL.md without the H1 yet — sweep skips the Prerequisites insertion with a warning rather than corrupting structure.
- Edge case: per-CLI write failure restores all touched files for that CLI from snapshot.
- Edge case: synthesis-only CLI (no spec.yaml) is processed identically — sweep doesn't read spec.yaml.
- Integration: full sweep against a fixture library tree produces files that re-parse cleanly through yaml.v3.

**Verification:**
- Sweep tests pass.
- After running against the live library: every `library/<cat>/<api>/SKILL.md` parses cleanly, has the new frontmatter shape, has the Prerequisites section. Every `README.md` has the two new install sections.
- After workflow runs (Phase 1b's trigger fix), `cli-skills/pp-<api>/SKILL.md` is byte-identical to its library counterpart for every CLI.

---

### U7. Retire `tools/migrate-skill-metadata/main.go`

**Repo:** `cli-printing-press`

**Goal:** Delete the dormant migrator. It was a one-shot historical conversion (legacy `metadata: '{...JSON...}'` string → nested YAML) that long since ran across the library. Its emission shape has drifted from the current template (still emits `primaryEnv`, `bins`-before-`env` ordering, no `envVars` block). After U1 lands, the drift is wider; the file is a foot-gun if anyone ever re-runs it.

**Requirements:** none load-bearing; cleanup hygiene that pairs with U1's frontmatter-shape changes.

**Dependencies:** U1.

**Files:**
- Delete: `tools/migrate-skill-metadata/main.go`
- Delete: `tools/migrate-skill-metadata/main_test.go`

**Approach:**
- Delete both files.
- Verify no internal imports reference the package (`grep -r "migrate-skill-metadata" .` — should only match removed files and possibly historical commit references in docs, which are fine).

**Test scenarios:**
- No tests needed; deletion is mechanical.
- If updating: existing tests adjusted to the new shape.

**Verification:**
- `go build ./...` and `go test ./...` pass.

---

## System-Wide Impact

- **Interaction graph:** No runtime callbacks change. Generator templates gain content; emission paths unchanged.
- **Error propagation:** Empty `OwnerName` triggers a stderr warning + slug-shaped fallback (loud but non-fatal). Sweep rolls back per-CLI file sets via snapshot-restore on failure.
- **State lifecycle:** The four-file mutation per CLI in U6 is the highest-risk path. Snapshot-restore + idempotency test cover partial-failure and re-run cases.
- **API surface parity:** SKILL.md and `cli-skills/pp-<api>/SKILL.md` stay byte-identical via the workflow mirror (after U5 lands the trigger fix).
- **Unchanged invariants:**
  - Copyright header format (`// Copyright YYYY <slug>.`) — unchanged. `OwnerSlug` continues to drive headers; `OwnerName` only flows into prose surfaces.
  - MCPB `manifest.json` author/license fields — unchanged.
  - OpenAPI parser, auth-classification logic, `IsRequestCredential()` — unchanged.
  - Press repo's MIT license — unchanged (migration deferred).

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Sweep clobbers hand-edits in non-frontmatter content of legacy SKILL.md / README | Line-targeted edits; body and non-anchored README content byte-preserved. Snapshot-restore on failure. Idempotency test catches regressions. |
| Empty `git config user.name` during sweep blocks the entire run | Sweep checks once at start, fails fast with clear error. Operator sets git config and re-runs. |
| Anchor-based README insertion lands in the wrong place for legacy READMEs without any named install section | Documented fallback chain (Use with Claude Desktop → Use with Claude Code → Install → EOF). Sweep's dry-run prints which fallback fires per CLI so operator reviews before merging the sweep PR. |
| Workflow trigger fix not landed before the sweep | Sweep PR can manually run `tools/generate-skills/main.go` once at end of the sweep run, committing the cli-skills updates alongside. Or land U5 first (recommended). |
| Supply-chain risk: `go install ...@latest` in agent-executable Prerequisites prose | Accepted-for-v1. Same risk applies to all existing READMEs; addressing it requires per-CLI release-tag plumbing that's out of scope. Tracked in Deferred. Mitigated by Go module proxy + checksum DB once a version has been previously fetched. |

---

## Phased Delivery

```
Phase 1 (DONE) — printing-press-library#266 merged: verbatim mirror live
        │
        ▼
Phase 2 — Press changes (cli-printing-press): U1, U2, U3, U4, U7
        │  + small cross-repo prep PR: workflow trigger fix (U5) in
        │    printing-press-library, lands before or alongside Phase 2
        ▼
Phase 3 — issue #654: 5 reprints (cross-repo, parallel with Phase 4, supersedes sweep edits for those 5)
        │
        ▼
Phase 4 — sweep PR (printing-press-library): U6
```

- **Workflow trigger fix (U5)** is a small standalone PR that lands before or alongside Phase 2. Folded into the prep work — not a separate phase since it's two YAML lines and unblocks nothing on its own.
- **Phase 2** lands the canonical shape in the Press generator. New prints from this point use it.
- **Phase 3** is tracked in [#654](https://github.com/mvanhorn/cli-printing-press/issues/654), runs in parallel with Phase 4. Independent.
- **Phase 4** sweeps existing library entries to the canonical shape via line-targeted edits. Includes all 49 CLIs (Phase 3 reprints later supersede for those 5).

Each phase ships as its own PR; nothing batched.

---

## Sources & References

- Hermes documentation: `https://hermes-agent.nousresearch.com/docs/developer-guide/creating-skills`
- Public library verbatim-mirror PR: [`mvanhorn/printing-press-library#266`](https://github.com/mvanhorn/printing-press-library/pull/266) (merged)
- Phase 3 reprint tracking: [`mvanhorn/cli-printing-press#654`](https://github.com/mvanhorn/cli-printing-press/issues/654)
- Sweep precedent (line-targeted edits): `tools/migrate-skill-metadata/main.go` (retired in U7 of this plan; see git history for the source)
- Round-2 superseded plan: `docs/plans/2026-05-06-001-feat-hermes-agent-compatibility-plan.md` (status: superseded)
