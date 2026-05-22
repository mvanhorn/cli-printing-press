# PII Scrubbing for `/printing-press-amend`

This reference is loaded by Phase 5. The scrub mechanism has two layers — credentials (regex patterns, reused from `/printing-press-retro`) and entities (user-maintained stop-list, specific to amend).

The scrub operates on **temp staging copies** of the artifacts that will leave the local machine — never on the user's original session transcript or the in-progress source code in the managed clone. Source code in `$CLI_DIR` is presumed PII-free by the agent's own restraint (the agent should never have introduced PII into Go source); this is verified as a defense-in-depth check, not as a primary control.

## What gets scrubbed

In priority order (highest leak risk first):

1. **The PR title and body draft** (composed in Phase 6, scrubbed before display in Phase 6's checkpoint)
2. **The per-run plan doc body** at `$PRESS_MANUSCRIPTS/<slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>.md` — this is the artifact reviewers may follow links to from the PR body's Evidence section
3. **The deferred-findings list** at `$PRESS_MANUSCRIPTS/<slug>/<run-id>/proofs/<timestamp>-amend-<cli-name>-deferred.md` — same audience
4. **Any test fixtures or example outputs** newly added to `$CLI_DIR` (defense-in-depth — the agent should not have added PII here, but verify)

For each target, copy to `<path>.pre-pii-scrub` BEFORE scrubbing so the user can audit what changed.

## Layer 1: Credentials (reuse retro patterns)

Run the credential-pattern scan from `skills/printing-press-retro/references/secret-scrubbing.md` — it covers Stripe keys, GitHub PATs/OAuth, bearer tokens, generic API keys, AWS access keys, etc. Point at the same regex set; do not duplicate the patterns here so both skills evolve together.

For Phase 5's purposes, the credential scan's redaction tags (`<REDACTED:bearer-token>`, etc.) become the shape-preserving tokens for credential entities. Same surface, same tag.

**Critical addition for amend** that retro's patterns don't cover by default:
- `Authorization: Bearer ...` headers in hand-rolled API payloads quoted from the session transcript. Retro's `bearer-token` pattern catches the value but only when the prefix is exactly `Bearer ` — verify amend's evidence quotes also normalize to that shape, OR add a broader `Authorization: <scheme> <opaque>` regex specific to amend.
- `Cookie: ...` headers from session-replay payloads. These often contain session IDs that uniquely identify the user even if not technically secret.
- `X-API-Key:` and similar header-based auth shapes.

## Layer 2: Entities (companies, persons, custom stop-list)

Read the user's stop-list at `~/.printing-press/amend-config.yaml`. If the file doesn't exist, create a default with a starter list and a comment explaining the format:

```yaml
# ~/.printing-press/amend-config.yaml
# User-maintained stop-list for /printing-press-amend's PII scrub.
# Add company and person names that should be replaced with shape-preserving
# tokens before any artifact leaves the local machine.

stoplist:
  companies:
    # - "Esper Labs"
    # - "Acme Corp"
  people:
    # - "Matt Van Horn"
    # - "Trevin Chow"
  emails:
    # Domain-level scrubbing — any email at this domain becomes <email-N>
    # - "esperlabs.ai"
    # - "company.com"

# Behavior knobs
behavior:
  # When true, also flag capitalized non-stop-listed strings for user review
  # before the PR draft is shown (defense against first-mention leaks).
  prompt_unrecognized_capitalized: true
```

When the file exists, read it. Validate file mode (warn if world-writable; abort if owned by another user — symlink attack surface).

For each artifact, replace stop-listed values with shape-preserving tokens:

| Original shape | Token |
|---|---|
| Company name | `<company-1>`, `<company-2>`, ... |
| Person name | `<person-1>`, `<person-2>`, ... |
| Email address | `<email-1>`, `<email-2>`, ... |

Same source value gets the same token across the run; distinct sources get distinct tokens. Track the mapping in a per-run scrub report (NOT committed; written to `$PRESS_MANUSCRIPTS/<slug>/<run-id>/scrub-report.json` for the user's audit).

## Layer 3: Defense against first-mention leaks

The stop-list is by definition incomplete on first encounter with a new entity. To catch this:

1. After Layers 1+2, walk each artifact again and find capitalized multi-word phrases that look like proper nouns (e.g. matches `\b[A-Z][a-z]+ [A-Z][a-z]+\b`) and were NOT replaced by Layer 2.
2. Filter out a known-safe allowlist (`GitHub`, `Slack`, `Linear`, `Claude`, `Anthropic`, common HTTP-shape words, the API/CLI vendor name, etc.).
3. Surface remaining candidates inline before the Phase 6 PR-draft display:

   > "Found unrecognized capitalized phrase: `Esper Labs` (appears 3 times in plan doc, 1 time in PR body draft). Add to stop-list and scrub, or accept as legitimate?"

   Options: scrub (add to stop-list), accept (don't scrub), show context (display surrounding lines).

This isn't perfect — uncapitalized PII (lowercase email handles, internal codenames) still slips through — but it catches the most common failure mode without forcing the user to maintain an exhaustive stop-list.

## Defense-in-depth: Go source check

Walk every `*.go` file in `$CLI_DIR` and scan for stop-list matches. If any match is found, treat as a hard error:

> "BLOCKING: PII pattern matched Go source at `<file>:<line>`. The skill should not have introduced PII into Go source — please review the diff in `$CLI_DIR/<file>` and resolve before continuing. The patch will not proceed to Phase 6 until this clears."

Do NOT auto-modify Go source. Pause and require user resolution.

## Output

Phase 5 emits to Phase 6:

```yaml
scrub_report_path: $PRESS_MANUSCRIPTS/<slug>/<run-id>/scrub-report.json
artifacts_scrubbed:
  - path: <plan-doc-path>
    tokens_replaced: 7
    backup: <plan-doc-path>.pre-pii-scrub
  - ...
unrecognized_phrases: []           # may be present if user accepted some
go_source_check: clean             # or [list of blocking matches]
```

If `go_source_check` is non-empty, Phase 6 cannot proceed.
