---
title: "fix: Wave 2 retro blockers — invalidateCache template + library backfill + destructive-auth dogfood skip"
type: fix
status: active
date: 2026-05-05
---

# fix: Wave 2 retro blockers — invalidateCache template + library backfill + destructive-auth dogfood skip

## Summary

Three PRs across two repos: (1) generator template fix in `client.go.tmpl` adding `invalidateCache()` and the `do()` success-branch hook so all future printed CLIs nuke the response cache after non-GET requests; (2) one-PR backfill on `mvanhorn/printing-press-library` applying the identical patch to every existing CLI's `internal/client/client.go` via inline regex (mirroring PR #213's shape); (3) live-dogfood scorer fix that classifies token-rotating endpoints as "destructive-at-auth" and skips them from `--live` runs unless `--allow-destructive` is set.

---

## Problem Frame

Two reproducible failure modes still ship in the canonical pipeline. **#603:** every printed CLI's HTTP client writes a 5-minute disk cache on GETs but never invalidates it on POST/PUT/PATCH/DELETE — humans running `<cli> create` immediately followed by `<cli> list` see the pre-mutation list. PR #521 fixed this for MCP via `NoCache=true`, but the human-CLI path is still uncovered. The cal-com retro caught it during a smoke test and patched the printed CLI directly (PR mvanhorn/printing-press-library#237 commit `d26a2862`); 30+ other library CLIs still ship the bug. **#602:** `printing-press dogfood --live` invokes endpoints like `POST /api-keys/refresh` as test #1 against APIs (Cal.com, Linear, Stripe-equivalent) where that rotates the runner's bearer — Cal.com cascade was 595/723 false 401s after a single self-rotation. Both are P1 from the Cal.com retro #597 series.

---

## Requirements

- R1. The generator template at `internal/generator/templates/client.go.tmpl` emits a `Client.invalidateCache()` method whose body wholesale-removes `c.cacheDir` (best-effort, ignores RemoveAll error).
- R2. The template's `do()` success branch calls `c.invalidateCache()` when `resp.StatusCode < 400` AND `method != http.MethodGet`. Invocation lives between `c.limiter.OnSuccess()` and the existing success-return.
- R3. Cache invalidation does **not** fire on dry-run, on responses with status >= 400, or on retry-loop intermediate failures — only on the final terminal success of a non-GET.
- R4. Existing GET-only flows are unchanged: GETs continue to read from and write to the cache; back-to-back GETs without intervening mutation still hit the cache (no extra API call).
- R5. Every existing library CLI under `mvanhorn/printing-press-library/library/<category>/<api-slug>/` whose `internal/client/client.go` contains a `writeCache` method (the asymmetry diagnostic) gains the same `invalidateCache` method body and the same `do()` hook, applied identically. CLIs without `writeCache` are skipped.
- R6. The library backfill ships as **one PR** to `mvanhorn/printing-press-library`; each affected CLI builds (`go build ./...`) post-patch.
- R7. `printing-press dogfood --live` classifies a command as destructive-at-auth using `cmd.Annotations["pp:endpoint"]` as the primary signal (substring match against `refresh`/`rotate`/`revoke`, e.g. `api-keys.keys-refresh` from a promoted command). When the annotation is absent (novel hand-built commands), fall back to Cobra-leaf-segment match against the same set. Commands annotated `mcp:read-only: true` are exempt regardless of name.
- R8. Skipped destructive endpoints emit four `LiveDogfoodTestResult` entries (one per `LiveDogfoodTestKind`) with `Status: LiveDogfoodStatusSkip` and `Reason: "destructive-at-auth"`; they don't increment `MatrixSize` (per the existing skip-classification contract).
- R9. A new `--allow-destructive` boolean flag on `printing-press dogfood` (defaulting to false) re-enables the skipped endpoints. When set, no destructive-at-auth skips are emitted; runs are identical to today's behavior on those commands.
- R10. CLIs without destructive endpoints produce identical `--live` output to today (no behavioral or accounting drift in the matrix).

---

## Scope Boundaries

- Other open retro WUs (#593, #595, #596, #598, #599, #600, #601) are out of scope. Wave 3+.
- Re-running `printing-press regen-merge` per CLI is rejected — user explicitly chose hand-patch over regen for the library backfill.
- Selective per-resource cache invalidation is out of scope. #603's body explicitly rejects it ("10x bigger change for marginal benefit; cache TTL is 5 minutes, losing it after a mutation costs at most one extra GET on the next read").
- No changes to the cache file format (`sha256(path,params)[0:8].json`) or 5-minute TTL.
- Non-auth destructive endpoints in #602 (DELETE on resources, batch operations) are out of scope per #602's own scope boundary. The classifier only catches endpoints whose effect is to rotate or revoke the bearer the runner is using.
- Backfilling test fixtures or hand-coded examples is not in scope; only live library CLIs (R5).
- No durable `scripts/` or `internal/patch/`-style tooling for the library backfill — the inline regex approach is one-shot, mirroring PR #213's precedent.

---

## Context & Research

### Relevant Code and Patterns

- `internal/generator/templates/client.go.tmpl:39-40` — `NoCache bool` and `cacheDir string` fields. Existing constructor uses already there from PR #521.
- `internal/generator/templates/client.go.tmpl:405-409` — `Client.writeCache` method. The existing companion to which `invalidateCache` is added.
- `internal/generator/templates/client.go.tmpl:445` — `Client.do` declaration.
- `internal/generator/templates/client.go.tmpl:675-679` — `do()` success branch (`if resp.StatusCode < 400 { c.limiter.OnSuccess(); return ... }`). Insertion point for the `invalidateCache()` call.
- `internal/pipeline/live_dogfood.go:35-42` — `LiveDogfoodOptions` struct. New `AllowDestructive bool` field flows through here.
- `internal/pipeline/live_dogfood.go:58-66` — `LiveDogfoodTestResult` struct (`Command, Kind, Args, Status, ExitCode, Reason, OutputSample`). The shape of every emitted matrix entry.
- `internal/pipeline/live_dogfood.go:80, :126-130` — `RunLiveDogfood` and the per-command loop that expands each Cobra leaf into 4 test entries via `runLiveDogfoodCommand`.
- `internal/pipeline/live_dogfood.go:584-585` — `runLiveDogfoodCommand` entry point. Natural insertion site for the destructive-classifier check (between `commandName := strings.Join(command.Path, " ")` and the help-probe).
- `internal/pipeline/live_dogfood.go:605-607, :617-618` — existing skip emission call sites (`help check failed`, `missing runnable example`). Pattern to mirror.
- `internal/pipeline/live_dogfood.go:824-831` — `skippedLiveDogfoodResult` constructor. Returns a four-field result with `Status: LiveDogfoodStatusSkip` plus the reason. Reuse directly.
- `internal/pipeline/live_dogfood.go:880-892` — `finalizeLiveDogfoodReport`. Skips don't increment `MatrixSize`; this protects the matrix-size floor at line 901 from being tanked by destructive-skip clusters.
- `internal/cli/dogfood.go:16-24, :82-90` — flag declaration block and flag-binding site. New `--allow-destructive` lands here, plumbed into `LiveDogfoodOptions` at `:37-43`.
- `mvanhorn/printing-press-library` PR #237 commit `d26a2862` — the cal-com printed-CLI patch. Reference shape for the library backfill: identical method body and hook.
- `mvanhorn/printing-press-library` PR #213 — precedent for the bulk backfill PR. 38 CLIs, single PR, inline `perl -i -pe`, mechanical-change-per-file body, expandable CLI list, `go build ./...` per-CLI verification.

### Institutional Learnings

- `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` — the canonical guidance for #603. Names the asymmetry diagnostic ("any client whose code base has a `writeCache()` but no `invalidateCache()`"), the wholesale-nuke rationale, and the open follow-up that this plan now addresses.
- `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md` (active, U4) — the source-of-truth verdict-gate logic that #602's classifier must integrate with. Quick-PASS = `Failed == 0 && Passed+Skipped >= min(5, MatrixSize) && MatrixSize >= 4`. Skips don't count toward MatrixSize (already true at `:880-892`); destructive-skip clusters won't depress the floor.
- `docs/plans/2026-05-04-005-fix-wave-1-retro-blockers-plan.md` — Wave 1 plan template. Same shape for this plan; Wave 1's doc-review feedback dropped Phased Delivery as ornamental, so it's omitted here too. Wave 1's lesson on phase5_gate drift applies: when changing scorer logic, audit every consumer of the marker — comment the gate site naming the source-of-truth runner code.
- `docs/plans/2026-05-04-004-test-live-dogfood-resolve-success-coverage-plan.md` — `internal/pipeline/` test conventions: per-test top-level `Test*` functions, testify, `t.TempDir()`, no `t.Run` nesting. Apply to U4 and U5 tests.
- `docs/plans/2026-04-18-001-feat-patch-library-clis-v2-plan.md` — alternate library-backfill precedent using AST injection via `dave/dst` (not regex). Considered and rejected for this plan: AST is the right shape when patches are field-level resource configuration that must be idempotent across regenerations. The cache-invalidate patch is a one-shot mechanical injection that maps cleanly to two regex anchors; AST tooling is overkill and adds maintenance debt.
- AGENTS.md — "Don't change the Printing Press for one CLI's edge case" applies inversely here: the cal-com patch was the printed-CLI fix; this plan correctly elevates it to the machine. AGENTS.md golden-harness rule applies to U2 (sync template-affected goldens re-baseline). Generator-reserved namespaces (`internal/cliutil/`, `internal/mcp/cobratree/`) are not touched.

### External References

None required. All decisions ground in the local design-pattern doc, active plans, and the two precedent PRs (#237 cal-com, #213 NoCache backfill).

---

## Key Technical Decisions

- **Three separate PRs across two repos: PR-A (cli-printing-press, #603 template); PR-B (printing-press-library, library backfill); PR-C (cli-printing-press, #602 scorer).** Ship in this order for review-batch sanity, but no technical blockers enforce sequence. PR-A's only effect on PR-B is "future generations get the fix automatically without backfill." PR-C is fully independent.
- **PR-B mechanism: inline `perl -i -pe` regex applied to two anchors per file, executed in the PR description.** Mirrors PR #213's shape exactly. The alternative — adding a durable `scripts/backfill-invalidate-cache.sh` artifact in `cli-printing-press` — invests in maintenance for a one-time backfill. Inline is cheaper, the patch is mechanical, and the PR body itself documents the regex for future readers. AST injection (precedent: patch-v2 plan) is rejected as overkill for two-anchor mechanical insertion.
- **PR-B discovery filter: presence of `writeCache` in `internal/client/client.go`.** This is the asymmetry diagnostic from `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md`. CLIs that ship without `writeCache` (proxy-envelope clients, minimal clients) aren't broken and shouldn't be patched. The discovery one-liner is `grep -l 'writeCache' library/*/*/internal/client/client.go`.
- **Destructive-at-auth classification reads `cmd.Annotations["pp:endpoint"]` as primary signal, with Cobra-leaf-path fallback.** The annotation is set by the generator on every endpoint-mirror command (per AGENTS.md "Endpoint mirrors: cmd.Annotations[\"pp:endpoint\"] = \"<resource>.<endpoint>\""); a value like `api-keys.keys-refresh` carries the destructive signal even when the command was promoted to a leaf with a non-matching name (`Use: "api-keys"`). Cal.com's `POST /api-keys/refresh` matches on annotation; novel hand-built commands without the annotation fall back to leaf-segment match. Commands with `mcp:read-only: true` (e.g. `craigslist catalog refresh`) are exempt — read-only commands cannot rotate auth regardless of name. Classifier match list intentionally limited to the three terms #602's body names (`refresh`/`rotate`/`revoke`); extension-by-discovery is the deferred-implementation hook for new patterns.
- **Skip emission for destructive endpoints reuses `skippedLiveDogfoodResult` directly.** Four entries per skipped command (one per `LiveDogfoodTestKind`: help, happy_path, json_fidelity, error_path), all with `Reason: "destructive-at-auth"`. No new emission shape; the existing skip envelope and reporter accounting handle it. `MatrixSize` invariant preserved.
- **`--allow-destructive` is a `BoolVar` on `dogfood`, defaulted false.** Threaded through `LiveDogfoodOptions.AllowDestructive` to the matrix builder. When true, the classifier short-circuits and emits no skips — runs are byte-identical to today's behavior on those commands. No new test mode or environment variable.
- **Cache-invalidation comment block in the template names the design pattern doc.** Per Wave 1's drift-prevention pattern: a one-line comment at the `do()` hook site says "see `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` for rationale." Future readers can reach the full guidance without reading the printed CLI's history.

---

## Open Questions

### Resolved During Planning

- **Why not regen-merge per CLI for the library backfill?** Regen-merge runs the full generator on each CLI's spec and merges into the existing tree. Per the cache-invalidate patch, only one method and one hook line need to change in each CLI's `client.go`. Regen-merge would touch every other generated file and burn time on equivalent-output diffs. User explicitly rejected at synthesis time.
- **Is the cache directory under any path `publish package`'s `CopyDir` walks?** No. The template at `client.go.tmpl:286` resolves `cacheDir` to `~/.cache/{{.Name}}-pp-cli/` via `os.UserHomeDir()` — outside the generated CLI's source tree. `os.RemoveAll(c.cacheDir)` cannot leak into publish.
- **Do we need a new "Errored" status for destructive-classified entries?** No. `LiveDogfoodStatusSkip` with a structured `Reason` field already covers the use case.
- **Annotation-primary vs Cobra-leaf-path-only classifier (U4).** The Cobra leaf path alone misses promoted commands like cal-com's `Use: "api-keys"` (the named example from #602). Generator already populates `cmd.Annotations["pp:endpoint"]` on every endpoint mirror; the classifier reads that first, falls back to leaf-segment matching for novel hand-built commands. Read-only commands are exempt via `mcp:read-only` annotation.
- **Discovery filter for PR-B.** Match `writeCache AND NOT invalidateCache` to exclude already-patched files (cal-com today; possibly more if pre-emptive patches shipped after PR #237). Re-applying the patch to an already-patched CLI would create a duplicate-method compile error.
- **`perl -0777 -i -pe` slurp mode, not `perl -i -pe` line mode.** The first patch anchor (insert multi-line method body after `writeCache`'s closing brace) requires a multi-line regex; line mode cannot reliably bound the match across function boundaries.
- **MCP cross-surface cache effect.** When an MCP-driven mutation completes, the human CLI's cache gets nuked too (shared `~/.cache/<api-slug>-pp-cli/`). Accepted: the human's cache IS now stale after the MCP mutation; this is correct behavior, consistent with the wholesale-nuke rationale.
- **R3 dry-run safety contractual.** Hook reads `if method != http.MethodGet && !c.DryRun { c.invalidateCache() }`. The `!c.DryRun` is structurally redundant today but defends against future refactors that move the dry-run check.
- **U2 symmetry assertion covers R1 AND R2.** Test asserts both method definition AND the call-site call inside `do()` — catches the regression where method exists but hook is dropped.

### Deferred to Implementation

- **Exact regex anchors for PR-B's `perl -0777 -i -pe` invocation.** The patch has two insertion points: (a) the new `invalidateCache` method after `writeCache`'s closing brace (multi-line bound), and (b) the `do()` hook between `c.limiter.OnSuccess()` and the success-return (single-line). Anchors land in the PR description, derived by reading 2-3 representative library CLIs and confirming patterns are stable. Defer because implementation pass needs to inspect actual library files.
- **Library CLI `client.go` formatting variance.** If any CLI deviates from the template's gofmt-stable shape (rare but possible), hand-patch in the same PR with a one-line note. Defer detection until the dry-run sweep.
- **Classifier match list extension (U4).** Match list intentionally limited to `{refresh, rotate, revoke}` per #602's stated scope. If implementation surfaces a real destructive pattern that doesn't match (e.g., a `cycle` or `regenerate` endpoint without the standard naming), extend the list and document the addition. Default is to NOT pre-extend — false-positive risk dominates.
- **Library repo `library/` directory layout.** Plan assumes two-level glob `library/<category>/<api-slug>/`. Verify on first sweep; adjust glob if the remote uses a flat layout.

---

## Implementation Units

- U1. **Add `invalidateCache()` method and `do()` hook to client template**

**Goal:** Generator emits `Client.invalidateCache()` and the `do()` success-branch call to it on every printed CLI's `internal/client/client.go`. Future generations inherit the fix automatically.

**Requirements:** R1, R2, R3, R4.

**Dependencies:** None.

**Files:**
- Modify: `internal/generator/templates/client.go.tmpl` (add method ~after `writeCache` at `:409`; add hook in `do()` success branch at `:675-679`)

**Approach:**
- After the closing brace of `writeCache` at `:409`, append the `invalidateCache` method: guard if `cacheDir == ""`, then `_ = os.RemoveAll(c.cacheDir)`. Best-effort — ignore the error per the design-pattern doc.
- In the `do()` success branch at `:675-679`, between `c.limiter.OnSuccess()` and the success-return, add `if method != http.MethodGet && !c.DryRun { c.invalidateCache() }`. Two-condition check: skip GETs (cache should be retained) and skip dry-run paths (no real mutation occurred). The `!c.DryRun` guard is structurally redundant today (template already short-circuits dry-run at `:553-556` before the retry loop), but its presence makes R3 contractual rather than incidental — defends against future refactors that move the dry-run check.
- Add a one-line comment at the hook citing `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` for rationale (Wave 1's drift-prevention pattern).
- The `do()` retry loop short-circuits to the success-return on the first `< 400` response, so the hook only fires on terminal success. No additional guard needed against intermediate retry success.
- **MCP cross-surface effect, accepted:** the cache directory `~/.cache/<api-slug>-pp-cli/` is shared between the human CLI binary and the MCP server (which has `c.NoCache = true` per PR #521). When an MCP-driven mutation completes, the human CLI's cache gets nuked too. This is correct behavior — the human's cached snapshot IS now stale after the MCP mutation — and is consistent with the design-pattern doc's wholesale-nuke rationale. No `!c.NoCache` guard.

**Patterns to follow:**
- `internal/generator/templates/client.go.tmpl:405-409` — `writeCache` shape: short, best-effort, ignores errors.
- The cal-com patch at `mvanhorn/printing-press-library` PR #237 commit `d26a2862` — exact body and hook location.

**Test scenarios:**
- *Happy path:* generate any spec; assert that the emitted `internal/client/client.go` contains `func (c *Client) invalidateCache()` and that `do()`'s success branch calls `c.invalidateCache()` guarded by `method != http.MethodGet` (assert via grep on the emitted bytes or via a generator test that compiles output).
- *Edge case:* generate a spec with proxy-envelope client pattern (if applicable); the same method emits — proxy-envelope clients still have `cacheDir` and `writeCache`, so they're not exempt.
- *Regression:* generate a representative spec and compare against an updated golden — the only diff vs. pre-patch is the new method body and the one-line hook in `do()`.

**Verification:**
- `go test ./internal/generator/... -count=1` passes.
- Grep on the emitted source of any test-fixture CLI confirms both insertion points are present and correct.

---

- U2. **Update template tests and re-baseline goldens**

**Goal:** Existing template tests cover the new method. Two `generate-*-api` goldens (printing-press-golden, tier-routing-golden) re-baseline with the additive change.

**Requirements:** R1, R2.

**Dependencies:** U1.

**Files:**
- Modify (re-baseline only): `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/client/client.go`
- Modify (re-baseline only): `testdata/golden/expected/generate-tier-routing-api/tier-routing-golden/internal/client/client.go`
- Modify (re-baseline only): `testdata/golden/expected/generate-golden-api-oauth2-cc/printing-press-oauth2-cc/internal/client/client.go`
- Add or extend a test in `internal/generator/` (test file location to be confirmed at implementation; likely a `client_template_test.go` or a generator-level test) covering the symmetry diagnostic — assert that emitted clients have BOTH the `invalidateCache` method definition AND a `c.invalidateCache()` call inside `do()`'s body. Method-presence alone is insufficient: a future refactor could keep the method but drop the call.

**Approach:**
- Run `scripts/golden.sh verify` to enumerate which goldens diverge. Three goldens ship `client.go` (the two `generate-*-api` cases plus `generate-golden-api-oauth2-cc/printing-press-oauth2-cc/`); other cases (catalog-list, dogfood-*, schema-traffic-analysis, verify-runtime-matrix) don't include client.go and won't change.
- Run `scripts/golden.sh update`, inspect the diff, confirm only `+` lines are the new method body + the one-line hook in `do()`. No `case` reordering or unrelated drift expected (Wave 1's schema sort fix already prevents that).
- Add a generator test (or extend an existing client-template test) to assert the symmetry diagnostic — `invalidateCache` method definition AND a `c.invalidateCache()` call in `do()` co-occur in every generation. Grep on emitted bytes for `func (c *Client) invalidateCache` and `c.invalidateCache()` (call form). The two-prong assertion catches both R1 (method exists) and R2 (hook calls it).

**Patterns to follow:**
- Wave 1 PR #608 (`fix(cli): route sync --json events to stdout`) — same shape: source change + golden re-baseline as separate commits in one PR.
- `docs/GOLDEN.md` — canonical update flow.

**Test scenarios:**
- *Happy path:* `scripts/golden.sh verify` returns 11/11 PASS after `update` + diff inspection.
- *Symmetry assertion (method-side):* the new generator test fails when the template ships `writeCache` without `invalidateCache` (introduce the bug locally, run the test, confirm fail; revert).
- *Symmetry assertion (call-side):* the test also fails when the template ships `invalidateCache` definition but no `c.invalidateCache()` call in `do()` (drop the hook line locally, confirm fail; revert).
- *Regression:* full `go test ./...` passes, no Go test regressions from the template change.

**Verification:**
- `scripts/golden.sh verify` clean.
- New symmetry test passes with current template; fails with each of two synthetic regressions (drop method body / drop call). Both signals captured in the PR description.

---

- U3. **Library backfill PR — patch every existing CLI's `client.go`**

**Goal:** One PR to `mvanhorn/printing-press-library` that adds `invalidateCache` and the `do()` hook to every CLI whose `client.go` ships `writeCache`. Each affected CLI builds post-patch.

**Requirements:** R5, R6.

**Dependencies:** None functionally (independent of U1 — the library CLIs don't auto-regenerate). Sequencing: ship after U1+U2 land for narrative cohesion, but no technical block.

**Files (in `mvanhorn/printing-press-library` repo, NOT this one):**
- Modify: every `library/<category>/<api-slug>/internal/client/client.go` matched by the discovery filter.

**Approach:**
- **Discovery sweep:** clone or update local `printing-press-library`. First, verify the layout matches `library/<category>/<api-slug>/...` (two-level glob); if the remote uses a flat `library/<api-slug>/` layout, adjust the glob accordingly. Then run the symmetric-asymmetry filter:
  ```
  grep -lZ 'writeCache' library/*/*/internal/client/client.go | xargs -0 grep -L 'invalidateCache'
  ```
  This excludes already-patched CLIs (notably cal-com, which received the patch in PR #237 commit `d26a2862` — re-applying would create a duplicate-method compile error). Cross-check the count against PR #213's 38; expect 37-40 candidates.
- **Mechanical patch via `perl -0777 -i -pe` (slurp mode, NOT line-mode `-pe`):** the first anchor inserts a multi-line method body, which line-mode regex cannot reliably match across `}` → blank-line → next-`func` boundaries. Slurp mode reads each file as one string so a multi-line regex like `func \(c \*Client\) writeCache[\s\S]+?\n\}\n` can bound the writeCache function and append after its closing brace. The second anchor (hook line in `do()`'s success branch) is single-line-insertable and works in either mode, but stay in slurp mode for consistency.
  Two anchors per file:
  1. After `writeCache`'s closing brace, insert the `invalidateCache` method body (4 lines).
  2. Between `c.limiter.OnSuccess()` and the success-return in `do()`, insert `if method != http.MethodGet && !c.DryRun { c.invalidateCache() }`.
- **Per-CLI build verification:** loop over patched CLIs, run `(cd library/<cat>/<api> && go build ./...)`. Any build failure halts the sweep — investigate (likely formatting drift on that CLI's client.go, or a CLI accidentally matched by both prongs of the filter).
- **PR description:** mirror PR #213's shape — Summary citing the upstream template fix (U1's PR), "Mechanical change per file" section showing the before/after, expandable CLI list, three-step verification (patch applied / `go build ./...` per CLI / sanity-check diff in 2 CLIs). Explicitly note cal-com is intentionally excluded because PR #237 already shipped the patch.
- **Skipped CLIs:** any CLI without `writeCache` is excluded by the filter (proxy-envelope minimal clients, if any). Any CLI WITH `writeCache` AND `invalidateCache` is also excluded (cal-com today; potentially others if more pre-emptive fixes shipped). Note both classes in the PR description.

**Patterns to follow:**
- `mvanhorn/printing-press-library` PR #213 — same shape, same PR-body structure, same verification rigor.

**Test scenarios:**
- *Happy path:* every CLI matched by the discovery filter gains exactly two diff hunks (method insertion + hook insertion); `git diff --name-only | wc -l` equals the expected CLI count.
- *Edge case:* a CLI with non-default `client.go` formatting (e.g., a hand-edit added an extra blank line, breaking the regex anchor) — fail loudly, hand-patch in the same PR, document.
- *Regression:* every patched CLI passes `go build ./...`; the (~5 or so) CLIs without `writeCache` are unmodified.

**Verification:**
- `git diff --name-only | wc -l` matches the discovery filter count.
- `for d in library/*/*/internal/client/; do (cd "$d/.." && go build ./...) || echo FAIL "$d"; done` returns no FAIL lines.
- Sanity diff inspection of 2 randomly chosen CLIs shows the expected two-hunk shape only.

---

- U4. **Destructive-at-auth classifier in live-dogfood matrix builder**

**Goal:** `runLiveDogfoodCommand` recognizes commands whose Cobra leaf path matches the destructive-at-auth signal and short-circuits to skip emission before any probe runs.

**Requirements:** R7, R8, R10.

**Dependencies:** None.

**Files:**
- Modify: `internal/pipeline/live_dogfood.go` (add classifier; integrate at `runLiveDogfoodCommand` entry, around `:584-585`)
- Modify: `internal/pipeline/live_dogfood_test.go` (new tests)

**Approach:**
- Add a small unexported `isDestructiveAtAuth(annotations map[string]string, commandPath []string) bool` helper. Returns true when ALL of:
  1. **Not read-only:** `annotations["mcp:read-only"] != "true"`. Read-only commands cannot rotate auth regardless of name (e.g., `craigslist catalog refresh` re-syncs a public catalog to the local store; `allrecipes auth refresh` is the auth read-only metadata refresh — both are annotated read-only).
  2. **Annotation primary signal (preferred):** `annotations["pp:endpoint"]` contains (case-insensitive substring) any of `refresh`, `rotate`, `revoke`. Catches `api-keys.keys-refresh`, `tokens.token-rotate`, etc., even when the command was promoted to a leaf with a non-matching `Use` (cal-com's `Use: "api-keys"` ships annotation `api-keys.keys-refresh` — current classifier MUST match this).
  3. **Cobra-leaf fallback (novel commands):** when `pp:endpoint` annotation is absent (hand-built novel command), check `command.Path` segments — any segment whose lowercase form contains `refresh`, `rotate`, or `revoke` qualifies. Substring (not exact) matching catches compound leaf names like `oauth-client-force-refresh`.
- The classifier needs both annotation source and leaf path — but `runLiveDogfoodCommand` today receives `(command liveDogfoodCommand, ctx resolveCtx)` where `liveDogfoodCommand` is `struct { Path []string; Help string }` (no annotations). **Plumbing change:** extend `liveDogfoodCommand` to carry `Annotations map[string]string` populated from `cmd.Annotations` at the `collectLiveDogfoodCommandPaths` site (`internal/pipeline/live_dogfood.go:569`). Forward-propagating an existing per-command field, not a struct redesign.
- In `runLiveDogfoodCommand` after computing `commandName := strings.Join(command.Path, " ")` (line ~584-585), check `isDestructiveAtAuth(command.Annotations, command.Path)`. If true AND `ctx.allowDestructive == false`, emit four `skippedLiveDogfoodResult(commandName, kind, "destructive-at-auth")` entries (one per `LiveDogfoodTestKind`) and return.
- The existing skip envelope (`Status: LiveDogfoodStatusSkip`, `Reason: "destructive-at-auth"`) handles the rest — `finalizeLiveDogfoodReport` excludes skips from `MatrixSize`, so the verdict-gate logic already handles destructive clusters correctly.
- Add a one-line comment at the classifier site naming the design issue (#602) and the source-of-truth (the runner is authoritative — no separate gate consumes this signal beyond the existing skip-with-reason path).
- Match list intentionally limited to the three terms #602's body names (`refresh`, `rotate`, `revoke`). Do NOT pre-add `destroy`, `auth-keys`, `api_keys`, etc. — extending the list when implementation surfaces a real miss is the deferred-question hook (per Open Questions). Pre-loading creates false-positive risk in the opposite direction.

**Patterns to follow:**
- `internal/pipeline/live_dogfood.go:605-607, :617-618` — existing skip-emission call sites. Mirror exactly.
- `docs/plans/2026-05-04-004-test-live-dogfood-resolve-success-coverage-plan.md` — test conventions: per-test top-level `Test*` functions, testify, `t.TempDir()`. Match exactly.

**Test scenarios:**
- *Happy path (annotation-primary, cal-com case):* `isDestructiveAtAuth(map[string]string{"pp:endpoint": "api-keys.keys-refresh"}, []string{"my-cli", "api-keys"})` returns true. This is the named example from #602; the leaf path has no `refresh` segment but the annotation does.
- *Happy path (annotation-primary, rotate):* annotation `tokens.token-rotate`, leaf `["my-cli", "tokens"]` → true.
- *Happy path (leaf fallback, novel command):* no annotation, leaf `["my-cli", "auth", "refresh"]` → true.
- *Happy path (compound-leaf fallback):* no annotation, leaf `["my-cli", "oauth-clients", "users", "oauth-client-force-refresh"]` → true (substring match on the compound segment).
- *Edge case (read-only exempt):* `isDestructiveAtAuth(map[string]string{"mcp:read-only": "true", "pp:endpoint": "catalog.refresh"}, []string{"my-cli", "catalog", "refresh"})` returns false. Verifies the craigslist `catalog refresh` and similar read-only-annotated commands are NOT skipped.
- *Edge case (negative, no destructive signal):* annotation `users.list-users`, leaf `["my-cli", "users", "list"]` → false.
- *Edge case (case-insensitive):* annotation `API-Keys.Refresh-Key`, leaf `["my-cli", "API-Keys"]` → true.
- *Edge case (annotation present but harmless):* annotation `users.list-active` (no `refresh`/`rotate`/`revoke` substring), leaf `["my-cli", "users"]` → false. Annotation absence vs presence-without-signal both correctly classify as non-destructive.
- *Integration:* given a fake Cobra tree with one destructive command (annotated `pp:endpoint: api-keys.keys-refresh`), one read-only `refresh` (annotated `mcp:read-only: true`), and one normal command, run `RunLiveDogfood` with `AllowDestructive: false`; assert: destructive emits 4 skip entries; read-only runs through the full probe matrix; normal runs through the full probe matrix.
- *Integration:* same fixture with `AllowDestructive: true`; assert all three run through the full probe matrix.
- *Regression:* a fake Cobra tree with no destructive commands produces `--live` output identical to current behavior (compared against a baseline matrix from a prior commit).

**Verification:**
- `go test ./internal/pipeline/ -run TestLiveDogfood -count=1` passes including new classifier and integration cases.
- Manual smoke: regenerate a CLI for an API with `/api-keys/refresh`; run `printing-press dogfood --live --level full`; assert the JSON output contains `status: "skip", reason: "destructive-at-auth"` for the refresh command and that subsequent tests don't 401-cascade.

---

- U5. **`--allow-destructive` flag wiring**

**Goal:** Operators can opt back into destructive-endpoint testing with a single flag. When set, U4's classifier short-circuits and the run is byte-identical to today's behavior.

**Requirements:** R9.

**Dependencies:** U4 (the classifier exists and reads `opts.AllowDestructive`).

**Files:**
- Modify: `internal/cli/dogfood.go` (declare `var allowDestructive bool` near `:16-24`; bind via `cmd.Flags().BoolVar` near `:82-90`; pipe into `pipeline.LiveDogfoodOptions{...}` at `:37-43`)
- Modify: `internal/pipeline/live_dogfood.go:35-42` (add `AllowDestructive bool` to `LiveDogfoodOptions`)
- Modify: `internal/cli/dogfood_test.go` (extend existing test to exercise the flag)

**Approach:**
- Standard flag-binding pattern from `internal/cli/dogfood.go:82-90`: `cmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Re-enable testing of endpoints classified as destructive-at-auth (path/annotation matches refresh/rotate/revoke). Default skips them to prevent runner-credential rotation.")`.
- The flag flows through `LiveDogfoodOptions` (struct at `internal/pipeline/live_dogfood.go:35-42`); add `AllowDestructive bool`, populate from `allowDestructive` at the call site at `internal/cli/dogfood.go:37-43`.
- **resolveCtx plumbing:** `runLiveDogfoodCommand` reads from `resolveCtx`, not `LiveDogfoodOptions` directly. Add `allowDestructive bool` to the `resolveCtx` struct (defined ~`internal/pipeline/live_dogfood.go:237-243`) and populate it at the conversion site (~`:118-124`) where `RunLiveDogfood` builds the resolveCtx from `opts`. U4's classifier reads `ctx.allowDestructive`. This matches the existing pattern of threading run-scoped state through resolveCtx so individual helpers don't need parameter cascades.

**Patterns to follow:**
- Existing flag-binding pattern at `internal/cli/dogfood.go:82-90` (`--level`, `--write-acceptance`, etc.). Same shape.
- `LiveDogfoodOptions` struct at `internal/pipeline/live_dogfood.go:35-42`.

**Test scenarios:**
- *Happy path (default off):* `printing-press dogfood --live` (no `--allow-destructive`) on a fixture with one destructive command emits 4 skip entries for that command.
- *Happy path (flag set):* `printing-press dogfood --live --allow-destructive` on the same fixture exercises the destructive command (no skips).
- *Help text:* `printing-press dogfood --help` shows the `--allow-destructive` flag with its description.
- *Regression:* CLIs without destructive endpoints produce identical output regardless of `--allow-destructive` value.

**Verification:**
- `go test ./internal/cli/ -run TestDogfood -count=1` passes including new flag cases.
- `printing-press dogfood --help | grep allow-destructive` shows the flag.
- The U4 integration test (which exercises both flag states) passes end-to-end.

---

## System-Wide Impact

- **Interaction graph:** U1+U2 affect every printed CLI's HTTP client behavior at runtime — read-after-write becomes correct; no other code path. U3 affects the same surface in already-published CLIs. U4+U5 affect only `printing-press dogfood --live` runs; no printed-CLI behavior change.
- **Error propagation:** `invalidateCache()` is best-effort and ignores `os.RemoveAll` errors — a cache-clear failure does NOT propagate to fail an otherwise-successful mutation. Per the design-pattern doc, the worst-case fallback is "one extra GET on the next read."
- **State lifecycle risks:** None for U4/U5 (read-only matrix-builder change). U1+U2+U3 wholesale-nuke the cache on mutation; this is intentional and the cache TTL was already 5 minutes, so the post-mutation read penalty is bounded.
- **API surface parity:** U1's template change applies symmetrically across `Get`/`Post`/`Put`/`Patch`/`Delete` wrappers (all funnel through `do()`); no per-method exemption. The MCP path (PR #521's `NoCache=true`) is unaffected — those clients skip the cache entirely; the new invalidation hook is dead code there but harmless.
- **Integration coverage:** U3 (library backfill) is the integration test for U1 — the same patch validated on the live cal-com CLI is what U1 emits and U3 backfills. A build sweep across every patched CLI catches generator-template-vs-existing-CLI drift.
- **Unchanged invariants:** Cache file format (`sha256(path,params)[0:8].json`), 5-minute TTL, the existing `NoCache` MCP path, the `writeCache` write-on-GET behavior, every dogfood matrix entry shape that isn't classified as destructive-at-auth.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Library CLIs with non-default `client.go` formatting break the `perl -0777 -i -pe` regex anchors. | Discovery sweep includes a dry-run pass — apply patch to a temp copy, diff, and confirm two-hunk shape per CLI before committing. Hand-patch any outliers in the same PR with a one-line note. |
| Annotation-primary classifier (U4) misses a destructive endpoint that lacks the `pp:endpoint` annotation AND has a non-matching leaf name (e.g., a hand-built command named `cycle-credentials` calling a token-cycle endpoint). | Match list extended when discovered. False negatives surface as a 401 cascade in the operator's run; recoverable. False positives (the worse failure mode) are bounded by the read-only annotation exemption. |
| Operator sets `--allow-destructive` in CI accidentally and triggers runner-credential rotation. | Default is false. Help text explicitly names the rotation hazard. The flag is opt-in only; if a CI pipeline sets it intentionally, the operator owns the choice. |
| Remote `printing-press-library` repo glob layout (`library/<category>/<api-slug>/`) differs from local layout. | Plan's discovery sweep explicitly verifies the glob matches the expected ~37-40 file count BEFORE running the patch. A `wc -l` mismatch with PR #213's 38 is the canary — investigate before proceeding. |

---

## Documentation / Operational Notes

- **U1 PR description** must cite `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md` for the rationale and link the cal-com reference patch (PR mvanhorn/printing-press-library#237 commit `d26a2862`). The asymmetry diagnostic should be the lead — reviewers see why a `writeCache`-without-`invalidateCache` template was a latent bug, not just an oversight.
- **U3 PR description** must mirror PR #213's structure: Summary citing U1's upstream template fix; "Mechanical change per file" with the literal regex; expandable CLI list; three-step verification (patch / `go build ./...` per CLI / sanity diff in 2 CLIs).
- **U4 PR description** must cite #602 directly and explain the choice of Cobra-leaf-path classification over HTTP-path classification (the matrix builder doesn't have spec-level path mapping in scope). Document the classifier match list verbatim so future additions are easy to discover.
- **Per AGENTS.md commit-style:** all three PRs are `fix(cli):` (this repo) or `fix(library):` (printing-press-library). PR-A and PR-C carry tests; PR-B is a backfill (test-by-build, no new Go tests).
- **No release-please bump narrative needed.** All `fix(cli):` changes roll into the next patch release. PR-B is in a separate repo with its own release cadence.

---

## Sources & References

- Wave 2 retro WUs: [#603](https://github.com/mvanhorn/cli-printing-press/issues/603), [#602](https://github.com/mvanhorn/cli-printing-press/issues/602)
- Parent retro: [#597](https://github.com/mvanhorn/cli-printing-press/issues/597) (Cal.com)
- Cal.com printed-CLI reference patch: [printing-press-library#237 commit `d26a2862`](https://github.com/mvanhorn/printing-press-library/pull/237/commits/d26a2862)
- Library-backfill precedent: [printing-press-library#213](https://github.com/mvanhorn/printing-press-library/pull/213) (`fix(library): backfill MCP NoCache=true to 38 CLIs`)
- Upstream MCP-side fix: [#521](https://github.com/mvanhorn/cli-printing-press/pull/521) (template-side `NoCache=true` for MCP)
- Design-pattern doc: `docs/solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md`
- Wave 1 plan (template + drift-prevention pattern): `docs/plans/2026-05-04-005-fix-wave-1-retro-blockers-plan.md`
- Live-dogfood verdict-gate source-of-truth: `docs/plans/2026-05-04-003-fix-live-dogfood-matrix-accuracy-plan.md`
- `internal/pipeline/` test conventions: `docs/plans/2026-05-04-004-test-live-dogfood-resolve-success-coverage-plan.md`
- Code: `internal/generator/templates/client.go.tmpl`, `internal/pipeline/live_dogfood.go`, `internal/cli/dogfood.go`
- Convention docs: `AGENTS.md`, `docs/GOLDEN.md`
