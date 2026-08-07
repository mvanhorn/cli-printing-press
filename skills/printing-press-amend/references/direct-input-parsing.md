# Direct-Input Parsing — Ask Capture for `/printing-press-amend`

**Scope:** This reference applies when `MODE=direct` (Phase 0 detected user-supplied asks in the slash-command prompt) or when running the direct-input half of `MODE=both`. For session-friction mode (`MODE=dogfood`), see `transcript-parsing.md` instead — that mode walks a transcript and never reads the prompt body.

This reference is loaded by Phase 1's `### 1b. Direct-input mode` sub-section of `printing-press-amend`. It defines how the agent parses the user's verbatim asks in the slash-command invocation and converts them to the typed finding list that Phase 2 consumes.

## Input

The agent reads two sources:

1. **The slash-command prompt body** — everything the user typed after `/printing-press-amend ...` in the invocation that fired this skill. This is the primary signal.
2. **The immediate agent-message turn** — the user's prior turn that fired the skill (when applicable). Sometimes the user names asks in conversational context just before invoking the skill; that context is in-scope here.

Do NOT read the conversation transcript beyond the immediate invocation turn — that's `MODE=dogfood` behavior. Direct-input mode trusts the user's explicit prompt and does not infer asks from earlier conversational drift.

## Parsing rubric — verbs to finding kinds

Map each ask in the prompt to one finding using the following rubric. When a single prompt contains multiple asks (which is the common case), produce one finding per ask.

| User phrasing | `kind` | `classification` | Notes |
|---------------|--------|------------------|-------|
| "rename X to Y", "call it X instead of Y", "should be named X not Y" | `rename` | `feature` | Renaming a command, subcommand, flag, or output label. Capture both the old and new names in `evidence`. |
| "add command X", "add subcommand X", "add a Y subcommand" | `add-command` | `feature` | New top-level or nested Cobra command. |
| "add feed <url>", "add these feeds: <url>, <url>", enumerated URLs | `add-feed` | `feature` | One finding per URL. `evidence` carries the full URL. |
| "add endpoint <path>", "add the /v1/foo endpoint", explicit API path | `add-endpoint` | `feature` | Hand-named endpoint to wrap. `evidence` carries the path. |
| "fix X", "X is broken", "X returns null", "X errors out", "broken: X" | `fix-bug` | `bug` | Behavior is wrong in the published CLI. |
| "sniff for new APIs", "find new endpoints", "discover more", "what else is there in <site>" | `sniff` | `feature` | Triggers the sniff subroutine (`### 1b.i`); produces zero-to-many `add-endpoint` findings with `provenance: sniff`. |

When a phrase fits multiple kinds (e.g., "add the X feed" — `add-feed` or `add-command`?), prefer the more specific kind based on context: a URL → `add-feed`; a noun like "command" or "subcommand" → `add-command`; an API path with a method → `add-endpoint`.

## Finding shape

Each finding emitted by 1b carries the same fields as 1a findings, with one new field (`provenance`):

```yaml
- id: F<n>                    # F1, F2, ... — continues numbering when MODE=both
  kind: <rename|add-command|add-feed|add-endpoint|fix-bug>
  category: <free-text categorical label, e.g. "command-rename", "feed-add">
  classification: <bug|feature>
  evidence: "<verbatim user phrasing>"
  target_cli: <slug>-pp-cli
  rationale: "<one-line agent summary of what this finding means>"
  provenance: user-ask        # or "sniff" for sniff-derived findings
```

The `evidence` field carries the user's verbatim phrasing — not the agent's paraphrase — so the Phase 3 scope-confirmation modal shows the user exactly what they wrote. This makes mis-classification recoverable: the user sees their own words and can correct the agent's tier or kind at the U4 modal.

## Target-CLI resolution

When the user names the CLI inside the prompt, extract it via regex (in order):

1. `<slug>-pp-cli` literal (e.g., `digg-pp-cli`)
2. `the <slug> CLI` or `the <slug> cli` (e.g., `the digg CLI` → `digg-pp-cli`)
3. `for <slug>` when followed by an ask verb (e.g., `for digg, add feed ...`)
4. `<slug>` alone when the prompt has only one short-name candidate

If no slug is named anywhere in the prompt, fall back to Phase 0 auto-detection: list recently-touched `<slug>-pp-cli` invocations in the immediate invocation turn, propose the most-touched, and confirm with `AskUserQuestion`. If even auto-detect can't resolve, ask the user.

Once resolved, accept any of the three forms (short name, full name, path) per origin R4 — the resolution rules are identical to 1a step 4-5.

## Edge cases

**Multi-CLI asks** — Out of scope for v0.2. When the prompt names two or more distinct CLIs ("amend foo-pp-cli and bar-pp-cli"), ask the user to pick one and re-invoke for the other:

> "This prompt names multiple CLIs (foo-pp-cli, bar-pp-cli). v0.2 amend handles one CLI per run. Which one should I scope to first?"

**Ambiguous verbs** — "update X", "improve X", "make X better" without further specifics trigger an `AskUserQuestion` clarification rather than a guess. Offer two to three concrete kind options based on the surrounding context.

**Bare URLs without context** — A URL in the prompt with no surrounding verb ("https://example.com/feed/x" alone) triggers a clarifying ask: is this a feed to add, an endpoint to wrap, or a sniff target?

**Conflicting kinds in one ask** — "rename X to Y AND fix the bug in Y" splits into two findings: one `rename`, one `fix-bug`. Findings stay atomic; don't merge them into one mixed-kind entry.

**Combined-mode merging (MODE=both)** — When 1b runs after 1a, 1b's finding IDs continue numbering from where 1a left off (1a emits F1..Fn; 1b emits F(n+1)..). Findings keep their own `provenance` regardless of which sub-section produced them. The Phase 3 modal groups by tier, not by mode — the user sees one merged list.

## Output

1b emits the same structured finding list shape as 1a. Phase 2 consumes the list without branching on `provenance`. Sniff findings (when present) are produced by the `### 1b.i` subroutine and appended to this same list before handoff.
