# Review Policy

@CODING_STANDARDS.md

This file holds how code review behaves in this repository: what each severity means, what not to flag, and the evidence a finding needs.

## Severity

- **P0**: a supply-chain or credential exposure. Examples: a workflow that runs PR-head code with base secrets, a new `id-token: write`, a Go module-resolution override in a workflow env block, or generator code reading credential paths without a stated reason. Blocks merge.
- **P1**: a behavior bug in emitted or pipeline code. That means a generated CLI that fails to compile, a safety-gate gap, a race or data loss, a dry-run that writes, or a false pass or fail in verify, dogfood, or the scorer. Blocks merge and is fixed in the PR.
- **P2**: a breach of a repository rule (including any rule in CODING_STANDARDS.md), a missing test, an edge case, or an efficiency issue. Fix it or reply with the concrete reason for deferring; a P2 is never silently ignored.

## What not to flag

- **Missing golden or fixture updates** when the changed output is outside the golden artifact set or the fixtures are already committed. Check `docs/GOLDEN.md` and the case's artifact list before flagging.
- **Record-identity fallback order in the OpenAPI parser.** `x-resource-id` wins, then id-shaped keys, then qualified URL-shaped keys, then `name`. Qualified URL-shaped keys ahead of `name` is intended.
- **URL-valued resource IDs reduced to their trailing path segment** in dependent path-param substitution. Child paths must not receive a full URL.
- **`GO-YYYY-NNNN` advisory IDs and CVE IDs in dependency-pin or floor comments.** They tie the floor to a named advisory.

## Evidence a finding needs

- A finding that argues against a stated contract in AGENTS.md, a solution record, or a PR description cites that contract and shows why it is wrong. "This looks inconsistent" is not enough.
- A P1 on emitted code names the generated shape that fails: the spec feature, auth type, or fallback path that triggers it.
