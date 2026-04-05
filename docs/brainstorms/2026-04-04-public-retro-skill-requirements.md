---
date: 2026-04-04
topic: public-retro-skill
---

# Public Retro Skill

## Problem Frame

The `/printing-press-retro` skill captures high-value machine improvement signals -- bugs, template gaps, recurring friction, scorer issues -- but it's private (`.claude/skills/`). Only repo maintainers can run it. External users who generate CLIs via the plugin discover the same issues but have no structured way to feed those findings back. Making the skill public turns every external user's generation run into a potential machine improvement contribution.

The output target changes fundamentally: instead of saving findings to `docs/retros/` in the repo (which external users can't access), the skill creates a GitHub issue on `mvanhorn/cli-printing-press` with enough information -- including manuscript and source artifacts -- for someone (human or AI agent) to fix the problem using only what's in the issue.

## Requirements

**Analysis**

- R1. The skill runs the full retro methodology (evidence gathering, session mining, classification with cross-API stress tests, scorer accuracy audits, prioritization, work unit creation). Same analytical depth as the current private skill -- findings are pre-analyzed, not raw symptoms. Phases that have repo-local dependencies (e.g., Phase 5.5 work units resolving target file paths via `find`/`grep` in the printing-press repo) must adapt: when the repo is not available, work units describe target components and acceptance criteria without resolved file paths.
- R2. The retro document is distilled into a well-structured GitHub issue body with prioritized findings, work units, and enough context for an AI agent or human to act on it without additional information.

**Artifact Packaging**

- R3. The skill zips the manuscript run directory (`~/printing-press/manuscripts/<api-slug>/<run-id>/`) and uploads it to catbox.moe via their curl API (`curl -F "reqtype=fileupload" -F "fileToUpload=@file.zip" https://catbox.moe/user/api.php`). The returned URL is linked from the GitHub issue body. Manuscripts are typically 24-268KB per run.
- R4. The skill zips the generated CLI source code (`~/printing-press/library/<cli-name>/`) excluding the compiled binary, `vendor/`, and `go.sum`, and uploads it to catbox.moe. The returned URL is linked from the issue body. Source is ~2.2MB uncompressed; zipped Go source compresses well (~200-400KB).
- R5. Before zipping, a defensive secrets/PII scrub runs on both the manuscript and CLI source. This includes: (a) exact-value scanning for the session API key (existing pattern from `secret-protection.md`), and (b) pattern-based scanning for common secret formats: `sk_live_*`, `sk_test_*`, `ghp_*`, `Bearer` tokens in code, JWT-shaped strings (`eyJ...`), and env var assignments containing `SECRET`, `TOKEN`, `KEY`, `PASSWORD`. Conservative patterns with low false-positive risk.

**Output Routing**

- R6. The skill creates a GitHub issue on `mvanhorn/cli-printing-press` via `gh issue create`. The issue body contains the distilled retro analysis with links to the catbox-hosted artifact zips.
- R7. If the current working directory is inside the `cli-printing-press` repo (detected by checking for a known marker file like `cmd/printing-press/main.go`), also save the retro document to `docs/retros/YYYY-MM-DD-<api>-retro.md`. When running from inside the repo, the "Offer to plan" handoff (invoking `/ce:plan`) is retained. For external users, this step is skipped.
- R8. The retro document is always saved to the manuscript proofs directory (`~/printing-press/manuscripts/<api-slug>/<run-id>/proofs/`), regardless of context.

**Graceful Degradation**

- R9. The skill refuses to run if `~/printing-press/manuscripts/` is empty or doesn't exist. Clear message: "No manuscripts found. Run `/printing-press` first to generate a CLI."
- R10. If multiple APIs exist in manuscripts and the user didn't specify which one, the skill asks for clarification (listing available APIs with their most recent run dates).
- R11. If an API has multiple runs, the skill defaults to the most recent run but lets the user choose if they specify a run ID.
- R14. If `gh` is not authenticated or `gh issue create` fails, the skill saves the retro document and artifact zips locally (next to the manuscript proofs), prints the catbox URLs if upload succeeded, and tells the user to file the issue manually with the generated content.
- R15. If catbox.moe upload fails (service down, network issue), the skill still creates the GitHub issue with the retro analysis in the body but notes that artifacts could not be uploaded. The local zips are preserved for the user to attach manually.

**Skill Distribution**

- R12. Move the skill from `.claude/skills/printing-press-retro/` (private) to `skills/printing-press-retro/` (public, packaged with the plugin). Apply the reference file pattern from AGENTS.md to keep the SKILL.md lean -- extract conditional content (issue templates, scrub patterns, work unit formatting) into `references/` files.
- R13. The current private skill is removed after the public skill is verified working. No duplication.

## Success Criteria

- An external user (not a repo contributor) can run `/printing-press-retro` after generating a CLI, and a GitHub issue is created on `mvanhorn/cli-printing-press` with the retro analysis and links to artifact zips hosted on catbox.moe.
- A maintainer or AI agent can read the issue and download the artifacts, understand the findings, and implement fixes using only the information in the issue.
- When run from inside the `cli-printing-press` repo, the retro also saves to `docs/retros/` and offers to plan implementation.
- No secrets or PII appear in the issue body or uploaded artifacts.
- When `gh` auth or catbox upload fails, the skill degrades gracefully with clear instructions for manual filing.

## Scope Boundaries

- The retro methodology's analytical framework (classification, scorer audit protocol, cross-API stress tests) is not being redesigned -- it's being adapted for two contexts (in-repo with file path resolution, external without).
- The skill does not implement fixes. It analyzes and reports.
- The skill does not modify the generation pipeline or the printing-press binary.
- Issue labels, milestones, or project board assignment are out of scope -- just create the issue with a clear title and structured body.
- The skill does not create separate child issues per finding -- one issue per retro run.

## Key Decisions

- **Full analysis on user's side**: The skill runs the complete retro methodology before distilling into an issue. The fixer gets pre-analyzed findings, not raw symptoms.
- **Catbox.moe for artifact hosting**: Zip manuscripts and CLI source, upload via catbox's curl API, link from issue body. No GitHub auth needed for uploads, no file size concerns, simple implementation. Third-party dependency accepted since artifacts are intentionally public.
- **One issue per retro run**: All findings go into one well-structured issue. Work units serve as splitting points.
- **Context-aware behavior**: In-repo runs get local save + offer-to-plan. External runs get issue-only output. Detected via marker file, not origin URL parsing.
- **Layered secrets scrub**: Exact-value scanning (session key) plus conservative pattern matching (sk_live_*, JWT, etc.) before any public upload. Defense in depth for public artifacts.
- **Graceful degradation over hard failures**: If gh or catbox fails, save locally and guide the user to file manually. The retro analysis is never lost.

## Dependencies / Assumptions

- `gh` CLI is authenticated and can create issues on `mvanhorn/cli-printing-press`. Any authenticated GitHub user can create issues on public repos. If auth fails, R14 handles the fallback.
- catbox.moe's API (`https://catbox.moe/user/api.php`) is available. If it fails, R15 handles the fallback.
- `curl` is available on the user's system (standard on macOS/Linux, available on modern Windows).

## Outstanding Questions

### Deferred to Planning

- [Affects R2][Technical] What's the exact GitHub issue body template? The retro markdown format is comprehensive but may need trimming for issue readability. Planning should determine which sections go in the issue body vs the attached retro document.
- [Affects R12][Technical] What adjustments are needed to the skill frontmatter and description for marketplace discovery? The trigger phrases need to work for external users, not just maintainers.
- [Affects R5][Technical] Exact regex patterns for the secret scanner. Planning should define the pattern list and test against real manuscripts for false-positive rate.
- [Affects R1][Technical] Which phases need conditional logic for in-repo vs external context? Phase 5.5 (work units with file path resolution) is identified; planning should audit all 6 phases.

## Next Steps

--> `/ce:plan` for structured implementation planning
