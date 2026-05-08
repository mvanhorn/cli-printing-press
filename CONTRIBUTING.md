# Contributing

Thanks for helping improve the Printing Press. This repository contains the generator, templates, binary, skills, catalog, tests, and docs that produce printed CLIs. Printed-CLI-only fixes belong in the generated CLI or public library repo, not here.

## Before Opening a PR

- Check for an existing issue or PR that already covers the same work.
- Read [AGENTS.md](AGENTS.md) for the repository conventions, especially the machine vs printed CLI boundary, golden-output rules, commit style, and verification expectations.
- For larger behavior changes, open or comment on an issue first so the scope is clear before implementation.

## Pull Requests

Community PRs must keep and complete the repository PR template. The template is meant to help reviewers see intent, approach, repo fit, risk, output-contract impact, verification, and AI/automation status without reading a file-by-file change log.

Maintainer-owned PRs may use a shorter body. A maintainer-owned PR is one opened by, or explicitly on behalf of, a trusted maintainer account with write/admin access to this repository.

Do not treat GitHub's `CONTRIBUTOR` author association as exempt. Repeat external contributors still use the community PR template unless a maintainer says otherwise. If you are unsure whether a PR is exempt, keep the template.

## AI / Automation Disclosure

The PR template asks for one disclosure choice:

- **No AI or automation was used**: the work was authored manually.
- **Human-reviewed**: AI or automation was used, and a human reviewed the work for intent, fit, and obvious issues before submission. This does not mean a maintainer-level code review or line-by-line diff audit.
- **AI-reviewed only**: an AI agent reviewed the work, but no human reviewed it before submission.
- **Fully automated**: the change was generated and submitted without human review for this specific change.

## Verification

List the commands actually run. If verification was skipped, say why. For changes that affect templates, generated artifacts, command output, manifests, MCP schemas, scorecard output, catalog rendering, or pipeline artifacts, explain the output-contract decision and run `scripts/golden.sh verify` unless the PR explains why golden coverage is not applicable.
