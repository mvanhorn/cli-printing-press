---
title: "Generator identifier collisions: dedupe with a hidden IdentName field and a late pre-pass"
date: 2026-05-08
category: design-patterns
module: cli-printing-press-generator
problem_type: design_pattern
component: tooling
severity: medium
applies_when:
  - "Adding a new spec struct whose Name field flows through camel-casing into a Go identifier the generator emits"
  - "Two or more spec entries in the same Go scope (struct, endpoint, namespace) can produce the same identifier through toCamel"
  - "Wire-side serialization (json tags, URL params, GraphQL selections) must continue to read the original Name regardless of any disambiguation"
  - "Golden output must stay byte-stable across regenerations even when source names are non-unique after sanitization"
tags:
  - identifier-collision
  - code-generation
  - go-identifiers
  - deduplication
  - ident-name
  - generator-pre-pass
  - deterministic-suffixing
related_components:
  - generator
  - templates
  - spec
---

# Generator identifier collisions: dedupe with a hidden IdentName field and a late pre-pass

## Context

The generator emits Go source from spec data. Spec field names — query params, body fields, struct field names — are written by API owners for wire-side semantics (URL keys, JSON object keys), not Go identifier legality. When `toCamel` normalizes two spec names to the same string, the generator emits two `var flag<X>` declarations or two struct fields with identical names, which fail to compile.

The codebase has reached for this collision problem three separate times across different spec classes:

- **#275 F-2** — endpoint query/path params (Twilio's `StartTime`, `StartTime>`, `StartTime<` all collapse to `StartTime`).
- **#287** — request body fields (`start_time` and `StartTime` both yield `bodyStartTime`).
- **#697** — shared-type struct fields (GitHub Reactions `+1` and `-1` both yield `V1`).

Each recurrence reached for the same structural pattern: a hidden override field, a generator pre-pass, a fallback helper. After three instances, the pattern is durable enough to write down so the next collision class doesn't re-derive it.

## Guidance

The pattern has three fixed components.

### 1. An `IdentName string` override field on the spec struct, tagged for invisibility

```go
// internal/spec/spec.go
type Param struct {
    Name      string
    // ...
    IdentName string `yaml:"-" json:"-"`
}

type TypeField struct {
    Name      string
    // ...
    IdentName string `yaml:"-" json:"-"`
}
```

The `yaml:"-" json:"-"` tags mean the field never lands in manuscript YAML, in catalog JSON, or in any agent-authored file. It is purely a generator-internal scratch slot.

### 2. A generator pre-pass that walks the spec and suffixes IdentName on collisions

The pre-pass is a method on `*Generator` invoked from `prepareOutput()`. It keeps a `used` map of camelized identifiers already claimed in the current scope. The first occurrence of a camel form keeps `IdentName` empty. Each subsequent collision tries `Name + "_2"`, `"_3"`, ... until the candidate's camel form is unused.

```go
// internal/generator/type_collision.go (#697)
func uniquifyTypeFieldIdentifiers(fields []spec.TypeField) []spec.TypeField {
    used := make(map[string]struct{}, len(fields))
    out := make([]spec.TypeField, len(fields))
    for i, f := range fields {
        f.IdentName = ""              // idempotence guard
        ident := toCamel(f.Name)
        if _, taken := used[ident]; !taken {
            used[ident] = struct{}{}
            out[i] = f
            continue
        }
        for n := 2; ; n++ {
            candidate := fmt.Sprintf("%s_%d", f.Name, n)
            if _, taken := used[toCamel(candidate)]; !taken {
                f.IdentName = candidate
                used[toCamel(candidate)] = struct{}{}
                out[i] = f
                break
            }
        }
    }
    return out
}
```

The suffix is appended to `Name` and re-camelized, not to the camelized form directly. That preserves the convention that `IdentName` is a *raw name* the same shape as `Name`, just disambiguated.

### 3. A fallback helper in the template FuncMap

```go
func paramIdent(p spec.Param) string {
    if p.IdentName != "" { return p.IdentName }
    return p.Name
}

func typeFieldIdent(f spec.TypeField) string {
    if f.IdentName != "" { return f.IdentName }
    return f.Name
}
```

Templates use the helper for Go identifiers and pass `Name` directly for wire-side serialization:

```go-template
// types.go.tmpl
{{camel (typeFieldIdent .)}} {{goStructType .Type}} `json:"{{.Name}}"`

// command_endpoint.go.tmpl
var flag{{camel (paramIdent .)}} {{goTypeForParam .Name .Type}}
```

### Wiring

The pre-passes run late in `prepareOutput()` (after `DetectAsyncJobs`) so they can take into account any reserved identifiers the generator introduces dynamically (`flagAll` for paginated endpoints, `flagWait*` for async ones). For TypeField, no such reservation exists; the call is unconditional.

```go
// internal/generator/generator.go, prepareOutput()
if err := g.dedupeFlagIdentifiers(); err != nil {
    return err
}
g.dedupeTypeFieldIdentifiers()
```

## Why This Matters

Not following this pattern produces compile failures, not just bad output. Duplicate `var flag<X>` declarations at package scope and duplicate Go struct fields are hard `go vet`/`go build` errors that surface only at the quality-gate stage, after generation has run cleanly.

The critical invariant is that **`Name` is never mutated**. Wire-side serialization reads `Name` in every template:

- `\`json:"{{.Name}}"\`` in struct field tags
- `params["{{.Name}}"] = ...` in URL-query construction
- `path = replacePathParam(path, "{{.Name}}", ...)` in path substitution
- GraphQL `{{.Name}} { ... }` selection blocks

If a future maintainer "fixes" a collision by mutating `Name`, every printed CLI starts shipping wrong API calls — wrong URL keys, renamed JSON tags, broken GraphQL queries — and the failure mode is silent at compile time. The override-field shape exists specifically to prevent that.

The deterministic `_2`, `_3`, ... suffix is also load-bearing: golden output stability depends on the same spec producing the same generated code across runs. Random or set-iteration-ordered suffixes would cause golden drift on every regeneration.

## When to Apply

Add a new dedup pass when **all** of these hold:

1. The generator emits a new class of Go identifier from spec data (a new template loop doing `var <prefix>{{camel .Name}}` or a new struct type whose fields come from spec names).
2. Two or more spec entries in the same Go scope can map to the same `toCamel(...)` form.
3. Wire-side keys (JSON, URL params, GraphQL) must remain on the original `Name`.

Concrete signal: ask "does `toCamel(A) == toCamel(B)` for any plausible pair of entries in the same scope?" If yes, the pattern is needed.

Do **not** apply when:

- The generator authors the name itself (generator-introduced identifiers like `stdinBody`, `flagAll` are reserved explicitly via the dedup pass's reserved sets, not added as authored entries).
- The collision class is already covered by an existing pre-pass — extend the existing one rather than adding a parallel structure (issue #287 extended `flag_collision.go` to body fields rather than creating `body_collision.go`).
- A simpler `toCamel` change would cover the case without dedup. It usually won't — sanitization is intentionally lossy.

## Examples

### Instance 1 — endpoint query/path params (`flag_collision.go`, #275 F-2)

- Dedupes: two params on the same endpoint whose `toCamel(.Name)` collides, or whose cobra flag name collides with a generator-introduced reserved name.
- Pre-pass wired at: `internal/generator/generator.go::prepareOutput` → `g.dedupeFlagIdentifiers()`.
- Helper consumed in: `internal/generator/templates/command_endpoint.go.tmpl` and `command_promoted.go.tmpl` — `var flag{{camel (paramIdent .)}}`.
- Namespace managed: two-pass (params first, then body fields), sharing the cobra flag-name namespace across both.

### Instance 2 — body fields cross-namespace (#287)

- Extended the same `flag_collision.go` / `uniquifyIdentifiers` to body fields as Pass 2 in `dedupeEndpointIdentifiers`. Body fields use the `body<Camel>` Go-identifier namespace and share the cobra flag-name namespace with already-processed params.
- No new file or struct — the pattern absorbed the new collision class by widening the existing pass.

### Instance 3 — shared-type struct fields (`type_collision.go`, #697)

- Dedupes: two `spec.TypeField` entries in the same `spec.TypeDef` whose `toCamel(.Name)` collides.
- Pre-pass wired at: `internal/generator/generator.go::prepareOutput` → `g.dedupeTypeFieldIdentifiers()`.
- Helper consumed in: `internal/generator/templates/types.go.tmpl` — `{{camel (typeFieldIdent .)}}`.
- JSON tag preserves `Name` verbatim: `\`json:"{{.Name}}"\``.

### Test convention — assert non-colliding fields survive unchanged

Every uniquifier test must include a fixture that should *not* be modified and assert it passes through untouched. Without it, a regression that over-eagerly suffixes every field would still pass collision-only tests. The boundary case is the regression guard.

```go
// internal/generator/types_collision_test.go
{
    name: "unrelated fields untouched",
    input: []spec.TypeField{
        {Name: "id"},
        {Name: "name"},
    },
    wantIdents: []string{"Id", "Name"},  // plain toCamel, no IdentName set
},
```

The test should also assert `Name` itself is never mutated across all cases:

```go
for i, f := range out {
    got[i] = toCamel(typeFieldIdent(f))
    assert.Equal(t, tc.input[i].Name, f.Name, "Name must never be mutated")
}
```

(auto memory [claude]: the "include fixtures that should NOT be modified" rule comes from prior find-and-replace test conventions and applies to any rewrite/replace function.)

## Related

- `docs/solutions/design-patterns/dual-key-identity-fields-2026-05-06.md` — shares the structural mechanism (a hidden spec field decouples rendering from the wire name) but solves a different problem. There the override is user-authored at generate time and carries a semantically distinct form of the same value (slug vs. display name). Here the override is generator-computed in a pre-pass and exists solely to resolve identifier collisions; the suffix is mechanical, not semantic.
- GitHub issues: [#275](https://github.com/mvanhorn/cli-printing-press/issues/275) (F-2 param collisions), [#287](https://github.com/mvanhorn/cli-printing-press/issues/287) (body field collisions), [#697](https://github.com/mvanhorn/cli-printing-press/issues/697) (TypeField collisions).
- Source files: `internal/generator/flag_collision.go`, `internal/generator/type_collision.go`, `internal/spec/spec.go` (`Param.IdentName`, `TypeField.IdentName`).
