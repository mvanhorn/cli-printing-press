# Versioning and Release

Normal releases are automated by release-please + goreleaser. The flow:

1. Merge normal feature/fix PRs through the Mergify queue by adding `ready-to-merge` after review and CI pass.
2. release-please opens and updates a release PR with the accumulated changelog.
3. When ready to ship, merge the release PR. It does not need `ready-to-merge`; the Mergify config allows release-please PRs to satisfy merge protection after CI passes.
4. release-please bumps the version files, creates a git tag, opens a GitHub release, and goreleaser builds and attaches cross-platform binaries.
5. The release job verifies that all six platform archives and `checksums.txt` exist and are non-empty. A release page alone does not mean the binaries are available.

## Recovering an incomplete release

If Release Please succeeded but GoReleaser failed, re-run only the failed GoReleaser job from the original Release workflow run. GitHub preserves the successful dependency outputs, including the release tag. Re-running the entire workflow can skip GoReleaser because Release Please already created the release and no longer reports `release_created`.

Check the failed job's annotations and build logs first. Cross-platform builds run one at a time, with `GOMAXPROCS=2` to bound compiler concurrency. GoReleaser has a 35-minute timeout inside the job's 45-minute timeout, leaving time to report errors and finish cleanup. A timeout still needs investigation; increasing the limit alone does not establish why a build stalled.

After recovery, confirm the Release workflow succeeds and all expected assets are available. Do not raise `supported-versions.txt` to that release until its binaries are available. Asset presence is a completeness check; it does not verify download checksums or installation behavior.

Do not manually edit version numbers or release artifacts to bypass this flow. If release behavior changes, update the inline `AGENTS.md` versioning rule in the same PR.
