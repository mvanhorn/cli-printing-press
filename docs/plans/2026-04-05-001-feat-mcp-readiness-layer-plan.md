---
title: "feat: MCP Readiness Layer — Per-Endpoint Auth Awareness and Public Library MCP Catalog"
type: feat
status: active
date: 2026-04-05
---

# feat: MCP Readiness Layer — Per-Endpoint Auth Awareness and Public Library MCP Catalog

## Overview

**Step 1 of 2** toward a Printing Press Mega MCP — a single installable MCP server that gives agents access to every API in the library.

This plan builds the data layer: per-endpoint auth awareness, MCP metadata in manifests and registry, self-describing `about` tools, marketplace metadata files, and minority-side auth annotations. Every artifact is designed to be consumed by the Mega MCP (Step 2, separate plan) which aggregates all CLIs into one server with catalog discovery and passthrough execution.

Without this plan, the Mega MCP has no data to work with — no way to know which tools need auth, what env vars to check, or which APIs are MCP-ready. Without the Mega MCP, this plan's metadata describes things users can't easily access. Both steps are needed; this one goes first because the data layer must be correct before the access layer is built on top.

This plan covers three layers: machine changes in this repo (generator, parser, templates, manifest, publish), public library repo changes (registry schema, README), and targeted fixups to the 6 existing published CLIs (without regeneration).

**Target repos:**
- **Primary:** `cli-printing-press` (this repo) — all machine changes
- **Secondary:** `mvanhorn/printing-press-library` — registry, README, and existing CLI fixups

## Problem Frame

Every printed CLI already ships an MCP binary — but without metadata, those binaries are invisible. MCP tool descriptions don't indicate auth requirements, the public library registry has no MCP fields, and marketplace listing metadata doesn't exist. This is table-stakes infrastructure: any tool that ships an MCP binary should be self-describing about what it can do and what credentials it needs.

The `fli` project (Google Flights CLI, 1,400 stars, 12K PyPI downloads/month) is an aspirational analogy, not a direct precedent — fli succeeded with a single high-demand API (Google Flights), while the printing press library covers niche APIs (Pagliacci Pizza, ESPN, Postman Explore). Whether marketplace presence drives adoption for *these* specific CLIs is unvalidated. The infrastructure is worth building regardless: it makes every MCP binary self-describing, gives agents auth hints before the first 401, and positions the library for when high-demand APIs are added.

Even cookie-auth CLIs like Pagliacci Pizza have public endpoints (store finder, menus) that work as MCP tools without any auth. But today, the MCP tool descriptions don't say this, the registry doesn't surface it, and users have no way to know which tools work out of the box.

## Requirements Trace

- R1. Per-endpoint `NoAuth` detection from OpenAPI specs (`security: []` override)
- R2. MCP tool descriptions annotate the **minority** auth side — only when a CLI has a mix of public and auth-required tools. If all tools share the same auth status, no annotations (the default speaks for itself).
- R3. CLI manifest extended with MCP metadata (binary name, tool counts, readiness level)
- R4. README template includes MCP server install section
- R5. Publish pipeline generates `smithery.yaml` marketplace metadata
- R6. Public library `registry.json` extended with MCP fields
- R7. Existing 6 published CLIs updated with auth annotations (no regeneration)
- R8. Scorecard reports public/auth tool split (informational, not new scored dimension)

## Scope Boundaries

- **In scope:** OpenAPI-sourced specs only. Sniffed/internal specs get `NoAuth` support later.
- **In scope:** Informational scorecard reporting of public tool counts. NOT a new scored dimension (avoids tier constant changes).
- **Out of scope:** Auto-submission to MCP marketplaces. We generate metadata files; humans decide whether to list.
- **Out of scope:** MCP OAuth2 native auth (MCP spec auth RFC). Worth monitoring; not building against today.
- **Out of scope:** Auto-refresh of cookies from MCP context. Separate feature.
- **Out of scope:** Changes to the MCP server binary structure or transport (stdio is correct).
- **Known limitation:** CLIs generated via `--docs` (no spec.json produced) will not have MCP metadata in the manifest or registry until a spec.json is produced. MCP fields are left empty — the publish skill treats an empty `MCPBinary` as "omit the `mcp` block from registry.json."
- **Registry includes all readiness levels:** CLIs with `mcp_ready: "cli-only"` still appear in the registry with an `mcp` block — the block signals "MCP binary exists but all tools need CLI auth setup." Smithery.yaml is NOT generated for cli-only CLIs (no useful MCP without browser setup).

## Context & Research

### Relevant Code and Patterns

- **Scorecard already parses per-operation security:** `parseSecurityRequirementSet()` at `internal/pipeline/scorecard.go:1113-1157` correctly handles `security: []` (empty array → `AllowsAnonymous = true`) and `security: [{}]` (empty object → anonymous). This is reference code for the parser change.
- **OpenAPI parser endpoint construction:** `mapResources()` at `internal/openapi/parser.go:800-851` iterates operations via `pathItem.Operations()`. Each `op` is `*openapi3.Operation` with `Security *SecurityRequirements` field. Currently ignored.
- **kin-openapi types:** `SecurityRequirements` is `[]SecurityRequirement`. When spec says `security: []`, kin-openapi sets `op.Security` to non-nil pointer to empty slice. When no per-operation security is declared, `op.Security` is nil (inherits global).
- **Template conditionals:** Existing pattern `{{- if and .Auth.Type (ne .Auth.Type "none")}}` in `mcp_tools.go.tmpl` shows how auth-conditional rendering works.
- **Endpoint.Meta:** Exists as `map[string]string` on Endpoint but is only used by crowd-sniff for source provenance. A dedicated `NoAuth bool` field is cleaner and matches the pattern of `Required`, `Positional` on `Param`.
- **CLIManifest:** Schema version 1, clean struct at `internal/pipeline/climanifest.go:27-41`. Adding fields with `omitempty` is backward-compatible.
- **Publish skill registry schema:** Documented in `skills/printing-press-publish/SKILL.md` lines 564-579.

### Institutional Learnings

- **Scorecard update is mandatory when adding capabilities** (AGENTS.md + `docs/solutions/logic-errors/scorecard-accuracy-broadened-pattern-matching`). For this feature, we add informational reporting only — no new scored dimension — so tier constants stay unchanged.
- **Traversal protection for spec-derived strings** (`docs/solutions/security-issues/filepath-join-traversal-with-user-input`). MCP binary names derived from API slugs flow through existing `naming.CLI()` which is already safe. Smithery.yaml values are written to known paths, not user-derived paths.
- **Validation must not mutate source directory** (`docs/solutions/best-practices/validation-must-not-mutate-source-directory`). The smithery.yaml is written during `publish package` (which copies to staging), not during validation.
- **Layout contract** (`docs/solutions/best-practices/checkout-scoped-printing-press-output-layout`). MCP metadata extends `.printing-press.json` (the provenance manifest), not a separate file. Smithery.yaml is a new file alongside it in the published CLI directory.

## Key Technical Decisions

- **Dedicated `NoAuth bool` field over `Meta["no_auth"]`:** Type-safe, template-friendly (`{{if .NoAuth}}`), matches existing boolean field patterns on `Param`. Serialized as `no_auth,omitempty` to stay backward-compatible with existing YAML specs.
- **Detection logic: explicit override only, not inheritance:** Only set `NoAuth = true` when `op.Security` is non-nil AND empty. When `op.Security` is nil, the operation inherits global auth — which could be anything. This avoids false positives where a spec has no global security but individual operations aren't explicitly marked.
- **Post-parse sweep for no-auth specs, guarded by `!Auth.Inferred`:** When `Auth.Type == "none"` AND `!Auth.Inferred`, mark all endpoints `NoAuth = true`. The `Inferred` field is set to `true` by the parser when auth was guessed from description keywords (e.g., "Bearer" found in description text). In practice, the keyword-inference paths set a non-none type when they find a match, so `Type == "none" && Inferred == true` is unlikely. The `!Inferred` guard acts as a safety catch — verify during implementation whether this combination can actually occur and add a concrete test if so.
- **MCP binary naming: `{name}-pp-mcp` with `-pp-` infix:** Consistent with the CLI's `{name}-pp-cli` collision avoidance rationale. If the infix was worth doing for CLIs, it's worth doing for MCPs — especially since MCP marketplaces are where vendor collisions are most likely. The generator currently produces `{name}-mcp` (no infix) and all 6 published CLIs use that convention. This plan renames to `{name}-pp-mcp` everywhere: generator template, naming function, published CLI directories, goreleaser configs, smithery listings, and registry entries. The rename is cheapest now — before any marketplace listings exist and before users have wired `claude mcp add` configs.
- **`naming.MCP()` function:** Add to `internal/naming/naming.go` returning `name + "-pp-mcp"`. Update the generator at `generator.go:441` to use this function instead of the current inline `name + "-mcp"`. Centralizes the convention and applies the `-pp-` infix consistently.
- **Minority-side annotation logic:** Only annotate when a CLI has a mix of public and auth-required tools. Annotate whichever side is the minority:
  - All tools same auth status → no annotations (default speaks for itself)
  - Mixed, public is minority → append `(public)` on public tools
  - Mixed, auth-required is minority → append auth-type-specific suffix on auth-required tools: `(requires API key)`, `(requires auth)`, or `(requires browser login)`
  The `mcpDescription` template function handles this: it takes the endpoint description, `NoAuth` flag, the spec-level auth type, and the total/public tool counts. It decides whether and how to annotate based on the minority logic, then applies oneline cleanup/truncation. Prepending/appending is chosen so the annotation is always visible after truncation.
- **`GenerateManifestParams` needs the parsed spec:** `WriteManifestForGenerate()` currently takes only `APIName`, `SpecSrcs`, `DocsURL`, `OutputDir` — no access to the parsed `*spec.APISpec` for tool counting. Add a `Spec *spec.APISpec` field to `GenerateManifestParams` and thread it through both callers in `internal/cli/root.go` (lines 189 and 331). Both callers already have the parsed spec in scope (`parsed` and `apiSpec` respectively). For `writeCLIManifestForPublish()`, which operates on `PipelineState`, re-parse the spec from the output directory's `spec.json` — the file is always present at publish time.
- **`AuthEnvVars` in manifest:** Add `AuthEnvVars []string` to `CLIManifest` alongside `AuthType`. Without this, the publish skill cannot populate `env_vars` in the registry.json `mcp` block — it would have to parse CLI source code. The manifest is the right place to carry this data from generation to publish.
- **Informational scorecard reporting, not a new dimension:** Adding a scored MCP dimension would require tier constant changes, unscored handling for non-MCP CLIs, and broader calibration. Instead, the scorecard's summary output gets a one-line note: "MCP: 42 tools (15 public, 27 auth-required)". This satisfies the AGENTS.md rule (scorer reflects capability) without inflating scores.
- **Scorecard reads MCP counts from manifest, not by re-parsing:** The scorecard reads `.printing-press.json` (which already exists when the scorecard runs) and uses `MCPToolCount` / `MCPPublicToolCount` from Unit 4. This avoids calling `openapi.Parse()` from the scorecard, which would be disproportionate — adding a full-spec-parse dependency and latency (especially for large specs like Steam's 164 tools) for an informational one-line summary.
- **Smithery.yaml generated at publish time, not generate time:** The smithery file needs category, description, and auth metadata that may be enriched during publish. It lives alongside `.printing-press.json` in the published directory.
- **Registry.json `mcp` block is additive:** New fields are optional. Existing registry entries without `mcp` remain valid. The publish skill adds `mcp` when packaging.
- **Smithery.yaml `env` semantics for partial-readiness CLIs:** For cookie/composed CLIs, env vars are marked `required: false` with a description noting "required for authenticated endpoints only — some tools work without credentials." This accurately reflects MCP server startup behavior (server starts fine without cookies) while signaling that not all tools will work.

## Open Questions

### Resolved During Planning

- **Should `NoAuth` go on Endpoint or AuthConfig?** → Endpoint. Auth config is spec-level. NoAuth is per-endpoint. These are different granularities.
- **Should the OpenAPI parser also detect when the global security array is empty?** → Yes, handled via post-parse sweep. Guarded by `Auth.Type == "none" && !Auth.Inferred` to avoid false positives from specs that simply omit security declarations.
- **What about operations that have security but also allow anonymous (`security: [{}, {"api_key": []}]`)?** → Set `NoAuth = true`. If anonymous is one of the alternatives, the tool works without auth. The agent can always provide auth for better results.
- **How does `WriteManifestForGenerate` get spec data for tool counts?** → Add `Spec *spec.APISpec` to `GenerateManifestParams`. Both callers in `root.go` already have the parsed spec in scope.
- **Should MCP binaries use `-pp-` infix?** → Yes. `{name}-pp-mcp` everywhere — binary, marketplace listing, registry, naming function. Consistent with CLI's `{name}-pp-cli` collision avoidance. Cheapest to rename now before any marketplace listings exist. The 6 published CLIs get renamed in the same library repo PR as the auth annotations (Unit 9).
- **How does the scorecard access NoAuth data?** → Read the `.printing-press.json` manifest which already has `MCPToolCount` and `MCPPublicToolCount` from Unit 4. Avoids calling the full parser from the scorecard.
- **Where do env var names come from in the registry?** → Add `AuthEnvVars []string` to `CLIManifest`, populated from `spec.Auth.EnvVars` at generation time. Publish skill reads from manifest.
- **How to handle `oneline()` truncation with auth annotation?** → `mcpDescription()` template function applies the minority-side annotation (prepend or append), then calls `oneline()` internally. Annotation is part of the truncation input.
- **Should we annotate all public tools or all auth-required tools?** → Neither uniformly. Annotate the minority side only, and only when the CLI has a mix. If all tools share the same auth status, no annotations — the default speaks for itself. Auth-type-specific suffixes (`(requires API key)`, `(requires browser login)`) tell the agent what action is needed, not just that auth is needed.

### Deferred to Implementation

- **Smithery.yaml field names** — verify against current Smithery docs (https://smithery.ai/docs) at the start of Unit 6 implementation. If the schema has changed from the assumed `name`/`description`/`startCommand`/`env` structure, adjust before writing the generator code.
- **Which Pagliacci/Steam endpoints are actually public** — determined during Unit 9 by making unauthenticated HTTP requests to each candidate endpoint. If 200 with meaningful data → public. If 401/403 → auth-required. Results documented in the PR.

## Implementation Units

### This Repo (cli-printing-press)

- [ ] **Unit 1: Add `NoAuth` field to Endpoint spec**

  **Goal:** Extend the spec model so endpoints can declare whether they require auth.

  **Requirements:** R1

  **Dependencies:** None

  **Files:**
  - Modify: `internal/spec/spec.go`
  - Modify: `internal/spec/spec_test.go`

  **Approach:**
  Add `NoAuth bool` to `Endpoint` struct with `yaml:"no_auth,omitempty" json:"no_auth,omitempty"` tags. Place it after `Meta` to keep auth-related fields grouped. Ensure the `Validate()` method doesn't reject it.

  **Patterns to follow:**
  - `Param.Required`, `Param.Positional` — same boolean-with-omitempty pattern
  - `Endpoint.Meta` — neighboring field in the struct

  **Test scenarios:**
  - Happy path: Endpoint with `NoAuth: true` round-trips through YAML marshal/unmarshal
  - Happy path: Endpoint with `NoAuth: false` (or unset) omits the field in JSON/YAML output
  - Edge case: Existing spec fixtures that don't have `no_auth` still parse without error (backward compat)

  **Verification:** `go test ./internal/spec/...` passes. Existing parser tests unaffected.

- [ ] **Unit 2: Parse per-operation `security: []` in OpenAPI parser**

  **Goal:** Detect when an OpenAPI operation explicitly opts out of auth and set `NoAuth = true` on the corresponding endpoint.

  **Requirements:** R1

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `internal/openapi/parser.go` (in `mapResources()`, ~line 839-845)
  - Modify: `internal/openapi/parser_test.go`
  - Create: `testdata/openapi/mixed-auth.yaml` (test fixture)

  **Approach:**
  Two detection paths:

  **Path 1 — Per-operation detection (in `mapResources()`):**
  After constructing each `endpoint` struct (line 839), check `op.Security`:
  - If `op.Security != nil && len(*op.Security) == 0` → `endpoint.NoAuth = true` (explicit `security: []`)
  - If `op.Security != nil` and any element is an empty map → `endpoint.NoAuth = true` (anonymous alternative)
  - If `op.Security == nil` → leave `NoAuth` as false (inherits global)

  **Path 2 — Global security detection (in `mapResources()`, `doc` already available as first parameter):**
  Also check the global `security` array on the OpenAPI document. When `doc.Security` is non-nil but empty (`security: []` at root level), ALL operations without their own per-operation security inherit anonymous access. For each endpoint where `op.Security == nil` and global security is explicitly empty, set `endpoint.NoAuth = true`. This handles specs that define securitySchemes (for optional use) but declare `security: []` globally.

  **Path 3 — Post-parse sweep:**
  After all resources are built, if `out.Auth.Type == "none"`, iterate all endpoints and set `NoAuth = true`. The `Auth.Inferred` field is set to `true` by the parser when auth was guessed from description keywords (e.g., "Bearer" mentioned in description). The combination `Auth.Type == "none" && Auth.Inferred == true` is unlikely in practice because the keyword-inference paths set a non-none type when they find a match. The `!Inferred` guard acts as a safety catch — verify during implementation whether this case can actually occur, and add a concrete test if so.

  Reference `parseSecurityRequirementSet()` in `scorecard.go:1113-1157` for the anonymous-detection logic.

  **Patterns to follow:**
  - `scorecard.go` `parseSecurityRequirementSet()` — same security array interpretation
  - Existing `mapResources()` patterns for setting endpoint fields

  **Test scenarios:**
  - Happy path: Operation with `security: []` → `endpoint.NoAuth == true`
  - Happy path: Operation with `security: [{}]` (empty object alternative) → `endpoint.NoAuth == true`
  - Happy path: Operation with `security: [{"api_key": []}]` (normal auth) → `endpoint.NoAuth == false`
  - Happy path: Operation with no per-operation security (nil) inheriting global auth → `endpoint.NoAuth == false`
  - Happy path: Spec with global `security: []` (root level) → operations without own security get `NoAuth == true`
  - Happy path: Spec with no securitySchemes and no global security (`Auth.Type == "none"`, `Inferred: false`) → all endpoints get `NoAuth == true`
  - Edge case: Spec with securitySchemes defined but global `security: []` → operations inherit anonymous despite schemes existing
  - Edge case: Mixed spec — some operations with `security: []`, some with auth, some inheriting → correct per-endpoint flags
  - Edge case: Spec with `Auth.Type == "none"` but `Inferred: true` → endpoints do NOT get `NoAuth == true` (fails safe)
  - Edge case: Existing petstore fixture still parses identically (no regression)

  **Verification:** `go test ./internal/openapi/...` passes including new tests. Petstore tests unchanged.

- [ ] **Unit 3: Annotate MCP tool descriptions with auth status**

  **Goal:** MCP tool descriptions annotate the minority auth side — helping agents identify which tools need setup without cluttering descriptions when the answer is uniform.

  **Requirements:** R2

  **Dependencies:** Unit 1, Unit 2

  **Approach:**
  The annotation logic depends on the mix of public vs auth-required tools across the whole CLI:

  - **All tools same auth status** → no annotations. If everything needs an API key, the agent learns that from the first 401 hint or the `about` tool (Unit 3b). If everything is public, nothing to say.
  - **Mixed, public is minority** → append `(public)` on public tools. Example: Pagliacci has 41 tools, 7 public → the 7 get `Find store locations (public)`.
  - **Mixed, auth-required is minority** → append auth-type-specific suffix on auth-required tools:
    - API key: `(requires API key)`
    - OAuth/bearer: `(requires auth)`
    - Cookie/composed: `(requires browser login)`

  Register a template function `mcpDescription(desc string, noAuth bool, authType string, publicCount int, totalCount int) string` in `generator.go`'s FuncMap. The function:
  1. Determines whether to annotate (mixed tools only) and which side is the minority
  2. Prepends or appends the appropriate annotation
  3. Calls `oneline()` internally on the combined string (reusing all sanitization + truncation)

  The template passes spec-level data: `{{mcpDescription $endpoint.Description $endpoint.NoAuth .Auth.Type .MCPPublicCount .MCPTotalCount}}`. The generator pre-computes `MCPPublicCount` and `MCPTotalCount` and adds them to the template data struct.

  **Files:**
  - Modify: `internal/generator/templates/mcp_tools.go.tmpl`
  - Modify: `internal/generator/generator.go` (register `mcpDescription` in FuncMap, add counts to template data)
  - Modify: `internal/generator/generator_test.go`

  **Patterns to follow:**
  - Existing template function registration: `"oneline": oneline` in FuncMap at `generator.go:104`
  - Generator test pattern: `gen.Generate()` then `os.ReadFile()` and `assert.Contains()`

  **Pre-existing context:** `generator.go:439` creates `cmd/{name}-mcp/` and renders `main_mcp.go.tmpl` guarded by `VisionSet.MCP || true` (always runs). But `generator.go:579` renders `mcp_tools.go.tmpl` guarded by just `VisionSet.MCP`. The `mcpDescription` change only takes effect when `tools.go` is rendered. In practice, `VisionSet.MCP` is always true today (`|| true` override), but test scenarios should set the flag explicitly to be safe.

  **Test scenarios:**
  - Happy path: All-auth-required spec (api_key, 0 public) → no annotations on any tool
  - Happy path: All-public spec (Auth.Type == "none") → no annotations on any tool
  - Happy path: Mixed spec, public is minority (30 auth, 5 public) → public tools get `(public)` suffix, auth tools unannotated
  - Happy path: Mixed spec, auth is minority (30 public, 5 auth, api_key) → auth tools get `(requires API key)` suffix, public tools unannotated
  - Happy path: Mixed cookie auth (30 cookie, 5 public) → public tools get `(public)`, cookie tools unannotated (cookie is majority)
  - Edge case: Exactly 50/50 split → annotate the auth-required side (when tied, mark what needs setup)

  **Verification:** `go test ./internal/generator/...` passes. Generated MCP tools file compiles.

- [ ] **Unit 3b: Add `about` tool to every MCP server**

  **Goal:** Every MCP server has a self-describing `about` tool that returns API identity, auth requirements, tool count, and transcendence features — so agents can ask "what can you do?" at runtime.

  **Requirements:** R2, R3

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `internal/generator/templates/mcp_tools.go.tmpl`
  - Modify: `internal/generator/generator.go` (pass novel features to MCP template data)

  **Approach:**
  Register an `about` tool in `RegisterTools()` that returns a JSON object:
  - `api`: API name and description (from spec)
  - `tool_count`: total tools registered
  - `auth`: type, env var names, key URL, readiness level
  - `unique_capabilities`: array of transcendence features from `NovelFeatures` (name, command, description) — only present when the CLI has novel features

  The generator already passes `NovelFeatures` to the README template data. Pass the same list to the MCP template data struct. The `about` handler is a static response — no API calls, no config needed. It works even when auth isn't configured, which makes it the natural first tool an agent calls.

  **Patterns to follow:**
  - Existing vision tool registration pattern (sync, search, sql) in `mcp_tools.go.tmpl`
  - `readmeData()` pattern for threading `NovelFeatures` through template data

  **Test scenarios:**
  - Happy path: CLI with novel features → `about` tool response includes `unique_capabilities` array
  - Happy path: CLI without novel features → `about` tool response omits `unique_capabilities`
  - Happy path: CLI with api_key auth → `about` response includes env var name and key URL
  - Happy path: CLI with no auth → `about` response shows auth type "none"
  - Edge case: `about` tool works even when auth is not configured (no API call needed)

  **Verification:** `go test ./internal/generator/...` passes. Generated `about` tool compiles and returns valid JSON.

- [ ] **Unit 4: Add MCP metadata to CLI manifest**

  **Goal:** The `.printing-press.json` provenance manifest includes MCP metadata so published CLIs are self-describing for MCP capabilities.

  **Requirements:** R3

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `internal/pipeline/climanifest.go`
  - Modify: `internal/pipeline/publish.go` (where manifest is populated)
  - Modify: `internal/naming/naming.go` (add `MCP()` function)
  - Modify: `internal/cli/root.go` (thread spec to manifest writer at lines 189 and 331)
  - Modify: `internal/generator/generator.go` (replace inline `name + "-mcp"` with `naming.MCP()`)
  - Modify: `internal/generator/templates/main_mcp.go.tmpl` (update server name string)
  - Modify: `internal/generator/templates/makefile.tmpl` (update MCP build target binary name)
  - Modify: `internal/generator/templates/goreleaser.yaml.tmpl` (update MCP build ID and binary name)

  **Approach:**
  Add fields to `CLIManifest`:
  - `MCPBinary string` (`json:"mcp_binary,omitempty"`) — e.g., "notion-pp-mcp"
  - `MCPToolCount int` (`json:"mcp_tool_count,omitempty"`) — total tools registered
  - `MCPPublicToolCount int` (`json:"mcp_public_tool_count,omitempty"`) — tools with `NoAuth`
  - `MCPReady string` (`json:"mcp_ready,omitempty"`) — "full", "partial", or "cli-only"
  - `AuthType string` (`json:"auth_type,omitempty"`) — from spec auth config
  - `AuthEnvVars []string` (`json:"auth_env_vars,omitempty"`) — from spec.Auth.EnvVars
  - `NovelFeatures []NovelFeatureManifest` (`json:"novel_features,omitempty"`) — transcendence features from `research.json`'s `novel_features_built`. Each entry: `{name, command, description}`. The mega MCP's `library_info` tool surfaces these as highlights.

  **Data plumbing for `WriteManifestForGenerate()`:** Add `Spec *spec.APISpec` to `GenerateManifestParams`. Both callers in `root.go` already have the parsed spec in scope — the `--docs` path has `parsed` (line 189) and the `--spec` path has `apiSpec` (line 331). Thread it through. Tool counts are computed by a new helper `countMCPTools(spec *spec.APISpec) (total, public int)` that iterates `spec.Resources` and their sub-resources.

  **Data plumbing for `writeCLIManifestForPublish()`:** `PipelineState` does not carry the parsed spec. Re-parse from `spec.json` in the working directory via `openapi.Parse()`. Three callers: `publish.go:109` (PublishWorkingCLI), `lock.go:221` (PromoteWorkingCLI), and indirectly via the pipeline. All have access to the working directory. If `openapi.Parse()` fails or spec.json is missing (e.g., `--docs` runs), log a warning and leave MCP fields empty (non-blocking). The publish skill treats an empty `MCPBinary` as "omit the `mcp` block."

  Add `naming.MCP(name string) string` returning `name + "-pp-mcp"` to `internal/naming/naming.go`. Update both inline `g.Spec.Name+"-mcp"` references in generator.go (the directory path `cmd/{name}-mcp` and the template rendering path `cmd/{name}-mcp/main.go`) to use `naming.MCP()`.

  **MCP binary rename — all template occurrences:**
  - `goreleaser.yaml.tmpl`: build id (line 21), main path (line 22), binary name (line 23), brew install (line 54) — 4 occurrences of `{{.Name}}-mcp`
  - `makefile.tmpl`: build-mcp target (line 20, 2 occurrences), install-mcp target (line 23, 1 occurrence) — 3 string replacements across 2 lines
  - `main_mcp.go.tmpl`: server name string (line 16) — 1 occurrence
  - Verification: grep generated output for old suffix `-mcp"` (without `-pp-`) to confirm no remnants

  MCP readiness is computed: "full" if auth is `none` or `api_key`/`bearer_token` (env-var passable), "partial" if some endpoints are public but auth is cookie/composed, "cli-only" if all endpoints need cookie/composed auth and none are public.

  **Patterns to follow:**
  - Existing `CLIManifest` field patterns with `omitempty`
  - `WriteManifestForGenerate()` catalog lookup enrichment pattern
  - `naming.CLI()` pattern for `naming.MCP()`

  **Test scenarios:**
  - Happy path: Manifest for api-key CLI → `mcp_ready: "full"`, correct tool counts
  - Happy path: Manifest for no-auth CLI → `mcp_ready: "full"`, all tools are public
  - Happy path: Manifest for cookie CLI with mixed endpoints → `mcp_ready: "partial"`, public count < total
  - Happy path: Manifest for cookie CLI with no public endpoints → `mcp_ready: "cli-only"`
  - Edge case: Manifest fields omitted when zero (backward compat with schema_version 1)

  **Verification:** `go test ./internal/pipeline/...` passes. Manifests are valid JSON.

- [ ] **Unit 5: Add MCP section to README template**

  **Goal:** Every generated CLI README includes instructions for using the MCP server.

  **Requirements:** R4

  **Dependencies:** Unit 1

  **Files:**
  - Modify: `internal/generator/templates/readme.md.tmpl`

  **Approach:**
  Add a new "## Use as MCP Server" section after the "Agent Usage" section. Content varies by auth type:
  - API key: show `claude mcp add` with `-e ENV_VAR=<key>` and link to key URL
  - OAuth/bearer: show `claude mcp add` and note to run CLI `auth login` first
  - Cookie/composed: show `claude mcp add` and note partial availability, list that some tools work without auth
  - No auth: show bare `claude mcp add` — works immediately

  Also include Claude Desktop JSON config snippet for all auth types.

  **Patterns to follow:**
  - Existing auth-type conditional blocks in the README template (lines 24-83)
  - `{{.Name}}-pp-cli` naming pattern

  **Test scenarios:**
  - Test expectation: none — README template rendering is verified indirectly through compilation tests and manual review. No behavioral logic to unit test.

  **Verification:** Generated README for a test spec includes the MCP section with correct auth-type-specific content.

- [ ] **Unit 6: Generate smithery.yaml at publish time**

  **Goal:** The publish pipeline produces a `smithery.yaml` marketplace metadata file alongside each published CLI.

  **Requirements:** R5

  **Dependencies:** Unit 4

  **Files:**
  - Modify: `internal/pipeline/publish.go` (companion to `writeCLIManifestForPublish()`)

  **Approach:**
  Write a `smithery.yaml` as a companion function called from the same sites as `writeCLIManifestForPublish()`. There are two callers:
  1. `PublishWorkingCLI()` at `internal/pipeline/publish.go:109`
  2. `PromoteWorkingCLI()` at `internal/pipeline/lock.go:221` (primary fullrun path — the pipeline calls this)

  Content is derived from the manifest:
  - `name`: MCP binary name
  - `description`: from manifest description (no spec-source suffix — keep it clean for marketplace display)
  - `startCommand.command`: `./` + MCP binary name
  - `env`: map of env var name → `{description, required}` from auth config

  Only write smithery.yaml when `MCPReady` is "full" or "partial". Skip for "cli-only" (no useful MCP without browser setup).

  Apply traversal protection per institutional learning: smithery values don't flow into path construction, but validate that the MCP binary name matches expected patterns.

  **Patterns to follow:**
  - `WriteCLIManifest()` pattern — write file to dir with known name
  - Non-mutating publish pattern — write during `publish package`, not during `publish validate`

  **Test scenarios:**
  - Happy path: API-key CLI → smithery.yaml with env var marked required
  - Happy path: No-auth CLI → smithery.yaml with no env section
  - Happy path: Cookie CLI with partial readiness → smithery.yaml generated with env vars optional
  - Edge case: CLI-only readiness → no smithery.yaml written

  **Verification:** `go test ./internal/pipeline/...` passes. Generated smithery.yaml is valid YAML.

- [ ] **Unit 7: Report MCP tool split in scorecard**

  **Goal:** The scorecard summary includes an informational line about MCP tool counts (public vs auth-required) without adding a new scored dimension.

  **Requirements:** R8

  **Dependencies:** Unit 4

  **Files:**
  - Modify: `internal/pipeline/scorecard.go`

  **Approach:**
  In the scorecard's summary output section, add a one-line note when MCP tools exist: "MCP: {total} tools ({public} public, {auth} auth-required)". This is informational only — does not affect the score, tier constants, or grade.

  Read the CLI manifest (`.printing-press.json`) from the CLI directory and use the `MCPToolCount` and `MCPPublicToolCount` fields from Unit 4. This avoids calling `openapi.Parse()` from the scorecard (which would be disproportionate — adding a full-spec-parse dependency for an informational one-line summary). The manifest is always available when the scorecard runs because the pipeline writes it before scoring.

  If the manifest has no MCP fields (empty `MCPBinary`), skip the MCP line.

  **Patterns to follow:**
  - Existing summary/gap reporting in scorecard output
  - Manifest reading pattern already used elsewhere in the pipeline

  **Test scenarios:**
  - Happy path: Scorecard for CLI with mixed auth endpoints → summary line shows correct split
  - Happy path: Scorecard for CLI with all-public endpoints → summary shows "X tools (X public, 0 auth-required)"
  - Edge case: Scorecard for CLI without MCP → no MCP line in summary
  - Edge case: Scorecard for CLI directory without `.printing-press.json` (standalone usage) → no MCP line, no error

  **Verification:** `go test ./internal/pipeline/...` passes. Scorecard output includes MCP line when applicable.

### Public Library Repo (mvanhorn/printing-press-library)

- [ ] **Unit 8: Extend registry.json with MCP metadata**

  **Goal:** Each entry in `registry.json` includes an `mcp` block with binary name, tool counts, auth type, and readiness level.

  **Requirements:** R6

  **Dependencies:** Unit 4 (schema), Unit 9 (Pagliacci/Steam public tool counts)

  **Files:**
  - Modify: `registry.json` (in `mvanhorn/printing-press-library`)

  **Approach:**
  Add `mcp` object to each of the 6 entries:
  ```json
  "mcp": {
    "binary": "<name>-pp-mcp",
    "transport": "stdio",
    "tool_count": <N>,
    "public_tool_count": <N>,
    "auth_type": "<api_key|none|composed>",
    "env_vars": ["<VAR>"],
    "mcp_ready": "<full|partial|cli-only>"
  }
  ```

  Values per CLI:
  - `dub-pp-cli`: 53 tools, api_key (DUB_TOKEN), mcp_ready: full
  - `espn-pp-cli`: 3 tools, none, mcp_ready: full (all public)
  - `linear-pp-cli`: tools TBD, api_key (LINEAR_TOKEN), mcp_ready: full
  - `pagliacci-pizza-pp-cli`: 41 tools, composed, mcp_ready: partial (public endpoints TBD during fixup)
  - `postman-explore-pp-cli`: 9 tools, none, mcp_ready: full (all public)
  - `steam-web-pp-cli`: 164 tools, api_key (STEAM_WEB_API_KEY), mcp_ready: full

  **Sequencing note:** `public_tool_count` values for Pagliacci and Steam depend on Unit 9 completion (HTTP verification of which endpoints are public). Finalize those registry values after Unit 9.

  **Patterns to follow:**
  - Existing registry.json entry schema
  - Additive schema extension (new fields, existing fields unchanged)

  **Test scenarios:**
  - Test expectation: none — registry.json is a data file. Validated by the publish skill reading it successfully.

  **Verification:** `registry.json` is valid JSON. All 6 entries have `mcp` blocks.

- [ ] **Unit 9: Annotate existing CLI MCP tool descriptions**

  **Goal:** Each published CLI gets two changes: (1) MCP binary renamed from `{name}-mcp` to `{name}-pp-mcp` for collision avoidance, and (2) minority-side auth annotations added to tool descriptions in `internal/mcp/tools.go`. Targeted source edits — no regeneration.

  **Requirements:** R7

  **Dependencies:** None (can run in parallel with machine changes)

  **Files:**
  For each of the 6 CLIs:
  - Rename: `cmd/{name}-mcp/` → `cmd/{name}-pp-mcp/`
  - Modify: `cmd/{name}-pp-mcp/main.go` (update server name string from `"{name}-mcp"` to `"{name}-pp-mcp"`)
  - Modify: `internal/mcp/tools.go` (auth annotations)
  - Modify: `Makefile` (update MCP build target)
  - Modify: `.goreleaser.yaml` (update MCP binary name and build ID)
  - Modify: `README.md` (update MCP binary name in install/usage instructions)
  - Grep all files for old binary name (`{name}-mcp` without `-pp-`) to catch remaining references in Dockerfiles, CI configs, and shell scripts

  **Approach:**
  For each CLI, rename the MCP binary and annotate public endpoints:

  **Step 0 — Rename MCP binary:**
  For all 6 CLIs: rename `cmd/{name}-mcp/` to `cmd/{name}-pp-mcp/`, update the server name string in `main.go`, update `Makefile` and `.goreleaser.yaml` build targets. This is a mechanical find-and-replace per CLI.

  **Step 1 — Classify endpoints and determine annotation strategy per CLI:**
  - **espn, postman-explore**: Auth type is `none` → ALL endpoints are public. No annotations needed (uniform).
  - **dub, linear**: Auth type is api_key → ALL endpoints require auth. No annotations needed (uniform).
  - **pagliacci-pizza**: Composed cookie auth. For each candidate public endpoint (store finder, menu, location search), make one actual HTTP request without credentials using `curl`. If 200 with meaningful data → public. If 401/403 → auth-required. Document results in the PR. Public endpoints are the minority → append `(public)` on them.
  - **steam-web**: API key auth but Steam has many public endpoints. Same verification protocol: test each endpoint path with `curl` without the API key. Count public vs auth-required. Annotate the minority side:
    - If most tools are public → append `(requires API key)` on auth-required tools
    - If most tools need auth → append `(public)` on public tools

  **Step 2 — Annotate `tools.go`:**
  Apply the minority-side annotation to tool descriptions in `mcplib.WithDescription("...")` calls. Be conservative — only annotate endpoints verified via actual HTTP requests.

  **Step 3 — Verify:**
  After editing, run `gofmt -w` on each modified file, then `go build ./...` in each CLI directory.

  **Patterns to follow:**
  - Minority is public → append ` (public)` before closing `"`
  - Minority is auth-required → append ` (requires API key)` (or `(requires browser login)`) before closing `"`

  **Test scenarios:**
  - Happy path: espn tools.go → no annotations (all public, uniform)
  - Happy path: dub tools.go → no annotations (all auth, uniform)
  - Happy path: pagliacci tools.go → verified-public tools get `(public)` suffix, auth tools unannotated
  - Happy path: steam tools.go → minority side annotated based on actual count
  - Edge case: Annotations don't break Go compilation — verify with `go build ./...` in each CLI dir

  **Verification:** Each modified CLI compiles: `cd <cli-dir> && go build ./...`. Every annotated endpoint was verified with an actual unauthenticated HTTP request. Verification results documented in the PR body.

- [ ] **Unit 10: Update library README with dual-interface install paths**

  **Goal:** The public library README shows both CLI and MCP install options for each published CLI.

  **Requirements:** R6

  **Dependencies:** Unit 8

  **Files:**
  - Modify: `README.md` (in `mvanhorn/printing-press-library`)

  **Approach:**
  Update the library's main README to include MCP install commands alongside CLI install commands for each entry. Show the `claude mcp add` one-liner with the correct env vars. Group entries by MCP readiness level to highlight what works immediately vs. what needs setup.

  Also update the library's positioning to acknowledge the dual-interface story: "The printing press generates both CLIs and MCP servers from the same spec. CLIs are the efficiency layer — fewer tokens, composable with pipes, works with any shell-based agent. MCP servers are the discovery layer — show up in Claude Desktop, Cursor, and marketplace listings. Use the CLI to set up auth and explore interactively. Use the MCP to let your AI editor call the API."

  **Patterns to follow:**
  - Existing README structure in the library repo

  **Test scenarios:**
  - Test expectation: none — documentation file, validated by manual review.

  **Verification:** README renders correctly in GitHub. All 6 CLIs have both install paths shown.

### Publish Skill Update (this repo)

- [ ] **Unit 11: Update publish skill to write MCP registry fields**

  **Goal:** The `/printing-press-publish` skill populates the `mcp` block in `registry.json` entries when packaging CLIs.

  **Requirements:** R6

  **Dependencies:** Unit 4, Unit 8

  **Files:**
  - Modify: `skills/printing-press-publish/SKILL.md`

  **Approach:**
  Update the registry entry construction in the publish skill to include the `mcp` block. The skill reads `.printing-press.json` for metadata — the new MCP fields from Unit 4 provide `mcp_binary`, `mcp_tool_count`, `mcp_public_tool_count`, `mcp_ready`, and `auth_type`. The skill maps these into the registry entry format.

  Also update the skill's documentation of the registry schema to include the new `mcp` block.

  **Patterns to follow:**
  - Existing registry entry construction in SKILL.md Step 8

  **Test scenarios:**
  - Test expectation: none — skill definitions are tested through end-to-end pipeline runs, not unit tests.

  **Verification:** Skill documentation accurately reflects the new registry schema. A manual publish dry-run produces correct registry entries.

## System-Wide Impact

- **Interaction graph:** The `NoAuth` field flows: OpenAPI parser → spec model → generator templates (MCP + potentially CLI help) → manifest → publish pipeline → registry.json → library README. Each hop is additive — no existing behavior changes.
- **Error propagation:** No new error paths. `NoAuth` defaults to `false` (zero value), so missing data fails safe (assume auth required).
- **State lifecycle risks:** None. `NoAuth` is computed at parse time and immutable thereafter.
- **API surface parity:** The CLI commands themselves don't surface `NoAuth` today. The MCP description annotation is the first consumer. CLI `--help` output could be enhanced later but is out of scope.
- **Integration coverage:** The end-to-end path (parse spec → generate MCP → read manifest → write registry) should be verified with one full pipeline run on a mixed-auth spec.
- **Unchanged invariants:** CLI binary generation, auth flows, doctor command, verify/dogfood, scorecard scoring formula — none of these change. The scorecard gets an informational line but no scoring changes.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Existing published CLIs may not compile after annotation edits | Run `go build ./...` in each CLI directory after editing. Revert on failure. |
| Some OpenAPI specs may have ambiguous security (e.g., `security: [{}]` meaning different things) | Follow the same interpretation as the scorecard's `parseSecurityRequirementSet()` which is already battle-tested. |
| Smithery.yaml schema may change | Pin to current schema. The file is opt-in metadata — if Smithery changes, regeneration fixes it. |
| Registry.json schema extension breaks existing consumers | All new fields are optional (`mcp` block). Existing consumers that don't read `mcp` are unaffected. |
| Steam/Pagliacci public endpoint identification may be wrong | Verify every candidate with actual HTTP request without credentials. Only annotate confirmed-public endpoints. Document results in PR. |
| MCP binary rename breaks existing users | No marketplace listings exist yet. The 6 published CLIs are source-only in the library repo — no users have `claude mcp add` configs pointing to the old names. Rename cost is zero. |
| Specs that omit security but actually require auth (false public sweep) | Post-parse sweep guarded by `!Auth.Inferred`. Only marks all endpoints public when parser actively concluded no auth exists, not when detection failed. |
| `oneline()` truncation drops auth annotation on long descriptions | `mcpDescription()` appends annotation then calls `oneline()`. Both `(public)` and `(requires API key)` are suffixes that could be cut on very long descriptions. In practice, most MCP tool descriptions are under 100 chars, leaving room for the suffix within the 120-char limit. |

## Contract with Follow-Up Plan: Printing Press Mega MCP

This plan is **Step 1 of 2.** It builds the data layer. Step 2 (separate plan) builds the Printing Press Mega MCP — a single MCP server that aggregates all published CLIs into one installable tool with catalog discovery and passthrough execution.

### What the Mega MCP needs from this plan

Every artifact this plan produces is designed to be consumed by the mega MCP:

| This plan produces | Mega MCP consumes it as |
|-------------------|------------------------|
| `registry.json` with `mcp` block per CLI | Backend for `library_search`, `library_info`, `library_install` catalog tools |
| `NoAuth` on endpoints | Decides which tools work without auth — registered as immediately usable vs gated |
| `AuthType` + `AuthEnvVars` in manifest | Per-API env var checking at call time — helpful error when env var not set |
| `MCPReady` levels | Determines which APIs are included in the mega MCP (full/partial yes, cli-only no) |
| `MCPToolCount` / `MCPPublicToolCount` | `library_info` response: "Steam Web — 164 tools, 80 work without auth" |
| `about` tool pattern (Unit 3b) | Individual MCP servers have `about`; mega MCP has `library_info` (same data, different scope) |
| `novel_features_built` in manifest | `library_info` response includes transcendence features as highlights |
| `smithery.yaml` per CLI | Mega MCP gets its own smithery.yaml; individual CLIs have theirs for standalone listing |
| Minority-side auth annotations | Same annotation logic applied to mega MCP tool descriptions |
| `{name}-pp-mcp` naming | Mega MCP: `printing-press-library-pp-mcp`. Individual: `{name}-pp-mcp`. No collision. |

### What the Mega MCP will deliver (Step 2)

- Single binary: `printing-press-library-pp-mcp`
- Catalog tools: `library_search` (find by name/category/keyword), `library_info` (details + transcendence features + install instructions), `library_install` (CLI and MCP install commands per platform)
- Passthrough tools: all individual API tools under namespaced names (`steam_web__app_details`, `dub__links_create`), using the same generic `makeAPIHandler` pattern the generator already produces
- Per-API auth: env vars checked at call time, not startup. Missing env var → helpful error with setup instructions. Public tools work immediately.
- Cookie-auth APIs: public tools work, auth-required tools return "install the standalone CLI and run `auth login --chrome`"
- User journey: one `brew install` + one `claude mcp add` → every public tool across all APIs works → set env vars to unlock specific APIs → graduate to standalone MCP if desired

### Design decisions this plan locks in for the mega MCP

These choices in this plan constrain the mega MCP's design. They should not be changed without considering the downstream impact:

1. **`registry.json` schema** — The `mcp` block is the mega MCP's source of truth for which APIs exist and what they need. Fields must be stable.
2. **`NoAuth` on Endpoint** — The mega MCP registers tools as "works without auth" or "needs env var" based on this flag. The flag must be set correctly by the parser.
3. **`MCPReady` levels** — The mega MCP only includes `full` and `partial` CLIs. `cli-only` CLIs are excluded (need browser, can't passthrough).
4. **Manifest `AuthEnvVars`** — The mega MCP checks these env vars at call time per-API. The field must be populated.
5. **Naming: `{name}-pp-mcp`** — Individual servers use this pattern. The mega MCP uses `printing-press-library-pp-mcp`. No collision.
6. **`about` tool response shape** — Individual MCP servers return this shape; the mega MCP's `library_info` returns a compatible superset.

### Separate follow-up: Library Reorganization

Rename published CLI directories from `{api}-pp-cli` to `{api-slug}` (e.g., `dub-pp-cli/` → `dub/`). The directory contains both CLI and MCP binaries, so naming it after the CLI is misleading. Impacts: go.mod module paths, publish collision detection, registry paths, `go install` paths, branch naming. Full problem description and migration strategy documented separately.

**Depends on:** Both this plan and the mega MCP plan completing first (avoid conflicting migrations).

## Sources & References

- **Inspiration:** [fli project](https://www.punitarani.com/projects/fli) — Google Flights CLI/MCP that grew via marketplace listings
- Scorecard security parsing: `internal/pipeline/scorecard.go:1113-1157`
- OpenAPI parser endpoint construction: `internal/openapi/parser.go:800-851`
- MCP tools template: `internal/generator/templates/mcp_tools.go.tmpl`
- CLI manifest: `internal/pipeline/climanifest.go`
- Publish pipeline: `internal/pipeline/publish.go`, `internal/cli/publish.go`
- README template: `internal/generator/templates/readme.md.tmpl`
- Publish skill: `skills/printing-press-publish/SKILL.md`
- Institutional learnings: scorecard accuracy, filepath traversal, non-mutating validation, layout contract
- kin-openapi Operation.Security: `*SecurityRequirements` (`[]SecurityRequirement`)
