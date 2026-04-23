---
title: "feat: MCP production readiness — remote transport, intent tools, code-orchestration"
type: feat
status: active
date: 2026-04-22
---

# feat: MCP production readiness — remote transport, intent tools, code-orchestration

## Overview

Anthropic's April 22 2026 post "Building agents that reach production systems with MCP" argues that as production agents move to the cloud, MCP becomes the critical integration layer. Remote servers, intent-grouped tools, and code-orchestration for large surfaces are the three patterns that separate production-ready MCP from stdio-era toys.

The Printing Press already generates an MCP server alongside every printed CLI, bundles skills + MCP as a plugin, and ships a meta-MCP (`megamcp`) that does library-level tool search. But the per-API MCP servers it emits today are stdio-only, one-tool-per-endpoint, and flat regardless of API size. This plan closes the three highest-leverage gaps. A follow-up plan will cover elicitation, MCP Apps, and OAuth/CIMD.

---

## Problem Frame

Every printed CLI ships `<api>-pp-mcp` built from `internal/generator/templates/main_mcp.go.tmpl` and `mcp_tools.go.tmpl`. Three concrete limits, each matching a named pattern in the article:

1. `main_mcp.go.tmpl` hard-codes `server.ServeStdio`. Cloud-hosted agents (Claude Cowork, Managed Agents, ChatGPT, Cursor, anything running in a container without filesystem handoff) cannot reach a stdio server. The article: "A remote server is what gives you distribution — it's the only configuration that runs across web, mobile, and cloud-hosted agents."

2. `mcp_tools.go.tmpl` ranges over `$resource.Endpoints` and emits one MCP tool per endpoint. APIs with many small endpoints (Kalshi ~60, Linear via GraphQL, HubSpot ~200+) force agents to stitch primitives. The article: "A single `create_issue_from_thread` tool beats `get_thread + parse_messages + create_issue + link_attachment`."

3. For very large surfaces (HubSpot, Salesforce-360, any future Cloudflare/AWS-style CLI), even intent-grouped tools still overflow context. The article's reference case is Cloudflare covering ~2,500 endpoints in ~1K tokens via a `search`+`execute` pair. PP has no equivalent.

The scorer currently rewards `mcp_token_efficiency` (file size of tool definitions) and generic `mcp_quality`. It does not penalize stdio-only transport, endpoint-mirror tool design, or missed code-orchestration opportunities on large surfaces. Per AGENTS.md, capability additions must update the scorer in the same change so the bar reflects reality.

---

## Requirements Trace

- R1. Printed CLIs' MCP binary must support an HTTP streamable transport alongside stdio, selectable at runtime via flag and at generate time via spec config.
- R2. Generator must support declaring intent-grouped MCP tools in the spec (composing 2+ endpoint calls server-side) and emit them as first-class tools, with per-tool opt-in to also keep the raw endpoint tools.
- R3. When a spec's endpoint count exceeds a configurable threshold (default 50), generator emits a thin `<api>_search` + `<api>_execute` tool pair that proxies raw API calls via an agent-written script, mirroring the Cloudflare pattern.
- R4. Scorecard must reward remote transport, intent tool declarations, and code-orchestration strategy on large-surface APIs — and penalize their absence in a calibrated way that doesn't double-count existing dimensions.
- R5. Existing printed CLIs continue to build and pass quality gates with no spec changes (remote transport, intent tools, and code-orchestration are all opt-in; stdio one-tool-per-endpoint remains the default).
- R6. The machine-side retro/publish flow surfaces recommendations for each existing library CLI indicating whether it should enable remote transport, declare intent tools, or switch to code-orchestration.

---

## Scope Boundaries

- Remote transport is HTTP streamable only. SSE-specific transport is deferred — the article treats HTTP streamable as the modern remote path.
- No authentication beyond what exists today (env var / bearer / API key). OAuth, CIMD, and token-vault integration are deferred to a separate plan.
- No elicitation (form/URL mode) or MCP Apps (interactive UI returns). Deferred to a separate plan.
- No client-side patterns (tool search, programmatic tool calling). PP builds servers, not clients. `megamcp` already implements library-level tool search and is out of scope here.
- Intent-grouped tools are declared in the spec, not inferred automatically. Auto-inference from endpoint semantics is a future consideration.

### Deferred to Follow-Up Work

- Elicitation + MCP Apps plan: after this ships.
- OAuth + CIMD + managed-agent vault integration plan: after this ships.
- Auto-inference of intent tools from endpoint patterns: tracked as a future consideration.
- Backfill sweep of existing published CLIs to adopt the new defaults: see U5; may land as separate per-CLI PRs.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/main_mcp.go.tmpl` — the stdio-only server entry point; needs transport selection.
- `internal/generator/templates/mcp_tools.go.tmpl` — endpoint-mirror tool registration; needs intent-tool emission and code-orchestration branch.
- `internal/spec/spec.go` — spec schema; needs a new `mcp:` section for transport + intents + code-orchestration config. The `ExtraCommands` precedent (hand-written cobra commands declared in the spec, documented in SKILL.md) is the right model: spec-declared, generator-respected.
- `internal/pipeline/scorecard.go` — `SteinerScore` holds the dimension struct; add fields here. `MCPQuality` and `MCPTokenEff` already exist; preserve them and extend.
- `internal/pipeline/mcp_size.go` — pattern for opt-in scoring with `scored bool` return; follow for new dimensions that only apply when MCP is emitted or the API surface is large.
- `internal/megamcp/` — existing meta-MCP with library-level tool search, setup guide, activation. Out of scope to modify; in-scope to understand so new transport fields line up with `RegistryMCP.Transport` already declared in `types.go`.
- `github.com/mark3labs/mcp-go v0.47.0` — pinned MCP SDK. Confirm HTTP streamable server support is available at this version before starting U1.

### Institutional Learnings

- AGENTS.md "When adding a capability that affects scoring, update the scorer in the same change" — every unit that ships new capability also ships scorer recognition.
- AGENTS.md "Default to machine changes" — this plan is entirely machine changes; per-library CLI updates are one-shot regenerates, not hand-edits.
- AGENTS.md "Anti-Reimplementation" — intent tools must call the real endpoints (or read from the generated store). U2 must resist agents building intent tools that invent data.

### External References

- Anthropic, "Building agents that reach production systems with MCP" (2026-04-22) — source of the three patterns.
- Cloudflare's MCP server — reference implementation of `search`+`execute` over ~2,500 endpoints in ~1K tokens; linked from the Anthropic post.

---

## Key Technical Decisions

- **Spec surface for transport**: add `mcp.transport: [stdio, http]` as an opt-in list; default is `[stdio]` for backward compatibility. When `http` is in the list, the generated `main.go` reads a `--transport` flag and `PP_MCP_TRANSPORT` env var and binds `server.ServeStdio` or `server.ServeStreamableHTTP` accordingly.
- **Spec surface for intent tools**: add `mcp.intents:` list. Each intent declares `name`, `description`, and `steps` (an ordered list of `endpoint: <resource>.<endpoint>` calls with an `output_binding` expression that maps each step's response into the next step's params). This is declarative orchestration, not code — keeps the spec the source of truth and keeps handlers generator-owned.
- **Code-orchestration threshold**: default trigger is `endpoint_count > 50` OR `mcp.orchestration: code` explicit in spec. Below that, the endpoint-mirror stays. This keeps small CLIs (ESPN, Dominos) simple.
- **Search+execute semantics**: `<api>_search` returns a ranked list of endpoint metadata (path, method, param names, description). `<api>_execute` takes an `endpoint_id` and a `params` map and performs the call. No agent-authored code runs inside the MCP server — "code orchestration" here means the *agent* writes code in its own sandbox that calls these two tools in a loop, matching Cloudflare's framing.
- **Scorer additions**: three new dimensions, each with `scored bool` opt-in so absence doesn't zero out a CLI that legitimately shouldn't have them. No renumbering of existing dimensions. Tier-1 denominator updates follow the mcp_token_efficiency + cache_freshness precedent in scorecard.go.

---

## Open Questions

### Resolved During Planning

- Should transport selection be generate-time or runtime? **Runtime via flag**, with a spec-level allowlist that determines which transports the binary compiles in. Allows one binary to run stdio locally and HTTP in Docker.
- Should intent tools replace or supplement the endpoint-mirror tools? **Supplement by default**; spec can set `mcp.endpoint_tools: hidden` to suppress raw tools when intents cover the surface. Prevents breaking existing agents that call raw tools while still allowing clean intent-only surfaces.
- How should code-orchestration scope interact with intent tools? **Mutually exclusive per API**: if `mcp.orchestration: code` is set, only `search`+`execute` are emitted. Intent tools are a middle ground for mid-size APIs; code-orchestration is for huge ones.

### Deferred to Implementation

- Exact shape of the `output_binding` expression syntax in intent declarations (JSONPath vs. Go-template vs. a tiny DSL). Prototype during U2 and pick the simplest that handles the Kalshi and Linear use cases.
- Whether to surface endpoint params as JSON Schema inside `<api>_search` results or hold them for `<api>_execute` to return on demand. Measured trade-off: search-result size vs. one extra round-trip for the agent.
- HTTP transport port binding, base path, and TLS posture in the default generated `main.go`. Adopt the mcp-go library defaults; revisit if the SDK leaves critical knobs unset.
- Whether existing published CLIs should be regenerated en masse via `printing-press emboss` or handled per-CLI by retro PRs. Resolved in U5 after U1–U4 are green.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

Spec surface addition (additive, all optional):

    # spec.yaml for a printed CLI
    mcp:
      transport: [stdio, http]          # default [stdio]; http opts into remote
      orchestration: endpoint-mirror    # endpoint-mirror | intent | code; default endpoint-mirror
      endpoint_tools: visible           # visible | hidden; default visible
      intents:                          # only read when orchestration includes 'intent'
        - name: create_issue_from_thread
          description: "Create a Linear issue from a Slack thread, preserving messages as the description."
          steps:
            - endpoint: messages.get_thread
              bind: { thread_id: "{{ .params.thread_id }}" }
              capture: thread
            - endpoint: issues.create
              bind:
                title: "{{ .thread.subject }}"
                description: "{{ .thread.messages | to_markdown }}"
              capture: issue
          returns: issue

Generated server branches at startup:

    transport = --transport flag | PP_MCP_TRANSPORT env | spec.mcp.transport[0]
    switch transport:
      stdio -> server.ServeStdio(s)
      http  -> server.ServeStreamableHTTP(s, :port)

Tool registration branches on orchestration mode:

    switch spec.mcp.orchestration:
      endpoint-mirror -> register one tool per endpoint (today's behavior)
      intent          -> register intent tools; endpoint tools visible unless endpoint_tools=hidden
      code            -> register <api>_search + <api>_execute only

Scorer adds three fields, all opt-in:

    MCPRemoteTransport  int  // 0-10; unscored when no MCP emitted
    MCPToolDesign       int  // 0-10; endpoint-mirror scores middling on large APIs, intent scores high, code-orch scores high on huge APIs
    MCPSurfaceStrategy  int  // 0-10; unscored when endpoint_count <= threshold

---

## Implementation Units

- [ ] U1. **Remote transport (HTTP streamable) in generated MCP servers**

**Goal:** Let every printed CLI's MCP binary serve over HTTP streamable in addition to stdio, selectable at runtime.

**Requirements:** R1, R5

**Dependencies:** none

**Files:**
- Modify: `internal/spec/spec.go` (add `MCP` struct with `Transport []string`, validation)
- Modify: `internal/generator/templates/main_mcp.go.tmpl` (read flag/env, branch transport)
- Modify: `internal/generator/generator.go` (pipe MCP config into template context)
- Modify: `testdata/` fixtures that exercise MCP generation (add one remote-enabled spec)
- Test: `internal/spec/spec_test.go` (validation of new MCP struct)
- Test: `internal/generator/generator_test.go` (golden-file check that http-transport spec produces a flag-aware main)

**Approach:**
- Add `MCP MCPConfig` to the spec struct. `MCPConfig.Transport` defaults to `[]string{"stdio"}` when absent.
- Validation rejects transports not in `{stdio, http}` with a clear error listing the allowed set.
- `main_mcp.go.tmpl` becomes flag-aware: reads `--transport` flag and `PP_MCP_TRANSPORT` env with stdio as fallback. Only includes the branches the spec opts into — a spec that doesn't list `http` doesn't get the `ServeStreamableHTTP` import.
- Address/port binding surfaces via `--addr` flag (default `:7777`) for the http branch only.

**Patterns to follow:**
- `ExtraCommands` in `internal/spec/spec.go` — additive, spec-declared, validator-gated.
- Existing `{{- if ... }}` conditional imports in `mcp_tools.go.tmpl` — emit imports only when the branch is compiled in.

**Test scenarios:**
- Happy path: spec with `transport: [stdio]` generates today's binary unchanged. Golden file diff is empty for the main.
- Happy path: spec with `transport: [stdio, http]` generates a main that compiles, includes both code paths, and accepts `--transport http --addr :8080`.
- Edge case: spec with empty `transport: []` is rejected at validation with a message naming the allowed set.
- Edge case: spec omitting `mcp:` entirely behaves identically to today (default `[stdio]`).
- Error path: spec with `transport: [rest]` is rejected.
- Integration: generated binary built with both transports runs, serves one MCP `list_tools` over stdio and then over HTTP, and exits cleanly on SIGINT. Verified via `go test` spawning the binary.

**Verification:**
- `go build` on a scaffold produced from a `transport: [stdio, http]` spec succeeds.
- `./<api>-pp-mcp --transport http --addr :0` binds a port and serves an MCP hello exchange.
- Quality gates still pass for a default stdio-only spec.

---

- [ ] U2. **Intent-grouped MCP tools declared in spec**

**Goal:** Let spec authors declare higher-level MCP tools that compose multiple endpoint calls, and have the generator emit them as first-class tools alongside the raw endpoint tools.

**Requirements:** R2, R5

**Dependencies:** U1 (reuses the new `MCP` spec struct)

**Files:**
- Modify: `internal/spec/spec.go` (extend `MCPConfig` with `Intents []Intent`, `EndpointTools string`, validation)
- Create: `internal/generator/intent_handler.go` (code generation for intent handlers — each handler fans out to endpoint handlers already emitted by `mcp_tools.go.tmpl`)
- Create: `internal/generator/templates/mcp_intents.go.tmpl` (renders the intent registrations + handlers)
- Modify: `internal/generator/templates/mcp_tools.go.tmpl` (respect `EndpointTools=hidden` to suppress raw tool registration)
- Modify: `internal/generator/generator.go` (call the new intent generator when intents are declared)
- Create: `testdata/specs/intent-tools-fixture.yaml`
- Test: `internal/spec/spec_test.go` (intent validation, binding expression parsing)
- Test: `internal/generator/intent_handler_test.go`
- Test: `internal/generator/generator_test.go` (golden: intent-declared spec emits the right tool surface)

**Approach:**
- Intent syntax declares `steps: [ { endpoint, bind, capture } ]` and a `returns` expression. Binding expression format is decided during U2 prototyping; prefer the simplest dialect that covers the common case. Validator rejects references to undeclared endpoints, undeclared captures, or missing required endpoint params after binding is applied.
- Intent handler composes existing generated endpoint client calls — no new HTTP code. The handler is a generator-owned Go function that the agent should not hand-modify.
- `EndpointTools: hidden` hides raw endpoint tools from MCP registration but leaves the underlying Go endpoint handlers intact so intents can call them.
- Each intent contributes to `tools-manifest.json` so `auth doctor`, scorecard, and megamcp discovery see them.

**Patterns to follow:**
- `internal/generator/vision_templates.go` — generator-owned higher-level code emission.
- `internal/pipeline/toolsmanifest.go` — manifest shape to preserve; extend to mark intent vs. endpoint tools.
- AGENTS.md Anti-Reimplementation rule — intent handlers must fan out to real endpoints, verified in tests.

**Test scenarios:**
- Happy path: spec declares one intent composing two endpoints. Generator emits the intent tool, preserves both raw tools, and the intent handler calls both endpoint clients in order.
- Happy path: `endpoint_tools: hidden` suppresses raw tool registration; intent tool is the only entry; `tools-manifest.json` reflects this.
- Edge case: intent with zero steps is rejected at validation.
- Edge case: intent with a `capture` name that a later step's bind does not reference still generates successfully (unused captures allowed, warning logged).
- Edge case: intent referencing an endpoint that does not exist in the spec is rejected with a clear error.
- Error path: binding expression references undeclared capture (`{{ .doesnotexist.id }}`) is rejected at validation with line number.
- Integration: generated MCP binary starts, `list_tools` includes the intent, calling the intent with valid params triggers the expected sequence of endpoint HTTP calls against a mock server, and returns the final bound value.
- Anti-reimplementation: dogfood's `reimplementation_check` still sees a real client call path in the intent handler (not a fabricated response).

**Verification:**
- `go vet ./...` and `go test ./...` pass for the cli-printing-press repo.
- Generated fixture CLI from `testdata/specs/intent-tools-fixture.yaml` builds, passes its own quality gates, and `list_tools` over stdio returns the intent + endpoint tools as declared.
- dogfood flags nothing new on the intent-declared fixture.

---

- [ ] U3. **Code-orchestration thin surface for large-surface APIs**

**Goal:** When a spec declares `mcp.orchestration: code` (or auto-triggers on endpoint count), the generator emits only `<api>_search` and `<api>_execute` as MCP tools, covering the full API surface in ~1K tokens.

**Requirements:** R3, R5

**Dependencies:** U1 (transport), U2 (MCPConfig struct already present)

**Files:**
- Create: `internal/generator/templates/mcp_code_orch.go.tmpl` (the search + execute tool registrations and handlers)
- Modify: `internal/generator/generator.go` (branch on orchestration mode; skip endpoint-mirror emission entirely when code-orch is on)
- Modify: `internal/spec/spec.go` (add `Orchestration string` to MCPConfig with `endpoint-mirror | intent | code` enum, threshold config with default 50)
- Modify: `internal/pipeline/scorecard.go` (unscored dimension when applicable — see U4)
- Create: `testdata/specs/code-orch-fixture.yaml` (fixture with 60+ endpoints)
- Test: `internal/generator/generator_test.go` (golden for code-orch output shape)
- Test: `internal/spec/spec_test.go` (orchestration mode validation, threshold auto-trigger)

**Approach:**
- `<api>_search` takes a free-text `query` and returns a ranked list of endpoints with `{ id, method, path, summary, params_schema_ref }`. Ranking uses the tokenized endpoint description match — naive but adequate; upgrade path is a future consideration.
- `<api>_execute` takes `{ endpoint_id, params, headers? }` and invokes the corresponding client call. Handler dispatches via the generator-emitted endpoint registry.
- Default threshold 50; configurable via spec `mcp.orchestration_threshold`. When endpoint count exceeds threshold and `mcp.orchestration` is unset, generator warns (not errors) that code-orch would be a better default.
- Auth, error sanitization, and store-read paths continue to work uniformly for both tools since they delegate to the same endpoint handlers.

**Patterns to follow:**
- `internal/megamcp/metatools.go` for the tool-registration shape of `search_tools` — lift the pattern into the per-API template.
- `internal/pipeline/reimplementation_check.go` — ensure `execute` handler passes the check (it calls the real client per endpoint).

**Test scenarios:**
- Happy path: spec with `orchestration: code` emits exactly two MCP tools regardless of endpoint count.
- Happy path: spec with 80 endpoints and unset orchestration emits endpoint-mirror tools but logs a generator warning recommending code-orch.
- Edge case: spec with 80 endpoints and `orchestration: code` emits search+execute; `tools-manifest.json` records the strategy.
- Edge case: `<api>_search` with a query that matches zero endpoints returns an empty list, not an error.
- Error path: `<api>_execute` with an unknown `endpoint_id` returns a structured MCP error naming the valid discovery path (`call <api>_search first`).
- Integration: generated code-orch MCP binary starts, agent workflow simulates `search("create issue") -> execute(endpoint_id, params)` against mock server, receives the expected API response.
- Anti-reimplementation: `<api>_execute` handler calls the real client; fabricated-response test fails if a regression introduces a canned payload.
- Size: tool-definition tokens emitted by code-orch mode remain under the MCP token efficiency "full marks" threshold regardless of underlying endpoint count.

**Verification:**
- `printing-press generate` on `testdata/specs/code-orch-fixture.yaml` produces a CLI whose MCP binary registers exactly two tools.
- dogfood's reimplementation check passes on the execute handler.
- `printing-press scorecard` reports `mcp_surface_strategy` as scored and in the high range.

---

- [ ] U4. **Scorecard dimensions for remote transport, tool design, surface strategy**

**Goal:** Extend `SteinerScore` so remote transport, intent-tool adoption, and code-orchestration fit are visible in the scorecard. Calibrate so default stdio endpoint-mirror CLIs stay at their current score and opt-in improvements raise it.

**Requirements:** R4

**Dependencies:** U1, U2, U3 (the capabilities these dimensions measure must exist)

**Files:**
- Modify: `internal/pipeline/scorecard.go` (add `MCPRemoteTransport`, `MCPToolDesign`, `MCPSurfaceStrategy` fields; extend tier-1 denominator logic per the existing `IsDimensionUnscored` pattern)
- Create: `internal/pipeline/mcp_transport_score.go` (`scoreMCPRemoteTransport(outputDir) (int, bool)` — inspects generated main for ServeStreamableHTTP branch)
- Create: `internal/pipeline/mcp_tool_design_score.go` (`scoreMCPToolDesign` — inspects `tools-manifest.json` to count intent tools vs. endpoint tools and rewards intent presence proportional to endpoint count)
- Create: `internal/pipeline/mcp_surface_strategy_score.go` (`scoreMCPSurfaceStrategy` — rewards code-orch when endpoint count > threshold; unscored when below threshold)
- Modify: `internal/pipeline/scorecard_tier2_test.go` (update expected unscored lists and tier-1 denominator tests)
- Test: per-file `_test.go` for each scorer
- Modify: `internal/pipeline/scorecard.go` README-rendering of scorecard.md to include the new rows

**Approach:**
- Each scorer returns `(int, bool)` so absence yields `unscored` rather than 0. This follows the mcp_token_efficiency + cache_freshness precedent and keeps existing CLIs from regressing.
- `scoreMCPRemoteTransport`: 10 if both stdio and http branches present, 7 if http-only, 5 if stdio-only, unscored when no MCP emitted.
- `scoreMCPToolDesign`: baseline from intent tool ratio; small APIs (<10 endpoints) get unscored to avoid penalizing Dominos-sized APIs where intent-grouping doesn't help.
- `scoreMCPSurfaceStrategy`: unscored when endpoint count <= threshold. Above threshold, code-orch scores 10, intent-tool coverage scores proportionally, endpoint-mirror scores low.
- Update `scorecard.md` template to render the three rows and the scorer summary in `review.md`.

**Patterns to follow:**
- `internal/pipeline/mcp_size.go:scoreMCPTokenEfficiency` — canonical shape for opt-in scorers.
- `internal/pipeline/scorecard_tier2_test.go` — pattern for updating the unscored-dimensions assertion set.

**Test scenarios:**
- Happy path: default stdio endpoint-mirror CLI's total score is unchanged by this unit (new dims unscored for small APIs; sized-out for remote transport dim at 5/10 which is baseline).
- Happy path: CLI with both stdio and http transports scores 10 on MCPRemoteTransport.
- Happy path: CLI with intents + endpoints scores above baseline on MCPToolDesign.
- Happy path: CLI with 80 endpoints using code-orch scores 10 on MCPSurfaceStrategy.
- Edge case: CLI with no MCP emitted leaves all three dims unscored; tier-1 denominator math matches existing tests.
- Edge case: CLI with 80 endpoints using endpoint-mirror scores low on MCPSurfaceStrategy (article's anti-pattern is named by the scorer, not silently rewarded).
- Error path: malformed `tools-manifest.json` causes the scorer to mark dim as unscored and log, not crash.
- Regression: `TestScoreMCPTokenEfficiency_FullMarksForLeanSurface` and friends still pass.

**Verification:**
- `go test ./internal/pipeline/...` green, including all existing tier-1 denominator tests.
- Scorecard for a default stdio fixture shows unchanged total vs. pre-plan baseline (within 1 point of rounding).
- Scorecard for the code-orch fixture shows MCPSurfaceStrategy=10 and an explanation line in `scorecard.md`.

---

- [ ] U5. **Library sweep recommendations + doc updates**

**Goal:** For each existing CLI in `~/printing-press/library/`, produce a machine-generated recommendation on whether to enable remote transport, declare intent tools, or switch to code-orchestration. Update README and AGENTS.md so future CLIs default to the new patterns where appropriate.

**Requirements:** R6

**Dependencies:** U1, U2, U3, U4 (recommendations reference the new spec fields)

**Files:**
- Create: `internal/cli/mcp_audit.go` (a `printing-press mcp-audit <library-path>` subcommand that reports per-CLI recommendations)
- Modify: `README.md` (document the three new spec fields and the audit command)
- Modify: `AGENTS.md` (add "Remote-first MCP" as a default expectation and update the MCP glossary entry)
- Modify: `docs/PIPELINE.md` (note the new scoring dims in the review phase section)
- Modify: `skills/printing-press/SKILL.md` (surface the new spec fields in the generation prompt so the skill-driven fast path uses them)
- Modify: `catalog/*.yaml` — no changes; spec lives alongside the CLI, not in the catalog entry. Confirm this is still true before finishing.
- Test: `internal/cli/mcp_audit_test.go`

**Approach:**
- Audit inspects each library entry's `tools-manifest.json`, counts endpoints vs. intents, detects transport from the built main, and emits a one-line recommendation per CLI (e.g., "hubspot-pp-cli: 214 endpoints, endpoint-mirror — recommend `orchestration: code`").
- Audit output format: machine-readable JSON plus human-readable table; exit 0 regardless of findings (informational).
- README + AGENTS.md + PIPELINE.md updates land with this unit to keep documentation truthful.

**Patterns to follow:**
- `internal/cli/auth.go` — pattern for a read-only, library-walking subcommand.
- `internal/authdoctor/` — manifest-inspection precedent.

**Test scenarios:**
- Happy path: audit against a fixture library with three CLIs produces one recommendation per CLI matching the expected strategy.
- Edge case: library entry missing `tools-manifest.json` is reported as "unknown — manifest missing" and audit does not fail.
- Edge case: library entry with intents already declared and transport already remote is reported as "ok".
- Error path: library path does not exist returns a clear error and exit 1.

**Verification:**
- `printing-press mcp-audit ~/printing-press/library` returns a sensible table on the real local library.
- README, AGENTS.md, and PIPELINE.md render the new surface in a spot-check grep.
- Skill-driven fast path (`/printing-press`) on a new API respects the new spec fields without hand-intervention.

---

## System-Wide Impact

- **Interaction graph:** `internal/spec` -> `internal/generator` (templates + intent handler) -> published CLI's `internal/mcp` and `cmd/<api>-pp-mcp`. `internal/pipeline/scorecard.go` reads generator output. `internal/megamcp` already consumes `tools-manifest.json`; intent tools must appear there correctly so megamcp discovery works without change.
- **Error propagation:** spec validation errors surface at `generate` time with line numbers. Runtime transport errors (HTTP bind failure) surface at MCP startup with an exit code. `<api>_execute` unknown endpoint returns a structured MCP error; no panics.
- **State lifecycle risks:** none new — intent tools are stateless compositions over existing endpoint calls. Code-orchestration adds no server-side state.
- **API surface parity:** CLI side is unchanged. All additions are on the MCP side. The `CLAUDE.md`/`AGENTS.md` principle "agents can do anything users can" is preserved; these changes *expand* what agents can do.
- **Integration coverage:** U1's transport integration test and U2's intent end-to-end test must build and run the generated binary, not just unit-test the templates. U3's size test must measure actual emitted token count, not theoretical.
- **Unchanged invariants:** `ExtraCommands`, `tools-manifest.json` schema backward compat, megamcp activation flow, the 7 quality gates, dogfood's reimplementation check, and per-CLI `doctor` behavior. All existing printed CLIs continue to build with zero spec changes.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| mcp-go v0.47.0 does not expose a stable HTTP streamable server API | U1 starts with a feasibility check on the pinned version; if missing, pin to a newer `mcp-go` in the same PR and rerun `go.sum`. Document the version floor in AGENTS.md. |
| Intent binding DSL becomes a maintenance burden | Start with the narrowest syntax that handles the 2-3 reference intents (Linear create-issue-from-thread, Kalshi buy-contract-if-price, HubSpot update-deal-and-note). Resist adding conditionals until a real spec demands them. |
| Scorecard recalibration regresses existing published CLIs below `ship` threshold | Golden-score the current top 5 library CLIs before U4 lands; confirm totals stay within +/-2 points. Any drift blocks U4 from merging. |
| Code-orchestration `<api>_execute` becomes a vector for agent over-reach on write endpoints | `<api>_execute` respects the same `NoAuth` / method guards as direct endpoint tools. Add a `destructive_confirm: true` opt-in per-spec that elicitation will fulfill in the follow-up plan; meanwhile, log destructive calls prominently. |
| Anti-reimplementation rule silently breaks when intent handlers fan out | Dogfood's `reimplementation_check` must be extended in U2 to recognize intent handlers that call endpoint client methods indirectly via generated dispatch. Explicit test case required. |
| AI sensitivity on open-source contributor projects — agents generating large PRs for PP could trigger review friction | Not applicable here; this is a Matt-owned repo. Feature-flag anything that would change the behavior of downstream library CLIs pending operator sign-off. |

---

## Documentation / Operational Notes

- README additions: new `mcp:` spec block with the three knobs; link to the Anthropic article as the source of the patterns.
- AGENTS.md additions: "Remote-first MCP" expectation under a new subsection; update the glossary entry for `tools-manifest.json` to cover intent tool entries.
- PIPELINE.md updates: mention that the review phase scorecard now includes the three new MCP dimensions.
- SKILL.md (`skills/printing-press/SKILL.md`): update the generation prompt so the fast path asks about orchestration mode when endpoint count is high.
- CHANGELOG: release-please will pick up the `feat(cli):` commits; no manual editing.

---

## Sources & References

- Anthropic, "Building agents that reach production systems with MCP" (2026-04-22) — primary source of the three patterns targeted.
- `internal/generator/templates/main_mcp.go.tmpl` — current stdio-only entry point.
- `internal/generator/templates/mcp_tools.go.tmpl` — current endpoint-mirror registration.
- `internal/spec/spec.go` — spec schema; `ExtraCommands` is the precedent for additive optional blocks.
- `internal/pipeline/scorecard.go`, `internal/pipeline/mcp_size.go` — scoring patterns to follow.
- `internal/megamcp/` — library-level tool search already exists; stays untouched.
- Cloudflare's MCP server — reference for code-orchestration (~2,500 endpoints in ~1K tokens).
