---
title: "refactor: Library directories keyed by API slug instead of CLI name"
type: refactor
status: completed
date: 2026-04-07
origin: docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md
---

# refactor: Library directories keyed by API slug instead of CLI name

## Overview

Change the library directory key from CLI name (`dub-pp-cli/`) to API slug (`dub/`). This aligns the local library with manuscripts (already slug-keyed), simplifies mega MCP discovery, and removes the misleading implication that a directory only contains a CLI binary when it now contains both CLI and MCP binaries.

With zero external users, there is no backward-compat cost. The existing backward-compat code in the mega MCP and library scanner can be removed rather than maintained.

## Problem Frame

The mega MCP plan (PR #147) deferred Phase 1 — the directory restructure — and shipped the mega MCP with backward-compat discovery that checks both `{slug}-pp-cli/` and `{slug}/` layouts. With no external users, this compat code is unnecessary complexity. Renaming now while the user base is 2 internal people avoids accumulating more CLIs under the old naming, making a future migration larger.

## Requirements Trace

- R1. New CLIs land in `~/printing-press/library/{api-slug}/` instead of `~/printing-press/library/{api-slug}-pp-cli/`
- R2. Existing local CLIs are migrated to the new directory names
- R3. The publish pipeline writes to slug-keyed paths in the public library repo
- R4. The publish skill uses slug-keyed branch names, registry paths, and collision detection
- R5. The mega MCP backward-compat discovery code is cleaned up (only check slug path)
- R6. `publish rename` works with slug-keyed `--dir` paths (the outer directory is slug-keyed; `--old-name`/`--new-name` remain CLI-name-suffixed since `RenameCLI` does content replacement)
- R7. Binary names remain unchanged (`dub-pp-cli`, `dub-pp-mcp`) — only the outer directory key changes

## Scope Boundaries

- **In scope:** Generator output path, publish pipeline, publish skill, mega MCP cleanup, local library migration, emboss resolution, library scanner, naming utilities
- **In scope:** Renaming the 5 existing local library directories
- **Out of scope:** Renaming binary names (`dub-pp-cli`, `dub-pp-mcp` stay as-is)
- **Out of scope:** Public library repo migration — that's a separate PR in `mvanhorn/printing-press-library` after the machine changes land. The publish skill changes here will write *new* publishes under slug paths; existing CLIs in the public repo stay until migrated.
- **Out of scope:** Changing the `go.mod` module path inside generated CLIs. The module path is a build-time concern and doesn't need to match the directory name. The publish pipeline's `RewriteModulePath` already handles setting the correct public module path at publish time.
- **Out of scope:** Changing working directory naming during pipeline runs (`WorkingCLIDir` in `internal/pipeline/paths.go`). Working directories are ephemeral and the naming there is irrelevant to the library layout.

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/pipeline.go:16-18` — `DefaultOutputDir` uses `naming.CLI(apiName)` to construct the library path. This is the single choke point for where new CLIs land.
- `internal/cli/root.go:325-338` — After `generate --spec`, the output dir is renamed to `naming.CLI(apiSpec.Name)`. This would undo the `DefaultOutputDir` change if not updated.
- `internal/naming/naming.go` — `CLI()`, `IsCLIDirName()`, `TrimCLISuffix()` all assume `-pp-cli` suffix. `CLI()` stays unchanged (binary naming), but `IsCLIDirName` and `TrimCLISuffix` need to handle both formats.
- `internal/cli/library.go:86-144` — `scanLibrary()` filters on `naming.IsCLIDirName(dirName) || entry.APIName != ""`. The manifest-based path (`APIName != ""`) already handles slug-keyed dirs.
- `internal/cli/emboss.go:182-215` — `resolveEmbossTarget()` tries exact name, then `naming.CLI(target)`. Needs slug-first lookup order.
- `internal/cli/publish.go:276-301` — `newPublishPackageCmd` constructs `outCLIDir` as `filepath.Join(dest, "library", category, cliName)` where `cliName` comes from the manifest's `CLIName` field. This needs to use the API slug from the manifest instead.
- `internal/cli/publish.go:403-428` — `stashExistingCLI` searches by `cliName` across categories. Needs to search by API slug.
- `internal/pipeline/renamecli.go:157-173` — `validateRenameInputs` rejects names that don't pass `IsCLIDirName()`. Needs to also accept slug-keyed names.
- `internal/megamcp/metatools.go:166-185` — `checkUpgradeAvailable` checks both `naming.CLI(slug)` and bare `slug` paths. Can be simplified to slug-only.
- `skills/printing-press-publish/SKILL.md` — References `feat/<cli-name>` branch naming, `library/<category>/<cli-name>` paths, registry.json `name` and `path` fields. All need updating.
- `internal/pipeline/publish.go:200` — `CLIName: naming.CLI(state.APIName)` in manifest. The `CLIName` in the manifest is the *binary* name, not the directory name, so this stays.

### Institutional Learnings

- **Layout contract** (`docs/solutions/best-practices/checkout-scoped-printing-press-output-layout`): Five assumptions shared across runtime, skills, and Go code about the naming contract. All consumers must update in lockstep.
- **Filepath traversal protection** (`docs/solutions/security-issues/filepath-join-traversal-with-user-input`): API slugs used as path segments must be validated. Belt-and-suspenders: reject traversal characters AND verify resolved path is under root.
- **Validation must not mutate source** (`docs/solutions/best-practices/validation-must-not-mutate-source-directory`): The migration step that renames local library directories must be separate from validation.

## Key Technical Decisions

- **Remove backward-compat rather than maintain it.** With 2 internal users and no external consumers, carrying both naming patterns adds test surface and cognitive overhead for no benefit. Migrate existing directories and drop the dual-lookup code.

- **Keep `naming.CLI()` and `naming.MCP()` unchanged.** These produce *binary names* (`dub-pp-cli`, `dub-pp-mcp`), not directory names. The directory key is the API slug; the binary name is a separate concern.

- **Migrate local library in-place.** The 5 existing directories (`cal-com-pp-cli`, `dub-pp-cli`, etc.) get renamed to their slug equivalents. A simple loop in a migration unit handles this.

- **Directory key in publish pipeline is `manifest.APIName`, not `manifest.CLIName`.** The `CLIName` field continues to hold the binary name (`dub-pp-cli`). The directory in the public library becomes `library/<category>/dub/` but the binary inside is still `dub-pp-cli`.

- **`publish rename` keeps CLI-name validation for `--old-name`/`--new-name`.** `RenameCLI` does `strings.ReplaceAll` with the old/new names across file contents — bare slugs like `"dub"` would corrupt any word containing that substring. The `--old-name`/`--new-name` parameters must remain CLI-name-suffixed (`IsCLIDirName`). What changes is the *outer directory*: after slug-keyed directories, the publish skill passes the slug-keyed `--dir` path but still uses CLI-suffixed binary names for `--old-name`/`--new-name`.

## Open Questions

### Resolved During Planning

- **Should backward-compat be kept or removed?** Removed. Zero external users means no migration burden. Simpler codebase.
- **Should `go.mod` module path inside CLIs change?** No. The module path doesn't need to match the directory name. `RewriteModulePath` at publish time already sets the correct public path.
- **Should working dir naming change?** No. Working dirs are ephemeral and don't affect the library layout.
- **What happens to the `CLIName` field in manifests?** It stays as-is (`dub-pp-cli`). It's the binary name, not the directory name. Consumers that need the directory name should use `APIName` instead.

### Deferred to Implementation

- **Public library migration PR.** After machine changes land, a separate PR in `mvanhorn/printing-press-library` renames existing directories and updates `registry.json`. Not in this plan.

## Implementation Units

- [ ] **Unit 1: Change `DefaultOutputDir` and generator rename path**

  **Goal:** New CLIs land in `~/printing-press/library/{api-slug}/` instead of `~/printing-press/library/{api-slug}-pp-cli/`.

  **Requirements:** R1

  **Dependencies:** None

  **Files:**
  - Modify: `internal/pipeline/pipeline.go` (`DefaultOutputDir`)
  - Modify: `internal/cli/root.go` (lines 325-338: change `derivedDir` from `naming.CLI(apiSpec.Name)` to `apiSpec.Name`; line 200: change `--docs` display name from `naming.CLI(parsed.Name)` to `parsed.Name`; line 363: change `--spec` display name from `naming.CLI(apiSpec.Name)` to `apiSpec.Name`)
  - Test: `internal/pipeline/pipeline_test.go`

  **Approach:**
  `DefaultOutputDir` changes from `filepath.Join(PublishedLibraryRoot(), naming.CLI(apiName))` to `filepath.Join(PublishedLibraryRoot(), apiName)`. The `generate --spec` rename path at `root.go:329` changes `derivedDir := naming.CLI(apiSpec.Name)` to `derivedDir := apiSpec.Name`. The surrounding logic (rename-if-different) works the same way.

  **`--docs` code path:** The `--docs` path at `root.go:150` also calls `DefaultOutputDir(parsed.Name)` — it gets the slug-keyed path automatically since it has no post-generation rename logic. No changes needed. However, `root.go:200` prints `naming.CLI(parsed.Name)` as the display name — update this to print `parsed.Name` so the display name matches the actual directory (`"Generated dub at .../library/dub"` instead of `"Generated dub-pp-cli at .../library/dub"`).

  **Not modified:** `naming.CLI()` usage in `generator_test.go`, `openapi/parser_test.go`, and fullrun working dir construction — these use `naming.CLI()` for binary naming, not library directory naming.

  **Patterns to follow:**
  - Existing `DefaultOutputDir` pattern

  **Test scenarios:**
  - Happy path: `DefaultOutputDir("dub")` returns path ending in `/dub` (not `/dub-pp-cli`)
  - Happy path: `DefaultOutputDir("cal-com")` returns path ending in `/cal-com`
  - Edge case: `DefaultOutputDir("steam-web")` returns path ending in `/steam-web` (hyphenated slugs work)

  **Verification:** `go test ./internal/pipeline/...` passes. `go build ./...` succeeds.

- [ ] **Unit 2: Update naming utilities and library scanner**

  **Goal:** `scanLibrary()` discovers slug-keyed directories. Add `IsValidLibraryDirName()` for directory validation.

  **Requirements:** R1, R6

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `internal/naming/naming.go` (add `IsValidLibraryDirName()`)
  - Modify: `internal/cli/library.go` (simplify `scanLibrary` filter)
  - Test: `internal/naming/naming_test.go`
  - Test: `internal/cli/library_test.go` (if exists, or add tests)

  **Approach:**
  Add `IsValidLibraryDirName(name string) bool` that returns true for names that are non-empty, contain no path separators or `..`, don't start with `.`, and either pass `IsCLIDirName()` (legacy) or match the slug grammar `^[a-z0-9][a-z0-9-]*$` (lowercase alphanumeric + hyphens, must start with alphanumeric — this accepts rerun suffixes like `dub-2`). This is Layer 1 (input validation) per the filepath traversal protection solution. Callers that use the validated name in `filepath.Join` must still apply Layer 2: verify the resolved absolute path has prefix `abs(root) + sep` before any filesystem operation. In `scanLibrary`, simplify the filter: a directory is included if it has a valid manifest (`APIName != ""`) or passes `IsValidLibraryDirName()`. This covers both old and new naming during the transition period (between this landing and the local migration running).

  `TrimCLISuffix` already returns the name unchanged for bare slugs (the `default` case at line 45), so no change needed there.

  **Patterns to follow:**
  - `IsCLIDirName()` pattern for the new function
  - `scanLibrary()` manifest-first discovery pattern

  **Test scenarios:**
  - Happy path: `IsValidLibraryDirName("dub")` returns true
  - Happy path: `IsValidLibraryDirName("dub-pp-cli")` returns true (legacy compat during transition)
  - Happy path: `IsValidLibraryDirName("cal-com")` returns true
  - Edge case: `IsValidLibraryDirName("")` returns false
  - Edge case: `IsValidLibraryDirName("../etc")` returns false
  - Edge case: `IsValidLibraryDirName(".DS_Store")` returns false (dotfiles rejected)
  - Edge case: `IsValidLibraryDirName("dub-pp-cli-2")` returns true (legacy rerun suffix)
  - Edge case: `IsValidLibraryDirName("dub-2")` returns true (slug rerun suffix — `ClaimOutputDir` produces these after migration)
  - Happy path: `TrimCLISuffix("dub")` returns `"dub"` unchanged (already works, add test to lock it in)

  **Verification:** `go test ./internal/naming/... ./internal/cli/...` passes.

- [ ] **Unit 3: Update emboss resolution and publish pipeline**

  **Goal:** `emboss` resolves slug-keyed library dirs. `publish package` writes to slug-keyed paths. `publish rename` accepts slug-keyed names.

  **Requirements:** R3, R6

  **Dependencies:** Unit 2

  **Files:**
  - Modify: `internal/cli/emboss.go` (`resolveEmbossTarget` — add `TrimCLISuffix` fallback as 3rd lookup step)
  - Modify: `internal/cli/publish.go` (`newPublishPackageCmd` — use `APIName` for directory key instead of `CLIName`; update `stashExistingCLI` to search by API slug; add `APIName` empty fallback)
  - Modify: `internal/pipeline/renamecli.go` (relax directory-base-mismatch check at line 53 to accept slug-keyed dir bases)
  - Modify: `internal/pipeline/lock.go` (`PromoteWorkingCLI` at line 187 and `LockStatus` at line 133 — change `filepath.Join(PublishedLibraryRoot(), cliName)` to use API slug instead)
  - Test: `internal/pipeline/renamecli_test.go`

  **Approach:**

  **Emboss:** The existing lookup order in `resolveEmbossTarget` is already correct: (1) try bare name at line 198, (2) try `naming.CLI(target)` at line 205 (guarded by `!strings.HasSuffix(target, CurrentCLISuffix)` — so `emboss dub-pp-cli` skips this step since the suffix already matches). No swap needed. The change is adding a 3rd fallback: when both fail, try `naming.TrimCLISuffix(target)` to handle `emboss dub-pp-cli` after migration — the bare lookup finds no `library/dub-pp-cli/`, step 2 is skipped (suffix guard), but `TrimCLISuffix("dub-pp-cli")` → `"dub"` → finds `library/dub/`.

  **Publish package:** At line 277, `cliName` is derived from `vResult.CLIName` (which comes from the manifest's `cli_name` field — e.g., `dub-pp-cli`). For directory construction, use `vResult.APIName` instead. Introduce `dirName := vResult.APIName` for path construction while keeping `cliName` for binary operations (build checks, `oldModPath` at line 333, etc.). If `APIName` is empty (older CLIs generated before the manifest writer was updated), fall back to `naming.TrimCLISuffix(cliName)`. Return an error if both are empty. Update `stashExistingCLI` to search by `dirName` (the API slug) — old-format dirs in the public repo are handled by the separate public library migration PR, not here. Keep `resolveManuscripts(cliName, vResult.APIName)` unchanged — manuscripts are already slug-keyed and the function's second argument (API name) handles that correctly; changing the first argument to the slug would make both args identical and lose backward-compat manuscript lookup by CLI name.

  **Lock pipeline:** `PromoteWorkingCLI` at `lock.go:187` and `LockStatus` at `lock.go:133` construct library paths via `filepath.Join(PublishedLibraryRoot(), cliName)`. Update both to derive the API slug from `cliName` (via `naming.TrimCLISuffix`) for directory lookup. The main skill's `lock promote --cli <api>-pp-cli` continues to pass the CLI name; the lock functions translate to slug-keyed paths internally.

  **Publish rename:** `validateRenameInputs` keeps `IsCLIDirName()` for `--old-name`/`--new-name` — these are binary names used for content replacement, and bare slugs would cause collateral damage via `strings.ReplaceAll`. The only change is that `RenameCLI`'s directory-base-mismatch check at line 53 (`filepath.Base(absDir) != oldCLIName`) must be relaxed: after slug-keyed directories, the dir base is `"dub"` but oldCLIName is `"dub-pp-cli"`. Change this check to accept the base being either `oldCLIName` or `naming.TrimCLISuffix(oldCLIName)`.

  **Patterns to follow:**
  - Existing `resolveEmbossTarget` cascading lookup
  - `stashExistingCLI` search pattern
  - `validateRenameInputs` safety checks

  **Test scenarios:**
  - Happy path: `emboss dub` finds `~/printing-press/library/dub/`
  - Happy path: `emboss dub-pp-cli` falls back and finds `~/printing-press/library/dub/` (via TrimCLISuffix)
  - Happy path: `publish package` stages to `library/<category>/dub/` (not `dub-pp-cli/`)
  - Happy path: `stashExistingCLI` finds and stashes `dub/` dirs when publishing `dub`
  - Happy path: `RenameCLI` works when dir base is `"dub"` (slug) but `--old-name` is `"dub-pp-cli"` (binary name)
  - Happy path: `validateRenameInputs("dub-pp-cli", "dub-alt-pp-cli")` still succeeds
  - Error path: `validateRenameInputs("dub", "dub-alt")` still fails (bare slugs rejected for content replacement safety)
  - Edge case: `publish package` correctly builds validation binary from `cmd/dub-pp-cli/` inside a `dub/`-named directory

  **Verification:** `go test ./internal/cli/... ./internal/pipeline/...` passes.

- [ ] **Unit 4: Migrate existing local library directories**

  **Goal:** Rename the 5 existing local library directories from `{slug}-pp-cli/` to `{slug}/`.

  **Requirements:** R2

  **Dependencies:** Units 1-3 (must land first so newly generated CLIs use slug paths)

  **Files:**
  - Modify: `internal/cli/library.go` (add `migrateLibrary` function)
  - Modify: `internal/cli/library.go` (add `library migrate` subcommand, or wire into existing `library` command)

  **Approach:**
  Add a `printing-press library migrate` subcommand that scans `~/printing-press/library/`, identifies directories matching the old `IsCLIDirName()` pattern, and renames them with `os.Rename`. Before each rename, verify the derived slug target resolves to a path under the library root (Layer 2 containment — `strings.HasPrefix(abs(target), abs(libRoot)+sep)`). Skip if the target slug dir already exists. Print what was renamed.

  **Slug derivation for migration:** Do NOT use `TrimCLISuffix` — it strips the numeric rerun suffix first (`dub-pp-cli-2` → `dub`, not `dub-2`). Instead, use `strings.Replace(name, "-pp-cli", "", 1)` (or a dedicated `MigrateLibraryDirName` function) that removes only the `-pp-cli` infix while preserving rerun suffixes: `dub-pp-cli` → `dub`, `dub-pp-cli-2` → `dub-2`.

  This is a one-time operation for the 2 internal users. Keep it simple — no dry-run mode needed.

  Current directories to migrate:
  - `cal-com-pp-cli/` → `cal-com/`
  - `dub-pp-cli/` → `dub/`
  - `pagliacci-pizza-pp-cli/` → `pagliacci-pizza/`
  - `postman-explore-pp-cli/` → `postman-explore/`
  - `steam-web-pp-cli/` → `steam-web/`
  - `postman-explore-pp-cli.bak-170836/` → skip (not a valid CLI dir)

  **Patterns to follow:**
  - `scanLibrary()` iteration pattern
  - `naming.IsCLIDirName()` for detection

  **Test scenarios:**
  - Happy path: `library migrate` renames `dub-pp-cli/` to `dub/`
  - Happy path: `library migrate` renames `dub-pp-cli-2/` to `dub-2/` (rerun suffix preserved)
  - Happy path: `library migrate` skips `postman-explore-pp-cli.bak-170836/` (not a valid CLI dir)
  - Edge case: `library migrate` skips when target already exists (idempotent)
  - Edge case: `library migrate` with empty library produces no errors
  - Edge case: `library migrate` rejects a derived slug whose resolved path escapes the library root (Layer 2 traversal containment)

  **Verification:** `go test ./internal/cli/...` passes. Running `library migrate` on the actual local library renames all 5 directories.

- [ ] **Unit 5: Clean up mega MCP backward-compat code**

  **Goal:** Remove dual-path discovery in mega MCP. Only check the slug-keyed path.

  **Requirements:** R5

  **Dependencies:** Unit 4 (local migration must complete first — removing old-path detection before migration means the mega MCP won't find existing CLIs)

  **Files:**
  - Modify: `internal/megamcp/metatools.go` (`checkUpgradeAvailable` — remove `naming.CLI(slug)` path check)

  **Approach:**
  At `metatools.go:175`, the `paths` slice currently checks both `naming.CLI(slug)` and bare `slug`. Remove the `naming.CLI(slug)` entry. Only check `filepath.Join(libraryRoot, slug, "cmd", mcpBinary)`.

  **Patterns to follow:**
  - Existing mega MCP path construction

  **Test scenarios:**
  - Happy path: `checkUpgradeAvailable("dub")` finds MCP binary at `library/dub/cmd/dub-pp-mcp/`
  - Edge case: `checkUpgradeAvailable("dub")` returns false when `library/dub-pp-cli/` exists but `library/dub/` does not (old layout no longer recognized — expected after migration)

  **Verification:** `go test ./internal/megamcp/...` passes.

- [ ] **Unit 6: Update publish skill for slug-keyed paths**

  **Goal:** The publish skill uses API slug as the directory key in branch names, registry entries, collision detection, and path construction.

  **Requirements:** R4

  **Dependencies:** Unit 3

  **Files:**
  - Modify: `skills/printing-press-publish/SKILL.md`
  - Modify: `skills/printing-press/SKILL.md` (6+ references to `<api>-pp-cli` as library paths: lines 297, 374, 1007, 1480, 1483, 1501, 1584. Update library path references to use slug format. Update `lock promote --cli` invocations.)

  **Approach:**
  Replace `<cli-name>` with `<api-slug>` as the directory key throughout **both** skills. Specific changes for the publish skill:

  1. **Step 6 (Package):** Module path construction changes from `<module_path_base>/<category>/<cli-name>` to `<module_path_base>/<category>/<api-slug>`
  2. **Step 7 (Collision detection):** Search by API slug instead of CLI name. `ls "$PUBLISH_REPO_DIR/library"/*/"<api-slug>"`. Branch detection uses `feat/<api-slug>` instead of `feat/<cli-name>`.
  3. **Step 8 (Branch/Commit/PR):** Branch name `feat/<api-slug>`. Commit message `feat(<api-slug>): add <api-slug>`. Registry entry `name` and `path` use slug.
  4. **Rename path (Alongside):** Suggestions use slug format: `<slug>-2`, `<slug>-alt` instead of `<slug>-2-pp-cli`.
  5. **Name resolution (Step 2):** Continue to accept both CLI name and slug as input, but resolve to the API slug for all downstream operations.
  6. **Registry.json entry:** `name` becomes the API slug, `path` becomes `library/<category>/<api-slug>`, `manifest_url` becomes `library/<category>/<api-slug>/tools-manifest.json`.

  **Test expectation:** none — skill files are tested via end-to-end publish runs, not unit tests.

  **Verification:** Read through the updated skill and verify all path references use slug format consistently.

- [ ] **Unit 7: Update examples and documentation**

  **Goal:** All help text, examples, and the AGENTS.md glossary reflect slug-keyed library paths.

  **Requirements:** R1

  **Dependencies:** Units 1-6 (can start after Unit 3; only needs all units for final consistency check)

  **Files:**
  - Modify: `internal/cli/publish.go` (example strings in command definitions)
  - Modify: `AGENTS.md` (glossary entries for local library, public library, publish)
  - Modify: `README.md` (layout diagram at line 328, emboss example at line 188)
  - Modify: `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout*.md` (layout contract doc)
  - Modify: `internal/pipeline/contracts_test.go` (hardcoded assertions for `$PRESS_LIBRARY/<api>-pp-cli`, `lock promote --cli <api>-pp-cli`, and `~/printing-press/library/<api>-pp-cli` in skill/README content checks)

  **Approach:**
  Find-and-replace example paths: `~/printing-press/library/notion-pp-cli` → `~/printing-press/library/notion`. Update the layout contract doc to reflect the new naming. Update the AGENTS.md glossary entry for `~/printing-press/` Layout. Update contracts_test.go assertions to match the new slug-keyed path patterns — these tests validate that skills and README contain expected library path formats.

  **Test scenarios:**
  - Happy path: `go test ./internal/pipeline/...` passes with updated contract assertions

  **Verification:** Grep for stale `library/.*-pp-cli` references in docs and help text.

## System-Wide Impact

- **Interaction graph:** The directory rename touches the generator output path, emboss resolution, library scanner, publish pipeline, publish skill, mega MCP discovery, and documentation. All are updated in this plan.
- **Error propagation:** If `DefaultOutputDir` changes but `root.go:329` doesn't, every `--spec` run would rename back to the old format. Unit 1 addresses both together.
- **State lifecycle risks:** Local library directories are renamed in-place. If the migration is interrupted mid-rename, some dirs will be old format and some new. The scanner accepts both formats (via manifest presence), so partial migration is non-fatal.
- **API surface parity:** The `publish rename` command's validation must accept slug-keyed names. The `buildValidationBinary` helper at `publish.go:672-692` builds from `cmd/<cliName>/` — this uses the *binary* name (from manifest `CLIName`), not the directory name, so it works without changes.
- **Unchanged invariants:** Binary names (`dub-pp-cli`, `dub-pp-mcp`) are explicitly not changed. `naming.CLI()`, `naming.MCP()` continue to produce suffixed binary names. `cmd/` subdirectories inside CLIs still use binary names. Only the *outer library directory* key changes.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| `root.go:329` rename undoes `DefaultOutputDir` change | Unit 1 updates both together. Test confirms output dir uses slug. |
| `buildValidationBinary` can't find binary in slug-keyed dir | It uses `cliName` (binary name from manifest) to look in `cmd/<cliName>/`, which is inside the dir regardless of the dir's name. No change needed. |
| `RenameCLI` with bare slugs corrupts file contents | `validateRenameInputs` keeps `IsCLIDirName` requirement for `--old-name`/`--new-name`. Only the `--dir` path becomes slug-keyed. The directory-base-mismatch check is relaxed to accept slug bases. |
| Public library publish creates slug-keyed dirs but old CLIs exist under `-pp-cli` | `stashExistingCLI` searches by API slug. Old-format dirs in the public repo won't be found — republishing an existing CLI creates a duplicate until the public library migration PR lands. Accepted risk for the 2-user window. |
| Mega MCP stops finding CLIs before migration runs | Unit 5 (compat removal) depends on Unit 4 (migration). Migration runs first. |
| Interrupted local migration leaves mixed naming | Scanner accepts both formats. `library migrate` is idempotent. |
| Path traversal via crafted API slug in `filepath.Join` | Layer 1: `IsValidLibraryDirName` rejects traversal characters. Layer 2: callers verify resolved path is under root before filesystem operations. Both layers required per institutional learning. |

## Sources & References

- **Origin document:** [docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md](docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md) (Phase 1)
- Related PRs: #147 (mega MCP), #145 (MCP readiness layer)
- Layout contract: `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout*.md`
- Traversal protection: `docs/solutions/security-issues/filepath-join-traversal-with-user-input*.md`
