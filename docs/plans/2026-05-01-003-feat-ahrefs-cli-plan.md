---
title: "feat(cli): generate ahrefs-pp-cli from reconstructed internal spec"
type: feat
status: draft
date: 2026-05-01
---

# feat(cli): generate ahrefs-pp-cli from reconstructed internal spec

## Summary

Generate a printed CLI (`ahrefs-pp-cli`) for Ahrefs API v3 from the official `ahrefs/ahrefs-python` SDK, scoped to the first 29 read endpoints. Do not reduce v1 below these 29 endpoints.

Ahrefs does not publish a downloadable OpenAPI spec. The official Python SDK is generated from Ahrefs' internal API contract and currently ships:

- `src/ahrefs/_search_index.json`
- `src/ahrefs/_generated_methods.py`
- `src/ahrefs/types/_generated.py`
- transport helpers in `src/ahrefs/_base_client.py` and `src/ahrefs/_sync_client.py`

This plan reconstructs a Printing Press internal YAML spec, not OpenAPI. The internal spec format supports the `mcp:` block that this CLI needs; the OpenAPI parser currently does not extract `mcp:` from OpenAPI documents.

The current SDK master inspected during this plan review is `15022030974ffbd46f82dd3d6fb9d298d6b30bf9`. At that revision, `_search_index.json` contains 105 methods across 11 sections. The reconstruction script must emit a manifest proving total methods discovered, sections discovered, selected v1 methods, skipped methods, transport paths, HTTP verbs, and source file SHAs. The manifest is the count authority. If counts drift on re-run, the reconstruction command fails until the allowlist and plan are consciously updated.

## Non-Negotiables

- Keep v1 at 29 endpoints.
- Do not invent generator, verifier, or catalog features.
- Use local `--spec` generation during development. Do not depend on catalog lookup finding `catalog/specs/**`.
- Keep the plan `status: draft` until Cathryn explicitly approves implementation.
- The printed CLI must wrap real Ahrefs API endpoints. No hand-written response builders or endpoint stubs.

## Repo Facts Confirmed

These repo behaviors shape the plan:

- OpenAPI parsing currently extracts `x-api-name`, `x-display-name`, `x-website`, and `x-proxy-routes`, but not an OpenAPI `mcp:` block (`internal/openapi/parser.go`).
- Internal YAML specs support `mcp.transport`, `mcp.orchestration`, and `mcp.endpoint_tools` (`internal/spec/spec.go`).
- Catalog embed only includes root `catalog/*.yaml`; `catalog/specs/**` is not embedded (`catalog/catalog.go`).
- `generate` takes `--spec <path>` or `--plan <path>`, and `--dry-run` is a flag on `generate`.
- `dogfood`, `verify`, `scorecard`, and `shipcheck` take `--dir`; `dogfood`, `verify`, `scorecard`, and `shipcheck` can also take `--spec`.
- `publish` is a command group with `validate`, `package`, and `rename`; there is no `printing-press publish ahrefs`.
- `verify` runs mock mode when no `--api-key` is supplied and live mode when `--api-key` is supplied. There is no `--mock`, `--live`, or `--probes-only`.
- `shipcheck` runs `scorecard --live-check` by default; pass `--no-live-check` to avoid live scorecard sampling.
- The generated doctor probes `auth.verify_path` when set, otherwise `/`.
- Endpoint commands register normal Cobra flags. There is no `@file` expansion for flag values.
- Generated endpoint commands can emit `mcp:read-only` annotations when `endpoint.meta["mcp:read-only"] == "true"` or the read heuristic classifies the command as read-only.
- Dogfood can load internal YAML specs, but its path-validity reporting is skipped for internal YAML with detail `internal-yaml spec: paths validated at parse time`. Scorecard still converts internal YAML paths and should be treated as a real path-validity signal.

## Goal

Land an `ahrefs` catalog entry and a generated `ahrefs-pp-cli` that:

1. Mirrors 29 v1 Ahrefs API endpoints across 7 sections.
2. Uses Printing Press internal YAML as the source spec at `catalog/specs/ahrefs-v3.yaml`.
3. Authenticates with `Authorization: Bearer <token>` from `AHREFS_API_KEY`.
4. Emits stdio and HTTP MCP transport support while keeping endpoint tools visible for the 29-endpoint v1 surface.
5. Avoids paid API-unit burn in routine shipcheck by running mock verification, omitting `scorecard --live-check`, and manually probing only known-free endpoints when live credentials are available.
6. Produces a reconstruction manifest that proves SDK provenance, endpoint counts, selected endpoints, skipped endpoints, paths, verbs, request placement, and free-endpoint status.
7. Marks the two public crawler endpoints as `no_auth: true` so generated MCP descriptions and `tools-manifest.json` reflect that those endpoints work without an Ahrefs key.

## Requirements

### Spec Reconstruction

- **R1.** Write `scripts/reconstruct-ahrefs-spec.py` to consume pinned SDK artifacts and emit:
  - `catalog/specs/ahrefs-v3.yaml`
  - `catalog/specs/ahrefs-v3.manifest.json`
- **R2.** Target Printing Press internal YAML, not OpenAPI.
- **R3.** Pin the SDK commit SHA and source file blob SHAs in `catalog/specs/ahrefs-v3.manifest.json`. The internal spec parser has no supported top-level metadata field, so the manifest is the provenance authority; YAML comments in the spec are acceptable for human context only.
- **R4.** Extract transport paths from `_generated_methods.py` `_request(api_section, endpoint, ...)` calls. Do not derive URLs from method names.
- **R5.** Extract base URL and auth behavior from SDK transport helpers:
  - base URL: `https://api.ahrefs.com/v3`
  - header: `Authorization: Bearer <key>`
  - SDK env var: `AHREFS_API_KEY`
- **R6.** Extract request parameters from generated method signatures and request model classes. Request placement is query for GET. If a future selected endpoint uses `http_method="POST"` with read-only filter body, put body fields under `body:` and set `meta: {"mcp:read-only": "true"}` explicitly.
- **R7.** Emit all response schema types needed by the 29 selected endpoints from `types/_generated.py`. If exact nested schema conversion is too expensive for v1, use object/array schemas conservatively but document the skipped fidelity in the manifest.
- **R8.** Emit `auth.verify_path: /subscription-info/limits-and-usage`.
- **R9.** Emit `mcp.transport: [stdio, http]`. Do not set `orchestration` or `endpoint_tools` in v1.
- **R10.** Emit `meta: {"mcp:read-only": "true"}` for all 29 endpoints. They are read endpoints, and explicit metadata avoids relying on the generator's write/read heuristic.
- **R10a.** Emit `http_transport: standard`. Current Printing Press defaults `spec_source: community` to browser-compatible transport, which is wrong for Ahrefs' normal API surface.
- **R10b.** Emit `no_auth: true` for `public_crawler_ips` and `public_crawler_ip_ranges`. `free_probe` in the manifest is not enough; the internal spec field drives generated MCP/tool metadata.

### Catalog

- **R11.** Add `catalog/ahrefs.yaml` with `spec_source: community`. The SDK is official, but the reconstructed spec is produced by this repo and is not vendor-published.
- **R12.** Use `tier: community` for v1. Revisit only if Ahrefs publishes a stable downloadable spec or explicitly blesses this reconstructed spec.
- **R13.** During development, always generate with local `--spec catalog/specs/ahrefs-v3.yaml`.
- **R14.** Set `spec_url` in `catalog/ahrefs.yaml` to the raw GitHub URL only after the spec file is committed in the same branch and expected to exist on `main` after merge. Do not rely on catalog lookup before merge.
- **R15.** Do not assume `catalog/specs/**` is embedded. The catalog entry is for discovery and future remote fetch, not for local embedded spec lookup.

### Auth and Doctor

- **R16.** Use internal spec auth:

```yaml
auth:
  type: api_key
  in: header
  header: Authorization
  format: "Bearer {api_key}"
  env_vars: [AHREFS_API_KEY]
  verify_path: /subscription-info/limits-and-usage
```

This preserves the Ahrefs SDK's documented `AHREFS_API_KEY` env var while still sending a bearer token. The `{api_key}` placeholder is derived from `AHREFS_API_KEY` by the generator's env-var placeholder logic. Do not use OpenAPI `http` bearer auth unless accepting `AHREFS_TOKEN` is intentional.

### Verification and Cost Control

- **R17.** Routine verification must not pass `--api-key`; this keeps `verify` in mock mode.
- **R18.** Routine `shipcheck` must pass `--no-live-check`; otherwise the umbrella invokes `scorecard --live-check` by default.
- **R19.** There is no current verifier flag that live-tests only selected free endpoints. Live free-endpoint probing is manual for v1:
  - build the printed CLI
  - export `AHREFS_API_KEY`
  - run only the three free endpoint commands directly
- **R20.** Full live `verify --api-key "$AHREFS_API_KEY"` is explicitly out of routine v1 shipcheck because it may call all 29 endpoints and burn API units.
- **R21.** Because internal YAML path validity is skipped by current dogfood, the reconstruction manifest must prove selected transport paths and the generated source must be inspected when dogfood reports the internal-YAML skip. Scorecard still evaluates internal-YAML paths and should be treated as a real path-validity signal.

### Publishing

- **R22.** Use concrete repo and GitHub commands, not `/printing-press-publish`, for public-library publication. The skill can be a convenience later, but this plan must be executable without it.
- **R23.** Use `printing-press publish validate` and `printing-press publish package` for packaging. Then use `git`, `gh repo clone`, branch creation, copy, commit, push, and `gh pr create` against `mvanhorn/printing-press-library`.

## V1 Endpoint Allowlist

The selected v1 surface remains 29 endpoints. Paths below are from the SDK `_request(api_section, endpoint, ...)` call sites, not inferred from method names. At inspected SDK SHA `15022030974ffbd46f82dd3d6fb9d298d6b30bf9`, all selected endpoints are GET-shaped because the generated `_request` call does not pass `http_method`.

| Method | HTTP | Path |
|---|---:|---|
| `public_crawler_ips` | GET | `/public/crawler-ips` |
| `public_crawler_ip_ranges` | GET | `/public/crawler-ip-ranges` |
| `subscription_info_limits_and_usage` | GET | `/subscription-info/limits-and-usage` |
| `site_explorer_domain_rating` | GET | `/site-explorer/domain-rating` |
| `site_explorer_domain_rating_history` | GET | `/site-explorer/domain-rating-history` |
| `site_explorer_backlinks_stats` | GET | `/site-explorer/backlinks-stats` |
| `site_explorer_refdomains_history` | GET | `/site-explorer/refdomains-history` |
| `site_explorer_metrics` | GET | `/site-explorer/metrics` |
| `site_explorer_all_backlinks` | GET | `/site-explorer/all-backlinks` |
| `site_explorer_broken_backlinks` | GET | `/site-explorer/broken-backlinks` |
| `site_explorer_organic_keywords` | GET | `/site-explorer/organic-keywords` |
| `site_explorer_organic_competitors` | GET | `/site-explorer/organic-competitors` |
| `site_explorer_top_pages` | GET | `/site-explorer/top-pages` |
| `site_explorer_pages_by_traffic` | GET | `/site-explorer/pages-by-traffic` |
| `site_explorer_metrics_by_country` | GET | `/site-explorer/metrics-by-country` |
| `keywords_explorer_overview` | GET | `/keywords-explorer/overview` |
| `keywords_explorer_matching_terms` | GET | `/keywords-explorer/matching-terms` |
| `keywords_explorer_related_terms` | GET | `/keywords-explorer/related-terms` |
| `keywords_explorer_search_suggestions` | GET | `/keywords-explorer/search-suggestions` |
| `keywords_explorer_volume_history` | GET | `/keywords-explorer/volume-history` |
| `keywords_explorer_volume_by_country` | GET | `/keywords-explorer/volume-by-country` |
| `rank_tracker_overview` | GET | `/rank-tracker/overview` |
| `rank_tracker_competitors_overview` | GET | `/rank-tracker/competitors-overview` |
| `rank_tracker_serp_overview` | GET | `/rank-tracker/serp-overview` |
| `site_audit_projects` | GET | `/site-audit/projects` |
| `site_audit_issues` | GET | `/site-audit/issues` |
| `site_audit_page_explorer` | GET | `/site-audit/page-explorer` |
| `site_audit_page_content` | GET | `/site-audit/page-content` |
| `serp_overview` | GET | `/serp-overview/serp-overview` |

Notes:

- The old plan name `site_explorer_backlinks` is replaced by the SDK method `site_explorer_all_backlinks`.
- The three free live probes are `public_crawler_ips`, `public_crawler_ip_ranges`, and `subscription_info_limits_and_usage`.
- The script must fail if any allowlisted method is missing from `_search_index.json` or `_generated_methods.py`.

## Reconstruction Manifest Contract

`catalog/specs/ahrefs-v3.manifest.json` must include:

```json
{
  "sdk_repo": "https://github.com/ahrefs/ahrefs-python",
  "sdk_commit": "15022030974ffbd46f82dd3d6fb9d298d6b30bf9",
  "source_files": {
    "src/ahrefs/_search_index.json": "<blob-sha>",
    "src/ahrefs/_generated_methods.py": "<blob-sha>",
    "src/ahrefs/types/_generated.py": "<blob-sha>",
    "src/ahrefs/_base_client.py": "<blob-sha>",
    "src/ahrefs/_sync_client.py": "<blob-sha>"
  },
  "total_methods_discovered": 105,
  "sections_discovered": {
    "batch-analysis": 1,
    "brand-radar": 9,
    "keywords-explorer": 6,
    "management": 16,
    "public": 2,
    "rank-tracker": 5,
    "serp-overview": 1,
    "site-audit": 4,
    "site-explorer": 26,
    "subscription-info": 1,
    "web-analytics": 34
  },
  "methods_selected": 29,
  "methods_skipped": 76,
  "selected": [
    {
      "method": "site_explorer_domain_rating",
      "path": "/site-explorer/domain-rating",
      "http_method": "GET",
      "request_placement": "query",
      "source_call": "self._request(\"site-explorer\", \"domain-rating\", ...)",
      "no_auth": false,
      "free_probe": false
    }
  ]
}
```

The exact source SHAs come from the pinned SDK commit. The counts above reflect the inspected SDK master. If Ahrefs changes the SDK before implementation starts, update this manifest and this plan consciously rather than silently accepting drift.

## Internal Spec Shape

The reconstructed spec should use this shape:

```yaml
name: ahrefs
display_name: Ahrefs
description: SEO and competitive intelligence API for backlinks, keywords, rank tracking, site audit, and SERP data.
base_url: https://api.ahrefs.com/v3
http_transport: standard
auth:
  type: api_key
  in: header
  header: Authorization
  format: "Bearer {api_key}"
  env_vars: [AHREFS_API_KEY]
  verify_path: /subscription-info/limits-and-usage
mcp:
  transport: [stdio, http]
resources:
  public:
    description: Public Ahrefs crawler endpoints.
    endpoints:
      crawler_ips:
        method: GET
        path: /public/crawler-ips
        no_auth: true
        description: Ahrefs crawler IP addresses.
        body: []
        response: {type: object}
        pagination: null
        meta:
          "mcp:read-only": "true"
  site_explorer:
    description: Site Explorer endpoints.
    endpoints:
      domain_rating:
        method: GET
        path: /site-explorer/domain-rating
        description: Domain Rating.
        params:
          - name: target
            type: string
            required: true
            positional: false
            description: Domain, URL, or path target.
          - name: protocol
            type: string
            required: false
            positional: false
            description: Target protocol mode.
          - name: date
            type: string
            required: false
            positional: false
            format: date
            description: Snapshot date.
        body: []
        response: {type: object, item: SiteExplorerDomainRatingResponse}
        pagination: null
        meta:
          "mcp:read-only": "true"
types:
  SiteExplorerDomainRatingResponse:
    fields:
      - name: data
        type: object
```

Parameter names and type details above are illustrative. The script must derive them from the SDK artifacts.

## Catalog Entry Draft

```yaml
name: ahrefs
display_name: Ahrefs
description: SEO and competitive intelligence for backlinks, keywords, rank tracking, site audit, and SERP data
category: marketing
spec_url: https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/catalog/specs/ahrefs-v3.yaml
spec_format: yaml
spec_source: community
http_transport: standard
tier: community
verified_date: "2026-05-01"
homepage: https://docs.ahrefs.com/docs/api/reference/introduction
notes: |
  Internal Printing Press spec reconstructed from the official ahrefs/ahrefs-python SDK, filtered to 29 read endpoints.
  Source SDK commit: 15022030974ffbd46f82dd3d6fb9d298d6b30bf9.
  Manifest: catalog/specs/ahrefs-v3.manifest.json.
  Use local --spec during development because catalog/specs/** is not embedded in catalog.FS.
  API units are shared across direct API v3, Ahrefs MCP, and Ahrefs Connect. Routine shipcheck must use mock verify and --no-live-check.
known_alternatives:
  - name: ahrefs-python
    url: https://github.com/ahrefs/ahrefs-python
    language: python
sandbox_endpoint: ""
```

Bootstrap order:

1. Generate and review `catalog/specs/ahrefs-v3.yaml` and `catalog/specs/ahrefs-v3.manifest.json`.
2. Add `catalog/ahrefs.yaml` pointing to the future raw GitHub URL.
3. Rebuild `./printing-press` after adding `catalog/ahrefs.yaml`; root catalog YAML is embedded at build time and manifest/category enrichment reads the embedded catalog.
4. During local development, run `generate --spec catalog/specs/ahrefs-v3.yaml`; do not run `generate ahrefs`.
5. After merge to `main`, the catalog `spec_url` becomes fetchable for future runs.

## Commands

All commands assume repo root.

### Build the printing-press binary

```bash
go build -o ./printing-press ./cmd/printing-press
```

Run this again after adding `catalog/ahrefs.yaml`, because catalog root YAML files are embedded in the binary.

### Reconstruct the internal spec

```bash
python3 scripts/reconstruct-ahrefs-spec.py \
  --sdk-repo https://github.com/ahrefs/ahrefs-python \
  --sdk-ref 15022030974ffbd46f82dd3d6fb9d298d6b30bf9 \
  --out catalog/specs/ahrefs-v3.yaml \
  --manifest catalog/specs/ahrefs-v3.manifest.json
```

### Validate catalog entry and spec parse

```bash
go test ./internal/catalog/...
./printing-press generate --spec catalog/specs/ahrefs-v3.yaml --spec-source community --transport standard --dry-run
```

### Generate the printed CLI

```bash
./printing-press generate \
  --spec catalog/specs/ahrefs-v3.yaml \
  --spec-source community \
  --transport standard \
  --output "$HOME/printing-press/library/ahrefs" \
  --force
```

### Run routine verification without paid API-unit burn

```bash
CLI_DIR="$HOME/printing-press/library/ahrefs"

./printing-press dogfood --dir "$CLI_DIR" --spec catalog/specs/ahrefs-v3.yaml
./printing-press verify --dir "$CLI_DIR" --spec catalog/specs/ahrefs-v3.yaml --fix
./printing-press workflow-verify --dir "$CLI_DIR"
./printing-press verify-skill --dir "$CLI_DIR"
./printing-press scorecard --dir "$CLI_DIR" --spec catalog/specs/ahrefs-v3.yaml
./printing-press shipcheck --dir "$CLI_DIR" --spec catalog/specs/ahrefs-v3.yaml --no-live-check
```

Expected dogfood nuance: path validity is skipped for internal YAML. Treat dead flags, auth protocol, MCP surface parity, anti-reimplementation, workflow, skill verification, and scorecard as required checks. Path correctness is proven by the manifest, generated source inspection, and scorecard's internal-YAML path-validity signal.

### Optional live free probes only

There is no verifier allowlist for live probes. Run these printed CLI commands directly instead of `verify --api-key`:

```bash
export AHREFS_API_KEY="<redacted>"
"$CLI_DIR/ahrefs-pp-cli" public crawler-ips --json
"$CLI_DIR/ahrefs-pp-cli" public crawler-ip-ranges --json
"$CLI_DIR/ahrefs-pp-cli" subscription-info --json
```

`subscription-info` is expected to be promoted because it is a single-endpoint resource. If these commands differ after generation, use the generated `--help` output and update this command list in the implementation PR.

### Package for public-library PR

```bash
STAGE_DIR="$PWD/.gotmp/publish-ahrefs"
PUBLIC_REPO_DIR="$PWD/.gotmp/printing-press-library"

./printing-press publish validate --dir "$CLI_DIR" --json
rm -rf "$STAGE_DIR"
./printing-press publish package \
  --dir "$CLI_DIR" \
  --category marketing \
  --target "$STAGE_DIR" \
  --json

gh repo clone mvanhorn/printing-press-library "$PUBLIC_REPO_DIR"
cd "$PUBLIC_REPO_DIR"
git checkout -b feat/ahrefs-cli
cp -R "$STAGE_DIR"/library/marketing/ahrefs ./library/marketing/
git status --short
git add library/marketing/ahrefs
git commit -m "feat(marketing): add ahrefs printed cli"
git push -u origin feat/ahrefs-cli
gh pr create --title "feat(marketing): add ahrefs printed cli" --body "Adds the Ahrefs printed CLI generated from the reconstructed internal spec."
```

The public-library repo is outside this repo. Do not run this packaging section while only revising this plan.

## Pricing and Unit Notes

Use USD in this plan. As of the referenced Ahrefs help docs reviewed during plan revision:

| Plan | Monthly API integration units | Max rows per request |
|---|---:|---:|
| Lite | 25,000 | 10 |
| Standard | 150,000 | 25 |
| Advanced | 500,000 | 100 |
| Enterprise | 2,000,000 | Unlimited |

Each API call has a 50-unit minimum. API units are shared across direct API v3, Ahrefs MCP, and Ahrefs Connect. Enterprise is not unlimited for API units; it has 2,000,000 included units and can buy additional units. Currency and pricing amounts are region-dependent, so this plan tracks unit limits rather than plan prices.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| SDK surface drifts before implementation | Pin SDK commit and blob SHAs; manifest fails count or allowlist drift. |
| Transport paths are guessed incorrectly | Extract from `_request(api_section, endpoint, ...)`; reject method-name-derived paths. |
| Internal YAML skips dogfood path-validity scoring | Treat as known dogfood limitation; use manifest path proof, generated source inspection, and scorecard's internal-YAML path-validity signal. |
| Routine shipcheck burns API units | Do not pass `--api-key`; pass `--no-live-check`; live-test free endpoints manually. |
| `where` filter UX is weaker than desired | v1 accepts JSON/filter strings as ordinary flags only. `@file` expansion is out of scope until implemented as a machine feature. |
| Reconstructed spec is not vendor-published | Use `spec_source: community` and `tier: community`; document official SDK provenance. |
| Response schema conversion takes longer than expected | Budget 6-10 hours and allow conservative object schemas with manifest-documented fidelity gaps for v1. |

## Out of Scope - Blocking Machine Work

These are not part of v1 implementation. If needed, write separate plans before relying on them:

1. **OpenAPI `mcp:` extraction.** Proposed plan path: `docs/plans/2026-05-XX-feat-openapi-mcp-extension-parser.md`. Files likely touched: `internal/openapi/parser.go`, `docs/SPEC-EXTENSIONS.md`, `internal/openapi/*_test.go`.
2. **Verifier live endpoint allowlist.** Proposed plan path: `docs/plans/2026-05-XX-feat-verify-live-allowlist.md`. Files likely touched: `internal/cli/verify.go`, `internal/pipeline/runtime.go`, `internal/cli/shipcheck.go`, tests.
3. **Dogfood internal-YAML path-validity reporting.** Proposed plan path: `docs/plans/2026-05-XX-feat-dogfood-internal-yaml-path-validity.md`. Files likely touched: `internal/pipeline/dogfood.go` and dogfood tests. Scorecard already converts internal YAML paths and remains in scope for v1 verification.
4. **Cobra `@file` flag expansion for JSON filters.** Proposed plan path: `docs/plans/2026-05-XX-feat-json-flag-file-expansion.md`. Files likely touched: `internal/generator/templates/command_endpoint.go.tmpl`, helper templates, generator tests.
5. **Golden snapshots for Ahrefs catalog generation.** Not needed for this catalog addition unless the implementation changes deterministic generator contracts. Use existing tests and shipcheck first.

## Review Finding Resolution

| # | Resolution |
|---:|---|
| 1 | Switched reconstruction target from OpenAPI to internal YAML because OpenAPI parser does not extract `mcp:`. |
| 2 | Rewrote all phase commands to actual `generate --spec`, `generate --dry-run`, and `--dir` forms. |
| 3 | Documented local `--spec` bootstrap and remote `spec_url` only after merge. |
| 4 | Documented that `catalog/specs/**` is not embedded and catalog lookup must not read it locally. |
| 5 | Set `spec_source: community` and `tier: community`. |
| 6 | Chose internal spec `api_key` header auth with `AHREFS_API_KEY` and `Authorization: Bearer {api_key}`. |
| 7 | Added `auth.verify_path: /subscription-info/limits-and-usage`. |
| 8 | Removed `@file` support from v1 and made it separate machine work. |
| 9 | Required path extraction from SDK `_request` call sites and listed the 29 SDK paths. |
| 10 | Added explicit `meta: {"mcp:read-only": "true"}` for all v1 endpoints; selected endpoints are currently GET. |
| 11 | Rewrote verification cost control around actual `verify`, `scorecard`, and `shipcheck` flags. |
| 12 | Fixed Enterprise API units to 2,000,000 and avoided region-specific price claims. |
| 13 | Added reconstruction manifest with counts, selected/skipped methods, sections, paths, and drift failure. |
| 14 | Revised estimate to 6-10 hours. |
| 15 | Dropped endpoint golden snapshots from v1 and listed them as unnecessary unless generator contracts change. |
| 16 | Replaced `/printing-press-publish` with concrete `publish validate`, `publish package`, `git`, and `gh` steps. |

## Estimate

Minimum implementation time: 6-10 hours.

Breakdown:

- SDK artifact retrieval, SHA pinning, and manifest emission: 1 hour
- Method and transport extraction from generated Python: 1.5-2 hours
- Parameter, enum, request placement, and response type conversion: 2-3 hours
- Internal spec validation and generator dry-run fixes: 1-2 hours
- Generation, dogfood, verify, scorecard, manual free probes, and packaging: 1.5-2 hours

## Deferred Expansion

v2 can expand beyond 29 endpoints after v1 proves reconstruction and generation against the current machine. The likely expansion areas are Web Analytics, Brand Radar, Management write endpoints, remaining Rank Tracker endpoints, remaining Site Explorer endpoints, and Batch Analysis.

When the surface grows past roughly 50 endpoints, switch the internal spec MCP block to:

```yaml
mcp:
  transport: [stdio, http]
  orchestration: code
  endpoint_tools: hidden
```

Do not make that switch in v1.
