# CLI Printing Press - Development Conventions

## Machine vs Printed CLI

This repo is **the machine** (generator, templates, binary, skills) that produces **printed CLIs**. When fixing a bug or adding a feature, ask: machine change or printed-CLI change?

- **Machine changes** (generator, templates, parser, skills) affect every future CLI; they must generalize across APIs, spec formats, and auth patterns.
- **Printed-CLI changes** (`~/printing-press/library/<api-slug>/`) fix one CLI; they don't compound.

Rules:

- **Default to machine changes.** If a problem appears in a printed CLI, ask first whether the generator should have gotten this right. Only fix the printed CLI directly when the issue is genuinely API-specific.
- **Don't change the machine for one CLI's edge case.** A fix that helps one API but breaks another doesn't belong in the generator — guard it with a clear conditional or leave it as a printed-CLI fix.
- **Don't hardcode API/site names in reusable artifacts.** Skills, templates, generator code, prompts, shared docs must use placeholders (`<api>`, `<site>`, "the target site") unless the text is explicitly an example or test fixture. Concrete names belong in `Example:` paragraphs, never in operational instructions.
- **Update dependent verifiers in the same change.** A new generator capability that affects scoring requires a scorer update; one that changes the MCP surface requires an audit update. Forgetting either half ships a CLI whose advertised contract diverges from what's emitted.

When iterating on a printed CLI to discover issues, label findings as systemic (retro candidate) vs specific (printed-CLI fix). The retro → plan → implement loop feeds discoveries back into the machine.

### Anti-reimplementation

A printed CLI wraps an API; it does not replace one. Novel-feature commands must call the real endpoint or read from the local store populated by sync. Reimplementations are worse than the API they pretend to replace.

Reject:

- Hand-rolled response builders that return constants, hardcoded JSON, or struct literals shaped like an API payload
- Endpoint stubs that return `"OK"` or a canned success message without calling the client
- Aggregations computed in-process when the API has an aggregation endpoint
- Enum mappings and reference data synthesized locally when the API returns them

Carve-outs:

- Commands that read from `internal/store` (the `stale`, `bottleneck`, `health`, `reconcile` family) — local-data, not fake API calls
- Commands that cache an API response in the store after calling it — both a client call and a store call is fine
- Commands whose data is the curated content itself (substitution tables, holiday lists, currency metadata) — opt in via `// pp:novel-static-reference` directive in the command's source file

Enforced by the absorb manifest's Kill Check (`skills/printing-press/references/absorb-scoring.md`) and dogfood's `reimplementation_check`, which flags handler files showing neither a client call nor a store access (without the static-reference opt-out).

## Agent-Native Surface

Every printed CLI exposes two surfaces: a CLI surface for humans and an MCP surface for agents. Any action a user can take should be reachable by an agent, but the surfaces are not identical — operator ergonomics belong on the human-facing CLI, not in an agent's tool catalog.

### Default: expose; skip rules are exceptions

The runtime walker in `internal/mcp/cobratree/` mirrors the Cobra tree at server start, registering every user-facing command as an MCP tool. It skips a command only if one of these applies:

1. **Endpoint mirrors keep typed schemas.** Commands annotated `cmd.Annotations["pp:endpoint"] = "<resource>.<endpoint>"` are already registered as typed tools elsewhere; the walker skips to avoid duplicates.
2. **Framework commands.** Listed by name in `cobratree/classify.go.tmpl`'s `frameworkCommands` set. Two reasons qualify a command for that set: a typed equivalent is strictly better (`sql`, `search`, `context`), or the command is non-functional via MCP (interactive setup, version reporting, local-only state — `auth`, `completion`, `doctor`, `version`, `feedback`, `profile`, `which`, `help`).
3. **Per-command opt-out.** `cmd.Annotations["mcp:hidden"] = "true"` for domain commands that need human-in-the-loop input.

**Store-population commands stay exposed.** `sync`, `stale`, `orphans`, `reconcile`, `load`, `export`, `import`, `workflow`, `analytics` look like operator commands but have real agent value — `sql` and `search` return empty until `sync` populates the store. Hiding either side breaks the contract.

**When in doubt, leave it exposed.** Hiding a command that should be exposed silently breaks contracts; exposing one that should be hidden just adds a low-value tool. The default flips toward exposure because agents must be able to do anything users can.

### Tool safety annotations

MCP hosts use `readOnlyHint` / `destructiveHint` / `idempotentHint` / `openWorldHint` to decide when to ask the user for permission. Missing annotations default to "could write or delete" — every call prompts.

The generator emits annotations automatically:

- **Endpoint mirrors:** from HTTP method. `GET` → read-only + open-world. `DELETE` → destructive + open-world. `POST`/`PUT`/`PATCH` → open-world (writes, not destructive).
- **Built-in tools:** `context`, `sql`, `search` are read-only (no open-world; they read local data).
- **Shell-out tools (runtime walker):** no annotations by default — the walker can't infer from a Cobra command alone. Opt into read-only with `cmd.Annotations["mcp:read-only"] = "true"` for novel commands that don't mutate external state (read-only API queries, cache lookups, derivations).

Wrong annotations are worse than missing ones: the host trusts the claim and stops asking. A `readOnlyHint: true` on a mutating tool is a real bug; a missing annotation is just a permission prompt.

## Build, Test & Lint

```bash
go build -o ./printing-press ./cmd/printing-press
go test ./...
go fmt ./...
golangci-lint run ./...
```

A pre-commit hook runs `gofmt -w` on staged Go files automatically. A pre-push hook runs `golangci-lint`. The same config (`.golangci.yml`: errcheck, govet, staticcheck, unused) runs in CI. Install hooks with `brew install lefthook && lefthook install --reset-hooks-path`; `--reset-hooks-path` clears stale local `core.hooksPath` settings that block hook sync. Avoid `lefthook install --force` unless intentionally overriding a custom hooks path. To run lint manually: `golangci-lint run ./...`

**After writing Go code, format it with `go fmt ./...` before handing back work.** This is intentionally redundant with the pre-commit hook: `gofmt` is idempotent, and the hook is a safety net for commits while agents often stop before committing. Use `go fmt ./...` for repo-wide formatting and `gofmt -w path/to/file.go` only for explicit files. Do not run `gofmt -w ./...` — `gofmt` does not accept Go package patterns. Do not run `gofmt -w .` from the repo root — it can walk into `testdata/golden/expected/` and rewrite frozen golden fixtures. `go fmt ./...` formats package files and skips `testdata` and `vendor` by convention. Code written to external directories (e.g., `~/printing-press/library/`) must be formatted explicitly because repo hooks will not see it.

**IMPORTANT: Always use relative paths for build output.** Never build to `/tmp` or any shared absolute path. Multiple worktrees run concurrently and will stomp on each other. Use `./printing-press` exactly as shown above.

## Golden Output Harness

The golden harness is a byte-level behavior check for deterministic, offline `printing-press` commands and generated artifacts. It complements unit tests by catching user-visible output drift and printed CLI artifact drift.

Use golden tests as refactor confidence rails for the machine. When changing internals, templates, pipeline plumbing, or broad architecture, a passing golden suite tells agents that the externally observable contracts captured by the fixtures did not move. That is the main purpose: preserve stable command output and generated artifact contracts through major machine changes, not exhaustively test every branch.

Run `scripts/golden.sh verify` whenever a change may affect CLI command output, catalog rendering, browser-sniff or crowd-sniff output, generated specs or generated printed CLI files, templates under `internal/generator/templates/`, naming, endpoint derivation, auth emission, manifest generation, scorecard output, or pipeline artifacts.

If a refactor changes machine code but claims behavior is identical, `scripts/golden.sh verify` should pass without fixture updates.

Run `scripts/golden.sh update` only when the behavior/output change is intentional. After updating, inspect the diffs manually and explain in the final response why the golden changes are expected. Never update goldens just to make a failing check pass.

Golden cases must be deterministic, offline, and auth-free. Do not add cases that depend on network access, user credentials or env vars, `~/printing-press`, wall-clock timestamps unless normalized, machine-specific absolute paths unless normalized, or large generated printed CLI trees unless the compared subset is intentional.

Passing `scripts/golden.sh verify` only proves existing fixtures did not drift. It does not prove golden coverage is complete. When adding a new deterministic CLI behavior or artifact contract, explicitly decide whether the golden suite needs a new or expanded case. Add golden coverage when the behavior is user-visible command output or persisted generated artifacts that should remain stable across refactors. Prefer unit tests for narrow helper logic, branchy internals, or cases where a golden snapshot would duplicate a focused package test without proving a CLI-level contract.

Decision rubric:

- **No golden update:** code changed but the captured external behavior is intentionally identical. Run `scripts/golden.sh verify`; it should pass unchanged.
- **Update an existing fixture:** the behavior already covered by a golden case intentionally changed. Run `scripts/golden.sh update`, then inspect and explain the exact expected diff.
- **Add or expand a fixture:** the change creates a new deterministic command output or persisted artifact contract that existing cases do not exercise. Add the smallest fixture that proves that contract.

To add a case, create `testdata/golden/cases/<case-name>/`, add expected outputs under `testdata/golden/expected/<case-name>/`, and list behaviorally important generated files in `artifacts.txt` when the command creates artifacts. Prefer a small, high-signal artifact subset over snapshotting huge trees.

Keep golden artifacts contract-shaped. Snapshot the specific files or output fields that demonstrate the stable behavior. Do not include broad reports, whole generated trees, or incidental diagnostics just because the harness can capture them; unrelated fields make refactors noisy and weaken the signal.

Maintain `testdata/golden/fixtures/golden-api.yaml` as the purpose-built generated-CLI fixture for the Printing Press. When the machine gains deterministic generation capabilities that should survive major refactors — for example new auth shapes, pagination contracts, MCP surfaces, manifest fields, or endpoint naming rules — extend this fixture and add the smallest useful artifact comparison that proves the capability. Do not mutate this fixture for one printed CLI's edge case unless it represents a general machine behavior.

If `verify` fails, inspect `.gotmp/golden/actual/<case-name>/` and the generated `.diff` files. Decide whether the change is a regression or an intentional behavior change. If it is a regression, fix code. If it is intentional, run `scripts/golden.sh update`, review fixture diffs, and mention the golden update in the final summary.

Golden verification does not replace `go test ./...`, `go vet ./...`, `golangci-lint run ./...`, or `go build -o ./printing-press ./cmd/printing-press`. It is an additional check for behavior-sensitive changes and runs in CI as a separate `Golden` workflow, not as part of `go test ./...`.

## Project Structure

- `cmd/printing-press/` - CLI entry point
- `internal/spec/` - Internal YAML spec parser
- `internal/openapi/` - OpenAPI 3.0+ parser
- `internal/generator/` - Template engine + quality gates
- `internal/catalog/` - Catalog schema validator
- `catalog/` - API catalog entries (YAML) + Go embed package (`catalog.FS`). Adding a YAML file here requires rebuilding the binary
- `skills/` - Claude Code skill definitions
- `testdata/` - Test fixtures (internal + OpenAPI specs)
- `docs/PIPELINE.md` - Portable contract for the 9-phase generation pipeline (preflight through ship). Phase names and ordering are authoritative in `internal/pipeline/state.go`; per-phase intent is authoritative in `internal/pipeline/seeds.go`. Update `docs/PIPELINE.md` in the same PR whenever those files change
- `docs/SKILLS.md` - Skill authoring conventions: workflow parity, reference-file pattern, frontmatter fields
- `docs/PATTERNS.md` - Cross-cutting design patterns (deterministic-inventory ledger, etc.)

## Glossary

Key terms used throughout this repo. Several have overloaded meanings — the glossary establishes canonical names to use in conversation and code comments.

**Use the canonical term** (left column) in your own responses so intent stays unambiguous. If the user's phrasing is ambiguous and the distinction affects what action to take — e.g., "publish it" could mean the pipeline step or pushing to the public library repo — ask before acting.

**In skills and user-facing output** (GitHub issues, retro documents, confirmation prompts), use **"the Printing Press"** as the system name — never "the machine." Skills run as a plugin without AGENTS.md loaded, so readers won't have this glossary. "The machine" is fine in AGENTS.md, code comments, and developer conversation within this repo.

**Subsystem names are fine alongside the Printing Press name.** When skills produce diagnostic output (retro findings, issue tables, work units), use component names — generator, scorer, skills, binary — to tell developers *where* to fix something. "Fix the Printing Press" is useless as an action item; "fix the scorer — it penalizes cookie auth" is actionable. The Printing Press is the system; the subsystems are how you navigate within it.

**Default disambiguation conventions.** Several terms below are overloaded; when body prose uses one without qualifier, default to the local form:

- "library" → local library (`~/printing-press/library/<api-slug>/`). The public library is always called out explicitly: "public library" or "public library repo."
- "publish" → in body prose, prefer "the publish step" (pipeline) or "publish to the public library" (skill workflow) when context isn't already established.
- "manifest" → `tools-manifest.json` (the MCP tool catalog). The other manifests (`manifest.json` for plugin metadata, `.printing-press.json` for provenance) are always called by full name.
- "catalog" → the embedded `catalog/` in this repo. The public library's category-organized catalog of finished CLIs is "public library catalog."

| Canonical term | Meaning |
|----------------|---------|
| **the printing press** / **the machine** | This repo's generator system — the Go binary, templates, skills, and catalog that together produce CLIs. |
| **printed CLI** / **`<api>-pp-cli`** | A CLI produced by the printing press (e.g., `notion-pp-cli`). The `-pp-` infix avoids collisions with official vendor CLIs. When someone says "the CLI" without qualification, they almost always mean a printed CLI. Use "printed CLI" in your responses to keep it clear. |
| **the printing-press binary** | The Go binary built from `cmd/printing-press/`. Commands: `generate`, `verify`, `emboss`, `scorecard`, `publish`, etc. Always say "printing-press binary" or "generator binary" — never just "the CLI" — when referring to this. |
| **spec** | The API contract that drives generation — OpenAPI 3.0+ YAML/JSON, GraphQL SDL, or internal YAML format. Can come from catalog, URL, local file, or browser-sniff discovery. Internal YAML specs may set `kind: synthetic` to declare a multi-source CLI where hand-built commands intentionally go beyond the spec; dogfood marks path-validity as skipped and scorecard excludes it from the tier-2 denominator. |
| **API slug** | Normalized API name derived from the spec title via `cleanSpecName()`. Directory key in manuscripts (`manuscripts/<api-slug>/`). The CLI name is `<api-slug>-pp-cli`. Distinct from the CLI name — don't use them interchangeably. |
| **brief** | The output of the machine's research phase (Phase 1) — a condensed doc covering API identity, competitors, data layer, and product thesis. Stored in `manuscripts/<api>/<run>/research/`. Drives all downstream decisions. |
| **browser-sniff** | Browser-driven API discovery. The user captures live traffic via browser automation (browser-use, agent-browser) or DevTools as a HAR; the `browser-sniff` subcommand analyzes the HAR and produces an OpenAPI-compatible spec. Produces a `discovery/` manuscript with `browser-sniff-report.md`, HAR captures, and `browser-sniff-unique-paths.txt`. Use when no official spec exists or to supplement one with endpoints the docs miss. |
| **crowd-sniff** | Discovery technique that scrapes npm, PyPI, and GitHub for unofficial API clients to learn undocumented endpoints, auth patterns, and rate limits. Produces a `discovery/` manuscript with `crowd-sniff-report.md`. Complementary to browser-sniff — community-sourced vs. browser-captured. Used when no official spec exists or to supplement one. |
| **manuscript** | The full archive of a generation run. Contains three subdirectories: `research/` (briefs, spec analysis), `proofs/` (dogfood, verify, scorecard results), and optionally `discovery/` (browser-sniff and crowd-sniff artifacts). Stored at `~/printing-press/manuscripts/<api-slug>/<run-id>/`. The local library is the working copy of the latest successful run for a given API; manuscripts are immutable archives across runs — same `<api-slug>` keys, separate top-level directories. |
| **emboss** | A second-pass improvement cycle for an already-printed CLI. Audits baseline, re-researches, identifies top improvements, rebuilds, re-verifies, reports delta. Subcommand: `printing-press emboss <api>`. Still active — not deprecated. |
| **polish** | Targeted fix-up of a printed CLI (distinct from emboss's full cycle). Skill: `/printing-press-polish`. The retro improves the machine; polish improves the printed CLI. |
| **retro** / **retrospective** | Post-generation analysis of *the machine itself* — not the printed CLI. Identifies systemic improvements to templates, the Go binary, skill instructions, or catalog. Output goes to `docs/retros/` and `manuscripts/<api>/<run>/proofs/`. |
| **quality gates** | 7 mechanical static checks every printed CLI must pass: go mod tidy, go vet, go build, binary build, `--help`, version, doctor. These are build-time checks — see **verify** for runtime testing. |
| **verify** | Runtime behavioral testing of a printed CLI — runs every command against the real API (read-only) or a mock server. Produces PASS/WARN/FAIL verdicts. Has `--fix` mode for auto-patching. Distinct from quality gates (static) and dogfood (structural). |
| **dogfood** | Generation-time structural validation of a printed CLI against its source spec. Catches dead flags, invalid API paths, auth mismatches, and MCP surface parity drift. Subcommand: `printing-press dogfood`. Compare with **doctor** (shipped in the CLI for end-users) and **verify** (runtime behavioral). |
| **cliutil** | The generator-owned Go package emitted into every printed CLI at `internal/cliutil/`. Houses shared helpers meant for agent-authored novel code to import: `cliutil.FanoutRun` for aggregation commands (per-source error collection, bounded concurrency, source-order output), `cliutil.CleanText` for HTML/JSON-LD text normalization, `cliutil.IsVerifyEnv()` for the side-effect short-circuit (see **side-effect command convention**). **Generator-reserved namespace** — agents authoring novel code in Phase 3 must not put their code in `internal/cliutil/` or name their own helpers that collide with cliutil's exports. |
| **cobratree** | The generator-owned Go package emitted into every printed CLI at `internal/mcp/cobratree/`. The MCP server uses it to walk the printed CLI's Cobra command tree at startup and register shell-out tools for user-facing commands that are not already typed endpoint tools. Classification rules and the framework skip list live in `cobratree/classify.go.tmpl`; see **Agent-Native Surface** for when to add to the framework set vs. annotate `mcp:hidden`. **Generator-reserved namespace** — do not hand-author code here. |
| **side-effect command convention** | Two-part rule for hand-written novel commands that perform visible actions (open browser tabs, send notifications, dial out to OS handlers). (1) Print by default; require explicit opt-in (`--launch`, `--send`, `--play`) to actually act. (2) Short-circuit when `cliutil.IsVerifyEnv()` is true — the verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess, and the env-var check is the floor that catches any command the verifier's heuristic side-effect classifier misses. Documented in `skills/printing-press/SKILL.md` Phase 3 (principle 9). |
| **canonicalargs** | Tiny generator subpackage at `internal/canonicalargs/` exporting `Lookup(name) (string, bool)` for cross-domain positional placeholder names (`since`, `until`, `tag`, `vertical`). Both verify mock-mode dispatch and the SKILL template consult this registry as one step in the lookup chain `spec.Param.Default → canonicalargs → legacy syntheticArgValue switch → "mock-value"`. **Domain-specific names belong in the spec author's `Param.Default`, not here** — anti-pattern: "Never change the machine for one CLI's edge case." |
| **mcp-sync** | Subcommand on the printing-press binary (`printing-press mcp-sync <cli-dir>`) that migrates generated MCP surfaces from the old static novel-feature list to the runtime Cobra-tree mirror. It rewrites generated MCP files, adds the root command export when possible, regenerates `tools-manifest.json`, and refuses hand-edited `internal/mcp/tools.go` unless `--force` is passed. |
| **shipcheck** | The verification block that gates publishing: dogfood + verify + workflow-verify + verify-skill + scorecard, run together. Dogfood includes `mcp_surface_parity`, so stale static MCP surfaces block shipping. All legs must pass before a printed CLI ships. |
| **scorecard** / **scoring** | Two-tier quality assessment with a 50/50 weighted composite. Tier 1: infrastructure (16 string-matching dimensions, raw max 160, normalized to 0-50). Tier 2: domain correctness (7 semantic dimensions, raw max 60 when live verify ran, normalized to 0-50). Total /100 with letter grades. Source of truth: `internal/pipeline/scorecard.go` (tier1Max / tier2Max). Subcommand: `printing-press scorecard`. |
| **machine-owned freshness** | Opt-in freshness contract for store-backed printed CLIs using `cache.enabled`. Covered command paths map to syncable resources; in `--data-source auto` they may run a bounded pre-read refresh before serving local data. `--data-source local` never refreshes, `--data-source live` must not mutate the local store, and env opt-out only disables the freshness hook. This is current-cache freshness, not a guarantee of full historical backfill or API-specific enrichment. |
| **doctor** | Self-diagnostic command shipped inside every printed CLI for end-users to run. Checks environment, auth config, and connectivity at the user's runtime. Unlike dogfood (which validates at generation time), doctor runs post-install. |
| **auth doctor** | Subcommand on the printing-press binary (`printing-press auth doctor`). Scans every installed printed CLI's `tools-manifest.json` under `~/printing-press/library/<api>/` and reports env-var status (ok / suspicious / not_set / no_auth / unknown) with redacted fingerprints. Diagnostic only — never gates, never probes the network. Lives in `internal/authdoctor/`. |
| **mcp-audit** | Subcommand on the printing-press binary (`printing-press mcp-audit`). Walks every library CLI and reports transport, tool-design, and per-CLI recommendations for the `mcp:` spec surface introduced in the U1-U3 machine work (remote transport, intent tools, code-orchestration). Diagnostic only — exit 0 regardless of findings. Supports `--json` for machine-readable output. |
| **mcp spec surface** | Opt-in fields on the spec's `mcp:` block introduced April 2026 to reach production agent-hosts: `transport: [stdio, http]` (remote-capable via streamable HTTP), `intents:` (multi-step composed MCP tools), `orchestration: code` (Cloudflare-style thin `<api>_search` + `<api>_execute` surface for 50+ endpoint APIs), `endpoint_tools: hidden` (suppress raw per-endpoint tools). Empty `mcp:` keeps today's stdio-only endpoint-mirror emission byte-compatible. |
| **local library** | `~/printing-press/library/<api-slug>/` — where printed CLIs land after a successful run. Directory is keyed by API slug (e.g., `notion`), not CLI name. Local directory, not a git repo. |
| **public library repo** | The GitHub repo [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) — public catalog of finished CLIs organized by category. `/printing-press-publish` pushes here. |
| **publish (pipeline)** | The pipeline step that moves a working CLI into the local library and writes the `.printing-press.json` provenance manifest. |
| **publish (to public library repo)** | The skill-driven workflow (`/printing-press-publish`) that packages a local library CLI and creates a PR in the public library repo. |
| **provenance** / **`.printing-press.json`** | Manifest written to each published CLI's root. Contains generation metadata: spec URL, checksum, run ID, printing-press version, timestamp. `api_name` is the canonical API identity; `cli_name` is the executable name. Makes the directory self-describing. Distinct from `manifest.json` (plugin metadata) and `tools-manifest.json` (MCP tool catalog). |
| **`manifest.json`** | Claude plugin manifest at the printed CLI root. Carries `display_name`, `description`, `homepage`, version, and other plugin-host fields. Read by Claude Desktop and other MCP-aware hosts when installing the CLI as a plugin. Distinct from `tools-manifest.json` (the MCP tool catalog) and `.printing-press.json` (provenance). |
| **`tools-manifest.json`** | MCP tool catalog at the printed CLI root. For each tool, carries name, description, parameters, and auth metadata. The MCP server reads it at runtime to register typed tools with full schemas; the audit and scorecard pipelines consume it. "The manifest" without qualifier means this file. Distinct from `manifest.json` (plugin metadata) and `.printing-press.json` (provenance). |
| **catalog** (embedded) | Embedded YAML entries in `catalog/` describing available APIs (name, spec URL, category, tier). Baked into the binary at build time via `catalog.FS`. Distinct from the **public library catalog**, which is the category-organized index of finished CLIs in the public library repo. |
| **tier** | Catalog classification: `official` (vendor-maintained spec) or `community` (unofficial/reverse-engineered). Affects risk expectations. |
| **runstate** | Mutable per-workspace state at `~/printing-press/.runstate/<scope>/`. Tracks current run and sync cursors. Distinct from manuscripts, which are immutable archives. |

## Commit Style

**Format:** `type(scope): description` — scope is always required.

**Scopes** (these appear in changelogs and release notes):

| Scope | Covers | Example |
|-------|--------|---------|
| `cli` | Go binary, commands, flags, embedded catalog, docs | `feat(cli): add catalog subcommands` |
| `skills` | Skill definitions (SKILL.md), references, setup contract | `fix(skills): remove repo checkout requirement` |
| `ci` | Workflows, release config, goreleaser | `feat(ci): add release-please` |
| `main` | release-please generated release PRs targeting `main` | `chore(main): release 2.2.0` |

`main` is reserved for release-please PR titles. Human-authored changes should use `cli`, `skills`, or `ci`.

Every commit and PR title must include one of these scopes. The `PR Title` action enforces this.

**Breaking changes** use `!` after the scope: `feat(cli)!: rename catalog command to registry`. This triggers a major version bump.

**Version bump rules** (release-please reads these from commit prefixes):
- `fix(scope):` → patch (0.4.0 → 0.4.1)
- `feat(scope):` → minor (0.4.0 → 0.5.0)
- `feat(scope)!:` or `BREAKING CHANGE:` footer → major (0.4.0 → 1.0.0)
- `refactor(scope):` → included in the next release PR but doesn't trigger a bump alone
- `docs:`, `chore:`, `test:` → don't trigger a bump alone and stay out of release notes by default

**PR titles must follow the same format.** GitHub's "Squash and merge" uses the PR title as the squash commit message, so release-please reads PR titles on main. The `PR Title` GitHub Action (`.github/workflows/pr-title.yml`) enforces this — PRs with invalid titles cannot merge.

## Versioning & Release

Releases are fully automated by release-please + goreleaser; no manual steps. The flow:

1. Merge PRs to main with conventional-commit titles.
2. release-please opens (and updates) a release PR with the accumulated changelog.
3. When ready to ship, merge the release PR. release-please bumps all version files, creates a git tag, opens a GitHub release; goreleaser builds and attaches cross-platform binaries.

**Never manually edit version numbers.** Three files carry the version and release-please keeps them in sync:
- `.claude-plugin/plugin.json` → `version`
- `.claude-plugin/marketplace.json` → `plugins[0].version`
- `internal/version/version.go` → `var Version` (annotated `x-release-please-version`)

`TestVersionConsistencyAcrossFiles` in `internal/cli/release_test.go` fails if they drift.

## Adding Catalog Entries

Catalog entries in `catalog/` must pass `internal/catalog` validation:
- Required fields: name, display_name, description, category, spec_url, spec_format, tier
- spec_url must use HTTPS
- category must be: ai, auth, cloud, commerce, developer-tools, devices, food-and-dining, marketing, media-and-entertainment, monitoring, payments, productivity, project-management, sales-and-crm, social-and-messaging, travel, or other
- tier must be: official or community

## Testing

**When you change code, check for a `_test.go` file in the same package.** If one exists, read it — your change likely requires a test update. If tests fail after your change, investigate whether it's a bug in your code or a stale test — don't just delete.

Add tests for new non-trivial logic. Match the package's existing style (typically table-driven with `testify/assert`). Skip tests for CLI glue, trivial wrappers, and code only meaningfully tested via integration (`FULL_RUN=1`).

Run `go test ./...` before considering your work done.

## Quality Gates

Generated CLIs must pass 7 gates: go mod tidy, go vet, go build, binary build, --help, version, doctor.

## Local Artifacts (`~/printing-press/`)

Generated artifacts live under the user's home directory, not in this repo.

- `library/<api-slug>/` — Local library: printed CLIs the generator has produced (e.g., `notion`). Directory is keyed by API slug, not CLI name. The binary inside is still `<api-slug>-pp-cli`. This is the working copy; the public library is the published-and-curated counterpart (see "Public Library" below).
- `manuscripts/<api-slug>/` — Archived research and verification proofs, keyed by API slug (e.g., `notion`), not CLI name. One API can have multiple runs.
- `.runstate/<scope>/` — Mutable per-workspace state (current run, sync cursors). Scoped by repo basename + hash.

The API slug is derived by the generator from the spec title (`cleanSpecName`), not manually chosen. The CLI binary name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it — names with periods (cal.com, dub.co) normalize differently than you'd guess.

The `-pp-` infix exists to avoid colliding with official CLIs. The binary `notion-pp-cli` can coexist with whatever `notion-cli` Notion ships themselves. The library directory is just `notion/` — the `-pp-cli` suffix only appears on binary names, not directory names.

## Public Library

The public library is the GitHub repo [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) — a curated, category-organized catalog of finished printed CLIs. Users install printed CLIs from here; this is where a CLI goes when it's ready to ship.

**Local → public flow.** A successfully generated printed CLI lives in the local library. The `/printing-press-publish` skill packages a local CLI and opens a PR against the public library repo. Merging that PR is what moves the CLI from "works on this machine" to "users can install it."

**Local-vs-public divergence.** The local library and public library can drift in two ways:

- **Expected divergence.** Some files are intentionally rewritten by the publish step — most notably `go.mod`'s module path. The polish skill's divergence check exempts these.
- **Unexpected divergence.** Local edits since the last publish — polish in progress, manual fixes, mcp-sync regen — that haven't been pushed. The polish skill's divergence check surfaces these so you can decide to either republish or discard the local changes.

Treat the public library as the durable artifact and the local library as the working copy. When users hit a bug, they're hitting the public library's version, not whatever's currently in `~/printing-press/library/`.

## Internal Skills

`.claude/skills/` contains internal skills for developing the printing press itself (e.g., `printing-press-retro`). These load automatically when Claude Code is started from inside this repo — no setup needed.

If you're running Claude Code from a different directory and need these skills available, install them globally:

```bash
.claude/scripts/install-internal-skills.sh
```

This copies the internal skills to `~/.claude/skills/`.

## Skill Authoring

When a machine change alters what an agent should do or what a command guarantees, update the relevant `SKILL.md` in the same change — don't leave the skill as a stale manual workaround for behavior the machine now owns.

Detail in [`docs/SKILLS.md`](docs/SKILLS.md): workflow parity (when machine changes require skill changes), the reference-file pattern (extracting conditional content from SKILL.md), and the `context: fork` / `user-invocable` frontmatter fields.

## Code & Comment Hygiene

### Write-time defaults

- **No speculative future-proofing in comments.** "Structured to absorb additional dimensions if future X needs them" — write the future struct when the future arrives. Today's reader can't act on a comment about hypothetical needs.
- **No dates, incidents, or ticket numbers in code comments.** Belongs in the PR description and commit message, not the code. Comments stay forever; incidents fade.
- **Code comments must be self-contained.** Don't make them load-bearing on in-repo skill prose, plans, or reference files that could be reorganized. RFCs, vendor API docs, and language specs are durable; in-repo prose is not. If you find yourself wanting to link, keep enough context inline that the code reads correctly when the link breaks.
- **Don't restate the field or function name in its comment.** `MCPDescriptionQuality int` does not need `// the score for MCP description quality`. Document WHY (hidden constraints, subtle invariants), not WHAT (the name already says it).
- **Categorical strings → typed const at introduction.** When adding an event kind, finding type, status name, or any string that names a category, declare the const in the same commit even with one call site. The compiler catches typos at every future site, and the const adds two lines today.
- **Single-case switch with default fallthrough → `||`.** If every branch returns the same thing, `switch x { case A, B: return true } return false` is just `return x == A || x == B`. Switch shape implies cases will diverge; if they won't, write the `||`.
- **Parse command inputs once at the entry point.** In a `RunE`, read files / manifests / configs at the entry and pass parsed results into helpers. Don't re-read "for clarity" — the cost compounds when helpers cross-call.
- **UTF-8 safe string truncation.** `s[:n] + "…"` cuts mid-rune on multibyte input. Use rune slicing or an existing truncate helper from the same package.

### Pre-commit: scan the diff

- Near-identical loops or functions that should share a helper
- A compound predicate (e.g., `f.Status != accepted || (requiresX(f.Kind) && missingX(f))`) inlined at 3+ sites that should be a named function
- Parallel `hasX() bool` / `xCount() int` that drifted apart — derive one from the other
- The same string literal repeated across sites where the categorical-const rule above would have applied — the const is cheap to add retroactively if missed at write-time

## Editing AGENTS.md

The "Code & Comment Hygiene" rules apply to this file too. Specifically:

- **No dates, incidents, or ticket numbers in rules.** Justification belongs in the PR introducing the rule, not embedded in it.
- **Don't defend the doc's structure inside the doc.** "We split this honestly because…" doesn't help future readers — write the rule, trust them.
- **Make rules applicable at the moment they fire.** Write-time rules in a write-time section, diff-review rules in a review section. A rule the agent can't apply at the relevant moment is worse than no rule.
- **Examples should be generic or anti-pattern-shaped, not lifted from the specific incident that prompted the rule.**

## Patterns

Cross-cutting design patterns are documented in [`docs/PATTERNS.md`](docs/PATTERNS.md). Notably **Deterministic Inventory + Agent-Marked Ledger** — the shape used by `printing-press tools-audit` for workflows that combine mechanical detection with per-item agent judgment, with four enforcement primitives (pre-decision fields, duplicate-rationale rejection, numeric end-state gate, resume protocol) for cases where bulk-accept is a realistic failure mode.
