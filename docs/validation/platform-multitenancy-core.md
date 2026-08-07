# Platform multitenancy generator validation

Issue: `mvanhorn/cli-printing-press#3678`

This change implements rollout stages 1–2 from the approved Phase B plan: the
shared contract/conformance harness and generator support consumed by the
four-source catalog canary. It does not claim the tenant-bearing fleet,
command-level output semantics, or fleet activation stages are complete.
Phase C has not started.

## Broken-first proof

Command, run against `origin/main` (`f000ba4c`) with only the contract test
added:

```text
go test ./internal/generator -run TestGeneratedPlatformRuntimeContract -count=1
```

Exact output:

```text
info: learn: defaulting self-learning loop on (opt out with learn.disabled: true)
WARNING: spec.Printer is empty; README printer attribution will be omitted. Set `git config github.user` (your GitHub @handle) to populate this correctly before publishing.
--- FAIL: TestGeneratedPlatformRuntimeContract (0.12s)
    platform_runtime_test.go:30:
        Error Trace:    /private/tmp/pp-platform-red-20260719/internal/generator/platform_runtime_test.go:30
        Error:          Received unexpected error:
                        stat /var/folders/0d/dmb3g7z57b1550p5zzzh32c00000gn/T/TestGeneratedPlatformRuntimeContract67633073/001/platform-runtime-pp-cli/internal/platform/profile.go: no such file or directory
        Test:           TestGeneratedPlatformRuntimeContract
        Messages:       generated platform runtime must include profile.go
FAIL
FAIL    github.com/mvanhorn/cli-printing-press/v4/internal/generator    0.519s
FAIL
```

## Passing proof

The exact same command after the implementation:

```text
ok      github.com/mvanhorn/cli-printing-press/v4/internal/generator    3.517s
```

The generated contract supplies strict cross-source profiles, exact external
references, fail-closed identity gates, isolated paths, HMAC credential
fingerprints, tenant metadata, resource-scoped caches, doctor v2, shared
rate-limit semantics, receipts and audit indexes, output-metadata primitives,
MCP profile binding, and black-box command/gate tests. Generated store tests
also preserve both concurrent fresh-open retry and fast future-schema refusal.

Window/truncation and silent-analytics helpers are primitives at this stage;
adoption by every existing command remains rollout stage 5.

## Gorgias documented identity contract

Live canary validation found that Gorgias's documented `GET /account` response
contains the tenant `domain` but no immutable account ID. Generated platform
contracts now require the canonical `expected_base_url` for Gorgias and reject
`expected_account_id` rather than depending on undocumented provider metadata.
The generated conformance suite proves canonical-domain mismatch remains a
fail-closed tenant error.

## Review hardening

Greptile's review found three blocking generator issues. Successful mutations
now evict every potentially related HTTP projection inside only the current
API or profile/source cache in both legacy and tenant-gated modes, while
non-cache state remains untouched. The generated `whoami` command is attached
only when a published CLI registers a tenant adapter, and runtime attachment
preserves an API-owned `whoami` resource because adapter presence cannot be
inferred safely from the source specification.

## Local validation matrix

| Check | Result |
|---|---|
| Broken-first contract command, repeated after fix | PASS |
| Generated platform and CLI conformance suites | PASS |
| Focused manifest and MCP-sync profile-only auth tests | PASS |
| Generated store contention and future-schema tests | PASS |
| `bash scripts/golden.sh verify` after refreshing intentional generated-output fixtures | PASS; 32 cases |
| `go test ./...` | PASS; generator package 301.517s |
| `go test -race ./internal/generator ./internal/pipeline ./internal/spec ./internal/pipeline/mcpsync` | PASS; generator package 478.191s |
| `go vet ./...` and `go build ./...` | PASS |
| `govulncheck ./...` | PASS; zero reachable vulnerabilities |

The same matrix is copied into the PR body.
