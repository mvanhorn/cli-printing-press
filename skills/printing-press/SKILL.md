---
name: printing-press
description: Set up a new integration, connector, or CLI binding for any API. Wrap or generate a ship-ready Go CLI from an OpenAPI, HAR, or Postman spec via the lean research -> generate -> build -> shipcheck loop. Use when the user says build a CLI, wrap this API, set up a new integration, add a connector, integrate with a service, or names an API by domain.
version: 2.0.0
min-binary-version: "4.0.0"
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - WebFetch
  - WebSearch
  - AskUserQuestion
  - Agent
created_by: user
---

# /printing-press

**Result:** A working Go CLI in `$PRESS_LIBRARY/<api-slug>/`, plus up to five dense
manuscripts archived to `$PRESS_MANUSCRIPTS/<api-slug>/<run-id>/`: research brief,
absorb manifest, build log, shipcheck proof, and live smoke proof when live testing ran.

**Next consumer:** The user, who runs the CLI or ships it, and the sibling skills that
take it from here: `printing-press-polish` for a second pass, `printing-press-publish`
for the public library, `printing-press-retro` for findings against the Press itself.

**Done:** Every feature approved at the absorb gate is implemented, shipcheck reached
ship, the live dogfood matrix ran against real targets, the CLI is promoted, and the
receipt ledger closes on the last phase.

**Intent:** The best useful CLI for an API without an hour of phase theater. Optimize
for time-to-ship, not time-to-document: reuse prior research when it is already good
enough, run the cheap high-signal checks early, fix blockers before polish.

## Boundaries

- **Never leak a secret.** API key **values**, token **values**, passwords, and session
  cookies must never reach source, manuscripts, proofs, READMEs, HARs, receipts, or
  anything committed. Env var **names** and placeholders are safe. Phase 5.6 and publish
  apply [references/secret-protection.md](references/secret-protection.md).
- **Never ship untested.** `go build` and `verify` pass-rate are structural signals, not
  correctness signals. A CLI that has not been through the live matrix in
  [phases/18-dogfood-testing.md](phases/18-dogfood-testing.md) is not shippable, and a
  bug a 1-3 file edit resolves is fixed now, not filed for v0.2.
- **Never quote human-time estimates** for sub-tasks ("~15-30 min", "quick fix"). The
  agent does the work, not the user; describe scope instead. The carve-outs are the
  genuinely time-bound: the whole run (30-60 minutes), tool installs, and the
  network-bound printing-press subcommands.
- **Never sequence from memory.** The receipt ledger decides which phase comes next; see
  [references/phase-receipts.md](references/phase-receipts.md).

## Authority

Invocation authorizes reading and writing under `$PRESS_RUNSTATE/`, `$PRESS_LIBRARY/`,
and `$PRESS_MANUSCRIPTS/`, running the printing-press binary and the Go toolchain, and
researching the API on the open web. It does not authorize publishing to the public
library, opening a pull request, or writing anywhere else on the user's machine.

## Steps

Enter [phases/01-preflight.md](phases/01-preflight.md) before any user-facing prompt,
then read [references/run-resolution.md](references/run-resolution.md): it resolves the
API target, the briefing context, and the source priority for combo runs.

**Never execute a phase from memory. When you enter a phase, Read its file from phases/ first.**

| Step | File |
|---:|---|
| 1 | [phases/01-preflight.md](phases/01-preflight.md) |
| 2 | [phases/02-run-initialization.md](phases/02-run-initialization.md) |
| 3 | [phases/03-resolve-and-reuse.md](phases/03-resolve-and-reuse.md) |
| 4 | [phases/04-research-brief.md](phases/04-research-brief.md) |
| 5 | [phases/05-pre-browser-sniff-auth-intelligence.md](phases/05-pre-browser-sniff-auth-intelligence.md) |
| 6 | [phases/06-browser-sniff-gate.md](phases/06-browser-sniff-gate.md) |
| 7 | [phases/07-crowd-sniff-gate.md](phases/07-crowd-sniff-gate.md) |
| 8 | [phases/08-ecosystem-absorb-gate.md](phases/08-ecosystem-absorb-gate.md) |
| 9 | [phases/09-api-reachability-gate.md](phases/09-api-reachability-gate.md) |
| 10 | [phases/10-generate.md](phases/10-generate.md) |
| 11 | [phases/11-build-the-goat.md](phases/11-build-the-goat.md) |
| 12 | [phases/12-shipcheck.md](phases/12-shipcheck.md) |
| 13 | [phases/13-sync-param-drop-gate.md](phases/13-sync-param-drop-gate.md) |
| 14 | [phases/14-agentic-skill-review.md](phases/14-agentic-skill-review.md) |
| 15 | [phases/15-readme-skill-agents-correctness-audit.md](phases/15-readme-skill-agents-correctness-audit.md) |
| 16 | [phases/16-agentic-output-review.md](phases/16-agentic-output-review.md) |
| 17 | [phases/17-local-code-review.md](phases/17-local-code-review.md) |
| 18 | [phases/18-dogfood-testing.md](phases/18-dogfood-testing.md) |
| 19 | [phases/19-polish.md](phases/19-polish.md) |
| 20 | [phases/20-promote-and-archive.md](phases/20-promote-and-archive.md) |
| 21 | [phases/21-next-steps.md](phases/21-next-steps.md) |

Follow each file's final `Next:` pointer, and record the receipt handoff each phase
names. Late procedure lives in the phase file or a role-named reference it loads
([references/phase-receipts.md](references/phase-receipts.md), [references/codex-delegation.md](references/codex-delegation.md),
[references/fetch-docs.md](references/fetch-docs.md)). Do not restate those files here.
