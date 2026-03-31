# CLI Printing Press - Development Conventions

## Build, Test & Lint

```bash
go build -o ./printing-press ./cmd/printing-press
go test ./...
gofmt -w ./...
golangci-lint run ./...
```

A pre-commit hook runs `gofmt -w` on staged Go files automatically. A pre-push hook runs `golangci-lint`. The same config (`.golangci.yml`: errcheck, govet, staticcheck, unused) runs in CI. To run lint manually: `golangci-lint run ./...`

**Always run `gofmt -w ./...` after writing Go code.** Subagents and code generators often produce valid but unformatted Go. The pre-commit hook catches this for commits in the repo, but code written to external directories (e.g., `~/printing-press/library/`) must be formatted explicitly.

**IMPORTANT: Always use relative paths for build output.** Never build to `/tmp` or any shared absolute path. Multiple worktrees run concurrently and will stomp on each other. Use `./printing-press` exactly as shown above.

## Project Structure

- `cmd/printing-press/` - CLI entry point
- `internal/spec/` - Internal YAML spec parser
- `internal/openapi/` - OpenAPI 3.0+ parser
- `internal/generator/` - Template engine + quality gates
- `internal/catalog/` - Catalog schema validator
- `catalog/` - API catalog entries (YAML) + Go embed package (`catalog.FS`). Adding a YAML file here requires rebuilding the binary
- `skills/` - Claude Code skill definitions
- `testdata/` - Test fixtures (internal + OpenAPI specs)

## Glossary

Key terms used throughout this repo. Several have overloaded meanings — the glossary establishes canonical names to use in conversation and code comments.

**Use the canonical term** (left column) in your own responses so intent stays unambiguous. If the user's phrasing is ambiguous and the distinction affects what action to take — e.g., "publish it" could mean the pipeline step or pushing to the public library repo — ask before acting.

| Canonical term | Meaning |
|----------------|---------|
| **the printing press** / **the machine** | This repo's generator system — the Go binary, templates, skills, and catalog that together produce CLIs. |
| **printed CLI** / **`<api>-pp-cli`** | A CLI produced by the printing press (e.g., `notion-pp-cli`). The `-pp-` infix avoids collisions with official vendor CLIs. When someone says "the CLI" without qualification, they almost always mean a printed CLI. Use "printed CLI" in your responses to keep it clear. |
| **the printing-press binary** | The Go binary built from `cmd/printing-press/`. Commands: `generate`, `verify`, `emboss`, `scorecard`, `publish`, etc. Always say "printing-press binary" or "generator binary" — never just "the CLI" — when referring to this. |
| **spec** | The API contract that drives generation — OpenAPI 3.0+ YAML/JSON, GraphQL SDL, or internal YAML format. Can come from catalog, URL, local file, or sniff discovery. |
| **API slug** | Normalized API name derived from the spec title via `cleanSpecName()`. Directory key in manuscripts (`manuscripts/<api-slug>/`). The CLI name is `<api-slug>-pp-cli`. Distinct from the CLI name — don't use them interchangeably. |
| **brief** | The output of the machine's research phase (Phase 1) — a condensed doc covering API identity, competitors, data layer, and product thesis. Stored in `manuscripts/<api>/<run>/research/`. Drives all downstream decisions. |
| **crowd sniff** / **sniff** | Discovery technique that scrapes npm, PyPI, and GitHub for unofficial API clients to learn undocumented endpoints, auth patterns, and rate limits. Produces a `discovery/` manuscript with sniff-report.md, HAR captures, and unique-paths.txt. Used when no official spec exists or to supplement one. |
| **manuscript** | The full archive of a generation run. Contains three subdirectories: `research/` (briefs, spec analysis), `proofs/` (dogfood, verify, scorecard results), and optionally `discovery/` (sniff artifacts). Stored at `~/printing-press/manuscripts/<api-slug>/<run-id>/`. |
| **emboss** | A second-pass improvement cycle for an already-printed CLI. Audits baseline, re-researches, identifies top improvements, rebuilds, re-verifies, reports delta. Subcommand: `printing-press emboss <api>`. Still active — not deprecated. |
| **polish** | Targeted fix-up of a printed CLI (distinct from emboss's full cycle). Skill: `/printing-press-polish`. The retro improves the machine; polish improves the printed CLI. |
| **retro** / **retrospective** | Post-generation analysis of *the machine itself* — not the printed CLI. Identifies systemic improvements to templates, the Go binary, skill instructions, or catalog. Output goes to `docs/retros/` and `manuscripts/<api>/<run>/proofs/`. |
| **quality gates** | 7 mechanical static checks every printed CLI must pass: go mod tidy, go vet, go build, binary build, `--help`, version, doctor. These are build-time checks — see **verify** for runtime testing. |
| **verify** | Runtime behavioral testing of a printed CLI — runs every command against the real API (read-only) or a mock server. Produces PASS/WARN/FAIL verdicts. Has `--fix` mode for auto-patching. Distinct from quality gates (static) and dogfood (structural). |
| **dogfood** | Generation-time structural validation of a printed CLI against its source spec. Catches dead flags, invalid API paths, auth mismatches. Subcommand: `printing-press dogfood`. Compare with **doctor** (shipped in the CLI for end-users) and **verify** (runtime behavioral). |
| **shipcheck** | The three-part verification block that gates publishing: dogfood + verify + scorecard, run together. All three must pass before a printed CLI ships. |
| **scorecard** / **scoring** | Two-tier quality assessment. Tier 1: infrastructure (12 dimensions, 60 pts). Tier 2: domain correctness (6 dimensions, 40 pts). Total /100 with letter grades. Subcommand: `printing-press scorecard`. |
| **doctor** | Self-diagnostic command shipped inside every printed CLI for end-users to run. Checks environment, auth config, and connectivity at the user's runtime. Unlike dogfood (which validates at generation time), doctor runs post-install. |
| **local library** | `~/printing-press/library/<cli-name>/` — where printed CLIs land after a successful run. Local directory, not a git repo. |
| **public library repo** | The GitHub repo [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) — public catalog of finished CLIs organized by category. `/printing-press-publish` pushes here. |
| **publish (pipeline)** | The pipeline step that moves a working CLI into the local library and writes the `.printing-press.json` provenance manifest. |
| **publish (to public library repo)** | The skill-driven workflow (`/printing-press-publish`) that packages a local library CLI and creates a PR in the public library repo. |
| **provenance** / **`.printing-press.json`** | Manifest written to each published CLI's root. Contains generation metadata: spec URL, checksum, run ID, printing-press version, timestamp. Makes the directory self-describing. |
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

Every commit and PR title must include one of these scopes. The `PR Title` action enforces this.

**Breaking changes** use `!` after the scope: `feat(cli)!: rename catalog command to registry`. This triggers a major version bump.

**Version bump rules** (release-please reads these from commit prefixes):
- `fix(scope):` → patch (0.4.0 → 0.4.1)
- `feat(scope):` → minor (0.4.0 → 0.5.0)
- `feat(scope)!:` or `BREAKING CHANGE:` footer → major (0.4.0 → 1.0.0)
- `docs:`, `chore:`, `refactor:`, `test:` → included in next release but don't trigger a bump alone

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
- category must be: developer-tools, monitoring, cloud, project-management, productivity, social-and-messaging, sales-and-crm, marketing, payments, auth, commerce, ai, media-and-entertainment, devices, other
- tier must be: official or community

## Testing

**When you change code, check for a `_test.go` file in the same package.** If one exists, read it — your change likely requires a test update. If tests fail after your change, investigate whether it's a bug in your code or a stale test — don't just delete.

Add tests for new non-trivial logic. Match the package's existing style (typically table-driven with `testify/assert`). Skip tests for CLI glue, trivial wrappers, and code only meaningfully tested via integration (`FULL_RUN=1`).

Run `go test ./...` before considering your work done.

## Quality Gates

Generated CLIs must pass 7 gates: go mod tidy, go vet, go build, binary build, --help, version, doctor.

## `~/printing-press/` Layout

Generated artifacts live under the user's home directory, not in this repo.

- `library/<cli-name>/` — Published CLIs (e.g., `notion-pp-cli`). Directory name matches the derived CLI name from `naming.CLI()`.
- `manuscripts/<api-slug>/` — Archived research and verification proofs, keyed by API slug (e.g., `notion`), not CLI name. One API can have multiple runs.
- `.runstate/<scope>/` — Mutable per-workspace state (current run, sync cursors). Scoped by repo basename + hash.

The API slug is derived by the generator from the spec title (`cleanSpecName`), not manually chosen. The CLI name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it — names with periods (cal.com, dub.co) normalize differently than you'd guess.

The `-pp-` infix exists to avoid colliding with official CLIs. `notion-pp-cli` can coexist with whatever `notion-cli` Notion ships themselves.

## Internal Skills

`.claude/skills/` contains internal skills for developing the printing press itself (e.g., `printing-press-retro`). These load automatically when Claude Code is started from inside this repo — no setup needed.

If you're running Claude Code from a different directory and need these skills available, install them globally:

```bash
.claude/scripts/install-internal-skills.sh
```

This copies the internal skills to `~/.claude/skills/`.
