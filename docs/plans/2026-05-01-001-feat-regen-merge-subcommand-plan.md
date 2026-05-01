---
title: "feat: Add printing-press regen-merge subcommand"
type: feat
status: active
date: 2026-05-01
---

# feat: Add printing-press regen-merge subcommand

## Summary

Add a new subcommand `printing-press regen-merge <cli-dir> --fresh <fresh-dir>` that classifies each Go file in a published library CLI by comparing its top-level decl-set against a fresh-generated tree, applies the safe overwrites, restores lost AddCommand registrations, merges go.mod, and surfaces hand-edited templated files for human review. The implementation mirrors `internal/pipeline/mcpsync/sync.go`'s tree-reconcile shape, reuses `github.com/dave/dst` for AST manipulation (already a direct dep, used in `internal/patch/ast_inject.go`), and reuses `golang.org/x/mod/modfile` for go.mod surgery. Stage-and-swap-with-recovery transactional model (write to sibling tempdir, two-step rename with bak preserved until commit) addresses the mid-apply failure recovery gap.

---

## Problem Frame

The `printing-press-library` repo holds 36 published CLIs, most generated months ago. Recent generator improvements (PR #449, #453, #457, #459) want absorbing into those CLIs. A blanket `rsync` of new templated files into published CLIs is unsafe — the postman-explore pilot landed in 14 minutes only because a human caught hand-editing collisions in real time, and the ebay attempt revealed `internal/cli/auth.go` had 402 lines of hand-written OAuth/PKCE code added post-generation that a naive sweep would silently delete. Per-CLI manual diff-review costs ~30-90 minutes; at 17 sweep-doable CLIs that's 8-25 hours of human attention.

A merge tool that automatically classifies each templated-file diff into "safe to apply" vs "human must review" would amortize the diff-review cost across CLIs and let the human focus only on genuinely-divergent files. Target: ~5-15 minutes per CLI.

---

## Requirements

- R1. Classify every `.go` file under `<cli-dir>/internal/` and `<cli-dir>/cmd/` into one of six verdicts: NOVEL, NEW-TEMPLATE-EMISSION, TEMPLATED-CLEAN, TEMPLATED-WITH-ADDITIONS, PUBLISHED-ONLY-TEMPLATED, NOVEL-COLLISION
- R2. For TEMPLATED-CLEAN files, overwrite published with fresh
- R3. For NEW-TEMPLATE-EMISSION files, copy fresh into published
- R4. For NOVEL, TEMPLATED-WITH-ADDITIONS, and NOVEL-COLLISION files, leave published untouched and report the divergence
- R5. For PUBLISHED-ONLY-TEMPLATED files, surface as "stale template emission" so the human can decide to delete
- R6. Restore lost `AddCommand` registrations in any cobra command host file (`root.go` and resource-parent files like `category.go`) after templated overwrite — the predicate must match `Sel.Name == "AddCommand"` against any cobra-typed receiver, not just `rootCmd`
- R7. Merge `go.mod` preserving the published CLI's `module` path while taking the fresh tree's `require`/`replace`/`go` directives, with local-path `replace` directives from published winning over fresh
- R8. Run `RewriteModulePath` over the merged tree BEFORE the go.mod merge writes the published module path (the rewrite reads the current go.mod's module line to find oldPath)
- R9. Default mode is `--dry-run` (classify and report, no writes); `--apply` performs the merge
- R10. `--json` produces a stable machine-readable schema, identical shape in both modes (an `applied` boolean per-file distinguishes them)
- R11. Apply mode is transactional with named recovery state: stage to a sibling tempdir, two-step rename with `<cli-dir>.bak-<ts>` preserved as a recovery anchor; on failure of the second rename, attempt to restore the bak; only remove bak after both renames succeed
- R12. Reject `<cli-dir>` arguments that contain path-traversal sequences (`..` segments) or fail prefix-containment check against CWD; allow override via `--force`
- R13. Reject `--apply` against a non-clean git tree at `<cli-dir>` unless `--force` is set; uncommitted edits to TEMPLATED-CLEAN files would be silently destroyed otherwise
- R14. Skip non-Go non-config files (compiled binaries, build artifacts) without error
- R15. Document supported OS matrix (macOS, Linux); Windows is not supported (rename semantics differ)

---

## Scope Boundaries

- Tool does NOT run `printing-press generate` itself — caller produces the fresh tree and passes it via `--fresh`
- Tool does NOT run `go build`, `go test`, `dogfood`, `verify`, or any behavioral check — caller does these after merge
- Tool does NOT auto-fix novel-command signature drift (e.g., `resolveRead(c, ...)` → `resolveRead(ctx, c, ...)`); those cases surface as compile failures the human resolves. The referent-existence check (R6 implementation) catches name drift only, not signature drift
- Tool does NOT regenerate `manifest.json`, `tools-manifest.json`, or `.printing-press.json` — those are mcp-sync's territory
- Tool does NOT render pretty per-file diffs — humans run `git diff` for review
- Tool does NOT handle GENERATE-FAIL or NO-SPEC CLIs (different bucket; full reprint via `/printing-press` skill)
- Tool does NOT auto-detect or auto-fix the inverse misclassification case where a generator legitimately removes a templated decl in a new version (older published file would be flagged TEMPLATED-WITH-ADDITIONS for the now-removed decl). This is documented as a known limitation; the user inspects via `git diff`

### Deferred to Follow-Up Work

- Custom registration patterns (e.g., a CLI author who wrote `registerNovelCommands(rootCmd)` instead of inline `AddCommand`): flagged as TEMPLATED-WITH-ADDITIONS rather than auto-restored. Adding pattern detection is a future iteration.
- Symbolic link handling beyond skip-with-warning: defer until a real CLI hits this case.
- Per-CLI ignore lists for known-stale templated files: defer until cumulative sweep results show the same false positive across 3+ CLIs.
- `--fix-removed-decls` allowlist for the "template removed a decl" inverse case: defer until evidence shows it's a recurring false positive in real sweep runs.
- Windows support: cross-process file holding and rename semantics differ enough to warrant separate testing/docs.

---

## Context & Research

### Relevant Code and Patterns

- `internal/pipeline/mcpsync/sync.go` — canonical tree-reconcile precedent. `Sync(cliDir, opts)` walks fresh tmpdir, classifies each file by `// Generated by CLI Printing Press` marker. Note: `writeFileAtomic` (lines 666-675) is **unexported** — copy the 10-line helper into the new package; do not attempt to import it.
- `internal/patch/ast_inject.go` — direct prior art for AddCommand injection using `github.com/dave/dst`. Note: `addCommands`, `appendExecuteStatementsAfterLast`, and `isRootAddCommand` are all **unexported** — copy the patterns, do not import. Also note: `isRootAddCommand` (lines 366-380) checks `id.Name == "rootCmd"` literally; the new tool must implement a generalized predicate matching `Sel.Name == "AddCommand"` against any receiver, since R6 requires restoration on resource-parent files where the receiver is `cmd`, not `rootCmd`.
- `internal/pipeline/modulepath.go::RewriteModulePath` — reads the current go.mod's `module` line to find oldPath, errors if oldPath isn't present. Critical ordering implication: this must run BEFORE the go.mod merge that writes the published module path; otherwise the rewrite has no oldPath to match.
- `internal/narrativecheck/narrativecheck.go` — package shape precedent: exported `Validate(ctx, ...)` entry point + exported `Report`/`Result`/`Section` types, helpers unexported, ~200 LOC core file.
- `internal/cli/mcp_sync.go` — closest cobra subcommand analogue. `Args: cobra.ExactArgs(1)`, `<cli-dir>` argument, `--force` flag, `&ExitError{Code: ExitPublishError, ...}` on write failures. Mirror this shape.
- `internal/cli/exitcodes.go` — `ExitInputError = 1`, `ExitPublishError = 5`, etc. Use `&ExitError{Code: ExitInputError}` for path validation, `ExitPublishError` for write failures.

### Institutional Learnings

- `docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md` — verification (`go mod tidy`, `go build`) must not pollute the source tree. We don't run those steps, so this is preventive: keep `--apply` strictly to file writes; if a future flag adds `--build`, follow snapshot-compare-restore.
- `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md` — stage merge work under a tempdir, swap into final location atomically. Reinforces R11.
- `docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md` — reject `/`, `\`, `..` at the input boundary AND verify `filepath.Abs(resolved)` has prefix `filepath.Abs(root) + string(filepath.Separator)`. Reinforces R12.

### External References

None. Local patterns cover all required primitives (AST via std-lib + dave/dst, go.mod via modfile, file walks via mcpsync).

---

## Key Technical Decisions

- **Package location: `internal/pipeline/regenmerge/regenmerge.go`** (not `internal/regenmerge/`). Mirrors `internal/pipeline/mcpsync/`. Conceptually a pipeline-adjacent reconciliation, like mcp-sync.

- **AST library: dave/dst for read-write, std-lib `go/parser` for read-only.** dave/dst already a direct dep (`go.mod:6`), preserves comments, used in `internal/patch/ast_inject.go`. Use it for AddCommand injection and any code that emits modified Go. Use std-lib `go/parser` + `go/ast` for read-only decl-set extraction (lighter, no decoration overhead).

- **Helpers from prior art are copied, not imported.** `writeFileAtomic` from mcpsync, AddCommand-detection patterns from ast_inject.go, are unexported. Copy ~30 LOC total into `regenmerge` rather than waiting on a refactor that exports them. Attribution comments cite the source.

- **Generalized AddCommand predicate.** Unlike `internal/patch/ast_inject.go::isRootAddCommand` (hardcoded to `rootCmd`), the new tool's predicate matches `Sel.Name == "AddCommand"` against any cobra-typed receiver. This is necessary for resource-parent files where calls take the form `cmd.AddCommand(newCategoryLandscapeCmd(flags))`.

- **Templated-vs-novel detection uses logical OR.** A file counts as templated when EITHER (a) its line-2 carries `// Generated by CLI Printing Press`, OR (b) its top-level decl-set is a strict subset of the fresh-generated equivalent file's decl-set. Resolves the older-CLI case where files pre-date the marker convention. When neither signal fires for a file present in both trees, decide between NOVEL-COLLISION (decl-sets disjoint) and TEMPLATED-WITH-ADDITIONS (decl-sets intersect with extras on the published side).

- **Six classification verdicts.**
  - NOVEL (file only in published, no marker, no decl-subset evidence)
  - NEW-TEMPLATE-EMISSION (file only in fresh)
  - TEMPLATED-CLEAN (file in both, marker-OR-decl-subset rule applies, published ⊆ fresh)
  - TEMPLATED-WITH-ADDITIONS (file in both, published has decls fresh doesn't, intersection non-empty)
  - PUBLISHED-ONLY-TEMPLATED (file only in published, marker present OR was-templated heuristic; fresh dropped it)
  - NOVEL-COLLISION (file in both, neither has marker, decl-sets disjoint — coincidental path collision; safe behavior is preserve published)

- **Cross-file decl search before flagging additions.** Before classifying as TEMPLATED-WITH-ADDITIONS, check whether the "missing" decl exists anywhere else in the fresh tree (generator may have moved it to a new file). Reduces the dominant false positive from "helper moved" cases.

- **Stage-and-swap-with-recovery transactional apply.** `--apply` writes everything to a sibling tempdir (`<cli-dir>.regen-merge-<timestamp>/`). On finalization:
  1. `Rename(<cli-dir>, <cli-dir>.bak-<ts>)`
  2. `Rename(tempdir, <cli-dir>)`
  3. `RemoveAll(<cli-dir>.bak-<ts>)`
  
  On failure between steps 1 and 2, attempt `Rename(<cli-dir>.bak-<ts>, <cli-dir>)` to recover. On any failure between steps 2 and 3, the new tree is in place and the bak is left for forensic inspection (logged with absolute path in error output). The bak path is always reported so the user can recover manually if both renames fail.

- **`--apply` requires a clean git tree by default.** Uncommitted edits to TEMPLATED-CLEAN files would be silently destroyed by the merge. Run `git status --porcelain <cli-dir>` as a precondition; refuse with `ExitInputError` if non-empty unless `--force` is passed. Document this in `--help`.

- **AddCommand restoration validates referent existence.** Before injecting `parent.AddCommand(newX(...))` into the staged tempdir, confirm `newX` is a top-level decl somewhere in the staged tempdir's `internal/cli/` package. If not, skip the injection and surface a warning. Note: this catches name drift only, not signature drift; mismatched-arity calls will silently inject and produce compile failures the user resolves. Documented limitation.

- **go.mod merge: published `module` line + fresh `go`/`require` + smart `replace` union.** Dependencies come from fresh (latest template's pinned versions). Module path stays from published (monorepo path). For `replace` directives:
  - **Local-path replaces (target starts with `.` or `/`) from published always win.** These are the monorepo's local forks that fresh would never have.
  - **Version-replaces** (target is a module path with version) use fresh.
  - Union otherwise.
  
  `exclude` directives unioned. `retract` directives stay from published.

- **Module-path rewrite chained BEFORE go.mod merge.** Apply ordering:
  1. Stage tempdir as deep copy of published (still has published module path in go.mod)
  2. Overwrite TEMPLATED-CLEAN and copy NEW-TEMPLATE-EMISSION files from fresh into tempdir (these now have standalone-form imports)
  3. Run `RewriteModulePath(tempdir, freshModulePath, publishedModulePath)` — at this point tempdir's go.mod still has the published module line (untouched in step 1's copy), but the new files have fresh-form imports. Wait — this is wrong. Need to write the fresh go.mod first, run RewriteModulePath against fresh→published rewrite, THEN write the merged go.mod that locks published module path in.
  
  Corrected order:
  1. Stage tempdir as deep copy of published
  2. Overwrite TEMPLATED-CLEAN and copy NEW-TEMPLATE-EMISSION files from fresh
  3. Write fresh's go.mod into tempdir (so RewriteModulePath sees the standalone-form module line)
  4. Run `RewriteModulePath(tempdir, freshStandaloneModulePath, publishedMonorepoModulePath)` — this rewrites all `.go` import lines AND the go.mod module line atomically
  5. Write the merged go.mod (which has published module + fresh require/replace) — overwrites step 4's go.mod with the final form
  6. Apply AddCommand restoration plans (against the staged tempdir's now-import-correct files)
  7. Two-step rename to swap

- **Test files (`*_test.go`) follow same rules as production code.** No special preservation logic. Document in command help and PR description as a known trade-off — a developer who added custom tests to a templated test file will see that file flagged as TEMPLATED-WITH-ADDITIONS and must merge by hand.

- **`--apply --json` and `--dry-run --json` share schema.** Same `MergeReport` JSON shape; the `applied` boolean per-file distinguishes whether a write actually happened. Top-level `applied: true` means the swap completed.

- **Path traversal protection at input boundary.** Reject `<cli-dir>` if it contains `..` segments or fails the `filepath.Abs(...)` containment check against the user's CWD. Override via `--force`.

- **Supported OS matrix: macOS, Linux.** Windows rename semantics differ (in-use files block rename); deferred. Documented in `--help`.

---

## Open Questions

### Resolved During Planning

- **AST library choice**: dave/dst for read-write, std-lib for read-only.
- **Package location**: `internal/pipeline/regenmerge/`.
- **Transactional model**: stage-and-swap with bak-recovery anchor.
- **Classification verdicts**: 6 (added NOVEL-COLLISION for the "neither has marker, disjoint decls" case).
- **Marker fallback**: logical OR with decl-subset check.
- **Apply ordering**: import rewrite via fresh module line BEFORE merged go.mod is written.
- **Replace-directive precedence**: local-path published wins over fresh; version replaces use fresh.
- **Helper reuse**: copy unexported helpers from prior art rather than waiting on extraction.

### Deferred to Implementation

- **Receiver canonicalization key format**: target `(*pkg.Type).Method` with type parameters stripped from the comparison key. Confirm during implementation with a fixture pair like `func (s *Store) Get(...)` vs `func (s *Store) Get[T any](...)`.
- **Atomic rename across filesystem boundaries**: `os.Rename` may fail across mount points. Sibling tempdir is on same FS by default. If hit in practice, add a copy+delete fallback.
- **Stale binary at root**: the compiled `<cli>-pp-cli` binary lives at the CLI dir root. Skip non-Go non-config files at the walker level; verify the skip list during implementation against a real published CLI tree.
- **U1 LOC budget**: original "200 LOC" target was optimistic; cross-file decl search and receiver canonicalization push closer to 350-450 LOC. Don't truncate logic to fit a budget.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

### Top-level flow

```
regen-merge <cli-dir> --fresh <fresh-dir> [--apply] [--json] [--force]
  │
  ├─ validate inputs (path traversal check, both dirs exist as Go modules,
  │                   git status clean unless --force)
  │
  ├─ Classify(publishedDir, freshDir) → MergeReport
  │   │
  │   ├─ walk publishedDir/internal, publishedDir/cmd, publishedDir/go.mod
  │   ├─ walk freshDir's matching paths
  │   ├─ for each file path encountered in either tree:
  │   │   classify → one of {NOVEL, NEW-TEMPLATE-EMISSION, TEMPLATED-CLEAN,
  │   │                       TEMPLATED-WITH-ADDITIONS, PUBLISHED-ONLY-TEMPLATED,
  │   │                       NOVEL-COLLISION}
  │   ├─ for cobra-host files (root.go, resource-parents):
  │   │   extract published-AddCommand call set (via dave/dst, generalized predicate)
  │   │   extract fresh-AddCommand call set
  │   │   compute lost = published − fresh
  │   └─ for go.mod: build merged-go.mod plan (published module + fresh requires
  │                   + smart replace union)
  │
  ├─ if --dry-run: render report (text or JSON), exit
  │
  └─ if --apply: Apply(report)
      ├─ create sibling tempdir <parent>/<basename>.regen-merge-<ts>/
      ├─ deep-copy publishedDir → tempdir (preserves novels, additions, collisions)
      ├─ overwrite TEMPLATED-CLEAN + NEW-TEMPLATE-EMISSION files from fresh
      ├─ delete PUBLISHED-ONLY-TEMPLATED files from tempdir
      ├─ write fresh's go.mod into tempdir (standalone module path)
      ├─ run RewriteModulePath(tempdir, freshModule, publishedModule)
      │   — rewrites all .go imports AND go.mod module line
      ├─ overwrite tempdir's go.mod with final merged form
      │   (published module + fresh requires + smart replaces)
      ├─ apply AddCommand restoration plans
      │   (referent-existence check against tempdir's internal/cli/ first;
      │    skip-with-warning when name not found)
      ├─ Rename(<cli-dir>, <cli-dir>.bak-<ts>)
      ├─ Rename(tempdir, <cli-dir>)
      │   — on failure: attempt Rename(bak, <cli-dir>) to recover, log forensics
      └─ RemoveAll(<cli-dir>.bak-<ts>)
          — on failure: leave bak in place, report path; tree is fine
```

### Classification decision tree

```
file F:
  in published?      no  → was-templated(F in fresh)?
                              yes → NEW-TEMPLATE-EMISSION (copy fresh in)
  in fresh?          no  → was-templated(F in published)?
                              yes → PUBLISHED-ONLY-TEMPLATED
                              no  → NOVEL (preserve)
  in both:
    is-templated(both)?
      yes:
        decl-set comparison:
          pub ⊆ fresh    → TEMPLATED-CLEAN (overwrite)
          pub has extras → cross-file: do extras exist anywhere in fresh?
                            all-found  → TEMPLATED-CLEAN (decls moved)
                            any-missing → TEMPLATED-WITH-ADDITIONS
      no:
        decl-set intersection?
          empty   → NOVEL-COLLISION (preserve, surface warning)
          partial → TEMPLATED-WITH-ADDITIONS (preserve, surface delta)

is-templated(F) = marker-on-line-2(F) OR decl-set-strict-subset(F, fresh-equivalent)
was-templated(F in tree) = marker-on-line-2(F) OR (file is named like a templated
                            file, e.g., promoted_*.go, *_get.go pattern matches)
```

### MergeReport JSON schema (six verdicts represented)

```json
{
  "cli_dir": "/path/to/postman-explore",
  "fresh_dir": "/tmp/fresh-postman-explore",
  "applied": false,
  "files": [
    {"path": "internal/cli/canonical.go", "verdict": "NOVEL", "applied": false},
    {"path": "internal/cli/helpers.go", "verdict": "TEMPLATED-CLEAN", "applied": false},
    {"path": "internal/cli/import.go", "verdict": "NEW-TEMPLATE-EMISSION", "applied": false},
    {"path": "internal/cli/promoted_old.go", "verdict": "PUBLISHED-ONLY-TEMPLATED", "applied": false},
    {
      "path": "internal/cli/auth.go",
      "verdict": "TEMPLATED-WITH-ADDITIONS",
      "applied": false,
      "decl_set_delta": {
        "in_published_not_fresh": ["pkceChallenge", "(*Client).oauthBrowserFlow", "persistToken"],
        "in_fresh_not_published": ["(*Client).Login"]
      }
    },
    {
      "path": "internal/cli/legacy.go",
      "verdict": "NOVEL-COLLISION",
      "applied": false,
      "decl_set_delta": {
        "in_published_not_fresh": ["legacyHandler"],
        "in_fresh_not_published": ["newWidgetCmd"]
      }
    }
  ],
  "lost_registrations": [
    {
      "host_file": "internal/cli/root.go",
      "calls": ["rootCmd.AddCommand(newCanonicalCmd(flags))"],
      "applied": false,
      "skipped_for_missing_referent": []
    }
  ],
  "go_mod": {
    "merged": false,
    "preserved_module_path": "github.com/mvanhorn/printing-press-library/library/.../postman-explore",
    "added_requires": ["github.com/x/y v1.2.3"],
    "removed_requires": [],
    "preserved_replaces": ["github.com/local/fork => ./fork"]
  }
}
```

The `applied` boolean at top level is true only when both renames succeeded. Per-file `applied` is true only when the merged file appears in the final `<cli-dir>` tree.

---

## Output Structure

```
internal/pipeline/regenmerge/
├── regenmerge.go              # Public API: Classify, Apply, MergeReport types
├── classify.go                # Per-file classifier; decl-set extraction
├── registrations.go           # AddCommand AST extraction + restoration plans
├── gomod.go                   # go.mod merge with smart replace handling
├── apply.go                   # Stage-and-swap orchestration
├── helpers.go                 # writeFileAtomic + small utilities (copied from prior art)
├── classify_test.go
├── registrations_test.go
├── gomod_test.go
├── apply_test.go
├── e2e_test.go                # End-to-end fixtures
└── testdata/
    ├── postman-explore/       # Postman-shape fixture
    │   ├── published/
    │   ├── fresh/
    │   └── expected/
    └── ebay-auth/             # Ebay-shape fixture (auth.go preservation)
        ├── published/
        ├── fresh/
        └── expected/

internal/cli/
└── regen_merge.go             # Cobra wiring (modify root.go to register)
```

---

## Implementation Units

- U0. **Create test fixtures**

**Goal:** Establish the postman-explore-shape and ebay-auth-shape fixtures up front so subsequent units can write tests against real-shape inputs from day one.

**Requirements:** R1, R6, R7

**Dependencies:** None

**Files:**
- Create: `internal/pipeline/regenmerge/testdata/postman-explore/{published,fresh,expected}/...`
- Create: `internal/pipeline/regenmerge/testdata/ebay-auth/{published,fresh,expected}/...`

**Approach:**
- Postman-explore fixture: trim the actual published postman-explore-pp-cli to a minimum reproducing subset (root.go with 23 AddCommand calls including 10 novels + 1 sub-command on category.go; helpers.go with new templated `printJSONFiltered`; novel_helpers.go with old `printJSONFiltered` collision; canonical.go and 1-2 other novels for novel preservation; auth.go as a clean templated file).
- Ebay-auth fixture: minimal root.go + auth.go in published (with 5+ hand-added OAuth functions) + auth.go in fresh (small templated stub).
- Expected directories carry the correct post-merge tree byte-for-byte.
- All fixtures reflect a stable template version so they don't rot every generator change. Document the template version in a fixture-level README.

**Patterns to follow:**
- `internal/pipeline/mcpsync/sync_test.go` for inline-fixture style
- `testdata/golden/expected/` for checked-in expected-output trees

**Test scenarios:**
- Test expectation: none — fixtures themselves; verification is by U1-U5's tests passing against them.

**Verification:**
- Each fixture's `published` and `fresh` trees parse cleanly with `go/parser`
- `expected/` trees represent the post-merge state matching the test scenarios in U1-U5

---

- U1. **Add Classify entry point and core types**

**Goal:** Establish the `internal/pipeline/regenmerge/` package with the public API surface (`Classify`, `MergeReport`, six-verdict classification enum) and the file-walker that produces a per-file verdict using marker-OR-decl-subset detection plus cross-file decl search.

**Requirements:** R1, R12, R14, R15

**Dependencies:** U0

**Files:**
- Create: `internal/pipeline/regenmerge/regenmerge.go` (entry point, types, `Classify` function)
- Create: `internal/pipeline/regenmerge/classify.go` (per-file classifier, decl-set extraction)
- Create: `internal/pipeline/regenmerge/helpers.go` (atomic write helper copied from mcpsync)
- Test: `internal/pipeline/regenmerge/classify_test.go`

**Approach:**
- Walk both trees with `filepath.WalkDir` over `internal/` and `cmd/`. Skip `build/`, compiled binaries, `.git/`, non-Go files except `go.mod`, `go.sum`.
- For each file path encountered in either tree, decide via the decision tree in High-Level Technical Design.
- Decl-set extraction: parse with std-lib `go/parser`, walk top-level `*ast.FuncDecl` and `*ast.GenDecl`. Methods canonicalize as `(*pkg.Type).Method`; type parameters stripped from key.
- Marker check: line 2 of the file contains `// Generated by CLI Printing Press`.
- Cross-file decl search: when published has extras, check the global decl-set across all of fresh's `internal/` files before flagging WITH-ADDITIONS.
- Path traversal validation: `filepath.Abs` + prefix-containment check against CWD; reject `..` segments unless `--force`.
- Copy `writeFileAtomic` from `internal/pipeline/mcpsync/sync.go:666-675` into `helpers.go` with attribution comment. Do not import the unexported function.

**Patterns to follow:**
- `internal/pipeline/mcpsync/sync.go` (tree walking, marker detection)
- `internal/pipeline/polish.go` (std-lib AST parse + walk)
- `internal/narrativecheck/narrativecheck.go` (package shape, exported types)

**Test scenarios:**
- Happy path: file present in both trees, identical decl-set → TEMPLATED-CLEAN.
- Happy path: published has marker, fresh has marker, decl-set ⊆ fresh → TEMPLATED-CLEAN.
- Happy path: published lacks marker but decl-set ⊆ fresh → TEMPLATED-CLEAN (older-CLI case).
- Happy path: file in fresh, not published → NEW-TEMPLATE-EMISSION.
- Edge case: published has extras → TEMPLATED-WITH-ADDITIONS with delta listing the extras (covers ebay-shape: 5+ added function names like `pkceChallenge`, `(*Client).oauthBrowserFlow`, `persistToken`).
- Edge case: published has extras but they exist in fresh elsewhere (helper moved file) → TEMPLATED-CLEAN.
- Edge case: file in both, neither has marker, decl-sets disjoint → NOVEL-COLLISION.
- Edge case: file in both, neither has marker, decl-sets intersect with extras on published → TEMPLATED-WITH-ADDITIONS.
- Edge case: file in published with marker, not in fresh → PUBLISHED-ONLY-TEMPLATED.
- Edge case: file in published without marker, not in fresh → NOVEL.
- Edge case: same method name on different receivers → distinct keys; `(*Client).Get` vs `(*Helper).Get` resolve as different decls.
- Edge case: method with type parameters: `func (s *Store) Get[T any](...)` and `func (s *Store) Get(...)` canonicalize to same key for comparison; report shows full form.
- Error path: `<cli-dir>` contains `..` → returns input error.
- Error path: `<cli-dir>` not under CWD prefix → returns input error.
- Error path: published contains a syntactically broken `.go` file → classifier surfaces parse error per-file, doesn't crash the whole walk.
- Error path: skipped non-Go non-config files (compiled binary, `build/` dir) — confirmed not in walk results.

**Verification:**
- `Classify` returns a `MergeReport` matching the JSON schema for each test fixture
- All 6 classification verdicts produced by at least one fixture
- Cross-file decl search confirmed by a "moved helper" fixture pair

---

- U2. **AddCommand restoration via dave/dst**

**Goal:** Extract `AddCommand` call sites from both published and fresh cobra-host files (root.go and any resource-parent file using `<receiver>.AddCommand(...)`), compute the lost set, and produce per-file restoration plans that include referent-existence validation against the FRESH tree's `internal/cli/`.

**Requirements:** R6

**Dependencies:** U0, U1

**Files:**
- Create: `internal/pipeline/regenmerge/registrations.go` (AST extraction, restoration planning)
- Test: `internal/pipeline/regenmerge/registrations_test.go`

**Approach:**
- Use `github.com/dave/dst` per `internal/patch/ast_inject.go:228-282` precedent (copy patterns; do not import the unexported functions).
- **Generalized predicate**: walk `*ast.CallExpr` nodes, match where `Sel.Name == "AddCommand"`. Accept any receiver (`rootCmd`, `cmd`, `parentCmd`, etc.). The literal `id.Name == "rootCmd"` check in `isRootAddCommand` is too restrictive for resource-parent files.
- Identify "registration host" files: any `.go` file under `internal/cli/` containing at least one `AddCommand` call.
- Compute `lost = published_calls − fresh_calls` per host file.
- **Referent existence check**: for each lost call `parent.AddCommand(newX(args...))`, search the FRESH tree's `internal/cli/` (not "merged tree" — merged tree doesn't exist yet at U2 time) for a top-level decl named `newX`. If absent, mark the lost registration as `skipped_for_missing_referent`. Note: this catches name drift only; signature drift produces a silent compile failure documented as a known limitation.
- Restoration plan: per-file list of registration source lines + the AST node to inject before the `return rootCmd` / `return cmd` statement.

**Patterns to follow:**
- `internal/patch/ast_inject.go:228-282` (`addCommands`, `appendExecuteStatementsAfterLast`)
- `internal/patch/ast_inject.go:366-380` (`isRootAddCommand` predicate — generalize, don't reuse)

**Test scenarios:**
- Happy path: published `root.go` has 23 AddCommand calls, fresh has 14 (the templated set), lost = 9 novel registrations → all 9 included in restoration plan.
- Happy path: published `category.go` has `cmd.AddCommand(newCategoryLandscapeCmd(flags))` (receiver `cmd`, not `rootCmd`) not in fresh → flagged for restoration on the resource-parent file. Confirms generalized predicate works.
- Edge case: lost registration `rootCmd.AddCommand(newDeletedNovelCmd(flags))` where `newDeletedNovelCmd` doesn't exist in fresh tree's `internal/cli/` → `skipped_for_missing_referent` warning, no AST injection.
- Edge case: lost registration where `newX` exists in fresh but with mismatched arity (`newX()` in fresh, lost call is `newX(flags, store)`) → injection succeeds (referent name exists), the resulting code does NOT compile. Documented as known limitation; test asserts the warning is NOT raised here, the user is expected to catch via post-merge `go build`.
- Edge case: published `root.go` uses a helper function for registration: `registerNovelCommands(rootCmd)`. The function call doesn't match `AddCommand`, so no calls extracted from helper → file flags as TEMPLATED-WITH-ADDITIONS at the U1 level (because `registerNovelCommands` isn't in fresh's decl set). U2 doesn't try to handle this; surfaced for human merge.
- Edge case: a registration appears in both published and fresh root.go but with different argument shapes — included in both sets, lost = empty, no action.

**Verification:**
- Restoration plans produced for postman-explore-shape fixture include all 10 lost root.go calls and 1 lost category.go call
- Referent-existence check produces a `skipped_for_missing_referent` entry for a fixture where the constructor doesn't exist
- Custom-registration-helper fixture's root.go flags as TEMPLATED-WITH-ADDITIONS (handled at U1 level)

---

- U3. **go.mod merge plan with smart replace handling**

**Goal:** Build the `go.mod` merge plan: published `module` line + fresh `go`/`require` + smart `replace` union (local-path published wins, version-replaces from fresh) + `exclude` union + published `retract`. U3 produces the plan; U4 chains it into the apply ordering.

**Requirements:** R7

**Dependencies:** U0

**Files:**
- Create: `internal/pipeline/regenmerge/gomod.go` (modfile merge logic)
- Test: `internal/pipeline/regenmerge/gomod_test.go`

**Approach:**
- Use `golang.org/x/mod/modfile` per `internal/pipeline/mcpsync/sync.go:701-735` precedent.
- Parse both go.mod files. Build merged file:
  - `module` line: published verbatim
  - `go` directive: fresh's value
  - `require`: fresh's set (latest pinned versions); for any directive in published not in fresh, drop (template no longer needs it)
  - `replace`: union with conflict resolution:
    - For each `replace target` in published where the target starts with `.` or `/` (local-path) → published wins
    - For each `replace target` in fresh → use fresh
    - Otherwise union
  - `exclude`: union of both
  - `retract`: published's set (template wouldn't emit retract directives anyway)
- Format via `mf.Format()`. U3 returns the formatted bytes; U4 writes them.
- Critical: U3 does NOT call `RewriteModulePath`. That's U4's orchestration responsibility (must run BEFORE the merged go.mod is written; see U4 ordering).

**Patterns to follow:**
- `internal/pipeline/mcpsync/sync.go:701-735` (modfile parse/merge/format)

**Test scenarios:**
- Happy path: published has 3 requires, fresh has 5 (2 new, same 3) → merged has 5 with fresh's pinned versions, published's module path.
- Edge case: published has `replace github.com/x/y => ./local-fork` (local path), fresh has none → merged preserves the replace.
- Edge case: published has `replace github.com/x/y => ./local-fork`, fresh has `replace github.com/x/y => upstream@v1.2.3` (version replace) → merged preserves PUBLISHED's local-path replace (the local fork is what makes the monorepo CLI compile).
- Edge case: published has no replace for `github.com/x/y`, fresh has `replace github.com/x/y => upstream@v1.2.3` → merged uses fresh.
- Edge case: published `go` directive is `1.21`, fresh is `1.23` → merged uses `1.23`.
- Edge case: published has `retract v1.0.0`, fresh has none → merged preserves the retract.
- Edge case: published has `exclude github.com/x/y v0.5.0`, fresh has `exclude github.com/a/b v0.1.0` → merged has both.

**Verification:**
- Merged go.mod parses cleanly via `modfile.Parse`
- All requires from fresh present
- Module path matches published
- Local-path replaces from published preserved when fresh would have replaced with a version

---

- U4. **Apply orchestration with stage-and-swap-with-recovery**

**Goal:** Wire `Classify` + restoration plans + go.mod merge into a transactional `Apply` function that writes everything to a sibling tempdir and uses two-step rename with bak-recovery.

**Requirements:** R2, R3, R8, R11, R13

**Dependencies:** U1, U2, U3

**Files:**
- Create: `internal/pipeline/regenmerge/apply.go` (orchestration, tempdir staging, rename-with-recovery)
- Test: `internal/pipeline/regenmerge/apply_test.go`

**Approach:**
- Stage tempdir adjacent to `<cli-dir>` (sibling, same parent) to ensure same-filesystem rename. Path: `<parent>/<basename>.regen-merge-<unix-ts>/`.
- **Pre-flight**: verify git tree is clean at `<cli-dir>` via `git status --porcelain <cli-dir>`. Refuse with `ExitInputError` if non-empty unless `--force`.
- **Apply ordering** (the corrected sequence — see Key Technical Decisions):
  1. Deep copy `<cli-dir>` → tempdir (preserves novels, additions, collisions verbatim)
  2. Overwrite TEMPLATED-CLEAN files: copy fresh version into tempdir
  3. Add NEW-TEMPLATE-EMISSION files: copy fresh version into tempdir at the same path
  4. Delete PUBLISHED-ONLY-TEMPLATED files from tempdir
  5. Write fresh's `go.mod` into tempdir (standalone module path)
  6. Run `RewriteModulePath(tempdir, freshStandaloneModulePath, publishedMonorepoModulePath)` — rewrites all `.go` import lines AND the `go.mod` module line in one sweep
  7. Overwrite tempdir's `go.mod` with the merged form from U3 (published module + fresh require/replace + smart replace handling)
  8. Apply U2's restoration plans against tempdir's now-import-correct cobra-host files (referent check uses tempdir's `internal/cli/`)
- **Two-step rename with bak-recovery**:
  1. `Rename(<cli-dir>, <cli-dir>.bak-<ts>)`
  2. `Rename(tempdir, <cli-dir>)`
     - On failure: attempt `Rename(<cli-dir>.bak-<ts>, <cli-dir>)` to recover
     - If recovery also fails: log absolute path of `<cli-dir>.bak-<ts>`; return error with both paths
  3. `RemoveAll(<cli-dir>.bak-<ts>)` — on failure here, leave bak in place (tree is fine, bak just lingers; report the path so user can clean up)
- All file writes use `writeFileAtomic` from `helpers.go`.
- On any error pre-rename, remove tempdir; published `<cli-dir>` is untouched.

**Patterns to follow:**
- `internal/pipeline/mcpsync/sync.go` (tempdir staging, atomic write)
- `internal/pipeline/publish.go:458-503` (recursive copy with symlink validation)

**Test scenarios:**
- Happy path: all-CLEAN fixture → tempdir created, files overwritten, swap completes; final tree matches expected.
- Happy path: postman-explore fixture → safe applications applied; TEMPLATED-WITH-ADDITIONS files unchanged; lost registrations restored in root.go and category.go; final tree compiles (asserted by parse, not by `go build`).
- Edge case: ebay-auth fixture → auth.go preserved byte-for-byte; merged tree report flags it as TEMPLATED-WITH-ADDITIONS with delta.
- Edge case: fixture where U2's referent-check skipped a registration → tempdir's root.go does NOT contain the dangling AddCommand; report carries the skipped warning.
- Edge case: published has dirty git tree → ExitInputError before any tempdir creation.
- Edge case: published has dirty git tree + `--force` → proceeds, uncommitted edits to TEMPLATED-CLEAN files lost (test verifies and warns are logged).
- Error path: simulated write failure mid-merge → tempdir cleaned up, original `<cli-dir>` byte-equal to pre-call state.
- Error path: simulated `os.Rename` step 2 failure → recovery rename restores `<cli-dir>` from bak; tempdir cleaned up.
- Error path: simulated double-rename failure (steps 2 AND recovery) → returns error with both bak path and tempdir path; original `<cli-dir>` is missing (this is the genuinely-unrecoverable state, but the error message tells the user where to find their data).
- Idempotency: running `Apply` twice on the same trees → second run produces all-CLEAN classification with `applied: true` for the safe verdicts but no actual content change (file bytes match before and after second run, asserted by checksums).

**Verification:**
- Apply on the postman-explore fixture produces a tree where `search-all` is non-Hidden and the 10 novel commands plus `category landscape` are all wired
- Apply on the ebay-fixture preserves `auth.go` byte-for-byte (TEMPLATED-WITH-ADDITIONS, not overwritten)
- Failure-injection tests confirm the original `<cli-dir>` is never observed in a partial state for the two single-rename failure modes; the double-failure mode reports clear paths

---

- U5. **Cobra subcommand wiring**

**Goal:** Expose `regen-merge` as a top-level subcommand with `--fresh`, `--apply`, `--json`, `--force` flags. Default mode is dry-run.

**Requirements:** R9, R10, R12, R13

**Dependencies:** U1, U2, U3, U4

**Files:**
- Create: `internal/cli/regen_merge.go` (cobra wiring)
- Modify: `internal/cli/root.go` (register the new command)
- Test: `internal/cli/regen_merge_test.go` (flag handling, error paths, basic invocation)

**Approach:**
- Mirror `internal/cli/mcp_sync.go` shape: `func newRegenMergeCmd() *cobra.Command`, `Args: cobra.ExactArgs(1)`, closure-captured flag vars.
- Flags:
  - `--fresh <dir>` (required, the fresh-generated tree)
  - `--apply` (default false → dry-run)
  - `--json` (default false → human-readable)
  - `--force` (allows: operating outside CWD prefix, dirty git tree, override of safety preconditions)
- RunE: validate inputs, call `regenmerge.Classify(...)`, then conditionally `regenmerge.Apply(...)`. Render report via `printReport(w io.Writer, r *MergeReport, asJSON bool)`.
- Use `cmd.OutOrStdout()` and `cmd.OutOrStderr()` (mcp_sync precedent: testable). Human report → stderr; JSON report → stdout (so callers can pipe `--json` cleanly).
- Failure handling: input errors return `&ExitError{Code: ExitInputError, Err: ...}`; write/AST failures return `&ExitError{Code: ExitPublishError, Err: ...}`.
- Help text mentions: macOS+Linux only; `--apply` requires clean git tree by default; `--force` for power users.
- Register in `internal/cli/root.go`'s `rootCmd.AddCommand(...)` block (currently lines 43-69).

**Patterns to follow:**
- `internal/cli/mcp_sync.go` (cobra structure, args, ExitError use, --force flag)
- `internal/cli/validate_narrative.go` (`--strict`, `--json`, ExitError patterns)

**Test scenarios:**
- Happy path: `regen-merge <cli-dir> --fresh <fresh>` (no `--apply`) → exits 0, prints classification report to stderr, no files modified.
- Happy path: `--apply` flag set → orchestrator's Apply called; success exit 0.
- Happy path: `--json` → JSON shape on stdout matches `MergeReport` schema.
- Error path: missing `--fresh` flag → ExitInputError with "--fresh is required" message.
- Error path: `<cli-dir>` doesn't exist → ExitInputError with clear message.
- Error path: `<cli-dir>` contains `..` → ExitInputError; also rejects when CWD-prefix containment fails (unless `--force`).
- Error path: `--apply` against dirty git tree → ExitInputError suggesting `git status` or `--force`.
- Error path: write failure during Apply → ExitPublishError, original `<cli-dir>` untouched (per U4's recovery).

**Verification:**
- Subcommand appears in `printing-press --help` listing
- Dry-run on the postman-explore fixture produces the expected classification table on stderr
- `--apply` on the same fixture produces a tree byte-equal to `testdata/postman-explore/expected/`

---

- U6. **End-to-end acceptance test**

**Goal:** Lock in the design against the two named acceptance scenarios — the postman-explore-shape success case and the ebay-shape preservation case — by running the full `Apply` orchestration against the fixtures from U0 and asserting tree-equality against `expected/`.

**Requirements:** R1, R2, R4, R6, R7, R11

**Dependencies:** U0, U1, U2, U3, U4, U5

**Files:**
- Create: `internal/pipeline/regenmerge/e2e_test.go`

**Approach:**
- Test invokes `regenmerge.Classify` followed by `regenmerge.Apply`, asserts the post-apply tempdir-after-swap matches the checked-in `expected/` directory tree byte-for-byte.
- For the ebay fixture, the assertion is "auth.go in expected matches auth.go in published" (preservation).
- For the postman fixture, the assertion is "expected has restored AddCommand calls in root.go and category.go, and helpers.go matches fresh."

**Test scenarios:**
- Postman-explore happy path: `Classify` produces the expected verdict per file; `Apply` produces a tree matching `testdata/postman-explore/expected/`.
- Ebay-auth happy path: `Classify` flags auth.go as TEMPLATED-WITH-ADDITIONS; `Apply` produces a tree where auth.go matches `testdata/ebay-auth/published/internal/cli/auth.go` (i.e., preserved).
- Idempotency: run `Apply` twice on the postman-explore fixture; second run reports all CLEAN/already-applied; final tree byte-equal to first-run output.

**Verification:**
- E2E test passes against checked-in expected/ trees for both fixtures
- Acceptance assertion: ebay's auth.go is byte-equal pre and post apply
- Idempotency assertion: applying twice produces no diff against single-apply output

---

## System-Wide Impact

- **Interaction graph:** `regen-merge` is a new top-level subcommand. No existing subcommands change. No callbacks, observers, or middleware touched.
- **Error propagation:** Failures use `&ExitError{Code: ExitInputError, ...}` for input issues and `ExitPublishError` for write/AST issues, consistent with mcp-sync and validate-narrative.
- **State lifecycle risks:** Stage-and-swap-with-recovery design ensures `<cli-dir>` is observed in only two normal states (pre-merge or post-merge), with one recoverable transient (bak alongside post-merge tempdir) and one genuinely-unrecoverable rare case (both renames fail; user has bak path in error message). Tempdir cleanup on failure is the only state-shaping concern; covered by U4's error-path tests.
- **API surface parity:** No existing API changes. New CLI surface only.
- **Integration coverage:** U6's e2e tests against the postman-explore and ebay fixtures cover the cross-layer story (classify → restoration plan → apply → final tree).
- **Unchanged invariants:** `printing-press generate`, `printing-press mcp-sync`, `printing-press publish`, all existing subcommands and their templates are unchanged. The generator's templates and FuncMap are not touched.
- **Documentation impact:** Add `regen-merge` to the AGENTS.md glossary alongside `mcp-sync`. Add a usage section to README or an inline `--help` example sufficient for discovery.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Decl-set false positives when generator legitimately splits files into multiple emit locations | Cross-file decl search before flagging WITH-ADDITIONS (U1 test scenario) |
| Decl-set false positives when generator legitimately REMOVES a templated decl in a new version (inverse case) | Documented as known limitation (Scope Boundaries); user inspects via `git diff`. Defer mitigation until evidence shows recurring pain. |
| AddCommand restoration injects a reference to a deleted constructor → non-compiling output | Referent-existence check against fresh tree's `internal/cli/` before injection; surfaced as `skipped_for_missing_referent` warning rather than crash. Only catches name drift, not signature drift. |
| AddCommand restoration injects a call with mismatched arity → non-compiling output | Documented as known limitation; user catches via post-merge `go build`. |
| Atomic rename fails across mount points | Same-filesystem sibling tempdir is the default placement; if it fails, fall back to copy+delete (deferred to implementation) |
| Custom registration patterns (helper functions, dynamic registration) defeat AST detection | File flagged as TEMPLATED-WITH-ADDITIONS when the standard pattern is missing — caller falls back to manual review for that file (documented limitation) |
| Hand-edited bodies of templated functions silently overwritten | Caller is expected to `git diff` the merged tree as part of the sweep workflow; documented limitation in `--help` and PR description |
| User runs `--apply` on a dirty git tree, uncommitted edits to TEMPLATED-CLEAN files destroyed | `--apply` rejects dirty trees by default (R13); `--force` overrides with logged warning |
| Both renames fail leaving original missing | Documented unrecoverable state; error message reports bak path so user can manually restore |
| Editor or IDE holding files in `<cli-dir>` during rename | Documented in `--help` as a precondition (close editors); test confirms macOS/Linux behavior; Windows explicitly unsupported |
| Helpers reused from prior art are unexported | Copy ~30 LOC into `helpers.go` with attribution rather than refactoring upstream packages (deferred, scope creep) |

---

## Documentation / Operational Notes

- Add `regen-merge` entry to the AGENTS.md glossary (currently lists `mcp-sync` at line 169).
- Add a brief usage example to `internal/cli/regen_merge.go`'s `Long` and `Example` fields per existing convention (mcp_sync.go shape).
- Help text must call out:
  - macOS+Linux only
  - `--apply` requires clean git tree by default
  - `<cli-dir>` editor/IDE locks may break the rename — close editors before applying
  - The bak path's location and meaning if a recovery scenario triggers
- No release coordination needed — this is a new additive subcommand with no breaking changes.
- After this lands, follow up with a sweep PR that uses the new tool against the SWEEP-OK and SIG-DRIFT-LIGHT buckets identified in the cli-printing-press#388 triage.

---

## Sources & References

- **Triage data motivating this work:**
  - https://github.com/mvanhorn/cli-printing-press/issues/388 (umbrella)
  - The earlier comment chain on #388 with the v1 and v2 triage tables and the post-correction analysis of why "compile clean" wasn't a safe sweep signal
- **Direct prior art in repo (to copy patterns, not import directly):**
  - `internal/pipeline/mcpsync/sync.go` — tree reconcile pattern, `writeFileAtomic` (unexported, copy)
  - `internal/patch/ast_inject.go` — dave/dst AddCommand manipulation (unexported, copy and generalize)
  - `internal/pipeline/modulepath.go` — `RewriteModulePath` exported helper (read existing module path; sequence dependency)
  - `internal/narrativecheck/narrativecheck.go` — package shape precedent
  - `internal/cli/mcp_sync.go` — cobra subcommand precedent
- **Institutional learnings:**
  - `docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md`
  - `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`
  - `docs/solutions/security-issues/filepath-join-traversal-with-user-input-2026-03-29.md`
- **External: none** — local patterns cover all required primitives
