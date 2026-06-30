# CLI Printing Press - Development Conventions

## Machine vs Printed CLI
This repo is **the machine** (generator, templates, binary, skills) that produces **printed CLIs**. When fixing a bug or adding a feature, ask: machine change or printed-CLI change?
- **Machine changes** (generator, templates, parser, skills) affect every future CLI and must generalize across APIs, spec formats, and auth patterns.
- **Printed-CLI changes** (`~/printing-press/library/<api-slug>/`) fix one CLI and do not compound.
- **Default to machine changes.** If a problem appears in a printed CLI, ask first whether the generator should have gotten it right. Only fix the printed CLI directly when the issue is genuinely API-specific.
- **Don't change the machine for one CLI's edge case.** If a fix helps one API but breaks another, guard it with a clear conditional or leave it as a printed-CLI fix.
- **Don't hardcode API/site names in reusable artifacts.** Skills, templates, generator code, prompts, and shared docs must use placeholders (`<api>`, `<site>`, "the target site") unless the text is explicitly an example or test fixture.
- **Update dependent verifiers in the same change.** A new generator capability that affects scoring requires a scorer update; one that changes the MCP surface requires an audit update.
When iterating on a printed CLI to discover issues, label findings as systemic (retro candidate) vs specific (printed-CLI fix).

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

OAuth browser authorization flows must also avoid impossible machine-mode combinations. If `--json` or another machine-output mode suppresses the authorize URL and `--no-open` or equivalent disables browser launch, either emit a deliberate structured continuation protocol (`authorize_url`, state handle, expiry, next command) or fail fast with an actionable usage error. Do not wait for a callback that no user or machine can initiate. See `skills/printing-press/references/oauth2-pkce-cli-checklist.md`.

Generated endpoint-mirror commands also gate mutating HTTP verbs (DELETE/POST/PUT/PATCH) at the transport layer (`internal/client/client.go`): under `PRINTING_PRESS_VERIFY=1` they short-circuit to a synthetic noop and never dial, while reads that ride a mutating verb (GraphQL/JSON-RPC reads, POST search; codegen-marked `mcp:read-only`) route through `doRead()` and bypass the gate. The command envelope reports `verify_noop: true` / `success: false`. `cli-printing-press verify` re-enables real HTTP to its mock server via `PRINTING_PRESS_VERIFY_LIVE_HTTP=1`; agents and ad-hoc runs leave it unset (mutations no-op), and live verifiers (`live_dogfood`, `workflow_verify`) strip both vars.


### Long-running commands under live-dogfood
Hand-written novel commands whose happy path is an expensive network operation (full sync loops, content crawlers, bulk archive walks) MUST curtail work when `cliutil.IsDogfoodEnv()` returns true. The `cli-printing-press dogfood --live` runner sets `PRINTING_PRESS_DOGFOOD=1` in every subprocess under a flat 30s per-command timeout, so an uncapped happy path trips the timeout and flips the matrix verdict to FAIL. Unlike `IsVerifyEnv`, this does NOT mean "don't hit the network" — dogfood is a real-API matrix; use it to bound work (paginate once, fetch a bounded sample, honor a smaller `--limit` default), never to substitute mock data for real calls.

### Generator-reserved namespaces
`internal/cliutil/`, `internal/learn/`, and `internal/mcp/cobratree/` are generator-owned packages emitted into every printed CLI. Do not hand-author code in them and do not name agent-authored helpers that collide with their exports — regen will overwrite the work. Novel-feature code goes in command packages and may import from `cliutil` or `learn`.

The `internal/learn` templates under `internal/generator/templates/learn**` must stay domain-neutral — every printed CLI inherits the loop without inheriting any source domain's vocabulary. `verify-learn-purity.yml` runs `scripts/verify-learn-purity.sh` on PRs touching those templates and fails if a domain-specific identifier (Polymarket / Kalshi / market / event slugs, etc.) appears. Per-CLI domain examples belong in the spec's `learn:` block or narrative recipes, never the templates.

### Typed exit-code verification
`cli-printing-press verify` treats exit `0` as success by default. For commands where a non-zero code is intentional control flow, declare it in Cobra with `Annotations: map[string]string{"pp:typed-exit-codes": "0,2"}`. The verifier reads that annotation first, then falls back to a command-level `Exit codes:` help block. Do not put the whole global failure palette in a command-level help block unless those codes should count as verify-pass for that specific command.

## Build, Test & Lint
```bash
go build -o ./cli-printing-press ./cmd/cli-printing-press
go test ./...
go fmt ./...
golangci-lint run ./...
```
A pre-commit hook runs `gofmt -w` on staged Go files; a pre-push hook runs `golangci-lint` (same `.golangci.yml` as CI). Install with `brew install lefthook && lefthook install --reset-hooks-path` — `--reset-hooks-path` clears stale `core.hooksPath` settings that block hook sync; avoid `lefthook install --force` unless intentionally overriding a custom hooks path.
Format with `go fmt ./...` before handing back work; use `gofmt -w path/to/file.go` only for explicit files. Do not run `gofmt -w ./...` (gofmt rejects Go package patterns) or `gofmt -w .` from the repo root (it walks into `testdata/golden/expected/` and rewrites frozen golden fixtures).
Always use relative paths for build output. Never build to `/tmp` or another shared absolute path; use `./cli-printing-press`.

## Generator Output Stability
Run `scripts/golden.sh verify` whenever a change may affect CLI command output, browser-sniff or crowd-sniff output, generated specs or generated printed CLI files, templates under `internal/generator/templates/`, naming, endpoint derivation, auth emission, manifest generation, scorecard output, or pipeline artifacts.
Never update goldens just to make a failing check pass. Run `scripts/golden.sh update` only when the behavior change is intentional, then inspect the diff and explain it in your final response. See [`docs/GOLDEN.md`](docs/GOLDEN.md) for the decision rubric, fixture conventions, and failure handling.
When adding a new deterministic CLI behavior or generated artifact contract, explicitly decide whether the golden suite needs a new or expanded fixture. A passing `scripts/golden.sh verify` on existing cases does not prove coverage for new auth, pagination, MCP, manifest, naming, or similar deterministic generation behavior.

### Generator fixes require generated-output proof
When touching `internal/generator/**`, `internal/openapi/**`, generator templates, parser-derived fields, MCP descriptions, naming, auth emission, or SKILL.md skeletons, verify the generated CLI behavior, not only the Printing Press source or template text.

Required before handoff:
- Run `go test ./...`, not only scoped `-run` tests. Scoped tests plus `scripts/golden.sh verify` are not enough for conditional or fallback branches.
- Run `scripts/golden.sh verify` when output shape may change.
- Run `scripts/verify-generator-output.sh` for generator/template changes that can alter emitted Go. Pass extra golden case names when the default cases do not exercise the affected variant.
- Add or update a generated-output test when the fix changes an emitted contract. Prefer assertions on emitted code or compile-level behavior over assertions on template source.
- When a generator test changes or verifies emitted helper definitions, call sites, imports, or cross-file contracts, compile the generated module in that test with `requireGeneratedCompiles(t, outputDir)` instead of relying only on `strings.Contains` or golden text.
- Cover the fallback shape affected by the fix: missing defaults, missing summaries, envelope responses, promoted templates, endpoint templates, or every generated file involved.
- When changing an emitted definition, grep for call sites and gate them with the same condition. When changing data flow, check dependent reporting and consumers.
- Prefer established generator idioms: `oneline` / `OneLineNormalize` / `printf "%q"` for emitted literals, and `text/template.IsTrue` or a shared helper when Go code mirrors template truthiness.

If the "obvious" fix violates a parser, verifier, scorer, or printed-CLI invariant, stop and resolve the invariant conflict rather than shipping a narrow band-aid.

## Cross-repo dependency: published-library sweep tool

When a change to `internal/generator/templates/readme.md.tmpl` or `skill.md.tmpl` shifts canonical published-library shape — install-block structure, top-of-README section ordering, presence/removal of `## ` sections, frontmatter top-level field set, install command syntax — also update `tools/sweep-canonical/main.go` in [`mvanhorn/printing-press-library`](https://github.com/mvanhorn/printing-press-library) so already-published CLIs can be retrofitted to match. Fresh prints get the new shape automatically; existing entries drift until the sweep runs.

The same rule applies to `internal/pipeline/agentcookie_manifest.go` and the `hasNonCookieAuth` template helper in `internal/generator/generator.go`: when manifest shape or sweep-eligibility logic changes here, update `tools/sweep-canonical/agentcookie.go` in lockstep so existing entries' `agentcookie.toml` retrofits match fresh prints byte-for-byte. The inline TOML render exists because the sweep tool runs in GOPATH mode — a deliberate duplication, not a candidate for "just import the helper".

If you can't make the matching sweep change in the same session, file a tracking issue at https://github.com/mvanhorn/printing-press-library/issues/new before merging the template PR, including: (1) a link to the template PR here; (2) the shape change(s) the sweep must handle — sweep runs must be idempotent (second run = zero diff), so name the heading boundaries, regex anchors, or section markers it can hang off; (3) any test additions needed in `tools/sweep-canonical/main_test.go`.

The downstream side of this contract (when/how to run the sweep, the `-readme-only` + author-preservation safeties) is documented in `printing-press-library/AGENTS.md` under "Bulk SKILL.md/README.md retrofits".

The same lockstep applies to the learn-loop templates under `internal/generator/templates/learn**` (the `internal/learn` package): when a change shifts the package shape — exported function rename, signature change, file rename, added/removed sub-package, schema field on the v3 store tables — the library-side sweep needs a parallel update. Track it via the same tracking-issue flow, naming the renamed export or schema field and the regex/AST anchors the sweep can hang off.

## Project Structure
- `cmd/cli-printing-press/` - CLI entry point
- `internal/spec/` - Internal YAML spec parser
- `internal/openapi/` - OpenAPI 3.0+ parser
- `internal/generator/` - Template engine + quality gates
- `skills/` - Claude Code skill definitions
- `testdata/` - Test fixtures (internal + OpenAPI specs)
- `docs/PIPELINE.md` - Portable contract for the 9-phase generation pipeline. Update it when `internal/pipeline/state.go` or `internal/pipeline/seeds.go` changes
- `docs/SPEC-EXTENSIONS.md` - Canonical reference for Printing Press-specific OpenAPI `x-*` extensions. Update it when `internal/openapi/parser.go` adds or changes an `Extensions["x-*"]` lookup
- `docs/SKILLS.md` - Skill authoring conventions: workflow parity, reference-file pattern, frontmatter fields
- `docs/PATTERNS.md` - Cross-cutting design patterns
- `docs/GOLDEN.md` - Golden harness decision rubric and fixture conventions
- `CONCEPTS.md` (repo root) - Shared domain vocabulary: what the core nouns mean (the Printing Press, printed CLI, spec, brief, manuscript, library, verify, scorecard, etc.), kept code-free. Relevant when orienting to the codebase or discussing domain concepts
- `docs/GLOSSARY.md` - Naming conventions, overloaded-term disambiguation defaults, and the implementation reference (packages, subcommands, on-disk files) behind the concepts in `CONCEPTS.md`
- `docs/RELEASE.md` - release-please / goreleaser flow
- `docs/ATTRIBUTION.md` - Creator + contributors model: resolver fallback, validation layers, legacy-field dual-write window
- `docs/ARTIFACTS.md` - Local library, manuscripts, and public-library flow
- `docs/DOCS.md` - Doc-authoring rules, including pointer-rot prevention
- `docs/solutions/` - Documented solutions to past problems (bugs, design patterns, best practices, conventions), organized by category subdir with YAML frontmatter (`module`, `tags`, `problem_type`). Relevant when implementing or debugging in documented areas.

## Naming and Disambiguation
Use canonical terms so intent stays unambiguous. In skills and user-facing output (GitHub issues, retros, confirmation prompts), call the system **"the Printing Press"**, never "the machine"; subsystem names (generator, scorer, skills, binary) are fine alongside it. When user phrasing is ambiguous and the distinction affects what action to take, ask before acting.
- "library" -> local library (`~/printing-press/library/<api-slug>/`) unless the public library is called out explicitly
- "publish" -> the publish step (pipeline) unless the public-library workflow is called out explicitly
- "manifest" -> `tools-manifest.json` unless another manifest is named explicitly
- "the CLI" -> a printed CLI, not the generator binary (say "cli-printing-press binary" for the latter)
See [`CONCEPTS.md`](CONCEPTS.md) for what the domain nouns mean, and [`docs/GLOSSARY.md`](docs/GLOSSARY.md) for naming conventions, the disambiguation defaults above in full, and the implementation reference behind each concept.

## Attribution: creator + contributors

A printed CLI's attribution is a single permanent **`creator`** plus a multi-valued **`contributors[]`** (`spec.Person{handle, name}` on `APISpec` and `CLIManifest`). Keep the `handle` (slug-safe @handle; anchors the `@handle` byline link and the legacy slug copyright recovery) and `name` (display name; drives the current `<name> and contributors` copyright header, `RewriteOwner`, SKILL `author:`, NOTICE) split *inside* each Person; never conflate them into one string.
- **Never hand-edit `creator` / `contributors[]`** (or the NOTICE/byline blocks) in a publish PR — the `publish` / `amend` / `reprint` commands and the library's post-merge refresh own them.
- **Creator is permanent** — the human who first got the CLI accepted into the library; never reassign it on a reprint or contribution.
- **Contributors accrue only via `cli-printing-press contributors add`** (run by the contribution flows; idempotent). A plain `generate --force` / `mcp-sync` / sweep preserves the list and never appends the operator.
- **Manifest is the source of truth** — resolution prefers it over re-derivation so others' regens don't overwrite attribution.
See [`docs/ATTRIBUTION.md`](docs/ATTRIBUTION.md) for the resolver fallback chain, the copyright-header format, layered validation, the legacy-field dual-write window, and the NOTICE co-creator credit.

## Issue Work Ownership
Contributor agents without maintainer or admin access must make sure a GitHub issue exists before fixing a bug or behavior change. Maintainers and admins may bypass these issue-ownership rules for maintainer-owned direct work. Do not treat a private plan, external doc, review artifact, or PR body as the only problem statement. Search open and recently closed issues first; reuse an existing issue when one matches instead of filing a duplicate. If no issue exists, open one with enough context for maintainers to understand the bug, scope, and intended fix.

Before implementation, claim the issue: assign it to yourself (or the GitHub user you are explicitly working on behalf of) and post a short comment that you are picking it up. Assignment may fail on permissions — that is fine, but still leave the comment. Claim *before* you start coding, not after a PR is open; that is when duplicate work is prevented.

If the issue already has an assignee, treat that as active ownership until you can determine otherwise from recent activity or direct confirmation. For a plausibly stale assignment, ask the current assignee by tagging them in an issue comment before taking over or reassigning the issue.

If you stop, abandon, or hand off before opening a PR, unclaim: remove the assignment and comment so the next picker-upper knows it is free. No need to unclaim on success — a merged PR closes the issue.

## Commit Style
Format: `type(scope): description`. Both type and scope are required.

**Allowed scopes:**
- `cli` covers the Go binary, commands, flags, and docs.
- `skills` covers skill definitions (`SKILL.md`), references, and setup contract.
- `ci` covers workflows, release config, and goreleaser.
- `main` is reserved for release-please generated release PRs targeting `main`.

**Allowed types:** standard conventional-commits — `feat` `fix` `docs` `refactor` `chore` `test` `ci` `perf` `build` `style` `revert` (the set `pr-title.yml` enforces). Repo-specific nuance: `docs` also covers **template wording changes that don't alter generator behavior** (e.g. rewording an install line in `readme.md.tmpl` / `skill.md.tmpl`) — those are `docs`/`fix`, never `feat`.

**Breaking changes** use `!` after the scope: `feat(cli)!: rename generate flag`. The `!` triggers a major version bump through release-please, so reserve it for changes that *break a downstream contract* — a renamed/removed command, a renamed/removed flag, a removed manifest field, an incompatible config-file shape. **What isn't breaking:** template wording changes, README updates, and generator-output diffs that don't remove or rename a documented surface are `docs(...)` or `fix(...)` — not `feat(...)!`, even when every printed CLI's output changes on next regen. The release-versioning consequence of `!` is intentional; if you're unsure, ask before adding it.

**Examples:** `feat(cli): add --select flag to all read commands` · `feat(cli)!: rename generate flag` · `docs(cli): clarify install instructions in generated README`

**Version bump rules:** `fix(scope):` -> patch; `feat(scope):` -> minor; `feat(scope)!:` or `BREAKING CHANGE:` -> major; `refactor(scope):` is included in the next release PR but does not trigger a bump alone; `docs:`, `chore:`, and `test:` do not trigger a bump alone and stay out of release notes by default.

Every commit and PR title must include one of the allowed scopes. GitHub squash-and-merge uses the PR title as the squash commit message, and `.github/workflows/pr-title.yml` enforces the format.

## Pull Requests
- Community PRs must keep and complete `.github/pull_request_template.md`.
- Maintainer-owned PRs may use a shorter body and omit the community template sections.
- A maintainer-owned PR is one opened by, or explicitly on behalf of, a trusted maintainer account with write/admin access to this repository.
- Do not treat GitHub's `CONTRIBUTOR` author association as exempt; repeat external contributors still use the community PR template unless a maintainer says otherwise.
- If unsure whether a PR is exempt, keep the template.
See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the human-facing contributor guide and AI / automation disclosure definitions.

## Automated code review with Greptile

Every PR gets automated Greptile review alongside CI. Resolve every Greptile finding before calling a PR ready: P0 and P1 comments block merge, and P2 comments need either a fix or a concrete reply explaining why the deferral is intentional. Do not use the score alone as the gate.

Greptile feedback is not limited to GitHub review threads. It also edits top-level PR summary comments, and those summaries can contain actionable issue blocks, including `Comments Outside Diff`, even when the thread list has zero unresolved comments. Before saying a PR is ready, run the repo-owned review-state helper:

```bash
python3 .github/scripts/pr-review-state/greptile_feedback.py <PR_NUMBER>
```

`PR_NUMBER` is the GitHub pull request number, for example `2492` - not a branch name, URL, issue number, or commit SHA. The helper defaults to `mvanhorn/cli-printing-press` and exits non-zero until all of these are true: Greptile Review passes, the `All conversations resolved` check passes, there are no unresolved non-outdated review threads, the latest `greptile-apps` top-level comment reviewed the current PR head SHA, and that latest comment has no actionable markers such as `Issue 1 of`, `Fix the following`, `Comments Outside Diff`, `remaining open item`, or `Safe to merge after fixing/reviewing`.

## Versioning
Releases are automated by release-please. Never manually edit version numbers.
- Normal feature/fix PRs land through the Mergify queue: add the `ready-to-merge` label when the PR is ready to merge, and let Mergify rebase/test/merge it. Do not use the GitHub merge button for normal PRs once `Mergify Merge Protections` is required on `main`.
- release-please PRs are the release control point: they collect already-merged conventional commits, so merge one only when you intend to cut a release. Mergify lets release-please PRs satisfy merge protection without `ready-to-merge`, so maintainers can merge them manually after CI passes.
- When enabling branch protection for the Mergify queue, require the `Mergify Merge Protections` status check and set `required_status_checks.strict=false`; Mergify owns latest-`main` validation through the queue, and GitHub's strict up-to-date requirement recreates the manual rebase loop.
- The plugin version lives in exactly two places and must stay in sync: `.claude-plugin/plugin.json` -> `version`, and `internal/version/version.go` -> `var Version` (annotated `x-release-please-version`; goreleaser injects via ldflags).
- `TestVersionConsistencyAcrossFiles` in [`internal/cli/release_test.go`](internal/cli/release_test.go#L57) fails if those two versions drift.
- Do not add a `version` field to `.claude-plugin/marketplace.json` plugin entries. `TestMarketplaceJSONHasNoPluginVersion` in [`internal/cli/release_test.go`](internal/cli/release_test.go#L81) fails if a reviewer re-adds one.
See [`docs/RELEASE.md`](docs/RELEASE.md) for the merge-the-release-PR flow.

## Supported-version floor
`supported-versions.txt` (repo root) is the **currency floor** — the lowest binary version the generation skills will generate with. The `printing-press` and `printing-press-amend` preflights (and `reprint`, via its hand-off) fetch it from `main` and **hard-block** with `[upgrade-required]` (interactive upgrade-or-abort, no skip) when the installed binary is below it. To push users off a known-buggy release, bump `min_supported` — a one-line PR that takes effect within the 24h version-check cache, no binary or skill release needed.
- `min_supported` must be `major.minor.patch`. At runtime it is clamped to `<= latest` (a value above the newest release is ignored, so a typo cannot brick installs) and is a no-op below the frozen `min-binary-version`.
- Distinct from `min-binary-version`: that is the release-managed, skill-frontmatter compatibility floor (the hard "skill cannot run below this" baseline, tracking the major and moving only on a major bump). The currency floor is a freely-tunable freshness gate. Do not conflate them.
- `TestSkillsEnforceCurrencyFloor` in [`internal/pipeline/contracts_test.go`](internal/pipeline/contracts_test.go) locks the file shape and both contracts' enforce-every-run gate and clamp.

## Testing
When you change code, check for a `_test.go` file in the same package. If one exists, read it; your change likely requires a test update. If tests fail after your change, investigate whether it is a bug in your code or a stale test; do not just delete the test.
Add tests for new non-trivial logic. Match the package's existing style (typically table-driven with `testify/assert`). Skip tests for CLI glue, trivial wrappers, and code only meaningfully tested via integration (`FULL_RUN=1`).
Run `go test ./...` before considering your work done.

## Quality Gates
Generated CLIs must pass 8 gates: `go mod tidy`, `govulncheck`, `go vet`, `go build`, binary build, `--help`, `version`, and `doctor`.
Run `govulncheck` in default mode only, scoped to the generated or publishing CLI module (`./...` from that CLI directory). Do not use `-show verbose` or a whole public-library scan as a blocking gate; blocking CI scans only added or changed CLI modules, leaving whole-library sweeps to scheduled/reporting workflows.
- For CLIs with `auth.type` of `cookie` or `composed`, `press-auth` (`cmd/press-auth/`) is the canonical cookie capture path. The generated `auth login --chrome` prefers it; the legacy extraction chain (pycookiecheat / browser-use / etc.) is the fallback when press-auth isn't installed. See [`skills/printing-press/references/auth-companion.md`](skills/printing-press/references/auth-companion.md).

## Supply-chain hardening

PRs touching `.github/workflows/**` are gated by Greptile rules in [`greptile.json`](greptile.json) and a Python scan in [`.github/scripts/verify-supply-chain/`](.github/scripts/verify-supply-chain/) run by `verify-supply-chain.yml` (a workflow-trust + Go-env subset of the published-library gate; scope adaptations in `signals.py`). Run locally with `python3 .github/scripts/verify-supply-chain/scan.py --base-ref origin/main`; tests are `python3 -m unittest scan_test` from that directory.

Runs informationally on landing — promote to a required branch-protection check only after a one-week green window. Canonical incident background lives in the [published-library solutions doc](https://github.com/mvanhorn/printing-press-library/blob/main/docs/solutions/security/2026-05-supply-chain-hardening.md).

## Local Artifacts
Generated artifacts live under `~/printing-press/`, not in this repo: `library/<api-slug>/`, `manuscripts/<api-slug>/`, and `.runstate/<scope>/`. The API slug is derived by the generator from the spec title (`cleanSpecName`), and the binary name is `<api-slug>-pp-cli`. Never hardcode an API slug when the generator can derive it. See [`docs/ARTIFACTS.md`](docs/ARTIFACTS.md) for local-vs-public flow and divergence rules.

## Plan documents stay local
When writing a plan document for cli-printing-press work, do not `git add` files under `docs/plans/`. This repo is public; plans frequently describe in-progress, unreleased, or third-party-collaborator work that should not be world-readable. The `/docs/plans/` entry in `.gitignore` enforces this for new files. `TestPlansDirectoryGitignored` in [`internal/cli/release_test.go`](internal/cli/release_test.go) fails if the gitignore line is removed.

## No private-module requires in printed CLIs
Printed CLIs are installed via `go install`, so a require on a private module (e.g., `github.com/mvanhorn/agentcookie`) breaks `go mod download` for any user without read access. `TestNoPrivateRequiresInGeneratedGoMod` in [`internal/generator/private_dep_guard_test.go`](internal/generator/private_dep_guard_test.go) regenerates a fixture per auth-type fork and asserts none carry a require on any prefix in `privateModulePrefixes`. When introducing a new internal-by-default module any printed CLI might consume, add its prefix to that list rather than relying on review.

## Publishing to the Public Library
The only supported path for **publishing a generated CLI** (adding or updating an entry under `library/<category>/<api-slug>/` in [mvanhorn/printing-press-library](https://github.com/mvanhorn/printing-press-library)) is to invoke the `/printing-press-publish` skill. The skill runs the required `gh`/`git` commands itself; do not reproduce them by hand.
- Invoke `/printing-press-publish` and let it drive the fork, branch, manifest checks, push, and PR creation. Following its prompts is the supported flow.
- Do not skip the skill and improvise the same steps from scratch (manual `gh repo fork` / `cp -r` into a library clone / `gh pr create --repo mvanhorn/printing-press-library …` / branch push to a fork without the skill driving it). The commands look similar; the difference is the preflight checks and conventions the skill enforces before they run.
- Do not edit `registry.json`, README catalog cells, or `cli-skills/pp-<api-slug>/SKILL.md` in a publish PR — the public library refreshes those post-merge (registry and READMEs from `.printing-press.json` / `manifest.json`; the cli-skills mirror via the library's `generate-skills.yml` workflow). The library's `Guard against hand-edits to cli-skills mirror` check rejects any fork PR whose commits touch the mirror, so committing it pre-rejects the publish before review.
- Do not hand-bump per-CLI release files. `CHANGELOG.md`, `.printing-press-release.json`, and runtime `var version = ...` are finalized by the public library's post-merge release-ledger workflow. Fresh prints may include blank skeletons; reprints must preserve the existing public-library ledger files when replacing a CLI tree.

The skill enforces preflight checks invisible from this repo's CWD (printer sentinel, manifest shape, vendor-spec PII scope, govulncheck scoped to the changed module) and mirrors the public library's own `AGENTS.md`. If `/printing-press-publish` fails, fix the underlying issue (or report it as a machine bug) — do not bypass the skill to land a CLI-publish PR.

## Internal Skills
`skills/` at the repo root contains the Printing Press skills (for example `printing-press-retro`). To make them available to Claude Code regardless of working directory, install them globally:
```bash
.claude/scripts/install-internal-skills.sh
```
This copies the skills to `~/.claude/skills/`.

## Skill Authoring
When a machine change alters what an agent should do or what a command guarantees, update the relevant `SKILL.md` in the same change; do not leave the skill as a stale manual workaround for behavior the machine now owns.
Detail in [`docs/SKILLS.md`](docs/SKILLS.md): install targets, workflow parity, the reference-file pattern, and the `context: fork` / `user-invocable` frontmatter fields.

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
Cross-cutting design patterns are documented in [`docs/PATTERNS.md`](docs/PATTERNS.md). Notably **Deterministic Inventory + Agent-Marked Ledger** — the shape used by `cli-printing-press tools-audit` and `cli-printing-press public-param-audit` for workflows that combine mechanical detection with per-item agent judgment.
