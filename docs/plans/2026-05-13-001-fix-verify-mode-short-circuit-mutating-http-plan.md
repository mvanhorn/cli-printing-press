---
title: "fix(cli): gate mutating HTTP verbs on PRINTING_PRESS_VERIFY in generated clients"
type: fix
status: active
date: 2026-05-13
deepened: 2026-05-13
depth: deep
---

# Plan: Verify-Mode Short-Circuit on Mutating HTTP Verbs

## Summary

Add a verb-gated short-circuit in `internal/generator/templates/client.go.tmpl` so generated CLIs return a synthetic `{"status":"noop","reason":"verify_short_circuit",...}` envelope for DELETE/POST/PUT/PATCH whenever `PRINTING_PRESS_VERIFY=1` is set, unless a new `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` opt-in is also set. `pipeline.RunVerify` sets both envs in mock mode so its existing httptest flow keeps exercising the real wire path; every other consumer (agents, narrative verification, ad-hoc operators) gets a safe no-op.

---

## Problem Frame

Agent-readiness review of the matrix-22cc2fd9 smoke pipeline (`.runstate/matrix-22cc2fd9/runs/20260513T220622Z-49578526/pipeline/agent-readiness.md`, finding B1) confirmed that `PRINTING_PRESS_VERIFY=1 ./petstore-pp-cli.exe pet delete 42` still attempts a real network call. Only handwritten novel commands (`auth setup --launch`) honor `cliutil.IsVerifyEnv()` today; generated endpoint-mirror handlers route through `Client.do()` (`internal/generator/templates/client.go.tmpl` around line 713), which has no verify-mode gate.

The AGENTS.md "Side-effect commands" rule (line 39) names this as the floor: defense-in-depth that catches anything the verifier's heuristic classifier misses. The cliutil docstring scopes it narrowly to "open browser tabs, send notifications, dial out to OS handlers" — that scoping is the source of ambiguity. Mutating HTTP verbs are arguably the canonical visible action on a remote system, and any consumer applying AGENTS.md strictly (agent-readiness reviewers, security audits, downstream skill authors) will flag the gap.

This plan extends the rule down to the transport layer so the contract is unambiguous: under verify mode, generated CLIs do not issue mutating HTTP verbs (DELETE/POST/PUT/PATCH) unless the verifier explicitly opts in. Non-HTTP mutation surfaces (local store writes, file outputs, future RPC) remain governed by their existing rules — see Scope Boundaries for the explicit non-goals.

---

## Requirements

| R-ID | Requirement |
|---|---|
| R1 | Generated CLIs short-circuit DELETE/POST/PUT/PATCH when `PRINTING_PRESS_VERIFY=1` is set and `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` is not set. Short-circuit returns a synthetic structured envelope; no network call is issued. |
| R2 | `printing-press verify` (mock mode) continues to validate request paths, headers, dry-run output, exit codes, and command surface at 100% pass rate against the httptest server. The mock server still receives mutating requests because verify opts back in via `PRINTING_PRESS_VERIFY_LIVE_HTTP=1`. |
| R3 | `printing-press dogfood --live`, `scorecard --live-check`, and any other live verification path must NOT short-circuit. These paths do not set `PRINTING_PRESS_VERIFY=1`. |
| R4 | The `cliutil.IsVerifyEnv()` contract (existing) is unchanged. The new `cliutil.IsVerifyLiveHTTPEnv()` helper is added in the generator-reserved namespace. |
| R5 | AGENTS.md "Side-effect commands" section and the `cliutil_verifyenv.go.tmpl` docstring are updated to reflect that the transport-layer gate is part of the rule (pointer-rot rule). |
| R6 | Goldens regenerate cleanly; the diff is intentional and explained. |
| R7 | An emitted printed-CLI test (template) verifies the short-circuit in every regenerated CLI's own test suite. |
| R8 | A generator-level test pins the template contract so a future template edit cannot silently regress the short-circuit. |
| R9 | regen-merge classifies the new template block correctly for users with hand-edited `client.go`; no TEMPLATED-VALUE-DRIFT confusion that silently drops hand-edits. |
| R10 | Every published library CLI receives the short-circuit gate via a documented, reproducible regen-merge path. (U8 captures the HOW; this R-ID captures the WHAT — that the rollout actually reaches every CLI.) |

---

## Key Technical Decisions

### KTD1. Resolution choice: verb-gated short-circuit + opt-out env (Option B)

The brief listed four candidates. Decision: **B** (verb-gated short-circuit with `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` opt-out).

| Option | Why rejected (or chosen) |
|---|---|
| A — base-URL heuristic ("short-circuit only when BaseURL doesn't look like localhost httptest") | Fragile. APIs with legitimate localhost endpoints exist; verify's httptest URL shape isn't a public contract. Leaks an implementation detail of verify (mock-server URL pattern) into every generated client. Reject. |
| **B — verb-gated + explicit opt-in env** (chosen) | Smallest blast radius (one template change + one runtime line). Clearest semantics: "mutating verbs are special in verify mode; verify itself opts in to dial." Mirrors the [dry-run-default-for-mutator-probes-in-test-harnesses](../solutions/design-patterns/dry-run-default-for-mutator-probes-in-test-harnesses-2026-05-05.md) two-pronged gate pattern. Easy to test (set both envs). Composes cleanly with `--dry-run`, with the existing client_credentials short-circuit, and with future verify-mode escape valves. |
| C — handler-level short-circuit | High blast radius — every endpoint handler template gets a check. More verbose generated code per CLI. Doesn't compose with the existing client-level mutation discriminator (the cache-invalidation block at `client.go.tmpl`). Reject. |
| D — tighten AGENTS.md only (no code change) | Resolves the rule-interpretation ambiguity but leaves the actual readiness gap unsolved. Any external auditor applying the rule strictly still flags the same thing. Doesn't compound (per AGENTS.md "Default to machine changes"). Reject. |

### KTD2. New helper in `cliutil`, not inline

Add `cliutil.IsVerifyLiveHTTPEnv()` symmetric with `IsVerifyEnv()`. AGENTS.md "Generator-reserved namespaces" mandates `cliutil` for cross-template helpers, and centralizing the env-var literal in one place avoids drift if the name ever changes. The new helper lives in `cliutil_verifyenv.go.tmpl`.

**Asymmetric semantics, intentionally.** `IsVerifyLiveHTTPEnv()` in isolation has no behavioral meaning — the gate condition only checks it when `IsVerifyEnv()` is also true. `LIVE_HTTP=1` alone (with `VERIFY` unset) produces normal operation, not a sandbox. This asymmetry is intentional: `LIVE_HTTP` is the opt-out from the verify-mode gate, not an independent activation. Documenting it here so an external consumer who sees `LIVE_HTTP=1` in an environment does not infer sandbox semantics.

### KTD3. Synthetic envelope shape

Return a structured noop envelope, not an empty `{}` or raw 2xx. The envelope leads with a namespace-reserved boolean sentinel that no real API would emit, so downstream consumers (`validate-narrative --full-examples`, agent inspections, future verify-mode assertions) key on one obvious field instead of trying to interpret common low-entropy literals like `status:"noop"` (which legitimate APIs sometimes emit — Stripe idempotency replays, Kubernetes NoChange, queue-dedup APIs):

```json
{
  "__pp_verify_synthetic__": true,
  "status": "noop",
  "reason": "verify_short_circuit",
  "method": "DELETE",
  "path": "/pet/42"
}
```

The double-underscore prefix is a reserved-namespace convention — `__pp_*` belongs to Printing Press and no upstream API spec is expected to use it. Downstream consumers should key on `__pp_verify_synthetic__ == true`. The remaining fields (`status`, `reason`, `method`, `path`) are diagnostic prose, useful for human / agent inspection but not the primary classifier. Borrowing the envelope-around-no-op shape from [generated-cli-idempotent-noops-and-export-validation](../solutions/logic-errors/generated-cli-idempotent-noops-and-export-validation-2026-05-05.md). Status code returned alongside the body: 200 (a synthetic 204 for DELETE would force callers to special-case empty-body parsing).

**Outer envelope wrapping note.** The generated endpoint handler in `internal/generator/templates/command_endpoint.go.tmpl` (around lines 650-707) wraps this synthetic body as the `data` field inside its own `{action, resource, path, status:200, success:true, data:{...}}` presentation envelope. The outer envelope reports `success:true`. Operator diagnosis under short-circuit therefore keys on the **inner** `reason:"verify_short_circuit"` literal, not on the outer success flag. This is an accepted tradeoff (the inner envelope is the diagnostic anchor) — but it informs the U9 `doctor` line that surfaces verify-env state explicitly so operators don't have to read a response body to detect they're in verify mode.

### KTD4. Short-circuit location: collocated with existing mutation discriminator in `Client.do()`

The cache-invalidation block already gates on `method != http.MethodGet && !c.DryRun` (per [http-client-cache-invalidate-on-mutation](../solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md)). Add the short-circuit as a sibling check at the top of `do()` so all mutation-aware logic lives in one place. The short-circuit fires BEFORE the request is constructed, so no URL building, no auth header minting, no cache key generation runs.

**Explicit cache-state guarantee.** Because the short-circuit returns BEFORE the success branch in `do()` reaches the `invalidateCache()` call (`client.go.tmpl` around line 996), the cache is **correctly NOT invalidated** under short-circuit. Cache state stays consistent with the un-mutated remote: a subsequent GET sees the still-present resource. The retry middleware (around lines 850-1040) sits below the short-circuit too, so no retry fires on the synthetic envelope. Inline code comment in U2's Approach makes this explicit so a future reader doesn't try to "fix" the missing cache call.

### KTD5. MCP tool annotations unchanged

`destructiveHint: true` on a DELETE tool stays true. Per [mcp-sql-search-readonly-bypass](../solutions/security-issues/mcp-sql-search-readonly-bypass-2026-05-08.md), wrong annotations are worse than missing ones. The short-circuit changes runtime dial-out, not the tool's advertised semantics — when `LIVE_HTTP=1` it really is destructive.

### KTD6. AGENTS.md update lands in the same PR (pointer-rot rule)

The AGENTS.md "Side-effect commands" inline trigger sentence at line 42 will be updated to add the transport-layer gate. The `cliutil_verifyenv.go.tmpl` docstring widens to include "mutating HTTP verbs."

### KTD7. Public library rollout is a tier-prioritized per-CLI sweep

There is no single library-wide regen command (`regen-merge` is per-CLI). Per [cross-repo-coordination-with-printing-press-library](../solutions/best-practices/cross-repo-coordination-with-printing-press-library-2026-05-06.md), coordinate landing order: this repo's change first, then a sweep PR in `printing-press-library` that regens tier:official CLIs first, then tier:community.

---

## High-Level Technical Design

The change touches three surfaces. Directional sketch — not implementation specification.

```mermaid
flowchart TD
    A[Agent or operator invokes] -->|PRINTING_PRESS_VERIFY=1| B{Client.do method?}
    B -->|GET| C[Real request path<br/>unchanged]
    B -->|DELETE/POST/PUT/PATCH| D{LIVE_HTTP=1 set?}
    D -->|Yes - verify's mock-mode opt-in| E[Real request path<br/>hits httptest]
    D -->|No - default safe| F[Short-circuit:<br/>synthetic noop envelope]
    F --> G[Return 200 +<br/>{status:noop, reason:verify_short_circuit}]
    E --> H[Real do path:<br/>buildURL, authHeader, retry, cache]
    C --> H
```

Behavior matrix (inputs to `Client.do()`):

| `PRINTING_PRESS_VERIFY` | `PRINTING_PRESS_VERIFY_LIVE_HTTP` | Verb | Behavior |
|---|---|---|---|
| unset | unset | (any) | normal — real request |
| unset | `1` | (any) | normal — real request (LIVE_HTTP alone has no behavioral effect; sandbox semantics require BOTH vars) |
| `1` | unset | GET | normal — real request |
| `1` | unset | DELETE/POST/PUT/PATCH | **short-circuit** — synthetic envelope |
| `1` | `1` | (any) | normal — real request (verify mock-mode path) |

Sketch of the short-circuit shape (directional — not implementation):

```go
// Inside Client.do(), as the first block after parameter normalization:
if isMutatingVerb(method) && cliutil.IsVerifyEnv() && !cliutil.IsVerifyLiveHTTPEnv() {
    // Return synthetic envelope; do not dial, do not mint auth, do not touch cache.
    return verifyShortCircuitEnvelope(method, path), http.StatusOK, nil
}
```

`isMutatingVerb` is a small helper in `client.go.tmpl`'s emitted code (one-line switch over the four verbs). `verifyShortCircuitEnvelope` returns a `json.RawMessage` shaped per KTD3. Both helpers can be local to `client.go` to keep `cliutil` minimal.

---

## Implementation Units

### U1. Add `cliutil.IsVerifyLiveHTTPEnv()` helper

**Goal:** Introduce the new env-var literal in the generator-reserved namespace, symmetric with `IsVerifyEnv()`.

**Requirements:** R4

**Dependencies:** none

**Files:**
- `internal/generator/templates/cliutil_verifyenv.go.tmpl` (modify — add constant + function; widen docstring)
- `internal/generator/cliutil_verifyenv_test.go` (modify or create — unit test for the new helper)

**Approach:**
- Add `VerifyLiveHTTPEnvVar = "PRINTING_PRESS_VERIFY_LIVE_HTTP"` constant.
- Add `IsVerifyLiveHTTPEnv() bool` returning `os.Getenv(VerifyLiveHTTPEnvVar) == "1"`.
- Widen the file-level docstring to mention "mutating HTTP verbs in generated clients" as a use case for the verify-env contract. The body of `IsVerifyEnv()`'s docstring stays unchanged.

**Patterns to follow:** existing `IsVerifyEnv()` shape in the same file.

**Test scenarios:**
- Helper returns true when env is exactly `"1"`.
- Helper returns false when env is unset, empty, `"0"`, `"true"`, or any other non-`"1"` value.
- Asymmetry pin: when `PRINTING_PRESS_VERIFY` is unset but `PRINTING_PRESS_VERIFY_LIVE_HTTP=1`, the gate condition (`isMutatingVerb && IsVerifyEnv() && !IsVerifyLiveHTTPEnv()`) evaluates false; no short-circuit fires for any verb. (Asserts the LIVE_HTTP-alone case produces normal operation, per the KTD2 asymmetric-semantics note.)

**Verification:** `go test ./internal/generator/...` for the new unit test; existing tests still pass.

---

### U2. Add verb-gated short-circuit in `Client.do()`

**Goal:** Generated CLIs short-circuit mutating verbs under verify mode.

**Requirements:** R1, R3, R4

**Dependencies:** U1

**Files:**
- `internal/generator/templates/client.go.tmpl` (modify — add short-circuit block at top of `do()`, plus `isMutatingVerb` and `verifyShortCircuitEnvelope` helpers)
- `internal/generator/templates/client_test.go.tmpl` (modify — emitted test, see U5)

**Approach:**
- At the top of `do()` (before EndpointTemplateVars resolution), check `isMutatingVerb(method) && cliutil.IsVerifyEnv() && !cliutil.IsVerifyLiveHTTPEnv()`. When true, build and return the synthetic envelope.
- `isMutatingVerb`: switch over `"DELETE","POST","PUT","PATCH"`. Case-sensitive (`Client.do` already receives uppercase methods from the generated wrappers).
- `verifyShortCircuitEnvelope(method, path string) json.RawMessage`: marshal the `{"status":"noop","reason":"verify_short_circuit","method":<METHOD>,"path":<PATH>}` shape. Return value documented via a code comment as obviously synthetic.
- Both helpers live in the template's package-level region (file body) alongside other small utilities.
- Place the check BEFORE auth header minting so the existing `authHeader()` client_credentials short-circuit doesn't fire unnecessarily during verify-mode mutations.
- Place the check BEFORE the success-branch cache-invalidation block at `client.go.tmpl:~996`. Add an inline code comment at the short-circuit: `// No cache invalidation — no remote state changed.` Code-as-documentation per AGENTS.md hygiene rules; prevents a future reader from "fixing" the missing `invalidateCache()` call.

**Execution note:** Test-first for the short-circuit branch. Add the generator test (U4) before the template change so the failing-test signal is recorded before the green pass.

**Technical design** (directional):

```go
// Helper, file-scoped:
func isMutatingVerb(method string) bool {
    switch method {
    case "DELETE", "POST", "PUT", "PATCH":
        return true
    }
    return false
}

// Inside Client.do, as the first non-trivial check:
if isMutatingVerb(method) && cliutil.IsVerifyEnv() && !cliutil.IsVerifyLiveHTTPEnv() {
    body := verifyShortCircuitEnvelope(method, path)
    return body, http.StatusOK, nil
}
```

**Patterns to follow:**
- The existing client_credentials short-circuit at `client.go.tmpl` line 1307 (inside `authHeader()`) — synthetic-value-injection precedent.
- The cache-invalidation `method != http.MethodGet && !c.DryRun` block — collocate the new mutation-aware check nearby.

**Test scenarios:**
- *Covers R1.* `PRINTING_PRESS_VERIFY=1`, no `LIVE_HTTP`, method `DELETE`: returns synthetic envelope with `__pp_verify_synthetic__: true`, `status:"noop"`, `reason:"verify_short_circuit"`, and the request method/path echoed back; no HTTP request is issued (verify via injected mock transport).
- *Covers R1.* Same shape for `POST`, `PUT`, `PATCH`.
- `PRINTING_PRESS_VERIFY=1`, no `LIVE_HTTP`, method `GET`: real request path is exercised; no short-circuit.
- `PRINTING_PRESS_VERIFY=1` AND `PRINTING_PRESS_VERIFY_LIVE_HTTP=1`, method `DELETE`: real request path is exercised (the verify mock-mode contract).
- `PRINTING_PRESS_VERIFY` unset, method `DELETE`: real request path is exercised (the operator's live path).
- Envelope is valid JSON with the five expected keys (`__pp_verify_synthetic__`, `status`, `reason`, `method`, `path`) and the exact literal values for the sentinel (`true`) and `reason` (`"verify_short_circuit"`).
- Short-circuit returns before `authHeader()` runs (assert via side-channel: a deliberately broken auth config does NOT produce an auth error in short-circuit mode).

**Verification:** Generator-level tests pass; emitted-CLI tests (U5) pass; goldens regenerate with the expected diff (see Verification Strategy).

---

### U3. Make `pipeline.RunVerify` set the live-HTTP opt-in

**Goal:** U3 sets `LIVE_HTTP=1` in mock mode; `narrativecheck.go:238` does the same parallel to U3. Verify mock-mode and narrative full-example runners both keep exercising the real wire path.

**Requirements:** R2

**Dependencies:** U1

**Files:**
- `internal/pipeline/runtime.go` (modify — add `env = append(env, "PRINTING_PRESS_VERIFY_LIVE_HTTP=1")` in `buildEnv()` next to line 265)
- `internal/pipeline/runtime_test.go` (modify — test that mock-mode env contains both verify vars)
- `internal/narrativecheck/narrativecheck.go` (modify — line 238: append `LIVE_HTTP=1` alongside the existing `PRINTING_PRESS_VERIFY=1`)

**Approach:**
- Inside the `if report.Mode == "mock"` branch of `buildEnv()`, after the existing `PRINTING_PRESS_VERIFY=1` append, add the `LIVE_HTTP=1` append. Add a comment explaining why: verify owns the mock httptest server and needs the real wire path to assert against.
- Parallel one-line change in `narrativecheck.go:238`: the runner currently does `cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1")`; append `"PRINTING_PRESS_VERIFY_LIVE_HTTP=1"` alongside so narrative full-example subprocesses also continue to hit the real wire path. Without this, every narrative recipe invoking a mutating verb receives the synthetic envelope and any body-shape assertion silently breaks.
- All three changes are one-line additions with one-line comments. The rest of the surrounding logic is unchanged.

**Patterns to follow:** existing `env = append(env, ...)` calls at runtime.go lines 246-266; the parallel `cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1")` shape at narrativecheck.go:238.

**Test scenarios:**
- *Covers R2.* `pipeline.RunVerify` mock-mode subprocess env contains both `PRINTING_PRESS_VERIFY=1` and `PRINTING_PRESS_VERIFY_LIVE_HTTP=1`.
- *Covers R3.* `pipeline.RunVerify` live-mode subprocess env contains NEITHER var.
- *Covers R2.* `narrativecheck` full-example subprocess env contains both `PRINTING_PRESS_VERIFY=1` and `PRINTING_PRESS_VERIFY_LIVE_HTTP=1`.
- **Credential-substitution integration leg:** with `pipeline.RunVerify` in mock mode and the CLI's auth config seeded with a real-looking bearer token (e.g., `production-token-do-not-leak`), the httptest mock server records that the incoming `Authorization` header is `Bearer mock-token-for-testing`, NOT the production literal. Pins the existing `buildEnv` substitution contract at the integration boundary so a future refactor of mock-mode env construction can't silently leak credentials into mock-server logs.
- End-to-end: running the existing `TestRunVerify_MockMode_AllPassPercent` (or equivalent) against a CLI built from the updated template still produces 100% pass rate. (This guards against accidentally breaking the mock-mode contract.)

**Verification:** `go test ./internal/pipeline/... ./internal/narrativecheck/...` passes; manual run of `printing-press verify --dir <smoke> --spec <spec> --fix --json` still reports `mode=mock` and 100% pass rate; manual run of `printing-press validate-narrative --full-examples` still passes against existing narrative fixtures.

---

### U4. Generator-level test pinning the template contract

**Goal:** Future edits to `client.go.tmpl` cannot silently regress the short-circuit.

**Requirements:** R8

**Dependencies:** U2

**Files:**
- `internal/generator/client_verify_short_circuit_test.go` (create)

**Approach:**
- Test renders `client.go.tmpl` against a small fixture APISpec and asserts the emitted Go source contains:
  - The `isMutatingVerb` helper.
  - The short-circuit check using both `cliutil.IsVerifyEnv()` and `cliutil.IsVerifyLiveHTTPEnv()`.
  - The synthetic envelope construction.
- Plain text-content assertions (no need to compile or execute the emitted code; that's covered by U5 and the goldens).

**Patterns to follow:** existing generator tests in `internal/generator/*_test.go` that assert on template output.

**Test scenarios:**
- *Covers R8.* Emitted `client.go` contains `isMutatingVerb` definition.
- Emitted `client.go` contains both env-var helper calls in the `do()` body.
- Emitted `client.go` contains the literal `"verify_short_circuit"` reason string and the `__pp_verify_synthetic__` sentinel field name.
- Test fails if the short-circuit block is deleted or any of the three conditions is removed.

**Verification:** `go test ./internal/generator/ -run TestClient_VerifyShortCircuit` passes.

---

### U5. Emitted printed-CLI test (template)

**Goal:** Every regenerated CLI's own test suite verifies the short-circuit in its own client.

**Requirements:** R7

**Dependencies:** U2

**Files:**
- `internal/generator/templates/client_verify_short_circuit_test.go.tmpl` (create — emits into `<cli>/internal/client/client_verify_short_circuit_test.go`)
- `internal/generator/generator.go` (modify — register in the `singleFiles` map at line ~1417, adjacent to `client_test.go.tmpl`. Do NOT register in the `mcpFiles` map at line ~1683; this test is part of the printed CLI's client package, not the MCP surface.)

**Approach:**
- The emitted test injects a recording-mock `http.RoundTripper` into the `Client`, calls `client.do("DELETE", "/test", nil, nil, nil)` with `PRINTING_PRESS_VERIFY=1` set via `t.Setenv`, and asserts:
  - The RoundTripper was never called (no network attempt).
  - The returned `json.RawMessage` parses to a struct with `__pp_verify_synthetic__: true`, `status:"noop"`, and `reason:"verify_short_circuit"`.
- Second subtest with `t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")`: assert the RoundTripper WAS called (verify mock-mode path).
- Third subtest with no env: assert real path.

**Patterns to follow:** existing emitted test templates (e.g., `internal/generator/templates/mcp_tools_test.go.tmpl` — same shape, same emission flow).

**Test scenarios** (the emitted test, not this template's own test):
- *Covers R1.* DELETE under verify-only → no network call, synthetic envelope returned.
- *Covers R2.* DELETE under verify + LIVE_HTTP → network call attempted.
- DELETE with no env → network call attempted.
- GET under verify-only → network call attempted (control case proving the gate is verb-specific).

**Verification:** After regen, every printed CLI's `go test ./internal/client/...` includes and passes the new test.

---

### U6. Update AGENTS.md and `cliutil_verifyenv.go.tmpl` docstring

**Goal:** Documentation matches the new transport-layer gate (pointer-rot rule).

**Requirements:** R5

**Dependencies:** U1, U2

**Files:**
- `AGENTS.md` (modify — "Side-effect commands" section, inline trigger sentence around line 42)
- `internal/generator/templates/cliutil_verifyenv.go.tmpl` (modify — widen file-level docstring; partially done in U1)

**Approach:**
- AGENTS.md: extend the side-effect-commands bullet list to explicitly mention "mutating HTTP verbs in generated endpoint-mirror commands are gated at the transport layer; `printing-press verify` opts back in via `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` for its mock-mode httptest flow."
- Docstring widening: add a paragraph naming HTTP DELETE/POST/PUT/PATCH as in-scope for the short-circuit, with a forward reference to `IsVerifyLiveHTTPEnv()` for the verifier opt-in.
- Both files keep their existing prose; the changes are additive.

**Execution note:** Land docs in the same commit as U1+U2+U3 (per AGENTS.md pointer-rot rule).

**Test scenarios:** None (docs change). Verification is by review.

**Verification:** A reviewer reading AGENTS.md "Side-effect commands" understands that mutating HTTP verbs are gated at the transport layer without having to read the template source.

---

### U7. regen-merge classification fixture for the new template block

**Goal:** Users with hand-edited `client.go` don't get TEMPLATED-VALUE-DRIFT confusion when they `--force` regen.

**Requirements:** R9

**Dependencies:** U2

**Files:**
- `internal/pipeline/regenmerge/testdata/verify_short_circuit_fixture/published_client.go` (create — fixture with hand-edits AROUND the do() function but not in it)
- `internal/pipeline/regenmerge/testdata/verify_short_circuit_fixture/fresh_client.go` (create — fresh template output with the new short-circuit block)
- `internal/pipeline/regenmerge/classify_test.go` (modify — add test case using the new fixture)

**Approach:**
- Fixture mimics a real-world hand-edit: e.g., a user added a custom header in `do()` between the URL building and the request dispatch. The regen-merge classifier should preserve that hand-edit and ALSO apply the new short-circuit block.
- The test asserts the classifier verdict is `TEMPLATED-WITH-ADDITIONS` (preserved-and-merged), not `TEMPLATED-VALUE-DRIFT` (which forces manual review).
- If the verdict comes out wrong, the fix isn't this plan's job — open a follow-up retro for the classifier. But the fixture must exist so future regressions are caught.

**Patterns to follow:** existing regenmerge fixtures under `internal/pipeline/regenmerge/testdata/`.

**Test scenarios:**
- *Covers R9.* Hand-edited `do()` + fresh template with short-circuit → classifier returns TEMPLATED-WITH-ADDITIONS, merged file contains both the hand-edit and the new short-circuit.
- Unmodified published `do()` + fresh template → classifier returns TEMPLATED, clean overwrite.

**Verification:** `go test ./internal/pipeline/regenmerge/...` passes.

---

### U8. Public library regen-sweep playbook

**Goal:** A documented, tier-prioritized rollout path so every published CLI gets the short-circuit.

**Requirements:** R10

**Dependencies:** U1–U7 and U9 merged in this repo

**Files:**
- `docs/solutions/best-practices/verify-mode-short-circuit-rollout-2026-05-13.md` (create — playbook)
- `scripts/library-regen-verify-mode-rollout.sh` (optional — helper script that loops `regen-merge --apply` across the public library's local checkout, tier-prioritized)

**Approach:**
- Playbook documents the order: tier:official CLIs first (smaller blast radius, higher review attention), tier:community after. For each CLI: clone or pull, run `printing-press regen-merge <cli-dir> --fresh <tmp-fresh> --apply`, run the CLI's `go test ./...`, commit, batch-PR.
- Script is optional sugar: parses `registry.json` for tier, loops by tier with a pause between CLIs. Skips on test failure, prints a summary at the end.
- Playbook calls out the macOS/Linux limitation of `regen-merge` (Windows is not supported per its --help) and the implication that the Windows-based author of this plan will need a macOS/Linux box (or VM/WSL) to run the sweep.
- Playbook also calls out the latent risk that `live_dogfood` and `workflow_verify` inherit parent env: operators must NOT have `PRINTING_PRESS_VERIFY=1` set in their shell when running those.

**Patterns to follow:** existing playbooks under `docs/solutions/best-practices/`, especially `cross-repo-coordination-with-printing-press-library-2026-05-06.md`.

**Test scenarios:** None for the playbook (prose). The optional script gets a smoke test against a 2-CLI fixture if it ships.

**Verification:** A reviewer reading the playbook can execute the sweep without back-channel questions.

---

### U10. Strip verify-env vars from live-verifier subprocess env

**Goal:** `pipeline.RunLiveDogfood` and `pipeline.RunWorkflowVerification` (and any other near-destructive runner) must NOT honor a verify-mode short-circuit inherited from the caller's shell. The destructive-side silent-success path closes.

**Requirements:** R3

**Dependencies:** U1

**Files:**
- `internal/pipeline/live_dogfood.go` (modify — at line 845, assign `cmd.Env` to a filtered copy of `os.Environ()` that drops `PRINTING_PRESS_VERIFY=*` and `PRINTING_PRESS_VERIFY_LIVE_HTTP=*` entries)
- `internal/pipeline/workflow_verify.go` (modify — at line 147, same filtered env assignment)
- `internal/pipeline/live_dogfood_test.go` (modify or add — assert subprocess env does NOT contain verify vars even when parent process has them set via `t.Setenv`)
- `internal/pipeline/workflow_verify_test.go` (modify or add — same assertion)

**Approach:**
- Add a small filter helper (e.g., `filterVerifyEnv(env []string) []string`) co-located with the runners or in an internal helper file. The filter drops any `key=value` entry whose key matches the two verify env vars.
- At each `exec.CommandContext` site, set `cmd.Env = filterVerifyEnv(os.Environ())`. Inline code comment explains why: these runners exercise real / near-real destructive behavior and must not honor verify-mode short-circuits inherited from the caller's shell.

**Patterns to follow:** existing `cmd.Env = ...` patterns elsewhere in `internal/pipeline/*.go`; the env-filtering shape parallels `buildEnv` in runtime.go (which adds env vars; this helper drops them).

**Test scenarios:**
- *Covers R3.* `t.Setenv("PRINTING_PRESS_VERIFY", "1")` followed by invoking the runner: subprocess env (captured via a recording exec.Cmd substitute or via a process that re-emits its own env) does NOT contain `PRINTING_PRESS_VERIFY` or `PRINTING_PRESS_VERIFY_LIVE_HTTP`.
- Same scenario for `PRINTING_PRESS_VERIFY_LIVE_HTTP`.
- All non-verify env vars survive the filter (`PATH`, `HOME`, API tokens, etc.).

**Verification:** `go test ./internal/pipeline/...` passes; manual smoke: `PRINTING_PRESS_VERIFY=1 printing-press dogfood --dir <smoke> --live --allow-destructive` actually performs destructive ops (does not silently noop).

---

### U9. Surface verify-env state in `doctor`

**Goal:** Operators who unintentionally inherit `PRINTING_PRESS_VERIFY=1` (parent shell, CI runner, container image) can detect the foot-gun without reading response bodies. Closes the "silent inheritance" gap that U8's playbook only flags as a caution.

**Requirements:** R1 (operator legibility of the short-circuit), supports R3 indirectly

**Dependencies:** U1

**Files:**
- `internal/generator/templates/doctor.go.tmpl` (modify — add an "Env" section line that reports `PRINTING_PRESS_VERIFY`/`PRINTING_PRESS_VERIFY_LIVE_HTTP` state alongside the existing env-driven flag readouts)
- `internal/generator/templates/doctor_test.go.tmpl` (modify or create — emitted test asserting the lines appear when env is set)
- `internal/generator/generator.go` (modify if `doctor_test.go.tmpl` is created — register in the `singleFiles` map, parallel to where `doctor.go.tmpl` emits)

**Approach:**
- After the existing `Env Vars` block in `doctor`'s output, add a `Verify Mode` line:
  - When neither env is set: `Verify Mode: not active (normal operation)`
  - When `PRINTING_PRESS_VERIFY=1`, no `LIVE_HTTP`: `Verify Mode: ACTIVE — mutating HTTP verbs short-circuit (no network calls for DELETE/POST/PUT/PATCH)` — flagged as INFO with a hint pointing at the env var.
  - When both envs set: `Verify Mode: ACTIVE — live HTTP opt-in (mutating verbs dial out)` — INFO without warning.
- The point: an operator running `<cli> doctor` after an unexpected `success:true / status:noop` sees the diagnostic context in one place. Pairs with the inner-envelope `reason` literal as the two-anchor diagnosis story.

**Patterns to follow:** existing `doctor` env-readout lines (Config, Auth, Env Vars, API, Cache sections).

**Test scenarios:**
- *Covers R1.* Doctor output with no env vars set: line reports "not active."
- Doctor output with `PRINTING_PRESS_VERIFY=1` only: line reports the short-circuit state.
- Doctor output with both envs: line reports the live-HTTP state.
- Existing doctor checks (Config / Auth / Env / API / Cache) unaffected.

**Verification:** Emitted `doctor_test.go` passes; petstore smoke retest shows the new line in `./petstore-pp-cli.exe doctor`.

---

## Verification Strategy

End-to-end verification per requirement, in landing order:

1. **U1–U3 land + goldens regen.**
   - Run `scripts/golden.sh verify`. Expect diffs in 3 fixtures: `testdata/golden/expected/generate-golden-api/printing-press-golden/internal/client/client.go`, `testdata/golden/expected/generate-golden-api-oauth2-cc/printing-press-oauth2-cc/internal/client/client.go`, `testdata/golden/expected/generate-tier-routing-api/tier-routing-golden/internal/client/client.go`.
   - Inspect each diff. Each should show: new `isMutatingVerb` helper, new short-circuit block at top of `do()`, new envelope helper. No incidental changes elsewhere.
   - Run `scripts/golden.sh update` on a clean worktree.
   - Commit the golden diffs in the same PR as the template change.

2. **R1 (short-circuit fires) — Repo-level.** Generator test `TestClient_VerifyShortCircuit` (U4) passes.

3. **R1 + R3 + R7 (short-circuit fires in printed CLI; live path unchanged) — Smoke retest.**
   - Regenerate the matrix-22cc2fd9 petstore smoke CLI (in the local matrix output dir — e.g., `<matrix-output-root>/_smoke/petstore` — substitute your matrix checkout path).
   - With `PRINTING_PRESS_VERIFY=1` and no `LIVE_HTTP`: `./petstore-pp-cli.exe pet delete 42 --dry-run=false` returns `{"status":"noop","reason":"verify_short_circuit",...}`; no network call (verify via OS-level packet capture or by setting an unroutable base URL — see deferred notes).
   - With both envs set: same command attempts the network call (control case).
   - With no envs: same command attempts the network call (operator path).
   - **LIVE_HTTP plumbing assertion** (R2 integration leg): re-run U5's emitted printed-CLI test against a regenerated smoke CLI binary with `PRINTING_PRESS_VERIFY=1 PRINTING_PRESS_VERIFY_LIVE_HTTP=1` set. The recording-mock `http.RoundTripper` the test injects must record at least one outbound mutating-verb request — proving the `LIVE_HTTP=1` opt-in actually plumbs through and short-circuits do not fire when it's set. If the recording transport sees zero requests for mutating verbs, the verify mock-mode contract is silently broken even when pass-rate stays at 100. (This uses the in-process recording RoundTripper U5 already builds; no new mock-server-side counter infrastructure required.)

4. **R2 (verify mock-mode integrity) — `printing-press verify`.** Re-run `printing-press verify --dir <regenerated-smoke> --spec <merged-spec> --fix --json` on a regenerated CLI. Expect `mode=mock`, `verdict=PASS`, `pass_rate=100`. Confirm non-zero `total` (at least one test ran). Any drop in `pass_rate` below 100% is a regression.

5. **R3 (live paths unchanged) — Live verifiers.** Confirm `pipeline.RunLiveDogfood` and `pipeline.RunLiveCheck` do not set `PRINTING_PRESS_VERIFY=1` (existing behavior, confirmed by code read). No regression test needed; preserve by code review.

6. **R6 (golden diffs explained).** PR description includes a section explaining the three golden diffs with the reasoning above.

7. **R7 (emitted printed-CLI test).** After regen, every printed CLI runs `go test ./internal/client/...` and the new test passes. Verify on at least: petstore smoke CLI, plus one tier:official library CLI (e.g., notion, stripe, GitHub — whichever is fastest to regen locally) before the broader sweep.

8. **R8 (generator-level test).** Already covered by U4's verification.

9. **R9 (regen-merge fixture).** Covered by U7's verification.

10. **R10 (rollout playbook).** Playbook reviewed; first sweep batch (3-5 tier:official CLIs) verifies the recipe end-to-end before broader rollout.

11. **Agent-readiness re-test.** After regen, re-run the agent-readiness reviewer (or its substitute — see Risks) on the petstore smoke CLI. B1 should no longer fire.

---

## Scope Boundaries

### In scope
- Verb-gated transport-layer short-circuit
- `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` opt-in env
- Generator-level and emitted-printed-CLI test coverage
- AGENTS.md + cliutil docstring update (same PR)
- regen-merge classification fixture
- Public library rollout playbook

### Out of scope (machine deferrals already captured in `morning-report.md`)
- Generator CRLF emission on Windows
- verify-skill Python resolution (Windows `py` vs working `python`)
- Cobra flag aliases invisible in `--help`
- Spec-author test-instruction debris in descriptions
- `DeriveRunIDFromResearchDir` not matching Matrix run-id format
- `verify-skill`'s `likely_false_positive` heuristic that mislabeled the `auth set-token` ghost recipe
- Misclassified exit code on missing `base_url` (separate friction from agent-readiness review)
- Array-flag JSON-only without help-text indication (separate friction)
- `doctor` API line lacking inline `config_path` (polish-tier finding)

### Deferred to Follow-Up Work
<!-- Removed: live_dogfood and workflow_verify env-inheritance hardening is now covered by U10 (strips verify env vars from subprocess env). -->
<!-- Removed: narrative validation handling is now covered by U3 expansion (sets LIVE_HTTP=1 in narrativecheck.go:238). -->
- **New `mcp:verify-allow-live` annotation taxonomy.** Currently env-var gating is sufficient. If future CLIs need finer-grained control (e.g., "this DELETE is safe to actually run in verify"), introduce annotation then. Not now.

### Outside this product's identity
- Changing the meaning of `PRINTING_PRESS_VERIFY=1` for non-HTTP side effects. The browser-launch / notification / OS-handler scope of the existing rule is preserved; the new gate is additive.
- Adding any client-level mocking that competes with `printing-press verify`'s httptest flow. Verify owns mock-mode; this short-circuit is the floor underneath verify, not a replacement for it.
- **Gating non-HTTP mutation surfaces under verify mode.** This plan does NOT extend `IsVerifyEnv()` short-circuiting to:
  - Local SQLite store writes (sync, import, workflow, analytics commands writing to `internal/store/`)
  - File outputs from `--deliver file:<path>`, `export`, manuscript writes, and similar
  - Future RPC / gRPC / SSE / WebSocket / IPC surfaces that bypass `Client.do`
  - In-process state mutations inside handlers (local counters, in-flight retry state)

  These surfaces remain gated only by the existing side-effect-command rule and any hand-written `cliutil.IsVerifyEnv()` checks. If broader verify-mode safety becomes a requirement, address in a separate plan with its own scope analysis.

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Anti-reimplementation rule flags the synthetic envelope as a banned "endpoint stub" | Medium | High (PR review block) | Frame the change explicitly as transport-layer infrastructure in PR description. Confirm `reimplementation_check` scans handlers (root.go, etc.) not `client.go` — research confirms this. Add a code comment in `do()` referencing AGENTS.md "Side-effect commands" so future readers see the rationale. |
| Verify mock-mode regresses because `LIVE_HTTP=1` isn't honored everywhere | Low | High | U3 covers the verify runtime. The dependency chain (U1 → U3 + U2) ensures the helper exists before verify uses it. Verification step 4 catches regressions. |
| `live_dogfood` / `workflow_verify` operators have `PRINTING_PRESS_VERIFY=1` in their shell and silently get no-ops on destructive tests | Low | Low (resolved by U10) | Resolved: U10 strips `PRINTING_PRESS_VERIFY` and `PRINTING_PRESS_VERIFY_LIVE_HTTP` from `live_dogfood.go:845` and `workflow_verify.go:147` subprocess env. Operators with the env set in their shell get real destructive behavior; the verify-mode short-circuit cannot be inherited. |
| Hand-edited `client.go` users lose customizations on `--force` regen | Low | High (data loss equivalent) | U7 fixture pins regen-merge classification. If verdict is wrong, follow-up retro before the sweep starts. |
| Goldens diff includes unintended drift beyond the short-circuit | Low | Low (caught in review) | Diff inspection during verification step 1. Three fixtures total — easy to eyeball. |
| `validate-narrative --full-examples` breaks because a recipe asserts on mutating-verb response body | Low | Low (resolved by U3 expansion) | Resolved: U3 now expands to set `LIVE_HTTP=1` in `narrativecheck.go:238` alongside the existing `PRINTING_PRESS_VERIFY=1`, parallel to `pipeline.RunVerify`'s opt-in. Narrative full-example subprocesses continue to hit the real wire path; no fixture audit required. |
| Agent-readiness reviewer (canonical, `cli-agent-readiness-reviewer`) isn't installed when the re-test runs; substitute reviewer disagrees on whether B1 is fixed | Medium | Low | Use the same substitute (`ce-agent-native-reviewer`) for the re-test; require both manual `pet delete 42` test and reviewer's nod. |
| Cross-platform: `regen-merge` is macOS/Linux-only; primary author is on Windows | High | Medium (rollout friction) | Document in U8 playbook. Plan author either uses WSL/VM or hands off the sweep to a Linux-capable maintainer. |

---

## System-Wide Impact

The short-circuit fires at the transport layer (`Client.do`), below every surface in a printed CLI. Interfaces crossed and their expected behavior:

| Surface | File | Behavior under short-circuit | Notes |
|---|---|---|---|
| Endpoint handler envelope | `internal/generator/templates/command_endpoint.go.tmpl` (around lines 650-707) | Wraps synthetic body as `data` inside `{action, resource, path, status:200, success:true, data:{status:"noop",...}}`. `--select`/`--compact` pass through unchanged. | Operator-visible distinguishability: rely on the inner `status:"noop"` + `reason:"verify_short_circuit"` — the OUTER envelope reports `success:true`. U2 adds an inline code-comment in the envelope assembly noting the synthetic case; U9's `doctor` line is the second diagnosis anchor. |
| Cache layer | `Client.invalidateCache()` at `client.go.tmpl:473`, called at `:996` | NOT invoked — short-circuit returns BEFORE the success branch reaches the `method != http.MethodGet` block. Cache state stays consistent: a subsequent GET sees the still-present resource. | KTD4 states this explicitly. |
| Retry middleware | Retry/backoff loop at `client.go.tmpl` (around lines 850-1040) | NOT invoked — sits below the short-circuit. No retry on the synthetic 200. | Correct. |
| MCP shellout walker | `internal/generator/templates/cobratree/shellout.go.tmpl` (`exec.CommandContext` around line 37) | Spawns the same binary; inherits parent env including `PRINTING_PRESS_VERIFY=1`. Mutating tools advertise `destructiveHint:true` and return synthetic envelopes at runtime under verify. Annotation matches advertised semantics when `LIVE_HTTP=1` is also set (verify's contract). | Confirms KTD5: no annotation drift; the tool IS destructive when actually dialed. |
| MCP typed endpoint tools | Endpoint-mirror tools registered via `pp:endpoint` annotation | Same `do()` path — same short-circuit, same envelope. | No special handling needed. |
| Narrative full-example runner | `internal/narrativecheck/narrativecheck.go:238` | Already sets `PRINTING_PRESS_VERIFY=1` without the new opt-in. Mutating-verb recipes will receive synthetic envelopes after this lands. | Promoted to a Medium/Medium risk; scan recipe fixtures BEFORE landing — pre-land gate, not follow-up. |
| Cache invalidation contract | [`http-client-cache-invalidate-on-mutation`](../solutions/design-patterns/http-client-cache-invalidate-on-mutation-2026-05-05.md) | Contract is "invalidate ONLY on successful real mutation." Short-circuit returns success but is not a real mutation; correctly skips invalidation by virtue of placement. | U2 Approach adds an inline comment at the short-circuit explicitly noting "no cache invalidation — no state changed." |

**Failure-mode legibility.** If `PRINTING_PRESS_VERIFY=1` is set unintentionally (inherited from a parent shell, CI runner, container image), a confused operator sees `success:true` (outer envelope) plus an inner `__pp_verify_synthetic__:true` and `reason:"verify_short_circuit"`. The `__pp_verify_synthetic__` sentinel is the primary diagnostic anchor — namespace-reserved and unambiguous; the `reason` literal is the secondary prose anchor; U9's new `doctor` line is the third anchor that surfaces the same context without requiring the operator to inspect a response body.

**Cross-boundary env contract.** `PRINTING_PRESS_VERIFY_LIVE_HTTP=1` is a new contract spanning the generator (template), the runtime (`pipeline.RunVerify`), and every published CLI. The cliutil docstring (U6) is the canonical home; external consumers (downstream skill authors, security auditors, library CI maintainers) find the contract there without reading template source. Future deeper documentation can land in a dedicated `docs/VERIFY-ENV.md` if more contracts accumulate.

---

## Rollout Plan

### Phase R1: This repo (cli-printing-press)
1. PR with U1–U7, U9, and U10 in one commit set. Conventional commit: `fix(cli): gate mutating HTTP verbs on PRINTING_PRESS_VERIFY in generated clients`. U8 (playbook + optional script) can land in the same PR or as a follow-up before the sweep begins — playbook prose doesn't gate the binary release.
2. CI gate: `go test ./...`, `scripts/golden.sh verify` (the goldens are now correct).
3. Mergify queue with `ready-to-merge` label.
4. Release-please picks up the `fix(cli):` commit on next release PR.

### Phase R2: Public library sweep (printing-press-library)
1. Wait for the cli-printing-press release that includes U1–U7 to ship a new printing-press binary (or use the unreleased binary from a tagged commit; document either path).
2. Tier:official CLIs first. Per CLI: `printing-press regen-merge <cli> --fresh <tmp> --apply`, `go test ./...`, commit, batch-PR or one-PR-per-CLI per the public library's convention.
3. Tier:community CLIs second. Same flow.
4. Each PR's description references this plan and U8 playbook.

### Phase R3: Smoke re-verification (matrix-22cc2fd9 or equivalent)
1. Re-run the petstore smoke pipeline end-to-end with the regenerated binary.
2. Confirm agent-readiness B1 no longer fires.
3. Update the matrix run's `agent-readiness.md` to reflect the verdict change (optional housekeeping).

---

## Documentation Plan

Inline with U6 (AGENTS.md + cliutil docstring). No separate docs PR.

`docs/SPEC-EXTENSIONS.md` — no change (no new `x-*` extensions).
`docs/PIPELINE.md` — no change (no new pipeline phase).
`docs/PATTERNS.md` — optional: add the verb-gated short-circuit pattern as a cross-cutting design pattern entry referencing this plan. Defer unless reviewer requests.
`docs/solutions/best-practices/verify-mode-short-circuit-rollout-2026-05-13.md` — created in U8.

---

## Deferred to Implementation

These are knowable from code but cheaper to resolve when the implementer has the editor open:

- Exact placement of the short-circuit block in `do()` — before or after the proxy-envelope vs standard-path fork. Implementer chooses based on whichever reads cleaner; the contract (no network call when conditions match) holds either way.
- Whether `isMutatingVerb` lives in the template body or as a generated helper. Implementer judgment.
- Whether U7's fixture goes under existing testdata or a new subdir. Match nearest existing pattern.
- Whether the U8 helper script ships or stays as documented manual steps. Lean toward shipping if the per-CLI loop is more than ~5 steps; otherwise keep it as a checklist.
- Exact naming of the regen-merge test case in U7.

These are NOT planning-time questions; they're 30-second decisions during implementation that don't affect plan shape.

---

## Deferred / Open Questions

### From 2026-05-13 review

**Q1. Premise validation: should the canonical `cli-agent-readiness-reviewer` confirm B1 before committing to Option B?** (P1, root; deferred from doc-review round 1)

The agent-readiness finding B1 that triggered this plan was flagged by a substitute reviewer (`ce-agent-native-reviewer`), not the canonical `cli-agent-readiness-reviewer` (which isn't installed in this session). AGENTS.md line 40 scopes the side-effect rule to "Hand-written novel commands that perform visible actions," and the cliutil docstring limits to OS-visible side effects. On a strict reading, generated transport code may be out of scope for the rule — in which case B1 is a misapplication and Option D (docs-only) closes the gap without committing to a permanent env-var contract, three golden diffs, a 200+ CLI sweep, and a narrativecheck fixture audit.

Before Rollout Phase R1 lands, get one canonical-reviewer validation pass (or an explicit maintainer ruling on whether AGENTS.md's side-effect rule reaches generated transport code). If the canonical reviewer agrees with the substitute, proceed with Option B as planned. If it disagrees, scope-shrink to Option D and defer Option B until evidence accumulates.

The following 6 sub-questions are dependents of Q1 — they auto-resolve if Q1 is answered "Option D suffices; cancel Option B work":

- **Q1a.** Should we consider a typed `cliutil.VerifyMode` struct (intent named at one place, parsed once from env) instead of accumulating per-question env vars? (product-lens P3)
- **Q1b.** What's the realistic public-library sweep cost in maintainer-hours vs. the 9 deferred frictions in `morning-report.md`'s out-of-scope list? Is this sweep the highest-leverage use of that time? (product-lens P4)
- **Q1c.** Is `PRINTING_PRESS_VERIFY_LIVE_HTTP` a long-term contract or scaffolding tied to verify's current httptest mock-server architecture? If verify pivots to request-replay/contract-testing, does the opt-in become dead code in 200+ CLIs? (product-lens P6)
- **Q1d.** Is U9 (doctor verify-env line) genuinely required by R1, or post-deepening scope creep? No R-ID actually mandates operator-legibility output. (scope-guardian SG1)
- **Q1e.** Is U8 (rollout playbook) in scope for this PR or a follow-up? Rollout Plan Phase R1 explicitly permits deferral but U8 remains in active scope. (scope-guardian SG2)
- **Q1f.** Should the conventional commit type be `feat(cli):` rather than `fix(cli):`? The new env-var contract is a public-API addition, not a behavior correction — semver implications favor `feat`. (adversarial A1)

These items remain in the plan as Q1 + sub-questions until a maintainer resolves Q1; ce-work should not begin implementation of Phase R1 until the canonical-reviewer ruling is recorded here.

---

## Grep Receipts

Evidence gathered during planning that grounds the file/symbol references above. All commands were run from the cli-printing-press repo root unless noted.

```text
$ grep -rln "PRINTING_PRESS_VERIFY\|IsVerifyEnv" internal/ AGENTS.md
testdata/golden/expected/generate-golden-api/printing-press-golden/internal/cliutil/verifyenv.go
testdata/golden/expected/generate-golden-api-oauth2-cc/printing-press-oauth2-cc/internal/client/client.go
testdata/golden/expected/generate-golden-api-oauth2-cc/printing-press-oauth2-cc/internal/cli/auth.go
skills/printing-press/SKILL.md
internal/pipeline/runtime_commands.go
internal/pipeline/runtime.go
internal/narrativecheck/narrativecheck.go
internal/generator/templates/config.go.tmpl
internal/generator/templates/cliutil_verifyenv.go.tmpl
internal/generator/templates/client.go.tmpl
internal/generator/templates/auth_client_credentials.go.tmpl
internal/generator/templates/auth_simple.go.tmpl
internal/generator/templates/auth.go.tmpl
internal/generator/generator_test.go
internal/generator/auth_env_precedence_test.go
internal/cli/validate_narrative.go
AGENTS.md
```

Confirms the surface this plan must touch: `cliutil_verifyenv.go.tmpl`, `client.go.tmpl`, `runtime.go`, `AGENTS.md`. Plus three golden trees that will diff.

```text
$ grep -n "^func (c \*Client) do(" internal/generator/templates/client.go.tmpl
713:func (c *Client) do(method, path string, params map[string]string, body any, headerOverrides map[string]string) (json.RawMessage, int, error) {
```

Anchors KTD4 and U2: the `do()` function is the single dispatch point, line 713 in the template.

```text
$ grep -n "IsVerifyEnv\|PRINTING_PRESS_VERIFY" internal/generator/templates/client.go.tmpl
1306:{{- if eq .Auth.EffectiveOAuth2Grant "client_credentials"}}
1307:    if c.Config.AccessToken == "" && cliutil.IsVerifyEnv() {
1308:        c.Config.AccessToken = "mock-token-for-testing"
1309:        return c.Config.AuthHeader(), nil
1310:    }
```

Anchors KTD2 (synthetic-value precedent at line 1307) and U2's placement choice (short-circuit must fire BEFORE this so client_credentials minting doesn't run unnecessarily).

```text
$ grep -n "PRINTING_PRESS_VERIFY\|IsVerifyEnv" internal/pipeline/runtime.go
260:			// cliutil.IsVerifyEnv() and short-circuit when set, so even
265:			env = append(env, "PRINTING_PRESS_VERIFY=1")
275:		// PRINTING_PRESS_VERIFY env var alone may not gate, skip its
411:	// PRINTING_PRESS_VERIFY=1 is already set in env, so well-behaved
430:	// that during verify even with PRINTING_PRESS_VERIFY=1 set, because
```

Anchors U3: line 265 is the exact insertion point for the new `LIVE_HTTP=1` env append.

```text
$ grep -n "Env\b\|Setenv\|exec.Command" internal/pipeline/live_dogfood.go internal/pipeline/live_check.go internal/pipeline/workflow_verify.go
internal/pipeline/live_dogfood.go:845:    cmd := exec.CommandContext(ctx, binaryPath, args...)
internal/pipeline/workflow_verify.go:147:    cmd := exec.CommandContext(ctx, binary, args...)
```

Confirms R3: live_dogfood, live_check, and workflow_verify do NOT explicitly set `PRINTING_PRESS_VERIFY=1`. They inherit parent env (Risks table calls this out as a latent inheritance risk for operators).

```text
$ grep -rln "client.go" testdata/golden/cases/
testdata/golden/cases/generate-golden-api/artifacts.txt
testdata/golden/cases/generate-golden-api-oauth2-cc/artifacts.txt
testdata/golden/cases/generate-tier-routing-api/artifacts.txt
```

Confirms three golden fixtures touch `client.go` — Verification Strategy step 1 enumerates these three.

```text
$ grep -n "Side-effect\|IsVerifyEnv\|Generator-reserved\|Anti-reimplementation\|Generator Output Stability" AGENTS.md | head
3:## Machine vs Printed CLI
13:### Anti-reimplementation
39:### Side-effect commands
44:### Generator-reserved namespaces
61:## Generator Output Stability
```

Anchors the AGENTS.md sections cited in Problem Frame, KTD6, and Risks. Line 39 is the section that U6 updates; line 44's "Generator-reserved namespaces" rule mandates the cliutil placement in KTD2.

```text
$ grep -rn "client.go\b" internal/pipeline/reimplementation_check.go
(no matches)
```

Confirms the Risk-table mitigation: `reimplementation_check` does not scan `client.go`, so the synthetic envelope in `do()` won't trip the anti-reimplementation heuristic (which targets handlers and `root.go`).
