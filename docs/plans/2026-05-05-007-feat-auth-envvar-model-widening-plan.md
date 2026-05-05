---
title: Auth env-var model widening + library remediation
type: feat
status: completed
date: 2026-05-05
---

# Auth env-var model widening + library remediation

## Summary

Widen the generator's auth env-var model from a flat `[]string` to a richer typed shape that carries kind classification (per-call user-supplied vs. auth-flow input vs. harvested byproduct), required-vs-optional flags, AND/OR group membership, and canonical-name-with-legacy-aliases. Route the richer model through every downstream surface — auth.go, doctor, MCP agent-context, helpers, SKILL.md body, OpenClaw frontmatter, manifests, verifier, scorer, host auth-doctor — so each one tells the truth about what users supply versus what the auth flow harvests. Build on PR #633's parser-side canonical extraction. Audit all 46 published CLIs, classify each into surgical / safe-regen / full-reprint remediation tiers, execute the surgical fixes inline, and queue the others as follow-up. Closes umbrella issue #632 and instances #630/#631.

---

## Problem Frame

The generator's auth model is `Auth.EnvVars []string` — a flat list of names with no semantics. That single shape is being asked to drive at least seven downstream surfaces (auth.go, doctor, MCP agent-context, helpers.go error messages, SKILL.md body prose, install docs, the proposed OpenClaw frontmatter), and it can't carry enough information for any of them to render correctly. Concrete failure modes already shipped to the public library:

- Spurious extra vars from multi-scheme specs (`TRIGGER_DEV_API_KEY` alongside `TRIGGER_SECRET_KEY`; `DUB_TOKEN` alongside `DUB_API_KEY`; `STEAM_API_KEY` alongside `STEAM_WEB_API_KEY`) — instance #630
- Wrong names from slug-plus-scheme-name derivation (`FLIGHTGOAT_API_KEY_AUTH` for what is actually FlightAware's AeroAPI) — instance #631
- Harvested-from-browser cookies advertised as user-supplied credentials (`ALLRECIPES_COOKIES`, Pagliacci) when in reality `auth login --chrome` populates them
- OAuth2 client_id+secret flagged as required-per-call when they're only used during `auth login`
- AND-groups (FedEx 4 keys; Kalshi key+private-key) and OR-groups (Slack bot/user; HubSpot access/private-app) collapsed into the same flat list with no relational structure
- The first-element-only assumption in human-prose templates (`auth.go.tmpl`, `skill.md.tmpl`, `helpers.go.tmpl`) silently drops every var beyond `[0]`

The OpenClaw frontmatter ask that surfaced this — declaring `requires.env` and `envVars` honestly — is blocked on the model: the frontmatter would either over-claim (mark harvested cookies as user-required, blocking the binary on a var the user never sets) or under-claim (omit referenced vars, triggering OpenClaw hub's metadata-mismatch warning). Both are wrong.

---

## Requirements

- R1. The auth model in `internal/spec/spec.go` carries kind and required-vs-optional for each env var. AND/OR group semantics are encoded by per-var `Required` flags and `Description` text rather than first-class group structure (AND = each var has `required: true`; OR = each var has `required: false` and the description names the alternative). Legacy-alias backwards compat for renamed vars is handled per-CLI in surgical patches (U7), not at the model level
- R2. The model is additive: existing `EnvVars []string` continues to work for templates that haven't migrated, and existing fixtures continue to pass goldens unchanged unless their behavior is intentionally updated
- R3. A spec-extension override path lets spec authors declare the rich shape directly when heuristics get it wrong, mirroring the existing `x-auth-*` extension precedent and the pointer-rot rule on `docs/SPEC-EXTENSIONS.md`
- R4. The OpenAPI parser populates the richer model from spec security schemes, falling back conservatively to the legacy single-string-list shape when classification cannot be made confidently
- R5. All consuming templates that read `.Auth.EnvVars` render correctly against the richer model — first-element-only sites updated to choose the canonical entry intentionally, iterate sites updated to honor kind/required filtering. U4 and U5 must enumerate the full set (the codebase has 12 such templates today: `auth.go`, `auth_simple.go`, `auth_browser.go`, `auth_client_credentials.go`, `agent_context.go`, `mcp_tools.go`, `config.go`, `client.go`, `helpers.go`, `doctor.go`, `readme.md`, `skill.md`)
- R6. Printed `SKILL.md` emits OpenClaw frontmatter `requires.env` (unconditionally-required vars only) and `envVars` (every referenced var with honest `required` flags and `description` text that conveys per-call vs. auth-flow vs. harvested)
- R7. Tier-level auth (`tier.Auth.EnvVars`) honors the same widening; the merge logic in `internal/pipeline/climanifest.go` carries the richer model through to `.printing-press.json`
- R8. The published `tools-manifest.json` (`ManifestAuth`) carries richer auth metadata in a backwards-compatible shape so the host `printing-press auth doctor` and any external readers continue to work against pre-widening manifests
- R9. The Steinberger scorer's Auth and AuthProtocol dimensions are revalidated against the new emission shape and updated where the regex set drifts
- R10. The host `printing-press auth doctor` consumes the richer model where it improves classification quality (kind-aware presence reporting); legacy single-list manifests continue to work
- R11. The PR description files a follow-up campaign issue ("Library auth env-var remediation") to (a) audit all 46 published CLIs and produce the surgical / safe-regen / full-reprint matrix, and (b) execute the surgical-tier fixes. The audit and surgical fixes are blocked on the machine PR's release tag — they are not in this plan's PR scope
- R12. Surgical-tier renames in U7 are clean breaks — no legacy-alias fallback. The published library is pre-official-launch; no committed user setups need preservation. Internal users with old env vars set will see ERROR-status doctor output naming the new canonical name, which is the appropriate signal to update. No model-level alias machinery; no per-CLI shim either
- R13. Issue #632 closes when this plan's machine PR merges. Issues #630 and #631 are instances that close when their corresponding surgical library PRs land in the follow-up campaign (post-machine-release). PR #633's disposition is "absorbed by extension" — already merged on 2026-05-05; this plan extends its helpers
- R14. Generator output stability holds: `scripts/golden.sh verify` passes, new fixtures cover each new model dimension (kind, AND-group, OR-group, canonical+alias), and any intentional golden updates are explained in the PR description
- R15. The Printing Press skill (`skills/printing-press/SKILL.md`) is updated in the same PR per the AGENTS.md "machine change touches skill" rule

---

## Scope Boundaries

- OpenClaw client-side or hub-side warning behavior — we are spec consumers, not the OpenClaw runtime
- Broader auth-manager refactors (the unified-auth-manager track from `2026-04-19-003-feat-unified-auth-manager-plan.md`)
- New auth mechanisms (OAuth2 PKCE variants, mTLS, SCIM provisioning) — this plan widens metadata for existing mechanisms, not adds new ones
- Re-running browser-sniff or crowd-sniff against any API to discover env vars that weren't already discovered
- Unrelated bugs in `auth.go` / `doctor.go` template emission surfaced incidentally during the audit
- The `config-override` env-var category (e.g., `KALSHI_BASE_URL`, `KALSHI_ENV`) — these are runtime config, not auth, and don't belong in this widening

### Deferred to Follow-Up Work

- **Library auth env-var audit + surgical-tier remediation campaign**: filed as a follow-up issue, blocked on this plan's machine release. The campaign's first deliverable is the audit matrix (originally U1), which inventories all 46 published CLIs by env-var kind and assigns each to surgical / safe-regen / full-reprint tier. The campaign's second deliverable executes the surgical-tier fixes (originally U7) — per-CLI rename + alias-fallback shim + `x-auth-vars` spec block to prevent regen drift. Issues #630 and #631 close when their corresponding surgical PRs land. Removed from this plan's PR scope so the machine PR doesn't block on cross-repo work.
- **Safe-regen tier library remediation**: bulk regen + republish PR for CLIs whose generator output changes but whose specs need no edits (analog to PR #213 from the cache-invalidation pattern). Acceptance criterion: `golden.sh verify` clean against pre-regen baseline plus expected diffs limited to the new model emission. Filed as a separate issue in the campaign above.
- **Full-reprint tier library remediation**: per-CLI reprints for CLIs that need spec edits (`x-auth-vars` annotations) or sniff re-runs to classify kind correctly. Each gets its own retro-driven reprint following the existing reprint contract.
- **OpenClaw hub validation integration**: if OpenClaw publishes a static-analysis schema or linter that runs against `SKILL.md` frontmatter, we'd integrate it as a CI check. Out of scope until OpenClaw publishes that surface.

---

## Context & Research

### Relevant Code and Patterns

- `internal/spec/spec.go:406-464` — `AuthConfig` model; existing additive growth pattern (`Optional`, `KeyURL`, `Title`, `Description`, `Inferred`, `RequiresBrowserSession`)
- `internal/openapi/parser.go:30-41,466-487,515-535` — extension constants, default name derivation, override application via `applyAuthOverrideExtensions`. PR #633 sits on this exact code path
- `internal/openapi/parser.go:490-503` — `isGenericAPIKeySchemeSuffix` (PR #633 expands the generic-suffix set)
- `internal/generator/templates/skill.md.tmpl:6-14,173-208` — current frontmatter emission and body prose (first-element-only)
- `internal/generator/templates/auth.go.tmpl:28-32`, `auth_browser.go.tmpl:103-873` — first-element-only human-prose surfaces
- `internal/generator/templates/agent_context.go.tmpl:42-120`, `mcp_tools.go.tmpl:421-440` — full-list iteration for machine-consumed surfaces
- `internal/generator/templates/auth_client_credentials.go.tmpl:28-267` — already handles positional pairs (`index 0` / `index 1`); precedent for AND-group emission
- `internal/generator/templates/config.go.tmpl:34-284` — printed CLI's config; first-element assumption is most load-bearing here (AuthSource, Bearer-prefix builder, legacy-header rewrite)
- `internal/generator/templates/doctor.go.tmpl:28,142,196,210-243,425` — mixed first-element + iterate; presence reporter
- `internal/generator/templates/helpers.go.tmpl:279-340` — first-element-only error hints (6 sites)
- `internal/generator/templates/readme.md.tmpl:72-455` — mixed; `(ne .Auth.Type "cookie") (ne .Auth.Type "composed")` guards already separate cookie path
- `internal/pipeline/climanifest.go:67-83,271,293-321` — `manifestAuthEnvVars` global+tier merge; `.printing-press.json` shape
- `internal/pipeline/toolsmanifest.go:41-66,195-197` — `ManifestAuth` published-manifest shape
- `internal/pipeline/runtime.go:153-200` — verifier env-var aggregation
- `internal/pipeline/mcpb_manifest.go:282-353` — MCPB `user_config` field emission (treats every entry as required user-config — must update with kind awareness)
- `internal/authdoctor/classify.go:72-86` — host `auth doctor` consumer of `ManifestAuth.EnvVars`
- `docs/SPEC-EXTENSIONS.md:12-32,228-240` — extension table and `x-auth-env-vars` reference; pointer-rot rule binds same-PR updates
- `testdata/golden/fixtures/golden-api.yaml`, `golden-api-oauth2-cc.yaml`, `tier-routing-api.yaml` — existing auth fixtures
- `skills/printing-press/SKILL.md:1480-1495` — env_vars authoring guidance (to extend)

### Institutional Learnings

- **HTTP cache invalidation precedent** (`docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md`): exact structural template for machine-change + library-backfill + per-CLI surgical patches. PR #521 (generator) + PR #213 (bulk backfill) + PR #237 (single-CLI patch). Frame this plan the same way: ship the model widening plus a backfill plan; otherwise pre-widening CLIs silently inherit the old shape forever.
- **Lookup priority by convention owner** (`docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md`): when canonical + legacy aliases coexist, label by convention (`// canonical envVar` / `// legacy alias`), never temporally (`// new` / `// old`). Add a both-keys-present regression test for any helper that walks an alias list.
- **Conservative classification gate** (`docs/solutions/design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md`): when the generator can't confidently classify, fall through to legacy single-string-list behavior. Cost of being too aggressive (mis-tagging a per-call var as harvested) is much higher than too conservative.
- **Validation must not mutate source dir** (`docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md`): for safe-regen tier execution, snapshot/restore `go.mod`/`go.sum`, build to a temp dir, no compiled binaries in published artifacts.
- **Scorecard architecture** (`docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md`): Auth and AuthProtocol dimensions read against generated files; pattern strings drift silently. Same-PR scorer revalidation required per AGENTS.md.
- **Sniff complementary discovery** (`docs/solutions/best-practices/sniff-and-crowd-sniff-complementary-discovery-2026-03-30.md`): existing repo vocabulary uses `cookie` / `bearer_token` / `api_key` / `oauth` for auth types. The harvested-from-browser kind maps onto the existing `Auth.Type = "cookie"` lineage from `2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md`. Don't invent a parallel taxonomy.

### External References

- OpenClaw skill format spec (`https://github.com/openclaw/clawhub/blob/main/docs/skill-format.md`): `requires.env` lists unconditionally-required env vars; `envVars` carries detailed shape with `name`, `required`, `description`. Hub-side security analysis flags references-vs-declaration mismatches at publish time. Spec is **silent** on:
  - Whether OpenClaw clients warn at runtime (only the hub validation is documented)
  - How to declare harvested vs. user-supplied (no separate field; description-only)
  - OR-group syntax (no native expression; description-only is the workaround)

  Resolution adopted by this plan: declare every referenced var in `envVars` with `required: true|false` reflecting whether the binary blocks without it, and use `description` to convey kind ("populated by `auth login --chrome`", "only needed during initial auth setup", "set this OR `OTHER_VAR`"). Use `requires.env` only for vars where the binary cannot start without them.

---

## Key Technical Decisions

- **Additive widening, not replacement**: keep `AuthConfig.EnvVars []string` as legacy field; add `EnvVarSpecs []AuthEnvVar` (or similar named field) alongside. Migration is per-template, not big-bang. Rationale: 12 templates and several non-template consumers cannot all migrate simultaneously without a goldens explosion. The additive pattern matches existing additive growth on `AuthConfig` (`Optional`, `KeyURL`, `Title`, `Description`, `Inferred`).

- **Build on PR #633's already-merged parser changes**: PR #633 merged on 2026-05-05. Its parser-side canonical extraction (`applyAuthEnvVars`, `remapAuthFormatForEnvOverride`, generic-suffix expansion at `internal/openapi/parser.go:490-503,538+`) is now in `main` and is the base this plan extends. U3 wraps the rich-model override logic around those helpers rather than reimplementing them. Before U3 starts, read the merged diff to confirm `stringListExtension`'s polymorphism shape can absorb a list-of-objects variant; if it can't, the U3 implementer extends the helper rather than working around it.

- **Why model widening, not just frontmatter emission tweaks**: a smaller fix — extend `skill.md.tmpl` to derive `requires.env` and `envVars` from existing signals (`Auth.Type == "cookie"` for harvested, OAuth2 grant detection for auth_flow_input, PR #633's canonical name) — was considered. It fails because every downstream surface beyond OpenClaw frontmatter needs the same kind/required information that the smaller fix would compute only inside the skill template. `helpers.go` 401 hints currently mention every env var the spec declares; without kind classification on the data side, the hint can't filter to per_call and will continue to advertise harvested cookies as user-required. `doctor` presence checks today produce ERROR for any unset env var; without kind in the model, doctor can't differentiate "user must set per-call API key" from "auth-login-flow input not yet captured." MCP `agent_context` emits the raw env var list to agents; without kind, agents can't tell which vars they should ask the user about. Each of these surfaces would need its own classification logic redoing the same heuristic work — and they would silently drift apart. The wider model is the right intervention because the classification is needed by 6+ surfaces, not 1.

- **Kind enum: 3 values, conservative default**: `per_call` (default; user-supplied per command), `auth_flow_input` (used during `auth login`, not after), `harvested` (populated by auth flow into config). Excludes `config_override` (`*_BASE_URL`, etc.) — those are runtime config, not auth metadata, and routing them through this model would over-broaden scope. Rationale: each of the three kinds maps cleanly to a distinct downstream-surface behavior (frontmatter `required` flag, doctor presence-check semantics, error hint phrasing). A fourth kind for config-override would conflate concerns.

- **No first-class AND/OR groups; encode via `Required` flag and description text**: AND is semantically `each var has required: true` (no separate "group" needed since all-required-independently is equivalent to all-required-together for downstream gates). OR is rare (3-4 CLIs in the audit: Slack bot/user, HubSpot access/private-app, Kalshi private-key path/inline) and gets `required: false` on each member with description text naming the alternative ("Set this OR `OTHER_VAR`"). Doctor template does **not** enforce OR satisfaction at runtime — the API's 401 response is the source of truth, and the helpers.go 401 hint surfaces the OR relationship from description text. Rationale: 42 of 46 CLIs have no group semantics, so a Groups map taxes every spec for the benefit of 4. Description-text encoding satisfies OpenClaw frontmatter (which is silent on groups anyway), keeps the doctor template simple, and trades minor doctor-UX precision for substantial model-simplicity gain.

- **Canonical name only; no legacy aliases at any level**: each `AuthEnvVar` carries `Name string` (canonical) — no `LegacyAliases` field. The published library is pre-official-launch; surgical-tier renames in U7 are clean breaks rather than shimmed transitions. Internal users with old env vars set will see doctor ERROR naming the new canonical, which is the appropriate signal. Rationale: backwards-compat machinery only earns its keep when there are committed users to preserve; the maintainer is the only known consumer today. Promote alias support if the library officially ships and accumulates users.

- **Spec-extension shape: introduce a new `x-auth-vars` key for the rich shape; preserve `x-auth-env-vars` as PR #633's legacy string/list-of-strings**: each key carries an unambiguous shape — `x-auth-env-vars` continues to accept string-or-list-of-strings (the form #633 shipped); `x-auth-vars` accepts a list-of-objects (each with `name`, `kind`, `required`, `group`, `aliases`, `description`). Avoids the three-arm type switch a polymorphic key would require, keeps reversibility high before any spec author adopts the new shape, and lets `docs/SPEC-EXTENSIONS.md` document each key with one set of `Rules:`. Update `docs/SPEC-EXTENSIONS.md` in the same PR per pointer-rot rule.

- **Conservative parser default**: when the parser cannot confidently classify (no x-extension, no Speakeasy example, ambiguous scheme name), produce `EnvVarSpecs` entries with `kind: per_call`, `required: true`, no group, no aliases — matching legacy behavior exactly. Rationale: classification errors silently shipped to 46 CLIs are much worse than missing classifications that show up identically to today.

- **OpenClaw frontmatter emission**: `requires.env` lists only `kind=per_call AND required=true AND group=none` vars. Everything else (auth_flow_input, harvested, OR-group members, optional per_call vars) lives in `envVars` with `required: true|false` reflecting binary-blocking semantics, and `description` text conveying the relationship. Rationale: OpenClaw hub will warn on referenced-but-undeclared vars; this satisfies the hub by declaring everything, while honoring the user's actual intent by only marking truly-required vars as `requires.env`.

- **`tools-manifest.json` carries richer model**: `ManifestAuth` grows a new `EnvVarSpecs` field alongside the existing `EnvVars` slice. Old manifests (without the new field) continue to work — host `auth doctor` reads from the legacy field as fallback. Rationale: backwards compatibility for the host doctor consuming pre-widening published manifests.

- **Surgical-tier remediation lives in this plan; safe-regen and full-reprint deferred**: U1's matrix output assigns each of 46 CLIs to a tier; U7 executes only surgical-tier fixes (rename in-place with legacy alias). Safe-regen (bulk regen + republish) and full-reprint (per-CLI spec edits + reprints) are filed as follow-up issues with clear acceptance criteria. Rationale: keeping all three tiers in one plan grows scope unboundedly and risks the model PR getting blocked on every-CLI churn.

- **PR #633 disposition: already merged, absorb-and-extend**: PR #633 merged on 2026-05-05; its parser changes are the base this plan builds on. U8's disposition step becomes a confirmation ("rebased on post-#633 main; helpers extended, not duplicated") rather than a coordination decision.

---

## Open Questions

### Resolved During Planning

- **PR #633 disposition**: already merged on 2026-05-05; this plan extends its parser helpers rather than coordinating around its review. U8's disposition step is a confirmation, not a decision.
- **Model representation (additive vs. replacement)**: additive. Decided in Key Technical Decisions.
- **Kind enum scope**: 3 values (per_call, auth_flow_input, harvested), excluding config_override. Decided in Key Technical Decisions.
- **AND/OR representation**: first-class fields, not description-only. Decided in Key Technical Decisions.
- **OpenClaw warning concern**: the user's specific concern that exclusion would trigger warnings. Resolution: declare every referenced var in `envVars` with honest `required` flags. The hub-side static analysis is satisfied by declaration; the user's intent is honored by accurate `required` flags. The OpenClaw spec is silent on runtime warning behavior, so this is the safest declaration shape.

### Deferred to Implementation

- **Per-CLI tier assignment for the 46 CLIs**: the campaign issue's audit produces this matrix; the exact tier per CLI cannot be decided from outside the audit. Some CLIs may move tiers as the audit reveals constraints.
- **MCPB `user_config` shape under kind awareness**: `internal/pipeline/mcpb_manifest.go:282-353` currently treats every entry as a required user-config field. Should `auth_flow_input` and `harvested` kinds be excluded from `user_config` entirely, or marked with a UI hint? Defer to U6a implementation; the right answer depends on what MCPB clients (Claude Desktop, etc.) do with optional / hidden entries.
- **Whether the `Inferred bool` field on `AuthConfig` should be promoted to per-`AuthEnvVar`**: classification (kind, required) may be partly inferred. The existing `Inferred` flag is a coarse boolean on the whole auth block. Decide during U2 whether to keep it coarse or fan it out.

### From 2026-05-05 review

Items the document review surfaced and the user routed here rather than into the plan body:

- **Sunset path for the legacy `EnvVars []string` field**: the additive widening keeps both representations indefinitely. The plan does not name when (or whether) `EnvVars` gets removed. Internal tooling can absorb dual-shape long-term, but a sunset criterion ("when all consumers migrate" / "next major version" / "never") would help future maintainers reason about the field. Adversarial finding 9 / product-lens finding 5.
- **OpenClaw spec evolution monitoring**: the plan adopts "declare every referenced var" as the safe stance under spec ambiguity. If OpenClaw later defines runtime warning semantics that punish over-declaration (e.g., "envVars with required:false that are never read"), the model carries enough information to re-derive the correct frontmatter, but no monitoring trigger is named. Adversarial finding 7.
- **Kind enum vocabulary for spec authors**: the 3-value enum (per_call / auth_flow_input / harvested) is generator-internal language. Spec authors writing `x-auth-vars` overrides have to learn it. Validation against real spec authors before committing the model would surface adoption friction. Product-lens finding 3.
- **Opportunity cost vs unified-auth-manager track**: `2026-04-19-003-feat-unified-auth-manager-plan.md` is in scope-boundaries as deferred. Both plans touch overlapping code paths. Confirm with the unified-auth-manager owner that this widening is on-path, not a parallel surface that will need re-migration. Product-lens finding 7.
- **Coarse vs per-EnvVar `Inferred` flag**: already deferred above; flagging here too because adversarial review noted that fanning out mid-U2 expands scope at the most sensitive point in the critical path. Decide explicitly in U2 KTD: "keep coarse; do not fan out."

---

## High-Level Technical Design

> *This illustrates the intended shape of the model and how the surfaces consume it. Directional guidance for review, not implementation specification — the implementing agent should treat it as context, not code to reproduce.*

### Data model shape (directional)

```
AuthConfig (existing, additive growth)
├── EnvVars []string           // legacy — preserved, populated from EnvVarSpecs[*].Name during transition
├── EnvVarSpecs []AuthEnvVar   // new — richer model, source of truth
└── ...existing fields (Optional, KeyURL, Title, Description, Inferred, ...)

AuthEnvVar (new)
├── Name string                // canonical name as emitted to printed CLI
├── Kind AuthEnvVarKind        // per_call | auth_flow_input | harvested
├── Required bool              // does the binary block without this var?
├── Sensitive bool             // is this a credential (true: API key, OAuth secret, token) or
│                              // public configuration (false: OAuth client_id, account slug)?
│                              // orthogonal to Kind; drives redaction policy in logs / agent-context
├── Description string         // user-facing prose for SKILL.md envVars and OpenClaw description
│                              // for OR-cases, the description names the alternative ("Set this OR OTHER_VAR")
└── Inferred bool              // optional: was this auto-derived rather than spec-declared?
```

Note: no `Groups map`, no `GroupID`, no `LegacyAliases`. AND is encoded by per-var `Required: true`; OR is encoded by per-var `Required: false` plus description text. Doctor enforces presence of `Required: true` vars only; OR satisfaction is left to the API's 401 response and the helpers.go hint. Legacy-alias backwards compat for the 3 renames is per-CLI surgical work in U7's `config.go` patches.

### Surface routing matrix (directional)

| Surface | Reads | Behavior under richer model |
|---|---|---|
| `requires.env` (OpenClaw frontmatter) | EnvVarSpecs filtered to `kind=per_call AND required=true` | Only unconditionally-required user-supplied vars; OR-group members (all `required=false`) are excluded |
| `envVars` (OpenClaw frontmatter) | All EnvVarSpecs | `required` reflects binary-blocking; `description` conveys kind and group relationships. For `Sensitive=true && Kind=auth_flow_input` entries (OAuth client_secret, etc.): description is generic ("Set during application setup") rather than naming the auth flow specifics, to avoid publishing secret-container narrative in a public hub artifact |
| `auth.go` short-help | `CanonicalEnvVar().Name` | Single-name display |
| `helpers.go` 401/403 hint | EnvVarSpecs filtered to `kind=per_call` | Hint mentions the per-call vars; harvested/auth-flow names omitted |
| `doctor` presence | All EnvVarSpecs | Per-kind presence reporting; only `required=true` vars produce ERROR on missing; OR-case (all `required=false` with descriptive text) reports as INFO with the description hint |
| MCP `agent_context` | All EnvVarSpecs, projected | Emit `name`, `kind`, `required`, `sensitive` for every entry. Emit `description` only for `Sensitive=false` entries (public configuration). Sensitive entries get a generic placeholder description to give agents enough to ask the user without leaking auth-flow specifics. Internal flags (`Inferred`, etc.) are not emitted |
| `config.go` AuthSource | `CanonicalEnvVar()` only | Canonical lookup. Per-CLI legacy-alias shims (where renames apply) are added in U7's surgical patches, not in the template. |
| `tools-manifest.json` | ManifestAuth.EnvVarSpecs (new) + EnvVars (legacy fallback) | Old manifests continue to work via legacy path |

### Conservative parser fallback (directional)

```
parse spec security scheme
  ↓
canonical-name resolution (PR #633 path: x-speakeasy-example, x-auth-env-vars override, default derivation)
  ↓
classify kind:
  - if spec says cookie/session-handshake → harvested
  - if scheme is OAuth2 client_credentials with client_id+client_secret → auth_flow_input for both
  - if x-auth-env-vars supplies kind explicitly → use it
  - otherwise → per_call (legacy default)
  ↓
classify required:
  - if Optional flag set on AuthConfig → required=false for derived vars
  - if x-auth-env-vars supplies required explicitly → use it
  - otherwise → required=true (legacy default)
  ↓
no group by default; only set when x-auth-env-vars or a known scheme pattern (e.g., FedEx-style scoped pairs) declares it
```

When in doubt, the parser produces a single-element `EnvVarSpecs` entry that's behaviorally identical to today's `EnvVars[0]`.

---

## Implementation Units

> *U1 (audit) and U7 (surgical library fixes) have been moved to a follow-up campaign filed as a separate issue, blocked on this plan's machine release. They retain their original U-IDs (U1, U7) so cross-references in earlier discussions resolve correctly, but they are not part of this PR's deliverables. See "Deferred to Follow-Up Work" in Scope Boundaries.*

- U2. **Widen `spec.AuthConfig` with `AuthEnvVar` struct and kind enum**

**Goal:** Introduce the typed model in `internal/spec/spec.go` alongside the existing `EnvVars []string`. Define `AuthEnvVarKind` as a typed string with the three constants (per_call, auth_flow_input, harvested), `GroupMode` as a typed string with `and`/`or`. Wire the new fields through `AuthConfig` validation.

**Requirements:** R1, R2, R12

**Dependencies:** U1 may refine the field shape based on audit findings (e.g., audit may reveal a kind we haven't named) — but U2 can start in parallel; defer-final-shape until U1 lands.

**Files:**
- Modify: `internal/spec/spec.go`
- Modify: `internal/spec/spec_test.go`
- Test: `internal/spec/spec_test.go`

**Approach:**
- Add `AuthEnvVar` struct with `Name`, `Kind`, `Required`, `Sensitive`, `Description`, `Inferred` fields. `Sensitive` is orthogonal to `Kind`: distinguishes confidential credentials (API keys, OAuth client_secret, bearer tokens, harvested cookies) from public configuration (OAuth client_id, account slugs, host overrides). Defaults to `true` for `kind=per_call` and `kind=harvested`; defaults to `false` for `kind=auth_flow_input` only when the var is unambiguously public (e.g., the spec's OAuth grant explicitly exposes client_id as the "client identifier" — fall back to `true` when uncertain). Drives redaction policy in any future logger or agent-context filter that needs to distinguish credentials from configuration
- Add `AuthEnvVarKind` typed string with three constants; existing `OAuth2GrantAuthorizationCode` const at line 468 is the precedent for typed-const-at-introduction
- Add validation: each `EnvVarSpecs[i].Name` must be unique within the auth config
- Define migration shim: if `EnvVarSpecs` is empty and `EnvVars` is non-empty, derive `EnvVarSpecs` lazily during read (kind=per_call, required=true, no group, no aliases). If both are populated, `EnvVarSpecs` wins; emit a one-time warning if they disagree on names
- Define a `CanonicalEnvVar() *AuthEnvVar` method on `AuthConfig` (and tier-level Auth) with deterministic selection: first entry where `Kind == per_call && Required == true`; if no such entry, fall back to `EnvVarSpecs[0]`; if `EnvVarSpecs` is empty, fall back to lazy-derived from `EnvVars[0]`. Every consumer that today uses `index .Auth.EnvVars 0` or `EnvVarSpecs[0]` must call `CanonicalEnvVar()` instead. Rationale: ordering of `EnvVarSpecs` can shift between regens (spec source order vs. x-auth-vars-declared order), so `[0]` is non-deterministic for human-prose surfaces
- Tier-level `Auth` honors the same widening — `internal/spec/spec.go:1369` (tier-credentials check) continues to require non-empty `EnvVars`, with `EnvVarSpecs` as alternative population source
- **Pre-merge normalization**: lazy derivation runs at the per-tier and per-global `AuthConfig` level **before** the `climanifest.go:293-321` merge, not during merge. This guarantees every merge input is already in rich shape, eliminating the failure mode where one side has legacy `EnvVars` only and the other has `EnvVarSpecs` only, producing duplicated entries on merge instead of override semantics

**Patterns to follow:**
- Additive growth pattern already in `AuthConfig` (`Optional`, `KeyURL`, `Title`, `Description`, `Inferred`)
- Typed-const introduction matching `OAuth2GrantAuthorizationCode` precedent (`internal/spec/spec.go:468`)
- Validation pattern from existing `AuthConfig.Validate()` calls

**Test scenarios:**
- Happy path: spec with `EnvVarSpecs` populated parses, validates, exposes both legacy `EnvVars` (back-derived) and new `EnvVarSpecs`
- Happy path: legacy spec with only `EnvVars []string` parses, validates, exposes lazy `EnvVarSpecs` derivation with kind=per_call defaults
- Edge case: spec with both `EnvVars` and `EnvVarSpecs` populated and consistent — both fields readable, `EnvVarSpecs` wins for kind/required/group
- Edge case: spec with both populated and inconsistent (different names) — validation produces a warning; `EnvVarSpecs` wins
- Edge case: empty `EnvVarSpecs` + empty `EnvVars` — validation accepts (no-auth case, matches existing behavior)
- OR-case: spec with two `EnvVarSpecs` entries both `Required: false` and description text mentioning each other — validates fine; doctor reports both as INFO; helpers.go hint surfaces the OR relationship
- Edge case: tier-level `Auth` with `EnvVarSpecs` honors same validation rules
- Edge case: tier populates only legacy `EnvVars` while global populates only `EnvVarSpecs` — pre-merge normalization derives tier's `EnvVarSpecs` lazily before climanifest merge; merge result reflects override semantics, not duplication
- `CanonicalEnvVar()` deterministic selection: spec with `[{Kind: harvested}, {Kind: per_call, Required: true}]` returns the per_call entry, not `[0]`; spec with two per_call+required entries returns the first in source order; spec with only `EnvVars []string` returns lazy-derived entry from `EnvVars[0]`

**Verification:**
- `go test ./internal/spec/...` passes with new test cases
- `go vet ./...` clean
- `golangci-lint run ./...` clean
- Existing fixture-driven tests still pass (legacy `EnvVars` path remains valid)

---

- U3. **OpenAPI parser populates rich model with conservative classification + spec-extension override**

**Goal:** Update `internal/openapi/parser.go` to populate `EnvVarSpecs` and `Groups` from spec security schemes, with conservative defaults and a spec-extension override path. Build on PR #633's `applyAuthEnvVars` / `remapAuthFormatForEnvOverride` helpers.

**Requirements:** R3, R4

**Dependencies:** U2 (model must exist). PR #633 has already merged; its helpers are the base.

**Files:**
- Modify: `internal/openapi/parser.go`
- Modify: `internal/openapi/parser_test.go`
- Modify: `docs/SPEC-EXTENSIONS.md`
- Test: `internal/openapi/parser_test.go`

**Approach:**
- Use the new `x-auth-vars` key for the rich list-of-objects shape (decided in Key Technical Decisions); leave `x-auth-env-vars` exactly as PR #633 shipped it (string-or-list-of-strings)
- For each parsed security scheme: derive default `Name` (PR #633's path — `x-speakeasy-example` or scheme-name + scheme-type); set `Kind = per_call`, `Required = true`, no legacy aliases. This is the conservative fallback behavior
- If the scheme has an extension override (`x-auth-vars`) declaring kind/required/aliases, replace the conservative values with the declared ones
- Special-case `Auth.Type = "cookie"` schemes to default `Kind = harvested` (matches the existing cookie-runtime plan's lineage)
- Special-case OAuth2 client_credentials grants to default `Kind = auth_flow_input` for both client_id and client_secret (matches `auth_client_credentials.go.tmpl`'s existing positional-pair handling); set `Sensitive = false` for client_id (public OAuth identifier) and `Sensitive = true` for client_secret (confidential)
- Update `docs/SPEC-EXTENSIONS.md` Extensions table (lines 12-32) and the `x-auth-env-vars` spec block (lines 228-240) — pointer-rot rule binds same-PR update

**Patterns to follow:**
- PR #633's `applyAuthEnvVars` and `stringListExtension` (the new helpers it adds)
- Existing `applyAuthOverrideExtensions` at `parser.go:515-` as the place to extend
- `isGenericAPIKeySchemeSuffix` (PR #633 expansion) for scheme-name normalization
- Conservative-gate pattern from the dry-run learning — when in doubt, fall through to legacy single-string behavior

**Test scenarios:**
- Happy path: spec with `x-auth-env-vars: [TODOIST_API_KEY]` produces single per_call/required entry; back-compat with PR #633
- Happy path: spec with `x-auth-vars: [{name: TODOIST_API_KEY, kind: per_call, required: true}]` produces same shape via rich path
- Happy path: spec with cookie auth scheme produces `Kind = harvested` entry
- Happy path: spec with OAuth2 client_credentials grant produces two `Kind = auth_flow_input` entries (client_id, client_secret)
- Happy path: spec with multi-var auth where all are required (e.g., FedEx-shape) produces N `EnvVarSpecs` entries each with `Required: true`; no group structure needed
- Happy path: spec with OR-shape auth (e.g., Slack-shape) produces N entries each with `Required: false` and description text naming the alternative
- Edge case: spec with no security schemes — parser produces empty `EnvVarSpecs` and empty `EnvVars` (no-auth case)
- Edge case: spec with multiple security schemes pointing at the same logical credential (the #630 case) — without explicit override, parser produces multiple entries (preserving legacy behavior); with override, parser respects the override's consolidation
- Edge case: spec with `x-auth-env-vars` declaring an aliased entry (`name: FLIGHTAWARE_API_KEY, legacy_aliases: [FLIGHTGOAT_API_KEY_AUTH]`) produces the canonical+aliases shape
- Conservative gate: spec with malformed `x-auth-env-vars` extension — parser logs a warning and falls back to legacy default-derivation, does not panic or skip
- Edge case: tier-level `x-auth-env-vars` honored independently per tier

**Verification:**
- `go test ./internal/openapi/...` passes
- `scripts/golden.sh verify` clean against existing fixtures (no behavior change for legacy fixtures)
- `docs/SPEC-EXTENSIONS.md` Extensions table includes the new shape and a `Rules:` block documents the polymorphic accept

---

- U4. **Migrate machine-consumed templates to richer model**

**Goal:** Update `agent_context.go.tmpl`, `mcp_tools.go.tmpl`, and `config.go.tmpl` to consume `EnvVarSpecs` (preferred) with fallback to `EnvVars` (legacy). Templates emit kind, required, group membership, and aliases into the structured surfaces (MCP env_vars JSON, agent-context JSON, printed config struct).

**Requirements:** R5, R7

**Dependencies:** U2, U3

**Files:**
- Modify: `internal/generator/templates/agent_context.go.tmpl`
- Modify: `internal/generator/templates/mcp_tools.go.tmpl`
- Modify: `internal/generator/templates/config.go.tmpl`
- Test: `testdata/golden/expected/...` (regen + verify)
- Test: new fixture `testdata/golden/fixtures/golden-api-rich-auth.yaml` exercising kind, group, alias

**Approach:**
- `agent_context.go.tmpl`: replace `range .Auth.EnvVars` with `range .Auth.EnvVarSpecs` when populated; emit `{name, kind, required, group_id, aliases}` per entry
- `mcp_tools.go.tmpl`: same pattern for the `env_vars` array at line 421-440 (and tier variant at 439-440); preserve the `index 0` first-element error-hint sites by switching to "find canonical entry" logic that picks the first per_call required entry rather than blindly `[0]`
- `config.go.tmpl`: this is the most load-bearing first-element site (AuthSource, Bearer-prefix builder, legacy-header rewrite at lines 129, 170-208, 228-243); switch to `CanonicalEnvVar()`-based lookup. The template emits no alias fallback — that's per-CLI surgical work in U7 for the 3 renames, hand-edited into specific config.go files
- Add new golden fixture exercising all model dimensions; existing fixtures remain unchanged unless behavior intentionally changes

**Patterns to follow:**
- `auth_client_credentials.go.tmpl` already handles positional pairs cleanly; use as template for multi-element handling
- Lookup-priority-by-convention pattern from the canonical/legacy learning

**Test scenarios:**
- Happy path: agent-context JSON includes `kind` and `required` fields per env var when `EnvVarSpecs` populated
- Happy path: MCP `env_vars` array honors per_call filter for error hints (auth_flow_input / harvested vars don't appear in the "set X to authenticate" hint)
- Happy path: printed config.go reads canonical name first, falls back to legacy aliases
- Edge case: spec with only legacy `EnvVars []string` — templates fall back to lazy-derived `EnvVarSpecs`, behave identically to today
- Edge case: AND-group in spec — agent-context emits both members with same group_id; doctor logic (U5) treats them together
- Edge case: OR-group — same shape; consumer behavior diverges in U5/U6
- Integration: regen one canary CLI through the updated templates; inspect emitted Go source for correctness; build and run `doctor` against the canary

**Verification:**
- `scripts/golden.sh verify` clean against existing fixtures (no diff)
- New rich-auth fixture's expected output captured and committed
- Manual canary regen produces a building, running CLI

---

- U5. **Migrate human-prose templates and add OpenClaw frontmatter emission**

**Goal:** Update `auth.go.tmpl`, `auth_browser.go.tmpl`, `helpers.go.tmpl`, `skill.md.tmpl`, `doctor.go.tmpl`, and `readme.md.tmpl` to consume the richer model. Add OpenClaw `requires.env` and `envVars` blocks to `skill.md.tmpl` frontmatter with kind-aware filtering and description content.

**Requirements:** R5, R6

**Dependencies:** U2, U3, U4

**Files:**
- Modify: `internal/generator/templates/skill.md.tmpl`
- Modify: `internal/generator/templates/auth.go.tmpl`
- Modify: `internal/generator/templates/auth_browser.go.tmpl`
- Modify: `internal/generator/templates/helpers.go.tmpl`
- Modify: `internal/generator/templates/doctor.go.tmpl`
- Modify: `internal/generator/templates/readme.md.tmpl`
- Test: `testdata/golden/expected/...` (regen + verify)

**Approach:**
- `skill.md.tmpl`: add new `requires.env:` block to OpenClaw frontmatter listing only per_call+required+ungrouped vars; add `envVars:` block listing every entry in `EnvVarSpecs` with `name`, `required`, `description`. Description text is constructed per kind:
  - `per_call` + required: `"<existing description>"`
  - `auth_flow_input`: `"Only needed during \`auth login\`; not required for normal use. <existing description>"`
  - `harvested`: `"Populated automatically by \`auth login\`. <existing description>"`
  - OR-group member: `"Set this OR <other-member-name>. <existing description>"`
  - AND-group member: `"Required together with <other-member-names>. <existing description>"`
- Body prose ("export FOO=...") at lines 173-208 switches from `index .Auth.EnvVars 0` to "the canonical per_call required entry" lookup
- `auth.go.tmpl`, `auth_browser.go.tmpl`, `helpers.go.tmpl`: same canonical-entry lookup pattern for short-help and error hints
- `doctor.go.tmpl`: presence-check logic becomes kind-aware — required per_call vars produce "missing" status; OR-group satisfied if any member set; AND-group requires all members; auth_flow_input vars get "informational" status only
- `readme.md.tmpl`: env-var table at line 441 includes kind column; existing cookie/composed type guards continue to apply
- Skip the OpenClaw frontmatter emission when `auth.type == "none"` (preserve existing behavior from `2026-04-26-002`)

**Patterns to follow:**
- Existing `(ne .Auth.Type "cookie") (ne .Auth.Type "composed")` guard pattern in readme.md.tmpl for type-aware branching
- Kind-aware predicate pattern from the dry-run learning (`commandSupportsDryRun` style — sibling predicates that mirror existing API shape)

**Test scenarios:**
- Happy path: SKILL.md emits `requires.env` containing only the canonical per_call var; `envVars` includes all referenced vars with honest `required` flags
- Happy path: harvested-cookie CLI (e.g., allrecipes after U7 surgical fix) emits `envVars: [{name: ALLRECIPES_COOKIES, required: false, description: "Populated automatically by auth login..."}]` and no `requires.env` entry for that var
- Happy path: OAuth2 client_credentials CLI (e.g., google-photos) emits both client_id and client_secret as `required: false` with auth-flow-input description
- Happy path: OR-group CLI (e.g., slack) emits both bot_token and user_token as `required: false` with description text indicating the OR relationship
- Happy path: AND-group CLI (e.g., fedex) emits all 4 members as `required: true` with description text indicating the pair structure
- Edge case: no-auth CLI (e.g., open-meteo) — skill frontmatter has no `requires.env` and no `envVars` blocks
- Edge case: tier-routed CLI — frontmatter reflects merged global+tier vars
- Edge case: legacy spec (no `EnvVarSpecs`) — frontmatter generated via lazy derivation, all vars marked `required: true` per_call (matches today's implicit assumption)
- Doctor: canary CLI with mixed kinds — `doctor` reports per_call missing as ERROR, auth_flow_input missing as INFO, harvested with cookies-on-disk as OK
- Auth.go short-help: only the canonical per_call var appears; aliases mentioned in long-help only
- Helpers.go 401 hint: hint mentions per_call var, omits auth_flow_input and harvested

**Verification:**
- `scripts/golden.sh verify` clean
- Manual canary regen for each kind (per_call, auth_flow_input, harvested, OR-group, AND-group); inspect emitted SKILL.md frontmatter against the OpenClaw spec shape
- Build and run `--help` on each canary; output reflects expected canonical-entry behavior

---

- U6a. **Update manifest, verifier, MCPB, and host auth-doctor**

**Goal:** Carry the richer model through `tools-manifest.json` (`ManifestAuth`), `internal/pipeline/climanifest.go` merge logic, `internal/pipeline/runtime.go` verifier, `internal/pipeline/mcpb_manifest.go` MCPB user_config, and the host `internal/authdoctor/classify.go`. These all share the `EnvVarSpecs` wire format and move together.

**Requirements:** R7, R8, R10

**Dependencies:** U2, U3, U4, U5

**Files:**
- Modify: `internal/pipeline/toolsmanifest.go`
- Modify: `internal/pipeline/climanifest.go`
- Modify: `internal/pipeline/runtime.go`
- Modify: `internal/pipeline/mcpb_manifest.go`
- Modify: `internal/pipeline/publish.go` (manifest preservation at line 219)
- Modify: `internal/authdoctor/classify.go`
- Test: each modified package's `_test.go`

**Approach:**
- `ManifestAuth`: add `EnvVarSpecs` field alongside existing `EnvVars`; both populated on emit; readers prefer `EnvVarSpecs` and fall back to `EnvVars` (treating each as kind=per_call required when only `EnvVars` present)
- `manifestAuthEnvVars` merge at `climanifest.go:293-321`: extend to merge `EnvVarSpecs` global+tier with the same precedence rules
- Verifier at `runtime.go:153-200`: discover env vars from emitted `config.go` continues to work; richer model adds kind-aware presence check for verify summary
- MCPB `user_config` at `mcpb_manifest.go:282-353`: skip `auth_flow_input` and `harvested` entries (not user-config); per_call entries become `user_config` fields with `required` reflecting the model
- Host `auth doctor` at `internal/authdoctor/classify.go:72-86`: prefer `EnvVarSpecs` for kind-aware presence reporting; fall back to legacy `EnvVars` slice for pre-widening manifests. **Critical dedup guard**: classify.go must use early-return — if `auth.EnvVarSpecs` is non-empty, iterate that and return; do NOT then also iterate `auth.EnvVars`. Otherwise mixed-version manifests (both fields populated, EnvVars back-derived from EnvVarSpecs for pre-widening reader compat) cause every credential to be reported twice as if it were two independent vars

**Patterns to follow:**
- Backwards-compat pattern: prefer rich field, fall back to legacy field (matches `2026-04-19-004-feat-auth-doctor-plan.md` KTD-2 "env-var-wins; store is additive")
- Scorer dimension update from the scorecard architecture learning — same-PR validation
- Lookup-priority-by-convention for legacy/canonical reads

**Test scenarios:**
- Happy path: published `tools-manifest.json` includes `EnvVarSpecs` field; legacy `EnvVars` field also populated for pre-widening readers
- Happy path: host `auth doctor` reads richer model from new manifest; reports kind-aware status
- Happy path: host `auth doctor` against pre-widening manifest (no `EnvVarSpecs` field) reads legacy field; behavior identical to today
- Happy path: MCPB bundle's `user_config` excludes harvested and auth-flow-input entries; per_call entries appear with correct required flags
- Edge case: manifest with empty `EnvVarSpecs` and non-empty `EnvVars` — host doctor lazily derives EnvVarSpecs at read time
- Edge case: manifest with both populated and inconsistent — `EnvVarSpecs` wins; warning logged
- Edge case (mixed-version dedup): manifest with both fields populated and consistent (EnvVars back-derived from EnvVarSpecs for pre-widening reader compat) — host doctor reports each credential exactly once via the `EnvVarSpecs` path, not twice. Add explicit test fixture for this case
- Edge case: tier-routed CLI — merge produces correct global+tier rich shape
- Verifier: new fixture exercising all kinds — verify pass rate reflects required-only blocking
**Verification:**
- `go test ./internal/pipeline/...` passes
- `go test ./internal/authdoctor/...` passes
- Manual: regen one canary CLI, publish to a temp library, run `printing-press auth doctor` against the canary
- `scripts/golden.sh verify` clean

---

- U6b. **Revalidate Steinberger scorer Auth and AuthProtocol dimensions against new emission**

**Goal:** Confirm the scorer's Auth and AuthProtocol pattern strings still match the post-widening emission shape; update any regex that drifted; add explicit unscored-dimension handling for cases where classification cannot be determined.

**Requirements:** R9

**Dependencies:** U6a (the new emission shape must be stable before scorer revalidates against it)

**Files:**
- Modify: `internal/pipeline/scorer/...` (Auth and AuthProtocol dimensions; exact path determined during implementation)
- Test: `internal/pipeline/scorer/..._test.go`

**Approach:**
- Identify the regex set used for Auth and AuthProtocol scoring
- Run the scorer against representative new-model emissions (the canary CLIs from U4/U5); diff scores against pre-widening baseline
- Update regex strings only where drift exists; do not "fix" scoring outcomes — the goal is parity, not score inflation
- Per the scorecard learning, mark dimensions as unscored (not midpoint) when evidence is missing
- Same PR as U6a per AGENTS.md "update dependent verifiers in the same change"

**Patterns to follow:**
- Scorecard architecture learning's "regex strings drift silently" diagnostic
- Unscored-dimension convention from the same learning

**Test scenarios:**
- Existing scoring fixture: produces stable Auth/AuthProtocol scores within tolerance after the widening
- Rich-auth fixture from U4/U5: produces a defensible score (not midpoint, not zero) — exact value captured in the fixture's expected output
- Adversarial scenario: a fixture intentionally emitting only the new shape (no legacy back-derivation) scores correctly against the regex set; if it doesn't, the regex needs updating
- Unscored case: a fixture with kind-unknown classification produces an `unscored_dimensions` entry rather than a midpoint score

**Verification:**
- `go test ./internal/pipeline/scorer/...` passes
- Score deltas against the existing baseline are explained in the PR description
- No "score inflation" from regex updates — diffs are parity adjustments, not gaming

---

> *U7 has been moved to the follow-up campaign filed as a separate issue (see "Deferred to Follow-Up Work"). Original responsibility — surgical-tier library remediation across `mvanhorn/printing-press-library` — runs after this plan's machine release. Renames are clean breaks (pre-official-launch, no legacy-alias shims); each surgical PR also commits the corresponding `x-auth-vars` spec block to prevent regen drift. Details documented in the campaign issue.*

- U8. **Documentation, goldens, skill update, and issue closeout**

**Goal:** Final polish — update `docs/SPEC-EXTENSIONS.md`, regenerate goldens with intentional explanations, update `skills/printing-press/SKILL.md`, file a new `docs/solutions/` learning entry, decide PR #633 disposition, and close issues #630, #631, #632.

**Requirements:** R3, R14, R15, R13

**Dependencies:** U2, U3, U4, U5, U6a, U6b complete

**Files:**
- Modify: `docs/SPEC-EXTENSIONS.md`
- Modify: `skills/printing-press/SKILL.md`
- Create: `docs/solutions/design-patterns/auth-envvar-rich-model-2026-MM-DD.md`
- Modify: `testdata/golden/expected/...` (intentional updates from U4/U5 work, captured here)
- Modify: this plan's `status: active` → `status: completed`

**Approach:**
- `docs/SPEC-EXTENSIONS.md`: extend the Extensions table with the rich shape; update the `x-auth-env-vars` block (lines 228-240) with the new polymorphic accept and full `Rules:` documentation
- `skills/printing-press/SKILL.md` env_vars guidance (lines 1480-1495): expand to cover kind classification, group declarations, and canonical+aliases pattern; show example spec snippets
- New solutions doc: capture the rich-model design, why it widened, the OpenClaw frontmatter resolution, and the surgical/safe-regen/full-reprint tiering. Filed under `design-patterns/` with frontmatter (`module`, `tags`, `problem_type`)
- Goldens: any intentional diff from U4/U5 captured here with explanation in the PR description (per AGENTS.md "explain it in your final response")
- PR #633 disposition: already merged on 2026-05-05. Confirm in PR description that the rich model extends the merged helpers rather than duplicating them
- Issue closeout: in the PR description, `Closes #632`. `#630` and `#631` close when their corresponding surgical library PRs land (the model PR fixes the generator, the surgical PRs fix the published instances)
- File the follow-up campaign issue: title "Library auth env-var remediation campaign", body links this plan + #630 + #631 + #632, lists the 4 known surgical CLIs (flightgoat rename, dub rename, steam-web rename, trigger-dev spurious-var drop) as clean-break renames (no legacy-alias shims, pre-official-launch state), notes that the audit (originally U1) is the campaign's first deliverable

**Patterns to follow:**
- Pointer-rot rule for `docs/SPEC-EXTENSIONS.md`
- Solutions doc shape (module, tags, problem_type frontmatter) from existing entries
- Issue-close convention from existing PRs

**Test scenarios:**
- `golangci-lint run ./...` clean
- `scripts/golden.sh verify` clean against committed expected files
- Skill loads in Claude Code without errors (existing user pattern)
- New solutions doc renders correctly and is discoverable by `ce-learnings-researcher`

**Verification:**
- All four `Closes #N` references in PR description are valid
- PR #633 disposition is documented in the PR body
- New solutions entry exists under `docs/solutions/design-patterns/`
- Plan's status field updated when this PR merges

---

## System-Wide Impact

- **Interaction graph**: 12 templates + 6 non-template consumers + 1 host command + 1 published-manifest schema + 1 spec-extension catalog. Every entry needs to honor the additive contract.
- **Error propagation**: parser-level malformed `x-auth-env-vars` extension produces a logged warning + fallback to legacy single-list behavior; never aborts generation. Spec validation rejects structurally invalid groups (1-member, alias collisions). Runtime config-load in printed CLIs treats unset canonical + set legacy alias as success with a one-time stderr deprecation note.
- **State lifecycle risks**: `tools-manifest.json` is preserved across re-publish at `internal/pipeline/publish.go:219`; the new `EnvVarSpecs` field must be preserved alongside `EnvVars` to avoid drift between published-manifest reads. Both fields populated on emit ensures pre-widening readers don't lose data.
- **API surface parity**: every surface that exposes env-var info (CLI `--help`, `doctor`, MCP `agent-context`, `SKILL.md` frontmatter, README, install docs) renders the same canonical-vs-legacy distinction; the canonical name is the public contract.
- **Integration coverage**: golden harness covers template emission deterministically; canary regen tests cover end-to-end (spec → emitted CLI → runtime behavior). Scorer dimension validation covers the regression risk that scoring patterns drift silently against the new emission.
- **Unchanged invariants**: the no-auth case (`Auth.Type = "none"`) skips emission entirely; the cookie path (`Auth.Type = "cookie"`) keeps its existing template guards; tier-level auth merge logic preserves global+tier precedence; the existing `Optional bool` flag on `AuthConfig` continues to work alongside per-var `Required` flags (auth-block-level optional means whole-block; per-var required means individual).

---

## Risks & Dependencies

| Risk | Mitigation |
|---|---|
| PR #633's `stringListExtension` polymorphism is incompatible with list-of-objects shape | U3 reads the merged #633 diff first; if the helper rejects list-of-objects, extend the helper to accept the third arm rather than working around it (helper extension is in-scope for U3) |
| Goldens drift silently from kind/group emission | Explicit new fixtures for each new model dimension; existing fixtures unchanged unless intentional; `scripts/golden.sh verify` runs in CI and locally; PR description explains every intentional diff |
| Surgical-tier library PRs land before machine PR merges, leaving orphan canonical names with no template support | Sequence: machine PR merges first; surgical PRs open only after a published release of the machine that emits the new shape; surgical PRs reference the published version in description |
| Scorer Auth/AuthProtocol regex set drifts; scores fluctuate after widening | Same-PR scorer revalidation per learning #5; explicit unscored-dimension handling for unknown classifications; tolerance-based scoring tests |
| Backwards compat breakage for users who set legacy env-var names | Legacy aliases first-class in the model; runtime config-load reads canonical-then-aliases; both-aliases-present test fixture; deprecation note (not error) when legacy is the only source |
| Audit (U1) reveals a kind we haven't named | Plan accepts that U2 may be revised based on U1 findings; the additive model design absorbs new kinds without breaking existing ones; deferred-to-implementation question already flags this |
| MCPB `user_config` shape under kind awareness uncertain | Defer to U6 implementation; the right answer depends on what MCPB clients do with hidden entries; conservative default is "skip" but UI hint may be better |
| OpenClaw spec ambiguity on warning behavior | Plan adopts "declare everything in `envVars`" as the safe default; if OpenClaw clients later define warning semantics differently, the model already carries enough information to re-derive the correct frontmatter shape without changing the spec |

---

## Success Metrics

The plan resolves in three milestones; each is user-visible at a different time. Naming them avoids "Closes #632" being misread as "the user-visible OpenClaw frontmatter problem is fully solved."

| Milestone | When | What's true after |
|---|---|---|
| **Machine PR merges** | This plan ships | #632 closes. Generator emits correct frontmatter for any newly-generated CLI. The 46 already-published CLIs still emit the old (incorrect) frontmatter; OpenClaw hub will continue to warn on those until follow-up campaigns land. |
| **Surgical-tier PRs land** | Follow-up campaign, post-machine-release | #630 and #631 close. The 4 known worst-offender CLIs (flightgoat, dub, steam-web rename + trigger-dev spurious var drop) emit correct frontmatter; user-visible auth-naming bugs in those CLIs are resolved. |
| **Safe-regen + full-reprint campaigns complete** | Subsequent follow-up | All 46 CLIs in `mvanhorn/printing-press-library` emit correct OpenClaw frontmatter. The hub's metadata-mismatch warning surface goes quiet across the full library. |

Reviewers and downstream readers should not interpret a single PR merge as full closure of the user-visible symptom. The campaign issue tracks the residual work explicitly.

---

## Documentation / Operational Notes

- **Same-PR doc updates** (mandatory): `docs/SPEC-EXTENSIONS.md`, `skills/printing-press/SKILL.md`, this plan's status field
- **Pre-merge release note**: machine release before any surgical-tier library PRs ship; tag in `version.go` per release-please flow
- **Post-merge solutions entry**: file `docs/solutions/design-patterns/auth-envvar-rich-model-...` so the next plan touching auth metadata has prior art
- **Rollout sequencing**: machine PR → release → surgical-tier library PRs → safe-regen follow-up → full-reprint follow-ups (each its own PR or batch)
- **No feature flag**: the additive shape means there's nothing to gate behind a flag; old templates keep working through the lazy-derivation path during the transition
- **Monitoring**: `printing-press auth doctor` results before-and-after for a sample of CLIs; verify no regression in classification quality

---

## Sources & References

- **Issue**: [#632 — auth env-var model is too thin](https://github.com/mvanhorn/cli-printing-press/issues/632) (umbrella)
- **Issue**: [#631 — flightgoat env-var name doesn't match underlying API](https://github.com/mvanhorn/cli-printing-press/issues/631) (instance)
- **Issue**: [#630 — trigger-dev spurious env-var](https://github.com/mvanhorn/cli-printing-press/issues/630) (instance)
- **PR**: [#633 — preserve canonical auth env hints](https://github.com/mvanhorn/cli-printing-press/pull/633) — **merged 2026-05-05**; this plan's U3 extends its helpers
- **Related plan (active)**: `docs/plans/2026-03-31-001-fix-auth-envvar-hint-relevance-plan.md` — narrow upstream fix to crowd-sniff `extractEnvVarHint`; compatible with this widening
- **Related plan (draft)**: `docs/plans/2026-04-02-001-feat-browser-auth-cookie-runtime-plan.md` — cookie auth flow; `Auth.Type = "cookie"` lineage maps onto `kind = harvested`
- **Related plan (completed)**: `docs/plans/2026-04-19-004-feat-auth-doctor-plan.md` — host `auth doctor`; KTD-2 "env-var-wins; store is additive" honored here
- **Related plan (active)**: `docs/plans/2026-04-26-002-feat-printing-press-p1-machine-fixes-plan.md` — preserves "auth.type=='none' skips emission" semantic
- **Solutions**: `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` — machine + library remediation precedent
- **Solutions**: `docs/solutions/best-practices/checkout-scoped-printing-press-output-layout-2026-03-28.md` — canonical/legacy lookup priority
- **Solutions**: `docs/solutions/design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md` — conservative classification gate
- **Solutions**: `docs/solutions/best-practices/validation-must-not-mutate-source-directory-2026-03-29.md` — safe-regen tier constraint
- **Solutions**: `docs/solutions/best-practices/steinberger-scorecard-scoring-architecture-2026-03-27.md` — scorer revalidation
- **Solutions**: `docs/solutions/best-practices/sniff-and-crowd-sniff-complementary-discovery-2026-03-30.md` — vocabulary alignment
- **External**: [OpenClaw skill format spec](https://github.com/openclaw/clawhub/blob/main/docs/skill-format.md) — frontmatter `requires.env` and `envVars` reference
- **External**: [OpenClaw clawhub repo](https://github.com/openclaw/clawhub) — hub validation context
