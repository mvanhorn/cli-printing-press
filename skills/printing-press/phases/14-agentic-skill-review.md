## Phase 4.8: Agentic SKILL Review

**Runs after shipcheck, before [Phase 5](18-dogfood-testing.md).** `verify-skill` ([Phase 4](12-shipcheck.md)) is a mechanical check — it catches wrong flags on wrong commands, undeclared flags, and positional-arg count mismatches. It cannot catch **semantic** issues that only a reader notices:

- A trigger phrase promises behavior the CLI doesn't have ("plan dinners for the week" when there's no `meal-plan suggest`, only manual `meal-plan set`)
- A novel-feature description says the feature does X; the actual command does Y
- The AuthNarrative mentions `auth login --chrome` when the CLI's auth subcommands are only `set-token`/`logout`/`status`
- Novel features shipped as stubs aren't labeled as such in the SKILL (contradicts [Phase 1.5](08-ecosystem-absorb-gate.md) stub-marking rule)
- Recipes/worked examples produce output that doesn't match their prose claims
- Trigger phrases sound agent-natural or sound like marketing copy

### Dispatch

Use the Agent tool (general-purpose or a dedicated reviewer) with this prompt contract:

> Review the SKILL.md at `$CLI_WORK_DIR/SKILL.md` against the shipped CLI. You have these ground-truth sources:
>
> - `<cli> --help` output — enumerate it recursively if needed.
> - The absorb manifest in `$RESEARCH_DIR/<stamp>-feat-<api>-pp-cli-absorb-manifest.md`.
> - The `research.json` `novel_features` (planned) and `novel_features_built` (verified) fields.
> - The README at `$CLI_WORK_DIR/README.md`.
>
> For each of these semantic checks, report findings under 50 words each:
>
> 1. **Trigger phrases match capabilities.** Does every trigger phrase in the SKILL's description frontmatter correspond to something the CLI can actually do? Flag phrases that imply missing capabilities.
> 2. **Verified-set alignment.** The SKILL's "Unique Capabilities" commands must exactly match `novel_features_built` from research.json. Planned-only features from `novel_features` must not appear there after dogfood sync. Any extra or missing command is a finding.
> 3. **Novel-feature descriptions match commands.** For each feature in the "Unique Capabilities" section, run `<cli> <command> --help` and verify the description matches the actual behavior. Mismatches are findings.
> 4. **Stub/gated disclosure.** If a feature that remains in `novel_features_built` is intentionally stubbed, CF-gated, unavailable without external setup, or returns a known-gap response, the SKILL must label that limitation where an agent decides whether to use the command. Unlabeled limitations are findings.
> 5. **Auth narrative accuracy.** Read the auth section. Does every `auth login/set-token/status` invocation mentioned actually exist on the CLI? Does the narrative match the CLI's auth type (api_key vs cookie vs session_handshake)?
> 6. **Recipe output claims.** For the worked examples, does the prose claim match what the command actually produces? (Not the exact output — the shape and intent.)
> 7. **Marketing-copy smell.** Does the SKILL read like ad copy ("comprehensive", "seamless", "powerful") instead of concrete capability descriptions? Those phrases are findings.
>
> Return a list of findings. For each: check name, severity (error/warning), line number, one-sentence fix. If SKILL passes all seven checks, return "PASS — no findings."

**Description source-of-truth fix path.** When a finding says a novel-feature
description, capability summary, or surrounding narrative content is inaccurate,
fix the source text in `$RESEARCH_DIR/research.json`
(`novel_features[].description`, `novel_features[].narrative`, or the matching
`novel_features_built` entry) and regenerate/sync from there. Do not patch only
README.md or SKILL.md for content-level description fixes: the same description
fans out to README "Unique Features", SKILL "Unique Capabilities",
`internal/cli/root.go` Highlights, `internal/cli/which.go`,
`internal/mcp/tools.go`, and `.printing-press.json`. Flag, command, auth,
example, and structural findings that are not generated from research.json keep
their current local fix path.

### Gate

- If the reviewer returns PASS, proceed to [Phase 5](18-dogfood-testing.md).
- If the reviewer returns findings of severity `error`, fix them before [Phase 5](18-dogfood-testing.md). Same fix-now contract as other shipcheck findings.
- If the reviewer returns only `warning` findings, surface them to the user and proceed if they approve.

### Why agentic vs template-only

A template-level check would require every possible semantic mismatch to be pattern-matchable against source. Many aren't — "does this trigger phrase correspond to what the CLI does" is an LLM-shaped question. Accept the token cost for the catch.

### Known blind spots

The agent can't verify runtime behavior without running commands; stick to help-text and source-based claims. For runtime-behavior claims (e.g., "returns 5 matching recipes"), [Phase 5](18-dogfood-testing.md) dogfood is the right gate.


Next: phases/15-readme-skill-agents-correctness-audit.md
