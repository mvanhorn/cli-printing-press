## Phase 5: Dogfood Testing

**MANDATORY when an API key is available. Do NOT skip or shortcut this phase.**

Shipcheck verified commands start and return exit codes. Dogfood verifies the CLI
produces correct, useful output for real workflows. These are different checks.

### Step 1: Ask the user for depth

Present via `AskUserQuestion`:

> "Shipcheck passed. How thoroughly should I test against the live API?"
>
> 1. **Full dogfood (recommended)** — Complete mechanical test matrix across every leaf subcommand, including help, happy-path, JSON parse validation, output-mode fidelity, and error paths. Includes write-side lifecycle only with an approved disposable fixture/sandbox plan.
> 2. **Quick check** — A compromise subset when the user explicitly wants speed or full dogfood would consume unapproved real-world cost/side effects.

**Recommendation rule:** Full dogfood is the default recommendation. Do not downgrade because of ordinary time cost; a few extra minutes is cheap compared with the generation run and the cost of shipping a broken CLI. Recommend Quick only when the user asks for speed or when full live testing would create unapproved real-world cost/side effects (paid credits, outbound messages, public posts, real orders, irreversible deletes, invites, bookings, charges). Potential mutation is not itself a reason to downgrade: if the user approves a test account/workspace/calendar/project or the CLI can create and clean up disposable fixtures, Full dogfood remains recommended.

There is no skip option when an API key is available. Phase 5 auto-skips ONLY
when the API requires auth AND no key is available: display "No API key
available — skipping live dogfood testing. The CLI was verified against exit
codes and dry-run only."

For APIs with `auth.type: none` (or no auth section in the spec), Phase 5
is MANDATORY — the API is freely testable without any credentials. Do not
skip testing just because no API key was detected. No-auth APIs are the
easiest to test and the most embarrassing to ship untested.

**LAN-only no-auth carve-out.** Some no-auth APIs are real hardware or
private-network APIs that are testable only from the user's LAN (SSDP, mDNS,
RFC1918/private hostnames, localhost-shaped appliance endpoints). If Phase 5
cannot reach the hardware because the generation host is not on that LAN, do
not fabricate an API-key skip and do not hand-author `phase5-acceptance.json`.
Ask the user whether to hold the CLI or skip live dogfood and promote anyway.
Only when the user explicitly chooses the skip/promote path, write
`phase5-skip.json` with `skip_reason:
"lan-unreachable-from-generation-host"`, `auth_context.type: "none"`, and
`auth_context.local_network_only: true`.

Do NOT proceed without asking. Do NOT substitute an ad-hoc smoke test. If some commands cannot be exercised because fixture values are missing, classify them as `BLOCKED_FIXTURE` and file/fix the machine gap; do not use that as a reason to recommend Quick.

### Step 2: Run the binary-owned test matrix

**Full dogfood is not a judgment call about "enough."** Run the Printing
Press-owned live matrix so command enumeration, exit-code capture, JSON parsing,
and acceptance-marker writing are deterministic:

```bash
cli-printing-press dogfood --live \
  --dir "$CLI_WORK_DIR" \
  --level full \
  --json \
  --write-acceptance "$PROOFS_DIR/phase5-acceptance.json"
```

Use `--level quick` only when the user selected Quick Check in Step 1.

**Cookie / composed / session-handshake auth.** The runner executes the binary
in a sandboxed HOME with no captured browser session, so a CLI whose auth is a
cookie jar will 401 every command unless you hand it the session. To exercise
the matrix for real, point the CLI's config-override env var (the
`<PREFIX>_CONFIG`-style override, e.g. `<API>_CONFIG`) at a `config.toml` that
carries the captured session, and export it before the `dogfood --live` call:

```bash
<API>_CONFIG="$SESSION_DIR/config.toml" cli-printing-press dogfood --live \
  --dir "$CLI_WORK_DIR" --level full --json \
  --write-acceptance "$PROOFS_DIR/phase5-acceptance.json"
```

If no captured session is available to the harness, the runner does not count
the 401s as failures: it records a clean skip verdict
(`skip-cookie-auth-no-session`) and writes `phase5-skip.json` with
`skip_reason: "cookie-auth-no-harness-session"` (see Step 4). The promote gate
accepts that skip marker, so do not hand-author or fabricate a pass marker for a
cookie-auth CLI you could not exercise.

The live dogfood runner enumerates the CLI's `agent-context` command tree,
runs help, happy-path, JSON-fidelity, and error-path checks where applicable,
captures subprocess exit codes directly without shell pipes, and emits a
structured report with pass/fail/skipped counts. Before running the matrix, it
atomically rebuilds a selected binary that is older than the CLI's Go sources;
binaries at least as new as the sources are reused. Save the JSON report to:

`$PROOFS_DIR/<stamp>-dogfood-results.json`

If the command exits non-zero, inspect the structured failures, fix the CLI, and
rerun live dogfood. The runner writes `phase5-acceptance.json` on every outcome
(`status: "pass"` on success, `status: "fail"` with a `failure_summary` block on
failure), so the [Phase 5.6](20-promote-and-archive.md) gate always has a marker to read. Do not hand-edit
`phase5-acceptance.json`; it must come from the runner.

**Quick check (auto-selected test subset):**
1. `doctor` — auth valid, API reachable.
2. 3-5 list commands — return data, not empty.
3. `sync --full` → data appears in local store.
4. `search "<term from synced data>"` — finds results.
5. One list command with `--json`, `--select <fields>`, `--csv` — all produce correct output.
6. One transcendence command — produces output that relates to the query (not just non-empty: verify relevance by checking output content contains query tokens or expected shape).

**Full dogfood adds to the matrix:**
- Every approved feature in the [Phase 1.5](08-ecosystem-absorb-gate.md) manifest gets a sample invocation with domain-realistic args.
- For every command that takes an arg, one error-path test.
- For every command that supports `--json`, one JSON parse validation.
- For write-side commands (when API key + user consent): create test entity with obviously-test data, verify in subsequent list/get, test one mutation, verify change.

### Context budget for long matrices

A full live matrix on a 35+ command CLI can outgrow a single agent's reliable context
window (~200k tokens). When the testing agent approaches that budget: finish the current
test row, append a handoff entry to `$PROOFS_DIR` (matrix position, failures found so
far, fixes applied, acceptance-marker path), and continue in a fresh agent that reads
only the handoff plus the dogfood results file — not the prior conversation. The
runner's acceptance-marker workflow is unchanged by a handoff: markers are written by
the runner on runner outcomes, never authored by an agent.

### Step 3: Fix issues inline

When a test fails, fix it immediately — do not accumulate failures. Tag each fix:
- **CLI fix** — specific to this printed CLI
- **Printing Press issue** — should be fixed in the Printing Press (note for retro)

### Step 4: Report and gate

Write a structured acceptance report and a machine-readable gate marker. The
JSON marker is **required** — [Phase 5.6](20-promote-and-archive.md) and `publish validate` check for it
before promoting or publishing.

```
Acceptance Report: <api>
  Level: Quick Check / Full Dogfood
  Tests: N/M passed
  Failures:
    - [command]: expected [X], got [Y]
  Fixes applied: K
    - [each fix]
  Printing Press issues: J
    - [each issue for retro]
  Gate: PASS / FAIL
```

**Redact PII while authoring the report.** When live API responses include an
organization or workspace name, user email, assignee/collaborator name, or any
other human-identifying string, describe the result generically instead of
quoting the literal value:
- organization or workspace name -> "the test workspace"
- authenticated user email/name -> "the authenticated viewer"
- assignees or collaborators -> "the highest-loaded assignee" / "the project lead"
- team identifiers such as `ENG` or `T2` are OK when they are structural keys

The [Phase 5.6](20-promote-and-archive.md) manuscript scan and publish-skill PII scan are defense in depth;
keep PII out of the acceptance report from the moment you write it.

**Acceptance threshold:**
- Quick Check: 5/6 core tests must pass. Auth (`doctor`) or sync failure is automatic FAIL.
- Full Dogfood: every mandatory test in the matrix must pass. A single broken flagship feature is automatic FAIL. Auth/sync failures are automatic FAIL.

**Bugs surfaced in Phase 5 must be fixed now, not deferred.** Do not offer the user a "ship as-is and file for v0.2" option when the fix is a 1-3 file edit. Present a "Fix now" (default), "Fix critical only", "Hold (don't ship)" set. Deferring bugs to a v0.2 backlog is an anti-pattern — context is freshest in-session, and a backlog that may never be revisited ships known-broken CLIs.

**Gate = PASS:** proceed to [Phase 5.5](19-polish.md) (Polish).

**Gate = FAIL:** fix issues inline (Step 3) and re-run failing tests, up to
2 fix loops. If the gate still fails after 2 loops, put the CLI on hold:
```bash
"$PRINTING_PRESS_BIN" lock release --cli <api>-pp-cli
```
The working copy remains in `$CLI_WORK_DIR`. Proceed to [Phase 5.6](20-promote-and-archive.md) to archive
manuscripts (archiving still happens on hold). Tag the failure reason in the
acceptance report so the next run can learn from it.

See [references/dogfood-testing.md](../references/dogfood-testing.md) for additional
guidance on common failure patterns and what NOT to test.

Write:

`$PROOFS_DIR/<stamp>-fix-<api>-pp-cli-acceptance.md`

For every outcome (PASS or FAIL), the runner writes:

`$PROOFS_DIR/phase5-acceptance.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "pass",
  "level": "quick|full",
  "matrix_size": 42,
  "tests_passed": 42,
  "tests_failed": 0,
  "auth_context": {
    "type": "none|api_key|bearer_token|cookie|composed|session_handshake",
    "api_key_available": true,
    "browser_session_available": false
  }
}
```

On `Gate: FAIL` the same path is written with `status: "fail"` and a
`failure_summary` block grouping failures by category
(`transport_error` / `http_4xx` / `http_5xx` / `exit_nonzero` /
`output_mismatch` / `other`) plus the list of contributing commands. The
[Phase 5.6](20-promote-and-archive.md) gate routes this marker to the hold path; do not promote.

For `level: "quick"`, `tests_failed` may be `1` only when the Quick Check
threshold still passed (`matrix_size: 6`, `tests_passed >= 5`) and the miss was
not auth or sync related. For `level: "full"`, `tests_failed` must be `0`.

If Phase 5 is legitimately skipped because the API requires API-key or bearer
auth and no credential was available, write:

`$PROOFS_DIR/phase5-skip.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "skip",
  "level": "none",
  "skip_reason": "auth_required_no_credential",
  "auth_context": {
    "type": "api_key|bearer_token|oauth2",
    "api_key_available": false,
    "browser_session_available": false
  }
}
```

If Phase 5 is legitimately skipped because a no-auth API is LAN-only and the
generation host cannot reach the user's LAN hardware, write:

`$PROOFS_DIR/phase5-skip.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "skip",
  "level": "none",
  "skip_reason": "lan-unreachable-from-generation-host",
  "auth_context": {
    "type": "none",
    "api_key_available": false,
    "browser_session_available": false,
    "local_network_only": true
  }
}
```

If a cookie / composed / session-handshake CLI could not be exercised because
no captured session was available to the harness (the sandboxed HOME has no
cookie jar, so every command 401s), the runner writes this skip marker for you
on a `skip-cookie-auth-no-session` verdict — do not hand-author it:

`$PROOFS_DIR/phase5-skip.json`

```json
{
  "schema_version": 1,
  "api_name": "<api>",
  "run_id": "<run-id>",
  "status": "skip",
  "level": "none",
  "skip_reason": "cookie-auth-no-harness-session",
  "auth_context": {
    "type": "cookie|composed|session_handshake",
    "browser_session_available": false
  }
}
```

Prefer the inject-session path in Step 2 (pass the captured session via the
`<API>_CONFIG` override) so the matrix runs for real; the skip is the floor when
no session is available.

Do **not** write a skip marker for ordinary `auth.type: none` cloud/public APIs.
No-auth APIs are testable and require `phase5-acceptance.json` unless they match
the LAN-only carve-out above. Do **not** use missing API key
(`auth_required_no_credential`) as the skip reason for cookie, composed, or
session-handshake auth; inject the session, or rely on the runner-emitted
`cookie-auth-no-harness-session` skip when none is available.


Next: phases/19-polish.md
