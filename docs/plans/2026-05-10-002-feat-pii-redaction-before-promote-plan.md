---
title: 'feat: Deterministic PII inventory + agent-marked ledger gate before promote/publish'
type: feat
status: active
date: 2026-05-10
---

# feat: Deterministic PII inventory + agent-marked ledger gate before promote/publish

## Summary

Build a PII gate using the **Deterministic Inventory + Agent-Marked Ledger** pattern documented in [`docs/PATTERNS.md`](../PATTERNS.md) — the same shape `printing-press tools-audit` and `printing-press public-param-audit` already use. A new `printing-press pii-audit <cli-dir>` subcommand walks high-risk files with deterministic regex detectors and emits a persistent ledger of candidate findings. The agent layer (a new `pii-polish.md` reference in the polish skill) walks the ledger and either fixes findings in source (auto-removed next run) or marks them `accepted` with pre-decision fields explaining why. Promote and publish gates invoke the audit and refuse if any finding is pending or if enforcement primitives flag the run incomplete. No auto-mutation of files at the gate.

---

## Problem Frame

Issue [#958](https://github.com/mvanhorn/cli-printing-press/issues/958) (P1, sub-issue of the amazon-orders retro [#955](https://github.com/mvanhorn/cli-printing-press/issues/955)): real customer PII captured during browser-sniff sessions has reached published printed CLIs. The amazon-orders retro found 4 files with real order IDs, a real card last-4, and 8 real ASINs — caught only by an ad-hoc `grep` against the public PR clone.

The Printing Press already scans for vendor-prefix credentials (`sk_*`, `ghp_*`, `xoxb-*`, etc.) at publish via [`internal/artifacts/secrets.go`](../../internal/artifacts/secrets.go) and [`internal/cli/publish.go:382`](../../internal/cli/publish.go#L382). It does not scan for customer PII at any gate, and [`internal/pipeline/lock.go:259`](../../internal/pipeline/lock.go#L259) (`CopyDir(workingDir, stagingDir)`) runs unguarded.

**Why the prior plan was wrong:** an earlier draft of this plan proposed pure mechanical detection + auto-redaction triggered by a `--accept-redaction-list` flag. Doc review revealed three structural problems that pushed the approach toward fool's-errand territory:

1. **Person-name detection is not solvable at regex level.** "Two capitalized words in a JSON value" matches "New York", "Acme Corp", "Test User", "Apple Watch" — names of places, companies, products, and examples are indistinguishable from real PII by shape alone. Microsoft Presidio uses an ML NER model + names lookup database and still runs at ~75-85% precision; regex lands at 30-50% in real code.
2. **Auto-redaction silently mutates files.** Agent operators reflexively pass `--accept-redaction-list=<all-findings>` to clear halts, corrupting legitimate content (markdown bullets, attribution metadata) with no review trail.
3. **Ephemeral CLI-flag acks are not an audit trail.** "More auditable than blanket bypass" was the framing, but no acknowledgment was persisted anywhere — `git reflog` of shell history is not an audit log.

The codebase already has a pattern that solves exactly this shape — **mechanical detection plus per-item agent judgment with persisted rationale**. `tools-audit` does this for thin MCP descriptions; `public-param-audit` does it for cryptic wire parameters. This plan applies the same shape to PII.

Phase-1 detector families remain shape-only and require no spec-schema work. Spec-aware detectors (order/transaction ID shapes from `auth.id_shapes`, cookie name+value from `auth.cookies`, ASIN-style entity IDs) defer to [#960](https://github.com/mvanhorn/cli-printing-press/issues/960).

---

## Requirements

Carried forward from issue #958 acceptance criteria (F1 positive/negative), restated to fit the ledger pattern:

- **R1.** A new content-scan gate runs before promote. When the audit's ledger contains pending findings (or enforcement primitives flag the run incomplete), promote halts with a pointer to the ledger and a summary of finding categories. (issue F1 positive #1)
- **R2.** Detectors run only against high-risk file paths (`**/*.json`, `**/*.yaml`, `**/*.yml`, `**/*.md`, `**/*_test.go`, `**/.manuscripts/**`, `**/testdata/**`, `README.md`). Generator-emitted artifacts and non-test Go source are excluded by default.
- **R3.** Phase-1 detector families: card last-4 (with required context tokens), email address, US phone number, ZIP+4, postal-address line. Person-name detection is intentionally omitted — see KTD-3.
- **R4.** A clean OpenAPI-spec CLI (no browser-sniff, no captured PII) passes both gates: audit emits zero findings; ledger is empty. (issue F1 negative #1)
- **R5.** A browser-sniff CLI whose captured values are already synthetic placeholders passes: audit emits zero findings. (issue F1 negative #2)
- **R6.** Per-finding agent decisions persist in a ledger file (`<cli-dir>/.printing-press-pii-polish.json`) that survives across runs. Finding identity (file, line, kind, normalized matched span) is the stable key — re-runs preserve agent `status`/`note`/pre-decision fields whose identity key matches.
- **R7.** Enforcement primitives (mirroring `tools-audit`) gate the "done" state: pre-decision fields required for accept on every kind, duplicate-rationale rejection above threshold 5, delta tracking between runs.
- **R8.** PII audit runs at publish alongside the existing vendor-prefix secret scan. The two are independent — neither replaces the other, and both must clear for publish to proceed.

---

## Key Technical Decisions

### KTD-1. Apply the Deterministic Inventory + Agent-Marked Ledger pattern

The work has the canonical shape: mechanical detection, per-item judgment, persisted rationale. Existing implementations in [`internal/cli/tools_audit.go`](../../internal/cli/tools_audit.go) and [`internal/cli/public_param_audit.go`](../../internal/cli/public_param_audit.go) provide the structural template; the polish skill at [`skills/printing-press-polish/references/tools-polish.md`](../../skills/printing-press-polish/references/tools-polish.md) provides the agent-side template.

The binary emits a ledger; the agent walks it. The agent makes fixes in source files (auto-removed next run) or marks findings accepted with rationale. No mutation at gate time. No CLI flag for acceptance — accepts live in the ledger.

### KTD-2. File-scoping shrinks the false-positive surface to the actual leak vectors

The amazon-orders retro identified the leak path as captured browser-sniff content flowing into a narrow file set: `internal/parser/parser_test.go`, `internal/mcp/tools.go` (generated from spec parameter examples), `.manuscripts/<run>/research/<api>-browser-sniff-spec.yaml`, `README.md`. Detectors run only against the high-risk file globs in R2. Excluded: non-test Go source (`*.go` excluding `*_test.go`), `go.mod`, `go.sum`, `tools-manifest.json` (generator-emitted from spec), and the generated MCP shim files.

This is a deliberate scope cut. A name leaked into hand-written novel `*.go` source isn't caught by Phase 1 — that path is rare enough that the false-positive cost of broader scanning isn't worth it. If a real leak surfaces in non-test Go source, file a follow-up; the file-glob set is configurable.

### KTD-3. Person-name detection deferred — regex precision is unrecoverable

The agent-marked ledger pattern hedges low-precision detection by adding per-item judgment, but only when finding volume is manageable. Person-name detection over arbitrary JSON values produces hundreds of findings per CLI from "New York", "Acme Corp", "Test User", "Apple Watch", etc. Even with agent judgment, the per-CLI walk-through cost is disproportionate to the leak signal — the parent retro's name leaks were *adjacent to* postal-address values, so the address detector catches the leak shape by proximity.

Defer person-name detection to a follow-up that uses NER (Presidio Go bridge or equivalent), tracked separately from #960. The Phase-1 cut is: emails, phones, card-last-4, ZIP+4, postal-address-line.

### KTD-4. Detector list with context-token requirements

Each detector is high-precision because false-positive cost falls on the agent walker:

- **Card last-4:** `(?i)(card|visa|mastercard|amex|ending in|last\s+4|x{4,}|\*{4,})[\s:.-]{0,5}\d{4}` — requires a context token within a few characters. Bare `\d{4}` is too noisy even with agent judgment.
- **Email:** `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b` — high precision; accept TLD validation.
- **US phone:** `(?:\+?1[\s.-]?)?\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4}` — medium precision; agent judges (toll-free, fake-numbers, support-line examples).
- **ZIP+4:** `\b\d{5}-\d{4}\b` — emitted but the agent layer rejects when not in address context. Common false positives: API request IDs, batch identifiers.
- **Postal-address line:** `(?i)\d+\s+[A-Z][A-Z\s]{3,30}(?:ST|STREET|AVE|AVENUE|RD|ROAD|BLVD|BOULEVARD|DR|DRIVE|LN|LANE|CT|COURT|PL|PLACE|WAY)\b` — street-number + ALL-CAPS street name + suffix.

Each detector emits findings with `kind`, `file`, `line`, `column`, and `matched_span` (the offending substring). The agent layer reads context around the line to judge.

### KTD-5. Finding identity = file + line + kind + normalized-span

The stable key for ledger preservation. Normalizing the matched span (lowercase + collapsed whitespace) tolerates minor formatting churn during agent fixes without invalidating accepts. The finding-ID is `sha1(file + ":" + line + ":" + kind + ":" + normalized-span)` truncated to 12 hex chars — same shape `tools-audit` uses for stable identity.

When the operator edits a file (adding a comment, reformatting), line numbers shift and prior accepts no longer match. This is the expected behavior — significant file churn deserves a fresh agent review. Cosmetic-only edits (whitespace, comment additions) don't trigger re-scan because the regex pattern still matches and the matched span is normalized; only the line-number change forces a fresh ID.

### KTD-6. Pre-decision fields enforce per-item deliberation on accepts

Mirroring `tools-audit`'s `requiresPreDecisionFields` policy. Every `status: "accepted"` entry must populate:

- `category`: one of `attribution` (the value is intentional author/maintainer attribution), `place_name` (geographic place, not a person), `corporate_name` (company/org, not a person), `documentation_example` (synthetic example data in docs/help text), `api_provider_data` (vendor-published example data from the API's own docs), `synthetic_placeholder` (already a placeholder like `EXAMPLE_*`), `other` (free-form, requires `note` to carry the rationale).
- `evidence_context`: the surrounding 1-2 lines from the source file showing why the category applies. Empty `evidence_context` on an accept is a gate failure.

The third optional field, `note`, is free-form rationale. Required when `category` is `other`.

The pre-decision fields shift acceptance from "agent stamps 'false positive'" to "agent records the specific shape that explains why this isn't real PII." This is the load-bearing primitive against agent reflexive-accept.

### KTD-7. Duplicate-rationale threshold = 5; delta-tracked numeric gate

Mirroring `tools-audit`. If 6+ accepts share a normalized `note` (or the same `category` + `evidence_context` pattern), the run is incomplete. Differentiated rationales survive; bulk paste-the-same-thing-everywhere does not.

Numeric end-state hedge: the binary tracks `findings_count_before` and `findings_count_now` between runs. If `findings_before - findings_now < code_fixes_applied`, the run is suspect — accepts dominated where source fixes should have. The audit emits a one-line delta summary on every run and a `gate_warnings` array in the ledger header.

### KTD-8. Gate semantics: refuse on pending OR enforcement-primitive failure

Promote and publish gates each invoke `pii-audit <target-dir>` directly and read the ledger header. Gate succeeds iff:

1. Ledger contains zero findings with `status: ""` (pending).
2. No `gate_warnings` from enforcement primitives (duplicate-rationale threshold breach, missing pre-decision fields on accepts, delta-tracking suspicion).
3. Ledger is fresh (mtime within the staleness window, default 24h matching `tools-audit`).

A pending finding is the failure state. An accepted finding with valid pre-decision fields is the cleared state. The gate doesn't read individual finding content — it reads the header summary and trusts the agent's per-item work.

### KTD-9. No auto-mutation at the gate; agent makes fixes in source

The agent walks the ledger during polish. For each finding the agent reads the surrounding context and either:

- **Fixes in source** — replaces the real value with a placeholder (`PII_EMAIL_EXAMPLE`, `EXAMPLE_PHONE`, etc.) in the working dir directly. Next audit run won't detect anything at that line; finding auto-disappears from the ledger.
- **Accepts with rationale** — edits the ledger entry to set `status: "accepted"` + the pre-decision fields above.

This eliminates the silent-mutation and operator-bypass failure modes from the prior plan. The audit trail is the ledger itself, which lives in the CLI dir and is visible to PR reviewers.

### KTD-10. No new dependencies — pure stdlib

`regexp`, `bufio`, `os`, `io/fs`, `path/filepath`, `crypto/sha1`, `encoding/hex`, `encoding/json`. Mirrors both `secrets.go` and `tools_audit.go`'s stdlib posture.

---

## Implementation Units

### U1. PII detector library

**Goal:** Build `internal/artifacts/pii.go` exporting detection + ledger I/O helpers. No CLI wiring, no mutation logic.

**Requirements:** R2 (file scoping), R3 (detector families), R6 (ledger identity).

**Dependencies:** none.

**Files:**
- `internal/artifacts/pii.go` (new, ~220 LOC)
- `internal/artifacts/pii_test.go` (new)
- `internal/artifacts/testdata/pii/` (new — fixture files per detector + ledger scenarios)

**Approach:**
- Define `PIIFinding{Kind, File, Line, Column, MatchedSpan, Status, Note, Category, EvidenceContext}` struct. `Status`, `Note`, `Category`, `EvidenceContext` are `omitempty` (ledger-only, agent writes).
- Define `piiDetector{kind, pattern *regexp.Regexp}` — no per-detector `accept` callback; high-precision regexes per KTD-4 carry their own context requirements.
- Implement `FindPII(root string) ([]PIIFinding, error)`:
  - `filepath.WalkDir` with the file-glob set from R2 (see helper `isHighRiskFile`).
  - Per file: same binary-probe + line-oriented scan pattern as `secrets.go:127-172`. Skip binary files via null-byte probe.
  - Per line: each detector's `FindAllStringIndex` produces (match, column) pairs.
  - Sort findings by `(file, line, column, kind)` for stable output.
- Implement finding-ID computation per KTD-5: `sha1(file + ":" + line + ":" + kind + ":" + normalizeSpan(matched))[:12]`. `normalizeSpan` is `strings.ToLower(strings.Join(strings.Fields(s), " "))`.
- Implement ledger I/O mirroring `tools_audit.go:118-123`:
  - `ReadPIILedger(cliDir string) (*PIILedger, error)` — reads `<cliDir>/.printing-press-pii-polish.json`, returns nil on missing file.
  - `WritePIILedger(cliDir string, ledger *PIILedger) error` — atomic write via temp file + rename.
  - `ReconcilePIILedger(previous *PIILedger, findings []PIIFinding) *PIILedger` — merges agent state from previous run onto new findings by identity-key.
- Ledger header fields: `findings_count_before` (sticky), `last_audit_at`, `gate_warnings []string`, `progress.last_processed_finding_id`.
- Enforcement primitive helpers:
  - `validateAcceptedFindings(findings []PIIFinding) []string` — returns gate-warning strings for missing pre-decision fields, empty `evidence_context`, missing `category`.
  - `validateDuplicateRationales(findings []PIIFinding, threshold int) []string` — clusters by normalized note + category, flags clusters above threshold.
  - `validateDeltaProgress(before, now int, fixesApplied int) []string` — hedges agent-bypass via delta math.

**Patterns to follow:**
- [`internal/artifacts/secrets.go`](../../internal/artifacts/secrets.go) for walker + binary-probe + line-scan shape.
- [`internal/cli/tools_audit.go:118-167`](../../internal/cli/tools_audit.go#L118) for ledger struct + reconcile + write semantics.

**Test scenarios:**

*Detector behavior (one fixture file per kind under `testdata/pii/`):*
- Card last-4: `"card ending in 1234"` → flagged; `"* 1234 markdown bullet"` → NOT flagged (no context token).
- Email: `"user@example.com"` → flagged; `"x@y"` (no TLD) → NOT flagged.
- Phone: `"(415) 555-0123"` → flagged; `"version 1.2.3"` → NOT flagged.
- ZIP+4: `"62701-1234"` → flagged. Agent layer judges if it's real PII; the detector emits unconditionally.
- Postal-address: `"1234 MAIN STREET"` → flagged; `"SEE README.MD"` → NOT flagged.

*File scoping:*
- Plant identical patterns in a `.go` (non-test), a `_test.go`, a `.json`, and a `.md` file. Audit emits findings only from `_test.go`, `.json`, `.md`.
- Binary file (planted PNG with embedded text) → skipped via null-byte probe.

*Finding-ID stability:*
- Same matched span at same file + line + kind across two `FindPII` calls → identical 12-char ID.
- Whitespace-only change to the matched span → identical ID (normalization).
- Same span at different line number → different ID.
- Different span at same line → different ID.

*Ledger I/O:*
- `ReadPIILedger` on missing file → nil + nil error.
- `ReconcilePIILedger`: prior finding with `status: "accepted"` whose ID matches current finding → status preserved on output. Prior accepted whose ID doesn't match → dropped (finding was fixed in source). Current pending without prior → appears fresh.
- `WritePIILedger` atomic via temp + rename: simulated mid-write crash leaves the previous ledger intact.

*Enforcement primitives:*
- `validateAcceptedFindings`: accept with missing `category` → returns gate-warning string. Accept with empty `evidence_context` → returns gate-warning string.
- `validateDuplicateRationales`: 5 accepts with the same normalized note → no warning. 6 accepts → returns gate-warning. Different `category` values with same note → not clustered.
- `validateDeltaProgress`: before=10, now=10, fixes=0 → no warning (no change). before=10, now=5, fixes=5 → no warning (all reductions explained). before=10, now=5, fixes=0 → warning (accepts cleared findings that should have been fixed).

**Verification:** `go test ./internal/artifacts/...` passes. `go vet ./...` clean.

---

### U2. `printing-press pii-audit` subcommand

**Goal:** New CLI subcommand that runs `FindPII`, reconciles against the existing ledger, writes the new ledger, and renders a human-readable table (or JSON via `--json`). Exit 0 regardless of findings (diagnostic, not gating — gates check the ledger separately).

**Requirements:** R1, R6, R7.

**Dependencies:** U1.

**Files:**
- `internal/cli/pii_audit.go` (new, ~150 LOC) — mirrors `tools_audit.go` structure but slimmer.
- `internal/cli/pii_audit_test.go` (new) — table-driven tests for the renderer + exit semantics.
- `internal/cli/root.go` (modify — register `newPIIAuditCmd()`).

**Approach:**
- Define `newPIIAuditCmd()` cobra command. Single positional arg: `<cli-dir>`. `--json` flag for machine-readable output.
- Flow mirrors `tools_audit.go:99-127`:
  1. Read previous ledger (`ReadPIILedger`).
  2. Run `FindPII(cliDir)`.
  3. Reconcile findings with previous ledger.
  4. Write the new ledger.
  5. Evaluate enforcement primitives → collect gate warnings.
  6. Render to stdout: human table with `next:` pointer + delta summary, OR JSON when `--json`.
- Command should carry `Annotations: map[string]string{"mcp:read-only": "true"}` since it reads files and writes a status ledger.
- Add `--strict` flag (default false) that exits non-zero when pending findings or gate warnings exist. Used by gates (U3, U4) to detect failure without re-parsing the table. Default remains zero so polish-flow integration matches `tools-audit` posture.

**Patterns to follow:** [`internal/cli/tools_audit.go:81-132`](../../internal/cli/tools_audit.go#L81) for command shape + render + ledger flow. [`internal/cli/public_param_audit.go`](../../internal/cli/public_param_audit.go) for a slimmer reference.

**Test scenarios:**
- Run against a fixture CLI dir with planted real-shaped PII in `.manuscripts/foo.json` → audit emits findings; ledger persists; exit 0.
- Run with `--json` → emits valid JSON array; ledger still persisted.
- Run with `--strict` against fixture with pending findings → exit non-zero.
- Run with `--strict` against fixture with all-accepted findings (pre-decision fields populated) → exit 0.
- Run with `--strict` against fixture with all-accepted findings but missing `category` on one → exit non-zero (gate warning).
- Run twice in succession: second run preserves agent-written accept fields when finding IDs match.
- Run against a clean OpenAPI-spec CLI dir → zero findings; ledger header notes `findings_count_before: 0`.

**Verification:** `go test ./internal/cli/... -run PIIAudit` passes. Smoke: `./printing-press pii-audit ./internal/artifacts/testdata/pii-fixture-cli` produces expected table.

---

### U3. Wire PII gate into promote flow

**Goal:** `pipeline.PromoteWorkingCLI` invokes the audit against the working dir before staging and refuses if the audit's strict-mode exit is non-zero.

**Requirements:** R1.

**Dependencies:** U2.

**Files:**
- `internal/pipeline/lock.go` (modify — add audit invocation between line 231 [`validatePhase5GateForPromote`] and line 259 [`CopyDir`]).
- `internal/pipeline/lock_test.go` (modify — extend existing `TestPromoteWorkingCLI*` tests to assert PII gate behavior; add planted-PII fixtures).

**Approach:**
- After `validatePhase5GateForPromote(workingDir, state)` at line 229, before the staging clean at line 255:
  - Invoke `pii-audit <workingDir> --strict` via `exec.Command` against the same binary that's running. This mirrors how `printing-press-polish` already invokes the binary's own subcommands.
  - On non-zero exit: return wrapped error pointing operators to the ledger file (`<workingDir>/.printing-press-pii-polish.json`) and to the polish skill's pii-polish playbook.
  - On zero exit: continue to `CopyDir` and the existing flow.
- The audit writes its ledger into `workingDir`. The subsequent `CopyDir` carries the ledger into staging — that's intentional. Reviewers seeing the published CLI can read the ledger to understand which findings were accepted and why.
- No signature change to `PromoteWorkingCLI`. No new flag. The flow is: agent runs polish (which includes the pii-polish playbook), polish clears the ledger, promote sees a clean ledger and proceeds.

**Patterns to follow:** `validatePhase5GateForPromote` pre-check pattern at [`internal/pipeline/lock.go:229`](../../internal/pipeline/lock.go#L229).

**Test scenarios:**
- Promote a working dir with planted real-shaped PII and no prior ledger → halts; staging dir not created; error message references ledger path.
- Promote a working dir whose ledger has all findings accepted with valid pre-decision fields → succeeds; staging carries the ledger forward.
- Promote a working dir whose ledger has 6+ accepts with identical rationale → halts (enforcement primitive); error references the duplicate-rationale gate warning.
- Promote a working dir with no PII at all → audit emits no findings; succeeds; ledger is created with `findings_count: 0`.
- Promote a working dir whose ledger is older than the 24h staleness window → audit re-runs (refreshes timestamp), then gate proceeds normally.

**Verification:** `go test ./internal/pipeline/... -run Promote` passes. End-to-end smoke: plant a real-shape email in `<working-dir>/.manuscripts/foo.json`, run `printing-press lock promote --cli foo-pp-cli --dir <path>` → halt with error referencing ledger.

---

### U4. Wire PII gate into publish flow

**Goal:** Run `pii-audit` against the staged outCLIDir at publish, alongside the existing vendor-prefix secret scan. Both must clear independently.

**Requirements:** R8.

**Dependencies:** U2.

**Files:**
- `internal/cli/publish.go` (modify — add audit invocation after the existing `FindVendorPrefixSecrets` call at line 382).
- `internal/cli/publish_test.go` (modify — add publish-with-PII test scenarios).

**Approach:**
- After the existing secret-scan block (line 382-393), run `pii-audit <outCLIDir> --strict`.
- On non-zero exit: combine with any preceding secret-scan findings into a single error message with clear section headers ("Vendor-prefix tokens" + "Customer PII") and call `cleanupOnFailure()` to remove the staged output.
- On zero exit: continue with the existing manuscripts copy + success path.
- The audit scans the manuscripts directory by default (R2 includes `**/.manuscripts/**`). This is intentional — manuscripts are the highest-risk PII source.

**Patterns to follow:** Existing secret-scan call at [`internal/cli/publish.go:382-393`](../../internal/cli/publish.go#L382) — same `cleanupOnFailure()` + `ExitError{Code: ExitPublishError}` shape.

**Test scenarios:**
- Publish a staged dir with PII only (no secrets) → halts; combined error has only "Customer PII" section; staged output cleaned up.
- Publish with secrets only → existing behavior; only "Vendor-prefix tokens" section.
- Publish with both → halts; combined report has both sections; cleanup ran.
- Publish a clean OpenAPI-spec CLI → both scans pass; existing success path unchanged. Covers R4.
- Publish a staged dir whose `.manuscripts/<runID>/` has planted PII → halts (verifies manuscripts are in scan scope per KTD-2).

**Verification:** `go test ./internal/cli/... -run Publish` passes.

---

### U5. Agent playbook for PII polish

**Goal:** New reference document `skills/printing-press-polish/references/pii-polish.md` that walks the agent through the PII ledger — fix in source vs accept with pre-decision fields.

**Requirements:** R6, R7.

**Dependencies:** U2.

**Files:**
- `skills/printing-press-polish/references/pii-polish.md` (new, ~250 lines following the `tools-polish.md` shape).
- `skills/printing-press-polish/SKILL.md` (modify — add a step that invokes `pii-audit` and routes the agent through `pii-polish.md` between the existing tools-polish step and the verify step).

**Approach:** Document the playbook with this structure (matching `tools-polish.md`):
- **Run the audit** — exact command, expected output shape.
- **Per-finding decision tree** — for each detector kind:
  - **Card last-4:** if it looks like a real card number, replace in source with `PII_CARD_LAST4_EXAMPLE`. If it's a documentation example or test fixture for masking display, accept with `category: documentation_example`.
  - **Email:** if a real address, replace with `user@example.com`. If a public support address (`support@vendor.com` documented by the vendor), accept with `category: api_provider_data` + `evidence_context` quoting the vendor's published docs reference.
  - **Phone:** if real, replace with `+1-555-0100` (RFC 3966 fictional). If a vendor support line, accept with `category: api_provider_data`.
  - **ZIP+4:** if part of a real address, replace with `90210-1234`. If a request-ID or batch identifier, accept with `category: other` + note explaining the API field name.
  - **Postal-address line:** if real, replace with `1 EXAMPLE WAY` or similar fictional address. If a documentation/help-text example, accept with `category: documentation_example`.
- **Pre-decision fields** — what each `category` value means and what evidence justifies it.
- **Duplicate rationale rule** — threshold 5; if you find yourself stamping the same note across many findings, you're punting; differentiate or fix in source.
- **DO-NOT-EDIT files** — generated files (carrying the `Generated by CLI Printing Press` header) need a retro filing, not an inline edit. Same pattern as `tools-polish.md`'s DO-NOT-EDIT section.
- **Manuscripts policy** — captured browser-sniff content is the highest-risk PII source. Fix in source means editing the manuscript file directly with a synthetic placeholder. Acceptance for manuscripts requires explicit `evidence_context` showing the value is vendor-documented or a synthetic placeholder.
- **End-state checklist** — `printing-press pii-audit <cli-dir>` returns zero pending; ledger header `gate_warnings` is empty; delta math holds.

**Patterns to follow:** [`skills/printing-press-polish/references/tools-polish.md`](../../skills/printing-press-polish/references/tools-polish.md) — exact section ordering, decision-tree shape, and DO-NOT-EDIT handling.

**Test scenarios:** N/A (markdown reference doc).

**Verification:** Manual review by the implementer. The doc is consumed by the polish skill in subsequent runs — its correctness surfaces through agent-driven polish runs against real CLIs, not via unit tests.

---

### U6. Polish skill integration

**Goal:** Wire `pii-polish.md` into the polish skill's main flow so every polish run includes the PII pass.

**Requirements:** R6.

**Dependencies:** U5.

**Files:**
- `skills/printing-press-polish/SKILL.md` (modify — add a step block that points at `pii-polish.md` between tools-polish and the verify/promote-prep step).

**Approach:**
- Add a step to the polish skill's ordered playbook: after `tools-polish` completes, before any verify/promote step, the agent invokes `pii-audit` and walks `pii-polish.md`. The skill block names the reference file and notes the end-state expectation ("zero pending findings, zero gate warnings").
- The skill block should be terse — the detailed instructions live in the reference file, mirroring how `tools-polish.md` is referenced.

**Patterns to follow:** Whatever pattern SKILL.md already uses to reference `tools-polish.md` — copy the same shape.

**Test scenarios:** N/A (skill markdown).

**Verification:** Read SKILL.md after the edit to confirm the new step lands in the correct sequence and references the new file.

---

### U7. Golden-fixture review

**Goal:** Confirm no existing golden fixtures contain PII shapes the new detectors flag. If any do, sanitize.

**Requirements:** R4 (clean spec passes both gates).

**Dependencies:** U1.

**Files:**
- `testdata/golden/expected/**` (read-only audit; modify only on real findings).
- `scripts/golden.sh verify` output.

**Approach:**
- Run `FindPII` against `testdata/golden/expected/` after U1 lands.
- For each finding: either a real shape needing replacement (sanitize with `PII_*_EXAMPLE` placeholder, update goldens, document the diff in the PR per AGENTS.md golden rule), or a fixture that's intentionally synthetic but matches the regex (e.g., `1234 EXAMPLE STREET`) — adjust the regex or accept as a fixture-level constant.

**Test scenarios:** N/A — verification unit. Test expectation: `scripts/golden.sh verify` passes after any fixture updates this unit makes.

**Verification:** `scripts/golden.sh verify` green; PR description names any fixtures touched and why.

---

## Scope Boundaries

### In scope
- Five detector families: card last-4 with context tokens, email, US phone, ZIP+4, postal-address line.
- File scoping to high-risk paths (R2).
- Ledger + enforcement primitives (pre-decision fields, duplicate-rationale, delta math).
- Promote and publish gate wiring.
- Polish skill integration via `pii-polish.md`.

### Deferred to Follow-Up Work
- **Person-name detection.** Regex precision is unrecoverable; agent walk-through volume is disproportionate. Needs NER (Presidio Go bridge or equivalent). File a follow-up issue when prioritized.
- **Broader file scope** (non-test Go source, generated MCP shims). If a real leak surfaces in those file types, expand the glob set; until then, keep the false-positive surface narrow.
- **Refactor of `secrets.go` and `pii.go` to share scaffold.** Worth doing once a third audit kind appears.
- **JSON-aware parsing** for multi-line pretty-printed values. Current line-oriented scanner misses keys split from values across lines. If real captures use this format, expand to a tokenizer.

### Outside this work's identity (tracked elsewhere)
- **Spec-aware detectors** — order/transaction ID shapes from `auth.id_shapes`, cookie name+value from `auth.cookies`, ASIN-style entity IDs. **Tracked by [#960](https://github.com/mvanhorn/cli-printing-press/issues/960).**
- **Browser-sniff capture sanitization** (preventing PII from being captured in the first place). Adjacent retro work unit.
- **Test-fixture authoring guidance** — **Tracked by WU-3 in [#955](https://github.com/mvanhorn/cli-printing-press/issues/955).** The structural fix that complements the gate.
- **Retroactive library audit** — re-scanning already-published CLIs. Separate one-shot operation; not part of this gate.

---

## System-Wide Impact

- **Generated CLI behavior:** no change. The new gates run on the printing-press binary's promote/publish paths, not in printed-CLI runtime code.
- **Existing published CLIs:** unaffected until next regen → polish → promote cycle.
- **Polish workflow:** adds one new pass between tools-polish and verify. Per-CLI agent walk-through cost depends on finding count — expected 5-30 findings per browser-sniff CLI, 0-3 for OpenAPI-spec CLIs.
- **CI:** publish-time audit adds a few hundred ms walk over staged dir.
- **Operator UX:** new step in the polish skill; agents follow the playbook. No new CLI flags.

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Postal-address regex false positives | Medium | Agent walk-through noise | Context-token requirement (street suffix) in KTD-4; agent layer judges per-item; tighten in U1 against fixtures |
| Detector misses real PII the parent retro found | Medium | The leak this WU exists to prevent | Phase-1 is shape-only; #960 covers spec-aware shapes; WU-3 prevents capture-into-fixture |
| Agent stamps every finding `accepted` with the same note | Medium | Bypass of the gate | Enforcement primitives: pre-decision fields, duplicate-rationale threshold = 5, delta math (KTD-7) |
| Finding-ID instability when operators edit files | Low | Re-walk needed | Line-number shift forces fresh ID by design; cosmetic edits don't shift line numbers and normalization tolerates whitespace |
| `pii-audit` subcommand wires into MCP and gets called by agents at unexpected times | Low | Confusion in audit semantics | `mcp:read-only` annotation; audit exit 0 default (only `--strict` exits non-zero); ledger writes are idempotent |
| Golden fixtures contain shapes the new detectors flag | Medium | U7 fails until sanitized | U7 explicitly audits; documented per AGENTS.md golden-update rule |
| Person-name leaks slip through Phase-1 entirely | Medium | Names alone (no adjacent address) leak | Acknowledged trade-off; KTD-3 documents the deferral; address-proximity caught the leak in #955 |

---

## Verification Strategy

- Unit tests per implementation unit (table-driven, `testify/assert` per AGENTS.md).
- Integration smoke at end of U3 and U4: end-to-end promote/publish against a working dir with planted PII.
- `scripts/golden.sh verify` after U7.
- Manual smoke: replay the amazon-orders-style scenario (planted card last-4 + recipient address + ZIP+4) through `printing-press lock promote` → confirm halt with ledger pointer.
- `go test ./...` clean before PR.
- `go vet ./...` and `golangci-lint run ./...` clean.

---

## Notes for the implementer

- **AGENTS.md "machine vs printed CLI":** machine change. New gates affect every future printed CLI. No printed-CLI template changes.
- **AGENTS.md "Deterministic Inventory + Agent-Marked Ledger" pattern:** the entire design comes from [`docs/PATTERNS.md`](../PATTERNS.md). Read both that pattern and `tools_audit.go` + `tools-polish.md` before coding — they're the closest reference implementations.
- **AGENTS.md "Code & Comment Hygiene":** issue references in PR/commits, not in source.
- **Commit scope:** `cli` (Go binary) for U1-U4, U7; `skills` for U5, U6.
- **Commit type:** `feat` (new subcommand, new gates, new skill content).
- **Test-first execution posture is appropriate here** — the binary side has clean unit-test seams (detector + ledger I/O + enforcement primitives are all pure functions). Write the tests for U1's enforcement primitives before the implementation; the test scenarios in U1 are specific enough to drive TDD.
- **Issue claim is already posted on #958.**
