# Versioning and Release

Releases are fully automated by release-please + goreleaser; no manual steps. The flow:

1. Merge normal feature/fix PRs through the Mergify queue by adding `ready-to-merge` after review and CI pass.
2. release-please opens and updates a release PR with the accumulated changelog.
3. When ready to ship, merge the release PR. It does not need `ready-to-merge`; the Mergify config allows release-please PRs to satisfy merge protection after CI passes.
4. release-please bumps the version files, creates a git tag, opens a GitHub release, and goreleaser builds and attaches cross-platform binaries.

Do not manually edit version numbers or release artifacts to bypass this flow. If release behavior changes, update the inline `AGENTS.md` versioning rule in the same PR.
