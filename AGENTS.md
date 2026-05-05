# CLI Printing Press - Development Conventions

## Machine vs Printed CLI
This repo is **the machine** (generator, templates, binary, skills) that produces **printed CLIs**. When fixing a bug or adding a feature, ask: machine change or printed-CLI change?
- **Machine changes** (generator, templates, parser, skills) affect every future CLI and must generalize across APIs, spec formats, and auth patterns.
- **Printed-CLI changes** (`~/printing-press/library/<api-slug>/`) fix one CLI and do not compound.
- **Default to machine changes.** If a problem appears in a printed CLI, ask first whether the generator should have gotten it right. Only fix the printed CLI directly when the issue is genuinely API-specific.
- **Don't change the machine for one CLI's edge case.** If a fix helps one API but breaks another, guard it with a clear conditional or leave it as a printed-CLI fix.
- **Don't hardcode API/site names in reusable artifacts.** Skills, templates, generator code, prompts, and shared docs must use placeholders (`<api>`, `<site>`, "the target site") unless the text is explicitly an example or test fixture.
- **Update dependent verifiers in the same change.** A new generator capability that affects scoring requires a scorer update; one that changes the MCP surface requires an audit update.
When iterating on a printed CLI to discover issues, label findings as systemic (retro candidate) vs specific (printed-CLI fix). The retro -> plan -> implement loop feeds discoveries back into the machine.

### Anti-reimplementation
A printed CLI wraps an API; it does not replace one. Novel-feature commands must call the real endpoint or read from the local store populated by sync.
- Reject hand-rolled response builders that return constants, hardcoded JSON, or struct literals shaped like an API payload.
- Reject endpoint stubs that return `"OK"` or a canned success message without calling the client.
- Reject aggregations computed in-process when the API has an aggregation endpoint.
- Reject enum mappings and reference data synthesized locally when the API returns them.
- Carve-outs: commands that read from `internal/store`; commands that operate on the local SQLite file via `database/sql`; commands that call the API and then cache to the store; commands whose data is curated static content via `// pp:novel-static-reference`; commands that make a real hidden client call via `// pp:client-call`, but only when the hidden helper performs a real external API call. Do not use `// pp:client-call` for hardcoded payloads, local-only transforms, or fake endpoint stubs.
Enforced by the absorb manifest's Kill Check (`skills/printing-press/references/absorb-scoring.md`) and dogfood's `reimplementation_check`, which flags handler files showing neither a client call nor a store access without an opt-out.

## Agent-Native Surface
Every printed CLI exposes two surfaces: a CLI surface for humans and an MCP surface for agents. Any action a user can take should be reachable by an agent, but operator ergonomics belong on the human-facing CLI, not in an agent's tool catalog.

### Default: expose; skip rules are exceptions
The runtime walker in `internal/mcp/cobratree/` mirrors the Cobra tree at server start and registers every user-facing command as an MCP tool unless one of these applies:
1. Commands annotated `cmd.Annotations["pp:endpoint"] = "<resource>.<endpoint>"` already have typed tools and are skipped to avoid duplicates.
2. Framework commands listed in `cobratree/classify.go.tmpl`'s `frameworkCommands` set are skipped because a typed equivalent is better (`sql`, `search`, `context`) or the command is non-functional via MCP (`auth`, `completion`, `doctor`, `version`, `feedback`, `profile`, `which`, `help`).
3. `cmd.Annotations["mcp:hidden"] = "true"` opts out a domain command that needs human-in-the-loop input.
Store-population commands stay exposed: `sync`, `stale`, `orphans`, `reconcile`, `load`, `export`, `import`, `workflow`, `analytics`. `sql` and `search` return empty until `sync` populates the store. When in doubt, leave it exposed.

### Tool safety annotations
MCP hosts use `readOnlyHint` / `destructiveHint` / `idempotentHint` / `openWorldHint` to decide when to ask for permission. Missing annotations default to "could write or delete."
- Endpoint mirrors: `GET` -> read-only + open-world, `DELETE` -> destructive + open-world, `POST`/`PUT`/`PATCH` -> open-world.
- Built-in tools: `context`, `sql`, `search` are read-only and local-only.
- Runtime walker shell-out tools get no annotations by default. Opt into read-only with `cmd.Annotations["mcp:read-only"] = "true"` for novel commands that only read from the API, the local store, or the CLI tree itself. Skip the annotation when the command can mutate external state (writes via API, store updates, git pushes) or write to user-visible files outside the local cache (commands accepting `--output <file>`, `--repo <dir>`, etc.).
Wrong annotations are worse than missing ones. A false `readOnlyHint: true` on a mutating tool is a real bug; a missing annotation is just a permission prompt.

### Side-effect commands
Hand-written novel commands that perform visible actions (open browser tabs, send notifications, dial out to OS handlers) follow a two-part rule:
1. Print by default; require explicit opt-in (`--launch`, `--send`, `--play`, etc.) to actually act.
2. Short-circuit when `cliutil.IsVerifyEnv()` is true. The verifier sets `PRINTING_PRESS_VERIFY=1` in every mock-mode subprocess; this env-var check is the floor that catches any side-effect command the verifier's heuristic classifier misses.

### Generator-reserved namespaces
`internal/cliutil/` and `internal/mcp/cobratree/` are generator-owned packages emitted into every printed CLI. Do not hand-author code in them and do not name agent-authored helpers that collide with their exports — regen will overwrite the work. Novel-feature code goes in command packages and may import from `cliutil`.

### Typed exit-code verification
`printing-press verify` treats exit `0` as success by default. For commands where a non-zero code is intentional control flow, declare it in Cobra with `Annotations: map[string]string{"pp:typed-exit-codes": "0,2"}`. The verifier reads that annotation first, then falls back to a command-level `Exit codes:` help block. Do not put the whole global failure palette in a command-level help block unless those codes should count as verify-pass for that specific command.

## Build, Test & Lint
```bash
go build -o ./printing-press ./cmd/printing-press
go test ./...
go fmt ./...
golangci-lint run ./...
```
A pre-commit hook runs `gofmt -w` on staged Go files automatically. A pre-push hook runs `golangci-lint`. The same config in `.golangci.yml` runs in CI. Install hooks with `brew install lefthook && lefthook install --reset-hooks-path`; the `--reset-hooks-path` flag clears stale local `core.hooksPath` settings that block hook sync. Avoid `lefthook install --force` unless intentionally overriding a custom hooks path.
After writing Go code, format it with `go fmt ./...` before handing back work. Use `go fmt ./...` for repo-wide formatting and `gofmt -w path/to/file.go` only for explicit files. Do not run `gofmt -w ./...` (gofmt does not accept Go package patterns) or `gofmt -w .` from the repo root (it walks into `testdata/golden/expected/` and rewrites frozen golden fixtures).
Always use relative paths for build output. Never build to `/tmp` or another shared absolute path; use `./printing-press`.

## Generator Output Stability
Run `scripts/golden.sh verify` whenever a change may affect CLI command output, catalog rendering, browser-sniff or crowd-sniff output, generated specs or generated printed CLI files, templates under `internal/generator/templates/`, naming, endpoint derivation, auth emission, manifest generation, scorecard output, or pipeline artifacts.
Never update goldens just to make a failing check pass. Run `scripts/golden.sh update` only when the behavior change is intentional, then inspect the diff and explain it in your final response. See [`docs/GOLDEN.md`](docs/GOLDEN.md) for the decision rubric, fixture conventions, and failure handling.
When adding a new deterministic CLI behavior or generated artifact contract, explicitly decide whether the golden suite needs a new or expanded fixture. A passing `scripts/golden.sh verify` on existing cases does not prove coverage for new auth, pagination, MCP, manifest, naming, or similar deterministic generation behavior.

## Project Structure
- `cmd/printing-press/` - CLI entry point
- `internal/spec/` - Internal YAML spec parser
- `internal/openapi/` - OpenAPI 3.0+ parser
- `internal/generator/` - Template engine + quality gates
- `internal/catalog/` - Catalog schema validator
- `catalog/` - API catalog entries (YAML) + Go embed package (`catalog.FS`). Adding a YAML file here requires rebuilding the binary
- `skills/` - Claude Code skill definitions
- `testdata/` - Test fixtures (internal + OpenAPI specs)
- `docs/PIPELINE.md` - Portable contract for the 9-phase generation pipeline. Update it when `internal/pipeline/state.go` or `internal/pipeline/seeds.go` changes
- `docs/SPEC-EXTENSIONS.md` - Canonical reference for Printing Press-specific OpenAPI `x-*` extensions. Update it when `internal/openapi/parser.go` adds or changes an `Extensions["x-*"]` lookup
- `docs/SKILLS.md` - Skill authoring conventions: workflow parity, reference-file pattern, frontmatter fields
- `docs/PATTERNS.md` - Cross-cutting design patterns
- `docs/GOLDEN.md` - Golden harness decision rubric and fixture conventions
- `docs/GLOSSARY.md` - Canonical terms and the full disambiguation table
- `docs/RELEASE.md` - release-please / goreleaser flow
- `docs/CATALOG.md` - Catalog validation rationale and wrapper-only entry shape
- `docs/ARTIFACTS.md` - Local library, manuscripts, and public-library flow
- `docs/DOCS.md` - Doc-authoring rules, including pointer-rot prevention
- `docs/solutions/` - Documented solutions to past problems (bugs, design patterns, best practices, conventions), organized by category subdir with YAML frontmatter (`module`, `tags`, `problem_type`). Relevant when implementing or debugging in documented areas.

## Naming and Disambiguation
Use canonical terms in your responses so intent stays unambiguous. In skills and user-facing output (GitHub issues, retro documents, confirmation prompts), use **"the Printing Press"** as the system name, never "the machine." Subsystem names (generator, scorer, skills, binary) are fine alongside it. When user phrasing is ambiguous and the distinction affects what action to take, ask before acting.
- "library" -> local library (`~/printing-press/library/<api-slug>/`) unless the public library is called out explicitly
- "publish" -> the publish step (pipeline) unless the public-library workflow is called out explicitly
- "manifest" -> `tools-manifest.json` unless another manifest is named explicitly
- "catalog" -> embedded `catalog/` unless "public library catalog" is stated
See [`docs/GLOSSARY.md`](docs/GLOSSARY.md) for the full term table and disambiguation cases.

## Commit Style
Format: `type(scope): description`. Scope is always required.
- `cli` covers the Go binary, commands, flags, embedded catalog, and docs.
- `skills` covers skill definitions (`SKILL.md`), references, and setup contract.
- `ci` covers workflows, release config, and goreleaser.
- `main` is reserved for release-please generated release PRs targeting `main`.
Breaking changes use `!` after the scope: `feat(cli)!: rename catalog command to registry`.
Version bump rules: `fix(scope):` -> patch; `feat(scope):` -> minor; `feat(scope)!:` or `BREAKING CHANGE:` -> major; `refactor(scope):` is included in the next release PR but does not trigger a bump alone; `docs:`, `chore:`, and `test:` do not trigger a bump alone and stay out of release notes by default.
Every commit and PR title must include one of the allowed scopes. GitHub squash-and-merge uses the PR title as the squash commit message, and `.github/workflows/pr-title.yml` enforces the format.

## Versioning
Releases are automated by release-please. Never manually edit version numbers.
- The plugin version lives in exactly two places and must stay in sync: `.claude-plugin/plugin.json` -> `version`, and `internal/version/version.go` -> `var Version` (annotated `x-release-please-version`; goreleaser injects via ldflags).
- `TestVersionConsistencyAcrossFiles` in [`internal/cli/release_test.go`](internal/cli/release_test.go#L57) fails if those two versions drift.
- Do not add a `version` field to `.claude-plugin/marketplace.json` plugin entries. `TestMarketplaceJSONHasNoPluginVersion` in [`internal/cli/release_test.go`](internal/cli/release_test.go#L81) fails if a reviewer re-adds one.
See [`docs/RELEASE.md`](docs/RELEASE.md) for the merge-the-release-PR flow.

## Adding Catalog Entries
When adding or editing `catalog/*.yaml`, the entry must pass `internal/catalog` validation.
- Required fields: `name`, `display_name`, `description`, `category`, and `tier`, plus `spec_url` and `spec_format` unless the entry is wrapper-only (`wrapper_libraries` is set and `spec_url` is omitted).
- `spec_url`, when present, must use HTTPS.
- `category` must be one of `ai`, `auth`, `cloud`, `commerce`, `developer-tools`, `devices`, `food-and-dining`, `marketing`, `media-and-entertainment`, `monitoring`, `payments`, `productivity`, `project-management`, `sales-and-crm`, `social-and-messaging`, `travel`, or `other`. The validator also accepts `example` as a test-only catch-all; do not use it for real catalog entries.
- `tier` must be `official` or `community`.
- `bearer_refresh`, when present, must include `bundle_url` and `pattern`; `bundle_url` must use HTTPS, and `pattern` must compile as a Go regexp.
- Rebuild the binary after editing; `catalog.FS` is a Go embed.
See [`docs/CATALOG.md`](docs/CATALOG.md) for validation rationale, the wrapper-only entry shape, and bearer-refresh metadata.

## Testing
When you change code, check for a `_test.go` file in the same package. If one exists, read it; your change likely requires a test update. If tests fail after your change, investigate whether it is a bug in your code or a stale test; do not just delete the test.
Add tests for new non-trivial logic. Match the package's existing style (typically table-driven with `testify/assert`). Skip tests for CLI glue, trivial wrappers, and code only meaningfully tested via integration (`FULL_RUN=1`).
Run `go test ./...` before considering your work done.

## Quality Gates
Generated CLIs must pass 7 gates: `go mod tidy`, `go vet`, `go build`, binary build, `--help`, `version`, and `doctor`.

## Local Artifacts
Generated artifacts live under `~/printing-press/`, not in this repo: `library/<api-slug>/`, `manuscripts/<api-slug>/`, and `.runstate/<scope>/`. The API slug is derived by the generator from the spec title (`cleanSpecName`), and the binary name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it. See [`docs/ARTIFACTS.md`](docs/ARTIFACTS.md) for local-vs-public flow and divergence rules.

## Internal Skills
`.claude/skills/` contains internal skills for developing the Printing Press itself (for example `printing-press-retro`). These load automatically when Claude Code is started from inside this repo.
If you are running Claude Code from a different directory and need these skills available, install them globally:
```bash
.claude/scripts/install-internal-skills.sh
```
This copies the internal skills to `~/.claude/skills/`.

## Skill Authoring
When a machine change alters what an agent should do or what a command guarantees, update the relevant `SKILL.md` in the same change; do not leave the skill as a stale manual workaround for behavior the machine now owns.
Detail in [`docs/SKILLS.md`](docs/SKILLS.md): workflow parity, the reference-file pattern, and the `context: fork` / `user-invocable` frontmatter fields.

## Code & Comment Hygiene
### Write-time defaults
- No speculative future-proofing in comments.
- No dates, incidents, or ticket numbers in code comments.
- Code comments must be self-contained; do not make them load-bearing on in-repo skills, plans, or reference prose.
- Do not restate the field or function name in its comment; document why, not what.
- Categorical strings -> typed const at introduction.
- Single-case switch with default fallthrough -> `||`.
- Parse command inputs once at the entry point.
- Use UTF-8-safe string truncation.

### Pre-commit: scan the diff
- Near-identical loops or functions that should share a helper
- A compound predicate inlined at 3+ sites that should be a named function
- Parallel `hasX() bool` / `xCount() int` that drifted apart
- The same string literal repeated across sites where the categorical-const rule should have applied

## Editing AGENTS.md
The "Code & Comment Hygiene" rules apply here too. Keep inline `AGENTS.md` rules command-shaped: trigger, required action or prohibition, concrete values, then a pointer to any longer doc.

**Pointer-rot rule.** When editing a doc under `docs/` that `AGENTS.md` points to, update the inline trigger sentence here in the same PR if applicability changes — a new fire condition, a removed fire condition, or a changed prohibition, enum, file path, test name, or required value. The inline rule is what the agent sees on every turn; the extracted doc is only loaded if the agent follows the pointer.

See [`docs/DOCS.md`](docs/DOCS.md) for the full doc-authoring rules.

## Patterns
Cross-cutting design patterns are documented in [`docs/PATTERNS.md`](docs/PATTERNS.md). Notably **Deterministic Inventory + Agent-Marked Ledger** — the shape used by `printing-press tools-audit` for workflows that combine mechanical detection with per-item agent judgment.
