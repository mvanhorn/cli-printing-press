---
title: "Soft validation in reusable library packages: warn-and-fallback over hard-fail when the package serves multiple callers"
date: 2026-05-06
category: conventions
module: cli-printing-press-generator
problem_type: convention
component: tooling
severity: medium
applies_when:
  - "A library package's Generate / Run / Apply entry point is called from multiple contexts (production CLI, internal tooling, test fixtures, regen-merge, mcp-sync)"
  - "A field is required for production correctness but awkward to plumb through every caller's test fixture"
  - "Hard-failing the entry point breaks tests on CI runners that don't have the field's source available (e.g., `git config user.name` unset)"
  - "A degraded-but-visible fallback is preferable to either (a) blocking generation entirely or (b) silently emitting an empty / wrong value"
tags:
  - validation
  - library-design
  - test-friendly
  - fail-soft
  - stderr-warning
related_components:
  - generator
  - testing-framework
  - tooling
---

# Soft validation in reusable library packages: warn-and-fallback over hard-fail when the package serves multiple callers

## Context

`internal/generator` is reused by at least four callers in this repo: the `/printing-press` CLI command, `internal/pipeline/mcpsync` (rebuilds the MCP surface against a target tree), `internal/pipeline/regenmerge` (regen-merges existing CLIs), and the test suite (over 500 tests construct synthetic `APISpec` values directly). When a new required field is added to `APISpec`, the question of how to validate it is shaped by which callers must set it.

We added `OwnerName` (a prose-shaped display name for Hermes `author:` and other prose surfaces). The first cut hard-failed `Generate()` if `OwnerName` was empty:

```go
// First cut — hard error. Looked correct in isolation.
if g.Spec.OwnerName == "" {
    return fmt.Errorf("spec.OwnerName is empty: set `git config user.name` so the generator can populate Hermes `author:`")
}
```

That fired across ~50 tests on CI because GitHub Actions runners don't have `git config user.name` set, and most test fixtures construct `APISpec` inline without setting `OwnerName`. Updating every test fixture to set the field (mechanical but tedious — six files, dozens of constructions) was one option. The other was to recognize that the validation, while correct for the production path, was over-strict for a library package that intentionally serves many callers.

## Guidance

When a library package's entry point is called from many contexts, validate fields that production cares about but that tests / internal tooling can't reasonably set, with a **stderr warning + degraded-but-visible fallback** rather than an error return. The warning is the signal a real-print operator catches; the fallback keeps tests and tooling unbroken.

```go
// internal/generator/generator.go
func (g *Generator) Generate() error {
    if g.Spec.OwnerName == "" {
        // OwnerName flows into Hermes `author:` and other prose
        // surfaces. We don't hard-fail on an empty value because the
        // generator package is reused by many callers (tests,
        // mcp-sync, regen-merge) where setting it is awkward. Instead,
        // fall back to the slug-shaped Owner so emission is non-empty,
        // and warn loudly so a real-print operator catches the
        // misconfiguration. The library-wide sweep tool overrides this
        // via its own per-CLI authorship mapping, so this fallback only
        // ever lands on fresh prints by users who haven't set
        // `git config user.name`.
        fmt.Fprintf(os.Stderr,
            "WARNING: spec.OwnerName is empty; falling back to slug-shaped Owner (%q) for `author:` field. "+
                "Set `git config user.name` (display name, e.g. \"Trevin Chow\") to populate this correctly.\n",
            g.Spec.Owner,
        )
        g.Spec.OwnerName = g.Spec.Owner
    }
    // ... rest of Generate()
}
```

The fallback's three properties matter:

1. **Visible in output.** Falling back to the slug means the rendered SKILL.md ships `author: "trevin-chow"` instead of `author: ""`. A reviewer or downstream consumer can spot the slug-shape and know something's miscofigured. Empty would be silently broken.
2. **Loud at generation time.** The stderr warning fires every print where the fallback triggers. A user running `/printing-press` once sees the warning, sets git config, and re-runs. Tests and CI silently swallow the warning (it's stderr, not an error return) — exactly the asymmetric handling the use case calls for.
3. **Cheap to short-circuit later.** A different code path that owns the correct value (in our case: the library-wide sweep tool's per-CLI authorship mapping) overrides `OwnerName` before `Generate()` runs, so the fallback never fires for the path that genuinely needs accuracy.

Add a test that pins the soft-fallback behavior so a future refactor doesn't accidentally restore the hard-error:

```go
func TestGenerateSoftFallsBackOnEmptyOwnerName(t *testing.T) {
    t.Parallel()

    apiSpec := minimalSpec("ownerless")
    apiSpec.Owner = "trevin-chow"
    apiSpec.OwnerName = ""
    gen := New(apiSpec, t.TempDir())

    require.NoError(t, gen.Generate())
    assert.Equal(t, "trevin-chow", apiSpec.OwnerName,
        "soft-fallback should set OwnerName to slug-shaped Owner when empty")

    skill, _ := os.ReadFile(filepath.Join(gen.OutputDir, "SKILL.md"))
    assert.Contains(t, string(skill), `author: "trevin-chow"`,
        "author field should fall back to the slug rather than be empty")
}
```

## Why This Matters

The instinct to hard-fail is right when the package has a single caller. With multiple callers, hard-failing forces every caller to learn about every required field — a coupling the library was supposed to abstract over. Worse, it pushes the validation upstream into the test fixtures, where the validation has zero correctness value (a test doesn't care that `author:` is right; a published SKILL.md does).

Stderr warnings are the right channel because:

- **Asymmetric audience by default.** Production CLI users see stderr in their terminal. CI systems route stderr to logs that are ignored unless something fails. Test runners discard stderr entirely. The signal lands where it should without explicit per-context handling.
- **Non-fatal by construction.** The library's contract stays unchanged — `Generate()` still returns `nil` on a successful render. Callers that want to escalate the warning to an error (e.g., a publish workflow) can wrap and check stderr themselves.
- **Surfaces the problem, not the workaround.** "WARNING: spec.OwnerName is empty; falling back..." names the misconfiguration directly. A developer sees the warning and fixes git config, not the symptom of slug-shaped authors.

The risk being managed: a user without git config publishes 49 CLIs with `author: "trevin-chow"`. Mitigation comes from a different layer (the sweep tool's per-CLI mapping, which overrides `OwnerName` before `Generate()` runs), not from a hard error in the library. That layered defense is the architecturally honest answer — the library should be reusable; the publish path should be strict.

## When to Apply

- The library package's entry point is called from at least three distinct contexts (production CLI, internal tooling, tests are the typical trio)
- Every test author setting the field is the only alternative to soft-fallback, and the field is awkward to construct in test fixtures
- A degraded fallback exists that is **visibly wrong** (not silently empty or syntactically valid-looking-but-incorrect)
- A higher-level path (orchestrator, publish workflow, sweep tool) already enforces the strict invariant before the library is invoked

Don't apply this pattern when:

- The library has a single caller (just hard-fail; the caller is the right place to validate)
- The fallback would be silent or invisible (e.g., emitting `author: ""` is silent corruption — fail instead)
- There's no reasonable fallback (e.g., a missing API key has no degraded-but-visible default)

## Examples

### Anti-pattern — hard error breaks unrelated callers

```go
// internal/generator/generator.go (rejected)
func (g *Generator) Generate() error {
    if g.Spec.OwnerName == "" {
        return fmt.Errorf("spec.OwnerName is empty: ...")
    }
    // ...
}
```

CI failure on PR #655: 50+ tests broke because:

```
generator_test.go:1502: Generate: spec.OwnerName is empty: set `git config user.name` ...
session_handshake_test.go:286: Generate: spec.OwnerName is empty: set `git config user.name` ...
session_handshake_test.go:259: Generate: spec.OwnerName is empty: set `git config user.name` ...
... (47 more)
```

The fix shape "update every test fixture" is mechanical but doesn't capture the lesson — the library's contract was wrong, not the tests.

### Pattern — warn + fallback

```go
// internal/generator/generator.go
func (g *Generator) Generate() error {
    if g.Spec.OwnerName == "" {
        fmt.Fprintf(os.Stderr,
            "WARNING: spec.OwnerName is empty; falling back to slug-shaped Owner (%q) ...\n",
            g.Spec.Owner,
        )
        g.Spec.OwnerName = g.Spec.Owner
    }
    // ...
}
```

CI passes. Production users without git config see:

```
$ printing-press generate ...
WARNING: spec.OwnerName is empty; falling back to slug-shaped Owner ("trevin-chow") for `author:` field. Set `git config user.name` (display name, e.g. "Trevin Chow") to populate this correctly.
✓ Generated cli at ~/printing-press/library/.../trevin-chow/...
```

The user sees the warning, sets `git config user.name "Trevin Chow"`, regenerates, and the output is correct. Tests and CI silently ignore the warning. The library remains reusable.

## Related

- `docs/solutions/design-patterns/dual-key-identity-fields-2026-05-06.md` — the dual-field pattern that introduced `OwnerName` in the first place; this convention complements it by handling the "what if the prose-shaped field is unset" edge case without hard-failing reusable callers
- `internal/generator/generator.go` — `Generate()` empty-OwnerName check (post-soft-fallback shape)
- `internal/generator/skill_test.go` — `TestGenerateSoftFallsBackOnEmptyOwnerName` pins the behavior
- `AGENTS.md` — Naming and Disambiguation section flags both the dual-key pattern and the soft-fallback behavior to future contributors
