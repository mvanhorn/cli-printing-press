# Coding Standards

Rules that code in this repository follows. Each rule names a defect class that has shipped, been caught in review, or been fixed across the codebase, and that no linter or CI check catches. Code review checks every change against this file.

## Generator and templates

- **An emitted definition and its call sites share one gate.** When a template emits a file, helper, import, or struct field under a condition, every call site, import, and generated test that uses it is gated by the same condition. A "none" or fallback path emits no dead scaffolding and no references to symbols it skipped. Emitted parameter names do not shadow imported packages.
- **A template change covers every shape its predicate produces.** That includes missing defaults, missing summaries, envelope responses, promoted templates, endpoint templates, and each auth type, not only the fixture that motivated the change.
- **A normalization applied at one site is applied at its siblings.** Type spellings from the two parsers, letter case, and `Name` versus the wire name are compared through the shared helper exposed in the FuncMap. Templates never compare against one literal spelling.
- **Positional `args[N]` indexes come from the positional-only ordinal,** never from `range $i` over all params. Path placeholders and positional query args stay separate categories.
- **Identifier collisions are resolved in the existing late pre-pass,** which writes a disambiguated Go identifier while the wire key stays on `Name`. Do not add a parallel pass.
- **A body handed to a helper that marshals internally is never pre-marshaled.** Pass the value or a `json.RawMessage`; a `[]byte` is base64-encoded on the wire.
- **Emitted SQL quotes every identifier** instead of relying on a reserved-word allowlist, and qualifies FTS `rank` with the bound table or alias.
- **Store columns come from the response schema,** not from request params or body fields.
- **A command is emitted only for a capability the spec actually has.** Inferring capability from resource names is not enough.
- **A value that affects emitted files enters the generator before emission;** do not patch one artifact afterward. Regen paths read identity fields from the existing manifest before deriving them from the spec. Slug and display name have separate resolvers, and neither falls back to the other.
- **The multi-spec merge names every per-spec field.** A field the merge literal omits is silently dropped. Runtime-global fields such as auth scopes merge only across compatible auth boundaries.
- **Emitted literals go through the established idioms** (`oneline`, `OneLineNormalize`, `printf "%q"`). Go code that mirrors template truthiness uses `text/template.IsTrue` or a shared helper.
- **Generator-owned packages** (`internal/cliutil/`, `internal/learn/`, `internal/mcp/cobratree/`) get no hand-authored code, and novel code does not reuse their export names.
- **Reusable artifacts do not hardcode API or site names.** That covers templates, generator code, skills, and prompts, except explicit examples and fixtures.
- **Printed Chrome identity literals change only together,** and the advertised major never exceeds the printed `utls` `HelloChrome_Auto` alias. The impersonation-off UA is a separate frozen string.
- **Target-OS executable names go through the platform helper** (for example `.exe` on Windows).

## Spec parsing

- **A spec lever is wired into both parsers,** the internal YAML parser and the OpenAPI parser. Object-valued extensions use the typed generic helper.
- **Auth inference runs from the most to the least authoritative source.** The primary scheme is chosen by operation usage, not declaration order. AND-required siblings all stay required. Inferring bearer auth from an inline `Authorization` param needs both structural and textual evidence.
- **Broad filters exempt semantically required items through one named predicate,** never through vendor allowlists.
- **When an authoritative source is combined with a heuristic or LLM derivation, the authoritative field stays verbatim.** For example, request paths from docs stay as written.
- **URL normalization keeps grammar separators** such as `://` while placeholders remain.

## Printed-CLI runtime

- **A safety gate covers every path that does the same thing.** A guard on the main request path also covers multipart bodies, token mint and refresh, sibling commands, recipe tools, aliases, inherited persistent flags, and wrapped output sinks.
- **Dry-run and preview paths have no side effects and report their real status.** They do not open or rename stores or write files. A completed dry run reports `success: true` with `dry_run: true`; a failed dry run or a synthetic harness noop (`verify_noop`) reports `success: false`. Suppression decisions run after every source that can set dry-run.
- **A wrapper calls the real API or reads the local store.** It never returns canned payloads, `"OK"` stubs, locally synthesized reference data, or in-process aggregations of something the API already aggregates. The carve-outs are the annotated ones in AGENTS.md.
- **Side-effect commands print by default.** They act only with an explicit opt-in flag, refuse under `cliutil.IsAnyHarness()`, and emit structured JSON in machine modes.
- **Expensive read paths curtail their work under `cliutil.IsDogfoodEnv()`** while still calling the real API.
- **Emitted code that shares files across processes holds the lock across check-and-act.** It never deletes another process's lock, never rolls back over another process's save, and never does an unlocked read-modify-write of shared state.
- **Happy-path work is bounded.** Migrations and renames process rows in batches. Budgets do not reset per argument. Hot paths do not sweep the whole cache.
- **Errors keep their class.** A read error other than not-found is not treated as empty input. A configuration or transport error is not reported as an auth or session error. Exit codes match the documented failure.
- **Credential getters do not rewrite provenance** that `Load()` established.
- **A billed or paid mutation is never replayed after an ambiguous failure** unless it is marked replay-safe or carries an idempotency key. Per-request deadlines are set on a copy of the client, never on the shared one.
- **A successful non-GET request evicts cached responses** on final success only, not on dry-run.
- **Response transforms run before item extraction on every path** (direct, flat sync, dependent sync), and the profiler's envelope heuristics match the sync extraction.
- **Upstream 4xx responses become exit 0 only behind an explicit opt-in flag.**
- **Numeric fields whose precision matters are decoded with `UseNumber`.** String truncation is UTF-8 safe.
- **User input joined into a path is validated for traversal and checked for containment** after the join. Paths passed to `filepath.Glob` are escaped.

## MCP surface

- **Annotations are never wrong in the permissive direction.** A false `readOnlyHint: true` is a bug; a missing hint only costs a permission prompt.
- **Novel flags that choose a write destination or name a file to read are declared** in `mcp:write-flags` or `mcp:read-flags` on the command that defines them.
- **A read-only claim is enforced by a read-only handle,** not by a keyword blocklist. Query classification strips comments and statement separators first.
- **Cobratree classification uses the full command path and depth,** not the leaf name. The template, the token estimator, and tools-audit change together.
- **The rich env-var spec is the single source of truth.** Downstream emitters do not drop vars by kind.
- **A change to the MCP surface or to scoring updates its verifier, audit, or scorer in the same change.**

## Verify, dogfood, and scorer

- **Argument and flag parsing matches Cobra and pflag semantics.** That covers `--` as a value, `--flag=false`, shorthand clusters, and case. Command examples are tokenized with the shared shellargs tokenizer after continuations are removed.
- **Static analyzers resolve identifiers with scope.** They account for receivers, aliases, const groups, and cross-file use, rather than plain name matching.
- **Absent evidence is never a neutral score.** Unknown data is N/A and leaves the denominator; a missing readiness or score artifact means HOLD.
- **Scorer patterns are precise.** Identifiers match on word boundaries. A file that defines an identifier needs at least two matches. Regexes do not cross lines. Values from other modules are checked for units. Caps apply before totals and floors after.
- **Scorers follow reachable, registered commands and runtime evidence,** not hardcoded filenames. They never reward a pattern the skills forbid, and never count hidden tools as agent-visible.
- **Harness skip heuristics stay narrow.** Prefer command annotations over substring matching on status codes. Auth-unavailable results are classified separately from endpoint failures.
- **Mutator probes default to dry-run.** A paid real run needs both a command annotation and an operator flag.
- **Validation never mutates the directory it validates.** A verifier that refreshes an artifact reports the refresh and fails if the refresh fails. Lossy fallback matchers emit a diagnostic for what they hide.

## Tests

- **A test of emitted helpers, call sites, imports, or cross-file contracts compiles the generated module** with `requireGeneratedCompiles`. Text assertions alone are not enough.
- **Tests on conditional emission cover every branch of the predicate.** Each heuristic, exemption, or skip gets a paired positive and negative fixture, and the fixture actually emits the bug's shape.
- **Precedence tests use mutually exclusive values and a negative assertion.** Scorer calibration asserts exact scores.
- **Tests are hermetic.** They clear inherited harness and auth env vars. They redirect `HOME`, `USERPROFILE`, and the `XDG_*` variables together. They never reach live local ports or host binaries on `PATH`. An empty env slice stays empty and is not turned into nil, which would inherit the parent environment.
- **Tests and emitted code work on Windows and macOS.** They do not depend on Unix permission bits, Unix-only syscalls, Unix error text, or executable names without `.exe`.

## Skills and contract docs

- **Skill text that depends on a new binary capability raises `min-binary-version`.**
- **A change to a file that a contract doc describes updates that doc in the same change.** That includes `docs/PIPELINE.md` and `docs/SPEC-EXTENSIONS.md`. A machine change that alters what an agent should do also updates the skill router and phase files.

## Comments and code shape

- **Comments explain a non-obvious why.** They do not restate the field or function name, carry dates, incidents, issue or PR numbers, or speculative future plans, and they do not depend on in-repo skills, plans, or reference prose to make sense.
- **Dependency-pin and floor comments may cite `GO-YYYY-NNNN` advisory IDs and CVE IDs** so the floor stays tied to a named advisory (precedent: `internal/generator/xnet_guard.go`). Issue and PR numbers, incident tickets, and dates remain forbidden there too.
- **Categorical strings become typed constants** when they are introduced, so the same literal is not repeated across sites.
- **A single-case switch with a default fallthrough is written as `||`.**
- **Command inputs are parsed once,** at the entry point.
- **Near-identical loops or functions share a helper.** A compound predicate used at three or more sites gets a name. Paired `hasX()` / `xCount()` helpers do not drift apart.

## Maintaining this file

A rule belongs here only when the defect class appeared in a review finding the author fixed, a fix commit, a cleanup across the codebase, or a solution record, and no linter or CI check catches it. State the rule as a condition that stays true without naming volatile numbers or symbols. Add rules when a change establishes one, and remove them when a check takes over or the code retires the class.
