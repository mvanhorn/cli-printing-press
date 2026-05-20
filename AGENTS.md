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

Generated endpoint-mirror commands also gate mutating HTTP verbs (DELETE/POST/PUT/PATCH) at the transport layer in `internal/client/client.go`. Under `PRINTING_PRESS_VERIFY=1` such requests short-circuit with a synthetic `{"__pp_verify_synthetic__":true,"status":"noop","reason":"verify_short_circuit",...}` envelope and never dial. Read-only operations that ride a mutating verb on the wire (GraphQL queries, JSON-RPC reads, POST-based search; codegen-marked `mcp:read-only`) route through `doRead()` and bypass the gate, so verify-mode does not silently break reads on shared-endpoint APIs. The outer command envelope reports `verify_noop: true` and `success: false` so naive validators do not read the noop as a real mutation. `printing-press verify` opts its mock-mode subprocesses back in via `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` so the httptest mock server still receives mutating requests; agents, narrative full-example runs, and ad-hoc operators leave `LIVE_HTTP` unset so mutating requests no-op. Live verifiers (`live_dogfood`, `workflow_verify`) strip both env vars from subprocess env so they cannot inherit verify-mode short-circuiting. `<cli> doctor` surfaces the active verify state as a defense-in-depth diagnosis anchor.


### Long-running commands under live-dogfood
Hand-written novel commands whose happy path is an expensive network operation (full sync loops, content crawlers, bulk archive walks) MUST curtail work when `cliutil.IsDogfoodEnv()` returns true. The `printing-press dogfood --live` runner sets `PRINTING_PRESS_DOGFOOD=1` in every subprocess and applies a flat 30s per-command timeout; without a short-circuit, the happy-path test trips the timeout and the matrix verdict flips to FAIL even when the command itself is healthy. Unlike `IsVerifyEnv`, this does NOT mean "don't hit the network" — dogfood is a real-API matrix. Use it to bound work (paginate once, fetch a bounded sample, honor a smaller `--limit` default), never to substitute mock data for real calls.

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

## Cross-repo dependency: published-library sweep tool

When a change to `internal/generator/templates/readme.md.tmpl` or `skill.md.tmpl` shifts canonical published-library shape — install-block structure, top-of-README section ordering, presence or removal of `## ` sections, frontmatter top-level field set, install command syntax — also update `tools/sweep-canonical/main.go` in [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) so the already-published CLIs can be retrofitted to match. Fresh prints from this generator will produce the new shape automatically, but every existing entry in the public library silently drifts from canonical shape until the sweep retrofit runs.

If you can't make the matching sweep change in the same session, file a tracking issue at https://github.com/mvanhorn/printing-press-library/issues/new before merging the template PR. The issue should include:

1. A link to the template PR here.
2. The shape change(s) the sweep needs to handle. Sweep changes must be idempotent (second run produces zero textual diff) — name the heading boundaries, regex anchors, or section markers the sweep can hang off.
3. Any test additions needed in `tools/sweep-canonical/main_test.go`.

Without the sweep update or a tracking issue, the divergence between fresh prints and existing entries is invisible until someone notices a specific published README looks "old" relative to the rest. The downstream side of this contract (the published library's stance on when to run the sweep, how to scope it, and the `-readme-only` + author-preservation safeties on the sweep tool) is documented in `printing-press-library/AGENTS.md` under "Bulk SKILL.md/README.md retrofits".

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

**`Owner` (slug) vs `OwnerName` (display) — keep them straight.** `APISpec` carries two related but semantically distinct fields. `Owner` is path-safe (e.g. `trevin-chow`) and drives Go module paths and `// Copyright YYYY <slug>.` headers — sanitized in `New()`. `OwnerName` is prose-shaped (e.g. `Trevin Chow`) and flows into Hermes `author:`, README byline, and other human-facing surfaces — preserved verbatim, YAML-escaped at template emission. Resolution paths are deliberately different: `OwnerName` reads raw `git config user.name` only (no `github.user` fallback, no slug sanitization, no `"USER"` default). When `OwnerName` is unset at `Generate()` time, the generator emits a stderr warning and falls back to the slug — non-fatal so the package stays reusable by tests and `mcp-sync`/`regen-merge`. The library-wide sweep tool overrides this code path with its own per-CLI authorship mapping. Don't conflate the two fields when authoring helpers, templates, or test fixtures.

**`Printer` (slug @handle) vs `PrinterName` (display) — parallel pair, distinct from Owner/OwnerName.** `APISpec` and `CLIManifest` carry a second slug-vs-display pair for printer attribution. `Printer` is the GitHub @handle of the human who originally ran the press (e.g. `mvanhorn`), drives the per-CLI README byline link and the library-side registry attribution, and reads `git config github.user`. `PrinterName` is the prose-shaped display name (e.g. `Matt Van Horn`), rendered as the README byline parenthetical, and reads raw `git config user.name`. Resolution is tiered for both: the existing manifest wins over git config, so regens by other contributors do not overwrite the original printer (mirrors `resolveOwnerForExisting`'s preserve-on-regen guarantee). When `Printer` is unset at `Generate()` time, the generator emits a stderr warning and leaves it empty; the publish skill (Step 6) refuses to publish empty or literal `"USER"`/`"user"` sentinel values. Distinct from `Owner`/`OwnerName` (which carry the API-spec / wrapper-author identity); the printer is the human, the owner is the API vendor or wrapper author.

## Issue Work Ownership
Contributor agents without maintainer or admin access must make sure a GitHub issue exists before fixing a bug or behavior change. Maintainers and admins may bypass these issue-ownership rules for maintainer-owned direct work. Do not treat a private plan, external doc, review artifact, or PR body as the only problem statement. Search open and recently closed issues first; reuse an existing issue when one matches instead of filing a duplicate. If no issue exists, open one with enough context for maintainers to understand the bug, scope, and intended fix.

Before implementation, claim the issue: assign it to yourself (or to the GitHub user you are explicitly working on behalf of) and post a short comment stating you are picking it up. Assignment may fail because of permissions; that is fine, but still leave the claim comment. Claim *before* you start coding so duplicate work is prevented at the moment it would otherwise begin; claiming after a PR is open is too late.

If the issue already has an assignee, treat that as active ownership until you can determine otherwise from recent activity or direct confirmation. For a plausibly stale assignment, ask the current assignee by tagging them in an issue comment before taking over or reassigning the issue.

If you stop, abandon, or hand off the work before opening a PR, unclaim the issue: remove the assignment and post a short comment so the next picker-upper knows it is free. You do not need to unclaim on successful completion — a merged PR closes the issue.

## Commit Style
Format: `type(scope): description`. Both type and scope are required.

**Allowed scopes:**
- `cli` covers the Go binary, commands, flags, embedded catalog, and docs.
- `skills` covers skill definitions (`SKILL.md`), references, and setup contract.
- `ci` covers workflows, release config, and goreleaser.
- `main` is reserved for release-please generated release PRs targeting `main`.

**Allowed types:**
- `feat` — new functionality, capability, command, or flag (a user can do something they couldn't before).
- `fix` — corrects incorrect behavior in code that's already shipping.
- `docs` — documentation changes including AGENTS.md, README, doc comments, **and template wording changes that don't alter generator behavior** (e.g., reword an install instruction in `readme.md.tmpl` or `skill.md.tmpl`).
- `refactor` — internal restructuring with no observable behavior change.
- `chore` — build, tooling, dependency, or housekeeping work outside production code.
- `test` — test-only additions or corrections.

**Breaking changes** use `!` after the scope: `feat(cli)!: rename catalog command to registry`. The `!` triggers a major version bump through release-please, so reserve it for changes that *break a downstream contract* — a renamed/removed command, a renamed/removed flag, a removed manifest field, an incompatible config-file shape. **What isn't breaking:** template wording changes, README updates, and generator-output diffs that don't remove or rename a documented surface are `docs(...)` or `fix(...)` — not `feat(...)!`. Even if every printed CLI's output changes on next regen, that alone doesn't qualify as breaking unless something downstream breaks too. The release-versioning consequence of `!` is intentional; if you're unsure, ask before adding it.

**Examples:**
- `feat(cli): add --select flag to all read commands`
- `feat(cli)!: rename catalog command to registry`
- `fix(cli): correct trailing newline in skill.md.tmpl`
- `docs(cli): clarify install instructions in generated README`
- `chore(ci): bump goreleaser to v2.5`

**Version bump rules:** `fix(scope):` -> patch; `feat(scope):` -> minor; `feat(scope)!:` or `BREAKING CHANGE:` -> major; `refactor(scope):` is included in the next release PR but does not trigger a bump alone; `docs:`, `chore:`, and `test:` do not trigger a bump alone and stay out of release notes by default.

Every commit and PR title must include one of the allowed scopes. GitHub squash-and-merge uses the PR title as the squash commit message, and `.github/workflows/pr-title.yml` enforces the format.

## Pull Requests
- Community PRs must keep and complete `.github/pull_request_template.md`.
- Maintainer-owned PRs may use a shorter body and omit the community template sections.
- A maintainer-owned PR is one opened by, or explicitly on behalf of, a trusted maintainer account with write/admin access to this repository.
- Do not treat GitHub's `CONTRIBUTOR` author association as exempt; repeat external contributors still use the community PR template unless a maintainer says otherwise.
- If unsure whether a PR is exempt, keep the template.
See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the human-facing contributor guide and AI / automation disclosure definitions.

## Versioning
Releases are automated by release-please. Never manually edit version numbers.
- Normal feature/fix PRs land through the Mergify queue: add the `ready-to-merge` label when the PR is ready to merge, and let Mergify rebase/test/merge it. Do not use the GitHub merge button for normal PRs once `Mergify Merge Protections` is required on `main`.
- release-please PRs are the release control point. They collect already-merged conventional commits; merge the release PR only when you intend to cut a release. The Mergify config allows release-please PRs to satisfy merge protection without `ready-to-merge`, so maintainers can still merge the release PR manually after CI passes.
- When enabling branch protection for the Mergify queue, require the `Mergify Merge Protections` status check and set `required_status_checks.strict=false`; Mergify owns latest-`main` validation through the queue, and GitHub's strict up-to-date requirement recreates the manual rebase loop.
- The plugin version lives in exactly two places and must stay in sync: `.claude-plugin/plugin.json` -> `version`, and `internal/version/version.go` -> `var Version` (annotated `x-release-please-version`; goreleaser injects via ldflags).
- `TestVersionConsistencyAcrossFiles` in [`internal/cli/release_test.go`](internal/cli/release_test.go#L57) fails if those two versions drift.
- Do not add a `version` field to `.claude-plugin/marketplace.json` plugin entries. `TestMarketplaceJSONHasNoPluginVersion` in [`internal/cli/release_test.go`](internal/cli/release_test.go#L81) fails if a reviewer re-adds one.
See [`docs/RELEASE.md`](docs/RELEASE.md) for the merge-the-release-PR flow.

## Adding Catalog Entries
When adding or editing `catalog/*.yaml`, first decide whether the entry belongs in the curated blueprint catalog. The embedded catalog is not a public-library index or a shortcut for reprinting existing CLIs; reprint from the current local/public library artifact when that is the source of truth. Add catalog entries only when they represent a distinct, reusable Printing Press pattern and have a real user-facing workflow, a reachable maintained source, and a reproducible generation route: vendor spec, docs-derived in-repo spec, verified sniffed spec, or wrapper-only backing that truthfully describes what the generator can do today. Do not add aspirational entries, dead wrappers, unproven private endpoints, personalized app flows without an auth model, duplicate examples of an already-covered pattern, or scrape ideas without live crawl evidence.
- Document provenance in the PR: source URL(s), source type (`official`, `docs`, `sniffed`, `community`, or wrapper-only), live smoke evidence, auth requirements, and what is intentionally out of scope.
- If the entry should make `printing-press generate <name>` work, provide a real `spec_url` or in-repo spec; wrapper-only entries are discovery/backing notes unless the generator has a concrete spec path.
- If catalog output intentionally changes, update `testdata/golden/expected/catalog-list/stdout.txt`.
- The entry must pass `internal/catalog` validation.
- Required fields: `name`, `display_name`, `description`, `category`, and `tier`, plus `spec_url` and `spec_format` unless the entry is wrapper-only (`wrapper_libraries` is set and `spec_url` is omitted).
- `spec_url`, when present, must use HTTPS.
- `category` must be one of `ai`, `auth`, `cloud`, `commerce`, `developer-tools`, `devices`, `food-and-dining`, `maps`, `marketing`, `media-and-entertainment`, `monitoring`, `payments`, `productivity`, `project-management`, `sales-and-crm`, `social-and-messaging`, `travel`, or `other`. The validator also accepts `example` as a test-only catch-all; do not use it for real catalog entries.
- `tier` must be `official` or `community`.
- `bearer_refresh`, when present, must include `bundle_url` and `pattern`; `bundle_url` must use HTTPS, and `pattern` must compile as a Go regexp.
- `auth_key_url`, when present, must use HTTPS. It overrides any URL inferred from the spec and surfaces in the printed CLI as `Get a key at: <URL>`.
- `auth_instructions`, when present, is a one-line string rendered under the URL. It overrides any `x-auth-instructions` value from the spec.
- `auth_env_vars`, when present, is an ordered list of canonical credential env var names (`^[A-Z][A-Z0-9_]*$`, no duplicates, no empties). The generator merges them in front of the parser's name-derived default and emits `config.go` reading each in order. Ignored for HTTP Basic auth.
- `base_url`, when present, must use HTTPS. Use it only when the upstream spec omits `servers:` and the correct API origin is known.
- Rebuild the binary after editing; `catalog.FS` is a Go embed.
See [`docs/CATALOG.md`](docs/CATALOG.md) for the inclusion rubric, evidence checklist, validation rationale, wrapper-only entry shape, and bearer-refresh metadata.

## Testing
When you change code, check for a `_test.go` file in the same package. If one exists, read it; your change likely requires a test update. If tests fail after your change, investigate whether it is a bug in your code or a stale test; do not just delete the test.
Add tests for new non-trivial logic. Match the package's existing style (typically table-driven with `testify/assert`). Skip tests for CLI glue, trivial wrappers, and code only meaningfully tested via integration (`FULL_RUN=1`).
Run `go test ./...` before considering your work done.

## Quality Gates
Generated CLIs must pass 8 gates: `go mod tidy`, `govulncheck`, `go vet`, `go build`, binary build, `--help`, `version`, and `doctor`.
Run `govulncheck` in default mode only, scoped to the generated or publishing CLI module (`./...` from that CLI directory). Do not use `-show verbose` or a whole public-library scan as a blocking gate; the public library is a historical collection, so its blocking CI should scan only added or changed CLI modules and leave whole-library sweeps to scheduled/reporting workflows.
- For CLIs with `auth.type` of `cookie` or `composed`, `press-auth` (`cmd/press-auth/`) is the canonical cookie capture path. The generated `auth login --chrome` prefers it; the legacy extraction chain (pycookiecheat / browser-use / etc.) is the fallback when press-auth isn't installed. See [`skills/printing-press/references/auth-companion.md`](skills/printing-press/references/auth-companion.md).

## Supply-chain hardening

PRs touching `.github/workflows/**` are gated by Greptile rules in [`greptile.json`](greptile.json) and a Python scan in [`.github/scripts/verify-supply-chain/`](.github/scripts/verify-supply-chain/) run by `verify-supply-chain.yml`. The signal set is the workflow-trust + Go-env subset of the published-library gate; see `signals.py` for scope adaptations (no library go.mod, no npm wrapper, no published-CLI module paths). Run locally with `python3 .github/scripts/verify-supply-chain/scan.py --base-ref origin/main`; tests are `python3 -m unittest scan_test` from that directory.

Runs informationally on landing — promote to a required branch-protection check only after a one-week green window. Canonical incident background lives in the [published-library solutions doc](https://github.com/mvanhorn/printing-press-library/blob/main/docs/solutions/security/2026-05-supply-chain-hardening.md).

## Local Artifacts
Generated artifacts live under `~/printing-press/`, not in this repo: `library/<api-slug>/`, `manuscripts/<api-slug>/`, and `.runstate/<scope>/`. The API slug is derived by the generator from the spec title (`cleanSpecName`), and the binary name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it. See [`docs/ARTIFACTS.md`](docs/ARTIFACTS.md) for local-vs-public flow and divergence rules.

## Publishing to the Public Library
The only supported path for **publishing a generated CLI** (adding or updating an entry under `library/<category>/<api-slug>/` in [mvanhorn/printing-press-library](https://github.com/mvanhorn/printing-press-library)) is to invoke the `/printing-press-publish` skill. The skill runs the required `gh`/`git` commands itself; do not reproduce them by hand.
- Invoke `/printing-press-publish` and let it drive the fork, branch, manifest checks, push, and PR creation. Following its prompts is the supported flow.
- Do not skip the skill and improvise the same steps from scratch (manual `gh repo fork` / `cp -r` into a library clone / `gh pr create --repo mvanhorn/printing-press-library …` / branch push to a fork without the skill driving it). The commands look similar; the difference is the preflight checks and conventions the skill enforces before they run.
- Do not edit `registry.json`, README catalog cells, or `cli-skills/pp-<api-slug>/SKILL.md` in a publish PR — the public library refreshes those post-merge (registry and READMEs from `.printing-press.json` / `manifest.json`; the cli-skills mirror via the library's `generate-skills.yml` workflow). The library's `Guard against hand-edits to cli-skills mirror` check rejects any fork PR whose commits touch the mirror, so committing it pre-rejects the publish before review.

Why this matters: the publish skill enforces preflight checks (printer sentinel validation, manifest shape, vendor-spec PII scope, govulncheck scoped to the changed module) and mirrors the public library's own `AGENTS.md` requirements. An agent operating in this repo's CWD never loads the public library's `AGENTS.md`, so those rules are invisible unless the skill is the entry point.

If `/printing-press-publish` fails, fix the underlying issue (or report it as a machine bug) — do not bypass the skill to land a CLI-publish PR.

## Internal Skills
`skills/` at the repo root contains the Printing Press skills (for example `printing-press-retro`). To make them available to Claude Code regardless of working directory, install them globally:
```bash
.claude/scripts/install-internal-skills.sh
```
This copies the skills to `~/.claude/skills/`.

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
Cross-cutting design patterns are documented in [`docs/PATTERNS.md`](docs/PATTERNS.md). Notably **Deterministic Inventory + Agent-Marked Ledger** — the shape used by `printing-press tools-audit` and `printing-press public-param-audit` for workflows that combine mechanical detection with per-item agent judgment.
