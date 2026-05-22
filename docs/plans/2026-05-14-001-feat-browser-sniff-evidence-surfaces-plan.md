---
title: Surface per-endpoint evidence and confidence from browser-sniff
status: active
created: 2026-05-14
type: feat
depth: standard
---

# Surface per-endpoint evidence and confidence from browser-sniff

Target repo: cli-printing-press

## Summary

PP's `internal/browsersniff` package already computes most of what this plan needs. `AnalyzeTraffic` produces a versioned `TrafficAnalysis` with `EndpointClusters`, an `AuthAnalysis` of `AuthCandidate`s with header names and evidence-back-references, and a numeric `Confidence` per observation. `buildEndpoint` in `specgen.go` already calls `detectAuth(group.Entries, "")` per endpoint group. None of that per-endpoint detail escapes in a form downstream skills can act on, and the spec YAML carries no per-operation evidence handles.

This plan surfaces four signals without inventing new analysis:

1. Per-endpoint `ObservedAuth` field added to `EndpointCluster` and to `spec.Endpoint`. Population reuses the existing `detectAuth(group.Entries, "")` call site in `buildEndpoint`; only header names travel to YAML, values are never written. Enables Phase 2 Free/Paid Tier Routing Enrichment to route by per-endpoint evidence rather than spec-level guesses.
2. Per-endpoint confidence + normalization flags added as fields on the existing `EndpointCluster` struct, not a new sidecar file. `confidence: low | medium | high` and a small flag set (`single-sample`, `single-status`, `mixed-content-types`, `request-body-only-on-some-samples`, `divergent-response-shape`). The existing `<spec-stem>-traffic-analysis.json` becomes the canonical confidence surface; no second file.
3. New `<spec-stem>-samples/<method>__<path-slug>__<hash>.json` directory of one redacted request/response sample per endpoint group. New redaction helper is required (browsersniff has no redactor today). Mirrors the `writeBrowserSniffOutputs` temp-then-atomic-rename pattern for directory output.
4. New `--min-samples N` flag on the `browser-sniff` subcommand, and a new `--include` flag complementing the existing `--blocklist`. Below-threshold endpoints drop from emitted spec YAML but remain visible in `TrafficAnalysis.EndpointClusters` so the audit trail is preserved.

Recommendation: ship in order U1 -> U2 -> U3 -> U4. U1 lands the biggest downstream payoff and is structurally simplest. U2 and U3 are independent of U1. U4 depends on U2 so dropped endpoints stay auditable.

Explicitly deferred: HTML coverage report, multi-stage pipeline refactor, spec preview helper. See Scope Boundaries.

---

## Problem Frame

`internal/browsersniff/analysis.go` already does the heavy lifting. The data exists; the surface does not.

- `AnalyzeTraffic` produces `TrafficAnalysis{Auth, EndpointClusters, Protections, ...}` (analysis.go L420-453). `EndpointClusters` is a list of grouped endpoints. The struct does not currently carry sample counts, observed status codes, or any confidence bucket.
- `detectTrafficAuth(capture, entries)` (analysis.go L714-785) produces `AuthCandidate`s with `HeaderNames`, `CookieNames`, `QueryNames`, numeric `Confidence`, and `Evidence []EvidenceRef`. `EvidenceRef.EntryIndex` points at the source `EnrichedEntry`. That's the join key from auth candidate to endpoint cluster - but nothing actually joins them.
- `buildEndpoint(group EndpointGroup)` in specgen.go L559-601 already calls `detectAuth(nil, group.Entries, "")` to produce a `spec.Auth` it uses internally for query-param filtering. The function knows the auth headers for this endpoint group at the moment of spec emission. It throws that detail away after using it for filtering.
- `spec.Endpoint` (the YAML emission target) has no field for observed auth and no extensions map. The information cannot land on the spec without a typed field addition.
- Response bodies in `EnrichedEntry.ResponseBody` are stored unredacted. There is no redactor in `internal/browsersniff`. The only redaction-shaped helper is the auth-domain binding check in `internal/cli/browser_sniff.go`. A samples-on-disk feature needs a fresh redactor.
- `writeBrowserSniffOutputs` (browser_sniff.go L95-138) writes the spec and the traffic-analysis JSON with a temp-then-atomic-rename + backup-restore pattern. A samples directory needs the same crash-safety treatment adapted for directory output.
- The `browser-sniff` subcommand exposes `--har, --output, --analysis-output, --name, --blocklist, --auth-from`. There is no `--min-samples`, no `--include`. The `--blocklist` flag already feeds `SetAdditionalBlocklist` (classifier.go L53-58) so the existing extension pattern is hostname-list-based.

The four units below build on top of these existing surfaces, not around them.

---

## Scope Boundaries

### In scope

- Additive field changes to `EndpointCluster` (analysis.go) and `spec.Endpoint` (internal/spec) that carry per-endpoint observed auth, sample count, status codes, normalization flags, and a coarse confidence bucket.
- A redaction helper in `internal/browsersniff` covering header names, body keys, and a small regex set (JWT, email, phone).
- A new `<spec-stem>-samples/` output directory with one redacted file per endpoint group.
- Two new `browser-sniff` flags: `--min-samples N` and `--include <csv>`.
- Phase 2 Auth Enrichment note in `skills/printing-press/SKILL.md` mentioning the new per-endpoint signal.
- Golden harness updates (`testdata/golden/...`) for the new spec fields, traffic-analysis fields, and samples directory.
- Test cases in `internal/cli/browser_sniff_test.go`, `internal/browsersniff/analysis_test.go`, `internal/browsersniff/specgen_test.go`, and `internal/browsersniff/classifier_test.go`.

### Outside this product's identity

- Cloud-hosted capture backends. PP capture stays local.
- JS or non-Go client emission. PP outputs Go.
- HTML coverage report as a primary deliverable. The existing markdown discovery report stays canonical for user-facing review.

### Deferred to Follow-Up Work

- Re-architecting `internal/browsersniff` into discrete load/filter/normalize/infer/emit stages with re-runnable sub-steps. Genuinely useful for debugging but a separate refactor plan.
- Spec preview helper (`browser-sniff --preview` opening a local Swagger UI). Quality-of-life, not load-bearing.
- A `--slug` variable type in `normalizeEntryPath` (varying-alpha-tokens segment detection). Today PP recognizes `{id}/{uuid}/{hash}`; slug heuristics would be a separate ask.
- Hoisting structurally-identical schemas into `components.schemas`. Out of scope; deals with spec ergonomics, not evidence.

---

## Key Technical Decisions

- The confidence and observed-auth signals live on the existing `EndpointCluster` type (analysis.go). A separate `confidence.json` sidecar would duplicate state that already belongs in `TrafficAnalysis`. The single sidecar file (`<spec-stem>-traffic-analysis.json`) becomes the canonical surface.
- `EndpointCluster` gains four additive fields: `SampleCount int`, `StatusCodes []int`, `NormalizationFlags []string`, `Confidence string`, `ObservedAuth []string`. All `omitempty`. `trafficAnalysisVersion` stays at `"1"` because the existing `UnmarshalJSON` flow tolerates unknown fields. No legacy-compat hook needed; readers older than this change ignore the new fields, which is the right behavior.
- `spec.Endpoint` gains one additive field: `ObservedAuth []string` with `yaml:"observed_auth,omitempty"`. Spec consumers older than this change ignore it. No version bump on the spec format.
- Confidence bucket assignment lives in a small helper inside `buildEndpointClusters` (analysis.go around L435 in `AnalyzeTraffic`). Rule: `low` if `SampleCount < 3` OR any normalization flag is set; `medium` if `3 <= SampleCount <= 9` AND no flags; `high` if `SampleCount >= 10` AND distinct status codes >= 2 AND no flags. Coarse on purpose so future numeric-confidence tuning does not break downstream consumers.
- Normalization flag names: `single-sample`, `single-status`, `mixed-content-types`, `request-body-only-on-some-samples`, `divergent-response-shape`. PP-specific flags may be added later (e.g. `proxy-envelope-detected`, `graphql-persisted-hash`) but those names should not collide.
- Per-endpoint `ObservedAuth` is derived inside `buildEndpoint` (specgen.go L559) by extending the existing `detectAuth(nil, group.Entries, "")` call to also return the set of auth-shaped header names it observed. The set is lowercased, deduplicated, sorted. Empty sets are not written. Population on `EndpointCluster` happens in `buildEndpointClusters` by replaying the same per-group detection (or by passing the spec endpoints back into cluster building - the call site is small enough that duplicating the detection is cheaper than threading a shared map).
- Samples directory uses path `<spec-stem>-samples/<method>__<path-slug>__<hash>.json`. Filename slug rules: method lowercased; path slug replaces `/` with `_` and removes `{` `}`; hash is the first 8 chars of `sha256(method + " " + normalizedPath)`. Same endpoint -> same filename across reruns. Collision probability with 8 hex chars is ~10^-9 at typical endpoint counts; acceptable.
- Sample selection per endpoint: most-recent successful 2xx; fall back to most-recent non-error; fall back to most-recent entry. Ordering within `EndpointGroup.Entries` is capture-order (preserved through `classifyInCaptureOrder`).
- Redaction is new code in `internal/browsersniff/redact.go`. Header names to redact (case-insensitive): `authorization, cookie, set-cookie, x-csrf-token, x-xsrf-token, x-api-key, proxy-authorization`, plus name patterns `*token*, *secret*, *signature*`. Body keys: `password, token, secret, api_key, apiKey, accessToken, refreshToken`. Body regexes: JWT (`^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`), email, phone (E.164-ish). Replacement is the literal string `<redacted>` so JSON structure is preserved for the reviewer. A per-file `redactions []string` field lists which header names and body keys were stripped, so the reviewer knows what is missing.
- Samples directory write uses a `<spec-stem>-samples.tmp/` directory next to the spec, written first; on success the existing samples directory is renamed to a `.backup` sibling and the `.tmp` is renamed to the canonical name; on failure the backup is restored. Mirrors the existing `writeBrowserSniffOutputs` two-phase commit pattern adapted for directory atomicity. `os.Rename` on a directory is atomic on POSIX, which is sufficient.
- `--min-samples` filtering happens at the spec emission boundary in `AnalyzeCapture` (specgen.go around L35) - endpoints whose `EndpointGroup.Entries` count is below the threshold are excluded from the emitted `apiSpec.Resources[].Endpoints[]`. They remain in `TrafficAnalysis.EndpointClusters` so the audit trail is intact.
- `--include <csv>` adds a new precedence rule in `scoreEntry`: if the URL host or path matches any include pattern, the score is forced positive regardless of blocklist or static-asset suffix demotion. Include wins over blocklist.
- `--blocklist` stays as the existing flag. Documentation in the SKILL.md note clarifies that `--blocklist` extends `DefaultBlocklist` and `--include` rescues.
- All four units are independently mergeable. Recommended order is U1 -> U2 -> U3 -> U4 but no unit depends on another to compile or pass tests.

---

## Implementation Units

### U1. Per-endpoint observed-auth on cluster and spec endpoint

Goal: thread the auth-shaped header names that `detectAuth` already extracts per endpoint group up to both `EndpointCluster` and `spec.Endpoint`, so downstream consumers (Phase 2 Auth Enrichment, MCP routing, tier detection) can act on per-operation evidence.

Requirements: per-endpoint auth visibility.

Dependencies: none.

Files:
- internal/spec/ (the package containing `spec.Endpoint` definition - add `ObservedAuth []string` field with `yaml:"observed_auth,omitempty"` tag)
- internal/browsersniff/analysis.go (add `ObservedAuth []string` to `EndpointCluster`; populate in `buildEndpointClusters` around L435)
- internal/browsersniff/specgen.go (extend the existing `detectAuth(nil, group.Entries, "")` call in `buildEndpoint` L569 to also return observed header names; set `endpoint.ObservedAuth` before return)
- internal/browsersniff/analysis_test.go (cluster-level observed-auth cases)
- internal/browsersniff/specgen_test.go (spec-level observed-auth cases)
- skills/printing-press/SKILL.md (Phase 2 Free/Paid Tier Routing Enrichment around L1707 - mention reading `endpoint.ObservedAuth` and `cluster.ObservedAuth` for per-endpoint signal)
- testdata/golden/expected/ (refresh affected browser-sniff golden specs and traffic-analysis fixtures)

Approach: the existing `detectAuth(capture *EnrichedCapture, entries []EnrichedEntry, ...)` signature already takes entries. Add a sibling helper or extend the return shape to also surface `[]string` of observed auth-shaped header names. Capture-level auth context (the `*EnrichedCapture` arg) is left alone. The header-name set is the union across `group.Entries` of names matching: case-insensitive `authorization` (Bearer-prefixed or not), case-insensitive `cookie`, names containing `api-key` / `api_key` / `x-auth-token`, plus the patterns matched by the existing `isAuthQueryName` filter for query-string auth (those go into a separate per-endpoint `observed_auth_query []string` if useful - see deferred items, not in this unit).

Patterns to follow: the existing `detectTrafficAuth` accumulation pattern at analysis.go L714 (case switches on header name, dedup via `uniqueStrings`). Reuse `uniqueStrings` and the existing case predicates - do not invent new ones.

Test scenarios:
- Happy path: one sample with `Authorization: Bearer XYZ` -> cluster `ObservedAuth == ["authorization"]` and spec endpoint `ObservedAuth == ["authorization"]`.
- Multiple auth headers across one sample: `Authorization` + `X-CSRF-Token` -> both present in alphabetical order.
- Mixed across samples: sample A has `Authorization`, sample B does not -> union still emits `["authorization"]` (record presence across the group, not requirement per sample).
- No auth headers anywhere on the endpoint -> field is omitted from the spec YAML and from the cluster JSON (omitempty respected).
- Case-insensitive merge: `Authorization` in one sample and `AUTHORIZATION` in another collapse to a single `authorization` entry.
- Header value redaction: a sample with `Authorization: Bearer eyJ...` produces `ObservedAuth == ["authorization"]` without leaking the token value into either the cluster or the spec.
- GraphQL endpoints (built via `buildGraphQLOperationEndpoint` at specgen.go L228) also carry `ObservedAuth` on their spec endpoint.

Verification: existing browser-sniff goldens refreshed; new auth-mixed golden fixture renders the field; `go test ./internal/browsersniff/... ./internal/cli/...` passes; `scripts/golden.sh verify` clean.

### U2. Confidence bucket and normalization flags on `EndpointCluster`

Goal: surface the per-cluster sample stats and a coarse `low | medium | high` bucket on the existing `EndpointCluster` type, so absorb / dogfood / novel-feature gating skills can reason about which endpoints are anecdotes vs staples.

Requirements: machine-readable confidence signal for downstream gates.

Dependencies: none. Independent of U1 even though both land on `EndpointCluster`.

Files:
- internal/browsersniff/analysis.go (extend `EndpointCluster` with `SampleCount int`, `StatusCodes []int`, `NormalizationFlags []string`, `Confidence string`; populate in `buildEndpointClusters` around L435)
- internal/browsersniff/analysis_test.go (bucket boundary tests, flag tests)
- testdata/golden/expected/ (refresh affected traffic-analysis goldens)
- skills/printing-press/SKILL.md (absorb gate around L1241 region - mention reading `EndpointCluster.Confidence`)

Approach: add a `bucketConfidence(samples int, statuses []int, flags []string) string` helper in analysis.go. Populate `SampleCount` from `len(group.Entries)`. Populate `StatusCodes` from the deduplicated sorted `ResponseStatus` values across the group's entries. Populate `NormalizationFlags`:
- `single-sample` if `SampleCount == 1`
- `single-status` if `len(StatusCodes) == 1`
- `mixed-content-types` if the group's entries show more than one distinct `ResponseContentType` (case-insensitive prefix grouping: `application/json` vs `text/html` etc.)
- `request-body-only-on-some-samples` if the group's method is one of `POST/PUT/PATCH` AND `0 < count(entries with non-empty RequestBody) < SampleCount`
- `divergent-response-shape` - reserved; this signal is intended for the future case where pre-normalization paths collapsed but responses diverge structurally. Today PP keys clusters by `host + method + normalizedPath` so collapse is already prevented. Leave the flag name reserved; do not populate in this unit. Document in a comment.

Bucket rule:
- `low` if `SampleCount < 3` OR `len(NormalizationFlags) > 0`
- `medium` if `3 <= SampleCount <= 9` AND `len(NormalizationFlags) == 0`
- `high` if `SampleCount >= 10` AND `len(StatusCodes) >= 2` AND `len(NormalizationFlags) == 0`

Patterns to follow: the existing `EndpointCluster` definition and writer (whatever shape it has today - the cluster type is built in `buildEndpointClusters`, sorted via `sortTrafficAnalysis`). The new fields are populated in the same builder, then sorted alongside existing fields by the existing sort code.

Test scenarios:
- Single-sample endpoint -> `Confidence: "low"`, `NormalizationFlags: ["single-sample"]`, `SampleCount: 1`.
- 5 samples, all returning 200 -> `Confidence: "low"`, flags include `single-status`, `SampleCount: 5`, `StatusCodes: [200]`.
- 12 samples, statuses 200 and 404, no flags -> `Confidence: "high"`, `StatusCodes: [200, 404]`, `SampleCount: 12`.
- 4 samples, statuses 200 and 401, no flags -> `Confidence: "medium"`.
- POST endpoint where 2 of 3 samples carry a request body -> flag `request-body-only-on-some-samples`, bucket `low`.
- Endpoint with both `application/json` and `text/html` response content-types -> flag `mixed-content-types`.
- GET endpoint with body samples (impossible in HTTP-correct terms, but seen in the wild) does NOT trigger `request-body-only-on-some-samples` because the method is not POST/PUT/PATCH.
- Cluster sort order remains deterministic (existing sort by host/method/path is preserved).
- The traffic-analysis JSON round-trips: write -> read -> all new fields preserved with version still `"1"`.

Verification: traffic-analysis golden fixtures show the new fields; `go test ./internal/browsersniff/...` passes; `scripts/golden.sh verify` clean.

### U3. Redacted per-endpoint samples directory

Goal: write one redacted JSON file per endpoint group to `<spec-stem>-samples/`, named by method + path slug + hash, so absorb / dogfood / reviewer flows can point at concrete evidence per endpoint.

Requirements: discoverable per-endpoint evidence.

Dependencies: none (the redaction helper is built inside this unit; not reused from elsewhere).

Files:
- internal/browsersniff/redact.go (new file - redaction helper for header maps and JSON body trees)
- internal/browsersniff/redact_test.go (new file - redactor unit tests)
- internal/browsersniff/specgen.go (sample selection logic in or alongside `buildEndpoint`, then directory write triggered from a new `WriteSamples(apiSpec, capture, outputDir)` helper)
- internal/cli/browser_sniff.go (extend `writeBrowserSniffOutputs` to also write the samples directory using the temp-then-rename + backup-restore pattern)
- internal/cli/browser_sniff_test.go (extend `TestWriteBrowserSniffOutputsRestoresExistingFilesWhenSpecPublishFails` and add new test for samples-dir restore-on-failure)
- internal/browsersniff/specgen_test.go (sample selection rules, filename hashing determinism)
- skills/printing-press/references/browser-sniff-capture.md (add a short section pointing users at `<spec-stem>-samples/`)
- testdata/golden/expected/ (add representative golden samples for an existing capture fixture)

Approach: redaction helper API:
- `RedactHeaders(headers map[string]string) (map[string]string, []string)` - returns redacted headers plus the list of header names that were redacted (lowercased, sorted, deduplicated).
- `RedactJSONBody(body string) (string, []string)` - parses body as JSON; if parse succeeds, walks the object tree redacting sensitive keys and value-pattern matches, returning the re-serialized JSON and the list of redacted dotted paths. If body is not JSON, applies value-pattern regex sweep against the raw string and returns it with a list of pattern names that matched.

Sample selection (per `EndpointGroup`): iterate `group.Entries` in capture order; pick the most-recent successful (200-299) entry. If none successful, pick the most-recent entry regardless of status. Capture order is preserved through `classifyInCaptureOrder`.

Per-file format:

```json
{
  "endpoint": "GET /v1/items/{id}",
  "raw_url": "/v1/items/42?page=2",
  "status": 200,
  "method": "GET",
  "request_headers": { "accept": "application/json" },
  "request_body": null,
  "response_headers": { "content-type": "application/json" },
  "response_body": { "id": 42, "name": "Widget" },
  "redactions": ["request_headers.authorization", "response_body.api_key"],
  "response_body_known": true
}
```

Filename rule: `<method>__<path-slug>__<hash>.json` where method is lowercased, path-slug replaces `/` with `_` and strips `{` `}`, hash is `sha256(METHOD + " " + normalizedPath)[:8]` hex.

Directory write contract:
1. Build the sample-file contents in memory for every endpoint group.
2. Create `<spec-stem>-samples.tmp/` (clean if pre-existing).
3. Write every sample file into the temp directory.
4. If a `<spec-stem>-samples/` directory already exists, rename it to `<spec-stem>-samples.backup/`.
5. Rename the `.tmp/` directory to `<spec-stem>-samples/`.
6. On error at step 4 or 5, restore the backup.
7. On success, remove the backup.

Patterns to follow: `writeBrowserSniffOutputs` two-phase pattern in browser_sniff.go L95-138 (`siblingTempPath`, `backupFileForReplace`, `restoreFileBackup`). Mirror for directories using `os.Rename` on the directory.

Test scenarios:
- Two endpoints with identical templated path but different methods produce different filenames (different hash inputs).
- Same endpoint captured twice across reruns produces the same filename (hash deterministic).
- Sample selection: 5 entries (200, 200, 500, 200, 404) -> the most-recent 200 wins.
- Sample selection: 3 entries all 500 -> the most-recent 500 wins.
- Redaction header: `Authorization: Bearer XYZ` becomes `Authorization: <redacted>` and `redactions` contains `request_headers.authorization`.
- Redaction body key: response body `{"api_key": "sk_live_..."}` becomes `{"api_key": "<redacted>"}` and `redactions` contains `response_body.api_key`.
- Redaction value regex: response body containing a JWT-shaped string outside a known key still gets replaced with `<redacted>` token-by-token; `redactions` contains `pattern:jwt`.
- Filename slugging: `POST /v1/orders/{orderId}/items/{itemId}` slugs to `post__v1_orders_orderId_items_itemId__<hash>.json`.
- Empty response body: file is written with `response_body: null` and `response_body_known: false`.
- HTML response (`groupLooksHTML` is true): sample still written; `response_body` is the raw HTML string truncated to a reasonable limit (e.g. 16 KB) with a `response_body_truncated: true` flag if truncation occurred. Truncation limit goes in a named constant.
- Directory restore: if spec write fails after samples directory is in place, the previous samples directory is restored from the `.backup/` sibling (test extends the existing `TestWriteBrowserSniffOutputsRestoresExistingFilesWhenSpecPublishFails`).

Verification: samples directory exists with one file per endpoint group; filenames pass `regexp.MustCompile("^[a-z]+__[a-zA-Z0-9_]+__[0-9a-f]{8}\\.json$")`; redaction tests pass; restore-on-failure test passes; golden fixture matches.

### U4. `--min-samples` and `--include` flags

Goal: give operators tunable knobs for noise reduction at the spec-emission boundary, and a rescue mechanism for cases where the default blocklist or static-asset demotion drops something they need.

Requirements: operator-visible noise filtering and rescue.

Dependencies: U2 (so dropped-by-min-samples endpoints remain visible in `TrafficAnalysis.EndpointClusters` with their `low` bucket and `single-sample` flag for audit).

Files:
- internal/cli/browser_sniff.go (`newBrowserSniffCmd` flag definitions; thread values to spec emission)
- internal/browsersniff/classifier.go (add `SetAdditionalIncludeList` mirror of `SetAdditionalBlocklist`; extend `scoreEntry` to short-circuit positive on include match)
- internal/browsersniff/classifier_test.go (include precedence, score short-circuit)
- internal/browsersniff/specgen.go (apply `MinSamples` filter at the boundary where groups become spec endpoints in `AnalyzeCapture` - below-threshold groups skip endpoint emission but still appear in `TrafficAnalysis.EndpointClusters` because that path runs independently)
- internal/browsersniff/specgen_test.go (min-samples emission cases)
- internal/cli/browser_sniff_test.go (flag parsing + integration)
- skills/printing-press/references/browser-sniff-capture.md (document `--min-samples`, `--include`, and the relationship to `--blocklist`)
- testdata/golden/expected/ (help-text golden refresh; specific min-samples golden case)

Approach:
- Add `--min-samples` (int, default 1) and `--include` (string, comma-separated, default empty) to `newBrowserSniffCmd`.
- Add a package-level `SetAdditionalIncludeList(patterns []string)` and `additionalIncludeList []string` mirror of the existing `SetAdditionalBlocklist` / `additionalBlocklist` machinery in classifier.go.
- Modify `scoreEntry` to check the include list first. If the URL host or path matches any include pattern (substring match; mirror the simple `HasSuffix(host, blocked)` style used by blocklist), force the score to a high positive value and skip the rest of the scoring. This makes include unconditionally win over blocklist demotion AND static-asset suffix demotion.
- Thread `--min-samples` through `AnalyzeCapture` to the point where endpoint groups become `apiSpec.Resources[].Endpoints[]`. Drop groups whose `len(Entries) < minSamples`. Do not touch `AnalyzeTraffic` - clusters with low sample counts still appear in `TrafficAnalysis` so the user can see what was filtered.
- Document `--min-samples=2` as the recommended value for production capture in the SKILL.md reference.

Patterns to follow: existing `SetAdditionalBlocklist` and `additionalBlocklist` machinery (classifier.go L22-23, L53-58). Existing `splitCSV` helper (browser_sniff.go L173). Existing `cmd.Flags().StringVar` pattern in `newBrowserSniffCmd`.

Test scenarios:
- Default behavior (`--min-samples=1`): existing capture fixtures produce identical spec output (regression guard).
- `--min-samples=2`: an endpoint with one sample drops from the emitted spec but remains in `TrafficAnalysis.EndpointClusters` with `Confidence: "low"` and `NormalizationFlags: ["single-sample"]`.
- `--min-samples=5`: endpoints with four or fewer samples drop from the spec; their samples files (from U3) still exist on disk so the audit trail is intact.
- `--include "/track/important"`: a URL that the existing analytics blocklist would normally demote returns from `scoreEntry` with positive score, becomes an API entry, and lands in the emitted spec.
- `--include "api.competitor.com"`: a host on the default blocklist is rescued; classifier returns it as API.
- `--include` and `--blocklist` both set, overlapping pattern: include wins.
- `--include` matches override the static-asset suffix demotion: a `.js` URL listed in include lands as API.
- Help golden reflects the new flags with the recommended-value note in the description text.

Verification: `printing-press browser-sniff --help` shows both new flags; default behavior on existing golden fixtures is unchanged; targeted goldens cover the new behavior; `scripts/golden.sh verify` clean.

---

## System-Wide Impact

- Phase 2 Free/Paid Tier Routing Enrichment (skills/printing-press/SKILL.md L1707 region) and Public Parameter Name Enrichment (L1637 region) gain per-endpoint signal. Tier routing accuracy on sites that mix anonymous and authenticated endpoints improves. The SKILL.md change is a note pointing at `endpoint.ObservedAuth` and `cluster.ObservedAuth`; the enrichment logic itself is not in scope.
- Absorb manifest gating (skills/printing-press/SKILL.md L1241 region) can read `TrafficAnalysis.EndpointClusters[].Confidence` to weight novel-feature suggestions and de-prioritize `low`-bucket endpoints. Plan emits the data; the absorb-side consumer is a separate change.
- Dogfood `reimplementation_check` (per AGENTS.md "Anti-reimplementation") can read `<spec-stem>-samples/` to ground a "this endpoint actually exists in the trace" assertion. Consumer is out of scope.
- Golden harness churn: every unit touches `testdata/golden/expected/...`. Land one unit per PR, refresh that unit's goldens, explain the diff in the PR body per AGENTS.md.
- `crowd-sniff` (internal/cli/crowd_sniff.go) calls `browsersniff.WriteSpec` directly. The `ObservedAuth` field on `spec.Endpoint` becomes available to crowd-sniff too; crowd-sniff would emit empty arrays (no observed traffic to derive from). The omitempty tag keeps the YAML clean.
- The `internal/spec` package gains a single field. Other consumers of `spec.Endpoint` (`internal/generator`, `internal/openapi`, ingestion paths) tolerate unknown fields and ignore the new one by default. No generator behavior changes until a future consumer wires `ObservedAuth` in.
- `internal/browsersniff` package gains roughly 300-500 LoC across four units plus one new file (`redact.go`). No external dependency added; everything uses the Go standard library + existing PP packages.
- Printed CLIs are not directly affected. They consume the spec; a new optional field on `spec.Endpoint` is invisible to them until a generator consumer is added.

## Risks & Mitigations

- Golden churn across all four units. Mitigation: one unit per PR, golden diffs explained in PR body.
- `spec.Endpoint` field addition is a public-ish surface change because the YAML format is the contract for vendor-spec inputs and printed-CLI generation. Mitigation: field is additive with `omitempty`; older readers ignore it; no version bump required. The contract is preserved.
- `--min-samples` default of 1 keeps behavior identical for existing callers. A future patch might raise the default to 2 once novel-feature ranking actually consumes confidence; that change is explicitly out of scope here.
- Samples directory atomicity uses `os.Rename` on directories, which is atomic on POSIX. On Windows the rename may fail if the target exists; PP is already POSIX-first per AGENTS.md tooling and CI matrix. Mitigation: document POSIX assumption near the writer; defer Windows support.
- Path-hash collisions at 8 hex chars are theoretically possible (~10^-9 birthday for typical endpoint counts). If two endpoints ever collide the failure is visible (two endpoints share a file, the second write overwrites the first). Mitigation if it ever fires: extend hash to 12 chars in a follow-up.
- `ObservedAuth` is observation-only; it must not be confused with a security scheme declaration. Mitigation: field name, doc-comment on the spec field, and the SKILL.md consumer note all reinforce the distinction.
- Redaction is best-effort. The default header/key/regex set covers common credentials but app-specific secrets may slip through. Mitigation: document this limitation in `browser-sniff-capture.md`; a follow-up unit could add a `--redact` flag to extend the defaults (deferred).

## Verification (whole plan)

- `go test ./internal/browsersniff/... ./internal/cli/... ./internal/spec/...` is green.
- `scripts/golden.sh verify` is green after each unit's golden refresh.
- `printing-press browser-sniff --har <existing-fixture>.har` produces: spec YAML where mixed-auth endpoints carry `observed_auth`; a `<spec-stem>-traffic-analysis.json` where each `EndpointCluster` has `sample_count`, `status_codes`, `normalization_flags`, `confidence`, `observed_auth`; a `<spec-stem>-samples/` directory with one redacted file per endpoint group; and `--min-samples=2` produces a spec with the long tail dropped while the traffic-analysis still lists every cluster.
- One end-to-end run of `/printing-press <api>` on a site with mixed anonymous + authenticated endpoints shows the tier-routing enrichment phase referencing per-endpoint auth surface.
