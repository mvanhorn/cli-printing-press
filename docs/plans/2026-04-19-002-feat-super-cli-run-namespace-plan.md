---
title: "feat(cli): super-CLI run namespace for cross-API execute, search, info, and proxy"
type: feat
status: active
date: 2026-04-19
---

# feat(cli): super-CLI run namespace for cross-API execute, search, info, and proxy

## Overview

Add a `printing-press run` subcommand group that gives agents and humans one front door to every printed CLI the user has installed. The surface mirrors the MCP-side capabilities megamcp already provides (library_info, activate_api, search_tools) but as Cobra subcommands on the existing printing-press binary. It does not replace printed CLIs; it sits beside them as a discovery, batching, and raw-HTTP surface for cross-API work.

The plan has one blocking precondition (manifest schema versioning) and five implementation units. It depends on the unified auth plan (`2026-04-19-003-feat-unified-auth-manager-plan.md`) for `run execute` and `run proxy` but not for `run list`, `run info`, or `run search`. Those three can ship independently.

## Problem Frame

The Printing Press generates one printed CLI per API plus a matching per-API MCP server. The library ships 21 printed CLIs today. Agents that need cross-API behaviour must currently:

1. Know every binary name (`espn-pp-cli`, `dub-pp-cli`, `linear-pp-cli`, etc).
2. Shell out to each one separately, threading auth via per-API env vars.
3. Re-discover each CLI's command vocabulary from its own `--help`.

The MCP side already solves this via megamcp: one aggregate server with `library_info`, `activate_api`, `deactivate_api`, `search_tools`, `setup_guide`. The CLI side has no equivalent. The same manifest data megamcp uses can drive a CLI-side super-surface. This plan delivers that.

The PP philosophy (README, "Absorb and Transcend") says the GOAT CLI is built by absorbing every good idea and compounding on top. Cross-API single-entry invocation is table-stakes in the broader ecosystem and the absence of a CLI-side version is a visible gap for any agent that composes across APIs.

## Requirements Trace

- R1. Expose one CLI entry point (`printing-press run ...`) that can list installed printed CLIs, show tool schemas, search tools across APIs, execute a tool, and make authenticated raw HTTP calls.
- R2. Read from the same `tools-manifest.json` source megamcp reads, so MCP and CLI stay in lockstep.
- R3. Preserve PP's agent-native behaviours: auto-JSON when piped, typed exits (0/2/3/4/5/7), actionable errors, `--dry-run`, `--get-schema`.
- R4. Do not duplicate the local data-layer behaviour of printed CLIs. Super-CLI is raw-HTTP; printed CLIs remain the opinionated product surface with SQLite, FTS5, compound commands, and domain archetypes.
- R5. Ship manifest schema versioning before any new additive field so runtime and generator versions can drift safely.

## Scope Boundaries

In scope:
- `printing-press run list`, `run info`, `run search`, `run execute`, `run proxy`.
- `internal/apihttp/` shared package extracted from megamcp's handler.
- `tools-manifest.json` `schema_version` field and reader gate.
- Parity tests between `run execute` and megamcp's `MakeToolHandler` output for raw HTTP response.

Out of scope:
- Local data-layer routing. `run execute` does not touch printed-CLI SQLite stores, compound commands, FTS5, or `--select`/`--compact` semantics. Users wanting those keep using `<api>-pp-cli` directly.
- Authentication flows. Covered by `2026-04-19-003-feat-unified-auth-manager-plan.md`. `run execute` and `run proxy` consume the auth store; they do not create it.
- Trigger daemon, `--allow`/`--deny` MCP filtering, parallel-batch semantics beyond a thin `--parallel` shim. Those are separate plans.
- A separate `pp` binary. This is a subcommand group on the existing `printing-press` binary.

## Context & Research

### Relevant code and patterns

- `internal/megamcp/handler.go` - `MakeToolHandler` is the existing request-builder. Path-param substitution, body routing, header handling, host allowlist, auth gate, response classification, and 32KB agent truncation all live here. This is the extraction target for `internal/apihttp/`.
- `internal/megamcp/manifest.go` - `ToolsManifest` and loader. Schema versioning lands here.
- `internal/megamcp/registry.go` - multi-manifest loader. `run list` and `run search` read through it.
- `internal/megamcp/metatools.go` - the shape of `library_info`, `search_tools`, `setup_guide`. `run list`, `run search`, `run info` mirror these shapes on the CLI side.
- `internal/megamcp/auth.go` - `BuildAuthHeader`, `ApplyAuthFormat`, `hasAuthConfigured`. Used unchanged by `apihttp`.
- `internal/cli/` - existing Cobra wiring for `generate`, `verify`, `scorecard`, etc. New `run` subcommand group lands beside them.
- `internal/cliutil/` - generator-reserved helpers (`FanoutRun`, `CleanText`). Do not put super-CLI code here; `internal/supercli/` is its own namespace.
- `AGENTS.md` glossary - "the printing-press binary" vs "printed CLI" distinction. The super-CLI is part of the printing-press binary (machine role), operating on printed CLIs by reading their manifests.

### Institutional learnings

- `AGENTS.md` machine-vs-printed rule: changes to the super-CLI are machine changes; they affect every installed printed CLI but do not alter any printed CLI's on-disk code.
- README "Dual interface from one spec" and "Absorb and Transcend" framing: one spec produces CLI + MCP; the super-CLI lets that same manifest drive cross-API execution without duplicating logic.
- Existing megamcp plan (`docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md`, shipped) established the manifest-as-contract model that this plan extends to the CLI side.

### External references

- `internal/megamcp/` test suite as the ground truth for expected handler behaviour across 21 live manifests in the library.
- No external library research needed. The super-CLI is a thin reuse of existing in-repo infrastructure.

## Key Technical Decisions

KTD-1. The super-CLI ships as subcommands on the existing `printing-press` binary, not a new binary. Rationale: one install path, one release train, one `/install mvanhorn/cli-printing-press` gets both generator and runtime surfaces. A separate binary doubles distribution concerns without clear upside.

KTD-2. Super-CLI verbs live under a dedicated `printing-press run` namespace. Rationale: `list`, `search`, `info` are high-value English verbs that future generator features (catalog search, spec info) will want. Grouping runtime-role commands under `run` reserves the top-level namespace for generator-role commands.

KTD-3. `run execute` is raw-HTTP-through-manifest. It does NOT invoke local SQLite, compound commands (`stale`, `orphans`, `load`, `reconcile`), domain archetypes, FTS5 search, or `--select`/`--compact` projections. Rationale: without this boundary, `run execute <api> <tool>` silently diverges from `<api>-pp-cli <tool>` for the same tool name, and the user cannot reason about which call gives which result. Help text on both surfaces names the other and states the scope boundary. The scorer adds a dimension for "does help text point to the sibling surface" so drift is caught before ship.

KTD-4. A shared `internal/apihttp/` package returns a transport-neutral `Response` type (`{StatusCode, Body, Headers}`), not `mcp.CallToolResult`. MCP-side wraps it and applies the 32KB `maxAgentResponse` truncation in that wrapper. CLI-side maps status codes to typed exits (0/2/3/4/5/7) in its own wrapper. Rationale: without the split, the shared package grows cli-mode flag branches and the 32KB agent cap silently applies to CLI output. Host validation and credential redaction stay in `apihttp` because both callers need them.

KTD-5. Manifest schema versioning is a blocking precondition. `tools-manifest.json` gets a `schema_version: int` field starting at 1. The reader refuses unknown major versions with an actionable message and accepts unknown minor-version fields with a debug log. Rationale: KTD-4's extraction and future additive fields make every manifest write-then-read a backward-compat contract between two release trains (generator releases vs regenerated library manifests). Without an explicit gate, every new field becomes a silent-fail surface.

KTD-6. `run execute` auth resolution delegates to the `internal/authstore` + `cliutil/authload.HydrateForRequest` pattern defined in the unified auth plan. Env-var-wins precedence is preserved. When the auth plan has not yet shipped, `run execute` still works for APIs whose env vars are set directly, and surfaces a named "credentials not found" exit-4 error with a pointer to `printing-press auth link <api>` (which is planned but not yet present). Rationale: decoupling sequencing so read-only `run list`, `run info`, `run search` can ship in Unit 1 without waiting for auth.

## Open Questions

### Resolved During Planning

- Q: Ship as a new `pp` binary or as subcommands on `printing-press`? A: Subcommands on `printing-press`. See KTD-1.
- Q: Reserve top-level verbs (`list`, `search`, `info`) or namespace them? A: Namespace under `run`. See KTD-2.
- Q: Does `run execute` read printed-CLI SQLite when installed? A: No. KTD-3.
- Q: Does the extracted package return MCP types or neutral types? A: Neutral. KTD-4.
- Q: Is manifest versioning a precondition or a later addition? A: Precondition. KTD-5.

### Deferred to Implementation

- Exact column order and width heuristics for the `run list` human-friendly table. Resolve once the first implementation hits real terminal widths.
- Ranking function inside `run search`. Start with the existing `search_tools` ranking in megamcp and adjust only if CLI callers find it inadequate.
- Whether `run proxy` supports streaming response bodies. Default is buffered; streaming is a P2 follow-up if a printed CLI surfaces a need (e.g., large CSV export endpoints).

## High-Level Technical Design

> This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.

### Command surface

```
printing-press                       # existing generator entry point, unchanged
  generate ...
  verify ...
  scorecard ...
  mega-mcp ...

  run list                           # installed printed CLIs + auth status
  run info <api> [<tool>]            # manifest or tool-schema lookup
  run search <query> [--api X]       # delegates to manifest registry search
  run execute <api> <tool> [-d ...]  # raw-HTTP-through-manifest (KTD-3)
      [--parallel -d @batch.json]
      [--dry-run] [--get-schema] [--skip-auth-check]
  run proxy --api X -X METHOD /path  # authenticated raw HTTP
      [-H ...] [-d ...]
```

### Package boundary

```
internal/apihttp/                    # new, shared
  Execute(ctx, manifest, tool, args) -> (Response, error)
  Response = { StatusCode, Body, Headers }
  host allowlist, credential redaction, path-param substitution

internal/megamcp/handler.go          # existing, refactored
  wraps apihttp.Execute
  applies 32KB maxAgentResponse truncation here, not in apihttp
  shapes mcp.CallToolResult

internal/supercli/                   # new, CLI wrappers
  list.go, info.go, search.go, execute.go, proxy.go, batch.go
  maps apihttp Response to typed exits (0/2/3/4/5/7)
  auto-JSON when piped; human table in TTY
```

### Manifest versioning gate

```
when reader encounters manifest:
  missing schema_version  -> treat as 1
  schema_version = 1 + known fields -> load
  schema_version = 1 + unknown field -> load, debug-log the skip
  schema_version > 1      -> refuse with upgrade instruction; do not register
```

## Implementation Units

- [ ] Unit 0: Manifest schema versioning (blocking precondition)

  Goal: Add `schema_version` to `tools-manifest.json`, enforce it in the manifest reader, and establish the backward-compat contract before any new field ships.

  Requirements: R2, R5

  Dependencies: None. Must land before Units 1-5.

  Files:
  - Modify: `internal/megamcp/manifest.go` to add `SchemaVersion int` on `ToolsManifest` and refuse unknown major versions.
  - Modify: `internal/pipeline/toolsmanifest.go` to emit `schema_version: 1` on generation.
  - Modify: `printing-press-library/registry.json` tolerance for mixed-version manifests.
  - Test: `internal/megamcp/manifest_test.go` extended with version-missing, version-current, version-future-major, version-future-minor cases.

  Approach:
  - Current version is 1. Missing field is treated as 1 so all shipped manifests remain valid.
  - Future-major refusal is actionable: "manifest schema_version=2 is newer than this printing-press build; upgrade to at least vX.Y."
  - Future-minor unknown-field handling accepts with a debug-level log and never fails. Forward-compat minor bumps must be free.

  Patterns to follow: existing `ToolsManifest` struct and manifest tests.

  Test scenarios:
  - Happy path: manifest without `schema_version` loads as v1.
  - Happy path: manifest with `schema_version: 1` and known fields loads.
  - Edge case: manifest with `schema_version: 1` and an unknown forward-compat field loads with one debug log and no error.
  - Error path: manifest with `schema_version: 2` returns a typed error naming the minimum required build; no partial registration.
  - Integration: megamcp started against a library containing one v1 manifest and one v2 manifest registers only the v1 one and surfaces the skip distinctly.

  Verification: `printing-press run list` (Unit 1) shows accepted and skipped manifests so users can diagnose version mismatches without reading logs.

- [ ] Unit 1: Super-CLI read surface - run list, run info, run search

  Goal: Ship the three read-only subcommands. Zero dependency on the auth plan; can ship immediately after Unit 0.

  Requirements: R1, R2, R3

  Dependencies: Unit 0.

  Files:
  - Create: `internal/supercli/registry.go` wrapping `internal/megamcp/registry.go`.
  - Create: `internal/supercli/list.go`, `info.go`, `search.go`.
  - Create: `internal/cli/supercli_cmd.go` for Cobra wiring under `run`.
  - Modify: `cmd/printing-press/main.go` to register the `run` group.
  - Test: `internal/supercli/registry_test.go`, `list_test.go`, `info_test.go`, `search_test.go`.

  Approach:
  - `run list` output: table in TTY, JSON when piped. Columns: api, cli-binary, mcp-binary, tool_count, auth_type, auth_status. `auth_status` is best-effort against env vars only in Unit 1; it is refined in Unit 3 once auth store is live.
  - `run info <api>` returns the full manifest. `run info <api> <tool>` returns a single tool schema.
  - `run search <query>` reuses megamcp's `makeSearchToolsHandler` ranking. Result shape matches what the MCP meta-tool returns so agents can move between surfaces without reshaping payloads.

  Patterns to follow: `internal/megamcp/metatools.go` handlers.

  Test scenarios:
  - Happy path: `run list` returns all 21 library manifests when the library is installed.
  - Happy path: `run info espn` returns the ESPN manifest.
  - Happy path: `run info espn scores_get` returns the single tool schema.
  - Happy path: `run search scores` ranks `espn:scores_get` above unrelated tools.
  - Edge case: `run info unknown-api` returns exit 3 with a "did you mean" suggestion list.
  - Edge case: empty library returns `[]` in JSON mode with exit 0, not an error.
  - Edge case: manifest with `schema_version: 2` is listed as skipped in `run list --all` and omitted by default.
  - Integration: `run search --api espn scores` filters to ESPN only; same ranking as the unfiltered search when restricted to ESPN's tool set.

  Verification: `run list`, `run info`, `run search` all pass `--json | jq` round-trips. Auto-JSON triggers when piped. Human table output fits 120-column terminals for the 21-library baseline.

- [ ] Unit 2: Shared `internal/apihttp/` extraction

  Goal: Lift the request-building, auth-header, host-allowlist, path-param, and response-classification logic from `internal/megamcp/handler.go` into a transport-neutral `internal/apihttp/` package. Megamcp becomes a thin wrapper; the super-CLI becomes the second caller in Unit 3.

  Requirements: R2, R4

  Dependencies: Unit 0.

  Files:
  - Create: `internal/apihttp/execute.go` with `Execute(ctx, manifest, tool, args) -> (Response, error)` and the `Response` type.
  - Create: `internal/apihttp/hosts.go` for host allowlist logic lifted from handler.go.
  - Create: `internal/apihttp/redact.go` for credential redaction.
  - Modify: `internal/megamcp/handler.go` to call `apihttp.Execute`, then apply 32KB truncation and shape `mcp.CallToolResult`.
  - Test: `internal/apihttp/execute_test.go`, `hosts_test.go`, `redact_test.go`.
  - Modify: `internal/megamcp/handler_test.go` to prove parity pre and post extraction.

  Approach:
  - Characterization-first. Snapshot the current handler behaviour with a fixture suite covering all 21 manifests, then extract, then re-run the snapshot suite to prove parity.
  - `Response` is a thin struct: status code, body bytes, response headers. No MCP types. No truncation.
  - Host allowlist continues to enforce the manifest's declared base URL host; no wildcard scheme changes.
  - Credential redaction stays keyed on the same auth-format tokens megamcp uses.

  Execution note: characterization-first. Write the pre-extraction snapshot suite first and hold it fixed across the refactor. Add the CLI-side callers in Unit 3 only after snapshots are green.

  Patterns to follow: `internal/megamcp/handler.go`, `internal/megamcp/auth.go`.

  Test scenarios:
  - Happy path: request construction for a GET with path params produces byte-identical output pre and post extraction across all 21 manifests.
  - Happy path: POST with body routing produces byte-identical output.
  - Happy path: auth header is applied identically (same header name, same value, same format token expansion).
  - Edge case: unsubstituted path placeholder returns the same typed error pre and post.
  - Edge case: response body larger than 10MB is still rejected at the wire by `apihttp`; the 32KB agent cap lives only in the megamcp wrapper and does not apply via `apihttp` directly.
  - Error path: host not in allowlist returns the same error on both sides.
  - Integration: existing megamcp handler_test.go suite passes without modification after the refactor.

  Verification: handler_test.go passes pre and post extraction with no assertion changes. `apihttp` is importable from `supercli` in Unit 3 without circular dependency.

- [ ] Unit 3: run execute + run proxy

  Goal: Ship `printing-press run execute <api> <tool>` and `run proxy --api X -X METHOD /path` on top of `apihttp`, with typed exits, auto-JSON, and auth resolution through the unified auth store.

  Requirements: R1, R3, R4

  Dependencies: Unit 0, Unit 1, Unit 2, and the unified auth plan's Unit 3 (authstore) for credentials.

  Files:
  - Create: `internal/supercli/execute.go`, `proxy.go`, `batch.go`.
  - Modify: `internal/cli/supercli_cmd.go` to wire `run execute`, `run proxy`.
  - Test: `internal/supercli/execute_test.go`, `proxy_test.go`, `batch_test.go`.

  Approach:
  - `run execute` resolves `<api>` via the registry, resolves `<tool>` against manifest tools, calls `apihttp.Execute`, and maps the status code to a typed exit: 2xx -> 0, 401/403 -> 4, 404 -> 3, 429 -> 7 (after one retry-after backoff), 4xx-other -> 2, 5xx -> 5.
  - Human output: pretty-printed response body in TTY. JSON when piped. No truncation cap; users can pipe to `head` if they want.
  - `run proxy` skips the tool registry, resolves auth for the named API, and issues a raw request. Curl-shaped flags: `-X`, `-H` (repeatable), `-d`.
  - Both honour `--dry-run` (print the request, no send), `--get-schema` (print the tool schema for `execute`; for `proxy` returns an error since there is no schema).
  - `--skip-auth-check` allows the caller to hit an unauthenticated endpoint without a store lookup.
  - Help text on both commands names `<api>-pp-cli` as the full-product alternative and explicitly states "this command does not use the local data layer" (KTD-3).

  Patterns to follow: megamcp handler wiring, `internal/cliutil/FanoutRun` for the optional `--parallel` batch shim.

  Test scenarios:
  - Happy path: `run execute espn scores_get --league nfl` returns valid JSON with exit 0.
  - Happy path: `run proxy --api dub -X GET /links` returns the raw API response body; no local-store augmentation.
  - Happy path: megamcp wrapper around `apihttp.Execute` still truncates at 32KB; `run execute` does not.
  - Edge case: missing required path param returns exit 2 and names the missing placeholder.
  - Edge case: `--skip-auth-check` lets the caller hit an unauthenticated endpoint without a store lookup.
  - Edge case: `--dry-run` prints the fully-resolved request (URL, headers with redacted auth, body) without sending.
  - Error path: 401 returns exit 4 with "run `printing-press auth link <api>`" hint.
  - Error path: 404 returns exit 3 with the attempted path.
  - Error path: 429 with retry-after returns exit 7 after exactly one backoff attempt.
  - Error path: host not in manifest allowlist returns exit 2 with "host not permitted by manifest".
  - Integration: `run execute --parallel -d @batch.json` runs three tool calls concurrently, returns responses in input order, and a single failure does not abort the batch.
  - Integration: parity on raw HTTP status and body between `run execute espn X` and `espn-pp-cli X --json` for five tools (raw body only; KTD-3 scope).

  Verification: parity matrix for five tools passes on the raw-HTTP subset defined in KTD-3. Megamcp snapshot suite continues to pass.

- [ ] Unit 4: Scorer dimension for sibling-surface discoverability

  Goal: Add one scorer dimension that checks whether a printed CLI's help text and its `--help` top-of-page reference the sibling surface (`printing-press run execute` for printed CLIs, and a pointer to `<api>-pp-cli` for the super-CLI help text).

  Requirements: R4

  Dependencies: Unit 3.

  Files:
  - Modify: `internal/generator/scorer.go` (or whichever file holds the Tier 1 dimension logic) to add the new check.
  - Modify: generator templates for printed CLI root command help text to include the sibling pointer.
  - Modify: super-CLI help text (`internal/cli/supercli_cmd.go`) to include the printed-CLI pointer.
  - Test: scorer test fixtures updated; template snapshot tests updated.

  Approach:
  - Dimension is binary: help text either mentions the sibling surface or does not. Worth 2 points in Tier 1.
  - Anti-gaming: the scorer checks for a specific sentence shape ("Use `<api>-pp-cli` when you want the local SQLite layer and compound commands") rather than a keyword, so printed CLIs cannot pass by sprinkling the word "run" into unrelated text.

  Patterns to follow: existing Tier 1 dimension entries in the scorer.

  Test scenarios:
  - Happy path: a printed CLI whose template emits the sibling-pointer sentence scores the dimension.
  - Edge case: a printed CLI whose help text was manually edited to remove the pointer fails the dimension on rescore.
  - Integration: a re-scored library after this unit shows 21 CLIs all passing the new dimension (because the template change propagated through a regeneration pass).

  Verification: re-running `printing-press scorecard` across the library shows the new dimension evaluated and the score delta is proportional to the number of CLIs that carry the sibling pointer.

- [ ] Unit 5: Documentation updates

  Goal: Update README, AGENTS.md, and the generator-emitted printed-CLI README template so that the super-CLI is discoverable and the KTD-3 boundary is explicit.

  Requirements: R1, R4

  Dependencies: Units 1-3.

  Files:
  - Modify: `README.md` to add a "Super-CLI" section between "Dual interface from one spec" and "Domain Archetypes".
  - Modify: `AGENTS.md` glossary to add `run` namespace, `internal/apihttp/`, and a cross-reference between the super-CLI and printed CLIs.
  - Modify: generator's README template so every printed CLI's README points to `printing-press run execute` as the cross-API entry point.
  - Test: `internal/cli/release_test.go` `TestReadmeMentionsSuperCli` ensures future README edits do not drop the pointer.

  Approach: documentation-only unit. Keep the tone consistent with existing README voice. Tables and fenced code blocks, no emphasis markers.

  Test scenarios:
  - Happy path: README renders with the new section present between the declared anchor headings.
  - Edge case: generator-emitted README for a newly-printed CLI contains the sibling pointer.

  Verification: a fresh generation pass on one catalog API emits a README mentioning `printing-press run` in the expected position.

## System-Wide Impact

- Interaction graph: megamcp, the super-CLI, and any future surface that needs to execute tools all converge on `internal/apihttp/`. A change to request-building ripples to both MCP and CLI callers uniformly.
- Error propagation: typed exits (0/2/3/4/5/7) are preserved. Agents that already know PP's exit contract do not need new error-handling code to consume `run execute` alongside `<api>-pp-cli`.
- State lifecycle risks: no new persistent state. The super-CLI is stateless and reads manifests plus (in Unit 3+) the auth store owned by the auth plan.
- API surface parity: every `run` subcommand has an MCP meta-tool equivalent on megamcp today (`library_info`, `search_tools`, or the pending `batch_execute`). Keeping that parity is a non-goal per-feature but a guiding principle overall.
- Integration coverage: the parity tests between `apihttp` pre and post extraction are the backbone. Without them the refactor is unsafe.
- Unchanged invariants: printed CLIs keep reading env vars as their primary credential source. Printed CLI binaries are not renamed, their command vocabulary does not change, and their local data layers are untouched. The scorer bar does not move except for the one dimension added in Unit 4.

## Risks & Dependencies

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| `apihttp` extraction regresses megamcp's handler behaviour in a subtle way | Med | High | Characterization-first in Unit 2; snapshot suite covers all 21 live manifests; extraction must keep the suite green |
| Manifest schema drift between generator and runtime versions causes silent-fail on new fields | Med | High | Unit 0 blocks; `schema_version` field and reader gate enforce the contract |
| Agents get confused by two ways to do "the same" thing with different semantics (`run execute` vs `<api>-pp-cli`) | Med | Med | Help text on both surfaces names the other; KTD-3 states the boundary; Unit 4 scorer dimension enforces it |
| `run execute` is misused as a replacement for printed CLIs and users complain that local-layer features are missing | Med | Med | Help text explicit, README section explicit, error messages for missing compound commands point to the sibling surface |
| Parity matrix between `run execute` and `<api>-pp-cli --json` produces false-positive bug reports every time a printed CLI ships a new compound command | Low | Med | KTD-3 scopes the matrix to raw-HTTP body parity only; compound commands and projection flags are excluded from the matrix |
| `run proxy` is used to bypass manifest-declared host allowlist | Low | High | `apihttp` enforces the allowlist for `proxy` too; `proxy` accepts only paths against the named API's base URL, not arbitrary URLs |
| Dependency on auth plan delays Unit 3 ship date | Med | Med | Units 0, 1, 2 ship independently and deliver visible value (list, info, search, `apihttp` refactor); Unit 3 waits on the auth plan's authstore unit |

## Alternative Approaches Considered

- A separate `pp` binary. Rejected in KTD-1: doubles distribution and release plumbing without a new capability.
- Leaving `apihttp` inside megamcp and having the super-CLI shell out to the `<api>-pp-cli` binary for execute. Rejected: shelling out gives the local-layer path which KTD-3 explicitly rules out; raw-HTTP is what `run execute` must deliver.
- Reusing megamcp's activation model on the CLI side (activate-before-execute). Rejected: activation was an MCP-specific response-size concession; CLI callers do not need it and it would add latency and state for no gain.
- Reserving `list`, `search`, `info` at top level on the `printing-press` binary. Rejected in KTD-2: future generator-role commands will want those verbs.

## Success Metrics

- After Unit 1 ships, `printing-press run search <keyword>` returns cross-library results in under 200ms on a 21-manifest library with an FTS-free baseline.
- After Unit 3 ships, agents can compose a batch across three APIs with a single `run execute --parallel -d @batch.json` call and receive responses in input order with per-entry typed exit codes.
- Pre/post extraction of `apihttp` (Unit 2) shows zero assertion diffs in megamcp's handler test suite.
- README and AGENTS.md reference the `run` namespace and the KTD-3 boundary within the release that ships Unit 3.

## Documentation / Operational Notes

- Changelog scope `cli` applies to every unit here. `Unit 4` also carries a `feat(cli):` prefix (scoring change is a user-visible behaviour change).
- No runtime migration. No state migration. No new env vars introduced by this plan (auth env vars are the auth plan's concern).
- Breaking changes: none. Every unit is additive.
- Rollout: single binary release. No feature flag required. Manifest `schema_version: 1` lands in the first regenerated manifest and is retroactively tolerated as-if-1 for manifests already in the library.

## Sources & References

- Related code: `internal/megamcp/`, `internal/pipeline/toolsmanifest.go`, `internal/cli/`, `internal/generator/scorer.go`.
- Prior shipped plan: `docs/plans/2026-04-06-002-feat-mega-mcp-aggregate-server-plan.md`.
- Companion plan (dependency for Unit 3): `docs/plans/2026-04-19-003-feat-unified-auth-manager-plan.md`.
- Repo conventions: `AGENTS.md` (machine-vs-printed rule, glossary, commit style).
- Product philosophy: `README.md` "Absorb and Transcend", "Dual interface from one spec".
