# CLI Printing Press - Development Conventions

## Build & Test

```bash
go build -o ./printing-press ./cmd/printing-press
go test ./...
```

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
- `internal/cli/root.go` → `var version` (annotated with `x-release-please-version`)

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
- category must be: auth, payments, email, developer-tools, project-management, communication, crm, example
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
