# Glossary

Operational naming and disambiguation conventions for this repo, plus the implementation-level names — packages, subcommands, on-disk files — that [`CONCEPTS.md`](../CONCEPTS.md) deliberately omits to stay code-free.

For what a **domain noun means** — the Printing Press, printed CLI, spec, brief, manuscript, library, verify, dogfood, scorecard, emboss, polish, retro, creator, and the rest — see [`CONCEPTS.md`](../CONCEPTS.md) at the repo root. This file covers two things CONCEPTS.md does not: (1) how to *refer* to overloaded terms, and (2) the concrete files, packages, and subcommands that back those concepts.

## Naming conventions

Use the canonical term in your own responses so intent stays unambiguous. If the user's phrasing is ambiguous and the distinction affects what action to take, ask before acting.

In skills and user-facing output (GitHub issues, retro documents, confirmation prompts), use **"the Printing Press"** as the system name — never "the machine." Skills run as a plugin without `AGENTS.md` loaded, so readers will not have the inline glossary stub. "The machine" is fine in `AGENTS.md`, code comments, and developer conversation within this repo.

Subsystem names are fine alongside the Printing Press name. When skills produce diagnostic output (retro findings, issue tables, work units), use component names — generator, scorer, skills, binary — to tell developers where to fix something. "Fix the Printing Press" is useless as an action item; "fix the scorer — it penalizes cookie auth" is actionable.

## Default disambiguation conventions

These defaults resolve overloaded words; follow the cross-reference for the full concept.

- "library" → local library (`~/printing-press/library/<api-slug>/`). The public library is always called out explicitly: "public library" or "public library repo." (CONCEPTS: *local library*, *public library*.)
- "publish" → prefer "the publish step" (the pipeline's publish phase) or "publish to the public library" (the `/printing-press-publish` skill workflow) when context is not already established. (CONCEPTS: *publish*, under Flagged ambiguities.)
- "manifest" → `tools-manifest.json` (the MCP tool catalog). The other manifests (`manifest.json` for plugin metadata, `.printing-press.json` for provenance) are always called by full name. (See Implementation reference.)
- "catalog" → qualify the noun. Use "public library catalog" for the category-organized index of finished CLIs, and "tool catalog" for MCP/tool manifests.
- "the CLI" → a printed CLI, not the generator binary. Say "cli-printing-press binary" or "generator binary" for the latter. (CONCEPTS: *printed CLI*; below: *the cli-printing-press binary*.)

On-disk locations for the artifact concepts — local library, manuscripts, runstate — live in [`ARTIFACTS.md`](ARTIFACTS.md), not here.

## Implementation reference

Concrete names that back the concepts — kept here, out of `CONCEPTS.md`, because they are file paths, package names, and subcommands that move as the code moves.

Many concepts are also `cli-printing-press` subcommands (`generate`, `verify`, `dogfood`, `scorecard`, `emboss`, `browser-sniff`, `crowd-sniff`, `device-sniff` / `bluetooth-sniff`, …) or skill workflows (`polish`, `reprint`, `retro`); their *meaning* is in `CONCEPTS.md`. The table below lists names that are **purely** tooling — packages, conventions, diagnostic subcommands, and on-disk files with no standalone domain meaning.

| Term | What it is |
|------|------------|
| **the cli-printing-press binary** | The Go binary built from `cmd/cli-printing-press/`. Commands: `generate`, `verify`, `emboss`, `scorecard`, `publish`, etc. Always say "cli-printing-press binary" or "generator binary" — never just "the CLI." |
| **ble-probe** | The BLE device-sniff probe surface (scan, inspect, read, subscribe, capture write evidence) packaged as a standalone binary built from `cmd/ble-probe/`, for machines without the full Printing Press checkout. Same surface as `cli-printing-press device-sniff ble`; built via `scripts/build-ble-probe.sh`. |
| **cliutil** | Generator-owned Go package emitted into every printed CLI at `internal/cliutil/`. Shared helpers for agent-authored novel code: `cliutil.FanoutRun` (aggregation commands), `cliutil.CleanText` (text normalization), `cliutil.IsVerifyEnv()` (the side-effect short-circuit). **Generator-reserved namespace** — do not hand-author here or shadow its exports. |
| **cobratree** | Generator-owned package at `internal/mcp/cobratree/`. The MCP server walks the printed CLI's Cobra tree at startup and registers shell-out tools for user-facing commands that lack a typed endpoint tool. Classification rules and the framework skip list live in `cobratree/classify.go.tmpl`. **Generator-reserved namespace.** |
| **canonicalargs** | Subpackage at `internal/canonicalargs/` exporting `Lookup(name) (string, bool)` for cross-domain positional placeholders (`since`, `until`, `tag`, `vertical`). Domain-specific names belong in the spec author's `Param.Default`, not here — "never change the machine for one CLI's edge case." |
| **side-effect command convention** | Two-part rule for hand-written novel commands with visible actions: (1) print by default, require explicit opt-in (`--launch`, `--send`, `--play`) to act; (2) short-circuit when `cliutil.IsVerifyEnv()` is true (the verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess). Documented in `skills/printing-press/SKILL.md`. |
| **machine-owned freshness** | Opt-in freshness contract for store-backed printed CLIs using `cache.enabled`. In `--data-source auto`, covered paths may run a bounded pre-read refresh; `--data-source local` never refreshes, `--data-source live` must not mutate the store, and env opt-out only disables the freshness hook. Current-cache freshness, not full historical backfill. |
| **mcp spec surface** | Fields on the spec's `mcp:` block: `transport: [stdio, http]`, `intents:`, `orchestration: code`/`endpoint-mirror`, `endpoint_tools: hidden`. Empty `mcp:` keeps endpoint-mirror emission for small APIs and auto-compiles both transports at or under the remote-transport threshold; larger APIs auto-apply the code-orchestration pattern unless opted out. |
| **mcp-sync** | Binary subcommand (`cli-printing-press mcp-sync <cli-dir>`) migrating generated MCP surfaces from the old static novel-feature list to the runtime Cobra-tree mirror; rewrites generated MCP files, regenerates `tools-manifest.json`, and refuses a hand-edited `internal/mcp/tools.go` without `--force`. |
| **regen-merge** | Binary subcommand (`cli-printing-press regen-merge <cli-dir> --fresh <fresh-dir>`) that AST-classifies each Go file in a published CLI against a fresh tree, applies safe templated overwrites, restores lost `AddCommand` registrations, and merges `go.mod` while preserving the monorepo module path. Lives in `internal/pipeline/regenmerge/`. |
| **auth doctor** | Binary subcommand (`cli-printing-press auth doctor`) scanning every installed CLI's `tools-manifest.json` and reporting env-var status (`ok`/`suspicious`/`not_set`/`no_auth`/`unknown`) with redacted fingerprints. Diagnostic only; never gates or probes the network. Lives in `internal/authdoctor/`. |
| **mcp-audit** | Binary subcommand (`cli-printing-press mcp-audit`) walking every library CLI and reporting transport, tool-design, and per-CLI MCP recommendations. Diagnostic only; exit 0 regardless of findings. Supports `--json`. |
| **`tools-manifest.json`** | MCP tool catalog at a printed CLI's root — per-tool name, description, parameters, auth metadata. "The manifest" without qualifier means this file. Backs the *MCP surface* concept (CONCEPTS.md). |
| **`manifest.json`** | Claude plugin manifest at a printed CLI's root — `display_name`, `description`, `homepage`, version, and other plugin-host fields read when installing the CLI as a plugin. |
| **`.printing-press.json`** | Provenance manifest at a printed CLI's root — spec URL, checksum, run id, printing-press version, timestamp; `api_name` is the canonical API identity, `cli_name` the executable name. Backs the *provenance* concept (CONCEPTS.md). |
