# CLI Printing Press - Development Conventions

## Machine vs Printed CLI — What Are You Optimizing?

This repo contains **the machine** (generator, templates, binary, skills) that produces **printed CLIs**. When fixing bugs or adding features, always ask: is this a machine change or a printed CLI change?

- **Machine changes** (generator, templates, parser, skills) affect every future CLI. They must be generalized — think about how the fix applies across different APIs, spec formats, and auth patterns, not just the CLI you're looking at right now.
- **Printed CLI changes** (code in `~/printing-press/library/<cli>/`) fix one specific CLI. These are fine for targeted improvements but don't compound.

**Default to machine changes.** If a problem shows up in a printed CLI, the first question is: should the generator have gotten this right? If yes, fix the machine so every future CLI benefits. Only fix the printed CLI directly when the issue is genuinely specific to that one API.

**Never change the machine for one CLI's edge case** unless explicitly told to. If a fix only helps Pagliacci but would be wrong for Stripe, it doesn't belong in the generator. Add a conditional with a clear guard, or leave it as a printed-CLI-level fix.

**Do not hardcode one API/site into reusable machine artifacts.** Skills, templates, generator code, prompts, and shared docs must use placeholders or generic phrasing (`<api>`, `<site>`, "the target site") unless the text is explicitly labeled as an example or test fixture. A Product Hunt, Pagliacci, Stripe, etc. name in reusable guidance is usually a bug: it leaks one investigation into every future printed CLI. If a concrete example is useful, put it in an "Example:" paragraph and keep the operational instruction generic.

**When iterating on a printed CLI to discover issues**, note which problems are systemic (retro findings) vs specific. The retro → plan → implement loop exists to feed discoveries from individual CLIs back into the machine.

**When adding a capability that affects scoring**, update the scorer in the same change. The goal is not to inflate scores — it's to ensure the scorer accurately reflects the capability. If you add composed cookie auth but the scorer only recognizes Bearer/Basic, it will penalize a correctly-implemented CLI. Fix the scorer to recognize the new pattern, not to give it a free pass.

### Anti-Reimplementation

A printed CLI wraps an API. It does not replace the API. Novel-feature commands must call the real endpoint, or read from the local SQLite store populated by sync. Anything in between is a reimplementation, and reimplementations are worse than the API they pretend to replace.

Concretely, the generator and review loop reject:

- Hand-rolled response builders that return constants, hardcoded JSON, or struct literals shaped like an API payload
- Endpoint stubs that return `"OK"` or a canned success message without calling the client
- Aggregations computed in-process when the API has an aggregation endpoint
- Enum mappings and reference data synthesized locally when the API returns them

Three carve-outs are legitimate:

- Commands that read from the generated `internal/store` package to join or query sync'd data (the `stale`, `bottleneck`, `health`, `reconcile` family). These are local-data commands, not fake API calls.
- Commands that cache an API response in the store after calling it. Presence of both a client call and a store call is fine.
- Commands whose data is the curated content itself — substitution tables, holiday lists, currency metadata, conversion factors. The data IS the feature; calling an API or hitting the store would be wrong. Opt in by adding the directive `// pp:novel-static-reference` anywhere in the command's source file (typically near the package-level data declaration). The reimplementation check exempts the command on the same footing as the store/client carve-outs.

The rule is enforced in two places. The absorb manifest has a Kill Check (see `skills/printing-press/references/absorb-scoring.md`) that rejects reimplementation candidates before they enter the feature list. Dogfood runs `reimplementation_check` over every built novel-feature command and flags any handler file that shows neither a client call nor a store access (and lacks the static-reference opt-out).

## Build, Test & Lint

```bash
go build -o ./printing-press ./cmd/printing-press
go test ./...
go fmt ./...
golangci-lint run ./...
```

A pre-commit hook runs `gofmt -w` on staged Go files automatically. A pre-push hook runs `golangci-lint`. The same config (`.golangci.yml`: errcheck, govet, staticcheck, unused) runs in CI. To run lint manually: `golangci-lint run ./...`

**Always run `go fmt ./...` after writing Go code.** Use `go fmt`, not raw `gofmt` — `gofmt` doesn't accept Go package patterns, and `gofmt -w .` would walk into `testdata/golden/expected/` and rewrite frozen golden fixtures. `go fmt ./...` skips `testdata` and `vendor` by convention. Subagents and code generators often produce valid but unformatted Go. The pre-commit hook catches this for commits in the repo, but code written to external directories (e.g., `~/printing-press/library/`) must be formatted explicitly.

**IMPORTANT: Always use relative paths for build output.** Never build to `/tmp` or any shared absolute path. Multiple worktrees run concurrently and will stomp on each other. Use `./printing-press` exactly as shown above.

## Golden Output Harness

The golden harness is a byte-level behavior check for deterministic, offline `printing-press` commands and generated artifacts. It complements unit tests by catching user-visible output drift and printed CLI artifact drift.

Run `scripts/golden.sh verify` whenever a change may affect CLI command output, catalog rendering, browser-sniff or crowd-sniff output, generated specs or generated printed CLI files, templates under `internal/generator/templates/`, naming, endpoint derivation, auth emission, manifest generation, scorecard output, or pipeline artifacts.

If a refactor changes machine code but claims behavior is identical, `scripts/golden.sh verify` should pass without fixture updates.

Run `scripts/golden.sh update` only when the behavior/output change is intentional. After updating, inspect the diffs manually and explain in the final response why the golden changes are expected. Never update goldens just to make a failing check pass.

Golden cases must be deterministic, offline, and auth-free. Do not add cases that depend on network access, user credentials or env vars, `~/printing-press`, wall-clock timestamps unless normalized, machine-specific absolute paths unless normalized, or large generated printed CLI trees unless the compared subset is intentional.

To add a case, create `testdata/golden/cases/<case-name>/`, add expected outputs under `testdata/golden/expected/<case-name>/`, and list behaviorally important generated files in `artifacts.txt` when the command creates artifacts. Prefer a small, high-signal artifact subset over snapshotting huge trees.

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

## Glossary

Key terms used throughout this repo. Several have overloaded meanings — the glossary establishes canonical names to use in conversation and code comments.

**Use the canonical term** (left column) in your own responses so intent stays unambiguous. If the user's phrasing is ambiguous and the distinction affects what action to take — e.g., "publish it" could mean the pipeline step or pushing to the public library repo — ask before acting.

**In skills and user-facing output** (GitHub issues, retro documents, confirmation prompts), use **"the Printing Press"** as the system name — never "the machine." Skills run as a plugin without AGENTS.md loaded, so readers won't have this glossary. "The machine" is fine in AGENTS.md, code comments, and developer conversation within this repo.

**Subsystem names are fine alongside the Printing Press name.** When skills produce diagnostic output (retro findings, issue tables, work units), use component names — generator, scorer, skills, binary — to tell developers *where* to fix something. "Fix the Printing Press" is useless as an action item; "fix the scorer — it penalizes cookie auth" is actionable. The Printing Press is the system; the subsystems are how you navigate within it.

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
| **manuscript** | The full archive of a generation run. Contains three subdirectories: `research/` (briefs, spec analysis), `proofs/` (dogfood, verify, scorecard results), and optionally `discovery/` (browser-sniff and crowd-sniff artifacts). Stored at `~/printing-press/manuscripts/<api-slug>/<run-id>/`. |
| **emboss** | A second-pass improvement cycle for an already-printed CLI. Audits baseline, re-researches, identifies top improvements, rebuilds, re-verifies, reports delta. Subcommand: `printing-press emboss <api>`. Still active — not deprecated. |
| **polish** | Targeted fix-up of a printed CLI (distinct from emboss's full cycle). Skill: `/printing-press-polish`. The retro improves the machine; polish improves the printed CLI. |
| **retro** / **retrospective** | Post-generation analysis of *the machine itself* — not the printed CLI. Identifies systemic improvements to templates, the Go binary, skill instructions, or catalog. Output goes to `docs/retros/` and `manuscripts/<api>/<run>/proofs/`. |
| **quality gates** | 7 mechanical static checks every printed CLI must pass: go mod tidy, go vet, go build, binary build, `--help`, version, doctor. These are build-time checks — see **verify** for runtime testing. |
| **verify** | Runtime behavioral testing of a printed CLI — runs every command against the real API (read-only) or a mock server. Produces PASS/WARN/FAIL verdicts. Has `--fix` mode for auto-patching. Distinct from quality gates (static) and dogfood (structural). |
| **dogfood** | Generation-time structural validation of a printed CLI against its source spec. Catches dead flags, invalid API paths, auth mismatches. Subcommand: `printing-press dogfood`. Compare with **doctor** (shipped in the CLI for end-users) and **verify** (runtime behavioral). |
| **cliutil** | The generator-owned Go package emitted into every printed CLI at `internal/cliutil/`. Houses shared helpers meant for agent-authored novel code to import: `cliutil.FanoutRun` for aggregation commands (per-source error collection, bounded concurrency, source-order output), `cliutil.CleanText` for HTML/JSON-LD text normalization, `cliutil.IsVerifyEnv()` for the side-effect short-circuit (see **side-effect command convention**). **Generator-reserved namespace** — agents authoring novel code in Phase 3 must not put their code in `internal/cliutil/` or name their own helpers that collide with cliutil's exports. |
| **side-effect command convention** | Two-part rule for hand-written novel commands that perform visible actions (open browser tabs, send notifications, dial out to OS handlers). (1) Print by default; require explicit opt-in (`--launch`, `--send`, `--play`) to actually act. (2) Short-circuit when `cliutil.IsVerifyEnv()` is true — the verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess, and the env-var check is the floor that catches any command the verifier's heuristic side-effect classifier misses. Documented in `skills/printing-press/SKILL.md` Phase 3 (principle 9). |
| **canonicalargs** | Tiny generator subpackage at `internal/canonicalargs/` exporting `Lookup(name) (string, bool)` for cross-domain positional placeholder names (`since`, `until`, `tag`, `vertical`). Both verify mock-mode dispatch and the SKILL template consult this registry as one step in the lookup chain `spec.Param.Default → canonicalargs → legacy syntheticArgValue switch → "mock-value"`. **Domain-specific names belong in the spec author's `Param.Default`, not here** — anti-pattern: "Never change the machine for one CLI's edge case." |
| **shipcheck** | The three-part verification block that gates publishing: dogfood + verify + scorecard, run together. All three must pass before a printed CLI ships. |
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
| **provenance** / **`.printing-press.json`** | Manifest written to each published CLI's root. Contains generation metadata: spec URL, checksum, run ID, printing-press version, timestamp. `api_name` is the canonical API identity; `cli_name` is the executable name. Makes the directory self-describing. |
| **catalog** | Embedded YAML entries in `catalog/` describing available APIs (name, spec URL, category, tier). Baked into the binary at build time via `catalog.FS`. |
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

## Versioning

**Never manually edit version numbers.** Three files carry the version and release-please keeps them in sync:
- `.claude-plugin/plugin.json` → `version`
- `.claude-plugin/marketplace.json` → `plugins[0].version`
- `internal/version/version.go` → `var Version` (annotated with `x-release-please-version`)

`TestVersionConsistencyAcrossFiles` in `internal/cli/release_test.go` will fail if versions drift.

## Release Process

Releases are fully automated. No manual steps required.

1. **Merge PRs to main** with conventional commit messages / PR titles
2. **release-please opens a release PR** accumulating all changes since the last release, with a generated changelog
3. **Merge the release PR** when ready to cut a release
4. **Automated:** release-please bumps all three version files, creates a git tag, and creates a GitHub release
5. **Automated:** goreleaser builds cross-platform binaries (linux/darwin/windows × amd64/arm64) and attaches them to the release
6. **Users update** via `go install ...@latest` (picks up the new tag) or download binaries from the release

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

## `~/printing-press/` Layout

Generated artifacts live under the user's home directory, not in this repo.

- `library/<api-slug>/` — Published CLIs (e.g., `notion`). Directory is keyed by API slug, not CLI name. The binary inside is still `<api-slug>-pp-cli`.
- `manuscripts/<api-slug>/` — Archived research and verification proofs, keyed by API slug (e.g., `notion`), not CLI name. One API can have multiple runs.
- `.runstate/<scope>/` — Mutable per-workspace state (current run, sync cursors). Scoped by repo basename + hash.

The API slug is derived by the generator from the spec title (`cleanSpecName`), not manually chosen. The CLI binary name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it — names with periods (cal.com, dub.co) normalize differently than you'd guess.

The `-pp-` infix exists to avoid colliding with official CLIs. The binary `notion-pp-cli` can coexist with whatever `notion-cli` Notion ships themselves. The library directory is just `notion/` — the `-pp-cli` suffix only appears on binary names, not directory names.

## Internal Skills

`.claude/skills/` contains internal skills for developing the printing press itself (e.g., `printing-press-retro`). These load automatically when Claude Code is started from inside this repo — no setup needed.

If you're running Claude Code from a different directory and need these skills available, install them globally:

```bash
.claude/scripts/install-internal-skills.sh
```

This copies the internal skills to `~/.claude/skills/`.

## Skill Authoring: Reference File Pattern

Skills use a `references/` directory for content that is only needed during specific phases or conditions. The SKILL.md stays lean with inline pointers (`Read [references/foo.md](...) when X`), and the agent loads the reference file only when the condition is met.

**Why this matters:** SKILL.md content is loaded into the context window for every tool call in the session. A 2,000-line skill burns tokens on every phase — even phases that don't need most of the content. Extracting conditional sections (e.g., browser capture flows only needed when browser-sniffing, codex templates only needed in codex mode) into reference files reduces baseline context by 30-40%.

**What stays inline:** Cardinal rules, decision matrices, phase structure, user-facing prompts — anything the agent needs at all times or to decide whether to load more.

**What gets extracted:** Implementation details for conditional paths: capture tool CLI commands, delegation templates, scoring frameworks, report templates. These are loaded on-demand when the agent reaches the relevant phase gate.
