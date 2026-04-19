---
title: "feat(cli): unified auth manager for api-key credentials"
type: feat
status: active
date: 2026-04-19
---

# feat(cli): unified auth manager for api-key credentials

## Overview

Add a `printing-press auth` subcommand group that manages api-key credentials for every printed CLI in one place. Today users juggle 15+ env vars (`ESPN_KEY`, `HUBSPOT_ACCESS_TOKEN`, `DUB_TOKEN`, `LINEAR_API_KEY`, `CAL_COM_TOKEN`, `KALSHI_API_KEY`, etc.) by hand with no visibility, no doctor, and no shared store. This plan ships the minimum viable unified auth: a file at `~/.pp/credentials.json`, a tty-only capture flow, a doctor that detects env-vs-store divergence, and a generator-reserved `authload.Get` helper that new printed CLIs call from their HTTP client.

OAuth is explicitly out of scope for this plan. No current library CLI uses OAuth. When one does, a separate plan will design the loopback flow against a real provider, not three hypothetical ones.

## Problem Frame

Every printed CLI in the library reads its own env var: `ESPN_KEY` for ESPN, `HUBSPOT_ACCESS_TOKEN` for HubSpot, `DUB_TOKEN` for Dub, `LINEAR_API_KEY` for Linear, `CAL_COM_TOKEN` for Cal.com, `KALSHI_API_KEY` for Kalshi, `FLIGHTGOAT_API_KEY_AUTH` for FlightGoat, and so on. The onboarding path is: get a token from the vendor's dashboard, paste an `export` line into `.zshrc` or a project `.env`, repeat for each CLI. There is no visibility into what is linked, no expiry awareness, no `doctor` command, and no way to know a token has gone stale except by seeing 401s.

`/ppl` already handles discovery, install, and routing. The remaining gap is credentials. That is what this plan closes.

## Requirements Trace

- R1. One command flow (`printing-press auth link <api>`) that captures an api key via a tty-only prompt and stores it.
- R2. Centralized store at `~/.pp/credentials.json` with 0600 perms, 0700 parent dir, atomic writes.
- R3. Printed CLIs keep their existing env-var-first behaviour. The store is additive and never silently overrides a set env var.
- R4. `auth doctor` surfaces env-vs-store divergence, permission drift, and missing credentials so stale tokens are visible.
- R5. An `authload.Get` helper that returns the token in-process (no `os.Setenv`). Generator emits one call in new printed CLIs' HTTP client construction.
- R6. `auth list`, `auth remove`, `auth fix-perms` round out the surface.

## Scope Boundaries

In scope:
- `printing-press auth link`, `auth list`, `auth remove`, `auth doctor`, `auth fix-perms`.
- File-backed credential store at `~/.pp/credentials.json`.
- `cliutil/authload` package with `Get(slug)`.
- Generator template change so new printed CLIs call `authload.Get` in HTTP client construction.

### Deferred to Separate Tasks

- OAuth loopback flow, PKCE, state, provider registry. No current library CLI uses OAuth; when one does, that is its own plan.
- OS keychain backends (macOS Keychain, Linux Secret Service, Windows Credential Manager). File store covers P1; keychain is a follow-up plan.
- Subprocess env-scrub helper (`SafeExec`). No printed CLI shells out to secret-carrying subprocesses today. If one appears, handle it then.
- HMAC per-entry integrity. Same-UID read access to the store is the threat model; HMAC with a same-directory key does not raise the bar.
- Single-writer lock file. Last-write-wins is acceptable for a file a single user rarely touches from two places simultaneously. Revisit if contention shows up.
- Scorer dimension enforcing `authload.Get` usage. Template emits it; enforcement machinery is premature.
- Shell-file origin scan for divergence attribution. Brittle; `auth doctor` just says "env var is set in current shell" and moves on.

## Context & Research

### Relevant code and patterns

- `internal/megamcp/auth.go` - existing env-var-to-header expansion via `ApplyAuthFormat`, `BuildAuthHeader`, `hasAuthConfigured`. Read path stays compatible; the store supplies values when env vars are unset.
- `internal/megamcp/manifest.go` - `ToolsManifest.Auth` declares type, env vars, format string. The store indexes by API slug; printed CLIs map their slug to the primary env var they already know.
- `internal/cliutil/` - generator-reserved package shipped into every printed CLI. `authload` lands here next to `FanoutRun` and `CleanText`.
- `internal/pipeline/toolsmanifest.go` - generator side that knows each API's primary env var name. Template emits one `authload.Get` call in HTTP client construction for new printed CLIs.
- `printing-press-library/registry.json` - declares `auth_type` and `env_vars` per CLI. `auth doctor` and `auth list` join against it to know which APIs need credentials.
- `internal/cli/` - existing Cobra wiring for other subcommand groups.

### Institutional learnings

- AGENTS.md machine-vs-printed rule: the generator template change here affects new printed CLIs going forward. Already-printed CLIs continue to work unchanged because env-var-first is preserved.
- PP exit-code contract (0/2/3/4/5/7) applies to every auth subcommand. 4 = auth error, 5 = API/filesystem error, 2 = usage.
- README "Dual interface from one spec": the store can be read by megamcp and by future surfaces (trigger daemon if it ever lands) without duplicating logic.

### External references

- None needed. This is a straightforward file-backed credential store with tty capture.

## Key Technical Decisions

KTD-1. Landing repo is `cli-printing-press`. Auth is a machine capability; generator templates need the helper available in every new printed CLI.

KTD-2. Credential resolution order (read path), explicit and stable:
```
hasCred(api) =
  os.Getenv(primaryEnvVar)      # existing behaviour, unchanged
    || authstore.Read(api)       # shared file at ~/.pp/credentials.json
    || nil
```
Env-var-wins preserves every already-printed CLI's behaviour without template changes. The store is additive. The silent-stale-env-var class of bugs is addressed by KTD-4.

KTD-3. `authload.Get(slug) -> (string, error)` returns the token for in-process use directly in an HTTP header. No `os.Setenv`. Rationale: `os.Setenv` inherits into subprocesses, shows up in `/proc/<pid>/environ`, and survives into core dumps. An in-process return value closes those paths for new printed CLIs at zero cost. Already-printed CLIs keep their existing env-var reads and are unaffected.

KTD-4. Env-var-wins is preserved (KTD-2), AND divergence is first-class in the tooling:
- `auth doctor` detects "env var and store both present, different values" as a yellow finding.
- `auth link` post-success prints a warning if the corresponding env var is set in the current shell.
- `auth list` adds a `shadowed_by_env` column.

Rationale: silent stale env vars are the most common failure mode after a fresh `auth link`. Without surfacing divergence, users see "linked" but get 401s and attribute them to the API.

KTD-5. P1 ships api-key capture only. Tty-only prompt, echo disabled, argv-supplied tokens refused. OAuth is a separate plan triggered when a real library CLI needs it.

KTD-6. File store with 0600 perms and atomic `O_EXCL` tempfile + rename. No HMAC, no lock file. Threat model: same-UID read-access defeats any on-disk mitigation short of a real keychain, and a keychain backend is its own plan. Atomic writes prevent torn files. Last-write-wins is acceptable for a single-user store.

## Open Questions

### Resolved During Planning

- Q: Ship OAuth in this plan? A: No. KTD-5. Library is api-key. OAuth gets its own plan when a library CLI needs it.
- Q: HMAC integrity on entries? A: No. KTD-6. Integrity theater given the threat model.
- Q: Keychain backend in P1? A: No. Separate plan.
- Q: Preserve env-var-wins or flip to store-wins? A: Preserve. KTD-2 + KTD-4.
- Q: Ship `SafeExec` subprocess guardrail? A: No. No CLI shells out to secret-carrying subprocesses today.

### Deferred to Implementation

- Exact column widths for `auth list` TTY output. Decide at first render against a real library.
- Whether `auth doctor` probes live API health. Default offline-only; a `--network` flag can add online checks later if wanted.

## High-Level Technical Design

> This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.

### Command surface

```
printing-press auth
  link <api>          # tty-only api-key prompt, writes to store
  list [--api X]      # linked creds, shadowed_by_env column
  remove <api>        # delete the entry
  doctor              # env divergence, perms, missing credentials
  fix-perms           # restore 0600 / 0700 across ~/.pp
```

### Store layout

```
~/.pp/
  credentials.json    # 0600; JSON
```

Credential entry shape (directional, not implementation):

```
{
  "schema_version": 1,
  "credentials": {
    "hubspot": {
      "type": "api_key",
      "token": "...",
      "linked_at": "2026-04-19T12:05:00Z"
    },
    "linear": {
      "type": "api_key",
      "token": "...",
      "linked_at": "2026-04-19T12:10:00Z"
    }
  }
}
```

`schema_version` is present from day one so future additive fields (scopes, expires_at when OAuth lands) can ship without a migration.

## Implementation Units

- [ ] Unit 1: authstore file backend + authload.Get

  Goal: Ship the on-disk store and the generator-reserved helper that printed CLIs consume.

  Requirements: R2, R3, R5

  Dependencies: None.

  Files:
  - Create: `internal/authstore/store.go` (Read, Write, Delete, List).
  - Create: `internal/authstore/file.go` (file IO with O_EXCL + rename, perm enforcement).
  - Create: `internal/cliutil/authload/authload.go` (`Get(slug)` resolving env-var then store per KTD-2).
  - Test: `internal/authstore/store_test.go`, `file_test.go`, `internal/cliutil/authload/authload_test.go`.

  Approach:
  - Parent dir `~/.pp/` created with 0700 on first write. `credentials.json` written 0600 via `O_EXCL` tempfile + rename.
  - Store JSON is versioned with `schema_version: 1` so additive fields land cleanly later.
  - Missing store file is not an error for read callers; `Get` returns "not found" cleanly.
  - `Get(slug)`: read `os.Getenv(primaryEnvVar)` first, fall back to store, return `(token, nil)` or `("", ErrNotFound)`.

  Execution note: test-first. Permission bits and atomic-write discipline are easier to verify upfront than retrofit.

  Patterns to follow: `internal/cliutil/` package conventions for generator-reserved helpers.

  Test scenarios:
  - Happy path: Write then Read round-trips a single slug.
  - Happy path: fresh install creates `~/.pp/` with 0700 and `credentials.json` with 0600.
  - Happy path: `Get` with env var set returns the env value without touching the store file.
  - Happy path: `Get` with env var unset and store entry present returns the stored token.
  - Edge case: missing parent dir is created atomically with 0700.
  - Edge case: missing store file causes `Get` to return `ErrNotFound`, not an error.
  - Edge case: corrupted JSON returns a typed error; file is not overwritten.
  - Error path: write fails mid-rename; original file is intact; tempfile is cleaned up.
  - Error path: `credentials.json` exists with 0644 perms; Write refuses with a typed "unsafe perms" error suggesting `auth fix-perms`.

  Verification: `authload.Get("hubspot")` reads a token from `~/.pp/credentials.json` on a fresh machine and returns it without populating `os.Environ()`.

- [ ] Unit 2: auth link, auth list, auth remove

  Goal: Ship the capture flow and read-only inspection commands.

  Requirements: R1, R6

  Dependencies: Unit 1.

  Files:
  - Create: `internal/cli/auth_cmd.go`.
  - Create: `internal/authstore/prompt.go` (tty-only api-key prompt with echo disabled).
  - Modify: `cmd/printing-press/main.go` to register the `auth` subcommand group.
  - Test: `internal/cli/auth_cmd_test.go`, `internal/authstore/prompt_test.go`.

  Approach:
  - `auth link <api>`: resolve the API slug against the library registry (`printing-press-library/registry.json`) to confirm it is a known CLI. Reject unknown slugs with exit 3 and a "did you mean" list. Prompt for the api key via tty with echo disabled. Write to the store.
  - `auth link --from-stdin <api>`: accept the token on stdin for CI use. Fail if stdin is a tty.
  - Reject `--token <value>` argv flag with a named error to prevent secrets in shell history.
  - `auth list`: read the store, join against the registry, print table in TTY and JSON when piped. Columns: slug, type, linked_at, shadowed_by_env.
  - `auth remove <api>`: delete the entry. Exit 0 even if the entry did not exist (idempotent).
  - Post-success on `auth link`: if the corresponding env var is set in the current shell, print a warning with `unset <VAR>` guidance (KTD-4).

  Patterns to follow: existing Cobra subcommand style under `internal/cli/`.

  Test scenarios:
  - Happy path: `auth link hubspot` with a simulated tty captures the token and writes it to the store.
  - Happy path: `auth link --from-stdin hubspot < token.txt` works non-interactively.
  - Happy path: `auth list --json` auto-triggers when stdout is piped.
  - Happy path: `auth remove hubspot` removes the entry and reports success.
  - Edge case: `auth link hubspot` with non-tty stdin and no `--from-stdin` refuses with exit 2.
  - Edge case: `auth link hubspot --token abc` is refused with a named error.
  - Edge case: `auth link unknown-api` returns exit 3 with suggestions from the registry.
  - Edge case: `auth link hubspot` succeeds while `HUBSPOT_ACCESS_TOKEN` is set in the env: entry is stored AND a warning names the env var.
  - Edge case: `auth remove hubspot` on a missing entry returns exit 0.
  - Error path: `auth list` with a missing store file returns exit 0 and empty output.

  Verification: on a fresh machine, `auth link hubspot && hubspot-pp-cli contacts list` succeeds without the user setting `HUBSPOT_ACCESS_TOKEN` (once the generator template change in Unit 4 has been applied to the HubSpot printed CLI).

- [ ] Unit 3: auth doctor + auth fix-perms

  Goal: Ship the diagnostic surface.

  Requirements: R4, R6

  Dependencies: Unit 1, Unit 2.

  Files:
  - Create: `internal/authstore/doctor.go`.
  - Create: `internal/cli/auth_doctor_cmd.go` (or extend `auth_cmd.go`).
  - Test: `internal/authstore/doctor_test.go`.

  Approach:
  - For each slug in `printing-press-library/registry.json`, doctor checks:
    - store entry present?
    - primary env var set?
    - if both, do values match (compare by SHA-256 to avoid logging secrets)?
    - `credentials.json` perms are 0600?
    - `~/.pp/` perms are 0700?
  - Output: traffic-light table (green / yellow / red). Yellow findings are informational; red findings highlight actionable fixes.
  - `auth fix-perms`: chmod `~/.pp/` to 0700 and `credentials.json` to 0600. Idempotent. Reports what changed.
  - `auth doctor --json` produces machine-readable output for agent consumption.

  Patterns to follow: `doctor` subcommands inside printed CLIs.

  Test scenarios:
  - Happy path: green for a slug with store entry, env unset.
  - Happy path: green for a slug with env set, store unset (current behaviour, still works).
  - Yellow finding: env and store both present with identical values (info: "store unused while env is set").
  - Yellow finding: env and store both present with DIFFERENT values (flag as divergence).
  - Red finding: `credentials.json` is 0644; doctor suggests `auth fix-perms`.
  - Red finding: `~/.pp/` is 0755; doctor suggests `auth fix-perms`.
  - Red finding: slug in registry has neither env nor store entry (not linked).
  - Happy path: `auth fix-perms` on a seeded wrong-perm setup restores 0700/0600 and reports both changes.
  - Integration: `auth doctor --json` returns a schema suitable for agent parsing.

  Verification: doctor classifies a seeded set of 5 slugs covering all finding categories in one pass.

- [ ] Unit 4: Generator template change + docs

  Goal: New printed CLIs call `authload.Get` in HTTP client construction. Documentation updated.

  Requirements: R3, R5

  Dependencies: Unit 1.

  Files:
  - Modify: generator HTTP client template under `internal/generator/` (or wherever the HTTP client template lives).
  - Modify: `README.md` to add a short "Auth" section.
  - Modify: `AGENTS.md` glossary to add `authstore`, `authload`, and `~/.pp/` layout.
  - Modify: `ONBOARDING.md` to reference `printing-press auth link` as the setup path.
  - Test: generator template snapshot test updated.

  Approach:
  - Template emits one call site per printed CLI: HTTP client reads its token via `authload.Get("<slug>")` and places it in the outbound header per the manifest's auth format. Env var fallback is inside `Get` (KTD-2), so the template has no conditional.
  - Already-printed CLIs are untouched. Regenerating a CLI picks up the new pattern.
  - Documentation states the env-var-wins precedence clearly.

  Patterns to follow: existing HTTP client template.

  Test scenarios:
  - Happy path: a freshly generated CLI contains the `authload.Get` import and call in the expected file.
  - Integration: regenerating one catalog API produces a CLI that imports `authload`, calls `Get`, and successfully reads a token from the store at runtime.

  Verification: a regenerated library CLI makes an authenticated API call reading from `~/.pp/credentials.json` without the user exporting the env var.

## System-Wide Impact

- Interaction graph: `internal/authstore` is a new leaf package. `cliutil/authload` sits between printed CLIs and the store. Megamcp's existing auth path is unchanged and continues to use env vars plus manifest format strings; if megamcp later wants store fallback, it can import `authload.Get` with no other changes.
- Error propagation: all auth errors map to exit 4. Store-integrity and filesystem errors map to exit 5. Usage errors (argv-supplied token, non-tty without `--from-stdin`, unknown slug) map to exit 2 or 3.
- State lifecycle risks: one new file at `~/.pp/credentials.json`. Atomic writes prevent torn files. Perm enforcement on write catches drift early.
- API surface parity: no MCP-side equivalents in this plan. If megamcp-side `auth_status` / `auth_list` meta-tools are wanted, they are a trivial follow-up since the store read path is already there.
- Integration coverage: one cross-process integration test is required: super-CLI... no, there is no super-CLI. Just: `auth link` writes, `hubspot-pp-cli contacts list` reads via `authload.Get` and succeeds.
- Unchanged invariants: every already-printed CLI keeps reading env vars as its primary credential source. Env-var-wins precedence is preserved. The library registry stays authoritative for which slugs exist.

## Risks & Dependencies

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Stale env var silently shadows fresh `auth link` token | High | Med | KTD-4: `auth link` post-success warning when env var is set; `auth list` shadow column; `auth doctor` flags divergence |
| Store file is exfiltrated via same-UID process (editor extension, postinstall script) | Med | High | Documented scope: same-UID read is the threat model a file store does not defend against; keychain backend is a follow-up plan |
| Store file is backed up via Time Machine / iCloud / Dropbox | Med | Med | Document the exclusion procedure in README; no `.nosync` marker, which is folklore |
| Generator template change lands without regenerating existing library CLIs | Low | Low | Already-printed CLIs keep working via env vars; regeneration is opportunistic, not forced |
| Users expect OAuth and file issues | Med | Low | README states clearly that OAuth is a follow-up plan; scope boundary explicit |
| Two `auth link` invocations race and corrupt the store | Low | Low | Atomic rename prevents torn files; last-write-wins is acceptable for a single-user store with low contention; add a lock later if real-world contention appears |

## Documentation / Operational Notes

- Changelog scope `cli` for every unit in this plan.
- No runtime migration. Store is created lazily on first `auth link`. Users who keep using env vars see zero change.
- No feature flag. Single-binary release.
- README gets a short "Auth" section near setup. ONBOARDING.md points new users at `auth link` as the preferred path.

## Sources & References

- Related code: `internal/megamcp/auth.go`, `internal/megamcp/manifest.go`, `internal/cliutil/`, `internal/pipeline/toolsmanifest.go`, `internal/generator/`, `internal/cli/`.
- Repo conventions: `AGENTS.md` (machine-vs-printed rule, glossary, commit style), `README.md` ("Absorb and Transcend" philosophy).
- Killed-sibling plan: the super-CLI plan (`docs/plans/2026-04-19-002-feat-super-cli-run-namespace-plan.md`) was written alongside this one but retired because `/ppl` already provides discovery, install, and routing. This plan stands alone.
