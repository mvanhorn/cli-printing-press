---
title: "Dual-key identity fields: keep the slug and the display name as separate spec fields with separate resolution paths"
date: 2026-05-06
category: design-patterns
module: cli-printing-press-generator
problem_type: design_pattern
component: tooling
severity: medium
applies_when:
  - "A single conceptual identity (an owner, an author, a tenant) needs to flow into both path-shaped surfaces (module paths, copyright headers, file slugs) and prose-shaped surfaces (display names, README bylines, author metadata)"
  - "The path-shaped form would corrupt the prose-shaped form if reused (e.g., sanitizing 'Trevin Chow' to 'trevin-chow' is right for a Go module path and wrong for a SKILL.md `author:` field)"
  - "Conflating the two has historically led to slug-shaped values landing in published prose surfaces — visible to users, hard to retract once distributed"
tags:
  - identity
  - naming
  - slug-vs-display
  - generator-fields
  - resolution-paths
related_components:
  - generator
  - templates
  - manifest
---

# Dual-key identity fields: keep the slug and the display name as separate spec fields with separate resolution paths

## Context

`internal/spec/spec.go`'s `APISpec` originally carried a single `Owner` field that drove both Go module paths (`github.com/<owner>/...`) and prose surfaces (copyright headers, README bylines). The resolver sanitized whatever it found from `git config` so the value was safe to embed in a path: lowercase, hyphenate spaces, strip non-`[a-z0-9_-]` characters. That worked fine when both surfaces wanted the same shape — a slug like `trevin-chow` made the module path correct.

Once printed CLIs needed a Hermes-recognized `author:` field in their SKILL.md frontmatter, the conflation broke. The slug-shaped value (`trevin-chow`) is fine for a path component but visibly wrong as a prose attribution in a public skill registry. The instinct to "just stop sanitizing" is also wrong — the path surface depends on the sanitization. There's no single value that satisfies both surfaces.

## Guidance

Carry two fields, not one. Resolve each via a path-appropriate chain. Apply a path-appropriate transformation to each. Land each in its own template surface. Do not let either resolver fall through to the other's value.

```go
// internal/spec/spec.go
type APISpec struct {
    Owner     string `yaml:"owner,omitempty"`      // path-safe slug, e.g. "trevin-chow"
    OwnerName string `yaml:"owner_name,omitempty"` // display name, e.g. "Trevin Chow"
    // ...
}
```

```go
// internal/generator/plan_generate.go

// Slug resolver: tries github.user first (already path-shaped),
// falls back to sanitizing user.name, defaults to "USER".
func resolveOwnerForNew() string {
    if out, err := exec.Command("git", "config", "github.user").Output(); err == nil && len(out) > 0 {
        return strings.TrimSpace(string(out))
    }
    if out, err := exec.Command("git", "config", "user.name").Output(); err == nil && len(out) > 0 {
        return sanitizeOwner(strings.TrimSpace(string(out)))
    }
    return "USER"
}

// Display-name resolver: reads raw user.name, no sanitization, no
// fallback to a slug-shaped default. Empty value is the caller's
// problem to handle — the resolver does not synthesize.
func resolveOwnerNameForNew() string {
    out, err := exec.Command("git", "config", "user.name").Output()
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(out))
}
```

The `resolveOwnerNameForNew` deliberately does NOT reuse `resolveOwnerForNew`. Three things in the slug resolver would corrupt a display name if reused:

1. **`github.user` first.** GitHub usernames are slug-shaped (`mvanhorn`), not display-shaped (`Matt Van Horn`). Wrong source for prose.
2. **`sanitizeOwner` collapses spaces and casing.** `"Trevin Chow"` becomes `"trevin-chow"`. The very thing we wanted to avoid.
3. **`"USER"` default.** Better to surface empty + warn than ship `author: "USER"` to a public skill registry.

At the template emission layer, escape the prose value:

```yaml
# internal/generator/templates/skill.md.tmpl
---
name: pp-{{.Name}}
description: "..."
author: "{{yamlDoubleQuoted .OwnerName}}"  # prose, escaped
license: "Apache-2.0"
---

# {{.ProseName}} — Printing Press CLI
# (note: copyright header still uses the slug)
```

```go
// internal/generator/templates/copyright.go.tmpl
// Copyright {{currentYear}} {{.Owner}}. Licensed under Apache-2.0. See LICENSE.
```

## Why This Matters

The two values flow into surfaces with different correctness criteria:

- **Path surface** (Go module paths, file directories, copyright headers' regex anchor): must be path-safe. A space, quote, or non-ASCII character breaks `go install`, breaks `git ls-files`, breaks the existing `RewriteOwner` regex (`^//\s*Copyright\s+\d+\s+([A-Za-z0-9_-]+)\.`). Sanitization is required.
- **Prose surface** (`author:` in skill registries, README bylines, MCPB manifest `author.name`): humans read this. Slug-shaped values are visibly broken. YAML-quoting is required (so colons or quotes in the name don't corrupt the file).

A single field forces the conflict to land somewhere. Sanitize and the prose surface ships ugly. Don't sanitize and the path surface breaks. The dual-key model resolves the conflict structurally rather than per-call-site.

The cost is small: one extra spec field, one extra resolver, one extra template variable. The benefit compounds: every future surface that needs identity attribution can now ask "do I want path-shaped or prose-shaped?" and reach for the right field. New code stays correct by default.

## When to Apply

- Identity needs to flow into both path-shaped AND prose-shaped surfaces in the same artifact (this is the trigger; if all surfaces are one shape, one field is enough)
- Sanitization is required to make the path-shaped surface work
- The prose surface is published or persisted somewhere users will see (a SKILL.md `author:`, a README byline, a manifest field — not just a debug log)
- Resolving from `git config` or similar untrusted-shape source where the same input can be either path-suitable or prose-suitable depending on what the user has configured

If a future field falls into the same shape (e.g., team name, organization, contributor list), follow the same split. Don't synthesize a third resolver that "tries to figure out which shape we need" — that's the conflation we're avoiding.

## Examples

### Before — single conflated field

```go
// In Generator.New(), Owner gets sanitized whether the consumer
// wanted a path or a prose value. There's no way to recover the
// original prose form once sanitization runs.
func New(s *spec.APISpec, outputDir string) *Generator {
    if s.Owner == "" {
        s.Owner = resolveOwnerForNew()  // returns slug-shaped
    }
    s.Owner = sanitizeOwner(s.Owner)    // re-sanitizes anyway
    // ...
}
```

```yaml
# Result in generated SKILL.md (wrong shape for author):
---
name: pp-shopify
author: "trevin-chow"   # slug-shaped, ugly
---
```

### After — dual fields, dual resolvers

```go
func New(s *spec.APISpec, outputDir string) *Generator {
    if s.Owner == "" {
        s.Owner = resolveOwnerForExisting(outputDir)
    }
    s.Owner = sanitizeOwner(s.Owner)  // path surface

    if s.OwnerName == "" {
        s.OwnerName = resolveOwnerNameForExisting(outputDir)  // prose, no sanitization
    }
    // ...
}
```

```yaml
# Result in generated SKILL.md (correct prose):
---
name: pp-shopify
author: "Trevin Chow"   # display-shaped
---
```

```go
// And the copyright header stays slug-shaped (path-shape correctness preserved):
// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.
```

## Related

- `docs/solutions/conventions/soft-validation-in-reusable-library-packages-2026-05-06.md` — the empty-value behavior for `OwnerName` (soft warn + slug fallback) so the generator stays reusable from tests / mcp-sync / regen-merge without forcing every caller to set the field
- `docs/solutions/conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md` — when `OwnerName` flows through a sweep over published content (vs. a fresh print), the resolver must NOT trust the operator's git config; lessons from the public-library retrofit
- `internal/generator/plan_generate.go` — `resolveOwnerForNew` / `resolveOwnerNameForNew` split
- `internal/spec/spec.go:103` — `OwnerName` field declaration with comment cross-referencing the slug field
- `AGENTS.md` — Naming and Disambiguation section pins the lesson for future contributors
