# Versioning and Release

Releases are fully automated by release-please + goreleaser; no manual steps. The flow:

1. Merge PRs to `main` with conventional-commit titles.
2. release-please opens and updates a release PR with the accumulated changelog.
3. When ready to ship, merge the release PR. release-please bumps the version files, creates a git tag, opens a GitHub release, and goreleaser builds and attaches cross-platform binaries.

Do not manually edit version numbers or release artifacts to bypass this flow. If release behavior changes, update the inline `AGENTS.md` versioning rule in the same PR.
