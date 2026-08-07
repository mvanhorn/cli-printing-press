---
title: "Existing manifest wins over re-derivation for identity fields in regen paths"
date: 2026-05-12
category: conventions
module: cli-printing-press-generator
problem_type: convention
component: generator
severity: high
applies_when:
  - "Writing a recurring regen / sync command (mcp-sync, regen-merge, future per-CLI rewriters) that reloads the spec and re-emits identity-bearing surfaces"
  - "An identity field (api_name, owner, printer, display_name, env-var prefix) was set by the user at generate time and persisted to .printing-press.json"
  - "The spec parser would otherwise re-derive that field from the spec — info.title slug, copyright header parse, git config — every time the regen runs"
  - "The persisted manifest value and the re-derived value can disagree, and downstream emitters care which one wins"
tags:
  - preserve-on-regen
  - manifest-authority
  - identity-resolution
  - mcp-sync
  - regen-merge
  - naming
related_components:
  - generator
  - mcp-sync
  - regenmerge
---

# Existing manifest wins over re-derivation for identity fields in regen paths

## Context

The Printing Press's per-CLI surfaces include several identity fields that the user chooses at generate time (or that the generator chooses on the user's behalf from the operator's environment) and that persist forever in `.printing-press.json`:

| Field | Where it's first set | What pins it |
|---|---|---|
| `api_name` | `generate --name foo` | Manifest `api_name`, CLI binary directory name (`cmd/foo-pp-cli/`), README config path |
| `owner` | `generate` reads copyright header / git config | Manifest `owner`, copyright headers across every emitted .go file |
| `printer` | `generate` reads `git config github.user` | Manifest `printer`, README byline link |
| `display_name` | spec `x-display-name`, catalog entry, or info.title fallback | Manifest `display_name`, MCPB display, MCP server identity |

Every subsequent regen — `mcp-sync`, `regen-merge`, the per-CLI sweep tools — also reloads the spec, and the spec parser is happy to re-derive each of those fields from primary sources: `info.title` for the slug, the operator's environment for owner/printer. When those re-derivations disagree with the persisted manifest, downstream emitters (MCP surface generation, README rendering, tools-manifest emission) cascade the wrong value across many files.

Two prior incidents made this concrete:

- The **authorship retrofit** (see related doc below) flipped every `author:` field to the sweep operator on a multi-author library because the sweep trusted `git config user.name` over the existing copyright headers.
- The **telegram CLI rename** ([#1050](https://github.com/mvanhorn/cli-printing-press/issues/1050)) flipped `mcp_binary`, `tools-manifest.json:api_name`, `manifest.json:name`/`entry_point`, the README config-path prose, and produced stray `cmd/telegram-bot-pp-{cli,mcp}/` directories on every `mcp-sync` run because the spec's `info.title` "Telegram Bot API" slugified to `telegram-bot`, overwriting the operator's chosen `--name telegram`.

Same shape, different field. The fix shape is the same too.

## Guidance

Any regen path that reloads a spec and writes identity-bearing surfaces must consult the existing `.printing-press.json` first and prefer its value over re-derivation. The persisted manifest is authoritative; the spec derivation is the **fallback for entries the manifest doesn't cover** (older CLIs predating the field, fresh generates, library entries with no manifest yet).

Mirror the resolution order already established for `owner` and `printer`:

1. **Existing manifest field** — if `.printing-press.json` carries a non-empty value, use it verbatim
2. **Spec / environment derivation** — only when the manifest is missing or the field is empty
3. **Soft-fallback warning** — when the derivation produces a sentinel ("USER", empty slug), emit stderr and let publish-side validation fail closed

```go
// internal/pipeline/mcpsync/sync.go — the parsed.Name fix for #1050

func applyManifestNameOverride(cliDir string, parsed *spec.APISpec) string {
    if parsed == nil {
        return ""
    }
    m, err := pipeline.ReadCLIManifest(cliDir)
    if err != nil {
        return ""
    }
    if m.APIName == "" || m.APIName == parsed.Name {
        return ""
    }
    prior := parsed.Name
    if parsed.Config.Path == fmt.Sprintf(defaultConfigPathFormat, naming.CLI(prior)) {
        parsed.Config.Path = fmt.Sprintf(defaultConfigPathFormat, naming.CLI(m.APIName))
    }
    parsed.Name = m.APIName
    return prior
}
```

The function does three things by design and one by structure:

1. **Read the manifest first.** No spec touching until the manifest answer is known.
2. **Override `parsed.Name`** when the manifest disagrees. Downstream emitters all derive from `parsed.Name`, so a single point of override fixes every surface at once.
3. **Cascade the override into spec-shaped Config.Path.** README config prose still includes the platform-default `parsed.Config.Path`; if the spec parser emitted the default config-path shape, migrate the slug along with the name. Hand-customized paths are left alone, and runtime relocation now flows through the generated path resolver.
4. **Fall through silently** when the manifest is missing, the field is empty, or values already agree. Older CLIs predating the manifest still work via the legacy `reconcileSpecNameWithDir` path that uses the directory basename.

The `owner` and `printer` parallels use the same shape — see `resolveOwnerForExisting` (and its `printer` counterpart) in [`internal/pipeline/climanifest.go`](../../../internal/pipeline/climanifest.go) and the AGENTS.md "Owner vs OwnerName" / "Printer vs PrinterName" sections. The dual-key (slug / display) structure described there isn't directly relevant to `api_name` (which is single-key), but the **preserve-on-regen** tier ordering is the same.

## Why This Matters

Three failure modes the manifest-first approach prevents:

1. **Silent identity flips on every sync.** Without the preempt, `mcp-sync` regenerates name-bearing surfaces against the spec slug every run. A CLI generated with `--name telegram` against the "Telegram Bot API" spec silently becomes `telegram-bot` in the MCPB manifest, the tools manifest, and the README config path — but the actual built binary is still `telegram-pp-mcp`, so the install instructions point at a binary that doesn't exist. Polish detects most of the drift; the manifest field can re-flip silently between polish and publish.
2. **Cascading wrong values across many files.** The slug isn't just one field. `parsed.Name` flows into `cmd/<slug>-pp-cli/main.go` (the entry-point package dir), `mcp_binary` in the manifest, `api_name` in tools-manifest.json, `Server.EntryPoint: "bin/<slug>-pp-mcp"` in manifest.json, and README config-path prose. Fixing each emitter individually is fragile — a future emitter added without coordination silently reintroduces the bug.
3. **Multi-author trust damage.** When the re-derivation default flips an attribution field (owner, printer), the consequence is no longer "wrong path string" but "this tool just claimed someone else's work for whoever ran the sweep." The authorship retrofit incident bit hard enough that the curated per-CLI map (see related doc) was needed to clean up. The lesson generalized: the same default class of bug exists for any identity field, and the prevention is the same shape.

The "use derivation as the default" instinct is not malicious — it's the path of least resistance when you write the spec parser, because the spec is the obvious source. The lesson is to recognize that, after the user has chosen a value once and the system has persisted it, the persisted value outranks every re-derivation.

## When to Apply

- Adding a new identity-bearing field to `.printing-press.json` that downstream emitters consume
- Writing a new regen / sync / sweep tool that reloads a spec and re-emits per-CLI surfaces
- Refactoring an existing emitter that derives an identity field from the spec — confirm the regen path consults the manifest first
- Reviewing a change that touches `naming.CLI`, `naming.MCP`, `cleanSpecName`, or any helper that converts a spec value into a slug used in file paths or manifest fields

Don't apply when:

- The field genuinely derives from the spec at every read with no user-chosen-value semantics (e.g., `BaseURL`, `Version`, endpoint counts)
- The field is intentionally derived (a computed display string built from multiple spec fields, where preserving an older value would be wrong)
- The regen is a one-shot rewrite where the operator explicitly wants to overwrite the persisted value (publish-side `--force-rewrite` or similar; document the deviation at the call site)

## Examples

### Anti-pattern — re-derive every time

```go
// mcp-sync before the fix: parsed.Name comes from spec parsing every run.
// Whatever info.title's slug is, that's what propagates forward.
parsed, err := loadArchivedSpec(cliDir)  // parsed.Name = "telegram-bot"
if err != nil {
    return Result{}, err
}
gen := generator.New(parsed, cliDir)
gen.GenerateMCPSurface()           // emits cmd/telegram-bot-pp-mcp/main.go
pipeline.WriteToolsManifest(cliDir, parsed)  // api_name: "telegram-bot"
pipeline.WriteMCPBManifest(cliDir)           // manifest name: "telegram-bot-pp-mcp"
```

Result: every `mcp-sync` run regenerates against the title-derived slug regardless of what the user chose at generate time. Polish detects some drift but can't reliably fix the manifest field. Publish ships a `manifest.json` whose `entry_point` points at a binary name that doesn't exist on disk.

### Pattern — preempt with the manifest before downstream emitters run

```go
parsed, err := loadArchivedSpec(cliDir)  // parsed.Name = "telegram-bot"
if err != nil {
    return Result{}, err
}
// Preempt: the manifest's api_name outranks the spec-derived slug.
if prior := applyManifestNameOverride(cliDir, parsed); prior != "" {
    fmt.Fprintf(os.Stderr,
        "mcp-sync: using manifest api_name %q over spec-derived slug %q\n",
        parsed.Name, prior)
}
// parsed.Name is now "telegram" — downstream emitters all derive correctly.
gen := generator.New(parsed, cliDir)
gen.GenerateMCPSurface()           // emits cmd/telegram-pp-mcp/main.go
pipeline.WriteToolsManifest(cliDir, parsed)  // api_name: "telegram"
pipeline.WriteMCPBManifest(cliDir)           // manifest name: "telegram-pp-mcp"
```

Result: the user-chosen slug wins across every emitted surface. The override falls through silently for CLIs without a manifest, so the legacy path stays intact.

### Pattern — test the cascade end-to-end, not just the override helper

```go
func TestSyncManifestAPINameWinsOverSpecDerivedSlug(t *testing.T) {
    // Generate with --name telegram, then rewrite spec.yaml so the
    // archived slug becomes "telegram-bot" — the exact #1050 shape.
    // ...

    result, err := Sync(cliDir, Options{Force: true})
    require.NoError(t, err)

    // Every name-bearing surface must keep tracking the manifest's api_name.
    var prov pipeline.CLIManifest
    // ...
    assert.Equal(t, "telegram", prov.APIName)
    assert.Equal(t, "telegram-pp-mcp", prov.MCPBinary)
    assert.Equal(t, "bin/telegram-pp-mcp", manifest.Server.EntryPoint)

    // And no stray cmd/<bad-slug>-pp-{cli,mcp}/ directories.
    _, err = os.Stat(filepath.Join(cliDir, "cmd", "telegram-bot-pp-cli"))
    assert.True(t, os.IsNotExist(err))
}
```

Unit tests of the override helper alone aren't enough — the surfaces are where the bug bites, so the test must assert through to the on-disk manifests and the absence of stray directories.

## Related

- [`docs/solutions/conventions/preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md`](preserve-original-authorship-in-multi-author-retrofits-2026-05-06.md) — the authorship-retrofit lesson; same shape, applied to `Owner`/`OwnerName` retrofits via a curated map. Together with this doc, three identity fields now use the manifest-as-authority pattern.
- [`docs/solutions/design-patterns/snapshot-merge-with-ast-classifier-for-force-regen-2026-05-10.md`](../design-patterns/snapshot-merge-with-ast-classifier-for-force-regen-2026-05-10.md) — the broader principle that regen must preserve user-chosen identity over re-derivation from environment/spec.
- [`docs/solutions/design-patterns/dual-key-identity-fields-2026-05-06.md`](../design-patterns/dual-key-identity-fields-2026-05-06.md) — the slug-vs-display dual-key model used for Owner/OwnerName and Printer/PrinterName. Not directly relevant to single-key `api_name`, but the preserve-on-regen tier ordering applies.
- AGENTS.md "Owner vs OwnerName" and "Printer vs PrinterName" sections — the canonical reference for resolution-order rules across identity fields.
- [#1050](https://github.com/mvanhorn/cli-printing-press/issues/1050) — the originating issue.
