---
name: printing-press-retro
description: >
  Use when the user asks to retro, run a retrospective, file findings,
  or improve the Printing Press after a printed-CLI run. Trigger phrases:
  "retro", "retrospective", "what went wrong", "improve the press",
  "post-mortem", "lessons learned", "what can we improve", "file a retro",
  "submit findings". Do not use for printed-CLI polish or a single-CLI fix.
version: 0.1.0
allowed-tools:
  - Bash
  - Read
  - Glob
  - Grep
  - Write
  - Agent
  - AskUserQuestion
created_by: user
---

# /printing-press-retro

**Result:** A scrubbed retro document and, when the issue gate passes, GitHub
issues for Printing Press defects only.

**Next consumer:** A maintainer or agent who will change the Printing Press
(generator, scorer, skills, or binary) — not the printed CLI that just shipped.

**Done:** The manuscript proof exists and is scrubbed; every work unit is
file-new, comment-on-existing, or local-only; no unredacted secret or PII
remains; the user has the outcome list.

**Intent:** "Raised the floor" is analysis language, not a filing bar. File
only when the Printing Press must change to stop a current, reproducible,
generalizing P1 or P2 defect. Manual iteration on one CLI is expected work,
not a finding. In user-facing output say "the Printing Press"; name the
subsystem when pointing at a fix.

## Interaction Method

When prompting the user with discrete choices, use Claude Code's `AskUserQuestion` tool or Antigravity's `ask_question` tool (`{questions: [{question, options, is_multi_select}]}`). In Codex or non-interactive environments, prompt via stdout/stdin. For open-ended questions in Antigravity, output regular markdown text and end your turn.

## Boundaries

- **Redact** every real secret and PII before quoting. Issue bodies and retro
  docs are public. Use [references/secret-scrubbing.md](references/secret-scrubbing.md)
  Layer 0. Never quote a leaked value as evidence of the leak.
- **Don't change the machine** by default. The burden of proof is on the finding.
- **No P3.** File only P1/P2. Anything else is Skip or Drop, not a low-priority
  issue. The issue gate files P1/P2 Do work units only.
- **Never upload unscrubbed artifacts.** Never modify manuscript or library
  source trees; scrub copies only.

## Authority

Invocation authorizes reading manuscripts/library and writing the retro proof
plus scratch copies. It does not authorize GitHub filing or public upload until
the user confirms Submit. It does not authorize edits to the printed CLI or to
Printing Press source.

## Steps

Read [references/run-resolution.md](references/run-resolution.md) first. It
resolves `$API_NAME`, `$RUN_ID`, `$RUN_DIR`, `$CLI_DIR`, and `$IN_REPO`.

**Never execute a phase from memory. When you enter a phase, Read its file from phases/ first.**

| Step | File |
|---:|---|
| 1 | [phases/01-gather-evidence.md](phases/01-gather-evidence.md) |
| 2 | [phases/02-mine-the-session.md](phases/02-mine-the-session.md) |
| 3 | [phases/03-triage-candidates.md](phases/03-triage-candidates.md) |
| 4 | [phases/04-classify-findings.md](phases/04-classify-findings.md) |
| 5 | [phases/05-prioritize.md](phases/05-prioritize.md) |
| 6 | [phases/06-write-the-retro.md](phases/06-write-the-retro.md) |
| 7 | [phases/07-plannable-work-units.md](phases/07-plannable-work-units.md) |
| 8 | [phases/08-issue-gate.md](phases/08-issue-gate.md) |
| 9 | [phases/09-package-upload-present.md](phases/09-package-upload-present.md) |

Follow each file's final `Next:` pointer. Late procedure lives in the phase
file or a role-named reference it loads
([references/artifact-packaging.md](references/artifact-packaging.md),
[references/issue-template.md](references/issue-template.md),
[references/secret-scrubbing.md](references/secret-scrubbing.md)). Do not
restate those files here.
