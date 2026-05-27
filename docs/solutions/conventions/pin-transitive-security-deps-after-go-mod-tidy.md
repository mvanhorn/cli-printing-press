---
title: "Pin transitively-pulled security deps after go mod tidy, not via go.mod template conditions"
date: 2026-05-27
category: conventions
module: cli-printing-press-generator
problem_type: convention
component: tooling
severity: medium
applies_when:
  - "A printed CLI fails govulncheck on a transitively-pulled dependency (e.g. golang.org/x/net) and fresh prints should ship the patched version"
  - "Deciding whether to pin a security-sensitive transitive dependency via a go.mod.tmpl condition or a post-tidy bump"
  - "A mechanical library-wide sweep trips the changed-module govulncheck gate on pre-existing dependency drift"
related_components:
  - generator
  - go_mod
  - govulncheck
tags:
  - dependencies
  - govulncheck
  - golang-x-net
  - go-mod-tidy
  - transitive-dependencies
  - security
  - born-clean
---

# Pin transitively-pulled security deps after `go mod tidy`, not via go.mod template conditions

## Context

`golang.org/x/net` advisories (GO-2026-5025..5030) showed up in govulncheck across dozens of published CLIs. `x/net` is never a direct dependency of a printed CLI — it is dragged in transitively by several optional features that live in different places:

- `surf` (browser HTTP transport) → `golang.org/x/net/html/charset`
- `goquery` (search backends, e.g. DuckDuckGo) → `golang.org/x/net/html`
- `kooky` (cookie-auth extraction) → `golang.org/x/net`
- `chromedp` (auth0 SPA) → `golang.org/x/net`
- `net/html` extraction → `golang.org/x/net`

The generator's `go.mod.tmpl` *did* pin `golang.org/x/net v0.55.0`, but only behind `{{- if .HasHTMLExtraction}}`. Every other puller resolved whatever `x/net` its dependency happened to require, which drifted below the patched release. The natural instinct — "broaden the template condition to cover the other pullers" — is wrong for two independent reasons, which is what this convention records.

## Guidance

**For a transitively-pulled dependency that must satisfy a version floor, pin it with a post-`go mod tidy` bump, not a `go.mod.tmpl` condition.** In this repo that is `ensureSafeXNet` (`internal/generator/xnet_guard.go`), run as a gate immediately after `go mod tidy` and before `govulncheck`:

```go
func ensureSafeXNet(dir string) error {
    out, err := runCommand(dir, qualityGateTimeout, "go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/net")
    if err != nil {
        // go list -m exits non-zero when x/net isn't in the module graph; treat all
        // non-zero exits as "not present" — the downstream govulncheck gate will surface
        // any genuine go list failure as a build error.
        return nil
    }
    current := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
    if !semver.IsValid(current) || semver.Compare(current, safeXNetVersion) >= 0 {
        return nil
    }
    if _, err := runCommand(dir, qualityGateTimeout, "go", "get", "golang.org/x/net@"+safeXNetVersion); err != nil {
        return err
    }
    _, err = runCommand(dir, qualityGateTimeout, "go", "mod", "tidy")
    return err
}
```

Wire it into every path that finalizes a module's `go.sum` (here: the `Validate()` gate sequence and the plan-generation tidy).

## Why This Matters

Pinning via a `go.mod.tmpl` condition fails two ways at once:

1. **It is fragile and silently incomplete.** Pullers are spread across templates *and* copied-in packages (search backends, auth helpers), so no single condition or set of conditions reliably enumerates them. The original `HasHTMLExtraction`-only gate had already missed `surf`, `goquery`, and `kooky`.

2. **A too-broad condition breaks the `go mod tidy` gate.** If you pin `x/net` for a config that does *not* actually import it, the emitted `go.mod` carries an unused `require`, `go mod tidy` strips it, and the publish tidy-check (which asserts `tidy` is a no-op) fails. So you cannot just pin it everywhere "to be safe."

A post-tidy bump sidesteps both: it runs **after** the real import graph is resolved, so it acts only when the dependency is genuinely present (never an unused require, never a tidy-gate break) and it catches every puller — current and future — with zero condition maintenance. It is exact by construction.

## When to Apply

- A generated Go module pulls a security-sensitive dependency **transitively** and you need to hold a version floor.
- A `go.mod.tmpl` pin would require guessing which feature combinations pull the dependency.
- Don't use this for a **direct** dependency the generator always emits — pin those in `go.mod.tmpl` directly (e.g. `cobra`, `modernc.org/sqlite`).

## Examples

The fix shipped as a born-clean generator change plus a one-time library retrofit — the same shape used for the SQLite DSN and gofmt fixes:

| Concern | Born-clean (generator) | One-time retrofit (library) |
|---|---|---|
| `x/net` floor | `ensureSafeXNet` post-tidy bump (PR #2410) | `go get golang.org/x/net@v0.55.0 && go mod tidy` across 37 CLIs (library PR #881) |
| SQLite DSN syntax | `_pragma=` in `store.go.tmpl` (#2399, #2407) | DSN sweep (library #877) |
| gofmt drift | gofmt after module-path rewrite (#2405) | `gofmt -w` library sweep (library #879) |

A second, reusable observation came out of this: **a mechanical library-wide sweep surfaces pre-existing per-CLI debt through changed-module CI scans.** The govulncheck gate scans only changed modules, so a DSN or gofmt sweep that merely *touched* a CLI dragged its stale `x/net` into scope. That failure is real but pre-existing and orthogonal to the sweep — treat it as advisory and fix it born-clean + retrofit, rather than bundling unrelated dependency bumps into a formatting or DSN PR.

## Related

- mvanhorn/cli-printing-press#2394 (the originating SQLite `SQLITE_BUSY` bug that began this thread) and #2410 (the `ensureSafeXNet` born-clean fix)
- [modernc.org/sqlite DSN pragma syntax silently ignored](../runtime-errors/modernc-sqlite-dsn-pragma-syntax-silently-ignored.md) — the DSN bug from the same investigation
- `internal/generator/xnet_guard.go`, `internal/generator/validate.go`, `internal/generator/plan_generate.go`
